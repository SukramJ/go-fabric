// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// Per-attribute timed writes, after matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts at 862ad75f:
// a concrete write to an attribute with the timed access quality outside a
// timed interaction answers NeedsTimedInteraction after the writability
// verdict and the access check (:229-247); a wildcard write skips the
// attribute silently (:343-345); inside a timed interaction it is applied.
// chip WriteHandler.cpp:798-802 at 6170af84 answers the same. No attribute
// of the snapshot at the pin carries the quality (schema/timed_gen.go
// timedWritePaths is empty), so the test marks OnOff.OnTime (0x4001,
// writable) timed through the dispatcher's timedWrite field.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

func TestTimedWriteParity(t *testing.T) {
	t.Parallel()
	const onOff, onTime = uint32(0x0006), uint32(0x4001)
	timedOnTime := func(c, a uint32) bool { return c == onOff && a == onTime }
	wildcardEndpoint := im.ConcreteAttributePath{Cluster: onOff, Attribute: onTime, HasCluster: true, HasAttribute: true}
	wildcardAttribute := im.ConcreteAttributePath{Endpoint: 2, Cluster: onOff, HasEndpoint: true, HasCluster: true}
	deny := func(uint16, uint32, uint32) im.StatusCode { return im.StatusUnsupportedAccess }
	for _, c := range []struct {
		name      string
		ctx       context.Context
		path      im.ConcreteAttributePath
		authorize im.WriteAuthorizer
		want      []im.StatusCode // one per result; nil is no result
		applied   bool
	}{
		{"concrete, untimed: NeedsTimedInteraction", im.WithTimedInteraction(context.Background(), false), concreteAttrPath(2, onOff, onTime), nil, []im.StatusCode{im.StatusNeedsTimedInteraction}, false},
		{"concrete, timed: applied", im.WithTimedInteraction(context.Background(), true), concreteAttrPath(2, onOff, onTime), nil, []im.StatusCode{im.StatusSuccess}, true},
		{"concrete, untimed, access denied: the access status first", im.WithTimedInteraction(context.Background(), false), concreteAttrPath(2, onOff, onTime), deny, []im.StatusCode{im.StatusUnsupportedAccess}, false},
		{"wildcard endpoint, untimed: skipped", im.WithTimedInteraction(context.Background(), false), wildcardEndpoint, nil, nil, false},
		{"wildcard attribute, untimed: skipped", im.WithTimedInteraction(context.Background(), false), wildcardAttribute, nil, nil, false},
		{"wildcard attribute, timed: applied", im.WithTimedInteraction(context.Background(), true), wildcardAttribute, nil, []im.StatusCode{im.StatusSuccess}, true},
		{"no stamp: the caller's check, applied", context.Background(), concreteAttrPath(2, onOff, onTime), nil, []im.StatusCode{im.StatusSuccess}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := &recordingServer{id: onOff, attrs: []uint32{onTime}}
			d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: recordingSource{srv: srv}}))
			d.timedWrite = &timedWriteTable{isTimed: timedOnTime}
			results := d.WriteAuthorized(c.ctx, c.path, im.AttributeValue{Value: uint16(5)}, c.authorize)
			if len(results) != len(c.want) {
				t.Fatalf("results %+v, want statuses %v", results, c.want)
			}
			for i, r := range results {
				if r.Status != c.want[i] {
					t.Errorf("result %d: status %v, want %v", i, r.Status, c.want[i])
				}
			}
			if got := recordedWrite(srv.writeCalls, onTime); got != c.applied {
				t.Errorf("applied %t, want %t (calls %v)", got, c.applied, srv.writeCalls)
			}
		})
	}
	// Without the injection the pin's table is in force: OnTime is not timed.
	srv := &recordingServer{id: onOff, attrs: []uint32{onTime}}
	d := NewTopologyDispatcher(makeTopology(&Endpoint{ID: 2, Source: recordingSource{srv: srv}}))
	if r := d.Write(im.WithTimedInteraction(context.Background(), false), concreteAttrPath(2, onOff, onTime), im.AttributeValue{Value: uint16(5)}); len(r) != 1 || r[0].Status != im.StatusSuccess {
		t.Fatalf("untimed write with the schema's table: %+v, want applied", r)
	}
}
