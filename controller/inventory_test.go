// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestInventoryKeepsThePairedCode: with Config.KeepOnboardingCodes the
// code Pair was called with is the node's inventory entry, as paired; a
// code of another device is refused and leaves it; the device's QR code
// replaces it with both forms; a cleared code is gone.
func TestInventoryKeepsThePairedCode(t *testing.T) {
	t.Parallel()
	logs := &lockedBuffer{}
	site, dev, c, n := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.KeepOnboardingCodes = true
		cfg.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	manual := manualCode(t, dev)
	if s := logs.String() + fmt.Sprintf("%+v %#v %v", c.DiagnosticEvents(), c.Inventory(), c.Inventory()); strings.Contains(s, manual) {
		t.Fatal("the log, the trace or a formatted inventory holds the code")
	}
	inv := c.Inventory()
	if len(inv) != 1 {
		t.Fatalf("%d entries", len(inv))
	}
	e := inv[0]
	// An entry serialised or logged by a host — as a JSON API would —
	// carries no code either; the node pointer is left out of the copy,
	// it is not what a host serialises.
	flat := e
	flat.Node = nil
	var hostLog bytes.Buffer
	slog.New(slog.NewJSONHandler(&hostLog, nil)).Info("inventory", slog.Any("entries", []controller.InventoryEntry{flat}), slog.Any("holder", struct{ Entry controller.InventoryEntry }{flat}))
	b, err := json.Marshal(struct{ Entries []controller.InventoryEntry }{[]controller.InventoryEntry{flat}})
	if err != nil {
		t.Fatal(err)
	}
	if s := hostLog.String() + string(b) + fmt.Sprintf("%+v %#v", struct{ E controller.InventoryEntry }{flat}, &flat); strings.Contains(s, manual) {
		t.Fatalf("a serialised or logged entry holds the code: %s", s)
	}
	if !strings.Contains(string(b), `"ManualCode":"redacted"`) {
		t.Fatalf("the entry's JSON: %s", b)
	}
	if e.Node != n || !e.HasCode || e.ManualCode.Reveal() != manual || !e.QRCode.IsZero() || e.Source != client.OnboardingPaired || e.StoredAt.IsZero() {
		t.Fatalf("paired entry: has %v manual %q qr %q source %d at %v", e.HasCode, e.ManualCode, e.QRCode, e.Source, e.StoredAt)
	}
	if addr, _ := n.PeerAddress(); e.NodeID != addr.NodeID || e.VendorID == 0 || e.VendorName == "" || e.ProductName == "" {
		t.Fatalf("identity: node %d vendor 0x%04X %q product %q", e.NodeID, e.VendorID, e.VendorName, e.ProductName)
	}

	other, err := setup.QRCode(setup.Payload{
		VendorID: e.VendorID + 1, ProductID: e.ProductID, Discriminator: dev.Discriminator(), Passcode: dev.Passcode(), DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.SetOnboardingCode(other); !errors.Is(err, client.ErrOnboardingMismatch) {
		t.Fatalf("another vendor's code: %v", err)
	}
	if got := c.Inventory()[0]; got.ManualCode.Reveal() != manual || got.Source != client.OnboardingPaired {
		t.Fatalf("a refused code changed the entry: %q source %d", got.ManualCode, got.Source)
	}

	qr, err := setup.QRCode(setup.Payload{
		VendorID: e.VendorID, ProductID: e.ProductID, Discriminator: dev.Discriminator(), Passcode: dev.Passcode(), DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.SetOnboardingCode(qr); err != nil {
		t.Fatal(err)
	}
	got := c.Inventory()[0]
	if got.QRCode.Reveal() != qr || got.ManualCode.Reveal() != manual || got.Source != client.OnboardingAttached || got.Checks&client.OnboardingCheckedVendor == 0 || got.Checks&client.OnboardingCheckedProduct == 0 {
		t.Fatalf("attached entry: qr %q manual %q source %d checks %b", got.QRCode, got.ManualCode, got.Source, got.Checks)
	}

	if err := n.ClearOnboardingCode(); err != nil {
		t.Fatal(err)
	}
	if got := c.Inventory()[0]; got.HasCode || !got.QRCode.IsZero() || !got.ManualCode.IsZero() {
		t.Fatalf("cleared entry still has a code")
	}

	// Forget deletes the code with the node.
	if _, err := n.SetOnboardingCode(qr); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := site.Run(ctx, func(ctx context.Context) error { return c.Registry().Forget(ctx, n) }); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.OnboardingCode(); ok || len(c.Inventory()) != 0 {
		t.Fatalf("forgotten, and the code or the entry is still there")
	}
}

// lockedBuffer is a log sink the stack's goroutines write and the test
// reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestInventoryWithoutKeeping: by default Pair keeps no code; the node is
// listed without one.
func TestInventoryWithoutKeeping(t *testing.T) {
	t.Parallel()
	_, _, c, n := sitePair(t, fabrictest.Topology{}, nil)
	inv := c.Inventory()
	if len(inv) != 1 || inv[0].Node != n || inv[0].HasCode || !inv[0].ManualCode.IsZero() {
		t.Fatalf("inventory without keeping: %+v", inv)
	}
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("a code was kept without Config.KeepOnboardingCodes")
	}
}
