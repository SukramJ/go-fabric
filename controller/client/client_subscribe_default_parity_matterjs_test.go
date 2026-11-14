// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
)

// A port of matter.js packages/node/test/node/ClientSubscribeTest.ts at
// 862ad75f, describe "cache fabric filter" (:347-1114) and the two cases
// of "cancelled while being established" whose updated callback fails
// (:1275-1331): the default subscription changed at run time
// (Node.SetDefaultSubscription, NetworkClient.#handleDefaultSubscriptionChange),
// the cache's recorded fabric filter adopted from it
// (ClientStructure.adoptFabricFilter), and the reactions serialised against
// Start. matter.js's peer.set({ network: { defaultSubscription } }) is
// SetDefaultSubscription; its interceptSubscribe the node's
// client.InterceptSubscribe seam; its ClientNodeStore record the settings
// key client.SettingCacheFabricFiltered; a nodeLabel change on the device
// is an OnOff change of the light.

// recordingStore wraps the controller's store: it can refuse the record of
// the cache's fabric filter (refusingRecord, :631-656) and a version's
// forgetting (refusingStorage, :744-764).
type recordingStore struct {
	client.Store
	refuseRecord atomic.Bool
	refuseForget atomic.Bool
}

var errStorageRefused = errors.New("storage refused")

func (s *recordingStore) Save(id string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	if cluster == client.SettingsClusterID {
		if _, ok := values[client.SettingCacheFabricFiltered]; ok && s.refuseRecord.Load() {
			return errStorageRefused
		}
	} else if len(values) == 0 && version == nil && s.refuseForget.Load() {
		return errStorageRefused
	}
	return s.Store.Save(id, ep, cluster, values, version)
}

// dropRecord deletes the node's recorded fabric filter from the store —
// `delete storage[nodes.<id>.cache]`.
func (s *recordingStore) dropRecord(t *testing.T, id string) {
	t.Helper()
	if err := s.Store.Save(id, 0, client.SettingsClusterID, map[uint32][]byte{client.SettingCacheFabricFiltered: nil}, nil); err != nil {
		t.Fatal(err)
	}
}

// levelLog records each message with its level.
type levelLog struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *levelLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelLog) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *levelLog) WithGroup(string) slog.Handler            { return h }
func (h *levelLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r)
	h.mu.Unlock()
	return nil
}

// count is the number of msg logged at level since the last reset.
func (h *levelLog) count(level slog.Level, msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for i := range h.recs {
		if h.recs[i].Level == level && h.recs[i].Message == msg {
			n++
		}
	}
	return n
}

func (h *levelLog) reset() {
	h.mu.Lock()
	h.recs = nil
	h.mu.Unlock()
}

// filterPair is a client pair whose controller store is a recordingStore,
// whose log is recorded and whose node's status and alive hooks are
// counted.
type filterPair struct {
	*clientPair
	store    *recordingStore
	log      *levelLog
	mu       sync.Mutex
	statuses []bool
	alive    int
}

func newFilterPair(t *testing.T, autoSubscribe bool) *filterPair {
	t.Helper()
	fp := &filterPair{store: &recordingStore{}, log: &levelLog{}}
	fp.clientPair = newClientPair(t, clientPairOptions{
		commission: client.CommissionOptions{AutoSubscribe: boolPtr(autoSubscribe)},
		controller: fabrictest.ControllerOptions{
			Logger: slog.New(fp.log),
			Registry: func(c *client.RegistryConfig) {
				// fabrictest keeps the edited store across restarts.
				if c.Store != client.Store(fp.store) {
					fp.store.Store = c.Store
					c.Store = fp.store
				}
				c.HooksFor = func(string) client.Hooks {
					return client.Hooks{
						SubscriptionStatusChanged: func(active bool) {
							fp.mu.Lock()
							fp.statuses = append(fp.statuses, active)
							fp.mu.Unlock()
						},
						SubscriptionAlive: func() {
							fp.mu.Lock()
							fp.alive++
							fp.mu.Unlock()
						},
					}
				}
			},
		},
	})
	return fp
}

// hooksSeen returns and forgets the statuses and alive calls counted.
func (fp *filterPair) hooksSeen() (statuses []bool, alive int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	statuses, alive = fp.statuses, fp.alive
	fp.statuses, fp.alive = nil, 0
	return statuses, alive
}

// setDefault is peer.set({ network: { defaultSubscription } }) settled.
func (fp *filterPair) setDefault(t *testing.T, o *client.SubscribeOptions) {
	t.Helper()
	fp.run(t, func(ctx context.Context) error { return fp.node.SetDefaultSubscription(ctx, o) })
}

// setAuto is peer.set({ network: { autoSubscribe } }) settled.
func (fp *filterPair) setAuto(t *testing.T, on bool) {
	t.Helper()
	fp.run(t, func(ctx context.Context) error { return fp.node.SetAutoSubscribe(ctx, on) })
}

// restarted is restartedPeer (:356-361): the controller restarts, the
// node is restored from the store and started (controller.start()).
func (fp *filterPair) restarted(t *testing.T) *client.Node {
	t.Helper()
	id := fp.node.ID()
	if err := fp.pair.Controller.Restart(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	n := fp.pair.Controller.Registry().Get(id)
	if n == nil {
		t.Fatalf("node %q not restored", id)
	}
	fp.run(t, n.Start)
	return n
}

// observeLast observes the node's subscription requests; it returns the
// latest one seen.
func observeLast(n *client.Node) func() (interaction.SubscribeRequest, bool) {
	var mu sync.Mutex
	var last interaction.SubscribeRequest
	seen := false
	client.ObserveRequests(n, func(r interaction.SubscribeRequest) {
		mu.Lock()
		last, seen = r, true
		mu.Unlock()
	})
	return func() (interaction.SubscribeRequest, bool) {
		mu.Lock()
		defer mu.Unlock()
		return last, seen
	}
}

// observeFirst observes the node's subscription requests; it returns the
// first one seen — the request a subscription was made with, whose
// callbacks are the node's (a refreshed request carries none).
func observeFirst(n *client.Node) func() interaction.SubscribeRequest {
	var mu sync.Mutex
	var first interaction.SubscribeRequest
	seen := false
	client.ObserveRequests(n, func(r interaction.SubscribeRequest) {
		mu.Lock()
		if !seen {
			first, seen = r, true
		}
		mu.Unlock()
	})
	return func() interaction.SubscribeRequest {
		mu.Lock()
		defer mu.Unlock()
		return first
	}
}

// onOffOf is the light's OnOff as the node's image holds it.
func (cp *clientPair) onOffOf(n *client.Node) any {
	v, _ := n.Endpoint(cp.lightEP).Cluster(onoff.ClusterID).Decoded(onoff.AttrOnOff)
	return v
}

// switchOn turns the device's light on.
func (cp *clientPair) switchOn(t *testing.T) {
	t.Helper()
	if err := cp.light.OnOff().Set(onoffdef.Patch{OnOff: new(true)}); err != nil {
		t.Fatal(err)
	}
}

func recorded(n *client.Node) string {
	switch f := client.RecordedCacheFabricFiltered(n); {
	case f == nil:
		return "unrecorded"
	case *f:
		return "filtered"
	default:
		return "unfiltered"
	}
}

func TestClientSubscribeDefaultSubscriptionParity(t *testing.T) {
	t.Parallel()

	t.Run("follows the fabric filter the default subscription sends", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, true)
		last := observeLast(fp.node)
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		fp.eventually(t, "the replaced default subscription", fp.node.SubscriptionActive)
		if r, ok := last(); !ok || !r.NoFabricFilter {
			t.Fatalf("the default subscription sent %+v, want it unfiltered", r)
		}
		if client.CacheFabricFiltered(fp.node) {
			t.Fatal("the cache keeps the fabric filter")
		}
		fp.switchOn(t)
		fp.eventually(t, "the change in the image", func() bool { return fp.onOffOf(fp.node) == true })
	})

	t.Run("sends a default subscription given as a built request as it is", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, true)
		last := observeLast(fp.node)
		paths := []client.AttributePath{clusterPath(0, basicinfo.ClusterID)}
		fp.setDefault(t, &client.SubscribeOptions{Attributes: paths, NoFabricFilter: true})
		fp.eventually(t, "the replaced default subscription", fp.node.SubscriptionActive)
		r, _ := last()
		if !slices.Equal(r.Attributes, paths) || !r.NoFabricFilter {
			t.Fatalf("the default subscription sent %+v unfiltered=%v, want %+v unfiltered", r.Attributes, r.NoFabricFilter, paths)
		}
		if client.CacheFabricFiltered(fp.node) {
			t.Fatal("the cache keeps the fabric filter")
		}
	})

	t.Run("forgets data versions when the default subscription's fabric filter changes", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		if !slices.Contains(filteredClusters(fp.node, true), basicinfo.ClusterID) {
			t.Fatal("no BasicInformation version before the switch")
		}
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		if f := filteredClusters(fp.node, false); len(f) != 0 {
			t.Fatalf("filters %v after the switch, want none", f)
		}
		if got := recorded(fp.node); got != "unfiltered" {
			t.Fatalf("record %s, want unfiltered", got)
		}
		if v, _ := fp.node.Endpoint(0).Cluster(basicinfo.ClusterID).Decoded(basicinfo.AttrVendorId); fmt.Sprint(v) != "65521" {
			t.Fatalf("VendorID %v, want the value kept", v)
		}
	})

	t.Run("forgets a version known only to storage when the fabric filter changes", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		onOffOf := func(n *client.Node, filtered bool) []im.DataVersionFilter {
			return client.KnownVersions(n, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{clusterPath(fp.lightEP, onoff.ClusterID)}, NoFabricFilter: !filtered})
		}
		if len(onOffOf(fp.node, true)) != 1 {
			t.Fatal("no OnOff version before the switch")
		}
		// As loading a cluster with unlisted values does: the stored
		// version stays.
		client.InvalidateVersion(fp.node, fp.lightEP, onoff.ClusterID)
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		if f := onOffOf(fp.restarted(t), false); len(f) != 0 {
			t.Fatalf("filters %v after the restart, want none", f)
		}
	})

	t.Run("forgets data versions cached under another fabric filter on restart", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		if err := client.RecordCacheFabricFiltered(fp.node, false); err != nil {
			t.Fatal(err)
		}
		n := fp.restarted(t)
		if f := filteredClusters(n, true); len(f) != 0 {
			t.Fatalf("filters %v after the restart, want none", f)
		}
		if got := recorded(n); got != "filtered" {
			t.Fatalf("record %s, want filtered", got)
		}
		if v, _ := n.Endpoint(0).Cluster(basicinfo.ClusterID).Decoded(basicinfo.AttrVendorId); fmt.Sprint(v) != "65521" {
			t.Fatalf("VendorID %v, want the value kept", v)
		}
	})

	t.Run("keeps forgotten data versions unknown across a restart", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		if got := recorded(fp.node); got != "unfiltered" {
			t.Fatalf("record %s before the restart, want unfiltered", got)
		}
		n := fp.restarted(t)
		if got := recorded(n); got != "unfiltered" {
			t.Fatalf("record %s after the restart, want unfiltered", got)
		}
		if f := filteredClusters(n, false); len(f) != 0 {
			t.Fatalf("filters %v after the restart, want none", f)
		}
	})

	t.Run("keeps data versions of a node stored without a fabric filter record and records the filter", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		before := filteredClusters(fp.node, true)
		if recorded(fp.node) == "unrecorded" {
			t.Fatal("no record after the start")
		}
		fp.store.dropRecord(t, fp.node.ID())
		n := fp.restarted(t)
		if after := filteredClusters(n, true); !slices.Equal(after, before) {
			t.Fatalf("filters %v after the restart, want %v", after, before)
		}
		if got := recorded(n); got != "filtered" {
			t.Fatalf("record %s, want filtered", got)
		}
	})

	t.Run("warns and keeps the cache usable when the first record cannot be written", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		before := filteredClusters(fp.node, true)
		fp.store.dropRecord(t, fp.node.ID())
		fp.store.refuseRecord.Store(true)
		fp.log.reset()
		n := fp.restarted(t)
		fp.store.refuseRecord.Store(false)
		if w := fp.log.count(slog.LevelWarn, "matter.controller.client.fabric_filter_record_failed"); w != 1 {
			t.Fatalf("%d record warnings, want 1", w)
		}
		if e := fp.log.count(slog.LevelError, "matter.controller.client.fabric_filter_switch_failed"); e != 0 {
			t.Fatalf("%d switch errors, want none", e)
		}
		if got := recorded(n); got != "unrecorded" {
			t.Fatalf("record %s, want none", got)
		}
		if !client.CacheFabricFiltered(n) {
			t.Fatal("the cache is not fabric filtered")
		}
		if after := filteredClusters(n, true); !slices.Equal(after, before) {
			t.Fatalf("filters %v after the restart, want %v", after, before)
		}
	})

	t.Run("logs an error when the switched filter cannot be recorded", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		if err := client.RecordCacheFabricFiltered(fp.node, false); err != nil {
			t.Fatal(err)
		}
		fp.store.refuseRecord.Store(true)
		fp.log.reset()
		n := fp.restarted(t)
		fp.store.refuseRecord.Store(false)
		if e := fp.log.count(slog.LevelError, "matter.controller.client.fabric_filter_switch_failed"); e != 1 {
			t.Fatalf("%d switch errors, want 1", e)
		}
		if w := fp.log.count(slog.LevelWarn, "matter.controller.client.fabric_filter_record_failed"); w != 0 {
			t.Fatalf("%d record warnings, want none", w)
		}
		if client.CacheFabricFiltered(n) {
			t.Fatal("the cache took the default subscription's filter")
		}
	})

	t.Run("keeps the old filter and still subscribes after a runtime change", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, true)
		last := observeLast(fp.node)
		fp.store.refuseForget.Store(true)
		fp.log.reset()
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		fp.store.refuseForget.Store(false)
		if e := fp.log.count(slog.LevelError, "matter.controller.client.fabric_filter_switch_failed"); e != 1 {
			t.Fatalf("%d switch errors, want 1", e)
		}
		if !client.CacheFabricFiltered(fp.node) {
			t.Fatal("the cache lost its filter")
		}
		fp.eventually(t, "the replaced default subscription", fp.node.SubscriptionActive)
		if r, _ := last(); !r.NoFabricFilter {
			t.Fatal("the default subscription is fabric filtered")
		}
		fp.switchOn(t)
		fp.run(t, func(ctx context.Context) error {
			_, err := fp.node.Read(ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Attributes: []client.AttributePath{clusterPath(fp.lightEP, onoff.ClusterID)}}, IncludeKnownVersions: true})
			return err
		})
		if v := fp.onOffOf(fp.node); v != true {
			t.Fatalf("OnOff %v after the filtered read, want true", v)
		}
	})

	t.Run("keeps the old filter and still starts the node", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		if err := client.RecordCacheFabricFiltered(fp.node, false); err != nil {
			t.Fatal(err)
		}
		fp.store.refuseForget.Store(true)
		fp.log.reset()
		n := fp.restarted(t)
		fp.store.refuseForget.Store(false)
		if e := fp.log.count(slog.LevelError, "matter.controller.client.fabric_filter_switch_failed"); e != 1 {
			t.Fatalf("%d switch errors, want 1", e)
		}
		if err := n.LoadError(); err != nil {
			t.Fatalf("the node did not load: %v", err)
		}
		// Started: it reads (ClientNode's lifecycle Active).
		fp.run(t, func(ctx context.Context) error {
			_, err := n.Read(ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Attributes: []client.AttributePath{clusterPath(0, basicinfo.ClusterID)}}})
			return err
		})
		if client.CacheFabricFiltered(n) {
			t.Fatal("the cache took the default subscription's filter")
		}
	})

	t.Run("ignores callbacks of a replaced default subscription", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		first := observeFirst(fp.node)
		fp.setAuto(t, true)
		fp.eventually(t, "the default subscription", fp.node.SubscriptionActive)
		replaced := first()
		fp.setDefault(t, &client.SubscribeOptions{NoFabricFilter: true})
		fp.eventually(t, "the replacement", fp.node.SubscriptionActive)
		successor := client.DefaultSubscriptionOf(fp.node)
		fp.hooksSeen()
		_ = replaced.Updated(&interaction.Report{})
		replaced.KeepaliveReceived()
		replaced.Closed()
		replaced.ActiveChanged(true)
		if statuses, alive := fp.hooksSeen(); alive != 0 || len(statuses) != 0 {
			t.Fatalf("the replaced subscription's callbacks acted: alive %d, statuses %v", alive, statuses)
		}
		if client.DefaultSubscriptionOf(fp.node) != successor {
			t.Fatal("the successor is no longer the default subscription")
		}
	})

	t.Run("does not install a default subscription that closed while it was established", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		fp.hooksSeen()
		var created []*interaction.Subscription
		client.InterceptSubscribe(fp.node, func(r interaction.SubscribeRequest, subscribe func() (*interaction.Subscription, error)) (*interaction.Subscription, error) {
			s, err := subscribe()
			if err == nil {
				created = append(created, s)
				r.Closed()
			}
			return s, err
		})
		fp.setAuto(t, true)
		client.InterceptSubscribe(fp.node, nil)
		if len(created) != 1 {
			t.Fatalf("%d subscriptions created, want 1", len(created))
		}
		fp.eventually(t, "the closed subscription done", func() bool {
			select {
			case <-created[0].Done():
				return true
			default:
				return false
			}
		})
		if client.DefaultSubscriptionOf(fp.node) != nil {
			t.Fatal("the closed subscription was installed")
		}
		if statuses, _ := fp.hooksSeen(); len(statuses) != 0 {
			t.Fatalf("statuses %v, want none", statuses)
		}
		fp.setAuto(t, false)
		fp.setAuto(t, true)
		fp.eventually(t, "the default subscription", fp.node.SubscriptionActive)
	})

	t.Run("subscribes at the next start after a default subscription failed to start", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		client.InterceptSubscribe(fp.node, func(interaction.SubscribeRequest, func() (*interaction.Subscription, error)) (*interaction.Subscription, error) {
			return nil, errors.New("subscribe refused")
		})
		if err := fp.site.Run(parityCtx(t), func(ctx context.Context) error { return fp.node.SetAutoSubscribe(ctx, true) }); err == nil {
			t.Fatal("the refused subscribe succeeded")
		}
		client.InterceptSubscribe(fp.node, nil)
		if client.DefaultSubscriptionOwned(fp.node) {
			t.Fatal("the refused default subscription is still owned")
		}
		fp.run(t, fp.node.Stop)
		fp.run(t, fp.node.Start)
		fp.eventually(t, "the default subscription", fp.node.SubscriptionActive)
	})

	t.Run("runs startup and the reaction to defaultSubscription one at a time", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, true)
		fp.run(t, fp.node.Stop)
		var mu sync.Mutex
		var sent []bool
		var created []*interaction.Subscription
		release := make(chan struct{})
		client.InterceptSubscribe(fp.node, func(r interaction.SubscribeRequest, subscribe func() (*interaction.Subscription, error)) (*interaction.Subscription, error) {
			mu.Lock()
			call := len(sent)
			sent = append(sent, !r.NoFabricFilter)
			mu.Unlock()
			s, err := subscribe()
			if err == nil {
				mu.Lock()
				created = append(created, s)
				mu.Unlock()
			}
			if call == 0 {
				<-release
			}
			return s, err
		})
		sentNow := func() []bool {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(sent)
		}
		var done atomic.Int32
		go func() { defer done.Add(1); _ = fp.node.Start(parityCtx(t)) }()
		fp.eventually(t, "the start's subscribe", func() bool { return len(sentNow()) == 1 })
		go func() {
			defer done.Add(1)
			_ = fp.node.SetDefaultSubscription(parityCtx(t), &client.SubscribeOptions{NoFabricFilter: true})
		}()
		fp.advance(t, advanceStep)
		if got := sentNow(); !slices.Equal(got, []bool{true}) {
			t.Fatalf("sent %v while the start is held, want [true]", got)
		}
		close(release)
		fp.eventually(t, "both reactions", func() bool { return done.Load() == 2 })
		client.InterceptSubscribe(fp.node, nil)
		if got := sentNow(); !slices.Equal(got, []bool{true, false}) {
			t.Fatalf("sent %v, want [true false]", got)
		}
		mu.Lock()
		installed := created[1]
		mu.Unlock()
		if client.DefaultSubscriptionOf(fp.node) != installed {
			t.Fatal("the second subscription is not the default one")
		}
		fp.eventually(t, "the installed subscription", installed.Active)
	})

	t.Run("runs the reactions to autoSubscribe and defaultSubscription one at a time", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		var mu sync.Mutex
		var sent []bool
		var created []*interaction.Subscription
		release := make(chan struct{})
		client.InterceptSubscribe(fp.node, func(r interaction.SubscribeRequest, subscribe func() (*interaction.Subscription, error)) (*interaction.Subscription, error) {
			mu.Lock()
			call := len(sent)
			sent = append(sent, !r.NoFabricFilter)
			mu.Unlock()
			s, err := subscribe()
			if err == nil {
				mu.Lock()
				created = append(created, s)
				mu.Unlock()
			}
			if call == 0 {
				<-release
			}
			return s, err
		})
		sentNow := func() []bool {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(sent)
		}
		var done atomic.Int32
		go func() { defer done.Add(1); _ = fp.node.SetAutoSubscribe(parityCtx(t), true) }()
		fp.eventually(t, "the first reaction's subscribe", func() bool { return len(sentNow()) == 1 })
		go func() {
			defer done.Add(1)
			_ = fp.node.SetDefaultSubscription(parityCtx(t), &client.SubscribeOptions{NoFabricFilter: true})
		}()
		fp.advance(t, advanceStep)
		if got := sentNow(); !slices.Equal(got, []bool{true}) {
			t.Fatalf("sent %v while the first reaction is held, want [true]", got)
		}
		close(release)
		fp.eventually(t, "both reactions", func() bool { return done.Load() == 2 })
		client.InterceptSubscribe(fp.node, nil)
		if got := sentNow(); !slices.Equal(got, []bool{true, false}) {
			t.Fatalf("sent %v, want [true false]", got)
		}
		mu.Lock()
		superseded, installed := created[0], created[1]
		mu.Unlock()
		if client.DefaultSubscriptionOf(fp.node) != installed {
			t.Fatal("the second subscription is not the default one")
		}
		fp.eventually(t, "the superseded subscription done", func() bool {
			select {
			case <-superseded.Done():
				return true
			default:
				return false
			}
		})
		fp.eventually(t, "the installed subscription", installed.Active)
	})

	t.Run("reports not subscribed when an active default subscription ends", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		first := observeFirst(fp.node)
		fp.setAuto(t, true)
		fp.eventually(t, "the default subscription", fp.node.SubscriptionActive)
		r := first()
		fp.hooksSeen()
		// Ends as an abort that lands before the subscription reported
		// itself not live would.
		r.Closed()
		fp.eventually(t, "the status hooks", func() bool {
			fp.mu.Lock()
			defer fp.mu.Unlock()
			return len(fp.statuses) > 0
		})
		fp.advance(t, advanceStep)
		if statuses, _ := fp.hooksSeen(); !slices.Equal(statuses, []bool{false}) {
			t.Fatalf("statuses %v, want [false]", statuses)
		}
		if client.DefaultSubscriptionOf(fp.node) != nil {
			t.Fatal("the ended subscription is still the default one")
		}
	})

	basicInformationRequest := func() client.SubscribeRequest {
		return client.SubscribeRequest{
			SubscribeRequest:     client.SubscribePaths{Attributes: []client.AttributePath{clusterPath(0, basicinfo.ClusterID)}, Sustain: true},
			IncludeKnownVersions: true,
		}
	}
	basicInformationFilters := func(n *client.Node) []im.DataVersionFilter {
		return client.KnownVersions(n, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{clusterPath(0, basicinfo.ClusterID)}})
	}

	t.Run("records no data version for a report of a subscription opened before a switch", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		var s *interaction.Subscription
		fp.run(t, func(ctx context.Context) (err error) {
			s, err = fp.node.Subscribe(ctx, basicInformationRequest())
			return err
		})
		defer s.Close()
		fp.eventually(t, "the subscription", s.Active)
		for _, f := range []bool{false, true} {
			if err := client.AdoptFabricFilter(fp.node, f); err != nil {
				t.Fatal(err)
			}
		}
		if err := write(t, fp.clientPair, client.WriteRequest{Path: client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel), Value: spec.String("relabeled")}); err != nil {
			t.Fatal(err)
		}
		fp.eventually(t, "the relabel in the image", func() bool {
			v, _ := fp.node.Endpoint(0).Cluster(basicinfo.ClusterID).Decoded(basicinfo.AttrNodeLabel)
			return v == "relabeled"
		})
		fp.advance(t, advanceStep)
		s.Close()
		if f := basicInformationFilters(fp.node); len(f) != 0 {
			t.Fatalf("filters %v, want none", f)
		}
	})

	t.Run("records data versions again once a subscription opened before a switch is replaced", func(t *testing.T) {
		t.Parallel()
		fp := newFilterPair(t, false)
		var s *interaction.Subscription
		fp.run(t, func(ctx context.Context) (err error) {
			s, err = fp.node.Subscribe(ctx, basicInformationRequest())
			return err
		})
		defer s.Close()
		fp.eventually(t, "the subscription", s.Active)
		for _, f := range []bool{false, true} {
			if err := client.AdoptFabricFilter(fp.node, f); err != nil {
				t.Fatal(err)
			}
		}
		if f := basicInformationFilters(fp.node); len(f) != 0 {
			t.Fatalf("filters %v after the switches, want none", f)
		}
		// matter.js closes the device's sessions; the port loses the
		// underlying subscription on the controller, which the sustained
		// subscription replaces.
		old := s.ID()
		subs := fabrictestseam.Subscriptions(fp.pair.Controller)
		subs.CloseForPeer(subs.Active()[0].Peer())
		fp.eventually(t, "the subscription replaced", func() bool { return s.Active() && s.ID() != old })
		fp.advance(t, advanceStep)
		s.Close()
		if f := basicInformationFilters(fp.node); len(f) == 0 {
			t.Fatal("no version recorded after the replacement")
		}
	})

	failing := func(t *testing.T, changed bool, updated func(*interaction.Report) error) {
		t.Helper()
		cp, log := newCancelPair(t)
		if changed {
			// The client holds a DataVersion for every cluster, so an
			// unchanged device primes with no data; a value changed on
			// the device since makes the priming report carry it.
			if err := cp.pair.Device.Act(func(n *node.Node) error { return n.SetNodeLabel(t.Context(), "changed") }); err != nil {
				t.Fatal(err)
			}
		}
		before := cp.pair.Device.Node().Subscriptions().Active()
		req := interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}, Updated: updated}
		var err error
		cp.run(t, func(ctx context.Context) error {
			_, err = cp.node.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: req})
			return nil
		})
		if !errors.Is(err, errSubscribeRefused) {
			t.Fatalf("subscribe: %v, want the callback's failure", err)
		}
		cp.eventually(t, "the priming report answered INVALID_SUBSCRIPTION", func() bool { return log.has(abandonedTerminated) })
		cp.eventually(t, "the device's subscriptions", func() bool { return cp.pair.Device.Node().Subscriptions().Active() == before })
	}

	t.Run("answers the priming report with INVALID_SUBSCRIPTION when processing fails before reading it", func(t *testing.T) {
		t.Parallel()
		failing(t, false, func(*interaction.Report) error { return errSubscribeRefused })
	})

	t.Run("answers the priming report with INVALID_SUBSCRIPTION when processing it fails", func(t *testing.T) {
		t.Parallel()
		failing(t, true, func(r *interaction.Report) error {
			if len(r.Attributes) > 0 {
				return errSubscribeRefused
			}
			return nil
		})
	})
}

// errSubscribeRefused is SubscribeRefusedError("priming refused").
var errSubscribeRefused = errors.New("priming refused")

// newCancelPair is the setup of TestClientSubscribeCancelledParity.
func newCancelPair(t *testing.T) (*clientPair, *msgLog) {
	t.Helper()
	log := &msgLog{}
	cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Logger: slog.New(log)}})
	return cp, log
}
