package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWordPressSetupServiceIsolationAndSecrets(t *testing.T) {
	payload := []byte(`{"password":"secret-only-on-stdin","root":"/home/litecp/example.com/public_html"}`)
	command, err := wordPressSetupCommand(context.Background(), "/home/litecp/example.com/public_html", "/home/litecp/example.com", "/usr/bin/php8.4", 1201, 33, payload)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(command.Args, "\n")
	for _, required := range []string{"systemd-run", "--wait", "--pipe", "--reuid=1201", "--regid=33", "--clear-groups", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs", "--property=NoNewPrivileges=yes", "--property=ProtectSystem=strict", "--property=ReadWritePaths=/home/litecp/example.com", "--property=RuntimeMaxSec=180"} {
		if !strings.Contains(args, required) {
			t.Fatalf("missing isolation option %s", required)
		}
	}
	for _, argument := range command.Args {
		if strings.HasPrefix(argument, "--uid=") || strings.HasPrefix(argument, "--gid=") {
			t.Fatal("systemd would try to resolve the website's numeric-only identity")
		}
	}
	separator := -1
	for i, argument := range command.Args {
		if argument == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || command.Args[separator+1] != "/usr/bin/setpriv" {
		t.Fatal("PHP must not run before the website credential drop")
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

func TestWordPressSetupNumericIdentityAndPrivilegeBoundary(t *testing.T) {
	// Real websites use large numeric IDs without entries in /etc/passwd.
	command, err := wordPressSetupCommand(context.Background(), "/home/litecp/example.com/public_html", "/home/litecp/example.com", "/opt/remi/php84/root/usr/bin/php", 1800123456, 994, nil)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(command.Args, "\n")
	for _, required := range []string{"--working-directory=/", "--reuid=1800123456", "--regid=994", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_SETPCAP", "--property=RestrictSUIDSGID=yes"} {
		if !strings.Contains(args, required) {
			t.Fatalf("missing numeric identity protection %s", required)
		}
	}
	if strings.Contains(args, "--working-directory=/home/") {
		t.Fatal("privileged launcher enters the private website directory")
	}
	if !strings.Contains(args, "--ambient-caps=-all\n--\n/opt/remi/php84/root/usr/bin/php\n-d\ndisplay_errors=stderr") {
		t.Fatal("PHP is not executed directly after the credential/capability drop")
	}
}

func TestWordPressSetupServiceFailureDiagnostic(t *testing.T) {
	if os.Getenv("LITECP_WORDPRESS_EXIT_TEST") == "1" {
		os.Exit(217)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestWordPressSetupServiceFailureDiagnostic$")
	command.Env = append(os.Environ(), "LITECP_WORDPRESS_EXIT_TEST=1")
	err := command.Run()
	if err == nil || !strings.Contains(wordPressSetupExitDiagnostic(err), "217/USER") {
		t.Fatalf("missing actionable user-identity diagnosis: %v", err)
	}
}

// This opt-in test starts only a short-lived identity probe. It neither loads
// WordPress nor reads/changes any website files or database records.
func TestWordPressSetupLinuxNumericIdentityExecution(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("LITECP_TEST_WORDPRESS_SYSTEMD") != "1" {
		t.Skip("requires opt-in root Linux host with systemd and Python 3")
	}
	if os.Geteuid() != 0 {
		t.Fatal("identity probe requires root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const uid, gid = 1800123456, 994
	command, err := wordPressSetupCommand(ctx, "/", "/tmp", "/usr/bin/python3", uid, gid, []byte(`{"probe":"stdin-only"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i, argument := range command.Args {
		if argument == "/usr/bin/python3" {
			command.Args = append(command.Args[:i+1], "-c", `import json,os,sys
s=dict(line.split(":",1) for line in open("/proc/self/status") if ":" in line)
json.dump({"uid":os.getuid(),"gid":os.getgid(),"groups":os.getgroups(),"caps":[int(s[k].strip(),16) for k in ["CapInh","CapPrm","CapEff","CapBnd","CapAmb"]],"nnp":int(s["NoNewPrivs"].strip()),"stdin":json.load(sys.stdin)["probe"]=="stdin-only"},sys.stdout)`)
			break
		}
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("numeric identity probe failed: %v: %s", err, output)
	}
	var identity struct {
		UID    int      `json:"uid"`
		GID    int      `json:"gid"`
		Groups []int    `json:"groups"`
		Caps   []uint64 `json:"caps"`
		NNP    int      `json:"nnp"`
		Stdin  bool     `json:"stdin"`
	}
	if err = json.Unmarshal(output, &identity); err != nil {
		t.Fatal(err)
	}
	if identity.UID != uid || identity.GID != gid || len(identity.Groups) != 0 || identity.NNP != 1 || !identity.Stdin {
		t.Fatalf("wrong execution credentials: %#v", identity)
	}
	if len(identity.Caps) != 5 {
		t.Fatal("capability probe incomplete")
	}
	for _, caps := range identity.Caps {
		if caps != 0 {
			t.Fatal("setup program retained Linux capabilities")
		}
	}
}
