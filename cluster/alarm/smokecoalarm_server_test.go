// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/alarm"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// device is a host alarm: its state, and the two capabilities.
type device struct {
	mu          sync.Mutex
	st          alarm.State
	selfTests   int
	selfTestErr error
	setErr      error
	written     []alarm.Sensitivity
}

func (d *device) SmokeCOState() alarm.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *device) set(f func(*alarm.State)) {
	d.mu.Lock()
	f(&d.st)
	d.mu.Unlock()
}

func (d *device) SelfTest(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.selfTestErr != nil {
		return d.selfTestErr
	}
	d.selfTests++
	d.st.TestInProgress = true
	return nil
}

func (d *device) SetSmokeSensitivityLevel(_ context.Context, level alarm.Sensitivity) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.setErr != nil {
		return d.setErr
	}
	d.written = append(d.written, level)
	d.st.SmokeSensitivityLevel = level
	return nil
}

// readOnlyDevice reports state but can neither test nor be written.
type readOnlyDevice struct{ st alarm.State }

func (r readOnlyDevice) SmokeCOState() alarm.State { return r.st }

// emitted is one recorded event.
type emitted struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

type recorder struct {
	mu     sync.Mutex
	events []emitted
}

func (r *recorder) MatterEmitEvent(endpoint uint16, clusterID, event uint32, data any, priority contract.EventPriority) {
	r.mu.Lock()
	r.events = append(r.events, emitted{endpoint, clusterID, event, data, priority})
	r.mu.Unlock()
}

func (r *recorder) take() []emitted {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func ids(evs []emitted) []uint32 {
	out := make([]uint32, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.event)
	}
	return out
}

const everyOptional = alarm.OptionalDeviceMuted | alarm.OptionalInterconnectSmokeAlarm |
	alarm.OptionalInterconnectCOAlarm | alarm.OptionalContaminationState |
	alarm.OptionalSmokeSensitivityLevel | alarm.OptionalExpiryDate | alarm.OptionalUnmounted

func newFull(t *testing.T, d *device) *alarm.Server {
	t.Helper()
	srv, err := alarm.NewServer(alarm.Config{
		Source:   d,
		Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm,
		Optional: everyOptional,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func statusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sce.MatterStatusCode()
}

func TestNewServerEnforcesConformance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  alarm.Config
		want error
	}{
		{"no feature", alarm.Config{Source: &device{}}, alarm.ErrNoAlarmFeature},
		{"contamination without SMOKE", alarm.Config{Source: &device{}, Features: alarm.FeatureCOAlarm, Optional: alarm.OptionalContaminationState}, alarm.ErrOptionalNeedsSmoke},
		{"sensitivity without SMOKE", alarm.Config{Source: &device{}, Features: alarm.FeatureCOAlarm, Optional: alarm.OptionalSmokeSensitivityLevel}, alarm.ErrOptionalNeedsSmoke},
		{"sensitivity without setter", alarm.Config{Source: readOnlyDevice{}, Features: alarm.FeatureSmokeAlarm, Optional: alarm.OptionalSmokeSensitivityLevel}, alarm.ErrSensitivityNotWritable},
		{"priority names CO without CO", alarm.Config{Source: &device{}, Features: alarm.FeatureSmokeAlarm, ExpressedStatePriority: []alarm.ExpressedState{alarm.ExpressedCOAlarm}}, alarm.ErrPriorityEntry},
		{"priority names Normal", alarm.Config{Source: &device{}, Features: alarm.FeatureSmokeAlarm, ExpressedStatePriority: []alarm.ExpressedState{alarm.ExpressedNormal}}, alarm.ErrPriorityEntry},
		{"priority names an unknown value", alarm.Config{Source: &device{}, Features: alarm.FeatureSmokeAlarm, ExpressedStatePriority: []alarm.ExpressedState{42}}, alarm.ErrPriorityEntry},
	}
	for _, tc := range cases {
		if _, err := alarm.NewServer(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := alarm.NewServer(alarm.Config{Features: 1 << 5}); err == nil {
		t.Error("unknown feature bit accepted")
	}
}

func TestAttributeListFollowsFeaturesAndOptionals(t *testing.T) {
	t.Parallel()
	smokeOnly, err := alarm.NewServer(alarm.Config{Source: readOnlyDevice{}, Features: alarm.FeatureSmokeAlarm})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{0x0, 0x1, 0x3, 0x5, 0x6, 0x7}
	if got := smokeOnly.MatterAttributes(); !slices.Equal(got, want) {
		t.Errorf("SMOKE-only attributes = %v, want %v", got, want)
	}
	if _, ok := smokeOnly.MatterRead(alarm.AttrCOState); ok {
		t.Error("CoState readable without CO")
	}
	full := newFull(t, &device{})
	want = []uint32{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xA, 0xB, 0xC, 0xD}
	if got := full.MatterAttributes(); !slices.Equal(got, want) {
		t.Errorf("full attributes = %v, want %v", got, want)
	}
	if got := full.MatterReportable(); slices.Contains(got, alarm.AttrExpiryDate) || len(got) != len(want)-1 {
		t.Errorf("reportable = %v, want every attribute but the fixed ExpiryDate", got)
	}
}

func TestReadsProjectTheHostState(t *testing.T) {
	t.Parallel()
	d := &device{st: alarm.State{
		SmokeState: alarm.AlarmWarning, COState: alarm.AlarmCritical, BatteryAlert: alarm.AlarmWarning,
		DeviceMuted: alarm.Muted, TestInProgress: true, HardwareFaultAlert: true,
		EndOfServiceAlert: alarm.EndOfServiceExpired, InterconnectSmokeAlarm: alarm.AlarmCritical,
		InterconnectCOAlarm: alarm.AlarmWarning, ContaminationState: alarm.ContaminationLow,
		SmokeSensitivityLevel: alarm.SensitivityLow, ExpiryDate: 123456, Unmounted: true,
	}}
	srv := newFull(t, d)
	want := map[uint32]any{
		alarm.AttrExpressedState:          uint8(alarm.ExpressedSmokeAlarm),
		alarm.AttrSmokeState:              uint8(1),
		alarm.AttrCOState:                 uint8(2),
		alarm.AttrBatteryAlert:            uint8(1),
		alarm.AttrDeviceMuted:             uint8(1),
		alarm.AttrTestInProgress:          true,
		alarm.AttrHardwareFaultAlert:      true,
		alarm.AttrEndOfServiceAlert:       uint8(1),
		alarm.AttrInterconnectSmokeAlarm:  uint8(2),
		alarm.AttrInterconnectCOAlarm:     uint8(1),
		alarm.AttrContaminationState:      uint8(1),
		alarm.AttrSmokeSensitivityLevel:   uint8(2),
		alarm.AttrExpiryDate:              uint32(123456),
		alarm.AttrUnmounted:               true,
		cluster.AttrGlobalFeatureMap:      uint32(3),
		cluster.AttrGlobalClusterRevision: alarm.Revision(),
	}
	for id, w := range want {
		got, ok := srv.MatterRead(id)
		if !ok || got != w {
			t.Errorf("MatterRead(0x%04X) = (%v %T, %v), want (%v %T, true)", id, got, got, ok, w, w)
		}
	}
	if _, ok := srv.MatterRead(0x0E); ok {
		t.Error("an undefined attribute reads as supported")
	}
}

func TestWithoutSourceTheInitialStateOfMatterJSIsReported(t *testing.T) {
	t.Parallel()
	// matter.js SmokeCoAlarmBaseServer.initialize: everything Normal /
	// false; the sensitivity reads Standard.
	srv, err := alarm.NewServer(alarm.Config{Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm, Optional: alarm.OptionalContaminationState})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range srv.MatterAttributes() {
		v, ok := srv.MatterRead(id)
		if !ok {
			t.Fatalf("MatterRead(0x%04X) unsupported", id)
		}
		switch x := v.(type) {
		case uint8:
			if x != 0 {
				t.Errorf("attribute 0x%04X = %d, want the Normal/false initial value", id, x)
			}
		case bool:
			if x {
				t.Errorf("attribute 0x%04X = true, want false", id)
			}
		}
	}
	if got := srv.MatterAcceptedCommands(); len(got) != 0 {
		t.Errorf("AcceptedCommandList without a SelfTester = %v, want empty", got)
	}
	if _, err := srv.MatterInvoke(context.Background(), alarm.CmdSelfTestRequest, nil); statusOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("SelfTestRequest without a SelfTester = %v", err)
	}
}

func TestExpressedStatePriority(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv := newFull(t, d)
	steps := []struct {
		name string
		st   alarm.State
		want alarm.ExpressedState
	}{
		{"nothing", alarm.State{}, alarm.ExpressedNormal},
		{"battery only", alarm.State{BatteryAlert: alarm.AlarmWarning}, alarm.ExpressedBatteryAlert},
		{"end of service beats battery", alarm.State{BatteryAlert: alarm.AlarmCritical, EndOfServiceAlert: alarm.EndOfServiceExpired}, alarm.ExpressedEndOfService},
		{"testing beats end of service", alarm.State{EndOfServiceAlert: alarm.EndOfServiceExpired, TestInProgress: true}, alarm.ExpressedTesting},
		{"hardware fault beats testing", alarm.State{TestInProgress: true, HardwareFaultAlert: true}, alarm.ExpressedHardwareFault},
		{"interconnect CO beats fault", alarm.State{HardwareFaultAlert: true, InterconnectCOAlarm: alarm.AlarmWarning}, alarm.ExpressedInterconnectCO},
		{"CO beats interconnect CO", alarm.State{InterconnectCOAlarm: alarm.AlarmWarning, COState: alarm.AlarmWarning}, alarm.ExpressedCOAlarm},
		{"interconnect smoke beats CO", alarm.State{COState: alarm.AlarmCritical, InterconnectSmokeAlarm: alarm.AlarmWarning}, alarm.ExpressedInterconnectSmoke},
		{"smoke beats everything", alarm.State{InterconnectSmokeAlarm: alarm.AlarmWarning, SmokeState: alarm.AlarmCritical, BatteryAlert: alarm.AlarmCritical}, alarm.ExpressedSmokeAlarm},
		{"inoperative is expressed first", alarm.State{SmokeState: alarm.AlarmCritical, Inoperative: true}, alarm.ExpressedInoperative},
	}
	for _, s := range steps {
		if got := srv.ExpressedStateOf(s.st); got != s.want {
			t.Errorf("%s: ExpressedState = %d, want %d", s.name, got, s.want)
		}
	}

	// A CO-only alarm never expresses SmokeAlarm or an interconnect it
	// does not serve, whatever the host reports.
	coOnly, err := alarm.NewServer(alarm.Config{Source: d, Features: alarm.FeatureCOAlarm})
	if err != nil {
		t.Fatal(err)
	}
	if got := coOnly.ExpressedStateOf(alarm.State{SmokeState: alarm.AlarmCritical, InterconnectSmokeAlarm: alarm.AlarmCritical}); got != alarm.ExpressedNormal {
		t.Errorf("CO-only ExpressedState = %d, want Normal", got)
	}

	// A manufacturer order replaces the default.
	custom, err := alarm.NewServer(alarm.Config{
		Source: d, Features: alarm.FeatureSmokeAlarm,
		ExpressedStatePriority: []alarm.ExpressedState{alarm.ExpressedBatteryAlert, alarm.ExpressedSmokeAlarm},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := custom.ExpressedStateOf(alarm.State{SmokeState: alarm.AlarmWarning, BatteryAlert: alarm.AlarmWarning}); got != alarm.ExpressedBatteryAlert {
		t.Errorf("custom order ExpressedState = %d, want BatteryAlert", got)
	}
	if got := custom.ExpressedStateOf(alarm.State{HardwareFaultAlert: true}); got != alarm.ExpressedNormal {
		t.Errorf("custom order without HardwareFault expresses %d, want Normal", got)
	}
}

func TestSmokeSensitivityLevelWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)
	before := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, uint64(alarm.SensitivityHigh)); err != nil {
		t.Fatalf("write High: %v", err)
	}
	if !slices.Equal(d.written, []alarm.Sensitivity{alarm.SensitivityHigh}) {
		t.Errorf("device received %v", d.written)
	}
	if srv.MatterDataVersion() == before {
		t.Error("DataVersion did not move on a successful write")
	}
	for _, bad := range []any{uint64(3), int64(-1), nil, "high"} {
		if err := srv.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, bad); statusOf(t, err) != im.StatusConstraintError {
			t.Errorf("write %v: %v, want ConstraintError", bad, err)
		}
	}
	if err := srv.MatterWrite(ctx, alarm.AttrExpressedState, uint64(0)); statusOf(t, err) != im.StatusUnsupportedWrite {
		t.Errorf("write ExpressedState: %v, want UnsupportedWrite", err)
	}
	if err := srv.MatterWrite(ctx, 0x0E, uint64(0)); statusOf(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("write undefined: %v, want UnsupportedAttribute", err)
	}
	d.setErr = errors.New("radio down")
	if err := srv.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, uint64(1)); err == nil || !errors.Is(err, d.setErr) {
		t.Errorf("host failure surfaced as %v", err)
	}
	// Without the optional attribute the write path does not exist.
	plain, _ := alarm.NewServer(alarm.Config{Source: d, Features: alarm.FeatureSmokeAlarm})
	if err := plain.MatterWrite(ctx, alarm.AttrSmokeSensitivityLevel, uint64(1)); statusOf(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("write without the attribute: %v, want UnsupportedAttribute", err)
	}
	if srv.MinWritePrivilege(alarm.AttrSmokeSensitivityLevel) != 4 || srv.MinWritePrivilege(alarm.AttrExpressedState) != 3 {
		t.Error("SmokeSensitivityLevel is written with Manage, the rest with the Operate default")
	}
}

func TestSelfTestRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{alarm.CmdSelfTestRequest}) {
		t.Fatalf("AcceptedCommandList = %v", got)
	}
	if got := srv.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("GeneratedCommandList = %v, want empty (status response)", got)
	}

	// Busy while alarming or testing (resource :201-204).
	for name, st := range map[string]alarm.State{
		"smoke":              {SmokeState: alarm.AlarmWarning},
		"co":                 {COState: alarm.AlarmCritical},
		"testing":            {TestInProgress: true},
		"interconnect smoke": {InterconnectSmokeAlarm: alarm.AlarmWarning},
		"interconnect co":    {InterconnectCOAlarm: alarm.AlarmWarning},
	} {
		d.set(func(s *alarm.State) { *s = st })
		if _, err := srv.MatterInvoke(ctx, alarm.CmdSelfTestRequest, nil); statusOf(t, err) != im.StatusBusy {
			t.Errorf("%s: SelfTestRequest = %v, want Busy", name, err)
		}
	}
	if d.selfTests != 0 {
		t.Fatalf("a busy self-test reached the device %d times", d.selfTests)
	}

	// A battery alert does not block the test.
	d.set(func(s *alarm.State) { *s = alarm.State{BatteryAlert: alarm.AlarmWarning} })
	srv.Refresh()
	rec.take()
	if _, err := srv.MatterInvoke(ctx, alarm.CmdSelfTestRequest, nil); err != nil {
		t.Fatalf("SelfTestRequest: %v", err)
	}
	if d.selfTests != 1 {
		t.Fatalf("self-tests = %d, want 1", d.selfTests)
	}
	if v, _ := srv.MatterRead(alarm.AttrExpressedState); v != uint8(alarm.ExpressedTesting) {
		t.Errorf("ExpressedState during the test = %v, want Testing", v)
	}
	// The test ends: SelfTestComplete.
	d.set(func(s *alarm.State) { s.TestInProgress = false })
	srv.Refresh()
	if got := ids(rec.take()); !slices.Equal(got, []uint32{alarm.EventSelfTestComplete}) {
		t.Errorf("events at test end = %v, want SelfTestComplete", got)
	}

	d.selfTestErr = errors.New("horn stuck")
	if _, err := srv.MatterInvoke(ctx, alarm.CmdSelfTestRequest, nil); !errors.Is(err, d.selfTestErr) {
		t.Errorf("host failure surfaced as %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, 0x01, nil); statusOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v", err)
	}
}

func TestRefreshEmitsTheSpecifiedEvents(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv := newFull(t, d)
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(9)

	step := func(name string, change func(*alarm.State), want ...uint32) []emitted {
		t.Helper()
		d.set(change)
		srv.Refresh()
		got := rec.take()
		if !slices.Equal(ids(got), want) {
			t.Errorf("%s: events %v, want %v", name, ids(got), want)
		}
		for _, e := range got {
			if e.endpoint != 9 || e.cluster != alarm.ClusterID {
				t.Errorf("%s: event addressed to %d/0x%04X", name, e.endpoint, e.cluster)
			}
		}
		return got
	}

	got := step("smoke warning", func(s *alarm.State) { s.SmokeState = alarm.AlarmWarning }, alarm.EventSmokeAlarm)
	if len(got) == 1 && (got[0].data != alarm.AlarmSeverityEvent{AlarmSeverityLevel: alarm.AlarmWarning} || got[0].priority != contract.EventPriorityCritical) {
		t.Errorf("SmokeAlarm = %+v", got[0])
	}
	step("unchanged", func(*alarm.State) {})
	step("warning to critical", func(s *alarm.State) { s.SmokeState = alarm.AlarmCritical }, alarm.EventSmokeAlarm)
	step("smoke clears: all clear", func(s *alarm.State) { s.SmokeState = alarm.AlarmNormal }, alarm.EventAllClear)
	step("co", func(s *alarm.State) { s.COState = alarm.AlarmCritical }, alarm.EventCOAlarm)
	step("co clears with battery low: no all clear", func(s *alarm.State) {
		s.COState = alarm.AlarmNormal
		s.BatteryAlert = alarm.AlarmWarning
	}, alarm.EventLowBattery)
	got = step("hardware fault + end of service", func(s *alarm.State) {
		s.HardwareFaultAlert = true
		s.EndOfServiceAlert = alarm.EndOfServiceExpired
	}, alarm.EventHardwareFault, alarm.EventEndOfService)
	for _, e := range got {
		if e.data != (clusterwire.FieldlessEvent{}) || e.priority != contract.EventPriorityInfo {
			t.Errorf("fieldless event = %+v", e)
		}
	}
	step("muted", func(s *alarm.State) { s.DeviceMuted = alarm.Muted }, alarm.EventAlarmMuted)
	step("mute ended", func(s *alarm.State) { s.DeviceMuted = alarm.NotMuted }, alarm.EventMuteEnded)
	step("interconnect smoke", func(s *alarm.State) { s.InterconnectSmokeAlarm = alarm.AlarmWarning }, alarm.EventInterconnectSmokeAlarm)
	step("interconnect co", func(s *alarm.State) { s.InterconnectCOAlarm = alarm.AlarmCritical }, alarm.EventInterconnectCOAlarm)
	step("everything clears", func(s *alarm.State) { *s = alarm.State{} }, alarm.EventAllClear)
}

func TestRefreshWithoutEmitterAdvancesTheBaseline(t *testing.T) {
	t.Parallel()
	d := &device{st: alarm.State{SmokeState: alarm.AlarmCritical}}
	srv := newFull(t, d)
	srv.Refresh() // nobody listening
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	srv.Refresh()
	if got := rec.take(); len(got) != 0 {
		t.Errorf("an unobserved change was replayed: %v", ids(got))
	}
}

func TestEventsNotServedAreNeverEmitted(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv, err := alarm.NewServer(alarm.Config{Source: d, Features: alarm.FeatureCOAlarm})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	d.set(func(s *alarm.State) {
		s.SmokeState = alarm.AlarmCritical
		s.DeviceMuted = alarm.Muted
		s.InterconnectCOAlarm = alarm.AlarmCritical
		s.InterconnectSmokeAlarm = alarm.AlarmCritical
	})
	srv.Refresh()
	if got := rec.take(); len(got) != 0 {
		t.Errorf("events outside the configuration were emitted: %v", ids(got))
	}
	want := []uint32{alarm.EventCOAlarm, alarm.EventLowBattery, alarm.EventHardwareFault, alarm.EventEndOfService, alarm.EventSelfTestComplete, alarm.EventAllClear}
	if got := srv.MatterEvents(); !slices.Equal(got, want) {
		t.Errorf("EventList = %v, want %v", got, want)
	}
	full := newFull(t, &device{})
	if got := full.MatterEvents(); len(got) != 11 {
		t.Errorf("full EventList = %v, want all eleven events", got)
	}
}

func TestHostDataVersionTrackerIsUsed(t *testing.T) {
	t.Parallel()
	var tracker cluster.DataVersionTracker
	srv, err := alarm.NewServer(alarm.Config{Source: &device{}, Features: alarm.FeatureSmokeAlarm, Optional: alarm.OptionalSmokeSensitivityLevel, DataVersion: &tracker})
	if err != nil {
		t.Fatal(err)
	}
	before := tracker.Current()
	if err := srv.MatterWrite(context.Background(), alarm.AttrSmokeSensitivityLevel, uint64(1)); err != nil {
		t.Fatal(err)
	}
	if tracker.Current() == before || srv.MatterDataVersion() != tracker.Current() {
		t.Error("the host's tracker did not carry the write")
	}
	if srv.MatterClusterID() != alarm.ClusterID {
		t.Error("cluster id")
	}
}
