package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

func backupConnectionCurlConfig(destination BackupDestination, password string) (string, error) {
	if strings.ContainsAny(password+destination.Username+destination.RemotePath, "\r\n\x00") {
		return "", errors.New("credentials and remote path cannot contain line breaks or null characters")
	}
	remoteURL := (&url.URL{Scheme: destination.Protocol, Host: net.JoinHostPort(destination.Host, strconv.Itoa(destination.Port)), Path: path.Clean(destination.RemotePath) + "/"}).String()
	lines := []string{"fail", "silent", "show-error", "list-only", `output = "/dev/null"`, "connect-timeout = 10", "max-time = 35",
		`user = "` + curlConfigEscape(destination.Username+":"+password) + `"`,
		`url = "` + curlConfigEscape(remoteURL) + `"`,
	}
	if destination.Protocol == "ftps" {
		lines = append(lines, "ssl-reqd")
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func (s *System) testBackupConnection(ctx context.Context, destination BackupDestination) error {
	if err := validateBackupDestination(destination, true); err != nil {
		return err
	}
	if !validBackupHostProbe(destination.Host, destination.Port) {
		return errors.New("invalid remote server hostname or port")
	}
	if s.demo {
		return errors.New("connection testing requires the installed server")
	}
	password, err := s.DecryptSecret(destination.PasswordCipher)
	if err != nil {
		return errors.New("cannot read saved credentials; edit and save the destination password again")
	}
	if password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return errors.New("saved password is empty or contains unsupported control characters")
	}
	if strings.ContainsAny(destination.Username+destination.RemotePath, "\r\n\x00") {
		return errors.New("username and remote path contain unsupported control characters")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if destination.Protocol == "sftp" {
		// These SFTP batch commands only read the configured directory. Reuse
		// the uploader's pinned host key and anonymous credential pipe.
		err = s.runBackupSFTP(ctx, destination, password, "cd "+sftpBatchQuote(destination.RemotePath)+"\nls -d .\n")
	} else {
		var config string
		config, err = backupConnectionCurlConfig(destination, password)
		if err == nil {
			_, err = runCommandInput(ctx, 40*time.Second, config, "curl", "--disable", "--config", "-")
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("connection test timed out; check the remote server and firewall")
		}
		message := strings.ReplaceAll(err.Error(), password, "[redacted]")
		if len(message) > 600 {
			message = message[:600]
		}
		return fmt.Errorf("connection test failed: %s", message)
	}
	return nil
}

func (a *App) testBackupDestination(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeJSONWithLimit(r, &input, 1024); err != nil || input.ID == "" {
		writeError(w, 400, "Choose a saved remote destination")
		return
	}
	destination := a.store.BackupDestinationByID(input.ID)
	if destination == nil {
		writeError(w, 404, "Remote destination not found")
		return
	}
	if err := a.system.testBackupConnection(r.Context(), *destination); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"message": "Connected successfully. Login and remote folder access verified. Upload permission was not tested; no remote files were changed."})
}
