// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

// The seven concentration clusters beyond CO2 / PM2.5 / PM10. Each is the
// ConcentrationMeasurement family shape (the same attribute layout,
// features and revision as the base cluster), so each server is the
// shared concentrationServer bound to its own generated definition and
// MeasurementUnit. matter.js has no server logic for any of them:
// packages/node/src/behaviors/<name>/<Name>Server.ts is an empty
// subclass, and the family base concentration-measurement/ has no server
// file.

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	codef "github.com/SukramJ/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	hchodef "github.com/SukramJ/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	no2def "github.com/SukramJ/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	o3def "github.com/SukramJ/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	pm1def "github.com/SukramJ/go-fabric/cluster/spec/pm1concentrationmeasurement"
	radondef "github.com/SukramJ/go-fabric/cluster/spec/radonconcentrationmeasurement"
	tvocdef "github.com/SukramJ/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	"github.com/SukramJ/go-fabric/contract"
)

// Cluster IDs of the seven further concentration clusters, each its
// generated definition's.
const (
	ClusterCOConcentration           = codef.ClusterID    // CarbonMonoxideConcentrationMeasurement
	ClusterNO2Concentration          = no2def.ClusterID   // NitrogenDioxideConcentrationMeasurement
	ClusterOzoneConcentration        = o3def.ClusterID    // OzoneConcentrationMeasurement
	ClusterFormaldehydeConcentration = hchodef.ClusterID  // FormaldehydeConcentrationMeasurement
	ClusterPM1Concentration          = pm1def.ClusterID   // Pm1ConcentrationMeasurement
	ClusterTVOCConcentration         = tvocdef.ClusterID  // TotalVolatileOrganicCompoundsConcentrationMeasurement
	ClusterRadonConcentration        = radondef.ClusterID // RadonConcentrationMeasurement
)

// concUnitBecquerelPerCubicMeter is the MeasurementUnitEnum member for
// Bq/m³, the unit the model carries for radon.
const concUnitBecquerelPerCubicMeter = uint8(radondef.MeasurementUnitBqm3)

// concOptions is the selection the seven servers serve: MEA only, as
// CO2 / PM2.5 / PM10; Uncertainty ("[MEA]") is not served.
var concOptions = spec.Options{Features: concFeatureMEA}

var (
	coInst    = mustInstance(codef.Definition, concOptions)
	no2Inst   = mustInstance(no2def.Definition, concOptions)
	o3Inst    = mustInstance(o3def.Definition, concOptions)
	hchoInst  = mustInstance(hchodef.Definition, concOptions)
	pm1Inst   = mustInstance(pm1def.Definition, concOptions)
	tvocInst  = mustInstance(tvocdef.Definition, concOptions)
	radonInst = mustInstance(radondef.Definition, concOptions)
)

// Each server inherits [cluster.DataVersionTracker] via concentrationServer.
var (
	_ contract.ClusterDataVersion = (*COConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*NO2ConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*OzoneConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*FormaldehydeConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*PM1ConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*TVOCConcentrationServer)(nil)
	_ contract.ClusterDataVersion = (*RadonConcentrationServer)(nil)
)

// COConcentrationServer projects a [contract.FloatMeasurementSource] onto
// CarbonMonoxideConcentrationMeasurement (0x040C). Model and wire unit:
// ppm.
type COConcentrationServer struct{ concentrationServer }

// NewCOConcentrationServer constructs a COConcentrationServer backed by src.
func NewCOConcentrationServer(src contract.FloatMeasurementSource) *COConcentrationServer {
	return &COConcentrationServer{concentrationServer{src: src, clusterID: ClusterCOConcentration, unit: concUnitPPM, inst: coInst}}
}

// NO2ConcentrationServer projects a [contract.FloatMeasurementSource]
// onto NitrogenDioxideConcentrationMeasurement (0x0413). Model and wire
// unit: ppm.
type NO2ConcentrationServer struct{ concentrationServer }

// NewNO2ConcentrationServer constructs a NO2ConcentrationServer backed by src.
func NewNO2ConcentrationServer(src contract.FloatMeasurementSource) *NO2ConcentrationServer {
	return &NO2ConcentrationServer{concentrationServer{src: src, clusterID: ClusterNO2Concentration, unit: concUnitPPM, inst: no2Inst}}
}

// OzoneConcentrationServer projects a [contract.FloatMeasurementSource]
// onto OzoneConcentrationMeasurement (0x0415). Model and wire unit: ppm.
type OzoneConcentrationServer struct{ concentrationServer }

// NewOzoneConcentrationServer constructs an OzoneConcentrationServer backed by src.
func NewOzoneConcentrationServer(src contract.FloatMeasurementSource) *OzoneConcentrationServer {
	return &OzoneConcentrationServer{concentrationServer{src: src, clusterID: ClusterOzoneConcentration, unit: concUnitPPM, inst: o3Inst}}
}

// FormaldehydeConcentrationServer projects a
// [contract.FloatMeasurementSource] onto
// FormaldehydeConcentrationMeasurement (0x042B). Model and wire unit: ppm.
type FormaldehydeConcentrationServer struct{ concentrationServer }

// NewFormaldehydeConcentrationServer constructs a FormaldehydeConcentrationServer backed by src.
func NewFormaldehydeConcentrationServer(src contract.FloatMeasurementSource) *FormaldehydeConcentrationServer {
	return &FormaldehydeConcentrationServer{concentrationServer{src: src, clusterID: ClusterFormaldehydeConcentration, unit: concUnitPPM, inst: hchoInst}}
}

// PM1ConcentrationServer projects a [contract.FloatMeasurementSource]
// onto Pm1ConcentrationMeasurement (0x042C). Model and wire unit: µg/m³,
// as PM2.5 and PM10.
type PM1ConcentrationServer struct{ concentrationServer }

// NewPM1ConcentrationServer constructs a PM1ConcentrationServer backed by src.
func NewPM1ConcentrationServer(src contract.FloatMeasurementSource) *PM1ConcentrationServer {
	return &PM1ConcentrationServer{concentrationServer{src: src, clusterID: ClusterPM1Concentration, unit: concUnitMicroGramPerCubicMeter, inst: pm1Inst}}
}

// TVOCConcentrationServer projects a [contract.FloatMeasurementSource]
// onto TotalVolatileOrganicCompoundsConcentrationMeasurement (0x042E).
// Model and wire unit: ppm.
type TVOCConcentrationServer struct{ concentrationServer }

// NewTVOCConcentrationServer constructs a TVOCConcentrationServer backed by src.
func NewTVOCConcentrationServer(src contract.FloatMeasurementSource) *TVOCConcentrationServer {
	return &TVOCConcentrationServer{concentrationServer{src: src, clusterID: ClusterTVOCConcentration, unit: concUnitPPM, inst: tvocInst}}
}

// RadonConcentrationServer projects a [contract.FloatMeasurementSource]
// onto RadonConcentrationMeasurement (0x042F). Model and wire unit: Bq/m³.
type RadonConcentrationServer struct{ concentrationServer }

// NewRadonConcentrationServer constructs a RadonConcentrationServer backed by src.
func NewRadonConcentrationServer(src contract.FloatMeasurementSource) *RadonConcentrationServer {
	return &RadonConcentrationServer{concentrationServer{src: src, clusterID: ClusterRadonConcentration, unit: concUnitBecquerelPerCubicMeter, inst: radonInst}}
}

// concentrationMaterializers installs the seven kinds the same way the
// CO2 / PM2.5 / PM10 kinds are installed: the concentration cluster plus
// the mandatory AirQuality cluster. None of the seven has a guideline in
// airQualityGoodBelow, so their AirQuality reads Unknown.
func concentrationMaterializers() {
	for class, build := range map[contract.MeasurementClass]func(contract.FloatMeasurementSource) contract.ClusterServer{
		contract.MeasurementCO:    func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewCOConcentrationServer(f) },
		contract.MeasurementNO2:   func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewNO2ConcentrationServer(f) },
		contract.MeasurementOzone: func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewOzoneConcentrationServer(f) },
		contract.MeasurementFormaldehyde: func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return NewFormaldehydeConcentrationServer(f)
		},
		contract.MeasurementPM1:   func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewPM1ConcentrationServer(f) },
		contract.MeasurementTVOC:  func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewTVOCConcentrationServer(f) },
		contract.MeasurementRadon: func(f contract.FloatMeasurementSource) contract.ClusterServer { return NewRadonConcentrationServer(f) },
	} {
		contract.SetMeasurementMaterializer(class, airQualityServers(class, build))
	}
}
