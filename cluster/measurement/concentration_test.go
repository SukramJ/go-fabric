// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
)

// TestFurtherConcentrationServers covers the seven concentration servers
// beyond CO2 / PM2.5 / PM10 the way the CO2 tests cover the first three:
// cluster id, MeasuredValue forwarded as float32, null while unobserved,
// MeasurementUnit, MeasurementMedium Air, FeatureMap MEA, revision 5,
// writes and invokes refused. Unit values are the MeasurementUnitEnum
// members (Ppm = 0, Ugm3 = 4, Bqm3 = 7).
func TestFurtherConcentrationServers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   uint32
		unit uint8
		mk   func(contract.FloatMeasurementSource) contract.ClusterServer
	}{
		{"CO", 0x040C, 0, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewCOConcentrationServer(f)
		}},
		{"NO2", 0x0413, 0, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewNO2ConcentrationServer(f)
		}},
		{"Ozone", 0x0415, 0, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewOzoneConcentrationServer(f)
		}},
		{"Formaldehyde", 0x042B, 0, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewFormaldehydeConcentrationServer(f)
		}},
		{"PM1", 0x042C, 4, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewPM1ConcentrationServer(f)
		}},
		{"TVOC", 0x042E, 0, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewTVOCConcentrationServer(f)
		}},
		{"Radon", 0x042F, 7, func(f contract.FloatMeasurementSource) contract.ClusterServer {
			return measurement.NewRadonConcentrationServer(f)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := tc.mk(fakeFloat{val: 12.5, obs: true})
			if got := s.MatterClusterID(); got != tc.id {
				t.Errorf("ClusterID = 0x%04X, want 0x%04X", got, tc.id)
			}
			if v, ok := s.MatterRead(0x0000); !ok || v != float32(12.5) {
				t.Errorf("MeasuredValue = %v (%v), want 12.5", v, ok)
			}
			if v, ok := s.MatterRead(0x0008); !ok || v != tc.unit {
				t.Errorf("MeasurementUnit = %v (%v), want %d", v, ok, tc.unit)
			}
			if v, ok := s.MatterRead(0x0009); !ok || v != uint8(0) {
				t.Errorf("MeasurementMedium = %v (%v), want 0 (Air)", v, ok)
			}
			if v, ok := s.MatterRead(0xFFFC); !ok || v != uint32(1) {
				t.Errorf("FeatureMap = %v (%v), want 1 (MEA)", v, ok)
			}
			if v, ok := s.MatterRead(attrClusterRevision); !ok || v != uint16(5) {
				t.Errorf("ClusterRevision = %v (%v), want 5", v, ok)
			}
			if err := s.MatterWrite(context.Background(), 0x0000, float32(1)); err == nil {
				t.Error("MatterWrite succeeded on a read-only cluster")
			}
			if _, err := s.MatterInvoke(context.Background(), 0x00, nil); err == nil {
				t.Error("MatterInvoke succeeded on a cluster without commands")
			}
			if _, ok := s.(contract.ClusterDataVersion); !ok {
				t.Error("server does not track its DataVersion")
			}
			unobserved := tc.mk(fakeFloat{obs: false})
			if v, ok := unobserved.MatterRead(0x0000); !ok || v != nil {
				t.Errorf("unobserved MeasuredValue = %v (%v), want (nil, true)", v, ok)
			}
		})
	}
}
