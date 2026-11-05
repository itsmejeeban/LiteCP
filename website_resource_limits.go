package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// WebsiteResourceLimits belongs to the immutable site identity, not its domain.
// A saved draft is not an assertion that Linux is enforcing these settings.
type WebsiteResourceLimits struct {
	Enabled            bool    `json:"enabled"`
	CPUPercent         float64 `json:"cpuPercent"`
	MemoryMiB          int64   `json:"memoryMiB"`
	DiskMiB            int64   `json:"diskMiB"`
	Tasks              int     `json:"tasks"`
	ConcurrentRequests int     `json:"concurrentRequests"`
	ReadMiBPerSecond   int64   `json:"readMiBPerSecond"`
	WriteMiBPerSecond  int64   `json:"writeMiBPerSecond"`
}

func (p WebsiteResourceLimits) validate() error {
	if math.IsNaN(p.CPUPercent) || math.IsInf(p.CPUPercent, 0) || p.CPUPercent < 1 || p.CPUPercent > 102400 {
		return fmt.Errorf("CPU must be between 1 and 102400 percent (100 percent is one core)")
	}
	if p.MemoryMiB < 32 || p.MemoryMiB > 1<<30 {
		return fmt.Errorf("memory must be between 32 and 1073741824 MiB")
	}
	if p.DiskMiB < 1 || p.DiskMiB > 1<<40 {
		return fmt.Errorf("an explicit disk allowance between 1 and 1099511627776 MiB is required")
	}
	if p.Tasks < 8 || p.Tasks > 1048576 {
		return fmt.Errorf("process count must be between 8 and 1048576")
	}
	if p.ConcurrentRequests < 1 || p.ConcurrentRequests > 65535 {
		return fmt.Errorf("concurrent requests must be between 1 and 65535")
	}
	if p.ReadMiBPerSecond < 0 || p.WriteMiBPerSecond < 0 || p.ReadMiBPerSecond > 1<<30 || p.WriteMiBPerSecond > 1<<30 {
		return fmt.Errorf("invalid disk I/O allowance")
	}
	return nil
}

type resourceCapabilities struct {
	OS            string   `json:"os"`
	CgroupV2      bool     `json:"cgroupV2"`
	Controllers   []string `json:"controllers"`
	Filesystem    string   `json:"filesystem"`
	Mount         string   `json:"mount"`
	ProjectQuota  bool     `json:"projectQuota"`
	Ready         bool     `json:"ready"`
	Prerequisites []string `json:"prerequisites"`
}

// Parse the longest matching mount from mountinfo. Do not mistake a parent's
// quota flags for those of a nested website filesystem.
func resourceMount(home, mountinfo string) (kind, mount string, projectQuota bool) {
	decode := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(mountinfo, "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 6 || len(right) < 3 {
			continue
		}
		path := decode.Replace(left[4])
		if home != path && !strings.HasPrefix(home, strings.TrimSuffix(path, "/")+"/") {
			continue
		}
		if len(path) <= len(mount) {
			continue
		}
		kind, mount, projectQuota = right[0], path, false
		for _, option := range strings.Split(left[5]+","+right[2], ",") {
			if option == "prjquota" || option == "pquota" {
				projectQuota = true
			}
		}
	}
	return
}

func inspectResourceCapabilities(site Site) resourceCapabilities {
	c := resourceCapabilities{OS: runtime.GOOS, Controllers: []string{}, Prerequisites: []string{}}
	if runtime.GOOS != "linux" {
		c.Prerequisites = append(c.Prerequisites, "Linux with systemd and cgroup v2 is required")
		return c
	}
	controllers, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	c.CgroupV2 = err == nil
	c.Controllers = strings.Fields(string(controllers))
	for _, wanted := range []string{"cpu", "memory", "pids", "io"} {
		found := false
		for _, v := range c.Controllers {
			if v == wanted {
				found = true
			}
		}
		if !found {
			c.Prerequisites = append(c.Prerequisites, "cgroup v2 controller unavailable: "+wanted)
		}
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		c.Prerequisites = append(c.Prerequisites, "systemd must manage this host")
	}
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err == nil {
		c.Filesystem, c.Mount, c.ProjectQuota = resourceMount(siteHome(site), string(info))
	}
	if c.Filesystem != "xfs" && c.Filesystem != "ext4" {
		c.Prerequisites = append(c.Prerequisites, "website home requires a supported XFS or ext4 project-quota filesystem")
	}
	if !c.ProjectQuota {
		c.Prerequisites = append(c.Prerequisites, "project quotas must already be enabled on the website-home filesystem; LiteCP does not change mounts")
	}
	if c.Filesystem != "ext4" {
		c.Prerequisites = append(c.Prerequisites, "native website enforcement currently requires ext4 project quotas")
	}
	if !resourceExecutableAvailable() {
		c.Prerequisites = append(c.Prerequisites, "install the quota package (setquota) before activation")
	}
	plan := resourceRuntimePlan{Site: site, DataDir: "/var/lib/litecp", Policy: WebsiteResourceLimits{CPUPercent: 25, MemoryMiB: 256, DiskMiB: 1024, Tasks: 64, ConcurrentRequests: 8}}
	if err := resourceRuntimeValidate(plan); err != nil {
		c.Prerequisites = append(c.Prerequisites, err.Error())
	}
	c.Ready = len(c.Prerequisites) == 0
	return c
}

// ResourceSample is made from cgroup counters, never a full directory walk.
type resourceSample struct {
	At                                                  time.Time
	UsageUsec, ThrottledUsec, Periods, ThrottledPeriods uint64
	OOMKills, PIDsDenied                                uint64
	DiskBytes                                           int64
}

type resourcePolicyState struct {
	State           string
	Reason          string
	NextRecovery    time.Time
	CPUExhaustedFor time.Duration
}

// evaluateResourcePolicy is deterministic to allow synthetic-counter tests.
// Actions are returned to an executor; this function never stops a service.
func evaluateResourcePolicy(p WebsiteResourceLimits, state resourcePolicyState, before, now resourceSample) (resourcePolicyState, []string) {
	events := []string{}
	if !p.Enabled {
		return resourcePolicyState{State: "Disabled"}, events
	}
	if state.State == "Resource blocked" {
		if now.At.Before(state.NextRecovery) {
			return state, events
		}
		if state.Reason == "Disk" && float64(now.DiskBytes) >= float64(p.DiskMiB)*1048576*.95 {
			state.NextRecovery = now.At.Add(5 * time.Minute)
			return state, events
		}
		state.State = "Recovering"
		return state, append(events, "recover")
	}
	if state.State != "Active" {
		return state, events
	}
	if now.PIDsDenied > before.PIDsDenied {
		events = append(events, "process limit reached")
	}
	reason := ""
	if now.OOMKills > before.OOMKills {
		reason = "Memory"
	}
	if p.DiskMiB > 0 && now.DiskBytes >= p.DiskMiB*1048576 {
		reason = "Disk"
	}
	elapsed := now.At.Sub(before.At)
	// Counter resets or missed samples must not count as consecutive seconds.
	if elapsed > 0 && elapsed <= 2*time.Second && now.UsageUsec >= before.UsageUsec && now.ThrottledUsec > before.ThrottledUsec && now.ThrottledPeriods > before.ThrottledPeriods {
		used := float64(now.UsageUsec-before.UsageUsec) / float64(elapsed.Microseconds()) * 100
		if used >= p.CPUPercent*.95 {
			state.CPUExhaustedFor += elapsed
		} else {
			state.CPUExhaustedFor = 0
		}
	} else {
		state.CPUExhaustedFor = 0
	}
	if state.CPUExhaustedFor >= 10*time.Second {
		reason = "CPU"
	}
	if reason != "" {
		state.State = "Resource blocked"
		state.Reason = reason
		state.NextRecovery = now.At.Add(5 * time.Minute)
		events = append(events, "block")
	}
	return state, events
}

func resourceSliceName(id string) string {
	// systemd interprets hyphens as hierarchy separators. Encode every byte so
	// distinct immutable IDs cannot alias through lossy filename sanitization.
	return fmt.Sprintf("litecpwebsite%x.slice", []byte(id))
}

func readResourceCounters(dir string, at time.Time) (resourceSample, error) {
	s := resourceSample{At: at}
	for file, values := range map[string]map[string]*uint64{
		"cpu.stat":      {"usage_usec": &s.UsageUsec, "throttled_usec": &s.ThrottledUsec, "nr_periods": &s.Periods, "nr_throttled": &s.ThrottledPeriods},
		"memory.events": {"oom_kill": &s.OOMKills}, "pids.events": {"max": &s.PIDsDenied},
	} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return s, fmt.Errorf("read %s: %w", file, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			target := values[fields[0]]
			if target == nil {
				continue
			}
			n, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return s, fmt.Errorf("invalid %s counter", file)
			}
			*target = n
		}
	}
	return s, nil
}

func (a *App) websiteResourceLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeError(w, 405, "Use GET, PUT, POST or DELETE")
		return
	}
	// Authenticated mutations are already serialized by withAuth.
	if r.Method == http.MethodGet && r.URL.Query().Get("siteId") == "" {
		a.websiteResourceList(w, r)
		return
	}
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	if r.Method == http.MethodGet {
		st := resourceStatusForSite(*site)
		writeJSON(w, 200, map[string]any{"settings": site.ResourceLimits, "state": st.State, "enforced": st.Enforced && time.Since(st.UpdatedAt) < 10*time.Second, "capabilities": inspectResourceCapabilities(*site), "usage": st, "nextRecovery": st.NextRecovery, "history": resourceHistory(site.ID)})
		return
	}
	if r.Method == http.MethodPost {
		if site.ResourceLimits == nil || !site.ResourceLimits.Enabled {
			writeError(w, 409, "Enable resource enforcement before retrying recovery")
			return
		}
		if site.Suspended {
			writeError(w, 409, "This website is manually suspended")
			return
		}
		if err := resourceHostAction(r.Context(), a.system, *site, *site.ResourceLimits, "retry"); err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == http.MethodDelete {
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if decodeJSONWithLimit(r, &body, 4096) != nil || !body.Confirm {
			writeError(w, 400, "Confirm removal of the saved resource settings.")
			return
		}
		if site.ResourceLimits != nil && site.ResourceLimits.Enabled {
			writeError(w, 409, "Disable enforcement before removing resource settings.")
			return
		}
		if site.ResourceLimits != nil {
			if err := a.store.UpdateSite(site.ID, func(s *Site) { s.ResourceLimits = nil }); err != nil {
				writeError(w, 500, "Could not remove resource settings")
				return
			}
			_ = a.store.AddActivity("Resource settings removed", site.Domain, "warning")
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	var p WebsiteResourceLimits
	if err := decodeJSONWithLimit(r, &p, 4096); err != nil {
		writeError(w, 400, "Invalid resource settings")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if resourceEnabled(*site) && p.Enabled {
		if p != *site.ResourceLimits {
			writeError(w, 409, "Disable enforcement before changing its allowance values, then enable it again.")
			return
		}
		st := resourceStatusForSite(*site)
		if !st.Enforced {
			writeError(w, 409, "The saved boundary requires recovery or revalidation; unchanged settings were retained.")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "state": st.State, "enforced": st.Enforced})
		return
	}
	if p.Enabled {
		caps := inspectResourceCapabilities(*site)
		if !caps.Ready {
			writeError(w, 409, "Cannot enable resource limits: "+strings.Join(caps.Prerequisites, "; "))
			return
		}
	}
	if p.Enabled || site.ResourceLimits != nil && site.ResourceLimits.Enabled {
		if a.system == nil {
			writeError(w, 409, "Linux host executor is unavailable")
			return
		}
		action := "disable"
		if p.Enabled {
			action = "apply"
		}
		if err := resourceHostAction(r.Context(), a.system, *site, p, action); err != nil {
			writeError(w, 502, "Resource transition failed: "+err.Error())
			return
		}
	}
	if err := a.store.UpdateSite(site.ID, func(s *Site) { s.ResourceLimits = &p }); err != nil {
		var rollback error
		if p.Enabled {
			rollback = resourceHostAction(context.Background(), a.system, *site, p, "disable")
		} else if resourceEnabled(*site) {
			rollback = resourceHostAction(context.Background(), a.system, *site, *site.ResourceLimits, "apply")
		}
		writeError(w, 500, fmt.Sprintf("Could not save resource settings; runtime rollback result: %v", rollback))
		return
	}
	activity := "Resource limit draft saved"
	if p.Enabled {
		activity = "Resource enforcement enabled"
	} else if resourceEnabled(*site) {
		activity = "Resource enforcement disabled"
	}
	_ = a.store.AddActivity(activity, site.Domain, "info")
	writeJSON(w, 200, map[string]any{"ok": true, "state": resourceRuntimeRead(site.ID).State, "enforced": p.Enabled})
}
