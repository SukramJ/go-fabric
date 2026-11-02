// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

// This file holds the built-in materialisers of the measurement classes.
// They live here, unexported, rather than in package contract because
// the dependency runs one way: contract declares the classes and keeps
// the host kinds' registry, this package owns the constructors that turn
// a source into cluster servers, and contract must not learn about them.
// A host cannot replace a built-in projection (review 2026-11
// W5-api-12); it registers a kind of its own.
//
// Each entry below is the arm the FromMeasurementClass switch used to
// carry, moved verbatim so a built-in class materialises exactly what it
// always did. Three classes get no entry at all, and
// [FromMeasurementClass] then returns nothing for them, which the
// assembler renders as "mount nothing":
//
//   - MeasurementNone has no Matter projection by design.
//   - MeasurementPower / MeasurementEnergy are per-parameter classes
//     that the host folds into one ElectricalGroup and dispatches as
//     MeasurementElectrical; neither builds an endpoint of its own.
//
// Two of the entries need the endpoint id, which they take from the
// [contract.MeasurementContext] the assembler passes in: MomentarySwitch
// at construction and Battery straight after it. They are registered
// here like every other built-in precisely so that nothing about their
// shape is library-only — a host kind reaches the same context through
// the same seam.

import (
	"fmt"

	switchserver "github.com/SukramJ/go-fabric/cluster/switch"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

func init() {
	setMaterializer(contract.MeasurementTemperature, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewTemperatureServer(f) },
	))
	setMaterializer(contract.MeasurementHumidity, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewHumidityServer(f) },
	))
	setMaterializer(contract.MeasurementIlluminance, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewIlluminanceServer(f) },
	))
	setMaterializer(contract.MeasurementPressure, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPressureServer(f) },
	))
	setMaterializer(contract.MeasurementFlow, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewFlowServer(f) },
	))

	// The air-quality classes return two servers: the concentration
	// cluster plus [AirQualityServer], which the AirQualitySensor device
	// type mandates while listing every concentration cluster as
	// optional. The class is closed over because AirQualityServer derives
	// its rating thresholds from which pollutant it reports on.
	setMaterializer(contract.MeasurementCO2, airQualityServers(
		contract.MeasurementCO2,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewCO2ConcentrationServer(f) },
	))
	setMaterializer(contract.MeasurementPM25, airQualityServers(
		contract.MeasurementPM25,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPM25ConcentrationServer(f) },
	))
	setMaterializer(contract.MeasurementPM10, airQualityServers(
		contract.MeasurementPM10,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPM10ConcentrationServer(f) },
	))
	concentrationMaterializers() // the seven further concentration kinds (concentration.go)

	booleanState := boolServer(
		func(b contract.BoolMeasurementSource) contract.ClusterServer { return NewBooleanStateServer(b) },
	)
	setMaterializer(contract.MeasurementContact, booleanState)
	setMaterializer(contract.MeasurementLeak, booleanState)
	setMaterializer(contract.MeasurementOccupancy, boolServer(
		func(b contract.BoolMeasurementSource) contract.ClusterServer { return NewOccupancySensingServer(b) },
	))

	setMaterializer(contract.MeasurementBattery, powerSourceServers)
	setMaterializer(contract.MeasurementElectrical, electricalSensorServers)
	setMaterializer(contract.MeasurementMomentarySwitch, genericSwitchServers)
	setMaterializer(contract.MeasurementSoilMoisture, soilServer) // soil.go
}

// floatServer adapts a single-server constructor over
// [contract.FloatMeasurementSource] to a materialiser. A src of another
// shape yields no cluster: the class says what the reading means, the
// interface says whether it can be read at all, and only both together
// make an endpoint honest.
func floatServer(build func(contract.FloatMeasurementSource) contract.ClusterServer) contract.MeasurementMaterializer {
	return func(src any, _ contract.MeasurementContext) []contract.ClusterServer {
		f, ok := src.(contract.FloatMeasurementSource)
		if !ok {
			return nil
		}
		return []contract.ClusterServer{build(f)}
	}
}

// boolServer is floatServer's counterpart for the boolean classes.
func boolServer(build func(contract.BoolMeasurementSource) contract.ClusterServer) contract.MeasurementMaterializer {
	return func(src any, _ contract.MeasurementContext) []contract.ClusterServer {
		b, ok := src.(contract.BoolMeasurementSource)
		if !ok {
			return nil
		}
		return []contract.ClusterServer{build(b)}
	}
}

// airQualityServers pairs a concentration cluster with the mandatory
// AirQualityServer, in that order: the ServerList the assembler derives
// preserves it, and the AirQuality cluster is the one the device type
// requires.
func airQualityServers(class contract.MeasurementClass, build func(contract.FloatMeasurementSource) contract.ClusterServer) contract.MeasurementMaterializer {
	return func(src any, _ contract.MeasurementContext) []contract.ClusterServer {
		f, ok := src.(contract.FloatMeasurementSource)
		if !ok {
			return nil
		}
		return []contract.ClusterServer{NewAirQualityServer(class, f), build(f)}
	}
}

// powerSourceServers materialises PowerSource (0x002F). A battery has no
// endpoint of its own — it rides on the endpoint of the device it
// powers, and mc.EndpointID names that endpoint. EndpointList (Matter
// §11.7.6.20) must report it, so the id is stamped here, immediately
// after construction: the assembler rebuilds these servers on every
// dispatch, so a stamp applied further out would not survive.
//
// Two source shapes project onto PowerSource: a LOWBAT bool
// (BatChargeLevel) or a derived battery-percentage float (e.g. an
// operating-voltage-level sensor — BatPercentRemaining). Checked in this
// order because both interfaces are structurally possible on a source
// that also implements other measurement surfaces; a bool source is the
// more specific and more common signal.
func powerSourceServers(src any, mc contract.MeasurementContext) []contract.ClusterServer {
	var ps *PowerSourceServer
	switch s := src.(type) {
	case contract.BoolMeasurementSource:
		ps = NewPowerSourceServer(s)
	case contract.FloatMeasurementSource:
		ps = NewPowerSourceServerFromFloat(s)
	default:
		return nil
	}
	ps.SetEndpoint(mc.EndpointID)
	return []contract.ClusterServer{ps}
}

// genericSwitchServers materialises Switch (0x003B) for a momentary
// press source: the cluster/switch server, the port of matter.js
// SwitchServer.ts. Unlike every other built-in it needs mc.EndpointID at
// construction rather than after it: the server emits events, and a Matter
// event carries the endpoint as its routing key, so the cluster must know
// the endpoint before it can report its first press.
//
// The source shape is cluster/wire's, not one of the value-reading
// interfaces the other built-ins assert, because this cluster is
// event-driven rather than attribute-driven: the source states what the
// switch can tell (positions, long press, multi press, action switch), and
// the readings arrive as positions (the source's WireMatterSwitchPositions)
// or as narrated gestures (WireMatterSwitchHandler), both wired by the
// bridge at assembly.
func genericSwitchServers(src any, mc contract.MeasurementContext) []contract.ClusterServer {
	s, ok := src.(wire.GenericSwitchSource)
	if !ok {
		return nil
	}
	srv, err := switchserver.New(switchConfig(mc.EndpointID, s))
	if err != nil {
		// switchConfig only builds selections inside the model.
		return nil
	}
	return []contract.ClusterServer{srv}
}

// switchConfig is the server configuration a source states. MS always; AS
// when the source is an action switch, MSR otherwise (the two exclude each
// other: MSR is "[MS & !AS]"); MSL when it tells a long press from a short
// one; MSM when it counts presses (MultiPressMax 2 or more), and always
// with AS, which makes it mandatory ("AS, [MS & MSR]", matter.js
// switch.element.ts). Two positions at the least ("min 2"); the start
// position is the source's live one, when it has one in range.
func switchConfig(endpoint uint16, s wire.GenericSwitchSource) switchserver.Config {
	features := switchserver.FeatureMS
	action := false
	if a, ok := s.(wire.GenericSwitchActionSource); ok && a.MatterSwitchActionSwitch() {
		action = true
		features |= switchserver.FeatureAS
	} else {
		features |= switchserver.FeatureMSR
	}
	if s.MatterSwitchSupportsLongPress() {
		features |= switchserver.FeatureMSL
	}
	var multiPressMax uint8
	if m, ok := s.(wire.GenericSwitchMultiPressSource); ok && m.MatterSwitchMultiPressMax() >= 2 {
		multiPressMax = m.MatterSwitchMultiPressMax()
	}
	if multiPressMax >= 2 || action {
		features |= switchserver.FeatureMSM
	}
	n := max(s.MatterSwitchPositions(), 2)
	cfg := switchserver.Config{Features: features, Endpoint: endpoint, NumberOfPositions: n, MultiPressMax: multiPressMax}
	if p, ok := s.(wire.GenericSwitchPositionSource); ok && p.MatterSwitchCurrentPosition() < n {
		cfg.CurrentPosition = p.MatterSwitchCurrentPosition()
	}
	return cfg
}

// electricalSensorServers materialises the ElectricalSensor endpoint's
// full surface. PowerTopology is mandatory for the device type, so it
// ships whether or not the device has anything topological to say;
// ElectricalEnergyMeasurement joins only when the channel actually
// reports a counter, since conformance O.a+ requires at least one of the
// two measurement clusters, not both.
func electricalSensorServers(src any, _ contract.MeasurementContext) []contract.ClusterServer {
	r, ok := src.(ElectricalReadingsSource)
	if !ok {
		return nil
	}
	servers := []contract.ClusterServer{
		NewElectricalPowerServerFromReadings(r),
		NewPowerTopologyServer(),
	}
	if r.HasEnergy() {
		servers = append(servers, NewElectricalEnergyServer(energyOf{r}))
	}
	return servers
}

// builtinMaterializers are the built-in classes' materialisers, filled by
// this package's init; read-only afterwards.
var builtinMaterializers = map[contract.MeasurementClass]contract.MeasurementMaterializer{}

// setMaterializer installs one built-in class's materialiser; a nil one
// and a class a host registered are wiring mistakes of this package.
func setMaterializer(class contract.MeasurementClass, m contract.MeasurementMaterializer) {
	if m == nil {
		panic(fmt.Sprintf("measurement: nil materializer for class %d", class))
	}
	if kind, ok := contract.MeasurementKindFor(class); !ok || kind.Materialize != nil {
		panic(fmt.Sprintf("measurement: class %d is not a built-in", class))
	}
	builtinMaterializers[class] = m
}
