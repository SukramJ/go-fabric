// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/clockvar"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// FailSafeChecker is the optional pre-open guard CommissioningWindow
// consults before opening a new commissioning window. Matter §11.19.7.1
// requires that OpenCommissioningWindow returns BUSY when a FailSafe is
// already armed by a different commissioner. The bridge wires this to
// GeneralCommissioning so OpenWindow can reject immediately rather than
// overwriting an active commissioning session.
//
// Returning true from FailSafeArmed causes OpenWindow to return
// [wire.ErrAdmCommBusy]. Implementations must be goroutine-safe.
type FailSafeChecker interface {
	FailSafeArmed() bool
}

// FailSafeArmer was the hook CommissioningWindow armed the fail-safe
// through when a window opened. A window arms no fail-safe any more (owner
// decision 47; matter.js AdministratorCommissioningServer.ts
// openCommissioningWindow starts the window only, at db021da0) — the
// commissioner's ArmFailSafe or the 60 s arm of its PASE session does — so
// the window never calls ArmFailSafeFor; [CommissioningWindow.SetFailSafeArmer]
// keeps wiring an armer that is a [FailSafeExpirer] as one.
//
// Deprecated: nothing in the module arms a fail-safe through it. There is
// no replacement: the fail-safe comes with the commissioner's ArmFailSafe
// (or the arm of its PASE session); what a window still needs of
// GeneralCommissioning is a [FailSafeExpirer]. Removable in v0.23.0
// (review II, owner decision 47).
type FailSafeArmer interface {
	ArmFailSafeFor(ctx context.Context, seconds uint32, fabricIndex uint8) error
}

// FailSafeExpirer is what RevokeCommissioning expires the fail-safe
// through (Matter §11.19.8.3 step 1): the fail-safe armed over the PASE
// session, with the rollback its timeout runs; one an administrator armed
// over CASE stays (owner decision 42). core.GeneralCommissioning
// implements it. Implementations must be goroutine-safe.
type FailSafeExpirer interface {
	ExpireFailSafeArmedOverPASE(ctx context.Context)
}

// PaseVerifierInstaller is the optional daemon-side hook CommissioningWindow
// calls when an Enhanced Commissioning Window is opened with a
// commissioner-supplied PAKE passcode verifier (Matter §11.19 / §3.10.5).
// It installs a PASE acceptor built from that verifier for the window
// lifetime — so the bridge accepts PASE against the passcode the
// commissioner chose, not the bridge's configured passcode — and returns a
// restore closure that re-installs the configured acceptor when the window
// closes. Mirrors matter.js AdministratorCommissioningServer
// openCommissioningWindow → PaseServer.fromVerificationValue.
//
// When no installer is wired (Basic Commissioning Window, or no verifier
// supplied), OpenWindow leaves the configured acceptor in place.
type PaseVerifierInstaller interface {
	// InstallVerifier installs a window-lifetime PASE acceptor from the
	// 97-byte Matter PAKE passcode verifier (w0 || L) plus the PBKDF
	// iterations + salt the commissioner supplied. Returns a restore
	// closure (re-installs the prior acceptor) or an error if the verifier
	// is malformed.
	InstallVerifier(verifier []byte, iterations uint32, salt []byte) (restore func(), err error)
}

// PaseSessionCloser is the optional host-side hook CommissioningWindow
// calls at the start of RevokeWindow. Matter §11.19.7.3 step 1 mandates
// closing any open PASE session regardless of window state. A window
// attached to a bridge ([Bridge.AttachCommissioningWindow]) closes the
// PASE sessions of the bridge's session registry by itself; the hook is
// for a window used without one, or for sessions the host keeps
// elsewhere, and runs before the bridge's own close.
//
// Mirrors matter.js
// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:137-141
// at db021da0 (revokeCommissioning: `SessionManager.getPaseSession()` →
// `paseSession.initiateClose()`).
type PaseSessionCloser interface {
	ClosePaseSessions(ctx context.Context) error
}

// CommissioningWindow tracks an open enhanced-commissioning window.
// The bridge owns at most one live window at a time per ADR 0012's
// single-PASE-acceptor design — concurrent windows would require
// multiple verifiers, which is a separate v1.2 extension.
//
// Window state transitions:
//
//	closed → OpenWindow → enhanced
//	enhanced → RevokeWindow → closed (early)
//	enhanced → closeTimer fires → closed (expired)
//
// Each transition fires `onTransition` (for the WS event publisher
// + the AdministratorCommissioning cluster's WindowStatus attribute)
// and, when set, the `restore` closure the
// [CommissioningWindowOpener.EphemeralProvider] returned at open —
// guaranteeing the bridge's long-lived PASE acceptor is reinstated
// regardless of whether the window closed early or by timeout.
type CommissioningWindow struct {
	// clock times the window (see [CommissioningWindow.SetClock]).
	clock clockvar.Var

	// mu guards the window's state: every field below it. The transition
	// hooks and the restore closure are read under it and run after it is
	// released.
	mu sync.RWMutex
	// blocked, while non-nil, refuses every window ([SetBlocked]).
	blocked        error
	open           bool
	openedAt       time.Time
	timeout        time.Duration
	expiresAt      time.Time
	discriminator  uint16
	durationSec    uint16
	adminFabric    uint8
	adminFabricSet bool
	adminVendor    uint16
	adminVendorSet bool
	// own marks a window the node opened itself ([CommissioningWindow.OpenOwnWindow])
	// rather than an administrator. It is open (IsOpen) but reads as
	// WindowNotOpen on the AdministratorCommissioning cluster, an
	// administrator's window replaces it instead of answering Busy, and
	// RevokeCommissioning closes it. Mirrors matter.js
	// DeviceCommissioner.ts CommissioningWindow.byAdministrator == false.
	own bool

	// fabricCounter, when set, gives OpenOwnWindow its default timeout
	// (see [CommissioningWindow.SetFabricCounter]).
	fabricCounter func(context.Context) (int, error)

	// isBasicWindow records whether the window was opened via
	// OpenBasicCommissioningWindow (BC feature). When false, the
	// status reported by CurrentWindow is EnhancedWindowOpen (1);
	// when true, it is BasicWindowOpen (2).
	isBasicWindow bool

	// hasVerifier records whether the current window was opened with a
	// commissioner-supplied PAKE verifier (the Matter cluster Enhanced
	// Commissioning Window path). It distinguishes a true ECW — which the
	// daemon advertises over mDNS with CM=2 — from the REST/operator open
	// (also Enhanced status, but its own passcode, advertised CM=1 by the
	// window opener). See [CommissioningWindow.HasSuppliedVerifier].
	hasVerifier bool

	// jointFabric records that the window is a Joint Fabric
	// administrator's joint commissioning window
	// ([wire.OpenWindowParams].JointFabric), advertised CM=3.
	jointFabric bool

	// closeTimer fires when the window expires so the bridge auto-
	// reverts to "no PASE acceptor". Cancelled when RevokeWindow
	// closes the window early.
	closeTimer clock.Timer
	// stopped: the bridge stopped ([CommissioningWindow.stopTimer]); no
	// window opens and no timer is armed from then on (review II,
	// R2-P2-stop-02).
	stopped bool

	// restore, when non-nil, is invoked on every window-close
	// transition (early revoke or timer expiry) so the ephemeral
	// PASE adapter the opener installed for the window is replaced
	// by the bridge's long-lived configured acceptor. Nil when the
	// window opened in legacy mode (configured passcode reused —
	// no swap happened).
	restore func()

	// onTransition gets called every time the window state changes
	// — typically wired to the AdministratorCommissioning cluster's
	// SubscriptionEventReporter so subscribers see WindowStatus flip.
	onTransition func()
	// onStateChanged is the bridge's own transition hook: it reports the
	// AdministratorCommissioning attributes to subscribers
	// (Bridge.AttachCommissioningWindow). Called with the caller's
	// hook, under the same conditions, without the lock.
	onStateChanged func()

	// failSafeChecker, when non-nil, is consulted at the start of every
	// OpenWindow call. A currently-armed FailSafe window (set by a prior
	// commissioner) causes OpenWindow to return BUSY rather than
	// overwriting the active commissioning session. Nil in unit-test
	// setups that do not exercise the fail-safe guard.
	failSafeChecker FailSafeChecker

	// failSafeExpirer, when non-nil, expires the PASE-armed fail-safe on
	// RevokeWindow ([SetFailSafeExpirer]); nil in unit-test setups that do
	// not exercise the fail-safe path.
	failSafeExpirer FailSafeExpirer

	// paseSessionCloser, when non-nil, is called at the start of
	// RevokeWindow to evict any open PASE session per Matter §11.19.7.3.
	paseSessionCloser PaseSessionCloser
	// sessionOwner, when non-nil, closes the PASE sessions of the bridge
	// the window is attached to; RevokeWindow runs it after
	// paseSessionCloser. Set by [Bridge.AttachCommissioningWindow].
	sessionOwner func()

	// paseVerifierInstaller, when non-nil, installs a window-lifetime PASE
	// acceptor from a commissioner-supplied PAKE verifier on the Enhanced
	// Commissioning Window path (see [PaseVerifierInstaller]).
	paseVerifierInstaller PaseVerifierInstaller
}

// SetClock sets the clock the window's timeout runs on; nil is
// [clock.System]. [Bridge.AttachCommissioningWindow] sets the bridge's.
// A window already open keeps the timer it was opened with.
func (w *CommissioningWindow) SetClock(c clock.Clock) { w.clock.Store(c) }

// NewCommissioningWindow returns a fresh, closed window.
func NewCommissioningWindow() *CommissioningWindow {
	return &CommissioningWindow{}
}

// SetFailSafeChecker wires (or clears, when nil) the guard consulted at
// the start of every [OpenWindow] call. When the checker reports an armed
// FailSafe, OpenWindow returns [wire.ErrAdmCommBusy] rather than opening
// a second window that would overwrite the active commissioning session.
// Safe to call concurrently with [OpenWindow].
func (w *CommissioningWindow) SetFailSafeChecker(c FailSafeChecker) {
	w.mu.Lock()
	w.failSafeChecker = c
	w.mu.Unlock()
}

// SetFailSafeArmer wires a as the window's [FailSafeExpirer] when it is
// one (core.GeneralCommissioning is), and clears it otherwise; the window
// arms no fail-safe (owner decision 47). [CommissioningWindow.SetFailSafeExpirer]
// is the direct wiring.
//
// Deprecated: the window arms no fail-safe, so there is nothing to wire an
// armer for; use [CommissioningWindow.SetFailSafeExpirer] for the expirer
// it wires. Removable in v0.23.0 (review II, owner decision 47).
func (w *CommissioningWindow) SetFailSafeArmer(a FailSafeArmer) {
	e, _ := a.(FailSafeExpirer)
	w.SetFailSafeExpirer(e)
}

// SetFailSafeExpirer wires (or clears, when nil) what RevokeWindow
// expires the PASE-armed fail-safe through. Safe to call concurrently
// with [OpenWindow].
func (w *CommissioningWindow) SetFailSafeExpirer(e FailSafeExpirer) {
	w.mu.Lock()
	w.failSafeExpirer = e
	w.mu.Unlock()
}

// SetPaseSessionCloser wires (or clears, when nil) the hook called at
// the start of [RevokeWindow] to evict open PASE sessions per Matter
// §11.19.7.3.
// Safe to call concurrently with [RevokeWindow].
func (w *CommissioningWindow) SetPaseSessionCloser(c PaseSessionCloser) {
	w.mu.Lock()
	w.paseSessionCloser = c
	w.mu.Unlock()
}

// setSessionOwner wires (or clears, when nil) the bridge's own close of
// its PASE sessions; see the sessionOwner field.
func (w *CommissioningWindow) setSessionOwner(closePASE func()) {
	w.mu.Lock()
	w.sessionOwner = closePASE
	w.mu.Unlock()
}

// SetPaseVerifierInstaller wires (or clears, when nil) the hook that
// installs a window-lifetime PASE acceptor from a commissioner-supplied
// PAKE verifier on the Enhanced Commissioning Window path (see
// [PaseVerifierInstaller]). Safe to call concurrently with [OpenWindow].
func (w *CommissioningWindow) SetPaseVerifierInstaller(i PaseVerifierInstaller) {
	w.mu.Lock()
	w.paseVerifierInstaller = i
	w.mu.Unlock()
}

// Discriminator returns the 12-bit discriminator of the currently-open
// window (0 when closed). Used by the daemon's transition hook to build
// the CM=2 commissionable advertisement for an Enhanced Commissioning
// Window.
func (w *CommissioningWindow) Discriminator() uint16 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.discriminator
}

// RequestedDurationSeconds returns the duration the currently-open window
// was opened with (0 when closed). It is not a Matter wire attribute — the
// AdministratorCommissioning cluster's WindowStatus carries no duration —
// so it is a sibling accessor rather than part of [CommissioningWindow.CurrentWindow]'s
// snapshot. It exists so a client that rehydrates state after a page
// reload (GET /api/v1/matter/status's commissioning_window_duration_seconds)
// can restore its countdown instead of losing it whenever it re-syncs
// against an already-open window.
func (w *CommissioningWindow) RequestedDurationSeconds() uint16 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.open {
		return 0
	}
	return w.durationSec
}

// Timing returns when the open window was opened, when its timer closes it
// and the timeout it was opened with, all on the window's clock
// ([CommissioningWindow.SetClock]); zero values while no window is open. A
// window the node restarts (OpenOwnWindow on an open own window) reports
// the restart. Unlike [CommissioningWindow.RequestedDurationSeconds] the
// timeout is not clamped to 16 bits, so the node's own 48-hour window
// reads as it runs. matter.js keeps the same three in the window's timer
// (packages/protocol/src/protocol/DeviceCommissioner.ts:256-265 at
// 862ad75f, `Time.getTimer("Commissioning timeout", timeout ?? …)`).
func (w *CommissioningWindow) Timing() (openedAt, deadline time.Time, timeout time.Duration) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.open {
		return time.Time{}, time.Time{}, 0
	}
	return w.openedAt, w.expiresAt, w.timeout
}

// IsJointFabric reports whether the open window is a joint commissioning
// window (OpenJointCommissioningWindow, connectedhomeip
// CommissioningWindowManager.cpp:413-419 at 6170af84), which the node
// advertises CM=3 (:491).
func (w *CommissioningWindow) IsJointFabric() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.jointFabric
}

// HasSuppliedVerifier reports whether the currently-open window was opened
// with a commissioner-supplied PAKE verifier (a true Matter Enhanced
// Commissioning Window), as opposed to the REST/operator open (also
// Enhanced status but the bridge's own passcode). The daemon advertises
// CM=2 only for the former; the REST path advertises CM=1 via the opener.
func (w *CommissioningWindow) HasSuppliedVerifier() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.hasVerifier
}

// SetTransitionHook wires the optional callback fired on Open / Close.
// Pass nil to detach. Safe to call concurrently.
func (w *CommissioningWindow) SetTransitionHook(fn func()) {
	w.mu.Lock()
	w.onTransition = fn
	w.mu.Unlock()
}

// IsOpen reports whether a commissioning window is open, whoever opened it:
// an administrator, the host's opener or the node itself
// ([CommissioningWindow.OpenOwnWindow]). Mirrors matter.js
// DeviceCommissioner.windowStatus, which GeneralCommissioningServer's
// ArmFailSafe guard and AdministratorCommissioningServer.revokeCommissioning
// consult.
func (w *CommissioningWindow) IsOpen() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.open
}

// IsOwnWindow reports whether the open window is one the node opened itself
// ([CommissioningWindow.OpenOwnWindow]). A host's transition hook reads it to
// tell the node's own window — commissionable with the configured passcode,
// advertised CM=1 — from an administrator's.
func (w *CommissioningWindow) IsOwnWindow() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.open && w.own
}

// SetFabricCounter wires (or clears, when nil) the fabric count
// [CommissioningWindow.OpenOwnWindow] derives its default timeout from.
// Without it the node counts as uncommissioned.
func (w *CommissioningWindow) SetFabricCounter(fn func(context.Context) (int, error)) {
	w.mu.Lock()
	w.fabricCounter = fn
	w.mu.Unlock()
}

// Timeouts of a window the node opens itself, matter.js
// DeviceCommissioner.ts #ownWindowTimeout: MAXIMUM_COMMISSIONING_TIMEOUT
// (48 h, Extended Announcement) while the node has no fabric,
// STANDARD_COMMISSIONING_TIMEOUT (15 min) once it has one (Matter §5.4.2.3).
const (
	OwnWindowTimeoutUncommissioned = 48 * time.Hour
	OwnWindowTimeoutCommissioned   = 15 * time.Minute
)

// ErrAdministratorWindowOpen is returned by [CommissioningWindow.OpenOwnWindow]
// while an administrator's window is open: the node's own window never
// replaces one (matter.js DeviceCommissioner #open throws MatterFlowError).
var ErrAdministratorWindowOpen = errors.New("bridge: an administrator's commissioning window is open")

// ErrCommissioningBlocked is wrapped by every window open refused while
// the host has blocked commissioning ([CommissioningWindow.SetBlocked]).
// An AdministratorCommissioning command refused this way answers
// FAILURE: none of the cluster's own statuses means "refused by policy"
// (matter.js AdministratorCommissioningServer.ts: Busy only for a window
// already open, :196; PakeParameterError and InvalidCommand for the
// request, :74-80, :200-209).
var ErrCommissioningBlocked = errors.New("bridge: commissioning is blocked by the host")

// SetBlocked refuses every commissioning window while err is non-nil:
// [CommissioningWindow.OpenOwnWindow] and [CommissioningWindow.OpenWindow]
// return an error wrapping [ErrCommissioningBlocked] and err, so the
// node's own window, a basic window, the host's opener and both
// AdministratorCommissioning commands stay closed and no commissionable
// record is advertised; a window open when the block is set is revoked.
// nil lifts the block; nothing reopens by itself here (the node facade
// reopens an uncommissioned node's own window). Paired controllers are
// unaffected: the block concerns new commissioning only.
//
// matter.js has no counterpart; it is a host extension for a node that
// cannot serve the attestation it promised (issue #133,
// BD-Matter-CommissioningBlocked).
func (w *CommissioningWindow) SetBlocked(ctx context.Context, err error) {
	w.mu.Lock()
	w.blocked = err
	open := w.open
	w.mu.Unlock()
	if err != nil && open {
		_ = w.RevokeWindow(ctx)
	}
}

// blockedErr is the refusal while commissioning is blocked, nil otherwise.
func (w *CommissioningWindow) blockedErr() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.blocked == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrCommissioningBlocked, w.blocked)
}

// OpenOwnWindow opens the node's own commissioning window — the window an
// uncommissioned node opens at start, or one the application opens — for
// timeout, or, when timeout is 0, for 48 h while the node has no fabric and
// 15 min once it has one. Invoked again while the node's own window is open,
// it restarts it. It is closed by its timer, by CommissioningComplete
// ([CommissioningWindow.EndCommissioning]) and by RevokeCommissioning; an
// administrator's OpenCommissioningWindow replaces it instead of answering
// Busy. The AdministratorCommissioning attributes keep reading
// WindowNotOpen, which matter.js keeps for administrator windows only.
//
// The window owns no PASE acceptor and no advertisement: the transition hook
// ([CommissioningWindow.SetTransitionHook]) fires on open and close, and the
// host attaches its configured-passcode acceptor and announces CM=1 while
// [CommissioningWindow.IsOwnWindow] holds, as matter.js
// DeviceCommissioner.allowBasicCommissioning sets the PASE commissioner and
// enters commissioning mode.
//
// Mirrors matter.js packages/protocol/src/protocol/DeviceCommissioner.ts
// (allowBasicCommissioning without byAdministrator, #ownWindowTimeout) and
// packages/node/src/behavior/system/commissioning/CommissioningServer.ts
// enterCommissionableMode.
func (w *CommissioningWindow) OpenOwnWindow(ctx context.Context, timeout time.Duration) error {
	if err := w.blockedErr(); err != nil {
		return err
	}
	w.mu.RLock()
	counter := w.fabricCounter
	w.mu.RUnlock()
	if timeout <= 0 {
		timeout = OwnWindowTimeoutUncommissioned
		if counter != nil {
			if n, err := counter(ctx); err == nil && n > 0 {
				timeout = OwnWindowTimeoutCommissioned
			}
		}
	}

	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return ErrStopped
	}
	if w.open && !w.own {
		w.mu.Unlock()
		return ErrAdministratorWindowOpen
	}
	if w.closeTimer != nil {
		// A window the node restarts is replaced without closing it.
		w.closeTimer.Stop()
	}
	w.open = true
	w.own = true
	w.openedAt = w.clock.Load().Now()
	w.timeout = timeout
	w.expiresAt = w.openedAt.Add(timeout)
	w.durationSec = uint16(min(timeout/time.Second, 1<<16-1)) //nolint:gosec // clamped to uint16
	var timer clock.Timer
	timer = w.clock.Load().AfterFunc(timeout, func() {
		w.mu.Lock()
		if w.closeTimer != timer {
			// Restarted or closed since.
			w.mu.Unlock()
			return
		}
		hook, restore := w.closeLocked()
		w.mu.Unlock()
		runClose(hook, restore)
	})
	w.closeTimer = timer
	hook := w.transitionHooks()
	w.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

// adminOpenRefusedLocked is why an administrator's window cannot open
// now: the bridge stopped ([ErrStopped]) or an administrator's window is
// open (Busy); nil when it can. Caller holds w.mu.
func (w *CommissioningWindow) adminOpenRefusedLocked() error {
	switch {
	case w.stopped:
		return ErrStopped
	case w.open && !w.own:
		return wire.ErrAdmCommBusy
	}
	return nil
}

// stopTimer stops the window's expiry timer, own or administrator, without
// closing the window or firing its hooks: [Bridge.Stop] calls it so no
// timer stays armed on the bridge's clock after Stop (ADR 0026 P5). A
// window open at Stop keeps its state, and an expiry it would have had is
// not run. The bridge is single-run: from then on OpenOwnWindow and
// OpenWindow return [ErrStopped] and arm nothing (review II,
// R2-P2-stop-02).
func (w *CommissioningWindow) stopTimer() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.closeTimer != nil {
		w.closeTimer.Stop()
		w.closeTimer = nil // a callback already running sees it is stale
	}
}

// closeLocked closes the window and returns what the transition fires:
// the hooks and the restore closure of the opener. Caller holds w.mu.
func (w *CommissioningWindow) closeLocked() (hook, restore func()) {
	w.open = false
	w.own = false
	w.adminFabricSet = false
	w.adminVendorSet = false
	w.isBasicWindow = false
	w.hasVerifier = false
	w.jointFabric = false
	if w.closeTimer != nil {
		w.closeTimer.Stop()
		w.closeTimer = nil
	}
	hook = w.transitionHooks()
	restore = w.restore
	w.restore = nil
	return hook, restore
}

// runClose runs what [CommissioningWindow.closeLocked] returned, outside the
// lock: the restore first, so the hook sees the host's standing acceptor.
func runClose(hook, restore func()) {
	if restore != nil {
		restore()
	}
	if hook != nil {
		hook()
	}
}

// CurrentWindow implements [wire.WindowController]. Returns the
// snapshot the AdministratorCommissioning cluster reads attributes
// from.
func (w *CommissioningWindow) CurrentWindow() wire.WindowStatusSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.open || w.own {
		// The node's own window is not an administrator's: matter.js keeps
		// AdministratorCommissioning state.windowStatus for those only.
		return wire.WindowStatusSnapshot{
			Status:            wire.WindowStatusClosed,
			AdminFabricIsNull: true,
			AdminVendorIsNull: true,
		}
	}
	// Derive window status from the mode the window was opened in.
	// matter.js AdministratorCommissioningServer.ts:114-119 sets
	// state.windowStatus = BasicWindowOpen (2) for basic windows and
	// EnhancedWindowOpen (1) for enhanced.
	status := wire.WindowStatusEnhanced
	if w.isBasicWindow {
		status = wire.WindowStatusBasic
	}
	snap := wire.WindowStatusSnapshot{
		Status: status,
	}
	if w.adminFabricSet {
		snap.AdminFabricIndex = w.adminFabric
	} else {
		snap.AdminFabricIsNull = true
	}
	if w.adminVendorSet {
		snap.AdminVendorID = w.adminVendor
	} else {
		snap.AdminVendorIsNull = true
	}
	return snap
}

// OpenWindow implements [wire.WindowController]. The cluster command
// path supplies a verifier-based [wire.OpenWindowParams]; v1.1 stores
// the resulting state but does not yet swap a per-window verifier
// into the bridge's PASE adapter (the bridge already accepts PASE
// against the configured passcode for `duration_seconds`).
//
// A second OpenWindow while one is already live returns BUSY per
// Matter §11.19.7.1.
//
// Opening a window arms no fail-safe (owner decision 47): the
// commissioner's ArmFailSafe or the 60 s arm of its PASE session
// (GeneralCommissioning.AutoArmOnPaseEstablished) does, as in matter.js.
func (w *CommissioningWindow) OpenWindow(ctx context.Context, params wire.OpenWindowParams) error {
	if err := w.blockedErr(); err != nil {
		return err
	}
	// Reject if a FailSafe from a prior commissioning session is still
	// armed. Opening a window while another commissioner's FailSafe is
	// active would overwrite its state; return BUSY instead.
	w.mu.RLock()
	checker := w.failSafeChecker
	w.mu.RUnlock()
	if checker != nil && checker.FailSafeArmed() {
		return wire.ErrAdmCommBusy
	}

	w.mu.Lock()
	if err := w.adminOpenRefusedLocked(); err != nil {
		w.mu.Unlock()
		return err
	}
	maxTimeout := uint32(commissioningWindowMaxSec)
	if params.IsUncommissioned {
		// Uncommissioned devices (no fabric installed yet) MAY use an
		// extended 48-h window per Matter §11.19.8.1, which matches
		// chip CommissioningWindowManager.cpp::MaxCommissioningTimeout()
		// and matter.js AdministratorCommissioningServer.ts:196-198 (the cap; :260 its default).
		// Commissioned bridges use the standard 900-s cap.
		maxTimeout = commissioningWindowMaxSecUncommissioned
	}
	if uint32(params.CommissioningTimeoutSeconds) < 180 || uint32(params.CommissioningTimeoutSeconds) > maxTimeout {
		w.mu.Unlock()
		return ErrCommissioningWindowDurationInvalid
	}
	var replaced func()
	if w.open {
		// An administrator's window replaces the one the node opened itself
		// (matter.js DeviceCommissioner #open with byAdministrator: the
		// node's window is closed — PASE commissioner removed, commissioning
		// mode exited — before the administrator's opens). The close's
		// hooks run before the open's, below.
		hook, restore := w.closeLocked()
		replaced = func() { runClose(hook, restore) }
	}
	w.open = true
	w.discriminator = params.Discriminator
	w.durationSec = params.CommissioningTimeoutSeconds
	w.openedAt = w.clock.Load().Now()
	w.timeout = time.Duration(params.CommissioningTimeoutSeconds) * time.Second
	w.expiresAt = w.openedAt.Add(w.timeout)
	// Persist admin fabric / vendor metadata so the cluster can
	// reflect them via AdminFabricIndex / AdminVendorId attributes
	// per Matter §11.19.5.2.
	w.isBasicWindow = params.IsBasicWindow
	w.hasVerifier = !params.IsBasicWindow && len(params.PAKEPasscodeVerifier) > 0
	w.jointFabric = params.JointFabric && w.hasVerifier
	if params.AdminFabricIndex != 0 {
		w.adminFabric = params.AdminFabricIndex
		w.adminFabricSet = true
	}
	if params.AdminVendorID != 0 {
		w.adminVendor = params.AdminVendorID
		w.adminVendorSet = true
	}
	hook := w.transitionHooks()
	var timer clock.Timer
	timer = w.clock.Load().AfterFunc(time.Duration(params.CommissioningTimeoutSeconds)*time.Second, func() {
		w.mu.Lock()
		if w.closeTimer != timer {
			// Closed (or replaced) since.
			w.mu.Unlock()
			return
		}
		closeHook, restore := w.closeLocked()
		w.mu.Unlock()
		runClose(closeHook, restore)
	})
	w.closeTimer = timer
	w.mu.Unlock()
	if replaced != nil {
		replaced()
	}
	// Enhanced Commissioning Window: install a PASE acceptor built from the
	// commissioner-supplied PAKE verifier for the window lifetime, so PASE
	// runs against the passcode the commissioner chose (not the bridge's
	// configured passcode). Mirrors matter.js
	// AdministratorCommissioningServer.openCommissioningWindow →
	// PaseServer.fromVerificationValue. The install happens before OpenWindow
	// returns — the cluster InvokeResponse, and therefore the commissioner's
	// first PASE datagram, come after — and the configured acceptor is
	// restored on close via setRestore (both the timer and RevokeWindow fire
	// w.restore). Basic windows and the REST/local passcode path carry no
	// verifier and keep the configured acceptor untouched.
	w.mu.RLock()
	installer := w.paseVerifierInstaller
	w.mu.RUnlock()
	if installer != nil && !params.IsBasicWindow && len(params.PAKEPasscodeVerifier) > 0 {
		restore, err := installer.InstallVerifier(params.PAKEPasscodeVerifier, params.Iterations, params.Salt)
		if err != nil {
			// The window is useless without the acceptor — revoke to avoid a
			// dangling open state, then surface the error to the cluster
			// handler.
			_ = w.RevokeWindow(ctx)
			return fmt.Errorf("install PASE verifier: %w", err)
		}
		if !w.setRestore(restore) {
			// Lost the race with the close timer — restore inline so the
			// verifier acceptor never lingers past the already-closed window.
			restore()
		}
	}

	if hook != nil {
		hook()
	}
	return nil
}

// setRestore records the closure invoked on every close transition
// so the ephemeral PASE adapter installed by the opener is replaced
// by the bridge's long-lived configured acceptor regardless of which
// path closes the window. Called by the opener immediately after the
// successful OpenWindow.
//
// Returns false if the window has since closed (race against the
// timer) so the caller can revert the swap itself.
func (w *CommissioningWindow) setRestore(restore func()) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return false
	}
	w.restore = restore
	return true
}

// RevokeWindow implements [wire.WindowController]. Per Matter §11.19.7.3
// step 1, any open PASE session is evicted first — by the host's
// [PaseSessionCloser] when one is wired, and by the bridge the window is
// attached to, which owns the sessions — regardless of whether a
// commissioning window is open. Then the window is closed (if open) and
// the transition hook fires.
func (w *CommissioningWindow) RevokeWindow(ctx context.Context) error {
	// Matter §11.19.7.3 step 1: close any open PASE session before
	// touching window state. Mirrors matter.js
	// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:137-141
	// at db021da0 (revokeCommissioning, step 1: getPaseSession() →
	// paseSession.initiateClose()).
	//
	// Revoked over the PASE session itself, the close waits for the
	// command's response, as matter.js's paseSession.initiateClose defers
	// the close until the session's exchanges end (NodeSession
	// deferredClose): closing first left the RevokeCommissioning
	// unanswered (TC-CADMIN-1.10 step 9).
	w.mu.RLock()
	closer := w.paseSessionCloser
	owner := w.sessionOwner
	w.mu.RUnlock()
	if closer != nil || owner != nil {
		closeNow := func() {
			if closer != nil {
				_ = closer.ClosePaseSessions(context.WithoutCancel(ctx))
			}
			if owner != nil {
				owner()
			}
		}
		if !im.IsPASEFromContext(ctx) || !im.DeferAfterResponse(ctx, closeNow) {
			closeNow()
		}
	}

	// Matter §11.19.7.3 step 1 also expires the fail-safe the PASE
	// session holds, before the window-state check, with the rollback its
	// timeout runs — an aborted commissioning's fabric does not survive
	// the revoke (TC-CGEN-2.4) and the next OpenCommissioningWindow is not
	// Busy. A fail-safe an administrator armed over CASE is left alone
	// (owner decision 42; GeneralCommissioning.ExpireFailSafeArmedOverPASE
	// names where matter.js and connectedhomeip differ).
	w.mu.RLock()
	expirer := w.failSafeExpirer
	w.mu.RUnlock()
	if expirer != nil {
		expirer.ExpireFailSafeArmedOverPASE(context.WithoutCancel(ctx))
	}

	w.mu.Lock()
	if !w.open {
		w.mu.Unlock()
		// Idempotent — internal Bridge.RevokeWindow is callable from
		// timer expiry, shutdown path, and the cluster wire handler.
		// The wire-layer WindowNotOpenError check (Matter §11.19.8.3 Step 2)
		// lives in cluster/wire/admincommissioning.go so the cluster server
		// can still call this method for PASE teardown without surfacing an
		// error on a no-op revoke.
		return nil
	}
	hook, restore := w.closeLocked()
	w.mu.Unlock()
	runClose(hook, restore)
	return nil
}

// EphemeralCredentials are the freshly-generated credentials for one
// commissioning window. The daemon-side [EphemeralProvider] builds
// them against a random salt + the configured iteration count and
// installs the corresponding PASE adapter into the bridge for the
// window's lifetime.
//
// Restore is invoked exactly once per successful generation, after
// the window closes (early revoke or timer expiry), so the bridge's
// long-lived configured PASE acceptor is re-armed. The opener wires
// it via [CommissioningWindow.setRestore] so both close paths fire
// it deterministically.
type EphemeralCredentials struct {
	Discriminator uint16
	Passcode      uint32
	Restore       func()
}

// EphemeralProvider is the daemon-side hook the
// [CommissioningWindowOpener] consumes when ephemeral mode is wired.
// `GenerateAndInstall` MUST: (1) compute a fresh Spake2+ verifier
// against a random passcode + salt, (2) install the resulting PASE
// adapter on the bridge — taking precedence over the configured
// long-lived adapter — and (3) return a Restore closure that re-arms
// the previous adapter on window close.
//
// Returning an error aborts the open: no swap happened, no Restore
// is owed. The opener never invokes Restore on the error path.
type EphemeralProvider interface {
	GenerateAndInstall(ctx context.Context) (EphemeralCredentials, error)
}

// CommissioningWindowOpener is the
// [handlers.MatterCommissioningOpener]-compatible REST adapter the
// daemon wires into [rest.Deps.MatterCommissioningOpener].
//
// Two operating modes:
//
//   - Configured mode (default): the bridge's PASE acceptor is open
//     against the configured passcode/discriminator. "Opening a
//     window" advertises that pair, returns the QR + manual code,
//     and starts the duration timer. Used by ops who want a fixed
//     pairing pad.
//   - Ephemeral mode (when [SetEphemeralProvider] is wired): each
//     OpenCommissioningWindow generates a fresh discriminator +
//     passcode + Spake2+ verifier, swaps them into the bridge's
//     PASE adapter for the window duration, and reverts on close.
//     Recommended for production: the configured passcode never
//     leaves the bridge process and pairing codes auto-rotate.
type CommissioningWindowOpener struct {
	window        *CommissioningWindow
	discriminator uint16
	passcode      uint32
	vendorID      uint16
	productID     uint16

	// mu guards ephemeral.
	mu        sync.Mutex
	ephemeral EphemeralProvider
}

// NewCommissioningWindowOpener wires the opener against an existing
// window controller and the bridge's setup-payload parameters.
func NewCommissioningWindowOpener(w *CommissioningWindow, discriminator uint16, passcode uint32, vendorID, productID uint16) *CommissioningWindowOpener {
	return &CommissioningWindowOpener{
		window:        w,
		discriminator: discriminator,
		passcode:      passcode,
		vendorID:      vendorID,
		productID:     productID,
	}
}

// SetEphemeralProvider wires (or clears, when nil) the per-window
// credential generator. When set, the next call to
// [OpenCommissioningWindow] generates fresh credentials and installs
// a window-scoped PASE adapter on the bridge instead of reusing the
// configured one.
//
// Safe to call concurrently with [OpenCommissioningWindow]; the
// switch is observed atomically per call.
func (o *CommissioningWindowOpener) SetEphemeralProvider(p EphemeralProvider) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.ephemeral = p
	o.mu.Unlock()
}

func (o *CommissioningWindowOpener) snapshotEphemeral() EphemeralProvider {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ephemeral
}

// OpenCommissioningWindowResult is the value the REST handler
// returns. Mirrors `handlers.MatterCommissioningWindowResult`
// without the import cycle.
type OpenCommissioningWindowResult struct {
	Discriminator   uint16
	Passcode        uint32
	DurationSeconds uint16
	QRCode          string
	ManualCode      string
}

// OpenCommissioningWindow generates the setup-payload artifacts and
// kicks the underlying [CommissioningWindow] into the open state.
// Returns a [ErrCommissioningWindowAlreadyOpen]-style error when a
// window is already live; the REST handler maps it to 409 Conflict.
//
// In ephemeral mode the call generates fresh credentials before
// touching the window. Failure paths after a successful generation
// invoke the Restore closure so the bridge's long-lived PASE adapter
// is re-armed even if the QR/manual code build or the window OpenWindow
// rejects later.
func (o *CommissioningWindowOpener) OpenCommissioningWindow(ctx context.Context, durationSeconds uint16) (OpenCommissioningWindowResult, error) {
	if o == nil || o.window == nil {
		return OpenCommissioningWindowResult{}, ErrCommissioningWindowNotConfigured
	}

	disc := o.discriminator
	pass := o.passcode
	var restore func()

	if provider := o.snapshotEphemeral(); provider != nil {
		creds, err := provider.GenerateAndInstall(ctx)
		if err != nil {
			return OpenCommissioningWindowResult{}, fmt.Errorf("ephemeral credentials: %w", err)
		}
		disc = creds.Discriminator
		pass = creds.Passcode
		restore = creds.Restore
	}

	if pass == 0 {
		if restore != nil {
			restore()
		}
		return OpenCommissioningWindowResult{}, ErrCommissioningWindowNotConfigured
	}

	if err := o.window.OpenWindow(ctx, wire.OpenWindowParams{
		CommissioningTimeoutSeconds: durationSeconds,
		Discriminator:               disc,
	}); err != nil {
		if restore != nil {
			restore()
		}
		if errors.Is(err, wire.ErrAdmCommBusy) {
			return OpenCommissioningWindowResult{}, ErrCommissioningWindowAlreadyOpen
		}
		return OpenCommissioningWindowResult{}, err
	}

	// Wire restore so both window-close paths (RevokeWindow + timer
	// expiry) re-arm the configured PASE adapter. setRestore returns
	// false if the window flipped closed already (lost the race with
	// the timer); restore inline in that case so we never leak the
	// ephemeral adapter.
	if restore != nil {
		if !o.window.setRestore(restore) {
			restore()
		}
	}

	qr, err := setup.QRCode(setup.Payload{
		Version:       0,
		VendorID:      o.vendorID,
		ProductID:     o.productID,
		Discriminator: disc,
		Passcode:      pass,
		DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		// Best-effort: revoke the window so the daemon doesn't dangle
		// a half-open state. RevokeWindow itself fires the restore.
		_ = o.window.RevokeWindow(ctx)
		return OpenCommissioningWindowResult{}, err
	}
	manual, err := setup.ManualCode(disc, pass)
	if err != nil {
		_ = o.window.RevokeWindow(ctx)
		return OpenCommissioningWindowResult{}, err
	}
	return OpenCommissioningWindowResult{
		Discriminator:   disc,
		Passcode:        pass,
		DurationSeconds: durationSeconds,
		QRCode:          qr,
		ManualCode:      manual,
	}, nil
}

// RandomDiscriminator returns a uniformly-random 12-bit discriminator
// suitable for a Matter setup payload. The Matter spec (§5.1.1.1.2)
// reserves no specific values, so any 0..0xFFF is legal.
func RandomDiscriminator() (uint16, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("rand discriminator: %w", err)
	}
	return binary.BigEndian.Uint16(b[:]) & 0x0FFF, nil
}

// RandomPasscode returns a uniformly-random 8-decimal-digit passcode
// from the §5.1.1.1.1 valid-passcode set: a candidate that fails
// [setup.IsValidPasscode] — 0, the all-same-digit values 11111111..99999999,
// 12345678 and 87654321, matter.js packages/types/src/schema/
// PairingCodeSchema.ts:160-162 INVALID_PASSCODES at 862ad75f — is
// re-rolled, as matter.js PaseClient.generateRandomPasscode does
// (packages/protocol/src/session/pase/PaseClient.ts:55-67). The rule is
// that one function; this one only draws.
func RandomPasscode() (uint32, error) {
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("rand passcode: %w", err)
		}
		// 0..99999998: 99999999 is out of range anyway.
		v := binary.BigEndian.Uint32(b[:]) % 99999999
		if !setup.IsValidPasscode(v) {
			continue
		}
		return v, nil
	}
}

// RandomSalt returns 32 random bytes for the Spake2+ Verifier — the
// longest salt Matter §3.10.1.5 admits (16 to 32 bytes). Mirrors matter.js
// packages/protocol/src/protocol/DeviceCommissioner.ts:134 (the basic
// window's `randomBytes(32)`) and
// packages/node/src/behavior/system/commissioning/CommissioningClient.ts:524
// (the enhanced window's).
func RandomSalt() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("rand salt: %w", err)
	}
	return b, nil
}

// Commissioning window timeout constants.
//
//   - commissioningWindowMaxSec — standard upper bound for a commissioned
//     bridge per Matter §11.19.8.1 (chip CommissioningWindowManager.cpp
//     kMaxCommissioningTimeout = 900 s).
//   - commissioningWindowMaxSecUncommissioned — extended upper bound for an
//     uncommissioned node (FabricCount == 0) per Matter §11.19.8.1 and chip
//     CommissioningWindowManager.cpp::MaxCommissioningTimeout() which returns
//     172 800 s (48 h) when the fabric table is empty.
const (
	commissioningWindowMaxSec               = uint16(900)
	commissioningWindowMaxSecUncommissioned = uint32(172800) // 48 h per Matter §11.19.8.1
)

// commWindowDurationInvalidErr is the typed [im.StatusCodeError] backing
// [ErrCommissioningWindowDurationInvalid]. The IM dispatcher type-asserts
// against [im.StatusCodeError] first, so this causes InvalidCommand (0x85)
// to be returned. Mirrors matter.js
// AdministratorCommissioningServer.ts:#assertCommissioningWindowRequirements
// (lines 199-209) which throws StatusResponseError with Status.InvalidCommand.
type commWindowDurationInvalidErr struct{}

func (commWindowDurationInvalidErr) Error() string {
	return "bridge: commissioning window duration out of range"
}

func (commWindowDurationInvalidErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// Errors surfaced by the commissioning-window APIs.
var (
	// ErrCommissioningWindowAlreadyOpen surfaces when a second
	// OpenCommissioningWindow lands while a window is already live.
	// The REST handler maps it to 409 Conflict.
	ErrCommissioningWindowAlreadyOpen = errors.New("bridge: commissioning window already open")
	// ErrCommissioningWindowNotConfigured surfaces when the bridge
	// has no passcode configured. The REST handler maps it to 503.
	ErrCommissioningWindowNotConfigured = errors.New("bridge: commissioning not configured (no passcode)")
	// ErrCommissioningWindowDurationInvalid surfaces when the
	// requested duration is outside the §11.19.8.1 valid range.
	// For commissioned bridges the range is [180, 900] s; for
	// uncommissioned devices (FabricCount == 0) the range is
	// [180, 172800] s (48 h).
	// The typed backing value causes the IM dispatcher to return
	// InvalidCommand (0x85) per matter.js
	// AdministratorCommissioningServer.ts:#assertCommissioningWindowRequirements
	// (lines 199-209) which throws Status.InvalidCommand for out-of-range
	// timeout values.
	ErrCommissioningWindowDurationInvalid error = commWindowDurationInvalidErr{}
)

// EndCommissioning closes the window after a successful
// CommissioningComplete: the commissioner it was opened for is done, so the
// window must not admit another one until an administrator opens a new
// one. Unlike [CommissioningWindow.RevokeWindow] it neither evicts PASE
// sessions nor touches the fail-safe — CommissioningComplete has already
// disarmed it, and the bridge clears the PASE sessions once the command's
// response is out (pase_close.go). No-op when no window is open. Mirrors matter.js
// DeviceCommissioner.endCommissioning, which failsafeContext.commissioned
// triggers (DeviceCommissioner.ts:160). Without it a second
// OpenCommissioningWindow right after a commissioning answered BUSY
// (TC-CADMIN-1.3 step 9, TC-ACL-2.8).
func (w *CommissioningWindow) EndCommissioning() {
	w.mu.Lock()
	if !w.open {
		w.mu.Unlock()
		return
	}
	hook, restore := w.closeLocked()
	w.mu.Unlock()
	runClose(hook, restore)
}

// FabricRemoved clears AdminFabricIndex when the fabric that opened the
// window is removed while it is open, and reports the change. Mirrors
// matter.js AdministratorCommissioningServer #fabricRemovedCallback, wired
// to the admin fabric's deleting event when the window opens; the window
// itself and AdminVendorId stay (TC-CADMIN-1.25 step 29).
func (w *CommissioningWindow) FabricRemoved(fabricIndex uint8) {
	w.mu.Lock()
	if !w.open || !w.adminFabricSet || w.adminFabric != fabricIndex {
		w.mu.Unlock()
		return
	}
	w.adminFabricSet = false
	hook := w.transitionHooks()
	w.mu.Unlock()
	if hook != nil {
		hook()
	}
}

// transitionHooks returns the hooks a transition fires — the host's
// SetTransitionHook and the bridge's attribute reporting — as one func, or
// nil when neither is set. Caller holds w.mu.
func (w *CommissioningWindow) transitionHooks() func() {
	host, own := w.onTransition, w.onStateChanged
	if host == nil && own == nil {
		return nil
	}
	return func() {
		if own != nil {
			own()
		}
		if host != nil {
			host()
		}
	}
}

// setStateChanged installs the bridge's transition hook.
func (w *CommissioningWindow) setStateChanged(fn func()) {
	w.mu.Lock()
	w.onStateChanged = fn
	w.mu.Unlock()
}
