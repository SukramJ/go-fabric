// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"

	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// rampCall is one call a declared NativeRamp (or one of its optional
// interfaces) saw.
type rampCall struct {
	method    string
	level     uint8
	d         time.Duration
	withOnOff bool
}

// fakeRamp is a device that ramps by itself: it arrives at once.
type fakeRamp struct {
	mu    sync.Mutex
	level uint8
	calls []rampCall
}

func (r *fakeRamp) record(c rampCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
}

func (r *fakeRamp) seen() []rampCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func (r *fakeRamp) Level() (uint8, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.level, true
}

func (r *fakeRamp) Ramp(_ context.Context, level uint8, d time.Duration, withOnOff bool) error {
	r.mu.Lock()
	r.level = level
	r.mu.Unlock()
	r.record(rampCall{method: "Ramp", level: level, d: d, withOnOff: withOnOff})
	return nil
}

func (r *fakeRamp) Stop(context.Context) error {
	r.record(rampCall{method: "Stop"})
	return nil
}

// moverRamp takes Move itself (levelcontroldef.NativeMover).
type moverRamp struct{ *fakeRamp }

func (r moverRamp) MoveLevel(_ context.Context, _ levelcontroldef.MoveRequest, withOnOff bool) error {
	r.record(rampCall{method: "MoveLevel", withOnOff: withOnOff})
	return nil
}

// switcherRamp writes On/Off and level together
// (levelcontroldef.NativeSwitcher).
type switcherRamp struct{ *fakeRamp }

func (r switcherRamp) SwitchOn(_ context.Context, level uint8, d time.Duration) error {
	r.mu.Lock()
	r.level = level
	r.mu.Unlock()
	r.record(rampCall{method: "SwitchOn", level: level, d: d})
	return nil
}

func (r switcherRamp) SwitchOff(_ context.Context, d time.Duration) error {
	r.record(rampCall{method: "SwitchOff", d: d})
	return nil
}

// persistRamp keeps Options and OnLevel (levelcontroldef.NativePersister).
type persistRamp struct {
	*fakeRamp
	options chan levelcontroldef.OptionsBitmap
}

func (r persistRamp) PersistLevel(_ context.Context, options levelcontroldef.OptionsBitmap, _ *uint8) error {
	r.options <- options
	return nil
}

var (
	_ levelcontroldef.NativeRamp      = (*fakeRamp)(nil)
	_ levelcontroldef.NativeMover     = moverRamp{}
	_ levelcontroldef.NativeSwitcher  = switcherRamp{}
	_ levelcontroldef.NativePersister = persistRamp{}
)

func levelCmd(ep uint16, cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: ep, Cluster: levelcontroldef.ClusterID, Command: cmd}
}

// TestDeclaredRampDimmer is #228 for a light: a DimmableLight declared
// with Ramp ramps the device, and MoveToLevelWithOnOff switches the
// sibling OnOff server on — the host implements no On/Off port.
func TestDeclaredRampDimmer(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := quietSite(t)
	ramp := &fakeRamp{level: 1}
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("ramp-light"), Name: "Hall dimmer"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}},
		levelcontroldef.Declare{Ramp: ramp})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, _ := light.EndpointNumber()

	if min := light.LevelControl().Get().MinLevel; min != levelcontroldef.LightingLevelMin {
		t.Errorf("MinLevel %d under Lighting, want %d", min, levelcontroldef.LightingLevelMin)
	}
	if _, err := fabrictest.Invoke(ctx, pair, levelCmd(ep, levelcontroldef.CmdMoveToLevelWithOnOff),
		levelcontroldef.MoveToLevelWithOnOffRequest{Level: 200}); err != nil {
		t.Fatal(err)
	}
	if got := ramp.seen(); !slices.ContainsFunc(got, func(c rampCall) bool { return c.method == "Ramp" && c.level == 200 && c.withOnOff }) {
		t.Errorf("the ramp saw %+v, want a Ramp to 200 with On/Off", got)
	}
	if !light.OnOff().Get().OnOff {
		t.Error("MoveToLevelWithOnOff left the sibling OnOff off")
	}
	if got := light.LevelControl().Get().CurrentLevel; got == nil || *got != 200 {
		t.Errorf("CurrentLevel %v, want 200 from the ramp", got)
	}
}

// TestDeclaredRampVolume is #228 for a non-Lighting level, a speaker's
// volume: MinLevel 0, Move goes to the NativeMover, and the "with On/Off"
// pair to the NativeSwitcher instead of the ramp and the OnOff server.
func TestDeclaredRampVolume(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := quietSite(t)
	base := &fakeRamp{level: 0}
	ramp := &moverSwitcher{base}
	speaker, err := device.NewSpeaker(device.Options{StableKey: endpoint.StringKey("ramp-speaker"), Name: "Kitchen speaker"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}},
		levelcontroldef.Declare{Ramp: ramp})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{speaker}}})
	ep, _ := speaker.EndpointNumber()

	if min := speaker.LevelControl().Get().MinLevel; min != levelcontroldef.LevelMin {
		t.Errorf("MinLevel %d without Lighting, want %d", min, levelcontroldef.LevelMin)
	}
	if _, err := fabrictest.Invoke(ctx, pair, levelCmd(ep, levelcontroldef.CmdMove),
		levelcontroldef.MoveRequest{MoveMode: levelcontroldef.MoveModeUp, Rate: spec.ValueOf(uint8(10)), OptionsMask: 1, OptionsOverride: 1}); err != nil {
		t.Fatal(err)
	}
	if got := base.seen(); len(got) != 1 || got[0].method != "MoveLevel" || got[0].withOnOff {
		t.Errorf("Move: the ramp saw %+v, want MoveLevel alone", got)
	}
	if _, err := fabrictest.Invoke(ctx, pair, levelCmd(ep, levelcontroldef.CmdMoveToLevelWithOnOff),
		levelcontroldef.MoveToLevelWithOnOffRequest{Level: 120}); err != nil {
		t.Fatal(err)
	}
	got := base.seen()[1:]
	if len(got) != 1 || got[0].method != "SwitchOn" || got[0].level != 120 {
		t.Errorf("MoveToLevelWithOnOff: the ramp saw %+v, want SwitchOn(120) alone", got)
	}
}

// moverSwitcher is a NativeRamp with both optional interfaces.
type moverSwitcher struct{ *fakeRamp }

func (r *moverSwitcher) MoveLevel(ctx context.Context, req levelcontroldef.MoveRequest, withOnOff bool) error {
	return moverRamp{r.fakeRamp}.MoveLevel(ctx, req, withOnOff)
}

func (r *moverSwitcher) SwitchOn(ctx context.Context, level uint8, d time.Duration) error {
	return switcherRamp{r.fakeRamp}.SwitchOn(ctx, level, d)
}

func (r *moverSwitcher) SwitchOff(ctx context.Context, d time.Duration) error {
	return switcherRamp{r.fakeRamp}.SwitchOff(ctx, d)
}

// TestDeclaredRampPersister is #228's NativePersister: a controller's
// write of Options reaches PersistLevel before it takes effect.
func TestDeclaredRampPersister(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := quietSite(t)
	ramp := persistRamp{fakeRamp: &fakeRamp{level: 10}, options: make(chan levelcontroldef.OptionsBitmap, 4)}
	var persister levelcontroldef.NativePersister = ramp // the optional interface the kit finds
	_ = persister
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("ramp-persist"), Name: "Desk dimmer"},
		levelcontroldef.Declare{Ramp: ramp})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, _ := light.EndpointNumber()
	if err := fabrictest.Write(ctx, pair, lifecycleAttr(ep, levelcontroldef.ClusterID, levelcontroldef.AttrOptions), uint8Value(1)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ramp.options:
		if got != levelcontroldef.OptionsExecuteIfOff {
			t.Errorf("PersistLevel saw Options %#x, want ExecuteIfOff", got)
		}
	default:
		t.Fatal("the write of Options did not reach PersistLevel")
	}
	if got := light.LevelControl().Get().Options; got != levelcontroldef.OptionsExecuteIfOff {
		t.Errorf("Options %#x after the write, want ExecuteIfOff", got)
	}
}

// TestDeclaredRampBesideLevelSource is #228's conflict: a declaration
// with both a Ramp and a LevelSource is refused.
func TestDeclaredRampBesideLevelSource(t *testing.T) {
	t.Parallel()
	_, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("ramp-both"), Name: "Both"},
		levelcontroldef.Declare{Ramp: &fakeRamp{}, LevelSource: &heldLevelSource{}})
	if !errors.Is(err, device.ErrConfiguration) {
		t.Fatalf("Ramp beside LevelSource: %v, want ErrConfiguration", err)
	}
}
