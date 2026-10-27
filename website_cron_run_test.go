package main

import (
	"strings"
	"testing"
)

func TestCronTestOutputIsBounded(t *testing.T) {
	output := &cronTestOutput{}
	input := []byte(strings.Repeat("x", 100000))
	n, err := output.Write(input)
	if err != nil || n != len(input) || len(output.data) != 65536 || !output.truncated {
		t.Fatal("cron output was not bounded")
	}
	if _, err = output.Write([]byte("more")); err != nil || len(output.data) != 65536 {
		t.Fatal("output limit changed")
	}
}
