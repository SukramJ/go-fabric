// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// The behaviour of the Presets and Occupancy features no matter.js test
// file covers: SetActivePresetRequest (ThermostatServer.ts:281-316), the
// active preset a direct setpoint write ends (:682-706, :744-768), the
// setpoints in effect while unoccupied (:327-361), the atomic writes'
// fabric removal (AtomicWriteHandler.ts:378-388) and the refusals of the
// request itself, the pending value the holding peer reads (:425-450), and
// the PresetStore. The server is called directly, a controller's request
// being a context with its fabric, subject, access check and command path.

// remoteCtx is a controller's request: node on fabric, granted every
// privilege, on endpoint 1.
func remoteCtx(fabric uint8, node uint64) context.Context {
	ctx := im.WithFabricFilter(context.Background(), true, fabric)
	ctx = im.WithSubject(ctx, node, nil)
	ctx = im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
	return im.WithCommandPath(ctx, im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdAtomicRequest, HasEndpoint: true})
}

// memoryPresetStore is a PresetStore in memory; fail refuses saves.
type memoryPresetStore struct {
	presets, active []byte
	saved           bool
	fail            error
}

func (m *memoryPresetStore) LoadPresets(context.Context) (presets, activeHandle []byte, ok bool, err error) {
	return m.presets, m.active, m.saved, nil
}

func (m *memoryPresetStore) SavePresets(_ context.Context, presets, activeHandle []byte) error {
	if m.fail != nil {
		return m.fail
	}
	m.presets, m.active, m.saved = presets, activeHandle, true
	return nil
}

func mustNew(t *testing.T, cfg thermo.ThermostatConfig) *thermo.ThermostatServer {
	t.Helper()
	if cfg.Clock == nil {
		cfg.Clock = clocktest.New(time.Unix(0, 0))
	}
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func setpointPreset(handle byte, heat, cool int16) thermo.Preset {
	return newPreset(withHandle([]byte{handle}), func(p *thermo.Preset) { p.HeatingSetpoint, p.CoolingSetpoint = &heat, &cool })
}

func activate(t *testing.T, srv *thermo.ThermostatServer, handle []byte) error {
	t.Helper()
	req := tdef.SetActivePresetRequestRequest{PresetHandle: spec.NullOf[[]byte]()}
	if handle != nil {
		req.PresetHandle = spec.ValueOf(handle)
	}
	_, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdSetActivePresetRequest, req)
	return err
}

func TestSetActivePresetRequest(t *testing.T) {
	t.Parallel()
	srv := mustNew(t, thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2200, 2700)}))
	wantStatusCode(t, activate(t, srv, []byte{2}), im.StatusInvalidCommand, "Requested PresetHandle not found")

	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActivePresetHandle(); !bytes.Equal(h, []byte{1}) {
		t.Errorf("active handle %x, want 01", h)
	}
	if h, c := readInt16(t, srv, tdef.AttrOccupiedHeatingSetpoint), readInt16(t, srv, tdef.AttrOccupiedCoolingSetpoint); h != 2200 || c != 2700 {
		t.Errorf("setpoints %d / %d, want the preset's 2200 / 2700", h, c)
	}

	// A command moves the setpoints without ending the preset.
	if _, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdSetpointRaiseLower, tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 5}); err != nil {
		t.Fatal(err)
	}
	if srv.ActivePresetHandle() == nil {
		t.Error("SetpointRaiseLower ended the active preset")
	}
	// A direct write ends it.
	if err := srv.MatterWrite(remoteCtx(1, 5), tdef.AttrOccupiedHeatingSetpoint, int16(2100)); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActivePresetHandle(); h != nil {
		t.Errorf("active handle %x after a direct write, want null", h)
	}
	// So does the device's own change.
	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	srv.SetOccupiedCoolingSetpoint(2800)
	if h := srv.ActivePresetHandle(); h != nil {
		t.Errorf("active handle %x after an inbound setpoint, want null", h)
	}
	// Null clears it.
	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := activate(t, srv, nil); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActivePresetHandle(); h != nil {
		t.Errorf("active handle %x, want null", h)
	}
}

func TestOccupancySetpointsInEffect(t *testing.T) {
	t.Parallel()
	unoccupied := false
	cfg := thermostatConfig(5, []thermo.Preset{setpointPreset(1, 1800, 2900)})
	cfg.Features |= thermo.ThermostatFeatureOCC
	cfg.Occupied = &unoccupied
	srv := mustNew(t, cfg)
	changes := recordThermostatChanges(srv)

	if v, _ := srv.MatterRead(tdef.AttrOccupancy); v != uint8(0) {
		t.Errorf("Occupancy %v, want 0", v)
	}
	if h, c := readInt16(t, srv, tdef.AttrUnoccupiedHeatingSetpoint), readInt16(t, srv, tdef.AttrUnoccupiedCoolingSetpoint); h != 2000 || c != 2600 {
		t.Errorf("unoccupied setpoints %d / %d, want the defaults 2000 / 2600", h, c)
	}

	// SetpointRaiseLower moves the unoccupied pair.
	if _, err := srv.MatterInvoke(remoteCtx(1, 5), tdef.CmdSetpointRaiseLower, tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if h, o := readInt16(t, srv, tdef.AttrUnoccupiedHeatingSetpoint), readInt16(t, srv, tdef.AttrOccupiedHeatingSetpoint); h != 2100 || o != 2000 {
		t.Errorf("unoccupied heating %d, occupied %d, want 2100 and 2000", h, o)
	}

	// A preset sets the unoccupied pair.
	if err := activate(t, srv, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if h, c := readInt16(t, srv, tdef.AttrUnoccupiedHeatingSetpoint), readInt16(t, srv, tdef.AttrUnoccupiedCoolingSetpoint); h != 1800 || c != 2900 {
		t.Errorf("unoccupied setpoints %d / %d, want the preset's 1800 / 2900", h, c)
	}
	// A direct write of the occupied pair is not the one in effect.
	if err := srv.MatterWrite(remoteCtx(1, 5), tdef.AttrOccupiedHeatingSetpoint, int16(1900)); err != nil {
		t.Fatal(err)
	}
	if srv.ActivePresetHandle() == nil {
		t.Error("a write of the occupied pair ended the preset while unoccupied")
	}
	err := srv.MatterWrite(remoteCtx(1, 5), tdef.AttrUnoccupiedHeatingSetpoint, int16(500))
	wantStatusCode(t, err, im.StatusConstraintError, "HeatUnoccupiedSetpoint (500) must be greater than or equal to minHeatSetpointLimit (700)")
	if err := srv.MatterWrite(remoteCtx(1, 5), tdef.AttrUnoccupiedHeatingSetpoint, int16(2200)); err != nil {
		t.Fatal(err)
	}
	if h := srv.ActivePresetHandle(); h != nil {
		t.Errorf("active handle %x after a write of the unoccupied pair, want null", h)
	}

	srv.SetOccupied(true)
	if v, _ := srv.MatterRead(tdef.AttrOccupancy); v != uint8(1) {
		t.Errorf("Occupancy %v, want 1", v)
	}
	if changes.count(tdef.AttrOccupancy) != 1 {
		t.Errorf("Occupancy announced %d times, want 1", changes.count(tdef.AttrOccupancy))
	}
}

func TestAtomicRequestRefusals(t *testing.T) {
	t.Parallel()
	ps := &memoryPresetStore{}
	cfg := thermostatConfig(5, []thermo.Preset{})
	cfg.PresetStore = ps
	srv := mustNew(t, cfg)
	atomic := func(ctx context.Context, rt uint8, attrs []uint32, timeout *uint16) (tdef.AtomicResponse, error) {
		r, err := srv.MatterInvoke(ctx, tdef.CmdAtomicRequest, tdef.AtomicRequestRequest{RequestType: rt, AttributeRequests: attrs, Timeout: timeout})
		if err != nil {
			return tdef.AtomicResponse{}, err
		}
		return r.(tdef.AtomicResponse), nil
	}
	presets := []uint32{presetsAttribute}
	peer, other := remoteCtx(1, 5), remoteCtx(2, 5)

	_, err := atomic(context.Background(), beginWrite, presets, timeoutMs(5000))
	wantStatusCode(t, err, im.StatusInvalidCommand, "operational session")
	_, err = atomic(peer, 3, presets, nil)
	wantStatusCode(t, err, im.StatusConstraintError, "")
	_, err = atomic(peer, beginWrite, nil, timeoutMs(5000))
	wantStatusCode(t, err, im.StatusInvalidCommand, "no attribute requests")
	_, err = atomic(peer, beginWrite, []uint32{presetsAttribute, presetsAttribute}, timeoutMs(5000))
	wantStatusCode(t, err, im.StatusInvalidCommand, "duplicate")
	_, err = atomic(peer, commitWrite, presets, nil)
	wantStatusCode(t, err, im.StatusInvalidInState, "")

	r, err := atomic(peer, beginWrite, presets, timeoutMs(60000))
	if err != nil {
		t.Fatal(err)
	}
	if r.Timeout == nil || *r.Timeout != 9000 {
		t.Errorf("timeout %v, want the 9 s maximum", r.Timeout)
	}
	// Another peer: BUSY per attribute, INVALID_IN_STATE for a commit.
	r, err = atomic(other, beginWrite, presets, timeoutMs(5000))
	if err != nil || r.StatusCode != tdef.StatusFailure || r.AttributeStatus[0].StatusCode != tdef.StatusBusy {
		t.Errorf("another peer's BeginWrite %+v, %v, want BUSY", r, err)
	}
	_, err = atomic(other, rollbackWrite, presets, nil)
	wantStatusCode(t, err, im.StatusInvalidInState, "")
	_, err = atomic(peer, commitWrite, []uint32{tdef.AttrOccupiedHeatingSetpoint}, nil)
	wantStatusCode(t, err, im.StatusInvalidInState, "do not match")

	// The holding peer reads what it staged, another the stored value.
	if err := srv.MatterWrite(peer, presetsAttribute, spec.List[thermo.Preset]{newPreset()}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterReadFiltered(peer, presetsAttribute); len(v.(spec.List[thermo.Preset])) != 1 {
		t.Errorf("the holding peer reads %v, want its staged preset", v)
	}
	if v, _ := srv.MatterReadFiltered(other, presetsAttribute); len(v.(spec.List[thermo.Preset])) != 0 {
		t.Errorf("another peer reads %v, want the stored presets", v)
	}
	if err := srv.MatterWrite(other, presetsAttribute, spec.List[thermo.Preset]{}); err == nil {
		t.Error("another peer's write accepted")
	}

	// A save the store refuses fails the commit with FAILURE for the
	// attribute, and nothing is stored.
	ps.fail = errors.New("disk full")
	r, err = atomic(peer, commitWrite, presets, nil)
	if err != nil || r.StatusCode != tdef.StatusFailure || r.AttributeStatus[0].StatusCode != tdef.StatusFailure {
		t.Errorf("commit with a failing store %+v, %v, want FAILURE", r, err)
	}
	if len(srv.Presets()) != 0 {
		t.Error("presets stored although the store refused")
	}
	ps.fail = nil

	// The removal of the holding peer's fabric ends its write.
	if _, err := atomic(peer, beginWrite, presets, timeoutMs(5000)); err != nil {
		t.Fatal(err)
	}
	srv.MatterFabricRemoved(1)
	if r, err := atomic(other, beginWrite, presets, timeoutMs(5000)); err != nil || r.StatusCode != tdef.StatusSuccess {
		t.Errorf("BeginWrite after the holder's fabric left %+v, %v, want SUCCESS", r, err)
	}

	// Without Presets the command is unsupported.
	plain := mustNew(t, thermo.DefaultThermostatConfig())
	_, err = plain.MatterInvoke(peer, tdef.CmdAtomicRequest, tdef.AtomicRequestRequest{RequestType: beginWrite, AttributeRequests: presets, Timeout: timeoutMs(5000)})
	wantStatusCode(t, err, im.StatusUnsupportedCommand, "")
}

func TestPresetStoreRestoresPresetsAndActiveHandle(t *testing.T) {
	t.Parallel()
	ps := &memoryPresetStore{}
	cfg := thermostatConfig(5, []thermo.Preset{newPreset()})
	cfg.PresetStore = ps
	first := mustNew(t, cfg)
	handle := first.Presets()[0].PresetHandle.Value
	if err := activate(t, first, handle); err != nil {
		t.Fatal(err)
	}

	// A second start on the store ignores the configured presets.
	cfg.Presets = []thermo.Preset{newPreset(), newPreset(withName("x"))}
	second := mustNew(t, cfg)
	if got := second.Presets(); len(got) != 1 || !bytes.Equal(got[0].PresetHandle.Value, handle) {
		t.Errorf("restored presets %+v, want the stored one", got)
	}
	if h := second.ActivePresetHandle(); !bytes.Equal(h, handle) {
		t.Errorf("restored active handle %x, want %x", h, handle)
	}

	decoded, err := thermo.DecodePresets(thermo.EncodePresets(first.Presets()))
	if err != nil || len(decoded) != 1 || !bytes.Equal(decoded[0].PresetHandle.Value, handle) {
		t.Errorf("round trip %+v, %v", decoded, err)
	}
}

func TestPresetConfigurationRefused(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func(*thermo.ThermostatConfig){
		"no presets":        func(c *thermo.ThermostatConfig) { c.NumberOfPresets = 0 },
		"no preset types":   func(c *thermo.ThermostatConfig) { c.PresetTypes = nil },
		"an unnamed type":   func(c *thermo.ThermostatConfig) { c.Presets = []thermo.Preset{newPreset(withName("Home"))} },
		"a missing type":    func(c *thermo.ThermostatConfig) { c.Presets = []thermo.Preset{newPreset(func(p *thermo.Preset) { p.PresetScenario = tdef.PresetScenarioVacation })} },
		"too many presets":  func(c *thermo.ThermostatConfig) { c.NumberOfPresets = 1; c.Presets = []thermo.Preset{newPreset(), newPreset()} },
		"setpoint too high": func(c *thermo.ThermostatConfig) { h := int16(3500); c.Presets = []thermo.Preset{newPreset(func(p *thermo.Preset) { p.HeatingSetpoint = &h })} },
	} {
		cfg := thermostatConfig(5, nil)
		edit(&cfg)
		if _, err := thermo.New(cfg); !errors.Is(err, thermo.ErrPresets) {
			t.Errorf("%s: New error %v, want ErrPresets", name, err)
		}
	}
	negative := int8(-1)
	cfg := thermo.DefaultThermostatConfig()
	cfg.MinSetpointDeadBand = &negative
	if _, err := thermo.New(cfg); !errors.Is(err, thermo.ErrSetpoints) {
		t.Errorf("negative deadband: New error %v, want ErrSetpoints", err)
	}
}
