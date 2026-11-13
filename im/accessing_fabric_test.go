// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// grantingDispatcher grants every access check it is asked and records what
// reaches it: the gates in front of it are what the tests measure.
type grantingDispatcher struct {
	checked []uint8 // the fabric index of every CheckACL call
	invoked []ConcreteCommandPath
	written []ConcreteAttributePath
}

func (d *grantingDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	return []ReadResult{{Path: p, Status: StatusSuccess}}
}

func (d *grantingDispatcher) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	d.written = append(d.written, p)
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

func (d *grantingDispatcher) Invoke(ctx context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	d.invoked = append(d.invoked, p)
	return InvokeResult{Path: p, Status: AuthorityAt(ctx, p.Endpoint, p.Cluster, 5)}
}

// InvokeAuthorized expands a wildcard-endpoint command to endpoints 2 and 3.
func (d *grantingDispatcher) InvokeAuthorized(ctx context.Context, p ConcreteCommandPath, f any, authorize CommandAuthorizer) []InvokeResult {
	var out []InvokeResult
	for _, ep := range []uint16{2, 3} {
		if !authorize(ep, p.Cluster, p.Command).IsSuccess() {
			continue
		}
		at := p
		at.Endpoint, at.HasEndpoint = ep, true
		out = append(out, d.Invoke(ctx, at, f))
	}
	return out
}

// WriteAuthorized expands a wildcard-endpoint write to endpoints 2 and 3.
func (d *grantingDispatcher) WriteAuthorized(ctx context.Context, p ConcreteAttributePath, v AttributeValue, authorize WriteAuthorizer) []WriteResult {
	if p.HasEndpoint {
		return d.Write(ctx, p, v)
	}
	var out []WriteResult
	for _, ep := range []uint16{2, 3} {
		if authorize != nil && !authorize(ep, p.Cluster, p.Attribute).IsSuccess() {
			continue
		}
		at := p
		at.Endpoint, at.HasEndpoint = ep, true
		out = append(out, d.Write(ctx, at, v)...)
	}
	return out
}

func (d *grantingDispatcher) CheckACL(_ context.Context, fabric uint8, _ uint64, _ []uint32, _ uint16, _ uint32, _ uint8) StatusCode {
	d.checked = append(d.checked, fabric)
	return StatusSuccess
}

var (
	_ Dispatcher         = (*grantingDispatcher)(nil)
	_ AuthorizingInvoker = (*grantingDispatcher)(nil)
	_ AuthorizingWriter  = (*grantingDispatcher)(nil)
	_ ACLChecker         = (*grantingDispatcher)(nil)
)

// TestHasAccessingFabric pins the one predicate every gate of this package
// asks: a fabric index names a fabric from 1 to 254 (matter.js
// packages/model/src/standard/elements/fabric-index.element.ts:12-14 at
// db021da0, constraint "1 to 254"); 0 is a session without one and 255 the
// index of a session whose fabric the bridge could not name
// (endpoint.FabricIndexUnresolvable).
func TestHasAccessingFabric(t *testing.T) {
	t.Parallel()
	for index, want := range map[uint8]bool{0: false, 1: true, 7: true, 254: true, 255: false} {
		if got := hasAccessingFabric(index); got != want {
			t.Errorf("hasAccessingFabric(%d) = %v, want %v", index, got, want)
		}
	}
}

// TestGatesTreatAnUnresolvableFabricAsNoFabric holds every gate of the
// package to the same reading of "no accessing fabric": the index 0 and the
// unresolvable index 255 are refused alike, whatever the access-control
// source behind the gate would grant. Before, the write gate alone knew 255;
// the fabric-scoped invoke gate, the subject's access check and the group
// authorizer tested 0 only (review II, phase 0 second wave, noticed 1).
// matter.js asks `!this.session.fabric` at each of them
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:287-290 and
// :388, AttributeWriteResponse.ts:248-251 at db021da0); no reference models
// an index it cannot resolve, so reading it as "no fabric" is this module's
// fail-closed rule.
func TestGatesTreatAnUnresolvableFabricAsNoFabric(t *testing.T) {
	t.Parallel()
	const (
		groupKeyManagement uint32 = 0x003F
		keySetRead         uint32 = 0x01 // "F A": fabric-scoped
		groupKeyMap        uint32 = 0x0000
		onOff              uint32 = 0x0006
		toggle             uint32 = 0x02
		onTime             uint32 = 0x4001
	)
	for _, index := range []uint8{0, 255} {
		pase := WithAuthModePASE(WithFabricFilter(context.Background(), false, index))
		cas := WithSubject(WithFabricFilter(context.Background(), false, index), 0x77, nil)

		// The fabric-scoped invoke gate, concrete and wildcard, on the
		// commissioning channel (whose implicit Administer passes the
		// access check).
		d := &grantingDispatcher{}
		resp := HandleInvokeRequest(pase, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 0, Cluster: groupKeyManagement, Command: keySetRead, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusUnsupportedAccess || len(d.invoked) != 0 {
			t.Errorf("fabric %d, PASE: KeySetRead answered %+v and ran %v, want UnsupportedAccess and no dispatch", index, resp.Responses, d.invoked)
		}
		d = &grantingDispatcher{}
		HandleInvokeRequest(pase, d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(groupKeyManagement, keySetRead)}})
		if len(d.invoked) != 0 {
			t.Errorf("fabric %d, PASE: wildcard KeySetRead ran on %v, want no endpoint", index, d.invoked)
		}

		// The subject's access check, where the session is not PASE: the
		// access-control source is not asked at all.
		d = &grantingDispatcher{}
		resp = HandleInvokeRequest(cas, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 2, Cluster: onOff, Command: toggle, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusUnsupportedAccess || len(d.invoked) != 0 || len(d.checked) != 0 {
			t.Errorf("fabric %d, not PASE: Toggle answered %+v, ran %v, asked the ACL for %v; want UnsupportedAccess, no dispatch, no ACL lookup", index, resp.Responses, d.invoked, d.checked)
		}
		d = &grantingDispatcher{}
		HandleInvokeRequest(cas, d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(onOff, toggle)}})
		if len(d.invoked) != 0 || len(d.checked) != 0 {
			t.Errorf("fabric %d, not PASE: wildcard Toggle ran on %v, asked the ACL for %v", index, d.invoked, d.checked)
		}
		d = &grantingDispatcher{}
		wr := HandleWriteRequest(cas, d, WriteRequest{Writes: []AttributeWrite{{
			Path: ConcreteAttributePath{Endpoint: 2, Cluster: onOff, Attribute: onTime, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		}}})
		if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusUnsupportedAccess || len(d.written) != 0 || len(d.checked) != 0 {
			t.Errorf("fabric %d, not PASE: OnTime write answered %+v, stored %v, asked the ACL for %v", index, wr.Responses, d.written, d.checked)
		}
		d = &grantingDispatcher{}
		HandleWriteRequest(cas, d, WriteRequest{Writes: []AttributeWrite{{
			Path: ConcreteAttributePath{Cluster: onOff, Attribute: onTime, HasCluster: true, HasAttribute: true},
		}}})
		if len(d.written) != 0 || len(d.checked) != 0 {
			t.Errorf("fabric %d, not PASE: wildcard OnTime write stored %v, asked the ACL for %v", index, d.written, d.checked)
		}
		d = &grantingDispatcher{}
		if got := checkSubjectACL(cas, d, index, 0x77, nil, 2, onOff, 1); got != StatusUnsupportedAccess || len(d.checked) != 0 {
			t.Errorf("fabric %d: checkSubjectACL = %v after asking the ACL for %v, want UnsupportedAccess without a lookup", index, got, d.checked)
		}

		// The fabric-scoped write gate (the second wave's, unchanged).
		d = &grantingDispatcher{}
		wr = HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{
			Path: ConcreteAttributePath{Endpoint: 0, Cluster: groupKeyManagement, Attribute: groupKeyMap, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		}}})
		if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusUnsupportedAccess || len(d.written) != 0 {
			t.Errorf("fabric %d, PASE: GroupKeyMap write answered %+v and stored %v", index, wr.Responses, d.written)
		}

		// The group authorizer.
		d = &grantingDispatcher{}
		group := WithGroupSubject(WithFabricFilter(context.Background(), false, index), GroupSubject{GroupID: 1, HasValidMapping: true, Endpoints: []uint16{2, 3}})
		HandleGroupInvokeRequest(group, d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(onOff, toggle)}}, nil)
		if len(d.invoked) != 0 || len(d.checked) != 0 {
			t.Errorf("fabric %d: group Toggle ran on %v, asked the ACL for %v", index, d.invoked, d.checked)
		}
	}
}

// TestGatesPassAnAccessingFabric is the other side: with a fabric the same
// requests reach the access-control source and the dispatcher — a CASE
// subject's command and write, a fabric-scoped command and write over the
// commissioning channel after AddNOC, a group command — for the first and
// the last index a fabric can have.
func TestGatesPassAnAccessingFabric(t *testing.T) {
	t.Parallel()
	for _, index := range []uint8{1, 254} {
		pase := WithAuthModePASE(WithFabricFilter(context.Background(), false, index))
		cas := WithSubject(WithFabricFilter(context.Background(), false, index), 0x77, nil)

		d := &grantingDispatcher{}
		resp := HandleInvokeRequest(pase, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 0, Cluster: 0x003F, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusSuccess || len(d.invoked) != 1 {
			t.Errorf("fabric %d, PASE after AddNOC: KeySetRead answered %+v, ran %v", index, resp.Responses, d.invoked)
		}
		d = &grantingDispatcher{}
		resp = HandleInvokeRequest(cas, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 2, Cluster: 0x0006, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusSuccess || len(d.invoked) != 1 || len(d.checked) == 0 || d.checked[0] != index {
			t.Errorf("fabric %d, CASE: Toggle answered %+v, ran %v, asked the ACL for %v", index, resp.Responses, d.invoked, d.checked)
		}
		d = &grantingDispatcher{}
		wr := HandleWriteRequest(cas, d, WriteRequest{Writes: []AttributeWrite{{
			Path: ConcreteAttributePath{Endpoint: 2, Cluster: 0x0006, Attribute: 0x4001, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		}}})
		if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusSuccess || len(d.written) != 1 {
			t.Errorf("fabric %d, CASE: OnTime write answered %+v, stored %v", index, wr.Responses, d.written)
		}
		d = &grantingDispatcher{}
		wr = HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{
			Path: ConcreteAttributePath{Endpoint: 0, Cluster: 0x003F, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		}}})
		if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != StatusSuccess || len(d.written) != 1 {
			t.Errorf("fabric %d, PASE after AddNOC: GroupKeyMap write answered %+v, stored %v", index, wr.Responses, d.written)
		}
		d = &grantingDispatcher{}
		group := WithGroupSubject(WithFabricFilter(context.Background(), false, index), GroupSubject{GroupID: 1, HasValidMapping: true, Endpoints: []uint16{2, 3}})
		HandleGroupInvokeRequest(group, d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(0x0006, 0x02)}}, nil)
		if len(d.invoked) != 2 {
			t.Errorf("fabric %d: group Toggle ran on %v, want endpoints 2 and 3", index, d.invoked)
		}
	}
}
