// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"slices"
	"testing"
)

// TestCompositeComponents: a composite is a device type whose requirements
// are device types only (VideoDoorbell, FloodlightCamera); a type with
// cluster requirements of its own and components is none (Oven,
// SmokeCoAlarm), and an uncommitted component fails.
func TestCompositeComponents(t *testing.T) {
	t.Parallel()
	g := testGenerator(t)
	all := map[string]bool{}
	for _, c := range committed {
		all[c] = true
	}
	for name, want := range map[string][]string{
		"VideoDoorbell":    {"Camera", "Doorbell"},
		"FloodlightCamera": {"OnOffLight", "Camera"},
		"Oven":             nil,
		"SmokeCoAlarm":     nil,
		"DimmableLight":    nil,
	} {
		got, err := compositeComponents(g.snap.deviceType(name), all)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %v %v, want %v", name, got, err, want)
		}
	}
	delete(all, "Doorbell")
	if _, err := compositeComponents(g.snap.deviceType("VideoDoorbell"), all); err == nil {
		t.Error("an uncommitted component was accepted")
	}
}
