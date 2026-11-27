// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/cluster/spec"
	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	onoffdef "thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
)

// Issue #589, decisions 592–595, 604, 618: a reassembly that serves a new
// instance behind a key the topology keeps gives every cluster of the
// endpoint a new DataVersion and announces every readable attribute to the
// running subscriptions, as matter.js does for a re-added endpoint (a new
// Datasource per behaviour, Datasource.ts:763; addCluster emits every
// readable attribute, ProtocolService.ts:290-296, at db91886c). The
// key-scoped state a controller wrote stays with the key.

// announcePlug builds the plug of the issue's reproduction under key.
func announcePlug(t *testing.T, key, sw string, on bool) *device.OnOffPlugInUnit {
	t.Helper()
	d, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey(key), Name: "Plug"},
		bdbidef.Declare{Initial: bdbidef.Patch{SoftwareVersionString: &sw}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.OnOff().Set(onoffdef.Patch{OnOff: &on}); err != nil {
		t.Fatal(err)
	}
	return d
}

// announceRig is a commissioned pair serving the plug, with a wildcard
// subscription on the plug's endpoint, primed.
type announceRig struct {
	site *fabrictest.Site
	pair *fabrictest.Pair
	n    *node.Node
	ep   uint16
	mu   sync.Mutex
	got  []*interaction.Report
	// primed is the number of reports when the subscription was set up;
	// versions the DataVersions the priming report carried, per cluster.
	primed   int
	versions map[uint32]uint32
}

func newAnnounceRig(ctx context.Context, t *testing.T, served ...node.Endpoint) *announceRig {
	t.Helper()
	r := &announceRig{site: quietSite(t), versions: map[uint32]uint32{}}
	r.pair = r.site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: served}})
	r.n = r.pair.Device.Node()
	ep, ok := r.number("plug")
	if !ok {
		t.Fatal("the plug has no endpoint number")
	}
	r.ep = ep
	// Every endpoint: a key served anew under another number is reported
	// too (Forget + Add).
	sub, err := fabrictest.Subscribe(ctx, r.pair, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{{}},
		Updated: func(rep *interaction.Report) error {
			r.mu.Lock()
			r.got = append(r.got, rep)
			r.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	if err := r.site.Step(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.primed = len(r.got)
	for _, rep := range r.got {
		for i := range rep.Attributes {
			if a := &rep.Attributes[i]; a.Path.Endpoint == r.ep {
				r.versions[a.Path.Cluster] = a.DataVersion
			}
		}
	}
	if r.primed == 0 || r.versions[onoffdef.ClusterID] == 0 {
		t.Fatalf("no priming report with OnOff (%d reports)", r.primed)
	}
	return r
}

// number is the endpoint number the node serves key at.
func (r *announceRig) number(key string) (uint16, bool) {
	devices := r.n.Topology().Devices
	for i := range devices {
		if devices[i].Key.String() == key {
			return devices[i].Endpoint, true
		}
	}
	return 0, false
}

// since returns the attributes of the plug's endpoint reported after
// priming, by cluster and attribute, and how many reports carried one.
func (r *announceRig) since(ctx context.Context, t *testing.T) (reports int, attrs map[[2]uint32]interaction.AttributeData) {
	t.Helper()
	if err := r.site.Step(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attrs = map[[2]uint32]interaction.AttributeData{}
	for _, rep := range r.got[r.primed:] {
		carried := false
		for i := range rep.Attributes {
			if a := rep.Attributes[i]; a.Path.Endpoint == r.ep {
				attrs[[2]uint32{a.Path.Cluster, a.Path.Attribute}] = a
				carried = true
			}
		}
		if carried {
			reports++
		}
	}
	return reports, attrs
}

// filtered reads the plug's OnOff cluster with a DataVersionFilter at the
// priming report's version: true when the device answered the cluster.
func (r *announceRig) filtered(ctx context.Context, t *testing.T) bool {
	t.Helper()
	rep, err := fabrictest.ReadWith(ctx, r.pair, fabrictest.ReadRequest{
		Attributes:         []im.ConcreteAttributePath{{Endpoint: r.ep, HasEndpoint: true, Cluster: onoffdef.ClusterID, HasCluster: true}},
		DataVersionFilters: []im.DataVersionFilter{{Endpoint: r.ep, Cluster: onoffdef.ClusterID, DataVersion: r.versions[onoffdef.ClusterID]}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return len(rep.Attributes) > 0
}

// wantAnnounced checks the issue's acceptance: a report after priming
// carrying the new OnOff and SoftwareVersionString, every reported cluster
// at a new DataVersion, and a filtered read at the old version answering
// OnOff.
func (r *announceRig) wantAnnounced(ctx context.Context, t *testing.T) {
	t.Helper()
	n, attrs := r.since(ctx, t)
	if n == 0 {
		t.Fatal("no report after a same-key change of instance that changed OnOff and SoftwareVersionString")
	}
	if a, ok := attrs[[2]uint32{onoffdef.ClusterID, onoffdef.AttrOnOff}]; !ok || !a.Value.El.Bool {
		t.Errorf("OnOff after the change: %+v (reported %v), want true", a.Value, ok)
	}
	if a, ok := attrs[[2]uint32{bdbidef.ClusterID, bdbidef.AttrSoftwareVersionString}]; !ok || a.Value.El.String != "2.0" {
		t.Errorf("SoftwareVersionString after the change: %+v (reported %v), want 2.0", a.Value, ok)
	}
	for k := range attrs {
		if v := attrs[k].DataVersion; v == r.versions[k[0]] {
			t.Errorf("cluster 0x%04x reported at its old DataVersion %d", k[0], v)
		}
	}
	if !r.filtered(ctx, t) {
		t.Error("a read filtered at OnOff's pre-change DataVersion answered no OnOff cluster")
	}
}

// wantQuiet checks that the plug's endpoint reported nothing and kept its
// DataVersions.
func (r *announceRig) wantQuiet(ctx context.Context, t *testing.T, what string) {
	t.Helper()
	if n, attrs := r.since(ctx, t); n != 0 {
		t.Errorf("%s: %d reports after priming (%v), want none", what, n, attrs)
	}
	if r.filtered(ctx, t) {
		t.Errorf("%s: OnOff's DataVersion moved", what)
	}
}

func (r *announceRig) batch(ctx context.Context, t *testing.T, fn func(b *node.Batch) error) {
	t.Helper()
	if err := r.n.Aggregator().Batch(ctx, fn); err != nil {
		t.Fatal(err)
	}
}

// TestReplaceAnnouncesTheNewInstance is issue #589's reproduction: a
// same-key Batch.Replace whose device serves other values reports them
// and moves the DataVersions.
func TestReplaceAnnouncesTheNewInstance(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false))
	b := announcePlug(t, "plug", "2.0", true)
	r.batch(ctx, t, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), b) })
	if ep, ok := b.EndpointNumber(); !ok || ep != r.ep {
		t.Fatalf("the replacement serves %d (%v), want %d", ep, ok, r.ep)
	}
	r.wantAnnounced(ctx, t)
}

// TestRemoveAndAddOfOneKeyInOneBatchAnnounces: a Remove and an Add of the
// same key in one batch serve a new instance under the kept number — the
// trigger is the instance, not the call (decision 595).
func TestRemoveAndAddOfOneKeyInOneBatchAnnounces(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false))
	b := announcePlug(t, "plug", "2.0", true)
	r.batch(ctx, t, func(bt *node.Batch) error {
		return errors.Join(bt.Remove(device.StringKey("plug")), bt.Add(b))
	})
	if ep, ok := b.EndpointNumber(); !ok || ep != r.ep {
		t.Fatalf("the new device serves %d (%v), want %d", ep, ok, r.ep)
	}
	r.wantAnnounced(ctx, t)
}

// TestForgetAndAddOfOneKeyInOneBatchAnnounces: a Forget and an Add of the
// same key in one batch serve a new instance; its clusters are reported
// at new DataVersions as well.
func TestForgetAndAddOfOneKeyInOneBatchAnnounces(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false))
	b := announcePlug(t, "plug", "2.0", true)
	r.batch(ctx, t, func(bt *node.Batch) error {
		return errors.Join(bt.Forget(device.StringKey("plug")), bt.Add(b))
	})
	ep, ok := b.EndpointNumber()
	if !ok {
		t.Fatal("the new device is not served")
	}
	// The forgotten key starts as a new device would, under a new number
	// (decision 560, R3-store-3702); its endpoint is announced as added,
	// at DataVersions of its own.
	if ep == r.ep {
		t.Fatalf("Forget + Add serves the old number %d", ep)
	}
	r.ep = ep
	r.versions = map[uint32]uint32{}
	n, attrs := r.since(ctx, t)
	if n == 0 {
		t.Fatal("no report of the endpoint Forget + Add serves")
	}
	if a, ok := attrs[[2]uint32{bdbidef.ClusterID, bdbidef.AttrSoftwareVersionString}]; !ok || a.Value.El.String != "2.0" {
		t.Errorf("SoftwareVersionString of the new endpoint: %+v (reported %v), want 2.0", a.Value, ok)
	}
}

// TestSameInstanceReplaceAnnouncesNothing: a Replace by the endpoint the
// key serves is a no-op — no report, no DataVersion moved.
func TestSameInstanceReplaceAnnouncesNothing(t *testing.T) {
	ctx := parityCtx(t)
	a := announcePlug(t, "plug", "1.0", false)
	r := newAnnounceRig(ctx, t, a)
	r.batch(ctx, t, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), a) })
	if err := r.n.Aggregator().Replace(ctx, device.StringKey("plug"), a); err != nil {
		t.Fatal(err)
	}
	r.wantQuiet(ctx, t, "a Replace by the served instance")
}

// TestUnrelatedReassemblyKeepsDataVersions: a sibling added and removed
// reassembles, and the plug's endpoint keeps its DataVersions and
// reports nothing — for a plug of a value that cannot be compared too,
// which is never "the same instance" by sameInstance but was not replaced.
func TestUnrelatedReassemblyKeepsDataVersions(t *testing.T) {
	for _, tc := range []struct {
		name string
		mk   func(t *testing.T) node.Endpoint
	}{
		{"device", func(t *testing.T) node.Endpoint { t.Helper(); return announcePlug(t, "plug", "1.0", false) }},
		{"uncomparable value", func(t *testing.T) node.Endpoint {
			t.Helper()
			return valueEndpoint{s: announcePlug(t, "plug", "1.0", false).Spec(), tags: []string{"host"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := parityCtx(t)
			r := newAnnounceRig(ctx, t, tc.mk(t))
			lamp, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("lamp"), Name: "lamp"})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.n.Aggregator().Add(ctx, lamp); err != nil {
				t.Fatal(err)
			}
			r.wantQuiet(ctx, t, "a sibling added")
			if err := r.n.Aggregator().Remove(ctx, device.StringKey("lamp")); err != nil {
				t.Fatal(err)
			}
			r.wantQuiet(ctx, t, "a sibling removed")
		})
	}
}

// TestReplaceByAValueThatCannotBeComparedAnnounces: a Replace by an
// endpoint whose value cannot be compared is a new instance (H9's
// sameInstance, decision 618), so it is announced.
func TestReplaceByAValueThatCannotBeComparedAnnounces(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, valueEndpoint{s: announcePlug(t, "plug", "1.0", false).Spec(), tags: []string{"old"}})
	b := valueEndpoint{s: announcePlug(t, "plug", "2.0", true).Spec(), tags: []string{"new"}}
	r.batch(ctx, t, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), b) })
	r.wantAnnounced(ctx, t)
}

// TestRefusedReplaceAnnouncesNothing: a Replace the batch refuses at
// commit serves nothing new and announces nothing (decisions 550, 560).
func TestRefusedReplaceAnnouncesNothing(t *testing.T) {
	ctx := parityCtx(t)
	other := announcePlug(t, "other", "1.0", false)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false), other)
	// The replacement carries a part whose key "other" serves: held, and
	// refused at commit as "other" still serves it.
	s := announcePlug(t, "plug", "2.0", true).Spec()
	s.Parts = []endpoint.Spec{announcePlug(t, "other", "1.0", false).Spec()}
	b := &specEndpoint{s: s}
	err := r.n.Aggregator().Batch(ctx, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), b) })
	if !errors.Is(err, node.ErrDuplicateKey) {
		t.Fatalf("Replace colliding with a served key: %v, want ErrDuplicateKey", err)
	}
	r.wantQuiet(ctx, t, "a refused Replace")
}

// TestReplaceKeepsTheControllerWrittenLabel: the NodeLabel a controller
// wrote stays with the key and the new instance serves it, in the
// announce like every other attribute (decisions 594, 604).
func TestReplaceKeepsTheControllerWrittenLabel(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false))
	label := lifecycleAttr(r.ep, bdbidef.ClusterID, bdbidef.AttrNodeLabel)
	if err := fabrictest.Write(ctx, r.pair, label, spec.String("kitchen")); err != nil {
		t.Fatal(err)
	}
	if err := r.site.Step(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.primed = len(r.got)
	r.mu.Unlock()
	b := announcePlug(t, "plug", "2.0", true)
	r.batch(ctx, t, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), b) })
	_, attrs := r.since(ctx, t)
	if a, ok := attrs[[2]uint32{bdbidef.ClusterID, bdbidef.AttrNodeLabel}]; !ok || a.Value.El.String != "kitchen" {
		t.Errorf("NodeLabel in the announce: %+v (reported %v), want kitchen", a.Value, ok)
	}
	got, err := fabrictest.Read(ctx, r.pair, label)
	if err != nil || got.Value.El.String != "kitchen" {
		t.Errorf("NodeLabel read after the Replace: %+v, %v; want kitchen", got.Value, err)
	}
}

// specEndpoint is a host Endpoint serving a fixed Spec, by pointer.
type specEndpoint struct{ s endpoint.Spec }

func (e *specEndpoint) Spec() endpoint.Spec { return e.s }

// blockingOnOff is an OnOff server whose On waits for release once it
// entered.
type blockingOnOff struct {
	*onoff.Server
	entered chan struct{}
	release chan struct{}
}

func (b *blockingOnOff) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID == onoffdef.CmdOn {
		close(b.entered)
		<-b.release
	}
	return b.Server.MatterInvoke(ctx, cmdID, fields)
}

// TestInvokeInFlightAtTheSwapCompletesOnTheOldInstance (decision 593): an
// invoke the old instance is handling when a same-key Replace swaps in a
// new one completes there and answers success; the announce of the new
// instance follows, with the new instance's values.
func TestInvokeInFlightAtTheSwapCompletesOnTheOldInstance(t *testing.T) {
	ctx := parityCtx(t)
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	host := &blockingOnOff{Server: srv, entered: make(chan struct{}), release: make(chan struct{})}
	sw := "1.0"
	old, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "Plug"},
		bdbidef.Declare{Initial: bdbidef.Patch{SoftwareVersionString: &sw}}, onoffdef.Declare{HostServer: host})
	if err != nil {
		t.Fatal(err)
	}
	r := newAnnounceRig(ctx, t, old)
	done := make(chan error, 1)
	go func() {
		_, err := fabrictest.Invoke(ctx, r.pair, cmdPath(r.ep, onoffdef.ClusterID, onoffdef.CmdOn), nil)
		done <- err
	}()
	select {
	case <-host.entered:
	case <-ctx.Done():
		t.Fatal("the On never reached the old instance")
	}
	b := announcePlug(t, "plug", "2.0", false)
	r.batch(ctx, t, func(bt *node.Batch) error { return bt.Replace(device.StringKey("plug"), b) })
	close(host.release)
	if err := <-done; err != nil {
		t.Fatalf("the On in flight at the swap: %v, want success", err)
	}
	if on, _ := srv.MatterRead(onoffdef.AttrOnOff); on != true {
		t.Errorf("the old instance's OnOff after its On: %v, want true", on)
	}
	n, attrs := r.since(ctx, t)
	if n == 0 {
		t.Fatal("no announce after the swap")
	}
	if a, ok := attrs[[2]uint32{bdbidef.ClusterID, bdbidef.AttrSoftwareVersionString}]; !ok || a.Value.El.String != "2.0" {
		t.Errorf("SoftwareVersionString after the swap: %+v (reported %v), want 2.0", a.Value, ok)
	}
	got, err := fabrictest.Read(ctx, r.pair, lifecycleAttr(r.ep, onoffdef.ClusterID, onoffdef.AttrOnOff))
	if err != nil || got.Value.El.Bool {
		t.Errorf("OnOff served after the swap: %+v, %v; want the new instance's false", got.Value, err)
	}
}

// TestReplaceUnderARefusingRequestAnnounces (R5-par-5106): a same-key
// Replace whose ctx carries a request's access check that refuses every
// path — a host replacing a device from a command handler, with the
// handler's ctx — still announces the new instance to the subscriber: the
// caller's request does not decide what the subscriptions are told.
func TestReplaceUnderARefusingRequestAnnounces(t *testing.T) {
	ctx := parityCtx(t)
	r := newAnnounceRig(ctx, t, announcePlug(t, "plug", "1.0", false))
	b := announcePlug(t, "plug", "2.0", true)
	refusing := im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusUnsupportedAccess })
	if err := r.n.Aggregator().Replace(refusing, device.StringKey("plug"), b); err != nil {
		t.Fatal(err)
	}
	r.wantAnnounced(ctx, t)
}
