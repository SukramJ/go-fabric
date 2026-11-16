// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package helper is a fixture: a throttle that arms a timer for its holder.
package helper

import "time"

// Throttle arms a timer.
type Throttle struct{ t *time.Timer }

// Arm arms it.
func (h *Throttle) Arm() { h.t = time.AfterFunc(time.Second, func() {}) }

// Stop stops it.
func (h *Throttle) Stop() { h.t.Stop() }
