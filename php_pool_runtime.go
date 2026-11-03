package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Pool changes must not stop every other website on the same native FPM
// service. Graceful reloads also avoid exhausting systemd's start-rate limit
// during bulk restores. The legacy container does not implement ExecReload.
func applyPHPPoolRuntime(ctx context.Context, unit string, container bool, run func(context.Context, time.Duration, string, ...string) (string, error)) error {
	action := "reload-or-restart"
	if container {
		action = "restart"
	}
	_, applyErr := run(ctx, 30*time.Second, "systemctl", action, unit)
	if applyErr == nil {
		return nil
	}
	if ctx.Err() != nil {
		return applyErr
	}
	// Recover the specific historical restart-storm failure. Never retry a
	// syntax error or repeatedly reset a genuinely crashing service.
	result, err := run(ctx, 10*time.Second, "systemctl", "show", "--property=Result", "--value", unit)
	if err != nil || strings.TrimSpace(result) != "start-limit-hit" {
		return applyErr
	}
	if _, err := run(ctx, 10*time.Second, "systemctl", "reset-failed", unit); err != nil {
		return fmt.Errorf("PHP pool update failed (%v); reset start limit: %w", applyErr, err)
	}
	if _, err := run(ctx, 30*time.Second, "systemctl", "start", unit); err != nil {
		return fmt.Errorf("PHP pool update failed (%v); start after rate-limit reset: %w", applyErr, err)
	}
	return nil
}
