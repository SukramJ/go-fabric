// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientSubscribeTest.ts at
// 862ad75f: the controller's subscribe through a client node — the empty
// subscription refused, the data version filters a read or subscription
// sends (injected, the caller's kept and winning, none with the other
// fabric filter, recomputed for a replaced sustained subscription), and
// the versions kept across a controller restart.
//
// matter.js observes the filters through clientStructureOf(peer)
// (injectVersionFilters, cacheFabricFiltered) and a patched
// ClientInteraction.subscribe; the port uses the test seams
// client.KnownVersions, client.CacheFabricFiltered and
// client.ObserveRequests, and otherwise what the device sends back: a
// cluster with a current version is absent from the report. The refused
// subscription is the Go error interaction.ErrMalformedRequest where
// matter.js throws an ImplementationError with the same message.
//
// Not ported (each with its reason in the register): includeKnownVersions
// on a subscription (client.Node.Subscribe takes an
// interaction.SubscribeRequest, which has no such field); the runtime
// change of the default subscription and its fabric filter, the cache's
// recorded fabric filter and its storage failures (the Go cache records no
// filter and the default subscription is fixed at Start); the default
// subscription's ownership across replacements; and the priming report
// answered INVALID_SUBSCRIPTION after a cancel (controller/interaction).
package client_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// accessControlCluster is the AccessControl cluster id.
const accessControlCluster uint32 = 0x001F

func TestClientSubscribeParity(t *testing.T) {
	t.Parallel()
	t.Run("rejects a subscription that designates no attributes or events", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{rootOnly: true})
		err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
			_, err := cp.node.Subscribe(ctx, interaction.SubscribeRequest{})
			return err
		})
		if !errors.Is(err, interaction.ErrMalformedRequest) {
			t.Fatalf("subscribe without paths: %v, want ErrMalformedRequest", err)
		}
	})
}

// readClusters is ClientSubscribeTest.ts:133-148 readClusters: a read of
// endpoint 0 through the node and the clusters it reported.
func readClusters(t *testing.T, cp *clientPair, req client.ReadRequest) map[uint32]uint32 {
	t.Helper()
	req.Attributes = []im.ConcreteAttributePath{rootPath()}
	got := map[uint32]uint32{}
	cp.run(t, func(ctx context.Context) error {
		r, err := cp.node.Read(ctx, req)
		clustersOf(r, got)
		return err
	})
	return got
}

// primingClusters is ClientSubscribeTest.ts:86-101 primingClusters: a
// subscription of endpoint 0 that keeps the others, closed after its
// priming report, and the clusters that report carried.
func primingClusters(t *testing.T, cp *clientPair, req interaction.SubscribeRequest) map[uint32]uint32 {
	t.Helper()
	req.Attributes = []im.ConcreteAttributePath{rootPath()}
	var mu sync.Mutex
	got := map[uint32]uint32{}
	req.Updated = func(r *interaction.Report) {
		mu.Lock()
		clustersOf(r, got)
		mu.Unlock()
	}
	cp.run(t, func(ctx context.Context) error {
		s, err := cp.node.Subscribe(ctx, req)
		if err != nil {
			return err
		}
		s.Close()
		return nil
	})
	mu.Lock()
	defer mu.Unlock()
	return maps(got)
}

// resubscribedClusters is ClientSubscribeTest.ts:103-131
// resubscribedClusters: a sustained subscription of endpoint 0 through one
// loss of its peer, and the clusters of the replacement's priming report.
// matter.js stops and starts the device; the Go node draws new random data
// versions at every start (contract.DataVersionTracker) where matter.js
// persists them, so the port loses the underlying subscriptions to the
// controller's registry instead (interaction.Subscriptions.CloseForPeer,
// as TestLiveClientNode does) and keeps the device's versions.
func resubscribedClusters(t *testing.T, req interaction.SubscribeRequest) map[uint32]uint32 {
	t.Helper()
	cp := newClientPair(t, clientPairOptions{})
	req.Attributes = []im.ConcreteAttributePath{rootPath()}
	req.Sustain = true
	var mu sync.Mutex
	got := map[uint32]uint32{}
	activations := 0
	req.Updated = func(r *interaction.Report) {
		mu.Lock()
		clustersOf(r, got)
		mu.Unlock()
	}
	req.ActiveChanged = func(active bool) {
		if active {
			mu.Lock()
			activations++
			mu.Unlock()
		}
	}
	var s *interaction.Subscription
	cp.run(t, func(ctx context.Context) error {
		var err error
		s, err = cp.node.Subscribe(ctx, req)
		return err
	})
	defer s.Close()
	cp.eventually(t, "the sustained subscription", s.Active)
	mu.Lock()
	got = map[uint32]uint32{}
	mu.Unlock()
	subs := cp.pair.Controller.Subscriptions()
	subs.CloseForPeer(subs.Active()[0].Peer())
	cp.eventually(t, "the replacement subscription", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return activations >= 2
	})
	mu.Lock()
	defer mu.Unlock()
	return maps(got)
}

// maps copies m.
func maps(m map[uint32]uint32) map[uint32]uint32 {
	out := make(map[uint32]uint32, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// has reports whether the report clusters contain cluster.
func has(m map[uint32]uint32, cluster uint32) bool {
	_, ok := m[cluster]
	return ok
}

// filteredClusters is ClientSubscribeTest.ts:351-356 filteredClusters: the
// clusters a read of endpoint 0 with the fabric filter would filter.
func filteredClusters(n *client.Node, fabricFiltered bool) []uint32 {
	filters := client.KnownVersions(n, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{rootPath()}, NoFabricFilter: !fabricFiltered})
	out := make([]uint32, 0, len(filters))
	for _, f := range filters {
		out = append(out, f.Cluster)
	}
	slices.Sort(out)
	return out
}

func TestClientSubscribeDataVersionFiltersParity(t *testing.T) {
	t.Parallel()
	staleBasicInfo := []im.DataVersionFilter{{Endpoint: 0, Cluster: basicinfo.ClusterID, DataVersion: 0}}

	t.Run("injects known data versions into a read by default", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if has(readClusters(t, cp, client.ReadRequest{}), basicinfo.ClusterID) {
			t.Fatal("the read reported BasicInformation despite its known version")
		}
	})

	t.Run("injects no data versions into a read with includeKnownVersions", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if !has(readClusters(t, cp, client.ReadRequest{IncludeKnownVersions: true}), basicinfo.ClusterID) {
			t.Fatal("the read with IncludeKnownVersions omitted BasicInformation")
		}
	})

	t.Run("keeps the caller's data version filters and injects none for their clusters", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		unknown := im.DataVersionFilter{Endpoint: 99, Cluster: 0x1234, DataVersion: 5}
		known := im.DataVersionFilter{Endpoint: 0, Cluster: basicinfo.ClusterID, DataVersion: 5}
		caller := []im.DataVersionFilter{unknown, known}
		injected := client.KnownVersions(cp.node, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}, DataVersionFilters: caller})
		if len(injected) < 2 || injected[0] != unknown || injected[1] != known {
			t.Fatalf("the caller's filters are not first: %+v", injected)
		}
		var basic []im.DataVersionFilter
		acl := false
		for _, f := range injected {
			if f.Cluster == basicinfo.ClusterID {
				basic = append(basic, f)
			}
			acl = acl || f.Cluster == accessControlCluster
		}
		if !slices.Equal(basic, []im.DataVersionFilter{known}) {
			t.Fatalf("BasicInformation filters %+v, want only the caller's", basic)
		}
		if !acl {
			t.Fatal("no AccessControl filter injected")
		}
		if !slices.Equal(caller, []im.DataVersionFilter{unknown, known}) {
			t.Fatalf("the caller's filters changed: %+v", caller)
		}
	})

	t.Run("lets a caller's data version filter win over the cached version", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		read := readClusters(t, cp, client.ReadRequest{ReadRequest: interaction.ReadRequest{DataVersionFilters: staleBasicInfo}})
		primed := primingClusters(t, cp, interaction.SubscribeRequest{DataVersionFilters: staleBasicInfo})
		if !has(read, basicinfo.ClusterID) || !has(primed, basicinfo.ClusterID) {
			t.Fatalf("BasicInformation read %v, primed %v; want both", has(read, basicinfo.ClusterID), has(primed, basicinfo.ClusterID))
		}
	})

	t.Run("lets a caller's data version filter win when a sustained subscription replaces a lost one", func(t *testing.T) {
		t.Parallel()
		if !has(resubscribedClusters(t, interaction.SubscribeRequest{DataVersionFilters: staleBasicInfo}), basicinfo.ClusterID) {
			t.Fatal("the replacement's priming report omitted BasicInformation")
		}
	})

	t.Run("injects known data versions by default", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if has(primingClusters(t, cp, interaction.SubscribeRequest{}), basicinfo.ClusterID) {
			t.Fatal("the priming report carried BasicInformation despite its known version")
		}
	})

	t.Run("does not modify the caller's data version filters when injecting known versions", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		filter := im.DataVersionFilter{Endpoint: 99, Cluster: 0x1234, DataVersion: 0}
		filters := []im.DataVersionFilter{filter}
		if has(primingClusters(t, cp, interaction.SubscribeRequest{DataVersionFilters: filters}), basicinfo.ClusterID) {
			t.Fatal("the priming report carried BasicInformation")
		}
		if !slices.Equal(filters, []im.DataVersionFilter{filter}) {
			t.Fatalf("the caller's filters changed: %+v", filters)
		}
	})

	t.Run("injects no data versions into a subscription with the other fabric filter", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if !has(primingClusters(t, cp, interaction.SubscribeRequest{NoFabricFilter: true}), basicinfo.ClusterID) {
			t.Fatal("the unfiltered priming report omitted BasicInformation")
		}
	})

	t.Run("sends no data version filters with an unfiltered subscription while the cache is fabric filtered", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if !client.CacheFabricFiltered(cp.node) {
			t.Fatal("the cache is not fabric filtered")
		}
		var mu sync.Mutex
		var sent []interaction.SubscribeRequest
		client.ObserveRequests(cp.node, func(r interaction.SubscribeRequest) {
			if !r.Sustain {
				mu.Lock()
				sent = append(sent, r)
				mu.Unlock()
			}
		})
		for _, noFilter := range []bool{false, true} {
			primingClusters(t, cp, interaction.SubscribeRequest{NoFabricFilter: noFilter})
		}
		mu.Lock()
		defer mu.Unlock()
		if len(sent) != 2 {
			t.Fatalf("%d requests observed, want 2", len(sent))
		}
		filtered, unfiltered := sent[0], sent[1]
		if filtered.NoFabricFilter || len(filtered.DataVersionFilters) == 0 {
			t.Fatalf("the filtered request: unfiltered %v, %d filters", filtered.NoFabricFilter, len(filtered.DataVersionFilters))
		}
		if !unfiltered.NoFabricFilter || len(unfiltered.DataVersionFilters) != 0 {
			t.Fatalf("the unfiltered request: unfiltered %v, filters %+v", unfiltered.NoFabricFilter, unfiltered.DataVersionFilters)
		}
	})

	t.Run("injects known data versions when a sustained subscription replaces a lost one", func(t *testing.T) {
		t.Parallel()
		if has(resubscribedClusters(t, interaction.SubscribeRequest{}), basicinfo.ClusterID) {
			t.Fatal("the replacement's priming report carried BasicInformation")
		}
	})

	t.Run("applies the caller's refreshRequest to the caller's filters when replacing a lost subscription", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var refreshed [][]im.DataVersionFilter
		clusters := resubscribedClusters(t, interaction.SubscribeRequest{RefreshRequest: func(r interaction.SubscribeRequest) interaction.SubscribeRequest {
			mu.Lock()
			refreshed = append(refreshed, r.DataVersionFilters)
			mu.Unlock()
			return r
		}})
		mu.Lock()
		defer mu.Unlock()
		if len(refreshed) == 0 {
			t.Fatal("the caller's RefreshRequest never ran")
		}
		for _, f := range refreshed {
			if f != nil {
				t.Fatalf("the caller's RefreshRequest saw filters %+v, want the caller's own (none)", f)
			}
		}
		if has(clusters, basicinfo.ClusterID) {
			t.Fatal("the replacement's priming report carried BasicInformation")
		}
	})
}

func TestClientSubscribeCacheFabricFilterParity(t *testing.T) {
	t.Parallel()
	t.Run("applies reports of a sustained subscription by the fabric filter its refreshRequest returned", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)}})
		var once sync.Once
		reported := make(chan struct{})
		// BootstrapRead stands for bootstrapWithRead: the Go node layer
		// fills the hook, and RefreshRequest runs after it as after
		// matter.js's bootstrap read.
		req := interaction.SubscribeRequest{
			Attributes:     []im.ConcreteAttributePath{clusterPath(cp.lightEP, onoff.ClusterID)},
			NoFabricFilter: true,
			Sustain:        true,
			BootstrapRead:  func(context.Context) error { return nil },
			RefreshRequest: func(r interaction.SubscribeRequest) interaction.SubscribeRequest {
				r.NoFabricFilter = false
				return r
			},
			Updated: func(r *interaction.Report) {
				for _, a := range r.Attributes {
					if a.Path.Attribute == onoff.AttrOnOff && a.Value.El.Bool {
						once.Do(func() { close(reported) })
					}
				}
			},
		}
		var s *interaction.Subscription
		cp.run(t, func(ctx context.Context) error {
			var err error
			s, err = cp.node.Subscribe(ctx, req)
			return err
		})
		defer s.Close()
		cp.eventually(t, "the sustained subscription", s.Active)
		onOffOf := func() any {
			v, _ := cp.node.Endpoint(cp.lightEP).Cluster(onoff.ClusterID).Decoded(onoff.AttrOnOff)
			return v
		}
		if v := onOffOf(); v != false {
			t.Fatalf("OnOff %v before the change, want false", v)
		}
		cp.light.OnOff().Set(true)
		cp.eventually(t, "the report of OnOff true", func() bool {
			select {
			case <-reported:
				return true
			default:
				return false
			}
		})
		if v := onOffOf(); v != true {
			t.Fatalf("OnOff %v in the image after the report, want true", v)
		}
	})
	t.Run("keeps data versions on restart with an unchanged fabric filter", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)}})
		before := filteredClusters(cp.node, true)
		if !slices.Contains(before, basicinfo.ClusterID) {
			t.Fatalf("filters before the restart %v lack BasicInformation", before)
		}
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		restarted := cp.pair.Controller.Registry().Get(id)
		if restarted == nil {
			t.Fatalf("node %q not restored", id)
		}
		if after := filteredClusters(restarted, true); !slices.Equal(after, before) {
			t.Fatalf("filters after the restart %v, want %v", after, before)
		}
	})
}
