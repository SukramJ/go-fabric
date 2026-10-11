// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

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
// device type mandates them — a Groups or ScenesManagement server the
// source supplies is replaced by the stack's (endpoint/materialize.go
// ClusterServers, partClusterServers, mountGroups, mountScenes).
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
// packages/node/src/endpoint/Endpoint.ts:595-636).
func New(t *Type, opts Options, with ...Option) (*Device, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: no device type", ErrConfiguration)
	}
	if opts.StableKey == nil {
		return nil, fmt.Errorf("%w: %s needs a StableKey", ErrConfiguration, t.Name)
	}
	s := newSettings(with)
	if s.nilSrv {
		return nil, fmt.Errorf("%w: %s: WithServer(nil)", ErrConfiguration, t.Name)
	}
	if err := checkOptedClusters(t, s); err != nil {
		return nil, err
	}
	d := &Device{typ: t, opts: opts}
	for i := range t.Servers {
		r := &t.Servers[i]
		if _, ok := assemblerMounted[r.ID]; ok {
			continue
		}
		srv, ok := s.servers[r.ID]
		if !ok && !r.Mandatory && !s.optional[r.ID] {
			continue
		}
		if !ok {
			var err error
			if srv, err = defaultServer(t, r, s); err != nil {
				return nil, err
			}
		}
		d.servers = append(d.servers, srv)
	}
	return d, nil
}

// checkOptedClusters refuses an option for a cluster the device type does
// not allow as a server, or one the assembler mounts itself.
func checkOptedClusters(t *Type, s *settings) error {
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
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if name, ok := assemblerMounted[id]; ok {
			return fmt.Errorf("%w: %s: the endpoint assembler mounts %s (0x%04X) itself", ErrConfiguration, t.Name, name, id)
		}
		if _, ok := t.Server(id); !ok {
			return fmt.Errorf("%w: %s has no server cluster requirement for cluster 0x%04X", ErrConfiguration, t.Name, id)
		}
	}
	for id := range s.servers {
		if _, ok := s.features[id]; ok {
			return fmt.Errorf("%w: %s: WithFeatures for cluster 0x%04X, whose server the host supplies", ErrConfiguration, t.Name, id)
		}
		if _, ok := s.initial[id]; ok {
			return fmt.Errorf("%w: %s: WithInitial for cluster 0x%04X, whose server the host supplies", ErrConfiguration, t.Name, id)
		}
	}
	return nil
}

// defaultServer builds the default server of one cluster: the generated
// definition with the mandated (or the host's) features, the elements the
// device type makes mandatory, and the defaults it states as initial
// values — matter.js's `BaseServer.with(features).alter(...)` of the
// generated device file (ClusterRequirements.ts:137-208,
// RequirementGenerator.ts:253-296).
func defaultServer(t *Type, r *ClusterRequirement, s *settings) (contract.ClusterServer, error) {
	def := r.Definition
	if def == nil {
		return nil, fmt.Errorf("%w: %s requires %s (0x%04X, conformance %q) and no generated definition is committed for it: supply its server with WithServer",
			ErrNoDefaultServer, t.Name, r.Name, r.ID, r.Conformance)
	}
	features := r.Features
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
	factory, ok := defaultFactories[r.ID]
	if !ok {
		factory = newSpecServer
	}
	srv, err := factory(def, opts, spec.ServerConfig{Initial: initial})
	switch {
	case errors.Is(err, spec.ErrFabricScoped):
		return nil, fmt.Errorf("%w: %s %s: %w: supply its server with WithServer", ErrNoDefaultServer, t.Name, r.Name, err)
	case err != nil:
		return nil, fmt.Errorf("%w: %s %s: %w", ErrConfiguration, t.Name, r.Name, err)
	}
	return srv, nil
}

// serverFactory builds a cluster's default server from its definition,
// the feature and element selection and the initial values.
type serverFactory func(def *spec.Cluster, opts spec.Options, cfg spec.ServerConfig) (contract.ClusterServer, error)

// defaultFactories is the device layer's default table: the clusters whose
// default server is one of the module's self-held servers rather than the
// generated [spec.Server] every other cluster gets (ADR 0018, decisions of
// wave 6, 2). Keep it in step with script/devicegen/servers.go, which
// types the generated accessors.
var defaultFactories = map[uint32]serverFactory{
	0x0006: newSpecServer, // OnOff. TODO(brief Q): cluster/onoff.Server once it lands.
}

// newSpecServer is the generated server, [spec.NewServer].
func newSpecServer(def *spec.Cluster, opts spec.Options, cfg spec.ServerConfig) (contract.ClusterServer, error) {
	srv, err := spec.NewServer(def, opts, cfg)
	if err != nil {
		return nil, err
	}
	return srv, nil
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
// none.
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

// serverAs returns the device's server for clusterID as T, or T's zero
// value when it mounts none or the host's server is of another type.
func serverAs[T contract.ClusterServer](d *Device, clusterID uint32) T {
	v, _ := d.Server(clusterID).(T)
	return v
}

// AddPart makes p a part of the device — matter.js's `endpoint.parts.add`
// (packages/node/src/endpoint/properties/Parts.ts:36-61). matter.js
// validates an insertion for endpoint-number identity only
// (Parts.ts:170-201) and leaves composition to device-type validation;
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
		Source:           d,
		NodeLabel:        d.opts.NodeLabel,
		DeviceConditions: slices.Clone(d.opts.Conditions),
	}
	for _, p := range d.Parts() {
		s.Parts = append(s.Parts, p.Spec())
	}
	return s
}

// OnChanged calls fn with the ids of the attributes of the device's
// clusterID server that changed — matter.js's
// `events.<cluster>.<attribute>$Changed` (Endpoint.ts:456), delivered
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
