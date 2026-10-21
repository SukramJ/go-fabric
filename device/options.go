// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// Options is what the host states about one device beyond its type: the
// fields of [endpoint.Spec] the device layer passes through, as a matter.js
// host passes `id` and `bridgedDeviceBasicInformation` to `new Endpoint`
// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:77-122).
type Options struct {
	// StableKey identifies the device across reassemblies and restarts
	// ([endpoint.Spec.StableKey]); required, and unique per device and
	// per part.
	StableKey SourceKey
	// Name is the BridgedDeviceBasicInformation NodeLabel
	// ([endpoint.Spec.FriendlyName]); [Device.Rename] changes it.
	Name string
	// Vendor is the bridged device's manufacturer
	// ([endpoint.Spec.VendorName]).
	Vendor string
	// DeviceAddress and ChannelAddress are the host's own addresses of the
	// physical device and of this function of it, carried through
	// verbatim.
	DeviceAddress  string
	ChannelAddress string
	// Reachable probes the device's live reachability
	// ([endpoint.Spec.Availability]); nil reads as always reachable. The
	// device is reachable while the probe says so and [Device.SetReachable]
	// has not set it unreachable.
	Reachable AvailabilityProbe
	// Conditions are the device-type conditions the host states for the
	// endpoint ([endpoint.Spec.DeviceConditions]).
	Conditions []string
	// NodeLabel is a NodeLabel a controller wrote in an earlier run and
	// the host persisted ([endpoint.Spec.NodeLabel]).
	NodeLabel string
}

// SourceKey is the type of [Options].StableKey ([endpoint.SourceKey]).
type SourceKey = endpoint.SourceKey

// AvailabilityProbe is the type of [Options].Reachable
// ([endpoint.AvailabilityProbe]).
type AvailabilityProbe = endpoint.AvailabilityProbe

// Declaration is one statement about a device beside its [Options], which
// [New], [FromSpec] and every generated constructor (NewDimmableLight, …)
// take: a cluster's declaration — <cluster>def.Declare of the generated
// cluster packages (cluster/spec/<cluster>, imported as <cluster>def), the
// one form of a cluster's initial state, feature selection, write veto,
// command handlers and host ports — or one of the kit's own options
// ([WithServer], [WithChangeNotifier], [WithMeasurement],
// [WithClientClusters]). It is the
// Go form of what matter.js states on an endpoint type and its behaviors
// (`Endpoint.with(…)`, the behaviors' initial state, a behavior subclass
// overriding a command; packages/node/src/devices/on-off-light.ts:103-135,
// packages/node/src/endpoint/Endpoint.ts:596-637).
type Declaration interface {
	// ClusterID is the cluster declared; 0 for a statement about the
	// device as a whole.
	ClusterID() uint32
	// ClusterDeclaration is the declaration in the form the kit applies
	// it.
	ClusterDeclaration() spec.ClusterDeclaration
}

// Option is the kit's own [Declaration], what the With… functions return.
type Option = Declaration

// clusterOption is a per-cluster option of the kit's own ([WithServer], and
// the tests' untyped forms): a declaration that mounts the cluster when
// mount is set; undeclared marks a WithServer, which takes only a cluster
// without a generated declaration.
type clusterOption struct {
	decl       spec.ClusterDeclaration
	mount      bool
	undeclared bool
}

func (o clusterOption) ClusterID() uint32                           { return o.decl.Cluster }
func (o clusterOption) ClusterDeclaration() spec.ClusterDeclaration { return o.decl }

// deviceOption is an option about the device as a whole.
type deviceOption func(*settings)

func (deviceOption) ClusterID() uint32                           { return 0 }
func (deviceOption) ClusterDeclaration() spec.ClusterDeclaration { return spec.ClusterDeclaration{} }

// settings is what the declarations of one New call collected.
type settings struct {
	features map[uint32]uint32
	optional map[uint32]bool
	servers  map[uint32]contract.ClusterServer
	// serverOrder is the order the host's servers were declared in; a host
	// server for a cluster the type does not list is mounted in it.
	serverOrder []uint32
	notifier    contract.ChangeNotifier
	// measurement is the host's live reading ([WithMeasurement]);
	// measurementSet tells a WithMeasurement(nil) from no call.
	measurement    contract.MeasurementSource
	measurementSet bool
	initial        map[uint32]map[uint32]any
	elements       map[uint32]spec.Options
	// clients are the client clusters [WithClientClusters] declared, in
	// call order.
	clients []uint32
	// clock is the device's clock, for the servers the device builds.
	clock clock.Clock
	// typed are the clusters a generated Declare declared; attach their
	// hooks once the server is built; ports and transitions what their
	// kit servers are built with.
	typed       map[uint32]bool
	implied     map[uint32]spec.Options
	attach      map[uint32][]func(contract.ClusterServer) error
	ports       map[uint32]any
	transitions map[uint32]bool
}

func newSettings() *settings {
	return &settings{
		features: map[uint32]uint32{}, optional: map[uint32]bool{},
		servers: map[uint32]contract.ClusterServer{}, initial: map[uint32]map[uint32]any{},
		elements: map[uint32]spec.Options{}, typed: map[uint32]bool{}, implied: map[uint32]spec.Options{},
		attach: map[uint32][]func(contract.ClusterServer) error{}, ports: map[uint32]any{},
		transitions: map[uint32]bool{},
	}
}

// declare collects one declaration. A cluster takes one generated
// Declare; the kit's per-cluster options accumulate as before.
func (s *settings) declare(d Declaration) error {
	switch o := d.(type) {
	case nil:
		return nil
	case deviceOption:
		o(s)
		return nil
	case clusterOption:
		if o.undeclared {
			if o.decl.Server == nil {
				return fmt.Errorf("%w: WithServer(nil)", ErrConfiguration)
			}
			if def := spec.Lookup(o.decl.Cluster); def != nil {
				return fmt.Errorf("%w: WithServer for %s (0x%04X): declare it, %sdef.Declare{HostServer: …}",
					ErrConfiguration, def.Name, o.decl.Cluster, strings.ToLower(def.Name))
			}
		}
		s.merge(o.decl, o.mount)
		return nil
	}
	decl := d.ClusterDeclaration()
	id := d.ClusterID()
	if decl.Cluster != id || id == 0 {
		return fmt.Errorf("%w: declaration %T states cluster 0x%04X, its ClusterDeclaration 0x%04X", ErrConfiguration, d, id, decl.Cluster)
	}
	if s.typed[id] {
		return fmt.Errorf("%w: cluster 0x%04X is declared twice", ErrConfiguration, id)
	}
	s.typed[id] = true
	if len(decl.Initial) == 0 {
		// An empty Initial states nothing: the declaration's hooks may
		// go on the host's server (HostServer).
		decl.Initial = nil
	}
	if decl.Server != nil && hasPorts(decl.Ports) {
		return fmt.Errorf("%w: cluster 0x%04X: a host port and a HostServer, the port is the kit's server's", ErrConfiguration, id)
	}
	// An optional attribute the declaration gives a value is served, and
	// an optional command it has a handler for accepted
	// (ValidatedElements.ts:251-252, :270-278, :301-318): elements the
	// kit's server takes, which a host's server ignores.
	implied := decl.Elements
	implied.Attributes = append(implied.Attributes, slices.Sorted(maps.Keys(decl.Initial))...)
	s.implied[id] = implied
	decl.Elements = spec.Options{}
	// The host's server is mounted as such; the kit's is mounted by the
	// declaration.
	s.merge(decl, decl.Server == nil)
	return nil
}

// hasPorts reports whether a typed declaration carries a host port: a
// non-nil interface field other than its HostServer.
func hasPorts(typed any) bool {
	rv := reflect.ValueOf(typed)
	if rv.Kind() != reflect.Struct {
		return false
	}
	for i := range rv.NumField() {
		f := rv.Type().Field(i)
		if f.Type.Kind() == reflect.Interface && f.Name != "HostServer" && !rv.Field(i).IsNil() {
			return true
		}
	}
	return false
}

// merge folds one cluster declaration into the settings.
func (s *settings) merge(decl spec.ClusterDeclaration, mount bool) {
	id := decl.Cluster
	if mount {
		s.optional[id] = true
	}
	if decl.HasFeatures {
		s.features[id] = decl.Features
	}
	if decl.Initial != nil {
		m := s.initial[id]
		if m == nil {
			m = map[uint32]any{}
			s.initial[id] = m
		}
		maps.Copy(m, decl.Initial)
	}
	if el := decl.Elements; len(el.Attributes)+len(el.Commands)+len(el.Events) > 0 || el.Features != 0 {
		e := s.elements[id]
		e.Features |= el.Features
		e.Attributes = append(e.Attributes, el.Attributes...)
		e.Commands = append(e.Commands, el.Commands...)
		e.Events = append(e.Events, el.Events...)
		s.elements[id] = e
	}
	if decl.Server != nil {
		if _, dup := s.servers[id]; !dup {
			s.serverOrder = append(s.serverOrder, id)
		}
		s.servers[id] = decl.Server
	}
	if decl.Transitions {
		s.transitions[id] = true
	}
	if decl.Attach != nil {
		s.attach[id] = append(s.attach[id], decl.Attach)
	}
	if decl.Ports != nil {
		s.ports[id] = decl.Ports
	}
}

// WithServer mounts the host's own server for a cluster without a
// generated package — a manufacturer-specific cluster, which the device
// type does not list — after the type's clusters, in the order of the
// calls: matter.js's MutableEndpoint.with adds any behavior to the type's
// (packages/node/src/endpoint/type/MutableEndpoint.ts:95-110,
// SupportedBehaviors.extend). A cluster with a generated package is
// declared, its own server as the declaration's HostServer
// (<cluster>def.Declare{HostServer: srv}); WithServer refuses it.
func WithServer(srv contract.ClusterServer) Option {
	if srv == nil {
		return clusterOption{undeclared: true}
	}
	return clusterOption{decl: spec.ClusterDeclaration{Cluster: srv.MatterClusterID(), Server: srv}, undeclared: true}
}

// WithChangeNotifier makes the device's endpoint source a
// [contract.ChangeNotifier] that fires whenever n fires: the host-model
// relay, the host's own model telling the bridge that state its servers
// read through a host port (valve.StateSource, modeselect.ModeSource, …)
// has moved, so the endpoint's reportable attributes are reported. It is
// the Go form of a matter.js host setting behavior state from its own
// device events, whose Datasource then reports the change
// (packages/node/src/behavior/state/managed/Datasource.ts). A server that
// notifies its own changes ([contract.ChangeNotifier] or
// [contract.AttributeChangeNotifier]) is heard by the bridge without it;
// the relay is for a host model whose servers do not forward its
// notifications. A later call replaces an earlier one; nil leaves the
// source without one.
func WithChangeNotifier(n contract.ChangeNotifier) Option {
	return deviceOption(func(s *settings) { s.notifier = n })
}

// WithMeasurement serves the device's measurement clusters from the host's
// live reading m: the endpoint assembler materialises them from m's
// measurement class ([endpoint.Spec.Measurement], cluster/measurement
// FromMeasurementClass) on every dispatch, exactly as for a measurement
// endpoint the host hands over as an [endpoint.Spec], and the bridge
// subscribes to m when it is a [contract.ChangeNotifier] and wires a
// GenericSwitch's presses to it. The clusters m's class materialises are
// then the assembler's for this device, as Descriptor is: New mounts no
// default server for them, refuses every other option for them, and
// [Device.Server] and the typed accessors answer nil for them. A device
// without the option is unchanged.
//
// It is the Go form of a matter.js host setting a measurement behavior's
// state from its own reading, which the behavior's Datasource then reports
// (packages/node/src/behavior/state/managed/Datasource.ts:261-264
// `onChange`, :776-796). New refuses a nil m and a class that materialises
// no cluster from m. A later call replaces an earlier one.
func WithMeasurement(m contract.MeasurementSource) Option {
	return deviceOption(func(s *settings) {
		s.measurement = m
		s.measurementSet = true
	})
}

// WithClientClusters declares client clusters on the endpoint beyond the
// ones its device type mandates — matter.js's
// `MutableEndpoint.withClientClusters` (packages/node/src/endpoint/type/
// MutableEndpoint.ts:123-129), which extends the type's clientClusters.
// They join the Descriptor's ClientList and are what the binding manager
// selects for a Binding entry (BindingManager.ts:563-577,
// #selectClientClusters reads the type's clientClusters). A declared
// bindable application client makes Base mandate Binding, which the
// assembler then mounts where the type does not ([endpoint.Spec.ClientClusters]).
func WithClientClusters(clusterIDs ...uint32) Option {
	return deviceOption(func(s *settings) { s.clients = append(s.clients, clusterIDs...) })
}
