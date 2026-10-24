// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/endpoint/EndpointNumberConflictTest.ts
// at 862ad75f: endpoint numbers kept, refused and retracted.
//
// A matter.js endpoint may claim a number; in Go a host claims none — the
// assembler allocates each number for a StableKey from the persisted
// counter (endpoint/identity.go Store, endpoint/assembler.go
// assignOrReuseID) — so the claimant refusals have no subject. What has
// one ports: an addition the node refuses (strict validation, the Go
// counterpart of a failed essential add) and its retry under the same key
// and number; a second endpoint with the key of one added
// (node.ErrDuplicateKey, the guard in place of the index collision);
// parts of different parents; the PartsList report of a removal; and a
// persisted number (the Go "preset") beside a new allocation, on the
// production in-memory store.
package node_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

const skipClaimant = "a host claims no endpoint number in Go: the assembler allocates every number for a StableKey from the persisted counter (endpoint/identity.go Store, endpoint/assembler.go assignOrReuseID), so there is no claimant of a live, root or sibling number to refuse"

// strictDevice is a started device whose node refuses any new violation an
// addition causes.
func strictDevice(t *testing.T) *fabrictest.Device {
	t.Helper()
	return quietSite(t).AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationStrict
	}})
}

// failingEndpoint is an OnOffLight that serves OnOff alone, which strict
// validation refuses — the Go counterpart of FailingOnOffServer's refused
// initialization (EndpointNumberConflictTest.ts:21-25).
func failingEndpoint(t *testing.T, key string) node.Endpoint {
	t.Helper()
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return fabrictest.Endpoint(key, onoff.DeviceTypeOnOffLight, srv)
}

// refusedNumber adds ep and returns the number the refusal names.
func refusedNumber(t *testing.T, d *fabrictest.Device, ep node.Endpoint) uint16 {
	t.Helper()
	err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(context.Background(), ep) })
	var refusal *endpoint.DeviceTypeConformanceError
	if !errors.As(err, &refusal) {
		t.Fatalf("Add = %v, want a device type refusal", err)
	}
	return refusal.Violations[0].Endpoint
}

// assembleWith assembles specs on store as one model-complete snapshot.
func assembleWith(t *testing.T, store endpoint.Store, specs ...endpoint.Spec) *endpoint.Topology {
	t.Helper()
	a, err := endpoint.New(store, endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "host", Endpoints: specs, ModelComplete: true}})
	if err != nil {
		t.Fatal(err)
	}
	return topo
}

func kitSpec(t *testing.T, key string) endpoint.Spec {
	t.Helper()
	return kitLight(t, key).Spec()
}

// numberOf is the number of the endpoint keyed key in topo, 0 for none.
func numberOf(topo *endpoint.Topology, key string) uint16 {
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == key {
			return ep.ID
		}
	}
	return 0
}

// preset persists key at number on store — a number the store already
// holds for the key, the Go form of a preset number.
func preset(t *testing.T, store endpoint.Store, key string, number uint16) {
	t.Helper()
	if _, err := store.UpsertEndpointAssigning(context.Background(), endpoint.Record{Key: endpoint.StringKey(key), Scope: "host", EndpointID: number, DeviceType: onoff.DeviceTypeOnOffLight}); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointNumberConflictParityMatterJS(t *testing.T) {
	t.Run("refusing a claimant of a live endpoint's number", func(t *testing.T) {
		for _, title := range []string{
			"keeps the live endpoint in the node's index",
			"keeps the live endpoint's number in the node's PartsList",
			"keeps refusing further claimants",
			"reports no deletion of the live endpoint's number",
		} {
			t.Run(title, func(t *testing.T) { t.Skip(skipClaimant) })
		}
	})

	t.Run("a part that claims endpoint number 0", func(t *testing.T) {
		t.Run("is refused by a running node", func(t *testing.T) { t.Skip(skipClaimant) })
		t.Run("is refused before the node starts", func(t *testing.T) { t.Skip(skipClaimant) })
	})

	t.Run("refuses an added subtree whose leaves share a number", func(t *testing.T) { t.Skip(skipClaimant) })
	t.Run("refuses an automatic id that duplicates a sibling's id", func(t *testing.T) {
		t.Skip("an automatic id has no Go subject: every endpoint names its StableKey (node.ErrNoStableKey refuses one without)")
	})
	t.Run("refuses a duplicate preset number in a tree built before start", func(t *testing.T) { t.Skip(skipClaimant) })
	t.Run("refuses a nested preset number another endpoint holds", func(t *testing.T) { t.Skip(skipClaimant) })

	t.Run("a failed add of an essential endpoint", func(t *testing.T) {
		t.Run("closes the endpoint", func(t *testing.T) {
			d := strictDevice(t)
			number := refusedNumber(t, d, failingEndpoint(t, "failing"))
			if _, ok := d.EndpointOf("failing"); ok {
				t.Error("the refused endpoint is installed")
			}
			if ep := d.Bridge().Topology().FindByID(number); ep != nil {
				t.Errorf("endpoint %d is held by %v", number, ep.SourceKey)
			}
		})

		t.Run("accepts a retry with the same id and number", func(t *testing.T) {
			d := strictDevice(t)
			number := refusedNumber(t, d, failingEndpoint(t, "retried"))
			if err := d.Act(func(n *node.Node) error {
				return n.Aggregator().Replace(context.Background(), endpoint.StringKey("retried"), kitLight(t, "retried"))
			}); err != nil {
				t.Fatalf("the retry: %v", err)
			}
			if got, ok := d.EndpointOf("retried"); !ok || got != number {
				t.Errorf("retried at %d, %v; want %d", got, ok, number)
			}
		})
	})

	t.Run("keeps a preset number added after an automatic sibling", func(t *testing.T) {
		store := memstore.New()
		preset(t, store, "preset", 5)
		topo := assembleWith(t, store, kitSpec(t, "automatic"), kitSpec(t, "preset"))
		if numberOf(topo, "preset") != 5 || numberOf(topo, "automatic") == 5 {
			t.Errorf("preset %d, automatic %d", numberOf(topo, "preset"), numberOf(topo, "automatic"))
		}
	})

	t.Run("keeps a preset number of a cousin under another aggregator", func(t *testing.T) {
		// One Aggregator in Go: the cousins are parts of two bridged
		// devices.
		store := memstore.New()
		preset(t, store, "second:preset", 6)
		first, second := kitSpec(t, "first"), kitSpec(t, "second")
		first.Parts = []endpoint.Spec{kitSpec(t, "first:automatic")}
		second.Parts = []endpoint.Spec{kitSpec(t, "second:preset")}
		topo := assembleWith(t, store, first, second)
		if numberOf(topo, "second:preset") != 6 {
			t.Errorf("the preset cousin is at %d, want 6", numberOf(topo, "second:preset"))
		}
	})

	t.Run("logs an index collision and keeps the holder", func(t *testing.T) {
		// The Go guard refuses the second endpoint with the key rather than
		// logging a collision the index would see.
		d := quietSite(t).AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "holder")}})
		holder, _ := d.EndpointOf("holder")
		err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(context.Background(), kitLight(t, "holder")) })
		if !errors.Is(err, node.ErrDuplicateKey) {
			t.Fatalf("Add = %v, want ErrDuplicateKey", err)
		}
		if got, ok := d.EndpointOf("holder"); !ok || got != holder {
			t.Errorf("holder at %d, %v; want %d", got, ok, holder)
		}
	})

	t.Run("indexes parts with the same ID under different parents", func(t *testing.T) {
		store := memstore.New()
		first, second := kitSpec(t, "first"), kitSpec(t, "second")
		first.Parts = []endpoint.Spec{kitSpec(t, "first:light")}
		second.Parts = []endpoint.Spec{kitSpec(t, "second:light")}
		topo := assembleWith(t, store, first, second)
		a, b := numberOf(topo, "first:light"), numberOf(topo, "second:light")
		if a == 0 || b == 0 || a == b || topo.FindByID(a).SourceKey.String() != "first:light" || topo.FindByID(b).SourceKey.String() != "second:light" {
			t.Errorf("first:light %d, second:light %d", a, b)
		}
	})

	t.Run("retraction", func(t *testing.T) {
		partsList := client.AttributeAt(1, 0x001D, 0x0003)
		t.Run("reports no deletion when a refused endpoint closes", func(t *testing.T) {
			site := quietSite(t)
			p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
				cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationStrict
			}}})
			refusedNumber(t, p.Device, failingEndpoint(t, "refused"))
			before, err := fabrictest.Read(parityCtx(t), p, partsList)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Device.Act(func(n *node.Node) error {
				return n.Aggregator().Remove(context.Background(), endpoint.StringKey("refused"))
			}); err != nil {
				t.Fatal(err)
			}
			after, err := fabrictest.Read(parityCtx(t), p, partsList)
			if err != nil {
				t.Fatal(err)
			}
			if after.DataVersion != before.DataVersion {
				t.Errorf("the PartsList moved from version %d to %d", before.DataVersion, after.DataVersion)
			}
		})

		t.Run("does not list a refused child of a parent without an index", func(t *testing.T) {
			t.Skip("a refusal is atomic per reassembly in Go: a refused part refuses its parent with it (endpoint.DeviceTypeValidator), so no parent is installed without its child")
		})

		t.Run("reports one deletion for an endpoint that was erased and restarted", func(t *testing.T) {
			// Erasing and restarting an endpoint has no Go subject; its
			// removal is reported once.
			site := quietSite(t)
			p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "restarted")}}})
			number, _ := p.Device.EndpointOf("restarted")
			before, err := fabrictest.Read(parityCtx(t), p, partsList)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Device.Act(func(n *node.Node) error {
				return n.Aggregator().Remove(context.Background(), endpoint.StringKey("restarted"))
			}); err != nil {
				t.Fatal(err)
			}
			after, err := fabrictest.Read(parityCtx(t), p, partsList)
			if err != nil {
				t.Fatal(err)
			}
			if after.DataVersion != before.DataVersion+1 {
				t.Errorf("the PartsList moved from version %d to %d, want one change", before.DataVersion, after.DataVersion)
			}
			listed := make([]uint64, 0, len(after.Value.Children))
			for _, c := range after.Value.Children {
				listed = append(listed, c.El.Uint)
			}
			if slices.Contains(listed, uint64(number)) {
				t.Errorf("PartsList %v still lists %d", listed, number)
			}
		})
	})

	t.Run("allocates around a preset sibling", func(t *testing.T) {
		store := memstore.New()
		preset(t, store, "preset", 2)
		topo := assembleWith(t, store, kitSpec(t, "preset"), kitSpec(t, "automatic"))
		automatic := numberOf(topo, "automatic")
		if numberOf(topo, "preset") != 2 || automatic == 2 || automatic == 0 || topo.FindByID(automatic).SourceKey.String() != "automatic" {
			t.Errorf("preset %d, automatic %d", numberOf(topo, "preset"), automatic)
		}
	})
}
