// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff

// TimersRunning reports whether the timed-on and the delayed-off
// countdowns run: the Go form of matter.js's MockTime.timerCountFor("Timed
// on") and timerCountFor("Delayed off") (OnOffServerTest.ts), which count
// the server's two named periodic timers.
func TimersRunning(s *Server) (timedOn, delayedOff bool) {
	s.op.Lock()
	defer s.op.Unlock()
	return s.timedOn.running, s.delayedOff.running
}
