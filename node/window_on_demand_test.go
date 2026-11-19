// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// onDemandNode is an uncommissioned node with Commissioning.WindowOnDemand,
// advertising on adv, started.
func onDemandNode(t *testing.T, adv *mdns.Noop) (*node.Node, node.Config) {
	t.Helper()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	cfg.Commissioning.WindowOnDemand = true
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return n, cfg
}

// assertNoWindow fails when the node has a window open, a commissionable
// record published or a PASE acceptor answering.
func assertNoWindow(t *testing.T, n *node.Node, adv *mdns.Noop, when string) {
	t.Helper()
	if w := n.Window(); w.Open {
		t.Fatalf("%s: window %+v, want none", when, w)
	}
	if _, _, ok := commissionableRecord(adv); ok {
		t.Fatalf("%s: a commissionable record is published", when)
	}
	if pbkdfParamRequestAnswered(t, n.Bridge().LocalAddr()) {
		t.Fatalf("%s: a PBKDFParamRequest earned a PBKDFParamResponse", when)
	}
}

// TestWindowOnDemandStartOpensNothing (R2-P5-apinode-01, decision 96): an
// uncommissioned node with WindowOnDemand starts with no window, no
// commissionable record and no PASE acceptor, and opens its own window on
// the host's OpenOwnWindow — matter.js `commissioning.enabled: false`
// (CommissioningServer.ts:266-273 at b25ed960).
func TestWindowOnDemandStartOpensNothing(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n, _ := onDemandNode(t, adv)
	assertNoWindow(t, n, adv, "after Start")
	if svcs := n.MDNSServices(); len(svcs) != 0 {
		for _, s := range svcs {
			if s.ServiceType == mdns.ServiceTypeCommissionable {
				t.Fatalf("MDNSServices lists a %s record after Start", s.ServiceType)
			}
		}
	}

	if err := n.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatalf("OpenOwnWindow: %v", err)
	}
	if w := n.Window(); !w.Open || w.Kind != node.WindowOwn {
		t.Fatalf("after OpenOwnWindow: %+v, want the own window", w)
	}
	waitRecord(t, adv, "after OpenOwnWindow", func(d, mode uint64, ok bool) bool { return ok && mode == 1 && d == 3840 })
	if !pbkdfParamRequestAnswered(t, n.Bridge().LocalAddr()) {
		t.Fatal("the own window the host opened answers no PBKDFParamRequest")
	}
}

// TestWindowOnDemandStaysClosed: none of the three later sites that open
// the own window by themselves opens it with WindowOnDemand — a basic
// window that closes, a lifted block, and the last fabric's removal.
func TestWindowOnDemandStaysClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("basic window closed", func(t *testing.T) {
		t.Parallel()
		adv := mdns.NewNoop()
		n, _ := onDemandNode(t, adv)
		if err := n.OpenBasicWindow(ctx, randomCredentials(t, time.Minute)); err != nil {
			t.Fatalf("OpenBasicWindow: %v", err)
		}
		if err := n.CloseWindow(ctx); err != nil {
			t.Fatalf("CloseWindow: %v", err)
		}
		assertNoWindow(t, n, adv, "after the basic window closed")
	})

	t.Run("block lifted", func(t *testing.T) {
		t.Parallel()
		adv := mdns.NewNoop()
		n, _ := onDemandNode(t, adv)
		n.SetCommissioningBlocked(ctx, errChainNotLoaded)
		n.SetCommissioningBlocked(ctx, nil)
		assertNoWindow(t, n, adv, "after the block was lifted")
	})

	t.Run("last fabric removed", func(t *testing.T) {
		t.Parallel()
		adv := mdns.NewNoop()
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.Advertiser = adv
		cfg.Commissioning.WindowOnDemand = true
		n := newNode(t, cfg)
		if err := n.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := n.OpenOwnWindow(ctx, 0); err != nil {
			t.Fatalf("OpenOwnWindow: %v", err)
		}
		c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
		if _, err := c.Commission(ctx); err != nil {
			t.Fatalf("Commission: %v", err)
		}
		if err := n.RemoveFabric(ctx, c.FabricIndex()); err != nil {
			t.Fatalf("RemoveFabric: %v", err)
		}
		// The removal's pending reopen runs once the node is idle; it
		// decides and ends.
		waitLive(t, "the pending reopen to end", func() bool { return !node.ReopenPending(n) })
		assertNoWindow(t, n, adv, "after the last fabric was removed")
	})
}

// TestWindowCallsBeforeStartAreRefused (R2-P5-apinode-06): before Start the
// three window calls return ErrNotStarted and open nothing.
func TestWindowCallsBeforeStartAreRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if err := n.OpenOwnWindow(ctx, 0); !errors.Is(err, node.ErrNotStarted) {
		t.Errorf("OpenOwnWindow before Start: %v, want ErrNotStarted", err)
	}
	if err := n.OpenBasicWindow(ctx, randomCredentials(t, time.Minute)); !errors.Is(err, node.ErrNotStarted) {
		t.Errorf("OpenBasicWindow before Start: %v, want ErrNotStarted", err)
	}
	if err := n.CloseWindow(ctx); !errors.Is(err, node.ErrNotStarted) {
		t.Errorf("CloseWindow before Start: %v, want ErrNotStarted", err)
	}
	if w := n.Window(); w.Open {
		t.Errorf("a window is open before Start: %+v", w)
	}
}

// TestSetupPayloadStatesTheFlow (R2-P5-apinode-02): the QR code carries
// Commissioning.Flow, and a flow other than Standard takes the long manual
// code with the vendor and product id (CommissioningServer.ts:438,
// :446-454 at db021da0).
func TestSetupPayloadStatesTheFlow(t *testing.T) {
	t.Parallel()
	for _, flow := range []node.CommissioningFlow{node.FlowStandard, node.FlowUserIntent, node.FlowCustom} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.Commissioning.Flow = flow
		n := newNode(t, cfg)
		sp, err := n.SetupPayload()
		if err != nil {
			t.Fatalf("flow %d: SetupPayload: %v", flow, err)
		}
		if sp.Flow != flow {
			t.Errorf("flow %d: SetupPayload.Flow = %d", flow, sp.Flow)
		}
		p, err := setup.DecodeQRCode(sp.QR)
		if err != nil {
			t.Fatalf("flow %d: DecodeQRCode(%q): %v", flow, sp.QR, err)
		}
		if p.CustomFlow != uint8(flow) || p.VendorID != 0xFFF1 || p.ProductID != 0x8001 {
			t.Errorf("flow %d: QR payload %+v", flow, p)
		}
		var want string
		if flow == node.FlowStandard {
			want, err = setup.ManualCode(3840, testPasscode)
		} else {
			want, err = setup.ManualCodeLong(3840, testPasscode, 0xFFF1, 0x8001)
		}
		if err != nil {
			t.Fatal(err)
		}
		if sp.Manual != want {
			t.Errorf("flow %d: manual code %q, want %q", flow, sp.Manual, want)
		}
		qr, manual, err := n.PairingCodesFor(randomCredentials(t, 0))
		if err != nil {
			t.Fatal(err)
		}
		if p, err := setup.DecodeQRCode(qr); err != nil || p.CustomFlow != uint8(flow) {
			t.Errorf("flow %d: PairingCodesFor QR %+v (%v)", flow, p, err)
		}
		if long := len(strings.ReplaceAll(manual, "-", "")) == 21; long != (flow != node.FlowStandard) {
			t.Errorf("flow %d: PairingCodesFor manual code %q, long form %v", flow, manual, long)
		}
	}
}

// TestConfigRefusesUnknownFlowAndBadHints: Validate refuses a flow above
// FlowCustom and a pairing hint without the instruction it needs.
func TestConfigRefusesUnknownFlowAndBadHints(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: "unused.db"}
	cfg.Commissioning.Flow = node.FlowCustom + 1
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Errorf("Flow %d: %v, want ErrInvalidConfig", cfg.Commissioning.Flow, err)
	}
	cfg.Commissioning.Flow = node.FlowStandard
	cfg.Commissioning.PairingHints = mdns.PairingHintPressSetupButtonNumberOfTimes
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) || !errors.Is(err, mdns.ErrInvalidService) {
		t.Errorf("a hint that needs an instruction without one: %v, want ErrInvalidConfig wrapping mdns.ErrInvalidService", err)
	}
	cfg.Commissioning.PairingInstruction = "3"
	if err := cfg.Validate(); err != nil {
		t.Errorf("a hint with its instruction: %v", err)
	}
}

// TestPairingHintsAdvertised (R2-P5-apinode-03): the facade's hint and
// instruction reach the commissionable record, a bit above 15 included.
func TestPairingHintsAdvertised(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	cfg.Commissioning.PairingHints = mdns.PairingHintPowerCycleUntilIndication | mdns.PairingHintPressSetupButtonNumberOfTimes
	cfg.Commissioning.PairingInstruction = "5"
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitRecord(t, adv, "the own window", func(_, mode uint64, ok bool) bool { return ok && mode == 1 })
	var ph, pi string
	for _, s := range adv.Active() {
		if s.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		for _, txt := range s.TXT {
			switch txt.Key {
			case "PH":
				ph = txt.Value
			case "PI":
				pi = txt.Value
			}
		}
	}
	if want := "4718592"; ph != want || pi != "5" { // 1<<22 | 1<<19
		t.Errorf("PH=%q PI=%q, want PH=%s PI=5", ph, pi, want)
	}
}
