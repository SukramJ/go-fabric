// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"testing"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster/spec"
	thermostatdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/device"
)

// TestDemoThermostatServesSuggestions: the demo thermostat serves
// ThermostatSuggestions by default, without --extensions (owner decision
// 336), and a suggestion of one of its presets is taken and followed on
// the node's Matter time, which the node hands the server as the device
// joins its topology (attached here as the node attaches it).
func TestDemoThermostatServesSuggestions(t *testing.T) {
	t.Parallel()
	opts := device.Options{StableKey: device.StringKey("demo-thermostat"), Name: "Thermostat"}
	d, err := newDemoThermostat("Thermostat").device(opts)
	if err != nil {
		t.Fatal(err)
	}
	if fm, _ := d.Thermostat().Underlying().MatterRead(spec.AttrFeatureMap); fm.(uint32)&uint32(thermostatdef.FeatureThermostatSuggestions) == 0 {
		t.Errorf("FeatureMap 0x%X does not serve TSUGGEST by default", fm)
	}
	srv := d.Thermostat()
	if got := srv.Get().MaxThermostatSuggestions; got != 5 {
		t.Errorf("MaxThermostatSuggestions %d, want 5", got)
	}
	handle := []byte("UserDef")
	participant, ok := srv.Underlying().(interface {
		AttachMatterTime(clock.MatterTime)
	})
	if !ok {
		t.Fatal("the demo thermostat takes no Matter time")
	}
	participant.AttachMatterTime(clock.MatterTimeOf(nil)) // the host clock, as a host attaches it
	if _, err := srv.Underlying().MatterInvoke(context.Background(), thermostatdef.CmdAddThermostatSuggestion,
		thermostatdef.AddThermostatSuggestionRequest{PresetHandle: handle, EffectiveTime: spec.Nullable[uint32]{Null: true}, ExpirationInMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	if st := srv.Get(); st.ActivePresetHandle == nil || !bytes.Equal(*st.ActivePresetHandle, handle) {
		t.Errorf("ActivePresetHandle %v, want %q", st.ActivePresetHandle, handle)
	}
}
