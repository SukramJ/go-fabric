// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

// Behavioural parity for group-addressed interactions against matter.js
// HEAD: packages/node/src/node/server/InteractionServer.ts
// (handleInvokeRequest / handleWriteRequest on a group session) and
// packages/protocol/src/action/server/CommandInvokeResponse.ts /
// AttributeWriteResponse.ts (#processWildcard and the wildcard target
// checks). Each case names the rule it pins.

import (
	"context"
	"slices"
	"sync"
	"testing"
)

// groupFakeDispatcher hosts cluster 6 (commands 0..2) on endpoints 2, 3
// and 4, records what runs where, and grants access per (endpoint,
// privilege) through a table.
type groupFakeDispatcher struct {
	mu       sync.Mutex
	invoked  []uint16
	written  []uint16
	deny     map[uint16]bool // endpoint → ACL denies
	acl      []uint8         // privileges requested
	readOnly bool
}

func (d *groupFakeDispatcher) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }

func (d *groupFakeDispatcher) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	return []WriteResult{{Path: p}}
}

func (d *groupFakeDispatcher) Invoke(_ context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	return InvokeResult{Path: p}
}

func (d *groupFakeDispatcher) InvokeAuthorized(_ context.Context, p ConcreteCommandPath, _ any, authorize CommandAuthorizer) []InvokeResult {
	var out []InvokeResult
	for _, ep := range []uint16{0, 2, 3, 4} {
		if ep == 0 || p.Cluster != 6 || p.Command > 2 {
			continue
		}
		if !authorize(ep, p.Cluster, p.Command).IsSuccess() {
			continue
		}
		d.mu.Lock()
		d.invoked = append(d.invoked, ep)
		d.mu.Unlock()
		out = append(out, InvokeResult{Path: p})
	}
	return out
}

func (d *groupFakeDispatcher) WriteAuthorized(_ context.Context, p ConcreteAttributePath, _ AttributeValue, authorize WriteAuthorizer) []WriteResult {
	if d.readOnly {
		return nil
	}
	for _, ep := range []uint16{0, 2, 3, 4} {
		if ep == 0 || p.Cluster != 6 {
			continue
		}
		if !authorize(ep, p.Cluster, p.Attribute).IsSuccess() {
			continue
		}
		d.mu.Lock()
		d.written = append(d.written, ep)
		d.mu.Unlock()
	}
	return nil
}

func (d *groupFakeDispatcher) CheckACL(ctx context.Context, fabric uint8, node uint64, cats []uint32, endpoint uint16, _ uint32, privilege uint8) StatusCode {
	d.mu.Lock()
	d.acl = append(d.acl, privilege)
	d.mu.Unlock()
	if _, ok := GroupSubjectFromContext(ctx); !ok || node != 0 || cats != nil || fabric != 7 {
		return StatusUnsupportedAccess
	}
	if d.deny[endpoint] {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

func (d *groupFakeDispatcher) MinInvokePrivilege(_ uint16, _ uint32, cmd uint32) uint8 {
	if cmd == 1 {
		return 4
	}
	return 3
}

func (d *groupFakeDispatcher) MinWritePrivilege(uint16, uint32, uint32) uint8 { return 4 }

func groupCtx(endpoints ...uint16) context.Context {
	ctx := WithFabricFilter(context.Background(), false, 7)
	return WithGroupSubject(ctx, GroupSubject{GroupID: 0x0101, HasValidMapping: true, Endpoints: endpoints})
}

func wildcardInvoke(cluster, cmd uint32) CommandInvocation {
	return CommandInvocation{Path: ConcreteCommandPath{Cluster: cluster, Command: cmd, HasCluster: true, HasCommand: true}}
}

func TestGroupInvokeParityMatterJS(t *testing.T) {
	t.Parallel()
	toggle := wildcardInvoke(6, 2)
	concrete := toggle
	concrete.Path.Endpoint, concrete.Path.HasEndpoint = 2, true
	noCommand := toggle
	noCommand.Path.HasCommand = false

	cases := []struct {
		name    string
		req     InvokeRequest
		members []uint16
		timed   func(uint32, uint32) bool
		deny    map[uint16]bool
		want    StatusCode
		ran     []uint16
	}{
		{"handleInvokeRequest: a TimedRequest flag mismatches the absent timed interaction", InvokeRequest{TimedRequest: true, Invokes: []CommandInvocation{toggle}}, []uint16{2}, nil, nil, StatusTimedRequestMismatch, nil},
		{"process: group commands cannot be concrete paths", InvokeRequest{Invokes: []CommandInvocation{concrete}}, []uint16{2}, nil, nil, StatusInvalidAction, nil},
		{"#processWildcard: clusterId and commandId are required", InvokeRequest{Invokes: []CommandInvocation{noCommand}}, []uint16{2}, nil, nil, StatusInvalidAction, nil},
		{"process: a wildcard path must not share the message", InvokeRequest{Invokes: []CommandInvocation{toggle, toggle}}, []uint16{2}, nil, nil, StatusInvalidAction, nil},
		{"#processWildcard: no member endpoints, nothing runs", InvokeRequest{Invokes: []CommandInvocation{toggle}}, nil, nil, nil, StatusSuccess, nil},
		{"#wildcardEndpoints: only member endpoints", InvokeRequest{Invokes: []CommandInvocation{toggle}}, []uint16{2, 4}, nil, nil, StatusSuccess, []uint16{2, 4}},
		{"#wildcardTargetOf: a denied endpoint is skipped", InvokeRequest{Invokes: []CommandInvocation{toggle}}, []uint16{2, 3, 4}, nil, map[uint16]bool{3: true}, StatusSuccess, []uint16{2, 4}},
		{"#wildcardTargetOf: a timed-required command is skipped", InvokeRequest{Invokes: []CommandInvocation{toggle}}, []uint16{2, 3}, func(uint32, uint32) bool { return true }, nil, StatusSuccess, nil},
		{"#invokeCommand: fields that failed decoding run nowhere", InvokeRequest{Invokes: []CommandInvocation{{Path: toggle.Path, DecodeStatus: StatusConstraintError}}}, []uint16{2}, nil, nil, StatusConstraintError, nil},
	}
	for _, tc := range cases {
		d := &groupFakeDispatcher{deny: tc.deny}
		if got := HandleGroupInvokeRequest(groupCtx(tc.members...), d, tc.req, tc.timed); got != tc.want {
			t.Errorf("%s: status %v, want %v", tc.name, got, tc.want)
		}
		if !slices.Equal(d.invoked, tc.ran) {
			t.Errorf("%s: ran on %v, want %v", tc.name, d.invoked, tc.ran)
		}
	}

	// The command's own privilege is what the Group subject is checked
	// for (#wildcardTargetOf: limits.writeLevel).
	d := &groupFakeDispatcher{}
	HandleGroupInvokeRequest(groupCtx(2), d, InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(6, 1)}}, nil)
	if !slices.Equal(d.acl, []uint8{4}) {
		t.Errorf("privileges checked = %v, want the command's Manage (4)", d.acl)
	}

	// No Group subject in the context, or a dispatcher that cannot
	// authorize a wildcard command: nothing runs.
	d = &groupFakeDispatcher{}
	HandleGroupInvokeRequest(WithFabricFilter(context.Background(), false, 7), d, InvokeRequest{Invokes: []CommandInvocation{toggle}}, nil)
	if len(d.invoked) != 0 {
		t.Error("a group invoke without a Group subject ran")
	}
}

// plainDispatcher has no ACL checker and no authorizing paths.
type plainDispatcher struct{}

func (plainDispatcher) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }
func (plainDispatcher) Write(context.Context, ConcreteAttributePath, AttributeValue) []WriteResult {
	return nil
}

func (plainDispatcher) Invoke(_ context.Context, p ConcreteCommandPath, _ any) InvokeResult {
	return InvokeResult{Path: p}
}

func TestGroupInteractionsFailClosed(t *testing.T) {
	t.Parallel()
	toggle := InvokeRequest{Invokes: []CommandInvocation{wildcardInvoke(6, 2)}}
	if got := HandleGroupInvokeRequest(groupCtx(2), plainDispatcher{}, toggle, nil); got != StatusSuccess {
		t.Errorf("dispatcher without wildcard invoke: %v", got)
	}
	// Without an ACL checker every location is denied.
	type noACL struct {
		AuthorizingInvoker
		AuthorizingWriter
		Dispatcher
	}
	inner := &groupFakeDispatcher{}
	d := noACL{AuthorizingInvoker: inner, AuthorizingWriter: inner, Dispatcher: plainDispatcher{}}
	HandleGroupInvokeRequest(groupCtx(2, 3), d, toggle, nil)
	if len(inner.invoked) != 0 {
		t.Errorf("ran on %v without any ACL checker", inner.invoked)
	}
	HandleGroupWriteRequest(groupCtx(2, 3), d, WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{{Path: ConcreteAttributePath{Cluster: 6, Attribute: 0x4001, HasCluster: true, HasAttribute: true}}}})
	if len(inner.written) != 0 {
		t.Errorf("wrote on %v without any ACL checker", inner.written)
	}
	// A Group subject without a fabric is denied as well.
	d2 := &groupFakeDispatcher{}
	HandleGroupInvokeRequest(WithGroupSubject(context.Background(), GroupSubject{GroupID: 1, HasValidMapping: true, Endpoints: []uint16{2}}), d2, toggle, nil)
	if len(d2.invoked) != 0 {
		t.Errorf("ran on %v without a fabric", d2.invoked)
	}
}

func TestGroupWriteParityMatterJS(t *testing.T) {
	t.Parallel()
	attr := AttributeWrite{Path: ConcreteAttributePath{Cluster: 6, Attribute: 0x4001, HasCluster: true, HasAttribute: true}}
	concrete := attr
	concrete.Path.Endpoint, concrete.Path.HasEndpoint = 2, true
	noAttr := attr
	noAttr.Path.HasAttribute = false

	cases := []struct {
		name    string
		req     WriteRequest
		members []uint16
		deny    map[uint16]bool
		want    StatusCode
		wrote   []uint16
	}{
		{"handleWriteRequest: MoreChunkedMessages with SuppressResponse", WriteRequest{SuppressResponse: true, MoreChunkedMessages: true, Writes: []AttributeWrite{attr}}, []uint16{2}, nil, StatusInvalidAction, nil},
		{"handleWriteRequest: a TimedRequest flag mismatches", WriteRequest{SuppressResponse: true, TimedRequest: true, Writes: []AttributeWrite{attr}}, []uint16{2}, nil, StatusTimedRequestMismatch, nil},
		{"handleWriteRequest: a group write needs SuppressResponse", WriteRequest{Writes: []AttributeWrite{attr}}, []uint16{2}, nil, StatusInvalidAction, nil},
		{"process: group writes can not be concrete paths", WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{concrete}}, []uint16{2}, nil, StatusInvalidAction, nil},
		{"process: writes before a concrete path have run", WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{attr, concrete}}, []uint16{2}, nil, StatusInvalidAction, []uint16{2}},
		{"#writeEndpointForWildcard: clusterId and attributeId are required", WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{noAttr}}, []uint16{2}, nil, StatusInvalidAction, nil},
		{"#processWildcard: no member endpoints, nothing written", WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{attr}}, nil, nil, StatusSuccess, nil},
		{"#writeAttributeForWildcard: members only, denied skipped", WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{attr}}, []uint16{2, 3, 4}, map[uint16]bool{4: true}, StatusSuccess, []uint16{2, 3}},
	}
	for _, tc := range cases {
		d := &groupFakeDispatcher{deny: tc.deny}
		if got := HandleGroupWriteRequest(groupCtx(tc.members...), d, tc.req); got != tc.want {
			t.Errorf("%s: status %v, want %v", tc.name, got, tc.want)
		}
		if !slices.Equal(d.written, tc.wrote) {
			t.Errorf("%s: wrote %v, want %v", tc.name, d.written, tc.wrote)
		}
		for _, p := range d.acl {
			if p != 4 {
				t.Errorf("%s: write checked privilege %d, want the attribute's (4)", tc.name, p)
			}
		}
	}
	if got := HandleGroupWriteRequest(WithFabricFilter(context.Background(), true, 7), &groupFakeDispatcher{}, WriteRequest{SuppressResponse: true, Writes: []AttributeWrite{attr}}); got != StatusSuccess {
		t.Errorf("write without a Group subject: %v", got)
	}
}
