// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/store"
)

// Multi-admin: the AdministratorCommissioning cluster (0x003C), which
// RootNode mandates, and the commissioning window behind it.
//
// A controller that already owns a fabric shares the device with a second
// one by invoking OpenCommissioningWindow with a PAKE verifier computed from
// a passcode it chose (chip-tool `pairing open-commissioning-window`, the
// "share" button in every ecosystem app). For the window's lifetime the
// device must run PASE against THAT verifier rather than its own passcode,
// advertise itself as in an enhanced window (CM=2) under the discriminator
// the window carries, and fall back to its configured state when the window
// closes or is revoked. The cluster server owns the command surface; the
// [bridge.CommissioningWindow] owns the window; this file plugs the
// two into the node's PASE provider and advertisement (ported from
// examples/reference-bridge/commissioning.go). Mirrors matter.js
// AdministratorCommissioningServer.ts openCommissioningWindow →
// PaseServer.fromVerificationValue, and the advertisement of
// DeviceCommissioner.ts.

// commissioningHost is what the window needs from the node. Each field is
// set once during wiring.
type commissioningHost struct {
	br       *bridge.Bridge
	sessions *operational.Manager
	root     *RootClusters
	logger   *slog.Logger
	baseline *bridge.PerExchangePaseProvider // the configured-passcode provider
	// responderParams is what the window's PASE adapters advertise
	// (security.go responderSessionParams).
	responderParams *sessionparams.Parameters
	advert          bridge.CommissioningAdvertisement
	// installMu guards active.
	installMu sync.Mutex
	active    *bridge.PerExchangePaseProvider // the window's provider, nil between windows
	// observer is the host's OnWindowTransition (nil: none).
	observer func(WindowSnapshot) error

	// basicMu guards basic.
	basicMu sync.Mutex
	basic   *basicWindow // the open OpenBasicWindow window, nil without one

	// announcedMu guards announcedDiscriminator and announcedMode.
	announcedMu            sync.Mutex
	announcedMode          uint8  // CM of the published commissionable record; 0 none
	announcedDiscriminator uint16 // its discriminator
}

// wireCommissioningWindow builds the window, gives it to the bridge and the
// cluster, and installs the hooks. Called once the session manager and the
// configured PASE provider exist.
func wireCommissioningWindow(ctx context.Context, h *commissioningHost, st store.Fabrics) {
	window := bridge.NewCommissioningWindow()
	h.br.AttachCommissioningWindow(window)
	admin := h.root.AdministratorCommissioning
	admin.SetController(window)
	admin.SetIsFailSafeArmed(h.root.GeneralCommissioning.FailSafeArmed)
	admin.SetVendorIDResolver(func(ctx context.Context, fabricIndex uint8) uint16 {
		rec, err := st.GetFabric(ctx, fabricIndex)
		if err != nil {
			return 0
		}
		return rec.VendorID
	})
	fabrics := func(ctx context.Context) (int, error) {
		recs, err := st.ListFabrics(ctx)
		return len(recs), err
	}
	admin.SetFabricCounter(fabrics)
	window.SetFabricCounter(fabrics)

	window.SetFailSafeChecker(h.root.GeneralCommissioning)
	window.SetFailSafeArmer(h.root.GeneralCommissioning)
	window.SetPaseSessionCloser(paseCloser{h.sessions})
	window.SetPaseVerifierInstaller(h)
	window.SetTransitionHook(func() {
		//nolint:contextcheck // the hook fires on a window transition, outside any request; the announce is bounded by the node's lifetime
		reopen := h.transition(ctx, window)
		// The host's observer runs after the facade's handling.
		h.notifyTransition(window)
		if reopen {
			// A basic window closed on a node without a fabric: the node's
			// own window with the configured credentials comes back, as at
			// start. After the observer saw the close, so the host sees
			// close, then open.
			h.reopenIfDecommissioned(ctx, st) //nolint:contextcheck // as above
		}
	})
}

// transition is the facade's handling of a window transition: the PASE
// acceptor and the commissionable record follow the window. It reports
// whether a [Node.OpenBasicWindow] window just closed with no other window
// open, after which the caller restores the node's own window if the node
// has no fabric.
func (h *commissioningHost) transition(ctx context.Context, window *bridge.CommissioningWindow) (reopen bool) {
	snap := window.CurrentWindow()
	basic := h.currentBasic()
	endedBasic := false
	if basic != nil && !window.IsOwnWindow() {
		// The basic window closed, or an administrator's replaced it: its
		// acceptor goes (the configured one is not attached here — the
		// branches below decide what the window now accepts).
		h.endBasic(basic)
		basic, endedBasic = nil, true
	}
	if window.IsOwnWindow() {
		if basic != nil && !h.consumeOpening(basic) {
			// The own window was (re)opened directly
			// (CommissioningWindow().OpenOwnWindow) while a basic window
			// was open: the basic window ends, and the restarted window
			// runs with the configured credentials and discriminator.
			h.endBasic(basic)
			basic = nil
		}
		// The node's own window: PASE with the configured passcode,
		// advertised CM=1 (matter.js allowBasicCommissioning) — or, for
		// the transition OpenBasicWindow's own open fires, the basic
		// window's acceptor (installed, so attachPase leaves it) and its
		// discriminator.
		h.attachPase(h.baseline)
		adv := h.advert
		if basic != nil {
			adv.Discriminator = basic.discriminator
		}
		h.announce(ctx, adv, "commissioning.own_window.announce")
		h.logger.Info("commissioning.own_window.open", slog.Int("discriminator", int(adv.Discriminator)))
		return false
	}
	if snap.Status == wire.WindowStatusEnhanced && window.HasSuppliedVerifier() {
		adv := h.advert
		adv.Discriminator = window.Discriminator()
		adv.CommissioningMode = 2 // §4.3.1.3 CM=2: enhanced window opened by an admin
		h.announce(ctx, adv, "commissioning.window.announce")
		h.logger.Info("commissioning.window.open", slog.Int("discriminator", int(adv.Discriminator)))
		return false
	}
	if snap.Status == wire.WindowStatusClosed {
		// No window: no PASE acceptor and no commissionable record
		// (matter.js #closeWindow: removePaseCommissioner,
		// exitCommissioningMode).
		h.attachPase(nil)
		h.br.WithdrawCommissioning(ctx)
		h.setAnnounced(0, 0)
		h.logger.Info("commissioning.window.closed")
		return endedBasic && !window.IsOpen()
	}
	return false
}

// basicWindow is an open [Node.OpenBasicWindow] window: the discriminator
// it is advertised under and the restore of its PASE acceptor.
type basicWindow struct {
	discriminator uint16
	restore       func()
	// opening marks the window until the transition its own
	// OpenOwnWindow fires has consumed it (guarded by basicMu).
	opening bool
}

// consumeOpening reports whether b was marked opening, clearing the mark.
func (h *commissioningHost) consumeOpening(b *basicWindow) bool {
	h.basicMu.Lock()
	defer h.basicMu.Unlock()
	was := b.opening
	b.opening = false
	return was
}

// currentBasic returns the open basic window, nil without one.
func (h *commissioningHost) currentBasic() *basicWindow {
	h.basicMu.Lock()
	defer h.basicMu.Unlock()
	return h.basic
}

// endBasic removes b's acceptor once, if b is still the open basic window.
func (h *commissioningHost) endBasic(b *basicWindow) {
	h.basicMu.Lock()
	if h.basic != b {
		h.basicMu.Unlock()
		return
	}
	h.basic = nil
	h.basicMu.Unlock()
	b.restore()
}

// openBasic opens the node's own window with vc as its PASE acceptor,
// advertised CM=1 under discriminator (see [Node.OpenBasicWindow]).
func (h *commissioningHost) openBasic(ctx context.Context, vc *spake2.VerifierContext, iterations uint32, salt []byte, discriminator uint16, timeout time.Duration) error {
	window := h.br.CommissioningWindow()
	if window.IsOpen() && !window.IsOwnWindow() {
		// Checked before the install, which would replace the
		// administrator's acceptor.
		return bridge.ErrAdministratorWindowOpen
	}
	//nolint:contextcheck // the provider outlives this call; the adapters it builds carry no ctx of their own (as InstallVerifier's)
	b := &basicWindow{discriminator: discriminator, restore: h.installVerifierContext(vc, iterations, salt), opening: true}
	h.basicMu.Lock()
	prev := h.basic
	h.basic = b
	h.basicMu.Unlock()
	if prev != nil {
		// A basic window replaced by the next: its provider stops (the
		// new one is installed already, so its restore leaves the
		// bridge's acceptor alone).
		prev.restore()
	}
	if err := window.OpenOwnWindow(ctx, timeout); err != nil {
		h.endBasic(b)
		return err
	}
	return nil
}

// announce publishes adv and, once published, records what the node
// advertises.
func (h *commissioningHost) announce(ctx context.Context, adv bridge.CommissioningAdvertisement, event string) {
	if err := h.br.AnnounceCommissioning(ctx, adv); err != nil {
		h.logger.Warn(event, slog.Any("err", err))
		return
	}
	h.setAnnounced(adv.CommissioningMode, adv.Discriminator)
}

// setAnnounced records the commissioning mode and discriminator of the
// published commissionable record (0, 0: none).
func (h *commissioningHost) setAnnounced(mode uint8, discriminator uint16) {
	h.announcedMu.Lock()
	h.announcedMode, h.announcedDiscriminator = mode, discriminator
	h.announcedMu.Unlock()
}

// notifyTransition hands the window after a transition to the host's
// OnWindowTransition.
func (h *commissioningHost) notifyTransition(window *bridge.CommissioningWindow) {
	fn := h.observer
	if fn == nil {
		return
	}
	h.announcedMu.Lock()
	snap := WindowSnapshot{
		Open:              window.IsOpen(),
		Own:               window.IsOwnWindow(),
		Administrator:     window.CurrentWindow(),
		CommissioningMode: h.announcedMode,
		Discriminator:     h.announcedDiscriminator,
	}
	h.announcedMu.Unlock()
	observe(h.logger, "window_transition", func() error { return fn(snap) })
}

// attachPase makes p the bridge's PASE acceptor (nil: none) unless an
// administrator's verifier window has its own installed.
func (h *commissioningHost) attachPase(p *bridge.PerExchangePaseProvider) {
	h.installMu.Lock()
	defer h.installMu.Unlock()
	if h.active != nil {
		return
	}
	if p == nil {
		h.br.AttachPaseHandlerProvider(nil)
		return
	}
	h.br.AttachPaseHandlerProvider(p.Resolve)
}

// reopenIfDecommissioned opens the node's own window once its last fabric is
// gone and no window is open (a rolled-back first commissioning leaves the
// node's own window open, which keeps its timer).
func (h *commissioningHost) reopenIfDecommissioned(ctx context.Context, st store.Fabrics) {
	win := h.br.CommissioningWindow()
	if win == nil || win.IsOpen() {
		return
	}
	if recs, err := st.ListFabrics(ctx); err != nil || len(recs) > 0 {
		return
	}
	if err := win.OpenOwnWindow(ctx, 0); err != nil {
		h.logger.Warn("commissioning.own_window", slog.Any("err", err))
	}
}

// InstallVerifier implements [bridge.PaseVerifierInstaller]: for the
// window's lifetime, PASE runs against the commissioner's verifier.
func (h *commissioningHost) InstallVerifier(verifier []byte, iterations uint32, salt []byte) (func(), error) {
	const want = spake2.VerifierW0Size + spake2.VerifierLSize // 97 bytes, Matter §3.10.5
	if len(verifier) != want {
		return nil, fmt.Errorf("node: PAKE passcode verifier is %d bytes, want %d", len(verifier), want)
	}
	vc, err := spake2.NewVerifierFromValue(verifier[:spake2.VerifierW0Size], verifier[spake2.VerifierW0Size:])
	if err != nil {
		return nil, fmt.Errorf("PAKE passcode verifier: %w", err)
	}
	return h.installVerifierContext(vc, iterations, salt), nil
}

// installVerifierContext makes a PASE acceptor over vc the bridge's until
// the returned restore runs: the path both an administrator's verifier
// (InstallVerifier) and [Node.OpenBasicWindow]'s credentials take.
func (h *commissioningHost) installVerifierContext(vc *spake2.VerifierContext, iterations uint32, salt []byte) (restore func()) {
	provider := bridge.NewPerExchangePaseProvider(func() *bridge.PaseAdapter {
		adapter, err := buildPaseAdapterFromContext(h.sessions, h.root, vc, salt, int(iterations), h.responderParams)
		if err != nil {
			h.logger.Warn("pase.window.build_failed", slog.Any("err", err))
			return nil
		}
		return adapter
	})
	provider.SetClock(h.br.Clock())
	provider.StartReaper(context.Background(), 30*time.Second, time.Minute)

	h.installMu.Lock()
	if h.active != nil {
		h.active.Stop()
	}
	h.active = provider
	h.br.AttachPaseHandlerProvider(provider.Resolve)
	h.installMu.Unlock()
	h.logger.Info("pase.window.verifier_installed", slog.Int("iterations", int(iterations)))

	return func() {
		h.installMu.Lock()
		defer h.installMu.Unlock()
		if h.active == provider {
			// Back to no acceptor; the window's close hook attaches the
			// configured one again if the node's own window is open.
			h.active = nil
			h.br.AttachPaseHandlerProvider(nil)
		}
		provider.Stop()
		h.logger.Info("pase.window.verifier_restored")
	}
}

// stop ends a window provider still running at shutdown.
func (h *commissioningHost) stop() {
	h.installMu.Lock()
	defer h.installMu.Unlock()
	if h.active != nil {
		h.active.Stop()
		h.active = nil
	}
}

// paseCloser closes every PASE session when a window is revoked (Matter
// §11.19.7.3 step 1). PASE sessions live under fabric index 0.
type paseCloser struct{ sessions *operational.Manager }

// ClosePaseSessions implements [bridge.PaseSessionCloser].
func (c paseCloser) ClosePaseSessions(context.Context) error {
	if c.sessions == nil {
		return errors.New("node: no session manager")
	}
	c.sessions.CloseFabric(0)
	return nil
}

// WindowCredentials are the setup credentials of one [Node.OpenBasicWindow]
// window.
type WindowCredentials struct {
	// Discriminator is the 12-bit discriminator the window is advertised
	// under.
	Discriminator uint16
	// Passcode is the window's setup passcode; it must pass
	// setup.IsValidSetupPIN.
	Passcode uint32
	// Iterations is the SPAKE2+ PBKDF2 iteration count, within
	// [spake2.IterationsMin, spake2.IterationsMax].
	Iterations uint32
	// Salt is the SPAKE2+ salt, spake2.PBKDFMinSaltSize to
	// spake2.PBKDFMaxSaltSize bytes.
	Salt []byte
	// Timeout is how long the window stays open; 0 is the node's own
	// window default (bridge.OwnWindowTimeoutUncommissioned while the node
	// has no fabric, bridge.OwnWindowTimeoutCommissioned once it has one).
	Timeout time.Duration
}

// ErrInvalidWindowCredentials wraps every [Node.OpenBasicWindow]
// credential failure.
var ErrInvalidWindowCredentials = errors.New("node: invalid window credentials")

// validate holds the credential checks [Config.Validate] applies to the
// configured commissioning secret.
func (c *WindowCredentials) validate() error {
	var errs []error
	if err := checkDiscriminator(c.Discriminator); err != nil {
		errs = append(errs, err)
	}
	if !validSetupPIN(c.Passcode) {
		errs = append(errs, errSetupPIN(c.Passcode))
	}
	errs = append(errs, checkPBKDF(int64(c.Iterations), len(c.Salt))...)
	if c.Timeout < 0 {
		errs = append(errs, fmt.Errorf("node: timeout %s is negative", c.Timeout))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidWindowCredentials, err)
	}
	return nil
}

// OpenBasicWindow opens the node's own commissioning window with its own
// credentials: PASE runs against a verifier computed from creds
// (spake2.NewVerifierContext, the function commissioning/ uses) and
// installed through the path an administrator's verifier takes, and the
// node is advertised with CM=1 under creds.Discriminator. The window is
// the node's own (CommissioningWindow().IsOwnWindow): it reads WindowNotOpen
// on the AdministratorCommissioning cluster, an administrator's
// OpenCommissioningWindow replaces it, and it closes on its timeout, on
// CommissioningComplete and on RevokeCommissioning. Opened while a basic
// window or the node's own window is open, it replaces that window; while
// an administrator's window is open it returns
// bridge.ErrAdministratorWindowOpen. A direct
// CommissioningWindow().OpenOwnWindow while it is open ends it: the
// restarted window runs with the configured credentials and is advertised
// under the configured discriminator. Once it closes, the configured
// credentials are back: a node without a fabric reopens its own window
// under the configured discriminator, a commissioned node publishes no
// commissionable record until the next window — the facade's window
// semantics, unchanged.
//
// matter.js has no per-window basic credentials: openBasicCommissioningWindow
// opens the window with the node's own passcode
// (packages/node/src/behaviors/administrator-commissioning/
// AdministratorCommissioningServer.ts:105-121), and DeviceCommissioner
// #openWindow advertises `discriminator ?? commissioningConfig.discriminator`
// (packages/protocol/src/protocol/DeviceCommissioner.ts:236-256). This is a
// host extension, exposed as the facade's own API rather than as a cluster
// command.
func (n *Node) OpenBasicWindow(ctx context.Context, creds WindowCredentials) error {
	if err := creds.validate(); err != nil {
		return err
	}
	vc, err := spake2.NewVerifierContext(creds.Passcode, creds.Salt, int(creds.Iterations))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidWindowCredentials, err)
	}
	return n.window.openBasic(ctx, vc, creds.Iterations, append([]byte(nil), creds.Salt...), creds.Discriminator, creds.Timeout)
}
