// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/bdx"
)

// TestParityMatterJS_OTAProviderTransferKeepsEntryAlive ports OtaTest.ts:
// 2550 ("A running BDX transfer keeps its in-progress entry alive,
// including a session opened for a retry", packages/node/test/behaviors/ota/OtaTest.ts
// at b25ed960) on the provider: the blocks of a running BDX transfer touch
// the update's attempt (progressInfo → attempt.touch(),
// OtaSoftwareUpdateProviderServer.ts:281), and a fresh session that
// retries the download within the same attempt still finds the provider
// listening (:277-283): the attempt moves to Downloading and its blocks
// touch it too. A test of the provider alone; no seam.
func TestParityMatterJS_OTAProviderTransferKeepsEntryAlive(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		f := newOTAFixture(t, nil)
		r := f.query(t, 7, bdxQuery())
		attempt := f.p.mgr.AttemptFor(otaTestPeer, *r.UpdateToken)
		if attempt == nil {
			t.Fatal("no attempt for the offer")
		}
		age := func() time.Time {
			attempt.mu.Lock()
			defer attempt.mu.Unlock()
			aged := f.clock.Now().Add(-20 * time.Minute)
			attempt.lastActivity = aged
			return aged
		}
		// refreshed waits for the attempt's activity to move past aged; the
		// progress listener runs on the session's goroutine.
		refreshed := func(what string, aged time.Time) {
			t.Helper()
			synctest.Wait() // the session's goroutine has run the progress listener
			if !attempt.LastActivity().After(aged) {
				t.Fatalf("%s: the attempt kept its aged activity", what)
			}
		}
		block := func(m *bdx.Messenger, counter uint32) bdx.MessageType {
			t.Helper()
			if err := m.SendBlockQuery(ctx, counter); err != nil {
				t.Fatal(err)
			}
			typ, _, err := m.ReadBlock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return typ
		}

		m, done := f.startDownload(t, 7, *r.ImageUri)
		aged := age()
		block(m, 0)
		refreshed("the first transfer", aged)
		counter := uint32(1)
		for block(m, counter) != bdx.MessageBlockEOF {
			counter++
		}
		if err := m.SendBlockAckEOF(ctx, counter); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the first transfer did not end")
		}
		if s := attempt.Status(); s != OTAUpdateWaitForApply {
			t.Fatalf("state %v after the transfer, want WaitForApply", s)
		}

		// The retry on a fresh session of the same query cycle.
		retry, retryDone := f.startDownload(t, 7, *r.ImageUri)
		if s := attempt.Status(); s != OTAUpdateDownloading {
			t.Fatalf("state %v of the retry, want Downloading", s)
		}
		aged = age()
		block(retry, 0)
		refreshed("the retry", aged)
		_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, "ota", true)
		<-retryDone
	})
}
