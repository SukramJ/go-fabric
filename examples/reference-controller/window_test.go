// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPakePasscodeVerifier: the verifier is w0 ‖ L, 32 + 65 bytes, and
// the device side's NewVerifierFromValue reads back the same w0 and L
// NewVerifierContext derives.
func TestPakePasscodeVerifier(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	v, err := pakePasscodeVerifier(20202021, salt, windowIterations)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != spake2.VerifierW0Size+spake2.VerifierLSize {
		t.Fatalf("verifier length %d", len(v))
	}
	got, err := spake2.NewVerifierFromValue(v[:spake2.VerifierW0Size], v[spake2.VerifierW0Size:])
	if err != nil {
		t.Fatal(err)
	}
	want, err := spake2.NewVerifierContext(20202021, salt, windowIterations)
	if err != nil {
		t.Fatal(err)
	}
	if got.W0.Cmp(want.W0) != 0 || !got.L.Equal(want.L) {
		t.Fatal("verifier does not round-trip")
	}
}

// TestRandomWindowCredentials: every passcode is a valid setup passcode
// and every discriminator 12 bits.
func TestRandomWindowCredentials(t *testing.T) {
	for range 1000 {
		p, err := randomPasscode()
		if err != nil || !setup.IsValidPasscode(p) {
			t.Fatalf("passcode %d %v", p, err)
		}
		d, err := randomDiscriminator()
		if err != nil || d > setup.MaxDiscriminator {
			t.Fatalf("discriminator %d %v", d, err)
		}
	}
}

var windowLine = regexp.MustCompile(`(?m)^(discriminator|passcode|manual code|QR code): +(\S+)$`)

// TestReferenceControllerWindow — the proof of window open after
// node/commissioner_control_live_test.go: the tool pairs the harness,
// opens an enhanced window, and a second tool on a fresh database (a
// second fabric) pairs the harness with the printed passcode and
// discriminator; the printed codes decode to the same credentials; revoke
// closes a window and answers a node without one; --basic fails before
// sending anything on a node without the BC feature. Not parallel.
func TestReferenceControllerWindow(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	tl.run("pair", "--code", manual, "--node-id", "0x4321")

	out := tl.run("window", "open", "0x4321", "--timeout", "300")
	vals := map[string]string{}
	for _, m := range windowLine.FindAllStringSubmatch(out, -1) {
		vals[m[1]] = m[2]
	}
	if len(vals) != 4 || !strings.Contains(out, "for 300 s") {
		t.Fatalf("window open:\n%s", out)
	}
	disc64, err := strconv.ParseUint(vals["discriminator"], 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	disc := uint16(disc64)
	pass64, err := strconv.ParseUint(vals["passcode"], 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	pass := uint32(pass64)
	qr, err := setup.DecodeQRCode(vals["QR code"])
	if err != nil || qr.Passcode != pass || qr.Discriminator != disc || qr.VendorID != 0xFFF1 || qr.ProductID != 0x8001 || qr.DiscoveryCaps != setup.DiscoveryOnIP {
		t.Fatalf("QR %+v %v", qr, err)
	}
	mc, err := setup.DecodeManualCode(vals["manual code"])
	if err != nil || mc.Passcode != pass || uint16(mc.ShortDiscriminator) != disc>>8 {
		t.Fatalf("manual %+v %v", mc, err)
	}

	// A second commissioner on a fresh fabric pairs with the window's
	// credentials; the harness advertises the window's discriminator.
	tl.r.mu.Lock()
	tl.r.window = &disc
	tl.r.operational[0x5555] = h.addr.Port
	tl.r.mu.Unlock()
	second := &tool{t: t, db: filepath.Join(t.TempDir(), "second.db"), r: tl.r, groupDst: h.addr}
	out = second.run("pair", "--passcode", vals["passcode"], "--discriminator", vals["discriminator"], "--node-id", "0x5555")
	if !strings.Contains(out, "paired node 21845") {
		t.Fatalf("second pair:\n%s", out)
	}
	fabrics, err := h.n.Fabrics(context.Background())
	if err != nil || len(fabrics) != 2 {
		t.Fatalf("harness fabrics %+v %v", fabrics, err)
	}
	tl.r.mu.Lock()
	tl.r.window = nil
	tl.r.mu.Unlock()

	// Revoke: an open window is closed, no window is not an error.
	tl.run("window", "open", "0x4321")
	if out := tl.run("window", "revoke", "0x4321"); !strings.Contains(out, "revoked") {
		t.Fatalf("revoke:\n%s", out)
	}
	if out := tl.run("window", "revoke", "0x4321"); !strings.Contains(out, "no commissioning window open") {
		t.Fatalf("revoke without a window:\n%s", out)
	}

	// The basic variant: the harness does not advertise BC.
	if out, err := tl.runIn("", "window", "open", "0x4321", "--basic"); err == nil || !strings.Contains(err.Error(), "basic commissioning method") {
		t.Fatalf("window open --basic: %v\n%s", err, out)
	}
	if _, err := tl.runIn("", "window", "close", "0x4321"); err == nil {
		t.Fatal("window close: no usage error")
	}
}

// TestWindowTimeout: --timeout after "window open" is the window's, in
// seconds, 1 to 65535; the rest is left for parseArgs.
func TestWindowTimeout(t *testing.T) {
	for _, c := range []struct {
		args []string
		rest string
		sec  uint
		ok   bool
	}{
		{[]string{"0x1"}, "0x1", 900, true},
		{[]string{"0x1", "--timeout", "60", "--basic"}, "0x1 --basic", 60, true},
		{[]string{"-timeout=120", "0x1"}, "0x1", 120, true},
		{[]string{"0x1", "--", "--timeout", "5"}, "0x1 -- --timeout 5", 900, true},
		{[]string{"0x1", "--timeout"}, "", 0, false},
		{[]string{"0x1", "--timeout", "0"}, "", 0, false},
		{[]string{"0x1", "--timeout=65536"}, "", 0, false},
	} {
		rest, sec, err := windowTimeout(c.args)
		if (err == nil) != c.ok || (c.ok && (strings.Join(rest, " ") != c.rest || sec != c.sec)) {
			t.Errorf("%q: %q %d %v", c.args, rest, sec, err)
		}
	}
}
