// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// Node.OpenBasicWindow (issue #58): the node's own window with per-window
// credentials, advertised CM=1 under the window's discriminator, and the
// configured credentials back once it closes.

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// randomCredentials draws a window's credentials as the ephemeral-window
// host does: random discriminator (not the configured 3840), passcode and
// salt.
func randomCredentials(t *testing.T, timeout time.Duration) node.WindowCredentials {
	t.Helper()
	creds := node.WindowCredentials{Iterations: 1000, Timeout: timeout}
	var err error
	for creds.Discriminator == 0 || creds.Discriminator == 3840 {
		if creds.Discriminator, err = bridge.RandomDiscriminator(); err != nil {
			t.Fatal(err)
		}
	}
	for creds.Passcode == 0 || creds.Passcode == testPasscode {
		if creds.Passcode, err = bridge.RandomPasscode(); err != nil {
			t.Fatal(err)
		}
	}
	if creds.Salt, err = bridge.RandomSalt(); err != nil {
		t.Fatal(err)
	}
	return creds
}

// waitRecord polls the commissionable record until want holds.
func waitRecord(t *testing.T, adv *mdns.Noop, what string, want func(discriminator, mode uint64, ok bool) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, m, ok := commissionableRecord(adv)
		if want(d, m, ok) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("commissionable record D=%d CM=%d present=%v; want %s", d, m, ok, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startedNode starts an uncommissioned node advertising on adv.
func startedNode(t *testing.T, adv *mdns.Noop) *node.Node {
	t.Helper()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return n
}

// TestOpenBasicWindowPairsWithItsCredentials: a commissioner holding only
// the window's random credentials finds the node under the window's
// discriminator (CM=1) and commissions it.
func TestOpenBasicWindowPairsWithItsCredentials(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startedNode(t, adv)
	creds := randomCredentials(t, 0)
	if err := n.OpenBasicWindow(context.Background(), creds); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	if d, m, ok := commissionableRecord(adv); !ok || d != uint64(creds.Discriminator) || m != 1 {
		t.Fatalf("commissionable record D=%d CM=%d present=%v; want D=%d CM=1", d, m, ok, creds.Discriminator)
	}
	if !n.CommissioningWindow().IsOwnWindow() {
		t.Error("the basic window is not the node's own window")
	}
	opts := commissionerOptions()
	opts.Passcode, opts.Discriminator, opts.Advertiser = creds.Passcode, creds.Discriminator, adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := c.Commission(context.Background()); err != nil {
		t.Fatalf("Commission with the window's credentials: %v", err)
	}
	fabrics, err := n.FabricInfos(context.Background())
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("Fabrics() = %+v, %v; want the commissioner's", fabrics, err)
	}
	// CommissioningComplete closed the window; the node has a fabric, so
	// nothing is commissionable any more.
	waitRecord(t, adv, "none after the commissioning", func(_, _ uint64, ok bool) bool { return !ok })
	if n.CommissioningWindow().IsOpen() {
		t.Error("the window is still open after CommissioningComplete")
	}
}

// TestOpenBasicWindowTimeoutRestoresConfigured: on an uncommissioned node
// the basic window's record carries its discriminator; after its timeout
// the record is the configured one again and PASE runs against the
// configured passcode.
func TestOpenBasicWindowTimeoutRestoresConfigured(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startedNode(t, adv)
	if d, m, ok := commissionableRecord(adv); !ok || d != 3840 || m != 1 {
		t.Fatalf("own window record D=%d CM=%d present=%v; want D=3840 CM=1", d, m, ok)
	}
	creds := randomCredentials(t, 300*time.Millisecond)
	if err := n.OpenBasicWindow(context.Background(), creds); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	if d, m, ok := commissionableRecord(adv); !ok || d != uint64(creds.Discriminator) || m != 1 {
		t.Fatalf("basic window record D=%d CM=%d present=%v; want D=%d CM=1", d, m, ok, creds.Discriminator)
	}
	waitRecord(t, adv, "the configured D=3840 CM=1 after the timeout", func(d, m uint64, ok bool) bool { return ok && d == 3840 && m == 1 })
	if !n.CommissioningWindow().IsOwnWindow() {
		t.Error("the node's own window is not open after the basic window's timeout")
	}
	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := c.Commission(context.Background()); err != nil {
		t.Fatalf("Commission with the configured credentials after the basic window: %v", err)
	}
}

// TestOpenBasicWindowOnCommissionedNode: on a commissioned node the basic
// window is the only commissionable record while it is open, and none is
// left after its timeout.
func TestOpenBasicWindowOnCommissionedNode(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	var (
		mu          sync.Mutex
		transitions []node.WindowSnapshot
		recording   bool
	)
	done := make(chan struct{}, 8)
	cfg.Observers.OnWindowTransition = func(s node.WindowSnapshot) error {
		mu.Lock()
		defer mu.Unlock()
		if recording {
			transitions = append(transitions, s)
			done <- struct{}{}
		}
		return nil
	}
	n, _, _ := commissioned(t, cfg)
	waitRecord(t, adv, "none after the commissioning", func(_, _ uint64, ok bool) bool { return !ok })
	mu.Lock()
	recording = true
	mu.Unlock()

	creds := randomCredentials(t, 300*time.Millisecond)
	if err := n.OpenBasicWindow(context.Background(), creds); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	if d, m, ok := commissionableRecord(adv); !ok || d != uint64(creds.Discriminator) || m != 1 {
		t.Fatalf("basic window record D=%d CM=%d present=%v; want D=%d CM=1", d, m, ok, creds.Discriminator)
	}
	waitRecord(t, adv, "none after the timeout", func(_, _ uint64, ok bool) bool { return !ok })
	for range 2 {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("OnWindowTransition did not run twice")
		}
	}
	mu.Lock()
	got := slices.Clone(transitions)
	mu.Unlock()
	if len(got) != 2 || !got[0].Open || got[0].CommissioningMode != 1 || got[0].Discriminator != creds.Discriminator || got[1].Open || got[1].CommissioningMode != 0 {
		t.Errorf("transitions %+v, want open (CM=1, D=%d) then closed", got, creds.Discriminator)
	}
}

// TestOpenBasicWindowRefusals: invalid credentials are refused before
// anything changes, and an administrator's open window is not replaced.
func TestOpenBasicWindowRefusals(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startedNode(t, adv)
	ctx := context.Background()
	for name, mutate := range map[string]func(*node.WindowCredentials){
		"discriminator": func(c *node.WindowCredentials) { c.Discriminator = 0x1000 },
		"passcode":      func(c *node.WindowCredentials) { c.Passcode = 11111111 },
		"iterations":    func(c *node.WindowCredentials) { c.Iterations = 999 },
		"salt":          func(c *node.WindowCredentials) { c.Salt = c.Salt[:15] },
		"timeout":       func(c *node.WindowCredentials) { c.Timeout = -time.Second },
	} {
		creds := randomCredentials(t, 0)
		mutate(&creds)
		if err := n.OpenBasicWindow(ctx, creds); !errors.Is(err, node.ErrInvalidWindowCredentials) {
			t.Errorf("%s: OpenBasicWindow = %v, want ErrInvalidWindowCredentials", name, err)
		}
	}
	if d, _, ok := commissionableRecord(adv); !ok || d != 3840 {
		t.Errorf("record after refused opens D=%d present=%v; want the configured 3840", d, ok)
	}

	if err := n.CommissioningWindow().OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180, Discriminator: 100, IsUncommissioned: true}); err != nil {
		t.Fatalf("administrator OpenWindow: %v", err)
	}
	if err := n.OpenBasicWindow(ctx, randomCredentials(t, 0)); !errors.Is(err, bridge.ErrAdministratorWindowOpen) {
		t.Errorf("OpenBasicWindow over an administrator's window = %v, want ErrAdministratorWindowOpen", err)
	}
}

// TestOwnWindowOpenEndsBasicWindow: a direct OpenOwnWindow while a basic
// window is open ends the basic window — the restarted window is
// advertised under the configured discriminator and PASE runs against the
// configured passcode.
func TestOwnWindowOpenEndsBasicWindow(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	n := startedNode(t, adv)
	ctx := context.Background()
	creds := randomCredentials(t, 0)
	if err := n.OpenBasicWindow(ctx, creds); err != nil {
		t.Fatalf("OpenBasicWindow: %v", err)
	}
	if d, _, ok := commissionableRecord(adv); !ok || d != uint64(creds.Discriminator) {
		t.Fatalf("basic window record D=%d present=%v; want D=%d", d, ok, creds.Discriminator)
	}
	if err := n.CommissioningWindow().OpenOwnWindow(ctx, 0); err != nil {
		t.Fatalf("OpenOwnWindow: %v", err)
	}
	if d, m, ok := commissionableRecord(adv); !ok || d != 3840 || m != 1 {
		t.Fatalf("record after OpenOwnWindow D=%d CM=%d present=%v; want the configured D=3840 CM=1", d, m, ok)
	}
	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := c.Commission(ctx); err != nil {
		t.Fatalf("Commission with the configured credentials after OpenOwnWindow: %v", err)
	}
}
