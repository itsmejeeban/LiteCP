package main

import "testing"

func TestLiteCPImmutableAccountIdentifiers(t *testing.T) {
	for _, id := range []string{"-vYbtz2ZL38", "_leading_underscore", "normal123"} {
		if !validLiveAccount("litecp", id) {
			t.Fatalf("valid LiteCP identity rejected: %q", id)
		}
	}
	for _, id := range []string{"../escape", "-option;command", "", "account name"} {
		if validLiveAccount("litecp", id) {
			t.Fatalf("invalid identity accepted: %q", id)
		}
	}
	for _, platform := range []string{"cpanel", "plesk"} {
		if validLiveAccount(platform, "-option") || validLiveAccount(platform, "_account") {
			t.Fatal("non-LiteCP validation weakened")
		}
	}
}
