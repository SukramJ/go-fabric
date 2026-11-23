// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	provdef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// otaScopeEnabled reports whether the test requestor may start a transfer
// of the fixture's image: a ReceiveInit is accepted. An accepted probe is
// closed again.
func (f *otaFixture) otaScopeEnabled(t *testing.T, sessionID uint16) bool {
	t.Helper()
	uri := bdx.FileDesignator(otaScope + "/" + string(f.src.img.FileDesignator)).BdxURI(otaTestOwnNode)
	fd, _, err := bdx.ParseBdxURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	req, resp := newOTAPipe()
	init, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: fd}.Encode()
	peer := bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode, SessionID: sessionID}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.bdx.HandleExchange(context.Background(), peer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: init}, resp)
	}()
	m := bdx.NewMessenger(req, 5*time.Second)
	_, _, err = m.Next(context.Background(), bdx.MessageReceiveAccept)
	if err == nil {
		_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, otaScope, true)
	}
	<-done
	return err == nil
}

// TestOTAProviderFabricDroppedMidQueryLeavesNoScope (R2-C2-conc-1202): a
// fabric removal while the query runs the host's status observer — after
// the attempt's last "ended?" check, before the BDX scope is enabled —
// answers NotAvailable and leaves no scope enabled for the removed
// fabric's (index, node), which a later fabric on the index could use.
func TestOTAProviderFabricDroppedMidQueryLeavesNoScope(t *testing.T) {
	t.Parallel()
	var f *otaFixture
	var once sync.Once
	f = newOTAFixture(t, func(cfg *OTAProviderConfig) {
		cfg.OnAttemptStatus = func(_ OTAPeer, status OTAUpdateStatus, _ uint32) {
			if status == OTAUpdateQuerying {
				once.Do(func() {
					if err := f.p.MatterDropFabric(context.Background(), otaTestFabric); err != nil {
						t.Error(err)
					}
				})
			}
		}
	})
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusNotAvailable || r.ImageUri != nil || r.UpdateToken != nil {
		t.Errorf("query whose fabric was removed mid-query = %+v, want NotAvailable", r)
	}
	if f.otaScopeEnabled(t, 7) {
		t.Error("the removed fabric's peer is still enabled for the ota scope")
	}
}

// TestOTAProviderSupersededQueryKeepsTheNewerScope (R2-C2-conc-1202): a
// second query of the same requestor that ends the first while the first
// runs its observer gets the offer; the first answers NotAvailable and
// does not withdraw the scope the second enabled.
func TestOTAProviderSupersededQueryKeepsTheNewerScope(t *testing.T) {
	t.Parallel()
	var f *otaFixture
	var fired atomic.Bool // not a sync.Once: the second query re-enters the observer
	var second provdef.QueryImageResponse
	f = newOTAFixture(t, func(cfg *OTAProviderConfig) {
		cfg.OnAttemptStatus = func(_ OTAPeer, status OTAUpdateStatus, _ uint32) {
			if status == OTAUpdateQuerying && fired.CompareAndSwap(false, true) {
				second = f.query(t, 8, bdxQuery())
			}
		}
	})
	first := f.query(t, 7, bdxQuery())
	if second.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("second query = %+v, want UpdateAvailable", second)
	}
	if first.Status != provdef.StatusNotAvailable {
		t.Errorf("superseded query = %+v, want NotAvailable", first)
	}
	if !f.otaScopeEnabled(t, 8) {
		t.Error("the superseded query withdrew the scope of the second")
	}
}
