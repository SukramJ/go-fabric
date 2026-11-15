// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "time"

// The package's tests wait at most this long for the answer to an ongoing
// report ([Bridge.awaitReportAnswer]): many send a report and never answer
// it, and the production bound (the chunk bound, 18 s and more) would hold
// each for that long. Long enough for a test that answers to do so first.
// Set before any test runs; read only afterwards.
func init() {
	reportAnswerBoundForTests = 5 * time.Second
}
