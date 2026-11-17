// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package twotypes is a fixture: one type quiesces, the second, which
// arms its own timer, does not.
package twotypes

import "time"

// First quiesces.
type First struct{ t *time.Timer }

// Arm arms.
func (f *First) Arm() { f.t = time.AfterFunc(time.Second, func() {}) }

// MatterQuiesce stops.
func (f *First) MatterQuiesce() { f.t.Stop() }

// Second does not.
type Second struct{ t *time.Timer }

// Arm arms.
func (s *Second) Arm() { s.t = time.AfterFunc(time.Second, func() {}) }
