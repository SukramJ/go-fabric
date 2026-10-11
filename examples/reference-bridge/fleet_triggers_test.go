// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/energy"
	mtrid "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
)

// TestBoolCfgTestEventTriggers replays CHIP's all-clusters handler
// (boolcfg-stub.cpp:27-49) through the daemon's dispatch: SensorTrigger
// raises the enabled alarms, SensorUntrigger clears them, the endpoint
// bits are ignored.
func TestBoolCfgTestEventTriggers(t *testing.T) {
	f, _ := startFleetBridge(t)
	ctx := context.Background()
	if err := f.testEventTrigger(ctx, triggerBoolCfgSensorTrigger|0x0000_0014_0000_0000); err != nil {
		t.Fatalf("SensorTrigger: %v", err)
	}
	if got := f.contact.config.State().AlarmsActive; got != boolcfg.AlarmVisual {
		t.Fatalf("AlarmsActive after SensorTrigger = %v, want visual", got)
	}
	if err := f.testEventTrigger(ctx, triggerBoolCfgSensorUntrigger); err != nil {
		t.Fatalf("SensorUntrigger: %v", err)
	}
	if got := f.contact.config.State().AlarmsActive; got != 0 {
		t.Fatalf("AlarmsActive after SensorUntrigger = %v, want none", got)
	}
}

// TestMeterTestEventTriggers replays the energy-gateway app's handler
// (MeterIdentificationEventTriggers.cpp:184-238): two updates alternate
// the presets, the clear restores what the first update saved.
func TestMeterTestEventTriggers(t *testing.T) {
	f, _ := startFleetBridge(t)
	ctx := context.Background()
	read := func(id uint32) any {
		t.Helper()
		v, _ := f.meter.srv.MatterRead(id)
		return v
	}
	beforeType, beforeSerial := read(mtrid.AttrMeterType), read(mtrid.AttrMeterSerialNumber)
	if beforeType != nil {
		t.Fatalf("MeterType before the test event = %v, want null", beforeType)
	}

	if err := f.testEventTrigger(ctx, triggerMeterAttributesValueUpdate); err != nil {
		t.Fatalf("AttributesValueUpdate: %v", err)
	}
	if got := read(mtrid.AttrMeterSerialNumber); got != "TST-123456789" {
		t.Fatalf("MeterSerialNumber after the first update = %v", got)
	}
	if got := read(mtrid.AttrMeterType); got != uint8(energy.MeterTypeUtility) {
		t.Fatalf("MeterType after the first update = %v (%T)", got, got)
	}
	if err := f.testEventTrigger(ctx, triggerMeterAttributesValueUpdate); err != nil {
		t.Fatalf("AttributesValueUpdate: %v", err)
	}
	if got := read(mtrid.AttrPointOfDelivery); got != "New delivery point" {
		t.Fatalf("PointOfDelivery after the second update = %v", got)
	}

	if err := f.testEventTrigger(ctx, triggerMeterAttributesValueUpdateClear); err != nil {
		t.Fatalf("AttributesValueUpdateClear: %v", err)
	}
	if got := read(mtrid.AttrMeterType); got != nil {
		t.Fatalf("MeterType after the clear = %v, want null", got)
	}
	if got := read(mtrid.AttrMeterSerialNumber); got != beforeSerial {
		t.Fatalf("MeterSerialNumber after the clear = %v, want %v", got, beforeSerial)
	}
	if got := read(mtrid.AttrPointOfDelivery); got != nil {
		t.Fatalf("PointOfDelivery after the clear = %v, want null", got)
	}
}
