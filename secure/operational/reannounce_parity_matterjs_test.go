// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// A port of matter.js packages/protocol/test/protocol/DeviceAdvertiserTest.ts
// at 862ad75f: the session deleted handler, which decides whether a
// closed session resumes the advertisement.
//
// The Go surface differs: DeviceAdvertiser's sessions.deleted observer
// is the Manager's reannounce trigger (the bridge republishes the active
// records, the commissionable one among them), and a CASE session of a
// deleted fabric is one CloseFabric tears down on RemoveFabric.

import (
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sigma"
)

func TestDeviceAdvertiserSessionDeletedHandler(t *testing.T) {
	t.Run("resumes commissioning advertisement when a PASE session closes", func(t *testing.T) {
		m := NewManager(nil)
		var fired atomic.Int32
		m.SetReannounceTrigger(func() { fired.Add(1) })
		entry, err := m.OpenFromPase(0xB0B, 0, 0x2222, []byte("shared-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Close(entry.SessionID); err != nil {
			t.Fatal(err)
		}
		if n := fired.Load(); n != 1 {
			t.Fatalf("reannounce fired %d times, want 1", n)
		}
	})
	t.Run("does NOT resume commissioning advertisement when a CASE session for a deleted fabric closes", func(t *testing.T) {
		m := NewManager(nil)
		var fired atomic.Int32
		m.SetReannounceTrigger(func() { fired.Add(1) })
		var keys sigma.SessionKeys
		for i := range keys.I2RKey {
			keys.I2RKey[i] = byte(i)
			keys.R2IKey[i] = byte(i + 16)
		}
		if _, err := m.OpenFromSigma(1, 0xB0B, 1, keys); err != nil {
			t.Fatal(err)
		}
		m.CloseFabric(1)
		if n := fired.Load(); n != 0 {
			t.Fatalf("reannounce fired %d times on the fabric's teardown, want 0", n)
		}
	})
}
