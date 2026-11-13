package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestTerminalRootAuthenticationRejectsInvalidAccounts(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		python, err = exec.LookPath("python3")
	}
	if err != nil {
		t.Skip("Python unavailable")
	}
	for _, tc := range []struct {
		name, password, account string
		valid                   bool
	}{
		{"correct", "correct", "root:$6$fixture:20000:0:99999:7:::", true},
		{"wrong", "incorrect", "root:$6$fixture:20000:0:99999:7:::", false},
		{"locked", "correct", "root:!$6$fixture:20000:0:99999:7:::", false},
		{"empty hash", "correct", "root::20000:0:99999:7:::", false},
		{"expired", "correct", "root:$6$fixture:20000:0:99999:7::1:", false},
		{"nul", "correct\x00", "root:$6$fixture:20000:0:99999:7:::", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account, _ := json.Marshal(tc.account + "\n")
			// A memory-only shadow file and crypt stub exercise the gate without
			// reading an OS account or changing any real password.
			prelude := `import builtins, ctypes, io
class FakeCrypt:
    def __call__(self, password, salt):
        return b"$6$fixture" if password == b"correct" else b"wrong"
class FakeLibrary:
    crypt = FakeCrypt()
ctypes.CDLL = lambda *args: FakeLibrary()
builtins.open = lambda *args, **kwargs: io.StringIO(` + string(account) + `)
`
			cmd := exec.Command(python, "-c", prelude+terminalRootAuth)
			payload, _ := json.Marshal(map[string]string{"password": tc.password})
			cmd.Stdin = strings.NewReader(string(payload) + "\n")
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.valid {
				t.Fatalf("authentication outcome mismatch: %v %s", err, output)
			}
			if strings.Contains(string(output), tc.password) && tc.password != "" {
				t.Fatal("Password leaked to output")
			}
		})
	}
}
