// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestDropReasons: every entry the manager cannot use is dropped with
// matter.js's reason verbatim (BindingManager.ts:255-353, :721-737), and
// nothing is established.
func TestDropReasons(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		setup  func(h *harness)
		cfg    func(*Config)
		source uint16
		entry  Entry
		want   string
	}{
		{name: "node and group", entry: Entry{FabricIndex: 1, Node: 2, Group: 3, Endpoint: 1}, want: "Binding entry must have exactly one of node/group"},
		{name: "neither node nor group", entry: Entry{FabricIndex: 1, Endpoint: 1}, want: "Binding entry must have exactly one of node/group"},
		{
			name: "no client declared", setup: func(h *harness) { h.topo.setClients(1) },
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1}, want: "Ignoring binding entry: endpoint 1 has no client cluster declared",
		},
		{
			name:  "no client for the cluster",
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0008}, want: "Ignoring binding entry for cluster LevelControl: endpoint 1 has no client for this cluster declared",
		},
		{
			name:  "no client for an unknown cluster",
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0xFC00}, want: "Ignoring binding entry for cluster 0xfc00: endpoint 1 has no client for this cluster declared",
		},
		{
			name: "one unbindable client", setup: func(h *harness) { h.topo.setClients(1, 0x0553) },
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1}, want: "Ignoring binding entry for cluster WebRtcTransportProvider: we never use this client through a binding",
		},
		{
			name: "unbindable clients", setup: func(h *harness) { h.topo.setClients(1, 0x0553, 0x0554) },
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1}, want: "Ignoring binding entry for clusters WebRtcTransportProvider, WebRtcTransportRequestor: we never use these clients through a binding",
		},
		{name: "group on an unknown fabric", entry: Entry{FabricIndex: 9, Group: 5}, want: "Group binding fabric unknown"},
		{name: "self-binding without endpoint", entry: Entry{FabricIndex: 1, Node: 100}, want: "Self-binding to non-existent endpoint"},
		{name: "self-binding to a missing endpoint", entry: Entry{FabricIndex: 1, Node: 100, Endpoint: 7}, want: "Self-binding to non-existent endpoint"},
		{
			name:  "self-binding to a cluster not served",
			entry: Entry{FabricIndex: 1, Node: 100, Endpoint: 2, Cluster: 0x0003}, want: "Self-binding references cluster not installed as server on target endpoint",
		},
		{name: "client without endpoint", entry: Entry{FabricIndex: 1, Node: 2}, want: "Client binding entry missing endpoint"},
		{
			name: "peer registration fails", setup: func(h *harness) { h.peers.err = errNoPeer },
			entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1}, want: "Client binding peer registration failed",
		},
		{name: "client stack disabled", cfg: func(c *Config) { c.Peers = nil }, entry: Entry{FabricIndex: 1, Node: 2, Endpoint: 1}, want: "client stack disabled"},
		{
			name: "no group sender", cfg: func(c *Config) { c.Groups = nil }, setup: func(h *harness) { h.topo.setKey(1, 5, true) },
			entry: Entry{FabricIndex: 1, Group: 5}, want: "Group binding peer registration failed",
		},
		{
			name: "group sender fails", setup: func(h *harness) { h.topo.setKey(1, 5, true); h.group.err = errors.New("no key") },
			entry: Entry{FabricIndex: 1, Group: 5}, want: "Group binding peer registration failed",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, c.cfg)
			if c.setup != nil {
				c.setup(h)
			}
			h.listen()
			source := c.source
			if source == 0 {
				source = 1
			}
			h.m.Register(source, c.entry)
			h.flush()
			if got := h.log.reasons(slog.LevelWarn); !slices.Equal(got, []string{c.want}) {
				t.Fatalf("warnings %q, want %q", got, c.want)
			}
			if got := h.takeEstablished(); len(got) != 0 {
				t.Fatalf("established %+v", got)
			}
		})
	}
}

// TestDormantThenWoken: without an established observer an entry waits,
// logged once (BindingManager.ts:385-424); the first observer wakes it and
// a self-binding resolves locally to the server kind (:322-328).
func TestDormantThenWoken(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	self := Entry{FabricIndex: 1, Node: 100, Endpoint: 2, Cluster: 0x0006}
	group := Entry{FabricIndex: 1, Group: 5}
	h.m.Register(1, self)
	h.m.Register(1, group)
	h.flush()
	// A key change resolves the group entry again: it parks again, without
	// a second line.
	h.topo.setKey(1, 5, true)
	h.m.GroupKeysChanged(1)
	h.flush()
	if got := h.log.reasons(slog.LevelInfo); !slices.Equal(got, []string{reasonDormant, reasonDormant}) {
		t.Fatalf("info %q", got)
	}
	if len(h.takeEstablished()) != 0 || h.group.calls != 0 {
		t.Fatal("a dormant entry resolved")
	}
	h.listen()
	h.flush()
	got := h.takeEstablished()
	if len(got) != 2 {
		t.Fatalf("established %+v", got)
	}
	if r := got[0]; r.Kind != KindServer || r.Entry != self || r.SourceEndpoint != 1 || r.Endpoint != 2 || !slices.Equal(r.Clients, []uint32{0x0006}) || r.Peer != nil {
		t.Fatalf("self-binding %+v", r)
	}
	if r := got[1]; r.Kind != KindGroup || r.Endpoint != 1 || r.Group != (fakeGroupTarget{1, 5}) || !slices.Equal(r.Clients, []uint32{0x0003, 0x0006}) {
		t.Fatalf("group %+v", r)
	}
	if n := len(h.m.Established()); n != 2 {
		t.Fatalf("Established() has %d", n)
	}
}

// TestParkedAgainWithoutObserver: an entry that resolves after the last
// observer left parks again (#emitEstablished, :359-362).
func TestParkedAgainWithoutObserver(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	stop := h.listen()
	e := Entry{FabricIndex: 1, Node: 2, Endpoint: 1}
	h.m.Register(1, e)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	stop()
	h.link.sessionAdded(p)
	h.flush()
	if len(h.takeEstablished()) != 0 || len(h.m.Established()) != 0 {
		t.Fatal("established without an observer")
	}
	h.listen()
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 || got[0].Kind != KindClient {
		t.Fatalf("woken %+v", got)
	}
}

// TestClientEstablishedOnSession: a node entry registers the peer,
// connects in the background (info "Initiating CASE session for bound
// peer") and is established once the peer has a session — on its
// SessionAdded, or at once when it already has one (:585-627).
func TestClientEstablishedOnSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	e := Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0006}
	h.m.Register(1, e)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	waitFor(t, func() bool { return h.link.connected(p) == 1 })
	if h.link.connected(p) != 1 || h.link.observing(p) != 1 {
		t.Fatalf("connects %d, observers %d", h.link.connected(p), h.link.observing(p))
	}
	if got := h.log.reasons(slog.LevelInfo); !slices.Equal(got, []string{reasonInitiatingCASE}) {
		t.Fatalf("info %q", got)
	}
	if len(h.takeEstablished()) != 0 {
		t.Fatal("established before a session")
	}
	h.link.sessionAdded(p)
	h.flush()
	got := h.takeEstablished()
	if len(got) != 1 {
		t.Fatalf("established %+v", got)
	}
	r := got[0]
	if r.Kind != KindClient || r.Peer != p || r.Interaction == nil || r.Endpoint != 1 || r.Entry != e || !slices.Equal(r.Clients, []uint32{0x0006}) {
		t.Fatalf("resolution %+v", r)
	}
	if h.link.observing(p) != 0 {
		t.Fatal("the session observer stayed")
	}
	// A second entry to the same peer, which has a session: established at
	// once.
	e2 := Entry{FabricIndex: 1, Node: 2, Endpoint: 3}
	h.m.Register(1, e2)
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 || got[0].Entry != e2 || !slices.Equal(got[0].Clients, []uint32{0x0003, 0x0006}) {
		t.Fatalf("immediate %+v", got)
	}
}

// TestUnregister: an established entry's unregister emits removed
// (:147-157, :165-205); a pending one's cancels it, so a later session
// establishes nothing; a dormant one's forgets it.
func TestUnregister(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	est := Entry{FabricIndex: 1, Node: 100, Endpoint: 2}
	pending := Entry{FabricIndex: 1, Node: 3, Endpoint: 1}
	h.m.Register(1, est)
	h.m.Register(1, pending)
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 {
		t.Fatalf("established %+v", got)
	}
	h.m.Unregister(1, est)
	h.m.Unregister(1, pending)
	h.flush()
	if got := h.takeRemoved(); len(got) != 1 || got[0].Entry != est || got[0].Kind != KindServer {
		t.Fatalf("removed %+v", got)
	}
	p, _ := h.peers.For(peerAddr(1, 3))
	h.link.sessionAdded(p)
	h.flush()
	if got := h.takeEstablished(); len(got) != 0 {
		t.Fatalf("an unregistered pending entry established %+v", got)
	}
	if len(h.m.Established()) != 0 {
		t.Fatal("still established")
	}
}

// TestGroupWaitsForKey: a group entry waits until the fabric holds a key
// for the group (info, :309-316), resolves through the group sender on the
// key change (:492-541, membership not needed :477-480), and stays when
// the key goes (:522-525); its unregister emits removed.
func TestGroupWaitsForKey(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	e := Entry{FabricIndex: 1, Group: 0x0101}
	h.m.Register(1, e)
	h.flush()
	if got := h.log.reasons(slog.LevelInfo); !slices.Equal(got, []string{reasonGroupWaitsForKey}) {
		t.Fatalf("info %q", got)
	}
	if h.group.calls != 0 {
		t.Fatal("the sender was asked without a key")
	}
	// Another fabric's change does not resolve it.
	h.topo.setKey(1, 0x0101, true)
	h.m.GroupKeysChanged(2)
	h.flush()
	if len(h.takeEstablished()) != 0 {
		t.Fatal("resolved on another fabric's change")
	}
	h.m.GroupKeysChanged(1)
	h.m.GroupKeysChanged(1) // coalesced into the one recheck
	h.flush()
	got := h.takeEstablished()
	if len(got) != 1 || got[0].Kind != KindGroup || got[0].Group != (fakeGroupTarget{1, 0x0101}) || h.group.calls != 1 {
		t.Fatalf("established %+v after %d sender calls", got, h.group.calls)
	}
	h.topo.setKey(1, 0x0101, false)
	h.m.GroupKeysChanged(1)
	h.flush()
	if len(h.takeRemoved()) != 0 || len(h.m.Established()) != 1 {
		t.Fatal("the key's removal tore the binding down")
	}
	h.m.Unregister(1, e)
	h.flush()
	if got := h.takeRemoved(); len(got) != 1 || got[0].Kind != KindGroup {
		t.Fatalf("removed %+v", got)
	}
}

// TestHandlerPanic: a handler's panic is logged with matter.js's reason
// and not raised; the next handler still runs (:374-382, :196-204).
func TestHandlerPanic(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.m.OnEstablished(func(Resolution) { panic("established boom") })
	h.listen()
	h.m.OnRemoved(func(Resolution) { panic("removed boom") })
	e := Entry{FabricIndex: 1, Node: 100, Endpoint: 2}
	h.m.Register(1, e)
	h.flush()
	if len(h.takeEstablished()) != 1 {
		t.Fatal("the second handler did not run")
	}
	h.m.Unregister(1, e)
	h.flush()
	if len(h.takeRemoved()) != 1 {
		t.Fatal("the removed recorder did not run")
	}
	if got := h.log.reasons(slog.LevelWarn); !slices.Equal(got, []string{reasonEstablishedFailed, reasonRemovedFailed}) {
		t.Fatalf("warnings %q", got)
	}
}

// TestAttachDetachClose: an attached Binding server's entries are
// registered at once and on every write; a write that removes an entry
// emits removed; Detach and Close emit removed for every live binding
// (BindingServer.ts:120-163, BindingManager.ts:207-244).
func TestAttachDetachClose(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.topo.setClients(3, 0x0006)
	h.listen()
	b := mattercore.NewBinding()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	a := mattercore.TargetStruct{Node: 100, Endpoint: 2}
	if err := b.MatterWrite(ctx, 0, []mattercore.TargetStruct{a}); err != nil {
		t.Fatal(err)
	}
	h.m.Attach(1, b)
	h.m.Attach(1, b) // again: nothing
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 || got[0].Entry.Key() != "1/100//2/" {
		t.Fatalf("initial %+v", got)
	}
	c := mattercore.TargetStruct{Node: 100, Endpoint: 2, Cluster: 0x0006}
	if err := b.MatterWrite(ctx, 0, []mattercore.TargetStruct{c}); err != nil {
		t.Fatal(err)
	}
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 || got[0].Entry.Key() != "1/100//2/6" {
		t.Fatalf("after the change %+v", got)
	}
	if got := h.takeRemoved(); len(got) != 1 || got[0].Entry.Key() != "1/100//2/" {
		t.Fatalf("removed after the change %+v", got)
	}
	b3 := mattercore.NewBinding()
	if err := b3.MatterWrite(ctx, 0, []mattercore.TargetStruct{{Node: 100, Endpoint: 2}}); err != nil {
		t.Fatal(err)
	}
	h.m.Attach(3, b3)
	h.flush()
	if got := h.takeEstablished(); len(got) != 1 || got[0].SourceEndpoint != 3 {
		t.Fatalf("second server %+v", got)
	}
	if !slices.Equal(h.m.Attached(), []uint16{1, 3}) {
		t.Fatalf("attached %v", h.m.Attached())
	}
	h.m.Detach(1)
	h.flush()
	if got := h.takeRemoved(); len(got) != 1 || got[0].SourceEndpoint != 1 {
		t.Fatalf("detach removed %+v", got)
	}
	// A detached server's writes reach nobody.
	if err := b.MatterWrite(ctx, 0, []mattercore.TargetStruct{a}); err != nil {
		t.Fatal(err)
	}
	h.flush()
	if got := h.takeEstablished(); len(got) != 0 {
		t.Fatalf("a detached server's write established %+v", got)
	}
	h.m.Close()
	if got := h.takeRemoved(); len(got) != 1 || got[0].SourceEndpoint != 3 {
		t.Fatalf("close removed %+v", got)
	}
	h.m.Close() // idempotent
	h.m.Register(1, a)
	h.m.GroupKeysChanged(1)
	h.m.Attach(4, mattercore.NewBinding())
	if len(h.m.Attached()) != 0 {
		t.Fatal("attached after Close")
	}
}

// TestReplaceAttachedServer: attaching another server to an endpoint
// disposes the first one's bindings.
func TestReplaceAttachedServer(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	first, second := mattercore.NewBinding(), mattercore.NewBinding()
	if err := first.MatterWrite(ctx, 0, []mattercore.TargetStruct{{Node: 100, Endpoint: 2}}); err != nil {
		t.Fatal(err)
	}
	h.m.Attach(1, first)
	h.flush()
	h.takeEstablished()
	h.m.Attach(1, second)
	h.flush()
	if got := h.takeRemoved(); len(got) != 1 {
		t.Fatalf("removed %+v", got)
	}
}

func TestKindString(t *testing.T) {
	t.Parallel()
	for k, want := range map[Kind]string{KindClient: "client", KindGroup: "group", KindServer: "server", 0: "unknown"} {
		if k.String() != want {
			t.Errorf("%d: %q", k, k.String())
		}
	}
	if _, err := New(Config{}); !errors.Is(err, ErrNoTopology) {
		t.Fatal(err)
	}
	var f GroupSenderFunc = func(context.Context, uint8, uint16) (GroupTarget, error) { return fakeGroupTarget{}, nil }
	if g, err := f.Sender(context.Background(), 1, 1); err != nil || g == nil {
		t.Fatal(err)
	}
}
