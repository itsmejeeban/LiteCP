package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var editorEncodings = []string{"UTF-8", "UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE", "ISO-8859-1", "ISO-8859-2", "ISO-8859-5", "ISO-8859-7", "ISO-8859-9", "ISO-8859-15", "WINDOWS-1250", "WINDOWS-1251", "WINDOWS-1252", "WINDOWS-1253", "WINDOWS-1254", "WINDOWS-1255", "WINDOWS-1256", "WINDOWS-1257", "WINDOWS-1258", "SHIFT_JIS", "EUC-JP", "EUC-KR", "GB18030", "GBK", "BIG5", "KOI8-R", "KOI8-U", "CP437", "CP850", "ASCII"}
var encodingCatalogOnce sync.Once
var encodingCatalog []string
var safeEncodingName = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._:-]{0,63}$`)

func availableEditorEncodings() []string {
	encodingCatalogOnce.Do(func() {
		encodingCatalog = append([]string{}, editorEncodings...)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "iconv", "-l").Output()
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, name := range encodingCatalog {
			seen[name] = true
		}
		extra := []string{}
		for _, value := range strings.Fields(string(output)) {
			name := strings.ToUpper(strings.TrimSuffix(value, "//"))
			if safeEncodingName.MatchString(name) && !seen[name] {
				seen[name] = true
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		encodingCatalog = append(encodingCatalog, extra...)
	})
	return append([]string{}, encodingCatalog...)
}
func normalizeEditorEncoding(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || value == "UTF8" {
		return "UTF-8", nil
	}
	for _, encoding := range availableEditorEncodings() {
		if value == encoding {
			return encoding, nil
		}
	}
	return "", errors.New("select a supported character encoding")
}

type encodedTextBuffer struct{ bytes.Buffer }

func (buffer *encodedTextBuffer) Write(value []byte) (int, error) {
	if buffer.Len()+len(value) > maxEditableFileSize {
		return 0, errors.New("converted text exceeds the editor size limit")
	}
	return buffer.Buffer.Write(value)
}
func convertEditorText(input []byte, from, to string) ([]byte, error) {
	if from == to {
		return input, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "iconv", "-f", from, "-t", to)
	command.Stdin = bytes.NewReader(input)
	var output encodedTextBuffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("cannot convert %s to %s; check the encoding and characters (iconv must be installed)", from, to)
	}
	return output.Bytes(), nil
}
func (s *System) ReadManagedText(site Site, relative, encoding string) (string, os.FileInfo, error) {
	encoding, err := normalizeEditorEncoding(encoding)
	if err != nil {
		return "", nil, err
	}
	file, err := secureOpenManaged(managedSiteRoot(site), relative, os.O_RDONLY, 0)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxEditableFileSize {
		return "", nil, fmt.Errorf("choose a text file up to %d MiB", maxEditableFileSize>>20)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxEditableFileSize+1))
	if err != nil {
		return "", nil, err
	}
	if len(content) > maxEditableFileSize {
		return "", nil, errors.New("file exceeds the editor size limit")
	}
	content, err = convertEditorText(content, encoding, "UTF-8")
	if err != nil {
		return "", nil, err
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return "", nil, errors.New("this is not text in the selected encoding; choose the correct encoding")
	}
	return string(content), info, nil
}
func (s *System) WriteManagedText(site Site, relative, content, encoding string) error {
	encoding, err := normalizeEditorEncoding(encoding)
	if err != nil {
		return err
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) || len(content) > maxEditableFileSize {
		return errors.New("invalid text or editor size limit exceeded")
	}
	converted, err := convertEditorText([]byte(content), "UTF-8", encoding)
	if err != nil {
		return err
	}
	return s.WriteManagedFile(site, relative, string(converted))
}
