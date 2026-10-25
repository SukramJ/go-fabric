// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bscdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/contract"
)

func noSiblings(uint32) contract.ClusterServer { return nil }

// TestSelfHeldFactoriesRefuseASelectionTheyDoNotServe: every factory
// answers a selection its server does not serve with ErrSelectionNotServed,
// so the cluster keeps the generated server.
func TestSelfHeldFactoriesRefuseASelectionTheyDoNotServe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cluster uint32
		opts    spec.Options
	}{
		{"LevelControl FQ", 0x0008, spec.Options{Features: uint32(lvldef.FeatureFrequency)}},
		{"ColorControl unknown bit", 0x0300, spec.Options{Features: 1 << 7}},
		{"Thermostat without HEAT or COOL", 0x0201, spec.Options{}},
		{"DoorLock unserved bit", 0x0101, spec.Options{Features: 1 << 20}},
		{"WindowCovering AbsolutePosition", 0x0102, spec.Options{Features: 0x1 | 0x4 | 0x8}},
		{"HepaFilterMonitoring ResetCondition", 0x0071, spec.Options{Commands: []uint32{hepadef.CmdResetCondition}}},
	}
	for _, tc := range cases {
		_, err := selfHeldDefaults[tc.cluster](defaultBuild{opts: tc.opts, sibling: noSiblings})
		if !errors.Is(err, ErrSelectionNotServed) {
			t.Errorf("%s: %v, want ErrSelectionNotServed", tc.name, err)
		}
	}
}

// TestSelfHeldFactoriesMapTheSelection: the optional attributes a selection
// mandates reach the server's configuration.
func TestSelfHeldFactoriesMapTheSelection(t *testing.T) {
	t.Parallel()
	attrs := func(srv contract.ClusterServer) []uint32 {
		return srv.(contract.ClusterAttributeLister).MatterAttributes()
	}
	bsc, err := selfHeldDefaults[0x0080](defaultBuild{
		opts:    spec.Options{Features: uint32(bscdef.FeatureSensitivityLevel | bscdef.FeatureVisual), Attributes: []uint32{bscdef.AttrDefaultSensitivityLevel, bscdef.AttrAlarmsEnabled, bscdef.AttrSensorFault}},
		sibling: noSiblings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing := absent([]uint32{bscdef.AttrDefaultSensitivityLevel, bscdef.AttrAlarmsEnabled, bscdef.AttrSensorFault}, attrs(bsc)); len(missing) > 0 {
		t.Errorf("BooleanStateConfiguration lacks %v", missing)
	}
	hepa, err := selfHeldDefaults[0x0071](defaultBuild{
		opts:    spec.Options{Attributes: []uint32{hepadef.AttrInPlaceIndicator, hepadef.AttrLastChangedTime}},
		sibling: noSiblings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing := absent([]uint32{hepadef.AttrInPlaceIndicator, hepadef.AttrLastChangedTime}, attrs(hepa)); len(missing) > 0 {
		t.Errorf("HepaFilterMonitoring lacks %v", missing)
	}
	tui, err := selfHeldDefaults[0x0204](defaultBuild{
		opts:    spec.Options{Attributes: []uint32{tuidef.AttrScheduleProgrammingVisibility}},
		sibling: noSiblings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := tui.MatterRead(tuidef.AttrScheduleProgrammingVisibility); !ok || v == nil {
		t.Errorf("ScheduleProgrammingVisibility %v, %t", v, ok)
	}
}

// bareServer is a cluster server with nothing but the contract: no
// setter, no notifier, no listers.
type bareServer struct {
	id   uint32
	read map[uint32]any
}

func (b bareServer) MatterClusterID() uint32 { return b.id }
func (b bareServer) MatterRead(attrID uint32) (any, bool) {
	v, ok := b.read[attrID]
	return v, ok
}
func (bareServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (bareServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (bareServer) MatterReportable() []uint32                             { return nil }

// listingServer adds the attribute, command and event lists.
type listingServer struct {
	bareServer
	attrs, cmds, events []uint32
}

func (l listingServer) MatterAttributes() []uint32       { return l.attrs }
func (l listingServer) MatterAcceptedCommands() []uint32 { return l.cmds }
func (listingServer) MatterGeneratedCommands() []uint32  { return nil }
func (l listingServer) MatterEvents() []uint32           { return l.events }

// TestCheckSelection: a FeatureMap, attribute, command or event the
// selection mandates and the server does not advertise is not served.
func TestCheckSelection(t *testing.T) {
	t.Parallel()
	srv := listingServer{bareServer: bareServer{read: map[uint32]any{0xFFFC: uint32(1)}}, attrs: []uint32{1}, cmds: []uint32{2}, events: []uint32{3}}
	if err := checkSelection("X", srv, spec.Options{Features: 1, Attributes: []uint32{1}, Commands: []uint32{2}, Events: []uint32{3}}); err != nil {
		t.Errorf("a served selection: %v", err)
	}
	for name, opts := range map[string]spec.Options{
		"features":  {Features: 3},
		"attribute": {Features: 1, Attributes: []uint32{9}},
		"command":   {Features: 1, Commands: []uint32{9}},
		"event":     {Features: 1, Events: []uint32{9}},
	} {
		if err := checkSelection("X", srv, opts); !errors.Is(err, ErrSelectionNotServed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if missing := absent([]uint32{1, 2}, []uint32{2}); len(missing) != 1 || missing[0] != 1 {
		t.Errorf("absent %v", missing)
	}
}

// refusingSetter refuses every value.
type refusingSetter struct{ bareServer }

func (refusingSetter) SetAttributes(map[uint32]any) error { return spec.ErrInvalidValue }

// TestApplyInitial: no values is no call; a server without a setter, and
// a setter's refusal, are errors that name the cluster.
func TestApplyInitial(t *testing.T) {
	t.Parallel()
	if err := applyInitial("X", bareServer{}, nil); err != nil {
		t.Errorf("no values: %v", err)
	}
	if err := applyInitial("X", bareServer{}, map[uint32]any{1: uint8(1)}); !errors.Is(err, spec.ErrNotSettable) {
		t.Errorf("no setter: %v", err)
	}
	if err := applyInitial("X", refusingSetter{}, map[uint32]any{1: uint8(1)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a refusal: %v", err)
	}
}

// TestOnOffPortOverABareServer: the coupling reads OnOff through the
// attribute, refuses to set a server without a setter and follows nothing
// on a server without a notifier.
func TestOnOffPortOverABareServer(t *testing.T) {
	t.Parallel()
	p := onOffOf(bareServer{read: map[uint32]any{0x0000: true}})
	if !p.OnOff() {
		t.Error("OnOff not read through the attribute")
	}
	if err := p.SetOnOff(context.Background(), false); !errors.Is(err, spec.ErrNotSettable) {
		t.Errorf("SetOnOff: %v", err)
	}
	called := false
	p.OnAttributeChange(func(contract.Change) { called = true })()
	if called {
		t.Error("a bare server notified")
	}
}
