package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestWordPressSetupServiceIsolationAndSecrets(t *testing.T) {
	payload := []byte(`{"password":"secret-only-on-stdin","root":"/home/litecp/example.com/public_html"}`)
	command, err := wordPressSetupCommand(context.Background(), "/home/litecp/example.com/public_html", "/home/litecp/example.com", "/usr/bin/php8.4", 1201, 33, payload)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(command.Args, "\n")
	for _, required := range []string{"systemd-run", "--wait", "--pipe", "--uid=1201", "--gid=33", "--property=NoNewPrivileges=yes", "--property=ProtectSystem=strict", "--property=ReadWritePaths=/home/litecp/example.com", "--property=RuntimeMaxSec=180"} {
		if !strings.Contains(args, required) {
			t.Fatalf("missing isolation option %s", required)
		}
	}
	if strings.Contains(args, "secret-only-on-stdin") {
		t.Fatal("password leaked into command arguments")
	}
	stdin, err := io.ReadAll(command.Stdin)
	if err != nil || string(stdin) != string(payload) {
		t.Fatal("setup credentials not forwarded through stdin")
	}
	program := command.Args[len(command.Args)-1]
	if strings.ReplaceAll(program, "$$", "$") != wordPressSetupScript {
		t.Fatal("PHP program was changed by systemd environment escaping")
	}
}

func TestWordPressSetupRejectsRootIdentity(t *testing.T) {
	for _, identity := range [][2]int{{0, 0}, {0, 33}, {999, 33}, {1201, 0}} {
		if _, err := wordPressSetupCommand(context.Background(), "/home/litecp/example.com/public_html", "/home/litecp/example.com", "/usr/bin/php8.4", identity[0], identity[1], nil); err == nil {
			t.Fatalf("accepted invalid identity %v", identity)
		}
	}
}
