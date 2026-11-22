// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/descriptor"
	icddef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	"thingfabric.dev/go-fabric/icd"
	"thingfabric.dev/go-fabric/tlv"
)

// TestICDTimingsBeyondTheirTypeAreAbsent (review II, R2-P6-bound-2352): a
// peer's IdleModeDuration, ActiveModeDuration or ActiveModeThreshold wider
// than the attribute's type is absent — the schedule's default stands —
// instead of overflowing the time.Duration it is multiplied into. Before,
// an IdleModeDuration of 2^34 s gave −351 909 h, a subscription ceiling of
// 0 and a negative idle window.
func TestICDTimingsBeyondTheirTypeAreAbsent(t *testing.T) {
	t.Parallel()
	n, _ := quotaNode(t, &MemoryStore{}, 0)
	b := &report{}
	b.cluster(0, descriptor.ClusterID, 0, map[uint32]spec.Node{
		descriptor.AttrServerList:     listNode(uint64(descriptor.ClusterID), uint64(icddef.ClusterID)),
		descriptor.AttrPartsList:      listNode(),
		descriptor.AttrDeviceTypeList: deviceTypes(dtRootNode),
	})
	wide := func(v uint64) spec.Node { return spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: v}} }
	b.cluster(0, icddef.ClusterID, 0, map[uint32]spec.Node{
		icddef.AttrIdleModeDuration:    wide(1 << 34),
		icddef.AttrActiveModeDuration:  wide(1 << 34),
		icddef.AttrActiveModeThreshold: wide(1 << 17),
	})
	n.apply(wildcard, &b.r, true)
	if p := n.Properties(); p.IdleModeDuration != nil {
		t.Fatalf("IdleModeDuration %v from a value beyond uint32, want absent", *p.IdleModeDuration)
	}
	_, ceiling := intervalBounds(n.Properties(), nil, nil, func() float64 { return 0 }, slog.New(slog.DiscardHandler))
	if ceiling <= 0 {
		t.Fatalf("subscription ceiling %v, want positive", ceiling)
	}
	if got := n.icdImage().timings; got != icd.DefaultTimings {
		t.Fatalf("ICD timings %+v, want the defaults %+v", got, icd.DefaultTimings)
	}
}
