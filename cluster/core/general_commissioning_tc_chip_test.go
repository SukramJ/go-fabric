// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

// The TermsAndConditions feature after connectedhomeip
// (general_commissioning_tc.go): the cases of the CSA's TC-CGEN-2.5 to
// 2.11 (src/python_testing/TC_CGEN_2_5.py … TC_CGEN_2_11.py at 6170af84)
// and of src/app/clusters/general-commissioning-server/tests/
// TestGeneralCommissioningCluster.cpp, ported by meaning onto the server.

// settingsFake is an in-memory store.Settings with DeleteSetting.
type settingsFake struct {
	mu   sync.Mutex
	kv   map[string]string
	fail error
}

func newSettingsFake() *settingsFake { return &settingsFake{kv: map[string]string{}} }

func (f *settingsFake) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.kv[key]
	return v, ok, f.fail
}

func (f *settingsFake) SetSetting(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.kv[key] = value
	return nil
}

func (f *settingsFake) DeleteSetting(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.kv, key)
	return f.fail
}

func (f *settingsFake) GetMetadataCounter(context.Context, string) (value uint64, ok bool, err error) {
	return 0, false, nil
}

func (f *settingsFake) SetMetadataCounter(context.Context, string, uint64) error { return nil }

var _ store.Settings = (*settingsFake)(nil)

const tcFabric uint8 = 1

// newTermsGencomm builds the server with the requirement version 1,
// acknowledgement bit 1 (PIXIT.CGEN.TCRevision / RequiredTCAcknowledgements
// of the CGEN cases) over st.
func newTermsGencomm(t *testing.T, st store.Settings, deadline *uint32) *core.GeneralCommissioning {
	t.Helper()
	gc := newGencomm(t, core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoorOutdoor,
		TermsAndConditions: &core.TermsAndConditionsConfig{RequiredVersion: 1, RequiredAcknowledgements: 1, UpdateDeadline: deadline},
		Settings:           st,
	})
	if err := gc.RestoreTermsAcceptance(context.Background()); err != nil {
		t.Fatalf("RestoreTermsAcceptance: %v", err)
	}
	return gc
}

func caseCtx() context.Context { return im.WithFabricFilter(context.Background(), false, tcFabric) }

func readUint16(t *testing.T, gc *core.GeneralCommissioning, attr uint32) uint16 {
	t.Helper()
	v, ok := gc.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X: not served", attr)
	}
	u, isU := v.(uint16)
	if !isU {
		t.Fatalf("attribute 0x%04X: %T, want uint16", attr, v)
	}
	return u
}

func readBool(t *testing.T, gc *core.GeneralCommissioning, attr uint32) bool {
	t.Helper()
	v, ok := gc.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X: not served", attr)
	}
	b, isB := v.(bool)
	if !isB {
		t.Fatalf("attribute 0x%04X: %T, want bool", attr, v)
	}
	return b
}

func setTerms(ctx context.Context, t *testing.T, gc *core.GeneralCommissioning, version, acks uint16) gencommdef.CommissioningErrorEnum {
	t.Helper()
	resp, err := gc.MatterInvoke(ctx, gencommdef.CmdSetTcAcknowledgements, gencommdef.SetTcAcknowledgementsRequest{TcVersion: version, TcUserResponse: acks})
	if err != nil {
		t.Fatalf("SetTCAcknowledgements: %v", err)
	}
	return resp.(gencommdef.SetTcAcknowledgementsResponse).ErrorCode
}

func armFailSafe(ctx context.Context, t *testing.T, gc *core.GeneralCommissioning, seconds uint16) {
	t.Helper()
	resp, err := gc.MatterInvoke(ctx, gencommdef.CmdArmFailSafe, core.ArmFailSafeRequest{ExpiryLengthSeconds: seconds})
	if err != nil {
		t.Fatalf("ArmFailSafe: %v", err)
	}
	if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: error code %d", code)
	}
}

func commissioningComplete(ctx context.Context, t *testing.T, gc *core.GeneralCommissioning) uint8 {
	t.Helper()
	resp, err := gc.MatterInvoke(ctx, gencommdef.CmdCommissioningComplete, nil)
	if err != nil {
		t.Fatalf("CommissioningComplete: %v", err)
	}
	return resp.(core.CommissioningCompleteResponse).ErrorCode
}

// Without the requirement the surface is the one of before: no feature,
// no TC attribute, SetTCAcknowledgements unsupported.
func TestGencommTerms_OffWithoutRequirement(t *testing.T) {
	t.Parallel()
	gc := defaultGencomm(t)
	if fm, _ := gc.MatterRead(cluster.AttrGlobalFeatureMap); fm.(uint32) != 0 {
		t.Fatalf("FeatureMap = %v, want 0", fm)
	}
	if _, ok := gc.MatterRead(gencommdef.AttrTcAcceptedVersion); ok {
		t.Fatal("TcAcceptedVersion served without the feature")
	}
	if slices.Contains(gc.MatterAcceptedCommands(), gencommdef.CmdSetTcAcknowledgements) {
		t.Fatal("SetTCAcknowledgements accepted without the feature")
	}
	_, err := gc.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1})
	if !errors.Is(err, im.ErrUnsupportedCommand) {
		t.Fatalf("SetTCAcknowledgements without the feature: %v, want unsupported command", err)
	}
	if gc.TermsAndConditions() != nil {
		t.Fatal("TermsAndConditions() != nil")
	}
}

// TC-CGEN-2.5 steps 2-4: the surface with the requirement, before any
// acceptance.
func TestGencommTerms_SurfaceAndInitialReads(t *testing.T) {
	t.Parallel()
	deadline := uint32(1_700_000_000)
	gc := newTermsGencomm(t, newSettingsFake(), &deadline)
	if fm, _ := gc.MatterRead(cluster.AttrGlobalFeatureMap); fm.(uint32)&uint32(gencommdef.FeatureTermsAndConditions) == 0 {
		t.Fatalf("FeatureMap = %v, want the TC bit", fm)
	}
	for _, attr := range []uint32{
		gencommdef.AttrTcAcceptedVersion, gencommdef.AttrTcMinRequiredVersion, gencommdef.AttrTcAcknowledgements,
		gencommdef.AttrTcAcknowledgementsRequired, gencommdef.AttrTcUpdateDeadline,
	} {
		if !slices.Contains(gc.MatterAttributes(), attr) {
			t.Errorf("attribute 0x%04X not listed", attr)
		}
		if !slices.Contains(gc.MatterReportable(), attr) {
			t.Errorf("attribute 0x%04X not reportable", attr)
		}
	}
	if !slices.Contains(gc.MatterAcceptedCommands(), gencommdef.CmdSetTcAcknowledgements) ||
		!slices.Contains(gc.MatterGeneratedCommands(), gencommdef.CmdSetTcAcknowledgementsResponse) {
		t.Fatal("SetTCAcknowledgements and its response not listed")
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion = %d, want 0 before any acceptance", got)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcMinRequiredVersion); got != 1 {
		t.Fatalf("TcMinRequiredVersion = %d, want 1", got)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcknowledgements); got != 0 {
		t.Fatalf("TcAcknowledgements = %d, want 0", got)
	}
	if !readBool(t, gc, gencommdef.AttrTcAcknowledgementsRequired) {
		t.Fatal("TcAcknowledgementsRequired = false before any acceptance (TC-CGEN-2.5 step 2)")
	}
	v, _ := gc.MatterRead(gencommdef.AttrTcUpdateDeadline)
	if n, ok := v.(spec.Nullable[uint32]); !ok || n.Null || n.Value != deadline {
		t.Fatalf("TcUpdateDeadline = %#v, want %d", v, deadline)
	}
	gcNoDeadline := newTermsGencomm(t, newSettingsFake(), nil)
	v, _ = gcNoDeadline.MatterRead(gencommdef.AttrTcUpdateDeadline)
	if n, ok := v.(spec.Nullable[uint32]); !ok || !n.Null {
		t.Fatalf("TcUpdateDeadline = %#v, want null", v)
	}
	if tc := gc.TermsAndConditions(); tc == nil || tc.RequiredVersion != 1 || tc.RequiredAcknowledgements != 1 {
		t.Fatalf("TermsAndConditions() = %+v", tc)
	}
}

// TC-CGEN-2.5: an acceptance during the fail-safe window is tentative;
// CommissioningComplete commits it, TcAcknowledgementsRequired flips.
func TestGencommTerms_AcceptanceCommitsAtCommissioningComplete(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	var changed []uint32
	var mu sync.Mutex
	gc.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		defer mu.Unlock()
		changed = append(changed, c.Attributes...)
	})
	ctx := caseCtx()
	armFailSafe(ctx, t, gc, 60)
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("SetTCAcknowledgements = %d, want OK", code)
	}
	if readBool(t, gc, gencommdef.AttrTcAcknowledgementsRequired) {
		t.Fatal("TcAcknowledgementsRequired still true after the acceptance (TC-CGEN-2.5 step 6)")
	}
	if _, ok := st.kv[store.SettingTermsAcceptance]; ok {
		t.Fatal("a tentative acceptance was persisted before CommissioningComplete")
	}
	if code := commissioningComplete(ctx, t, gc); code != core.CommissioningErrorOK {
		t.Fatalf("CommissioningComplete = %d, want OK", code)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 1 {
		t.Fatalf("TcAcceptedVersion = %d, want 1 (TC-CGEN-2.5 step 9)", got)
	}
	if v := st.kv[store.SettingTermsAcceptance]; v != "1:1" {
		t.Fatalf("persisted acceptance %q, want 1:1", v)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, attr := range []uint32{gencommdef.AttrTcAcceptedVersion, gencommdef.AttrTcAcknowledgements, gencommdef.AttrTcAcknowledgementsRequired} {
		if !slices.Contains(changed, attr) {
			t.Errorf("attribute 0x%04X change not reported", attr)
		}
	}
}

// TC-CGEN-2.6: CommissioningComplete without any acceptance is
// TCAcknowledgementsNotReceived.
func TestGencommTerms_CommissioningCompleteWithoutAcceptance(t *testing.T) {
	t.Parallel()
	gc := newTermsGencomm(t, newSettingsFake(), nil)
	ctx := caseCtx()
	armFailSafe(ctx, t, gc, 60)
	if code := commissioningComplete(ctx, t, gc); code != uint8(gencommdef.CommissioningErrorTcAcknowledgementsNotReceived) {
		t.Fatalf("CommissioningComplete = %d, want TCAcknowledgementsNotReceived (6)", code)
	}
	if !gc.FailSafeArmed() {
		t.Fatal("the refusal disarmed the fail-safe")
	}
}

// TC-CGEN-2.7 and 2.10: a version below the requirement is
// TCMinVersionNotMet, missing bits are RequiredTCNotAccepted; neither
// touches the acceptance in force, and CommissioningComplete judges the
// one in force.
func TestGencommTerms_InvalidAcceptanceIsRefused(t *testing.T) {
	t.Parallel()
	gc := newTermsGencomm(t, newSettingsFake(), nil)
	ctx := caseCtx()
	armFailSafe(ctx, t, gc, 60)
	if code := setTerms(ctx, t, gc, 0, 0xFFFF); code != gencommdef.CommissioningErrorTcMinVersionNotMet {
		t.Fatalf("version 0: %d, want TCMinVersionNotMet (7)", code)
	}
	if code := setTerms(ctx, t, gc, 1, 0); code != gencommdef.CommissioningErrorRequiredTcNotAccepted {
		t.Fatalf("bits 0: %d, want RequiredTCNotAccepted (5)", code)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion = %d after refused acceptances, want 0", got)
	}
	if code := commissioningComplete(ctx, t, gc); code != uint8(gencommdef.CommissioningErrorTcAcknowledgementsNotReceived) {
		t.Fatalf("CommissioningComplete = %d, want TCAcknowledgementsNotReceived", code)
	}
	// A valid acceptance, then CommissioningComplete succeeds (2.7 steps 6-7).
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("valid acceptance: %d", code)
	}
	if code := commissioningComplete(ctx, t, gc); code != core.CommissioningErrorOK {
		t.Fatalf("CommissioningComplete = %d, want OK", code)
	}
	// 2.10 after commissioning: invalid acceptances leave the committed one.
	if code := setTerms(ctx, t, gc, 0, 0xFFFF); code != gencommdef.CommissioningErrorTcMinVersionNotMet {
		t.Fatalf("version 0 after commissioning: %d", code)
	}
	if code := setTerms(ctx, t, gc, 2, 0); code != gencommdef.CommissioningErrorRequiredTcNotAccepted {
		t.Fatalf("bits 0 after commissioning: %d", code)
	}
	if v, a := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion), readUint16(t, gc, gencommdef.AttrTcAcknowledgements); v != 1 || a != 1 {
		t.Fatalf("acceptance moved to %d/%d after refused updates", v, a)
	}
}

// TC-CGEN-2.11: an acceptance outside a fail-safe window commits at once.
func TestGencommTerms_UpdateAfterCommissioningCommitsAtOnce(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	ctx := caseCtx()
	if code := setTerms(ctx, t, gc, 2, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("SetTCAcknowledgements = %d", code)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 2 {
		t.Fatalf("TcAcceptedVersion = %d, want 2", got)
	}
	if v := st.kv[store.SettingTermsAcceptance]; v != "2:1" {
		t.Fatalf("persisted %q, want 2:1", v)
	}
	if code := setTerms(ctx, t, gc, 2, 0xFFFF); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("SetTCAcknowledgements all bits = %d", code)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcknowledgements); got != 0xFFFF {
		t.Fatalf("TcAcknowledgements = %d, want 65535", got)
	}
	// The same acceptance again changes nothing and is OK.
	if code := setTerms(ctx, t, gc, 2, 0xFFFF); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("repeated acceptance = %d", code)
	}
}

// A fail-safe that expires or is disarmed reverts the window's tentative
// acceptance (chip OnPlatformEventHandler kFailSafeTimerExpired); one it
// did not set stays.
func TestGencommTerms_FailSafeExpiryRevertsTentativeAcceptance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		end  func(ctx context.Context, gc *core.GeneralCommissioning)
	}{
		{"expire", func(ctx context.Context, gc *core.GeneralCommissioning) { gc.ExpireFailSafe(ctx) }},
		{"disarm", func(ctx context.Context, gc *core.GeneralCommissioning) {
			_, _ = gc.MatterInvoke(ctx, gencommdef.CmdArmFailSafe, core.ArmFailSafeRequest{ExpiryLengthSeconds: 0})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := newSettingsFake()
			gc := newTermsGencomm(t, st, nil)
			ctx := caseCtx()
			// A committed acceptance from an earlier commissioning.
			if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
				t.Fatalf("committed acceptance: %d", code)
			}
			armFailSafe(ctx, t, gc, 60)
			if code := setTerms(ctx, t, gc, 3, 0xFFFF); code != gencommdef.CommissioningErrorOk {
				t.Fatalf("tentative acceptance: %d", code)
			}
			if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 3 {
				t.Fatalf("TcAcceptedVersion = %d during the window, want 3", got)
			}
			tc.end(ctx, gc)
			if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 1 {
				t.Fatalf("TcAcceptedVersion = %d after the window, want the committed 1", got)
			}
			if v := st.kv[store.SettingTermsAcceptance]; v != "1:1" {
				t.Fatalf("persisted %q, want the committed 1:1", v)
			}
		})
	}
	t.Run("untouched window keeps the acceptance", func(t *testing.T) {
		t.Parallel()
		gc := newTermsGencomm(t, newSettingsFake(), nil)
		ctx := caseCtx()
		if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
			t.Fatalf("acceptance: %d", code)
		}
		armFailSafe(ctx, t, gc, 60)
		gc.ExpireFailSafe(ctx)
		if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 1 {
			t.Fatalf("TcAcceptedVersion = %d, want 1", got)
		}
	})
}

// TC-CGEN-2.8 (a restart keeps the committed acceptance; the factory reset
// erases the store) and TC-CGEN-2.9 (the last fabric's removal resets it).
func TestGencommTerms_PersistenceAndReset(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	ctx := caseCtx()
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("acceptance: %d", code)
	}
	restarted := newTermsGencomm(t, st, nil)
	if got := readUint16(t, restarted, gencommdef.AttrTcAcceptedVersion); got != 1 {
		t.Fatalf("TcAcceptedVersion after restart = %d, want 1", got)
	}
	if readBool(t, restarted, gencommdef.AttrTcAcknowledgementsRequired) {
		t.Fatal("TcAcknowledgementsRequired after restart = true")
	}
	if err := restarted.LastFabricRemoved(ctx); err != nil {
		t.Fatalf("LastFabricRemoved: %v", err)
	}
	if got := readUint16(t, restarted, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion after the last fabric = %d, want 0 (TC-CGEN-2.9 step 8)", got)
	}
	if !readBool(t, restarted, gencommdef.AttrTcAcknowledgementsRequired) {
		t.Fatal("TcAcknowledgementsRequired after the reset = false")
	}
	if _, ok := st.kv[store.SettingTermsAcceptance]; ok {
		t.Fatal("the reset left the persisted acceptance")
	}
	// A reset with nothing to reset is a no-op; an erased store restores
	// nothing (TC-CGEN-2.8 step 8).
	if err := restarted.LastFabricRemoved(ctx); err != nil {
		t.Fatalf("second LastFabricRemoved: %v", err)
	}
	fresh := newTermsGencomm(t, newSettingsFake(), nil)
	if got := readUint16(t, fresh, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion on an erased store = %d", got)
	}
}

// A malformed persisted acceptance refuses the restore; a failing store
// fails the commit.
func TestGencommTerms_StoreErrors(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	st.kv[store.SettingTermsAcceptance] = "garbage"
	gc := newGencomm(t, core.GeneralCommissioningConfig{
		TermsAndConditions: &core.TermsAndConditionsConfig{RequiredVersion: 1, RequiredAcknowledgements: 1},
		Settings:           st,
	})
	if err := gc.RestoreTermsAcceptance(context.Background()); err == nil {
		t.Fatal("RestoreTermsAcceptance accepted a malformed value")
	}
	failing := newSettingsFake()
	failing.fail = errors.New("disk")
	gc = newGencomm(t, core.GeneralCommissioningConfig{
		TermsAndConditions: &core.TermsAndConditionsConfig{RequiredVersion: 1, RequiredAcknowledgements: 1},
		Settings:           failing,
	})
	if _, err := gc.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1}); err == nil {
		t.Fatal("a failing store did not fail the commit")
	}
	if _, err := gc.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, "wrong"); err == nil {
		t.Fatal("a wrong payload type was accepted")
	}
	// The request may arrive as a pointer.
	ok := newTermsGencomm(t, newSettingsFake(), nil)
	resp, err := ok.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, &gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1})
	if err != nil || resp.(gencommdef.SetTcAcknowledgementsResponse).ErrorCode != gencommdef.CommissioningErrorOk {
		t.Fatalf("pointer request: %v / %v", resp, err)
	}
}

// The register row the feature implements is declared by the server's
// AheadSet (cluster/core/ahead.go), so the node tags the TC bit with it.
func TestGencommTerms_AheadRow(t *testing.T) {
	t.Parallel()
	const row = "BD-chip-GeneralCommissioning-TermsAndConditions"
	if got := core.GeneralCommissioningAhead.Row(surface.KindFeature, uint32(gencommdef.FeatureTermsAndConditions)); got != row {
		t.Fatalf("Row(TC) = %q, want %q", got, row)
	}
	if ids := core.GeneralCommissioningAhead.IDs(); len(ids) != 1 || ids[0] != row {
		t.Fatalf("IDs() = %v", ids)
	}
}

// A commit that fails at CommissioningComplete keeps the acceptance
// tentative: the fail-safe window's expiry still reverts it (chip keeps
// UpdateTermsAndConditionsHasBeenInvoked on a failed commit).
func TestGencommTerms_FailedCommitStaysTentative(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	ctx := caseCtx()
	armFailSafe(ctx, t, gc, 60)
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("acceptance: %d", code)
	}
	st.mu.Lock()
	st.fail = errors.New("disk")
	st.mu.Unlock()
	if _, err := gc.MatterInvoke(ctx, gencommdef.CmdCommissioningComplete, nil); err == nil {
		t.Fatal("CommissioningComplete succeeded with a failing store")
	}
	if !gc.FailSafeArmed() {
		t.Fatal("the failed commit disarmed the fail-safe")
	}
	gc.ExpireFailSafe(ctx)
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion = %d after the expiry, want the unpersisted acceptance reverted", got)
	}
	if _, ok := st.kv[store.SettingTermsAcceptance]; ok {
		t.Fatal("an acceptance was persisted by the failing store")
	}
}

// ArmFailSafeFor's disarm (the window's revocation fallback) reverts a
// tentative acceptance as the wire disarm does.
func TestGencommTerms_ArmFailSafeForZeroReverts(t *testing.T) {
	t.Parallel()
	gc := newTermsGencomm(t, newSettingsFake(), nil)
	ctx := caseCtx()
	armFailSafe(ctx, t, gc, 60)
	if code := setTerms(ctx, t, gc, 1, 1); code != gencommdef.CommissioningErrorOk {
		t.Fatalf("acceptance: %d", code)
	}
	if err := gc.ArmFailSafeFor(ctx, 0, tcFabric); err != nil {
		t.Fatal(err)
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TcAcceptedVersion = %d, want 0", got)
	}
}

// TestGencommTerms_FailedImmediateCommitLeavesNoAcceptance pins
// R2-P1-core-04: a SetTCAcknowledgements outside a fail-safe whose store
// write fails answers the error and leaves the acceptance as it was — not
// in force unpersisted, which would pass CommissioningComplete's gate, be
// lost at the next restart, and turn a retry into a no-op that never
// writes it.
func TestGencommTerms_FailedImmediateCommitLeavesNoAcceptance(t *testing.T) {
	t.Parallel()
	st := newSettingsFake()
	gc := newTermsGencomm(t, st, nil)
	v0 := gc.MatterDataVersion()
	st.mu.Lock()
	st.fail = errors.New("disk")
	st.mu.Unlock()
	req := gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1}
	// commitTermsLocked's store error: the IM layer's Failure.
	if _, err := gc.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, req); spec.StatusOf(err) != im.StatusFailure {
		t.Fatalf("a failing store: status %v (%v), want %v", spec.StatusOf(err), err, im.StatusFailure)
	}
	if !readBool(t, gc, gencommdef.AttrTcAcknowledgementsRequired) {
		t.Fatal("TCAcknowledgementsRequired = false after the failed commit, want true")
	}
	if got := readUint16(t, gc, gencommdef.AttrTcAcceptedVersion); got != 0 {
		t.Fatalf("TCAcceptedVersion = %d after the failed commit, want 0", got)
	}
	if v := gc.MatterDataVersion(); v != v0 {
		t.Fatalf("DataVersion %d -> %d with nothing changed", v0, v)
	}
	st.mu.Lock()
	st.fail = nil
	st.mu.Unlock()
	if _, err := gc.MatterInvoke(caseCtx(), gencommdef.CmdSetTcAcknowledgements, req); err != nil {
		t.Fatalf("retry: %v", err)
	}
	st.mu.Lock()
	stored := st.kv[store.SettingTermsAcceptance]
	st.mu.Unlock()
	if stored != "1:1" {
		t.Fatalf("the retry stored %q, want \"1:1\"", stored)
	}
}
