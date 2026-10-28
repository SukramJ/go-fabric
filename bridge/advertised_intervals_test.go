// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"
)

// The operational record carries the node's session intervals
// (Config.SessionIntervals), as matter.js DeviceAdvertiser.ts:361-364
// advertises the session manager's, capped by
// SessionIntervals.forAdvertisement (SessionIntervals.ts:69-92) at
// 862ad75f; an ICD's provider replaces them.
func TestOperationalRecordCarriesSessionIntervals(t *testing.T) {
	newBridge := func(t *testing.T, iv SessionIntervals) (*Bridge, *deviceAdvertiserRecorder) {
		t.Helper()
		rec := &deviceAdvertiserRecorder{fakeAdvertiser: newFakeAdvertiser()}
		b, err := New(wbEmptySnapshotter, rec, Config{
			Listen: ":0", VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "Test Device", SessionIntervals: iv,
		}, nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return b, rec
	}
	want := func(t *testing.T, b *Bridge, rec *deviceAdvertiserRecorder, kv map[string]string) {
		t.Helper()
		svc := deviceAdvertiserFabric(t, b, rec)
		for k, v := range kv {
			got, ok := deviceAdvertiserTXT(svc, k)
			if v == "" {
				if ok {
					t.Errorf("%s = %q present, want absent", k, got)
				}
				continue
			}
			if got != v {
				t.Errorf("%s = %q (present %v), want %s", k, got, ok, v)
			}
		}
	}
	t.Run("zero is the defaults", func(t *testing.T) {
		b, rec := newBridge(t, SessionIntervals{})
		want(t, b, rec, map[string]string{"SII": "500", "SAI": "300", "SAT": "4000"})
	})
	t.Run("configured intervals", func(t *testing.T) {
		b, rec := newBridge(t, SessionIntervals{Idle: 5 * time.Second, Active: time.Second, ActiveThreshold: 2 * time.Second})
		want(t, b, rec, map[string]string{"SII": "5000", "SAI": "1000", "SAT": "2000"})
	})
	t.Run("capped at the advertisement's maximum", func(t *testing.T) {
		b, rec := newBridge(t, SessionIntervals{Idle: 2 * time.Hour, Active: time.Second})
		want(t, b, rec, map[string]string{"SII": "3600000", "SAI": "1000"})
	})
	t.Run("an ICD replaces the threshold and omits SII while LIT", func(t *testing.T) {
		b, rec := newBridge(t, SessionIntervals{Idle: 5 * time.Second, Active: time.Second, ActiveThreshold: 2 * time.Second})
		b.SetICDAdvertisement(func() ICDAdvertisement {
			return ICDAdvertisement{Mode: 1, ActiveThreshold: 300 * time.Millisecond, OmitIdleInterval: true}
		})
		want(t, b, rec, map[string]string{"SII": "", "SAI": "1000", "SAT": "300", "ICD": "1"})
	})
}
