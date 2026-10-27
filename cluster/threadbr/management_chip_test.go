// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of connectedhomeip's
// src/app/clusters/thread-border-router-management-server/tests/
// TestThreadBorderRouterManagementCluster.cpp at 6170af84, ported by
// meaning, one test per TEST_F (all 22): chip's MockDelegate is
// fakeBorderRouter, its FailSafeContext the IsFailSafeArmed function, its
// BreadCrumbTracker breadcrumbs, and the platform events the server's
// OnFailSafeExpired and OnCommissioningComplete. A delegate that defers
// its completion is an activation the fake keeps waiting.

const waitFor = 5 * time.Second

func newManagement(t *testing.T, br *fakeBorderRouter, features tbrm.Feature, armed bool, bc *breadcrumbs) *threadbr.ManagementServer {
	t.Helper()
	cfg := threadbr.ManagementConfig{BorderRouter: br, Features: features, IsFailSafeArmed: func() bool { return armed }}
	if bc != nil {
		cfg.SetBreadcrumb = bc.set
	}
	s, err := threadbr.NewManagement(context.Background(), cfg)
	mustT(t, err)
	return s
}

// invoke runs a command on a CASE session.
func invoke(s *threadbr.ManagementServer, cmd uint32, req any) (any, im.StatusCode) {
	resp, err := s.MatterInvoke(caseCtx(), cmd, req)
	return resp, statusOf(err)
}

// invokeAsync starts a command that waits for its activation.
func invokeAsync(s *threadbr.ManagementServer, cmd uint32, req any) <-chan im.StatusCode {
	out := make(chan im.StatusCode, 1)
	go func() {
		_, st := invoke(s, cmd, req)
		out <- st
	}()
	return out
}

func awaitStatus(t *testing.T, ch <-chan im.StatusCode) im.StatusCode {
	t.Helper()
	select {
	case st := <-ch:
		return st
	case <-time.After(waitFor):
		t.Fatal("the request was not answered")
		return 0
	}
}

func awaitStarted(t *testing.T, br *fakeBorderRouter) []byte {
	t.Helper()
	select {
	case ds := <-br.started:
		return ds
	case <-time.After(waitFor):
		t.Fatal("the border router was not asked to activate")
		return nil
	}
}

// :156-165.
func TestThreadBorderRouterManagementCluster_TestReadClusterRevision(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	if v, _ := s.MatterRead(cluster.AttrGlobalClusterRevision); v != tbrm.Revision {
		t.Errorf("ClusterRevision = %v, want %d", v, tbrm.Revision)
	}
}

// :167-176: the attribute list is the mandatory set.
func TestThreadBorderRouterManagementCluster_TestAttributesList(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	want := []uint32{
		tbrm.AttrBorderRouterName, tbrm.AttrBorderAgentId, tbrm.AttrThreadVersion,
		tbrm.AttrInterfaceEnabled, tbrm.AttrActiveDatasetTimestamp, tbrm.AttrPendingDatasetTimestamp,
	}
	for _, id := range want {
		if !slices.Contains(s.MatterAttributes(), id) {
			t.Errorf("attribute 0x%04X not listed", id)
		}
	}
}

// :178-184.
func TestThreadBorderRouterManagementCluster_TestGeneratedCommandsList(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	if got := s.MatterGeneratedCommands(); !slices.Equal(got, []uint32{tbrm.CmdDatasetResponse}) {
		t.Errorf("GeneratedCommandList = %v", got)
	}
}

// :186-215.
func TestThreadBorderRouterManagementCluster_TestReadAttributes(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	for id, want := range map[uint32]any{
		tbrm.AttrBorderRouterName:       "MockBR",
		tbrm.AttrThreadVersion:          uint16(1),
		tbrm.AttrInterfaceEnabled:       true,
		tbrm.AttrActiveDatasetTimestamp: uint64(12345),
	} {
		if v, _ := s.MatterRead(id); v != want {
			t.Errorf("attribute 0x%04X = %v (%T), want %v", id, v, v, want)
		}
	}
	if v, ok := s.MatterRead(tbrm.AttrBorderAgentId); !ok || len(v.([]byte)) != threadbr.BorderAgentIDLen {
		t.Errorf("BorderAgentID = %v", v)
	}
	if v, ok := s.MatterRead(tbrm.AttrPendingDatasetTimestamp); !ok || v != nil {
		t.Errorf("PendingDatasetTimestamp = %v, want null", v)
	}
}

// :217-225: an agent id of 10 bytes fails. chip fails the read; this
// server reads the border router at construction and refuses it then.
func TestThreadBorderRouterManagementCluster_TestReadBorderAgentID_InvalidLength(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.agentID = make([]byte, 10)
	_, err := threadbr.NewManagement(context.Background(), threadbr.ManagementConfig{BorderRouter: br})
	if !errors.Is(err, threadbr.ErrBorderAgentID) {
		t.Errorf("NewManagement = %v, want ErrBorderAgentID", err)
	}
}

// :227-236.
func TestThreadBorderRouterManagementCluster_TestReadBorderAgentID_ValidLength(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	if v, _ := s.MatterRead(tbrm.AttrBorderAgentId); len(v.([]byte)) != 16 {
		t.Errorf("BorderAgentID of %d bytes", len(v.([]byte)))
	}
}

// :238-247.
func TestThreadBorderRouterManagementCluster_TestFeatureMap_PanChangeSupported(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	if v, _ := s.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(tbrm.FeaturePanChange) {
		t.Errorf("FeatureMap = %v", v)
	}
}

// :249-265.
func TestThreadBorderRouterManagementCluster_TestFeatureMap_PanChangeNotSupported(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	if v, _ := s.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(0) {
		t.Errorf("FeatureMap = %v", v)
	}
}

// :267-278.
func TestThreadBorderRouterManagementCluster_TestGetActiveDatasetRequest(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	resp, st := invoke(s, tbrm.CmdGetActiveDatasetRequest, tbrm.GetActiveDatasetRequestRequest{})
	if st != im.StatusSuccess {
		t.Fatalf("GetActiveDatasetRequest = %v", st)
	}
	if ds := resp.(tbrm.DatasetResponse).Dataset; len(ds) != 10 {
		t.Errorf("DatasetResponse = %x", ds)
	}
}

// :280-291.
func TestThreadBorderRouterManagementCluster_TestGetPendingDatasetRequest(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	resp, st := invoke(s, tbrm.CmdGetPendingDatasetRequest, tbrm.GetPendingDatasetRequestRequest{})
	if st != im.StatusSuccess {
		t.Fatalf("GetPendingDatasetRequest = %v", st)
	}
	if _, ok := resp.(tbrm.DatasetResponse); !ok {
		t.Errorf("response %T", resp)
	}
}

// :293-315: no active dataset, the fail-safe armed, a valid dataset and a
// breadcrumb: the activation completes and the request succeeds.
func TestThreadBorderRouterManagementCluster_TestSetActiveDatasetRequest(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound = true
	bc := &breadcrumbs{}
	s := newManagement(t, br, 0, true, bc)
	one := uint64(1)
	_, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset, Breadcrumb: &one})
	if st != im.StatusSuccess {
		t.Fatalf("SetActiveDatasetRequest = %v", st)
	}
	if got := awaitStarted(t, br); !slices.Equal(got, chipDataset) {
		t.Errorf("activated %x", got)
	}
	if v, called := bc.get(); !called || v != 1 {
		t.Errorf("breadcrumb %d, %v", v, called)
	}
}

// :317-335.
func TestThreadBorderRouterManagementCluster_TestSetPendingDatasetRequest(t *testing.T) {
	t.Parallel()
	br := newFake()
	s := newManagement(t, br, tbrm.FeaturePanChange, false, nil)
	_, st := invoke(s, tbrm.CmdSetPendingDatasetRequest, tbrm.SetPendingDatasetRequestRequest{PendingDataset: chipDataset})
	if st != im.StatusSuccess {
		t.Fatalf("SetPendingDatasetRequest = %v", st)
	}
	if !slices.Equal(br.setPending, chipDataset) {
		t.Errorf("pending %x", br.setPending)
	}
}

// :337-347.
func TestThreadBorderRouterManagementCluster_TestAcceptedCommandsList_PanChangeSupported(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, false, nil)
	want := []uint32{tbrm.CmdGetActiveDatasetRequest, tbrm.CmdGetPendingDatasetRequest, tbrm.CmdSetActiveDatasetRequest, tbrm.CmdSetPendingDatasetRequest}
	if got := s.MatterAcceptedCommands(); !slices.Equal(got, want) {
		t.Errorf("AcceptedCommandList = %v, want %v", got, want)
	}
}

// :349-359.
func TestThreadBorderRouterManagementCluster_TestAcceptedCommandsList_PanChangeNotSupported(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	want := []uint32{tbrm.CmdGetActiveDatasetRequest, tbrm.CmdGetPendingDatasetRequest, tbrm.CmdSetActiveDatasetRequest}
	if got := s.MatterAcceptedCommands(); !slices.Equal(got, want) {
		t.Errorf("AcceptedCommandList = %v, want %v", got, want)
	}
}

// :361-375.
func TestThreadBorderRouterManagementCluster_TestSetActiveDatasetRequestFailsafeRequired(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, false, nil)
	one := uint64(1)
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{Breadcrumb: &one}); st != im.StatusFailsafeRequired {
		t.Errorf("SetActiveDatasetRequest = %v, want FAILSAFE_REQUIRED", st)
	}
}

// :377-390.
func TestThreadBorderRouterManagementCluster_TestFailSafeTimerExpired(t *testing.T) {
	t.Parallel()
	br := newFake()
	s := newManagement(t, br, 0, false, nil)
	s.OnFailSafeExpired(context.Background())
	if !br.revertCalled {
		t.Error("RevertActiveDataset not called")
	}
}

// :392-420: the activation waits, the fail-safe expires, the border router
// reports its cancelled activation: the cluster ignores it and answers
// TIMEOUT, and writes no breadcrumb.
func TestThreadBorderRouterManagementCluster_TestFailSafeExpiryRespondsTimeoutWhenDelegateCompletesInRevert(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound, br.deferActivation = true, true
	bc := &breadcrumbs{}
	s := newManagement(t, br, 0, true, bc)
	crumb := uint64(42)
	answer := invokeAsync(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: []byte{}, Breadcrumb: &crumb})
	awaitStarted(t, br)
	s.OnFailSafeExpired(context.Background())
	if st := awaitStatus(t, answer); st != im.StatusTimeout {
		t.Errorf("SetActiveDatasetRequest = %v, want TIMEOUT", st)
	}
	if _, called := bc.get(); called {
		t.Error("a breadcrumb was written")
	}
}

// :422-444.
func TestThreadBorderRouterManagementCluster_TestBreadcrumbHandling(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound, br.deferActivation = true, true
	bc := &breadcrumbs{}
	s := newManagement(t, br, 0, true, bc)
	crumb := uint64(42)
	answer := invokeAsync(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: []byte{}, Breadcrumb: &crumb})
	awaitStarted(t, br)
	br.release <- nil // the asynchronous completion (:438-439)
	if st := awaitStatus(t, answer); st != im.StatusSuccess {
		t.Errorf("SetActiveDatasetRequest = %v", st)
	}
	if v, called := bc.get(); !called || v != 42 {
		t.Errorf("breadcrumb %d, %v, want 42", v, called)
	}
}

// :446-465: a session that is not CASE is UNSUPPORTED_ACCESS; chip's
// AuthMode kNone, here PASE and a group message too.
func TestThreadBorderRouterManagementCluster_TestGetActiveDatasetRequestRequiresCASE(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), tbrm.FeaturePanChange, true, nil)
	for name, ctx := range map[string]context.Context{"none": context.Background(), "PASE": paseCtx(), "group": groupCtx()} {
		for _, cmd := range []uint32{tbrm.CmdGetActiveDatasetRequest, tbrm.CmdGetPendingDatasetRequest, tbrm.CmdSetActiveDatasetRequest, tbrm.CmdSetPendingDatasetRequest} {
			if _, err := s.MatterInvoke(ctx, cmd, nil); statusOf(err) != im.StatusUnsupportedAccess {
				t.Errorf("%s: command 0x%02X = %v, want UNSUPPORTED_ACCESS", name, cmd, err)
			}
		}
	}
}

// :467-482.
func TestThreadBorderRouterManagementCluster_TestGetActiveDatasetRequestNotFoundReturnsEmpty(t *testing.T) {
	t.Parallel()
	br := newFake()
	br.notFound = true
	s := newManagement(t, br, 0, false, nil)
	resp, st := invoke(s, tbrm.CmdGetActiveDatasetRequest, tbrm.GetActiveDatasetRequestRequest{})
	if st != im.StatusSuccess {
		t.Fatalf("GetActiveDatasetRequest = %v", st)
	}
	if ds := resp.(tbrm.DatasetResponse).Dataset; ds == nil || len(ds) != 0 {
		t.Errorf("DatasetResponse = %x, want empty", ds)
	}
}

// :484-507.
func TestThreadBorderRouterManagementCluster_TestSetActiveDatasetRequestInvalidInState(t *testing.T) {
	t.Parallel()
	s := newManagement(t, newFake(), 0, true, nil)
	if _, st := invoke(s, tbrm.CmdSetActiveDatasetRequest, tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset}); st != im.StatusInvalidInState {
		t.Errorf("SetActiveDatasetRequest = %v, want INVALID_IN_STATE", st)
	}
}

// :509-522.
func TestThreadBorderRouterManagementCluster_TestCommissioningComplete(t *testing.T) {
	t.Parallel()
	br := newFake()
	s := newManagement(t, br, 0, false, nil)
	s.OnCommissioningComplete(context.Background())
	if !br.commitCalled {
		t.Error("CommitActiveDataset not called")
	}
}
