// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// ControllerOptions.Sessions reaches the controller's handshake: the
// device records every field the controller reported.
func TestControllerOptionsSessionsAreReported(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	want := &controller.SessionIntervals{Idle: 2345 * time.Millisecond, Active: 234 * time.Millisecond, ActiveThreshold: 5 * time.Second}
	p := site.CommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{Sessions: want}})
	var seen bool
	for _, s := range p.Device.Node().SessionDiagnostics().Sessions {
		if s.PASE {
			continue
		}
		seen = true
		if s.PeerIdleInterval != want.Idle || s.PeerActiveInterval != want.Active || s.PeerActiveThreshold != want.ActiveThreshold {
			t.Errorf("device recorded %v/%v/%v, want %v/%v/%v", s.PeerIdleInterval, s.PeerActiveInterval, s.PeerActiveThreshold, want.Idle, want.Active, want.ActiveThreshold)
		}
	}
	if !seen {
		t.Fatal("no CASE session on the device")
	}
}
