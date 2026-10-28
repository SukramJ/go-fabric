// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// testWiFi is the harness's Wi-Fi driver: every scan finds the SSID, every
// connect succeeds.
type testWiFi struct {
	mu       sync.Mutex
	connects []string
}

func (w *testWiFi) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w *testWiFi) ConnectNetwork(_ context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connects = append(w.connects, string(ssid)+"/"+string(credentials))
	return 0, "", 0
}

func TestPairBLEWithoutBLEOrNetwork(t *testing.T) {
	stderr = &bytes.Buffer{}
	db := t.TempDir() + "/x.db"
	if err := run([]string{"--db", db, "pair", "--ble", "--passcode", "20202021", "--discriminator", "3840", "--wifi-ssid", "lab"}, strings.NewReader(""), &bytes.Buffer{}); !errors.Is(err, errNoBLE) || !strings.Contains(err.Error(), "ble/bluez/cmd/reference-controller") {
		t.Fatalf("plain build: %v", err)
	}
	central := func(string) (ble.Central, error) { return bletest.NewCentral(), nil }
	err := Run([]string{"--db", db, "pair", "--ble", "--passcode", "20202021", "--discriminator", "3840"}, strings.NewReader(""), &bytes.Buffer{}, Options{Central: central})
	if !errors.Is(err, errBLENeedsNetwork) || !strings.Contains(err.Error(), "a device on BLE has no network yet") {
		t.Fatalf("without a network: %v", err)
	}
	refused := errors.New("adapter refused")
	err = Run([]string{"--db", db, "--listen", "127.0.0.1:0", "pair", "--ble", "--ble-adapter", "hci9", "--passcode", "20202021", "--discriminator", "3840", "--thread-dataset", "0e080000000000010000"}, strings.NewReader(""), &bytes.Buffer{},
		Options{Central: func(adapter string) (ble.Central, error) {
			if adapter != "hci9" {
				t.Errorf("adapter %q", adapter)
			}
			return nil, refused
		}})
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "ble adapter hci9") {
		t.Fatalf("adapter error: %v", err)
	}
}

// TestPairBluetoothProxyFlag: --bluetooth-proxy needs the ESPHome build,
// implies --ble, and hands every spec, in order, to Options.Proxies.
func TestPairBluetoothProxyFlag(t *testing.T) {
	stderr = &bytes.Buffer{}
	db := t.TempDir() + "/x.db"
	if err := run([]string{"--db", db, "pair", "--bluetooth-proxy", "p.local", "--passcode", "20202021", "--discriminator", "3840", "--wifi-ssid", "lab"}, strings.NewReader(""), &bytes.Buffer{}); !errors.Is(err, errNoProxies) || !strings.Contains(err.Error(), "ble/esphome/cmd/reference-controller") {
		t.Fatalf("plain build: %v", err)
	}
	refused := errors.New("proxy refused")
	var got ProxyFlags
	err := Run([]string{"--db", db, "--listen", "127.0.0.1:0", "pair", "--bluetooth-proxy", "a.local,KEY", "--bluetooth-proxy", "b.local:6054", "--passcode", "20202021", "--discriminator", "3840", "--thread-dataset", "0e080000000000010000"}, strings.NewReader(""), &bytes.Buffer{},
		Options{Proxies: func(flags ProxyFlags, _ *slog.Logger) (ble.Central, error) {
			got = flags
			return nil, refused
		}})
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "bluetooth proxies") || strings.Join(got.Proxies, " ") != "a.local,KEY b.local:6054" || got.Discover {
		t.Fatalf("proxies %+v: %v", got, err)
	}
	// Discovery with keys per device, which implies --ble as well.
	err = Run([]string{"--db", db, "--listen", "127.0.0.1:0", "pair", "--bluetooth-proxy-discover", "--bluetooth-proxy-key", "kitchen=K1", "--bluetooth-proxy-key", "AA:BB:CC:DD:EE:FF=K2", "--passcode", "20202021", "--discriminator", "3840", "--thread-dataset", "0e080000000000010000"}, strings.NewReader(""), &bytes.Buffer{},
		Options{Proxies: func(flags ProxyFlags, _ *slog.Logger) (ble.Central, error) {
			got = flags
			return nil, refused
		}})
	if !errors.Is(err, refused) || !got.Discover || len(got.Proxies) != 0 || strings.Join(got.Keys, " ") != "kitchen=K1 AA:BB:CC:DD:EE:FF=K2" {
		t.Fatalf("discovery %+v: %v", got, err)
	}
	// A key without discovery is refused before anything opens.
	if err := run([]string{"--db", db, "pair", "--bluetooth-proxy-key", "kitchen=K1", "--passcode", "20202021", "--discriminator", "3840", "--wifi-ssid", "lab"}, strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--bluetooth-proxy-discover") {
		t.Fatalf("key without discovery: %v", err)
	}
}

// TestPairOverBLE pairs the harness over an in-memory BTP pair: found by
// its advertisement, PASE over BTP, the Wi-Fi network steps, then CASE
// over IP found by DNS-SD, and a read over it.
func TestPairOverBLE(t *testing.T) {
	drv := &testWiFi{}
	h := startHarness(t, func(c *node.Config) { c.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv} })
	tl := newTool(t, h)
	p := &bletest.Peripheral{
		Address: "C0:FF:EE:00:0B:1E", Discriminator: harnessDiscriminator, VendorID: 0xFFF1, ProductID: 0x8001,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			srv, err := bridgetest.ServeConn(h.n.Bridge(), conn)
			if err != nil {
				t.Error(err)
				return nil, nil
			}
			return srv.Deliver, srv.Close
		},
	}
	opts := Options{Central: func(string) (ble.Central, error) { return bletest.NewCentral(p), nil }}
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(tl.args("pair", "--ble", "--code", manual, "--node-id", "0x4321", "--wifi-ssid", "lab", "--wifi-credentials", "secret12"), strings.NewReader(""), &out, opts); err != nil {
		t.Fatalf("pair --ble: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "paired node 17185") {
		t.Fatalf("pair --ble:\n%s", out.String())
	}
	drv.mu.Lock()
	connects := drv.connects
	drv.mu.Unlock()
	if len(connects) != 1 || connects[0] != "lab/secret12" {
		t.Fatalf("driver connects %q", connects)
	}
	if out := tl.run("read", "0x4321", "0/NetworkCommissioning/Networks"); !strings.Contains(out, `"connected":true`) {
		t.Fatalf("read Networks:\n%s", out)
	}
}
