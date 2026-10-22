// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
)

// Measurement serves clusters of the device from a classified host source
// — a host whose sensors are data (a measurement class per data point)
// rather than per-type code: src's class ([contract.MeasurementClass])
// names the clusters, cluster/measurement's materialiser for it builds
// their servers (measurement.FromMeasurementClass), and src is read on
// every dispatch. The kit hands src to the endpoint assembler
// ([endpoint.Spec.Measurement]), which runs the materialiser once the
// device is added and its endpoint number is known
// (endpoint/materialize.go ClusterServers) — the moment the momentary
// switch (which addresses its events to the endpoint) and the battery
// (whose EndpointList names it) need — and the bridge subscribes to src
// when it is a [contract.ChangeNotifier] and wires a switch's presses to
// it, as for an endpoint the host assembles itself. A battery-class source
// ([contract.MeasurementBattery]) rides as the device's PowerSource
// ([endpoint.Spec.PowerSource]) beside a measurement of another class.
//
// The device stays the kit's: its type's other clusters, the
// construction-time checks and the mandatory client clusters are as
// without the declaration. The clusters the source materialises are the
// assembler's for this device, as Descriptor is: New mounts no default
// server for them and refuses every other declaration of them, and
// [Device.Server] and the typed accessors answer nil for them. New
// refuses a nil src, a class that materialises no cluster from src, and a
// second source of a measurement class or a second battery (an endpoint
// carries one of each; a second sensor is a part or a device of its own).
//
// A host with per-type code declares the sensor's cluster instead
// (<cluster>def.Declare{Initial: …}) and sets each reading through the
// typed server; this is the form for a host whose sensors are classified
// data. It is the Go form of a matter.js host setting a measurement
// behavior's state from its own reading, which the behavior's Datasource
// then reports (packages/node/src/behavior/state/managed/Datasource.ts:268-270,
// the Options.onChange callback invoked when properties change).
func Measurement(src contract.MeasurementSource) Declaration {
	return deviceOption(func(s *settings) {
		s.measurements = append(s.measurements, src)
	})
}

// measured is what the device's [Measurement] declarations resolved to:
// the source the assembler materialises, the battery, and the clusters
// both serve.
type measured struct {
	source  contract.MeasurementSource
	battery contract.MeasurementSource
	ids     map[uint32]bool
}

// resolveMeasurements checks the [Measurement] declarations and learns
// the clusters they serve: the materialiser runs once here with no
// endpoint id, to learn the set; the assembler runs it again on every
// dispatch with the endpoint's.
func resolveMeasurements(t *Type, s *settings) (measured, error) {
	var m measured
	for _, src := range s.measurements {
		if src == nil {
			return measured{}, fmt.Errorf("%w: %s: Measurement(nil)", ErrConfiguration, t.Name)
		}
		class := src.MatterMeasurementClass()
		servers := measurement.FromMeasurementClass(class, src, contract.MeasurementContext{})
		if len(servers) == 0 {
			return measured{}, fmt.Errorf("%w: %s: the measurement source (class %d) materialises no cluster", ErrConfiguration, t.Name, class)
		}
		switch {
		case class == contract.MeasurementBattery && m.battery != nil:
			return measured{}, fmt.Errorf("%w: %s: a second battery (Measurement)", ErrConfiguration, t.Name)
		case class == contract.MeasurementBattery:
			m.battery = src
		case m.source != nil:
			return measured{}, fmt.Errorf("%w: %s: a second measurement source (class %d); an endpoint carries one", ErrConfiguration, t.Name, class)
		default:
			m.source = src
		}
		if m.ids == nil {
			m.ids = map[uint32]bool{}
		}
		for _, srv := range servers {
			m.ids[srv.MatterClusterID()] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.ids)) {
		if declaredAt(s, id) {
			return measured{}, fmt.Errorf("%w: %s: cluster 0x%04X is served from the measurement source (Measurement)", ErrConfiguration, t.Name, id)
		}
	}
	return m, nil
}

// declaredAt reports whether any declaration states something about id.
func declaredAt(s *settings, id uint32) bool {
	_, host := s.servers[id]
	_, initial := s.initial[id]
	_, elements := s.elements[id]
	_, features := s.features[id]
	_, attach := s.attach[id]
	return host || initial || elements || features || attach || s.optional[id] || s.typed[id]
}
