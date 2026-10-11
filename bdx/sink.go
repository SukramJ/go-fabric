// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Sink receives the bytes of an inbound transfer: written in order, then
// closed — with nil when the transfer completed, which commits what was
// written, or with the transfer's error, which discards it.
//
// It is the Go form of matter.js's inbound write path
// (flow/InboundFlow.ts:24-56): PersistedFileDesignator.writeFromStream
// consumes a stream the flow enqueues each block into, closes it on BlockEof
// and errors it when the transfer fails.
type Sink interface {
	io.Writer
	// Close ends the write; err nil commits, non-nil discards.
	Close(err error) error
}

// FileSink is a [Sink] writing a file at Path through a temporary file in
// the same directory, renamed into place on a successful Close. A failed
// transfer leaves Path as it was. matter.js's blob drivers replace the
// named blob as a whole (PersistedFileDesignator.ts:72-81); the rename is
// how a file system does the same.
type FileSink struct {
	Path string

	mu      sync.Mutex
	tmp     *os.File
	written int64
	closed  bool
}

// NewFileSink returns a sink for path. The temporary file is created on the
// first write, so a transfer that never starts leaves nothing behind.
func NewFileSink(path string) *FileSink { return &FileSink{Path: path} }

// Write implements [io.Writer].
func (s *FileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("bdx: write to a closed file sink")
	}
	if s.tmp == nil {
		f, err := os.CreateTemp(filepath.Dir(s.Path), filepath.Base(s.Path)+".part-*")
		if err != nil {
			return 0, fmt.Errorf("bdx: file sink: %w", err)
		}
		s.tmp = f
	}
	n, err := s.tmp.Write(p)
	s.written += int64(n)
	return n, err
}

// Written is the number of bytes written so far.
func (s *FileSink) Written() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// Close implements [Sink].
func (s *FileSink) Close(transferErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.tmp == nil {
		if transferErr != nil {
			return nil
		}
		// An empty transfer still names a file.
		f, err := os.CreateTemp(filepath.Dir(s.Path), filepath.Base(s.Path)+".part-*")
		if err != nil {
			return fmt.Errorf("bdx: file sink: %w", err)
		}
		s.tmp = f
	}
	name := s.tmp.Name()
	if transferErr != nil {
		_ = s.tmp.Close()
		_ = os.Remove(name)
		return nil
	}
	if err := s.tmp.Sync(); err != nil {
		_ = s.tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("bdx: file sink: %w", err)
	}
	if err := s.tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("bdx: file sink: %w", err)
	}
	if err := os.Rename(name, s.Path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("bdx: file sink: %w", err)
	}
	return nil
}
