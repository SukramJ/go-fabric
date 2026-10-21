// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"reflect"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readFields encodes req as a command's fields and runs commandFieldsReader
// on them.
func readFields(t *testing.T, cluster, command uint32, req spec.Encodable) any {
	t.Helper()
	enc := tlv.NewEncoder()
	req.EncodeTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	opener, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	got, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: cluster, Command: command}, dec, opener)
	if err != nil {
		t.Fatalf("cluster 0x%04X command 0x%02X: %v", cluster, command, err)
	}
	return got
}

// TestCommandFieldsReader_DefinitionFirst pins the decoder order of gate 1
// (G1-2): every request of the application clusters that had a
// hand-written decoder ahead of their generated definition — LevelControl,
// the four ColorControl moves, FanControl Step, the four mode clusters'
// ChangeToMode — and DoorLock decodes through the generated definition,
// into the generated request every self-held server takes as its primary
// form. Only the commissioning, group and scene readers still run first,
// into their cluster/core request structs
// (BD-Matter-Commissioning-HandDecoders).
func TestCommandFieldsReader_DefinitionFirst(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cluster, command uint32
		req              spec.Encodable
	}{
		{lvldef.ClusterID, lvldef.CmdMoveToLevel, lvldef.MoveToLevelRequest{Level: 100, TransitionTime: spec.ValueOf(uint16(5))}},
		{lvldef.ClusterID, lvldef.CmdMove, lvldef.MoveRequest{Rate: spec.ValueOf(uint8(10))}},
		{lvldef.ClusterID, lvldef.CmdStep, lvldef.StepRequest{StepSize: 10, TransitionTime: spec.NullOf[uint16]()}},
		{lvldef.ClusterID, lvldef.CmdStop, lvldef.StopRequest{OptionsMask: 1, OptionsOverride: 1}},
		{lvldef.ClusterID, lvldef.CmdMoveToLevelWithOnOff, lvldef.MoveToLevelWithOnOffRequest{Level: 7, TransitionTime: spec.NullOf[uint16]()}},
		{lvldef.ClusterID, lvldef.CmdMoveWithOnOff, lvldef.MoveWithOnOffRequest{Rate: spec.NullOf[uint8]()}},
		{lvldef.ClusterID, lvldef.CmdStepWithOnOff, lvldef.StepWithOnOffRequest{StepSize: 3, TransitionTime: spec.ValueOf(uint16(0))}},
		{lvldef.ClusterID, lvldef.CmdStopWithOnOff, lvldef.StopWithOnOffRequest{}},
		{ccdef.ClusterID, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 100, TransitionTime: 10}},
		{ccdef.ClusterID, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 100}},
		{ccdef.ClusterID, ccdef.CmdMoveToHueAndSaturation, ccdef.MoveToHueAndSaturationRequest{Hue: 1, Saturation: 2, TransitionTime: 3}},
		{ccdef.ClusterID, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370}},
		{fandef.ClusterID, fandef.CmdStep, fandef.StepRequest{Direction: 1}},
		{laundrywashermode.ClusterID, laundrywashermode.CmdChangeToMode, laundrywashermode.ChangeToModeRequest{NewMode: 1}},
		{rvcrunmode.ClusterID, rvcrunmode.CmdChangeToMode, rvcrunmode.ChangeToModeRequest{NewMode: 2}},
		{rvccleanmode.ClusterID, rvccleanmode.CmdChangeToMode, rvccleanmode.ChangeToModeRequest{NewMode: 3}},
		{dishwashermode.ClusterID, dishwashermode.CmdChangeToMode, dishwashermode.ChangeToModeRequest{NewMode: 4}},
		{lockdef.ClusterID, lockdef.CmdLockDoor, lockdef.LockDoorRequest{}},
	}
	for _, tc := range cases {
		got := readFields(t, tc.cluster, tc.command, tc.req)
		if !reflect.DeepEqual(got, any(tc.req)) {
			t.Errorf("cluster 0x%04X command 0x%02X: decoded %T %+v, want the generated %T %+v", tc.cluster, tc.command, got, got, tc.req, tc.req)
		}
	}

	// The commissioning readers keep running first (by_design).
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 60) // ExpiryLengthSeconds
	enc.PutUint(tlv.ContextTag(1), 1)  // Breadcrumb
	_ = enc.EndContainer()
	raw, _ := enc.Bytes()
	dec := tlv.NewDecoder(raw)
	opener, _ := dec.Next()
	got, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: 0x0030, Command: 0x00}, dec, opener)
	if _, ok := got.(mattercore.ArmFailSafeRequest); err != nil || !ok {
		t.Errorf("ArmFailSafe: %T, %v; want the cluster/core request of the hand-written reader", got, err)
	}
}
