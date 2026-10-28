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

func TestCronTestDropsToWebsiteIdentityBeforeShell(t *testing.T) {
	args := strings.Join(cronTestArguments(Site{RuntimeUID:1934344478,RuntimeGID:1934344478})," ")
	if !strings.Contains(args,"-- /usr/bin/setpriv --reuid=1934344478 --regid=1934344478 --clear-groups --no-new-privs /bin/sh") { t.Fatal("website identity drop missing") }
	for _, property := range []string{"RuntimeMaxSec=30","MemoryMax=256M","TasksMax=32","NoNewPrivileges=yes"} { if !strings.Contains(args,property) {t.Fatal("resource protection missing: "+property)} }
}
