// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// booster is a host that records the commands and fails on request.
type booster struct {
	mu              sync.Mutex
	boosts, cancels int
	last            energy.BoostInfo
	err             error
}

func (b *booster) Boost(_ context.Context, info energy.BoostInfo) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.boosts++
	b.last = info
	return b.err
}

func (b *booster) CancelBoost(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancels++
	return b.err
}

// event is one emitted event.
type event struct {
	endpoint uint16
	cluster  uint32
	id       uint32
	data     any
	priority contract.EventPriority
}

type emitter struct {
	mu     sync.Mutex
	events []event
}

func (e *emitter) MatterEmitEvent(endpoint uint16, cluster, id uint32, data any, priority contract.EventPriority) {
	e.mu.Lock()
	e.events = append(e.events, event{endpoint, cluster, id, data, priority})
	e.mu.Unlock()
}

func (e *emitter) take() []event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.events
	e.events = nil
	return out
}

func newWaterHeater(t *testing.T, features energy.WaterHeaterFeature, host *booster) *energy.WaterHeaterManagementServer {
	t.Helper()
	srv, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
		Features:    features,
		HeaterTypes: energy.HeatSourceImmersionElement1 | energy.HeatSourceImmersionElement2,
		Initial:     energy.WaterHeaterState{TankVolume: 100, TankPercentage: 40},
		Booster:     host,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func preferenceConfig(features energy.PreferenceFeature) energy.PreferenceConfig {
	label := func(s string) *string { return &s }
	return energy.PreferenceConfig{
		Features: features,
		EnergyBalances: []energy.Balance{
			{Step: 0, Label: label("Efficient")}, {Step: 50}, {Step: 100, Label: label("Comfort")},
		},
		EnergyPriorities: []energy.Priority{energy.PriorityEfficiency, energy.PriorityComfort},
		LowPowerModeSensitivities: []energy.Balance{
			{Step: 0, Label: label("1 Minute")}, {Step: 100, Label: label("Never")},
		},
	}
}

// TestBoostLifecycle: a Boost sets BoostState Active and emits
// BoostStarted with the request's BoostInfo on the stamped endpoint; a
// second Boost replaces the first and emits again; CancelBoost calls the
// host, sets Inactive and emits BoostEnded; a CancelBoost without a
// running boost succeeds silently; EndBoost ends a running boost.
func TestBoostLifecycle(t *testing.T) {
	t.Parallel()
	host := &booster{}
	srv := newWaterHeater(t, energy.WaterHeaterFeatureTankPercent, host)
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	srv.SetEndpoint(7)
	var changed [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { changed = append(changed, ids) })
	ctx := context.Background()

	yes := true
	info := energy.BoostInfo{Duration: 600, OneShot: &yes}
	if _, err := srv.MatterInvoke(ctx, whm.CmdBoost, whm.BoostRequest{BoostInfo: info}); err != nil {
		t.Fatal(err)
	}
	if srv.BoostState() != energy.BoostStateActive || host.boosts != 1 || host.last.Duration != 600 {
		t.Fatalf("after Boost: state %d, host %+v", srv.BoostState(), host)
	}
	evs := em.take()
	if len(evs) != 1 || evs[0].id != whm.EventBoostStarted || evs[0].endpoint != 7 || evs[0].cluster != whm.ClusterID ||
		evs[0].priority != contract.EventPriorityInfo {
		t.Fatalf("events %+v", evs)
	}
	if got := evs[0].data.(whm.BoostStartedEvent).BoostInfo; got.Duration != 600 || got.OneShot == nil || !*got.OneShot {
		t.Errorf("BoostStarted %+v", got)
	}
	if len(changed) != 1 || changed[0][0] != whm.AttrBoostState {
		t.Errorf("changes %v", changed)
	}

	// A second boost while one runs: accepted, emitted again.
	if _, err := srv.MatterInvoke(ctx, whm.CmdBoost, &whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 5}}); err != nil {
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != whm.EventBoostStarted {
		t.Errorf("re-boost events %+v", evs)
	}

	if _, err := srv.MatterInvoke(ctx, whm.CmdCancelBoost, whm.CancelBoostRequest{}); err != nil {
		t.Fatal(err)
	}
	if srv.BoostState() != energy.BoostStateInactive || host.cancels != 1 {
		t.Errorf("after CancelBoost: state %d, cancels %d", srv.BoostState(), host.cancels)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != whm.EventBoostEnded {
		t.Errorf("cancel events %+v", evs)
	}

	// CancelBoost without a boost: SUCCESS, no host call, no event.
	if _, err := srv.MatterInvoke(ctx, whm.CmdCancelBoost, whm.CancelBoostRequest{}); err != nil {
		t.Errorf("idle CancelBoost: %v", err)
	}
	if host.cancels != 1 || len(em.take()) != 0 {
		t.Errorf("idle CancelBoost reached the host (%d) or emitted", host.cancels)
	}

	// EndBoost: the host's report that the boost ended on its own.
	if _, err := srv.MatterInvoke(ctx, whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 5}}); err != nil {
		t.Fatal(err)
	}
	em.take()
	if err := srv.EndBoost(); err != nil {
		t.Fatal(err)
	}
	if srv.BoostState() != energy.BoostStateInactive {
		t.Error("EndBoost left the boost running")
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != whm.EventBoostEnded {
		t.Errorf("EndBoost events %+v", evs)
	}
	if err := srv.EndBoost(); err != nil || len(em.take()) != 0 {
		t.Errorf("idle EndBoost: %v", err)
	}
}

// TestBoostHostRefusal: a host error refuses the command, its IM status
// passes through, and BoostState stays.
func TestBoostHostRefusal(t *testing.T) {
	t.Parallel()
	host := &booster{err: refusal{}}
	srv := newWaterHeater(t, 0, host)
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	_, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 60}})
	wantStatus(t, err, im.StatusInvalidInState)
	if srv.BoostState() != energy.BoostStateInactive || len(em.take()) != 0 {
		t.Error("a refused Boost changed the state or emitted")
	}

	host.err = nil
	if _, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 60}}); err != nil {
		t.Fatal(err)
	}
	host.err = refusal{}
	_, err = srv.MatterInvoke(context.Background(), whm.CmdCancelBoost, whm.CancelBoostRequest{})
	wantStatus(t, err, im.StatusInvalidInState)
	if srv.BoostState() != energy.BoostStateActive {
		t.Error("a refused CancelBoost ended the boost")
	}
}

// TestBoostMalformedFields: fields of another type are INVALID_COMMAND.
func TestBoostMalformedFields(t *testing.T) {
	t.Parallel()
	srv := newWaterHeater(t, 0, &booster{})
	for _, f := range []any{nil, "x", (*whm.BoostRequest)(nil)} {
		_, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, f)
		wantStatus(t, err, im.StatusInvalidCommand)
	}
}

type refusal struct{}

func (refusal) Error() string                   { return "refused" }
func (refusal) MatterStatusCode() im.StatusCode { return im.StatusInvalidInState }

// TestWaterHeaterState: the setters store and report what the device
// sets, HeatDemand stays inside HeaterTypes, and the definition's
// constraints hold.
func TestWaterHeaterState(t *testing.T) {
	t.Parallel()
	srv := newWaterHeater(t, energy.WaterHeaterFeatureEnergyManagement|energy.WaterHeaterFeatureTankPercent, &booster{})
	if v, _ := srv.MatterRead(whm.AttrHeaterTypes); v != uint8(energy.HeatSourceImmersionElement1|energy.HeatSourceImmersionElement2) {
		t.Errorf("HeaterTypes = %v", v)
	}
	if v, _ := srv.MatterRead(whm.AttrBoostState); v != uint8(energy.BoostStateInactive) {
		t.Errorf("BoostState = %v", v)
	}
	if err := srv.SetHeatDemand(energy.HeatSourceImmersionElement2); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetTankPercentage(76); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetTankVolume(150); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetEstimatedHeatRequired(4000); err != nil {
		t.Fatal(err)
	}
	want := energy.WaterHeaterState{HeatDemand: energy.HeatSourceImmersionElement2, TankVolume: 150, EstimatedHeatRequired: 4000, TankPercentage: 76}
	if st := srv.State(); st != want {
		t.Errorf("State %+v, want %+v", st, want)
	}
	for name, err := range map[string]error{
		"demand outside types": srv.SetHeatDemand(energy.HeatSourceBoiler),
		"reserved bit":         srv.SetHeatDemand(1 << 6),
		"percentage over 100":  srv.SetTankPercentage(101),
		"negative energy":      srv.SetEstimatedHeatRequired(-1),
	} {
		if !errors.Is(err, energy.ErrInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if st := srv.State(); st != want {
		t.Errorf("refused sets changed the state: %+v", st)
	}

	// Without TP or EM the feature attributes are not served.
	plain := newWaterHeater(t, 0, &booster{})
	if err := plain.SetTankPercentage(10); err == nil {
		t.Error("TankPercentage set without TP")
	}
	if _, ok := plain.MatterRead(whm.AttrTankVolume); ok {
		t.Error("TankVolume served without EM")
	}
}

// TestWaterHeaterConfig: construction refuses a missing host and an
// initial state outside the model.
func TestWaterHeaterConfig(t *testing.T) {
	t.Parallel()
	if _, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{}); !errors.Is(err, energy.ErrNoBooster) {
		t.Errorf("no booster: %v", err)
	}
	if _, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
		HeaterTypes: energy.HeatSourceHeatPump, Initial: energy.WaterHeaterState{HeatDemand: energy.HeatSourceBoiler}, Booster: &booster{},
	}); !errors.Is(err, energy.ErrInvalidValue) {
		t.Errorf("demand outside types: %v", err)
	}
	if _, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
		Features: energy.WaterHeaterFeatureTankPercent, Initial: energy.WaterHeaterState{TankPercentage: 120}, Booster: &booster{},
	}); !errors.Is(err, energy.ErrInvalidValue) {
		t.Errorf("percentage over 100: %v", err)
	}
	if _, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{Features: 1 << 5, Booster: &booster{}}); err == nil {
		t.Error("an undefined feature was accepted")
	}
}

// changer records the preference writes and refuses on request.
type changer struct {
	attr, index []uint32
	err         error
}

func (c *changer) ChangePreference(_ context.Context, attrID uint32, index uint8) error {
	if c.err != nil {
		return c.err
	}
	c.attr = append(c.attr, attrID)
	c.index = append(c.index, uint32(index))
	return nil
}

// TestEnergyPreference: the lists read as configured, a write within the
// list reaches the host and is stored, a host refusal keeps the value,
// and construction holds the lists and indexes to the model.
func TestEnergyPreference(t *testing.T) {
	t.Parallel()
	cfg := preferenceConfig(energy.PreferenceFeatureEnergyBalance)
	host := &changer{}
	cfg.Changer = host
	cfg.CurrentEnergyBalance = 1
	srv, err := energy.NewEnergyPreference(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := srv.CurrentEnergyBalance(); !ok || v != 1 {
		t.Errorf("CurrentEnergyBalance %d %v", v, ok)
	}
	if _, ok := srv.CurrentLowPowerModeSensitivity(); ok {
		t.Error("CurrentLowPowerModeSensitivity served without LPMS")
	}
	if v, ok := srv.MatterRead(ep.AttrEnergyBalances); !ok || len(v.(spec.List[energy.Balance])) != 3 {
		t.Errorf("EnergyBalances %v", v)
	}
	if v, _ := srv.MatterRead(ep.AttrEnergyPriorities); v == nil {
		t.Error("EnergyPriorities not served")
	}
	if err := srv.MatterWrite(context.Background(), ep.AttrCurrentEnergyBalance, uint64(2)); err != nil {
		t.Fatal(err)
	}
	if len(host.attr) != 1 || host.attr[0] != ep.AttrCurrentEnergyBalance || host.index[0] != 2 {
		t.Errorf("host saw %v %v", host.attr, host.index)
	}
	host.err = refusal{}
	wantStatus(t, srv.MatterWrite(context.Background(), ep.AttrCurrentEnergyBalance, uint64(0)), im.StatusInvalidInState)
	if v, _ := srv.CurrentEnergyBalance(); v != 2 {
		t.Errorf("a refused write changed the value to %d", v)
	}
	wantStatus(t, srv.MatterWrite(context.Background(), ep.AttrEnergyBalances, nil), im.StatusUnsupportedWrite)
	if _, err := srv.MatterInvoke(context.Background(), 0, nil); err == nil {
		t.Error("a command was accepted")
	}

	for name, mutate := range map[string]func(*energy.PreferenceConfig){
		"balance index":     func(c *energy.PreferenceConfig) { c.CurrentEnergyBalance = 3 },
		"sensitivity index": func(c *energy.PreferenceConfig) { c.CurrentLowPowerModeSensitivity = 2 },
		"one balance":       func(c *energy.PreferenceConfig) { c.EnergyBalances = c.EnergyBalances[:1] },
		"three priorities": func(c *energy.PreferenceConfig) {
			c.EnergyPriorities = append(c.EnergyPriorities, energy.PrioritySpeed)
		},
		"step over 100": func(c *energy.PreferenceConfig) { c.EnergyBalances[2].Step = 101 },
	} {
		c := preferenceConfig(energy.PreferenceFeatureEnergyBalance | energy.PreferenceFeatureLowPowerModeSensitivity)
		mutate(&c)
		if _, err := energy.NewEnergyPreference(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
