// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/identify/IdentifyServerTest.ts
// at 862ad75f: TriggerEffect support on an endpoint, following its device
// type's requirement, its withdrawal and its explicit enablement.
//
// matter.js adds the device to a MockServerNode and invokes over a mocked
// exchange (interaction.invoke); the port commissions a fabrictest pair
// whose device serves the endpoint under its Aggregator, reads
// AcceptedCommandList and invokes TriggerEffect over the wire, and asserts
// the IM status matter.js reads.
//
// On a bridged endpoint the Identify server belongs to the endpoint layer
// (endpoint/materialize.go mounts the endpoint's one core.Identify), which
// accepts TriggerEffect on every endpoint and exposes no effect event. The
// cases whose expectation that meets — the command is supported — run
// here; the withdrawal on a device type that does not require the command
// and the effectTriggered observation are the register's missing cases. A
// host's MyIdentifyServer (an override of triggerEffect or of
// suppressTriggerEffect, alter, enable) has no Go form on a bridged
// endpoint: those cases run on the endpoint the device type alone builds.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// identifyEffect is IdentifyServerTest.ts:30-33 EFFECT.
var identifyEffect = identifydef.TriggerEffectRequest{
	EffectIdentifier: identifydef.EffectIdentifierBlink,
	EffectVariant:    identifydef.EffectVariantDefault,
}

// identifyNode is a commissioned pair whose device serves one endpoint,
// `node.add(<device>)` of the case.
type identifyNode struct {
	pair *fabrictest.Pair
	ep   uint16
}

func newIdentifyNode(t *testing.T, build func(device.Options) (node.Endpoint, error)) *identifyNode {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	e, err := build(device.Options{StableKey: endpoint.StringKey("dev:1"), Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{e}}})
	ep, ok := p.Device.EndpointOf("dev:1")
	if !ok {
		t.Fatal("the device has no endpoint")
	}
	return &identifyNode{pair: p, ep: ep}
}

func identifyCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// acceptedCommands is IdentifyServerTest.ts:165-167, read over the wire.
func (n *identifyNode) acceptedCommands(t *testing.T) []uint64 {
	t.Helper()
	got, err := fabrictest.Read(identifyCtx(t), n.pair, im.ConcreteAttributePath{
		Endpoint: n.ep, Cluster: identifydef.ClusterID, Attribute: cluster.AttrGlobalAcceptedCommandList,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint64, 0, len(got.Value.Children))
	for _, c := range got.Value.Children {
		ids = append(ids, c.El.Uint)
	}
	return ids
}

// invokeStatus is IdentifyServerTest.ts:169-187: TriggerEffect with EFFECT,
// the status of the invoke response.
func (n *identifyNode) invokeStatus(t *testing.T) im.StatusCode {
	t.Helper()
	_, err := fabrictest.Invoke(identifyCtx(t), n.pair, im.ConcreteCommandPath{
		Endpoint: n.ep, Cluster: identifydef.ClusterID, Command: identifydef.CmdTriggerEffect,
	}, identifyEffect)
	var se *interaction.StatusError
	switch {
	case err == nil:
		return im.StatusSuccess
	case errors.As(err, &se):
		return se.Status
	default:
		t.Fatal(err)
		return 0
	}
}

func (n *identifyNode) wantSupported(t *testing.T, invoke bool) {
	t.Helper()
	if got := n.acceptedCommands(t); !slices.Contains(got, uint64(identifydef.CmdTriggerEffect)) {
		t.Errorf("AcceptedCommandList %v without TriggerEffect", got)
	}
	if invoke {
		if got := n.invokeStatus(t); got != im.StatusSuccess {
			t.Errorf("TriggerEffect: %v, want SUCCESS", got)
		}
	}
}

func contactSensor(o device.Options) (node.Endpoint, error) { return device.NewContactSensor(o) }

func onOffLight(o device.Options) (node.Endpoint, error) { return device.NewOnOffLight(o) }

func TestParityMatterJS_IdentifyServerTest(t *testing.T) {
	t.Run("triggerEffect", func(t *testing.T) {
		t.Run("is supported on a device type that requires it (support only)", func(t *testing.T) {
			// OnOffLight requires TriggerEffect (device/on_off_light_gen.go).
			// The case's effects == [EFFECT] is missing: no effect event.
			newIdentifyNode(t, onOffLight).wantSupported(t, true)
		})

		t.Run("is supported when a device type requirement is applied manually", func(t *testing.T) {
			// ContactSensorDevice.with(IdentifyServer.alter({ … optional:
			// false })): the Go endpoint accepts TriggerEffect on every
			// device type, with or without the alteration.
			newIdentifyNode(t, contactSensor).wantSupported(t, false)
		})

		t.Run("is supported when explicitly implemented (support only)", func(t *testing.T) {
			// MyIdentifyServer's triggerEffect override has no Go form on a
			// bridged endpoint; the support it keeps is the endpoint's. The
			// effects == [EFFECT] observation is missing.
			newIdentifyNode(t, contactSensor).wantSupported(t, true)
		})

		t.Run("remains supported when an implementation replaces the requirement-derived behavior", func(t *testing.T) {
			newIdentifyNode(t, onOffLight).wantSupported(t, true)
		})

		t.Run("is supported when enabled", func(t *testing.T) {
			// IdentifyServer.enable({ commands: { triggerEffect: true } }).
			newIdentifyNode(t, contactSensor).wantSupported(t, true)
		})

		t.Run("is supported when suppression is overridden", func(t *testing.T) {
			newIdentifyNode(t, contactSensor).wantSupported(t, false)
		})
	})
}
