package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
)

// resourceSliceProperties defines aggregate kernel limits. Child services and
// containers must be placed in this slice before an enabled policy is exposed.
func resourceSliceProperties(p WebsiteResourceLimits, device string) ([]string, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	properties := []string{
		"CPUAccounting=yes", "MemoryAccounting=yes", "TasksAccounting=yes", "IOAccounting=yes",
		"CPUQuota=" + strconv.FormatFloat(p.CPUPercent, 'f', -1, 64) + "%",
		"CPUQuotaPeriodSec=100ms",
		fmt.Sprintf("MemoryMax=%d", p.MemoryMiB*1048576),
		"MemorySwapMax=0",
		fmt.Sprintf("TasksMax=%d", p.Tasks),
	}
	if p.ReadMiBPerSecond > 0 || p.WriteMiBPerSecond > 0 {
		if !path.IsAbs(device) || path.Clean(device) != device || !strings.HasPrefix(device, "/dev/") || strings.ContainsAny(device, " \t\r\n\x00\"'%;\\") {
			return nil, fmt.Errorf("disk I/O limits require a verified block device")
		}
		if p.ReadMiBPerSecond > 0 {
			properties = append(properties, fmt.Sprintf("IOReadBandwidthMax=%s %d", device, p.ReadMiBPerSecond*1048576))
		}
		if p.WriteMiBPerSecond > 0 {
			properties = append(properties, fmt.Sprintf("IOWriteBandwidthMax=%s %d", device, p.WriteMiBPerSecond*1048576))
		}
	}
	return properties, nil
}

// resourceSliceUnit renders a persistent slice. The executor must also set
// memory.oom.group=1 on the slice before starting child workloads and verify
// that value. MemoryOOMGroup is not a systemd slice setting; child services
// should use OOMPolicy=kill. Saving a draft never installs this unit.
func resourceSliceUnit(id string, p WebsiteResourceLimits, device string) (string, error) {
	if id == "" || len(id) > 128 {
		return "", fmt.Errorf("invalid website identity")
	}
	properties, err := resourceSliceProperties(p, device)
	if err != nil {
		return "", err
	}
	return "[Unit]\nDescription=LiteCP website resource boundary\nBefore=slices.target\n\n[Slice]\n" + strings.Join(properties, "\n") + "\n", nil
}

// verifyResourceSlice checks the kernel rather than trusting a successful
// systemctl exit status. A missing controller or unenforced value is an error.
var errResourceOOMGroup = errors.New("kernel memory.oom.group does not match the website policy")

func verifyResourceSlice(p WebsiteResourceLimits, read func(string) ([]byte, error)) error {
	if err := p.validate(); err != nil {
		return err
	}
	readText := func(name string) (string, error) {
		b, err := read(name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	cpu, err := readText("cpu.max")
	if err != nil {
		return err
	}
	f := strings.Fields(cpu)
	if len(f) != 2 || f[0] == "max" {
		return fmt.Errorf("CPU quota is not enforced")
	}
	quota, e1 := strconv.ParseFloat(f[0], 64)
	period, e2 := strconv.ParseFloat(f[1], 64)
	if e1 != nil || e2 != nil || math.IsNaN(quota) || math.IsInf(quota, 0) || math.IsNaN(period) || math.IsInf(period, 0) || quota <= 0 || period <= 0 || math.Abs(quota/period*100-p.CPUPercent) > 0.11 {
		return fmt.Errorf("kernel CPU allowance does not match the website policy")
	}
	for name, wanted := range map[string]int64{"memory.max": p.MemoryMiB * 1048576, "memory.swap.max": 0, "pids.max": int64(p.Tasks)} {
		value, err := readText(name)
		if err != nil {
			return err
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n != wanted {
			return fmt.Errorf("kernel %s does not match the website policy", name)
		}
	}
	value, err := readText("memory.oom.group")
	if err != nil {
		return err
	}
	if value != "1" {
		return errResourceOOMGroup
	}
	return nil
}

// A slice may only be stopped after HTTP blocking is confirmed. If the block
// fails, workloads still have their kernel caps; do not report suspension.
type resourceRuntimeActions struct {
	BlockHTTP      func(context.Context) error
	StopWorkloads  func(context.Context) error
	StartWorkloads func(context.Context) error
	Health         func(context.Context) error
	AllowHTTP      func(context.Context) error
	Save           func(resourcePolicyState) error
}

func resourceBlock(ctx context.Context, state resourcePolicyState, actions resourceRuntimeActions) error {
	if state.State != "Resource blocked" {
		return fmt.Errorf("invalid resource block transition")
	}
	if err := actions.BlockHTTP(ctx); err != nil {
		return fmt.Errorf("block website access: %w", err)
	}
	// Persist the cooldown before stopping processes so a monitor restart cannot
	// forget a block or automatically revive the site early.
	if err := actions.Save(state); err != nil {
		return fmt.Errorf("persist resource block: %w", err)
	}
	if err := actions.StopWorkloads(ctx); err != nil {
		return fmt.Errorf("stop website workloads: %w", err)
	}
	return nil
}

func resourceRecover(ctx context.Context, p WebsiteResourceLimits, state resourcePolicyState, now time.Time, manuallySuspended bool, diskBytes int64, actions resourceRuntimeActions) (resourcePolicyState, error) {
	if !p.Enabled || manuallySuspended || state.State != "Resource blocked" || now.Before(state.NextRecovery) {
		return state, nil
	}
	if diskBytes < 0 {
		return state, fmt.Errorf("quota usage is unavailable; recovery remains blocked")
	}
	if state.Reason == "Disk" && float64(diskBytes) >= float64(p.DiskMiB)*1048576*.95 {
		state.NextRecovery = now.Add(5 * time.Minute)
		return state, actions.Save(state)
	}
	recovering := state
	recovering.State = "Recovering"
	if err := actions.Save(recovering); err != nil {
		return state, err
	}
	fail := func(cause error) (resourcePolicyState, error) {
		// Retain a closed HTTP gate if a partial recovery failed.
		blockErr := actions.BlockHTTP(ctx)
		stopErr := actions.StopWorkloads(ctx)
		state.NextRecovery = now.Add(5 * time.Minute)
		saveErr := actions.Save(state)
		return state, fmt.Errorf("recovery failed: %v (HTTP guard: %v; stop: %v; persist: %v)", cause, blockErr, stopErr, saveErr)
	}
	if err := actions.StartWorkloads(ctx); err != nil {
		return fail(err)
	}
	if err := actions.Health(ctx); err != nil {
		return fail(err)
	}
	// Persist Active before opening the HTTP gate. A failed open must return to
	// blocked state, preserving the cooldown and stopping partially started work.
	active := resourcePolicyState{State: "Active"}
	if err := actions.Save(active); err != nil {
		return fail(err)
	}
	if err := actions.AllowHTTP(ctx); err != nil {
		return fail(err)
	}
	return active, nil
}
