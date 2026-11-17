// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// countingServer counts its reads.
type countingServer struct{ reads atomic.Int32 }

func (*countingServer) MatterClusterID() uint32 { return 0xFFF1FC06 }
func (s *countingServer) MatterRead(attr uint32) (any, bool) {
	s.reads.Add(1)
	if attr <= 1 {
		return uint8(attr), true
	}
	return nil, false
}

func (*countingServer) MatterAttributes() []uint32                             { return []uint32{0, 1} }
func (*countingServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*countingServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*countingServer) MatterReportable() []uint32                             { return nil }

// TestDeniedReadDoesNotReadTheServer (owner decision 162,
// R2-P6-bound-0405): a subject without a grant drives no read of a server —
// the dispatcher authorises each resolved location before it reads there,
// for a concrete and a wildcard path alike; a granted subject reads.
func TestDeniedReadDoesNotReadTheServer(t *testing.T) {
	t.Parallel()
	srv := &countingServer{}
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntryWithSubjects(1, store.PrivilegeAdminister, []uint64{0x1111}, nil)}})
	denied := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x2222, nil)
	granted := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
	paths := []im.ConcreteAttributePath{
		concreteAttrPath(2, 0xFFF1FC06, 0),
		{Endpoint: 2, Cluster: 0xFFF1FC06, HasEndpoint: true, HasCluster: true},
		{Cluster: 0xFFF1FC06, HasCluster: true},
	}
	rd := im.HandleReadRequest(denied, d, im.ReadRequest{AttributeRequests: paths})
	if n := srv.reads.Load(); n != 0 {
		t.Errorf("a denied subject drove %d reads of the server", n)
	}
	if len(rd.Reports) != 1 || !rd.Reports[0].IsStatus || rd.Reports[0].Status.Status != im.StatusUnsupportedAccess {
		t.Errorf("denied: reports %+v, want one UnsupportedAccess (the concrete path)", rd.Reports)
	}
	rd = im.HandleReadRequest(granted, d, im.ReadRequest{AttributeRequests: paths[:1]})
	if srv.reads.Load() == 0 || len(rd.Reports) != 1 || rd.Reports[0].IsStatus {
		t.Errorf("granted: reports %+v, server reads %d", rd.Reports, srv.reads.Load())
	}
}
