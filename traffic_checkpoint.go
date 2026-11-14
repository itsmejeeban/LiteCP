package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Bound the cache by encoded bytes and entry count. Checkpoints contain both
// counters and offsets, so unflushed requests replay exactly once after restart.
const trafficCacheBytes = 8 << 20
const trafficCacheEntries = 32
const trafficFlushInterval = 30 * time.Second

type trafficCheckpoint struct {
	state   *trafficState
	bytes   int64
	dirty   bool
	used    time.Time
	flushed time.Time
	stamp   os.FileInfo
}

var errTrafficCheckpointChanged = errors.New("analytics checkpoint changed outside the collector; restart collection to reconcile")

func trafficCheckpointStamp(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return info, err
}

func sameTrafficCheckpoint(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (s *trafficService) flushTrafficCheckpoint(path string, c *trafficCheckpoint) error {
	if !c.dirty {
		return nil
	}
	stamp, err := trafficCheckpointStamp(path)
	if err != nil {
		return err
	}
	if !sameTrafficCheckpoint(stamp, c.stamp) {
		return errTrafficCheckpointChanged
	}
	data, err := json.Marshal(c.state)
	if err != nil {
		return err
	}
	if len(data) > trafficMaxState {
		return errors.New("analytics history exceeded 16 MiB; existing checkpoint retained")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err = writeAtomic(path, data, 0600); err != nil {
		return err
	}
	c.bytes = int64(len(data))
	c.dirty = false
	c.flushed = time.Now()
	c.stamp, err = trafficCheckpointStamp(path)
	if err != nil {
		delete(s.states, path)
		return err
	}
	if c.bytes > trafficCacheBytes {
		delete(s.states, path)
	}
	return nil
}

// Caller holds trafficService.mu.
func (s *trafficService) trafficCheckpoint(path string) (*trafficCheckpoint, error) {
	if s.states == nil {
		s.states = map[string]*trafficCheckpoint{}
	}
	stamp, err := trafficCheckpointStamp(path)
	if err != nil {
		return nil, err
	}
	if c := s.states[path]; c != nil {
		if sameTrafficCheckpoint(stamp, c.stamp) {
			c.used = time.Now()
			return c, nil
		}
		if c.dirty {
			return nil, errTrafficCheckpointChanged
		}
		delete(s.states, path)
	}
	var retained int64
	for _, c := range s.states {
		retained += c.bytes
	}
	incoming := int64(0)
	if stamp != nil {
		incoming = stamp.Size()
	}
	if incoming <= trafficCacheBytes {
		for len(s.states) > 0 && (len(s.states) >= trafficCacheEntries || retained+incoming > trafficCacheBytes) {
			removed, err := s.evictTrafficCheckpoint()
			if err != nil {
				return nil, err
			}
			retained -= removed
		}
	}
	state, err := readTrafficState(path)
	if err != nil {
		return nil, err
	}
	after, err := trafficCheckpointStamp(path)
	if err != nil {
		return nil, err
	}
	if !sameTrafficCheckpoint(stamp, after) {
		return nil, errTrafficCheckpointChanged
	}
	now := time.Now()
	c := &trafficCheckpoint{state: state, bytes: incoming, used: now, flushed: now, stamp: stamp}
	if incoming > trafficCacheBytes {
		return c, nil // Oversized histories remain valid but bypass the cache.
	}
	s.states[path] = c
	return c, nil
}

func (s *trafficService) evictTrafficCheckpoint() (int64, error) {
	paths := make([]string, 0, len(s.states))
	for path := range s.states {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return s.states[paths[i]].used.Before(s.states[paths[j]].used)
	})
	var lastErr error
	for _, path := range paths {
		c := s.states[path]
		bytes := c.bytes
		if err := s.flushTrafficCheckpoint(path, c); err != nil {
			lastErr = err
			continue
		}
		delete(s.states, path)
		return bytes, nil
	}
	return 0, lastErr
}

func (s *trafficService) trimTrafficCheckpoints() error {
	var retained int64
	for _, c := range s.states {
		retained += c.bytes
	}
	for retained > trafficCacheBytes {
		removed, err := s.evictTrafficCheckpoint()
		if err != nil {
			return err
		}
		retained -= removed
	}
	return nil
}
