// SPDX-License-Identifier: MIT
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
	br        *bridge.Bridge
	sessions  *operational.Manager
	root      *RootClusters
	logger    *slog.Logger
	baseline  *bridge.PerExchangePaseProvider // the configured-passcode provider
	advert    bridge.CommissioningAdvertisement
	installMu sync.Mutex
	active    *bridge.PerExchangePaseProvider // the window's provider, nil between windows
	// observer is the host's OnWindowTransition (nil: none).
	observer func(WindowSnapshot) error

	announcedMu            sync.Mutex
	announcedMode          uint8  // CM of the published commissionable record; 0 none
	announcedDiscriminator uint16 // its discriminator
}

// wireCommissioningWindow builds the window, gives it to the bridge and the
// cluster, and installs the hooks. Called once the session manager and the
// configured PASE provider exist.
func wireCommissioningWindow(ctx context.Context, h *commissioningHost, st *store.Store) {
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
		h.transition(ctx, window)
		// The host's observer runs after the facade's handling.
		h.notifyTransition(window)
	})
}

// transition is the facade's handling of a window transition: the PASE
// acceptor and the commissionable record follow the window.
func (h *commissioningHost) transition(ctx context.Context, window *bridge.CommissioningWindow) {
	snap := window.CurrentWindow()
	if window.IsOwnWindow() {
		// The node's own window: PASE with the configured passcode,
		// advertised CM=1 (matter.js allowBasicCommissioning).
		h.attachPase(h.baseline)
		h.announce(ctx, h.advert, "commissioning.own_window.announce")
		h.logger.Info("commissioning.own_window.open")
		return
	}
	if snap.Status == wire.WindowStatusEnhanced && window.HasSuppliedVerifier() {
		adv := h.advert
		adv.Discriminator = window.Discriminator()
		adv.CommissioningMode = 2 // §4.3.1.3 CM=2: enhanced window opened by an admin
		h.announce(ctx, adv, "commissioning.window.announce")
		h.logger.Info("commissioning.window.open", slog.Int("discriminator", int(adv.Discriminator)))
		return
	}
	if snap.Status == wire.WindowStatusClosed {
		// No window: no PASE acceptor and no commissionable record
		// (matter.js #closeWindow: removePaseCommissioner,
		// exitCommissioningMode).
		h.attachPase(nil)
		h.br.WithdrawCommissioning(ctx)
		h.setAnnounced(0, 0)
		h.logger.Info("commissioning.window.closed")
	}
}

// announce publishes adv and, once published, records what the node
// advertises.
func (h *commissioningHost) announce(ctx context.Context, adv bridge.CommissioningAdvertisement, event string) {
	if err := h.br.AnnounceCommissioning(ctx, adv); err != nil {
		h.logger.Warn(event, slog.String("err", err.Error()))
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
func (h *commissioningHost) reopenIfDecommissioned(ctx context.Context, st *store.Store) {
	win := h.br.CommissioningWindow()
	if win == nil || win.IsOpen() {
		return
	}
	if recs, err := st.ListFabrics(ctx); err != nil || len(recs) > 0 {
		return
	}
	if err := win.OpenOwnWindow(ctx, 0); err != nil {
		h.logger.Warn("commissioning.own_window", slog.String("err", err.Error()))
	}
}

// InstallVerifier implements [bridge.PaseVerifierInstaller]: for the
// window's lifetime, PASE runs against the commissioner's verifier.
func (h *commissioningHost) InstallVerifier(verifier []byte, iterations uint32, salt []byte) (func(), error) {
	const want = spake2.VerifierW0Size + spake2.VerifierLSize // 97 bytes, Matter §3.10.5
	if len(verifier) != want {
		return nil, fmt.Errorf("PAKE passcode verifier is %d bytes, want %d", len(verifier), want)
	}
	vc, err := spake2.NewVerifierFromValue(verifier[:spake2.VerifierW0Size], verifier[spake2.VerifierW0Size:])
	if err != nil {
		return nil, fmt.Errorf("PAKE passcode verifier: %w", err)
	}
	provider := bridge.NewPerExchangePaseProvider(func() *bridge.PaseAdapter {
		adapter, err := buildPaseAdapterFromContext(h.sessions, h.root, vc, salt, int(iterations))
		if err != nil {
			h.logger.Warn("pase.window.build_failed", slog.String("err", err.Error()))
			return nil
		}
		return adapter
	})
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
	}, nil
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
		return errors.New("no session manager")
	}
	c.sessions.CloseFabric(0)
	return nil
}
