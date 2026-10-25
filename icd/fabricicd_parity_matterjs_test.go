// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/FabricIcdTest.ts at
// 862ad75f. matter.js's FabricIcd carries both roles of a fabric's ICD
// state; Go splits them by role: the device's registrations are
// icd.Registrations (TestFabricIcdDeviceRole), the controller's peers are
// icd.PeerTable (TestFabricIcdControllerRole).
package icd_test

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/icd"
)

var (
	fabricKeyA, _ = hex.DecodeString("d90e13180d00baadd20cf5ed4913d3ff")
	fabricKeyB, _ = hex.DecodeString("18fdbceaef01955b0ec875eda3ae6ee8")
)

func TestFabricIcdDeviceRole(t *testing.T) {
	t.Parallel()
	t.Run("device role — registrations", func(t *testing.T) {
		t.Parallel()
		t.Run("stores, lists and removes registrations", func(t *testing.T) {
			t.Parallel()
			var r icd.Registrations
			r.Set(icd.Registration{CheckInNodeID: 1, MonitoredSubject: 1, Key: fabricKeyA, ClientType: icd.ClientPermanent})
			if n := len(r.All()); n != 1 {
				t.Fatalf("%d registrations", n)
			}
			r.Delete(1)
			if n := len(r.All()); n != 0 {
				t.Fatalf("%d registrations", n)
			}
		})
		t.Run("replaces registration for the same checkInNodeId", func(t *testing.T) {
			t.Parallel()
			var r icd.Registrations
			r.Set(icd.Registration{CheckInNodeID: 1, MonitoredSubject: 1, Key: fabricKeyA, ClientType: icd.ClientPermanent})
			r.Set(icd.Registration{CheckInNodeID: 1, MonitoredSubject: 7, Key: fabricKeyB, ClientType: icd.ClientEphemeral})
			all := r.All()
			if len(all) != 1 || all[0].MonitoredSubject != 7 {
				t.Fatalf("%+v", all)
			}
		})
	})
}
