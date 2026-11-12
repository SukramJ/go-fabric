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
		ifaceInterface + ".RemoveNetwork", ifaceInterface + ".AddNetwork", ifaceInterface + ".Disconnect", ifaceInterface + ".SelectNetwork",
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

// TestScanIgnoresAStaleScanDone: a ScanDone that arrives before this
// scan's Scan call returned ended an earlier scan; the scan keeps waiting
// (here until its bound).
func TestScanIgnoresAStaleScanDone(t *testing.T) {
	t.Parallel()
	b := newFakeBus()
	b.staleScanDone = true
	b.props["BSSs"] = dbus.MakeVariant([]dbus.ObjectPath{})
	d, _ := startDriver(t, b, Options{ScanTimeout: 50 * time.Millisecond})
	if status, debug, _ := d.ScanNetworks(t.Context(), nil); status != statusUnknown || !strings.Contains(debug, "in time") {
		t.Fatalf("ScanNetworks after a stale ScanDone = %d %q, want the bound's UnknownError", status, debug)
	}
	if slices.Contains(b.called(), methodGet) {
		t.Error("the BSSs were read on the stale ScanDone")
	}
}
