// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// pairLive is a node.Node with one bridged plug in process, commissioned
// by the module's own commissioner, whose context carries the
// subscription registry and the CASE responder — a fabrictest pair on the
// in-memory network and the site's fake clock.
type pairLive struct {
	site   *fabrictest.Site
	dev    *fabrictest.Device
	srv    *onoff.Server
	reg    *interaction.Subscriptions
	peer   *peer.Peer
	c      *interaction.Interaction
	plugEP uint16
}

func newPairLive(t *testing.T) *pairLive {
	t.Helper()
	holdSite(t)
	site := fabrictest.NewSite(t, fabrictest.Options{})
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, TimeSource: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{
			Bridged: []node.Endpoint{&plug{srv: srv}},
			Configure: func(c *node.Config) {
				c.Commissioning.Passcode = testPasscode
				c.BasicInformation.ProductName, c.BasicInformation.NodeLabel, c.BasicInformation.SerialNumber = "client live test", "live node", "CL-1"
			},
		},
		Controller: fabrictest.ControllerOptions{FabricID: liveFabricID, NodeID: liveController, Label: "live"},
		Commission: fabrictest.CommissionOptions{NodeID: liveNode, Edit: func(o *commissioner.Options) { o.OnAttestationFailure = attestation.Accept() }},
	})
	lv := &pairLive{site: site, dev: pair.Device, srv: srv, reg: pair.Controller.Subscriptions(), peer: pair.Peer, c: pair.Interaction}
	ep, ok := pair.Device.EndpointOf("plug:1")
	if !ok {
		t.Fatal("the plug has no endpoint")
	}
	lv.plugEP = ep
	return lv
}

// eventually waits for cond, moving the site's clock (at most 60 s).
func (lv *pairLive) eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := lv.site.Eventually(ctx, what, cond); err != nil {
		t.Fatal(err)
	}
}

// liveHooks records what a live client node reports.
type liveHooks struct {
	mu        sync.Mutex
	installed []uint16
	deleted   []string
	changed   chan struct{}
	state     chan client.ConnectionState
	events    int
}

func newLiveHooks() *liveHooks {
	return &liveHooks{changed: make(chan struct{}, 1024), state: make(chan client.ConnectionState, 64)}
}

func (h *liveHooks) hooks() client.Hooks {
	return client.Hooks{
		EndpointInstalled: func(ep *client.Endpoint) {
			h.mu.Lock()
			h.installed = append(h.installed, ep.Number())
			h.mu.Unlock()
		},
		ClusterDeleted: func(ep *client.Endpoint, c *client.Cluster) {
			h.mu.Lock()
			h.deleted = append(h.deleted, c.Name())
			h.mu.Unlock()
		},
		Changed: func() {
			select {
			case h.changed <- struct{}{}:
			default:
			}
		},
		ConnectionStateChanged: func(s client.ConnectionState) { h.state <- s },
		Event: func(uint16, uint32, uint32, interaction.EventData, any) {
			h.mu.Lock()
			h.events++
			h.mu.Unlock()
		},
	}
}

// waitState waits for the transition to want, moving the site's clock.
func (h *liveHooks) waitState(t *testing.T, lv *pairLive, want client.ConnectionState) {
	t.Helper()
	lv.eventually(t, fmt.Sprintf("the transition to %v", want), func() bool {
		for {
			select {
			case s := <-h.state:
				if s == want {
					return true
				}
			default:
				return false
			}
		}
	})
}

// waitFor waits for cond, moving the site's clock (at most 60 s).
func (h *liveHooks) waitFor(t *testing.T, lv *pairLive, what string, cond func() bool) {
	t.Helper()
	lv.eventually(t, what, cond)
}

// tree describes the image: number, device type, owner and cluster count
// per endpoint.
func tree(n *client.Node) []string {
	eps := n.Endpoints()
	out := make([]string, 0, len(eps))
	for _, ep := range eps {
		owner := "-"
		if o := ep.Owner(); o != nil {
			owner = strconv.Itoa(int(o.Number()))
		}
		out = append(out, fmt.Sprintf("ep%d type %#x owner %s clusters %d", ep.Number(), ep.DeviceType(), owner, len(ep.Clusters())))
	}
	return out
}

// TestLiveClientNode: the client node over a commissioned node.Node — the
// initial read of a newly commissioned node and the default subscription
// (NetworkClient.ts:113-196 at 0ceb202d) install the tree the node's
// Descriptors describe (ClientStructure.ts:851-1009); a toggle on the node
// reaches the image through the subscription; the node restarted on its
// database delivers again; a lost subscription is re-established with the
// refreshed versions and event minimum (ClientNodeInteraction.ts:148-155);
// the plug removed from the node is erased from the image (:971-1008); a
// second client node on the store loads the tree without a read
// (:183-232).
func TestLiveClientNode(t *testing.T) {
	t.Parallel()
	lv := newPairLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st := &client.MemoryStore{}
	h := newLiveHooks()
	n, err := client.New(client.Config{
		ID: "peer1", Peer: lv.peer, Interaction: lv.c, Store: st, AutoSubscribe: true, NewlyCommissioned: true,
		Hooks: h.hooks(), Logger: discardLog(), Rand: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []interaction.SubscribeRequest
	client.ObserveRequests(n, func(r interaction.SubscribeRequest) {
		mu.Lock()
		requests = append(requests, r)
		mu.Unlock()
	})
	if err := lv.site.Run(ctx, n.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.waitState(t, lv, client.Connected)
	if !n.Seeded() || !n.SubscriptionActive() {
		t.Fatal("not seeded or not subscribed")
	}

	// The tree: the root keeps the RootNode type with an unknown revision
	// (ClientStructure.ts:876-892); the aggregator under it; the plug under
	// the aggregator.
	root := n.Endpoint(0)
	if root == nil || root.DeviceType() != 0x0016 || root.Name() != "RootNode" {
		t.Fatalf("root %+v", tree(n))
	}
	if _, ok := root.DeviceRevision(); ok {
		t.Fatal("the root took a revision from its DeviceTypeList")
	}
	plugEP := n.Endpoint(lv.plugEP)
	if plugEP == nil || plugEP.DeviceType() != uint32(onoff.DeviceTypeOnOffPlugInUnit) || plugEP.Name() != "OnOffPlugInUnit" {
		t.Fatalf("plug endpoint %v", tree(n))
	}
	agg := plugEP.Owner()
	if agg == nil || agg.DeviceType() != 0x000E || agg.Name() != "Aggregator" || agg.Owner() != root {
		t.Fatalf("aggregator %v", tree(n))
	}
	c := plugEP.Cluster(onoff.ClusterID)
	if c == nil || c.Definition() == nil || !c.Supports(onoff.AttrOnOff) || !c.SupportsCommand(onoff.CmdToggle) {
		t.Fatal("the plug's OnOff cluster")
	}
	if v, err := c.Decoded(onoff.AttrOnOff); err != nil || v != false {
		t.Fatalf("OnOff %v %v", v, err)
	}
	// The client view: the generated device type, and the image judged
	// by the server-side validator.
	if d := plugEP.Device(); d == nil || d.Name != "OnOffPlugInUnit" {
		t.Fatalf("the plug's Device() %v", d)
	}
	if r := plugEP.Conformance(); r.Status != client.ConformanceClean {
		t.Fatalf("the plug's conformance %v: %v, unjudged %v", r.Status, r.Violations, r.Unjudged)
	}
	rev, revOK := plugEP.DeviceRevision()
	t.Logf("tree after the initial read: %v (plug revision %d %v)", tree(n), rev, revOK)
	var named []string
	for _, ep := range n.Endpoints() {
		for _, cl := range ep.Clusters() {
			if cl.Definition() == nil && !slices.Contains(named, cl.Name()) {
				named = append(named, cl.Name())
			}
		}
	}
	sort.Strings(named)
	t.Logf("named clusters met (no generated definition): %v", named)
	h.mu.Lock()
	t.Logf("events delivered: %d", h.events)
	h.mu.Unlock()
	p := n.Properties()
	floor, ceiling := n.IntervalBounds(nil, nil)
	t.Logf("properties: %+v; bounds %v %v", p, floor, ceiling)

	// The first subscription request carried the versions of the initial
	// read and the event minimum.
	mu.Lock()
	first := requests[0]
	mu.Unlock()
	if len(first.DataVersionFilters) == 0 || len(first.EventFilters) != 1 || first.EventFilters[0].EventMin == 0 {
		t.Fatalf("the default subscription's request: %d filters, events %+v", len(first.DataVersionFilters), first.EventFilters)
	}

	// A toggle on the node reaches the image.
	lv.srv.Set(true)
	h.waitFor(t, lv, "OnOff true", func() bool { v, _ := c.Decoded(onoff.AttrOnOff); return v == true })

	// The node restarts on its database: the subscription delivers again.
	if err := lv.dev.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, lv, "the subscription after the restart", n.SubscriptionActive)
	lv.srv.Set(false)
	h.waitFor(t, lv, "OnOff false after the restart", func() bool {
		cl := n.Endpoint(lv.plugEP).Cluster(onoff.ClusterID)
		v, _ := cl.Decoded(onoff.AttrOnOff)
		return v == false
	})
	mu.Lock()
	afterRestart := len(requests)
	mu.Unlock()
	t.Logf("subscription requests after the node's restart: %d", afterRestart)

	// The underlying subscription is lost: the replacement carries the
	// refreshed versions and the event minimum.
	lv.reg.CloseForPeer(lv.reg.Active()[0].Peer())
	h.waitFor(t, lv, "a re-subscription", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(requests) > afterRestart
	})
	mu.Lock()
	refreshed := requests[len(requests)-1]
	mu.Unlock()
	if len(refreshed.DataVersionFilters) == 0 || len(refreshed.EventFilters) != 1 || refreshed.EventFilters[0].EventMin == 0 {
		t.Fatalf("the refreshed request: %d filters, events %+v", len(refreshed.DataVersionFilters), refreshed.EventFilters)
	}
	h.waitFor(t, lv, "the re-established subscription", n.SubscriptionActive)

	// The plug leaves the node: erased from the image.
	h.mu.Lock()
	h.installed, h.deleted = nil, nil
	h.mu.Unlock()
	if err := lv.dev.Node().Aggregator().Remove(ctx, endpoint.StringKey("plug:1")); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, lv, "the plug's erasure", func() bool { return n.Endpoint(lv.plugEP) == nil })
	h.mu.Lock()
	installed, deleted := h.installed, h.deleted
	h.mu.Unlock()
	if len(installed) != 0 {
		t.Fatalf("endpoints installed on the removal: %v", installed)
	}
	t.Logf("cluster deletions on the removal: %v; tree %v", deleted, tree(n))
	want := tree(n)

	// A second node on the store loads the tree from the cache.
	if err := lv.site.Run(ctx, n.Close); err != nil {
		t.Fatal(err)
	}
	second, err := client.New(client.Config{ID: "peer1", Peer: lv.peer, Interaction: lv.c, Store: st, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree(second); !slices.Equal(got, want) {
		t.Fatalf("reloaded tree %v, want %v", got, want)
	}
	if second.SubscriptionActive() || second.ConnectionState() != client.Disconnected {
		t.Fatal("the reloaded node is online")
	}
}
