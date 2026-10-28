// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

// The main module's BLE tests, run against this central: the same
// peripherals (controller/ble/bletest), reached through a fake proxy
// instead of the in-memory central. newCentral stands where
// bletest.NewCentral stands in controller/ble_test.go and
// controller/ble/channel_test.go; the bodies are theirs.

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/transport/btp"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"

	"github.com/SukramJ/go-fabric/ble/esphome"
	"github.com/SukramJ/go-fabric/ble/esphome/esphometest"
)

// modes are the two ways a central learns its proxy: given, or
// discovered over DNS-SD.
var modes = []struct {
	name     string
	discover bool
}{{"static", false}, {"discovered", true}}

// newCentral is a central over a fake proxy — Noise when key is set — in
// front of the peripherals, which it has seen once, so a Dial without a
// scan finds them as bletest's central does. With discover the proxy is
// not given but advertised on an in-memory DNS-SD network, its key given
// by its name.
func newCentral(t *testing.T, key string, discover bool, ps ...*bletest.Peripheral) ble.Central {
	t.Helper()
	f := fake(t, esphometest.Options{Key: key, Name: "commission-proxy"}, ps...)
	var c *esphome.Central
	if discover {
		mdnsNet := esphometest.NewNetwork()
		mdnsNet.Advertise(f)
		c = central(t, esphome.Options{Discover: &esphome.DiscoverOptions{
			Browser: names(t, mdnsNet), Keys: esphome.Keys{"commission-proxy": key}, Timeout: browseWindow,
		}})
	} else {
		c = central(t, esphome.Options{}, esphome.Proxy{Addr: f.Addr(), Key: key})
	}
	if len(ps) > 0 {
		s := watch(c)
		if err := c.StartScanning(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			eventually(t, "the advertisement of "+p.Address, func() bool { d, _ := s.get(p.Address); return d != nil })
		}
		_ = c.StopScanning(context.Background())
		c.SetDiscoveryCallback(nil)
	}
	return c
}

// wifiDriver is a Wi-Fi driver that joins every network it is given.
type wifiDriver struct {
	mu       sync.Mutex
	connects []string
}

func (w *wifiDriver) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w *wifiDriver) ConnectNetwork(_ context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connects = append(w.connects, string(ssid)+"/"+string(credentials))
	return 0, "", 0
}

// siteController is controller/site_test.go's.
func siteController(t *testing.T, topo fabrictest.Topology) (*fabrictest.Site, *fabrictest.Device, *controller.Controller) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(topo)
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, cfg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = site.Run(ctx, c.Close)
	})
	return site, dev, c
}

func manualCode(t *testing.T, dev *fabrictest.Device) string {
	t.Helper()
	code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// TestPairOverBLE is controller/ble_test.go's TestPairOverBLE through a
// proxy, plaintext and with Noise: PairOptions.BLE finds the Wi-Fi device by
// its advertisement, runs PASE over BTP, provisions the network and
// completes over CASE on the site's network.
func TestPairOverBLE(t *testing.T) {
	for _, mode := range modes {
		for _, key := range []string{"", testKey} {
			t.Run(mode.name+"/key="+strconv.FormatBool(key != ""), func(t *testing.T) { pairOverBLE(t, key, mode.discover) })
		}
	}
}

func pairOverBLE(t *testing.T, key string, discover bool) {
	t.Helper()
	drv := &wifiDriver{}
	site, dev, c := siteController(t, fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv}
	}})
	p := dev.BLE()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if _, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{BLE: newCentral(t, key, discover, p)}); !errors.Is(err, controller.ErrConfiguration) {
		t.Fatalf("BLE without a network: %v, want ErrConfiguration", err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			BLE: newCentral(t, key, discover, p), WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(),
		})
		return err
	}); err != nil {
		t.Fatalf("Pair over BLE (key %q): %v", key, err)
	}
	cancel()
	drv.mu.Lock()
	if len(drv.connects) != 1 || drv.connects[0] != "lab/secret12" {
		t.Fatalf("driver connects %q", drv.connects)
	}
	drv.mu.Unlock()
	if len(c.Nodes()) != 1 {
		t.Fatalf("%d nodes", len(c.Nodes()))
	}
}

// echo is controller/ble/channel_test.go's: a peripheral that answers
// every unsecured Matter message with one on the same exchange.
func echo(t *testing.T, got chan<- message.ProtocolHeader) *bletest.Peripheral {
	t.Helper()
	return &bletest.Peripheral{
		Address: "11:22:33:44:55:66", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001, ATTMTU: 100,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			var counter uint32 = 7
			return func(msg []byte) {
				hdr, hl, err := message.UnmarshalHeader(msg)
				if err != nil {
					t.Error(err)
					return
				}
				proto, pl, err := message.UnmarshalProtocolHeader(msg[hl:])
				if err != nil {
					t.Error(err)
					return
				}
				got <- proto
				counter++
				rh := message.Header{MessageCounter: counter, DestSize: message.DestNodeID, DestNodeID: hdr.SourceNodeID}
				rp := message.ProtocolHeader{NeedsAck: true, Opcode: mrp.SCOpcodePBKDFParamResponse, ExchangeID: proto.ExchangeID, ProtocolID: proto.ProtocolID}
				out := append(rh.Marshal(), rp.Marshal()...)
				_ = conn.Send(append(out, msg[hl+pl:]...))
			}, nil
		},
	}
}

// TestDialRunsExchangesWithoutMRP is controller/ble/channel_test.go's
// through a proxy: the segment size from the proxy's ATT_MTU, a message of
// several segments each way.
func TestDialRunsExchangesWithoutMRP(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) { dialRunsExchangesWithoutMRP(t, mode.discover) })
	}
}

func dialRunsExchangesWithoutMRP(t *testing.T, discover bool) {
	t.Helper()
	got := make(chan message.ProtocolHeader, 4)
	p := echo(t, got)
	central := newCentral(t, testKey, discover, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := ble.Dial(ctx, central, ble.Peripheral{Address: p.Address}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Peripheral().Address != p.Address || ch.Conn().Session().FragmentSize() != 97 || ch.PeerAddress() == nil {
		t.Fatalf("channel %v %d %v", ch.Peripheral(), ch.Conn().Session().FragmentSize(), ch.PeerAddress())
	}
	s, err := ch.Exchanges().NewUnsecuredSession(ch.PeerAddress(), nil)
	if err != nil {
		t.Fatal(err)
	}
	x, err := ch.Exchanges().Initiate(s, mrp.SecureChannelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 300) // several BTP segments
	payload[299] = 0x5a
	if err := x.Send(ctx, mrp.SCOpcodePBKDFParamRequest, payload, exchange.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if proto := <-got; proto.NeedsAck || proto.HasAck {
		t.Fatalf("request over BTP asks for MRP: %+v", proto)
	}
	msg, err := x.Next(ctx, exchange.ReceiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Opcode != mrp.SCOpcodePBKDFParamResponse || len(msg.Payload) != 300 || msg.Payload[299] != 0x5a || msg.RequiresAck {
		t.Fatalf("response %+v", msg)
	}
	if err := ch.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("not closed")
	}
	if !errors.Is(ch.ClosedCause(), btp.ErrDisconnected) {
		t.Fatal("the channel's close cause is no disconnect")
	}
}

// TestChannelClosesWhenPeripheralDrops is controller/ble/channel_test.go's
// through a proxy.
func TestChannelClosesWhenPeripheralDrops(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) { channelClosesWhenPeripheralDrops(t, mode.discover) })
	}
}

func channelClosesWhenPeripheralDrops(t *testing.T, discover bool) {
	t.Helper()
	got := make(chan message.ProtocolHeader, 4)
	p := echo(t, got)
	central := newCentral(t, "", discover, p)
	ch, err := ble.Dial(context.Background(), central, ble.Peripheral{Address: p.Address}, ble.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The peripheral's side ends the session: the central sees C2 go.
	_ = ch.Conn().Session().Close()
	select {
	case <-ch.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the channel did not close")
	}
}
