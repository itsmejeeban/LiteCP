package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (a *App) uploadWebsiteZIP(ctx context.Context, directory string, item *websiteZIPBackup, destination BackupDestination, remoteOnly bool, temporaryFiles []string) error {
	transfer := filepath.Join(directory, item.Domain+"-"+item.ID+".zip")
	// A unique name prevents archives from different websites overwriting remotely.
	if err := os.Link(filepath.Join(directory, "website.zip"), transfer); err != nil {
		return err
	}
	remote, err := a.system.UploadBackup(ctx, destination, transfer)
	if err != nil {
		return err
	}
	record := Backup{Path: transfer, RemotePath: remote, Size: item.Size}
	if err = a.system.historyRemoteOperation(ctx, &destination, record, "stat", ""); err != nil {
		return fmt.Errorf("remote verification failed; local backup retained: %w", err)
	}
	item.RemotePath, item.DestinationID, item.RemoteOnly = remote, destination.ID, remoteOnly
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(directory, "manifest.json"), data, 0600); err != nil {
		return err
	}
	if remoteOnly {
		for _, name := range append([]string{"public_html.zip", "databases.zip", "home_directories.zip", "website.zip", filepath.Base(transfer)}, temporaryFiles...) {
			if filepath.Base(name) != name {
				return fmt.Errorf("Invalid temporary backup filename")
			}
			if err = os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("Remote backup uploaded; local cleanup needs review: %w", err)
			}
		}
	}
	return nil
}

func (a *App) websiteBackupArchive(ctx context.Context, root, id, siteID string) (string, func(), error) {
	item, err := loadWebsiteZIPBackup(root, id, siteID)
	if err != nil {
		return "", func() {}, err
	}
	if !item.RemoteOnly {
		return filepath.Join(root, id, "website.zip"), func() {}, nil
	}
	destination := a.store.BackupDestinationByID(item.DestinationID)
	if destination == nil {
		return "", func() {}, fmt.Errorf("Remote destination is no longer configured")
	}
	file, err := os.CreateTemp(filepath.Join(root, id), ".retrieved-*.zip")
	if err != nil {
		return "", func() {}, err
	}
	local := file.Name()
	file.Close()
	cleanup := func() { _ = os.Remove(local) }
	record := Backup{Path: item.Domain + "-" + item.ID + ".zip", RemotePath: item.RemotePath, Size: item.Size}
	if err = a.system.historyRemoteOperation(ctx, destination, record, "download", local); err != nil {
		cleanup()
		return "", func() {}, err
	}
	info, err := os.Stat(local)
	if err != nil || info.Size() != item.Size {
		cleanup()
		return "", func() {}, fmt.Errorf("Remote backup size does not match its record")
	}
	return local, cleanup, nil
}
