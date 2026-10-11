// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package appliance_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// oven is a host microwave: state, modes and the calls it received.
type oven struct {
	state     uint8
	noNormal  bool
	noStart   bool
	refuse    error
	params    []cooking
	cookTimes []uint32
}

type cooking struct {
	mode         uint8
	cookTime     uint32
	start        bool
	power, watts *uint8
}

func (o *oven) OperationalState() uint8 { return o.state }
func (o *oven) NormalMode() (uint8, bool) {
	return 0, !o.noNormal
}
func (o *oven) SupportedMode(m uint8) bool { return m <= 1 }
func (o *oven) StartSupported() bool       { return !o.noStart }
func (o *oven) SetCookingParameters(_ context.Context, mode uint8, cookTime uint32, start bool, power, watts *uint8) error {
	o.params = append(o.params, cooking{mode, cookTime, start, power, watts})
	return o.refuse
}

func (o *oven) ModifyCookTime(_ context.Context, cookTime uint32) error {
	o.cookTimes = append(o.cookTimes, cookTime)
	return o.refuse
}

func status(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no status", err)
	}
	return sce.MatterStatusCode()
}

func ptr[T any](v T) *T { return &v }

func newMicrowave(t *testing.T, o *oven, f uint32) *appliance.MicrowaveServer {
	t.Helper()
	srv, err := appliance.NewMicrowaveOvenControl(appliance.MicrowaveConfig{
		Features: f, Oven: o, AddMoreTime: true, MaxCookTime: 600,
		PowerSetting: 80, MinPower: 20, MaxPower: 80, PowerStep: 20,
		SupportedWatts: []uint16{300, 600, 900}, SelectedWattIndex: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestMicrowaveDefaults(t *testing.T) {
	t.Parallel()
	srv := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerAsNumber)
	if srv.CookTime() != appliance.DefaultCookTime {
		t.Errorf("CookTime %d", srv.CookTime())
	}
	// PowerSetting 100 is out of the PWRLMTS range 20..80 but within the
	// default 10..100 of a server without PWRLMTS.
	if _, err := appliance.NewMicrowaveOvenControl(appliance.MicrowaveConfig{
		Features: appliance.MicrowaveFeaturePowerAsNumber | appliance.MicrowaveFeaturePowerNumberLimits, Oven: &oven{},
		MaxCookTime: 600, PowerSetting: 100, MinPower: 20, MaxPower: 80, PowerStep: 20,
	}); !errors.Is(err, appliance.ErrMicrowaveValue) {
		t.Errorf("PowerSetting outside the limits: %v", err)
	}
	for name, cfg := range map[string]appliance.MicrowaveConfig{
		"no oven":         {Features: appliance.MicrowaveFeaturePowerAsNumber, MaxCookTime: 60, PowerSetting: 100},
		"no max":          {Features: appliance.MicrowaveFeaturePowerAsNumber, Oven: &oven{}, PowerSetting: 100},
		"cook above max":  {Features: appliance.MicrowaveFeaturePowerAsNumber, Oven: &oven{}, MaxCookTime: 60, CookTime: 61, PowerSetting: 100},
		"no watts":        {Features: appliance.MicrowaveFeaturePowerInWatts, Oven: &oven{}, MaxCookTime: 60},
		"no feature":      {Oven: &oven{}, MaxCookTime: 60},
		"power off-step":  {Features: appliance.MicrowaveFeaturePowerAsNumber, Oven: &oven{}, MaxCookTime: 60, PowerSetting: 15},
		"max above 86400": {Features: appliance.MicrowaveFeaturePowerAsNumber, Oven: &oven{}, MaxCookTime: 86401, PowerSetting: 100},
	} {
		if _, err := appliance.NewMicrowaveOvenControl(cfg); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

// TestSetCookingParameters walks chip's HandleSetCookingParameters checks
// in order (MicrowaveOvenControlCluster.cpp:195-318).
func TestSetCookingParameters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := appliance.MicrowaveFeaturePowerAsNumber | appliance.MicrowaveFeaturePowerNumberLimits
	for _, c := range []struct {
		name string
		o    *oven
		req  mwo.SetCookingParametersRequest
		want im.StatusCode
	}{
		{"running", &oven{state: 1}, mwo.SetCookingParametersRequest{}, im.StatusInvalidInState},
		{"start unsupported", &oven{noStart: true}, mwo.SetCookingParametersRequest{StartAfterSetting: ptr(false)}, im.StatusInvalidCommand},
		{"no normal mode", &oven{noNormal: true}, mwo.SetCookingParametersRequest{}, im.StatusInvalidCommand},
		{"mode unsupported", &oven{}, mwo.SetCookingParametersRequest{CookMode: ptr(uint8(7))}, im.StatusConstraintError},
		{"cook time 0", &oven{}, mwo.SetCookingParametersRequest{CookTime: ptr(uint32(0))}, im.StatusConstraintError},
		{"cook time above max", &oven{}, mwo.SetCookingParametersRequest{CookTime: ptr(uint32(601))}, im.StatusConstraintError},
		{"watt index with PWRNUM", &oven{}, mwo.SetCookingParametersRequest{WattSettingIndex: ptr(uint8(0))}, im.StatusInvalidCommand},
		{"power below min", &oven{}, mwo.SetCookingParametersRequest{PowerSetting: ptr(uint8(10))}, im.StatusConstraintError},
		{"power off step", &oven{}, mwo.SetCookingParametersRequest{PowerSetting: ptr(uint8(30))}, im.StatusConstraintError},
		{"host refuses", &oven{refuse: errors.New("door open")}, mwo.SetCookingParametersRequest{}, im.StatusFailure},
		{"host refuses with status", &oven{refuse: spec.Errorf(im.StatusBusy, "busy")}, mwo.SetCookingParametersRequest{}, im.StatusBusy},
	} {
		srv := newMicrowave(t, c.o, f)
		_, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, c.req)
		if got := status(t, err); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if srv.CookTime() != appliance.DefaultCookTime {
			t.Errorf("%s: CookTime changed to %d", c.name, srv.CookTime())
		}
	}

	o := &oven{}
	srv := newMicrowave(t, o, f)
	// Defaults: the Normal mode, 30 s, the maximum power.
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{CookTime: ptr(uint32(120))}); err != nil {
		t.Fatal(err)
	}
	if got := o.params[0]; got.mode != 0 || got.cookTime != 120 || got.start || *got.power != 80 || got.watts != nil {
		t.Errorf("host saw %+v", got)
	}
	if srv.CookTime() != 120 {
		t.Errorf("CookTime %d", srv.CookTime())
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{CookMode: ptr(uint8(1)), PowerSetting: ptr(uint8(40)), StartAfterSetting: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if got := o.params[1]; got.mode != 1 || got.cookTime != 30 || !got.start || *got.power != 40 {
		t.Errorf("host saw %+v", got)
	}
	if v, _ := srv.MatterRead(mwo.AttrPowerSetting); v != uint8(40) {
		t.Errorf("PowerSetting %v", v)
	}
}

func TestSetCookingParametersWatts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o := &oven{}
	srv := newMicrowave(t, o, appliance.MicrowaveFeaturePowerInWatts)
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{PowerSetting: ptr(uint8(50))}); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("PowerSetting with WATTS: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{WattSettingIndex: ptr(uint8(3))}); status(t, err) != im.StatusConstraintError {
		t.Errorf("index 3 of 3: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{WattSettingIndex: ptr(uint8(0))}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(mwo.AttrSelectedWattIndex); v != uint8(0) || *o.params[0].watts != 0 {
		t.Errorf("SelectedWattIndex %v", v)
	}
	// Default: the last index.
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(mwo.AttrSelectedWattIndex); v != uint8(2) {
		t.Errorf("SelectedWattIndex %v", v)
	}
	if err := srv.SetSelectedWattIndex(3); err == nil {
		t.Error("watt index 3 set")
	}
	if err := srv.SetPowerSetting(10); err == nil {
		t.Error("PowerSetting on WATTS set")
	}
	// SupportedWatts encodes as a list of unsigned integers.
	v, _ := srv.MatterRead(mwo.AttrSupportedWatts)
	enc := tlv.NewEncoder()
	v.(spec.Encodable).EncodeTLV(enc, tlv.AnonymousTag())
	got, _ := enc.Bytes()
	want := spectest.Encode(t, wattsList{300, 600, 900})
	if !bytes.Equal(got, want) {
		t.Errorf("SupportedWatts % X, want % X", got, want)
	}
}

// wattsList is the reference encoding: the generated list encoder.
type wattsList []uint16

func (l wattsList) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	spec.PutList(spec.PutUint[uint16])(enc, tag, []uint16(l))
}

// TestAddMoreTime follows chip's HandleAddMoreTime
// (MicrowaveOvenControlCluster.cpp:320-339).
func TestAddMoreTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o := &oven{state: 1} // running is fine
	srv := newMicrowave(t, o, appliance.MicrowaveFeaturePowerAsNumber)
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 60}); err != nil {
		t.Fatal(err)
	}
	if srv.CookTime() != 90 || o.cookTimes[0] != 90 {
		t.Errorf("CookTime %d, host %v", srv.CookTime(), o.cookTimes)
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 511}); status(t, err) != im.StatusConstraintError {
		t.Errorf("past MaxCookTime: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 601}); status(t, err) != im.StatusConstraintError {
		t.Errorf("above MaxCookTime: %v", err)
	}
	o.state = 3
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 1}); status(t, err) != im.StatusInvalidInState {
		t.Errorf("in Error: %v", err)
	}
	o.state, o.refuse = 0, errors.New("no")
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 1}); status(t, err) != im.StatusFailure {
		t.Errorf("refused: %v", err)
	}
	if srv.CookTime() != 90 {
		t.Errorf("CookTime %d", srv.CookTime())
	}
	if err := srv.SetCookTime(0); status(t, err) != im.StatusConstraintError {
		t.Errorf("SetCookTime(0): %v", err)
	}
	if err := srv.SetCookTime(600); err != nil || srv.CookTime() != 600 {
		t.Errorf("SetCookTime(600): %v %d", err, srv.CookTime())
	}
	if err := srv.SetPowerSetting(50); err != nil {
		t.Error(err)
	}
	if err := srv.SetSelectedWattIndex(0); err == nil {
		t.Error("watt index on PWRNUM set")
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdAddMoreTime, "junk"); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("junk: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, mwo.CmdSetCookingParameters, "junk"); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("junk: %v", err)
	}
}

// washerListener records the controller changes it hears of.
type washerListener struct {
	spins  []*uint8
	rinses []appliance.NumberOfRinses
}

func (l *washerListener) SpinSpeedCurrentChanged(s *uint8) { l.spins = append(l.spins, s) }
func (l *washerListener) NumberOfRinsesChanged(r appliance.NumberOfRinses) {
	l.rinses = append(l.rinses, r)
}

func TestLaundryWasherControls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	l := &washerListener{}
	srv, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
		Features: appliance.WasherFeatureSpin | appliance.WasherFeatureRinse, Listener: l,
		SpinSpeeds: []string{"Off", "800", "1200"}, SpinSpeedCurrent: ptr(uint8(1)),
		SupportedRinses: []appliance.NumberOfRinses{lwc.NumberOfRinsesNormal, lwc.NumberOfRinsesExtra}, NumberOfRinses: lwc.NumberOfRinsesNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	// An index past the list: CONSTRAINT_ERROR (:115-130); past "max 15"
	// the definition answers the same.
	for _, v := range []uint64{3, 16} {
		if err := srv.MatterWrite(ctx, lwc.AttrSpinSpeedCurrent, v); status(t, err) != im.StatusConstraintError {
			t.Errorf("SpinSpeedCurrent %d: %v", v, err)
		}
	}
	// A defined but unsupported rinse count: INVALID_IN_STATE (:132-145).
	if err := srv.MatterWrite(ctx, lwc.AttrNumberOfRinses, uint64(lwc.NumberOfRinsesMax)); status(t, err) != im.StatusInvalidInState {
		t.Errorf("NumberOfRinses Max: %v", err)
	}
	if err := srv.MatterWrite(ctx, lwc.AttrSpinSpeedCurrent, uint64(2)); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, lwc.AttrSpinSpeedCurrent, nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, lwc.AttrSpinSpeedCurrent, nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, lwc.AttrNumberOfRinses, uint64(lwc.NumberOfRinsesExtra)); err != nil {
		t.Fatal(err)
	}
	if len(l.spins) != 2 || *l.spins[0] != 2 || l.spins[1] != nil || len(l.rinses) != 1 || l.rinses[0] != lwc.NumberOfRinsesExtra {
		t.Errorf("listener heard %v %v", l.spins, l.rinses)
	}
	if srv.SpinSpeedCurrent() != nil || srv.NumberOfRinses() != lwc.NumberOfRinsesExtra {
		t.Errorf("state %v %v", srv.SpinSpeedCurrent(), srv.NumberOfRinses())
	}
	if err := srv.SetSpinSpeedCurrent(ptr(uint8(5))); status(t, err) != im.StatusConstraintError {
		t.Errorf("SetSpinSpeedCurrent 5: %v", err)
	}
	if err := srv.SetSpinSpeedCurrent(ptr(uint8(0))); err != nil {
		t.Error(err)
	}
	if err := srv.SetNumberOfRinses(lwc.NumberOfRinsesNone); status(t, err) != im.StatusInvalidInState {
		t.Errorf("SetNumberOfRinses None: %v", err)
	}
	if err := srv.SetNumberOfRinses(lwc.NumberOfRinsesNormal); err != nil {
		t.Error(err)
	}
	if len(l.spins) != 2 {
		t.Error("a device-side change reached the listener")
	}
	if v, _ := srv.MatterRead(lwc.AttrSpinSpeeds); len(v.([]string)) != 3 {
		t.Errorf("SpinSpeeds %v", v)
	}

	spinOnly, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{Features: appliance.WasherFeatureSpin, SpinSpeeds: []string{"Off"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := spinOnly.SetNumberOfRinses(lwc.NumberOfRinsesNormal); status(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("rinses without RINSE: %v", err)
	}
	if err := spinOnly.MatterWrite(ctx, lwc.AttrNumberOfRinses, uint64(1)); status(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("write rinses without RINSE: %v", err)
	}
	rinseOnly, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{Features: appliance.WasherFeatureRinse, SupportedRinses: []appliance.NumberOfRinses{0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rinseOnly.SetSpinSpeedCurrent(nil); status(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("spin without SPIN: %v", err)
	}
	for name, cfg := range map[string]appliance.WasherConfig{
		"spin index past list": {Features: appliance.WasherFeatureSpin, SpinSpeeds: []string{"Off"}, SpinSpeedCurrent: ptr(uint8(1))},
		"rinses not supported": {Features: appliance.WasherFeatureRinse, SupportedRinses: []appliance.NumberOfRinses{0}, NumberOfRinses: 1},
		"five rinses":          {Features: appliance.WasherFeatureRinse, SupportedRinses: []appliance.NumberOfRinses{0, 1, 2, 3, 0}},
		"no feature":           {},
	} {
		if _, err := appliance.NewLaundryWasherControls(cfg); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

type dryerListener struct{ levels []*appliance.DrynessLevel }

func (l *dryerListener) SelectedDrynessLevelChanged(level *appliance.DrynessLevel) {
	l.levels = append(l.levels, level)
}

func TestLaundryDryerControls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	l := &dryerListener{}
	srv, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{
		SupportedDrynessLevels: []appliance.DrynessLevel{ldc.DrynessLevelLow, ldc.DrynessLevelNormal},
		SelectedDrynessLevel:   ptr(ldc.DrynessLevelNormal), Listener: l,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Unsupported level: CONSTRAINT_ERROR (:47-53).
	if err := srv.MatterWrite(ctx, ldc.AttrSelectedDrynessLevel, uint64(ldc.DrynessLevelMax)); status(t, err) != im.StatusConstraintError {
		t.Errorf("Max: %v", err)
	}
	if err := srv.MatterWrite(ctx, ldc.AttrSelectedDrynessLevel, uint64(ldc.DrynessLevelLow)); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, ldc.AttrSelectedDrynessLevel, nil); err != nil {
		t.Fatal(err)
	}
	if len(l.levels) != 2 || *l.levels[0] != ldc.DrynessLevelLow || l.levels[1] != nil || srv.SelectedDrynessLevel() != nil {
		t.Errorf("listener %v, level %v", l.levels, srv.SelectedDrynessLevel())
	}
	if err := srv.SetSelectedDrynessLevel(ptr(ldc.DrynessLevelExtra)); status(t, err) != im.StatusConstraintError {
		t.Errorf("SetSelected Extra: %v", err)
	}
	if err := srv.SetSelectedDrynessLevel(ptr(ldc.DrynessLevelLow)); err != nil || *srv.SelectedDrynessLevel() != ldc.DrynessLevelLow {
		t.Errorf("SetSelected Low: %v", err)
	}
	if err := srv.SetSelectedDrynessLevel(nil); err != nil {
		t.Error(err)
	}
	if _, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{}); err == nil {
		t.Error("no levels built")
	}
	if _, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{0}, SelectedDrynessLevel: ptr(ldc.DrynessLevelMax)}); err == nil {
		t.Error("unsupported initial level built")
	}
	if _, err := srv.MatterInvoke(ctx, 0, nil); status(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("invoke: %v", err)
	}
}

// TestChangeReporting: each server reports a change with a data-version
// bump and a notification, through the generated server.
func TestChangeReporting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type server interface {
		MatterDataVersion() uint32
		OnMatterAttributesChanged(func([]uint32)) func()
	}
	mw := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerAsNumber)
	washer, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{Features: appliance.WasherFeatureSpin, SpinSpeeds: []string{"Off", "800"}})
	if err != nil {
		t.Fatal(err)
	}
	dryer, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		srv    server
		change func() error
		attr   uint32
	}{
		{mw, func() error { return mw.SetCookTime(60) }, mwo.AttrCookTime},
		{washer, func() error { return washer.SetSpinSpeedCurrent(ptr(uint8(1))) }, lwc.AttrSpinSpeedCurrent},
		{dryer, func() error { return dryer.SetSelectedDrynessLevel(ptr(appliance.DrynessLevel(1))) }, ldc.AttrSelectedDrynessLevel},
	} {
		var seen []uint32
		stop := c.srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids...) })
		before := c.srv.MatterDataVersion()
		if err := c.change(); err != nil {
			t.Fatal(err)
		}
		stop()
		if c.srv.MatterDataVersion() == before || len(seen) != 1 || seen[0] != c.attr {
			t.Errorf("%T: version %d → %d, notified %v", c.srv, before, c.srv.MatterDataVersion(), seen)
		}
	}
	if _, err := washer.MatterInvoke(ctx, 0, nil); status(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("washer invoke: %v", err)
	}
	// A write without a listener stores the value.
	if err := washer.MatterWrite(ctx, lwc.AttrSpinSpeedCurrent, uint64(0)); err != nil || *washer.SpinSpeedCurrent() != 0 {
		t.Errorf("write: %v", err)
	}
	if err := mw.SetPowerSetting(15); status(t, err) != im.StatusConstraintError {
		t.Errorf("SetPowerSetting off step: %v", err)
	}
	watts := newMicrowave(t, &oven{}, appliance.MicrowaveFeaturePowerInWatts)
	if err := watts.SetSelectedWattIndex(1); err != nil {
		t.Error(err)
	}
	if _, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{9}}); err == nil {
		t.Error("undefined dryness level built")
	}
}
