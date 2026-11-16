// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// fabricList is a core.FabricSource over a fixed set of indices.
type fabricList []uint8

func (l fabricList) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	out := make([]store.FabricRecord, 0, len(l))
	for _, f := range l {
		out = append(out, store.FabricRecord{FabricIndex: f})
	}
	return out, nil
}

// TestBindingRefusesARemovedFabricsWrite (review II, R2-P6-store-1010,
// decision 158): a persisted Binding checks a request's fabric against the
// node's fabric list — the source AccessControl uses — and refuses, with
// nothing stored, a write of a fabric the list no longer holds; without a
// fabric source a persisted Binding refuses every request write (fail
// closed).
func TestBindingRefusesARemovedFabricsWrite(t *testing.T) {
	t.Parallel()
	entry := core.TargetStruct{Node: 7, Endpoint: 1, EndpointPresent: true}
	st := &bindingStore{}
	b := core.NewBinding()
	cfg := st.config()
	cfg.Fabrics = fabricList{1}
	if err := b.Persist(cfg); err != nil {
		t.Fatal(err)
	}
	err := b.MatterWrite(fabricCtx(2), 0x0000, []core.TargetStruct{entry})
	if code := statusOf(err); code != im.StatusUnsupportedAccess {
		t.Errorf("write of removed fabric 2: %v, want UNSUPPORTED_ACCESS", err)
	}
	if got := st.stored(); len(got) != 0 {
		t.Errorf("stored %+v for a removed fabric", got)
	}
	if err := b.MatterWrite(fabricCtx(1), 0x0000, []core.TargetStruct{entry}); err != nil {
		t.Errorf("write of fabric 1: %v", err)
	}

	closed := core.NewBinding()
	if err := closed.Persist(core.BindingConfig{Save: (&bindingStore{}).save}); err != nil {
		t.Fatal(err)
	}
	if err := closed.MatterWrite(fabricCtx(1), 0x0000, []core.TargetStruct{entry}); statusOf(err) != im.StatusUnsupportedAccess {
		t.Errorf("persisted Binding without a fabric source: %v, want UNSUPPORTED_ACCESS", err)
	}
}

// TestICDRegistrationOfARemovedFabricStoresNothing (review II,
// R2-P6-store-1010, decision 158): RegisterClient of a fabric the store no
// longer lists is refused and nothing is stored.
func TestICDRegistrationOfARemovedFabricStoresNothing(t *testing.T) {
	t.Parallel()
	st := icdStore(t)
	s := newICDServer(t, core.ICDManagementConfig{Store: st})
	if err := st.RemoveFabric(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	_, err := s.MatterInvoke(icdInvokeCtx(2, privAdminister), icddef.CmdRegisterClient,
		icddef.RegisterClientRequest{CheckInNodeId: 9, MonitoredSubject: 9, Key: icdKey(1)})
	if icdStatus(err) != im.StatusUnsupportedAccess {
		t.Errorf("RegisterClient of removed fabric 2: %v, want UNSUPPORTED_ACCESS", err)
	}
	regs, err := st.ListICDRegistrations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(regs, func(r store.ICDRegistration) bool { return r.FabricIndex == 2 }) {
		t.Errorf("registrations %+v stored for a removed fabric", regs)
	}
}
