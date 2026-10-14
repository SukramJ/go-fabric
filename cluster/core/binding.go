// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Binding implements the Matter Binding cluster (0x001E) per Matter
// Core Specification 1.5.1 §9.6, which is mandatory on bridged
// endpoints that support local actions referencing other Matter nodes
// (e.g. a Light switch endpoint binding to a target Light endpoint).
//
// The server keeps the fabric-scoped list and reports every committed
// change through [Binding.OnChange], diffed by [TargetStruct.Key] per
// fabric — what matter.js's BindingServer hands its BindingManager after
// each interaction (packages/node/src/behaviors/binding/BindingServer.ts:
// 120-158). The binding manager (package binding) resolves the entries
// into peers; the server itself originates no traffic.
type Binding struct {
	// writeMu serialises the list's changes with their persistence, so the
	// saves land in the order the changes commit; mu guards the list for
	// readers, which a save never blocks.
	writeMu  sync.Mutex
	mu       sync.RWMutex
	bindings []TargetStruct
	save     func([]TargetStruct) error

	obsMu     sync.Mutex
	observers []*bindingObserver
}

// bindingObserver is one [Binding.OnChange] registration; its address
// identifies it for the stop function.
type bindingObserver struct {
	fn func(fabricIndex uint8, added, removed []TargetStruct)
}

// TargetStruct mirrors the Binding cluster's TargetStruct (Matter
// §9.6.5.1). All fields are optional except Endpoint XOR Group;
// FabricIndex is fabric-scoped. A zero Node, Group or Endpoint is the
// field's absence (endpoint 0 cannot be a target); Cluster is absent at 0
// and at 0xFFFFFFFF, and the read encoder omits it at 0.
type TargetStruct struct {
	Node        uint64 // 0 means "not present"
	Group       uint16 // 0 means "not present"
	Endpoint    uint16 // 0 means "not present"
	Cluster     uint32 // 0 or 0xFFFFFFFF mean "not present"
	FabricIndex uint8
}

// bindingClusterAbsent is the Cluster sentinel the struct's documentation
// named before the read encoder settled on 0; both read as absent.
const bindingClusterAbsent uint32 = 0xFFFFFFFF

// HasNode reports whether the entry names a node.
func (t TargetStruct) HasNode() bool { return t.Node != 0 }

// HasGroup reports whether the entry names a group.
func (t TargetStruct) HasGroup() bool { return t.Group != 0 }

// HasEndpoint reports whether the entry names an endpoint.
func (t TargetStruct) HasEndpoint() bool { return t.Endpoint != 0 }

// HasCluster reports whether the entry names a cluster.
func (t TargetStruct) HasCluster() bool { return t.Cluster != 0 && t.Cluster != bindingClusterAbsent }

// Key is the entry's stable identity: fabric index, node, group, endpoint
// and cluster joined by "/", an absent field empty. Mirrors matter.js
// BindingManager.entryKey (packages/node/src/behaviors/binding/
// BindingManager.ts:714-718), the key BindingServer diffs the list by and
// the manager tracks entries under.
func (t TargetStruct) Key() string {
	field := func(present bool, v uint64) string {
		if !present {
			return ""
		}
		return strconv.FormatUint(v, 10)
	}
	return strings.Join([]string{
		strconv.FormatUint(uint64(t.FabricIndex), 10),
		field(t.HasNode(), t.Node),
		field(t.HasGroup(), uint64(t.Group)),
		field(t.HasEndpoint(), uint64(t.Endpoint)),
		field(t.HasCluster(), uint64(t.Cluster)),
	}, "/")
}

// Cluster ID + revision per Matter §9.6.
const (
	bindingClusterID       = bindingdef.ClusterID
	bindingClusterRevision = bindingdef.Revision

	bindingAttrBinding = bindingdef.AttrBinding
)

// bindingInst is the definition (cluster/spec/binding, ADR 0013, from
// matter.js binding.element.ts): the attribute list and the status of a
// write to anything but Binding come from it, and a write of the
// generated TargetStruct is checked against it. TargetStruct keeps its
// sentinel encoding of the absent fields.
var bindingInst = mustInstance(bindingdef.Definition, spec.Options{})

// NewBinding returns a Binding cluster server with an empty list.
func NewBinding() *Binding {
	return &Binding{bindings: []TargetStruct{}}
}

// BindingConfig keeps a Binding server's list across restarts: the
// Binding attribute is nonvolatile (quality N, matter.js
// packages/model/src/standard/elements/binding.element.ts:21-25), and
// matter.js persists it as every nonvolatile behaviour state.
type BindingConfig struct {
	// Load returns the stored list: nil when none is stored (the server
	// keeps its list), a non-nil list — empty included — to restore.
	Load func() ([]TargetStruct, error)
	// Save stores the list after a change; nil stores nothing. A write
	// whose Save fails is refused and changes nothing.
	Save func([]TargetStruct) error
}

// Persist restores the list cfg.Load returns and saves every later change
// through cfg.Save — a write, and a removed fabric's entries
// ([Binding.RemoveFabric]). Call it before the server's entries are
// registered with a binding manager: matter.js's BindingServer registers
// the entries its restored state holds when it initialises
// (packages/node/src/behaviors/binding/BindingServer.ts:120-126), so the
// manager sees the stored entries at start. A restore that changes the
// list tells the [Binding.OnChange] observers, as a write does. A failed
// Load returns its error and installs nothing.
func (b *Binding) Persist(cfg BindingConfig) error {
	b.writeMu.Lock()
	var stored []TargetStruct
	if cfg.Load != nil {
		var err error
		if stored, err = cfg.Load(); err != nil {
			b.writeMu.Unlock()
			return fmt.Errorf("matter: Binding restore: %w", err)
		}
	}
	b.save = cfg.Save
	b.mu.Lock()
	before := b.bindings
	if stored != nil {
		b.bindings = append([]TargetStruct{}, stored...)
	}
	after := b.bindings
	b.mu.Unlock()
	b.writeMu.Unlock()
	b.notify(before, after)
	return nil
}

// Compile-time assertions: Binding satisfies MatterClusterServer, the
// attribute-lister and the fabric-scoped-reader capabilities.
var (
	_ contract.ClusterServer          = (*Binding)(nil)
	_ contract.ClusterAttributeLister = (*Binding)(nil)
	_ contract.FabricScopedReader     = (*Binding)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (b *Binding) MatterClusterID() uint32 { return bindingClusterID }

// MatterRead returns the bindings list under a copy: every fabric's
// entries, as a read without a request behind it sees them.
func (b *Binding) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case bindingAttrBinding:
		return b.Entries(), true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return bindingInst.ReadGlobal(attrID)
	}
	return nil, false
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a
// fabric-filtered read lists the accessing fabric's entries only; an
// unfiltered one, and a read without a request, list every fabric's (the
// struct carries no fabric-sensitive field). Mirrors matter.js's
// fabric-filtered list view (ListManager.ts FabricFilteredListProxyHandler).
func (b *Binding) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if attrID != bindingAttrBinding || !request || !filtered {
		return b.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := []TargetStruct{}
	for _, t := range b.bindings {
		if t.FabricIndex == fabric {
			out = append(out, t)
		}
	}
	return out, true
}

// Entries returns every fabric's entries, in list order, under a copy —
// what matter.js's BindingServer registers with its manager when it starts
// (BindingServer.ts:120-126).
func (b *Binding) Entries() []TargetStruct {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]TargetStruct{}, b.bindings...)
}

// MatterWrite replaces the bindings list. The value is a []TargetStruct,
// or the generated []bindingdef.TargetStruct the definition decodes, which
// is checked against the definition first (a refused value is
// CONSTRAINT_ERROR, as matter.js's schema decode answers it).
//
// The write is fabric-scoped. With a request behind it the accessing
// fabric arrives on ctx ([im.LookupFabricFilter]; the bridge stamps the
// session's fabric on every write, bridge/receive_dispatch.go
// writeFabricIndex): the write replaces that fabric's entries only, each
// stamped with the fabric index whatever it carried — matter.js injects
// the session's fabric into every written struct
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:461-467)
// — and a session without a fabric is refused with UNSUPPORTED_ACCESS
// (:246-249). Without a request (a local writer) the list is replaced as
// given.
//
// After the write commits, every [Binding.OnChange] observer is told the
// entries each fabric gained and lost, by [TargetStruct.Key]: a changed
// entry is one removed and one added (BindingServer.ts:127-157). A failed
// write tells nobody.
func (b *Binding) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != bindingAttrBinding {
		_, err := bindingInst.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("matter: Binding has no writable attribute 0x%04X", attrID), status: err}
	}
	_, fabric, request := im.LookupFabricFilter(ctx)
	if request && fabric == 0 {
		return bindingStatusError{code: im.StatusUnsupportedAccess, msg: "matter: Binding is fabric-scoped; the session has no fabric"}
	}
	list, err := bindingWriteList(value, fabric, request)
	if err != nil {
		return err
	}
	b.writeMu.Lock()
	b.mu.RLock()
	before := b.bindings
	b.mu.RUnlock()
	var after []TargetStruct
	if request {
		after = slices.DeleteFunc(slices.Clone(before), func(t TargetStruct) bool { return t.FabricIndex == fabric })
		for _, t := range list {
			t.FabricIndex = fabric
			after = append(after, t)
		}
	} else {
		after = list
	}
	if after == nil {
		after = []TargetStruct{}
	}
	// The change is stored before it commits: a write the store refuses
	// changes nothing, as a matter.js transaction whose persistence fails
	// rolls back.
	if b.save != nil {
		if err := b.save(slices.Clone(after)); err != nil {
			b.writeMu.Unlock()
			return fmt.Errorf("matter: Binding persist: %w", err)
		}
	}
	b.mu.Lock()
	b.bindings = after
	b.mu.Unlock()
	b.writeMu.Unlock()
	b.notify(before, after)
	return nil
}

// RemoveFabric drops a removed fabric's entries and tells the
// [Binding.OnChange] observers — matter.js limitNodeDataToAllowedFabrics
// strips a departed fabric's fabric-scoped attribute entries from every
// endpoint (packages/node/src/behavior/cluster/FabricScopedDataHandler.ts:
// 109-122).
//
// A persisted list ([Binding.Persist]) is saved; the fabric is gone
// whether or not the save succeeds, so a failed save still drops the
// entries here (the Save function reports its own failure).
func (b *Binding) RemoveFabric(fabricIndex uint8) {
	b.writeMu.Lock()
	b.mu.Lock()
	before := b.bindings
	after := slices.DeleteFunc(slices.Clone(before), func(t TargetStruct) bool { return t.FabricIndex == fabricIndex })
	if len(after) == len(before) {
		b.mu.Unlock()
		b.writeMu.Unlock()
		return
	}
	b.bindings = after
	b.mu.Unlock()
	if b.save != nil {
		_ = b.save(slices.Clone(after))
	}
	b.writeMu.Unlock()
	b.notify(before, after)
}

// bindingWriteList converts a written value into the server's entries.
func bindingWriteList(value any, fabric uint8, request bool) ([]TargetStruct, error) {
	// The generated decoder hands the list as spec.List (decodeAttrBinding);
	// the bridge's write path delivers it that way.
	if l, ok := value.(spec.List[bindingdef.TargetStruct]); ok {
		value = []bindingdef.TargetStruct(l)
	}
	switch v := value.(type) {
	case []TargetStruct:
		return append([]TargetStruct(nil), v...), nil
	case []bindingdef.TargetStruct:
		if request {
			// The accessing fabric is the entries' fabric before the
			// definition judges them (AttributeWriteResponse.ts:461-467).
			v = slices.Clone(v)
			for i := range v {
				v[i].FabricIndex = fabric
			}
		}
		if _, err := bindingInst.ValidateWrite(bindingAttrBinding, v, nil); err != nil {
			return nil, refusedWrite{sentinel: errBindingWriteRefused, status: err}
		}
		out := make([]TargetStruct, 0, len(v))
		for _, t := range v {
			e := TargetStruct{FabricIndex: t.FabricIndex}
			if t.Node != nil {
				e.Node = *t.Node
			}
			if t.Group != nil {
				e.Group = *t.Group
			}
			if t.Endpoint != nil {
				e.Endpoint = *t.Endpoint
			}
			if t.Cluster != nil {
				e.Cluster = *t.Cluster
			}
			out = append(out, e)
		}
		return out, nil
	default:
		_, err := bindingInst.ValidateWrite(bindingAttrBinding, value, nil)
		return nil, refusedWrite{sentinel: fmt.Errorf("matter: Binding write expected []TargetStruct, got %T", value), status: err}
	}
}

// errBindingWriteRefused is the sentinel of a written value the definition
// refused.
var errBindingWriteRefused = errors.New("matter: Binding write refused")

// bindingStatusError is a Binding refusal with the IM status matter.js
// answers.
type bindingStatusError struct {
	code im.StatusCode
	msg  string
}

func (e bindingStatusError) Error() string                   { return e.msg }
func (e bindingStatusError) MatterStatusCode() im.StatusCode { return e.code }

// OnChange registers fn to learn, after each committed write, the entries
// one fabric gained (added) and lost (removed) — called once per fabric
// whose entries changed, in ascending fabric order, outside the server's
// locks. It returns the function that removes the registration. Mirrors
// the interactionEnd reaction of matter.js's BindingServer, which diffs
// the list by BindingManager.entryKey and registers the added entries and
// unregisters the removed ones (BindingServer.ts:127-157).
func (b *Binding) OnChange(fn func(fabricIndex uint8, added, removed []TargetStruct)) (stop func()) {
	if fn == nil {
		return func() {}
	}
	o := &bindingObserver{fn: fn}
	b.obsMu.Lock()
	b.observers = append(b.observers, o)
	b.obsMu.Unlock()
	return func() {
		b.obsMu.Lock()
		defer b.obsMu.Unlock()
		b.observers = slices.DeleteFunc(b.observers, func(x *bindingObserver) bool { return x == o })
	}
}

// notify diffs before and after by key per fabric and tells the
// observers.
func (b *Binding) notify(before, after []TargetStruct) {
	b.obsMu.Lock()
	obs := slices.Clone(b.observers)
	b.obsMu.Unlock()
	if len(obs) == 0 {
		return
	}
	for _, d := range diffBindings(before, after) {
		for _, o := range obs {
			o.fn(d.fabric, d.added, d.removed)
		}
	}
}

// bindingDiff is one fabric's change.
type bindingDiff struct {
	fabric         uint8
	added, removed []TargetStruct
}

// diffBindings returns, per fabric in ascending order, the entries of
// after whose key before lacks and the entries of before whose key after
// lacks — BindingServer.ts:132-148.
func diffBindings(before, after []TargetStruct) []bindingDiff {
	keys := func(l []TargetStruct) map[string]bool {
		m := make(map[string]bool, len(l))
		for _, t := range l {
			m[t.Key()] = true
		}
		return m
	}
	oldKeys, newKeys := keys(before), keys(after)
	byFabric := map[uint8]*bindingDiff{}
	at := func(f uint8) *bindingDiff {
		d := byFabric[f]
		if d == nil {
			d = &bindingDiff{fabric: f}
			byFabric[f] = d
		}
		return d
	}
	seen := map[string]bool{}
	for _, t := range after {
		k := t.Key()
		if !oldKeys[k] && !seen[k] {
			seen[k] = true
			d := at(t.FabricIndex)
			d.added = append(d.added, t)
		}
	}
	for _, t := range before {
		k := t.Key()
		if !newKeys[k] && !seen[k] {
			seen[k] = true
			d := at(t.FabricIndex)
			d.removed = append(d.removed, t)
		}
	}
	out := make([]bindingDiff, 0, len(byFabric))
	for _, d := range byFabric {
		out = append(out, *d)
	}
	slices.SortFunc(out, func(a, b bindingDiff) int { return int(a.fabric) - int(b.fabric) })
	return out
}

// MatterInvoke always rejects — Binding has no commands.
func (b *Binding) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("matter: Binding has no commands (got 0x%02X)", cmdID)
}

// MatterReportable lists the subscribe-able attributes.
func (b *Binding) MatterReportable() []uint32 {
	return []uint32{bindingAttrBinding}
}

// MatterAttributes lists every Binding (0x001E) attribute the server
// implements via MatterRead. Apple Home's HAP service rebuild reads
// the full attribute set; without this the dispatcher falls back to
// MatterReportable's single attribute.
func (b *Binding) MatterAttributes() []uint32 {
	return bindingInst.MatterAttributes()
}
