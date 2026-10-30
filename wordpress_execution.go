package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// Run the credential drop in a separate system service: the panel's own
// restrictions can prohibit setresuid. Website UIDs deliberately have no NSS
// login account, so systemd User=/Group= cannot be used for these identities.
func wordPressSetupCommand(ctx context.Context, root, home, php string, uid, gid int, payload []byte) (*exec.Cmd, error) {
	if uid < 1000 || gid <= 0 {
		return nil, errors.New("website isolation identity is missing; WordPress setup was not started")
	}
	args := []string{
		"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
		// PHP enters root after changing identity. The privileged launcher must
		// not need DAC_OVERRIDE to enter the website's private document root.
		"--working-directory=/",
		"--setenv=HOME=" + home, "--setenv=LANG=C.UTF-8", "--setenv=PATH=/usr/local/bin:/usr/bin:/bin",
		"--property=RuntimeMaxSec=180", "--property=TimeoutStopSec=10", "--property=KillMode=control-group",
		"--property=NoNewPrivileges=yes", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_SETPCAP", "--property=RestrictSUIDSGID=yes",
		"--property=ProtectSystem=strict", "--property=ProtectHome=read-only", "--property=ReadWritePaths=" + home,
		"--property=PrivateTmp=yes", "--property=UMask=0027",
		"--", "/usr/bin/setpriv", "--reuid=" + strconv.Itoa(uid), "--regid=" + strconv.Itoa(gid),
		"--clear-groups", "--no-new-privs", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all",
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

func wordPressSetupExitDiagnostic(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 217:
			return "the setup service could not establish the website identity (systemd 217/USER); check the service journal"
		case 203:
			return "the setup launcher or selected PHP executable could not start (systemd 203/EXEC); check the installed runtime"
		case 226:
			return "the setup service could not create its filesystem sandbox (systemd 226/NAMESPACE); check the service journal"
		}
	}
	return err.Error()
}
