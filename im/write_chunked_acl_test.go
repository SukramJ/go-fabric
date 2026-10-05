// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// lockoutACL is a dispatcher whose ACL grants Administer until the ACL
// attribute (0, 0x001F, 0) is written with an empty list — the REPLACE-ALL
// that opens a chunked ACL write — and nothing after it.
type lockoutACL struct {
	emptied bool
	writes  int
}

func (d *lockoutACL) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }

func (d *lockoutACL) Write(_ context.Context, p ConcreteAttributePath, v AttributeValue) []WriteResult {
	d.writes++
	if l, ok := v.Value.([]int); ok && len(l) == 0 && !p.ListAppend {
		d.emptied = true
	}
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

func (d *lockoutACL) Invoke(context.Context, ConcreteCommandPath, any) InvokeResult {
	return InvokeResult{}
}

func (d *lockoutACL) CheckACL(context.Context, uint8, uint64, []uint32, uint16, uint32, uint8) StatusCode {
	if d.emptied {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

// TestChunkedWriteIsAuthorizedOnce pins chip WriteHandler::CheckWriteAccess
// ("only validate ACL if path has changed"): the appends that follow a
// successful REPLACE-ALL of the same attribute in one WriteRequest are not
// re-authorized, so an administrator rewriting the ACL in chunks keeps the
// write its own empty replace would otherwise revoke (TC-ACL-2.6, 2.8). A
// different attribute in between, or a later request, is authorized again.
func TestChunkedWriteIsAuthorizedOnce(t *testing.T) {
	t.Parallel()
	acl := ConcreteAttributePath{Endpoint: 0, Cluster: 0x001F, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	appendPath := acl
	appendPath.ListAppend = true
	other := ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	ctx := WithSubject(WithFabricFilter(context.Background(), false, 1), 0x77, nil)

	d := &lockoutACL{}
	resp := HandleWriteRequest(ctx, d, WriteRequest{Writes: []AttributeWrite{
		{Path: acl, Value: AttributeValue{Value: []int{}}},
		{Path: appendPath, Value: AttributeValue{Value: 1}},
		{Path: appendPath, Value: AttributeValue{Value: 2}},
		{Path: other, Value: AttributeValue{Value: "x"}},
		{Path: appendPath, Value: AttributeValue{Value: 3}},
	}})
	want := []StatusCode{StatusSuccess, StatusSuccess, StatusSuccess, StatusUnsupportedAccess, StatusUnsupportedAccess}
	if len(resp.Responses) != len(want) {
		t.Fatalf("%d responses, want %d", len(resp.Responses), len(want))
	}
	for i, w := range want {
		if got := resp.Responses[i].Status.Status; got != w {
			t.Errorf("element %d: %v, want %v", i, got, w)
		}
	}
	if d.writes != 3 {
		t.Errorf("%d writes reached the dispatcher, want the replace and its two appends", d.writes)
	}
	next := HandleWriteRequest(ctx, d, WriteRequest{Writes: []AttributeWrite{{Path: appendPath, Value: AttributeValue{Value: 4}}}})
	if got := next.Responses[0].Status.Status; got != StatusUnsupportedAccess {
		t.Errorf("a later request's append: %v, want UnsupportedAccess (authorized afresh)", got)
	}
}

// TestListAppendWriteMark pins the list-append write mark.
func TestListAppendWriteMark(t *testing.T) {
	t.Parallel()
	if IsListAppendWrite(context.Background()) {
		t.Fatal("a plain context is marked")
	}
	if !IsListAppendWrite(WithListAppendWrite(context.Background())) {
		t.Fatal("the mark is lost")
	}
}
