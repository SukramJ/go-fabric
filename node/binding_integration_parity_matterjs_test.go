// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/binding/BindingIntegrationTest.ts
// at 862ad75f: the group kind of "Binding integration" and the describe
// "Binding integration, group sends". The client kind written by an
// administrator and the self-binding are node:TestLiveBinding and
// binding:TestDormantThenWoken; the describe "kind=client with a default
// subscription of one attribute" is not ported (the register names why).
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
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	specgroups "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
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
	var out []im.InvokeRequest
	for _, d := range g.all() {
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
