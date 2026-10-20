// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/binding/BindingIntegrationTest.ts
// at 862ad75f: the group kind of "Binding integration" and the describe
// "Binding integration, group sends". The client kind written by an
// administrator and the self-binding are node:TestLiveBinding and
// binding:TestDormantThenWoken.
//
// The describe "kind=client with a default subscription of one attribute"
// (:352-474) commissions a switch (OnOffLightSwitchDevice on a root with
// ControllerBehavior) and a light onto one controller, grants any CASE
// subject of the fabric Operate on the light, binds the switch's endpoint
// to the light's and sets a sustained default subscription of the light's
// onOff on the bound ClientNode (boundLight :62-174, subscribeToOnOff
// :180-219). The port does the same on fabrictest: the switch is a
// bqSwitch whose node runs its client stack on the site (node.Client), the
// light a second device commissioned by the switch's controller, and the
// default subscription is what NetworkClient builds (NetworkClient.ts:168-210):
// a sustained subscription through the resolution's interaction. The
// switch's onOff$Changed is the subscription's reports; matter.js's
// isolation (router.intercept) is Network.Filter dropping every datagram
// from or to the light's address.
//
// matter.js runs these on one MockServerNode with an OnOffLightSwitch (the
// group sends with GroupsClient declared too) and an added fabric, writes
// the switch's Binding list locally and provisions the group key on the
// fabric; the port does the same on a commissioned fabrictest device
// (bqSwitch, binding_manager_parity_matterjs_test.go). The group
// interaction's invokes on the wire (recordSent, which wraps the node
// interaction beneath the group's) are the group datagrams on the
// site's network (Network.Filter on the multicast model), opened with the
// node's own group keys (groups.Manager.Decode) and parsed as the
// InvokeRequest they carry; the materialised group endpoint's OnOffClient
// is the resolution's Clients and its Group target, which sends.
package node_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	specgroups "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// bqGroup is the group sends' GROUP, 5.
const bqGroup uint16 = 5

// groupDatagrams records the datagrams sent to a multicast address on the
// site's network.
type groupDatagrams struct {
	mu   sync.Mutex
	sent []fabrictest.Datagram
}

func recordGroupDatagrams(t *testing.T, site *fabrictest.Site) *groupDatagrams {
	t.Helper()
	g := &groupDatagrams{}
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		if d.To.IP.IsMulticast() {
			g.mu.Lock()
			g.sent = append(g.sent, d)
			g.mu.Unlock()
		}
		return true
	})
	t.Cleanup(func() { site.Net().Filter(nil) })
	return g
}

func (g *groupDatagrams) all() []fabrictest.Datagram {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.sent)
}

// sentInvokes opens every group datagram with the device's group keys and
// returns the InvokeRequests they carry.
func (b *bqSwitch) sentInvokes(t *testing.T, g *groupDatagrams) []im.InvokeRequest {
	t.Helper()
	sent := g.all()
	out := make([]im.InvokeRequest, 0, len(sent))
	for _, d := range sent {
		msg, err := b.dev.Node().Groups().Decode(parityCtx(t), d.Payload)
		if err != nil {
			t.Fatalf("opening the group datagram: %v", err)
		}
		if msg.GroupID != bqGroup {
			t.Fatalf("a group datagram to group %d", msg.GroupID)
		}
		ph, n, err := message.UnmarshalProtocolHeader(msg.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if ph.ProtocolID != im.InteractionModelProtocolID || ph.Opcode != im.OpcodeInvokeRequest {
			t.Fatalf("a group message of protocol 0x%04X opcode 0x%02X", ph.ProtocolID, ph.Opcode)
		}
		req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(msg.Payload[n:]), nil)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, req)
	}
	return out
}

// groupSwitch is the describe's switchNode (:610-623):
// OnOffLightSwitchDevice.withClientClusters(GroupsClient), its established
// observer attached.
func groupSwitch(t *testing.T) (*bqSwitch, *bqHost) {
	t.Helper()
	b := newBQSwitch(t, bqOptions{clients: []uint32{specgroups.ClusterID}})
	return b, observeBindings(b.dev.Node().Bindings())
}

// bindGroup writes the group binding and waits for its resolution
// (writeGroupBinding and awaitResolution, :641-671).
func (b *bqSwitch) bindGroup(t *testing.T, h *bqHost) binding.Resolution {
	t.Helper()
	b.writeBinding(t, 0, core.TargetStruct{Group: bqGroup})
	return b.awaitResolution(t, h)
}

func (b *bqSwitch) awaitResolution(t *testing.T, h *bqHost) binding.Resolution {
	t.Helper()
	b.eventually(t, "the group binding resolved", func() bool { return len(h.takeEstablished()) > 0 })
	r := h.takeEstablished()[0]
	if r.Kind != binding.KindGroup || r.Group == nil {
		t.Fatalf("expected a group resolution, got %+v", r)
	}
	return r
}

// onPath is OnOffClient.on on a group: the cluster and the command, no
// endpoint.
var onPath = im.ConcreteCommandPath{Cluster: onoff.ClusterID, Command: 0x01}

// TestBindingIntegrationParityMatterJS ports the group kind of the
// describe "Binding integration" (BindingIntegrationTest.ts:57-601).
func TestBindingIntegrationParityMatterJS(t *testing.T) {
	holdSite(t)

	// :529-600.
	t.Run("kind=group: binding resolves to ClientGroup with OnOffClient installed on materialized endpoint", func(t *testing.T) {
		b := newBQSwitch(t, bqOptions{})
		h := observeBindings(b.dev.Node().Bindings())
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		if !slices.Contains(r.Clients, onoff.ClusterID) {
			t.Errorf("Clients %v lack OnOff", r.Clients)
		}
		if r.Endpoint != b.eps[0] {
			t.Errorf("the group's endpoint is %d, want the source endpoint %d", r.Endpoint, b.eps[0])
		}
		// `typeof commands.on === "function"`: the group target sends On.
		if err := r.Group.Invoke(parityCtx(t), onPath, nil); err != nil {
			t.Errorf("On through the group: %v", err)
		}
		b.writeBinding(t, 0)
		b.eventually(t, "the group binding removed", func() bool { return len(h.takeRemoved()) > 0 })
		if got := h.takeRemoved(); len(got) != 1 || got[0].Kind != binding.KindGroup {
			t.Fatalf("removed %+v, want one group resolution", got)
		}
	})
}

// TestBindingIntegrationGroupSendsParityMatterJS ports the describe
// "Binding integration, group sends" (BindingIntegrationTest.ts:602-857).
func TestBindingIntegrationGroupSendsParityMatterJS(t *testing.T) {
	holdSite(t)

	// :692-714. matter.js starts the ClientGroup and finds its node store
	// without a cache fabric filter and NetworkClient silent; a Go group
	// resolution has no client node, and so no cache: no peer, no
	// interaction, only the group target — and from the provisioning on,
	// through the resolution and a send, nothing at warn level.
	t.Run("records no cache fabric filter for a group", func(t *testing.T) {
		b, h := groupSwitch(t)
		mark := b.log.mark()
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		if err := r.Group.Invoke(parityCtx(t), onPath, nil); err != nil {
			t.Fatalf("On: %v", err)
		}
		if r.Peer != nil || r.Interaction != nil {
			t.Errorf("the group resolution carries a client node: peer %v, interaction %v", r.Peer, r.Interaction)
		}
		if p := b.log.problemsSince(mark); len(p) != 0 {
			t.Errorf("problems %q", p)
		}
	})

	// :716-737.
	t.Run("sends a command through a binding as a group command without an endpoint", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		sent := recordGroupDatagrams(t, b.site)
		if err := r.Group.Invoke(parityCtx(t), onPath, nil); err != nil {
			t.Fatalf("On: %v", err)
		}
		invokes := b.sentInvokes(t, sent)
		if len(invokes) != 1 {
			t.Fatalf("%d invokes sent, want 1", len(invokes))
		}
		req := invokes[0]
		if len(req.Invokes) != 1 {
			t.Fatalf("%d commands in the invoke, want 1", len(req.Invokes))
		}
		path := req.Invokes[0].Path
		if path.HasEndpoint || path.Cluster != onoff.ClusterID || path.Command != 0x01 {
			t.Errorf("command path %+v, want OnOff.on without an endpoint", path)
		}
		if !req.SuppressResponse {
			t.Error("the group invoke does not suppress its response")
		}
	})

	// :739-759. matter.js's line is "ClientInteraction Invoke » … group: 5
	// dest: … *.onOff.on"; the Go record is matter.groups.invoke with the
	// group, the destination, the cluster and the command.
	t.Run("names the group on the log line of a group command it sends", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		if err := r.Group.Invoke(parityCtx(t), onPath, nil); err != nil {
			t.Fatalf("On: %v", err)
		}
		found := false
		for _, rec := range b.log.records() {
			if rec.msg == "matter.groups.invoke" && rec.attrs["group"] == "5" && rec.attrs["dest"] != "" &&
				rec.attrs["cluster"] == "OnOff" && rec.attrs["command"] == "0x01" {
				found = true
			}
		}
		if !found {
			t.Error("no log line names group 5 and OnOff.on")
		}
	})

	// :761-776. The resolution's group target carries no timed option; the
	// group interaction beneath it — the node's group sender of the
	// fabric's group, the one the target wraps — refuses a timed invoke
	// (InvalidGroupOperationError is groups.ErrInvalidGroupOperation).
	t.Run("refuses a timed invoke on a group", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.provisionKey(t, bqGroup, bqKeySet)
		b.bindGroup(t, h)
		sent := recordGroupDatagrams(t, b.site)
		s, err := b.dev.Node().Groups().Sender(parityCtx(t), b.fabric, bqGroup)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Invoke(parityCtx(t), onPath, nil, groups.RequestOptions{Timed: true}); !errors.Is(err, groups.ErrInvalidGroupOperation) {
			t.Fatalf("timed group invoke: %v, want ErrInvalidGroupOperation", err)
		}
		if got := sent.all(); len(got) != 0 {
			t.Errorf("%d datagrams sent for a refused invoke", len(got))
		}
	})

	// :778-793. GroupTarget.Invoke answers no response at all; AddGroup's
	// AddGroupResponse never comes.
	t.Run("resolves a command with a response to undefined on a group", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		addGroup := im.ConcreteCommandPath{Cluster: specgroups.ClusterID, Command: specgroups.CmdAddGroup}
		if err := r.Group.Invoke(parityCtx(t), addGroup, specgroups.AddGroupRequest{GroupId: 7, GroupName: ""}); err != nil {
			t.Fatalf("AddGroup through the group: %v", err)
		}
	})

	// :795-818.
	t.Run("resolves a group binding written before the sender holds the group's key once it does, and sends", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.writeBinding(t, 0, core.TargetStruct{Group: bqGroup})
		b.waitReason(t, slog.LevelInfo, "Group binding waits for the fabric to hold a key for the group")
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v before the key", got)
		}
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.awaitResolution(t, h)
		sent := recordGroupDatagrams(t, b.site)
		if err := r.Group.Invoke(parityCtx(t), onPath, nil); err != nil {
			t.Fatalf("On: %v", err)
		}
		if got := b.sentInvokes(t, sent); len(got) != 1 {
			t.Fatalf("%d invokes sent, want 1", len(got))
		}
	})

	// :820-836. GroupKeySetMissingError "No group key set found for
	// groupId 5" is groups.ErrNoKey with that text; the Go sender fails
	// when it is asked for, before anything is sent.
	t.Run("fails a group send for which the sender holds no key", func(t *testing.T) {
		b, _ := groupSwitch(t)
		sent := recordGroupDatagrams(t, b.site)
		_, err := b.dev.Node().Groups().Sender(parityCtx(t), b.fabric, bqGroup)
		if !errors.Is(err, groups.ErrNoKey) || !strings.Contains(err.Error(), "no group key set found for groupId 5") {
			t.Fatalf("group send without a key: %v, want ErrNoKey for groupId 5", err)
		}
		if got := sent.all(); len(got) != 0 {
			t.Errorf("%d datagrams sent without a key", len(got))
		}
	})

	// :838-855. "Invoking a concrete command on a group address is not
	// supported." is groups.ErrInvalidGroupOperation with that text.
	t.Run("refuses a group invoke that names an endpoint", func(t *testing.T) {
		b, h := groupSwitch(t)
		b.provisionKey(t, bqGroup, bqKeySet)
		r := b.bindGroup(t, h)
		sent := recordGroupDatagrams(t, b.site)
		concrete := im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: onoff.ClusterID, Command: 0x01}
		err := r.Group.Invoke(parityCtx(t), concrete, nil)
		if !errors.Is(err, groups.ErrInvalidGroupOperation) || !strings.Contains(err.Error(), "invoking a concrete command on a group address is not supported") {
			t.Fatalf("group invoke naming an endpoint: %v, want ErrInvalidGroupOperation", err)
		}
		if got := sent.all(); len(got) != 0 {
			t.Errorf("%d datagrams sent for a refused invoke", len(got))
		}
	})
}

// boundLightSite is boundLight's result.
type boundLightSite struct {
	sw       *bqSwitch
	light    *device.OnOffLight
	lightDev *fabrictest.Device
	lightEp  uint16
	res      binding.Resolution
	// isolated drops every datagram from or to the light while set.
	isolated bool
	mu       sync.Mutex
	// reports are the onOff values the subscription delivered.
	reports []bool
	// paths are every attribute path a report carried.
	paths []im.ConcreteAttributePath
	// statuses are the subscription's activity changes.
	statuses []bool
}

// boundLight is matter.js boundLight (:62-174): a switch and a light on one
// controller's fabric, the light granting any CASE subject Operate, and
// the switch's endpoint bound to the light's.
func boundLight(t *testing.T) *boundLightSite {
	t.Helper()
	b := newBQSwitch(t, bqOptions{client: true})
	h := observeBindings(b.dev.Node().Bindings())
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light", Clock: b.site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	s := &boundLightSite{sw: b, light: light}
	s.lightDev = b.site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light}})
	if _, err := b.pair.Controller.Commission(parityCtx(t), s.lightDev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if s.lightEp, ok = s.lightDev.EndpointOf("light:1"); !ok {
		t.Fatal("the light has no endpoint")
	}
	fabrics, err := s.lightDev.Node().Fabrics(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("light fabrics %v, %v", fabrics, err)
	}
	lf := fabrics[0]
	// "grant-operate": any CASE subject of the fabric operates the light.
	if err := s.lightDev.Store().ReplaceACL(parityCtx(t), lf.FabricIndex, []store.ACLEntry{
		{FabricIndex: lf.FabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{bridgetest.DefaultControllerNodeID}},
		{FabricIndex: lf.FabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE},
	}); err != nil {
		t.Fatal(err)
	}
	lightAddr := s.lightDev.Addr().IP
	b.site.Net().Filter(func(d fabrictest.Datagram) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.isolated || (!d.From.IP.Equal(lightAddr) && !d.To.IP.Equal(lightAddr))
	})
	t.Cleanup(func() { b.site.Net().Filter(nil) })
	b.writeBinding(t, 0, core.TargetStruct{Node: lf.NodeID, Endpoint: s.lightEp, EndpointPresent: true})
	b.eventually(t, "the client binding resolved", func() bool { return len(h.takeEstablished()) > 0 })
	s.res = h.takeEstablished()[0]
	if s.res.Kind != binding.KindClient || s.res.Interaction == nil {
		t.Fatalf("expected a client resolution, got %+v", s.res)
	}
	return s
}

func (s *boundLightSite) isolate(on bool) {
	s.mu.Lock()
	s.isolated = on
	s.mu.Unlock()
}

// subscribeToOnOff is subscribeToOnOff (:180-219): the sustained default
// subscription of the light's onOff, returned once it is active.
func (s *boundLightSite) subscribeToOnOff(t *testing.T) *interaction.Subscription {
	t.Helper()
	var sub *interaction.Subscription
	floor, ceiling := time.Duration(0), time.Minute
	err := s.sw.site.Run(parityCtx(t), func(ctx context.Context) error {
		var err error
		sub, err = s.res.Interaction.Subscribe(ctx, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{{
				Endpoint: s.lightEp, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true,
			}},
			Sustain: true,
			// The bounds the bound client node asks by default: floor 0,
			// ceiling one minute (PhysicalDeviceProperties.ts:13, :24,
			// :117-129); the Go interaction applies none of its own.
			MinIntervalFloor:   &floor,
			MaxIntervalCeiling: &ceiling,
			Updated: func(r *interaction.Report) {
				s.mu.Lock()
				defer s.mu.Unlock()
				for i := range r.Attributes {
					a := &r.Attributes[i]
					s.paths = append(s.paths, a.Path)
					if a.Path.Attribute == onoff.AttrOnOff {
						s.reports = append(s.reports, a.Value.El.Bool)
					}
				}
			},
			ActiveChanged: func(active bool) {
				s.mu.Lock()
				s.statuses = append(s.statuses, active)
				s.mu.Unlock()
			},
		})
		return err
	})
	if err != nil {
		t.Fatalf("the default subscription: %v", err)
	}
	t.Cleanup(sub.Close)
	s.sw.eventually(t, "the default subscription active", sub.Active)
	return sub
}

// switchLight is switchLight (:222-241): the light switches locally and the
// bound endpoint's subscription reports the new value.
func (s *boundLightSite) switchLight(t *testing.T, on bool) {
	t.Helper()
	if err := s.lightDev.Act(func(*node.Node) error { s.light.OnOff().Set(on); return nil }); err != nil {
		t.Fatal(err)
	}
	s.sw.eventually(t, "the subscription reports the light", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.reports) > 0 && s.reports[len(s.reports)-1] == on
	})
}

func (s *boundLightSite) takeStatuses() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.statuses)
}

// TestBindingDefaultSubscriptionParityMatterJS ports the describe
// "kind=client with a default subscription of one attribute"
// (BindingIntegrationTest.ts:352-474).
func TestBindingDefaultSubscriptionParityMatterJS(t *testing.T) {
	holdSite(t)

	// :409-427. matter.js asserts the bound ClientNode installed no
	// BasicInformation, Descriptor or OperationalCredentials client (no read
	// of the root endpoint) and reads Connected; the Go resolution holds no
	// client node, so the observable is the wire: every attribute the
	// subscription carried is the light's onOff on its endpoint, nothing of
	// endpoint 0, the peer holds its session, the switch knows one peer and
	// nothing logs at warn level.
	t.Run("receives reports without data of the peer's root endpoint", func(t *testing.T) {
		s := boundLight(t)
		mark := s.sw.log.mark()
		s.subscribeToOnOff(t)
		s.switchLight(t, true)
		s.switchLight(t, false)
		s.mu.Lock()
		paths := slices.Clone(s.paths)
		s.mu.Unlock()
		for _, p := range paths {
			if p.Endpoint != s.lightEp || p.Cluster != onoff.ClusterID || p.Attribute != onoff.AttrOnOff {
				t.Errorf("a report carried %+v, not the light's onOff", p)
			}
		}
		if !s.res.Peer.HasSession() {
			t.Error("the bound peer holds no session")
		}
		if n := s.sw.dev.Peers().Size(); n != 1 {
			t.Errorf("the switch knows %d peers, want 1", n)
		}
		if p := s.sw.log.problemsSince(mark); len(p) != 0 {
			t.Errorf("problems %q", p)
		}
	})

	// :429-452.
	t.Run("detects a lost peer by the subscription timeout and resubscribes once it is back", func(t *testing.T) {
		s := boundLight(t)
		sub := s.subscribeToOnOff(t)
		maxInterval := sub.MaxInterval()
		before := len(s.takeStatuses())
		mark := s.sw.log.mark()
		s.switchLight(t, true)
		s.isolate(true)
		lostAt := s.sw.site.Clock().Now()
		s.sw.eventually(t, "the subscription inactive", func() bool { return !sub.Active() })
		if elapsed := s.sw.site.Clock().Now().Sub(lostAt); elapsed < maxInterval {
			t.Errorf("the loss was detected after %v, before the max interval %v", elapsed, maxInterval)
		}
		if got := s.takeStatuses()[before:]; !slices.Equal(got, []bool{false}) {
			t.Errorf("statuses %v after the loss, want [false]", got)
		}
		s.isolate(false)
		s.sw.eventually(t, "the subscription active again", sub.Active)
		if got := s.takeStatuses()[before:]; !slices.Equal(got, []bool{false, true}) {
			t.Errorf("statuses %v after the return, want [false true]", got)
		}
		s.switchLight(t, false)
		if n := s.sw.dev.Peers().Size(); n != 1 {
			t.Errorf("the switch knows %d peers, want 1", n)
		}
		if p := s.sw.log.problemsSince(mark); len(p) != 0 {
			t.Errorf("problems %q", p)
		}
	})

	// :454-473.
	t.Run("detects a restarting peer by the subscription timeout and resubscribes after its restart", func(t *testing.T) {
		s := boundLight(t)
		sub := s.subscribeToOnOff(t)
		mark := s.sw.log.mark()
		s.switchLight(t, true)
		// A power loss: the light goes away without a word to the switch.
		s.isolate(true)
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.sw.site.Run(stopCtx, s.lightDev.Stop); err != nil {
			t.Fatal(err)
		}
		s.sw.eventually(t, "the subscription inactive", func() bool { return !sub.Active() })
		s.isolate(false)
		// MockTime.resolve(lightNode.start()): the light re-establishes the
		// switch's former subscription while it starts, on the site's clock.
		if err := s.sw.site.Run(stopCtx, s.lightDev.Start); err != nil {
			t.Fatal(err)
		}
		s.sw.eventually(t, "the subscription active again", sub.Active)
		s.switchLight(t, false)
		if n := s.sw.dev.Peers().Size(); n != 1 {
			t.Errorf("the switch knows %d peers, want 1", n)
		}
		if p := s.sw.log.problemsSince(mark); len(p) != 0 {
			t.Errorf("problems %q", p)
		}
	})
}
