// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestIdentifyReachesTheHost: a bridged light's declared HandleIdentify
// gets the controller's Identify, and the assembler's server then counts
// IdentifyTime; a handler installed at run time through Device.Identify
// gets TriggerEffect; a handler's StatusError refuses the command and
// leaves IdentifyTime alone. A declaration with OnWrite is refused.
func TestIdentifyReachesTheHost(t *testing.T) {
	t.Parallel()
	if _, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("x")},
		identifydef.Declare{OnWrite: func(context.Context, identifydef.State, uint32) error { return nil }}); !errors.Is(err, device.ErrConfiguration) {
		t.Fatalf("an Identify OnWrite: %v, want ErrConfiguration", err)
	}
	var mu sync.Mutex
	var times []uint16
	var effects [][2]uint8
	refuse := false
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"},
		identifydef.Declare{HandleIdentify: func(_ context.Context, req identifydef.IdentifyRequest) (struct{}, error) {
			mu.Lock()
			defer mu.Unlock()
			if refuse {
				return struct{}{}, contract.StatusError{Status: uint8(im.StatusBusy)}
			}
			times = append(times, req.IdentifyTime)
			return struct{}{}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := light.Identify().HandleTriggerEffect(func(_ context.Context, req identifydef.TriggerEffectRequest) (struct{}, error) {
		mu.Lock()
		defer mu.Unlock()
		effects = append(effects, [2]uint8{uint8(req.EffectIdentifier), uint8(req.EffectVariant)})
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, _ := light.EndpointNumber()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	identifyTime := func() uint16 {
		v, err := fabrictest.Read(ctx, pair, client.AttributeAt(ep, identifydef.ClusterID, identifydef.AttrIdentifyTime))
		if err != nil {
			t.Fatal(err)
		}
		return uint16(v.Value.El.Uint) //nolint:gosec // a uint16 attribute
	}
	if _, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, identifydef.ClusterID, identifydef.CmdIdentify), identifydef.IdentifyRequest{IdentifyTime: 30}); err != nil {
		t.Fatal(err)
	}
	if got := identifyTime(); got == 0 || got > 30 {
		t.Fatalf("IdentifyTime %d after Identify(30)", got)
	}
	if _, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, identifydef.ClusterID, identifydef.CmdTriggerEffect),
		identifydef.TriggerEffectRequest{EffectIdentifier: identifydef.EffectIdentifierBlink, EffectVariant: identifydef.EffectVariantDefault}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	refuse = true
	mu.Unlock()
	_, err = fabrictest.Invoke(ctx, pair, client.CommandAt(ep, identifydef.ClusterID, identifydef.CmdIdentify), identifydef.IdentifyRequest{IdentifyTime: 0})
	var se *client.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusBusy {
		t.Fatalf("a refused Identify: %v, want BUSY", err)
	}
	if got := identifyTime(); got == 0 {
		t.Fatal("a refused Identify(0) stopped the identification")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(times) != 1 || times[0] != 30 {
		t.Fatalf("HandleIdentify got %v, want [30]", times)
	}
	if len(effects) != 1 || effects[0] != [2]uint8{uint8(identifydef.EffectIdentifierBlink), uint8(identifydef.EffectVariantDefault)} {
		t.Fatalf("HandleTriggerEffect got %v", effects)
	}
}
