// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package webrtctransportrequestor

import "testing"

// KeyOf carries the three fields that identify a session across fabrics
// (brief CK-2: the key moved here from cluster/camera).
func TestKeyOfCarriesIDFabricAndPeer(t *testing.T) {
	t.Parallel()
	got := KeyOf(WebRTCSessionStruct{Id: 7, FabricIndex: 2, PeerNodeId: 0x1122})
	want := SessionKey{ID: 7, FabricIndex: 2, PeerNodeID: 0x1122}
	if got != want {
		t.Fatalf("KeyOf = %+v, want %+v", got, want)
	}
}
