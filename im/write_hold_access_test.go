// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// holdACLCluster and holdACLAttribute name AccessControl.ACL, an attribute
// whose write access is fabric-scoped (schema.IsFabricScopedWrite).
const (
	holdACLCluster   uint32 = 0x001F
	holdACLAttribute uint32 = 0
	holdRefused      uint64 = 0xBAD
)

// aclLabelDispatcher is a labelDispatcher whose ACL refuses one subject
// every privilege and grants every other subject everything.
type aclLabelDispatcher struct {
	labelDispatcher
}

func (d *aclLabelDispatcher) CheckACL(_ context.Context, _ uint8, subject uint64, _ []uint32, _ uint16, _ uint32, _ uint8) StatusCode {
	if subject == holdRefused {
		return StatusUnsupportedAccess
	}
	return StatusSuccess
}

// fabricExchange is one exchange's write interaction on a session of a
// fabric with a subject.
type fabricExchange struct {
	d       *aclLabelDispatcher
	tx      *WriteTransaction
	fabric  uint8
	subject uint64
}

func (x fabricExchange) write(more bool, writes ...holdWrite) []StatusCode {
	req := WriteRequest{MoreChunkedMessages: more}
	for _, w := range writes {
		req.Writes = append(req.Writes, AttributeWrite{Path: w.path, Value: AttributeValue{Value: w.value}})
	}
	ctx := WithSubject(WithFabricFilter(context.Background(), false, x.fabric), x.subject, nil)
	resp := HandleWriteRequest(WithWriteTransaction(ctx, x.tx), x.d, req)
	out := make([]StatusCode, 0, len(resp.Responses))
	for _, r := range resp.Responses {
		out = append(out, r.Status.Status)
	}
	return out
}

func newFabricHoldNode() (d *aclLabelDispatcher, exchange func(fabric uint8, subject uint64) fabricExchange) {
	d = &aclLabelDispatcher{labelDispatcher{values: map[uint32]string{}}}
	holds := &WriteHolds{}
	return d, func(fabric uint8, subject uint64) fabricExchange {
		return fabricExchange{d: d, tx: NewWriteTransaction(holds), fabric: fabric, subject: subject}
	}
}

// TestChunkedWriteHoldsOnlyWhatTheSubjectMayWrite: a chunked write holds an
// attribute only once the subject's access to write it is granted, and an
// attribute whose write is fabric-scoped is held for the accessing fabric
// alone (review II, owner decision 52; notes/parity/by_design.md
// BD-chip-ChunkedWritesBounded). Any other attribute is held node-wide, as
// matter.js keys it (InteractionServer.ts:86-88 writePathKey at db021da0).
func TestChunkedWriteHoldsOnlyWhatTheSubjectMayWrite(t *testing.T) {
	t.Parallel()
	nodeLabel := holdAttr(holdNodeLabel)
	aclAttr := ConcreteAttributePath{Endpoint: 0, Cluster: holdACLCluster, Attribute: holdACLAttribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}

	t.Run("a write the subject is refused holds nothing", func(t *testing.T) {
		t.Parallel()
		_, exchange := newFabricHoldNode()
		refused := exchange(1, holdRefused)
		for _, p := range []ConcreteAttributePath{nodeLabel, aclAttr} {
			wantStatuses(t, "the refused subject's chunk", refused.write(true, holdWrite{p, "x"}), StatusUnsupportedAccess)
			wantStatuses(t, "another fabric's write after it", exchange(2, 0xB001).write(false, holdWrite{p, "y"}), StatusSuccess)
			wantStatuses(t, "the same fabric's write after it", exchange(1, 0xA0AD).write(false, holdWrite{p, "z"}), StatusSuccess)
		}
	})

	t.Run("a fabric-scoped attribute is held for its fabric only", func(t *testing.T) {
		t.Parallel()
		d, exchange := newFabricHoldNode()
		holder := exchange(1, 0xA0AD)
		wantStatuses(t, "fabric 1's first chunk", holder.write(true, holdWrite{aclAttr, "a1"}), StatusSuccess)
		wantStatuses(t, "fabric 2's write", exchange(2, 0xB001).write(false, holdWrite{aclAttr, "b"}), StatusSuccess)
		wild := aclAttr
		wild.HasEndpoint = false
		wantStatuses(t, "fabric 2's wildcard write", exchange(2, 0xB001).write(false, holdWrite{wild, "bw"}), StatusSuccess)
		if d.values[holdACLAttribute] != "bw" {
			t.Fatalf("ACL after fabric 2's wildcard write %q, want bw", d.values[holdACLAttribute])
		}
		wantStatuses(t, "another exchange of fabric 1", exchange(1, 0xA0AE).write(false, holdWrite{aclAttr, "a2"}), StatusBusy)
		if got := exchange(1, 0xA0AE).write(false, holdWrite{wild, "aw"}); len(got) != 0 {
			t.Fatalf("fabric 1's wildcard write while its fabric holds the attribute answered %v, want no status", got)
		}
		wantStatuses(t, "fabric 1's last chunk", holder.write(false, holdWrite{aclAttr, "a3"}), StatusSuccess)
		wantStatuses(t, "fabric 1 after the last chunk", exchange(1, 0xA0AE).write(false, holdWrite{aclAttr, "a4"}), StatusSuccess)
	})

	t.Run("an attribute that is not fabric-scoped is held for every fabric", func(t *testing.T) {
		t.Parallel()
		_, exchange := newFabricHoldNode()
		holder := exchange(1, 0xA0AD)
		wantStatuses(t, "fabric 1's first chunk", holder.write(true, holdWrite{nodeLabel, "a1"}), StatusSuccess)
		wantStatuses(t, "fabric 2's write", exchange(2, 0xB001).write(false, holdWrite{nodeLabel, "b"}), StatusBusy)
		holder.write(false, holdWrite{nodeLabel, "a2"})
	})
}
