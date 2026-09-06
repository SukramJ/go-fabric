// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestBuiltinMeasurementKindsAnswerUnchanged pins the answer every
// built-in class gave before the measurement-kind set was opened up.
//
// The expectations below are written out as literals rather than read
// back from the registry: a table derived from the same map it checks
// would confirm a transcription slip instead of catching it. Adding a
// built-in adds a row here; changing a row is a wire-visible change to
// what a bridged sensor advertises, never a refactor.
func TestBuiltinMeasurementKindsAnswerUnchanged(t *testing.T) {
	t.Parallel()

	// materializes records which built-ins carry a materialiser once
	// cluster/measurement is linked in — which this test package does not
	// import, so the column reads false throughout here and the same table
	// is re-run with the import in place by the cluster-side pin. What it
	// pins here is that the registry answers the ids for every built-in
	// whether or not the cluster package was linked.
	cases := []struct {
		name         string
		class        contract.MeasurementClass
		deviceType   uint16
		clusterID    uint32
		materializes bool
	}{
		{"None", contract.MeasurementNone, 0, 0, false},
		{"Temperature", contract.MeasurementTemperature, 0x0302, 0x0402, false},
		{"Humidity", contract.MeasurementHumidity, 0x0307, 0x0405, false},
		{"Illuminance", contract.MeasurementIlluminance, 0x0106, 0x0400, false},
		{"Pressure", contract.MeasurementPressure, 0x0305, 0x0403, false},
		{"CO2", contract.MeasurementCO2, 0x002C, 0x040D, false},
		{"PM25", contract.MeasurementPM25, 0x002C, 0x042A, false},
		{"PM10", contract.MeasurementPM10, 0x002C, 0x042D, false},
		{"Occupancy", contract.MeasurementOccupancy, 0x0107, 0x0406, false},
		{"Contact", contract.MeasurementContact, 0x0015, 0x0045, false},
		// Leak answers ContactSensor, not WaterLeakDetector (0x0043),
		// on purpose — see the entry's comment in matter.go.
		{"Leak", contract.MeasurementLeak, 0x0015, 0x0045, false},
		{"Battery", contract.MeasurementBattery, 0, 0x002F, false},
		{"Power", contract.MeasurementPower, 0, 0x0090, false},
		{"Energy", contract.MeasurementEnergy, 0, 0x0091, false},
		{"MomentarySwitch", contract.MeasurementMomentarySwitch, 0x000F, 0x003B, false},
		{"Electrical", contract.MeasurementElectrical, 0x0510, 0x0090, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := contract.MeasurementClassDeviceType(tc.class); got != tc.deviceType {
				t.Errorf("MeasurementClassDeviceType(%s) = 0x%04X, want 0x%04X", tc.name, got, tc.deviceType)
			}
			if got := contract.MeasurementClassClusterID(tc.class); got != tc.clusterID {
				t.Errorf("MeasurementClassClusterID(%s) = 0x%04X, want 0x%04X", tc.name, got, tc.clusterID)
			}
			kind, ok := contract.MeasurementKindFor(tc.class)
			if !ok {
				t.Fatalf("MeasurementKindFor(%s): not registered", tc.name)
			}
			if kind.DeviceType != tc.deviceType || kind.ClusterID != tc.clusterID {
				t.Errorf("MeasurementKindFor(%s) = %+v, want DeviceType 0x%04X / ClusterID 0x%04X",
					tc.name, kind, tc.deviceType, tc.clusterID)
			}
			if kind.Name == "" {
				t.Errorf("MeasurementKindFor(%s): empty Name", tc.name)
			}
			_, hasMaterializer := contract.MeasurementMaterializerFor(tc.class)
			if hasMaterializer != tc.materializes {
				t.Errorf("MeasurementMaterializerFor(%s) present = %v, want %v", tc.name, hasMaterializer, tc.materializes)
			}
		})
	}
}

// TestRegisterMeasurementKindIsAnsweredByBothLookups verifies that a
// host-registered kind is indistinguishable from a built-in at every
// lookup, which is the whole point of opening the set.
func TestRegisterMeasurementKindIsAnsweredByBothLookups(t *testing.T) {
	t.Parallel()

	want := contract.MeasurementKind{
		Name:        "Soil Moisture",
		DeviceType:  0x0307,
		ClusterID:   0x0405,
		Materialize: func(any, contract.MeasurementContext) []contract.ClusterServer { return nil },
	}
	class := contract.RegisterMeasurementKind(want)

	if class <= contract.MeasurementElectrical {
		t.Fatalf("RegisterMeasurementKind returned %d, want a class above the built-in range", class)
	}
	if got := contract.MeasurementClassDeviceType(class); got != want.DeviceType {
		t.Errorf("MeasurementClassDeviceType(registered) = 0x%04X, want 0x%04X", got, want.DeviceType)
	}
	if got := contract.MeasurementClassClusterID(class); got != want.ClusterID {
		t.Errorf("MeasurementClassClusterID(registered) = 0x%04X, want 0x%04X", got, want.ClusterID)
	}
	got, ok := contract.MeasurementKindFor(class)
	if !ok || got.Name != want.Name || got.DeviceType != want.DeviceType || got.ClusterID != want.ClusterID {
		t.Errorf("MeasurementKindFor(registered) = %+v, %v; want %+v, true", got, ok, want)
	}
	if _, ok := contract.MeasurementMaterializerFor(class); !ok {
		t.Error("MeasurementMaterializerFor(registered): no materializer; the registry accepted a kind it cannot build")
	}
}

// TestRegisterMeasurementKindDuplicateMintsASecondClass holds the
// documented duplicate behaviour: an equal descriptor registered twice
// yields two distinct classes that answer alike, rather than one shared
// class or an error.
func TestRegisterMeasurementKindDuplicateMintsASecondClass(t *testing.T) {
	t.Parallel()

	kind := contract.MeasurementKind{
		Name:        "Noise Level",
		DeviceType:  0x002C,
		ClusterID:   0x040D,
		Materialize: func(any, contract.MeasurementContext) []contract.ClusterServer { return nil },
	}
	first := contract.RegisterMeasurementKind(kind)
	second := contract.RegisterMeasurementKind(kind)

	if first == second {
		t.Fatalf("duplicate registration returned the same class %d; the documented behaviour is a fresh class per call", first)
	}
	for _, class := range []contract.MeasurementClass{first, second} {
		got, ok := contract.MeasurementKindFor(class)
		if !ok || got.Name != kind.Name || got.DeviceType != kind.DeviceType || got.ClusterID != kind.ClusterID {
			t.Errorf("MeasurementKindFor(%d) = %+v, %v; want %+v, true", class, got, ok, kind)
		}
	}
}

// TestRegisterMeasurementKindConcurrent holds the documented
// concurrency promise: registrations racing each other still get
// distinct classes, each resolving to its own descriptor. Run under
// -race it also covers the registry's locking.
func TestRegisterMeasurementKindConcurrent(t *testing.T) {
	t.Parallel()

	const registrations = 16

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		classes = make(map[contract.MeasurementClass]contract.MeasurementKind, registrations)
	)
	for i := range registrations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kind := contract.MeasurementKind{
				Name:        "Concurrent",
				ClusterID:   uint32(0x1000 + i),
				Materialize: func(any, contract.MeasurementContext) []contract.ClusterServer { return nil },
			}
			class := contract.RegisterMeasurementKind(kind)
			mu.Lock()
			defer mu.Unlock()
			classes[class] = kind
		}()
	}
	wg.Wait()

	if len(classes) != registrations {
		t.Fatalf("got %d distinct classes for %d registrations", len(classes), registrations)
	}
	for class, kind := range classes {
		if got := contract.MeasurementClassClusterID(class); got != kind.ClusterID {
			t.Errorf("MeasurementClassClusterID(%d) = 0x%04X, want 0x%04X", class, got, kind.ClusterID)
		}
	}
}

// TestUnregisteredMeasurementClassKeepsTheZeroFallback pins the
// behaviour the two switches had for a class they did not name: zero
// from both, never a panic. The eligibility classifier turns that into
// an Unmappable verdict.
func TestUnregisteredMeasurementClassKeepsTheZeroFallback(t *testing.T) {
	t.Parallel()

	unknown := contract.MeasurementClass(-1)
	if got := contract.MeasurementClassDeviceType(unknown); got != 0 {
		t.Errorf("MeasurementClassDeviceType(unknown) = 0x%04X, want 0", got)
	}
	if got := contract.MeasurementClassClusterID(unknown); got != 0 {
		t.Errorf("MeasurementClassClusterID(unknown) = 0x%04X, want 0", got)
	}
	if _, ok := contract.MeasurementKindFor(unknown); ok {
		t.Error("MeasurementKindFor(unknown) reported the class as registered")
	}
}

// TestRegisterMeasurementKindRefusesAKindItCannotBuild is the guard on
// the registry's one hard promise: everything it answers for is
// something the bridge can actually materialise. Without the refusal a
// host could register a name plus two ids, watch the eligibility
// classifier report the source as exposable, and get no endpoint —
// strictly worse than the class never having existed.
func TestRegisterMeasurementKindRefusesAKindItCannotBuild(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("RegisterMeasurementKind accepted a kind with no Materialize; it would advertise an exposure the bridge cannot deliver")
		}
	}()
	contract.RegisterMeasurementKind(contract.MeasurementKind{
		Name:       "Unbuildable",
		DeviceType: 0x0302,
		ClusterID:  0x0402,
	})
}

// TestSetMeasurementMaterializerRejectsANonBuiltinClass holds the seam
// to its stated scope: it completes the classes this library declares as
// constants, and a host kind carries its materialiser through
// RegisterMeasurementKind instead.
func TestSetMeasurementMaterializerRejectsANonBuiltinClass(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("SetMeasurementMaterializer accepted a class outside the built-in range")
		}
	}()
	contract.SetMeasurementMaterializer(
		contract.MeasurementClass(1<<20),
		func(any, contract.MeasurementContext) []contract.ClusterServer { return nil },
	)
}
