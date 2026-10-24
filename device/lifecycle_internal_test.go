// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// fakeHost is a node as deviceseam sees it.
type fakeHost struct {
	clk clock.Clock
	mu  sync.Mutex
	// numbers maps a key to its endpoint number; reachable and names
	// record what the device told the host.
	numbers   map[string]uint16
	reachable []bool
	names     []string
	// hostNames record the host statements of the name.
	hostNames []string
}

func (h *fakeHost) Clock() clock.Clock { return h.clk }

func (h *fakeHost) EndpointNumber(key endpoint.SourceKey) (uint16, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.numbers[key.String()]
	return n, ok
}

func (h *fakeHost) NotifyReachable(_ context.Context, _ endpoint.SourceKey, reachable bool) {
	h.mu.Lock()
	h.reachable = append(h.reachable, reachable)
	h.mu.Unlock()
}

func (h *fakeHost) Rename(_ context.Context, _ endpoint.SourceKey, name string) {
	h.mu.Lock()
	h.names = append(h.names, name)
	h.mu.Unlock()
}

func (h *fakeHost) HostName(_ context.Context, _ endpoint.SourceKey, name string) {
	h.mu.Lock()
	h.hostNames = append(h.hostNames, name)
	h.mu.Unlock()
}

// TestLifecycleThroughTheSeam: a device the node binds takes its clock,
// answers its endpoint number, tells it reachability and names, binds its
// parts, refuses a second node and lets go at detach.
func TestLifecycleThroughTheSeam(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := clocktest.New(time.Unix(1_000_000, 0))
	host := &fakeHost{clk: fake, numbers: map[string]uint16{"test:lamp": 7, "test:part": 8}}
	lamp, err := NewOnOffLight(testOptions("lamp"))
	if err != nil {
		t.Fatal(err)
	}
	// Before the node: values only.
	lamp.SetReachable(ctx, false)
	lamp.Rename(ctx, "Desk")
	if lamp.Reachable() || lamp.Spec().FriendlyName != "Desk" {
		t.Errorf("before Add: reachable %v, name %q", lamp.Reachable(), lamp.Spec().FriendlyName)
	}
	if _, ok := lamp.EndpointNumber(); ok {
		t.Error("an endpoint number before Add")
	}
	if _, isDevice, _ := deviceseam.Attach(struct{}{}, host); isDevice {
		t.Error("a non-device bound")
	}
	detach, isDevice, err := deviceseam.Attach(lamp, host)
	if err != nil || !isDevice {
		t.Fatalf("Attach: %v, %v", isDevice, err)
	}
	if _, _, err := deviceseam.Attach(lamp, host); !errors.Is(err, ErrAddedElsewhere) {
		t.Errorf("second Attach: %v, want ErrAddedElsewhere", err)
	}
	if n, ok := lamp.EndpointNumber(); !ok || n != 7 {
		t.Errorf("EndpointNumber %d, %v; want 7", n, ok)
	}
	if !lamp.clock.Now().Equal(fake.Now()) {
		t.Error("the device does not run on the node's clock")
	}
	lamp.SetReachable(ctx, true)
	lamp.SetReachable(ctx, true) // no change: not told again
	lamp.Rename(ctx, "Lamp")
	host.mu.Lock()
	if len(host.reachable) != 1 || !host.reachable[0] || len(host.names) != 1 || host.names[0] != "Lamp" {
		t.Errorf("the host was told reachable %v, names %v", host.reachable, host.names)
	}
	host.mu.Unlock()
	detach()
	// A part added to a bound device is bound with it.
	owner, err := NewBridgedNode(testOptions("owner"))
	if err != nil {
		t.Fatal(err)
	}
	part, err := NewOnOffLight(testOptions("part"))
	if err != nil {
		t.Fatal(err)
	}
	detachOwner, _, err := deviceseam.Attach(owner, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(part.Device); err != nil {
		t.Fatal(err)
	}
	if n, ok := part.EndpointNumber(); !ok || n != 8 {
		t.Errorf("the part's EndpointNumber %d, %v; want 8", n, ok)
	}
	detachOwner()
	if _, ok := lamp.EndpointNumber(); ok {
		t.Error("an endpoint number after detach")
	}
}

// TestDeviceClockAndQueue: the device's clock answers before and after a
// node, and its queue drops what arrives while it does not run.
func TestDeviceClockAndQueue(t *testing.T) {
	t.Parallel()
	c := &deviceClock{}
	if c.Now().IsZero() {
		t.Error("the system clock before a node")
	}
	fake := clocktest.New(time.Unix(2_000_000, 0))
	c.setNode(fake)
	fired := make(chan struct{}, 1)
	c.AfterFunc(time.Second, func() { fired <- struct{}{} })
	tm := c.NewTimer(time.Second)
	tk := c.NewTicker(time.Second)
	fake.Advance(time.Second)
	<-fired
	<-tm.C()
	<-tk.C()
	tk.Stop()

	q := newQueue(&deviceClock{})
	q.Enqueue(func() { t.Error("ran on a stopped queue") })
	q.Enqueue(nil)
	q.Stop() // not running: a no-op
	q.Start()
	q.Start() // running: a no-op
	done := make(chan struct{})
	q.Enqueue(func() { close(done) })
	q.Stop()
	<-done
}

// TestKitOptionsAndPorts: the kit's own declarations, a declared
// LevelSource and a declared mode changer reach their servers.
func TestKitOptionsAndPorts(t *testing.T) {
	t.Parallel()
	for _, o := range []Option{withOptional(6), WithServer(&hostServer{id: 0xFFF1_FC00}), WithChangeNotifier(nil)} {
		_ = o.ClusterID()
		_ = o.ClusterDeclaration()
	}
	if hasPorts(nil) || hasPorts(3) {
		t.Error("a non-struct carries a port")
	}
	if v, ok := consume(map[uint32]any{1: int64(5)}, 1); !ok || v != 5 {
		t.Errorf("consume int64: %d, %v", v, ok)
	}
	if _, ok := consume(map[uint32]any{1: int64(-1), 2: "x"}, 1); ok {
		t.Error("consume took a negative value")
	}
	if _, ok := consume(map[uint32]any{2: "x"}, 2); ok {
		t.Error("consume took a string")
	}
	if _, ok := consume8(map[uint32]any{1: uint64(300)}, 1); ok {
		t.Error("consume8 took 300")
	}
	d, err := NewDimmableLight(testOptions("port"), lvldef.Declare{LevelSource: &levelPort{}, Transitions: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Server(lvldef.ClusterID).(*levelcontrol.Server); !ok {
		t.Errorf("a declared LevelSource mounts %T", d.Server(lvldef.ClusterID))
	}
	ch := &fakeModes{}
	w, err := NewLaundryWasher(testOptions("washer"), lwmdef.Declare{ModeChanger: ch, Initial: lwmdef.Patch{
		SupportedModes: modes(func(l string, m uint8, tag uint16) lwmdef.ModeOptionStruct {
			return lwmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []lwmdef.ModeTagStruct{{Value: lwmdef.ModeTag(tag)}}}
		}, modebase.LaundryTagNormal, modebase.LaundryTagHeavy),
		CurrentMode: new(uint8(0)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Server(lwmdef.ClusterID).MatterInvoke(context.Background(), lwmdef.CmdChangeToMode, lwmdef.ChangeToModeRequest{NewMode: 1}); err != nil {
		t.Fatal(err)
	}
	if len(ch.changed) != 1 || ch.changed[0] != 1 {
		t.Errorf("the declared ModeChanger saw %v, want [1]", ch.changed)
	}
}

// TestPortConfigurationAndHookRefusal: a fan's configuration attributes
// come from the declaration's Initial; a hook the host's server cannot
// take refuses the device.
func TestPortConfigurationAndHookRefusal(t *testing.T) {
	t.Parallel()
	d, err := NewFan(testOptions("fan"), fandef.Declare{
		StateSource: &fakeFan{},
		Features:    fandef.FeatureMultiSpeed | fandef.FeatureRocking | fandef.FeatureWind,
		Initial: fandef.Patch{
			FanModeSequence: new(fandef.FanModeSequenceOffLowMedHigh), SpeedMax: new(uint8(10)),
			RockSupport: new(fandef.RockRockLeftRight), WindSupport: new(fandef.WindSleepWind),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Server(fandef.ClusterID).MatterRead(fandef.AttrSpeedMax); v != uint8(10) && v != uint64(10) {
		t.Errorf("SpeedMax %v, want the declared 10", v)
	}
	host := &hostServer{id: onoffdef.ClusterID}
	if _, err := NewOnOffLight(testOptions("refused"), onoffdef.Declare{
		HostServer: host,
		HandleOn:   func(context.Context, onoffdef.OnRequest) (struct{}, error) { return struct{}{}, nil },
	}); !errors.Is(err, ErrConfiguration) {
		t.Errorf("a handler the host's server cannot take: %v, want ErrConfiguration", err)
	}
	lamp, err := NewOnOffLight(testOptions("async"))
	if err != nil {
		t.Fatal(err)
	}
	lamp.OnChangedAsync(onoffdef.ClusterID, nil)()
	lamp.OnChanged(0xFFFF, func(contract.Change) {})()
}

// levelPort is a LevelSource in the generated requests.
type levelPort struct{ level uint8 }

func (p *levelPort) CurrentLevel() (uint8, bool)                            { return p.level, true }
func (p *levelPort) Options() lvldef.OptionsBitmap                          { return 0 }
func (p *levelPort) OnLevel() (uint8, bool)                                 { return 0, false }
func (p *levelPort) SetOptions(context.Context, lvldef.OptionsBitmap) error { return nil }
func (p *levelPort) SetOnLevel(context.Context, *uint8) error               { return nil }
func (p *levelPort) MoveToLevel(_ context.Context, r lvldef.MoveToLevelRequest) error {
	p.level = r.Level
	return nil
}

func (p *levelPort) MoveToLevelWithOnOff(_ context.Context, r lvldef.MoveToLevelWithOnOffRequest) error {
	p.level = r.Level
	return nil
}
func (p *levelPort) Move(context.Context, lvldef.MoveRequest) error                   { return nil }
func (p *levelPort) MoveWithOnOff(context.Context, lvldef.MoveWithOnOffRequest) error { return nil }
func (p *levelPort) Step(context.Context, lvldef.StepRequest) error                   { return nil }
func (p *levelPort) StepWithOnOff(context.Context, lvldef.StepWithOnOffRequest) error { return nil }
func (p *levelPort) Stop(context.Context, lvldef.StopRequest) error                   { return nil }
func (p *levelPort) StopWithOnOff(context.Context, lvldef.StopWithOnOffRequest) error { return nil }
