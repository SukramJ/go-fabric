// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
)

// TestBuiltinMaterializersAreInstalled pins which built-in classes carry
// a materialiser once this package is linked in — the other half of the
// table in contract's own kind test, which sees none of them because it
// does not import this package.
//
// The four classes marked false are empty on purpose and each for its
// own reason (see the file comment in materializers.go); the twelve
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
		{"MomentarySwitch", contract.MeasurementMomentarySwitch, false},
		{"Electrical", contract.MeasurementElectrical, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := contract.MeasurementMaterializerFor(tc.class); ok != tc.installed {
				t.Errorf("MeasurementMaterializerFor(%s) installed = %v, want %v", tc.name, ok, tc.installed)
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
		Materialize: func(src any) []contract.ClusterServer {
			want.src = src
			return []contract.ClusterServer{want}
		},
	})

	src := struct{}{}
	got := measurement.FromMeasurementClass(class, src)
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
