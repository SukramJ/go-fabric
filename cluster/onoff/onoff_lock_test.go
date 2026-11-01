// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
)

// funcSink is a Sink that runs fn.
type funcSink func(ctx context.Context, on bool) error

func (f funcSink) SetOnOff(ctx context.Context, on bool) error { return f(ctx, on) }

// lockDeadline bounds every call that would block forever if the Sink ran
// under a lock [onoff.Server.Set] takes (issue #63).
const lockDeadline = time.Second

// within runs fn on its own goroutine and fails the test when it has not
// returned within lockDeadline. It never sleeps: the deadline is a timer.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(lockDeadline):
		t.Fatalf("%s did not return within %v: the Sink runs under a lock Set takes", what, lockDeadline)
	}
}

// countOnOff counts the OnOff entries among the reported attribute ids.
func countOnOff(ids []uint32) int {
	n := 0
	for _, id := range ids {
		if id == onoff.AttrOnOff {
			n++
		}
	}
	return n
}

// TestSinkSynchronousEcho: a Sink that confirms through Set before it
// returns — the device's event arriving before the write's reply (issue
// #63) — lets the command complete; OnOff is stored once and reported
// once.
func TestSinkSynchronousEcho(t *testing.T) {
	t.Parallel()
	var srv *onoff.Server
	srv, err := onoff.New(onoff.Config{Sink: funcSink(func(_ context.Context, on bool) error {
		srv.Set(on)
		return nil
	})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var changed []uint32
	srv.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; changed = append(changed, ids...) })
	within(t, "On", func() {
		if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOn, nil); err != nil {
			t.Errorf("On: %v", err)
		}
	})
	if !srv.OnOff() {
		t.Error("OnOff false after On")
	}
	if n := countOnOff(changed); n != 1 {
		t.Errorf("OnOff reported %d times (%v), want once", n, changed)
	}
}

// TestSinkEchoFromAnotherGoroutine: the Sink waits until another
// goroutine's Set has returned — the host's event goroutine confirming
// while the command goroutine waits for the device's reply — and the
// command completes.
func TestSinkEchoFromAnotherGoroutine(t *testing.T) {
	t.Parallel()
	var srv *onoff.Server
	srv, err := onoff.New(onoff.Config{Sink: funcSink(func(_ context.Context, on bool) error {
		echoed := make(chan struct{})
		go func() {
			defer close(echoed)
			srv.Set(on)
		}()
		select {
		case <-echoed:
			return nil
		case <-time.After(lockDeadline):
			return context.DeadlineExceeded
		}
	})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	within(t, "On", func() {
		if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOn, nil); err != nil {
			t.Errorf("On: %v", err)
		}
	})
	if !srv.OnOff() {
		t.Error("OnOff false after On")
	}
}

// TestHostChangeDuringSinkLosesToTheCommand: a Set(true) that lands while
// the Sink of an Off is in flight is overwritten by the command's false
// once the Sink consents — the command wins its own attribute. The
// comparison that decides the scene invalidation reads the state the Set
// left (true), so the Off invalidates.
func TestHostChangeDuringSinkLosesToTheCommand(t *testing.T) {
	t.Parallel()
	sc := &recScenes{}
	var srv *onoff.Server
	srv, err := onoff.New(onoff.Config{Scenes: sc, Sink: funcSink(func(_ context.Context, on bool) error {
		if !on {
			srv.Set(true)
		}
		return nil
	})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	within(t, "Off", func() {
		if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOff, nil); err != nil {
			t.Errorf("Off: %v", err)
		}
	})
	if srv.OnOff() {
		t.Error("OnOff true: the host's Set during the Sink overwrote the command")
	}
	if len(sc.calls) != 1 || sc.calls[0] != "invalidate" {
		t.Errorf("scenes calls %v, want [invalidate]", sc.calls)
	}
}

// TestSinkEchoInTheTimedOffTick: the countdown that ends a timed-on phase
// turns the server off through the Sink; a Sink that echoes through Set
// does not block the tick, and the tick queue still runs after it.
func TestSinkEchoInTheTimedOffTick(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	var calls []bool
	var srv *onoff.Server
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, TimeSource: clock, Sink: funcSink(func(_ context.Context, on bool) error {
		calls = append(calls, on)
		srv.Set(on)
		return nil
	})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	within(t, "OnWithTimedOff", func() {
		if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOnWithTimedOff, onoffdef.OnWithTimedOffRequest{OnTime: 3}); err != nil {
			t.Errorf("OnWithTimedOff: %v", err)
		}
	})
	within(t, "the countdown", func() { clock.Advance(time.Second) })
	if srv.OnOff() {
		t.Error("OnOff true after the countdown")
	}
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Errorf("sink calls %v, want [true false]", calls)
	}
	if n := clock.Pending(); n != 0 {
		t.Errorf("%d timers pending after the countdown", n)
	}
}

// TestSinkEchoInvalidatesScenes: a Sink that confirms the very change
// through Set before it returns does not hide the change from the scene
// invalidation — matter.js judges On and Off on this.state.onOff at
// command start (packages/node/src/behaviors/on-off/OnOffServer.ts:98-100, :120-122 at
// 862ad75f; W3-club2-01). A command that changes nothing still
// invalidates nothing.
func TestSinkEchoInvalidatesScenes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		start bool
		cmd   uint32
		want  int
	}{
		{"On from off", false, onoff.CmdOn, 1},
		{"Off from on", true, onoff.CmdOff, 1},
		{"On when on", true, onoff.CmdOn, 0},
		{"Off when off", false, onoff.CmdOff, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sc := &recScenes{}
			var srv *onoff.Server
			srv, err := onoff.New(onoff.Config{Scenes: sc, Sink: funcSink(func(_ context.Context, on bool) error {
				srv.Set(on) // the host's echo of the change
				return nil
			})})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			srv.Set(c.start)
			within(t, c.name, func() {
				if _, err := srv.MatterInvoke(context.Background(), c.cmd, nil); err != nil {
					t.Errorf("%s: %v", c.name, err)
				}
			})
			if len(sc.calls) != c.want {
				t.Errorf("scenes calls %v, want %d invalidation(s)", sc.calls, c.want)
			}
		})
	}
}
