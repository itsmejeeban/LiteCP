package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWAFLabelsScopedRepair(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("root Linux shell fixtures")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash required")
	}
	source, err := os.ReadFile("scripts/repair-waf-labels.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"enforcing", "disabled", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			waf := filepath.Join(root, "waf")
			bin := filepath.Join(root, "bin")
			log := filepath.Join(root, "commands")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(waf, 0700); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				outside := filepath.Join(root, "outside")
				if err := os.Mkdir(outside, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(waf, "rules")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(waf, "rules"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, command := range []string{"chown", "semanage", "restorecon"} {
				content := "#!/bin/sh\n{ printf '" + command + "'; printf ' %s' \"$@\"; printf '\\n'; } >> \"$QA_WAF_LOG\"\n"
				if err := os.WriteFile(filepath.Join(bin, command), []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(bin, "selinuxenabled"), []byte("#!/bin/sh\nexit \"${QA_SELINUX_EXIT:-0}\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(bash, "-c", strings.ReplaceAll(string(source), "/etc/litecp/waf", waf))
			mode := "0"
			if scenario == "disabled" {
				mode = "1"
			}
			command.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "QA_WAF_LOG="+log, "QA_SELINUX_EXIT="+mode)
			output, err := command.CombinedOutput()
			if scenario == "symlink" {
				if err == nil {
					t.Fatal("symlinked vendor tree accepted")
				}
				if _, e := os.Stat(log); !os.IsNotExist(e) {
					t.Fatal("repair ran outside its scope")
				}
				return
			}
			if err != nil {
				t.Fatalf("repair failed: %v %s", err, output)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			calls := string(data)
			if !strings.Contains(calls, "chown -Rh root:root "+filepath.Join(waf, "rules")) {
				t.Fatal("vendor ownership not normalized", calls)
			}
			if scenario == "enforcing" {
				if !strings.Contains(calls, "semanage fcontext -a -t httpd_config_t "+waf+"(/.*)?") || !strings.Contains(calls, "restorecon -R "+waf) {
					t.Fatal("confined Nginx labels not applied", calls)
				}
			} else if strings.Contains(calls, "semanage") || strings.Contains(calls, "restorecon") {
				t.Fatal("SELinux commands ran when disabled", calls)
			}
		})
	}
}
