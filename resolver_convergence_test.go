package main

import (
	"context"
	"testing"
)

func TestDefaultResolversWaitForNetworkManager(t *testing.T) {
	reads := 0
	err := waitForDefaultResolvers(context.Background(), func() []string {
		reads++
		if reads < 3 { return []string{"185.12.64.2"} }
		return []string{"8.8.8.8", "8.8.4.4"}
	})
	if err != nil || reads != 3 { t.Fatalf("resolver convergence: reads=%d err=%v", reads, err) }
}

func TestDefaultResolversWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForDefaultResolvers(ctx, func() []string { return []string{"185.12.64.2"} }); err == nil {
		t.Fatal("unverified resolver state was accepted")
	}
}
