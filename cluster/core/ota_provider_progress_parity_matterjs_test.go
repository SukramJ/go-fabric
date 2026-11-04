// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// TestParityMatterJS_OTAProviderTransferKeepsEntryAlive ports OtaTest.ts:
// 2100-2183 (packages/node/test/behaviors/ota/OtaTest.ts at 862ad75f) on
// the provider: the blocks of a running BDX transfer refresh the update's
// in-progress entry (internal.inProgressDetails, the Go inProgress map;
// progressInfo, OtaSoftwareUpdateProviderServer.ts:329-331), and a fresh
// session that retries the download within the same query cycle still
// finds the provider listening (:314-320): the entry moves to Downloading
// and its blocks refresh it too. A test of the provider alone; no seam.
func TestParityMatterJS_OTAProviderTransferKeepsEntryAlive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	key := otaKey(otaTestPeer, *r.UpdateToken)
	entry := func() otaInProgress {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		d := f.p.inProgress[key]
		if d == nil {
			t.Fatal("no in-progress entry")
		}
		return *d
	}
	age := func() time.Time {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		aged := f.clock.Now().Add(-20 * time.Minute)
		f.p.inProgress[key].timestamp = aged
		return aged
	}
	// refreshed waits for the entry's timestamp to move past aged; the
	// progress listener runs on the session's goroutine.
	refreshed := func(what string, aged time.Time) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !entry().timestamp.After(aged) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: the entry kept its aged timestamp", what)
			}
			time.Sleep(time.Millisecond)
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
	if s := entry().lastState; s != OTAUpdateWaitForApply {
		t.Fatalf("state %v after the transfer, want WaitForApply", s)
	}

	// The retry on a fresh session of the same query cycle.
	retry, retryDone := f.startDownload(t, 7, *r.ImageUri)
	if s := entry().lastState; s != OTAUpdateDownloading {
		t.Fatalf("state %v of the retry, want Downloading", s)
	}
	aged = age()
	block(retry, 0)
	refreshed("the retry", aged)
	_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, "ota", true)
	<-retryDone
}
