package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var phpSettingDefaults = map[string]string{
	"memory_limit": "256M", "upload_max_filesize": "128M", "post_max_size": "128M",
	"max_execution_time": "120", "max_input_time": "60", "max_input_vars": "1000",
	"display_errors": "Off", "log_errors": "On", "date.timezone": "Asia/Kolkata",
}

// Custom directives are restricted to scalar, per-site settings. Paths, loaders,
// prepend scripts and security boundaries are deliberately not editable here.
var customPHPSettingTypes = map[string]string{
	"allow_url_fopen": "bool", "html_errors": "bool", "display_startup_errors": "bool",
	"output_buffering": "number", "implicit_flush": "bool", "zlib.output_compression": "bool",
	"default_socket_timeout": "number", "max_file_uploads": "number", "max_input_nesting_level": "number",
	"realpath_cache_size": "size", "realpath_cache_ttl": "number", "pcre.backtrack_limit": "number",
	"pcre.recursion_limit": "number", "serialize_precision": "precision", "precision": "precision",
	"session.gc_maxlifetime": "number", "session.cookie_lifetime": "number", "session.cookie_secure": "bool",
	"session.cookie_httponly": "bool", "session.use_strict_mode": "bool", "session.use_only_cookies": "bool",
	"session.cookie_samesite": "samesite", "default_charset": "charset", "error_reporting": "number",
}

func validateCustomPHPSetting(key, value string) error {
	kind, ok := customPHPSettingTypes[key]
	if !ok {
		return fmt.Errorf("custom directive %s is unsupported or protected", key)
	}
	valid := false
	switch kind {
	case "bool":
		valid = value == "On" || value == "Off" || value == "0" || value == "1"
	case "number":
		n, e := strconv.Atoi(value)
		valid = e == nil && n >= 0 && n <= 2147483647
	case "precision":
		n, e := strconv.Atoi(value)
		valid = e == nil && n >= -1 && n <= 100
	case "size":
		valid = regexp.MustCompile(`^[1-9][0-9]{0,5}[KMG]$`).MatchString(value)
	case "samesite":
		valid = value == "Lax" || value == "Strict" || value == "None"
	case "charset":
		valid = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`).MatchString(value)
	}
	if !valid {
		return fmt.Errorf("invalid value for custom directive %s", key)
	}
	return nil
}

func mergeCustomPHPSettings(settings map[string]string, custom string) (map[string]string, error) {
	result := map[string]string{}
	for k, v := range settings {
		result[k] = v
	}
	if len(custom) > 8192 {
		return nil, fmt.Errorf("custom settings must be at most 8192 characters")
	}
	for i, line := range strings.Split(strings.ReplaceAll(custom, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		pair := strings.SplitN(line, "=", 2)
		if len(pair) != 2 {
			return nil, fmt.Errorf("custom line %d must use directive = value", i+1)
		}
		key, value := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
		if _, ok := phpSettingDefaults[key]; ok {
			return nil, fmt.Errorf("set %s using the standard field above", key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate directive: %s", key)
		}
		if err := validateCustomPHPSetting(key, value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func validatePHPSettings(settings map[string]string) error {
	for key, value := range settings {
		if _, ok := phpSettingDefaults[key]; !ok {
			if err := validateCustomPHPSetting(key, value); err != nil {
				return err
			}
			continue
		}
		switch key {
		case "display_errors", "log_errors":
			if value != "On" && value != "Off" {
				return fmt.Errorf("%s must be On or Off", key)
			}
		case "date.timezone":
			if strings.ContainsAny(value, "\r\n\x00;[]\"") {
				return fmt.Errorf("invalid time zone")
			}
			if _, err := time.LoadLocation(value); err != nil {
				return fmt.Errorf("invalid time zone")
			}
		case "memory_limit", "upload_max_filesize", "post_max_size":
			if len(value) < 2 || value[len(value)-1] != 'M' {
				return fmt.Errorf("%s must use MiB, for example 256M", key)
			}
			n, err := strconv.Atoi(value[:len(value)-1])
			if err != nil || n < 1 || n > 8192 {
				return fmt.Errorf("%s must be between 1M and 8192M", key)
			}
		default:
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100000 {
				return fmt.Errorf("%s must be between 1 and 100000", key)
			}
		}
	}
	get := func(k string) int {
		v := settings[k]
		if v == "" {
			v = phpSettingDefaults[k]
		}
		n, _ := strconv.Atoi(strings.TrimSuffix(v, "M"))
		return n
	}
	if get("post_max_size") < get("upload_max_filesize") {
		return fmt.Errorf("post_max_size must be at least upload_max_filesize")
	}
	return nil
}

func phpPoolSettings(settings map[string]string) string {
	// Imported snapshots must not inject arbitrary pool directives.
	if validatePHPSettings(settings) != nil {
		return ""
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var result strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&result, "php_admin_value[%s] = %s\n", k, settings[k])
	}
	return result.String()
}

func (a *App) sitePHPSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT")
		return
	}
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	if r.Method == http.MethodGet {
		custom := map[string]string{}
		for k, v := range site.PHPSettings {
			if _, ok := phpSettingDefaults[k]; !ok {
				custom[k] = v
			}
		}
		writeJSON(w, 200, map[string]any{"settings": site.PHPSettings, "customSettings": custom, "supportedCustom": customPHPSettingTypes, "defaults": phpSettingDefaults, "version": site.PHPVersion})
		return
	}
	if resourceEnabled(*site) {
		writeError(w, 409, "Disable resource enforcement before changing PHP settings, then re-enable it.")
		return
	}
	var input struct {
		Settings map[string]string `json:"settings"`
		Custom   string            `json:"custom"`
	}
	if decodeJSONWithLimit(r, &input, 16<<10) != nil {
		writeError(w, 400, "Invalid settings")
		return
	}
	merged, err := mergeCustomPHPSettings(input.Settings, input.Custom)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.Settings = merged
	if err := validatePHPSettings(input.Settings); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated := *site
	updated.PHPSettings = input.Settings
	rollback := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return a.system.ensureSitePHPPool(ctx, *site)
	}
	if err := a.system.ensureSitePHPPool(r.Context(), updated); err != nil {
		if restoreErr := rollback(); restoreErr != nil {
			writeError(w, 500, "PHP settings failed; runtime recovery also failed. Check service logs.")
			return
		}
		writeError(w, 502, "PHP settings could not be applied; previous settings restored.")
		return
	}
	if err := a.store.UpdateSite(site.ID, func(value *Site) { value.PHPSettings = input.Settings }); err != nil {
		if restoreErr := rollback(); restoreErr != nil {
			writeError(w, 500, "Saving failed; runtime recovery also failed. Check service logs.")
			return
		}
		writeError(w, 500, "Saving failed; previous PHP settings restored.")
		return
	}
	_ = a.store.AddActivity("PHP settings updated", site.Domain, "info")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
