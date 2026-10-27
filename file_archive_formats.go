package main

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

func managedArchiveFormat(name string) string {
	name = strings.ToLower(name)
	for _, entry := range []struct{ suffix, format string }{{".tar.bz2", "tar.bz2"}, {".tbz2", "tar.bz2"}, {".tbz", "tar.bz2"}, {".tar.gz", "tar.gz"}, {".tgz", "tar.gz"}, {".tar", "tar"}, {".zip", "zip"}} {
		if strings.HasSuffix(name, entry.suffix) {
			return entry.format
		}
	}
	return ""
}

type managedArchiveOutput struct {
	zip        *zip.Writer
	tar        *tar.Writer
	compressor io.WriteCloser
	command    *exec.Cmd
}

func newManagedArchiveOutput(ctx context.Context, destination io.Writer, format string) (*managedArchiveOutput, error) {
	output := &managedArchiveOutput{}
	if format == "zip" {
		output.zip = zip.NewWriter(destination)
		return output, nil
	}
	switch format {
	case "tar.gz":
		output.compressor = gzip.NewWriter(destination)
		destination = output.compressor
	case "tar.bz2":
		output.command = exec.CommandContext(ctx, "bzip2", "-c")
		output.command.Stdout = destination
		input, err := output.command.StdinPipe()
		if err != nil {
			return nil, err
		}
		if err = output.command.Start(); err != nil {
			_ = input.Close()
			return nil, errors.New("bzip2 compression is unavailable; install the bzip2 package")
		}
		output.compressor = input
		destination = input
	case "tar":
	default:
		return nil, errors.New("unsupported archive format")
	}
	output.tar = tar.NewWriter(destination)
	return output, nil
}
func (output *managedArchiveOutput) Header(info os.FileInfo, name string) (io.Writer, error) {
	if output.zip != nil {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil, err
		}
		header.Name = name
		header.Method = zip.Deflate
		if info.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
		}
		return output.zip.CreateHeader(header)
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return nil, err
	}
	header.Name = name
	if info.IsDir() {
		header.Name += "/"
	}
	if err = output.tar.WriteHeader(header); err != nil {
		return nil, err
	}
	return output.tar, nil
}
func (output *managedArchiveOutput) Close() error {
	if output.zip != nil {
		return output.zip.Close()
	}
	err := output.tar.Close()
	if output.compressor != nil {
		if closeErr := output.compressor.Close(); err == nil {
			err = closeErr
		}
	}
	if output.command != nil {
		if waitErr := output.command.Wait(); err == nil {
			err = waitErr
		}
	}
	return err
}

type archiveContextReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader archiveContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.source.Read(data)
}

// TAR formats are converted to a bounded private ZIP for the existing
// transactional extractor. Validation/rollback and no-follow writes stay shared.
func openManagedArchiveReader(ctx context.Context, file *os.File, size int64, name, dataDir string) (*zip.Reader, func(), error) {
	noop := func() {}
	format := managedArchiveFormat(name)
	if format == "zip" || format == "" {
		reader, err := zip.NewReader(file, size)
		return reader, noop, err
	}
	var source io.Reader = file
	if format == "tar.gz" {
		reader, err := gzip.NewReader(file)
		if err != nil {
			return nil, noop, err
		}
		defer reader.Close()
		source = reader
	}
	if format == "tar.bz2" {
		source = bzip2.NewReader(file)
	}
	temporary, err := os.CreateTemp(dataDir, ".archive-convert-*.zip")
	if err != nil {
		return nil, noop, err
	}
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }
	writer := zip.NewWriter(&cappedArchiveWriter{destination: temporary, maximum: maxCreatedArchiveSize})
	fail := func(err error) (*zip.Reader, func(), error) { _ = writer.Close(); cleanup(); return nil, noop, err }
	reader := tar.NewReader(archiveContextReader{ctx: ctx, source: io.LimitReader(source, maxExtractedSize+int64(maxArchiveMembers)*2048+(2<<20))})
	var total int64
	members := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(fmt.Errorf("read TAR archive: %w", err))
		}
		members++
		if members > maxArchiveMembers {
			return fail(errors.New("archive contains too many entries"))
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return fail(errors.New("archive links and special files are not supported"))
		}
		clean := path.Clean(header.Name)
		if clean == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(header.Name, "\\\x00") {
			return fail(errors.New("archive contains an unsafe path"))
		}
		if header.Size < 0 || header.Size > maxExtractedSize-total {
			return fail(errors.New("archive expands beyond the 2 GiB safety limit"))
		}
		total += header.Size
		entry := &zip.FileHeader{Name: clean, Method: zip.Deflate}
		entry.SetMode(header.FileInfo().Mode())
		if header.Typeflag == tar.TypeDir {
			entry.Name += "/"
			entry.Method = zip.Store
		}
		destination, err := writer.CreateHeader(entry)
		if err != nil {
			return fail(err)
		}
		if header.Typeflag != tar.TypeDir {
			if _, err = io.CopyN(destination, reader, header.Size); err != nil {
				return fail(err)
			}
		}
	}
	if err = writer.Close(); err != nil {
		cleanup()
		return nil, noop, err
	}
	info, err := temporary.Stat()
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	converted, err := zip.NewReader(temporary, info.Size())
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	return converted, cleanup, nil
}
