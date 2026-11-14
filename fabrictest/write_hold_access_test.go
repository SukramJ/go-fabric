// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acl "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeFunc is a value encoded by a function.
type encodeFunc func(enc *tlv.Encoder, tag tlv.Tag)

func (f encodeFunc) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { f(enc, tag) }

// TestRefusedChunkedWriteHoldsNothing: a chunked write the subject is not
// allowed to make holds no attribute, so it leaves every other writer —
// another fabric's administrator and its own fabric's — free to write the
// attribute (review II, owner decision 52; by_design
// BD-chip-ChunkedWritesBounded). The refused subject holds View, or no ACL
// entry at all.
func TestRefusedChunkedWriteHoldsNothing(t *testing.T) {
	t.Parallel()
	t.Run("a View subject", func(t *testing.T) {
		t.Parallel()
		refusedChunkedWriteHoldsNothing(t, func(ctx context.Context, a *fabrictest.Pair) error { return a.Grant(ctx, store.PrivilegeView) })
	})
	t.Run("a node without any ACL entry", func(t *testing.T) {
		t.Parallel()
		refusedChunkedWriteHoldsNothing(t, func(ctx context.Context, a *fabrictest.Pair) error {
			fi := pairFabricIndex(t, a)
			return a.Device.Node().Store().ReplaceACL(ctx, fi, []store.ACLEntry{{
				FabricIndex: fi, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0xA0AD},
			}})
		})
	})
}

func refusedChunkedWriteHoldsNothing(t *testing.T, demote func(ctx context.Context, a *fabrictest.Pair) error) {
	t.Helper()
	_, a, b := twoFabrics(t, fabrictest.Topology{})
	ctx := testContext(t)
	if err := demote(ctx, a); err != nil {
		t.Fatal(err)
	}
	aclPath := rootAttr(acl.ClusterID, acl.AttrAcl)
	labelPath := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)

	emptyList := encodeFunc(func(enc *tlv.Encoder, tag tlv.Tag) { enc.StartArray(tag); _ = enc.EndContainer() })
	for _, p := range []im.ConcreteAttributePath{aclPath, labelPath} {
		enc := tlv.NewEncoder()
		im.WriteRequest{
			MoreChunkedMessages: true,
			Writes:              []im.AttributeWrite{{Path: p, Value: im.AttributeValue{Value: emptyList}}},
		}.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { v.Value.(encodeFunc).EncodeTLV(e, tag) })
		payload, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		m, err := fabrictestseam.Exchange(ctx, a, im.OpcodeWriteRequest, payload)
		if err != nil {
			t.Fatalf("chunk of %#x/%#x: %v", p.Cluster, p.Attribute, err)
		}
		if m.Opcode != im.OpcodeWriteResponse {
			t.Fatalf("the subject's chunk was answered with opcode %#x", m.Opcode)
		}
		resp, err := im.UnmarshalWriteResponseTLV(tlv.NewDecoder(m.Payload))
		if err != nil || len(resp.Responses) != 1 {
			t.Fatalf("WriteResponse %+v, %v", resp, err)
		}
		if got := resp.Responses[0].Status.Status; got != im.StatusUnsupportedAccess {
			t.Fatalf("the subject's write of %#x/%#x was answered %v, want UNSUPPORTED_ACCESS", p.Cluster, p.Attribute, got)
		}
	}

	// Fabric B's administrator rewrites its own ACL entry and the node label.
	bIndex := pairFabricIndex(t, b)
	own := encodeFunc(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		acl.AccessControlEntryStruct{
			Privilege:   acl.AccessControlEntryPrivilegeEnum(store.PrivilegeAdminister),
			AuthMode:    acl.AccessControlEntryAuthModeEnum(store.AuthModeCASE),
			Subjects:    spec.Nullable[[]uint64]{Value: []uint64{0xB001}},
			Targets:     spec.Nullable[[]acl.AccessControlTargetStruct]{Null: true},
			FabricIndex: bIndex,
		}.EncodeTLV(enc, tlv.AnonymousTag())
		_ = enc.EndContainer()
	})
	var se *interaction.StatusError
	for _, w := range []struct {
		what  string
		path  im.ConcreteAttributePath
		value spec.Encodable
	}{
		{"its own ACL", aclPath, own},
		{"NodeLabel", labelPath, stringValue("b's label")},
	} {
		if err := fabrictest.Write(ctx, b, w.path, w.value); err != nil {
			if errors.As(err, &se) {
				t.Errorf("fabric B's administrator writing %s after fabric A's refused chunked write: status %v, want SUCCESS", w.what, se.Status)
			} else {
				t.Errorf("fabric B's write of %s: %v", w.what, err)
			}
		}
	}
}
