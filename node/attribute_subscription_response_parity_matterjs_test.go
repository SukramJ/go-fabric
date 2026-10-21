// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/AttributeSubscriptionResponseTest.ts
// at 862ad75f: which attributes a subscription's data report carries when
// some attributes changed — the subscribed ones that changed, no sibling of
// them, nothing of a cluster without a change.
//
// matter.js hands AttributeSubscriptionResponse a dirty-attribute filter
// directly; the port subscribes over the wire from a commissioned fabrictest
// pair, changes attributes, and reads the report the change produces. The
// attributes matter.js marks dirty (VendorName, ProductName) are fixed on a
// node, so the port changes the writable ones of the same cluster —
// NodeLabel and Location, written over the wire — in their place, and for
// the five-attribute case five attributes of a Dimmable Light's
// LevelControl, set locally. Where matter.js expects no data, the port
// changes a probe — the light's OnOff, subscribed too — after the change
// under test and asserts that the report carries the probe alone.
package node_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func TestAttributeSubscriptionResponseParity(t *testing.T) {
	t.Parallel()

	nodeLabel := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	location := attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocation)
	wild := func(p im.ConcreteAttributePath) im.ConcreteAttributePath {
		p.HasEndpoint, p.Endpoint = false, 0
		return p
	}

	cases := []struct {
		name string
		// paths are subscribed beside the probe.
		paths []im.ConcreteAttributePath
		// change is the change under test.
		change func(t *testing.T, s *subscribed)
		// want are the paths the report carries besides the probe.
		want []im.ConcreteAttributePath
	}{
		{
			name:   "reads wildcard endpoint attribute when in filter",
			paths:  []im.ConcreteAttributePath{wild(nodeLabel)},
			change: func(t *testing.T, s *subscribed) { t.Helper(); s.write(t, nodeLabel, strVal("changed")) },
			want:   []im.ConcreteAttributePath{nodeLabel},
		},
		{
			name:   "reads no wildcard endpoint attribute when not in filter",
			paths:  []im.ConcreteAttributePath{wild(nodeLabel)},
			change: func(t *testing.T, s *subscribed) { t.Helper(); s.write(t, location, strVal("DE")) },
		},
		{
			name:   "reads concrete path when attribute is dirty",
			paths:  []im.ConcreteAttributePath{nodeLabel},
			change: func(t *testing.T, s *subscribed) { t.Helper(); s.write(t, nodeLabel, strVal("changed")) },
			want:   []im.ConcreteAttributePath{nodeLabel},
		},
		{
			name:   "ignores concrete path when only a sibling attribute of the same cluster is dirty",
			paths:  []im.ConcreteAttributePath{nodeLabel},
			change: func(t *testing.T, s *subscribed) { t.Helper(); s.write(t, location, strVal("DE")) },
		},
		{
			// The probe's cluster is the other cluster with a change.
			name:   "ignores concrete path when the cluster has no dirty attributes",
			paths:  []im.ConcreteAttributePath{nodeLabel},
			change: func(*testing.T, *subscribed) {},
		},
		{
			name:   "reads only the dirty attributes of a cluster with several concrete paths",
			paths:  []im.ConcreteAttributePath{location, nodeLabel},
			change: func(t *testing.T, s *subscribed) { t.Helper(); s.write(t, nodeLabel, strVal("changed")) },
			want:   []im.ConcreteAttributePath{nodeLabel},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := subscribeWithProbe(t, c.paths)
			c.change(t, s)
			s.flipProbe(t)
			s.assertReport(t, c.want)
		})
	}

	t.Run("reads wildcard endpoint & attributes with 5 in Filter", func(t *testing.T) {
		t.Parallel()
		s := subscribeWithProbe(t, []im.ConcreteAttributePath{{Cluster: levelcontrol.ClusterID, HasCluster: true}})
		five := map[uint32]any{
			levelcontrol.AttrCurrentLevel: uint64(50), levelcontrol.AttrRemainingTime: uint64(5), levelcontrol.AttrOptions: uint64(1),
			levelcontrol.AttrOnLevel: uint64(100), levelcontrol.AttrStartUpCurrentLevel: uint64(7),
		}
		if err := s.m.pair.Device.Act(func(*node.Node) error { return s.light.LevelControl().Underlying().(*spec.Server).SetAttributes(five) }); err != nil {
			t.Fatal(err)
		}
		s.flipProbe(t)
		want := make([]im.ConcreteAttributePath, 0, len(five))
		for id := range five {
			want = append(want, attrPath(s.ep, levelcontrol.ClusterID, id))
		}
		s.assertReport(t, want)
	})
}

// subscribed is a subscription to some paths and the probe — the light's
// OnOff — with the reports after the priming one.
type subscribed struct {
	m       *mockPair
	light   *device.DimmableLight
	ep      uint16
	reports chan *interaction.Report
}

func subscribeWithProbe(t *testing.T, paths []im.ConcreteAttributePath) *subscribed {
	t.Helper()
	site := quietSite(t)
	// The generated LevelControl server, whose five attributes the host
	// sets as matter.js's test sets the behavior's state — the kit's
	// self-held default derives RemainingTime from its transition.
	lvl, err := spec.NewServer(levelcontrol.Definition, spec.Options{
		Features:   uint32(levelcontrol.FeatureOnOff | levelcontrol.FeatureLighting),
		Attributes: []uint32{levelcontrol.AttrMinLevel, levelcontrol.AttrMaxLevel},
	}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("dimmer:1"), Name: "dimmer", Clock: site.Clock()}, device.WithServer(lvl))
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged:   []node.Endpoint{light},
		Configure: func(cfg *node.Config) { cfg.BasicInformation.VendorName = mockVendorName },
	}})
	ep, ok := p.Device.EndpointOf("dimmer:1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	s := &subscribed{m: &mockPair{site: site, pair: p}, light: light, ep: ep, reports: make(chan *interaction.Report, 16)}
	primed := false
	sub, err := fabrictest.Subscribe(parityCtx(t), p, interaction.SubscribeRequest{
		Attributes:       append(append([]im.ConcreteAttributePath(nil), paths...), attrPath(ep, onoff.ClusterID, onoff.AttrOnOff)),
		MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Hour),
		Updated: func(r *interaction.Report) {
			if !primed {
				primed = true
				return
			}
			s.reports <- r
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return s
}

// write writes an attribute of the node over the wire.
func (s *subscribed) write(t *testing.T, path im.ConcreteAttributePath, v spec.Encodable) {
	t.Helper()
	if err := fabrictest.Write(parityCtx(t), s.m.pair, path, v); err != nil {
		t.Fatal(err)
	}
}

// flipProbe changes the probe locally.
func (s *subscribed) flipProbe(t *testing.T) {
	t.Helper()
	if err := s.m.pair.Device.Act(func(*node.Node) error { _ = s.light.OnOff().Set(onoffdef.Patch{OnOff: new(true)}); return nil }); err != nil {
		t.Fatal(err)
	}
}

// assertReport waits for the report of the probe and asserts that it
// carries exactly the probe and want — every change made before the probe
// is in it, the subscription's floor having held them together.
func (s *subscribed) assertReport(t *testing.T, want []im.ConcreteAttributePath) {
	t.Helper()
	ctx := parityCtx(t)
	var got []interaction.AttributeData
	probe := attrPath(s.ep, onoff.ClusterID, onoff.AttrOnOff)
	err := s.m.site.Eventually(ctx, "the report of the probe", func() bool {
		select {
		case r := <-s.reports:
			got = append(got, r.Attributes...)
		default:
		}
		for i := range got {
			if samePath(got[i].Path, probe) {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want)+1 {
		t.Fatalf("report %+v, want the probe and %+v", got, want)
	}
	for _, w := range want {
		found := false
		for i := range got {
			found = found || samePath(got[i].Path, w)
		}
		if !found {
			t.Fatalf("report %+v lacks %d/%d/%d", got, w.Endpoint, w.Cluster, w.Attribute)
		}
	}
}
