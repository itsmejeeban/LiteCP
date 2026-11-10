package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFolderMigrationAcceptsLargeZIPMetadata(t *testing.T) {
	const size = 100 << 30
	entry := &zip.File{FileHeader: zip.FileHeader{UncompressedSize64: size}}
	required, err := migrationFolderZIPSpace([]*zip.File{entry})
	if err != nil || required < size {
		t.Fatalf("large archive size was rejected or undercounted: %d, %v", required, err)
	}
}

func folderMigrationTarFixture(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	compressed := gzip.NewWriter(&encoded)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "./", Mode: 0700, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for name, content := range entries {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestFolderMigrationStagesSupportedBackupLayouts(t *testing.T) {
	const index = "website files"
	const dump = "CREATE TABLE fixture(id INT);"
	for _, tc := range []struct {
		name, platform, root, sql string
		scheduled, nestedZIP      bool
	}{
		{name: "native cPanel", platform: "cpanel", root: "cpmove-account/homedir/public_html", sql: "cpmove-account/mysql/account_data.sql"},
		{name: "Plesk export", platform: "plesk", root: "domain/httpdocs", sql: "domain/databases/site.sql"},
		{name: "LiteCP manual ZIP", platform: "litecp", root: "public_html", sql: "databases/site.sql", nestedZIP: true},
		{name: "LiteCP scheduled archive", platform: "litecp", root: "public_html", sql: "database.sql", scheduled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceDir := t.TempDir()
			path := filepath.Join(sourceDir, "backup.tar.gz")
			var content []byte
			switch {
			case tc.nestedZIP:
				path = filepath.Join(sourceDir, "website.zip")
				web := filepath.Join(sourceDir, "public.zip")
				sql := filepath.Join(sourceDir, "database.zip")
				home := filepath.Join(sourceDir, "private.zip")
				manualZIPFixture(t, web, map[string]string{"public_html/index.html": index})
				manualZIPFixture(t, sql, map[string]string{"site.sql": dump})
				manualZIPFixture(t, home, map[string]string{"home/private/settings.ini": "private"})
				if err := zipBackupParts(path, map[string]string{"public_html.zip": web, "databases.zip": sql, "home_directories.zip": home}); err != nil {
					t.Fatal(err)
				}
				var err error
				content, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			case tc.scheduled:
				content = folderMigrationTarFixture(t, map[string][]byte{
					"./website.tar.gz": folderMigrationTarFixture(t, map[string][]byte{"./index.html": []byte(index)}),
					"./database.sql":   []byte(dump),
				})
			default:
				content = folderMigrationTarFixture(t, map[string][]byte{
					tc.root + "/index.html": []byte(index),
					tc.sql:                  []byte(dump),
				})
			}
			if !tc.nestedZIP {
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			app := folderMigrationFixtureApp(t, fixtureBackupFreeBytes)
			preview, err := app.stageFolderMigration(context.Background(), tc.platform, input)
			if err != nil || !reflect.DeepEqual(preview.Roots, []string{tc.root}) || !reflect.DeepEqual(preview.Databases, []string{tc.sql}) {
				t.Fatalf("unexpected stage: %+v, %v", preview, err)
			}
			_, stage, err := app.loadMigration(preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{tc.root + "/index.html": index, tc.sql: dump} {
				got, err := os.ReadFile(filepath.Join(stage, "files", filepath.FromSlash(name)))
				if err != nil || string(got) != want {
					t.Fatalf("staged %q: %q, %v", name, got, err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, content) {
				t.Fatal("source backup changed")
			}
		})
	}
}

func TestFolderMigrationChecksCapacityBeforeStaging(t *testing.T) {
	archive := folderMigrationTarFixture(t, map[string][]byte{"homedir/public_html/index.html": []byte("website")})
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	app := folderMigrationFixtureApp(t, func(context.Context, string) (int64, error) { return folderMigrationReserve, nil })
	if _, err := app.stageFolderMigration(context.Background(), "cpanel", input); !errors.Is(err, errFolderMigrationSpace) {
		t.Fatalf("expected capacity rejection, got %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(app.system.dataDir, "migrations"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("stage written despite capacity rejection: %v, %v", entries, err)
	}
}

func folderMigrationFixtureApp(t *testing.T, freeBytes func(context.Context, string) (int64, error)) *App {
	t.Helper()
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return &App{store: store, system: &System{dataDir: root, manualBackupSpaceProbe: freeBytes}}
}
