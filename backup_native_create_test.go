package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeBackupCreationIncludesPrivateFilesAndAllDatabaseDumps(t *testing.T) {
	home := t.TempDir()
	public := filepath.Join(home, "public_html")
	privateData := bytes.Repeat([]byte("private application data that must be restored\n"), 180000)
	fixtures := map[string][]byte{
		"public_html/index.php":                  []byte("<?php echo 'public';"),
		"application/.env":                       []byte("APP_ENV=testing"),
		"application/storage/campaigns/data.bin": privateData,
		"backups/scheduled/old.tar.gz":           []byte("retained old backup"),
	}
	var expectedFiles int64
	for name, data := range fixtures {
		path := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, "backups/") {
			expectedFiles += int64(len(data))
		}
	}
	site := Site{ID: "fixture", Domain: "fixture.example", Root: public, Database: "primary_db"}
	databases := []ManagedDatabase{{SiteID: site.ID, Name: "primary_db"}, {SiteID: site.ID, Name: "secondary_db"}, {SiteID: "other", Name: "unrelated_db"}}
	var dumped []string
	var expectedSQL int64
	dump := func(_ context.Context, name, destination string) error {
		dumped = append(dumped, name)
		data := []byte("-- verified dump for " + name + "\nCREATE TABLE fixture (id INT);\n")
		expectedSQL += int64(len(data))
		return os.WriteFile(destination, data, 0600)
	}
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	backup, err := publishNativeWebsiteBackup(context.Background(), site, databases, Backup{Path: path}, home, dump)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dumped, []string{"primary_db", "secondary_db"}) || backup.Status != "complete" || backup.Scope != "website-home" || backup.FileBytes == nil || *backup.FileBytes != expectedFiles || backup.DatabaseDumpBytes == nil || *backup.DatabaseDumpBytes != expectedSQL {
		t.Fatalf("incomplete creation: %+v, databases=%v", backup, dumped)
	}
	if backup.Size >= expectedFiles/10 {
		t.Fatal("fixture should demonstrate large compression without data loss", backup.Size, expectedFiles)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	stage := t.TempDir()
	preview, err := stageNativeBackup(context.Background(), input, stage, 32<<20, func(context.Context, string) (int64, error) { return 2 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Databases, []string{"databases/primary_db.sql", "databases/secondary_db.sql"}) {
		t.Fatal(preview.Databases)
	}
	for source, data := range fixtures {
		if strings.HasPrefix(source, "backups/") {
			continue
		}
		relative := "home/" + source
		if strings.HasPrefix(source, "public_html/") {
			relative = source
		}
		restored, err := os.ReadFile(filepath.Join(stage, "files", filepath.FromSlash(relative)))
		if err != nil || sha256.Sum256(restored) != sha256.Sum256(data) {
			t.Fatalf("file differs after restore staging: %s (%v)", source, err)
		}
	}
	for _, name := range dumped {
		if _, err := os.Stat(filepath.Join(stage, "files", "databases", name+".sql")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeBackupDumpFailureNeverPublishesCompleteArchive(t *testing.T) {
	public := t.TempDir()
	if err := os.WriteFile(filepath.Join(public, "index.html"), []byte("keep website"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "failed-backup.tar.gz")
	result, err := publishNativeWebsiteBackup(context.Background(), Site{ID: "fixture", Root: public, Database: "primary_db"}, nil, Backup{Path: path}, "", func(_ context.Context, _ string, destination string) error {
		if err := os.WriteFile(destination, []byte("partial SQL"), 0600); err != nil {
			return err
		}
		return errors.New("export failed")
	})
	if err == nil || result.Status == "complete" {
		t.Fatal("failed dump reported complete", result, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("partial archive was published", err)
	}
	data, err := os.ReadFile(filepath.Join(public, "index.html"))
	if err != nil || string(data) != "keep website" {
		t.Fatal("source website changed", err)
	}
}

func TestNativeBackupRoundTripShowsActualFileCoverage(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	public := filepath.Join(home, "public_html")
	parts := t.TempDir()
	body := strings.Repeat("compressible website data\n", 8192)
	private := "private application settings"
	for path, data := range map[string]string{
		"public_html/index.php":                  body,
		"application/config.ini":                 private,
		"backups/scheduled/previous.tar.gz":      "previous archive",
		".auto-migration-abc-previous/index.php": "previous restore",
		".trash/old.txt":                         "trash",
		"logs/large.log":                         "log",
	} {
		destination := filepath.Join(home, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := writeNativeBackupTree(ctx, public, filepath.Join(parts, "website.tar.gz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	extras, err := writeNativeBackupTree(ctx, home, filepath.Join(parts, "home.tar.gz"), excludedNativeHomeBackupPath)
	if err != nil || files != int64(len(body)) || extras != int64(len(private)) {
		t.Fatalf("source counts are wrong: %d + %d, %v", files, extras, err)
	}
	sqlDir := filepath.Join(parts, "databases")
	if err := os.Mkdir(sqlDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"primary.sql", "secondary.sql"} {
		if err := os.WriteFile(filepath.Join(sqlDir, name), []byte("SELECT 1;"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	if _, err := writeNativeBackupTree(ctx, parts, archivePath, nil); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || info.Size() >= files+extras {
		t.Fatalf("expected a smaller compressed archive, %v", err)
	}
	stage := t.TempDir()
	preview, err := stageNativeBackup(ctx, input, stage, 1<<20, func(context.Context, string) (int64, error) {
		return 2 << 30, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Databases, []string{"databases/primary.sql", "databases/secondary.sql"}) ||
		!reflect.DeepEqual(preview.Roots, []string{"public_html"}) || preview.Bytes != files+extras+18 {
		t.Fatalf("incorrect restore contents: %+v", preview)
	}
	for path, want := range map[string]string{"public_html/index.php": body, "home/application/config.ini": private} {
		data, err := os.ReadFile(filepath.Join(stage, "files", filepath.FromSlash(path)))
		if err != nil || string(data) != want {
			t.Fatalf("staged file %s differs: %v", path, err)
		}
	}
}

func TestNativeBackupDatabaseNamesIncludeOnlyLinkedSchemas(t *testing.T) {
	site := Site{ID: "fixture", Database: "first_db"}
	names, err := backupDatabaseNames(site, []ManagedDatabase{
		{SiteID: "fixture", Name: "first_db"},
		{SiteID: "fixture", Name: "second_db"},
		{SiteID: "other", Name: "outside_db"},
	})
	if err != nil || !reflect.DeepEqual(names, []string{"first_db", "second_db"}) {
		t.Fatalf("wrong backup scope: %v, %v", names, err)
	}
}
