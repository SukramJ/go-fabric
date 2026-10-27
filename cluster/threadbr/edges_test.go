// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"context"
	"errors"
	"testing"

	tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tnd "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/thread"
)

// failingBorderRouter fails the commit and the revert and answers a
// dataset that does not frame.
type failingBorderRouter struct{ *fakeBorderRouter }

func (failingBorderRouter) CommitActiveDataset(context.Context) error { return errors.New("commit") }

func (failingBorderRouter) RevertActiveDataset(context.Context) error { return errors.New("revert") }

// The server's edges: a commit and a revert that fail are logged and
// change nothing else (LogErrorOnFailure, :321; (void) RevertActiveDataset,
// :309); a dataset that does not frame has no timestamp; a refresh after a
// command that fails is logged; the accessor names the border router.
func TestManagementEdges(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.active = []byte{0x0e, 0x09, 0}
	s := newManagement(t, failingBorderRouter{br}.fakeBorderRouter, 0, true, nil)
	if v, _ := s.MatterRead(tbrm.AttrActiveDatasetTimestamp); v != nil {
		t.Errorf("timestamp of an unframed dataset = %v", v)
	}
	if s.BorderRouter() == nil {
		t.Error("no border router")
	}
	f, err := threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{BorderRouter: failingBorderRouter{newFake()}})
	mustT(t, err)
	f.OnCommissioningComplete(context.Background())
	f.OnFailSafeExpired(context.Background())
	// A pending dataset set while the border router stops answering is
	// answered, and the refresh after it logs.
	p := newManagement(t, br, tbrm.FeaturePanChange, false, nil)
	br.set(func(f *fakeBorderRouter) { f.agentErr = errors.New("down") })
	if _, st := invoke(p, tbrm.CmdSetPendingDatasetRequest, tbrm.SetPendingDatasetRequestRequest{PendingDataset: chipDataset}); st != im.StatusSuccess {
		t.Errorf("SetPendingDatasetRequest = %v", st)
	}
	if err := p.SetAttributes(map[uint32]any{tbrm.AttrInterfaceEnabled: true}); err != nil {
		t.Errorf("Set of the served InterfaceEnabled = %v", err)
	}
}

// The diagnostics server's edges: an extended address that is not 8
// bytes is null, the typed Set of the served value passes, a refresh
// reads a router link and a child the record adds, the first of two
// Network Name sub-TLVs counts, and a name over 16 bytes reads null.
func TestDiagnosticsEdges(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.active = chipDataset
	d := meshDiagnostics()
	d.ExtAddress = []byte{1, 2, 3}
	br.diag = d
	s := newDiagnostics(t, br, 0)
	if v := read(t, s, tnd.AttrExtAddress); v != nil {
		t.Errorf("ExtAddress of 3 bytes = %v", v)
	}
	if err := s.SetAttributes(map[uint32]any{tnd.AttrExtAddress: nil}); err != nil {
		t.Errorf("Set of the served null = %v", err)
	}
	br.set(func(f *fakeBorderRouter) {
		f.diag.Self.Route64.Entries = append(f.diag.Self.Route64.Entries, thread.Route64Entry{RouterID: 7, LinkQualityIn: 1, LinkQualityOut: 1})
		f.diag.Self.ChildTable = append(f.diag.Self.ChildTable, thread.ChildTableEntry{ChildID: 9, IncomingLinkQuality: 200})
	})
	mustT(t, s.Refresh(context.Background()))
	br.set(func(f *fakeBorderRouter) {
		f.active = append(append([]byte{}, chipDataset...), 0x03, 0x00) // a second, empty name; the first counts
	})
	mustT(t, s.Refresh(context.Background()))
	long := append([]byte{0x03, 17}, []byte("seventeen-bytes!!")...)
	br.set(func(f *fakeBorderRouter) { f.active = append(long, chipDataset...) })
	mustT(t, s.Refresh(context.Background()))
	if v := read(t, s, tnd.AttrNetworkName); v != nil {
		t.Errorf("NetworkName over 16 bytes = %v", v)
	}
	if _, err := threadbr.NewDiagnostics(context.Background(), threadbr.DiagnosticsConfig{BorderRouter: br, Features: tnd.FeatureMleCounts, DataVersion: nil}); err != nil {
		t.Errorf("MleCounts alone = %v", err)
	}
}
