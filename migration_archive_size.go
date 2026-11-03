package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
)

// Inspect the same regular files/directories that extraction accepts. Stream
// headers instead of retaining Python TarInfo objects for a large account.
func migrationArchiveBytes(ctx context.Context, archivePath string) (int64, error) {
	input, err := os.Open(archivePath)
	if err != nil {
		return 0, fmt.Errorf("open website archive: %w", err)
	}
	defer input.Close()
	var stream io.Reader = input
	var magic [2]byte
	_, err = io.ReadFull(input, magic[:])
	if err != nil {
		return 0, fmt.Errorf("read archive header: %w", err)
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, e := gzip.NewReader(input)
		if e != nil {
			return 0, fmt.Errorf("read compressed archive: %w", e)
		}
		defer gz.Close()
		stream = gz
	}
	reader := tar.NewReader(migrationContextReader{ctx, stream})
	var bytes, space int64
	paths := map[string]bool{} // true for directories, including implicit parents
	for entries := 0; ; entries++ {
		header, e := reader.Next()
		if e == io.EOF {
			return space, nil
		}
		if e != nil {
			return 0, fmt.Errorf("inspect website archive: %w", e)
		}
		if entries >= liveMigrationMemberLimit {
			return 0, fmt.Errorf("archive exceeds %d entries", liveMigrationMemberLimit)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			continue
		}
		clean, nameErr := safeMigrationArchiveName(header.Name)
		if nameErr != nil {
			e = nameErr
			return 0, fmt.Errorf("archive entry %q: %w", header.Name, e)
		}
		if clean == "." {
			continue
		}
		directory := header.Typeflag == tar.TypeDir
		if wasDirectory, exists := paths[clean]; exists && (!wasDirectory || !directory) {
			return 0, fmt.Errorf("archive path conflict at %q: a file and another entry use the same destination; correct the source archive before retrying (no files were extracted)", clean)
		}
		for parent := path.Dir(clean); parent != "."; parent = path.Dir(parent) {
			if isDirectory, exists := paths[parent]; exists && !isDirectory {
				return 0, fmt.Errorf("archive path conflict at %q: parent %q is a file (no files were extracted)", clean, parent)
			}
			paths[parent] = true
		}
		paths[clean] = directory
		if header.Size < 0 || header.Size > liveMigrationExtractLimit-bytes {
			return 0, fmt.Errorf("uncompressed website data exceeds the 64 GiB extraction limit")
		}
		bytes += header.Size
		// Round data to filesystem blocks and budget one metadata block per entry.
		space += ((header.Size+4095)/4096)*4096 + 4096
	}
}

type migrationContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r migrationContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
