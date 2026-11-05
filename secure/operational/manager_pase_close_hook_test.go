// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestSetOnPaseSessionClose pins the hook the bridge takes for the
// commissioning re-advertisement: it fires once per closed session that
// was established via PASE — after the SetOnSessionClose hook, whatever
// closed it, and still after AddNOC adopted the session onto a fabric —
// and never for a CASE session. matter.js's DeviceAdvertiser keys the
// same decision on `session.isPase` in its `sessions.deleted` observer
// (packages/protocol/src/protocol/DeviceAdvertiser.ts:131-162 at
// 862ad75f).
func TestSetOnPaseSessionClose(t *testing.T) {
	t.Parallel()
	secret := make([]byte, 16)
	for i := range secret {
		secret[i] = byte(i + 3)
	}
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i] = byte(i + 1)
		keys.R2IKey[i] = byte(i + 40)
	}
	type calls struct {
		mu    sync.Mutex
		order []string
		pase  []uint16
	}
	wire := func(t *testing.T) (m *operational.Manager, c *calls) {
		t.Helper()
		m, _ = newTestManager()
		c = &calls{}
		m.SetOnSessionClose(func(uint16) {
			c.mu.Lock()
			c.order = append(c.order, "close")
			c.mu.Unlock()
		})
		m.SetOnPaseSessionClose(func(id uint16) {
			c.mu.Lock()
			c.order = append(c.order, "pase")
			c.pase = append(c.pase, id)
			c.mu.Unlock()
		})
		return m, c
	}

	t.Run("a closed PASE session fires it after the close hook", func(t *testing.T) {
		t.Parallel()
		m, c := wire(t)
		pase, err := m.OpenFromPase(1, 2, 0, secret)
		if err != nil {
			t.Fatalf("OpenFromPase: %v", err)
		}
		if err := m.Close(pase.SessionID); err != nil {
			t.Fatalf("Close: %v", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.pase) != 1 || c.pase[0] != pase.SessionID {
			t.Fatalf("PASE hook ids = %v, want [%d]", c.pase, pase.SessionID)
		}
		if len(c.order) != 2 || c.order[0] != "close" || c.order[1] != "pase" {
			t.Fatalf("hook order = %v, want [close pase]", c.order)
		}
	})

	t.Run("a session adopted onto a fabric by AddNOC still fires it when the fabric closes", func(t *testing.T) {
		t.Parallel()
		m, c := wire(t)
		pase, err := m.OpenFromPase(1, 2, 0, secret)
		if err != nil {
			t.Fatalf("OpenFromPase: %v", err)
		}
		if err := m.AdoptFabricIndex(pase.SessionID, 2); err != nil {
			t.Fatalf("AdoptFabricIndex: %v", err)
		}
		m.CloseFabric(2)
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.pase) != 1 || c.pase[0] != pase.SessionID {
			t.Fatalf("PASE hook ids after CloseFabric = %v, want [%d]", c.pase, pase.SessionID)
		}
	})

	t.Run("a CASE session never fires it", func(t *testing.T) {
		t.Parallel()
		m, c := wire(t)
		cs, err := m.OpenFromSigma(2, 100, 200, keys)
		if err != nil {
			t.Fatalf("OpenFromSigma: %v", err)
		}
		if err := m.Close(cs.SessionID); err != nil {
			t.Fatalf("Close: %v", err)
		}
		m.CloseFabric(2)
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.pase) != 0 {
			t.Fatalf("PASE hook ids for a CASE session = %v, want none", c.pase)
		}
		if len(c.order) != 1 || c.order[0] != "close" {
			t.Fatalf("hook order = %v, want [close]", c.order)
		}
	})

	t.Run("nil clears it", func(t *testing.T) {
		t.Parallel()
		m, c := wire(t)
		m.SetOnPaseSessionClose(nil)
		if m.ClosePASESessions() != 0 {
			t.Fatal("sessions closed before any opened")
		}
		pase, err := m.OpenFromPase(1, 2, 0, secret)
		if err != nil {
			t.Fatalf("OpenFromPase: %v", err)
		}
		if n := m.ClosePASESessions(); n != 1 {
			t.Fatalf("ClosePASESessions = %d, want 1 (%d)", n, pase.SessionID)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.pase) != 0 {
			t.Fatalf("PASE hook ids after clearing = %v, want none", c.pase)
		}
	})
}
