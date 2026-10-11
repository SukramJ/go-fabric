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
// Endpoint (packages/node/src/endpoint/Endpoint.ts:595-636).
type Option func(*settings)

// settings is what the Options of one New call collected.
type settings struct {
	features map[uint32]uint32
	optional map[uint32]bool
	servers  map[uint32]contract.ClusterServer
	initial  map[uint32]map[uint32]any
	elements map[uint32]spec.Options
	nilSrv   bool
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
func WithOptional(clusterID uint32) Option {
	return func(s *settings) { s.optional[clusterID] = true }
}

// WithServer supplies the host's own server for a cluster the device type
// allows, in place of the default — as a matter.js host replaces a
// behavior with its own implementation. It mounts an optional cluster too.
func WithServer(srv contract.ClusterServer) Option {
	return func(s *settings) {
		if srv == nil {
			s.nilSrv = true
			return
		}
		s.servers[srv.MatterClusterID()] = srv
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
