package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

func (a *App) environmentInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET")
		return
	}
	type software struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Installed bool   `json:"installed"`
	}
	items := []software{}
	for _, spec := range []struct {
		name, command string
		args          []string
	}{
		{"Nginx", "nginx", []string{"-v"}}, {"MySQL", "mysqld", []string{"--version"}},
		{"Node.js (host)", "node", []string{"--version"}}, {"Python (host)", "python3", []string{"--version"}},
		{"Ruby (host)", "ruby", []string{"--version"}}, {"Podman", "podman", []string{"--version"}},
	} {
		output, err := runCommand(r.Context(), 3*time.Second, spec.command, spec.args...)
		value := strings.TrimSpace(output)
		if at := strings.IndexByte(value, '\n'); at >= 0 {
			value = value[:at]
		}
		if len(value) > 240 {
			value = value[:240]
		}
		if err != nil {
			value = "Not detected"
		}
		items = append(items, software{spec.name, value, err == nil})
	}
	images := map[string][]string{}
	imageStatus := "Checked"
	output, err := a.system.applicationHostCommand(r.Context(), 10*time.Second, "podman", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		imageStatus = "Unable to check installed container images"
	} else {
		present := map[string]bool{}
		for _, line := range strings.Split(output, "\n") {
			present[strings.TrimSpace(line)] = true
		}
		for language, versions := range hostingImages {
			for version, image := range versions {
				if present[image] {
					images[language] = append(images[language], version)
				}
			}
			sort.Strings(images[language])
		}
	}
	writeJSON(w, 200, map[string]any{"software": items, "images": images, "imageStatus": imageStatus, "enabled": a.store.PublicState().Settings.ApplicationHosting})
}
