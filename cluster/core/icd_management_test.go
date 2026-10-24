// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

var icdEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// icdStore is a memstore with fabrics 1 and 2.
func icdStore(t *testing.T) *memstore.Store {
	t.Helper()
	st := memstore.New()
	for i := range 2 {
		b := byte(i + 1)
		if _, err := st.AddFabric(t.Context(), store.FabricRecord{
			FabricID: uint64(b), RootPublicKey: bytes.Repeat([]byte{b}, 65), RootCert: []byte{b}, VendorID: 0xFFF1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// invokeCtx is a command's context on fabric, with privilege granted.
func icdInvokeCtx(fabric uint8, privilege uint8) context.Context {
	ctx := im.WithFabricFilter(context.Background(), true, fabric)
	ctx = im.WithCommandPath(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: core.ICDManagementClusterID})
	return im.WithAuthority(ctx, func(_ uint16, _ uint32, want uint8) im.StatusCode {
		if want <= privilege {
			return im.StatusSuccess
		}
		return im.StatusUnsupportedAccess
	})
}

const (
	privManage     = uint8(spec.PrivilegeManage)
	privAdminister = uint8(spec.PrivilegeAdminister)
)

func icdKey(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

func icdStatus(err error) im.StatusCode {
	var se im.StatusCodeError
	if errors.As(err, &se) {
		return se.MatterStatusCode()
	}
	return 0
}

func newICDServer(t *testing.T, cfg core.ICDManagementConfig) *core.ICDManagement {
	t.Helper()
	s, err := core.NewICDManagement(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func icdSit() *icddef.OperatingModeEnum {
	m := icddef.OperatingModeSit
	return &m
}

func TestICDManagementConfigValidate(t *testing.T) {
	t.Parallel()
	litFeatures := icddef.FeatureCheckInProtocolSupport | icddef.FeatureUserActiveModeTrigger | icddef.FeatureLongIdleTimeSupport
	for _, tc := range []struct {
		name string
		cfg  core.ICDManagementConfig
		ok   bool
	}{
		{"defaults", core.ICDManagementConfig{}, true},
		{"idle shorter than active", core.ICDManagementConfig{IdleModeDuration: 1, ActiveModeDuration: 5000}, false},
		{"backoff shorter than idle", core.ICDManagementConfig{IdleModeDuration: 3600, MaximumCheckInBackoff: 1800}, false},
		{"idle beyond 64800", core.ICDManagementConfig{IdleModeDuration: 64801, MaximumCheckInBackoff: 64801}, false},
		{"LITS without CIP", core.ICDManagementConfig{Features: icddef.FeatureLongIdleTimeSupport, ActiveModeThreshold: 5000, OperatingMode: icdSit()}, false},
		{"DSLS without LITS", core.ICDManagementConfig{Features: icddef.FeatureCheckInProtocolSupport | icddef.FeatureDynamicSitLitSupport}, false},
		{"LIT threshold under 5 s", core.ICDManagementConfig{Features: litFeatures, ActiveModeThreshold: 1000, OperatingMode: icdSit()}, false},
		{"LIT threshold exactly 5 s", core.ICDManagementConfig{Features: litFeatures, ActiveModeThreshold: 5000, OperatingMode: icdSit()}, true},
		{"LIT without OperatingMode", core.ICDManagementConfig{Features: litFeatures, ActiveModeThreshold: 5000}, false},
		{"UAT hint needing an instruction", core.ICDManagementConfig{Features: icddef.FeatureCheckInProtocolSupport | icddef.FeatureUserActiveModeTrigger, UserActiveModeTriggerHint: icddef.UserActiveModeTriggerResetButtonSeconds}, false},
		{"UAT hint without instruction need", core.ICDManagementConfig{Features: icddef.FeatureCheckInProtocolSupport | icddef.FeatureUserActiveModeTrigger, UserActiveModeTriggerHint: icddef.UserActiveModeTriggerPowerCycle}, true},
		{"UAT instruction too long", core.ICDManagementConfig{Features: icddef.FeatureCheckInProtocolSupport | icddef.FeatureUserActiveModeTrigger, UserActiveModeTriggerInstruction: string(make([]byte, 129))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if tc.ok != (err == nil) {
				t.Fatalf("Validate = %v, want ok %v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, core.ErrICDManagementConfig) {
				t.Fatalf("%v does not wrap ErrICDManagementConfig", err)
			}
			if _, nerr := core.NewICDManagement(tc.cfg); (nerr == nil) != tc.ok {
				t.Fatalf("NewICDManagement = %v", nerr)
			}
		})
	}
}

func TestICDManagementDefaults(t *testing.T) {
	t.Parallel()
	s := newICDServer(t, core.ICDManagementConfig{})
	for attr, want := range map[uint32]any{
		icddef.AttrIdleModeDuration:          uint32(1),
		icddef.AttrActiveModeDuration:        uint32(300),
		icddef.AttrActiveModeThreshold:       uint16(300),
		icddef.AttrClientsSupportedPerFabric: uint16(1),
		icddef.AttrMaximumCheckInBackoff:     uint32(1),
		icddef.AttrIcdCounter:                uint32(0),
		cluster.AttrGlobalFeatureMap:         uint32(icddef.FeatureCheckInProtocolSupport),
	} {
		if v, ok := s.MatterRead(attr); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", attr, v, ok, want)
		}
	}
	if _, ok := s.MatterRead(icddef.AttrOperatingMode); ok {
		t.Error("OperatingMode served without LITS")
	}
	if err := s.MatterWrite(context.Background(), icddef.AttrIdleModeDuration, uint32(2)); err == nil {
		t.Error("write accepted")
	}
	if _, err := s.MatterInvoke(context.Background(), 0xFF, nil); err == nil {
		t.Error("unknown command accepted")
	}
	if !slices.Contains(s.MatterAcceptedCommands(), icddef.CmdStayActiveRequest) || !slices.Contains(s.MatterGeneratedCommands(), icddef.CmdRegisterClientResponse) {
		t.Errorf("commands %v / %v", s.MatterAcceptedCommands(), s.MatterGeneratedCommands())
	}
	if len(s.MatterAttributes()) == 0 || len(s.MatterReportable()) == 0 {
		t.Error("no attributes listed")
	}
}

// recorder is a Check-In send that records what it sent.
type icdRecorder struct {
	mu   sync.Mutex
	sent []icd.CheckInRequest
}

func (r *icdRecorder) send(_ context.Context, req icd.CheckInRequest) (bool, error) {
	r.mu.Lock()
	r.sent = append(r.sent, req)
	r.mu.Unlock()
	return true, nil
}

func (r *icdRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func icdWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestICDManagementRegistrationLifecycle(t *testing.T) {
	t.Parallel()
	st := icdStore(t)
	rec := &icdRecorder{}
	cfg := core.ICDManagementConfig{
		IdleModeDuration: 4, MaximumCheckInBackoff: 4, ActiveModeDuration: 2000, ActiveModeThreshold: 1000,
		ClientsSupportedPerFabric: 2, Store: st, Clock: clocktest.New(icdEpoch),
	}
	s := newICDServer(t, cfg)
	s.SetCheckInSender(rec.send)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c := s.Counter(); c == nil || c.Value() <= icd.BootBump {
		t.Fatalf("counter %v", c)
	}
	persisted, ok, err := st.LoadICDCounter(t.Context())
	if err != nil || !ok || persisted != s.Counter().Value() {
		t.Fatalf("persisted counter %d %v %v", persisted, ok, err)
	}

	admin := icdInvokeCtx(1, privAdminister)
	reg := func(ctx context.Context, node uint64, k []byte, vk *[]byte, ct icddef.ClientTypeEnum) error {
		_, err := s.MatterInvoke(ctx, icddef.CmdRegisterClient, icddef.RegisterClientRequest{
			CheckInNodeId: node, MonitoredSubject: node, Key: k, VerificationKey: vk, ClientType: ct,
		})
		return err
	}
	if err := reg(admin, 0x10, icdKey(1), nil, icddef.ClientTypePermanent); err != nil {
		t.Fatal(err)
	}
	if err := reg(admin, 0x11, icdKey(2), nil, icddef.ClientTypePermanent); err != nil {
		t.Fatal(err)
	}
	if err := reg(admin, 0x12, icdKey(3), nil, icddef.ClientTypePermanent); icdStatus(err) != im.StatusResourceExhausted {
		t.Fatalf("third registration = %v", err)
	}
	if err := reg(icdInvokeCtx(2, privManage), 0x20, icdKey(4), nil, icddef.ClientTypeEphemeral); err != nil {
		t.Fatal(err)
	}
	// A Manage update needs the stored key.
	manage := icdInvokeCtx(1, privManage)
	wrong, right := icdKey(9), icdKey(1)
	if err := reg(manage, 0x10, icdKey(5), &wrong, icddef.ClientTypePermanent); icdStatus(err) != im.StatusFailure {
		t.Fatalf("wrong verification key = %v", err)
	}
	if err := reg(manage, 0x10, icdKey(5), nil, icddef.ClientTypePermanent); icdStatus(err) != im.StatusFailure {
		t.Fatalf("missing verification key = %v", err)
	}
	if err := reg(manage, 0x10, icdKey(5), &right, icddef.ClientTypePermanent); err != nil {
		t.Fatal(err)
	}
	if got := s.Registrations(1); len(got) != 2 || !bytes.Equal(got[0].Key, icdKey(5)) {
		t.Fatalf("fabric 1 registrations %+v", got)
	}

	// The fabric-filtered read lists one fabric, the unfiltered one every
	// fabric with another fabric's entry reduced to its index.
	filtered, _ := s.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 2), icddef.AttrRegisteredClients)
	if l := filtered.(spec.List[icddef.MonitoringRegistrationStruct]); len(l) != 1 || l[0].CheckInNodeId != 0x20 {
		t.Fatalf("filtered %+v", l)
	}
	unfiltered, _ := s.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 2), icddef.AttrRegisteredClients)
	if l := unfiltered.(spec.List[icddef.MonitoringRegistrationStruct]); len(l) != 3 || l[0].CheckInNodeId != 0 || l[0].FabricIndex != 1 || l[2].CheckInNodeId != 0x20 {
		t.Fatalf("unfiltered %+v", l)
	}

	// A wake sends one Check-In per Permanent client, post-increment.
	before := s.Counter().Value()
	s.EnterIdleMode()
	s.RequestActiveMode()
	icdWaitFor(t, "two Check-Ins", func() bool { return rec.count() == 2 })
	s.Stop()
	if got := s.Counter().Value(); got != before+2 {
		t.Fatalf("counter %d, want %d", got, before+2)
	}

	// Unregister: Manage with the key, NOT_FOUND for an unknown client.
	if _, err := s.MatterInvoke(manage, icddef.CmdUnregisterClient, icddef.UnregisterClientRequest{CheckInNodeId: 0x99}); icdStatus(err) != im.StatusNotFound {
		t.Fatalf("unknown client = %v", err)
	}
	vk := icdKey(2)
	if _, err := s.MatterInvoke(manage, icddef.CmdUnregisterClient, icddef.UnregisterClientRequest{CheckInNodeId: 0x11, VerificationKey: &vk}); err != nil {
		t.Fatal(err)
	}

	// A new server on the same store restores the registrations.
	again := newICDServer(t, cfg)
	if err := again.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := again.Registrations(1); len(got) != 1 || got[0].CheckInNodeID != 0x10 {
		t.Fatalf("restored %+v", got)
	}
	if again.Counter().Value() <= s.Counter().Value() {
		t.Fatalf("counter did not advance across a restart: %d after %d", again.Counter().Value(), s.Counter().Value())
	}

	// Fabric removal drops the fabric's registrations.
	if err := st.RemoveFabric(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	again.FabricRemoved(t.Context(), 2)
	if got := again.Registrations(2); len(got) != 0 {
		t.Fatalf("fabric 2 kept %+v", got)
	}
}

func TestICDManagementPrunesDepartedFabrics(t *testing.T) {
	t.Parallel()
	st := icdStore(t)
	if err := st.ReplaceICDRegistrations(t.Context(), 2, []store.ICDRegistration{{CheckInNodeID: 0x1234, MonitoredSubject: 0x1234, Key: icdKey(7)}}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveFabric(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	s := newICDServer(t, core.ICDManagementConfig{Store: st, Clock: clocktest.New(icdEpoch)})
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if regs, _ := st.ListICDRegistrations(t.Context()); len(regs) != 0 {
		t.Fatalf("persisted %+v", regs)
	}
}

func TestICDManagementOperatingMode(t *testing.T) {
	t.Parallel()
	feat := icddef.FeatureCheckInProtocolSupport | icddef.FeatureUserActiveModeTrigger | icddef.FeatureLongIdleTimeSupport
	s := newICDServer(t, core.ICDManagementConfig{Features: feat | icddef.FeatureDynamicSitLitSupport, ActiveModeThreshold: 5000, OperatingMode: icdSit(), Clock: clocktest.New(icdEpoch)})
	changed := 0
	s.SetOnOperatingModeChanged(func() { changed++ })
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if a := s.Advertisement(); a.Mode != icddef.OperatingModeSit || a.OmitIdleInterval || a.ActiveThreshold != 5*time.Second {
		t.Fatalf("advertisement %+v", a)
	}
	if err := s.SetOperatingMode(icddef.OperatingModeLit); err != nil {
		t.Fatal(err)
	}
	if a := s.Advertisement(); a.Mode != icddef.OperatingModeLit || !a.OmitIdleInterval || changed != 1 {
		t.Fatalf("forced LIT %+v, %d changes", a, changed)
	}
	if err := s.WithdrawForcedOperatingMode(); err != nil || s.OperatingMode() != icddef.OperatingModeSit || changed != 2 {
		t.Fatalf("withdraw %v, mode %v, %d changes", err, s.OperatingMode(), changed)
	}

	plain := newICDServer(t, core.ICDManagementConfig{Features: feat, ActiveModeThreshold: 5000, OperatingMode: icdSit()})
	if err := plain.SetOperatingMode(icddef.OperatingModeLit); !errors.Is(err, core.ErrICDOperatingMode) {
		t.Fatalf("non-DSLS force = %v", err)
	}
	if err := plain.SetOperatingMode(icddef.OperatingModeSit); err != nil {
		t.Fatalf("non-DSLS matching mode = %v", err)
	}
	if err := plain.WithdrawForcedOperatingMode(); !errors.Is(err, core.ErrICDOperatingMode) {
		t.Fatalf("non-DSLS withdraw = %v", err)
	}
	cip := newICDServer(t, core.ICDManagementConfig{})
	if err := cip.SetOperatingMode(icddef.OperatingModeLit); !errors.Is(err, core.ErrICDOperatingMode) {
		t.Fatalf("CIP-only force = %v", err)
	}
}

func TestICDManagementStayActiveAndEvents(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(icdEpoch)
	s := newICDServer(t, core.ICDManagementConfig{IdleModeDuration: 4, MaximumCheckInBackoff: 4, ActiveModeDuration: 2000, ActiveModeThreshold: 1000, Clock: fake})
	var mu sync.Mutex
	var events []string
	unsubscribe := s.OnModeEvent(func(e core.ICDModeEvent) {
		mu.Lock()
		events = append(events, e.String())
		mu.Unlock()
	})
	defer unsubscribe()
	stay := func(ms uint32) uint32 {
		t.Helper()
		resp, err := s.MatterInvoke(icdInvokeCtx(1, privManage), icddef.CmdStayActiveRequest, icddef.StayActiveRequestRequest{StayActiveDuration: ms})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(icddef.StayActiveResponse).PromisedActiveDuration
	}
	if got := stay(20); got != 20 {
		t.Fatalf("stopped server promised %d", got)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.EnterIdleMode()
	if got := stay(10); got != 2000 {
		t.Fatalf("short request promised %d", got)
	}
	fake.Advance(2 * time.Second)
	s.NoteActivity()
	s.TriggerUserActiveMode()
	mu.Lock()
	got := slices.Clone(events)
	mu.Unlock()
	if !slices.Equal(got, []string{"activeModeEntered", "idleModeEntered", "activeModeEntered", "mayEnterIdleMode"}) {
		t.Fatalf("events %v", got)
	}
	if s.Mode() != icd.ModeActive {
		t.Fatal(s.Mode())
	}
}

func TestICDManagementRefusals(t *testing.T) {
	t.Parallel()
	s := newICDServer(t, core.ICDManagementConfig{})
	if _, err := s.MatterInvoke(icdInvokeCtx(0, privAdminister), icddef.CmdRegisterClient, icddef.RegisterClientRequest{Key: icdKey(1)}); icdStatus(err) != im.StatusUnsupportedAccess {
		t.Fatalf("no fabric = %v", err)
	}
	for _, cmd := range []uint32{icddef.CmdRegisterClient, icddef.CmdUnregisterClient, icddef.CmdStayActiveRequest} {
		if _, err := s.MatterInvoke(icdInvokeCtx(1, privAdminister), cmd, "junk"); icdStatus(err) != im.StatusInvalidCommand {
			t.Errorf("command 0x%02X with junk = %v", cmd, err)
		}
	}
}
