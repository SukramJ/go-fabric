// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	writeMu sync.Mutex
	// mu guards bindings.
	mu       sync.RWMutex
	bindings []TargetStruct
	save     func([]TargetStruct) error

	// obsMu guards observers.
	obsMu     sync.Mutex
	observers []*bindingObserver

	host   spec.HostHandlers
	events spec.Events
	veto   spec.WriteVeto
	// changes tells the host's listeners (device.Device.OnChanged) and the
	// bridge's of each change of the list, with its origin.
	changes cluster.AttributeChanges
}

// bindingObserver is one [Binding.OnChange] registration; its address
// identifies it for the stop function.
type bindingObserver struct {
	fn func(fabricIndex uint8, added, removed []bindingdef.TargetStruct)
}

// TargetStruct mirrors the Binding cluster's TargetStruct (Matter
// §9.6.5.1). All fields are optional except Endpoint XOR Group;
// FabricIndex is fabric-scoped. A zero Node or Group is the field's
// absence (neither is a valid id: node-id 0 and group-id 0 are
// unspecified, binding.element.ts's Group "min 1"). Endpoint and Cluster
// have a valid zero — the root endpoint, the Identify cluster — so their
// presence is stated explicitly: EndpointPresent and ClusterPresent mark a
// zero value as present, as matter.js's optional fields do
// (binding.element.ts, BindingServer's entries keep `endpoint: 0`). A
// non-zero Endpoint or Cluster is present either way, and Cluster
// 0xFFFFFFFF stays absent.
type TargetStruct struct {
	Node        uint64 // 0 means "not present"
	Group       uint16 // 0 means "not present"
	Endpoint    uint16 // present when non-zero or EndpointPresent
	Cluster     uint32 // present when non-zero or ClusterPresent; 0xFFFFFFFF never
	FabricIndex uint8
	// EndpointPresent marks Endpoint present when it is 0.
	EndpointPresent bool
	// ClusterPresent marks Cluster present when it is 0.
	ClusterPresent bool
}

// bindingClusterAbsent is the Cluster sentinel the struct's documentation
// named before the read encoder settled on 0; both read as absent.
const bindingClusterAbsent uint32 = 0xFFFFFFFF

// HasNode reports whether the entry names a node.
func (t TargetStruct) HasNode() bool { return t.Node != 0 }

// HasGroup reports whether the entry names a group.
func (t TargetStruct) HasGroup() bool { return t.Group != 0 }

// HasEndpoint reports whether the entry names an endpoint.
func (t TargetStruct) HasEndpoint() bool { return t.Endpoint != 0 || t.EndpointPresent }

// HasCluster reports whether the entry names a cluster.
func (t TargetStruct) HasCluster() bool {
	return (t.Cluster != 0 || t.ClusterPresent) && t.Cluster != bindingClusterAbsent
}

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
			return fmt.Errorf("core: Binding restore: %w", err)
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
	b.notify(contract.OriginHostSet, before, after)
	return nil
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: fn
// learns of each committed change of the Binding list — a controller's
// write from [contract.OriginControllerWrite], the host's SetAttributes
// and a restore from [contract.OriginHostSet], a removed fabric's entries
// from [contract.OriginUnknown] — after the [Binding.OnChange] observers,
// outside the server's locks.
func (b *Binding) OnAttributeChange(fn func(contract.Change)) (unsubscribe func()) {
	return b.changes.OnAttributeChange(fn)
}

// Compile-time assertions: Binding satisfies MatterClusterServer, the
// attribute-lister and the fabric-scoped-reader capabilities.
var (
	_ contract.ClusterServer           = (*Binding)(nil)
	_ contract.ClusterAttributeLister  = (*Binding)(nil)
	_ contract.FabricScopedReader      = (*Binding)(nil)
	_ contract.EventReceiver           = (*Binding)(nil)
	_ contract.AttributeChangeNotifier = (*Binding)(nil)
	_ spec.AttributeSetter             = (*Binding)(nil)
	_ spec.CommandRegistrar            = (*Binding)(nil)
	_ spec.EventEmitting               = (*Binding)(nil)
	_ spec.BeforeWriteSetter           = (*Binding)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (b *Binding) MatterClusterID() uint32 { return bindingClusterID }

// MatterRead returns the bindings list under a copy: every fabric's
// entries, as a read without a request behind it sees them.
func (b *Binding) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case bindingAttrBinding:
		return b.entries(), true
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

// Entries returns every fabric's entries, in list order, in the
// generated form — what matter.js's BindingServer registers with its
// manager when it starts (BindingServer.ts:120-126). It is
// bindingdef.Host's.
func (b *Binding) Entries() []bindingdef.TargetStruct { return defTargets(b.entries()) }

// entries returns every fabric's entries, in list order, under a copy.
func (b *Binding) entries() []TargetStruct {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]TargetStruct{}, b.bindings...)
}

// Def returns the entry in the generated form: a field the entry lacks is
// nil.
func (t TargetStruct) Def() bindingdef.TargetStruct {
	d := bindingdef.TargetStruct{FabricIndex: t.FabricIndex}
	if t.HasNode() {
		n := t.Node
		d.Node = &n
	}
	if t.HasGroup() {
		g := t.Group
		d.Group = &g
	}
	if t.HasEndpoint() {
		e := t.Endpoint
		d.Endpoint = &e
	}
	if t.HasCluster() {
		c := t.Cluster
		d.Cluster = &c
	}
	return d
}

// TargetFromDef returns the entry of a generated TargetStruct, the
// presence flags set for a zero Endpoint or Cluster.
func TargetFromDef(t bindingdef.TargetStruct) TargetStruct {
	e := TargetStruct{FabricIndex: t.FabricIndex}
	if t.Node != nil {
		e.Node = *t.Node
	}
	if t.Group != nil {
		e.Group = *t.Group
	}
	// The flag is set for a zero value only, where it is needed: a
	// non-zero value is present by itself.
	if t.Endpoint != nil {
		e.Endpoint, e.EndpointPresent = *t.Endpoint, *t.Endpoint == 0
	}
	if t.Cluster != nil {
		e.Cluster, e.ClusterPresent = *t.Cluster, *t.Cluster == 0
	}
	return e
}

// defTargets is l in the generated form; nil for nil.
func defTargets(l []TargetStruct) []bindingdef.TargetStruct {
	if l == nil {
		return nil
	}
	out := make([]bindingdef.TargetStruct, len(l))
	for i, t := range l {
		out[i] = t.Def()
	}
	return out
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
		return refusedWrite{sentinel: fmt.Errorf("core: Binding has no writable attribute 0x%04X", attrID), status: err}
	}
	_, fabric, request := im.LookupFabricFilter(ctx)
	if request && fabric == 0 {
		return bindingStatusError{code: im.StatusUnsupportedAccess, msg: "core: Binding is fabric-scoped; the session has no fabric"}
	}
	list, err := bindingWriteList(value, fabric, request)
	if err != nil {
		return err
	}
	if request {
		for i := range list {
			list[i].FabricIndex = fabric
		}
	}
	// The veto sees the written entries as they would be stored, the
	// accessing fabric stamped.
	if err := b.veto.Check(ctx, bindingAttrBinding, slices.Clone(list)); err != nil {
		return err
	}
	origin := contract.OriginFrom(ctx)
	if origin == contract.OriginUnknown {
		origin = contract.OriginControllerWrite // a local writer writes as a controller does
	}
	return b.replace(origin, func(before []TargetStruct) []TargetStruct {
		if !request {
			return list
		}
		after := slices.DeleteFunc(slices.Clone(before), func(t TargetStruct) bool { return t.FabricIndex == fabric })
		return append(after, list...)
	})
}

// replace commits the list build makes of the current one: saved first,
// then stored, then told to the [Binding.OnChange] observers.
func (b *Binding) replace(origin contract.Origin, build func(before []TargetStruct) []TargetStruct) error {
	b.writeMu.Lock()
	b.mu.RLock()
	before := b.bindings
	b.mu.RUnlock()
	after := build(before)
	if after == nil {
		after = []TargetStruct{}
	}
	// The change is stored before it commits: a write the store refuses
	// changes nothing, as a matter.js transaction whose persistence fails
	// rolls back.
	if b.save != nil {
		if err := b.save(slices.Clone(after)); err != nil {
			b.writeMu.Unlock()
			return fmt.Errorf("core: Binding persist: %w", err)
		}
	}
	b.mu.Lock()
	b.bindings = after
	b.mu.Unlock()
	b.writeMu.Unlock()
	b.notify(origin, before, after)
	return nil
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of the Binding list — the only attribute the server holds — replacing
// every fabric's entries with the given ones ([]TargetStruct, or the
// generated []bindingdef.TargetStruct checked against the definition),
// each keeping its FabricIndex, as a matter.js host sets the behavior's
// state (the list is the BindingBehavior's state; BindingServer.ts:120-158
// registers the entries it holds at initialize and diffs them at the end
// of each interaction). It is saved ([Binding.Persist]) and told
// to the [Binding.OnChange] observers like a write; there is no write
// veto. Any other attribute is refused ([spec.ErrNotServed] or the
// definition's refusal).
func (b *Binding) SetAttributes(values map[uint32]any) error {
	var list []TargetStruct
	set := false
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if id != bindingAttrBinding {
			if _, err := bindingInst.CheckSet(id, values[id], nil); err != nil {
				return err
			}
			return fmt.Errorf("%w: matter: Binding attribute 0x%04X", spec.ErrNotHeld, id)
		}
		l, err := bindingWriteList(values[id], 0, false)
		if err != nil {
			return fmt.Errorf("%w: %w", spec.ErrInvalidValue, err)
		}
		list, set = l, true
	}
	if !set {
		return nil
	}
	return b.replace(contract.OriginHostSet, func([]TargetStruct) []TargetStruct { return list })
}

// Handle implements [spec.CommandRegistrar]. Binding has no command, so
// every handler is refused with [spec.ErrNotAccepted].
func (b *Binding) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return b.host.Register(bindingInst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]. Binding defines no event, so
// every event is refused with [spec.ErrNotEmitted].
func (b *Binding) Emit(endpoint uint16, eventID uint32, data any) error {
	return b.events.Emit(bindingInst, endpoint, eventID, data)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (b *Binding) SetMatterEventEmitter(emitter contract.EventEmitter) { b.events.SetEmitter(emitter) }

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write of the Binding list after it was decoded and checked
// and before it is saved or stored, with the written entries as they
// would be stored ([]TargetStruct, the accessing fabric stamped).
func (b *Binding) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	b.veto.Set(fn)
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
	b.notify(contract.OriginUnknown, before, after)
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
			out = append(out, TargetFromDef(t))
		}
		return out, nil
	default:
		_, err := bindingInst.ValidateWrite(bindingAttrBinding, value, nil)
		return nil, refusedWrite{sentinel: fmt.Errorf("core: Binding write expected []TargetStruct, got %T", value), status: err}
	}
}

// errBindingWriteRefused is the sentinel of a written value the definition
// refused.
var errBindingWriteRefused = errors.New("core: Binding write refused")

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
// unregisters the removed ones (BindingServer.ts:127-157). The entries
// are in the generated form; it is bindingdef.Host's.
func (b *Binding) OnChange(fn func(fabricIndex uint8, added, removed []bindingdef.TargetStruct)) (stop func()) {
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
// observers, then the attribute listeners of a list that changed, from
// origin.
func (b *Binding) notify(origin contract.Origin, before, after []TargetStruct) {
	b.obsMu.Lock()
	obs := slices.Clone(b.observers)
	b.obsMu.Unlock()
	diffs := diffBindings(before, after)
	for _, d := range diffs {
		added, removed := defTargets(d.added), defTargets(d.removed)
		for _, o := range obs {
			o.fn(d.fabric, added, removed)
		}
	}
	if len(diffs) > 0 {
		b.changes.NotifyFrom(origin, bindingAttrBinding)
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
	return nil, im.UnsupportedCommandf("core: Binding has no commands (got 0x%02X)", cmdID)
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

// The host methods the typed server's Host returns (brief CK-2).
var _ bindingdef.Host = (*Binding)(nil)
