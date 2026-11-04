// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
)

// TestDiscoveryCancelWithRealDeviceParity is a port of
// packages/node/test/behavior/system/controller/discovery/DiscoveryCancelTest.ts:568-679
// at 862ad75f ("with a real device", interruptMidPase): a commissioning
// race over the controller's DNS-SD transport against a fabrictest device
// whose PASE answers are held once the controller opens PASE
// (fabrictest.Network.Filter drops the device's datagrams to the
// controller until released; MRP brings them again, as the matter.js
// router's held packets are routed late). The commissioning is
// interrupted while PASE is in progress — by the race's Cancel, by the
// caller's context, by every scanner failing — and then released; the
// outcome is the interruption's reason, and the device is not
// commissioned: its fail-safe is not armed, it holds no fabric, and the
// controller's registry commissioned nothing.
func TestDiscoveryCancelWithRealDeviceParity(t *testing.T) {
	t.Parallel()
	reason := errors.New("caller gave up")
	t.Run("does not commission a device whose PASE was in progress when the commissioning was canceled", func(t *testing.T) {
		t.Parallel()
		err := interruptMidPASE(t, func(m midPASE) { m.session.Cancel(reason) })
		if !errors.Is(err, reason) {
			t.Fatalf("outcome %v, want the cancel reason", err)
		}
	})
	t.Run("does not commission a device whose PASE was in progress when the caller aborted", func(t *testing.T) {
		t.Parallel()
		err := interruptMidPASE(t, func(m midPASE) { m.abort(reason) })
		if !errors.Is(err, reason) {
			t.Fatalf("outcome %v, want the abort reason", err)
		}
	})
	t.Run("does not commission a device whose PASE was in progress when every scanner failed", func(t *testing.T) {
		t.Parallel()
		failure := errors.New("scanner broke")
		err := interruptMidPASE(t, func(m midPASE) {
			for _, s := range m.scanners {
				s.fail <- failure
			}
		})
		var agg *discovery.AggregateError
		if !errors.As(err, &agg) || !errors.Is(err, failure) {
			t.Fatalf("outcome %v, want an AggregateError caused by the scanner failure", err)
		}
	})
}

// midPASE is MidPaseInterruption (:599-603): the handles a test interrupts
// a commissioning with.
type midPASE struct {
	session  *discovery.Session[*client.Node]
	abort    context.CancelCauseFunc
	scanners []*failingScanner
}

// failingScanner is FailingScanner of discovery-helpers.ts: a transport
// that scans as the one it wraps until fail is sent an error, then ends
// its search with it.
type failingScanner struct {
	discovery.Transport
	fail chan error
}

func (f *failingScanner) FindContinuously(ctx context.Context, id discovery.Identifier, found func(discovery.Device)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Transport.FindContinuously(ctx, id, found) }()
	select {
	case err := <-f.fail:
		cancel()
		<-done
		return err
	case err := <-done:
		return err
	}
}

// interruptMidPASE is interruptMidPase (:605-679): it commissions a real
// device through a commissioning race, interrupts the race once the
// device's first PASE answer is held, releases the held answers and
// returns the race's error after checking the device was not commissioned.
func interruptMidPASE(t *testing.T, interrupt func(midPASE)) error {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	dev := site.AddDevice(fabrictest.Topology{})
	c := site.AddController(fabrictest.ControllerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Hold the device's answers once the controller opens PASE.
	held := make(chan struct{})
	var once sync.Once
	devAddr, ctlAddr := dev.Addr(), c.Addr()
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		if !d.From.IP.Equal(devAddr.IP) || !d.To.IP.Equal(ctlAddr.IP) || d.From.Port == 5353 || d.To.Port == 5353 {
			return true
		}
		once.Do(func() { close(held) })
		return false
	})

	sc := discovery.NewScanner(fabrictestseam.Names(c))
	defer sc.Close()
	scanner := &failingScanner{Transport: discovery.IPTransport(sc), fail: make(chan error, 1)}
	reg := c.Registry()
	var outcome error
	if err := site.Run(ctx, func(ctx context.Context) error {
		actx, abort := context.WithCancelCause(ctx)
		defer abort(nil)
		s := discovery.CommissioningRace(actx, []discovery.Transport{scanner}, discovery.SessionOptions{
			Identifier: discovery.ByLongDiscriminator(dev.Discriminator()), Timeout: 90 * time.Second, Clock: site.Clock(),
		}, discovery.Identity{}, func(actx context.Context, cand discovery.Candidate, win func() bool) (*client.Node, error) {
			n, err := reg.ForDevice(actx, cand.Device, "")
			if err != nil {
				return nil, err
			}
			return n, reg.Commission(actx, n, client.CommissionOptions{Options: commissioner.Options{
				Passcode:             dev.Passcode(),
				ClaimNodeIDAfterPASE: func() (uint64, bool) { return 0, win() },
			}})
		})
		select {
		case <-held:
		case <-ctx.Done():
			return ctx.Err()
		}
		interrupt(midPASE{session: s, abort: abort, scanners: []*failingScanner{scanner}})
		site.Net().Filter(nil)
		_, outcome = s.Wait(ctx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if outcome == nil {
		t.Fatal("the interrupted commissioning succeeded")
	}
	// Let the attempt settle (:668).
	if err := site.Step(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if dev.Node().Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the device's fail-safe is armed")
	}
	if infos, err := dev.Node().FabricInfos(ctx); err != nil || len(infos) != 0 {
		t.Fatalf("the device holds fabrics %v (%v)", infos, err)
	}
	if got := reg.Commissioned(); len(got) != 0 {
		t.Fatalf("the controller commissioned %d node(s)", len(got))
	}
	return outcome
}
