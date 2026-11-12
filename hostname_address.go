package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func hostnameAddressEntries(data []byte, hostname string) []string {
	addresses := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			continue
		}
		for _, name := range fields[1:] {
			if strings.EqualFold(name, hostname) {
				addresses = append(addresses, fields[0])
				break
			}
		}
	}
	return addresses
}

func hostnameAddressContent(data []byte, hostname, address string) ([]byte, error) {
	ip := net.ParseIP(address)
	if !validPanelSSLHostname(hostname) || ip == nil || !ip.IsGlobalUnicast() {
		return nil, fmt.Errorf("Enter a valid unicast IPv4 or IPv6 address for the saved fully qualified hostname")
	}
	short := strings.SplitN(hostname, ".", 2)[0]
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	result := []string{}
	for _, line := range lines {
		parts := strings.SplitN(line, "#", 2)
		fields := strings.Fields(parts[0])
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			result = append(result, line)
			continue
		}
		kept := []string{fields[0]}
		for _, name := range fields[1:] {
			if !strings.EqualFold(name, hostname) && !strings.EqualFold(name, short) {
				kept = append(kept, name)
			}
		}
		if len(kept) == len(fields) {
			result = append(result, line)
			continue
		}
		updated := ""
		if len(kept) > 1 {
			updated = strings.Join(kept, "\t")
		}
		if len(parts) == 2 {
			updated += " #" + parts[1]
		}
		if updated != "" {
			result = append(result, updated)
		}
	}
	result = append(result, ip.String()+"\t"+hostname+" "+short)
	return []byte(strings.Join(result, "\n") + "\n"), nil
}

func (a *App) hostnameAddress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT")
		return
	}
	if a.system.demo {
		writeError(w, 409, "Hostname address settings require the installed Linux server")
		return
	}
	hostname, err := os.Hostname()
	if err != nil {
		writeError(w, 500, "Could not read the saved hostname")
		return
	}
	hostname = strings.ToLower(hostname)
	info, err := os.Lstat("/etc/hosts")
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, 500, "Cannot safely read the server hosts file")
		return
	}
	original, err := os.ReadFile("/etc/hosts")
	if err != nil {
		writeError(w, 500, "Could not read the server hosts file")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"hostname": hostname, "addresses": hostnameAddressEntries(original, hostname)})
		return
	}
	var input struct {
		Hostname string `json:"hostname"`
		IP       string `json:"ip"`
	}
	if decodeJSONWithLimit(r, &input, 4096) != nil || input.Hostname != hostname {
		writeError(w, 409, "The saved hostname changed. Refresh this card and try again")
		return
	}
	replacement, err := hostnameAddressContent(original, hostname, strings.TrimSpace(input.IP))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if bytes.Equal(original, replacement) {
		writeJSON(w, 200, map[string]any{"hostname": hostname, "addresses": hostnameAddressEntries(original, hostname)})
		return
	}
	token, err := randomToken(12)
	if err != nil {
		writeError(w, 500, "Could not prepare hostname address recovery")
		return
	}
	backup := filepath.Join(a.system.dataDir, "hostname-address", "hosts-"+token+".bak")
	if err = os.MkdirAll(filepath.Dir(backup), 0700); err == nil {
		err = writeAtomic(backup, original, 0600)
	}
	if err != nil {
		writeError(w, 500, "Could not preserve the previous hosts file")
		return
	}
	latest, err := os.ReadFile("/etc/hosts")
	if err != nil || !bytes.Equal(latest, original) {
		writeError(w, 409, "The server hosts file changed. Refresh and try again")
		return
	}
	if err = writeAtomic("/etc/hosts", replacement, info.Mode().Perm()); err != nil {
		writeError(w, 500, "Could not save the hostname address. Previous hosts file was retained")
		return
	}
	if restorecon, err := exec.LookPath("restorecon"); err == nil {
		if _, err = runCommand(r.Context(), 10*time.Second, restorecon, "/etc/hosts"); err != nil {
			rollback := writeAtomic("/etc/hosts", original, info.Mode().Perm())
			_, _ = runCommand(r.Context(), 10*time.Second, restorecon, "/etc/hosts")
			if rollback != nil {
				writeError(w, 500, "Hosts label repair and rollback failed. Review the retained hostname-address recovery copy")
			} else {
				writeError(w, 500, "Hosts label repair failed. Previous hostname mapping was restored")
			}
			return
		}
	}
	_ = a.store.AddActivity("Local hostname address updated", hostname+" → "+strings.TrimSpace(input.IP), "info")
	writeJSON(w, 200, map[string]any{"hostname": hostname, "addresses": hostnameAddressEntries(replacement, hostname)})
}
