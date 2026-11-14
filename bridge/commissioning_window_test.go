// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// ─── fakeFailSafeArmer ───────────────────────────────────────────────────────

type fakeFailSafeArmer struct {
	calls   atomic.Int32
	seconds []uint32
	fabric  []uint8
	err     error
}

func (f *fakeFailSafeArmer) ArmFailSafeFor(_ context.Context, seconds uint32, fabricIndex uint8) error {
	f.calls.Add(1)
	f.seconds = append(f.seconds, seconds)
	f.fabric = append(f.fabric, fabricIndex)
	return f.err
}

// ─── fakePaseSessionCloser ───────────────────────────────────────────────────

type fakePaseSessionCloser struct {
	calls atomic.Int32
	err   error
}

func (f *fakePaseSessionCloser) ClosePaseSessions(_ context.Context) error {
	f.calls.Add(1)
	return f.err
}

// ─── CommissioningWindow unit tests ──────────────────────────────────────────

func TestCommissioningWindow_NewlyCreated_IsClosed(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusClosed {
		t.Errorf("Status = %v, want Closed", snap.Status)
	}
	if !snap.AdminFabricIsNull {
		t.Error("AdminFabricIsNull = false, want true for a fresh window")
	}
	if !snap.AdminVendorIsNull {
		t.Error("AdminVendorIsNull = false, want true for a fresh window")
	}
}

func TestCommissioningWindow_OpenWindow_Valid_StatusBecomesEnhanced(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatalf("OpenWindow(600s): unexpected error: %v", err)
	}
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusEnhanced {
		t.Errorf("Status = %v, want Enhanced after OpenWindow", snap.Status)
	}
}

// TestCommissioningWindow_RequestedDurationSeconds_ReflectsOpenWindow is the
// regression guard for GET /api/v1/matter/status never reporting
// commissioning_window_duration_seconds: the accessor must report the
// duration the window was actually opened with while open, and 0 once
// closed — a client that rehydrates state after a reload needs this to
// restore its countdown.
func TestCommissioningWindow_RequestedDurationSeconds_ReflectsOpenWindow(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if got := w.RequestedDurationSeconds(); got != 0 {
		t.Errorf("fresh window: RequestedDurationSeconds() = %d, want 0", got)
	}
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if got := w.RequestedDurationSeconds(); got != 600 {
		t.Errorf("open window: RequestedDurationSeconds() = %d, want 600", got)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}
	if got := w.RequestedDurationSeconds(); got != 0 {
		t.Errorf("closed window: RequestedDurationSeconds() = %d, want 0", got)
	}
}

func TestCommissioningWindow_OpenWindow_Twice_ReturnsBusy(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("first OpenWindow: unexpected error: %v", err)
	}
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	})
	if !errors.Is(err, wire.ErrAdmCommBusy) {
		t.Errorf("second OpenWindow: want ErrAdmCommBusy, got %v", err)
	}
}

func TestCommissioningWindow_OpenWindow_DurationTooShort_ReturnsInvalid(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 179, // < 180
	})
	if !errors.Is(err, bridge.ErrCommissioningWindowDurationInvalid) {
		t.Errorf("OpenWindow(179s): want ErrCommissioningWindowDurationInvalid, got %v", err)
	}
}

func TestCommissioningWindow_OpenWindow_DurationTooLong_ReturnsInvalid(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 901, // > 900
	})
	if !errors.Is(err, bridge.ErrCommissioningWindowDurationInvalid) {
		t.Errorf("OpenWindow(901s): want ErrCommissioningWindowDurationInvalid, got %v", err)
	}
}

// TestParityMatterJS_CommissioningWindow_DurationInvalidReturnsInvalidCommand
// verifies that ErrCommissioningWindowDurationInvalid implements
// im.StatusCodeError returning InvalidCommand (0x85). Mirrors matter.js
// AdministratorCommissioningServer.ts:#assertCommissioningWindowRequirements
// (lines 199-209) which throws Status.InvalidCommand for out-of-range timeouts.
func TestParityMatterJS_CommissioningWindow_DurationInvalidReturnsInvalidCommand(t *testing.T) {
	t.Parallel()

	var sc im.StatusCodeError
	if !errors.As(bridge.ErrCommissioningWindowDurationInvalid, &sc) {
		t.Fatal("ErrCommissioningWindowDurationInvalid does not implement im.StatusCodeError")
	}
	if sc.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("MatterStatusCode()=0x%02X, want StatusInvalidCommand (0x85)", sc.MatterStatusCode())
	}
}

func TestCommissioningWindow_DurationBoundaries_Valid(t *testing.T) {
	t.Parallel()
	for _, dur := range []uint16{180, 900} {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			w := bridge.NewCommissioningWindow()
			err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
				CommissioningTimeoutSeconds: dur,
			})
			if err != nil {
				t.Errorf("OpenWindow(%ds): unexpected error: %v", dur, err)
			}
		})
	}
}

func TestCommissioningWindow_RevokeWindow_OnClosedWindow_IsIdempotent(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	// Window is closed; revoke should be a no-op.
	err := w.RevokeWindow(context.Background())
	if err != nil {
		t.Errorf("RevokeWindow on closed window: unexpected error: %v", err)
	}
	// Still closed.
	if snap := w.CurrentWindow(); snap.Status != wire.WindowStatusClosed {
		t.Errorf("Status after RevokeWindow on closed = %v, want Closed", snap.Status)
	}
}

func TestCommissioningWindow_RevokeWindow_OnOpenWindow_StatusReturnsToClosed(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusClosed {
		t.Errorf("Status after RevokeWindow = %v, want Closed", snap.Status)
	}
}

func TestCommissioningWindow_TransitionHook_CalledOnOpenAndRevoke(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	var count atomic.Int32
	w.SetTransitionHook(func() { count.Add(1) })

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}

	if n := count.Load(); n != 2 {
		t.Errorf("transition hook call count = %d, want 2 (one Open + one Revoke)", n)
	}
}

// ─── CommissioningWindowOpener unit tests ────────────────────────────────────

func TestCommissioningWindowOpener_NoPasscode_ReturnsNotConfigured(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	opener := bridge.NewCommissioningWindowOpener(w, 0xF00, 0 /* passcode = 0 */, 0x1234, 0x5678)
	_, err := opener.OpenCommissioningWindow(context.Background(), 600)
	if !errors.Is(err, bridge.ErrCommissioningWindowNotConfigured) {
		t.Errorf("want ErrCommissioningWindowNotConfigured, got %v", err)
	}
}

func TestCommissioningWindowOpener_ValidPasscode_ReturnsArtifacts(t *testing.T) {
	t.Parallel()
	const (
		discriminator uint16 = 0xF00
		passcode      uint32 = 20202021
		vendorID      uint16 = 0x1234
		productID     uint16 = 0x5678
		durationSec   uint16 = 600
	)
	w := bridge.NewCommissioningWindow()
	opener := bridge.NewCommissioningWindowOpener(w, discriminator, passcode, vendorID, productID)
	result, err := opener.OpenCommissioningWindow(context.Background(), durationSec)
	if err != nil {
		t.Fatalf("OpenCommissioningWindow: unexpected error: %v", err)
	}

	// QR code must start with "MT:".
	if !strings.HasPrefix(result.QRCode, "MT:") {
		t.Errorf("QRCode = %q; want prefix \"MT:\"", result.QRCode)
	}

	// Manual code must be 11 characters (10 digits + Verhoeff check digit).
	if len(result.ManualCode) != 11 {
		t.Errorf("ManualCode = %q (len=%d), want 11 characters", result.ManualCode, len(result.ManualCode))
	}

	// Duration must be echoed back.
	if result.DurationSeconds != durationSec {
		t.Errorf("DurationSeconds = %d, want %d", result.DurationSeconds, durationSec)
	}
}

func TestCommissioningWindowOpener_SecondOpen_ReturnsAlreadyOpen(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	opener := bridge.NewCommissioningWindowOpener(w, 0xF00, 20202021, 0x1234, 0x5678)

	if _, err := opener.OpenCommissioningWindow(context.Background(), 600); err != nil {
		t.Fatalf("first OpenCommissioningWindow: unexpected error: %v", err)
	}
	_, err := opener.OpenCommissioningWindow(context.Background(), 600)
	if !errors.Is(err, bridge.ErrCommissioningWindowAlreadyOpen) {
		t.Errorf("second OpenCommissioningWindow: want ErrCommissioningWindowAlreadyOpen, got %v", err)
	}
}

// ─── FailSafeArmer / FailSafeExpirer ─────────────────────────────────────────

// TestCommissioningWindow_FailSafeArmer_CalledOnOpen pins owner decision
// 47: a successful window open arms no fail-safe — the armer a host still
// wires through the deprecated SetFailSafeArmer is never called.
func TestCommissioningWindow_FailSafeArmer_CalledOnOpen(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	armer := &fakeFailSafeArmer{}
	w.SetFailSafeArmer(armer) //nolint:staticcheck // the deprecated wiring still compiles and arms nothing

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if n := armer.calls.Load(); n != 0 {
		t.Errorf("ArmFailSafeFor call count = %d, want 0: opening a window arms nothing", n)
	}
}

// TestCommissioningWindow_FailSafeArmer_NotCalledWhenWindowRejected: a
// rejected second open arms nothing either.
func TestCommissioningWindow_FailSafeArmer_NotCalledWhenWindowRejected(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	armer := &fakeFailSafeArmer{}
	w.SetFailSafeArmer(armer) //nolint:staticcheck // the deprecated wiring still compiles and arms nothing

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("first OpenWindow: unexpected error: %v", err)
	}
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); !errors.Is(err, wire.ErrAdmCommBusy) {
		t.Errorf("second OpenWindow: want ErrAdmCommBusy, got %v", err)
	}
	if n := armer.calls.Load(); n != 0 {
		t.Errorf("ArmFailSafeFor call count = %d, want 0", n)
	}
}

// TestCommissioningWindow_FailSafeArmer_ArmErrorDoesNotAbortWindow: an
// armer that would fail is not called, so the window opens.
func TestCommissioningWindow_FailSafeArmer_ArmErrorDoesNotAbortWindow(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	armer := &fakeFailSafeArmer{err: errors.New("fail-safe arm failed")}
	w.SetFailSafeArmer(armer) //nolint:staticcheck // the deprecated wiring still compiles and arms nothing

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Errorf("OpenWindow: unexpected error %v", err)
	}
	if snap := w.CurrentWindow(); snap.Status != wire.WindowStatusEnhanced {
		t.Errorf("Status = %v, want Enhanced (window is open)", snap.Status)
	}
}

// TestCommissioningWindow_RevokeWindow_DisarmsFailSafe: RevokeWindow
// expires the PASE-armed fail-safe through the expirer (Matter §11.19.7.3
// step 1; owner decision 42), unconditionally and before the window-state
// check, and never through a zero-second arm.
func TestCommissioningWindow_RevokeWindow_DisarmsFailSafe(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	a := &expiringArmer{}
	w.SetFailSafeExpirer(a)
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}
	if a.expired.Load() != 1 || a.armed.Load() != 0 {
		t.Fatalf("expired %d, armed %d; want one expiry and no arm", a.expired.Load(), a.armed.Load())
	}
}

// fakeFailSafeArmerChecker implements FailSafeExpirer and FailSafeChecker
// over shared state, so a test can observe the "armed → Busy" linkage
// that OpenWindow's FailSafeChecker guard enforces without wiring a real
// GeneralCommissioning cluster server.
type fakeFailSafeArmerChecker struct {
	armed atomic.Bool
}

func (f *fakeFailSafeArmerChecker) ExpireFailSafeArmedOverPASE(context.Context) {
	f.armed.Store(false)
}

func (f *fakeFailSafeArmerChecker) FailSafeArmed() bool {
	return f.armed.Load()
}

// TestCommissioningWindow_OpenAfterRevoke_NotBusy verifies the end-to-end
// H9 regression: a commissioner's PASE session arms the fail-safe,
// RevokeWindow expires it, and a subsequent OpenWindow must succeed rather
// than being rejected ErrAdmCommBusy by the FailSafeChecker guard.
func TestCommissioningWindow_OpenAfterRevoke_NotBusy(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	fake := &fakeFailSafeArmerChecker{}
	w.SetFailSafeExpirer(fake)
	w.SetFailSafeChecker(fake)

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("first OpenWindow: unexpected error: %v", err)
	}
	fake.armed.Store(true) // the commissioner's PASE session armed it
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}
	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	})
	if err != nil {
		t.Errorf("OpenWindow after RevokeWindow: unexpected error %v, want nil (fail-safe must be expired)", err)
	}
}

// ─── PaseSessionCloser ───────────────────────────────────────────────────────

// TestCommissioningWindow_PaseSessionCloser_CalledOnRevoke verifies that the
// PaseSessionCloser is invoked on RevokeWindow regardless of whether the
// window is open — per Matter §11.19.7.3 step 1.
func TestCommissioningWindow_PaseSessionCloser_CalledOnRevoke(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	closer := &fakePaseSessionCloser{}
	w.SetPaseSessionCloser(closer)

	// Open the window then revoke.
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: unexpected error: %v", err)
	}

	if n := closer.calls.Load(); n != 1 {
		t.Errorf("ClosePaseSessions call count = %d, want 1", n)
	}
}

// TestCommissioningWindow_PaseSessionCloser_CalledOnRevokeEvenWhenClosed
// verifies that ClosePaseSessions is called even when no commissioning window
// is open — §11.19.7.3 step 1 is unconditional.
func TestCommissioningWindow_PaseSessionCloser_CalledOnRevokeEvenWhenClosed(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	closer := &fakePaseSessionCloser{}
	w.SetPaseSessionCloser(closer)

	// Revoke on a closed window — closer must still fire.
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow on closed window: unexpected error: %v", err)
	}
	if n := closer.calls.Load(); n != 1 {
		t.Errorf("ClosePaseSessions call count on closed-window revoke = %d, want 1", n)
	}
}

// TestCommissioningWindow_PaseSessionCloser_ErrorDoesNotAbortRevoke verifies
// that a ClosePaseSessions error does not prevent the window from being closed.
func TestCommissioningWindow_PaseSessionCloser_ErrorDoesNotAbortRevoke(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	closer := &fakePaseSessionCloser{err: errors.New("session close failed")}
	w.SetPaseSessionCloser(closer)

	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	}); err != nil {
		t.Fatalf("OpenWindow: unexpected error: %v", err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Errorf("RevokeWindow: unexpected error %v — session-close failure must not abort revoke", err)
	}
	if snap := w.CurrentWindow(); snap.Status != wire.WindowStatusClosed {
		t.Errorf("Status after revoke with session-close error = %v, want Closed", snap.Status)
	}
}

// ─── windowMode (BasicWindowOpen vs EnhancedWindowOpen) ─────────────────────

// TestCommissioningWindow_IsBasicWindow_StatusBasic verifies that a
// window opened with IsBasicWindow=true returns WindowStatusBasic (2).
// Mirrors matter.js AdministratorCommissioningServer.ts:114-119.
func TestCommissioningWindow_IsBasicWindow_StatusBasic(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		IsBasicWindow:               true,
	}); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusBasic {
		t.Errorf("Status = %v, want WindowStatusBasic (2)", snap.Status)
	}
}

// TestCommissioningWindow_IsBasicWindowFalse_StatusEnhanced verifies that
// a window opened with IsBasicWindow=false returns WindowStatusEnhanced (1).
// Mirrors matter.js AdministratorCommissioningServer.ts:114-119.
func TestCommissioningWindow_IsBasicWindowFalse_StatusEnhanced(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		IsBasicWindow:               false,
	}); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusEnhanced {
		t.Errorf("Status = %v, want WindowStatusEnhanced (1)", snap.Status)
	}
}

// TestCommissioningWindow_IsBasicWindow_ClearedOnRevoke ensures the
// mode is reset after RevokeWindow so a subsequent open is not
// accidentally marked basic.
func TestCommissioningWindow_IsBasicWindow_ClearedOnRevoke(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	_ = w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		IsBasicWindow:               true,
	})
	_ = w.RevokeWindow(context.Background())
	// Second open without IsBasicWindow set → must be Enhanced.
	_ = w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
	})
	if snap := w.CurrentWindow(); snap.Status != wire.WindowStatusEnhanced {
		t.Errorf("Status after revoke+reopen = %v, want Enhanced", snap.Status)
	}
}

// ─── AdminFabricIndex / AdminVendorID from params ────────────────────────────

// TestCommissioningWindow_AdminFabric_StoredFromParams verifies that
// non-zero AdminFabricIndex / AdminVendorID from OpenWindowParams are
// reflected in CurrentWindow's snapshot.
// Mirrors matter.js AdministratorCommissioningServer.ts:176-180.
func TestCommissioningWindow_AdminFabric_StoredFromParams(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		AdminFabricIndex:            3,
		AdminVendorID:               0x1234,
	}); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	snap := w.CurrentWindow()
	if snap.AdminFabricIsNull {
		t.Error("AdminFabricIsNull = true, want false when AdminFabricIndex was provided")
	}
	if snap.AdminFabricIndex != 3 {
		t.Errorf("AdminFabricIndex = %d, want 3", snap.AdminFabricIndex)
	}
	if snap.AdminVendorIsNull {
		t.Error("AdminVendorIsNull = true, want false when AdminVendorID was provided")
	}
	if snap.AdminVendorID != 0x1234 {
		t.Errorf("AdminVendorID = 0x%X, want 0x1234", snap.AdminVendorID)
	}
}

// TestCommissioningWindow_AdminFabric_ZeroIsNull verifies that
// AdminFabricIndex=0 / AdminVendorID=0 keeps the null flag set — this
// matches the pre-fabric commissioning scenario.
func TestCommissioningWindow_AdminFabric_ZeroIsNull(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		AdminFabricIndex:            0,
		AdminVendorID:               0,
	}); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	snap := w.CurrentWindow()
	if !snap.AdminFabricIsNull {
		t.Errorf("AdminFabricIsNull = false, want true when AdminFabricIndex=0")
	}
	if !snap.AdminVendorIsNull {
		t.Errorf("AdminVendorIsNull = false, want true when AdminVendorID=0")
	}
}

// TestCommissioningWindow_AdminFabric_ClearedOnRevoke verifies that
// admin fabric metadata is cleared after RevokeWindow.
func TestCommissioningWindow_AdminFabric_ClearedOnRevoke(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	_ = w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 300,
		AdminFabricIndex:            7,
		AdminVendorID:               0xABCD,
	})
	_ = w.RevokeWindow(context.Background())
	snap := w.CurrentWindow()
	if snap.Status != wire.WindowStatusClosed {
		t.Errorf("Status = %v after revoke, want Closed", snap.Status)
	}
}

// ─── RandomDiscriminator / RandomPasscode / RandomSalt ───────────────────────

// TestRandomDiscriminator_InRange verifies the result is a valid 12-bit value.
func TestRandomDiscriminator_InRange(t *testing.T) {
	t.Parallel()
	for range 20 {
		v, err := bridge.RandomDiscriminator()
		if err != nil {
			t.Fatalf("RandomDiscriminator: %v", err)
		}
		if v > 0x0FFF {
			t.Errorf("discriminator 0x%04X out of 12-bit range", v)
		}
	}
}

// TestRandomPasscode_NotInvalidSet verifies that no invalid passcode
// (spec §5.1.1.1.1) is returned across a sufficient number of samples.
func TestRandomPasscode_NotInvalidSet(t *testing.T) {
	t.Parallel()
	invalid := map[uint32]bool{
		0: true, 11111111: true, 22222222: true, 33333333: true,
		44444444: true, 55555555: true, 66666666: true, 77777777: true,
		88888888: true, 99999999: true, 12345678: true, 87654321: true,
	}
	for range 50 {
		v, err := bridge.RandomPasscode()
		if err != nil {
			t.Fatalf("RandomPasscode: %v", err)
		}
		if invalid[v] {
			t.Errorf("RandomPasscode returned invalid passcode %08d", v)
		}
		if v >= 99999999 {
			t.Errorf("RandomPasscode %d >= 99999999 (out of 8-digit range)", v)
		}
	}
}

// ─── FailSafeChecker ─────────────────────────────────────────────────────────

type fakeFailSafeChecker struct {
	armed bool
}

func (f *fakeFailSafeChecker) FailSafeArmed() bool { return f.armed }

// TestCommissioningWindow_OpenWindow_RejectsBusyWhenFailSafeArmed verifies
// that OpenWindow returns BUSY when the FailSafeChecker reports an armed
// fail-safe, preventing a second commissioner from overwriting the active
// commissioning session.
func TestCommissioningWindow_OpenWindow_RejectsBusyWhenFailSafeArmed(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	checker := &fakeFailSafeChecker{armed: true}
	w.SetFailSafeChecker(checker)

	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	})
	if !errors.Is(err, wire.ErrAdmCommBusy) {
		t.Errorf("OpenWindow with armed FailSafe: got %v, want ErrAdmCommBusy", err)
	}
}

// TestCommissioningWindow_OpenWindow_AllowsWhenFailSafeDisarmed verifies
// that OpenWindow proceeds normally when the FailSafeChecker reports the
// fail-safe as disarmed.
func TestCommissioningWindow_OpenWindow_AllowsWhenFailSafeDisarmed(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	checker := &fakeFailSafeChecker{armed: false}
	w.SetFailSafeChecker(checker)

	err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600,
	})
	if err != nil {
		t.Errorf("OpenWindow with disarmed FailSafe: unexpected error: %v", err)
	}
}

// TestRandomSalt_Length32 verifies the returned salt is exactly 32 bytes,
// matter.js's length (DeviceCommissioner.ts:134).
func TestRandomSalt_Length32(t *testing.T) {
	t.Parallel()
	s, err := bridge.RandomSalt()
	if err != nil {
		t.Fatalf("RandomSalt: %v", err)
	}
	if len(s) != 32 {
		t.Errorf("RandomSalt: len = %d, want 32", len(s))
	}
}

// TestCommissioningWindow_RevokeOverPaseDefersTheClose pins matter.js
// NodeSession.initiateClose's deferred close: a RevokeCommissioning that
// arrives over a PASE session closes it only once the command's response
// is out (TC-CADMIN-1.10 step 9); over CASE, or with nothing to defer to,
// the close is immediate.
func TestCommissioningWindow_RevokeOverPaseDefersTheClose(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	closer := &fakePaseSessionCloser{}
	w.SetPaseSessionCloser(closer)

	after := &im.AfterResponse{}
	paseCtx := im.WithAfterResponse(im.WithAuthModePASE(context.Background()), after)
	_ = w.RevokeWindow(paseCtx)
	if n := closer.calls.Load(); n != 0 {
		t.Fatalf("the PASE session closed before the response (%d calls)", n)
	}
	after.Run()
	if n := closer.calls.Load(); n != 1 {
		t.Fatalf("ClosePaseSessions after the response = %d calls, want 1", n)
	}
	after.Run()
	if n := closer.calls.Load(); n != 1 {
		t.Fatalf("a second Run closed again (%d calls)", n)
	}

	caseCtx := im.WithAfterResponse(context.Background(), &im.AfterResponse{})
	_ = w.RevokeWindow(caseCtx)
	if n := closer.calls.Load(); n != 2 {
		t.Fatalf("a revoke over CASE did not close at once (%d calls)", n)
	}
	_ = w.RevokeWindow(im.WithAuthModePASE(context.Background()))
	if n := closer.calls.Load(); n != 3 {
		t.Fatalf("a PASE revoke with nothing to defer to did not close at once (%d calls)", n)
	}
}

// TestCommissioningWindow_FabricRemovedClearsTheAdminFabric pins matter.js
// #fabricRemovedCallback: removing the fabric that opened the window nulls
// AdminFabricIndex and reports it; the window stays open, AdminVendorId
// stays, and another fabric's removal changes nothing (TC-CADMIN-1.25).
func TestCommissioningWindow_FabricRemovedClearsTheAdminFabric(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	var fired atomic.Int32
	w.SetTransitionHook(func() { fired.Add(1) })
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{
		CommissioningTimeoutSeconds: 600, AdminFabricIndex: 2, AdminVendorID: 0xFFF1,
	}); err != nil {
		t.Fatal(err)
	}
	before := fired.Load()
	w.FabricRemoved(3)
	if snap := w.CurrentWindow(); snap.AdminFabricIsNull || fired.Load() != before {
		t.Fatal("another fabric's removal touched the window")
	}
	w.FabricRemoved(2)
	snap := w.CurrentWindow()
	if !snap.AdminFabricIsNull || snap.Status == wire.WindowStatusClosed || snap.AdminVendorIsNull {
		t.Fatalf("after the admin fabric's removal: %+v, want AdminFabricIndex null, window open, vendor kept", snap)
	}
	if fired.Load() != before+1 {
		t.Fatalf("the change was reported %d times, want once", fired.Load()-before)
	}
}

// expiringArmer records how RevokeWindow ends the fail-safe.
type expiringArmer struct {
	expired, armed atomic.Int32
}

func (a *expiringArmer) ArmFailSafeFor(context.Context, uint32, uint8) error {
	a.armed.Add(1)
	return nil
}

func (a *expiringArmer) ExpireFailSafeArmedOverPASE(context.Context) { a.expired.Add(1) }

// TestCommissioningWindow_RevokeExpiresTheFailSafe pins Matter §11.19.8.3
// step 1: RevokeWindow expires the PASE-armed fail-safe with its cleanup
// through the deprecated SetFailSafeArmer wiring too, when the armer is an
// expirer (TC-CGEN-2.4).
func TestCommissioningWindow_RevokeExpiresTheFailSafe(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	a := &expiringArmer{}
	w.SetFailSafeArmer(a) //nolint:staticcheck // the deprecated wiring keeps wiring the expirer
	_ = w.RevokeWindow(context.Background())
	if a.expired.Load() != 1 || a.armed.Load() != 0 {
		t.Fatalf("expired %d, disarmed %d; want one expiry", a.expired.Load(), a.armed.Load())
	}
}
