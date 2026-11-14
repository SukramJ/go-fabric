// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// A rejoin attempt of the committed network holds the driver alone: a
// fail-safe expiry does not release it, a command cancels it and takes
// its turn, and a disabled interface stops the rejoin (review II,
// R2-P3-dr-net-05, R2-P3-dr-net-06, R2-P3-dr-net-07).

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// heldDriver is a contract.WiFiDriver whose joins wait until the test
// releases them (Success) or their context ends (UnknownError, counted as
// cancelled). It records the most joins that ran at once.
type heldDriver struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	cancelled   int
	link        []byte
	started     chan string
	release     chan struct{}
}

func newHeldDriver() *heldDriver {
	return &heldDriver{started: make(chan string, 16), release: make(chan struct{})}
}

func (d *heldDriver) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (d *heldDriver) ConnectNetwork(ctx context.Context, ssid, _ []byte) (status uint8, debugText string, errorValue int32) {
	d.mu.Lock()
	d.inFlight++
	d.maxInFlight = max(d.maxInFlight, d.inFlight)
	d.mu.Unlock()
	d.started <- string(ssid)
	ok := false
	select {
	case <-d.release:
		ok = true
	case <-ctx.Done():
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inFlight--
	if !ok {
		d.cancelled++
		return uint8(netcommdef.NetworkCommissioningStatusUnknownError), "the connect did not complete in time", 0
	}
	d.link = bytes.Clone(ssid)
	return 0, "", 0
}

func (d *heldDriver) stats() (maxInFlight, cancelled int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.maxInFlight, d.cancelled
}

// awaitStarted waits for the driver's next join to start, bounded by the
// test's deadline.
func awaitStarted(t *testing.T, d *heldDriver) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case ssid := <-d.started:
		return ssid
	case <-ctx.Done():
		t.Fatal("no join started")
		return ""
	}
}

// rejoinRunning commits "home" without joining it and lets the rejoin
// that CommissioningComplete starts reach the driver, where it waits.
// The returned channel closes once the clock's callback returned.
func rejoinRunning(t *testing.T, w *WiFiNetworkCommissioning, d *heldDriver, clk *clocktest.Fake, armed *atomic.Bool) <-chan struct{} {
	t.Helper()
	armed.Store(true)
	if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("home"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	w.OnCommissioningComplete(t.Context())
	armed.Store(false)
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		clk.Advance(0)
	}()
	if got := awaitStarted(t, d); got != "home" {
		t.Fatalf("the rejoin joined %q, want home", got)
	}
	return advanced
}

// awaitClosed waits for ch, bounded by the test's deadline.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("%s did not happen", what)
	}
}

// TestAFailSafeExpiryLeavesARunningRejoinTheDriver: a fail-safe that
// expires while a rejoin attempt runs does not hand the driver to a
// command — no two joins run at once.
func TestAFailSafeExpiryLeavesARunningRejoinTheDriver(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var armed atomic.Bool
	d := newHeldDriver()
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	advanced := rejoinRunning(t, w, d, clk, &armed)

	armed.Store(true)
	w.OnFailSafeExpired() // ArmFailSafe(0) while the attempt runs
	answered := make(chan error, 1)
	go func() {
		_, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("home")})
		answered <- err
	}()
	// The command either answers at once or reaches the driver; either way
	// the rejoin's join must have ended before the command's started.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-answered:
	case <-d.started:
	case <-ctx.Done():
		t.Fatal("the command neither answered nor reached the driver")
	}
	if most, _ := d.stats(); most > 1 {
		t.Fatalf("%d joins ran at once after the fail-safe expired during a rejoin attempt", most)
	}
	close(d.release)
	awaitClosed(t, advanced, "the end of the rejoin attempt")
}
