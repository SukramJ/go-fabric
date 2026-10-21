// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/controller/ca"
)

// blackhole is a channel whose peer never answers.
type blackhole struct {
	mgr       *exchange.Manager
	closed    chan struct{}
	closeOnce sync.Once
	closes    int
	mu        sync.Mutex
}

func (b *blackhole) Send(*net.UDPAddr, []byte) error { return nil }
func (b *blackhole) Exchanges() *exchange.Manager    { return b.mgr }
func (b *blackhole) PeerAddress() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv6zero, Zone: "ble-test"}
}
func (b *blackhole) Closed() <-chan struct{} { return b.closed }
func (b *blackhole) ClosedCause() error      { return errors.New("gone") }
func (b *blackhole) Close() error {
	b.mu.Lock()
	b.closes++
	b.mu.Unlock()
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// TestCommissionOverChannelClosesItOnFailure: PASE runs on the channel's
// manager, within the timeout; a failure closes the channel.
func TestCommissionOverChannelClosesItOnFailure(t *testing.T) {
	t.Parallel()
	b := &blackhole{closed: make(chan struct{})}
	mgr, err := exchange.NewManager(b, exchange.Config{Reliable: true})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	b.mgr = mgr
	authority, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := NewFabric(authority, FabricConfig{FabricID: 7, RootNodeID: 1, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	udp, err := exchange.NewManager(b, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	c := &Commissioner{ctx: Context{Exchanges: udp, Clock: exchange.SystemClock{}}, log: discard()}
	_, err = c.Commission(context.Background(), Options{Fabric: fabric, Passcode: 20202021, Channel: b, Timeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("PASE over a silent channel succeeded")
	}
	b.mu.Lock()
	closes := b.closes
	b.mu.Unlock()
	if closes != 1 {
		t.Fatalf("channel closed %d times", closes)
	}
}
