// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
)

// TestRouteInvoke pins the network a command goes out on: "unlimited" when
// the caller named none — matter.js ClientNodeInteraction.invoke
// (ClientNodeInteraction.ts:213-216 at 862ad75f), which never applies the
// await-mode ICD routing of a read or write to an invoke — and the
// caller's own otherwise.
func TestRouteInvoke(t *testing.T) {
	t.Parallel()
	var r interaction.ClientRequest
	routeInvoke(&r)
	if r.Network != peer.NetworkUnlimited {
		t.Fatalf("an invoke without a network goes out on %q, want %q", r.Network, peer.NetworkUnlimited)
	}
	named := interaction.ClientRequest{Network: peer.NetworkICDLit}
	routeInvoke(&named)
	if named.Network != peer.NetworkICDLit {
		t.Fatalf("a named network became %q, want %q", named.Network, peer.NetworkICDLit)
	}
}
