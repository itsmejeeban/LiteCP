package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPHPPoolRuntimeGracefulReloadAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		container bool
		failure   string
		result    string
		want      []string
		wantError bool
	}{
		{name: "native reload", want: []string{"reload-or-restart"}},
		{name: "legacy restart", container: true, want: []string{"restart"}},
		{name: "recover rate limit", failure: "reload-or-restart", result: "start-limit-hit\n", want: []string{"reload-or-restart", "show", "reset-failed", "start"}},
		{name: "configuration failure preserved", failure: "reload-or-restart", result: "exit-code", want: []string{"reload-or-restart", "show"}, wantError: true},
		{name: "reset failure preserved", failure: "reload-or-restart,reset-failed", result: "start-limit-hit", want: []string{"reload-or-restart", "show", "reset-failed"}, wantError: true},
		{name: "start failure preserved", failure: "reload-or-restart,start", result: "start-limit-hit", want: []string{"reload-or-restart", "show", "reset-failed", "start"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			run := func(_ context.Context, _ time.Duration, name string, args ...string) (string, error) {
				if name != "systemctl" || args[len(args)-1] != "php83-php-fpm.service" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				calls = append(calls, args[0])
				if strings.Contains(","+tc.failure+",", ","+args[0]+",") {
					return "", errors.New("service failed")
				}
				return tc.result, nil
			}
			err := applyPHPPoolRuntime(context.Background(), "php83-php-fpm.service", tc.container, run)
			if (err != nil) != tc.wantError || !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("calls %v, error %v", calls, err)
			}
		})
	}
}
