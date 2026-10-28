// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package forward holds the switches of Matter behaviour implemented
// ahead of a released specification revision — matter.js
// Specification.ForwardFeature and isForwardFeatureEnabled
// (packages/model/src/common/Specification.ts:78-105 at 862ad75f): off in
// every release, with no supported configuration or API to change them;
// matter.js's tests enable one feature through the MatterHooks harness
// hook, this module's through [Enable], which only its own tests reach
// (the package is internal).
//
// A guarded feature that ships in a released revision loses its guards
// and its name here.
package forward

import "sync"

// Feature names a forward feature (Specification.ForwardFeature, :93).
type Feature string

// The forward features this module guards. matter.js's other one,
// "delay-report-data", is acted on always (BD-Matter-DelayReportData).
const (
	// PQCPhase1 admits ML-DSA-44 and ML-DSA-65 in attestation
	// certificates (controller/attestation).
	PQCPhase1 Feature = "pqc-phase-1"
)

var (
	mu      sync.Mutex
	enabled = map[Feature]int{}
)

// Enabled reports whether f is on.
func Enabled(f Feature) bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled[f] > 0
}

// Enable switches f on until the returned function is called — the test
// harness hook (MockForwardFeatures.enable). The switch is process-wide:
// a test that enables a feature runs apart from the tests that need it
// off (not in parallel with them).
func Enable(f Feature) (restore func()) {
	mu.Lock()
	enabled[f]++
	mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			enabled[f]--
			mu.Unlock()
		})
	}
}
