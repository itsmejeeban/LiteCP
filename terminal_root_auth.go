package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Verify with the host's libcrypt so the web/broker never reads password hashes.
// The password travels only over stdin to the short-lived host task.
const terminalRootAuth = `
import ctypes, ctypes.util, hmac, json, sys, time
try:
    password = json.loads(sys.stdin.buffer.readline(32768))["password"]
    if not isinstance(password, str) or not password or len(password) > 4096 or "\0" in password:
        raise ValueError()
    with open("/etc/shadow", encoding="utf-8") as shadow:
        account = next(line.strip().split(":") for line in shadow if line.startswith("root:"))
    stored = account[1]
    today = int(time.time() // 86400)
    if len(account) < 8 or (account[7] and int(account[7]) >= 0 and today >= int(account[7])):
        raise ValueError()
    if account[2] == "0" or (account[2] and account[4] and int(account[4]) >= 0 and today >= int(account[2]) + int(account[4])):
        raise ValueError()
    if not stored or stored.startswith(("!", "*")):
        raise ValueError()
    crypt = ctypes.CDLL(ctypes.util.find_library("crypt") or "libcrypt.so.1").crypt
    crypt.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
    crypt.restype = ctypes.c_char_p
    actual = crypt(password.encode("utf-8"), stored.encode("ascii"))
    password = None
    if actual is None or not hmac.compare_digest(actual, stored.encode("ascii")):
        raise ValueError()
    print("verified")
except Exception:
    sys.exit(1)
`

func verifyTerminalRootPassword(ctx context.Context, password string) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	data, _ := json.Marshal(map[string]string{"password": password})
	cmd := exec.CommandContext(ctx, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--property=RuntimeMaxSec=10", "--", "python3", "-c", terminalRootAuth)
	cmd.Stdin = strings.NewReader(string(data) + "\n")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "verified" {
		return fmt.Errorf("Root password verification failed. Check the password and ensure the root account has an unlocked password")
	}
	return nil
}
