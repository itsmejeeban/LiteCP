package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func liveCaptureUnit(id string) string { return "litecp-capture-" + id }
func liveCaptureActive(id string) bool {
	if !liveJobIDPattern.MatchString(id) {
		return false
	}
	output, err := runCommand(context.Background(), 3*time.Second, "systemctl", "is-active", liveCaptureUnit(id))
	return err == nil && strings.TrimSpace(output) == "active"
}
func runLiveCaptureWorker(id string) error {
	if os.Geteuid() != 0 || !liveJobIDPattern.MatchString(id) {
		return fmt.Errorf("invalid capture worker request")
	}
	data := envOr("LITECP_DATA_DIR", "/var/lib/litecp")
	store, err := OpenStore(data)
	if err != nil {
		return err
	}
	app := &App{store: store, system: NewSystem(data)}
	job, err := app.loadLiveJob(id)
	if err != nil {
		return err
	}
	if !liveJobCancellable(job) {
		return nil
	}
	app.captureLiveMigration(job)
	return nil
}
