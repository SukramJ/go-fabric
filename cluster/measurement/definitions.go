// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	booldef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	flowdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	illdef "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement"
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

	// BAT; BatPercentRemaining ("[BAT]") only with a percentage source.
	powerSourceInst      = mustInstance(psdef.Definition, spec.Options{Features: pwrFeatureBAT})
	powerSourceFloatInst = mustInstance(psdef.Definition, spec.Options{Features: pwrFeatureBAT, Attributes: []uint32{psdef.AttrBatPercentRemaining}})
	// WIRED: a mains-powered device's source (contract.PowerSourceSelection).
	powerSourceWiredInst = mustInstance(psdef.Definition, spec.Options{Features: uint32(psdef.FeatureWired)})

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
