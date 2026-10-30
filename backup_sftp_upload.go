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

	if err := s.runBackupSFTP(ctx, destination, password, batch); err != nil {
		return "", fmt.Errorf("local backup retained; SFTP upload failed: %w", err)
	}
	return remoteURL, nil
}

func (s *System) runBackupSFTP(ctx context.Context, destination BackupDestination, password, batch string) error {
	for _, name := range []string{"ssh-keyscan", "sshpass", "sftp"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("SFTP requires %s", name)
		}
	}
	scan, err := runCommand(ctx, 20*time.Second, "ssh-keyscan", "-T", "10", "-p", strconv.Itoa(destination.Port), "-t", "ed25519,ecdsa,rsa", "--", destination.Host)
	if err != nil {
		return errors.New("cannot reach the SFTP server to verify its host key")
	}
	known, err := pinnedKnownHost(scan, destination.HostKeySHA256)
	if err != nil {
		return errors.New("SFTP host key differs from the saved fingerprint. Verify it before editing the destination")
	}
	directory, err := os.MkdirTemp(s.dataDir, "sftp-upload-")
	if err != nil {
		return err
	}
	// Public host-key metadata only; passwords are sent through an anonymous pipe.
	knownPath := filepath.Join(directory, "known_hosts")
	if err := os.WriteFile(knownPath, []byte(known), 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	if _, err := writer.Write([]byte(password + "\n")); err != nil {
		writer.Close()
		return err
	}
	writer.Close()
	args := []string{"-d", "3", "sftp", "-F", "/dev/null", "-oBatchMode=no", "-b", "-", "-P", strconv.Itoa(destination.Port), "-oStrictHostKeyChecking=yes", "-oGlobalKnownHostsFile=/dev/null", "-oUserKnownHostsFile=" + knownPath, "-oPubkeyAuthentication=no", "-oPasswordAuthentication=yes", "-oKbdInteractiveAuthentication=no", "-oNumberOfPasswordPrompts=1", "-oConnectTimeout=20", "-oIdentityAgent=none", "-oForwardAgent=no", "-oClearAllForwardings=yes", "--", destination.Username + "@" + destination.Host}
	command := exec.CommandContext(ctx, "sshpass", args...)
	command.ExtraFiles = []*os.File{reader}
	command.Stdin = strings.NewReader(batch)
	output, err := command.CombinedOutput()
	if err != nil {
		return backupSFTPFailure(output, password)
	}
	return nil
}

// SFTP prints its batch command prompt only after the SSH authentication and
// subsystem startup succeeded. Separate remote filesystem errors from login
// failures so users do not keep replacing an already valid saved password.
func backupSFTPFailure(output []byte, password string) error {
	message := strings.TrimSpace(string(output))
	if password != "" {
		message = strings.ReplaceAll(message, password, "[redacted]")
	}
	lower := strings.ToLower(message)
	authenticated := strings.HasPrefix(lower, "sftp> ") || strings.Contains(lower, "\nsftp> ")
	if authenticated {
		if strings.Contains(lower, "no such file or directory") {
			return errors.New("SFTP login succeeded, but the configured remote folder does not exist. Choose an existing folder or create it on the backup server. Backup uploads will try to create missing folders when the account has permission")
		}
		if strings.Contains(lower, "permission denied") {
			return errors.New("SFTP login succeeded, but access to the remote folder or file was denied. Check that this account can access the configured backup folder")
		}
	} else if strings.Contains(lower, "permission denied") || strings.Contains(lower, "authentication failed") {
		return errors.New("The remote SSH server rejected the saved login. This can also mean root password login is disallowed, the account is locked, or the source IP is blocked. Check the remote SSH authentication log; changing the backup folder will not fix a login rejection")
	}
	if len(message) > 600 {
		message = message[len(message)-600:]
	}
	if message == "" {
		message = "SFTP client failed"
	}
	return fmt.Errorf("SFTP operation failed: %s", message)
}
