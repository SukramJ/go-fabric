// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The node's BLE radio follows matter.js's uncommissioned mode
// (ServerNetworkRuntime.ts:255-275, :294-308 at db021da0): BLE while the
// node has no fabric, off once it has one — at start, and for any window
// an administrator or the host opens — and back with the own window when
// the last fabric is removed.

func bleCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// radioState waits, moving the site's clock, until r is on the radio
// (serving and advertising) or off it, as on says.
func radioState(t *testing.T, site *fabrictest.Site, r *bletest.Radio, on bool, what string) {
	t.Helper()
	if err := site.Eventually(bleCtx(t), what, func() bool {
		serving, _ := r.Serving()
		_, advertising := r.Advertising()
		return serving == on && advertising == on
	}); err != nil {
		serving, _ := r.Serving()
		_, advertising := r.Advertising()
		t.Fatalf("%v: serving %v, advertising %v, want %v", err, serving, advertising, on)
	}
}

func TestNodeBLEFollowsCommissioning(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	pair := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{BLE: true}})
	dev := pair.Device
	r := dev.BLE().Radio()

	// Uncommissioned: the own window is on BLE, the configured payload.
	radioState(t, site, r, true, "the uncommissioned node on BLE")
	ads, _ := r.Advertisements()
	if len(ads) == 0 {
		t.Fatal("no advertisement")
	}
	if got := dev.BLE().ServiceData(); len(got) != 8 || got[1] != byte(dev.Discriminator()) || got[2] != byte(dev.Discriminator()>>8) {
		t.Fatalf("service data %x, want discriminator %d", got, dev.Discriminator())
	}

	// CommissioningComplete (here over IP) takes the node off the radio.
	cn, err := pair.Controller.CommissionNode(bleCtx(t), dev, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	radioState(t, site, r, false, "the commissioned node off BLE")
	adsAfter, stops := r.Advertisements()

	// A commissioned node never touches the radio: not at start ...
	if err := dev.Restart(bleCtx(t)); err != nil {
		t.Fatal(err)
	}
	// ... not for an administrator's window ...
	if err := site.Eventually(bleCtx(t), "the default subscription", cn.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(bleCtx(t), func(ctx context.Context) error {
		_, err := cn.OpenCommissioningWindow(ctx, 3*time.Minute, client.WindowOptions{})
		return err
	}); err != nil {
		t.Fatalf("OpenCommissioningWindow: %v", err)
	}
	if !dev.Node().Window().Open {
		t.Fatal("the administrator's window is not open")
	}
	// ... nor for the own window the host opens.
	if err := dev.Act(func(n *node.Node) error {
		if err := n.CloseWindow(context.Background()); err != nil {
			return err
		}
		return n.OpenOwnWindow(context.Background(), 3*time.Minute)
	}); err != nil {
		t.Fatal(err)
	}
	// The window's transition hook runs inside OpenOwnWindow: nothing
	// that would start BLE is still pending.
	if serving, _ := r.Serving(); serving {
		t.Fatal("the commissioned node serves on the radio")
	}
	if ads, s := r.Advertisements(); len(ads) != len(adsAfter) || s != stops {
		t.Fatalf("the commissioned node advertised: %d advertisements, %d stops (was %d, %d)", len(ads), s, len(adsAfter), stops)
	}

	// Removing the last fabric brings the own window back on BLE. The
	// removal flushes the fabric's subscription and waits for the
	// controller's StatusResponse on the site's clock, so it runs under
	// site.Run, which moves that clock.
	if err := site.Run(bleCtx(t), func(ctx context.Context) error {
		return dev.Act(func(n *node.Node) error {
			if err := n.CloseWindow(ctx); err != nil {
				return err
			}
			return n.RemoveFabric(ctx, 1)
		})
	}); err != nil {
		t.Fatal(err)
	}
	radioState(t, site, r, true, "the decommissioned node on BLE")
}

// TestNodeSetupPayloadBLEBit: the QR code names BLE discovery beside the
// IP network only for a node with a radio (matter.js NetworkServer.ts:36-51,
// CommissioningServer.ts:430-443).
func TestNodeSetupPayloadBLEBit(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	for _, tc := range []struct {
		ble  bool
		want setup.DiscoveryCaps
	}{
		{false, setup.DiscoveryOnIP},
		{true, setup.DiscoveryOnIP | setup.DiscoveryBLE},
	} {
		dev := site.AddDevice(fabrictest.Topology{BLE: tc.ble})
		sp, err := dev.Node().SetupPayload()
		if err != nil {
			t.Fatal(err)
		}
		qr, _, err := dev.Node().PairingCodesFor(node.WindowCredentials{Passcode: 20202021, Discriminator: 1234})
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []string{sp.QR, qr} {
			p, err := setup.DecodeQRCode(code)
			if err != nil {
				t.Fatal(err)
			}
			if p.DiscoveryCaps != tc.want {
				t.Fatalf("BLE %v: discovery capabilities %#x, want %#x", tc.ble, p.DiscoveryCaps, tc.want)
			}
		}
	}
}

// joinWiFi is a Wi-Fi driver that joins every network, or none (fail).
type joinWiFi struct{ fail bool }

func (joinWiFi) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w joinWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	if w.fail {
		return 1, "no such network", 1 // OutOfRange
	}
	return 0, "", 0
}

// bleSite is a site with one BLE device on Wi-Fi commissioning and a
// controller.
func bleSite(t *testing.T, drv joinWiFi) (*fabrictest.Site, *fabrictest.Device, *fabrictest.Controller) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	c := site.AddController(fabrictest.ControllerOptions{})
	dev := site.AddDevice(fabrictest.Topology{BLE: true, Configure: func(cfg *node.Config) {
		fabrictest.UseWiFiCommissioning(cfg, drv)
	}})
	return site, dev, c
}

// commissionOverBLE commissions dev over a BTP channel to its radio — BLE
// only, no race with the IP network — calling progress on every step.
func commissionOverBLE(t *testing.T, site *fabrictest.Site, dev *fabrictest.Device, c *fabrictest.Controller, progress func(commissioner.Step)) error {
	t.Helper()
	var ch *ble.Channel
	if err := site.Run(bleCtx(t), func(ctx context.Context) (err error) {
		ch, err = ble.Dial(ctx, bletest.NewCentral(dev.BLE()), ble.Peripheral{Address: dev.BLE().Address}, ble.DialOptions{})
		return err
	}); err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, err := c.Commission(bleCtx(t), dev, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
		o.Addresses, o.Channel = nil, ch
		o.WiFiNetwork = &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"}
		o.OnAttestationFailure = attestation.Accept()
		o.Progress = progress
	}})
	return err
}

// closeWindowAfterPASE is a commissioning progress hook that closes the
// device's window once PASE over BLE succeeded — the transition the own
// window's timer runs (Node.CloseWindow and the timer both end it through
// CommissioningWindow.closeLocked, without touching the fail-safe) — and
// records the radio right after.
type closeWindowAfterPASE struct {
	dev         *fabrictest.Device
	mu          sync.Mutex
	closed      bool
	serving     bool
	advertising bool
	connections int
}

func (h *closeWindowAfterPASE) progress(s commissioner.Step) {
	if s.Name != commissioner.StepPASE || s.Phase != commissioner.StepSucceeded {
		return
	}
	_ = h.dev.Node().CloseWindow(context.Background())
	r := h.dev.BLE().Radio()
	serving, conns := r.Serving()
	_, advertising := r.Advertising()
	h.mu.Lock()
	h.closed, h.serving, h.advertising, h.connections = true, serving, advertising, conns
	h.mu.Unlock()
}

// TestNodeBLEWindowCloseKeepsTheLink: the window's end stops only the
// advertisement (matter.js BleAdvertisement.ts:69-71 at db021da0); the
// GATT service and the central stay, so a commissioning over BLE whose
// window closed after PASE completes over the same link, and only its
// commit takes the node off the radio (ServerNetworkRuntime.ts:294-308,
// CommissioningServer.ts:118-127).
func TestNodeBLEWindowCloseKeepsTheLink(t *testing.T) {
	site, dev, c := bleSite(t, joinWiFi{})
	r := dev.BLE().Radio()
	radioState(t, site, r, true, "the uncommissioned node on BLE")
	h := &closeWindowAfterPASE{dev: dev}
	if err := commissionOverBLE(t, site, dev, c, h.progress); err != nil {
		t.Fatalf("commission over BLE with the window closed after PASE: %v", err)
	}
	h.mu.Lock()
	closed, serving, advertising, conns := h.closed, h.serving, h.advertising, h.connections
	h.mu.Unlock()
	if !closed || !serving || advertising || conns != 1 {
		t.Fatalf("after the window closed: closed %v, serving %v, advertising %v, connections %d; want the link kept, the advertisement stopped", closed, serving, advertising, conns)
	}
	radioState(t, site, r, false, "the commissioned node off BLE")
}

// TestNodeBLEReopenedWindowAdvertisesAgain: after the window's end the
// service stays registered and a central stays connected; a new own window
// advertises again on the same service.
func TestNodeBLEReopenedWindowAdvertisesAgain(t *testing.T) {
	site, dev, _ := bleSite(t, joinWiFi{})
	r := dev.BLE().Radio()
	radioState(t, site, r, true, "the uncommissioned node on BLE")
	conn, err := bletest.NewCentral(dev.BLE()).Connect(bleCtx(t), ble.Peripheral{Address: dev.BLE().Address})
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Act(func(n *node.Node) error { return n.CloseWindow(context.Background()) }); err != nil {
		t.Fatal(err)
	}
	serving, conns := r.Serving()
	if _, advertising := r.Advertising(); advertising || !serving || conns != 1 {
		t.Fatalf("after the close: serving %v, connections %d, advertising %v", serving, conns, advertising)
	}
	ads, _ := r.Advertisements()
	if err := dev.Act(func(n *node.Node) error { return n.OpenOwnWindow(context.Background(), 3*time.Minute) }); err != nil {
		t.Fatal(err)
	}
	radioState(t, site, r, true, "the reopened window on BLE")
	if again, _ := r.Advertisements(); len(again) <= len(ads) {
		t.Fatal("the reopened window advertised nothing new")
	}
	select {
	case <-conn.Disconnected():
		t.Fatal("the central was dropped: the service was registered again")
	default:
	}
}

// TestNodeBLEFailSafeExpiryAfterWindowClose: a commissioning over BLE that
// fails after the window closed rolls its fabric back; the node stays
// uncommissioned with the window closed, the service registered and
// nothing advertised over BLE — matter.js reopens nothing for a fabric only
// the fail-safe added (CommissioningServer.ts:118-165 at db021da0) — until
// a window opens.
func TestNodeBLEFailSafeExpiryAfterWindowClose(t *testing.T) {
	site, dev, c := bleSite(t, joinWiFi{fail: true})
	r := dev.BLE().Radio()
	radioState(t, site, r, true, "the uncommissioned node on BLE")
	h := &closeWindowAfterPASE{dev: dev}
	if err := commissionOverBLE(t, site, dev, c, h.progress); err == nil {
		t.Fatal("a commissioning whose network does not join succeeded")
	}
	h.mu.Lock()
	closed, conns := h.closed, h.connections
	h.mu.Unlock()
	if !closed || conns != 1 {
		t.Fatalf("the window was not closed over the BLE link: closed %v, connections %d", closed, conns)
	}
	if err := site.Eventually(bleCtx(t), "the fail-safe ended", func() bool {
		return !dev.Node().Root().GeneralCommissioning.FailSafeArmed()
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(bleCtx(t), "the fabric rolled back", func() bool {
		infos, err := dev.Node().FabricInfos(context.Background())
		return err == nil && len(infos) == 0
	}); err != nil {
		t.Fatal(err)
	}
	// The rollback decommissions nothing (CommissioningServer.ts:148-152 at
	// db021da0): no reopen follows once the node is idle. Time moves well
	// past the node's idle poll before the state is read.
	if err := site.Eventually(bleCtx(t), "no session left", func() bool {
		return len(dev.Node().SessionDiagnostics().Sessions) == 0
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Step(bleCtx(t), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	serving, _ := r.Serving()
	_, advertising := r.Advertising()
	if !serving || advertising || dev.Node().Window().Open {
		t.Fatalf("serving %v, advertising %v, window open %v: want registered and silent", serving, advertising, dev.Node().Window().Open)
	}
}
