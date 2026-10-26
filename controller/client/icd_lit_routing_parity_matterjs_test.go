// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The icdLit routing case of matter.js
// packages/node/test/node/client/ClientNodeInteractionIcdTest.ts:202-217 at
// 862ad75f: a read of an await-mode LIT peer, held until the peer wakes,
// goes out through the unthrottled "icdLit" network profile
// (ClientNodeInteraction.ts:78-81, :300-318). matter.js records the ids
// NetworkProfiles.select is called with; Go counts the grants of the
// icdLit profile's semaphore.
package client_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/peer"
)

func TestClientNodeInteractionIcdLitRouting(t *testing.T) {
	t.Parallel()
	t.Run("routes a woken LIT read through the icdLit network profile", func(t *testing.T) {
		t.Parallel()
		p := registeredLITPair(t)
		if p.wakefulness(t).Awake() {
			t.Fatal("awake")
		}
		lit, err := p.pair.Controller.Peers().Networks().Get(peer.NetworkICDLit)
		if err != nil {
			t.Fatal(err)
		}
		before := lit.Semaphore.Granted()
		read := p.readAsync(parityCtx(t), descriptorRead)
		p.wakeDevice(t)
		var rerr error
		p.eventually(t, "the held read", func() bool {
			select {
			case rerr = <-read:
				return true
			default:
				return false
			}
		})
		if rerr != nil {
			t.Fatal(rerr)
		}
		if lit.Semaphore.Granted() == before {
			t.Error("the read did not go through the icdLit profile")
		}
	})
	t.Run("keeps a non-LIT peer on its medium profile", func(t *testing.T) {
		t.Parallel()
		p := newICDPair(t, icdPairOptions{})
		lit, _ := p.pair.Controller.Peers().Networks().Get(peer.NetworkICDLit)
		before := lit.Semaphore.Granted()
		if _, err := p.node.Read(parityCtx(t), descriptorRead); err != nil {
			t.Fatal(err)
		}
		if lit.Semaphore.Granted() != before {
			t.Error("a non-LIT read went through the icdLit profile")
		}
	})
}
