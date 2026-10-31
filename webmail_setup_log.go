package main

import (
	"io"
	"os"
	"strings"
)

// Return only a bounded tail from the server-owned setup log, never an arbitrary client path.
func webmailSetupLog(filename string) []string {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return []string{}
	}
	f, err := os.Open(filename)
	if err != nil {
		return []string{}
	}
	defer f.Close()
	offset := info.Size() - 32768
	if offset < 0 {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return []string{}
	}
	content, err := io.ReadAll(io.LimitReader(f, 32768))
	if err != nil {
		return []string{}
	}
	text := strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, string(content))
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:]
	}
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "password=") || strings.Contains(lower, "secret=") || strings.Contains(lower, "token=") {
			lines[i] = "[Sensitive setup output omitted]"
			continue
		}
		if len(line) > 1000 {
			lines[i] = line[:1000] + "…"
		}
	}
	return lines
}
