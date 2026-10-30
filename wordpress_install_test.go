package main

import (
	"strings"
	"testing"
)

func TestWordPressInstallValidation(t *testing.T) {
	input := CreateSiteInput{AppType: "wordpress", WordPressMode: "install", WordPressUsername: "owner", WordPressPassword: "Strong-password-123", WordPressEmail: "owner@example.test"}
	if err := validateWordPressInstall(input); err != nil {
		t.Fatal(err)
	}
	input.WordPressPassword = "short"
	if err := validateWordPressInstall(input); err == nil {
		t.Fatal("weak password accepted")
	}
	input.WordPressMode = "migrate"
	if err := validateWordPressInstall(input); err != nil {
		t.Fatal("migration requires install credentials", err)
	}
}
func TestWordPressPHPConfigQuoting(t *testing.T) {
	value := phpWordPressString("secret'\\$value")
	if !strings.HasPrefix(value, "'") || !strings.HasSuffix(value, "'") || !strings.Contains(value, "\\'") || !strings.Contains(value, "\\\\") {
		t.Fatalf("unsafe PHP literal: %q", value)
	}
}
