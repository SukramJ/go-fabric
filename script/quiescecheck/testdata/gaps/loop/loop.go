// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package loop is a fixture: a server whose goroutine waits on time.After.
package loop

import "time"

// Server polls.
type Server struct{ done chan struct{} }

// Start runs the loop.
func (s *Server) Start() {
	go func() {
		for {
			select {
			case <-s.done:
				return
			case <-time.After(time.Second):
			}
		}
	}()
}
