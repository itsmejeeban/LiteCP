package main

import (
	"strings"
	"testing"
)

func TestHostnameAddressPreservesUnrelatedHosts(t *testing.T) {
	input := "# system hosts\n127.0.0.1 localhost htws.hostingtom.org htws # local\n::1 localhost6 htws.hostingtom.org htws\n192.0.2.8 other.example other\n"
	output, err := hostnameAddressContent([]byte(input), "htws.hostingtom.org", "95.217.167.243")
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, expected := range []string{"# system hosts", "127.0.0.1\tlocalhost # local", "::1\tlocalhost6", "192.0.2.8 other.example other", "95.217.167.243\thtws.hostingtom.org htws"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Missing preserved or updated entry %q", expected)
		}
	}
	addresses := hostnameAddressEntries(output, "htws.hostingtom.org")
	if len(addresses) != 1 || addresses[0] != "95.217.167.243" {
		t.Fatal("Conflicting hostname mapping retained", addresses)
	}
	second, err := hostnameAddressContent(output, "htws.hostingtom.org", "95.217.167.243")
	if err != nil || len(hostnameAddressEntries(second, "htws.hostingtom.org")) != 1 {
		t.Fatal("Repeat save duplicated mappings")
	}
}

func TestHostnameAddressRejectsUnsafeInput(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "0.0.0.0", "169.254.1.1", "224.1.1.1", "bad", "1.2.3.4\nattacker"} {
		if _, err := hostnameAddressContent(nil, "htws.example.com", ip); err == nil {
			t.Fatalf("Unsafe IP accepted %q", ip)
		}
	}
	if _, err := hostnameAddressContent(nil, "bad\nhost", "192.0.2.1"); err == nil {
		t.Fatal("Unsafe hostname accepted")
	}
	output, err := hostnameAddressContent(nil, "htws.example.com", "2001:db8::1")
	if err != nil || !strings.Contains(string(output), "2001:db8::1") {
		t.Fatal("IPv6 address rejected", err)
	}
}
