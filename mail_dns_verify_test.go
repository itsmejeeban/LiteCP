package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeMailDNS struct {
	host []string
	mx   []*net.MX
	txt  []string
	err  error
}

func (f fakeMailDNS) LookupHost(context.Context, string) ([]string, error) { return f.host, f.err }
func (f fakeMailDNS) LookupMX(context.Context, string) ([]*net.MX, error)  { return f.mx, f.err }
func (f fakeMailDNS) LookupTXT(context.Context, string) ([]string, error)  { return f.txt, f.err }
func TestMailDNSVerification(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		record   DNSRecord
		resolver fakeMailDNS
		status   string
	}{
		{"matching A", DNSRecord{Type: "A", Name: "mail.test", Value: "192.0.2.1"}, fakeMailDNS{host: []string{"192.0.2.1", "::1"}}, "matching"},
		{"different A", DNSRecord{Type: "A", Name: "mail.test", Value: "192.0.2.1"}, fakeMailDNS{host: []string{"192.0.2.2"}}, "different"},
		{"missing SPF among unrelated TXT", DNSRecord{Type: "TXT", Name: "test", Value: "v=spf1 mx -all"}, fakeMailDNS{txt: []string{"site-verification=abc"}}, "missing"},
		{"duplicate SPF", DNSRecord{Type: "TXT", Name: "test", Value: "v=spf1 mx -all"}, fakeMailDNS{txt: []string{"v=spf1 mx -all", "v=spf1 -all"}}, "different"},
		{"MX normalization", DNSRecord{Type: "MX", Name: "test", Value: "10 MAIL.TEST."}, fakeMailDNS{mx: []*net.MX{{Host: "mail.test", Pref: 10}}}, "matching"},
		{"NXDOMAIN", DNSRecord{Type: "TXT", Name: "test", Value: "v=DKIM1; p=ABC"}, fakeMailDNS{err: &net.DNSError{IsNotFound: true}}, "missing"},
		{"lookup failure", DNSRecord{Type: "TXT", Name: "test", Value: "v=DKIM1; p=ABC"}, fakeMailDNS{err: errors.New("timeout")}, "error"},
		{"DKIM key case preserved", DNSRecord{Type: "TXT", Name: "test", Value: "v=DKIM1; p=ABC"}, fakeMailDNS{txt: []string{"v=DKIM1; p=abc"}}, "different"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := verifyMailDNS(ctx, []DNSRecord{tc.record}, tc.resolver)
			if len(result) != 1 || result[0].Status != tc.status {
				t.Fatal(result)
			}
		})
	}
}
