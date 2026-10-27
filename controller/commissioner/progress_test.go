// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// TestFlowProgress: Options.Progress hears every step started and then
// its result — Success and Stop as succeeded, Skipped as skipped, a
// Failure without an error as failed (ControllerCommissioningFlow.ts:164-169
// at 862ad75f) — a recoverable error as failed with the flow going on
// (:432-437), and a terminating status error as failed with the
// CommissioningError the flow returns (:438-462).
func TestFlowProgress(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{}
	f, _ := newTestFlow(t, dev.handle)
	var got []string
	var steps []Step
	f.opts.Progress = func(s Step) {
		steps = append(steps, s)
		got = append(got, fmt.Sprintf("%d.%d %s %s %v", s.Number, s.Sub, s.Name, s.Phase, s.Err != nil))
	}
	status := &interaction.StatusError{Status: im.StatusFailure}
	f.steps = []step{
		{0, 1, "ok", func(context.Context) (stepResult, error) { return stepSuccess, nil }},
		{8, 2, "skip", func(context.Context) (stepResult, error) { return stepSkipped, nil }},
		{10, 2, "recoverable", func(context.Context) (stepResult, error) { return 0, newError(errRecoverable, "continue", nil) }},
		{11, 1, "failure", func(context.Context) (stepResult, error) { return stepFailure, nil }},
		{15, 1, "status", func(context.Context) (stepResult, error) { return 0, status }},
		{20, 1, "never", func(context.Context) (stepResult, error) { return stepSuccess, nil }},
	}
	err := f.execute(context.Background())
	want := []string{
		"0.1 ok started false", "0.1 ok succeeded false",
		"8.2 skip started false", "8.2 skip skipped false",
		"10.2 recoverable started false", "10.2 recoverable failed true",
		"11.1 failure started false", "11.1 failure failed false",
		"15.1 status started false", "15.1 status failed true",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("progress %q,\nwant %q", got, want)
	}
	if last := steps[len(steps)-1]; err == nil || !errors.Is(err, last.Err) || !errors.Is(last.Err, ErrCommissioning) {
		t.Errorf("returned %v, reported %v", err, last.Err)
	}

	f, _ = newTestFlow(t, dev.handle)
	got = nil
	f.opts.Progress = func(s Step) { got = append(got, fmt.Sprintf("%d.%d %s", s.Number, s.Sub, s.Phase)) }
	f.steps = []step{{18, 1, "Reconnect", func(context.Context) (stepResult, error) { return stepStop, nil }}}
	if err := f.execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"18.1 started", "18.1 succeeded"}; !slices.Equal(got, want) {
		t.Fatalf("stop: %q, want %q", got, want)
	}
}

// TestStepPhaseNames: the phases' names, as a UI shows them.
func TestStepPhaseNames(t *testing.T) {
	t.Parallel()
	want := []string{"unknown", "started", "succeeded", "failed", "skipped", "unknown"}
	for p := range want {
		if got := StepPhase(p).String(); got != want[p] {
			t.Errorf("phase %d: %q, want %q", p, got, want[p])
		}
	}
	var got []Step
	ReportDiscovery(func(s Step) { got = append(got, s) }, StepStarted, nil)
	ReportDiscovery(nil, StepStarted, nil)
	if len(got) != 1 || got[0].Number != 2 || got[0].Sub != 1 || got[0].Name != StepDiscovery {
		t.Fatalf("ReportDiscovery: %+v", got)
	}
}
