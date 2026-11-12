// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha1" //nolint:gosec // WPA2's PSK derivation is PBKDF2-HMAC-SHA1 (IEEE 802.11i); not a choice
	"encoding/hex"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/contract"
)

// The live test runs the driver against a real wpa_supplicant over the
// system bus — the ble-virtual job's Wi-Fi variant starts one on a
// mac80211_hwsim radio with hostapd's access points
// (script/ble-virtual-test.sh). It is gated on the environment and skips
// with the reason when the variables are unset:
//
//	GOFABRIC_WPA_LIVE_IFACE       the station interface wpa_supplicant manages
//	GOFABRIC_WPA_LIVE_SSID        a WPA2-PSK network in range
//	GOFABRIC_WPA_LIVE_PASSPHRASE  its passphrase
//	GOFABRIC_WPA_LIVE_OPEN_SSID   an open network in range (optional)
const (
	envLiveIface      = "GOFABRIC_WPA_LIVE_IFACE"
	envLiveSSID       = "GOFABRIC_WPA_LIVE_SSID"
	envLivePassphrase = "GOFABRIC_WPA_LIVE_PASSPHRASE" //nolint:gosec // G101: the variable's name, not a credential
	envLiveOpenSSID   = "GOFABRIC_WPA_LIVE_OPEN_SSID"
)

// TestLiveWPASupplicant: a scan finds the WPA2 network; a WPA2 passphrase
// join and a raw-PSK join (the 64 hex digits of the same network's PSK)
// succeed; a wrong passphrase fails and leaves no network of the attempt
// in wpa_supplicant's list; an open network joins with empty credentials.
// Neither credential reaches the driver's log.
func TestLiveWPASupplicant(t *testing.T) {
	iface, ssid, pass := os.Getenv(envLiveIface), os.Getenv(envLiveSSID), os.Getenv(envLivePassphrase)
	if iface == "" || ssid == "" || pass == "" {
		t.Skipf("no live wpa_supplicant: set %s, %s and %s (the ble-virtual job's Wi-Fi variant does)", envLiveIface, envLiveSSID, envLivePassphrase)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// The driver's own scan bound (DefaultScanTimeout): a scan of the
	// job's associated mac80211_hwsim station takes about 10 s (run
	// 43137917486); the test logs how long it took.
	d, err := New(ctx, Options{
		Interface: iface,
		Logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("New(%s): %v", iface, err)
	}
	defer func() { _ = d.Close() }()

	psk, err := pbkdf2.Key(sha1.New, pass, []byte(ssid), 4096, 32)
	if err != nil {
		t.Fatal(err)
	}
	rawPSK := hex.EncodeToString(psk)
	// secrets are the credential in each form a text could carry it:
	// the passphrase and the raw PSK in lower- and uppercase hex.
	secrets := []string{pass, rawPSK, strings.ToUpper(rawPSK)}
	holdsSecret := func(text string) bool {
		return slices.ContainsFunc(secrets, func(secret string) bool { return strings.Contains(text, secret) })
	}
	defer func() {
		leaked := holdsSecret(logs.String())
		if leaked {
			t.Error("a credential reached the driver's log")
		}
		// The driver's log explains a failure; it is shown only when it
		// holds no credential.
		if t.Failed() && !leaked {
			t.Logf("the driver's log:\n%s", logs.String())
		}
	}()

	t.Run("scan", func(t *testing.T) {
		start := time.Now()
		status, debug, results := d.ScanNetworks(ctx, nil)
		t.Logf("ScanNetworks: status %d after %v, %d results", status, time.Since(start).Round(time.Millisecond), len(results))
		if status != statusSuccess {
			t.Fatalf("ScanNetworks: status %d (%s)", status, debug)
		}
		if !slices.ContainsFunc(results, func(r contract.WiFiScanResult) bool { return string(r.SSID) == ssid }) {
			t.Errorf("the scan did not find %q among %d results", ssid, len(results))
		}
	})
	join := func(t *testing.T, ssid string, credential []byte) uint8 {
		t.Helper()
		status, debug, errorValue := d.ConnectNetwork(ctx, []byte(ssid), credential)
		if holdsSecret(debug) {
			t.Errorf("ConnectNetwork(%q): the debug text carries the credential", ssid)
			debug = "(withheld)"
		}
		t.Logf("ConnectNetwork(%q): status %d, debug %q, error value %d", ssid, status, debug, errorValue)
		return status
	}
	t.Run("passphrase", func(t *testing.T) {
		if status := join(t, ssid, []byte(pass)); status != statusSuccess {
			t.Errorf("the WPA2 passphrase join: status %d, want Success", status)
		}
	})
	t.Run("raw PSK", func(t *testing.T) {
		if status := join(t, ssid, []byte(rawPSK)); status != statusSuccess {
			t.Errorf("the raw-PSK join: status %d, want Success", status)
		}
	})
	t.Run("wrong passphrase", func(t *testing.T) {
		if status := join(t, ssid, []byte("not-"+pass)); status == statusSuccess {
			t.Fatal("a wrong passphrase joined")
		}
		v, err := d.property(ctx, "Networks")
		if err != nil {
			t.Fatalf("Networks: %v", err)
		}
		networks, _ := v.Value().([]dbus.ObjectPath)
		// The join removed the network current before it (connectedhomeip
		// src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp:861-877
		// at 6170af84) and the failed one removes its own (removeAdded):
		// none is left.
		if len(networks) != 0 {
			t.Errorf("wpa_supplicant still lists %v after the failed join", networks)
		}
	})
	t.Run("open network", func(t *testing.T) {
		open := os.Getenv(envLiveOpenSSID)
		if open == "" {
			t.Skipf("no open network: %s is unset", envLiveOpenSSID)
		}
		if status := join(t, open, nil); status != statusSuccess {
			t.Errorf("the open-network join: status %d, want Success", status)
		}
	})
}
