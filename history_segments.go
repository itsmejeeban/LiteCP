package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const historySegmentBytes = 8 << 20
const historyRecordBytes = 64 << 10

func historySegmentName(day time.Time, part int) string {
	return fmt.Sprintf("%s-%04d.jsonl", day.UTC().Format("2006-01-02"), part)
}

// New records use bounded daily segments. Legacy files remain readable and are
// never rewritten. Older segments remain available for explicit archive/export.
func appendHistoryRecord(dir, kind string, at time.Time, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return appendHistoryLines(dir, kind, at, append(data, '\n'))
}

func appendHistoryLines(dir, kind string, at time.Time, lines []byte) error {
	if len(lines) > historySegmentBytes {
		return fmt.Errorf("history write exceeds %d bytes", historySegmentBytes)
	}
	root := filepath.Join(dir, "history", kind)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	for part := 0; part < 10000; part++ {
		path := filepath.Join(root, historySegmentName(at, part))
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if info != nil && info.Size()+int64(len(lines)) > historySegmentBytes {
			continue
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(lines)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	return fmt.Errorf("history daily segment capacity exceeded")
}

func scanHistoryFile(path string, maxBytes int64, visit func([]byte)) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	partial := false
	if maxBytes > 0 && size > maxBytes {
		// Inspect the preceding byte so a complete record at the exact byte
		// boundary is included.
		var previous [1]byte
		if _, err := f.ReadAt(previous[:], size-maxBytes-1); err != nil {
			return err
		}
		partial = previous[0] != '\n'
		if _, err := f.Seek(size-maxBytes, io.SeekStart); err != nil {
			return err
		}
		size = maxBytes
	}
	scanner := bufio.NewScanner(io.LimitReader(f, size))
	scanner.Buffer(make([]byte, 4096), historyRecordBytes)
	if partial {
		scanner.Scan() // The initial line may start in the middle of JSON.
	}
	for scanner.Scan() {
		visit(scanner.Bytes())
	}
	return scanner.Err()
}

func historySegmentPaths(root string, since time.Time) ([]string, error) {
	if since.IsZero() {
		// The audit tail has no age limit, so include archives across all days.
		paths, err := filepath.Glob(filepath.Join(root, "*.jsonl"))
		sort.Sort(sort.Reverse(sort.StringSlice(paths)))
		return paths, err
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	paths := []string{}
	cutoff := since.UTC().Truncate(24 * time.Hour)
	for day := time.Now().UTC().Truncate(24 * time.Hour); !day.Before(cutoff); day = day.AddDate(0, 0, -1) {
		parts := []string{}
		for part := 0; part < 10000; part++ {
			path := filepath.Join(root, historySegmentName(day, part))
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					break
				}
				return nil, err
			}
			parts = append(parts, path)
		}
		for i := len(parts) - 1; i >= 0; i-- {
			paths = append(paths, parts[i])
		}
	}
	return paths, nil
}

func scanHistoryRecords(dir, kind, legacy string, since time.Time, byteBudget int64, visit func([]byte)) error {
	paths, err := historySegmentPaths(filepath.Join(dir, "history", kind), since)
	if err != nil {
		return err
	}
	type segment struct {
		path  string
		limit int64
	}
	selected := []segment{}
	bounded := byteBudget > 0
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		limit := info.Size()
		if bounded && limit > byteBudget {
			limit = byteBudget
		}
		if limit == 0 {
			continue
		}
		selected = append(selected, segment{path, limit})
		if bounded {
			byteBudget -= limit
		}
		if bounded && byteBudget == 0 {
			break
		}
	}
	// Legacy files precede all new segments and use only the remaining
	// allowance when the caller requests a bounded tail.
	if !bounded || byteBudget > 0 {
		if err := scanHistoryFile(filepath.Join(dir, legacy), byteBudget, visit); err != nil {
			return err
		}
	}
	for i := len(selected) - 1; i >= 0; i-- {
		if err := scanHistoryFile(selected[i].path, selected[i].limit, visit); err != nil {
			return err
		}
	}
	return nil
}
