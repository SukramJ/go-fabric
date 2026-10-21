// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/measurement"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// Errors New and AddPart wrap.
var (
	// ErrConfiguration: a device the device type or the host's options do
	// not allow.
	ErrConfiguration = errors.New("device: configuration refused")
	// ErrNoDefaultServer: a cluster the device mounts that has no default
	// server here; the host supplies one with [WithServer].
	ErrNoDefaultServer = errors.New("device: no default server")
	// ErrComposition: a part the device type does not compose.
	ErrComposition = errors.New("device: composition refused")
)

// Device is one endpoint of a generated device type with its cluster
// servers: matter.js's Endpoint of a MutableEndpoint type
// (packages/node/src/endpoint/Endpoint.ts). It is the endpoint's
// [contract.EndpointSource]; [Device.Spec] hands it to the endpoint
// assembler.
type Device struct {
	typ     *Type
	opts    Options
	servers []contract.ClusterServer
	// source is what Spec hands the assembler: the device itself, or the
	// device with the host's change notifier ([WithChangeNotifier]).
	source contract.EndpointSource
	// measurement is the host's live reading ([WithMeasurement]) and
	// measured the clusters the assembler materialises from it.
	measurement contract.MeasurementSource
	measured    map[uint32]bool
	// optIn are the assembler's optional clusters the host opted into
	// ([WithOptional] for Groups or ScenesManagement).
	optIn []uint32
	// clients are the client clusters the host declared beyond the type's
	// mandatory ones ([WithClientClusters]).
	clients []uint32

	// mu guards parts.
	mu    sync.Mutex
	parts []*Device
}

// Compile-time assertion.
var _ contract.EndpointSource = (*Device)(nil)

// assemblerMounted are the clusters the endpoint assembler mounts on every
// endpoint whose device type asks for them, so the device layer never
// mounts them a second time: Descriptor and BridgedDeviceBasicInformation
// on every bridged endpoint, Identify on every bridged endpoint and on a
// part whose device type mandates it, Groups and ScenesManagement where the
// device type mandates them or allows them and the host opts in
// ([WithOptional] → [endpoint.Spec.OptIn]) — a Groups or ScenesManagement
// server the source supplies is replaced by the stack's
// (endpoint/materialize.go ClusterServers, partClusterServers,
// mountGroups, mountScenes).
var assemblerMounted = map[uint32]string{
	0x001D: "Descriptor",
	0x0039: "BridgedDeviceBasicInformation",
	0x0003: "Identify",
	0x0004: "Groups",
	0x0062: "ScenesManagement",
}

// New returns a device of type t: every server cluster the type mandates,
// and every optional one the host opted in, with its default server or the
// one the host supplied. It refuses what it can judge without a topology —
// a feature selection the cluster or the device type does not allow, a
// cluster the type does not allow, a mandatory cluster without a server —
// and names the schema requirement. The rest of device-type conformance is
// the assembler's ([endpoint.ValidateDeviceTypes], ADR 0016).
//
// Mirrors matter.js's construction of an Endpoint from its generated
// MutableEndpoint type (packages/node/src/devices/dimmable-light.ts:103-116,
// packages/node/src/endpoint/Endpoint.ts:596-637).
func New(t *Type, opts Options, with ...Option) (*Device, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: no device type", ErrConfiguration)
	}
	if opts.StableKey == nil {
		return nil, fmt.Errorf("%w: %s needs a StableKey", ErrConfiguration, t.Name)
	}
	s := newSettings(with)
	s.clock = opts.Clock
	if s.nilSrv {
		return nil, fmt.Errorf("%w: %s: WithServer(nil)", ErrConfiguration, t.Name)
	}
	measured, err := measuredClusters(t, s)
	if err != nil {
		return nil, err
	}
	if err := checkOptedClusters(t, s, measured); err != nil {
		return nil, err
	}
	d := &Device{typ: t, opts: opts, measurement: s.measurement, measured: measured, clients: slices.Clone(s.clients)}
	for id := range optInMounted {
		if s.optional[id] {
			d.optIn = append(d.optIn, id)
		}
	}
	slices.Sort(d.optIn)
	d.source = d
	if s.notifier != nil {
		d.source = &notifyingSource{Device: d, notifier: s.notifier}
	}
	// The defaults are built in [buildOrder] — a default that couples to
	// the endpoint's other servers after them — and mounted in the type's
	// order.
	built := make(map[uint32]contract.ClusterServer, len(t.Servers))
	sibling := func(id uint32) contract.ClusterServer { return built[id] }
	for _, i := range buildOrder(t.Servers) {
		r := &t.Servers[i]
		if _, ok := assemblerMounted[r.ID]; ok || measured[r.ID] {
			continue
		}
		srv, ok := s.servers[r.ID]
		if !ok && !r.Mandatory && !s.optional[r.ID] {
			continue
		}
		if !ok {
			var err error
			if srv, err = defaultServer(t, r, s, sibling); err != nil {
				return nil, err
			}
		}
		built[r.ID] = srv
	}
	for i := range t.Servers {
		if srv, ok := built[t.Servers[i].ID]; ok {
			d.servers = append(d.servers, srv)
		}
	}
	// Host servers for clusters the type does not list, after the type's
	// own (MutableEndpoint.ts:95-110 extends the type's behaviors).
	for _, id := range s.serverOrder {
		if _, listed := t.Server(id); !listed {
			d.servers = append(d.servers, s.servers[id])
		}
	}
	return d, nil
}

// notifyingSource is a device whose host model notifies its changes
// ([WithChangeNotifier]): the endpoint source the bridge subscribes to.
type notifyingSource struct {
	*Device
	notifier contract.ChangeNotifier
}

// OnMatterValueChanged implements [contract.ChangeNotifier].
func (n *notifyingSource) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	return n.notifier.OnMatterValueChanged(cb)
}

// measuredClusters are the clusters the host's measurement source
// ([WithMeasurement]) materialises — the assembler's for this device. The
// materialiser runs once here, with no endpoint id, to learn the set; the
// assembler runs it again on every dispatch with the endpoint's
// (endpoint/materialize.go ClusterServers).
func measuredClusters(t *Type, s *settings) (map[uint32]bool, error) {
	if !s.measurementSet {
		return nil, nil
	}
	if s.measurement == nil {
		return nil, fmt.Errorf("%w: %s: WithMeasurement(nil)", ErrConfiguration, t.Name)
	}
	class := s.measurement.MatterMeasurementClass()
	servers := measurement.FromMeasurementClass(class, s.measurement, contract.MeasurementContext{})
	if len(servers) == 0 {
		return nil, fmt.Errorf("%w: %s: the measurement source (class %d) materialises no cluster", ErrConfiguration, t.Name, class)
	}
	out := make(map[uint32]bool, len(servers))
	for _, srv := range servers {
		out[srv.MatterClusterID()] = true
	}
	return out, nil
}

// checkOptedClusters refuses an option for a cluster the device type does
// not allow as a server — a host server ([WithServer]) excepted — or one
// the assembler mounts itself, measured clusters included.
func checkOptedClusters(t *Type, s *settings, measured map[uint32]bool) error {
	ids := make([]uint32, 0, len(s.optional)+len(s.servers)+len(s.initial))
	for id := range s.optional {
		ids = append(ids, id)
	}
	for id := range s.servers {
		ids = append(ids, id)
	}
	for id := range s.initial {
		ids = append(ids, id)
	}
	for id, o := range s.elements {
		ids = append(ids, id)
		if o.Features != 0 {
			return fmt.Errorf("%w: %s: WithElements for cluster 0x%04X states features; select them with WithFeatures", ErrConfiguration, t.Name, id)
		}
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if name, ok := assemblerMounted[id]; ok && (!optInMounted[id] || !onlyOptIn(s, id)) {
			return fmt.Errorf("%w: %s: the endpoint assembler mounts %s (0x%04X) itself", ErrConfiguration, t.Name, name, id)
		}
		if measured[id] {
			return fmt.Errorf("%w: %s: cluster 0x%04X is served from the measurement source (WithMeasurement)", ErrConfiguration, t.Name, id)
		}
		if _, ok := t.Server(id); !ok && !onlyHostServer(s, id) {
			return fmt.Errorf("%w: %s has no server cluster requirement for cluster 0x%04X", ErrConfiguration, t.Name, id)
		}
	}
	for id := range s.servers {
		if _, ok := s.features[id]; ok {
			return fmt.Errorf("%w: %s: WithFeatures for cluster 0x%04X, whose server the host supplies", ErrConfiguration, t.Name, id)
		}
		if _, ok := s.elements[id]; ok {
			return fmt.Errorf("%w: %s: WithElements for cluster 0x%04X, whose server the host supplies", ErrConfiguration, t.Name, id)
		}
		if _, ok := s.initial[id]; ok {
			return fmt.Errorf("%w: %s: WithInitial for cluster 0x%04X, whose server the host supplies", ErrConfiguration, t.Name, id)
		}
	}
	return nil
}

// optInMounted are the assembler's clusters a device opts into with
// [WithOptional] where its type lists them as optional: the assembler then
// mounts the stack's server ([endpoint.Spec.OptIn]).
var optInMounted = map[uint32]bool{
	0x0004: true, // Groups
	0x0062: true, // ScenesManagement
}

// onlyOptIn reports whether the host opts into id and states nothing else
// about it.
func onlyOptIn(s *settings, id uint32) bool {
	_, host := s.servers[id]
	_, initial := s.initial[id]
	_, elements := s.elements[id]
	_, features := s.features[id]
	return s.optional[id] && !host && !initial && !elements && !features
}

// onlyHostServer reports whether the host supplies its own server for id
// and states nothing else about it: no feature selection, initial values,
// elements or opt-in, which need a requirement to apply to.
func onlyHostServer(s *settings, id uint32) bool {
	_, host := s.servers[id]
	_, initial := s.initial[id]
	_, elements := s.elements[id]
	_, features := s.features[id]
	return host && !initial && !elements && !features && !s.optional[id]
}

// defaultServer builds the default server of one cluster — its self-held
// server ([selfHeldDefaults]) or the generated one — with the mandated (or
// the host's) features, the elements the device type makes mandatory, and
// the defaults it states as initial values: matter.js's
// `BaseServer.with(features).alter(...)` of the generated device file
// (ClusterRequirements.ts:137-208, RequirementGenerator.ts:253-296).
// sibling returns the device's servers built before it.
func defaultServer(t *Type, r *ClusterRequirement, s *settings, sibling func(uint32) contract.ClusterServer) (contract.ClusterServer, error) {
	def := r.Definition
	if def == nil {
		return nil, fmt.Errorf("%w: %s requires %s (0x%04X, conformance %q) and no generated definition is committed for it: supply its server with WithServer",
			ErrNoDefaultServer, t.Name, r.Name, r.ID, r.Conformance)
	}
	// The default selection is the device type's mandated features and the
	// features the cluster itself marks mandatory ("M"), which
	// spec.CheckFeatures demands of every selection — EnergyEvse's PREF
	// (owner's decision, 2026-10-11).
	features := r.Features | clusterMandatedFeatures(def)
	if sel, ok := s.features[r.ID]; ok {
		if missing := r.Features &^ sel; missing != 0 {
			return nil, fmt.Errorf("%w: %s requires %s feature %s (conformance M); the selection 0x%X leaves it out",
				ErrConfiguration, t.Name, r.Name, strings.Join(featureNames(r, missing), ", "), sel)
		}
		features = sel
	}
	if err := spec.CheckFeatures(def, features); err != nil {
		// RequirementGenerator.ts:188-199, EndpointFile.ts:144-158: a
		// cluster whose mandated features are no legal selection is not
		// mounted by default; the application must choose its features.
		return nil, fmt.Errorf("%w: %s %s needs a feature selection (WithFeatures): %w", ErrConfiguration, t.Name, r.Name, err)
	}
	opts := spec.Options{Features: features}
	if extra, ok := s.elements[r.ID]; ok {
		opts.Attributes = append(opts.Attributes, extra.Attributes...)
		opts.Commands = append(opts.Commands, extra.Commands...)
		opts.Events = append(opts.Events, extra.Events...)
	}
	for _, e := range r.Elements {
		if e.Presence != PresenceMandatory {
			continue
		}
		switch e.Element {
		case schema.RequirementAttribute:
			opts.Attributes = append(opts.Attributes, e.ID)
		case schema.RequirementCommand:
			opts.Commands = append(opts.Commands, e.ID)
		case schema.RequirementEvent:
			opts.Events = append(opts.Events, e.ID)
		default:
		}
	}
	inst, err := spec.New(def, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrConfiguration, t.Name, r.Name, err)
	}
	initial := map[uint32]any{}
	for _, e := range r.Elements {
		if e.Element != schema.RequirementAttribute || !e.HasDefault || !inst.Serves(e.ID) {
			continue
		}
		if v, ok := typedDefault(def.Attribute(e.ID), e.Default); ok {
			initial[e.ID] = v
		}
	}
	for id, v := range s.initial[r.ID] {
		initial[id] = v
	}
	if factory, held := selfHeldDefaults[r.ID]; held {
		srv, err := factory(defaultBuild{def: def, opts: opts, clock: s.clock, sibling: sibling})
		if err == nil {
			err = checkSelection(r.Name, srv, opts)
		}
		if err == nil {
			err = applyInitial(r.Name, srv, initial)
		}
		switch {
		case err == nil:
			return srv, nil
		case !errors.Is(err, errSelectionNotServed):
			return nil, fmt.Errorf("%w: %s %s: %w", ErrConfiguration, t.Name, r.Name, err)
		}
		// A selection the self-held server does not serve keeps the
		// generated server.
	}
	// The generated default is matter.js's generated behavior: its
	// commands are unimplemented until the host registers handlers
	// (ValidatedElements.ts:341-343, :361-368 at 0ceb202d).
	srv, err := spec.NewServer(def, opts, spec.ServerConfig{Initial: initial, Unimplemented: true})
	switch {
	case errors.Is(err, spec.ErrFabricScoped):
		return nil, fmt.Errorf("%w: %s %s: %w: supply its server with WithServer", ErrNoDefaultServer, t.Name, r.Name, err)
	case err != nil:
		return nil, fmt.Errorf("%w: %s %s: %w", ErrConfiguration, t.Name, r.Name, err)
	}
	return srv, nil
}

// clusterMandatedFeatures is the FeatureMap of the features whose own
// conformance is "M".
func clusterMandatedFeatures(def *spec.Cluster) uint32 {
	var mask uint32
	for i := range def.Features {
		if def.Features[i].Conformance.Op == spec.ConfMandatory {
			mask |= def.Features[i].Mask()
		}
	}
	return mask
}

// typedDefault converts a numeric default the device type states into the
// Go type of the attribute's kind, as the generated server stores it.
func typedDefault(a *spec.Attribute, v float64) (any, bool) {
	if a == nil {
		return nil, false
	}
	switch a.Type.Kind {
	case spec.KindUint, spec.KindEnum, spec.KindBitmap:
		if v < 0 {
			return nil, false
		}
		return uint64(v), true
	case spec.KindInt:
		return int64(v), true
	case spec.KindFloat32, spec.KindFloat64:
		return v, true
	default:
		return nil, false
	}
}

// featureNames names the bits of mask among the requirement's mandated
// features.
func featureNames(r *ClusterRequirement, mask uint32) []string {
	var out []string
	for _, name := range r.FeatureNames {
		if r.Definition == nil {
			out = append(out, name)
			continue
		}
		if f := r.Definition.Feature(name); f != nil && mask&f.Mask() != 0 {
			out = append(out, name)
		}
	}
	return out
}

// Type returns the device's type.
func (d *Device) Type() *Type { return d.typ }

// Server returns the device's server for clusterID, or nil when it mounts
// none — a cluster served from the measurement source ([WithMeasurement])
// included, whose servers the assembler builds per dispatch.
func (d *Device) Server(clusterID uint32) contract.ClusterServer {
	for _, srv := range d.servers {
		if srv.MatterClusterID() == clusterID {
			return srv
		}
	}
	return nil
}

// Servers returns the device's servers in the device type's requirement
// order. The slice is fresh.
func (d *Device) Servers() []contract.ClusterServer { return slices.Clone(d.servers) }

// MatterDeviceType implements [contract.EndpointSource].
func (d *Device) MatterDeviceType() uint16 { return d.typ.ID }

// MatterClusterServers implements [contract.EndpointSource]: the device's
// servers, without the ones the assembler mounts.
func (d *Device) MatterClusterServers() []contract.ClusterServer { return d.Servers() }

// AddPart makes p a part of the device — matter.js's `endpoint.parts.add`
// (packages/node/src/endpoint/properties/Parts.ts:37-73). matter.js
// validates an insertion for endpoint-number identity only
// (Parts.ts:195-213) and leaves composition to device-type validation;
// the device layer refuses here what it can judge without a topology: a
// part whose device type the device type does not name as a component
// (its deviceType requirements, schema.DeviceTypeDefinitionOf, through its
// base types), except under a BridgedNode, whose "Separate Endpoints"
// pattern hosts any application device type on descendants
// (packages/node/src/endpoints/bridged-node.ts:39-73).
func (d *Device) AddPart(p *Device) error {
	if p == nil {
		return fmt.Errorf("%w: %s: nil part", ErrComposition, d.typ.Name)
	}
	if p == d || p.contains(d) {
		return fmt.Errorf("%w: %s: a part may not contain its owner", ErrComposition, d.typ.Name)
	}
	if !composes(d.typ, p.typ) {
		return fmt.Errorf("%w: %s (%s) states no component device type %s; its parts must be device types its requirements name",
			ErrComposition, d.typ.Name, d.typ.Classification, p.typ.Name)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if slices.Contains(d.parts, p) {
		return nil
	}
	d.parts = append(d.parts, p)
	return nil
}

// contains reports whether d is p or has p among its descendants.
func (d *Device) contains(p *Device) bool {
	if d == p {
		return true
	}
	for _, c := range d.Parts() {
		if c.contains(p) {
			return true
		}
	}
	return false
}

// bridgedNodeName is the device type whose composition hosts any device
// type on its parts.
const bridgedNodeName = "BridgedNode"

// composes reports whether owner's device type, or a type it derives from,
// names part's device type in a deviceType requirement.
func composes(owner, part *Type) bool {
	if owner.Name == bridgedNodeName && part.Classification != "node" {
		return true
	}
	seen := map[string]bool{}
	for t := owner; t != nil && !seen[t.Name]; t = typeByName(t.Base) {
		seen[t.Name] = true
		dt, ok := schema.DeviceTypeDefinitionOf(uint32(t.ID))
		if !ok {
			continue
		}
		for i := range dt.Requirements {
			r := &dt.Requirements[i]
			if r.Element == schema.RequirementDeviceType && r.Referent.Resolved && r.Referent.ID == uint32(part.ID) &&
				r.Conformance.Op != "X" {
				return true
			}
		}
	}
	return false
}

// Parts returns the device's parts in the order they were added. The
// slice is fresh.
func (d *Device) Parts() []*Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.parts)
}

// Spec converts the device into the endpoint assembler's input: the
// Options mapped onto [endpoint.Spec], the device itself as its
// [contract.EndpointSource], and its parts, recursively, as
// [endpoint.Spec.Parts].
func (d *Device) Spec() endpoint.Spec {
	s := endpoint.Spec{
		StableKey:        d.opts.StableKey,
		DeviceAddress:    d.opts.DeviceAddress,
		DeviceType:       d.typ.ID,
		VendorName:       d.opts.Vendor,
		FriendlyName:     d.opts.Name,
		ChannelAddress:   d.opts.ChannelAddress,
		Availability:     d.opts.Reachable,
		Source:           d.source,
		Measurement:      d.measurement,
		NodeLabel:        d.opts.NodeLabel,
		DeviceConditions: slices.Clone(d.opts.Conditions),
		ClientClusters:   d.clientClusters(),
		OptIn:            slices.Clone(d.optIn),
	}
	for _, p := range d.Parts() {
		s.Parts = append(s.Parts, p.Spec())
	}
	return s
}

// clientClusters are the client clusters the device type mandates — what
// matter.js's MutableEndpoint registers into the type
// (packages/node/src/endpoint/type/MutableEndpoint.ts:53-61) and its
// DescriptorServer lists as ClientList (DescriptorServer.ts:290-296) —
// followed by those the host declared ([WithClientClusters],
// MutableEndpoint.ts:123-129), each once.
func (d *Device) clientClusters() []uint32 {
	var out []uint32
	for _, r := range d.typ.Clients {
		if r.Mandatory {
			out = append(out, r.ID)
		}
	}
	for _, id := range d.clients {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// OnChanged calls fn with the ids of the attributes of the device's
// clusterID server that changed — matter.js's
// `events.<cluster>.<attribute>$Changed` (endpoint/Endpoint.ts:456), delivered
// through the server's [contract.AttributeChangeNotifier], or through its
// [contract.ChangeNotifier] with every attribute it reports. It returns
// the unsubscribe function; a cluster the device does not mount, or whose
// server notifies nothing, never calls fn.
func (d *Device) OnChanged(clusterID uint32, fn func(attrIDs []uint32)) (unsubscribe func()) {
	srv := d.Server(clusterID)
	switch n := srv.(type) {
	case contract.AttributeChangeNotifier:
		return n.OnMatterAttributesChanged(fn)
	case contract.ChangeNotifier:
		return n.OnMatterValueChanged(func() { fn(srv.MatterReportable()) })
	default:
		return func() {}
	}
}
