// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	booldef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	co2def "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	flowdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	illdef "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	pm10def "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm25def "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	ptdef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	prsdef "github.com/SukramJ/go-fabric/cluster/spec/pressuremeasurement"
	rhdef "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement"
	tmpdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/tlv"
)

// The servers' cluster identity is their generated definition
// (cluster/spec/<name>, ADR 0013): the FeatureMap and ClusterRevision they
// answer, the AttributeList and EventList, and the status a write
// answers. Each server here serves one fixed feature selection and one
// fixed set of optional attributes, so its definition is bound once. The
// projection of the host's readings onto the attributes' units stays in
// the servers.
var (
	// Tolerance (0x3, "O") is served as 0 by every single-value cluster.
	tempInst        = mustInstance(tmpdef.Definition, spec.Options{Attributes: []uint32{tmpdef.AttrTolerance}})
	humidityInst    = mustInstance(rhdef.Definition, spec.Options{Attributes: []uint32{rhdef.AttrTolerance}})
	illuminanceInst = mustInstance(illdef.Definition, spec.Options{Attributes: []uint32{illdef.AttrTolerance}})
	pressureInst    = mustInstance(prsdef.Definition, spec.Options{Attributes: []uint32{prsdef.AttrTolerance}})
	flowInst        = mustInstance(flowdef.Definition, spec.Options{Attributes: []uint32{flowdef.AttrTolerance}})

	// CHGEVENT: the StateChange event is emitted on every StateValue
	// change, the feature matter.js BooleanStateServer enables by
	// default; it makes the event mandatory.
	booleanStateInst = mustInstance(booldef.Definition, spec.Options{Features: uint32(booldef.FeatureChangeEvent)})

	// PIR: every HM motion detector is passive infrared. From revision 5
	// on one sensor-type feature is required, and controllers that
	// branch on it (matter.js OccupancySensingServer.ts
	// `features.passiveInfrared`) classify the sensor from this bit
	// alone. HoldTime (0x3) and the deprecated delay attributes it gates
	// are not served.
	occupancyInst = mustInstance(occdef.Definition, spec.Options{Features: uint32(occdef.FeaturePassiveInfrared)})

	// No feature: the reportable levels are the conformance-mandatory
	// Unknown / Good / Poor.
	airQualityInst = mustInstance(aqdef.Definition, spec.Options{})

	// MEA only; Uncertainty ("[MEA]") is not served.
	co2Inst  = mustInstance(co2def.Definition, spec.Options{Features: concFeatureMEA})
	pm25Inst = mustInstance(pm25def.Definition, spec.Options{Features: concFeatureMEA})
	pm10Inst = mustInstance(pm10def.Definition, spec.Options{Features: concFeatureMEA})

	// BAT; BatPercentRemaining ("[BAT]") only with a percentage source.
	powerSourceInst      = mustInstance(psdef.Definition, spec.Options{Features: pwrFeatureBAT})
	powerSourceFloatInst = mustInstance(psdef.Definition, spec.Options{Features: pwrFeatureBAT, Attributes: []uint32{psdef.AttrBatPercentRemaining}})

	// NODE: the cluster's whole surface is its globals.
	powerTopologyInst = mustInstance(ptdef.Definition, spec.Options{Features: powerTopologyFeatureNode})

	// ALTC with the optional Voltage and ActiveCurrent and the "[ALTC]"
	// Frequency, which read as null without a readings surface.
	electricalPowerInst = mustInstance(epmdef.Definition, spec.Options{
		Features:   elPwrFeatureAltC,
		Attributes: []uint32{epmdef.AttrVoltage, epmdef.AttrActiveCurrent, epmdef.AttrFrequency},
	})

	// IMPE | CUME: Accuracy and CumulativeEnergyImported.
	electricalEnergyInst = mustInstance(eemdef.Definition, spec.Options{Features: elEnFeatureIMPE | elEnFeatureCUME})
)

// mustInstance binds a definition to a server's fixed selection. The
// selections are constants of this package, so an error is a programming
// error the package's tests catch at init.
func mustInstance(def *spec.Cluster, opts spec.Options) *spec.Instance {
	inst, err := spec.New(def, opts)
	if err != nil {
		panic(fmt.Sprintf("measurement: %v", err))
	}
	return inst
}

// writeRefusal is what MatterWrite answers: the definition's status — a
// served attribute is read-only, so UNSUPPORTED_WRITE, any other
// UNSUPPORTED_ATTRIBUTE (matter.js AttributeWriteResponse) — which still
// matches errReadOnly.
type writeRefusal struct{ err error }

func (e writeRefusal) Error() string   { return e.err.Error() }
func (e writeRefusal) Unwrap() []error { return []error{e.err, errReadOnly} }

// refuseWrite answers a write to a measurement cluster. Every attribute
// these servers serve carries access "R V", so the definition refuses
// every write.
func refuseWrite(inst *spec.Instance, attrID uint32, value any) error {
	if _, err := inst.ValidateWrite(attrID, value, nil); err != nil {
		return writeRefusal{err}
	}
	return errReadOnly
}

// generated returns the entry as the generated MeasurementAccuracyStruct.
// FixedMax is always present: it is the half of the PercentMax / FixedMax
// choice group the server fills.
func (a AccuracyStruct) generated() epmdef.MeasurementAccuracyStruct {
	ranges := make([]epmdef.MeasurementAccuracyRangeStruct, len(a.AccuracyRanges))
	for i, r := range a.AccuracyRanges {
		fixedMax := r.FixedMax
		ranges[i] = epmdef.MeasurementAccuracyRangeStruct{RangeMin: r.RangeMin, RangeMax: r.RangeMax, FixedMax: &fixedMax}
	}
	return epmdef.MeasurementAccuracyStruct{
		MeasurementType:  epmdef.MeasurementTypeEnum(a.MeasurementType),
		Measured:         a.Measured,
		MinMeasuredValue: a.MinMeasuredValue,
		MaxMeasuredValue: a.MaxMeasuredValue,
		AccuracyRanges:   ranges,
	}
}

// EncodeTLV implements spec.Encodable with the generated codec: the
// ElectricalEnergyMeasurement Accuracy attribute is one struct.
func (a AccuracyStruct) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { a.generated().EncodeTLV(enc, tag) }

// AccuracyList is the ElectricalPowerMeasurement Accuracy attribute
// value: a list of MeasurementAccuracyStruct. Its EncodeTLV is the
// generated codec of electrical-power-measurement.element.ts, so the
// bridge encodes a []AccuracyStruct by converting it.
type AccuracyList []AccuracyStruct

// EncodeTLV implements spec.Encodable.
func (l AccuracyList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	out := make(spec.List[epmdef.MeasurementAccuracyStruct], len(l))
	for i, a := range l {
		out[i] = a.generated()
	}
	out.EncodeTLV(enc, tag)
}

// EnergyMeasurementStruct is the wire payload of the
// ElectricalEnergyMeasurement Cumulative/PeriodicEnergy* attributes, the
// generated struct (electrical-energy-measurement.element.ts). The server
// fills only the mandatory Energy (tag 0, int64 mWh, "0 to 2^62"): the
// timestamp and systime fields (tags 1-4) describe the recording period of
// PERIODIC measurements and are omitted for cumulative readings, as are
// the apparent and reactive energies (APPE / REAE stay clear).
type EnergyMeasurementStruct = eemdef.EnergyMeasurementStruct

// BooleanStateChangeEvent is the payload of BooleanState.StateChange
// (event 0x00, priority Info, field 0 StateValue), the generated payload
// of boolean-state.element.ts.
type BooleanStateChangeEvent = booldef.StateChangeEvent
