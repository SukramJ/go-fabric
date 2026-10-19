// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "time"

// UpTimeFrom exposes upTimeFrom to the external parity tests: UpTime from
// an elapsed monotonic and an elapsed wall-clock time, which a
// clocktest.Fake cannot step apart (MockTime.stepWallClock moves the wall
// clock alone).
func (g *GeneralDiagnostics) UpTimeFrom(mono, wall time.Duration) time.Duration {
	return g.upTimeFrom(mono, wall)
}
