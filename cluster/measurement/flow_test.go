// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

// flowSrc is a FloatMeasurementSource for the flow tests.
type flowSrc struct {
	value    float64
	observed bool
}

func (s flowSrc) MatterFloatValue() (float64, bool) { return s.value, s.observed }
func (flowSrc) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementFlow
}

func TestFlowServerMeasuredValueScalesAndClamps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		m3h  float64
		want uint16
	}{
		{"zero", 0, 0},
		{"one decimal", 12.3, 123},
		{"rounds half up", 0.05, 1},
		{"negative clamps to min", -4, 0},
		{"NaN clamps to min", math.NaN(), 0},
		{"above range clamps below the null sentinel", 1e6, 65534},
		{"exact max", 6553.4, 65534},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := NewFlowServer(flowSrc{value: tc.m3h, observed: true})
			got, ok := srv.MatterRead(attrMeasuredValue)
			if !ok {
				t.Fatal("MeasuredValue ok = false")
			}
			if v, isU16 := got.(uint16); !isU16 || v != tc.want {
				t.Fatalf("MeasuredValue = %v (%T), want uint16 %d", got, got, tc.want)
			}
		})
	}
}

func TestFlowServerUnobservedAndNilSourceReadNull(t *testing.T) {
	t.Parallel()
	for name, srv := range map[string]*FlowServer{
		"unobserved": NewFlowServer(flowSrc{value: 3, observed: false}),
		"nil source": NewFlowServer(nil),
	} {
		got, ok := srv.MatterRead(attrMeasuredValue)
		if !ok || got != nil {
			t.Errorf("%s: MeasuredValue = (%v, %v), want (nil, true)", name, got, ok)
		}
	}
}

func TestFlowServerStaticAttributesAndGlobals(t *testing.T) {
	t.Parallel()
	srv := NewFlowServer(flowSrc{value: 1, observed: true})
	want := map[uint32]any{
		attrMinMeasuredValue:              uint16(0),
		attrMaxMeasuredValue:              uint16(65534),
		attrTolerance:                     uint16(0),
		cluster.AttrGlobalFeatureMap:      uint32(0),
		cluster.AttrGlobalClusterRevision: FlowRevision(),
	}
	for id, w := range want {
		got, ok := srv.MatterRead(id)
		if !ok || got != w {
			t.Errorf("MatterRead(0x%04X) = (%v, %v), want (%v, true)", id, got, ok, w)
		}
	}
	if _, ok := srv.MatterRead(0x0004); ok {
		t.Error("MatterRead(0x0004) ok = true for an attribute the cluster does not define")
	}
	if srv.MatterClusterID() != ClusterFlowMeasurement {
		t.Errorf("MatterClusterID = 0x%04X", srv.MatterClusterID())
	}
	if !slices.Equal(srv.MatterReportable(), []uint32{attrMeasuredValue}) {
		t.Errorf("MatterReportable = %v", srv.MatterReportable())
	}
	if srv.MatterDataVersion() == 0 {
		t.Error("MatterDataVersion = 0; a DataVersion of zero is reserved")
	}
}

func TestFlowServerIsReadOnlyAndCommandless(t *testing.T) {
	t.Parallel()
	srv := NewFlowServer(flowSrc{})
	if err := srv.MatterWrite(context.Background(), attrMeasuredValue, uint64(1)); !errors.Is(err, errReadOnly) {
		t.Errorf("MatterWrite err = %v, want errReadOnly", err)
	}
	if _, err := srv.MatterInvoke(context.Background(), 0, nil); !errors.Is(err, errNoCommands) {
		t.Errorf("MatterInvoke err = %v, want errNoCommands", err)
	}
}

func TestFlowClassMaterialisesTheFlowServer(t *testing.T) {
	t.Parallel()
	servers := FromMeasurementClass(contract.MeasurementFlow, flowSrc{value: 2, observed: true}, contract.MeasurementContext{EndpointID: 3})
	if len(servers) != 1 {
		t.Fatalf("FromMeasurementClass(Flow) returned %d servers, want 1", len(servers))
	}
	if _, ok := servers[0].(*FlowServer); !ok {
		t.Fatalf("FromMeasurementClass(Flow) returned %T, want *FlowServer", servers[0])
	}
	// A source that cannot be read as a float yields nothing.
	if got := FromMeasurementClass(contract.MeasurementFlow, struct{}{}, contract.MeasurementContext{}); got != nil {
		t.Errorf("FromMeasurementClass(Flow, non-float) = %v, want nil", got)
	}
}
