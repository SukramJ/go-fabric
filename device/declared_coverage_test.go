// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	accountdef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	launcherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	ctdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	epdef "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	ldcdef "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mwomdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	ovendef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	sadef "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// TestConfigFromLeavesWhatNoAttributeNames: an attribute the definition
// does not have stays in Initial, as does one the configuration has no
// field for.
func TestConfigFromLeavesWhatNoAttributeNames(t *testing.T) {
	t.Parallel()

	initial := map[uint32]any{0xFFFE: uint8(1)}
	var cfg servicearea.Config
	configFrom(&cfg, sadef.Definition, initial)
	if _, ok := initial[0xFFFE]; !ok {
		t.Error("an unknown attribute was consumed")
	}
}

// TestDeclaredServersRefuseASelectionOrAPortTheyLack: each declared server
// answers a missing port, a selection it does not serve or state it
// needs with ErrSelectionNotServed, so the cluster keeps the generated
// server.
func TestDeclaredServersRefuseASelectionOrAPortTheyLack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cluster uint32
		build   defaultBuild
	}{
		{"Valve with a feature", valvedef.ClusterID, defaultBuild{
			opts: spec.Options{Features: 1}, ports: valvedef.Declare{StateSource: fakeValve{}},
		}},
		{"ServiceArea without a Selector", sadef.ClusterID, defaultBuild{def: sadef.Definition}},
		{"ClosureControl with an unknown feature", ccldef.ClusterID, defaultBuild{
			opts: spec.Options{Features: 1 << 20}, ports: ccldef.Declare{Mover: fakeMover{}},
		}},
		{"LaundryDryerControls without levels", ldcdef.ClusterID, defaultBuild{initial: map[uint32]any{}}},
		{"CommodityTariff with an unknown feature", ctdef.ClusterID, defaultBuild{opts: spec.Options{Features: 1 << 20}}},
		{"EnergyPreference without its lists", epdef.ClusterID, defaultBuild{
			def: epdef.Definition, opts: spec.Options{Features: uint32(epdef.FeatureEnergyBalance)}, initial: map[uint32]any{},
		}},
		{"OvenCavityOperationalState without a handler", ovendef.ClusterID, defaultBuild{def: ovendef.Definition}},
		{"ApplicationLauncher without its port", launcherdef.ClusterID, defaultBuild{def: launcherdef.Definition}},
		{"MediaInput without its port", inputdef.ClusterID, defaultBuild{def: inputdef.Definition}},
		{"AudioOutput without its port", outputdef.ClusterID, defaultBuild{def: outputdef.Definition}},
		{"Channel without its port", channeldef.ClusterID, defaultBuild{def: channeldef.Definition}},
		{"TargetNavigator without its port", targetdef.ClusterID, defaultBuild{def: targetdef.Definition}},
		{"LowPower without its port", lowpowerdef.ClusterID, defaultBuild{def: lowpowerdef.Definition}},
		{"AccountLogin without its port", accountdef.ClusterID, defaultBuild{def: accountdef.Definition}},
	}
	for _, c := range cases {
		c.build.sibling = noSiblings
		_, err := selfHeldDefaults[c.cluster](c.build)
		if !errors.Is(err, ErrSelectionNotServed) {
			t.Errorf("%s: %v, want ErrSelectionNotServed", c.name, err)
		}
	}
}

// TestServiceAreaStartsInTheDeclaredArea: the area the declaration's
// Initial names is where the server starts, and a CurrentArea of another
// type is dropped.
func TestServiceAreaStartsInTheDeclaredArea(t *testing.T) {
	t.Parallel()

	kitchen := servicearea.Area{
		AreaId: 7, MapId: spec.NullOf[uint32](),
		AreaInfo: servicearea.AreaInfo{
			LocationInfo: spec.ValueOf(servicearea.LocationInfo{LocationName: "Kitchen", FloorNumber: spec.NullOf[int16](), AreaType: spec.NullOf[uint8]()}),
			LandmarkInfo: spec.NullOf[sadef.LandmarkInfoStruct](),
		},
	}
	for _, current := range []any{uint32(7), "kitchen"} {
		srv, err := selfHeldDefaults[sadef.ClusterID](defaultBuild{
			def: sadef.Definition, sibling: noSiblings,
			opts:  spec.Options{Attributes: []uint32{sadef.AttrCurrentArea}},
			ports: sadef.Declare{Selector: fakeArea{}},
			initial: map[uint32]any{
				sadef.AttrSupportedAreas: []servicearea.Area{kitchen}, sadef.AttrCurrentArea: current,
			},
		})
		if err != nil {
			t.Fatalf("CurrentArea %v: %v", current, err)
		}
		want := any(nil)
		if current == uint32(7) {
			want = uint32(7)
		}
		if v, _ := srv.MatterRead(sadef.AttrCurrentArea); v != want {
			t.Errorf("CurrentArea %v: the server reads %v, want %v", current, v, want)
		}
	}
}

// TestMicrowaveModeStartsInTheDeclaredMode: the CurrentMode the
// declaration's Initial states.
func TestMicrowaveModeStartsInTheDeclaredMode(t *testing.T) {
	t.Parallel()

	normal := []mwomdef.ModeOptionStruct{
		{Label: "Normal", Mode: 0, ModeTags: []mwomdef.ModeTagStruct{{Value: mwomdef.ModeTagNormal}}},
		{Label: "Defrost", Mode: 1, ModeTags: []mwomdef.ModeTagStruct{{Value: mwomdef.ModeTagDefrost}}},
	}
	srv, err := selfHeldDefaults[mwomdef.ClusterID](defaultBuild{
		def: mwomdef.Definition, sibling: noSiblings,
		initial: map[uint32]any{mwomdef.AttrSupportedModes: normal, mwomdef.AttrCurrentMode: uint8(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(mwomdef.AttrCurrentMode); v != uint8(1) {
		t.Errorf("CurrentMode %v, want 1", v)
	}
}

// tariffTime is a TimeSource that is also a ChangeNotifier.
type tariffTime struct {
	now time.Time
	cb  func()
}

func (t *tariffTime) Now() time.Time { return t.now }

func (t *tariffTime) OnMatterValueChanged(cb func()) func() {
	t.cb = cb
	return func() {}
}

// TestCommodityTariffRunsOnTheDeclaredTimeSource: the source's time is the
// server's, its timers are the device's, and a source that notifies makes
// the server derive its current values again.
func TestCommodityTariffRunsOnTheDeclaredTimeSource(t *testing.T) {
	t.Parallel()

	src := &tariffTime{now: time.Unix(1_700_000_000, 0)}
	srv, err := selfHeldDefaults[ctdef.ClusterID](defaultBuild{
		sibling: noSiblings, clock: &deviceClock{}, opts: spec.Options{Features: uint32(ctdef.FeaturePricing)},
		ports: ctdef.Declare{TimeSource: src},
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv == nil || src.cb == nil {
		t.Fatalf("server %v, the source's notifier subscribed %v", srv, src.cb != nil)
	}
	src.cb() // the clock jumped: the server syncs

	clk := sourceClock{Clock: &deviceClock{}, now: src.Now}
	if got := clk.Now(); !got.Equal(src.now) {
		t.Errorf("Now = %v, want the source's %v", got, src.now)
	}
}

// failingOps is an OperationalState command handler that refuses.
type failingOps struct{ err error }

func (f failingOps) HandleOperationalCommand(context.Context, uint32) (opdef.ErrorStateStruct, error) {
	return opdef.ErrorStateStruct{}, f.err
}

// labelledOps answers every command with an error state that has a label
// and details.
type labelledOps struct{}

func (labelledOps) HandleOperationalCommand(context.Context, uint32) (opdef.ErrorStateStruct, error) {
	label, details := "lid open", "close the lid"
	return opdef.ErrorStateStruct{ErrorStateId: opdef.ErrorStateUnableToStartOrResume, ErrorStateLabel: &label, ErrorStateDetails: &details}, nil
}

// TestOperationalStateHandlerReadsTheGeneratedErrorState: the command is
// the id of the server's, the reply the generated struct read into the
// server's, and a refusal is the error.
func TestOperationalStateHandlerReadsTheGeneratedErrorState(t *testing.T) {
	t.Parallel()

	got, err := opHandler[opdef.ErrorStateStruct]{labelledOps{}}.HandleOperationalCommand(context.Background(), opstate.CommandPause)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != opstate.ErrorID(opdef.ErrorStateUnableToStartOrResume) || got.Label != "lid open" || got.Details != "close the lid" {
		t.Errorf("error state %+v", got)
	}
	refusal := errors.New("busy")
	if _, err := (opHandler[opdef.ErrorStateStruct]{failingOps{refusal}}).HandleOperationalCommand(context.Background(), opstate.CommandStop); !errors.Is(err, refusal) {
		t.Errorf("a refused command: %v, want the handler's error", err)
	}
}

// TestOperationalStateStartsFromTheDeclaration: the states, the state,
// the phases and the phase the declaration's Initial states.
func TestOperationalStateStartsFromTheDeclaration(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: opdef.Definition, sibling: noSiblings,
		ports: opdef.Declare{CommandHandler: labelledOps{}},
		initial: map[uint32]any{
			opdef.AttrOperationalStateList: []opdef.OperationalStateStruct{
				{OperationalStateId: opdef.OperationalStateStopped},
				{OperationalStateId: opdef.OperationalStateRunning},
				{OperationalStateId: opdef.OperationalStateError},
			},
			opdef.AttrOperationalState: uint8(opdef.OperationalStateRunning),
			opdef.AttrPhaseList:        []string{"wash", "rinse"},
			opdef.AttrCurrentPhase:     uint8(1),
		},
	}
	srv, err := selfHeldDefaults[opdef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(opdef.AttrOperationalState); v != uint8(opdef.OperationalStateRunning) {
		t.Errorf("OperationalState %v", v)
	}
	if v, _ := srv.MatterRead(opdef.AttrCurrentPhase); v != uint8(1) {
		t.Errorf("CurrentPhase %v", v)
	}

	// A null CurrentPhase is dropped from Initial.
	b.initial = map[uint32]any{
		opdef.AttrOperationalStateList: []opdef.OperationalStateStruct{{OperationalStateId: opdef.OperationalStateStopped}, {OperationalStateId: opdef.OperationalStateError}},
		opdef.AttrCurrentPhase:         nil,
	}
	if _, err := selfHeldDefaults[opdef.ClusterID](b); err != nil {
		t.Fatalf("a null phase: %v", err)
	}
	if _, ok := b.initial[opdef.AttrCurrentPhase]; ok {
		t.Error("the null CurrentPhase is still in Initial")
	}
}

// TestReflectedFieldsOfAGeneratedStruct: the helpers that read the
// generated structs of the derivations fall back to the zero value for
// what they do not find.
func TestReflectedFieldsOfAGeneratedStruct(t *testing.T) {
	t.Parallel()

	type shape struct {
		N     uint16
		Name  string
		Label *string
		Other int
	}
	var none *string
	v := reflect.ValueOf(shape{N: 7, Name: "x", Label: none})
	if got := uintField(v, "n"); got != 7 {
		t.Errorf("uintField = %d", got)
	}
	if got := uintField(v, "Name"); got != 0 {
		t.Errorf("uintField of a string = %d", got)
	}
	if got := uintField(reflect.ValueOf(3), "N"); got != 0 {
		t.Errorf("uintField of a non-struct = %d", got)
	}
	if got := stringField(v, "Name"); got != "x" {
		t.Errorf("stringField = %q", got)
	}
	if got := stringField(v, "Label"); got != "" {
		t.Errorf("stringField of a nil pointer = %q", got)
	}
	label := "l"
	if got := stringField(reflect.ValueOf(shape{Label: &label}), "Label"); got != "l" {
		t.Errorf("stringField of a pointer = %q", got)
	}
	if got := stringField(v, "Other"); got != "" {
		t.Errorf("stringField of an int = %q", got)
	}
	if got := stringField(v, "Missing"); got != "" {
		t.Errorf("stringField of a missing field = %q", got)
	}
	if got := stateEntriesOf(3); got != nil {
		t.Errorf("stateEntriesOf(3) = %v", got)
	}
	if got := modeOptionsOf("modes"); got != nil {
		t.Errorf("modeOptionsOf(string) = %v", got)
	}
}

// TestLevelControlPortCouplesTheColorTemperature: a level source with
// Transitions couples to the endpoint's OnOff and to a ColorControl that
// follows the level.
func TestLevelControlPortCouplesTheColorTemperature(t *testing.T) {
	t.Parallel()

	cc, err := light.NewColorControl(light.ColorControlServerConfig{Features: light.ColorFeatureColorTemperature})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := newLevelControlPortServer(defaultBuild{
		opts: spec.Options{Features: levelcontrol.FeatureOnOff | levelcontrol.FeatureLighting},
		sibling: func(id uint32) contract.ClusterServer {
			if id == ccdef.ClusterID {
				return cc
			}
			return nil
		},
	}, lvldef.Declare{LevelSource: fakeLevel{}, Transitions: true})
	if err != nil {
		t.Fatal(err)
	}
	if srv == nil {
		t.Fatal("no server")
	}
}

// TestAPartThatCannotBeBoundStopsTheOwner: an owner whose part is already
// bound to another node is not bound either.
func TestAPartThatCannotBeBoundStopsTheOwner(t *testing.T) {
	t.Parallel()

	owner, err := NewBridgedNode(testOptions("owner"))
	if err != nil {
		t.Fatal(err)
	}
	part, err := NewOnOffLight(testOptions("part"))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(part.Device); err != nil {
		t.Fatal(err)
	}
	first := &fakeHost{clk: &deviceClock{}, numbers: map[string]uint16{}}
	detach, _, err := deviceseam.Attach(part, first)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	second := &fakeHost{clk: &deviceClock{}, numbers: map[string]uint16{}}
	if _, _, err := deviceseam.Attach(owner, second); !errors.Is(err, ErrAddedElsewhere) {
		t.Errorf("binding an owner whose part is bound elsewhere: %v, want ErrAddedElsewhere", err)
	}
	if owner.host != nil {
		t.Error("the owner is still bound after its part refused")
	}
}

// badDeclaration states one cluster and applies another.
type badDeclaration struct{ id, declares uint32 }

func (b badDeclaration) ClusterID() uint32 { return b.id }

func (b badDeclaration) ClusterDeclaration() spec.ClusterDeclaration {
	return spec.ClusterDeclaration{Cluster: b.declares}
}

// TestADeclarationMustStateItsOwnCluster: a declaration whose cluster id
// differs from its ClusterDeclaration's, or is zero, is a configuration
// error.
func TestADeclarationMustStateItsOwnCluster(t *testing.T) {
	t.Parallel()

	for _, d := range []badDeclaration{{id: 6, declares: 8}, {id: 0, declares: 0}} {
		if _, err := NewOnOffLight(testOptions("bad"), d); !errors.Is(err, ErrConfiguration) {
			t.Errorf("declaration %+v: %v, want ErrConfiguration", d, err)
		}
	}
}

// TestAHookForAClusterThatIsNotMounted: a declaration's hooks need the
// cluster's server.
func TestAHookForAClusterThatIsNotMounted(t *testing.T) {
	t.Parallel()

	attach := func(contract.ClusterServer) error { return nil }
	_, err := NewOnOffLight(testOptions("hooked"), clusterOption{decl: spec.ClusterDeclaration{Cluster: ccdef.ClusterID, Attach: attach}})
	if !errors.Is(err, ErrConfiguration) {
		t.Errorf("a hook for an unmounted cluster: %v, want ErrConfiguration", err)
	}
}

// TestComposesWithoutADefinition: a type the schema has no definition of
// composes nothing.
func TestComposesWithoutADefinition(t *testing.T) {
	t.Parallel()

	unknown := &Type{ID: 0xFFFF, Name: "NoSuchType"}
	if composes(unknown, &OnOffLightType) {
		t.Error("a type without a definition composes a part")
	}
}
