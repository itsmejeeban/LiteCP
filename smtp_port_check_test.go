package main

import (
	"net"
	"testing"
)

func TestSMTPDiagnosticRejectsPrivateDestinations(t *testing.T) {
	for _, address:=range []string{"127.0.0.1","10.0.0.1","192.168.1.1","169.254.169.254","::1","fe80::1","fd00::1","0.0.0.0","224.0.0.1"} {
		if publicSMTPAddress(net.ParseIP(address)){t.Fatalf("nonpublic destination accepted: %s",address)}
	}
	if !publicSMTPAddress(net.ParseIP("8.8.8.8")){t.Fatal("public address rejected")}
}
