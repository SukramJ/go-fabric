// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// energyEvent is one emitted event.
type energyEvent struct {
	cluster uint32
	id      uint32
	data    any
}

// energyEvents records the emitted events.
type energyEvents struct {
	mu     sync.Mutex
	events []energyEvent
}

func (e *energyEvents) MatterEmitEvent(_ uint16, cluster, id uint32, data any, _ contract.EventPriority) {
	e.mu.Lock()
	e.events = append(e.events, energyEvent{cluster, id, data})
	e.mu.Unlock()
}

func (e *energyEvents) take() []energyEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.events
	e.events = nil
	return out
}

// ids returns the event ids taken.
func (e *energyEvents) ids() []uint32 {
	taken := e.take()
	out := make([]uint32, 0, len(taken))
	for _, ev := range taken {
		out = append(out, ev.id)
	}
	return out
}

// demHost is an DemManager that counts the hooks and fails on request.
type demHost struct {
	energy.NopDemManager
	mu       sync.Mutex
	calls    map[string]int
	fail     error
	energyUs int64
}

func (h *demHost) count(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.calls == nil {
		h.calls = map[string]int{}
	}
	h.calls[name]++
	return h.fail
}

func (h *demHost) ApproxEnergyDuringSession() int64 { return h.energyUs }

func (h *demHost) PowerAdjust(context.Context, int64, uint32, energy.AdjustmentCause) error {
	return h.count("PowerAdjust")
}

func (h *demHost) Pause(context.Context, uint32, energy.AdjustmentCause) error {
	return h.count("Pause")
}

func (h *demHost) StartTimeAdjust(context.Context, uint32, energy.AdjustmentCause) error {
	return h.count("StartTimeAdjust")
}

func (h *demHost) ModifyForecast(context.Context, uint32, []energy.SlotAdjustment, energy.AdjustmentCause) error {
	return h.count("ModifyForecast")
}

func demEpoch(t time.Time) uint32 { return uint32(t.Unix() - 946684800) } //nolint:gosec // test clock after 2000

func newDEM(t *testing.T, features energy.DemFeature, host *demHost) (*energy.DeviceEnergyManagementServer, *energyEvents) {
	t.Helper()
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: features, EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000, Manager: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(3)
	t.Cleanup(srv.Close)
	return srv, ev
}

// demWantStatus fails unless err carries status.
func demWantStatus(t *testing.T, what string, err error, status im.StatusCode) {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != status {
		t.Errorf("%s: %v, want status 0x%02X", what, err, status)
	}
}

func demInvoke(srv contract.ClusterServer, cmd uint32, fields any) error {
	_, err := srv.MatterInvoke(context.Background(), cmd, fields)
	return err
}

// The PowerAdjustment trigger's capability (connectedhomeip
// examples/energy-management/energy-management-triggers/
// DEMTestEventTriggers.cpp:137-155): 5 kW to 30 kW for 10 s to 60 s.
func triggerCapability() *energy.PowerAdjustCapability {
	return &energy.PowerAdjustCapability{
		PowerAdjustCapability: spec.ValueOf([]energy.PowerAdjustRange{{MinPower: 5000000, MaxPower: 30000000, MinDuration: 10, MaxDuration: 60}}),
		Cause:                 dem.PowerAdjustReasonNoAdjustment,
	}
}

// The ConfigureForecast(2) shape (DEMTestEventTriggers.cpp:51-135), for
// the PFR features: slot 0 pausable 10 s to 60 s, slot 1 not pausable.
func triggerForecast(now uint32) *energy.Forecast {
	yes, no := true, false
	nominal, lo, hi := int64(2500000), int64(1200000), int64(7600000)
	energy0, energy1 := int64(2000), int64(4000)
	minP0, maxP0, minP1, maxP1 := uint32(10), uint32(60), uint32(2), uint32(120)
	earliest, latest := spec.ValueOf(now), now*3
	return &energy.Forecast{
		StartTime: now + 60, EarliestStartTime: &earliest, EndTime: now * 3, LatestEndTime: &latest,
		IsPausable: true, ActiveSlotNumber: spec.ValueOf(uint16(0)),
		Slots: []energy.ForecastSlot{
			{MinDuration: 10, MaxDuration: 20, DefaultDuration: 15, SlotIsPausable: &yes, MinPauseDuration: &minP0, MaxPauseDuration: &maxP0, NominalPower: &nominal, MinPower: &lo, MaxPower: &hi, NominalEnergy: &energy0},
			{MinDuration: 20, MaxDuration: 40, DefaultDuration: 30, SlotIsPausable: &no, MinPauseDuration: &minP1, MaxPauseDuration: &maxP1, NominalPower: &nominal, MinPower: &lo, MaxPower: &hi, NominalEnergy: &energy1},
		},
	}
}

const demPFRSet = energy.DemFeaturePowerAdjustment | energy.DemFeaturePowerForecastReporting | energy.DemFeatureStartTimeAdjustment |
	energy.DemFeaturePausable | energy.DemFeatureForecastAdjustment | energy.DemFeatureConstraintBasedAdjustment

// TestDEMPowerAdjustLifecycle follows chip's PowerAdjustRequest
// (DeviceEnergyManagementCluster.cpp:324-379, DelegateImpl.cpp:100-241):
// no capability or out of range is CONSTRAINT_ERROR; a request starts the
// adjustment with PowerAdjustStart once, a second one restarts the timer
// without another; the timer ends it with PowerAdjustEnd NormalCompletion;
// a cancel ends it with Cancelled; a cancel without one is INVALID_IN_STATE.
func TestDEMPowerAdjustLifecycle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		host := &demHost{energyUs: 300}
		srv, ev := newDEM(t, demPFRSet, host)
		req := dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}
		demWantStatus(t, "no capability", demInvoke(srv, dem.CmdPowerAdjustRequest, req), im.StatusConstraintError)
		if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
			t.Fatal(err)
		}
		demWantStatus(t, "out of range", demInvoke(srv, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 1, Duration: 20}), im.StatusConstraintError)
		demWantStatus(t, "cancel idle", demInvoke(srv, dem.CmdCancelPowerAdjustRequest, dem.CancelPowerAdjustRequestRequest{}), im.StatusInvalidInState)

		if err := demInvoke(srv, dem.CmdPowerAdjustRequest, req); err != nil {
			t.Fatal(err)
		}
		if srv.EsaState() != energy.EsaStatePowerAdjustActive || srv.PowerAdjustmentCapability().Cause != dem.PowerAdjustReasonLocalOptimizationAdjustment {
			t.Errorf("after request: ESAState %d, cause %d", srv.EsaState(), srv.PowerAdjustmentCapability().Cause)
		}
		if err := demInvoke(srv, dem.CmdPowerAdjustRequest, &req); err != nil {
			t.Fatal(err)
		}
		if got := ev.ids(); len(got) != 1 || got[0] != dem.EventPowerAdjustStart {
			t.Errorf("events %v, want one PowerAdjustStart", got)
		}
		time.Sleep(21 * time.Second)
		synctest.Wait()
		evs := ev.take()
		if len(evs) != 1 || evs[0].id != dem.EventPowerAdjustEnd {
			t.Fatalf("events %+v, want PowerAdjustEnd", evs)
		}
		if end := evs[0].data.(dem.PowerAdjustEndEvent); end.Cause != dem.CauseNormalCompletion || end.EnergyUse != 300 || end.Duration < 20 {
			t.Errorf("PowerAdjustEnd %+v", end)
		}
		if srv.EsaState() != energy.EsaStateOnline || srv.PowerAdjustmentCapability().Cause != dem.PowerAdjustReasonNoAdjustment {
			t.Errorf("after expiry: ESAState %d", srv.EsaState())
		}

		if err := demInvoke(srv, dem.CmdPowerAdjustRequest, req); err != nil {
			t.Fatal(err)
		}
		if err := demInvoke(srv, dem.CmdCancelPowerAdjustRequest, dem.CancelPowerAdjustRequestRequest{}); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 2 || evs[1].data.(dem.PowerAdjustEndEvent).Cause != dem.CauseCancelled {
			t.Errorf("events %+v, want Start then End(Cancelled)", evs)
		}

		host.fail = errors.New("refused")
		demWantStatus(t, "host refusal", demInvoke(srv, dem.CmdPowerAdjustRequest, req), im.StatusFailure)
		if srv.EsaState() != energy.EsaStateOnline {
			t.Errorf("after refusal: ESAState %d", srv.EsaState())
		}
		if err := srv.SetEsaState(energy.EsaStateOffline); err != nil {
			t.Fatal(err)
		}
		host.fail = nil
		demWantStatus(t, "offline", demInvoke(srv, dem.CmdPowerAdjustRequest, req), im.StatusInvalidInState)
	})
}

// TestDEMOptOut follows CheckOptOutAllowsRequest (Cluster.cpp:275-322) and
// SetOptOutState (DelegateImpl.cpp:1274-1377).
func TestDEMOptOut(t *testing.T) {
	t.Parallel()
	srv, ev := newDEM(t, demPFRSet, &demHost{})
	if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
		t.Fatal(err)
	}
	local := dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}
	grid := local
	grid.Cause = dem.AdjustmentCauseGridOptimization
	demWantStatus(t, "unknown cause", demInvoke(srv, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: 7}), im.StatusConstraintError)

	if err := demInvoke(srv, dem.CmdPowerAdjustRequest, local); err != nil {
		t.Fatal(err)
	}
	ev.take()
	// A Local opt-out cancels the running Local adjustment.
	if err := srv.SetOptOutState(energy.OptOutLocal); err != nil {
		t.Fatal(err)
	}
	evs := ev.take()
	if len(evs) != 1 || evs[0].data.(dem.PowerAdjustEndEvent).Cause != dem.CauseUserOptOut {
		t.Errorf("events %+v, want PowerAdjustEnd(UserOptOut)", evs)
	}
	demWantStatus(t, "local opted out", demInvoke(srv, dem.CmdPowerAdjustRequest, local), im.StatusConstraintError)
	if err := demInvoke(srv, dem.CmdPowerAdjustRequest, grid); err != nil {
		t.Errorf("grid while local opted out: %v", err)
	}
	// Grid over Local is OptOut.
	if err := srv.SetOptOutState(energy.OptOutGrid); err != nil {
		t.Fatal(err)
	}
	if srv.OptOutState() != energy.OptOutAll {
		t.Errorf("OptOutState %d, want OptOut", srv.OptOutState())
	}
	demWantStatus(t, "all opted out", demInvoke(srv, dem.CmdPowerAdjustRequest, grid), im.StatusConstraintError)
	if err := srv.SetOptOutState(energy.OptOutNone); err != nil {
		t.Fatal(err)
	}
	if err := demInvoke(srv, dem.CmdPowerAdjustRequest, local); err != nil {
		t.Errorf("after clearing: %v", err)
	}
}

// TestDEMPauseResume follows HandlePauseRequest (Cluster.cpp:522-594),
// PauseRequest, ResumeRequest and the pause timer (DelegateImpl.cpp:
// 425-646).
func TestDEMPauseResume(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, ev := newDEM(t, demPFRSet, &demHost{})
		pause := dem.PauseRequestRequest{Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}
		demWantStatus(t, "no forecast", demInvoke(srv, dem.CmdPauseRequest, pause), im.StatusFailure)
		if err := srv.SetForecast(triggerForecast(demEpoch(time.Now()))); err != nil {
			t.Fatal(err)
		}
		demWantStatus(t, "too short", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 5}), im.StatusConstraintError)
		demWantStatus(t, "resume unpaused", demInvoke(srv, dem.CmdResumeRequest, dem.ResumeRequestRequest{}), im.StatusInvalidInState)
		if err := demInvoke(srv, dem.CmdPauseRequest, pause); err != nil {
			t.Fatal(err)
		}
		if srv.EsaState() != energy.EsaStatePaused || srv.Forecast().ForecastUpdateReason != dem.ForecastUpdateReasonLocalOptimization {
			t.Errorf("paused: ESAState %d, reason %d", srv.EsaState(), srv.Forecast().ForecastUpdateReason)
		}
		if err := demInvoke(srv, dem.CmdResumeRequest, dem.ResumeRequestRequest{}); err != nil {
			t.Fatal(err)
		}
		evs := ev.take()
		if len(evs) != 2 || evs[0].id != dem.EventPaused || evs[1].data.(dem.ResumedEvent).Cause != dem.CauseCancelled {
			t.Errorf("events %+v, want Paused, Resumed(Cancelled)", evs)
		}
		if srv.EsaState() != energy.EsaStateOnline || srv.Forecast().ForecastUpdateReason != dem.ForecastUpdateReasonInternalOptimization {
			t.Errorf("resumed: ESAState %d", srv.EsaState())
		}
		// The pause timer resumes with NormalCompletion.
		if err := demInvoke(srv, dem.CmdPauseRequest, pause); err != nil {
			t.Fatal(err)
		}
		time.Sleep(21 * time.Second)
		synctest.Wait()
		evs = ev.take()
		if len(evs) != 2 || evs[1].data.(dem.ResumedEvent).Cause != dem.CauseNormalCompletion {
			t.Errorf("events %+v, want Paused, Resumed(NormalCompletion)", evs)
		}
		// Slot 1 is not pausable (DEMTestEventTriggers.cpp:112-113).
		f := srv.Forecast()
		f.ActiveSlotNumber = spec.ValueOf(uint16(1))
		if err := srv.SetForecast(f); err != nil {
			t.Fatal(err)
		}
		demWantStatus(t, "unpausable slot", demInvoke(srv, dem.CmdPauseRequest, pause), im.StatusFailure)
	})
}

// TestDEMStartTimeAndCancel follows HandleStartTimeAdjustRequest
// (Cluster.cpp:416-520) and HandleCancelRequest (:798-826).
func TestDEMStartTimeAndCancel(t *testing.T) {
	t.Parallel()
	srv, _ := newDEM(t, demPFRSet, &demHost{})
	now := demEpoch(time.Now())
	f := triggerForecast(now)
	// The StartTimeAdjustment trigger's times (DEMTestEventTriggers.cpp:
	// 179-208).
	earliest, latest := spec.ValueOf(now-60), now*3+60
	f.StartTime, f.EarliestStartTime, f.EndTime, f.LatestEndTime = now, &earliest, now*3, &latest
	if err := srv.SetForecast(f); err != nil {
		t.Fatal(err)
	}
	duration := f.EndTime - f.StartTime
	demWantStatus(t, "before earliest", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{RequestedStartTime: now - 61}), im.StatusConstraintError)
	demWantStatus(t, "after latest", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{RequestedStartTime: now + 61}), im.StatusConstraintError)
	demWantStatus(t, "cancel unadjusted", demInvoke(srv, dem.CmdCancelRequest, dem.CancelRequestRequest{}), im.StatusInvalidInState)
	if err := demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{RequestedStartTime: now + 30, Cause: dem.AdjustmentCauseGridOptimization}); err != nil {
		t.Fatal(err)
	}
	if g := srv.Forecast(); g.StartTime != now+30 || g.EndTime != now+30+duration || g.ForecastId != 1 || g.ForecastUpdateReason != dem.ForecastUpdateReasonGridOptimization {
		t.Errorf("adjusted forecast %+v", g)
	}
	if err := demInvoke(srv, dem.CmdCancelRequest, dem.CancelRequestRequest{}); err != nil {
		t.Fatal(err)
	}
	if srv.Forecast().ForecastUpdateReason != dem.ForecastUpdateReasonInternalOptimization {
		t.Error("CancelRequest left the reason")
	}
}

// TestDEMModifyAndConstraints follows HandleModifyForecastRequest
// (Cluster.cpp:618-684) and HandleRequestConstraintBasedForecast
// (:686-796).
func TestDEMModifyAndConstraints(t *testing.T) {
	t.Parallel()
	host := &demHost{}
	srv, _ := newDEM(t, demPFRSet, host)
	now := demEpoch(time.Now())
	f := triggerForecast(now)
	// The ForecastAdjustment trigger's slot 0 (DEMTestEventTriggers.cpp:
	// 261-275).
	minPA, maxPA, minDA, maxDA := int64(20), int64(2000), uint32(120), uint32(240)
	f.Slots[0].MinPowerAdjustment, f.Slots[0].MaxPowerAdjustment = &minPA, &maxPA
	f.Slots[0].MinDurationAdjustment, f.Slots[0].MaxDurationAdjustment = &minDA, &maxDA
	if err := srv.SetForecast(f); err != nil {
		t.Fatal(err)
	}
	power := int64(1000)
	ok := dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 0, NominalPower: &power, Duration: 180}}, Cause: dem.AdjustmentCauseLocalOptimization}
	bad := power * 10
	for name, c := range map[string]struct {
		req    dem.ModifyForecastRequestRequest
		status im.StatusCode
	}{
		"slot index":   {dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 5, NominalPower: &power, Duration: 180}}}, im.StatusFailure},
		"power":        {dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 0, NominalPower: &bad, Duration: 180}}}, im.StatusConstraintError},
		"no power":     {dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 0, Duration: 180}}}, im.StatusConstraintError},
		"duration":     {dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 0, NominalPower: &power, Duration: 60}}}, im.StatusConstraintError},
		"unadjustable": {dem.ModifyForecastRequestRequest{SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 1, NominalPower: &power, Duration: 180}}}, im.StatusConstraintError},
		"forecast id":  {dem.ModifyForecastRequestRequest{ForecastId: 9, SlotAdjustments: ok.SlotAdjustments}, im.StatusFailure},
	} {
		demWantStatus(t, name, demInvoke(srv, dem.CmdModifyForecastRequest, c.req), c.status)
	}
	if err := demInvoke(srv, dem.CmdModifyForecastRequest, ok); err != nil {
		t.Fatal(err)
	}
	if g := srv.Forecast(); g.ForecastId != 1 || g.ForecastUpdateReason != dem.ForecastUpdateReasonLocalOptimization {
		t.Errorf("modified forecast id %d reason %d", g.ForecastId, g.ForecastUpdateReason)
	}

	nominal, maxEnergy := int64(2000000), int64(1000)
	good := energy.ForecastConstraint{StartTime: now + 120, Duration: 60, NominalPower: &nominal, MaximumEnergy: &maxEnergy}
	tooLow := int64(1)
	for name, c := range map[string]struct {
		cons   []energy.ForecastConstraint
		status im.StatusCode
	}{
		"past":        {[]energy.ForecastConstraint{{StartTime: now - 10, Duration: 60, NominalPower: &nominal, MaximumEnergy: &maxEnergy}}, im.StatusConstraintError},
		"no power":    {[]energy.ForecastConstraint{{StartTime: now + 120, Duration: 60, MaximumEnergy: &maxEnergy}}, im.StatusInvalidCommand},
		"power range": {[]energy.ForecastConstraint{{StartTime: now + 120, Duration: 60, NominalPower: &tooLow, MaximumEnergy: &maxEnergy}}, im.StatusConstraintError},
		"no energy":   {[]energy.ForecastConstraint{{StartTime: now + 120, Duration: 60, NominalPower: &nominal}}, im.StatusInvalidCommand},
		"overlap":     {[]energy.ForecastConstraint{good, {StartTime: now + 150, Duration: 60}}, im.StatusConstraintError},
	} {
		demWantStatus(t, name, demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{Constraints: c.cons}), c.status)
	}
	if err := demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{
		Constraints: []energy.ForecastConstraint{good, {StartTime: now + 300, Duration: 60}}, Cause: dem.AdjustmentCauseGridOptimization,
	}); err != nil {
		t.Fatal(err)
	}
	if g := srv.Forecast(); g.ForecastId != 2 || g.ForecastUpdateReason != dem.ForecastUpdateReasonGridOptimization {
		t.Errorf("constrained forecast id %d reason %d", g.ForecastId, g.ForecastUpdateReason)
	}
	host.fail = errors.New("refused")
	demWantStatus(t, "host refusal", demInvoke(srv, dem.CmdModifyForecastRequest, dem.ModifyForecastRequestRequest{ForecastId: 2, SlotAdjustments: ok.SlotAdjustments}), im.StatusFailure)
	demWantStatus(t, "malformed", demInvoke(srv, dem.CmdModifyForecastRequest, "x"), im.StatusInvalidCommand)
}

func TestDEMConfig(t *testing.T) {
	t.Parallel()
	if _, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{Features: demPFRSet}); !errors.Is(err, energy.ErrNoDemManager) {
		t.Errorf("without a manager: %v", err)
	}
	// SFR and PFR are exclusive without PA ("[!PA].a").
	if _, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: energy.DemFeaturePowerForecastReporting | energy.DemFeatureStateForecastReporting, Manager: energy.NopDemManager{},
	}); err == nil {
		t.Error("PFR and SFR without PA was built")
	}
	srv, _ := newDEM(t, demPFRSet, &demHost{})
	if srv.AbsMinPower() != 1200000 || srv.AbsMaxPower() != 7600000 || srv.Forecast() != nil || srv.PowerAdjustmentCapability() != nil {
		t.Error("initial state")
	}
	if err := srv.SetAbsMinPower(1); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetAbsMaxPower(2); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetForecast(nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetPowerAdjustmentCapability(nil); err != nil {
		t.Fatal(err)
	}
	var n energy.NopDemManager
	ctx := context.Background()
	if n.ApproxEnergyDuringSession() != 0 || n.PowerAdjust(ctx, 0, 0, 0) != nil || n.PowerAdjustCompleted() != nil ||
		n.CancelPowerAdjust(0) != nil || n.StartTimeAdjust(ctx, 0, 0) != nil || n.Pause(ctx, 0, 0) != nil ||
		n.PauseCompleted() != nil || n.CancelPause(0) != nil || n.CancelRequest(ctx) != nil ||
		n.ModifyForecast(ctx, 0, nil, 0) != nil || n.RequestConstraintBasedForecast(ctx, nil, 0) != nil {
		t.Error("NopDemManager hook failed")
	}
}
