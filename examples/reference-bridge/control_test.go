// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	ovendef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// TestAppPipeCommandsReachTheDevices drives every app-pipe command the
// daemon documents through the same parser the FIFO feeds, against a
// mounted fleet, and checks each lands on its device. An unknown command
// is refused, never ignored.
func TestAppPipeCommandsReachTheDevices(t *testing.T) {
	f, _ := startFleetBridge(t)
	lines := []string{
		`{"Name":"SimulateMultiPress","EndpointId":20,"MultiPressNumPresses":1}`,
		`{"Name":"SimulateLongPress","EndpointId":20}`,
		`{"Name":"SimulateMultiPress","EndpointId":20,"ButtonId":1,"MultiPressPressedTimeMillis":1,"MultiPressReleasedTimeMillis":1,"MultiPressNumPresses":3}`,
		`{"Name":"SimulateSwitchIdle","EndpointId":20}`,
		`{"Name":"SetBooleanState","EndpointId":19,"NewState":false}`,
		`{"Name":"SetOccupancy","EndpointId":18,"Occupancy":1}`,
		`{"Name":"SetSensorValue","Device":"humidity","Value":61.5}`,
		`{"Name":"SetSensorValue","Device":"flow","Value":2.25}`,
		`{"Name":"SetLocalTemperature","Value":2230}`,
		`{"Name":"SetLockJammed","Jammed":true}`,
		`{"Name":"PumpEvent","Event":"DryRunning"}`,
		`{"Name":"SetValveState","Value":1}`,
		`{"Name":"SetSelectorMode","Value":2}`,
		`{"Name":"SetSpeakerLevel","Value":42}`,
		`{"Name":"SetUnmounted","EndpointId":9,"Unmounted":1}`,
		`{"Name":"LongPress","EndpointId":9,"NewPosition":0}`,
		`{"Name":"OperationalStateChange","Device":"Generic","Operation":"Start"}`,
		`{"Name":"OperationalStateChange","Device":"Generic","Operation":"OnFault","Param":1}`,
		`{"Name":"OperationCompletion"}`,
		`{"Name":"ErrorEvent","Error":"Stuck"}`,
		`{"Name":"Docked"}`,
		`{"Name":"NotACommand"}`,
		`not json`,
	}
	var logs bytes.Buffer
	readPipe(f, strings.NewReader(strings.Join(lines, "\n")), slog.New(slog.NewTextHandler(&logs, nil)))
	out := logs.String()
	if got := strings.Count(out, "apppipe.applied"); got != len(lines)-2 {
		t.Errorf("%d commands applied, want %d\n%s", got, len(lines)-2, out)
	}
	if got := strings.Count(out, "apppipe.error"); got != 2 {
		t.Errorf("%d commands refused, want 2 (the unknown name and the malformed line)\n%s", got, out)
	}
	if v := f.humidity.reading(); v != 61.5 {
		t.Errorf("humidity = %v", v)
	}
	if f.contact.state() {
		t.Error("contact still closed")
	}
	if st := f.smoke.SmokeCOState(); !st.Unmounted || st.ExpressedState != smokedef.ExpressedStateInoperative || !st.TestInProgress {
		t.Errorf("smoke alarm after SetUnmounted and LongPress = %+v, want unmounted, inoperative and testing", st)
	}
	if !f.lock.IsJammed() {
		t.Error("lock not jammed")
	}
	if lvl, _ := f.speaker.CurrentLevel(); lvl != 42 {
		t.Errorf("speaker level = %d", lvl)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "Bogus"}); !errors.Is(err, errUnknownPipeCommand) {
		t.Errorf("unknown command error = %v", err)
	}
}

// TestSmokeTestEventTriggers maps CHIP's SmokeCoAlarm trigger codes onto
// the alarm the way CHIP's smoke-co-alarm app does, and refuses a code it
// does not implement.
func TestSmokeTestEventTriggers(t *testing.T) {
	f, _ := startFleetBridge(t)
	cases := []struct {
		trigger uint64
		check   func(smokedef.State) bool
	}{
		{triggerForceSmokeCritical, func(s smokedef.State) bool { return s.SmokeState == alarm.AlarmCritical }},
		{triggerClearSmoke, func(s smokedef.State) bool { return s.SmokeState == alarm.AlarmNormal }},
		{triggerForceCOWarning, func(s smokedef.State) bool { return s.CoState == alarm.AlarmWarning }},
		{triggerForceLowBatteryCrit, func(s smokedef.State) bool { return s.BatteryAlert == alarm.AlarmCritical }},
		{triggerForceSilence, func(s smokedef.State) bool { return s.DeviceMuted == alarm.Muted }},
		{triggerClearSilence, func(s smokedef.State) bool { return s.DeviceMuted == alarm.NotMuted }},
		// A critical alarm cannot be muted (TC-SMOKECO-2.5 step 55).
		{triggerForceSmokeCritical, func(s smokedef.State) bool { return s.SmokeState == alarm.AlarmCritical }},
		{triggerForceSilence, func(s smokedef.State) bool { return s.DeviceMuted == alarm.NotMuted }},
		{triggerClearSmoke, func(s smokedef.State) bool { return s.SmokeState == alarm.AlarmNormal }},
		{triggerForceMalfunction, func(s smokedef.State) bool { return s.HardwareFaultAlert }},
		{triggerForceEndOfLife, func(s smokedef.State) bool { return s.EndOfServiceAlert == alarm.EndOfServiceExpired }},
		{triggerForceContaminationHigh, func(s smokedef.State) bool { return s.ContaminationState == alarm.ContaminationCritical }},
		{triggerForceSensitivityLow, func(s smokedef.State) bool { return s.SmokeSensitivityLevel == alarm.SensitivityLow }},
		{triggerForceUnmounted, func(s smokedef.State) bool { return s.Unmounted }},
		// The endpoint bits CHIP clears (clearEndpointInEventTrigger).
		{triggerForceSmokeInterconnect | 0x0000_0009_0000_0000, func(s smokedef.State) bool { return s.InterconnectSmokeAlarm == alarm.AlarmWarning }},
	}
	for _, tc := range cases {
		if err := f.testEventTrigger(context.Background(), tc.trigger); err != nil {
			t.Errorf("trigger 0x%016X: %v", tc.trigger, err)
			continue
		}
		if !tc.check(f.smoke.SmokeCOState()) {
			t.Errorf("trigger 0x%016X left %+v", tc.trigger, f.smoke.SmokeCOState())
		}
	}
	for _, generic := range []uint64{triggerGeneric, triggerGenericAlias} {
		if err := f.testEventTrigger(context.Background(), generic); err != nil {
			t.Errorf("generic trigger 0x%016X: %v", generic, err)
		}
	}
	if err := f.testEventTrigger(context.Background(), 0x1234); err == nil {
		t.Error("an unknown trigger was accepted")
	}
	if err := f.washer.reportError(opdef.ErrorStateUnableToStartOrResume); err != nil {
		t.Errorf("washer error: %v", err)
	}
}

// TestWasherFaultBlocksStart: a fault reported through the app pipe blocks
// Start and Resume with UnableToStartOrResume until a NoError fault clears
// it and resumes the cycle — matter.js AllClustersTestInstance.ts OnFault
// with TestOperationalStateServer's startBlocked (TC-OPSTATE-2.2 step 17).
func TestWasherFaultBlocksStart(t *testing.T) {
	f, _ := startFleetBridge(t)
	one, zero := uint8(2), uint8(0)
	if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Generic", Operation: "OnFault", Param: &one}); err != nil {
		t.Fatalf("OnFault 2: %v", err)
	}
	got, err := f.washer.HandleOperationalCommand(context.Background(), opdef.CmdStart)
	if err != nil || got.ErrorStateId != opdef.ErrorStateUnableToStartOrResume {
		t.Fatalf("Start after a fault = %+v, %v; want UnableToStartOrResume", got, err)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Generic", Operation: "OnFault", Param: &zero}); err != nil {
		t.Fatalf("OnFault 0: %v", err)
	}
	got, err = f.washer.HandleOperationalCommand(context.Background(), opdef.CmdStart)
	if err != nil || got.ErrorStateId != opdef.ErrorStateNoError {
		t.Fatalf("Start after the fault cleared = %+v, %v; want NoError", got, err)
	}
	f.washer.stopCountdown()
}

// TestSmokeSelfTestEndsOnItsOwn: the self-test a SelfTestRequest or the
// test button starts ends after smokeSelfTestDuration (TC-SMOKECO-2.4).
func TestSmokeSelfTestEndsOnItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newDemoSmokeAlarm("selftest")
		if err := a.SelfTest(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !a.SmokeCOState().TestInProgress {
			t.Fatal("self-test did not start")
		}
		time.Sleep(smokeSelfTestDuration)
		synctest.Wait()
		if a.SmokeCOState().TestInProgress {
			t.Fatal("self-test still in progress after its duration")
		}
	})
}

// TestWasherStopReportsTheCycleTimes: the countdown stops at zero with the
// cycle still running, and the Stop that ends it emits OperationCompletion
// with the running and paused seconds (TC-OPSTATE-2.5 steps 11-13).
func TestWasherStopReportsTheCycleTimes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newDemoWasher("cycle")
		if _, err := w.device(bridged("washer", w.name)); err != nil {
			t.Fatal(err)
		}
		events := &eventCapture{}
		w.ops.Underlying().(contract.EventReceiver).SetMatterEventEmitter(events)
		ctx := context.Background()
		if got, err := w.HandleOperationalCommand(ctx, opdef.CmdStart); err != nil || got.ErrorStateId != opdef.ErrorStateNoError {
			t.Fatalf("Start = %+v, %v", got, err)
		}
		time.Sleep(washCycleSeconds*time.Second + time.Second)
		synctest.Wait()
		if v := w.ops.Get().OperationalState; v != opdef.OperationalStateRunning {
			t.Errorf("OperationalState after the countdown = %v, want still Running", v)
		}
		// A pause of 10 s counts into the total and into PausedTime.
		if _, err := w.HandleOperationalCommand(ctx, opdef.CmdPause); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		if _, err := w.HandleOperationalCommand(ctx, opdef.CmdResume); err != nil {
			t.Fatal(err)
		}
		if got, err := w.HandleOperationalCommand(ctx, opdef.CmdStop); err != nil || got.ErrorStateId != opdef.ErrorStateNoError {
			t.Fatalf("Stop = %+v, %v", got, err)
		}
		if v := w.ops.Get().OperationalState; v != opdef.OperationalStateStopped {
			t.Errorf("OperationalState after Stop = %v, want Stopped", v)
		}
		var done *opdef.OperationCompletionEvent
		for _, e := range events.all() {
			if ev, ok := e.(opdef.OperationCompletionEvent); ok {
				done = &ev
			}
		}
		if done == nil || done.TotalOperationalTime == nil || done.PausedTime == nil {
			t.Fatalf("OperationCompletion = %+v, want one with both times", done)
		}
		if got := done.TotalOperationalTime.Value; got != washCycleSeconds+11 || done.PausedTime.Value != 10 {
			t.Errorf("OperationCompletion times = %d total / %d paused, want %d / 10", got, done.PausedTime.Value, washCycleSeconds+11)
		}
	})
}

// eventCapture records the event payloads a server emits.
type eventCapture struct {
	mu   sync.Mutex
	data []any
}

func (c *eventCapture) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	c.mu.Lock()
	c.data = append(c.data, data)
	c.mu.Unlock()
}

func (c *eventCapture) all() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.data...)
}

// TestSmokeTestButtonIsIgnoredWhileAlarming: the test button starts no
// self-test while the alarm sounds, the rule a SelfTestRequest answers with
// BUSY (TC-SMOKECO-2.2 steps 9-10).
func TestSmokeTestButtonIsIgnoredWhileAlarming(t *testing.T) {
	a := newDemoSmokeAlarm("busy")
	d, err := device.NewSmokeCoAlarm(bridged("smoke", a.name), a.declaration())
	if err != nil {
		t.Fatal(err)
	}
	a.bind(d)
	a.update(func(s *smokedef.State) { s.SmokeState = alarm.AlarmWarning })
	if err := a.pressTestButton(context.Background()); err != nil {
		t.Fatalf("press while alarming: %v", err)
	}
	if a.SmokeCOState().TestInProgress {
		t.Fatal("a self-test started while the alarm sounds")
	}
}

// TestOvenPipeDrivesTheCavity: OperationalStateChange for Device "Oven" is
// CHIP's OnOvenOperationalStateChange (AllClustersCommandDelegate.cpp:995
// at the harness pin) on the oven's cavity: Start, Pause and Stop set the
// state directly, OnFault puts the cavity in Error with the error of Param,
// and the list the cavity advertises holds Paused although it offers no
// Pause command (TC-OVENOPSTATE-2.1 steps 6a-6h, 2.2 step 3, 2.4 step 3).
func TestOvenPipeDrivesTheCavity(t *testing.T) {
	f, _ := startFleetBridge(t)
	cavity := f.oven.cavity
	state := func() ovendef.OperationalStateEnum { return cavity.server().Get().OperationalState }
	for _, step := range []struct {
		op   string
		want ovendef.OperationalStateEnum
	}{
		{"Start", ovendef.OperationalStateRunning},
		{"Pause", ovendef.OperationalStatePaused},
		{"Resume", ovendef.OperationalStateRunning},
		{"Stop", ovendef.OperationalStateStopped},
	} {
		if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Oven", Operation: step.op}); err != nil {
			t.Fatalf("%s: %v", step.op, err)
		}
		if got := state(); got != step.want {
			t.Fatalf("after %s: OperationalState = %d, want %d", step.op, got, step.want)
		}
	}
	fault := uint8(ovendef.ErrorStateUnableToStartOrResume)
	if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Oven", Operation: "OnFault", Param: &fault}); err != nil {
		t.Fatalf("OnFault: %v", err)
	}
	if got := state(); got != ovendef.OperationalStateError {
		t.Fatalf("after OnFault: OperationalState = %d, want Error", got)
	}
	if e := cavity.server().Get().OperationalError; e.ErrorStateId != ovendef.ErrorStateUnableToStartOrResume {
		t.Fatalf("OperationalError = %#v, want UnableToStartOrResume", e)
	}
	// Start while the fault stands is UnableToStartOrResume, as CHIP's oven
	// delegate answers (HandleStartStateCallback :139-146; TC-OVENOPSTATE-2.2
	// step 17); clearing it with OnFault 0 lets Start through.
	got, err := cavity.cycle().handle(ovendef.CmdStart)
	if err != nil || got != uint8(ovendef.ErrorStateUnableToStartOrResume) {
		t.Fatalf("Start under a fault = %+v, %v; want UnableToStartOrResume", got, err)
	}
	zero := uint8(ovendef.ErrorStateNoError)
	if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Oven", Operation: "OnFault", Param: &zero}); err != nil {
		t.Fatalf("OnFault 0: %v", err)
	}
	got, err = cavity.cycle().handle(ovendef.CmdStart)
	if err != nil || got != uint8(ovendef.ErrorStateNoError) {
		t.Fatalf("Start after the fault cleared = %+v, %v; want NoError", got, err)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "OperationalStateChange", Device: "Oven", Operation: "Bake"}); !errors.Is(err, errUnknownPipeCommand) {
		t.Fatalf("unknown operation: %v, want errUnknownPipeCommand", err)
	}
}
