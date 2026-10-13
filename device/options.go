// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package device

import (
	"maps"

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
	StableKey endpoint.SourceKey
	// Name is the BridgedDeviceBasicInformation NodeLabel
	// ([endpoint.Spec.FriendlyName]).
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
	// ([endpoint.Spec.Availability]); nil reads as always reachable.
	Reachable endpoint.AvailabilityProbe
	// Conditions are the device-type conditions the host states for the
	// endpoint ([endpoint.Spec.DeviceConditions]).
	Conditions []string
	// NodeLabel is a NodeLabel a controller wrote in an earlier run and
	// the host persisted ([endpoint.Spec.NodeLabel]).
	NodeLabel string
}

// Option configures one device at construction, as matter.js's
// `Behavior.with(...)` and the endpoint's initial state configure an
// Endpoint (packages/node/src/endpoint/Endpoint.ts:596-637).
type Option func(*settings)

// settings is what the Options of one New call collected.
type settings struct {
	features map[uint32]uint32
	optional map[uint32]bool
	servers  map[uint32]contract.ClusterServer
	// serverOrder is the order WithServer named the clusters in; a host
	// server for a cluster the type does not list is mounted in it.
	serverOrder []uint32
	notifier    contract.ChangeNotifier
	// measurement is the host's live reading ([WithMeasurement]);
	// measurementSet tells a WithMeasurement(nil) from no call.
	measurement    contract.MeasurementSource
	measurementSet bool
	initial        map[uint32]map[uint32]any
	elements       map[uint32]spec.Options
	nilSrv         bool
}

func newSettings(with []Option) *settings {
	s := &settings{
		features: map[uint32]uint32{}, optional: map[uint32]bool{},
		servers: map[uint32]contract.ClusterServer{}, initial: map[uint32]map[uint32]any{},
		elements: map[uint32]spec.Options{},
	}
	for _, o := range with {
		if o != nil {
			o(s)
		}
	}
	return s
}

// WithFeatures selects the FeatureMap of a cluster's default server, as
// `OnOffServer.with("Lighting")` does in matter.js. The selection must
// pass the cluster's feature conformance ([spec.CheckFeatures]) and
// include every feature the device type mandates. It also mounts an
// optional cluster of the type, as matter.js's `Device.with(Server.with(…))`
// does (accepted by the owner, 2026-10-11).
func WithFeatures(clusterID, features uint32) Option {
	return func(s *settings) {
		s.features[clusterID] = features
		s.optional[clusterID] = true
	}
}

// WithOptional mounts an optional cluster of the device type with its
// default server — matter.js's `Device.with(LevelControlServer)` for a
// cluster in `server.optional` (devices/on-off-light.ts:111, 126-131).
// For Groups (0x0004) and ScenesManagement (0x0062), whose server is the
// stack's, it opts the endpoint in ([endpoint.Spec.OptIn]) and the
// assembler mounts them, as `MutableEndpoint.with(GroupsServer)` extends
// any endpoint type (packages/node/src/endpoint/type/MutableEndpoint.ts:95-112);
// every other option for them stays refused.
func WithOptional(clusterID uint32) Option {
	return func(s *settings) { s.optional[clusterID] = true }
}

// WithServer supplies the host's own server for a cluster, in place of the
// default — as a matter.js host replaces a behavior with its own
// implementation. It mounts an optional cluster too. A cluster the device
// type does not list is mounted as well, after the type's clusters and in
// the order of the WithServer calls: matter.js's MutableEndpoint.with adds
// any behavior to the type's (packages/node/src/endpoint/type/
// MutableEndpoint.ts:95-110, SupportedBehaviors.extend), and leaves the
// judgement to device-type validation — a FixedLabel on a light, say, or
// the EcosystemInformation BridgedNode allows on the bridged endpoint.
// The clusters the endpoint assembler mounts stay refused.
func WithServer(srv contract.ClusterServer) Option {
	return func(s *settings) {
		if srv == nil {
			s.nilSrv = true
			return
		}
		id := srv.MatterClusterID()
		if _, dup := s.servers[id]; !dup {
			s.serverOrder = append(s.serverOrder, id)
		}
		s.servers[id] = srv
	}
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
	return func(s *settings) { s.notifier = n }
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
	return func(s *settings) {
		s.measurement = m
		s.measurementSet = true
	}
}

// WithElements serves optional attributes, accepts optional commands and
// emits optional events of a cluster's default server beyond the ones the
// device type mandates: opts.Attributes, opts.Commands and opts.Events
// feed the default server's [spec.Options]; opts.Features must be zero —
// the feature selection is [WithFeatures]'s. It mounts an optional cluster
// too. Calls for one cluster accumulate.
func WithElements(clusterID uint32, opts spec.Options) Option {
	return func(s *settings) {
		e := s.elements[clusterID]
		e.Features |= opts.Features
		e.Attributes = append(e.Attributes, opts.Attributes...)
		e.Commands = append(e.Commands, opts.Commands...)
		e.Events = append(e.Events, opts.Events...)
		s.elements[clusterID] = e
		s.optional[clusterID] = true
	}
}

// WithInitial sets initial attribute values of a cluster's default server
// (attribute id → value), over the defaults the device type states —
// matter.js's initial state keyed by behavior id
// (Endpoint.ts:1385-1438).
func WithInitial(clusterID uint32, values map[uint32]any) Option {
	return func(s *settings) {
		m := s.initial[clusterID]
		if m == nil {
			m = map[uint32]any{}
			s.initial[clusterID] = m
		}
		maps.Copy(m, values)
	}
}
