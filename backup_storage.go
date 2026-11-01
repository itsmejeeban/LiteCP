package main

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type remoteStorageSpace struct {
	Available   bool      `json:"available"`
	Total       uint64    `json:"total"`
	Free        uint64    `json:"free"`
	FreePercent float64   `json:"freePercent"`
	CheckedAt   time.Time `json:"checkedAt"`
	Message     string    `json:"message,omitempty"`
}

// OpenSSH df without -h prints 1024-byte blocks in the fixed columns
// Size Used Avail (root) %Capacity. Avail is usable by this remote account;
// reserved root blocks must not be counted as available backup space.
func parseBackupStorage(output string) (remoteStorageSpace, error) {
	header := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "Size" && fields[1] == "Used" && fields[2] == "Avail" {
			header = true
			continue
		}
		if !header || len(fields) != 5 {
			continue
		}
		values := make([]uint64, 4)
		valid := true
		for i := 0; i < 4; i++ {
			n, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil || n > math.MaxUint64/1024 {
				valid = false
				break
			}
			values[i] = n
		}
		if !valid || values[0] == 0 || values[1] > values[0] || values[2] > values[0] || values[3] > values[0] || !strings.HasSuffix(fields[4], "%") {
			continue
		}
		return remoteStorageSpace{Available: true, Total: values[0] * 1024, Free: values[2] * 1024, FreePercent: float64(values[2]) / float64(values[0]) * 100, CheckedAt: time.Now().UTC()}, nil
	}
	return remoteStorageSpace{}, errors.New("Remote server did not return supported filesystem capacity information")
}

func (s *System) backupStorageSpace(ctx context.Context, d BackupDestination) remoteStorageSpace {
	unknown := remoteStorageSpace{CheckedAt: time.Now().UTC()}
	if d.Protocol != "sftp" {
		unknown.Message = "Capacity reporting is unavailable over FTP/FTPS"
		return unknown
	}
	if s.demo {
		unknown.Message = "Capacity requires an installed server"
		return unknown
	}
	if validateBackupDestination(d, true) != nil || !validBackupHostProbe(d.Host, d.Port) || strings.ContainsAny(d.RemotePath+d.Username, "\r\n\x00") {
		unknown.Message = "Invalid remote destination configuration"
		return unknown
	}
	password, err := s.DecryptSecret(d.PasswordCipher)
	if err != nil || password == "" || strings.ContainsAny(password, "\r\n\x00") {
		unknown.Message = "Edit and save the destination credentials again"
		return unknown
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	output, err := s.backupSFTPOutput(ctx, d, password, "df "+sftpBatchQuote(d.RemotePath)+"\n")
	if err != nil {
		unknown.Message = "Capacity unavailable: connection failed, folder inaccessible, or SFTP statvfs is unsupported"
		return unknown
	}
	space, err := parseBackupStorage(output)
	if err != nil {
		unknown.Message = err.Error()
		return unknown
	}
	space.Message = "Filesystem capacity at the remote path; account-specific quotas may differ"
	return space
}

func (a *App) backupDestinationStorage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if decodeJSONWithLimit(r, &input, 1024) != nil || input.ID == "" {
		writeError(w, 400, "Choose a saved remote destination")
		return
	}
	d := a.store.BackupDestinationByID(input.ID)
	if d == nil {
		writeError(w, 404, "Remote destination not found")
		return
	}
	writeJSON(w, 200, a.system.backupStorageSpace(r.Context(), *d))
}
