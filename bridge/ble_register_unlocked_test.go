// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/controller/ble/bletest"
)

// slowGATT is a GATT peripheral whose platform takes its time to register
// the Matter GATT service: RegisterService runs during, then waits for
// release — or for its ctx when honourCtx is set — before the radio
// registers it.
type slowGATT struct {
	*bletest.Radio
	honourCtx bool
	during    func(ctx context.Context)
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newSlowGATT(r *bletest.Radio, honourCtx bool) *slowGATT {
	return &slowGATT{Radio: r, honourCtx: honourCtx, entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *slowGATT) RegisterService(ctx context.Context, c3 []byte) (contract.GATTService, error) {
	g.once.Do(func() {
		if g.during != nil {
			g.during(ctx)
		}
		close(g.entered)
	})
	var ended <-chan struct{}
	if g.honourCtx {
		ended = ctx.Done()
	}
	select {
	case <-g.release:
	case <-ended:
		return nil, ctx.Err()
	}
	return g.Radio.RegisterService(ctx, c3)
}

// TestStartBLERegistersWithoutTheLock (review II, phase 5b): StartBLE calls the
// host's RegisterService without the BLE side's lock held. A StopBLE while
// a registration runs ends it instead of waiting for it; a service the
// platform registers after StopBLE is unregistered, so the next StartBLE
// registers it again; a host whose RegisterService calls back into the
// bridge's BLE surface does not deadlock. Before, the registration ran
// under the lock: StopBLE and Stop waited for it, and a call back from it
// never returned — each case below then hangs until the test binary's
// timeout, the regression's signal.
func TestStartBLERegistersWithoutTheLock(t *testing.T) {
	t.Parallel()
	t.Run("StopBLE ends the registration", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:01:01"}
		g := newSlowGATT(p.Radio(), true)
		h := newConnHarness(t, Config{Listen: "127.0.0.1:0", GATT: g})
		started := make(chan error, 1)
		go func() { started <- h.bridge.StartBLE(context.Background(), bleWindow) }()
		<-g.entered
		// The registration never ends on its own (release stays shut):
		// StopBLE returns only because it ends the registration.
		if stopErr := h.bridge.StopBLE(context.Background()); stopErr != nil {
			t.Errorf("StopBLE: %v", stopErr)
		}
		if err := <-started; !errors.Is(err, context.Canceled) {
			t.Errorf("StartBLE whose registration StopBLE ended = %v, want the registration's context.Canceled", err)
		}
		if _, advertising := p.Radio().Advertising(); advertising {
			t.Error("the radio advertises")
		}
	})
	t.Run("a late registration is unregistered", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:01:02"}
		g := newSlowGATT(p.Radio(), false)
		h := newConnHarness(t, Config{Listen: "127.0.0.1:0", GATT: g})
		started := make(chan error, 1)
		go func() { started <- h.bridge.StartBLE(context.Background(), bleWindow) }()
		<-g.entered
		// The platform ignores the registration's ctx: StopBLE waits for
		// it until its own ctx ends, not for the lock.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if stopErr := h.bridge.StopBLE(ctx); !errors.Is(stopErr, context.Canceled) {
			t.Errorf("StopBLE while the platform still registers = %v, want its context's end", stopErr)
		}
		close(g.release)
		if err := <-started; !errors.Is(err, errBLEStoppedWhileRegistering) {
			t.Errorf("StartBLE stopped while registering = %v, want errBLEStoppedWhileRegistering", err)
		}
		if _, advertising := p.Radio().Advertising(); advertising {
			t.Error("the radio advertises a service StopBLE took down")
		}
		// The late service was unregistered: a new StartBLE registers it.
		if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
			t.Fatalf("StartBLE after the late registration: %v", err)
		}
		waitFor(t, "the GATT service served", func() bool { s, _ := p.Radio().Serving(); return s })
	})
	t.Run("a call back from the registration", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:01:03", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
		g := newSlowGATT(p.Radio(), true)
		close(g.release)
		h := newConnHarness(t, Config{Listen: "127.0.0.1:0", GATT: g})
		g.during = func(ctx context.Context) {
			if err := h.bridge.WithdrawBLE(ctx); err != nil {
				t.Errorf("WithdrawBLE from inside RegisterService: %v", err)
			}
		}
		if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
			t.Fatalf("StartBLE: %v", err)
		}
		waitFor(t, "the GATT service served", func() bool { s, _ := p.Radio().Serving(); return s })
		waitFor(t, "advertised", func() bool { _, a := p.Radio().Advertising(); return a })
	})
}
