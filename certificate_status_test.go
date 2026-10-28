package main

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestCertificateStatusChecksDomainAndValidity(t *testing.T) {
	now := time.Now()
	cert := &x509.Certificate{DNSNames: []string{"example.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	if !certificateMatchesSite(cert, "example.test", now) {
		t.Fatal("valid certificate rejected")
	}
	if certificateMatchesSite(cert, "other.test", now) {
		t.Fatal("wrong hostname accepted")
	}
	cert.NotBefore = now.Add(time.Minute)
	if certificateMatchesSite(cert, "example.test", now) {
		t.Fatal("future certificate accepted")
	}
	cert.NotBefore = now.Add(-time.Hour)
	cert.NotAfter = now
	if certificateMatchesSite(cert, "example.test", now) {
		t.Fatal("expired certificate accepted")
	}
	if certificateMatchesSite(nil, "example.test", now) {
		t.Fatal("nil certificate accepted")
	}
}
