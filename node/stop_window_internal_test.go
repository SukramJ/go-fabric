// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// stopTestSalt is a 16-byte SPAKE2+ salt (spake2.PBKDFMinSaltSize).
var stopTestSalt = []byte("SPAKE2P Key Salt")

// activePase is the window's PASE provider, nil without one.
func (h *commissioningHost) activePase() *bridge.PerExchangePaseProvider {
	h.installMu.Lock()
	defer h.installMu.Unlock()
	return h.active
}

// TestWindowAPIsAfterStop (review II, R2-P2-stop-02): once the node has
// stopped, the host's window calls return ErrStopped, an administrator's
// window and its verifier are refused, and nothing arms a timer on the
// node's clock or leaves a PASE provider behind. Before, OpenOwnWindow
// armed a 48 h timer and OpenBasicWindow a 30 s reaper with a provider
// attached on the stopped node.
func TestWindowAPIsAfterStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	n, fake, _ := commissionedKeeperNode(t, nil)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.OpenOwnWindow(ctx, 0); !errors.Is(err, ErrStopped) {
		t.Errorf("OpenOwnWindow after Stop = %v, want ErrStopped", err)
	}
	creds := WindowCredentials{Discriminator: 0x123, Passcode: 20202020, PBKDFIterations: 1000, Salt: stopTestSalt}
	if err := n.OpenBasicWindow(ctx, creds); !errors.Is(err, ErrStopped) {
		t.Errorf("OpenBasicWindow after Stop = %v, want ErrStopped", err)
	}
	if err := n.CloseWindow(ctx); !errors.Is(err, ErrStopped) {
		t.Errorf("CloseWindow after Stop = %v, want ErrStopped", err)
	}
	// An administrator's OpenCommissioningWindow dispatched after the
	// bridge stopped: the window refuses it.
	if err := n.br.CommissioningWindow().OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180, IsBasicWindow: true}); !errors.Is(err, bridge.ErrStopped) {
		t.Errorf("an administrator's window after Stop = %v, want bridge.ErrStopped", err)
	}
	// Its verifier, installed once the node's window host stopped, is
	// refused and starts no reaper.
	vc, err := spake2.NewVerifierContext(20202020, stopTestSalt, 1000)
	if err != nil {
		t.Fatal(err)
	}
	l, err := vc.L.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	verifier := append(vc.W0.FillBytes(make([]byte, spake2.VerifierW0Size)), l...)
	if restore, err := n.window.InstallVerifier(verifier, 1000, stopTestSalt); err == nil {
		restore()
		t.Error("InstallVerifier after Stop succeeded, want an error")
	}
	if armed := fake.Armed(); len(armed) != 0 {
		t.Errorf("armed after the calls: %v", armed)
	}
	if p := n.window.activePase(); p != nil {
		t.Error("a PASE provider is active on the stopped node")
	}
	if w := n.Window(); w.Open {
		t.Errorf("window open after Stop: %+v", w)
	}
}

// publishRecorder records the commissionable publishes it receives.
type publishRecorder struct {
	*mdns.Noop
	mu        sync.Mutex
	published []error // each commissionable publish's ctx.Err()
}

func (a *publishRecorder) Publish(ctx context.Context, svc mdns.Service) error {
	if svc.ServiceType == mdns.ServiceTypeCommissionable {
		a.mu.Lock()
		a.published = append(a.published, ctx.Err())
		a.mu.Unlock()
	}
	return a.Noop.Publish(ctx, svc)
}

func (a *publishRecorder) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.published)
}

// TestTransitionDuringStopAnnouncesNothing (review II, R2-P2-stop-04): a
// window transition between the start of Stop and the bridge's stop — the
// own window's timer, an administrator's command, here the window opened
// directly — publishes no commissionable record and reaches no BLE step:
// Stop withdraws every record itself. Before, the transition announced
// under the cancelled window lifetime.
func TestTransitionDuringStopAnnouncesNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	adv := &publishRecorder{Noop: mdns.NewNoop()}
	n, _, _ := commissionedKeeperNode(t, adv)
	// Stop's first step (stopParts): the window lifetime ends and the
	// keepers' gate closes.
	n.cancelWindow()
	n.keeperMu.Lock()
	n.stopping = true
	n.keeperMu.Unlock()
	before := adv.count()
	if err := n.br.CommissioningWindow().OpenOwnWindow(ctx, 0); err != nil {
		t.Fatalf("the window's open in the gap: %v", err)
	}
	if got := adv.count() - before; got != 0 {
		t.Errorf("%d commissionable publishes after Stop began (ctx errors %v), want none", got, adv.published[before:])
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
