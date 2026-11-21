// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest_test

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// TestRadio: a peripheral's radio advertises only what the node gives, a
// connection reaches the node's accept only while it serves, writes and
// the subscription arrive in order, an indication completes once the
// central received it, and the end of Serve drops every connection.
func TestRadio(t *testing.T) {
	p := &bletest.Peripheral{Address: "aa", ATTMTU: 100}
	r := p.Radio()
	if p.Radio() != r {
		t.Fatal("a second Radio call returned another radio")
	}
	c := bletest.NewCentral(p)
	var ads [][]byte
	c.SetDiscoveryCallback(func(_ ble.Peripheral, d []byte) { ads = append(ads, d) })
	if err := c.StartScanning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ads) != 0 || p.ServiceData() != nil {
		t.Fatal("a radio that does not advertise was seen")
	}
	data := btp.EncodeAdvertisementData(3840, 1, 2, false, false)
	if err := r.Advertise(context.Background(), contract.BLEAdvertisement{Data: data, Interval: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if len(ads) != 1 || !bytes.Equal(ads[0], data[7:]) {
		t.Fatalf("advertisements %x", ads)
	}
	if err := r.Advertise(context.Background(), contract.BLEAdvertisement{Data: []byte{2, 1, 6}}); err == nil {
		t.Fatal("an advertisement without Matter service data was taken")
	}
	if adv, ok := r.Advertising(); !ok || adv.Interval != 20*time.Millisecond {
		t.Fatalf("advertising %v %v", adv, ok)
	}

	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: "aa"}); !errors.Is(err, bletest.ErrNoService) {
		t.Fatalf("connect without a service: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan string, 8)
	var peer contract.BLEConnection
	served := make(chan error, 1)
	go func() {
		served <- r.Serve(ctx, []byte{9}, func(bc contract.BLEConnection) {
			peer = bc
			bc.OnWrite(func(v []byte) { events <- "write " + string(v) })
			bc.OnSubscribe(func(mtu int) {
				if mtu != 100 {
					events <- "bad mtu"
				}
				events <- "subscribe"
			})
			bc.OnUnsubscribe(func() { events <- "unsubscribe" })
		})
	}()
	for {
		if s, _ := r.Serving(); s {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("not serving")
		default:
			runtime.Gosched()
		}
	}
	if err := r.Serve(context.Background(), nil, func(contract.BLEConnection) {}); !errors.Is(err, bletest.ErrServing) {
		t.Fatalf("second Serve: %v", err)
	}
	conn, err := c.Connect(context.Background(), ble.Peripheral{Address: "aa"})
	if err != nil {
		t.Fatal(err)
	}
	if peer.Remote() == "" {
		t.Fatal("no remote")
	}
	if v, err := conn.ReadC3(context.Background()); err != nil || !bytes.Equal(v, []byte{9}) {
		t.Fatalf("C3 %x, %v", v, err)
	}
	got := make(chan []byte, 1)
	_ = conn.WriteC1(context.Background(), []byte("hs"))
	_ = conn.SubscribeC2(context.Background(), func(v []byte) { got <- v })
	for _, want := range []string{"write hs", "subscribe"} {
		if e := <-events; e != want {
			t.Fatalf("event %q, want %q", e, want)
		}
	}
	if err := peer.Indicate(context.Background(), []byte("ind")); err != nil {
		t.Fatal(err)
	}
	if v := <-got; string(v) != "ind" {
		t.Fatalf("indication %q", v)
	}
	_ = conn.UnsubscribeC2(context.Background())
	if e := <-events; e != "unsubscribe" {
		t.Fatalf("event %q", e)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.Disconnected():
	case <-waitCtx.Done():
		t.Fatal("the connection outlived Serve")
	}
	if err := peer.Indicate(context.Background(), []byte("x")); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("indicate on a dropped connection: %v", err)
	}
	// Every GATT operation of a dropped connection fails as a lost link.
	if err := conn.WriteC1(context.Background(), []byte("x")); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("write on a dropped connection: %v", err)
	}
	if err := conn.SubscribeC2(context.Background(), func([]byte) {}); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("subscribe on a dropped connection: %v", err)
	}
	if err := conn.UnsubscribeC2(context.Background()); !errors.Is(err, btp.ErrDisconnected) {
		t.Fatalf("unsubscribe on a dropped connection: %v", err)
	}
	if conn.ATTMTU() != 100 {
		t.Fatalf("ATT_MTU %d", conn.ATTMTU())
	}
	if ex, ok := conn.(ble.MTUExchange); !ok {
		t.Fatal("no ble.MTUExchange")
	} else {
		<-ex.ATTMTUExchanged()
	}
	_ = peer.Disconnect()
	_ = conn.Disconnect()
	<-peer.Disconnected()
	if err := r.StopAdvertising(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, stops := r.Advertisements(); stops != 1 {
		t.Fatalf("%d stops", stops)
	}
	if _, ok := r.Advertising(); ok {
		t.Fatal("still advertising")
	}
}

// TestRadioAcceptsOneAtATime: centrals that connect at once reach the
// node's accept one at a time, as contract.GATTService.Serve promises; and
// Serve does not return while an accept runs, nor accepts once it
// returned (review II, R2-P6-conc-2051).
func TestRadioAcceptsOneAtATime(t *testing.T) {
	t.Parallel()
	per := &bletest.Peripheral{Address: "AA:BB:CC:00:00:51"}
	r := per.Radio()
	c := bletest.NewCentral(per)
	svc, err := r.RegisterService(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var in, overlaps atomic.Int32
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = svc.Serve(ctx, func(contract.BLEConnection) {
			if in.Add(1) > 1 {
				overlaps.Add(1)
			}
			if hold.Load() {
				entered <- struct{}{}
				<-release
			} else {
				for range 100 { // the other connects' chance to enter now
					runtime.Gosched()
				}
			}
			in.Add(-1)
		})
	}()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	for {
		if s, _ := r.Serving(); s {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("not serving")
		default:
			runtime.Gosched()
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := c.Connect(context.Background(), ble.Peripheral{Address: per.Address}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := overlaps.Load(); n > 0 {
		t.Fatalf("accept ran concurrently: %d overlapping calls", n)
	}

	// Serve's end waits for the accept in flight.
	hold.Store(true)
	wg.Go(func() { _, _ = c.Connect(context.Background(), ble.Peripheral{Address: per.Address}) })
	<-entered
	cancel()
	for range 100 { // Serve's chance to see ctx end and return early
		runtime.Gosched()
	}
	select {
	case <-serveDone:
		t.Fatal("Serve returned while an accept ran")
	default:
	}
	close(release)
	wg.Wait()
	<-serveDone
	if _, err := c.Connect(context.Background(), ble.Peripheral{Address: per.Address}); !errors.Is(err, bletest.ErrNoService) {
		t.Fatalf("Connect after Serve returned: %v, want ErrNoService", err)
	}
}

// TestRadioHoldsAConnectUntilServed: a central that connects after
// RegisterService returned and before Serve runs is held — bounded by its
// context — and accepted once Serve runs, as contract.GATTService.Serve
// obliges (the node registers, advertises and serves on another
// goroutine, bridge/ble.go); a radio with no registration, or whose
// service's Serve returned, refuses at once (review II P3b-R, candidate 4).
func TestRadioHoldsAConnectUntilServed(t *testing.T) {
	t.Parallel()
	per := &bletest.Peripheral{Address: "AA:BB:CC:00:00:52"}
	r := per.Radio()
	c := bletest.NewCentral(per)
	at := ble.Peripheral{Address: per.Address}
	if _, err := c.Connect(context.Background(), at); !errors.Is(err, bletest.ErrNoService) {
		t.Fatalf("connect without a registration: %v, want ErrNoService", err)
	}
	svc, err := r.RegisterService(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gone, cancelGone := context.WithCancel(context.Background())
	cancelGone()
	if _, err := c.Connect(gone, at); !errors.Is(err, context.Canceled) {
		t.Fatalf("connect between RegisterService and Serve, its context ended: %v, want context.Canceled", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		conn ble.Connection
		err  error
	}
	held := make(chan result, 1)
	go func() {
		conn, err := c.Connect(ctx, at)
		held <- result{conn, err}
	}()
	accepted := make(chan struct{}, 1)
	serveCtx, stop := context.WithCancel(ctx)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- svc.Serve(serveCtx, func(contract.BLEConnection) { accepted <- struct{}{} })
	}()
	res := <-held
	if res.err != nil {
		t.Fatalf("connect between RegisterService and Serve: %v, want it held and accepted", res.err)
	}
	select {
	case <-accepted:
	default:
		t.Fatal("the connect returned before the node's accept ran")
	}
	stop()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(ctx, at); !errors.Is(err, bletest.ErrNoService) {
		t.Fatalf("connect after Serve returned: %v, want ErrNoService", err)
	}
}
