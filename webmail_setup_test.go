package main

import (
	"path/filepath"
	"testing"
)

func TestValidateWebmailSetup(t *testing.T) {
	input := webmailSetupInput{Hostname: "mail.example.com", Port: 8444, Certificate: filepath.VolumeName(t.TempDir()) + "/tls/fullchain.pem", PrivateKey: filepath.VolumeName(t.TempDir()) + "/tls/privkey.pem"}
	// Linux is the deployment target; Windows paths use backslashes, which are deliberately forbidden.
	input.Certificate = filepath.ToSlash(input.Certificate)
	input.PrivateKey = filepath.ToSlash(input.PrivateKey)
	if err := validateWebmailSetup(input); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*webmailSetupInput){func(v *webmailSetupInput) { v.Hostname = "litecp" }, func(v *webmailSetupInput) { v.Hostname = "mail.example.com;evil" }, func(v *webmailSetupInput) { v.Port = 22 }, func(v *webmailSetupInput) { v.Port = 65536 }, func(v *webmailSetupInput) { v.Certificate = "relative.pem" }, func(v *webmailSetupInput) { v.PrivateKey = "/etc/key';evil" }, func(v *webmailSetupInput) { v.Certificate = "/etc/cert\ninvalid" }} {
		invalid := input
		change(&invalid)
		if validateWebmailSetup(invalid) == nil {
			t.Fatalf("accepted unsafe configuration: %#v", invalid)
		}
	}
}
