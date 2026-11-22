// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/measurement"
	psdef "thingfabric.dev/go-fabric/cluster/spec/powersource"
	"thingfabric.dev/go-fabric/contract"
)

// mainsSource is a mains-powered device's power source: it states the
// Wired selection and carries no battery reading.
type mainsSource struct{ current contract.WiredCurrent }

func (mainsSource) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementBattery
}

func (m mainsSource) MatterPowerSourceSelection() contract.PowerSourceSelection {
	return contract.PowerSourceSelection{Wired: true, WiredCurrent: m.current}
}

// lowBattery is a battery reading that states the zero selection.
type lowBattery struct{ fakeBool }

func (lowBattery) MatterPowerSourceSelection() contract.PowerSourceSelection {
	return contract.PowerSourceSelection{}
}

// TestPowerSourceProjectionWired: a source whose selection is Wired
// materialises the Wired PowerSource — FeatureMap WIRED, WiredCurrentType
// from the selection (AC for the zero value, matter.js's default,
// PowerSourceServer.ts:57-61 at b25ed960), Description "Mains power", no
// battery attribute, EndpointList its endpoint; a source stating the zero
// selection stays the battery it was.
func TestPowerSourceProjectionWired(t *testing.T) {
	t.Parallel()
	mc := contract.MeasurementContext{EndpointID: 7}
	for _, c := range []struct {
		src  contract.PowerSourceSelectionSource
		want psdef.WiredCurrentTypeEnum
	}{
		{mainsSource{}, psdef.WiredCurrentTypeAc},
		{mainsSource{contract.WiredCurrentDC}, psdef.WiredCurrentTypeDc},
	} {
		if !c.src.MatterPowerSourceSelection().Wired {
			t.Fatal("the source states no Wired selection")
		}
		srvs := measurement.FromMeasurementClass(contract.MeasurementBattery, c.src, mc)
		if len(srvs) != 1 {
			t.Fatalf("%d servers, want 1", len(srvs))
		}
		s := srvs[0]
		if fm, _ := s.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(psdef.FeatureWired) {
			t.Errorf("FeatureMap %v, want WIRED", fm)
		}
		if v, _ := s.MatterRead(psdef.AttrWiredCurrentType); v != uint8(c.want) {
			t.Errorf("WiredCurrentType %v, want %v", v, c.want)
		}
		if v, _ := s.MatterRead(psdef.AttrDescription); v != "Mains power" {
			t.Errorf("Description %v", v)
		}
		if v, ok := s.MatterRead(psdef.AttrBatChargeLevel); ok {
			t.Errorf("BatChargeLevel reads %v on a wired source", v)
		}
		if v, _ := s.MatterRead(psdef.AttrEndpointList); !slices.Equal(v.([]uint16), []uint16{7}) {
			t.Errorf("EndpointList %v, want [7]", v)
		}
	}
	srvs := measurement.FromMeasurementClass(contract.MeasurementBattery, lowBattery{fakeBool{class: contract.MeasurementBattery, val: true, obs: true}}, mc)
	if fm, _ := srvs[0].MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(psdef.FeatureBattery) {
		t.Errorf("the zero selection: FeatureMap %v, want BAT", fm)
	}
}

// replaceableBattery is a battery reading that states its battery's
// replaceability.
type replaceableBattery struct {
	fakeBool
	r contract.BatteryReplaceability
}

func (b replaceableBattery) MatterPowerSourceSelection() contract.PowerSourceSelection {
	return contract.PowerSourceSelection{BatteryReplaceability: b.r}
}

// TestPowerSourceBatteryReplaceability holds R2-P6-doc-1465 (decision
// 141): BatReplaceability is the host's statement on its selection, user
// replaceable when it states none — the value every battery was served
// with before.
func TestPowerSourceBatteryReplaceability(t *testing.T) {
	t.Parallel()
	mc := contract.MeasurementContext{EndpointID: 7}
	for _, c := range []struct {
		src  any
		want psdef.BatReplaceabilityEnum
	}{
		{fakeBool{class: contract.MeasurementBattery}, psdef.BatReplaceabilityUserReplaceable},
		{lowBattery{fakeBool{class: contract.MeasurementBattery}}, psdef.BatReplaceabilityUserReplaceable},
		{replaceableBattery{fakeBool{class: contract.MeasurementBattery}, contract.BatteryNotReplaceable}, psdef.BatReplaceabilityNotReplaceable},
		{replaceableBattery{fakeBool{class: contract.MeasurementBattery}, contract.BatteryFactoryReplaceable}, psdef.BatReplaceabilityFactoryReplaceable},
		{replaceableBattery{fakeBool{class: contract.MeasurementBattery}, contract.BatteryReplaceabilityUnspecified}, psdef.BatReplaceabilityUnspecified},
	} {
		srvs := measurement.FromMeasurementClass(contract.MeasurementBattery, c.src, mc)
		if len(srvs) != 1 {
			t.Fatalf("%T: %d servers, want 1", c.src, len(srvs))
		}
		if v, _ := srvs[0].MatterRead(psdef.AttrBatReplaceability); v != uint8(c.want) {
			t.Errorf("%T %+v: BatReplaceability %v, want %d", c.src, c.src, v, c.want)
		}
	}
}
