package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func sftpBatchQuote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
func (s *System) uploadSFTPBackup(ctx context.Context, destination BackupDestination, localPath, password, remoteURL string) (string, error) {
	for _, name := range []string{"ssh-keyscan", "sshpass", "sftp"} {
		if _, err := exec.LookPath(name); err != nil {
			return "", fmt.Errorf("local backup retained; SFTP upload requires %s", name)
		}
	}
	scan, err := runCommand(ctx, 20*time.Second, "ssh-keyscan", "-T", "10", "-p", strconv.Itoa(destination.Port), "-t", "ed25519,ecdsa,rsa", "--", destination.Host)
	if err != nil {
		return "", errors.New("local backup retained; cannot reach the SFTP server to verify its host key")
	}
	known, err := pinnedKnownHost(scan, destination.HostKeySHA256)
	if err != nil {
		return "", errors.New("local backup retained; SFTP host key differs from the saved fingerprint. Verify it before editing the destination")
	}
	directory, err := os.MkdirTemp(s.dataDir, "sftp-upload-")
	if err != nil {
		return "", err
	}
	// Public host-key metadata only; passwords are sent through an anonymous pipe.
	knownPath := filepath.Join(directory, "known_hosts")
	if err := os.WriteFile(knownPath, []byte(known), 0600); err != nil {
		return "", err
	}
	batch := ""
	current := "/"
	for _, segment := range strings.Split(path.Clean(destination.RemotePath), "/") {
		if segment == "" {
			continue
		}
		current = path.Join(current, segment)
		batch += "-mkdir " + sftpBatchQuote(current) + "\n"
	}
	batch += "put " + sftpBatchQuote(localPath) + " " + sftpBatchQuote(path.Join(destination.RemotePath, filepath.Base(localPath))) + "\n"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if _, err := writer.Write([]byte(password + "\n")); err != nil {
		writer.Close()
		return "", err
	}
	writer.Close()
	args := []string{"-d", "3", "sftp", "-F", "/dev/null", "-oBatchMode=no", "-b", "-", "-P", strconv.Itoa(destination.Port), "-oStrictHostKeyChecking=yes", "-oGlobalKnownHostsFile=/dev/null", "-oUserKnownHostsFile=" + knownPath, "-oPubkeyAuthentication=no", "-oPasswordAuthentication=yes", "-oKbdInteractiveAuthentication=no", "-oNumberOfPasswordPrompts=1", "-oConnectTimeout=20", "-oIdentityAgent=none", "-oForwardAgent=no", "-oClearAllForwardings=yes", "--", destination.Username + "@" + destination.Host}
	command := exec.CommandContext(ctx, "sshpass", args...)
	command.ExtraFiles = []*os.File{reader}
	command.Stdin = strings.NewReader(batch)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.ReplaceAll(strings.TrimSpace(string(output)), password, "[redacted]")
		if len(message) > 600 {
			message = message[len(message)-600:]
		}
		if message == "" {
			message = "SFTP client failed"
		}
		return "", fmt.Errorf("local backup retained; SFTP upload failed: %s", message)
	}
	return remoteURL, nil
}
