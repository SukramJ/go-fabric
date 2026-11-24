// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	icddef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// icdListHoldA2 holds one ListICDRegistrations — after it read the rows —
// until release is closed.
type icdListHoldA2 struct {
	*memstore.Store
	hold    atomic.Bool
	held    chan struct{}
	release chan struct{}
}

func (s *icdListHoldA2) ListICDRegistrations(ctx context.Context) ([]store.ICDRegistration, error) {
	regs, err := s.Store.ListICDRegistrations(ctx)
	if s.hold.CompareAndSwap(true, false) {
		close(s.held)
		<-s.release
	}
	return regs, err
}

// TestICDStartDoesNotUndoARegistration (review II, R4-conc-4056): a
// RegisterClient served while Start loads the registrations is not
// overwritten by Start's older list — Start loads and swaps under the
// command lock, so the registration waits for it and then lands.
func TestICDStartDoesNotUndoARegistration(t *testing.T) {
	t.Parallel()
	hs := &icdListHoldA2{Store: icdStore(t), held: make(chan struct{}), release: make(chan struct{})}
	s := newICDServer(t, core.ICDManagementConfig{Store: hs, ClientsSupportedPerFabric: 2})
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.Stop()

	hs.hold.Store(true)
	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	<-hs.held

	header := make(chan string, 1)
	regDone := make(chan struct{})
	var regErr error
	go func() {
		defer close(regDone)
		header <- goroutineHeaderA2()
		_, regErr = s.MatterInvoke(icdInvokeCtx(1, privAdminister), icddef.CmdRegisterClient, icddef.RegisterClientRequest{
			CheckInNodeId: 0x10, MonitoredSubject: 0x10, Key: icdKey(1),
		})
	}()
	waitLockedOrDoneA2(t, <-header, "registerClient", regDone)
	close(hs.release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	<-regDone
	if regErr != nil {
		t.Fatal(regErr)
	}
	if got := s.Registrations(1); len(got) != 1 || got[0].CheckInNodeID != 0x10 {
		t.Fatalf("Registrations(1) = %+v, want the registration of 0x10", got)
	}
	rows, err := hs.Store.ListICDRegistrations(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("stored registrations %+v %v, want one", rows, err)
	}
}
