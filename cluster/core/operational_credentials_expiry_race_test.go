// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// stallingStore holds the first write of the armed kind until release is
// closed, after signalling entered: a NOC command caught in its store
// write while the fail-safe expires.
type stallingStore struct {
	restartStore
	mu       sync.Mutex
	armed    string
	entered  chan struct{}
	release  chan struct{}
	disarmed bool
}

func (s *stallingStore) stall(kind string) {
	s.mu.Lock()
	hit := !s.disarmed && s.armed == kind
	if hit {
		s.disarmed = true
	}
	s.mu.Unlock()
	if hit {
		close(s.entered)
		<-s.release
	}
}

func (s *stallingStore) AddFabric(ctx context.Context, rec mstore.FabricRecord) (uint8, error) {
	s.stall("AddFabric")
	return s.restartStore.AddFabric(ctx, rec)
}

func (s *stallingStore) UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, c mstore.FabricCredentials) error {
	s.stall("UpdateFabricCredentials")
	return s.restartStore.UpdateFabricCredentials(ctx, fabricIndex, c)
}

// expireDuringStall runs cmd, which stalls in the store, fires the
// fail-safe expiry while it does, and returns once both are done. The
// command is released only once the expiry has either finished — it must
// not, because the NOC command has not recorded what it writes yet — or
// reported that it waits for the command (OnRollbackWaitsForNOC): no
// wall-clock wait decides the order (review II, R2-P1-dr-sec-05).
func expireDuringStall(t *testing.T, st *stallingStore, oc *core.OperationalCredentials, cmd func()) (expiryWaited bool) {
	t.Helper()
	waits := make(chan struct{})
	var once sync.Once
	oc.OnRollbackWaitsForNOC(func() { once.Do(func() { close(waits) }) })
	cmdDone := make(chan struct{})
	go func() { defer close(cmdDone); cmd() }()
	<-st.entered
	expiryDone := make(chan struct{})
	go func() { defer close(expiryDone); oc.OnFailSafeExpiry(context.Background(), 0) }()
	select {
	case <-expiryDone:
	case <-waits:
		expiryWaited = true
	}
	close(st.release)
	<-cmdDone
	<-expiryDone
	return expiryWaited
}

// TestOpcreds_ExpiryDuringAddNOC_RevertsTheFabric pins R2-P1-core-03: a
// fail-safe that expires while AddNOC is between its fail-safe check and
// recording the fabric as pending still rolls the fabric back — the
// expiry waits for the command instead of finding nothing to revert.
func TestOpcreds_ExpiryDuringAddNOC_RevertsTheFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &stallingStore{restartStore: memstore.New(), armed: "AddFabric", entered: make(chan struct{}), release: make(chan struct{})}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	req := prepareAddNOC(ctx, t, oc)
	waited := expireDuringStall(t, st, oc, func() { _, _ = oc.MatterInvoke(ctx, 0x06, req) })
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
		t.Fatalf("the fabric AddNOC installed while the fail-safe expired survived the expiry (expiry waited for AddNOC: %v)", waited)
	}
}

// TestOpcreds_ExpiryDuringUpdateNOC_RestoresTheIdentity is the UpdateNOC
// half of R2-P1-core-03.
func TestOpcreds_ExpiryDuringUpdateNOC_RestoresTheIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &stallingStore{restartStore: memstore.New(), entered: make(chan struct{}), release: make(chan struct{})}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc) // CommissioningComplete
	before, _ := st.GetIdentity(ctx, idx)
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
	st.mu.Lock()
	st.armed = "UpdateFabricCredentials"
	st.mu.Unlock()
	waited := expireDuringStall(t, st, oc, func() {
		_, _ = oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
	})
	after, _ := st.GetIdentity(ctx, idx)
	if !bytes.Equal(after.NOC, before.NOC) {
		t.Fatalf("the NOC UpdateNOC wrote while the fail-safe expired survived the expiry (expiry waited for UpdateNOC: %v)", waited)
	}
}
