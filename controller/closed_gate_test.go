// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestMethodsAfterClose (review II, R2-P2-stop-01): every method that
// reaches what Close tears down answers a closed controller with the
// closed error or its zero result. Before, Nodes and Inventory
// dereferenced the registry Close had cleared and panicked.
func TestMethodsAfterClose(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.DCL.Offline = true })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}

	// The methods that return an error: the closed error.
	addr := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	if _, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{Address: addr}); !errors.Is(err, controller.ErrConfiguration) {
		t.Errorf("Pair(Address) after Close = %v, want ErrConfiguration", err)
	}
	if _, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{}); !errors.Is(err, controller.ErrConfiguration) {
		t.Errorf("Pair after Close = %v, want ErrConfiguration", err)
	}
	if _, err := c.Discover(ctx, discovery.Identifier{}, time.Second); !errors.Is(err, controller.ErrConfiguration) {
		t.Errorf("Discover after Close = %v, want ErrConfiguration", err)
	}
	if svc, err := c.VendorInfo(ctx); svc != nil || !errors.Is(err, controller.ErrConfiguration) {
		t.Errorf("VendorInfo after Close = %v, %v, want nil, ErrConfiguration", svc, err)
	}

	// The methods without an error: the zero result.
	if got := c.Nodes(); got != nil {
		t.Errorf("Nodes after Close = %v, want nil", got)
	}
	if got := c.Inventory(); got != nil {
		t.Errorf("Inventory after Close = %v, want nil", got)
	}
	if got := c.Registry(); got != nil {
		t.Error("Registry after Close is not nil")
	}
	if got := c.Peers(); got != nil {
		t.Error("Peers after Close is not nil")
	}
	if got := c.Commissioner(); got != nil {
		t.Error("Commissioner after Close is not nil")
	}
	if got := c.Groups(); got != nil {
		t.Error("Groups after Close is not nil")
	}
	if got := c.BDX(); got != nil {
		t.Error("BDX after Close is not nil")
	}
	if got := c.SoftwareUpdates(); got != nil {
		t.Error("SoftwareUpdates after Close is not nil")
	}
	if got := c.BorderRouters(); got != nil {
		t.Error("BorderRouters after Close is not nil")
	}
	if got := c.CameraRequestor(); got != nil {
		t.Error("CameraRequestor after Close is not nil")
	}
	if got := c.SessionDiagnostics(); got.AsOf != (time.Time{}) || got.Peers != nil || got.Sessions != nil {
		t.Errorf("SessionDiagnostics after Close = %+v, want the zero value", got)
	}
	if got := c.DiagnoseMDNS(); got != nil {
		t.Errorf("DiagnoseMDNS after Close = %v, want nil", got)
	}
	// A registration on a closed controller registers nothing; its
	// unsubscribe is still callable.
	c.OnDiagnosticEvent(func(diag.Event) {})()
	c.OnCameraSignal(func(controller.CameraSignal) {})()
}

// TestMethodsDuringClose (review II, R2-P2-stop-01): the methods that
// reach what Close tears down run concurrently with Close without a race
// or a panic — one goroutine per class, each calling at least once and
// until Close has returned. Run under -race.
func TestMethodsDuringClose(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.DCL.Offline = true })
	code := manualCode(t, dev)
	// A ctx already done: the work methods return at once, after they
	// read what Close clears.
	done, cancelDone := context.WithCancel(t.Context())
	cancelDone()
	addr := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	classes := map[string]func(){
		"work": func() {
			_, _ = c.Pair(done, code, controller.PairOptions{Address: addr})
			_, _ = c.Discover(done, discovery.Identifier{}, time.Second)
		},
		"components": func() {
			_, _, _, _ = c.Registry(), c.Peers(), c.Commissioner(), c.Groups()
			_, _, _, _ = c.BDX(), c.SoftwareUpdates(), c.BorderRouters(), c.CameraRequestor()
		},
		"lists": func() {
			_, _ = c.Nodes(), c.Inventory()
			_ = c.SessionDiagnostics()
			_ = c.DiagnoseMDNS()
		},
		"observers": func() {
			c.OnDiagnosticEvent(func(diag.Event) {})()
			c.OnCameraSignal(func(controller.CameraSignal) {})()
		},
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, call := range classes {
		wg.Go(func() {
			for {
				call()
				select {
				case <-stop:
					return
				default:
				}
			}
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := site.Run(ctx, c.Close)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
}

// TestCloseDuringClose (review II, R2-P2-stop-01): a Close called while
// another runs waits for it and returns nil; the two do not tear the stack
// down twice. Run under -race.
func TestCloseDuringClose(t *testing.T) {
	t.Parallel()
	site, _, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	err := site.Run(ctx, func(ctx context.Context) error {
		second := make(chan error, 1)
		go func() { second <- c.Close(ctx) }()
		return errors.Join(c.Close(ctx), <-second)
	})
	if err != nil {
		t.Fatalf("two concurrent Closes = %v, want nil", err)
	}
}
