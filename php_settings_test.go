package main

import (
	"strings"
	"testing"
)

func TestCustomPHPDirectives(t *testing.T) {
	settings, err := mergeCustomPHPSettings(map[string]string{"memory_limit": "512M"}, "; custom settings\nmax_file_uploads = 50\nsession.cookie_httponly = On\n")
	if err != nil || settings["max_file_uploads"] != "50" || validatePHPSettings(settings) != nil {
		t.Fatalf("valid custom settings rejected: %v", err)
	}
	if !strings.Contains(phpPoolSettings(settings), "php_admin_value[max_file_uploads] = 50\n") {
		t.Fatal("custom directive missing from pool")
	}
	for _, custom := range []string{
		"open_basedir = /", "auto_prepend_file = /tmp/code.php", "extension = test.so",
		"max_file_uploads = 50\nmax_file_uploads = 60", "memory_limit = 512M",
		"session.cookie_httponly = On; unsafe", "[other-pool]", "unknown = 1",
		"default_charset = UTF-8\nphp_admin_value[open_basedir] = /",
	} {
		if _, err := mergeCustomPHPSettings(nil, custom); err == nil {
			t.Fatalf("unsafe custom settings accepted: %s", custom)
		}
	}
}

func TestPHPSettingsValidationAndPoolScope(t *testing.T) {
	for _, settings := range []map[string]string{
		{"open_basedir": "/"}, {"memory_limit": "256M\nphp_admin_value[open_basedir] = /"},
		{"upload_max_filesize": "512M", "post_max_size": "128M"}, {"display_errors": "anything"},
		{"max_execution_time": "-1"}, {"date.timezone": "invalid/zone"},
	} {
		if validatePHPSettings(settings) == nil {
			t.Fatalf("accepted unsafe settings: %v", settings)
		}
	}
	settings := map[string]string{"memory_limit": "512M", "date.timezone": "Asia/Kolkata", "display_errors": "Off"}
	if err := validatePHPSettings(settings); err != nil {
		t.Fatal(err)
	}
	output := phpPoolSettings(settings)
	if !strings.Contains(output, "php_admin_value[memory_limit] = 512M\n") || strings.Contains(output, "open_basedir") {
		t.Fatal(output)
	}
	if phpPoolSettings(nil) != "" {
		t.Fatal("existing websites must retain their pool configuration")
	}
}
