// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/contract"
)

// testCluster exercises every resolution rule: a mandatory, an optional, a
// feature-gated and a disallowed attribute; a mandatory and an optional
// request, a status-only one and their responses; events of each
// conformance; features with a choice group, a dependent and a mandatory
// one.
var testCluster = &spec.Cluster{
	ID: 0xFFF1, Name: "Test", Revision: 3,
	Features: []spec.Feature{
		{Name: "AA", Title: "Alpha", Bit: 0, Conformance: spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "a", Num: 1, OrMore: true}, Args: []spec.Conformance{op(spec.ConfOptional)}}},
		{Name: "BB", Title: "Beta", Bit: 1, Conformance: spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "a", Num: 1, OrMore: true}, Args: []spec.Conformance{op(spec.ConfOptional)}}},
		{Name: "CC", Title: "Gamma", Bit: 2, Conformance: op(spec.ConfOptionalIf, name("AA"))},
		{Name: "DD", Title: "Delta", Bit: 3, Conformance: op(spec.ConfDisallowed)},
		{Name: "EE", Title: "Epsilon", Bit: 4, Conformance: spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "b", Num: 1}, Args: []spec.Conformance{op(spec.ConfOptional)}}},
		{Name: "FF", Title: "Zeta", Bit: 5, Conformance: spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "b", Num: 1}, Args: []spec.Conformance{op(spec.ConfOptional)}}},
	},
	Attributes: []spec.Attribute{
		{ID: 0, Name: "Mandatory", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory), Access: spec.Access{RW: "R", Read: spec.PrivilegeView}},
		{ID: 1, Name: "Optional", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfOptional), Access: spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeManage}},
		{ID: 2, Name: "Gated", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: name("CC"), Quality: spec.Quality{Fixed: true}},
		{ID: 3, Name: "Disallowed", Conformance: op(spec.ConfDisallowed)},
	},
	Commands: []spec.Command{
		{ID: 0, Name: "Go", Direction: spec.Request, Response: "GoResponse", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeAdminister, Timed: true}},
		{ID: 1, Name: "GoResponse", Direction: spec.Response, Conformance: op(spec.ConfMandatory)},
		{ID: 2, Name: "Maybe", Direction: spec.Request, Response: "GoResponse", Conformance: op(spec.ConfOptional)},
		{ID: 3, Name: "Stop", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfOptional)},
		{ID: 4, Name: "Never", Direction: spec.Request, Conformance: op(spec.ConfDisallowed)},
		{ID: 5, Name: "Unanswered", Direction: spec.Request, Response: "Missing", Conformance: op(spec.ConfOptional)},
	},
	Events: []spec.Event{
		{ID: 0, Name: "Always", Priority: spec.PriorityCritical, Conformance: op(spec.ConfMandatory)},
		{ID: 1, Name: "Sometimes", Priority: spec.PriorityDebug, Conformance: op(spec.ConfOptional)},
		{ID: 2, Name: "Gone", Conformance: op(spec.ConfDeprecated)},
	},
}

func TestCheckFeatures(t *testing.T) {
	t.Parallel()
	const aa, bb, cc, dd, ee, ff = 1, 2, 4, 8, 16, 32
	cases := []struct {
		features uint32
		want     error
	}{
		{aa | ee, nil},
		{aa | cc | ee, nil},
		{aa | bb | cc | ee, nil},                 // "a+" allows more than one
		{1 << 6, spec.ErrUnknownFeature},         // an undefined bit
		{ee, spec.ErrFeatureSelection},           // group a needs one
		{aa, spec.ErrFeatureSelection},           // group b needs one
		{aa | ee | ff, spec.ErrFeatureSelection}, // group b allows one
		{aa | dd | ee, spec.ErrFeatureSelection}, // DD is "X"
		{bb | cc | ee, spec.ErrFeatureSelection}, // CC is "[AA]"
	}
	for _, tc := range cases {
		err := spec.CheckFeatures(testCluster, tc.features)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("0b%06b: %v, want %v", tc.features, err, tc.want)
		}
	}
	mandatory := &spec.Cluster{Name: "M", Features: []spec.Feature{{Name: "M", Title: "Must", Conformance: op(spec.ConfMandatory)}}}
	if err := spec.CheckFeatures(mandatory, 0); !errors.Is(err, spec.ErrFeatureSelection) {
		t.Errorf("mandatory feature left out: %v", err)
	}
}

func TestNewResolvesElements(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(testCluster, spec.Options{
		Features:   0b010101, // AA, CC, EE
		Attributes: []uint32{1},
		Commands:   []uint32{2, 3, 5},
		Events:     []uint32{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := inst.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2}) {
		t.Errorf("attributes %v", got)
	}
	if got := inst.MatterReportable(); !slices.Equal(got, []uint32{0, 1}) {
		t.Errorf("reportable %v (Gated is fixed)", got)
	}
	if got := inst.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0, 2, 3, 5}) {
		t.Errorf("accepted %v", got)
	}
	if got := inst.MatterGeneratedCommands(); !slices.Equal(got, []uint32{1}) {
		t.Errorf("generated %v", got)
	}
	if got := inst.MatterEvents(); !slices.Equal(got, []uint32{0, 1}) {
		t.Errorf("events %v", got)
	}
	if !inst.Serves(2) || inst.Serves(3) || !inst.Accepts(0) || inst.Accepts(4) || !inst.Emits(1) || inst.Emits(2) {
		t.Error("membership")
	}
	if inst.MatterClusterID() != 0xFFF1 || inst.Revision() != 3 || inst.FeatureMap() != 0b010101 || inst.Definition() != testCluster {
		t.Error("identity")
	}
	if !inst.HasFeature("AA") || !inst.HasFeature("Gamma") || inst.HasFeature("BB") || inst.HasFeature("nope") {
		t.Error("HasFeature")
	}
	if v, ok := inst.ReadGlobal(spec.AttrFeatureMap); !ok || v != uint32(0b010101) {
		t.Errorf("FeatureMap %v", v)
	}
	if v, ok := inst.ReadGlobal(spec.AttrClusterRevision); !ok || v != uint16(3) {
		t.Errorf("ClusterRevision %v", v)
	}
	if _, ok := inst.ReadGlobal(spec.AttrAttributeList); ok {
		t.Error("AttributeList is the dispatcher's")
	}
	if inst.MinReadPrivilege(0) != 1 || inst.MinReadPrivilege(99) != 1 {
		t.Error("read privilege")
	}
	if inst.MinWritePrivilege(1) != 4 || inst.MinWritePrivilege(0) != 3 || inst.MinWritePrivilege(99) != 3 {
		t.Error("write privilege")
	}
	if inst.MinInvokePrivilege(0) != 5 || inst.MinInvokePrivilege(2) != 3 || inst.MinInvokePrivilege(99) != 3 {
		t.Error("invoke privilege")
	}
	if !inst.IsTimed(0) || inst.IsTimed(2) || inst.IsTimed(99) {
		t.Error("timed")
	}
	if inst.EventPriority(0) != contract.EventPriorityCritical || inst.EventPriority(1) != contract.EventPriorityDebug ||
		inst.EventPriority(99) != contract.EventPriorityInfo {
		t.Error("event priority")
	}
	if inst.Context().Supported("BB") || !inst.Context().Supported("AA") {
		t.Error("context")
	}
}

func TestNewRejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		def  *spec.Cluster
		opts spec.Options
		want error
	}{
		{"nil definition", nil, spec.Options{}, spec.ErrNoDefinition},
		{"feature selection", testCluster, spec.Options{Features: 0b010000}, spec.ErrFeatureSelection},
		{"unknown attribute", testCluster, spec.Options{Features: 0b010001, Attributes: []uint32{9}}, spec.ErrUnknownElement},
		{"disallowed attribute", testCluster, spec.Options{Features: 0b010001, Attributes: []uint32{3}}, spec.ErrDisallowed},
		{"unknown command", testCluster, spec.Options{Features: 0b010001, Commands: []uint32{9}}, spec.ErrUnknownElement},
		{"response as accepted", testCluster, spec.Options{Features: 0b010001, Commands: []uint32{1}}, spec.ErrUnknownElement},
		{"disallowed command", testCluster, spec.Options{Features: 0b010001, Commands: []uint32{4}}, spec.ErrDisallowed},
		{"unknown event", testCluster, spec.Options{Features: 0b010001, Events: []uint32{9}}, spec.ErrUnknownElement},
		{"disallowed event", testCluster, spec.Options{Features: 0b010001, Events: []uint32{2}}, spec.ErrDisallowed},
	}
	for _, tc := range cases {
		if _, err := spec.New(tc.def, tc.opts); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestGeneratedPump resolves the generated PumpConfigurationAndControl
// definition: "O.a+" over the five control features, "<feature>, [AUTO]"
// limit pairs and the feature-gated enum values.
func TestGeneratedPump(t *testing.T) {
	t.Parallel()
	if _, err := spec.New(pumpdef.Definition, spec.Options{Features: uint32(pumpdef.FeatureAutomatic)}); !errors.Is(err, spec.ErrFeatureSelection) {
		t.Errorf("no control feature: %v", err)
	}
	inst, err := spec.New(pumpdef.Definition, spec.Options{
		Features:   uint32(pumpdef.FeatureConstantFlow | pumpdef.FeatureAutomatic),
		Attributes: []uint32{pumpdef.AttrMinConstSpeed, pumpdef.AttrMaxConstSpeed},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{pumpdef.AttrMinConstFlow, pumpdef.AttrMinConstSpeed} {
		if !inst.Serves(id) {
			t.Errorf("0x%04X not served", id)
		}
	}
	if inst.Serves(pumpdef.AttrMinConstPressure) || inst.Serves(pumpdef.AttrAlarmMask) {
		t.Error("an undeclared or deprecated attribute is served")
	}
	if inst.EnumSupported(pumpdef.OperationModeEnumDef, uint64(pumpdef.OperationModeMinimum)) {
		t.Error("Minimum needs SPD")
	}
	if !inst.EnumSupported(pumpdef.ControlModeEnumDef, uint64(pumpdef.ControlModeAutomatic)) ||
		inst.EnumSupported(pumpdef.ControlModeEnumDef, 4) {
		t.Error("ControlMode membership")
	}
	if inst.EventPriority(pumpdef.EventDryRunning) != contract.EventPriorityCritical {
		t.Error("DryRunning is critical")
	}
}

// TestDefinitionLookups covers the definition's finders.
func TestDefinitionLookups(t *testing.T) {
	t.Parallel()
	d := rvcrunmode.Definition
	if d.Attribute(rvcrunmode.AttrCurrentMode) == nil || d.Attribute(99) != nil {
		t.Error("Attribute")
	}
	if d.AttributeByName("currentMode") == nil || d.AttributeByName("nope") != nil {
		t.Error("AttributeByName")
	}
	if d.Command(rvcrunmode.CmdChangeToMode, spec.Response) != nil || d.CommandByName("ChangeToModeResponse") == nil ||
		d.CommandByName("nope") != nil {
		t.Error("Command")
	}
	if d.Event(0) != nil || d.Feature("DIRECTMODECH") == nil || d.Feature("DirectModeChange") == nil || d.Feature("x") != nil {
		t.Error("Event / Feature")
	}
	if d.FeatureMask() != 1|1<<20 {
		t.Errorf("FeatureMask 0x%X", d.FeatureMask())
	}
	bm := spec.Bitmap{Members: []spec.BitmapMember{{Bit: 0, Width: 1}, {Bit: 2, Width: 3}}}
	if bm.Defined() != 0b11101 {
		t.Errorf("Defined 0b%b", bm.Defined())
	}
	a := spec.Access{RW: "R[W]"}
	if !a.Readable() || !a.Writable() || a.ReadPrivilege() != spec.PrivilegeView || a.WritePrivilege() != spec.PrivilegeOperate {
		t.Error("Access defaults")
	}
	if (spec.Access{RW: "W"}).Readable() || (spec.Access{RW: "R"}).Writable() {
		t.Error("Access rw")
	}
	if spec.Lookup(rvcrunmode.ClusterID) != rvcrunmode.Definition || spec.Lookup(0xFFFFFF) != nil {
		t.Error("registry")
	}
}
