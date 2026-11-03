package main

import (
	"io"
	"strings"
	"testing"
)

func TestScopedMigrationSQLPreamble(t *testing.T) {
	input := "-- dump\nCREATE DATABASE IF NOT EXISTS `old`;\nUSE `old`;\nCREATE TABLE sample (value TEXT);\nINSERT INTO sample VALUES ('USE old;');\nUSE `other`;\n"
	r := scopedMigrationSQL(strings.NewReader(input), "new_target")
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "CREATE DATABASE") || strings.Contains(s, "USE `old`") || !strings.Contains(s, "USE `new_target`;") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "VALUES ('USE old;')") || !strings.Contains(s, "USE `other`;") {
		t.Fatal("data or later directives changed")
	}
}
