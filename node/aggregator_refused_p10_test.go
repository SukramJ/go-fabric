// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
)

// p10Plug is a plain Endpoint (no device): an OnOff plug.
type p10Plug struct{ spec endpoint.Spec }

func (p *p10Plug) Spec() endpoint.Spec { return p.spec }

// p10Singleton is a BasicInformation-id server: on a bridged endpoint it
// is a misplaced RootNode singleton, which refuses the assembly in every
// mode.
type p10Singleton struct{}

func (p10Singleton) MatterClusterID() uint32                                { return 0x0028 }
func (p10Singleton) MatterRead(uint32) (any, bool)                          { return nil, false }
func (p10Singleton) MatterWrite(context.Context, uint32, any) error         { return nil }
func (p10Singleton) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (p10Singleton) MatterReportable() []uint32                             { return nil }

// p10Source serves the plug's servers.
type p10Source struct{ servers []contract.ClusterServer }

func (p *p10Source) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *p10Source) MatterClusterServers() []contract.ClusterServer { return p.servers }

// newP10Plug is a plug under key; misplaced adds a RootNode singleton.
func newP10Plug(t *testing.T, key string, misplaced bool) *p10Plug {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	servers := []contract.ClusterServer{srv}
	if misplaced {
		servers = append(servers, p10Singleton{})
	}
	return &p10Plug{spec: endpoint.Spec{
		StableKey: endpoint.StringKey(key), DeviceAddress: key, DeviceType: onoff.DeviceTypeOnOffPlugInUnit,
		FriendlyName: key, ChannelAddress: key + ":1", Source: &p10Source{servers: servers},
	}}
}

func p10DataVersion(t *testing.T, n *Node, key string) uint32 {
	t.Helper()
	for _, ep := range n.Bridge().Topology().Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == key {
			return ep.ClusterDataVersion(onoff.ClusterID)
		}
	}
	t.Fatalf("key %s is not in the served topology", key)
	return 0
}

// TestRefusedReassemblyKeepsTheServedInstance (review II, P10F-fsm-6405,
// decision 765): a reassembly the bridge refuses after the node's
// snapshot was assembled changes no node state. Snapshot 2 drops key k
// and is refused; snapshot 3 serves k again, and k — served throughout —
// keeps its instance and so its DataVersion.
func TestRefusedReassemblyKeepsTheServedInstance(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationOff
	n := newMDNSTestNode(t, cfg)
	k, bad := newP10Plug(t, "k", false), newP10Plug(t, "bad", true)
	if err := n.Aggregator().Add(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	before := p10DataVersion(t, n, "k")

	// Snapshot 2: k removed, bad added — refused.
	err := n.Aggregator().Batch(ctx, func(b *Batch) error {
		if err := b.Remove(endpoint.StringKey("k")); err != nil {
			return err
		}
		return b.Add(bad)
	})
	var refusal *endpoint.DeviceTypeConformanceError
	if !errors.As(err, &refusal) {
		t.Fatalf("Batch = %v; the test needs its reassembly refused by the device-type judgement", err)
	}
	if got := p10DataVersion(t, n, "k"); got != before {
		t.Fatalf("DataVersion of k after the refusal = %d, want %d", got, before)
	}

	// Snapshot 3: k back, bad gone — installed.
	if err := n.Aggregator().Batch(ctx, func(b *Batch) error {
		if err := b.Remove(endpoint.StringKey("bad")); err != nil {
			return err
		}
		return b.Add(k)
	}); err != nil {
		t.Fatal(err)
	}
	if got := p10DataVersion(t, n, "k"); got != before {
		t.Errorf("DataVersion of k after the refused reassembly = %d, want %d: the node took the refused snapshot's instances", got, before)
	}
}
