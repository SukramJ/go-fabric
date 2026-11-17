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

// fencedServer serves attributes 0 and 1 and admits a request's read of
// attribute 1 only from fabric 1; it counts its reads.
type fencedServer struct{ reads atomic.Int32 }

func (*fencedServer) MatterClusterID() uint32 { return 0xFFF1FC05 }
func (s *fencedServer) MatterRead(attr uint32) (any, bool) {
	s.reads.Add(1)
	if attr <= 1 {
		return uint8(attr + 10), true
	}
	return nil, false
}

func (*fencedServer) MatterAttributes() []uint32                             { return []uint32{0, 1} }
func (*fencedServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*fencedServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*fencedServer) MatterReportable() []uint32                             { return nil }
func (*fencedServer) MatterReadAllowed(ctx context.Context, attr uint32) bool {
	fabric, _, _ := contract.AccessingFabric(ctx)
	return attr != 1 || fabric == 1
}

// TestReadAccessDeclarerIsAskedBeforeTheRead (decision 152): the
// dispatcher asks a contract.ReadAccessDeclarer before the server's read;
// a refused concrete path is answered UNSUPPORTED_ACCESS, a refused
// attribute under a wildcard is left out without a status, and the
// server's read is not called for it. A local read is not asked.
func TestReadAccessDeclarerIsAskedBeforeTheRead(t *testing.T) {
	t.Parallel()
	srv := &fencedServer{}
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}))
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(2, store.PrivilegeAdminister, nil), caseEntry(1, store.PrivilegeAdminister, nil)}})
	fabric2 := im.WithSubject(im.WithFabricFilter(context.Background(), true, 2), 0x2222, nil)
	fabric1 := im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), 0x1111, nil)
	concrete := concreteAttrPath(2, 0xFFF1FC05, 1)

	res := d.Read(fabric2, concrete)
	if len(res) != 1 || res[0].Status != im.StatusUnsupportedAccess || srv.reads.Load() != 0 {
		t.Fatalf("concrete refused read: %+v, server reads %d — want one UnsupportedAccess, no read", res, srv.reads.Load())
	}
	if st := d.AttributePathStatus(fabric2, concrete); st != im.StatusUnsupportedAccess {
		t.Errorf("AttributePathStatus = %v, want UnsupportedAccess", st)
	}
	// Through the IM read: the status reaches the wire as the path's.
	rd := im.HandleReadRequest(fabric2, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{concrete}})
	if len(rd.Reports) != 1 || !rd.Reports[0].IsStatus || rd.Reports[0].Status.Status != im.StatusUnsupportedAccess {
		t.Errorf("IM read reports %+v, want one UnsupportedAccess status", rd.Reports)
	}
	// Wildcards: attribute 1 left out, attribute 0 (and the globals) read.
	for _, wild := range []im.ConcreteAttributePath{
		{Endpoint: 2, Cluster: 0xFFF1FC05, HasEndpoint: true, HasCluster: true},
		{Cluster: 0xFFF1FC05, Attribute: 1, HasCluster: true, HasAttribute: true},
	} {
		for _, r := range d.Read(fabric2, wild) {
			if r.Path.Attribute == 1 {
				t.Errorf("wildcard %+v: attribute 1 reported %+v", wild, r)
			}
		}
	}
	if got := d.Read(fabric2, concreteAttrPath(2, 0xFFF1FC05, 0)); len(got) != 1 || got[0].Status != im.StatusSuccess {
		t.Errorf("an admitted attribute: %+v", got)
	}
	if got := d.Read(fabric1, concrete); len(got) != 1 || got[0].Status != im.StatusSuccess {
		t.Errorf("the admitted fabric: %+v", got)
	}
	if got := d.Read(context.Background(), concrete); len(got) != 1 || got[0].Status != im.StatusSuccess {
		t.Errorf("a local read: %+v", got)
	}
}
