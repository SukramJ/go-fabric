// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/binding/BindingManagerTest.ts
// at 862ad75f: the cases that need a commissioned node — the register
// queue before the node is online, two Binding servers sharing one group,
// the server check against an endpoint with the cluster as a client only,
// and the fabric replacement. The cases on the manager alone are in
// binding/manager_parity_matterjs_test.go.
//
// matter.js runs them on MockServerNode with an OnOffLightSwitch and an
// added fabric, calling BindingManager.register with a fake BindingServer;
// the port commissions the device on fabrictest with the switch under the
// Aggregator and writes the switch's Binding list locally, as the node's
// Binding server hands every entry to the manager (BindingServer.ts:
// 120-157). provisionGroupKey is KeySetWrite and GroupKeyMap on the root's
// GroupKeyManagement, locally with the fabric's administrator; the
// established and removed observers are the node's Bindings().OnEstablished
// and OnRemoved; a warning is the manager's warn-level log record, its
// reason matter.js's message; FabricManager.replaceFabric is UpdateNOC with
// the same fabric ID and node ID, committed by CommissioningComplete over a
// new CASE session (UpdateNOC closes the fabric's sessions, as matter.js
// Fabric.replaced does — bridge.CloseFabricSessionsAfterUpdate), as brief
// BP ported "fabric is replaced".
package node_test

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// bqEpochKey is the files' epoch key 0, d0d1…df.
var bqEpochKey, _ = hex.DecodeString("d0d1d2d3d4d5d6d7d8d9dadbdcdddedf")

// bqKeySet is the files' key set id 0x1a1.
const bqKeySet uint16 = 0x01A1

// bqRecord is one log record of the device: its level, message and
// attributes.
type bqRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

// bqLog records the device's log records at info and above.
type bqLog struct {
	mu   sync.Mutex
	recs []bqRecord
}

type bqHandler struct {
	log   *bqLog
	attrs []slog.Attr
}

func (h *bqHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }

func (h *bqHandler) Handle(_ context.Context, r slog.Record) error {
	rec := bqRecord{level: r.Level, msg: r.Message, attrs: map[string]string{}}
	for _, a := range h.attrs {
		rec.attrs[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	h.log.mu.Lock()
	h.log.recs = append(h.log.recs, rec)
	h.log.mu.Unlock()
	return nil
}

func (h *bqHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &bqHandler{log: h.log, attrs: append(slices.Clone(h.attrs), as...)}
}

func (h *bqHandler) WithGroup(string) slog.Handler { return h }

// records are the records so far.
func (l *bqLog) records() []bqRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.recs)
}

// reasons are the reason attributes of the binding manager's records at
// level — matter.js's messages of the BindingManager facility.
func (l *bqLog) reasons(level slog.Level) []string {
	var out []string
	for _, r := range l.records() {
		if reason, ok := r.attrs["reason"]; ok && r.level == level && strings.HasPrefix(r.msg, "binding.") {
			out = append(out, reason)
		}
	}
	return out
}

// mark is the number of records so far, for problemsSince.
func (l *bqLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recs)
}

// problemsSince are the records at warn level or above after mark — what
// matter.js's problemsDuring collects while its block runs.
func (l *bqLog) problemsSince(mark int) []string {
	var out []string
	for _, r := range l.records()[mark:] {
		if r.level >= slog.LevelWarn {
			out = append(out, r.msg+" "+r.attrs["reason"]+" "+r.attrs["err"])
		}
	}
	return out
}

// bqHost is what the established and removed observers saw.
type bqHost struct {
	mu          sync.Mutex
	established []binding.Resolution
	removed     []binding.Resolution
}

func observeBindings(m *binding.Manager) *bqHost {
	h := &bqHost{}
	m.OnRemoved(func(r binding.Resolution) {
		h.mu.Lock()
		h.removed = append(h.removed, r)
		h.mu.Unlock()
	})
	m.OnEstablished(func(r binding.Resolution) {
		h.mu.Lock()
		h.established = append(h.established, r)
		h.mu.Unlock()
	})
	return h
}

func (h *bqHost) takeEstablished() []binding.Resolution {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.established)
}

func (h *bqHost) takeRemoved() []binding.Resolution {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.removed)
}

// bqSwitch is a commissioned device with OnOffLightSwitches under the
// Aggregator — MockServerNode.createOnline({ device: OnOffLightSwitchDevice })
// with node.addFabric().
type bqSwitch struct {
	site     *fabrictest.Site
	pair     *fabrictest.Pair
	dev      *fabrictest.Device
	log      *bqLog
	fabric   uint8
	nodeID   uint64
	switches []*device.OnOffLightSwitch
	eps      []uint16
	keyMap   []core.GroupKeyMapStruct
}

// bqOptions shapes a bqSwitch.
type bqOptions struct {
	// switches is the number of switches; 0 is one.
	switches int
	// clients are client clusters the switches declare beyond the device
	// type's (withClientClusters).
	clients []uint32
	// prepare, when set, runs with the node before every start.
	prepare func(n *node.Node)
	// client runs the node's client stack on the site (node.Client).
	client bool
}

func newBQSwitch(t *testing.T, o bqOptions) *bqSwitch {
	t.Helper()
	b := &bqSwitch{log: &bqLog{}}
	if o.client {
		// Two nodes keep sub-second timers running for minutes of fake time;
		// a short settle steps them in seconds of wall time.
		b.site = steppedSite(t)
	} else {
		b.site = quietSite(t)
	}
	if o.switches == 0 {
		o.switches = 1
	}
	bridged := make([]node.Endpoint, 0, o.switches)
	for i := range o.switches {
		key := "switch:" + string(rune('1'+i))
		sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: b.site.Clock()},
			device.WithClientClusters(o.clients...))
		if err != nil {
			t.Fatal(err)
		}
		b.switches = append(b.switches, sw)
		bridged = append(bridged, sw)
	}
	b.pair = b.site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: bridged,
		Configure: func(cfg *node.Config) {
			cfg.Logger = slog.New(&bqHandler{log: b.log})
			cfg.Client.Enabled = o.client
		},
		Prepare: o.prepare,
	}})
	b.dev = b.pair.Device
	for i := range o.switches {
		ep, ok := b.dev.EndpointOf("switch:" + string(rune('1'+i)))
		if !ok {
			t.Fatalf("switch %d has no endpoint", i+1)
		}
		b.eps = append(b.eps, ep)
	}
	fabrics, err := b.dev.Node().Fabrics(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %v, %v", fabrics, err)
	}
	b.fabric, b.nodeID = fabrics[0].FabricIndex, fabrics[0].NodeID
	return b
}

// writeBinding sets the Binding list of switch i locally —
// `agent.get(BindingServer).state.binding = [...]` — on the fabric.
func (b *bqSwitch) writeBinding(t *testing.T, i int, entries ...core.TargetStruct) {
	t.Helper()
	if entries == nil {
		entries = []core.TargetStruct{}
	}
	if err := b.dev.Act(func(*node.Node) error {
		return b.switches[i].Binding().MatterWrite(im.WithFabricFilter(parityCtx(t), false, b.fabric), bindingdef.AttrBinding, entries)
	}); err != nil {
		t.Fatalf("Binding write: %v", err)
	}
}

// provisionKey is provisionGroupKey: KeySetWrite of the key set and the
// group mapped to it in GroupKeyMap, on the root's GroupKeyManagement.
func (b *bqSwitch) provisionKey(t *testing.T, group, keySet uint16) {
	t.Helper()
	b.keyMap = append(b.keyMap, core.GroupKeyMapStruct{GroupID: group, GroupKeySetID: keySet, FabricIndex: b.fabric})
	if err := b.dev.Act(func(n *node.Node) error {
		gkm := n.Root().GroupKeyManagement
		if _, err := gkm.MatterInvoke(adminContext(b.fabric), 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{
			GroupKeySetID: keySet, EpochKey0: bqEpochKey, EpochStartTime0: 1,
		}}); err != nil {
			return err
		}
		return gkm.MatterWrite(adminContext(b.fabric), 0x0000, slices.Clone(b.keyMap))
	}); err != nil {
		t.Fatalf("provisioning group %d: %v", group, err)
	}
}

// eventually waits for cond on the site.
func (b *bqSwitch) eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	if err := b.site.Eventually(parityCtx(t), what, cond); err != nil {
		t.Fatal(err)
	}
}

// waitReason waits for the binding manager's record of reason at level.
func (b *bqSwitch) waitReason(t *testing.T, level slog.Level, reason string) {
	t.Helper()
	b.eventually(t, reason, func() bool { return slices.Contains(b.log.reasons(level), reason) })
}

// selfBinding is an entry naming our node and endpoint, no cluster.
func (b *bqSwitch) selfBinding(ep uint16) core.TargetStruct {
	return core.TargetStruct{Node: b.nodeID, Endpoint: ep, EndpointPresent: true}
}

// TestBindingManagerNodeParityMatterJS ports the cases of the describe
// "BindingManager" (BindingManagerTest.ts:109-885) that need a
// commissioned node.
func TestBindingManagerNodeParityMatterJS(t *testing.T) {
	holdSite(t)

	// :118-145. matter.js registers before the node is online and the
	// queue flushes on its online event (BindingManager.ts:65-78,
	// :119-129, :230-237); the Go node attaches its Binding servers once it
	// is online and its CASE identities are loaded (node.Start,
	// startBindings), so the entry stored before a restart registers then.
	// The observer is wired before the start (Topology.Prepare).
	t.Run("queues register calls before node online; flushes after online", func(t *testing.T) {
		var (
			mu      sync.Mutex
			host    *bqHost
			restart bool
			early   int
		)
		b := newBQSwitch(t, bqOptions{prepare: func(n *node.Node) {
			mu.Lock()
			defer mu.Unlock()
			if restart {
				host = observeBindings(n.Bindings())
				early = len(host.takeEstablished())
			}
		}})
		b.writeBinding(t, 0, b.selfBinding(b.eps[0]))
		mu.Lock()
		restart = true
		mu.Unlock()
		if err := b.dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		h, before := host, early
		mu.Unlock()
		if h == nil || before != 0 {
			t.Fatalf("observer %v, %d established before the start", h, before)
		}
		b.eventually(t, "the stored entry established", func() bool { return len(h.takeEstablished()) > 0 })
		got := h.takeEstablished()
		if len(got) != 1 || got[0].Kind != binding.KindServer || got[0].Endpoint != b.eps[0] {
			t.Fatalf("established %+v, want one server resolution on the switch", got)
		}
		if got := b.log.reasons(slog.LevelWarn); len(got) != 0 {
			t.Errorf("the manager warned %q", got)
		}
	})

	// :327-362. Two Binding servers — two switches — bind one group: both
	// resolve to the one group session of the fabric's group, the same
	// sender (groups.Manager caches it per group and key).
	t.Run("group kind: two registrations against the same groupId share the same ClientGroup", func(t *testing.T) {
		b := newBQSwitch(t, bqOptions{switches: 2})
		h := observeBindings(b.dev.Node().Bindings())
		b.provisionKey(t, 7, bqKeySet)
		b.writeBinding(t, 0, core.TargetStruct{Group: 7})
		b.writeBinding(t, 1, core.TargetStruct{Group: 7})
		b.eventually(t, "both group bindings established", func() bool { return len(h.takeEstablished()) == 2 })
		got := h.takeEstablished()
		if got[0].Kind != binding.KindGroup || got[1].Kind != binding.KindGroup {
			t.Fatalf("established %+v, want two group resolutions", got)
		}
		if got[0].SourceEndpoint == got[1].SourceEndpoint {
			t.Fatalf("both resolutions are of endpoint %d", got[0].SourceEndpoint)
		}
		if got[0].Group == nil || got[0].Group != got[1].Group {
			t.Errorf("the resolutions' groups %#v and %#v are not the same", got[0].Group, got[1].Group)
		}
	})

	// :554-579. The switch declares OnOff as a client; a self-binding to
	// its OnOff finds no OnOff server (node.nodeTopology.HasServer asks the
	// endpoint's servers, never its client list).
	t.Run("kind=server rejects when the endpoint only has the cluster as a client, not a server", func(t *testing.T) {
		b := newBQSwitch(t, bqOptions{})
		h := observeBindings(b.dev.Node().Bindings())
		entry := b.selfBinding(b.eps[0])
		entry.Cluster = 0x0006
		b.writeBinding(t, 0, entry)
		b.waitReason(t, slog.LevelWarn, "Self-binding references cluster not installed as server on target endpoint")
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v", got)
		}
	})

	// replaceFabric is UpdateNOC with the fabric's ID and our node id,
	// committed by CommissioningComplete over a new CASE session: the
	// device closed the session UpdateNOC came on.
	replace := func(t *testing.T, b *bqSwitch) {
		t.Helper()
		if got := updateNOC(t, b.pair, b.pair.Controller.Fabric().FabricID, b.nodeID); got != uint64(core.NOCStatusOK) {
			t.Fatalf("UpdateNOC StatusCode %d, want Ok", got)
		}
		b.pair.Peer.Disconnect(parityCtx(t), errors.New("UpdateNOC closed the fabric's sessions"))
		res, err := fabrictest.Invoke(parityCtx(t), b.pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0030, Command: 0x04}, nil)
		if err != nil {
			t.Fatalf("CommissioningComplete: %v", err)
		}
		if el, ok := field(res.Fields, 0); !res.HasFields || !ok || el.Uint != 0 {
			t.Fatalf("CommissioningComplete %+v", res)
		}
	}

	// :792-820. The key goes in after the replacement, locally.
	t.Run("kind=group resolves on a key provisioned after its fabric was replaced", func(t *testing.T) {
		b := newBQSwitch(t, bqOptions{})
		h := observeBindings(b.dev.Node().Bindings())
		b.writeBinding(t, 0, core.TargetStruct{Group: 46})
		b.waitReason(t, slog.LevelInfo, "Group binding waits for the fabric to hold a key for the group")
		if got := h.takeEstablished(); len(got) != 0 {
			t.Fatalf("established %+v before the key", got)
		}
		replace(t, b)
		b.provisionKey(t, 46, bqKeySet)
		b.eventually(t, "the group binding established", func() bool { return len(h.takeEstablished()) > 0 })
		if got := h.takeEstablished(); len(got) != 1 || got[0].Kind != binding.KindGroup {
			t.Fatalf("established %+v, want one group resolution", got)
		}
	})

	// :822-853. "A session established before UpdateNOC still holds the
	// previous Fabric object", through which matter.js provisions the key.
	// The Go node keeps one group state per fabric index (groups.Manager)
	// and closes the fabric's sessions on UpdateNOC, so nothing holds a
	// superseded fabric: the key goes in the way an administrator's session
	// reaches the fabric, KeySetWrite and GroupKeyMap over CASE — the path
	// whose state the replacement must not leave behind.
	t.Run("kind=group resolves on a key provisioned through the fabric object a replacement superseded", func(t *testing.T) {
		b := newBQSwitch(t, bqOptions{})
		h := observeBindings(b.dev.Node().Bindings())
		b.writeBinding(t, 0, core.TargetStruct{Group: 45})
		b.waitReason(t, slog.LevelInfo, "Group binding waits for the fabric to hold a key for the group")
		replace(t, b)
		res, err := fabrictest.Invoke(parityCtx(t), b.pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003F, Command: 0x00}, keySetWrite{id: bqKeySet})
		if err != nil || res.Status != im.StatusSuccess {
			t.Fatalf("KeySetWrite: %+v %v", res, err)
		}
		if err := fabrictest.Write(parityCtx(t), b.pair, attrPath(0, 0x003F, 0x0000), groupKeyMap{group: 45, keySet: bqKeySet}); err != nil {
			t.Fatalf("GroupKeyMap: %v", err)
		}
		b.eventually(t, "the group binding established", func() bool { return len(h.takeEstablished()) > 0 })
		if got := h.takeEstablished(); len(got) != 1 || got[0].Kind != binding.KindGroup {
			t.Fatalf("established %+v, want one group resolution", got)
		}
	})
}
