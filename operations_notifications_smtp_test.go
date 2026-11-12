package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOperationsEmailMultipleRecipientsSMTP(t *testing.T) {
	for _, rejectSecond := range []bool{false, true} {
		name := "all recipients accepted"
		if rejectSecond {
			name = "second recipient rejected before data"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			type smtpResult struct {
				recipients []string
				dataSeen   bool
				message    string
			}
			result := make(chan smtpResult, 1)
			go func() {
				var received smtpResult
				defer func() { result <- received }()
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = fmt.Fprint(connection, "220 localhost audit SMTP\r\n")
				reader := bufio.NewReader(connection)
				inData := false
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if inData {
						if line != ".\r\n" {
							received.message += line
							continue
						}
						inData = false
						_, _ = fmt.Fprint(connection, "250 queued\r\n")
						continue
					}
					switch {
					case strings.HasPrefix(line, "RCPT TO:"):
						received.recipients = append(received.recipients, strings.TrimSpace(strings.TrimPrefix(line, "RCPT TO:")))
						if rejectSecond && len(received.recipients) == 2 {
							_, _ = fmt.Fprint(connection, "550 recipient unavailable\r\n")
						} else {
							_, _ = fmt.Fprint(connection, "250 recipient accepted\r\n")
						}
					case line == "DATA\r\n":
						received.dataSeen = true
						inData = true
						_, _ = fmt.Fprint(connection, "354 end with dot\r\n")
					case line == "QUIT\r\n":
						_, _ = fmt.Fprint(connection, "221 goodbye\r\n")
						return
					default:
						_, _ = fmt.Fprint(connection, "250 ok\r\n")
					}
				}
			}()
			settings := operationsSettings{
				RetentionDays: 14, EmailEnabled: true, EmailFrom: "alerts@example.test",
				EmailRecipients: []string{"first@example.test", "FIRST@example.test", "second@example.test"},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = submitOperationsEmail(ctx, settings, []operationsAlert{{Title: "Audit notification", Detail: "Loopback test only", Severity: "warning"}}, listener.Addr().String())
			if (err != nil) != rejectSecond {
				t.Fatalf("unexpected submission result: %v", err)
			}
			select {
			case received := <-result:
				expected := []string{"<first@example.test>", "<second@example.test>"}
				if !reflect.DeepEqual(received.recipients, expected) {
					t.Fatalf("duplicate or missing SMTP recipients: %v", received.recipients)
				}
				if rejectSecond {
					if received.dataSeen || received.message != "" {
						t.Fatal("SMTP DATA sent after a recipient was rejected")
					}
				} else if !received.dataSeen || !strings.Contains(received.message, "Loopback test only") || !strings.Contains(received.message, "To: undisclosed-recipients:;") {
					t.Fatal("SMTP sink did not receive the complete notification with recipient privacy")
				}
			case <-ctx.Done():
				t.Fatal("SMTP sink did not finish")
			}
		})
	}
}
