// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/im"
)

// The rules of ThreadBorderRouterManagementCluster.cpp at 6170af84 chip's
// unit cases do not reach, one test per cited line.

// :217-221: a dataset chip's Init refuses (a TLV past the end) is
// INVALID_COMMAND, for SetActiveDatasetRequest (:220) and
// SetPendingDatasetRequest (:261).
func TestManagementRefusesAnUnframedDataset(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound = true
	s := newManagement(t, br, tbrm.FeaturePanChange, true, nil)
	bad := []byte{0x03, 0x05, 'a'}
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: bad}); st != im.StatusInvalidCommand {
		t.Errorf("SetActiveDatasetRequest = %v, want INVALID_COMMAND", st)
	}
	if _, st := invoke(s, tbrm.CmdSetPendingDatasetRequest, tbrm.SetPendingDatasetRequestRequest{PendingDataset: bad}); st != im.StatusInvalidCommand {
		t.Errorf("SetPendingDatasetRequest = %v, want INVALID_COMMAND", st)
	}
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, "not a request"); st != im.StatusInvalidCommand {
		t.Errorf("a foreign payload = %v", st)
	}
	if _, st := invoke(s, tbrm.CmdSetPendingDatasetRequest, "not a request"); st != im.StatusInvalidCommand {
		t.Errorf("a foreign payload = %v", st)
	}
}

// :232-235: a second activation while one waits is BUSY; once the first
// completes the next is accepted (:279, the handle moves out).
func TestManagementIsBusyWhileAnActivationWaits(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound, br.deferActivation = true, true
	s := newManagement(t, br, 0, true, nil)
	first := invokeAsync(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset})
	awaitStarted(t, br)
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset}); st != im.StatusBusy {
		t.Errorf("second SetActiveDatasetRequest = %v, want BUSY", st)
	}
	br.release <- spec.Errorf(im.StatusFailure, "attach failed")
	if st := awaitStatus(t, first); st != im.StatusFailure {
		t.Errorf("first = %v, want the completion's FAILURE (:292-294)", st)
	}
	br.set(func(f *fakeBorderRouter) { f.deferActivation = false })
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset}); st != im.StatusSuccess {
		t.Errorf("third = %v", st)
	}
}

// :286-294: a failed activation writes no breadcrumb and answers the
// status its error carries, FAILURE for a plain one.
func TestManagementAnswersTheActivationsStatus(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		err  error
		want im.StatusCode
	}{
		{errors.New("plain"), im.StatusFailure},
		{spec.Errorf(im.StatusResourceExhausted, "carried"), im.StatusResourceExhausted},
	} {
		br := newFake()
		br.notFound, br.activateErr = true, c.err
		bc := &breadcrumbs{}
		s := newManagement(t, br, 0, true, bc)
		crumb := uint64(7)
		if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset, Breadcrumb: &crumb}); st != c.want {
			t.Errorf("%v: %v, want %v", c.err, st, c.want)
		}
		if _, called := bc.get(); called {
			t.Errorf("%v: a breadcrumb was written", c.err)
		}
	}
}

// :281-284: a request whose exchange ended is not answered and holds
// nothing up; its later completion writes no breadcrumb.
func TestManagementForgetsARequestWhoseExchangeEnded(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound, br.deferActivation = true, true
	bc := &breadcrumbs{}
	s := newManagement(t, br, 0, true, bc)
	ctx, cancel := context.WithCancel(caseCtx())
	crumb := uint64(9)
	done := make(chan error, 1)
	go func() {
		_, err := s.MatterInvoke(ctx, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset, Breadcrumb: &crumb})
		done <- err
	}()
	awaitStarted(t, br)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("abandoned request = %v", err)
	}
	br.release <- nil
	br.set(func(f *fakeBorderRouter) { f.deferActivation = false })
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset}); st != im.StatusSuccess {
		t.Errorf("next request = %v, want accepted", st)
	}
	if _, called := bc.get(); called {
		t.Error("the abandoned request's breadcrumb was written")
	}
}

// :164-173: a border router that fails to read the dataset fails the
// request with its error's status.
func TestManagementGetDatasetFailure(t *testing.T) {
	t.Parallel()
	br := newFake()
	s := newManagement(t, br, 0, false, nil)
	br.set(func(f *fakeBorderRouter) { f.datasetErr = errors.New("unreachable") })
	if _, st := invoke(s, tbrm.CmdGetActiveDatasetRequest, tbrm.GetActiveDatasetRequestRequest{}); st != im.StatusFailure {
		t.Errorf("GetActiveDatasetRequest = %v, want FAILURE", st)
	}
}

// :253-256, :264-265: without PanChange SetPendingDatasetRequest is not
// accepted; the border router's refusal answers with its status.
func TestManagementSetPendingDataset(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	if _, st := invoke(s, tbrm.CmdSetPendingDatasetRequest, tbrm.SetPendingDatasetRequestRequest{PendingDataset: chipDataset}); st != im.StatusUnsupportedCommand {
		t.Errorf("without PanChange = %v, want UNSUPPORTED_COMMAND", st)
	}
	br := newFake()
	br.pendingErr = errors.New("409")
	s = newManagement(t, br, tbrm.FeaturePanChange, false, nil)
	if _, st := invoke(s, tbrm.CmdSetPendingDatasetRequest, tbrm.SetPendingDatasetRequestRequest{PendingDataset: chipDataset}); st != im.StatusFailure {
		t.Errorf("refused = %v, want FAILURE", st)
	}
}

// :61-80 and ADR 0027's consequence: Refresh reports what moved, the
// pending dataset's Active Timestamp is PendingDatasetTimestamp, and a
// border router that stops answering reads InterfaceEnabled false and
// null timestamps while the agent id keeps its last value.
func TestManagementRefresh(t *testing.T) {
	t.Parallel()
	br := newFake()
	s := newManagement(t, br, 0, false, nil)
	dirty := watch(s)
	v0 := s.MatterDataVersion()
	br.set(func(f *fakeBorderRouter) { f.pending = timestamped(99) })
	mustT(t, s.Refresh(context.Background()))
	if v, _ := s.MatterRead(tbrm.AttrPendingDatasetTimestamp); v != uint64(99) {
		t.Errorf("PendingDatasetTimestamp = %v", v)
	}
	if !dirty.has(tbrm.AttrPendingDatasetTimestamp) || s.MatterDataVersion() == v0 {
		t.Error("the change was not reported")
	}
	br.set(func(f *fakeBorderRouter) {
		f.enabledErr, f.datasetErr, f.agentErr = errors.New("down"), errors.New("down"), errors.New("down")
	})
	if err := s.Refresh(context.Background()); err == nil {
		t.Error("Refresh of a silent border router reported no error")
	}
	if v, _ := s.MatterRead(tbrm.AttrInterfaceEnabled); v != false {
		t.Errorf("InterfaceEnabled = %v, want false", v)
	}
	for _, id := range []uint32{tbrm.AttrActiveDatasetTimestamp, tbrm.AttrPendingDatasetTimestamp} {
		if v, _ := s.MatterRead(id); v != nil {
			t.Errorf("attribute 0x%04X = %v, want null", id, v)
		}
	}
	if v, _ := s.MatterRead(tbrm.AttrBorderAgentId); len(v.([]byte)) != threadbr.BorderAgentIDLen {
		t.Error("the agent id was dropped")
	}
	br.set(func(f *fakeBorderRouter) { f.agentErr, f.agentID = nil, make([]byte, 3) })
	if err := s.Refresh(context.Background()); !errors.Is(err, threadbr.ErrBorderAgentID) {
		t.Errorf("Refresh with a short agent id = %v", err)
	}
	br.set(func(f *fakeBorderRouter) { f.name = "" })
	if err := s.Refresh(context.Background()); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Refresh with an empty name = %v, want the model's 1 to 63", err)
	}
}

// Construction: a border router is required; one that serves no agent id
// or no version is refused.
func TestNewManagementRefusals(t *testing.T) {
	t.Parallel()
	if _, err := threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{}); !errors.Is(err, threadbr.ErrNoBorderRouter) {
		t.Errorf("no border router = %v", err)
	}
	br := newFake()
	br.agentErr = errors.New("down")
	if _, err := threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{BorderRouter: br}); err == nil {
		t.Error("a border router without an agent id was accepted")
	}
	br = newFake()
	br.name = ""
	if _, err := threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{BorderRouter: br}); err == nil {
		t.Error("an empty name was accepted")
	}
}

// AttachFailSafe wires the fail-safe after construction; the typed Set
// cannot move an attribute the border router holds.
func TestManagementAttachFailSafeAndSet(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound = true
	s := newManagement(t, br, 0, false, nil)
	bc := &breadcrumbs{}
	s.AttachFailSafe(func() bool { return true }, bc.set)
	crumb := uint64(3)
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset, Breadcrumb: &crumb}); st != im.StatusSuccess {
		t.Errorf("SetActiveDatasetRequest = %v", st)
	}
	if v, _ := bc.get(); v != 3 {
		t.Errorf("breadcrumb %d", v)
	}
	if err := s.SetAttributes(map[uint32]any{tbrm.AttrBorderRouterName: "MockBR"}); err != nil {
		t.Errorf("Set of the served name = %v", err)
	}
	if err := s.SetAttributes(map[uint32]any{tbrm.AttrBorderRouterName: "Other"}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of another name = %v, want ErrNotHeld", err)
	}
	if err := s.SetAttributes(map[uint32]any{tbrm.AttrBorderAgentId: []byte{1}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of another agent id = %v, want ErrNotHeld", err)
	}
	if err := s.SetAttributes(map[uint32]any{tbrm.AttrThreadVersion: []byte{1}}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of bytes for a number = %v, want ErrNotHeld", err)
	}
	if err := s.MatterWrite(context.Background(), tbrm.AttrBorderRouterName, "x"); statusOf(err) != im.StatusUnsupportedWrite {
		t.Errorf("a controller's write = %v, want UNSUPPORTED_WRITE", err)
	}
}
