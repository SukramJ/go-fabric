// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/commissioning/CommissioningWindowLifetimeTest.ts
// at 862ad75f: the node's own window restarted and kept across its old
// timer, concurrent administrator windows, a close callback opening the next
// window, and an administrator's window the node's own does not replace.
//
// matter.js runs MockSite pairs and drives the node's DeviceCommissioner
// (device.env.get(DeviceCommissioner)) and CommissioningServer
// .enterCommissionableMode; the port runs fabrictest pairs and drives the
// node's bridge.CommissioningWindow (Node.CommissioningWindow, OpenOwnWindow
// being enterCommissionableMode), with the administrator's
// OpenCommissioningWindow sent over the wire as sendOpenCommissioningWindow
// does. windowStatusOf is the window's status as DeviceCommissioner reports
// it: the node's own window reads BasicWindowOpen there (it reads
// WindowNotOpen on the cluster). A MatterFlowError is the Go error of the
// open; Busy is FAILURE with the cluster status Busy (expectBusy).
//
// The cases ported earlier are bridge:TestOwnWindow_DefaultTimeoutFollowsFabricCount,
// TestOwnWindow_TimerClosesIt, TestOwnWindow_AdministratorReplacesIt,
// TestOwnWindow_RevokeCommissioningClosesIt,
// TestCommissioningWindow_OpenWindow_Twice_ReturnsBusy,
// TestWireCommissioned_ArmFailSafeOverCASEWhileWindowOpen and
// TestCommissioningWindow_RevokeWindow_OnOpenWindow_StatusReturnsToClosed.
//
// Missing (register): "replaces a window the node opened itself with an
// administrator's basic window" — the Go AdministratorCommissioning has no
// Basic feature (BC), so OpenBasicCommissioningWindow answers
// UnsupportedCommand; "refuses a window the node opens while an
// administrator's window is still opening" and "opens no window once the
// commissioner closes during the open" — js-runtime-only: DeviceCommissioner
// opens asynchronously (#open awaits the previous window's close), where the
// Go window opens and closes atomically under its lock, so there is no
// window still opening; "ignores a close after the commissioner closed" —
// js-runtime-only: DeviceCommissioner.close ends the commissioner for good,
// the Go window has no terminal close (it ends with the node, Node.Stop).
package node_test

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/tlv"
)

// windowStatusOf is DeviceCommissioner.windowStatus: WindowNotOpen,
// EnhancedWindowOpen or BasicWindowOpen, the node's own window reading
// BasicWindowOpen.
func windowStatusOf(d *fabrictest.Device) wire.CommissioningWindowStatus {
	w := d.Node().CommissioningWindow()
	switch {
	case !w.IsOpen():
		return wire.WindowStatusClosed
	case w.IsOwnWindow():
		return wire.WindowStatusBasic
	default:
		return w.CurrentWindow().Status
	}
}

// openOwnWindow is CommissioningServer.enterCommissionableMode.
func openOwnWindow(t *testing.T, d *fabrictest.Device) {
	t.Helper()
	if err := d.Act(func(n *node.Node) error { return n.CommissioningWindow().OpenOwnWindow(context.Background(), 0) }); err != nil {
		t.Fatalf("enterCommissionableMode: %v", err)
	}
}

// openWindowRequest is OpenCommissioningWindow as sendOpenCommissioningWindow
// sends it (CommissioningWindowLifetimeTest.ts:36-53): a 180 s timeout, the
// PAKE verifier of passcode 20202021 over a random 32-byte salt at the
// minimum iterations, discriminator 1234.
type openWindowRequest struct {
	timeout    uint16
	verifier   []byte
	iterations uint32
	salt       []byte
}

func newOpenWindowRequest(t *testing.T) openWindowRequest {
	t.Helper()
	const iterations = 1000 // CRYPTO_PBKDF_ITERATIONS_MIN
	salt := make([]byte, 32)
	_, _ = rand.Read(salt)
	vc, err := spake2.NewVerifierContext(20202021, salt, iterations)
	if err != nil {
		t.Fatal(err)
	}
	l, err := vc.L.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	verifier := make([]byte, spake2.VerifierW0Size, spake2.VerifierW0Size+len(l))
	vc.W0.FillBytes(verifier)
	verifier = append(verifier, l...)
	return openWindowRequest{timeout: 180, verifier: verifier, iterations: iterations, salt: salt}
}

func (r openWindowRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r.timeout))
	enc.PutOctets(tlv.ContextTag(1), r.verifier)
	enc.PutUint(tlv.ContextTag(2), 1234)
	enc.PutUint(tlv.ContextTag(3), uint64(r.iterations))
	enc.PutOctets(tlv.ContextTag(4), r.salt)
	_ = enc.EndContainer()
}

// openCommissioningWindowPath is AdministratorCommissioning
// OpenCommissioningWindow on the root.
var openCommissioningWindowPath = im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003C, Command: 0x00}

// openCommissioningWindow is the test's openCommissioningWindow: the
// administrator's enhanced window over the wire, a timed invoke.
func openCommissioningWindow(t *testing.T, p *fabrictest.Pair) error {
	t.Helper()
	_, err := fabrictest.InvokeTimed(parityCtx(t), p, openCommissioningWindowPath, newOpenWindowRequest(t))
	return err
}

// isBusy is expectBusy (:59-62): FAILURE with the cluster status Busy (2).
func isBusy(err error) bool {
	var se *interaction.StatusError
	return errors.As(err, &se) && se.Status == im.StatusFailure && se.ClusterStatus != nil && *se.ClusterStatus == 0x02
}

// TestCommissioningWindowLifetime ports the describe "Commissioning window
// lifetime" (CommissioningWindowLifetimeTest.ts:73-331) where the earlier
// ports and the missing cases leave it.
func TestCommissioningWindowLifetime(t *testing.T) {
	holdSite(t)

	t.Run("restarts a window the node opened itself when the node opens it again", func(t *testing.T) {
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		openOwnWindow(t, p.Device)
		site.Fake().Advance(10 * time.Minute)
		openOwnWindow(t, p.Device)

		site.Fake().Advance(15*time.Minute - time.Second)
		if got := windowStatusOf(p.Device); got != wire.WindowStatusBasic {
			t.Errorf("15 min - 1 s after the restart: %v, want BasicWindowOpen", got)
		}
		site.Fake().Advance(2 * time.Second)
		if got := windowStatusOf(p.Device); got != wire.WindowStatusClosed {
			t.Errorf("15 min + 1 s after the restart: %v, want WindowNotOpen", got)
		}
	})

	t.Run("keeps a window the node restarted while the timer of the one it replaced fired", func(t *testing.T) {
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		openOwnWindow(t, p.Device)
		site.Fake().Advance(15*time.Minute - 50*time.Millisecond)

		// The restart runs while the clock passes the first window's
		// deadline.
		restarted := make(chan error, 1)
		go func() {
			restarted <- p.Device.Act(func(n *node.Node) error { return n.CommissioningWindow().OpenOwnWindow(context.Background(), 0) })
		}()
		site.Fake().Advance(100 * time.Millisecond)
		if err := <-restarted; err != nil {
			t.Fatalf("restart: %v", err)
		}

		if got := windowStatusOf(p.Device); got != wire.WindowStatusBasic {
			t.Errorf("after the restart: %v, want BasicWindowOpen", got)
		}
	})

	t.Run("answers Busy to a second window opened while the first is still opening", func(t *testing.T) {
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		openOwnWindow(t, p.Device)

		var (
			wg      sync.WaitGroup
			results [2]error
		)
		if err := site.Run(parityCtx(t), func(ctx context.Context) error {
			for i := range results {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, results[i] = p.Interaction.Invoke(ctx, openCommissioningWindowPath, newOpenWindowRequest(t), interaction.InvokeOptions{Timed: true})
				}()
			}
			wg.Wait()
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		var ok, busy int
		for _, err := range results {
			switch {
			case err == nil:
				ok++
			case isBusy(err):
				busy++
			default:
				t.Errorf("an open answered %v", err)
			}
		}
		if ok != 1 || busy != 1 {
			t.Errorf("outcomes %v: %d opened, %d Busy; want one each", results, ok, busy)
		}
	})

	t.Run("lets a close callback open the next window", func(t *testing.T) {
		site := quietSite(t)
		var (
			dev     *fabrictest.Device
			mu      sync.Mutex
			reopens int
		)
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
			// The window's onClose: the first close opens the next window
			// (endCommissioning, then allowBasicCommissioning).
			cfg.Observers.OnWindowTransition = func(s node.WindowSnapshot) error {
				mu.Lock()
				armed := dev != nil
				mu.Unlock()
				if !armed || s.Open {
					return nil
				}
				mu.Lock()
				first := reopens == 0
				if first {
					reopens++
				}
				mu.Unlock()
				if !first {
					return nil
				}
				return dev.Node().CommissioningWindow().OpenOwnWindow(context.Background(), 0)
			}
		}}})
		w := p.Device.Node().CommissioningWindow()
		if err := w.OpenOwnWindow(context.Background(), 3*time.Minute); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		dev = p.Device
		mu.Unlock()

		w.EndCommissioning()

		mu.Lock()
		got := reopens
		mu.Unlock()
		if got != 1 {
			t.Errorf("the close callback ran %d times, want once", got)
		}
		if got := windowStatusOf(p.Device); got != wire.WindowStatusBasic {
			t.Errorf("after the close: %v, want BasicWindowOpen (the callback's window)", got)
		}
	})

	t.Run("keeps an administrator's window when the node tries to open its own", func(t *testing.T) {
		p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
		if err := openCommissioningWindow(t, p); err != nil {
			t.Fatalf("OpenCommissioningWindow: %v", err)
		}

		err := p.Device.Act(func(n *node.Node) error { return n.CommissioningWindow().OpenOwnWindow(context.Background(), 0) })

		if !errors.Is(err, bridge.ErrAdministratorWindowOpen) {
			t.Errorf("the node's own open = %v, want ErrAdministratorWindowOpen", err)
		}
		if got := windowStatusOf(p.Device); got != wire.WindowStatusEnhanced {
			t.Errorf("window %v, want EnhancedWindowOpen", got)
		}
	})
}
