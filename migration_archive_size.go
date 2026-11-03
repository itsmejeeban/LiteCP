package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
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
		if _, e = safeMigrationArchiveName(header.Name); e != nil {
			return 0, fmt.Errorf("archive entry %q: %w", header.Name, e)
		}
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
