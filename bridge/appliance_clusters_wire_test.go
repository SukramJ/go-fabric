// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// The appliance cluster servers (cluster/opstate, cluster/modebase) over
// the real wire path: a bridge assembled from host devices, a CASE
// session into it, and every command, read and event crossing
// InvokeRequest / ReadRequest encoding and decoding — commandFieldsReader,
// the dispatcher, the response rewrite and the value writers — rather
// than a direct MatterInvoke call.

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/tlv"
)

// washer is a host laundry washer: a Start starts it, a Pause pauses it.
type washer struct {
	mu  sync.Mutex
	srv *opstate.Server
}

func (w *washer) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	w.mu.Lock()
	srv := w.srv
	w.mu.Unlock()
	next := map[opstate.Command]opstate.State{
		opstate.CommandStart: opstate.StateRunning, opstate.CommandPause: opstate.StatePaused,
		opstate.CommandResume: opstate.StateRunning, opstate.CommandStop: opstate.StateStopped,
		opstate.CommandGoHome: opstate.StateSeekingCharger,
	}[cmd]
	return opstate.ErrorState{}, srv.SetOperationalState(next)
}

// operationalResponse invokes cmd and returns the OperationalCommandResponse
// ErrorStateID, failing unless the reply is that response.
func (h *appHarness) operationalResponse(t *testing.T, ep uint16, clusterID, cmd uint32) uint64 {
	t.Helper()
	id, fields, status, isStatus := invokeResult(t, h.invoke(ep, clusterID, cmd, nil))
	if isStatus {
		t.Fatalf("command 0x%02X answered status %v, want OperationalCommandResponse", cmd, status)
	}
	if id != opstate.CmdOperationalCommandResponse {
		t.Fatalf("command 0x%02X answered command 0x%02X, want OperationalCommandResponse (0x04)", cmd, id)
	}
	return fields.mustChild(t, 0).mustChild(t, 0).El.Uint
}

func TestOperationalStateOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &washer{}
	srv, err := opstate.NewServer(opstate.Config{
		Handler:  dev,
		Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandStart | opstate.CommandStop,
		States: []opstate.StateEntry{
			{ID: opstate.StateStopped},
			{ID: opstate.StateRunning},
			{ID: opstate.StatePaused},
			{ID: opstate.StateError},
			{ID: 0x80, Label: "Pre-soak"},
		},
		Phases: []string{"pre-soak", "wash", "spin"}, CurrentPhase: new(uint8),
		CountdownTime: true, DeviceType: opstate.DeviceTypeLaundryWasher,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.srv = srv
	h := newAppHarness(t, appDevice{deviceType: opstate.DeviceTypeLaundryWasher, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[opstate.DeviceTypeLaundryWasher]
	const cl = opstate.ClusterIDOperationalState

	// The structured attributes as a controller decodes them.
	list, _, _ := h.readAttribute(ep, cl, opstate.AttrOperationalStateList)
	if len(list.Children) != 5 || list.Children[4].mustChild(t, 1).El.String != "Pre-soak" {
		t.Errorf("OperationalStateList = %+v", list)
	}
	if _, ok := list.Children[0].child(1); ok {
		t.Error("a standard state carries a label")
	}
	phases, _, _ := h.readAttribute(ep, cl, opstate.AttrPhaseList)
	if len(phases.Children) != 3 || phases.Children[1].El.String != "wash" {
		t.Errorf("PhaseList = %+v", phases)
	}
	if v, _, _ := h.readAttribute(ep, cl, opstate.AttrCountdownTime); !v.El.IsNull {
		t.Errorf("CountdownTime = %+v, want null", v.El)
	}
	if v, _, _ := h.readAttribute(ep, cl, opstate.AttrOperationalError); v.El.Type != tlv.TypeStructure || v.mustChild(t, 0).El.Uint != 0 {
		t.Errorf("OperationalError = %+v, want {0: NoError}", v)
	}

	// Pause while Stopped: CommandInvalidInState, nothing moves.
	if got := h.operationalResponse(t, ep, cl, opstate.CmdPause); got != uint64(opstate.ErrorCommandInvalidInState) {
		t.Errorf("Pause while Stopped = 0x%02X", got)
	}
	// Start → Running (the device moves the state), then Pause → Paused.
	if got := h.operationalResponse(t, ep, cl, opstate.CmdStart); got != 0 {
		t.Fatalf("Start = 0x%02X", got)
	}
	if got := h.operationalResponse(t, ep, cl, opstate.CmdPause); got != 0 {
		t.Fatalf("Pause = 0x%02X", got)
	}
	if v, _, _ := h.readAttribute(ep, cl, opstate.AttrOperationalState); v.El.Uint != uint64(opstate.StatePaused) {
		t.Errorf("OperationalState after Pause = %d", v.El.Uint)
	}
	// GoHome is not an OperationalState command.
	if _, _, status, isStatus := invokeResult(t, h.invoke(ep, cl, opstate.CmdGoHome, nil)); !isStatus || status != im.StatusUnsupportedCommand {
		t.Errorf("GoHome = %v (status %v)", status, isStatus)
	}
	// Writes are refused before the server: the attributes are "R V".
	if st := h.writeAttribute(ep, cl, opstate.AttrOperationalState, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 1)
	}); st != uint64(im.StatusUnsupportedWrite) {
		t.Errorf("OperationalState write status 0x%02X", st)
	}

	// An error: OperationalState Error, and the critical event read back.
	if err := srv.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorUnableToCompleteOperation, Details: "drain blocked"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.EmitOperationCompletion(opstate.OperationCompletion{Code: opstate.ErrorUnableToCompleteOperation}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := h.readAttribute(ep, cl, opstate.AttrOperationalState); v.El.Uint != uint64(opstate.StateError) {
		t.Errorf("OperationalState after an error = %d", v.El.Uint)
	}
	var gotError, gotCompletion bool
	for _, ev := range h.readEvents(ep, cl) {
		switch uint32(ev.id) {
		case opstate.EventOperationalError:
			gotError = ev.data.mustChild(t, 0).mustChild(t, 2).El.String == "drain blocked"
		case opstate.EventOperationCompletion:
			gotCompletion = ev.data.mustChild(t, 0).El.Uint == uint64(opstate.ErrorUnableToCompleteOperation)
		}
	}
	if !gotError || !gotCompletion {
		t.Errorf("events: OperationalError %v, OperationCompletion %v", gotError, gotCompletion)
	}
}

func TestRvcOperationalStateOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &washer{}
	srv, err := opstate.NewRvcServer(opstate.Config{
		Handler:  dev,
		Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandGoHome,
		States: []opstate.StateEntry{
			{ID: opstate.StateStopped},
			{ID: opstate.StateRunning},
			{ID: opstate.StatePaused},
			{ID: opstate.StateError},
			{ID: opstate.StateSeekingCharger},
			{ID: opstate.StateCharging},
			{ID: opstate.StateDocked},
		},
		State: opstate.StateDocked, DeviceType: opstate.DeviceTypeRoboticVacuumCleaner,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.srv = srv
	h := newAppHarness(t, appDevice{deviceType: opstate.DeviceTypeRoboticVacuumCleaner, servers: []contract.ClusterServer{srv, newRunMode(t, &modeDevice{})}})
	ep := h.endpoints[opstate.DeviceTypeRoboticVacuumCleaner]
	const cl = opstate.ClusterIDRvcOperationalState

	if got := h.operationalResponse(t, ep, cl, opstate.CmdGoHome); got != uint64(opstate.ErrorCommandInvalidInState) {
		t.Errorf("GoHome while Docked = 0x%02X", got)
	}
	if got := h.operationalResponse(t, ep, cl, opstate.CmdPause); got != uint64(opstate.ErrorCommandInvalidInState) {
		t.Errorf("Pause while Docked = 0x%02X", got)
	}
	if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
		t.Fatal(err)
	}
	if got := h.operationalResponse(t, ep, cl, opstate.CmdGoHome); got != 0 {
		t.Errorf("GoHome while Running = 0x%02X", got)
	}
	if got := h.operationalResponse(t, ep, cl, opstate.CmdResume); got != uint64(opstate.ErrorCommandInvalidInState) {
		t.Errorf("Resume while SeekingCharger = 0x%02X", got)
	}
	for _, cmd := range []uint32{opstate.CmdStart, opstate.CmdStop} {
		if _, _, status, isStatus := invokeResult(t, h.invoke(ep, cl, cmd, nil)); !isStatus || status != im.StatusUnsupportedCommand {
			t.Errorf("RVC command 0x%02X = %v", cmd, status)
		}
	}
	// An RVC error state goes out with its details.
	if err := srv.SetOperationalError(opstate.ErrorState{ID: opstate.ErrorStuck, Details: "left wheel"}); err != nil {
		t.Fatal(err)
	}
	v, _, _ := h.readAttribute(ep, cl, opstate.AttrOperationalError)
	if v.mustChild(t, 0).El.Uint != uint64(opstate.ErrorStuck) || v.mustChild(t, 2).El.String != "left wheel" {
		t.Errorf("OperationalError = %+v", v)
	}
}

// modeDevice is a host's mode-switching device: it accepts every mode
// unless refuse is set.
type modeDevice struct {
	mu     sync.Mutex
	refuse modebase.Status
	asked  []uint8
}

func (d *modeDevice) ChangeToMode(_ context.Context, mode uint8) (modebase.Status, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, mode)
	if d.refuse != modebase.StatusSuccess {
		return d.refuse, "dust bin missing", nil
	}
	return modebase.StatusSuccess, "", nil
}

func newRunMode(t *testing.T, d *modeDevice) *modebase.Server {
	t.Helper()
	srv, err := modebase.NewRvcRunMode(modebase.Config{
		Changer: d,
		SupportedModes: []modebase.ModeOption{
			{Label: "Idle", Mode: 0, Tags: []modebase.ModeTag{{Value: modebase.RvcRunTagIdle}}},
			{Label: "Cleaning", Mode: 1, Tags: []modebase.ModeTag{{Value: modebase.RvcRunTagCleaning}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// changeToMode invokes ChangeToMode and returns the response's Status and
// StatusText.
func (h *appHarness) changeToMode(t *testing.T, ep uint16, clusterID uint32, mode uint8) (status uint64, text string) {
	t.Helper()
	id, fields, imStatus, isStatus := invokeResult(t, h.invoke(ep, clusterID, modebase.CmdChangeToMode, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(mode))
	}))
	if isStatus || id != modebase.CmdChangeToModeResponse {
		t.Fatalf("ChangeToMode answered command 0x%02X / status %v, want ChangeToModeResponse", id, imStatus)
	}
	return fields.mustChild(t, 0).El.Uint, fields.mustChild(t, 1).El.String
}

func TestModeBaseOverTheWire(t *testing.T) {
	t.Parallel()
	runDev, cleanDev := &modeDevice{}, &modeDevice{}
	run := newRunMode(t, runDev)
	clean, err := modebase.NewRvcCleanMode(modebase.Config{
		Changer: cleanDev, Features: modebase.FeatureDirectModeChange, CurrentMode: 1,
		SupportedModes: []modebase.ModeOption{
			{Label: "Vacuum", Mode: 1, Tags: []modebase.ModeTag{{Value: modebase.RvcCleanTagVacuum}}},
			{Label: "Vacuum, then mop", Mode: 2, Tags: []modebase.ModeTag{{Value: modebase.RvcCleanTagVacuumThenMop}, {Value: modebase.RvcCleanTagMop}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rvc, err := opstate.NewRvcServer(opstate.Config{
		States:     []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateError}, {ID: opstate.StateDocked}},
		DeviceType: opstate.DeviceTypeRoboticVacuumCleaner,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: opstate.DeviceTypeRoboticVacuumCleaner, servers: []contract.ClusterServer{rvc, run, clean}})
	ep := h.endpoints[opstate.DeviceTypeRoboticVacuumCleaner]

	modes, _, _ := h.readAttribute(ep, modebase.ClusterIDRvcCleanMode, modebase.AttrSupportedModes)
	if len(modes.Children) != 2 || modes.Children[1].mustChild(t, 0).El.String != "Vacuum, then mop" ||
		len(modes.Children[1].mustChild(t, 2).Children) != 2 {
		t.Errorf("SupportedModes = %+v", modes)
	}
	if fm, _, _ := h.readAttribute(ep, modebase.ClusterIDRvcCleanMode, 0xFFFC); fm.El.Uint != 1<<20 {
		t.Errorf("RvcCleanMode FeatureMap = 0x%X", fm.El.Uint)
	}

	// RvcRunMode: an accepted change moves CurrentMode.
	if st, text := h.changeToMode(t, ep, modebase.ClusterIDRvcRunMode, 1); st != 0 || text != "" {
		t.Errorf("ChangeToMode 1 = %d %q", st, text)
	}
	if v, _, _ := h.readAttribute(ep, modebase.ClusterIDRvcRunMode, modebase.AttrCurrentMode); v.El.Uint != 1 {
		t.Errorf("CurrentMode = %d", v.El.Uint)
	}
	// An unsupported mode never reaches the device.
	if st, text := h.changeToMode(t, ep, modebase.ClusterIDRvcRunMode, 5); st != uint64(modebase.StatusUnsupportedMode) || text != "Unsupported mode: 5" {
		t.Errorf("ChangeToMode 5 = %d %q", st, text)
	}
	// The device refuses with an RvcRunMode status.
	runDev.mu.Lock()
	runDev.refuse = modebase.StatusDustBinMissing
	runDev.mu.Unlock()
	if st, text := h.changeToMode(t, ep, modebase.ClusterIDRvcRunMode, 0); st != uint64(modebase.StatusDustBinMissing) || text != "dust bin missing" {
		t.Errorf("refused ChangeToMode = %d %q", st, text)
	}
	if len(runDev.asked) != 2 {
		t.Errorf("device asked %v", runDev.asked)
	}
	// Malformed requests.
	if _, _, status, _ := invokeResult(t, h.invoke(ep, modebase.ClusterIDRvcRunMode, modebase.CmdChangeToMode, nil)); status != im.StatusInvalidCommand {
		t.Errorf("ChangeToMode without NewMode = %v", status)
	}
	if _, _, status, _ := invokeResult(t, h.invoke(ep, modebase.ClusterIDRvcRunMode, modebase.CmdChangeToMode, func(enc *tlv.Encoder) {
		enc.PutUint16(tlv.ContextTag(0), 0x100)
	})); status != im.StatusConstraintError {
		t.Errorf("ChangeToMode 0x100 = %v", status)
	}
	if _, _, status, _ := invokeResult(t, h.invoke(ep, modebase.ClusterIDRvcRunMode, modebase.CmdChangeToMode, func(enc *tlv.Encoder) {
		enc.PutUTF8(tlv.ContextTag(0), "idle")
	})); status != im.StatusInvalidCommand {
		t.Errorf("ChangeToMode \"idle\" = %v", status)
	}
	if st := h.writeAttribute(ep, modebase.ClusterIDRvcRunMode, modebase.AttrCurrentMode, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 0)
	}); st != uint64(im.StatusUnsupportedWrite) {
		t.Errorf("CurrentMode write status 0x%02X", st)
	}
}

// TestLaundryWasherModeOverTheWire mounts LaundryWasherMode next to the
// washer's OperationalState and changes its mode.
func TestLaundryWasherModeOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &modeDevice{}
	mode, err := modebase.NewLaundryWasherMode(modebase.Config{
		Changer: dev,
		SupportedModes: []modebase.ModeOption{
			{Label: "Normal", Mode: 0, Tags: []modebase.ModeTag{{Value: modebase.LaundryTagNormal}}},
			{Label: "Whites", Mode: 3, Tags: []modebase.ModeTag{{Value: modebase.LaundryTagWhites}, {Value: modebase.TagMax}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := opstate.NewServer(opstate.Config{
		States: []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateError}}, DeviceType: opstate.DeviceTypeLaundryDryer,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: opstate.DeviceTypeLaundryDryer, servers: []contract.ClusterServer{state, mode}})
	ep := h.endpoints[opstate.DeviceTypeLaundryDryer]
	if st, _ := h.changeToMode(t, ep, clusterwire.LaundryWasherModeClusterID, 3); st != 0 {
		t.Errorf("ChangeToMode 3 = %d", st)
	}
	if v, _, _ := h.readAttribute(ep, clusterwire.LaundryWasherModeClusterID, modebase.AttrCurrentMode); v.El.Uint != 3 {
		t.Errorf("CurrentMode = %d", v.El.Uint)
	}
}

// TestOperationalStateChangesReachASubscriber: a state change the host
// makes through the server marks exactly the attributes that moved dirty,
// and CountdownTime — quality Q — reports its first value at once and then
// holds a change made within the second.
func TestOperationalStateChangesReachASubscriber(t *testing.T) {
	t.Parallel()
	srv, err := opstate.NewServer(opstate.Config{
		States:        []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StateError}},
		CountdownTime: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: opstate.DeviceTypeDishwasher, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[opstate.DeviceTypeDishwasher]
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: h.fabric, PeerNodeID: harnessControllerNodeID, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, HasCluster: true, Endpoint: ep, Cluster: opstate.ClusterIDOperationalState}},
	}); err != nil {
		t.Fatal(err)
	}
	h.bridge.AttachSubscriptionManager(mgr)
	tick := time.Now()
	dirty := func() []uint32 {
		tick = tick.Add(2 * time.Second)
		mgr.Tick(context.Background(), tick)
		spy.mu.Lock()
		defer spy.mu.Unlock()
		var out []uint32
		for _, call := range spy.calls {
			for _, p := range call {
				out = append(out, p.Attribute)
			}
		}
		spy.calls = nil
		return out
	}
	if err := srv.SetOperationalState(opstate.StateRunning); err != nil {
		t.Fatal(err)
	}
	if got := dirty(); !slices.Equal(got, []uint32{opstate.AttrOperationalState}) {
		t.Errorf("after a state change: dirty %v", got)
	}
	ten, nine := uint32(10), uint32(9)
	if err := srv.SetCountdownTime(&ten); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetCountdownTime(&nine); err != nil {
		t.Fatal(err)
	}
	if got := dirty(); !slices.Equal(got, []uint32{opstate.AttrCountdownTime}) {
		t.Errorf("after null → 10 → 9 within a second: dirty %v, want CountdownTime once", got)
	}
}
