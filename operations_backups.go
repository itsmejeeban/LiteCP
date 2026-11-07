package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type backupVerification struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	SHA256     string    `json:"sha256,omitempty"`
	Baseline   string    `json:"baseline,omitempty"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	CheckedAt  time.Time `json:"checkedAt"`
	Message    string    `json:"message"`
}
type operationsBackup struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Kind               string    `json:"kind"`
	Size               int64     `json:"size"`
	CreatedAt          time.Time `json:"createdAt"`
	Remote             bool      `json:"remote"`
	RetentionCandidate bool      `json:"retentionCandidate"`
	Path               string    `json:"-"`
}

var operationsVerifyMu sync.Mutex

// Protected by operationsMu. Only one verifier reads archive data at a time.
var operationsVerifyID string
var operationsVerifyCancel context.CancelFunc

func (a *App) operationsBackupInventory() []operationsBackup {
	items := []operationsBackup{}
	if a.system.demo {
		return items
	}
	for _, b := range a.store.PublicState().Backups {
		if backupInProgress(b.Status) {
			continue
		}
		if availableHistoryLocal(b) {
			items = append(items, operationsBackup{ID: "history:" + b.ID, Name: b.Domain + " / " + filepath.Base(b.Path), Kind: "History archive", Size: b.Size, CreatedAt: b.CreatedAt, Path: b.Path, Remote: b.RemotePath != ""})
		}
	}
	for _, site := range a.store.Sites() {
		root := a.system.websiteBackupsRoot(site)
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			v, e := loadWebsiteZIPBackup(root, entry.Name(), site.ID)
			if e == nil && !v.RemoteOnly {
				items = append(items, operationsBackup{ID: "website:" + site.ID + ":" + v.ID, Name: site.Domain + " / " + v.ID, Kind: "Website ZIP", Size: v.Size, CreatedAt: v.CreatedAt, Path: filepath.Join(root, v.ID, "website.zip"), Remote: v.RemotePath != ""})
			}
		}
	}
	entries, _ := os.ReadDir(snapshotRoot)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, e := entry.Info()
		if e == nil && info.Size() > 0 {
			items = append(items, operationsBackup{ID: "snapshot:" + entry.Name(), Name: entry.Name(), Kind: "System snapshot", Size: info.Size(), CreatedAt: info.ModTime(), Path: filepath.Join(snapshotRoot, entry.Name())})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}

type verificationReader struct {
	ctx    context.Context
	source io.Reader
}

func (r verificationReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.source.Read(p)
}

// Read-only archive verification. Never extracts, runs SQL, changes files or
// claims application compatibility. Bound decoded bytes to resist ZIP bombs.
func verifyOperationsArchive(ctx context.Context, item operationsBackup) (backupVerification, error) {
	v := backupVerification{ID: item.ID, CheckedAt: time.Now().UTC(), Status: "failed"}
	f, e := secureOpenManaged(filepath.Dir(item.Path), filepath.Base(item.Path), os.O_RDONLY, 0)
	if e != nil {
		return v, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil {
		return v, e
	}
	if !before.Mode().IsRegular() || before.Size() != item.Size {
		return v, fmt.Errorf("Archive identity or size changed")
	}
	v.Size = before.Size()
	v.ModifiedAt = before.ModTime()
	h := sha256.New()
	if _, e = io.Copy(h, verificationReader{ctx, f}); e != nil {
		return v, e
	}
	v.SHA256 = hex.EncodeToString(h.Sum(nil))
	if _, e = f.Seek(0, 0); e != nil {
		return v, e
	}
	const decodedLimit int64 = 64 << 30
	var decoded int64
	members := 0
	consume := func(reader io.Reader, expected int64) error {
		if expected < 0 || expected > decodedLimit-decoded {
			return fmt.Errorf("Archive exceeds 64 GiB verification budget")
		}
		n, err := io.Copy(io.Discard, io.LimitReader(verificationReader{ctx, reader}, expected+1))
		decoded += n
		if err != nil {
			return err
		}
		if n != expected {
			return fmt.Errorf("Archive member length mismatch")
		}
		return nil
	}
	if strings.HasSuffix(strings.ToLower(item.Path), ".zip") {
		z, err := zip.NewReader(f, before.Size())
		if err != nil {
			return v, err
		}
		if len(z.File) == 0 || len(z.File) > maxArchiveMembers {
			return v, fmt.Errorf("Empty or excessive ZIP members")
		}
		seen := map[string]bool{}
		for _, entry := range z.File {
			name, err := safeArchiveName(entry.Name)
			if err != nil || seen[name] || entry.Mode()&os.ModeSymlink != 0 {
				return v, fmt.Errorf("Unsafe, duplicate or linked ZIP member")
			}
			seen[name] = true
			if entry.FileInfo().IsDir() {
				continue
			}
			if entry.UncompressedSize64 > uint64(decodedLimit-decoded) {
				return v, fmt.Errorf("Archive exceeds verification budget")
			}
			stream, err := entry.Open()
			if err != nil {
				return v, err
			}
			err = consume(stream, int64(entry.UncompressedSize64))
			closeErr := stream.Close()
			if err != nil {
				return v, err
			}
			if closeErr != nil {
				return v, closeErr
			}
			members++
		}
	} else {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return v, err
		}
		defer gz.Close()
		tr := tar.NewReader(verificationReader{ctx, gz})
		seen := map[string]bool{}
		for {
			entry, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return v, err
			}
			name, err := safeArchiveName(entry.Name)
			if err != nil || seen[name] || len(seen) >= maxArchiveMembers {
				return v, fmt.Errorf("Unsafe or excessive TAR members")
			}
			seen[name] = true
			if entry.Typeflag == tar.TypeDir {
				continue
			}
			if entry.Typeflag != tar.TypeReg && entry.Typeflag != tar.TypeRegA {
				return v, fmt.Errorf("Linked or special TAR member")
			}
			if err = consume(tr, entry.Size); err != nil {
				return v, err
			}
			members++
		}
		// Reading through the gzip trailer verifies its checksum as well.
		n, err := io.Copy(io.Discard, io.LimitReader(verificationReader{ctx, gz}, decodedLimit-decoded+1))
		if err != nil {
			return v, err
		}
		if n > decodedLimit-decoded {
			return v, fmt.Errorf("Archive exceeds verification budget")
		}
	}
	after, e := f.Stat()
	if e != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return v, fmt.Errorf("Archive changed during verification")
	}
	if members == 0 {
		return v, fmt.Errorf("No file members in archive")
	}
	v.Status = "verified"
	v.Message = fmt.Sprintf("SHA-256 and archive integrity checked (%d members). Restore compatibility requires a separate test.", members)
	return v, nil
}
func (a *App) verifyOperationsBackup(ctx context.Context, item operationsBackup) {
	v, e := verifyOperationsArchive(ctx, item)
	if e != nil {
		v.Message = "Archive integrity check failed: " + e.Error()
		if ctx.Err() == context.Canceled {
			v.Status = "cancelled"
			v.Message = "Verification cancelled."
		}
	}
	operationsMu.Lock()
	s, err := a.readOperations()
	if err == nil {
		previous := s.Verification[item.ID]
		v.Baseline = previous.Baseline
		if v.Baseline == "" && previous.Status == "verified" {
			v.Baseline = previous.SHA256
		}
		if v.Status == "verified" {
			if v.Baseline == "" {
				v.Baseline = v.SHA256
			}
			if v.Baseline != v.SHA256 {
				v.Status = "changed"
				v.Message = "Archive checksum changed since initial verification. Review before restoring."
			}
		}
		s.Verification[item.ID] = v
		err = a.saveOperations(s)
	}
	operationsMu.Unlock()
	if err != nil {
		_ = a.store.AddActivity("Backup verification state error", "Verification results could not be saved; archives were preserved.", "warning")
	}
}
func (a *App) startOperationsVerification(id string) error {
	var selected *operationsBackup
	for _, v := range a.operationsBackupInventory() {
		if v.ID == id {
			copy := v
			selected = &copy
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("Local backup not found")
	}
	return a.beginOperationsVerification(*selected)
}

func (a *App) beginOperationsVerification(selected operationsBackup) error {
	if !operationsVerifyMu.TryLock() {
		return fmt.Errorf("Another archive verification is running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	id := selected.ID
	operationsMu.Lock()
	s, e := a.readOperations()
	if e == nil {
		previous := s.Verification[id]
		if previous.Baseline == "" && previous.Status == "verified" {
			previous.Baseline = previous.SHA256
		}
		previous.ID = id
		previous.Status = "running"
		previous.CheckedAt = time.Now().UTC()
		previous.Message = "Read-only archive verification running"
		s.Verification[id] = previous
		e = a.saveOperations(s)
	}
	if e == nil {
		operationsVerifyID, operationsVerifyCancel = id, cancel
	}
	operationsMu.Unlock()
	if e != nil {
		cancel()
		operationsVerifyMu.Unlock()
		return e
	}
	go func() {
		defer operationsVerifyMu.Unlock()
		defer cancel()
		a.verifyOperationsBackup(ctx, selected)
		operationsMu.Lock()
		operationsVerifyID, operationsVerifyCancel = "", nil
		operationsMu.Unlock()
	}()
	return nil
}
func (a *App) verifyNextOperationsBackup() {
	operationsMu.Lock()
	s, e := a.readOperations()
	operationsMu.Unlock()
	if e != nil {
		return
	}
	for _, v := range a.operationsBackupInventory() {
		old, ok := s.Verification[v.ID]
		if !ok || old.Status == "interrupted" || time.Since(old.CheckedAt) > 7*24*time.Hour {
			_ = a.beginOperationsVerification(v)
			return
		}
	}
}

func retentionCandidates(backups []Backup, s operationsState, now time.Time) []Backup {
	if !s.Settings.RetentionEnabled {
		return nil
	}
	latest := map[string]time.Time{}
	for _, b := range backups {
		v := s.Verification["history:"+b.ID]
		if b.Status == "complete" && b.RemotePath == "" && v.Status == "verified" && now.Sub(v.CheckedAt) <= 8*24*time.Hour && b.CreatedAt.After(latest[b.SiteID]) {
			latest[b.SiteID] = b.CreatedAt
		}
	}
	items := []Backup{}
	cutoff := now.Add(-time.Duration(s.Settings.RetentionDays) * 24 * time.Hour)
	for _, b := range backups {
		v, ok := s.Verification["history:"+b.ID]
		if !ok || v.Status != "verified" || b.RemotePath != "" || b.Status != "complete" || !b.CreatedAt.Before(cutoff) || !b.CreatedAt.Before(latest[b.SiteID]) || now.Sub(v.CheckedAt) > 8*24*time.Hour {
			continue
		}
		items = append(items, b)
	}
	return items
}
