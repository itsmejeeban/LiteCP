package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// systemd starts PHP with the website identity before applying its sandbox.
// setpriv in the web service inherits its syscall restrictions and can fail
// even when the panel itself has effective UID 0.
func wordPressSetupCommand(ctx context.Context, root, home, php string, uid, gid int, payload []byte) (*exec.Cmd, error) {
	if uid < 1000 || gid <= 0 {
		return nil, errors.New("website isolation identity is missing; WordPress setup was not started")
	}
	args := []string{
		"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
		"--uid=" + strconv.Itoa(uid), "--gid=" + strconv.Itoa(gid),
		"--working-directory=" + root,
		"--setenv=HOME=" + home, "--setenv=LANG=C.UTF-8", "--setenv=PATH=/usr/local/bin:/usr/bin:/bin",
		"--property=RuntimeMaxSec=180", "--property=TimeoutStopSec=10", "--property=KillMode=control-group",
		"--property=NoNewPrivileges=yes", "--property=CapabilityBoundingSet=", "--property=RestrictSUIDSGID=yes",
		"--property=ProtectSystem=strict", "--property=ProtectHome=read-only", "--property=ReadWritePaths=" + home,
		"--property=PrivateTmp=yes", "--property=UMask=0027",
		"--", php, "-d", "display_errors=stderr", "-d", "log_errors=0", "-d", "zend.exception_ignore_args=1", "-r",
		// systemd performs ExecStart environment expansion. Escape PHP dollar
		// signs so it receives the original program verbatim on older systemd too.
		strings.ReplaceAll(wordPressSetupScript, "$", "$$"),
	}
	command := exec.CommandContext(ctx, "systemd-run", args...)
	// Administrator credentials travel only through stdin, not argv, files,
	// service environment, or the system journal.
	command.Stdin = strings.NewReader(string(payload))
	return command, nil
}
