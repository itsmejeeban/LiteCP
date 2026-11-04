package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func websiteScanOutputComplete(text string, infected bool) bool {
	if !strings.Contains(text, "SCAN SUMMARY") || strings.Contains(text, "LibClamAV Error:") || strings.Contains(text, " ERROR") {
		return false
	}
	return !infected || strings.Contains(text, " FOUND")
}

// The panel cannot change identity inside its own sandbox. Let systemd start
// a separately restricted launcher, which drops all privileges before ClamAV.
func websiteScanCommand(ctx context.Context, site Site, target string, outputPath ...string) (*exec.Cmd, error) {
	if site.RuntimeUID < 1000 || site.RuntimeGID < 1 {
		return nil, errors.New("site runtime identity is not configured")
	}
	args := []string{
		"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
		"--working-directory=/", "--property=RuntimeMaxSec=1800",
		"--property=TimeoutStopSec=10", "--property=KillMode=control-group",
		"--property=NoNewPrivileges=yes", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_SETPCAP",
		"--property=RestrictSUIDSGID=yes", "--property=ProtectSystem=strict",
		"--property=ProtectHome=read-only", "--property=PrivateTmp=yes",
		"--property=PrivateNetwork=yes", "--property=Nice=19",
		"--", "/usr/bin/setpriv", fmt.Sprintf("--reuid=%d", site.RuntimeUID), fmt.Sprintf("--regid=%d", site.RuntimeGID),
		"--clear-groups", "--no-new-privs", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all",
		"--", "clamscan", "--recursive=yes", "--infected", "--follow-dir-symlinks=0", "--follow-file-symlinks=0", "--max-filesize=256M", "--max-scansize=2G", "--", target,
	}
	if len(outputPath) > 0 {
		// Passing broker-created pipes into another SELinux domain can fail.
		// PID 1 opens this new, root-owned private log before dropping identity.
		filtered := []string{}
		for _, arg := range args {
			if arg != "--pipe" {
				filtered = append(filtered, arg)
			}
		}
		for i, arg := range filtered {
			if arg == "--" {
				filtered = append(filtered[:i], append([]string{"--property=StandardOutput=file:" + outputPath[0], "--property=StandardError=inherit"}, filtered[i:]...)...)
				break
			}
		}
		args = filtered
	}
	return exec.CommandContext(ctx, "systemd-run", args...), nil
}
