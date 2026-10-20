// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/commissioning/CommissioningWindowTest.ts
// at 862ad75f.
//
// matter.js's helpers are CommissioningClient's openEnhancedCommissioningWindow
// and openBasicCommissioningWindow on the controller's peer; the module's
// are the reference controller's openEnhancedWindow and openBasicWindow
// (window.go), which mirror them over a client.Node. The pair is a
// fabrictest device commissioned through the controller's registry
// (MockSite.addCommissionedPair with subscribedPeer). matter.js spies on
// the OpenCommissioningWindow request; the Go observables are the device's
// window (the bridge's CommissioningWindow: open, the discriminator it
// advertises, a supplied verifier) and a second controller commissioning
// the device with the returned passcode through it, which holds only when
// the verifier sent was derived from that passcode. The Basic cases need a
// device with the Basic feature, which a node cannot serve (the register
// names why).
package cli

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/tlv"
)

// windowPair is addCommissionedPair with subscribedPeer.
type windowPair struct {
	site *fabrictest.Site
	pair *fabrictest.Pair
	node *client.Node
}

func newWindowPair(t *testing.T) *windowPair {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	wp := &windowPair{site: site, pair: site.UncommissionedPair(fabrictest.PairOptions{})}
	n, err := wp.pair.Controller.CommissionNode(windowCtx(t), wp.pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wp.node = n
	if err := site.Eventually(windowCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	return wp
}

func windowCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// openEnhanced runs openEnhancedWindow on the site.
func (wp *windowPair) openEnhanced(t *testing.T, timeout uint16) enhancedWindow {
	t.Helper()
	var w enhancedWindow
	if err := wp.site.Run(windowCtx(t), func(ctx context.Context) error {
		var err error
		w, err = openEnhancedWindow(ctx, wp.node, timeout)
		return err
	}); err != nil {
		t.Fatalf("openEnhancedWindow: %v", err)
	}
	return w
}

// adminFabric is the device window's AdminFabricIndex, false for null.
func (wp *windowPair) adminFabric() (uint8, bool) {
	snap := wp.pair.Device.Bridge().CommissioningWindow().CurrentWindow()
	return snap.AdminFabricIndex, !snap.AdminFabricIsNull
}

// deviceFabric is the device's fabric index of the fabric whose node id
// on the device is nodeID.
func (wp *windowPair) deviceFabric(t *testing.T, nodeID uint64) uint8 {
	t.Helper()
	fabrics, err := wp.pair.Device.Node().Fabrics(windowCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fabrics {
		if f.NodeID == nodeID {
			return f.FabricIndex
		}
	}
	t.Fatalf("the device holds no fabric with node id %d", nodeID)
	return 0
}

// removeFabric is OperationalCredentials RemoveFabric {[0] FabricIndex}.
type removeFabric uint8

func (r removeFabric) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r))
	_ = enc.EndContainer()
}

// TestCommissioningWindowParityMatterJS ports the describe
// "CommissioningClient commissioning-window helpers"
// (CommissioningWindowTest.ts:65-209).
func TestCommissioningWindowParityMatterJS(t *testing.T) {
	// :71-95.
	t.Run("opens a window with a generated verifier and returns pairing codes", func(t *testing.T) {
		wp := newWindowPair(t)
		w := wp.openEnhanced(t, uint16(defaultWindowTimeout/time.Second))
		if w.manual == "" || w.qr == "" {
			t.Fatalf("pairing codes %q / %q", w.manual, w.qr)
		}
		win := wp.pair.Device.Bridge().CommissioningWindow()
		if !win.IsOpen() || !win.HasSuppliedVerifier() {
			t.Fatalf("window open %v, supplied verifier %v", win.IsOpen(), win.HasSuppliedVerifier())
		}
		qr, err := setup.DecodeQRCode(w.qr)
		if err != nil {
			t.Fatal(err)
		}
		if qr.Discriminator != win.Discriminator() {
			t.Errorf("QR discriminator %d, the window advertises %d", qr.Discriminator, win.Discriminator())
		}
	})

	// :97-130. The helper takes whole seconds (the CLI's --timeout), so
	// Seconds(300.5) arrives as 300; the verifier is proven by a second
	// administrator commissioning with the returned passcode.
	t.Run("returns the values it sent to the device and encoded in the pairing codes", func(t *testing.T) {
		wp := newWindowPair(t)
		w := wp.openEnhanced(t, 300)
		win := wp.pair.Device.Bridge().CommissioningWindow()
		if w.discriminator != win.Discriminator() {
			t.Errorf("discriminator %d, the window advertises %d", w.discriminator, win.Discriminator())
		}
		if got := win.RequestedDurationSeconds(); got != 300 {
			t.Errorf("commissioningTimeout %d s, want 300", got)
		}
		qr, err := setup.DecodeQRCode(w.qr)
		if err != nil {
			t.Fatal(err)
		}
		manual, err := setup.DecodeManualCode(w.manual)
		if err != nil {
			t.Fatal(err)
		}
		if qr.Passcode != w.passcode || qr.Discriminator != w.discriminator || manual.Passcode != w.passcode {
			t.Errorf("QR %+v, manual %+v, sent passcode %d discriminator %d", qr, manual, w.passcode, w.discriminator)
		}
		// BasicInformation's vendor and product of the fabrictest device.
		if qr.VendorID != 0xFFF1 || qr.ProductID != 0x8001 {
			t.Errorf("QR vendor 0x%04X product 0x%04X, want BasicInformation's 0xFFF1 / 0x8001", qr.VendorID, qr.ProductID)
		}
		second := wp.site.AddController(fabrictest.ControllerOptions{})
		if _, err := second.Commission(windowCtx(t), wp.pair.Device, fabrictest.CommissionOptions{
			Edit: func(o *commissioner.Options) { o.Passcode = w.passcode },
		}); err != nil {
			t.Fatalf("commissioning with the returned passcode: %v", err)
		}
	})

	// :132-142. No window was ever open: the pre-emptive revoke meets
	// WindowNotOpen.
	t.Run("tolerates a WindowNotOpen error on the pre-emptive revoke (enhanced)", func(t *testing.T) {
		wp := newWindowPair(t)
		wp.openEnhanced(t, uint16(defaultWindowTimeout/time.Second))
	})

	// :144-150. matter.js throws ImplementationError; the Go helper
	// refuses with an error before sending anything.
	t.Run("throws ImplementationError when the peer does not support the basic feature", func(t *testing.T) {
		wp := newWindowPair(t)
		err := wp.site.Run(windowCtx(t), func(ctx context.Context) error {
			return openBasicWindow(ctx, wp.node, uint16(defaultWindowTimeout/time.Second))
		})
		if err == nil {
			t.Fatal("openBasicWindow on a device without the Basic feature succeeded")
		}
		if wp.pair.Device.Bridge().CommissioningWindow().IsOpen() {
			t.Error("a window opened")
		}
	})

	// :187-197. matter.js replaces the admin fabric's Fabric object first
	// (FabricManager.replaceFabric), the regression being a removal
	// callback on the replaced object; the Go window keys AdminFabricIndex
	// on the index (CommissioningWindow.FabricRemoved), so the removal of
	// the admin fabric — the administrator decommissioning itself — is the
	// observable.
	t.Run("clears AdminFabricIndex when the admin fabric is removed after its Fabric object was replaced", func(t *testing.T) {
		wp := newWindowPair(t)
		wp.openEnhanced(t, uint16(defaultWindowTimeout/time.Second))
		addr, _ := wp.node.PeerAddress()
		admin := wp.deviceFabric(t, addr.NodeID)
		if got, ok := wp.adminFabric(); !ok || got != admin {
			t.Fatalf("AdminFabricIndex %d (%v), want %d", got, ok, admin)
		}
		_ = wp.site.Run(windowCtx(t), func(ctx context.Context) error {
			return wp.pair.Controller.Registry().Decommission(ctx, wp.node)
		})
		if err := wp.site.Eventually(windowCtx(t), "the admin fabric removed", func() bool {
			fabrics, err := wp.pair.Device.Node().Fabrics(windowCtx(t))
			return err == nil && len(fabrics) == 0
		}); err != nil {
			t.Fatal(err)
		}
		if got, ok := wp.adminFabric(); ok {
			t.Errorf("AdminFabricIndex %d, want null", got)
		}
		if !wp.pair.Device.Bridge().CommissioningWindow().IsOpen() {
			t.Error("the window closed with the admin fabric")
		}
	})

	// :199-207. The other fabric is a second administrator commissioned
	// through a first window (matter.js adds a TestFabric directly); the
	// window under test is opened afterwards, and the other administrator
	// removes its own fabric.
	t.Run("keeps AdminFabricIndex when another fabric is removed", func(t *testing.T) {
		wp := newWindowPair(t)
		first := wp.openEnhanced(t, uint16(defaultWindowTimeout/time.Second))
		other := wp.site.AddController(fabrictest.ControllerOptions{})
		op, err := other.Commission(windowCtx(t), wp.pair.Device, fabrictest.CommissionOptions{
			Edit: func(o *commissioner.Options) { o.Passcode = first.passcode },
		})
		if err != nil {
			t.Fatal(err)
		}
		wp.openEnhanced(t, uint16(defaultWindowTimeout/time.Second))
		addr, _ := wp.node.PeerAddress()
		admin := wp.deviceFabric(t, addr.NodeID)
		otherFabric := wp.deviceFabric(t, op.Address.NodeID)
		_, _ = fabrictest.Invoke(windowCtx(t), op, im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003E, Command: 0x0A}, removeFabric(otherFabric))
		if err := wp.site.Eventually(windowCtx(t), "the other fabric removed", func() bool {
			fabrics, err := wp.pair.Device.Node().Fabrics(windowCtx(t))
			return err == nil && len(fabrics) == 1
		}); err != nil {
			t.Fatal(err)
		}
		if got, ok := wp.adminFabric(); !ok || got != admin {
			t.Errorf("AdminFabricIndex %d (%v), want %d", got, ok, admin)
		}
	})
}
