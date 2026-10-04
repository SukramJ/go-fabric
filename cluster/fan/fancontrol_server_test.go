// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/fan"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// device is a host fan. ApplyFanSettings applies the resolved settings
// the way a device with a simple mode mapping would.
type device struct {
	mu       sync.Mutex
	st       fan.State
	applied  []fan.Settings
	rock     []fan.RockBitmap
	wind     []fan.WindBitmap
	dirs     []fan.AirflowDirection
	applyErr error
}

func (d *device) FanState() fan.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *device) ApplyFanSettings(_ context.Context, s fan.Settings) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.applyErr != nil {
		return d.applyErr
	}
	d.applied = append(d.applied, s)
	if s.FanMode != nil {
		d.st.FanMode = *s.FanMode
	}
	if s.PercentSetting != nil {
		d.st.PercentSetting = settingPtr(*s.PercentSetting)
	}
	if s.SpeedSetting != nil {
		d.st.SpeedSetting = settingPtr(*s.SpeedSetting)
	}
	return nil
}

func settingPtr(s fan.Setting) *uint8 {
	if s.Null {
		return nil
	}
	v := s.Value
	return &v
}

func (d *device) SetRockSetting(_ context.Context, r fan.RockBitmap) error {
	d.rock = append(d.rock, r)
	d.st.RockSetting = r
	return nil
}

func (d *device) SetWindSetting(_ context.Context, w fan.WindBitmap) error {
	d.wind = append(d.wind, w)
	d.st.WindSetting = w
	return nil
}

func (d *device) SetAirflowDirection(_ context.Context, a fan.AirflowDirection) error {
	d.dirs = append(d.dirs, a)
	d.st.AirflowDirection = a
	return nil
}

func (d *device) last() fan.Settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.applied[len(d.applied)-1]
}

// plainDevice has only the mandatory port.
type plainDevice struct{ d *device }

func (p plainDevice) FanState() fan.State { return p.d.FanState() }
func (p plainDevice) ApplyFanSettings(ctx context.Context, s fan.Settings) error {
	return p.d.ApplyFanSettings(ctx, s)
}

// stepper takes Step over.
type stepper struct {
	plainDevice
	steps []clusterwire.FanStepRequest
	err   error
}

func (s *stepper) Step(_ context.Context, req clusterwire.FanStepRequest) error {
	if s.err != nil {
		return s.err
	}
	s.steps = append(s.steps, req)
	return nil
}

func u8(v uint8) *uint8 { return &v }

func statusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sce.MatterStatusCode()
}

const allFeatures = fan.FeatureMultiSpeed | fan.FeatureAuto | fan.FeatureRocking | fan.FeatureWind |
	fan.FeatureStep | fan.FeatureAirflowDirection

func newFull(t *testing.T, d *device) *fan.Server {
	t.Helper()
	srv, err := fan.NewServer(fan.Config{
		Source: d, Features: allFeatures, Sequence: fan.SequenceOffLowMedHighAuto,
		SpeedMax: 10, RockSupport: fan.RockLeftRight | fan.RockUpDown, WindSupport: fan.WindNatural,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func newBasic(t *testing.T, d *device, seq fan.Sequence, features fan.Feature) *fan.Server {
	t.Helper()
	srv, err := fan.NewServer(fan.Config{Source: plainDevice{d}, Features: features, Sequence: seq, SpeedMax: 3})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestNewServerEnforcesConformanceAndConstraints(t *testing.T) {
	t.Parallel()
	d := &device{}
	cases := []struct {
		name string
		cfg  fan.Config
		want error
	}{
		{"no source", fan.Config{}, fan.ErrNoSource},
		{"unknown feature", fan.Config{Source: d, Features: 1 << 6}, fan.ErrUnknownFeature},
		{"auto sequence without AUT", fan.Config{Source: d, Sequence: fan.SequenceOffHighAuto}, fan.ErrSequence},
		{"plain sequence with AUT", fan.Config{Source: d, Features: fan.FeatureAuto, Sequence: fan.SequenceOffHigh}, fan.ErrSequence},
		{"unknown sequence", fan.Config{Source: d, Sequence: 6}, fan.ErrSequence},
		{"SPD without SpeedMax", fan.Config{Source: d, Features: fan.FeatureMultiSpeed}, fan.ErrSpeedMax},
		{"SPD SpeedMax 101", fan.Config{Source: d, Features: fan.FeatureMultiSpeed, SpeedMax: 101}, fan.ErrSpeedMax},
		{"RCK without support", fan.Config{Source: d, Features: fan.FeatureRocking}, fan.ErrRockSupport},
		{"RCK with unknown bit", fan.Config{Source: d, Features: fan.FeatureRocking, RockSupport: 1 << 3}, fan.ErrRockSupport},
		{"RCK without setter", fan.Config{Source: plainDevice{d}, Features: fan.FeatureRocking, RockSupport: fan.RockRound}, fan.ErrRockSupport},
		{"WND without support", fan.Config{Source: d, Features: fan.FeatureWind}, fan.ErrWindSupport},
		{"WND without setter", fan.Config{Source: plainDevice{d}, Features: fan.FeatureWind, WindSupport: fan.WindSleep}, fan.ErrWindSupport},
		{"DIR without setter", fan.Config{Source: plainDevice{d}, Features: fan.FeatureAirflowDirection}, fan.ErrAirflowDirection},
		{"ExtractorHood with RCK", fan.Config{Source: d, DeviceType: fan.DeviceTypeExtractorHood, Features: fan.FeatureRocking, RockSupport: fan.RockRound}, fan.ErrDeviceTypeFeature},
		{"ExtractorHood with DIR", fan.Config{Source: d, DeviceType: fan.DeviceTypeExtractorHood, Features: fan.FeatureAirflowDirection}, fan.ErrDeviceTypeFeature},
	}
	for _, tc := range cases {
		if _, err := fan.NewServer(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := fan.NewServer(fan.Config{Source: d, DeviceType: fan.DeviceTypeExtractorHood, Features: fan.FeatureMultiSpeed | fan.FeatureStep, SpeedMax: 4}); err != nil {
		t.Errorf("ExtractorHood with SPD + STEP rejected: %v", err)
	}
}

func TestAttributeSurfaceFollowsFeatures(t *testing.T) {
	t.Parallel()
	d := &device{}
	basic := newBasic(t, d, fan.SequenceOffLowHigh, 0)
	if got := basic.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 3}) {
		t.Errorf("basic attributes = %v", got)
	}
	if got := basic.MatterReportable(); !slices.Equal(got, []uint32{0, 2, 3}) {
		t.Errorf("basic reportable = %v", got)
	}
	if got := basic.MatterAcceptedCommands(); len(got) != 0 {
		t.Errorf("basic accepted commands = %v", got)
	}
	full := newFull(t, d)
	if got := full.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}) {
		t.Errorf("full attributes = %v", got)
	}
	if got := full.MatterReportable(); !slices.Equal(got, []uint32{0, 2, 3, 5, 6, 8, 10, 11}) {
		t.Errorf("full reportable = %v", got)
	}
	if got := full.MatterAcceptedCommands(); !slices.Equal(got, []uint32{fan.CmdStep}) {
		t.Errorf("full accepted commands = %v", got)
	}
	if got := full.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("generated commands = %v", got)
	}
	if _, ok := basic.MatterRead(fan.AttrSpeedMax); ok {
		t.Error("SpeedMax readable without SPD")
	}
	if full.MatterClusterID() != fan.ClusterID || full.MatterDataVersion() == 0 {
		t.Error("identity")
	}
}

func TestReadsProjectAndClampTheHostState(t *testing.T) {
	t.Parallel()
	d := &device{st: fan.State{
		FanMode: fan.FanModeMedium, PercentSetting: u8(150), PercentCurrent: 120,
		SpeedSetting: u8(42), SpeedCurrent: 7, RockSetting: fan.RockLeftRight | fan.RockRound,
		WindSetting: fan.WindNatural | fan.WindSleep, AirflowDirection: fan.AirflowReverse,
	}}
	srv := newFull(t, d)
	want := map[uint32]any{
		fan.AttrFanMode:                   uint8(fan.FanModeMedium),
		fan.AttrFanModeSequence:           uint8(fan.SequenceOffLowMedHighAuto),
		fan.AttrPercentSetting:            uint8(100),
		fan.AttrPercentCurrent:            uint8(100),
		fan.AttrSpeedMax:                  uint8(10),
		fan.AttrSpeedSetting:              uint8(10),
		fan.AttrSpeedCurrent:              uint8(7),
		fan.AttrRockSupport:               uint8(fan.RockLeftRight | fan.RockUpDown),
		fan.AttrRockSetting:               uint8(fan.RockLeftRight),
		fan.AttrWindSupport:               uint8(fan.WindNatural),
		fan.AttrWindSetting:               uint8(fan.WindNatural),
		fan.AttrAirflowDirection:          uint8(fan.AirflowReverse),
		cluster.AttrGlobalFeatureMap:      uint32(allFeatures),
		cluster.AttrGlobalClusterRevision: fan.Revision(),
	}
	for id, w := range want {
		if got, ok := srv.MatterRead(id); !ok || got != w {
			t.Errorf("MatterRead(0x%04X) = (%v %T, %v), want %v %T", id, got, got, ok, w, w)
		}
	}
	d.st.PercentSetting, d.st.SpeedSetting = nil, nil
	for _, id := range []uint32{fan.AttrPercentSetting, fan.AttrSpeedSetting} {
		if got, ok := srv.MatterRead(id); !ok || got != nil {
			t.Errorf("null setting 0x%04X read (%v, %v)", id, got, ok)
		}
	}
	if _, ok := srv.MatterRead(0x0C); ok {
		t.Error("undefined attribute readable")
	}
}

func TestFanModeWriteCouplesPercentAndSpeed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)

	if err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeOff)); err != nil {
		t.Fatal(err)
	}
	if s := d.last(); *s.FanMode != fan.FanModeOff || *s.PercentSetting != (fan.Setting{}) || *s.SpeedSetting != (fan.Setting{}) {
		t.Errorf("Off resolved to %+v, want percent 0 / speed 0", s)
	}
	if err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeAuto)); err != nil {
		t.Fatal(err)
	}
	if s := d.last(); *s.FanMode != fan.FanModeAuto || !s.PercentSetting.Null || !s.SpeedSetting.Null {
		t.Errorf("Auto resolved to %+v, want null / null", s)
	}
	if err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeMedium)); err != nil {
		t.Fatal(err)
	}
	if s := d.last(); *s.FanMode != fan.FanModeMedium || s.PercentSetting != nil || s.SpeedSetting != nil {
		t.Errorf("Medium resolved to %+v, want the device's own mapping", s)
	}
	// Deprecated values: On → High, Smart → Auto where offered.
	_ = srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeOn))
	if s := d.last(); *s.FanMode != fan.FanModeHigh {
		t.Errorf("On resolved to %d, want High", *s.FanMode)
	}
	_ = srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeSmart))
	if s := d.last(); *s.FanMode != fan.FanModeAuto {
		t.Errorf("Smart resolved to %d, want Auto", *s.FanMode)
	}
	if err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(7)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("FanMode 7: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrFanMode, nil); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("FanMode null: %v", err)
	}

	// Without Auto: Smart becomes High, Auto is refused, and the speed
	// field stays nil without MultiSpeed.
	plain := newBasic(t, d, fan.SequenceOffLowHigh, 0)
	_ = plain.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeSmart))
	if s := d.last(); *s.FanMode != fan.FanModeHigh {
		t.Errorf("Smart without Auto resolved to %d, want High", *s.FanMode)
	}
	if err := plain.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeAuto)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("Auto without AUT: %v", err)
	}
	_ = plain.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeOff))
	if s := d.last(); s.SpeedSetting != nil {
		t.Errorf("Off without SPD carried a SpeedSetting %+v", s.SpeedSetting)
	}
}

func TestFanModeMustBeOfferedByTheSequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	offered := map[fan.Sequence][]fan.FanMode{
		fan.SequenceOffLowMedHigh:     {fan.FanModeOff, fan.FanModeLow, fan.FanModeMedium, fan.FanModeHigh},
		fan.SequenceOffLowHigh:        {fan.FanModeOff, fan.FanModeLow, fan.FanModeHigh},
		fan.SequenceOffLowMedHighAuto: {fan.FanModeOff, fan.FanModeLow, fan.FanModeMedium, fan.FanModeHigh, fan.FanModeAuto},
		fan.SequenceOffLowHighAuto:    {fan.FanModeOff, fan.FanModeLow, fan.FanModeHigh, fan.FanModeAuto},
		fan.SequenceOffHighAuto:       {fan.FanModeOff, fan.FanModeHigh, fan.FanModeAuto},
		fan.SequenceOffHigh:           {fan.FanModeOff, fan.FanModeHigh},
	}
	for seq, modes := range offered {
		features := fan.Feature(0)
		if slices.Contains(modes, fan.FanModeAuto) {
			features = fan.FeatureAuto
		}
		srv := newBasic(t, &device{}, seq, features)
		for _, m := range []fan.FanMode{fan.FanModeOff, fan.FanModeLow, fan.FanModeMedium, fan.FanModeHigh, fan.FanModeAuto} {
			err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(m))
			if slices.Contains(modes, m) != (err == nil) {
				t.Errorf("sequence %d mode %d: err = %v, offered = %v", seq, m, err, slices.Contains(modes, m))
			}
		}
	}
}

func TestPercentAndSpeedWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d) // SpeedMax 10

	cases := []struct {
		percent   uint8
		wantSpeed uint8
		wantOff   bool
	}{
		{0, 0, true}, {1, 1, false}, {10, 1, false}, {11, 2, false}, {50, 5, false}, {99, 10, false}, {100, 10, false},
	}
	for _, c := range cases {
		if err := srv.MatterWrite(ctx, fan.AttrPercentSetting, uint64(c.percent)); err != nil {
			t.Fatalf("percent %d: %v", c.percent, err)
		}
		s := d.last()
		if s.SpeedSetting.Value != c.wantSpeed || s.PercentSetting.Value != c.percent || (s.FanMode != nil) != c.wantOff {
			t.Errorf("percent %d resolved to %+v (speed %d), want speed %d off=%v", c.percent, s, s.SpeedSetting.Value, c.wantSpeed, c.wantOff)
		}
	}
	speeds := []struct {
		speed       uint8
		wantPercent uint8
	}{{0, 0}, {1, 10}, {3, 30}, {10, 100}}
	for _, c := range speeds {
		if err := srv.MatterWrite(ctx, fan.AttrSpeedSetting, uint64(c.speed)); err != nil {
			t.Fatalf("speed %d: %v", c.speed, err)
		}
		if s := d.last(); s.PercentSetting.Value != c.wantPercent || (s.FanMode != nil) != (c.speed == 0) {
			t.Errorf("speed %d resolved to %+v, want percent %d", c.speed, s, c.wantPercent)
		}
	}

	// Null writes leave the attributes unchanged and succeed.
	n := len(d.applied)
	for _, id := range []uint32{fan.AttrPercentSetting, fan.AttrSpeedSetting} {
		if err := srv.MatterWrite(ctx, id, nil); err != nil {
			t.Errorf("null write 0x%04X: %v", id, err)
		}
	}
	if len(d.applied) != n {
		t.Error("a null write reached the device")
	}
	if err := srv.MatterWrite(ctx, fan.AttrPercentSetting, uint64(101)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("percent 101: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrSpeedSetting, uint64(11)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("speed 11 > SpeedMax: %v", err)
	}
	// The floor of the speed rule: SpeedMax 3, speed 1 → 33 %.
	three := newBasic(t, d, fan.SequenceOffLowHigh, fan.FeatureMultiSpeed)
	_ = three.MatterWrite(ctx, fan.AttrSpeedSetting, uint64(1))
	if s := d.last(); s.PercentSetting.Value != 33 {
		t.Errorf("speed 1/3 → percent %d, want 33", s.PercentSetting.Value)
	}
	_ = three.MatterWrite(ctx, fan.AttrPercentSetting, uint64(34))
	if s := d.last(); s.SpeedSetting.Value != 2 {
		t.Errorf("percent 34 of SpeedMax 3 → speed %d, want 2 (ceiling)", s.SpeedSetting.Value)
	}
}

func TestBitmapAndDirectionWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d) // RockSupport LR|UD, WindSupport Natural
	if err := srv.MatterWrite(ctx, fan.AttrRockSetting, uint64(fan.RockUpDown)); err != nil || d.rock[0] != fan.RockUpDown {
		t.Errorf("RockSetting UpDown: %v %v", err, d.rock)
	}
	if err := srv.MatterWrite(ctx, fan.AttrRockSetting, uint64(fan.RockRound)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("RockSetting outside support: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrWindSetting, uint64(fan.WindNatural)); err != nil || d.wind[0] != fan.WindNatural {
		t.Errorf("WindSetting Natural: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrWindSetting, uint64(fan.WindSleep)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("WindSetting outside support: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrAirflowDirection, uint64(fan.AirflowReverse)); err != nil || d.dirs[0] != fan.AirflowReverse {
		t.Errorf("AirflowDirection Reverse: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrAirflowDirection, uint64(2)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("AirflowDirection 2: %v", err)
	}
	if err := srv.MatterWrite(ctx, fan.AttrPercentCurrent, uint64(1)); statusOf(t, err) != im.StatusUnsupportedWrite {
		t.Errorf("PercentCurrent write: %v", err)
	}
	basic := newBasic(t, d, fan.SequenceOffHigh, 0)
	if err := basic.MatterWrite(ctx, fan.AttrSpeedSetting, uint64(1)); statusOf(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("SpeedSetting without SPD: %v", err)
	}
}

func TestHostRefusalSurfaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{applyErr: fan.ErrInvalidInState}
	srv := newFull(t, d)
	err := srv.MatterWrite(ctx, fan.AttrFanMode, uint64(fan.FanModeHigh))
	if statusOf(t, err) != im.StatusInvalidInState {
		t.Errorf("host refusal: %v, want InvalidInState", err)
	}
	d.applyErr = errors.New("bus error")
	if err := srv.MatterWrite(ctx, fan.AttrPercentSetting, uint64(5)); !errors.Is(err, d.applyErr) {
		t.Errorf("host failure: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, clusterwire.FanStepRequest{}); !errors.Is(err, d.applyErr) {
		t.Errorf("Step host failure: %v", err)
	}
}

func TestDataVersionMovesOnWrites(t *testing.T) {
	t.Parallel()
	var tracker cluster.DataVersionTracker
	srv, err := fan.NewServer(fan.Config{Source: &device{}, Sequence: fan.SequenceOffHigh, DataVersion: &tracker})
	if err != nil {
		t.Fatal(err)
	}
	before := tracker.Current()
	if err := srv.MatterWrite(context.Background(), fan.AttrFanMode, uint64(fan.FanModeHigh)); err != nil {
		t.Fatal(err)
	}
	if tracker.Current() == before || srv.MatterDataVersion() != tracker.Current() {
		t.Error("the host tracker did not move")
	}
}

func step(dir uint8, wrap, lowestOff bool) clusterwire.FanStepRequest {
	return clusterwire.FanStepRequest{Direction: dir, Wrap: wrap, LowestOff: lowestOff}
}

func TestDefaultStepAlongSpeed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name  string
		from  *uint8
		cur   uint8
		req   clusterwire.FanStepRequest
		speed uint8
	}{
		{"up", u8(2), 0, step(fan.StepIncrease, false, true), 3},
		{"up at top stays", u8(10), 0, step(fan.StepIncrease, false, true), 10},
		{"up at top wraps to off", u8(10), 0, step(fan.StepIncrease, true, true), 0},
		{"up at top wraps to 1", u8(10), 0, step(fan.StepIncrease, true, false), 1},
		{"down", u8(5), 0, step(fan.StepDecrease, false, true), 4},
		{"down to off", u8(1), 0, step(fan.StepDecrease, false, true), 0},
		{"down stops at 1 without LowestOff", u8(1), 0, step(fan.StepDecrease, false, false), 1},
		{"down at bottom wraps", u8(0), 0, step(fan.StepDecrease, true, true), 10},
		{"null setting steps from SpeedCurrent", nil, 6, step(fan.StepIncrease, false, true), 7},
		{"off without LowestOff starts at 1", u8(0), 0, step(fan.StepIncrease, false, false), 2},
	}
	for _, c := range cases {
		d := &device{st: fan.State{SpeedSetting: c.from, SpeedCurrent: c.cur}}
		srv := newFull(t, d)
		if _, err := srv.MatterInvoke(ctx, fan.CmdStep, c.req); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := d.last().SpeedSetting.Value; got != c.speed {
			t.Errorf("%s: speed %d, want %d", c.name, got, c.speed)
		}
	}
}

func TestDefaultStepAlongModes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name string
		seq  fan.Sequence
		from fan.FanMode
		req  clusterwire.FanStepRequest
		want fan.FanMode
	}{
		// The specification's own example: sequence 0, Medium, Increase → High.
		{"spec example", fan.SequenceOffLowMedHigh, fan.FanModeMedium, step(fan.StepIncrease, false, true), fan.FanModeHigh},
		{"low skips the missing medium", fan.SequenceOffLowHigh, fan.FanModeLow, step(fan.StepIncrease, false, true), fan.FanModeHigh},
		{"high stays", fan.SequenceOffLowMedHigh, fan.FanModeHigh, step(fan.StepIncrease, false, true), fan.FanModeHigh},
		{"high wraps to off", fan.SequenceOffLowMedHigh, fan.FanModeHigh, step(fan.StepIncrease, true, true), fan.FanModeOff},
		{"high wraps to low without LowestOff", fan.SequenceOffLowMedHigh, fan.FanModeHigh, step(fan.StepIncrease, true, false), fan.FanModeLow},
		{"off up to low", fan.SequenceOffLowMedHigh, fan.FanModeOff, step(fan.StepIncrease, false, false), fan.FanModeLow},
		{"low down to off", fan.SequenceOffLowMedHigh, fan.FanModeLow, step(fan.StepDecrease, false, true), fan.FanModeOff},
		{"low stays without LowestOff", fan.SequenceOffLowMedHigh, fan.FanModeLow, step(fan.StepDecrease, false, false), fan.FanModeLow},
		{"off stays without LowestOff", fan.SequenceOffLowMedHigh, fan.FanModeOff, step(fan.StepDecrease, false, false), fan.FanModeOff},
		{"off wraps down to high", fan.SequenceOffLowMedHigh, fan.FanModeOff, step(fan.StepDecrease, true, true), fan.FanModeHigh},
		{"auto down to high", fan.SequenceOffLowMedHighAuto, fan.FanModeAuto, step(fan.StepDecrease, false, true), fan.FanModeHigh},
		{"auto up stays auto", fan.SequenceOffLowMedHighAuto, fan.FanModeAuto, step(fan.StepIncrease, false, true), fan.FanModeAuto},
		{"auto up wraps to off", fan.SequenceOffLowMedHighAuto, fan.FanModeAuto, step(fan.StepIncrease, true, true), fan.FanModeOff},
		{"off-high only", fan.SequenceOffHigh, fan.FanModeOff, step(fan.StepIncrease, false, true), fan.FanModeHigh},
	}
	for _, c := range cases {
		features := fan.FeatureStep
		if c.seq == fan.SequenceOffLowMedHighAuto {
			features |= fan.FeatureAuto
		}
		d := &device{st: fan.State{FanMode: c.from}}
		srv := newBasic(t, d, c.seq, features)
		if _, err := srv.MatterInvoke(ctx, fan.CmdStep, c.req); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := *d.last().FanMode; got != c.want {
			t.Errorf("%s: mode %d, want %d", c.name, got, c.want)
		}
	}
}

func TestStepPayloadsAndStepper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	st := &stepper{plainDevice: plainDevice{d}}
	srv, err := fan.NewServer(fan.Config{Source: st, Features: fan.FeatureStep, Sequence: fan.SequenceOffHigh})
	if err != nil {
		t.Fatal(err)
	}
	// The generic tag map a host decoder hands over: defaults applied.
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, map[uint8]any{0: uint64(1)}); err != nil {
		t.Fatal(err)
	}
	if got := st.steps[0]; got != (clusterwire.FanStepRequest{Direction: 1, Wrap: false, LowestOff: true}) {
		t.Errorf("tag-map step = %+v", got)
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, map[uint8]any{0: uint64(0), 1: true, 2: false}); err != nil || st.steps[1] != step(0, true, false) {
		t.Errorf("tag-map with fields: %v %+v", err, st.steps)
	}
	for name, fields := range map[string]any{
		"no direction":   map[uint8]any{1: true},
		"direction text": map[uint8]any{0: "up"},
		"nil":            nil,
	} {
		if _, err := srv.MatterInvoke(ctx, fan.CmdStep, fields); statusOf(t, err) != im.StatusInvalidCommand {
			t.Errorf("%s: %v, want InvalidCommand", name, err)
		}
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, clusterwire.FanStepRequest{Direction: 2}); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("direction 2: %v, want ConstraintError", err)
	}
	st.err = errors.New("motor")
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, clusterwire.FanStepRequest{}); !errors.Is(err, st.err) {
		t.Errorf("stepper failure: %v", err)
	}
	if len(d.applied) != 0 {
		t.Error("a Stepper host also received the default step")
	}
	noStep := newBasic(t, d, fan.SequenceOffHigh, 0)
	if _, err := noStep.MatterInvoke(ctx, fan.CmdStep, clusterwire.FanStepRequest{}); statusOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("Step without STEP: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, 0x01, nil); statusOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v", err)
	}
}
