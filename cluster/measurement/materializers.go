// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

// This file installs the built-in half of the measurement-kind
// registry. It lives here rather than in package contract because the
// dependency runs one way: contract declares the classes and owns the
// registry, this package owns the constructors that turn a source into
// cluster servers, and contract must not learn about them. The same
// shape as the module's other cross-package installs — an init that
// fills a seam the lower package declared.
//
// Each entry below is the arm the FromMeasurementClass switch used to
// carry, moved verbatim so a built-in class materialises exactly what it
// always did. Four classes get no entry at all, and
// [contract.MeasurementMaterializerFor] then reports them as having
// none, which the assembler renders as "mount nothing":
//
//   - MeasurementNone has no Matter projection by design.
//   - MeasurementMomentarySwitch (Switch 0x003B) is event-driven, not
//     attribute-driven — its projection is cluster/wire's GenericSwitch,
//     which needs the endpoint id at construction time and is therefore
//     built by the endpoint assembler, not from a source alone.
//   - MeasurementPower / MeasurementEnergy are per-parameter classes
//     that the host folds into one ElectricalGroup and dispatches as
//     MeasurementElectrical; neither builds an endpoint of its own.

import "github.com/SukramJ/go-fabric/contract"

func init() {
	contract.SetMeasurementMaterializer(contract.MeasurementTemperature, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewTemperatureServer(f) },
	))
	contract.SetMeasurementMaterializer(contract.MeasurementHumidity, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewHumidityServer(f) },
	))
	contract.SetMeasurementMaterializer(contract.MeasurementIlluminance, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewIlluminanceServer(f) },
	))
	contract.SetMeasurementMaterializer(contract.MeasurementPressure, floatServer(
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPressureServer(f) },
	))

	// The air-quality classes return two servers: the concentration
	// cluster plus [AirQualityServer], which the AirQualitySensor device
	// type mandates while listing every concentration cluster as
	// optional. The class is closed over because AirQualityServer derives
	// its rating thresholds from which pollutant it reports on.
	contract.SetMeasurementMaterializer(contract.MeasurementCO2, airQualityServers(
		contract.MeasurementCO2,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewCO2ConcentrationServer(f) },
	))
	contract.SetMeasurementMaterializer(contract.MeasurementPM25, airQualityServers(
		contract.MeasurementPM25,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPM25ConcentrationServer(f) },
	))
	contract.SetMeasurementMaterializer(contract.MeasurementPM10, airQualityServers(
		contract.MeasurementPM10,
		func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPM10ConcentrationServer(f) },
	))

	booleanState := boolServer(
		func(b contract.BoolMeasurementSource) contract.ClusterServer { return NewBooleanStateServer(b) },
	)
	contract.SetMeasurementMaterializer(contract.MeasurementContact, booleanState)
	contract.SetMeasurementMaterializer(contract.MeasurementLeak, booleanState)
	contract.SetMeasurementMaterializer(contract.MeasurementOccupancy, boolServer(
		func(b contract.BoolMeasurementSource) contract.ClusterServer { return NewOccupancySensingServer(b) },
	))

	contract.SetMeasurementMaterializer(contract.MeasurementBattery, powerSourceServers)
	contract.SetMeasurementMaterializer(contract.MeasurementElectrical, electricalSensorServers)
}

// floatServer adapts a single-server constructor over
// [contract.FloatMeasurementSource] to a materialiser. A src of another
// shape yields no cluster: the class says what the reading means, the
// interface says whether it can be read at all, and only both together
// make an endpoint honest.
func floatServer(build func(contract.FloatMeasurementSource) contract.ClusterServer) contract.MeasurementMaterializer {
	return func(src any) []contract.ClusterServer {
		f, ok := src.(contract.FloatMeasurementSource)
		if !ok {
			return nil
		}
		return []contract.ClusterServer{build(f)}
	}
}

// boolServer is floatServer's counterpart for the boolean classes.
func boolServer(build func(contract.BoolMeasurementSource) contract.ClusterServer) contract.MeasurementMaterializer {
	return func(src any) []contract.ClusterServer {
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
	return func(src any) []contract.ClusterServer {
		f, ok := src.(contract.FloatMeasurementSource)
		if !ok {
			return nil
		}
		return []contract.ClusterServer{NewAirQualityServer(class, f), build(f)}
	}
}

// powerSourceServers materialises PowerSource (0x002F). The resulting
// server is attached to a host endpoint by the caller — a battery has no
// endpoint of its own, it rides on the endpoint of the device it powers.
//
// Two source shapes project onto PowerSource: a LOWBAT bool
// (BatChargeLevel) or a derived battery-percentage float (e.g. an
// operating-voltage-level sensor — BatPercentRemaining). Checked in this
// order because both interfaces are structurally possible on a source
// that also implements other measurement surfaces; a bool source is the
// more specific and more common signal.
func powerSourceServers(src any) []contract.ClusterServer {
	if b, ok := src.(contract.BoolMeasurementSource); ok {
		return []contract.ClusterServer{NewPowerSourceServer(b)}
	}
	if f, ok := src.(contract.FloatMeasurementSource); ok {
		return []contract.ClusterServer{NewPowerSourceServerFromFloat(f)}
	}
	return nil
}

// electricalSensorServers materialises the ElectricalSensor endpoint's
// full surface. PowerTopology is mandatory for the device type, so it
// ships whether or not the device has anything topological to say;
// ElectricalEnergyMeasurement joins only when the channel actually
// reports a counter, since conformance O.a+ requires at least one of the
// two measurement clusters, not both.
func electricalSensorServers(src any) []contract.ClusterServer {
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
