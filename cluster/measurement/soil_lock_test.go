// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"testing"
	"time"

	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
)

// reentrantSoil is a source that reads its server from inside its own
// read, as a host's source that logs what Matter shows would.
type reentrantSoil struct {
	soilStub
	srv    *SoilMeasurementServer
	nested bool
}

func (s *reentrantSoil) MatterFloatValue() (float64, bool) {
	if !s.nested {
		s.nested = true
		s.srv.MatterRead(soildef.AttrSoilMoistureMeasuredValue)
	}
	return s.value, s.observed
}

// TestSoilSourceReadWithoutTheLock pins G1-16 (ADR 0026 P6): the source
// is host code and is read with the server's lock released.
func TestSoilSourceReadWithoutTheLock(t *testing.T) {
	t.Parallel()
	src := &reentrantSoil{soilStub: soilStub{value: 42, observed: true, limits: demoSoilLimits()}}
	srv := NewSoilMeasurementServer(src)
	src.srv = srv
	got := make(chan any, 1)
	go func() {
		v, _ := srv.MatterRead(soildef.AttrSoilMoistureMeasuredValue)
		got <- v
	}()
	select {
	case v := <-got:
		if v != uint8(42) {
			t.Fatalf("SoilMoistureMeasuredValue %v, want 42", v)
		}
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("the server holds its lock while it reads the source")
	}
}
