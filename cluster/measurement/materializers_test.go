// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

// TestBuiltinMaterializersAreInstalled pins which built-in classes carry
// a materialiser once this package is linked in — the other half of the
// table in contract's own kind test, which sees none of them because it
// does not import this package.
//
// The three classes marked false are empty on purpose and each for its
// own reason (see the file comment in materializers.go); the twenty-one
// marked true are the ones a bridged endpoint's cluster surface comes
// from. A class flipping either way changes what a bridged device
// advertises on the wire, which is never a refactor.
func TestBuiltinMaterializersAreInstalled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		class     contract.MeasurementClass
		installed bool
	}{
		{"None", contract.MeasurementNone, false},
		{"Temperature", contract.MeasurementTemperature, true},
		{"Humidity", contract.MeasurementHumidity, true},
		{"Illuminance", contract.MeasurementIlluminance, true},
		{"Pressure", contract.MeasurementPressure, true},
		{"CO2", contract.MeasurementCO2, true},
		{"PM25", contract.MeasurementPM25, true},
		{"PM10", contract.MeasurementPM10, true},
		{"Occupancy", contract.MeasurementOccupancy, true},
		{"Contact", contract.MeasurementContact, true},
		{"Leak", contract.MeasurementLeak, true},
		{"Battery", contract.MeasurementBattery, true},
		{"Power", contract.MeasurementPower, false},
		{"Energy", contract.MeasurementEnergy, false},
		{"MomentarySwitch", contract.MeasurementMomentarySwitch, true},
		{"Electrical", contract.MeasurementElectrical, true},
		{"Flow", contract.MeasurementFlow, true},
		{"CO", contract.MeasurementCO, true},
		{"NO2", contract.MeasurementNO2, true},
		{"Ozone", contract.MeasurementOzone, true},
		{"Formaldehyde", contract.MeasurementFormaldehyde, true},
		{"PM1", contract.MeasurementPM1, true},
		{"TVOC", contract.MeasurementTVOC, true},
		{"Radon", contract.MeasurementRadon, true},
		{"SoilMoisture", contract.MeasurementSoilMoisture, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if ok := measurement.HasBuiltinMaterializer(tc.class); ok != tc.installed {
				t.Errorf("built-in materializer of %s installed = %v, want %v", tc.name, ok, tc.installed)
			}
			// The built-ins are this package's: contract, which names no
			// server type, reports none for them (W5-api-12).
			if _, ok := contract.MeasurementMaterializerFor(tc.class); ok {
				t.Errorf("contract.MeasurementMaterializerFor(%s) answers a built-in", tc.name)
			}
		})
	}
}

// TestFromMeasurementClassRunsAHostRegisteredKind proves the dispatch is
// the registry rather than a switch over the built-in constants: a class
// this package cannot possibly name reaches its own materialiser through
// the same call the built-ins take.
func TestFromMeasurementClassRunsAHostRegisteredKind(t *testing.T) {
	t.Parallel()

	want := &recordingServer{}
	class := contract.RegisterMeasurementKind(contract.MeasurementKind{
		Name:       "Registry Dispatch",
		DeviceType: 0x0306,
		ClusterID:  0x0404,
		Materialize: func(src any, _ contract.MeasurementContext) []contract.ClusterServer {
			want.src = src
			return []contract.ClusterServer{want}
		},
	})

	src := struct{}{}
	got := measurement.FromMeasurementClass(class, src, contract.MeasurementContext{})
	if len(got) != 1 || got[0] != want {
		t.Fatalf("FromMeasurementClass(host class) = %v, want the registered kind's own server", got)
	}
	if want.src != any(src) {
		t.Errorf("materializer received src %v, want the source it was called with", want.src)
	}
}

// recordingServer is a minimal host-side [contract.ClusterServer] that
// remembers which source its materialiser was handed.
type recordingServer struct {
	src any
}

func (s *recordingServer) MatterClusterID() uint32 { return 0x0404 }

func (s *recordingServer) MatterRead(uint32) (any, bool) { return nil, false }

func (s *recordingServer) MatterWrite(context.Context, uint32, any) error { return nil }

func (s *recordingServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil //nolint:nilnil // command-less cluster
}

func (s *recordingServer) MatterReportable() []uint32 { return nil }

// TestEveryMeasurementDeviceTypeHasASchemaName: every device type a
// built-in measurement class advertises is named by schema.DeviceTypeName,
// the module's one device-type name (W5-api-15) — the guard
// contract.DeviceTypeName's test held for issue #57 (SoilSensor, 0x0045).
func TestEveryMeasurementDeviceTypeHasASchemaName(t *testing.T) {
	t.Parallel()
	for class := contract.MeasurementNone + 1; class <= contract.MeasurementSoilMoisture; class++ {
		dt := contract.MeasurementClassDeviceType(class)
		if dt == 0 {
			continue
		}
		if name, ok := schema.DeviceTypeName(schema.DeviceTypeID(dt)); !ok || name == "" {
			t.Errorf("measurement class %d advertises 0x%04X, which schema does not name", class, dt)
		}
	}
	if name, _ := schema.DeviceTypeName(0x0045); name != "SoilSensor" {
		t.Errorf("schema.DeviceTypeName(0x0045) = %q, want SoilSensor", name)
	}
}
