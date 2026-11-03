package main

import (
	"context"
	"testing"
)

func TestMigrationHealthSettlesAfterTransientForbidden(t *testing.T) {
	calls := 0
	code, err := retryMigrationHealth(context.Background(), 0, func() (string, error) {
		calls++
		if calls < 3 {
			return "403", nil
		}
		return "200", nil
	})
	if err != nil || code != "200" || calls != 3 {
		t.Fatalf("code=%s calls=%d err=%v", code, calls, err)
	}
}
func TestMigrationHealthPreservesPersistentFailure(t *testing.T) {
	calls := 0
	code, err := retryMigrationHealth(context.Background(), 0, func() (string, error) { calls++; return "403", nil })
	if err != nil || code != "403" || calls != 4 {
		t.Fatalf("code=%s calls=%d err=%v", code, calls, err)
	}
}
func TestMigrationHealthHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retryMigrationHealth(ctx, 0, func() (string, error) { t.Fatal("probe after cancellation"); return "200", nil })
	if err != context.Canceled {
		t.Fatal(err)
	}
}
