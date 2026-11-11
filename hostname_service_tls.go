package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hostnameTLSService struct {
	Name      string     `json:"name"`
	Endpoint  string     `json:"endpoint"`
	Status    string     `json:"status"`
	Detail    string     `json:"detail,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func checkHostnameTLS(ctx context.Context, host, address, protocol string) (*time.Time, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if protocol != "" {
		if err := startHostnameTLS(conn, protocol); err != nil {
			return nil, true, err
		}
	}
	secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, true, err
	}
	expiry := secure.ConnectionState().PeerCertificates[0].NotAfter
	return &expiry, true, nil
}

func startHostnameTLS(conn net.Conn, protocol string) error {
	reader := textproto.NewConn(struct {
		io.Reader
		io.Writer
		io.Closer
	}{io.LimitReader(conn, 16<<10), conn, conn})
	if _, _, err := reader.ReadResponse(220); err != nil {
		return err
	}
	commands := []struct {
		line string
		code int
	}{{"AUTH TLS", 234}}
	if protocol == "smtp" {
		commands = []struct {
			line string
			code int
		}{{"EHLO localhost", 250}, {"STARTTLS", 220}}
	}
	for _, command := range commands {
		if err := reader.PrintfLine("%s", command.line); err != nil {
			return err
		}
		if _, _, err := reader.ReadResponse(command.code); err != nil {
			return err
		}
	}
	return nil
}

func hostnameTLSServiceStatuses(ctx context.Context, host string, panelPort int) []hostnameTLSService {
	mailHost := host
	if output, err := runCommand(ctx, 2*time.Second, "postconf", "-h", "myhostname"); err == nil {
		configured := strings.ToLower(strings.TrimSpace(output))
		if validPanelSSLHostname(configured) && !strings.HasSuffix(configured, ".localdomain") && !strings.HasSuffix(configured, ".local") {
			mailHost = configured
		}
	}
	targets := []struct {
		name, host, protocol string
		port                 int
	}{
		{"LiteCP panel", host, "", panelPort},
		{"FTPS", host, "ftp", 21},
		{"SMTP", mailHost, "smtp", 25},
		{"SMTP submission", mailHost, "smtp", 587},
		{"SMTPS", mailHost, "", 465},
		{"IMAPS", mailHost, "", 993},
		{"POP3S", mailHost, "", 995},
	}
	results := make([]hostnameTLSService, len(targets))
	var checks sync.WaitGroup
	for index, target := range targets {
		result := hostnameTLSService{Name: target.name, Endpoint: fmt.Sprintf("%s:%d", target.host, target.port), Status: "unknown"}
		results[index] = result
		if !validPanelSSLHostname(target.host) {
			continue
		}
		checks.Add(1)
		go func(index int, result hostnameTLSService, host string, port int, protocol string) {
			defer checks.Done()
			expiry, reachable, err := checkHostnameTLS(ctx, host, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), protocol)
			switch {
			case err == nil:
				result.Status, result.ExpiresAt = "trusted", expiry
			case !reachable:
				result.Status, result.Detail = "unavailable", "No reachable local listener."
			default:
				result.Status, result.Detail = "untrusted", err.Error()
			}
			results[index] = result
		}(index, result, target.host, target.port, target.protocol)
	}
	checks.Wait()
	return results
}
