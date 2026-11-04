// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// A port of matter.js packages/node/test/behavior/system/icd/IcdClientTest.ts
// at 862ad75f, the operation cases whose outcome a Go test observes
// without matter.js's transaction model: a forget begun by a Check-In
// listener (:1285-1304), unregister() over a key refresh waiting for its
// response (:1404-1429), the registration operations joined, refused and
// abandoned (:1433-1492), forget() over an unregister() past its local
// clear (:1803-1818), the last Check-In time cleared with the
// registration (:1838-1854) and a registration after a forget() ended a
// refresh (:1895-1917). The operations' takeover rules are
// controller/client icd.go's icdTakesOver (IcdClient.ts:944-960).

// icdLog records the controller's log of an ICD pair.
func icdLogPair(t *testing.T) (*icdPair, *levelLog) {
	t.Helper()
	log := &levelLog{}
	return newICDPair(t, icdPairOptions{controller: fabrictest.ControllerOptions{Logger: slog.New(log)}}), log
}

// icdWarnings are the warnings of the ICD client logged.
func (h *levelLog) icdWarnings() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i := range h.recs {
		if h.recs[i].Level >= slog.LevelWarn && strings.Contains(h.recs[i].Message, "icd") {
			out = append(out, h.recs[i].Message)
		}
	}
	return out
}

// started runs fn on its own goroutine; done reports its end and err its
// error.
type started struct {
	done atomic.Bool
	err  atomic.Value
}

func start(fn func() error) *started {
	s := &started{}
	go func() {
		if err := fn(); err != nil {
			s.err.Store(err)
		}
		s.done.Store(true)
	}()
	return s
}

func (s *started) error() error {
	if v := s.err.Load(); v != nil {
		return v.(error)
	}
	return nil
}

func TestIcdClientOperationsParity(t *testing.T) {
	t.Parallel()

	t.Run("starts no refresh after a checkedIn listener began forget()", func(t *testing.T) {
		t.Parallel()
		p, log := icdLogPair(t)
		p.register(t, uncovered)
		p.requestRefreshOnNextCheckIn(t)
		var forgetting atomic.Pointer[started]
		var once atomic.Bool
		p.icd().OnCheckIn(func(client.ICDCheckIn) {
			if once.CompareAndSwap(false, true) {
				forgetting.Store(start(func() error { return p.icd().Forget(parityCtx(t)) }))
			}
		})
		p.wakeDevice(t)
		p.eventually(t, "the forget", func() bool { f := forgetting.Load(); return f != nil && f.done.Load() })
		p.advance(t, advanceStep)
		if p.icd().Registered() {
			t.Fatal("still registered")
		}
		if w := log.icdWarnings(); len(w) != 0 {
			t.Fatalf("warnings %v, want none", w)
		}
	})

	t.Run("unregister() abandons a key refresh that waits for its response", func(t *testing.T) {
		t.Parallel()
		p, log := icdLogPair(t)
		p.register(t, uncovered)
		p.requestRefreshOnNextCheckIn(t)
		oldKey := p.deviceKey(t)
		dev := p.pair.Device.Addr()
		var drop atomic.Bool
		drop.Store(true)
		p.site.Net().Filter(func(d fabrictest.Datagram) bool {
			return !drop.Load() || !sameUDP(d.From, dev) || bytes.Equal(p.deviceKeyNoT(), oldKey)
		})
		p.wakeDevice(t)
		p.eventually(t, "the device's new key", func() bool { return !bytes.Equal(p.deviceKeyNoT(), oldKey) })
		ending := start(func() error { return p.icd().Unregister(parityCtx(t)) })
		p.eventually(t, "the refresh abandoned", func() bool {
			return log.count(slog.LevelDebug, "matter.controller.client.icd_refresh_abandoned") > 0
		})
		drop.Store(false)
		p.eventually(t, "the unregister", ending.done.Load)
		if _, ok := p.icd().Registration(); ok || p.icd().Registered() {
			t.Fatal("still registered")
		}
		if n := len(p.deviceServerRegistrations(t)); n != 0 {
			t.Fatalf("%d registrations on the device, want none", n)
		}
		if n := log.count(slog.LevelWarn, "matter.controller.client.icd_refresh_retry"); n != 0 {
			t.Fatalf("%d retry warnings, want none", n)
		}
	})

	// holdToDevice holds every datagram to the device while hold is set.
	holdToDevice := func(p *icdPair) *atomic.Bool {
		dev := p.pair.Device.Addr()
		var hold atomic.Bool
		hold.Store(true)
		p.site.Net().Filter(func(d fabrictest.Datagram) bool { return !hold.Load() || !sameUDP(d.To, dev) })
		return &hold
	}

	t.Run("joins a registration in flight with the same options and refuses other options", func(t *testing.T) {
		t.Parallel()
		p, _ := icdLogPair(t)
		dev := p.pair.Device.Addr()
		var hold atomic.Bool
		var held atomic.Int32
		hold.Store(true)
		p.site.Net().Filter(func(d fabrictest.Datagram) bool {
			if hold.Load() && sameUDP(d.To, dev) {
				held.Add(1)
				return false
			}
			return true
		})
		first := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
		p.eventually(t, "the first RegisterClient on the wire", func() bool { return held.Load() > 0 })
		joined := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
		err := p.icd().Register(parityCtx(t), client.ICDRegisterOptions{MonitoredSubject: subject(0x1234)})
		if !errors.Is(err, client.ErrICDOperation) || !strings.Contains(err.Error(), "other options") {
			t.Fatalf("register with other options: %v, want refused", err)
		}
		hold.Store(false)
		p.eventually(t, "both registrations", func() bool { return first.done.Load() && joined.done.Load() })
		if first.error() != nil || joined.error() != nil {
			t.Fatalf("register: %v, joined: %v", first.error(), joined.error())
		}
		if !p.icd().Registered() || len(p.deviceServerRegistrations(t)) != 1 {
			t.Fatal("not registered once")
		}
	})

	t.Run("unregister() during a registration abandons it and removes it from the device", func(t *testing.T) {
		t.Parallel()
		p, _ := icdLogPair(t)
		registering := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
		p.eventually(t, "the registration on the device", func() bool { return len(p.deviceServerRegistrations(t)) == 1 })
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Unregister(ctx) }); err != nil {
			t.Fatal(err)
		}
		p.eventually(t, "the registration ended", registering.done.Load)
		if !errors.Is(registering.error(), client.ErrICDAbandoned) && registering.error() != nil {
			t.Fatalf("register: %v, want abandoned", registering.error())
		}
		if p.icd().Registered() || len(p.deviceServerRegistrations(t)) != 0 {
			t.Fatal("the registration survived")
		}
	})

	t.Run("forget() during a registration abandons it", func(t *testing.T) {
		t.Parallel()
		p, _ := icdLogPair(t)
		unregistered := &counter{}
		p.icd().OnUnregistered(unregistered.inc)
		hold := holdToDevice(p)
		registering := start(func() error { return p.icd().Register(parityCtx(t), uncovered) })
		p.advance(t, advanceStep)
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
			t.Fatal(err)
		}
		hold.Store(false)
		p.eventually(t, "the registration ended", registering.done.Load)
		if !errors.Is(registering.error(), client.ErrICDAbandoned) {
			t.Fatalf("register: %v, want abandoned", registering.error())
		}
		if p.icd().Registered() || unregistered.get() != 0 {
			t.Fatalf("registered %v, unregistered announced %d times", p.icd().Registered(), unregistered.get())
		}
	})

	t.Run("clears the time of the last Check-In with the registration", func(t *testing.T) {
		t.Parallel()
		for _, end := range []string{"unregister", "forget"} {
			t.Run(end, func(t *testing.T) {
				p, _ := icdLogPair(t)
				p.register(t, uncovered)
				got := &checkIns{}
				p.icd().OnCheckIn(got.record)
				p.wakeDevice(t)
				p.eventually(t, "a Check-In", func() bool { return got.n() > 0 })
				if _, ok := p.icd().LastCheckInAt(); !ok {
					t.Fatalf("%s: no Check-In time", end)
				}
				if err := p.do(t, func(ctx context.Context) error {
					if end == "unregister" {
						return p.icd().Unregister(ctx)
					}
					return p.icd().Forget(ctx)
				}); err != nil {
					t.Fatal(err)
				}
				if _, ok := p.icd().LastCheckInAt(); ok {
					t.Fatalf("%s kept the Check-In time", end)
				}
			})
		}
	})

	t.Run("registers without the pending key of a refresh that a forget() ended", func(t *testing.T) {
		t.Parallel()
		p, _ := icdLogPair(t)
		p.register(t, uncovered)
		p.requestRefreshOnNextCheckIn(t)
		dev := p.pair.Device.Addr()
		// The refresh's RegisterClient never reaches the device.
		p.site.Net().Filter(func(d fabrictest.Datagram) bool {
			r, _ := p.icd().Registration()
			return !sameUDP(d.To, dev) || r.PendingKey == nil
		})
		p.wakeDevice(t)
		p.eventually(t, "the pending key", func() bool { r, _ := p.icd().Registration(); return r.PendingKey != nil })
		if err := p.do(t, func(ctx context.Context) error { return p.icd().Forget(ctx) }); err != nil {
			t.Fatal(err)
		}
		p.site.Net().Filter(nil)
		p.register(t, uncovered)
		reg, ok := p.icd().Registration()
		if !ok || reg.PendingKey != nil {
			t.Fatalf("registration %+v, want one without a pending key", reg)
		}
	})
}
