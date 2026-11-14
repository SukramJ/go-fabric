// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
)

const testCredential = "s3cr3t-Pa55phrase-NEVER-LOGGED"

// syncBuffer is a log sink safe for the driver's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// startDriver brings a driver up over b, its log at Debug into the
// returned buffer.
func startDriver(t *testing.T, b *fakeBus, opts Options) (*Driver, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	opts.Logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := newDriver(t.Context(), b, opts)
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}
	b.mu.Lock()
	b.scanArmed = func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.scan != nil
	}
	b.mu.Unlock()
	t.Cleanup(func() { _ = d.Close() })
	return d, logs
}

// TestBringUp: GetInterface names the interface; when wpa_supplicant does
// not manage it, CreateInterface({"Ifname": …}) does; auto scan is
// switched off, and its failure is not fatal. Both failing is New's error.
func TestBringUp(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	d, _ := startDriver(t, b, Options{Interface: "wlp2s0"})
	if d.iface != fakeIface {
		t.Errorf("interface %q", d.iface)
	}
	calls := b.callsCopy()
	if len(calls) != 2 || calls[0].method != ifaceRoot+".GetInterface" || calls[0].path != rootPath || calls[0].args[0] != "wlp2s0" ||
		calls[1].method != ifaceInterface+".AutoScan" || calls[1].path != fakeIface || calls[1].args[0] != "" {
		t.Fatalf("calls %+v", calls)
	}

	b = newFakeBus()
	b.fail[ifaceRoot+".GetInterface"] = dbus.NewError("fi.w1.wpa_supplicant1.InterfaceUnknown", nil)
	b.fail[ifaceInterface+".AutoScan"] = dbus.NewError("fi.w1.wpa_supplicant1.UnknownError", nil)
	startDriver(t, b, Options{})
	calls = b.callsCopy()
	if len(calls) != 3 || calls[1].method != ifaceRoot+".CreateInterface" {
		t.Fatalf("calls %+v", calls)
	}
	args, _ := calls[1].args[0].(map[string]dbus.Variant)
	if v, _ := args["Ifname"].Value().(string); v != "wlan0" || len(args) != 1 {
		t.Errorf("CreateInterface args %v", args)
	}

	b = newFakeBus()
	b.fail[ifaceRoot+".GetInterface"] = dbus.NewError("fi.w1.wpa_supplicant1.InterfaceUnknown", nil)
	b.fail[ifaceRoot+".CreateInterface"] = dbus.NewError("fi.w1.wpa_supplicant1.UnknownError", nil)
	if _, err := newDriver(t.Context(), b, Options{Logger: slog.New(slog.DiscardHandler)}); err == nil || !strings.Contains(err.Error(), "wlan0") {
		t.Errorf("newDriver = %v, want the create error", err)
	}
}

func bssProps(ssid string, bssid []byte, signal int16, freq uint16, wpa, rsn []string) map[string]dbus.Variant {
	p := map[string]dbus.Variant{
		"SSID": dbus.MakeVariant([]byte(ssid)), "BSSID": dbus.MakeVariant(bssid),
		"Signal": dbus.MakeVariant(signal), "Frequency": dbus.MakeVariant(freq),
	}
	if wpa != nil {
		p["WPA"] = dbus.MakeVariant(map[string]dbus.Variant{"KeyMgmt": dbus.MakeVariant(wpa)})
	}
	if rsn != nil {
		p["RSN"] = dbus.MakeVariant(map[string]dbus.Variant{"KeyMgmt": dbus.MakeVariant(rsn)})
	}
	return p
}

var bssid1 = []byte{0x02, 0, 0x5E, 0x10, 0, 1}

// TestScan: an active scan, its results read once ScanDone arrives —
// signal clamped into int8, band and channel from the frequency, the
// security from the key managements — a malformed or vanished BSS
// dropped, and a directed scan filtered by SSID.
func TestScan(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	paths := []dbus.ObjectPath{"/b/1", "/b/2", "/b/3", "/b/4", "/b/gone", "/b/longssid", "/b/badbssid"}
	b.props["BSSs"] = dbus.MakeVariant(paths)
	b.bss["/b/1"] = bssProps("lab", bssid1, -200, 2437, nil, []string{"wpa-psk", "sae"})
	b.bss["/b/2"] = bssProps("lab5", bssid1, 300, 5180, []string{"wpa-psk"}, nil)
	b.bss["/b/3"] = bssProps("lab6", bssid1, -40, 5975, nil, []string{"wpa-eap"})
	b.bss["/b/4"] = bssProps("open", bssid1, -60, 2412, nil, nil)
	b.bss["/b/longssid"] = bssProps(strings.Repeat("x", 33), bssid1, -60, 2412, nil, nil)
	b.bss["/b/badbssid"] = bssProps("bad", []byte{1, 2, 3}, -60, 2412, nil, nil)
	d, _ := startDriver(t, b, Options{})

	status, debug, results := d.ScanNetworks(t.Context(), nil)
	if status != statusSuccess || debug != "" {
		t.Fatalf("ScanNetworks = %d %q", status, debug)
	}
	calls := b.callsCopy()
	scan := calls[2]
	if scan.method != ifaceInterface+".Scan" {
		t.Fatalf("calls %+v", calls)
	}
	if args, _ := scan.args[0].(map[string]dbus.Variant); len(args) != 1 || args["Type"].Value() != "active" {
		t.Errorf("Scan args %v", scan.args)
	}
	type want struct {
		ssid    string
		sec     netcommdef.WiFiSecurityBitmap
		band    netcommdef.WiFiBandEnum
		channel uint16
		rssi    int8
	}
	wants := []want{
		{"lab", netcommdef.WiFiSecurityWpa2Personal | netcommdef.WiFiSecurityWpa3Personal, netcommdef.WiFiBandV2G4, 6, -128},
		{"lab5", netcommdef.WiFiSecurityWpaPersonal, netcommdef.WiFiBandV5G, 36, 127},
		{"lab6", 0, netcommdef.WiFiBandV6G, 5, -40},
		{"open", netcommdef.WiFiSecurityUnencrypted, netcommdef.WiFiBandV2G4, 1, -60},
	}
	if len(results) != len(wants) {
		t.Fatalf("%d results: %+v", len(results), results)
	}
	for i, w := range wants {
		r := results[i]
		if string(r.SSID) != w.ssid || r.Security != uint8(w.sec) || r.Band != uint8(w.band) || !r.HasBand || r.Channel != w.channel ||
			r.RSSI != w.rssi || !r.HasRSSI || !bytes.Equal(r.BSSID, bssid1) {
			t.Errorf("result %d = %+v, want %+v", i, r, w)
		}
	}

	_, _, results = d.ScanNetworks(t.Context(), []byte("lab5"))
	if len(results) != 1 || string(results[0].SSID) != "lab5" {
		t.Errorf("directed scan %+v", results)
	}
}

// TestScanWithoutBSSs: a scan whose BSSs the service does not serve is a
// Success without networks (chip's "no network found").
func TestScanWithoutBSSs(t *testing.T) {
	t.Parallel()
	d, _ := startDriver(t, newFakeBus(), Options{})
	if status, _, results := d.ScanNetworks(t.Context(), nil); status != statusSuccess || len(results) != 0 {
		t.Errorf("ScanNetworks = %d, %v", status, results)
	}
}

// TestScanOneAtATime: a second scan while one waits is UnknownError; the
// scan's timeout is UnknownError; Close ends a waiting scan.
func TestScanOneAtATime(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.noScanDone = true
	d, _ := startDriver(t, b, Options{ScanTimeout: time.Hour})
	first := make(chan uint8)
	go func() {
		status, _, _ := d.ScanNetworks(context.Background(), nil)
		first <- status
	}()
	waitFor(t, func() bool { return slices.Contains(b.called(), ifaceInterface+".Scan") })
	if status, debug, _ := d.ScanNetworks(t.Context(), nil); status != statusUnknown || debug == "" {
		t.Errorf("second scan = %d %q", status, debug)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if status := <-first; status != statusUnknown {
		t.Errorf("scan ended by Close = %d", status)
	}

	b = newFakeBus()
	b.noScanDone = true
	d, _ = startDriver(t, b, Options{ScanTimeout: 20 * time.Millisecond})
	if status, debug, _ := d.ScanNetworks(t.Context(), nil); status != statusUnknown || !strings.Contains(debug, "in time") {
		t.Errorf("timed-out scan = %d %q", status, debug)
	}
}

// waitFor waits until cond holds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatal("condition not reached")
		case <-tick.C:
		}
	}
}

// addNetworkArgs is the argument of the driver's last AddNetwork.
func addNetworkArgs(t *testing.T, b *fakeBus) map[string]dbus.Variant {
	t.Helper()
	calls := b.callsCopy()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == ifaceInterface+".AddNetwork" {
			args, _ := calls[i].args[0].(map[string]dbus.Variant)
			return args
		}
	}
	t.Fatal("no AddNetwork")
	return nil
}

// TestConnect: the current network is removed, the network added
// ({ssid, psk, key_mgmt "SAE WPA-PSK"}), the interface disconnected and
// the network selected, in that order; "completed" after "associating" is
// Success.
func TestConnect(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["CurrentNetwork"] = dbus.MakeVariant(dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/7"))
	b.props["State"] = dbus.MakeVariant(stateCompleted)
	d, _ := startDriver(t, b, Options{})
	status, debug, errValue := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	if status != statusSuccess || debug != "" || errValue != 0 {
		t.Fatalf("ConnectNetwork = %d %q %d", status, debug, errValue)
	}
	want := []string{
		ifaceRoot + ".GetInterface", ifaceInterface + ".AutoScan",
		ifaceInterface + ".AddNetwork", ifaceInterface + ".Disconnect", ifaceInterface + ".SelectNetwork", ifaceInterface + ".RemoveNetwork",
	}
	if got := b.called(); !slices.Equal(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	calls := b.callsCopy()
	for _, c := range calls {
		switch c.method {
		case ifaceInterface + ".RemoveNetwork":
			if c.args[0] != dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/7") {
				t.Errorf("RemoveNetwork %v", c.args)
			}
		case ifaceInterface + ".SelectNetwork":
			if c.args[0] != fakeNetwork {
				t.Errorf("SelectNetwork %v", c.args)
			}
		}
	}
	args := addNetworkArgs(t, b)
	if len(args) != 3 || args["ssid"].Value() != "lab" || args["psk"].Value() != testCredential || args["key_mgmt"].Value() != "SAE WPA-PSK" {
		t.Errorf("AddNetwork %v", args)
	}
}

// TestConnectCompletedNeedsAssociating: a "completed" without an
// "associating" before it is no verdict; the one after it is.
func TestConnectCompletedNeedsAssociating(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) { b.setStateLocked(stateCompleted) }
	d, _ := startDriver(t, b, Options{})
	result := make(chan uint8, 1)
	go func() {
		status, _, _ := d.ConnectNetwork(context.Background(), []byte("lab"), []byte(testCredential))
		result <- status
	}()
	<-b.selected
	select {
	case s := <-result:
		t.Fatalf("verdict %d on a completed without associating", s)
	case <-time.After(50 * time.Millisecond):
	}
	b.setState("associating")
	b.setState(stateCompleted)
	if s := <-result; s != statusSuccess {
		t.Errorf("verdict %d", s)
	}
}

// TestConnectDisconnected: a "disconnected" after "associating" is
// UnknownError with the interface's DisconnectReason; a "disconnected"
// before it (the Disconnect call's own) is not.
func TestConnectDisconnected(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["State"] = dbus.MakeVariant(stateCompleted) // Disconnect walks to disconnected first
	b.props["DisconnectReason"] = dbus.MakeVariant(int32(-3))
	b.props["AssocStatusCode"] = dbus.MakeVariant(int32(1))
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
	}
	d, logs := startDriver(t, b, Options{})
	status, debug, errValue := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	if status != statusUnknown || errValue != -3 || !strings.Contains(debug, "-3") {
		t.Errorf("ConnectNetwork = %d %q %d", status, debug, errValue)
	}
	if strings.Contains(logs.String(), testCredential) || strings.Contains(debug, testCredential) {
		t.Error("the credential leaked")
	}
}

// TestConnectAuthTimeoutRetry: a disconnect with AssocStatusCode
// AUTH_TIMEOUT selects the network again, up to eight times; the ninth
// is the failure. A retry that then completes is Success.
func TestConnectAuthTimeoutRetry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		completes int // the select that completes; 0 none
		selects   int
		status    uint8
	}{
		{"exhausted", 0, 1 + assocMaxRetries, statusUnknown},
		{"recovers", 5, 5, statusSuccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newFakeBus()
			b.props["AssocStatusCode"] = dbus.MakeVariant(int32(wlanStatusAuthTimeout))
			b.props["DisconnectReason"] = dbus.MakeVariant(int32(15))
			b.onSelect = func(b *fakeBus, n int) {
				b.setStateLocked("associating")
				if n == tc.completes {
					b.setStateLocked(stateCompleted)
					return
				}
				b.setStateLocked(stateDisconnected)
			}
			d, _ := startDriver(t, b, Options{})
			status, _, errValue := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
			if status != tc.status {
				t.Errorf("status %d, want %d", status, tc.status)
			}
			if tc.status != statusSuccess && errValue != 15 {
				t.Errorf("errorValue %d", errValue)
			}
			b.mu.Lock()
			selects := b.selects
			b.mu.Unlock()
			if selects != tc.selects {
				t.Errorf("%d selects, want %d", selects, tc.selects)
			}
		})
	}
}

// TestConnectWithoutStatusProperties: a service that serves neither
// DisconnectReason nor AssocStatusCode (connectedhomeip's mock) fails with
// errorValue 0 and no retry.
func TestConnectWithoutStatusProperties(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
	}
	d, _ := startDriver(t, b, Options{})
	status, _, errValue := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	b.mu.Lock()
	selects := b.selects
	b.mu.Unlock()
	if status != statusUnknown || errValue != 0 || selects != 1 {
		t.Errorf("ConnectNetwork = %d %d after %d selects", status, errValue, selects)
	}
}

// TestConnectInterfaceDisabled: a disabled interface fails before any
// network is added.
func TestConnectInterfaceDisabled(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["State"] = dbus.MakeVariant(stateInterfaceDisabled)
	d, _ := startDriver(t, b, Options{})
	status, debug, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	if status != statusUnknown || !strings.Contains(debug, "disabled") || slices.Contains(b.called(), ifaceInterface+".AddNetwork") {
		t.Errorf("ConnectNetwork = %d %q, calls %v", status, debug, b.called())
	}
}

// TestConnectTimeoutAndOneAtATime: a join that never completes is
// UnknownError with errorValue 0 at the timeout; a second join while one
// waits is UnknownError; Close ends a waiting join.
func TestConnectTimeoutAndOneAtATime(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) { b.setStateLocked("associating") }
	d, _ := startDriver(t, b, Options{ConnectTimeout: 20 * time.Millisecond})
	if status, debug, errValue := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential)); status != statusUnknown || errValue != 0 || !strings.Contains(debug, "in time") {
		t.Errorf("timed-out connect = %d %q %d", status, debug, errValue)
	}

	b = newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) { b.setStateLocked("associating") }
	d, _ = startDriver(t, b, Options{ConnectTimeout: time.Hour})
	first := make(chan uint8)
	go func() {
		status, _, _ := d.ConnectNetwork(context.Background(), []byte("lab"), []byte(testCredential))
		first <- status
	}()
	<-b.selected
	if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential)); status != statusUnknown || !strings.Contains(debug, "in progress") {
		t.Errorf("second connect = %d %q", status, debug)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if status := <-first; status != statusUnknown {
		t.Errorf("connect ended by Close = %d", status)
	}
	if status, _, _ := d.ConnectNetwork(t.Context(), []byte("lab"), nil); status != statusUnknown {
		t.Errorf("connect after Close = %d", status)
	}
}

// TestConnectOpenNetwork: empty credentials add the network with key_mgmt
// "NONE" and no psk.
func TestConnectOpenNetwork(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	d, _ := startDriver(t, b, Options{})
	if status, _, _ := d.ConnectNetwork(t.Context(), []byte("guest"), nil); status != statusSuccess {
		t.Fatalf("status %d", status)
	}
	args := addNetworkArgs(t, b)
	if _, ok := args["psk"]; ok || len(args) != 2 || args["ssid"].Value() != "guest" || args["key_mgmt"].Value() != "NONE" {
		t.Errorf("AddNetwork %v", args)
	}
}

// TestConnectNonUTF8SSID: an SSID that is not valid UTF-8 or holds a NUL
// goes as a byte array (wpa_supplicant writes it as the hex form of its
// octets); one that is valid UTF-8 stays a string.
func TestConnectNonUTF8SSID(t *testing.T) {
	t.Parallel()
	for _, ssid := range [][]byte{{0xff, 0xfe, 'a'}, []byte("la\x00b")} {
		b := newFakeBus()
		d, logs := startDriver(t, b, Options{})
		status, debug, _ := d.ConnectNetwork(t.Context(), ssid, []byte(testCredential))
		if status != statusSuccess {
			t.Errorf("ConnectNetwork(%q) = %d %q", ssid, status, debug)
		}
		if got, ok := addNetworkArgs(t, b)["ssid"].Value().([]byte); !ok || !bytes.Equal(got, ssid) {
			t.Errorf("AddNetwork ssid %v, want the bytes %q", addNetworkArgs(t, b)["ssid"], ssid)
		}
		if strings.Contains(logs.String(), testCredential) {
			t.Error("the credential leaked")
		}
	}
}

// TestConnectRawPSK: a credential of exactly 64 hex digits is the raw PSK
// and goes as its 32 bytes; a 63-character passphrase and a 64-character
// credential that is not hex stay strings.
func TestConnectRawPSK(t *testing.T) {
	t.Parallel()
	raw := strings.Repeat("0123456789abcdEF", 4)
	for _, tc := range []struct {
		cred  string
		bytes bool
	}{
		{raw, true},
		{strings.Repeat("p", 63), false},
		{strings.Repeat("g", 64), false},
		{raw[:62] + "zz", false},
	} {
		b := newFakeBus()
		d, logs := startDriver(t, b, Options{})
		if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(tc.cred)); status != statusSuccess {
			t.Fatalf("ConnectNetwork = %d %q", status, debug)
		}
		psk := addNetworkArgs(t, b)["psk"].Value()
		if tc.bytes {
			want, _ := hex.DecodeString(raw)
			if got, ok := psk.([]byte); !ok || !bytes.Equal(got, want) || len(got) != 32 {
				t.Errorf("psk %T, want the 32 raw bytes", psk)
			}
		} else if got, ok := psk.(string); !ok || got != tc.cred {
			t.Errorf("psk %T, want the string", psk)
		}
		if strings.Contains(strings.ToLower(logs.String()), strings.ToLower(tc.cred)) {
			t.Error("the credential leaked")
		}
	}
}

// TestConnectIgnoresStatesBeforeSelect: State changes from before the
// join's SelectNetwork — the network that was current, its removal, the
// driver's own Disconnect — do not decide the join.
func TestConnectIgnoresStatesBeforeSelect(t *testing.T) {
	t.Parallel()
	// A stale "associating" before Disconnect's "disconnected": the new
	// network then walks to completed.
	b := newFakeBus()
	b.props["State"] = dbus.MakeVariant(stateCompleted)
	b.onDisconnect = func(b *fakeBus) { b.setStateLocked("associating") }
	d, _ := startDriver(t, b, Options{})
	if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential)); status != statusSuccess {
		t.Errorf("ConnectNetwork = %d %q, want Success from the new network's walk", status, debug)
	}

	// A stale "associating, completed" pair while the new network never
	// associates: no Success.
	b = newFakeBus()
	b.props["State"] = dbus.MakeVariant(stateDisconnected)
	b.onDisconnect = func(b *fakeBus) {
		b.setStateLocked("associating")
		b.setStateLocked(stateCompleted)
		b.setStateLocked(stateDisconnected)
	}
	b.onSelect = func(*fakeBus, int) {}
	d, _ = startDriver(t, b, Options{ConnectTimeout: 50 * time.Millisecond})
	if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential)); status != statusUnknown || !strings.Contains(debug, "in time") {
		t.Errorf("ConnectNetwork = %d %q, want the timeout", status, debug)
	}
}

// TestConnectFailureRemovesNetwork: every join that does not end in
// Success removes the network it added — a failed select, a
// "disconnected" verdict, the timeout, the call's context, Close (over the
// bus, before Close releases it); a Success keeps it.
func TestConnectFailureRemovesNetwork(t *testing.T) {
	t.Parallel()
	disconnects := func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
	}
	stalls := func(*fakeBus, int) {}
	for _, tc := range []struct {
		name    string
		setup   func(b *fakeBus)
		opts    Options
		ctx     func(t *testing.T) context.Context
		status  uint8
		removes int
	}{
		{"success", func(*fakeBus) {}, Options{}, nil, statusSuccess, 0},
		{"select fails", func(b *fakeBus) {
			b.fail[ifaceInterface+".SelectNetwork"] = dbus.NewError("fi.w1.wpa_supplicant1.UnknownError", nil)
		}, Options{}, nil, statusUnknown, 1},
		{"disconnected", func(b *fakeBus) { b.onSelect = disconnects }, Options{}, nil, statusUnknown, 1},
		{"timeout", func(b *fakeBus) { b.onSelect = stalls }, Options{ConnectTimeout: 20 * time.Millisecond}, nil, statusUnknown, 1},
		{"context", func(b *fakeBus) { b.onSelect = stalls }, Options{ConnectTimeout: time.Hour}, func(t *testing.T) context.Context {
			t.Helper()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			t.Cleanup(cancel)
			return ctx
		}, statusUnknown, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newFakeBus()
			tc.setup(b)
			d, _ := startDriver(t, b, tc.opts)
			ctx := t.Context()
			if tc.ctx != nil {
				ctx = tc.ctx(t)
			}
			if status, debug, _ := d.ConnectNetwork(ctx, []byte("lab"), []byte(testCredential)); status != tc.status {
				t.Fatalf("ConnectNetwork = %d %q", status, debug)
			}
			if n, _ := b.removed(); n != tc.removes {
				t.Errorf("%d removals of the added network, want %d", n, tc.removes)
			}
		})
	}

	t.Run("close", func(t *testing.T) {
		t.Parallel()
		b := newFakeBus()
		b.onSelect = stalls
		d, _ := startDriver(t, b, Options{ConnectTimeout: time.Hour})
		result := make(chan uint8, 1)
		go func() {
			status, _, _ := d.ConnectNetwork(context.Background(), []byte("lab"), []byte(testCredential))
			result <- status
		}()
		<-b.selected
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		if s := <-result; s != statusUnknown {
			t.Errorf("status %d", s)
		}
		if n, afterClose := b.removed(); n != 1 || afterClose {
			t.Errorf("%d removals, after the bus closed: %v", n, afterClose)
		}
	})
}

// TestScanDoneFailure: a ScanDone that reports success false ends the
// scan as one that reports true — connectedhomeip reads the BSSs either
// way.
func TestScanDoneFailure(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.scanFailed = true
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{"/b/1"})
	b.bss["/b/1"] = bssProps("lab", bssid1, -50, 2437, nil, nil)
	d, _ := startDriver(t, b, Options{})
	if status, _, results := d.ScanNetworks(t.Context(), nil); status != statusSuccess || len(results) != 1 {
		t.Errorf("ScanNetworks = %d, %v", status, results)
	}
}

// TestCredentialNeverLeaves: across a success, an AddNetwork error whose
// text echoes the credential, a failed join and a refused credential, the
// credential reaches the bus in AddNetwork alone — never a log line, a
// debug text or another call.
func TestCredentialNeverLeaves(t *testing.T) {
	t.Parallel()
	texts := make([]string, 0, 4)
	check := func(b *fakeBus, logs *syncBuffer) {
		t.Helper()
		if s := logs.String(); strings.Contains(s, testCredential) {
			t.Errorf("the credential is in the log:\n%s", s)
		}
		for _, d := range texts {
			if strings.Contains(d, testCredential) {
				t.Errorf("the credential is in the debug text %q", d)
			}
		}
		for _, c := range b.callsCopy() {
			if c.method == ifaceInterface+".AddNetwork" {
				continue
			}
			for _, a := range c.args {
				if strings.Contains(dbus.MakeVariant(a).String(), testCredential) {
					t.Errorf("the credential is in %s %v", c.method, c.args)
				}
			}
		}
	}

	b := newFakeBus()
	d, logs := startDriver(t, b, Options{})
	_, text, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	texts = append(texts, text)
	check(b, logs)

	b = newFakeBus()
	b.fail[ifaceInterface+".AddNetwork"] = dbus.NewError("fi.w1.wpa_supplicant1.InvalidArgs", []any{"invalid psk " + testCredential})
	d, logs = startDriver(t, b, Options{})
	status, text, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	texts = append(texts, text)
	if status != statusUnknown || !strings.Contains(text, "fi.w1.wpa_supplicant1.InvalidArgs") {
		t.Errorf("failed AddNetwork = %d %q", status, text)
	}
	check(b, logs)

	b = newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
	}
	d, logs = startDriver(t, b, Options{})
	_, text, _ = d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	texts = append(texts, text)
	check(b, logs)

	b = newFakeBus()
	d, logs = startDriver(t, b, Options{})
	_, text, _ = d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential+"\xff"))
	texts = append(texts, text)
	check(b, logs)
}

// TestSignalsOfOtherObjects: a State change of another object or another
// D-Bus interface is not the join's.
func TestSignalsOfOtherObjects(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.sigs <- &dbus.Signal{Path: "/fi/w1/wpa_supplicant1/Interfaces/1", Name: sigPropsChanged, Body: []any{ifaceInterface, map[string]dbus.Variant{"State": dbus.MakeVariant(stateCompleted)}, []string{}}}
		b.sigs <- &dbus.Signal{Path: fakeIface, Name: sigPropsChanged, Body: []any{"fi.w1.wpa_supplicant1.Network", map[string]dbus.Variant{"State": dbus.MakeVariant(stateCompleted)}, []string{}}}
		b.sigs <- &dbus.Signal{Path: fakeIface, Name: ifaceInterface + ".PropertiesChanged", Body: []any{map[string]dbus.Variant{"State": dbus.MakeVariant(stateCompleted)}}}
	}
	d, _ := startDriver(t, b, Options{ConnectTimeout: 50 * time.Millisecond})
	if status, _, _ := d.ConnectNetwork(t.Context(), []byte("lab"), nil); status != statusUnknown {
		t.Errorf("status %d from a foreign signal", status)
	}
}

func TestErrName(t *testing.T) {
	t.Parallel()
	if got := errName(errors.New("secret")); got != "failed" {
		t.Errorf("errName = %q", got)
	}
}

// TestScanReadsAfterTheBound: the scan's bound covers the wait for
// ScanDone, not the reads after it — a ScanDone close to the bound still
// yields every BSS (run 43137917486: a scan while associated ended at
// 9.99 s of a 10 s bound).
func TestScanReadsAfterTheBound(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{"/b/1", "/b/2"})
	b.bss["/b/1"] = bssProps("lab", bssid1, -40, 2437, nil, []string{"wpa-psk"})
	b.bss["/b/2"] = bssProps("lab5", bssid1, -50, 5180, nil, []string{"wpa-psk"})
	const bound = 30 * time.Millisecond
	// Each read starts after the scan's bound has passed.
	b.beforeRead = func() { time.Sleep(2 * bound) }
	d, _ := startDriver(t, b, Options{ScanTimeout: bound})
	status, debug, results := d.ScanNetworks(t.Context(), nil)
	if status != statusSuccess || len(results) != 2 {
		t.Fatalf("ScanNetworks = %d %q, %d results; want Success with 2", status, debug, len(results))
	}
}

// TestScanIgnoresAStaleScanDone: a ScanDone sent before ScanNetworks was
// called ended an earlier scan; the scan keeps waiting (here until its
// bound) and reads no BSSs.
func TestScanIgnoresAStaleScanDone(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.noScanDone = true
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{})
	d, _ := startDriver(t, b, Options{ScanTimeout: 50 * time.Millisecond})
	b.sigs <- &dbus.Signal{Path: fakeIface, Name: sigScanDone, Body: []any{true}}
	if status, debug, _ := d.ScanNetworks(t.Context(), nil); status != statusUnknown || !strings.Contains(debug, "in time") {
		t.Fatalf("ScanNetworks after a stale ScanDone = %d %q, want the bound's UnknownError", status, debug)
	}
	if slices.Contains(b.called(), methodGet) {
		t.Error("the BSSs were read on the stale ScanDone")
	}
}

// TestScanEndsAtAnImmediateScanDone: a ScanDone queued right behind the
// Scan call's reply — before the driver could have armed a wait after the
// call — ends the scan at once, as chip's mock and a fast scan send it.
// The bound is an hour: a lost ScanDone fails the test at its deadline.
func TestScanEndsAtAnImmediateScanDone(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.immediateScanDone = true
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{"/b/1"})
	b.bss["/b/1"] = bssProps("lab", bssid1, -50, 2437, nil, nil)
	d, _ := startDriver(t, b, Options{ScanTimeout: time.Hour})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	status, debug, results := d.ScanNetworks(ctx, nil)
	if status != statusSuccess || len(results) != 1 {
		t.Fatalf("ScanNetworks with an immediate ScanDone = %d %q, %d results; want Success with 1", status, debug, len(results))
	}
}

// TestScanFailureLeavesNoWaiter: a Scan call that fails is UnknownError
// and disarms its wait; the next scan waits for its own ScanDone and
// succeeds.
func TestScanFailureLeavesNoWaiter(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.fail[ifaceInterface+".Scan"] = dbus.NewError("fi.w1.wpa_supplicant1.Interface.ScanError", []any{"busy"})
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{"/b/1"})
	b.bss["/b/1"] = bssProps("lab", bssid1, -50, 2437, nil, nil)
	d, _ := startDriver(t, b, Options{})
	if status, debug, _ := d.ScanNetworks(t.Context(), nil); status != statusUnknown || !strings.Contains(debug, "scan:") {
		t.Fatalf("failed Scan = %d %q", status, debug)
	}
	d.mu.Lock()
	armed, scanning := d.scan != nil, d.scanning
	d.mu.Unlock()
	if armed || scanning {
		t.Fatalf("after a failed Scan: armed %v, scanning %v", armed, scanning)
	}
	b.mu.Lock()
	delete(b.fail, ifaceInterface+".Scan")
	b.mu.Unlock()
	if status, debug, results := d.ScanNetworks(t.Context(), nil); status != statusSuccess || len(results) != 1 {
		t.Fatalf("scan after a failed one = %d %q, %v", status, debug, results)
	}
}

// TestCallsEndWhenTheBusConnectionDrops: the system-bus connection lost
// (godbus closes the signal channel) ends the run loop; a scan and a join
// started afterwards fail at once with UnknownError instead of blocking
// on the loop that is gone, and a second scan is not refused as "in
// progress" (review II, R2-P1-drv-02).
func TestCallsEndWhenTheBusConnectionDrops(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	d, _ := startDriver(t, b, Options{ScanTimeout: time.Hour, ConnectTimeout: time.Hour})
	close(b.sigs)
	deadline, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-d.loopDone:
	case <-deadline.Done():
		t.Fatal("the run loop did not end when its signal channel closed")
	}
	type result struct {
		status uint8
		debug  string
	}
	for i := range 2 {
		res := make(chan result, 1)
		go func() {
			status, debug, _ := d.ScanNetworks(deadline, nil)
			res <- result{status, debug}
		}()
		select {
		case r := <-res:
			if r.status != statusUnknown || !strings.Contains(r.debug, "bus connection") {
				t.Fatalf("scan %d after the bus dropped = %d %q, want UnknownError naming the bus connection", i, r.status, r.debug)
			}
		case <-deadline.Done():
			t.Fatalf("scan %d still blocked after the bus dropped", i)
		}
	}
	res := make(chan result, 1)
	go func() {
		status, debug, _ := d.ConnectNetwork(deadline, []byte("lab"), []byte(testCredential))
		res <- result{status, debug}
	}()
	select {
	case r := <-res:
		if r.status != statusUnknown || !strings.Contains(r.debug, "bus connection") {
			t.Fatalf("join after the bus dropped = %d %q, want UnknownError naming the bus connection", r.status, r.debug)
		}
	case <-deadline.Done():
		t.Fatal("join still blocked after the bus dropped")
	}
}

// TestScanEndsWithItsContextWhileTheLoopIsBusy: a scan whose hand-over to
// the run loop cannot complete — the loop is held handling a signal — ends
// with its context, and the driver scans again once the loop is free
// (review II, R2-P1-drv-02). The loop waits on nothing of the driver's
// since core2 item 4, so a logger that blocks on the loop's "interface
// state" record holds it.
func TestScanEndsWithItsContextWhileTheLoopIsBusy(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{})
	release, held := make(chan struct{}), make(chan struct{})
	logs := &syncBuffer{}
	var once sync.Once
	logger := slog.New(&holdingHandler{
		Handler: slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}),
		hold: func(r slog.Record) {
			if r.Message == "wpasupplicant: interface state" {
				once.Do(func() { close(held); <-release })
			}
		},
	})
	d, err := newDriver(t.Context(), b, Options{ScanTimeout: 50 * time.Millisecond, ConnectTimeout: 50 * time.Millisecond, Logger: logger})
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}
	b.mu.Lock()
	b.scanArmed = func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.scan != nil
	}
	b.mu.Unlock()
	t.Cleanup(func() { _ = d.Close() })
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		_, _, _ = d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
	}()
	<-held
	deadline, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	res := make(chan string, 1)
	go func() {
		_, debug, _ := d.ScanNetworks(deadline, nil)
		res <- debug
	}()
	select {
	case debug := <-res:
		if !strings.Contains(debug, "in time") {
			t.Errorf("scan while the loop is busy = %q, want its bound's UnknownError", debug)
		}
	case <-deadline.Done():
		close(release)
		t.Fatal("scan still blocked after its context ended")
	}
	close(release)
	<-joined
	if status, debug, _ := d.ScanNetworks(deadline, nil); status != statusSuccess {
		t.Fatalf("scan once the loop is free = %d %q, want Success", status, debug)
	}
	d.mu.Lock()
	armed, scanning := d.scan != nil, d.scanning
	d.mu.Unlock()
	if armed || scanning {
		t.Fatalf("after the scans: armed %v, scanning %v", armed, scanning)
	}
}

// TestTheRunLoopKeepsReadingWhileAJoinIsInACall pins core2 item 4
// (R2-P1-drv-06): a join's State changes are queued for it without the
// run loop waiting on the join. Before, the loop handed each change over a
// 16-slot channel and blocked on the 17th while the join was in a bus call
// of its own (here the DisconnectReason read), so it read no signal and
// took no scan until the call returned — and on the system bus godbus
// handed every signal that found its channel full to a goroutine of its
// own, losing the order the join's "drain by length" relies on
// (default_handler.go:316-333 at v5.2.4).
func TestTheRunLoopKeepsReadingWhileAJoinIsInACall(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{})
	b.props["DisconnectReason"] = dbus.MakeVariant(int32(3))
	b.props["AssocStatusCode"] = dbus.MakeVariant(int32(0))
	inCall, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
		// The join's next property read is its DisconnectReason: it stays
		// in that call until release. Later reads (the scan's) pass.
		b.beforeRead = func() {
			if first.CompareAndSwap(false, true) {
				close(inCall)
				<-release
			}
		}
	}
	d, _ := startDriver(t, b, Options{})
	joined := make(chan uint8, 1)
	go func() {
		status, _, _ := d.ConnectNetwork(t.Context(), []byte("lab"), []byte(testCredential))
		joined <- status
	}()
	<-inCall
	for range 40 {
		b.setState("scanning")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	status, debug, _ := d.ScanNetworks(ctx, nil)
	close(release)
	if status != statusSuccess {
		t.Errorf("a scan while the join is in a call = %d %q, want Success: the run loop waited on the join", status, debug)
	}
	if status := <-joined; status != statusUnknown {
		t.Errorf("the join = %d, want UnknownError (disconnected)", status)
	}
}

// holdingHandler runs hold on every record before its handler writes it.
type holdingHandler struct {
	slog.Handler
	hold func(slog.Record)
}

func (h *holdingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.hold(r)
	return h.Handler.Handle(ctx, r)
}

// TestAHandedOverWaitIsNotInstalledAfterItsCallerGaveUp: the run loop takes
// a scan's waiter or a join's attempt, the caller's context ends before
// the loop installs it, and the loop installs it afterwards: neither is
// left registered (review II, R2-P1-drv-02). The test plays the run loop.
func TestAHandedOverWaitIsNotInstalledAfterItsCallerGaveUp(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	d := &Driver{
		bus: b, iface: fakeIface, scanTimeout: time.Hour, connectTimeout: time.Hour, log: slog.New(slog.DiscardHandler),
		register: make(chan *attempt), armScan: make(chan *scanWaiter),
		closed: make(chan struct{}), loopDone: make(chan struct{}),
	}
	deadline, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()

	ctx, cancel := context.WithCancel(deadline)
	scanned := make(chan string, 1)
	go func() {
		_, debug, _ := d.ScanNetworks(ctx, nil)
		scanned <- debug
	}()
	var w *scanWaiter
	select {
	case w = <-d.armScan:
	case <-deadline.Done():
		t.Fatal("the scan handed nothing over")
	}
	cancel()
	select {
	case debug := <-scanned:
		if !strings.Contains(debug, "in time") {
			t.Fatalf("scan given up during the hand-over = %q", debug)
		}
	case <-deadline.Done():
		t.Fatal("the scan did not end with its context")
	}
	d.installScan(w)
	d.mu.Lock()
	armed := d.scan != nil
	d.mu.Unlock()
	if armed {
		t.Fatal("the abandoned scan's waiter was installed")
	}

	ctx, cancel = context.WithCancel(deadline)
	listened := make(chan string, 1)
	go func() {
		_, debug := d.listen(ctx)
		listened <- debug
	}()
	var a *attempt
	select {
	case a = <-d.register:
	case <-deadline.Done():
		t.Fatal("the join handed nothing over")
	}
	cancel()
	select {
	case debug := <-listened:
		if !strings.Contains(debug, "in time") {
			t.Fatalf("join given up during the hand-over = %q", debug)
		}
	case <-deadline.Done():
		t.Fatal("the join did not end with its context")
	}
	d.installAttempt(a)
	d.mu.Lock()
	installed := d.connect != nil
	d.mu.Unlock()
	if installed {
		t.Fatal("the abandoned join's attempt was installed")
	}
	select {
	case <-a.done:
	default:
		t.Fatal("the abandoned attempt's done is open: a State change would block the loop")
	}
}

// TestAFailedJoinKeepsTheNetworkItWasOn: a join that does not end in
// Success leaves wpa_supplicant on the network that was current before
// it — kept, not removed, while the join runs, and selected again after
// the network the join added is removed — and a join that succeeds
// removes the network that was current only then (review II,
// R2-P3-net-01).
func TestAFailedJoinKeepsTheNetworkItWasOn(t *testing.T) {
	t.Parallel()
	const working = dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/7")
	newBus := func() *fakeBus {
		b := newFakeBus()
		b.props["State"] = dbus.MakeVariant(stateCompleted)
		b.props["CurrentNetwork"] = dbus.MakeVariant(working)
		b.props["DisconnectReason"] = dbus.MakeVariant(int32(15))
		b.props["AssocStatusCode"] = dbus.MakeVariant(int32(1))
		return b
	}
	// held is the networks wpa_supplicant holds after the calls the
	// driver made.
	held := func(b *fakeBus) map[dbus.ObjectPath]bool {
		out := map[dbus.ObjectPath]bool{working: true}
		for _, c := range b.callsCopy() {
			switch c.method {
			case ifaceInterface + ".AddNetwork":
				out[fakeNetwork] = true
			case ifaceInterface + ".RemoveNetwork":
				if p, ok := c.args[0].(dbus.ObjectPath); ok {
					delete(out, p)
				}
			}
		}
		return out
	}

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		b := newBus()
		b.onSelect = func(b *fakeBus, n int) {
			if n == 1 { // the join of the new network fails
				b.setStateLocked("associating")
				b.setStateLocked(stateDisconnected)
			}
		}
		d, _ := startDriver(t, b, Options{})
		if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("new"), []byte(testCredential)); status == statusSuccess {
			t.Fatalf("the join succeeded: %q", debug)
		}
		if h := held(b); len(h) != 1 || !h[working] {
			t.Fatalf("after the failed join wpa_supplicant holds %v, want only the network it was on (%s); calls: %v", h, working, b.called())
		}
		var selected []dbus.ObjectPath
		for _, c := range b.callsCopy() {
			if c.method == ifaceInterface+".SelectNetwork" {
				selected = append(selected, c.args[0].(dbus.ObjectPath))
			}
		}
		if !slices.Equal(selected, []dbus.ObjectPath{fakeNetwork, working}) {
			t.Errorf("SelectNetwork %v, want the new network and then the one it was on again", selected)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		b := newBus()
		d, _ := startDriver(t, b, Options{})
		if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("new"), []byte(testCredential)); status != statusSuccess {
			t.Fatalf("ConnectNetwork = %d %q", status, debug)
		}
		if h := held(b); len(h) != 1 || !h[fakeNetwork] {
			t.Errorf("after the join wpa_supplicant holds %v, want only the joined network; calls: %v", h, b.called())
		}
	})
}
