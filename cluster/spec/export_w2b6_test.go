// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

// ServerLockedW2B6 reports whether s's lock is held by anyone at the
// moment of the call.
func ServerLockedW2B6(s *Server) bool {
	if s.mu.TryLock() {
		s.mu.Unlock()
		return false
	}
	return true
}

// ValueLockedW2B6 is [Server.Value] for a caller that holds s's lock — a
// stored step of internal/specseam.SetAttributesStored.
func ValueLockedW2B6(s *Server, attrID uint32) (any, bool) {
	v, ok := s.values[attrID]
	return v, ok
}
