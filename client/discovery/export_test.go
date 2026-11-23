// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// The race's bounds, for the race tests of package discovery_test.
const (
	RaceStaggerCeiling        = raceStaggerCeiling
	MaxRaceAttempts           = maxRaceAttempts
	MaxRaceWaiting            = maxRaceWaiting
	MaxRaceUnwinding          = maxRaceUnwinding
	MaxConcurrentPreparations = maxConcurrentPreparations
)

// RaceWaiting is how many candidates of the race s wait, found and not
// yet released.
func RaceWaiting[W any](s *Session[W]) int {
	k, ok := s.kind.(*raceKind[W])
	if !ok {
		return -1
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.waiting)
}
