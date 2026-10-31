package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type mailDNSResolver interface {
	LookupHost(context.Context, string) ([]string, error)
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupTXT(context.Context, string) ([]string, error)
}
type mailDNSCheck struct {
	DNSRecord
	Status  string   `json:"status"`
	Actual  []string `json:"actual"`
	Message string   `json:"message"`
}

func verifyMailDNS(ctx context.Context, records []DNSRecord, resolver mailDNSResolver) []mailDNSCheck {
	checks := make([]mailDNSCheck, 0, len(records))
	for _, record := range records {
		check := mailDNSCheck{DNSRecord: record, Status: "missing", Actual: []string{}, Message: "Record not found."}
		query, cancel := context.WithTimeout(ctx, 4*time.Second)
		var values []string
		var err error
		switch record.Type {
		case "A":
			values, err = resolver.LookupHost(query, record.Name)
		case "MX":
			var entries []*net.MX
			entries, err = resolver.LookupMX(query, record.Name)
			for _, mx := range entries {
				values = append(values, fmt.Sprintf("%d %s.", mx.Pref, strings.TrimSuffix(strings.ToLower(mx.Host), ".")))
			}
		case "TXT":
			values, err = resolver.LookupTXT(query, record.Name)
		default:
			err = fmt.Errorf("unsupported record type")
		}
		cancel()
		if err != nil {
			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
				check.Status = "error"
				check.Message = "DNS lookup failed or timed out. Try again."
			}
			checks = append(checks, check)
			continue
		}
		for _, value := range values {
			if record.Type == "A" {
				ip := net.ParseIP(value)
				if ip == nil || ip.To4() == nil {
					continue
				}
			}
			if record.Type == "TXT" {
				prefix := strings.SplitN(record.Value, " ", 2)[0]
				prefix = strings.TrimSuffix(prefix, ";")
				if !strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
					continue
				}
			}
			check.Actual = append(check.Actual, value)
		}
		if len(check.Actual) > 0 {
			check.Status = "different"
			check.Message = "Record exists, but differs from the expected value."
		}
		for _, value := range check.Actual {
			expected := strings.TrimSpace(record.Value)
			actual := strings.TrimSpace(value)
			if record.Type == "MX" {
				expected = strings.ToLower(expected)
			}
			if actual == expected {
				check.Status = "matching"
				check.Message = "Expected record found."
				break
			}
		}
		if record.Type == "TXT" && strings.HasPrefix(record.Value, "v=spf1") && len(check.Actual) > 1 {
			check.Status = "different"
			check.Message = "Multiple SPF records found. Merge them into one SPF record."
		}
		checks = append(checks, check)
	}
	return checks
}
