package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResourceSliceKernelVerification(t *testing.T) {
	p := validResourcePolicy()
	files := map[string]string{"cpu.max": "50000 100000", "memory.max": "268435456", "memory.swap.max": "0", "memory.oom.group": "1", "pids.max": "64"}
	read := func(name string) ([]byte, error) {
		v, ok := files[name]
		if !ok {
			return nil, errors.New("missing controller")
		}
		return []byte(v), nil
	}
	if err := verifyResourceSlice(p, read); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{"cpu.max": "max 100000", "memory.max": "max", "memory.swap.max": "1024", "memory.oom.group": "0", "pids.max": "max"} {
		original := files[name]
		files[name] = bad
		if verifyResourceSlice(p, read) == nil {
			t.Fatalf("accepted unenforced %s", name)
		}
		files[name] = original
	}
	delete(files, "pids.max")
	if verifyResourceSlice(p, read) == nil {
		t.Fatal("missing controller accepted")
	}
}

func TestResourceSliceUnitUsesAggregateCaps(t *testing.T) {
	p := validResourcePolicy()
	p.ReadMiBPerSecond = 2
	p.WriteMiBPerSecond = 3
	unit, err := resourceSliceUnit("a-b", p, "/dev/sda1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CPUQuota=50%", "MemoryMax=268435456", "MemorySwapMax=0", "TasksMax=64", "IOReadBandwidthMax=/dev/sda1 2097152", "IOWriteBandwidthMax=/dev/sda1 3145728"} {
		if !strings.Contains(unit, want) {
			t.Fatal("missing", want)
		}
	}
	if _, err = resourceSliceUnit("a", p, "/dev/sda1\nExecStart=evil"); err == nil {
		t.Fatal("unit injection accepted")
	}
}

func resourceTestActions(events *[]string) resourceRuntimeActions {
	return resourceRuntimeActions{
		BlockHTTP:      func(context.Context) error { *events = append(*events, "block"); return nil },
		StopWorkloads:  func(context.Context) error { *events = append(*events, "stop"); return nil },
		StartWorkloads: func(context.Context) error { *events = append(*events, "start"); return nil },
		Health:         func(context.Context) error { *events = append(*events, "health"); return nil },
		AllowHTTP:      func(context.Context) error { *events = append(*events, "allow"); return nil },
		Save:           func(s resourcePolicyState) error { *events = append(*events, "save:"+s.State); return nil },
	}
}

func TestResourceBlockPersistsBeforeStopping(t *testing.T) {
	events := []string{}
	actions := resourceTestActions(&events)
	if err := resourceBlock(context.Background(), resourcePolicyState{State: "Resource blocked"}, actions); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "block,save:Resource blocked,stop" {
		t.Fatal(events)
	}
	events = nil
	actions.BlockHTTP = func(context.Context) error { return errors.New("nginx failed") }
	if resourceBlock(context.Background(), resourcePolicyState{State: "Resource blocked"}, actions) == nil || len(events) != 0 {
		t.Fatal("continued after guard failed", events)
	}
}

func TestResourceRecoveryDoesNotUndoManualSuspension(t *testing.T) {
	p := validResourcePolicy()
	now := time.Now()
	blocked := resourcePolicyState{State: "Resource blocked", Reason: "CPU", NextRecovery: now.Add(-time.Second)}
	events := []string{}
	actions := resourceTestActions(&events)
	state, err := resourceRecover(context.Background(), p, blocked, now, true, 0, actions)
	if err != nil || state.State != "Resource blocked" || len(events) != 0 {
		t.Fatal(state, events, err)
	}
	state, err = resourceRecover(context.Background(), p, blocked, now, false, 0, actions)
	if err != nil || state.State != "Active" || strings.Join(events, ",") != "save:Recovering,start,health,save:Active,allow" {
		t.Fatal(state, events, err)
	}
}

func TestResourceFailedRecoveryKeepsHTTPClosedAndRenewsCooldown(t *testing.T) {
	now := time.Now()
	state := resourcePolicyState{State: "Resource blocked", Reason: "Memory", NextRecovery: now.Add(-time.Second)}
	events := []string{}
	actions := resourceTestActions(&events)
	actions.Health = func(context.Context) error { return errors.New("PHP not ready") }
	next, err := resourceRecover(context.Background(), validResourcePolicy(), state, now, false, 0, actions)
	if err == nil || next.State != "Resource blocked" || !next.NextRecovery.Equal(now.Add(5*time.Minute)) || strings.Contains(strings.Join(events, ","), "allow") {
		t.Fatal(next, events, err)
	}
}
