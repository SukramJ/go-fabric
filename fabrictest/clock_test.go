// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestDeviceTimersOnTheSiteClock: a device built by the device layer on
// the site's clock, the bridged endpoint's Identify and the Aggregator's
// Identify all count down on the site's fake clock — stepped by the test,
// not by the wall clock: OnWithTimedOff's OnTime and both IdentifyTime
// writes end exactly when the test advances the fake past them.
func TestDeviceTimersOnTheSiteClock(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, ok := p.Device.EndpointOf("light:1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	ctx := testContext(t)
	fake := site.Fake()

	// OnWithTimedOff for 3 s (OnTime in tenths): on now, off once the
	// fake clock passes the OnTime.
	if _, err := fabrictest.Invoke(ctx, p, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdOnWithTimedOff},
		onoffdef.OnWithTimedOffRequest{OnTime: 30}); err != nil {
		t.Fatalf("OnWithTimedOff: %v", err)
	}
	if v, _ := light.OnOff().Underlying().MatterRead(onoff.AttrOnOff); v != true {
		t.Fatalf("OnOff %v after OnWithTimedOff", v)
	}
	fake.Advance(3500 * time.Millisecond)
	if v, _ := light.OnOff().Underlying().MatterRead(onoff.AttrOnOff); v != false {
		t.Fatalf("OnOff %v after the fake clock passed the OnTime", v)
	}

	// IdentifyTime on the bridged endpoint and on the Aggregator.
	for _, target := range []uint16{ep, 1} {
		path := im.ConcreteAttributePath{Endpoint: target, Cluster: identifydef.ClusterID, Attribute: identifydef.AttrIdentifyTime, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		if err := fabrictest.Write(ctx, p, path, uint16Value(5)); err != nil {
			t.Fatalf("IdentifyTime on endpoint %d: %v", target, err)
		}
		if d, err := fabrictest.Read(ctx, p, path); err != nil || d.Value.El.Uint == 0 {
			t.Fatalf("IdentifyTime on endpoint %d right after the write: %+v, %v", target, d, err)
		}
		// The countdown ticks once a second on a ticker; a tick its
		// goroutine has not taken yet is dropped, as a slow receiver
		// misses a real ticker's, so the test steps a second at a time.
		for range 6 {
			fake.Advance(time.Second)
			fake.Yield()
		}
		if d, err := fabrictest.Read(ctx, p, path); err != nil || d.Value.El.Uint != 0 {
			t.Fatalf("IdentifyTime on endpoint %d after the fake clock passed it: %+v, %v", target, d, err)
		}
	}
}

func uint16Value(v uint16) spec.Encodable {
	return rawUint(uint64(v))
}

// rawUint is an unsigned integer as a write's value.
type rawUint uint64

func (r rawUint) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, uint64(r)) }
