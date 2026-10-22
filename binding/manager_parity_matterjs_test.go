// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/binding/BindingManagerTest.ts
// at 862ad75f: the cases that run on the manager alone. The cases that need
// a commissioned node — the register queue before the node is online, two
// Binding servers sharing one group session, the server check against a
// client-only cluster, the fabric replacement — are in
// node/binding_manager_parity_matterjs_test.go.
//
// matter.js runs every case on MockServerNode with an OnOffLightSwitch on
// endpoint 1 and a fake BindingServer whose `established` has an observer
// unless the case says otherwise; the port runs the manager over the
// package's fakes (helpers_test.go) with the switch's topology: our node id
// 100 on fabric 1 (node.addFabric()), the root and the switch's endpoint 1,
// the switch's declared clients read from the device type's mandatory
// client clusters in the schema (matter.js's type clientClusters). A peer
// coming online (`peer.lifecycle.online.emit`) is the fake link's
// sessionAdded, the stand-in of the peer's session the Go manager waits
// for; settle is the worker's flush; a warning is the manager's warn-level
// log record's reason, matter.js's message verbatim; provisionGroupKey is
// the fake topology's key; a key change of the fabric's group key maps is
// GroupKeysChanged, which the node calls on every GroupKeyMap or key-set
// change (node/client.go).
package binding

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// Our node id on fabric 1 and the switch's endpoint, as the cases'
// node.addFabric() and node.parts.get(1) give them.
const (
	parityNode     uint64 = 100
	paritySwitchEP uint16 = 1
)

// Device type ids and cluster ids the cases name.
const (
	dtOnOffLight       uint32 = 0x0100
	dtOnOffLightSwitch uint32 = 0x0103
	dtCameraController uint32 = 0x0147

	clIdentify             uint32 = 0x0003
	clGroups               uint32 = 0x0004
	clOnOff                uint32 = 0x0006
	clOTAProvider          uint32 = 0x0029
	clWebRTCProvider       uint32 = 0x0553
	clLevelControl         uint32 = 0x0008
	clDescriptor           uint32 = 0x001D
	clBinding              uint32 = 0x001E
	clAccessControl        uint32 = 0x001F
	clBasicInformation     uint32 = 0x0028
	clGeneralCommissioning uint32 = 0x0030
)

// deviceClients are the client clusters an endpoint of device type dt
// declares — matter.js's `type.clientClusters`, the mandatory client
// requirements MutableEndpoint registers — then those `with` adds
// (`.withClientClusters`, `.with`), each once.
func deviceClients(t *testing.T, dt uint32, with ...uint32) []uint32 {
	t.Helper()
	def, ok := schema.DeviceTypeDefinitionOf(schema.DeviceTypeID(dt))
	if !ok {
		t.Fatalf("no device type 0x%04X in the schema", dt)
	}
	var out []uint32
	for i := range def.Requirements {
		r := &def.Requirements[i]
		if r.Element == schema.RequirementClientCluster && r.HasID && r.Conformance.Op == "M" && !slices.Contains(out, r.ID) {
			out = append(out, r.ID)
		}
	}
	for _, id := range with {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// switchHarness is a manager on the switch's node: the root (endpoint 0)
// and endpoint 1 with clients as its declared client clusters. Without
// listening, nothing observes established (makeFakeBindingServer({
// listening: false })).
func switchHarness(t *testing.T, clients []uint32, listening bool, mutate func(*Config)) *harness {
	t.Helper()
	h := newHarness(t, mutate)
	h.topo.mu.Lock()
	h.topo.nodeIDs = map[uint8]uint64{1: parityNode}
	h.topo.servers = map[uint16][]uint32{
		0:              {clDescriptor, clAccessControl, clBasicInformation, clGeneralCommissioning},
		paritySwitchEP: {clIdentify, clDescriptor, clBinding},
	}
	h.topo.clients = map[uint16][]uint32{paritySwitchEP: clients}
	h.topo.mu.Unlock()
	if listening {
		h.listen()
	}
	return h
}

// selfEntry is makeSelfBindingEntry: our node, the endpoint, no cluster.
func selfEntry(endpoint uint16) Entry {
	return Entry{FabricIndex: 1, Node: parityNode, Endpoint: endpoint, EndpointPresent: true}
}

// remoteEntry is a remote node's endpoint 1, no cluster.
func remoteEntry(node uint64) Entry {
	return Entry{FabricIndex: 1, Node: node, Endpoint: 1}
}

// groupEntry is the case's groupEntry: the group, nothing else.
func groupEntry(group uint16) Entry {
	return Entry{FabricIndex: 1, Group: group}
}

// once registers an observer of established that takes one resolution and
// removes itself — `established.once(resolution => taken.push(resolution))`.
func (h *harness) once() func() []Resolution {
	var (
		mu    sync.Mutex
		taken []Resolution
		stop  func()
	)
	mu.Lock()
	stop = h.m.OnEstablished(func(r Resolution) {
		mu.Lock()
		defer mu.Unlock()
		if len(taken) > 0 {
			return
		}
		taken = append(taken, r)
		stop()
	})
	mu.Unlock()
	return func() []Resolution {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(taken)
	}
}

// asked is how often the group sender was asked for a group's target.
func (g *fakeGroups) asked() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func kinds(rs []Resolution) []Kind {
	out := make([]Kind, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Kind)
	}
	return out
}

func endpoints(rs []Resolution) []uint16 {
	out := make([]uint16, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Entry.Endpoint)
	}
	return out
}

// hookedGroups is a GroupSender that runs hook inside Sender, before it
// answers — the turns `peers.forAddress` takes to register a group peer,
// during which matter.js lets a key change or an unregister in.
type hookedGroups struct {
	fakeGroups
	hook func(group uint16)
}

func (g *hookedGroups) Sender(ctx context.Context, fabricIndex uint8, group uint16) (GroupTarget, error) {
	if g.hook != nil {
		g.hook(group)
	}
	return g.fakeGroups.Sender(ctx, fabricIndex, group)
}

// TestBindingManagerParityMatterJS ports the describe "BindingManager"
// (BindingManagerTest.ts:109-885), the cases that run on the manager alone.
func TestBindingManagerParityMatterJS(t *testing.T) {
	t.Parallel()
	switchClients := func(t *testing.T) []uint32 {
		t.Helper()
		return deviceClients(t, dtOnOffLightSwitch)
	}

	// :179-209.
	t.Run("resolves kind=client for remote nodeId — no emission until peer lifecycle.online fires", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, remoteEntry(parityNode+1))
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v before the peer is online", got)
		}
		p, _ := h.peers.For(peerAddr(1, parityNode+1))
		if got := h.link.observing(p); got != 1 {
			t.Errorf("the entry waits on %d observers of the peer, want 1 (pending)", got)
		}
	})

	// :292-325. `node instanceof ClientGroup` is the group sender of the
	// entry's fabric and group.
	t.Run("resolves kind=group for groupId set", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.topo.setKey(1, 7, true)
		h.m.Register(paritySwitchEP, groupEntry(7))
		h.flush()
		got := h.takeEstablished()
		if len(got) != 1 || got[0].Kind != KindGroup {
			t.Fatalf("established %+v, want one group resolution", got)
		}
		if g, ok := got[0].Group.(fakeGroupTarget); !ok || g.fabricIndex != 1 || g.group != 7 {
			t.Errorf("the resolution's group is %#v, want the sender of fabric 1, group 7", got[0].Group)
		}
	})

	// :477-497. The warning is ignoredEntryReason's (BindingManager.ts:707-720).
	t.Run("rejects entry whose cluster filter is not in source's declared clientClusters", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, Entry{FabricIndex: 1, Node: parityNode, Endpoint: paritySwitchEP, Cluster: 0x1234})
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v", got)
		}
		want := []string{"Ignoring binding entry for cluster 0x1234: endpoint 1 has no client for this cluster declared"}
		if got := h.log.reasons(slog.LevelWarn); !slices.Equal(got, want) {
			t.Errorf("warnings %q, want %q", got, want)
		}
	})

	// :499-521. The switch declares OnOffClient but serves no OnOff.
	t.Run("kind=server rejects when target endpoint lacks the cluster server", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, Entry{FabricIndex: 1, Node: parityNode, Endpoint: paritySwitchEP, Cluster: clOnOff})
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v", got)
		}
		want := []string{"Self-binding references cluster not installed as server on target endpoint"}
		if got := h.log.reasons(slog.LevelWarn); !slices.Equal(got, want) {
			t.Errorf("warnings %q, want %q", got, want)
		}
	})

	// :523-552. `sourceEp.behaviors.require(OnOffServer)` after the node
	// is up: the manager asks the topology at resolution time.
	t.Run("kind=server accepts a cluster server added to the endpoint at runtime via behaviors.require", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.topo.mu.Lock()
		h.topo.servers[paritySwitchEP] = append(h.topo.servers[paritySwitchEP], clOnOff)
		h.topo.mu.Unlock()
		h.m.Register(paritySwitchEP, Entry{FabricIndex: 1, Node: parityNode, Endpoint: paritySwitchEP, Cluster: clOnOff})
		h.flush()
		got := h.takeEstablished()
		if len(got) != 1 || got[0].Kind != KindServer || got[0].Endpoint != paritySwitchEP {
			t.Fatalf("established %+v, want one server resolution on endpoint 1", got)
		}
	})

	// :690-703. provisionGroupKey, then the group remapped to key set
	// 0x1a2, which the fabric does not hold, and back.
	t.Run("kind=group establishes once when several key changes arrive together", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, groupEntry(45))
		h.flush()
		for _, held := range []bool{true, false, true} {
			h.topo.setKey(1, 45, held)
			h.m.GroupKeysChanged(1)
		}
		h.flush()
		if got := h.takeEstablished(); len(got) != 1 {
			t.Fatalf("established %d times, want once: %+v", len(got), got)
		}
	})

	// :705-717. disposeServer is Detach.
	t.Run("kind=group does not resolve a waiting entry after its server was disposed", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, groupEntry(46))
		h.flush()
		h.m.Detach(paritySwitchEP)
		h.topo.setKey(1, 46, true)
		h.m.GroupKeysChanged(1)
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v after the server was disposed", got)
		}
	})

	// :733-756. The key goes away while the group peer is registered —
	// here inside the group sender's lookup.
	t.Run("kind=group does not resolve when the key is withdrawn while the group is being registered", func(t *testing.T) {
		t.Parallel()
		hg := &hookedGroups{}
		h := switchHarness(t, switchClients(t), true, func(c *Config) { c.Groups = hg })
		hg.hook = func(group uint16) { h.topo.setKey(1, group, false) }
		h.topo.setKey(1, 47, true)
		h.m.Register(paritySwitchEP, groupEntry(47))
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v after the key was withdrawn", got)
		}
	})

	// :758-790. The unregister of group 48 arrives while its group peer is
	// registered — here while the group sender's lookup runs; the entry of
	// group 49 keeps the source endpoint's record alive.
	t.Run("kind=group does not resolve an entry unregistered while its group is being registered", func(t *testing.T) {
		t.Parallel()
		hg := &hookedGroups{}
		entered, release := make(chan struct{}), make(chan struct{})
		hg.hook = func(group uint16) {
			if group == 48 {
				close(entered)
				<-release
			}
		}
		h := switchHarness(t, switchClients(t), true, func(c *Config) { c.Groups = hg })
		h.topo.setKey(1, 48, true)
		h.topo.setKey(1, 49, true)
		h.m.Register(paritySwitchEP, groupEntry(48))
		h.m.Register(paritySwitchEP, groupEntry(49))
		<-entered
		h.m.Unregister(paritySwitchEP, groupEntry(48))
		close(release)
		h.flush()
		established := h.takeEstablished()
		groups := make([]uint16, 0, len(established))
		for _, r := range established {
			groups = append(groups, r.Entry.Group)
		}
		if !slices.Equal(groups, []uint16{49}) {
			t.Fatalf("established groups %v, want [49]", groups)
		}
		if got := h.takeRemoved(); len(got) != 0 {
			t.Errorf("removed %+v: the unregistered entry was established first", got)
		}
	})

	// :854-884. The client behaviors matter.js installs on the
	// materialised endpoint are the resolution's Clients.
	t.Run("kind=client installs declared client behaviors on the materialized endpoint", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), true, nil)
		h.m.Register(paritySwitchEP, remoteEntry(parityNode+1))
		h.flush()
		p, _ := h.peers.For(peerAddr(1, parityNode+1))
		h.link.sessionAdded(p)
		h.flush()
		got := h.takeEstablished()
		if len(got) != 1 {
			t.Fatalf("established %+v, want one", got)
		}
		if !slices.Contains(got[0].Clients, clOnOff) {
			t.Errorf("Clients %v lack OnOff", got[0].Clients)
		}
	})
}

// TestBindingManagerClientSelectionParityMatterJS ports the describe
// "client selection" (BindingManagerTest.ts:886-1106). The behaviors
// matter.js installs on the bound endpoint are the resolution's Clients
// (BindingManager.ts:547-567, binding/resolve.go selectClientClusters).
func TestBindingManagerClientSelectionParityMatterJS(t *testing.T) {
	t.Parallel()
	// switchWithMoreClients (:887-891).
	moreClients := func(t *testing.T) []uint32 {
		t.Helper()
		return deviceClients(t, dtOnOffLightSwitch, clGroups, clOTAProvider, clWebRTCProvider)
	}
	// resolveClientEntry (:893-913): a remote entry on endpoint 1, the peer
	// online.
	resolveClientEntry := func(t *testing.T, clients []uint32, cluster uint32, hasCluster bool) *harness {
		t.Helper()
		h := switchHarness(t, clients, true, nil)
		e := remoteEntry(parityNode + 1)
		if hasCluster {
			e.Cluster, e.ClusterPresent = cluster, true
		}
		h.m.Register(paritySwitchEP, e)
		h.flush()
		p, _ := h.peers.For(peerAddr(1, parityNode+1))
		h.link.sessionAdded(p)
		h.flush()
		return h
	}
	// warningsFor (:966-993): the warnings of a remote entry on endpoint 1.
	warningsFor := func(t *testing.T, clients []uint32, cluster uint32, hasCluster bool) []string {
		t.Helper()
		h := resolveClientEntry(t, clients, cluster, hasCluster)
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v", got)
		}
		return h.log.reasons(slog.LevelWarn)
	}
	expectWarnings := func(t *testing.T, got []string, want ...string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("warnings %q, want %q", got, want)
		}
	}

	t.Run("installs every declared client on a bound peer except those whose cluster chooses its peer", func(t *testing.T) {
		t.Parallel()
		got := resolveClientEntry(t, moreClients(t), 0, false).takeEstablished()
		if len(got) != 1 {
			t.Fatalf("established %+v, want one", got)
		}
		for _, c := range []uint32{clIdentify, clGroups, clOnOff} {
			if !slices.Contains(got[0].Clients, c) {
				t.Errorf("Clients %v lack 0x%04X", got[0].Clients, c)
			}
		}
		for _, c := range []uint32{clOTAProvider, clWebRTCProvider} {
			if slices.Contains(got[0].Clients, c) {
				t.Errorf("Clients %v carry 0x%04X, a cluster that chooses its peer", got[0].Clients, c)
			}
		}
	})

	t.Run("installs the utility client an entry filters to", func(t *testing.T) {
		t.Parallel()
		got := resolveClientEntry(t, moreClients(t), clIdentify, true).takeEstablished()
		if len(got) != 1 {
			t.Fatalf("established %+v, want one", got)
		}
		if !slices.Contains(got[0].Clients, clIdentify) || slices.Contains(got[0].Clients, clOnOff) {
			t.Errorf("Clients %v, want Identify and not OnOff", got[0].Clients)
		}
	})

	t.Run("rejects an entry filtered to a client whose cluster chooses its peer, saying so", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, moreClients(t), clOTAProvider, true),
			"Ignoring binding entry for cluster OtaSoftwareUpdateProvider: we never use this client through a binding")
	})

	t.Run("names both clusters when an entry matches only several clients a binding never directs", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, deviceClients(t, dtOnOffLight, clOTAProvider, clWebRTCProvider), 0, false),
			"Ignoring binding entry for clusters OtaSoftwareUpdateProvider, WebRtcTransportProvider: we never use these clients through a binding")
	})

	t.Run("says so when an entry without a cluster reaches an endpoint without client clusters", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, deviceClients(t, dtOnOffLight), 0, false),
			"Ignoring binding entry: endpoint 1 has no client cluster declared")
	})

	t.Run("names a cluster the model does not define by its ID", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, deviceClients(t, dtOnOffLight), 0xfff1fc01, true),
			"Ignoring binding entry for cluster 0xfff1fc01: endpoint 1 has no client for this cluster declared")
	})

	t.Run("rejects an entry for a cluster the endpoint has no client for, naming the cluster", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, moreClients(t), clLevelControl, true),
			"Ignoring binding entry for cluster LevelControl: endpoint 1 has no client for this cluster declared")
	})

	t.Run("rejects an entry of an endpoint whose only clients choose their peer, saying so", func(t *testing.T) {
		t.Parallel()
		expectWarnings(t, warningsFor(t, deviceClients(t, dtCameraController), 0, false),
			"Ignoring binding entry for cluster WebRtcTransportProvider: we never use this client through a binding")
	})

	t.Run("leaves a client whose cluster chooses its peer off a bound group", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, moreClients(t), true, nil)
		h.topo.setKey(1, 7, true)
		h.m.Register(paritySwitchEP, groupEntry(7))
		h.flush()
		got := h.takeEstablished()
		if len(got) != 1 || got[0].Kind != KindGroup {
			t.Fatalf("established %+v, want one group resolution", got)
		}
		if !slices.Contains(got[0].Clients, clOnOff) || slices.Contains(got[0].Clients, clWebRTCProvider) {
			t.Errorf("Clients %v, want OnOff and not WebRtcTransportProvider", got[0].Clients)
		}
	})
}

// TestBindingManagerWithoutObserverParityMatterJS ports the describe
// "without an observer of established" (BindingManagerTest.ts:1107-1374),
// the cases the register lists open. matter.js's
// `established.observed.isObserved` after a dispose, an unregister or a
// close is the record's listener watch (BindingManager.ts:399-410); the Go
// manager watches its observers as a whole (OnEstablished) and keeps no
// per-record watch, so those lines have no counterpart — what the host
// sees afterwards (nothing established, nothing removed) is asserted.
func TestBindingManagerWithoutObserverParityMatterJS(t *testing.T) {
	t.Parallel()
	switchClients := func(t *testing.T) []uint32 {
		t.Helper()
		return deviceClients(t, dtOnOffLightSwitch)
	}

	// :1154-1178. `node.peers.get(groupAddress)` is the group sender's
	// lookup.
	t.Run("registers no group until something observes", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		h.topo.setKey(1, 7, true)
		h.m.Register(paritySwitchEP, groupEntry(7))
		h.flush()
		if got := h.group.asked(); got != 0 {
			t.Fatalf("the group sender was asked %d times before anything observes", got)
		}
		h.listen()
		h.flush()
		if got := kinds(h.takeEstablished()); !slices.Equal(got, []Kind{KindGroup}) {
			t.Fatalf("established kinds %v, want [group]", got)
		}
		if got := h.group.asked(); got != 1 {
			t.Errorf("the group sender was asked %d times, want once", got)
		}
	})

	t.Run("leaves an entry dormant that arrives after a once observer was used up", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		taken := h.once()
		h.m.Register(paritySwitchEP, selfEntry(1))
		h.m.Register(paritySwitchEP, selfEntry(0))
		h.flush()
		if got := endpoints(taken()); !slices.Equal(got, []uint16{1}) {
			t.Fatalf("the once observer took endpoints %v, want [1]", got)
		}
		h.listen()
		h.flush()
		if got := endpoints(h.takeEstablished()); !slices.Equal(got, []uint16{0}) {
			t.Fatalf("established endpoints %v, want [0]", got)
		}
	})

	t.Run("parks a resolved entry when its once observer was taken while its peer came online", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		taken := h.once()
		nodes := []uint64{parityNode + 1, parityNode + 2}
		for _, n := range nodes {
			h.m.Register(paritySwitchEP, remoteEntry(n))
		}
		h.flush()
		for _, n := range nodes {
			p, _ := h.peers.For(peerAddr(1, n))
			h.link.sessionAdded(p)
			h.flush()
		}
		p1, _ := h.peers.For(peerAddr(1, nodes[0]))
		p2, _ := h.peers.For(peerAddr(1, nodes[1]))
		if got := taken(); len(got) != 1 || got[0].Peer != p1 {
			t.Fatalf("the once observer took %+v, want the first peer", got)
		}
		h.listen()
		h.flush()
		if got := h.takeEstablished(); len(got) != 1 || got[0].Peer != p2 {
			t.Fatalf("established %+v, want the second peer", got)
		}
	})

	t.Run("forgets dormant entries of a disposed server", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		h.m.Register(paritySwitchEP, selfEntry(1))
		h.flush()
		h.m.Detach(paritySwitchEP)
		h.listen()
		h.flush()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Errorf("established %+v", got)
		}
		if got := h.takeRemoved(); len(got) != 0 {
			t.Errorf("removed %+v", got)
		}
	})

	t.Run("keeps the other dormant entry when one is unregistered", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		h.m.Register(paritySwitchEP, selfEntry(1))
		h.m.Register(paritySwitchEP, selfEntry(0))
		h.flush()
		h.m.Unregister(paritySwitchEP, selfEntry(0))
		h.listen()
		h.flush()
		if got := endpoints(h.takeEstablished()); !slices.Equal(got, []uint16{1}) {
			t.Fatalf("established endpoints %v, want [1]", got)
		}
	})

	// :1315-1331. After Close the worker is gone; OnEstablished queues no
	// wake.
	t.Run("resolves no dormant entry after the manager closed", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		h.m.Register(paritySwitchEP, selfEntry(1))
		h.flush()
		h.m.Close()
		h.listen()
		if got := h.takeEstablished(); len(got) != 0 {
			t.Errorf("established %+v after Close", got)
		}
	})

	t.Run("still warns about an entry that can never resolve", func(t *testing.T) {
		t.Parallel()
		h := switchHarness(t, switchClients(t), false, nil)
		h.m.Register(paritySwitchEP, selfEntry(9))
		h.flush()
		if got, want := h.log.reasons(slog.LevelWarn), []string{"Self-binding to non-existent endpoint"}; !slices.Equal(got, want) {
			t.Fatalf("warnings %q, want %q", got, want)
		}
		h.m.Register(paritySwitchEP, Entry{FabricIndex: 1, Node: parityNode + 1})
		h.flush()
		want := []string{"Self-binding to non-existent endpoint", "Client binding entry missing endpoint"}
		if got := h.log.reasons(slog.LevelWarn); !slices.Equal(got, want) {
			t.Fatalf("warnings %q, want %q", got, want)
		}
	})
}
