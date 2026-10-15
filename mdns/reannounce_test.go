// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// plainAdvertiser implements mdns.Advertiser and nothing optional.
type plainAdvertiser struct{}

func (plainAdvertiser) Publish(context.Context, mdns.Service) error    { return nil }
func (plainAdvertiser) Withdraw(context.Context, string, string) error { return nil }
func (plainAdvertiser) Active() []mdns.Service                         { return nil }
func (plainAdvertiser) Close() error                                   { return nil }

// TestIsEnabled (issue #119): Zeroconf publishes, Noop does not, an
// advertiser that does not implement mdns.Enabled is taken as publishing,
// and nil is not an advertiser.
func TestIsEnabled(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		a    mdns.Advertiser
		want bool
	}{
		"zeroconf": {mdns.NewZeroconf(), true},
		"noop":     {mdns.NewNoop(), false},
		"plain":    {plainAdvertiser{}, true},
		"nil":      {nil, false},
	} {
		if got := mdns.IsEnabled(tc.a); got != tc.want {
			t.Errorf("%s: IsEnabled = %v, want %v", name, got, tc.want)
		}
	}
	var a mdns.Advertiser = mdns.NewZeroconf()
	if _, ok := a.(mdns.Reannouncer); !ok {
		t.Error("Zeroconf is not an mdns.Reannouncer")
	}
	if _, ok := mdns.Advertiser(mdns.NewNoop()).(mdns.Reannouncer); ok {
		t.Error("Noop is an mdns.Reannouncer")
	}
}

// TestZeroconfOneReannounceLoop (owner's answer to AU Q2): while a loop
// runs, a second StartReannounceLoop starts none and hands back the
// running loop's trigger with a no-op cancel; cancel is safe twice; after
// cancel, or once the loop's ctx ends, a new call starts a new loop.
func TestZeroconfOneReannounceLoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	z := mdns.NewZeroconf()
	t.Cleanup(func() { _ = z.Close() })
	cancel1, tr1 := z.StartReannounceLoop(ctx, time.Hour)
	cancel2, tr2 := z.StartReannounceLoop(ctx, time.Minute)
	if tr2 != tr1 {
		t.Fatal("a second loop was started beside the running one")
	}
	cancel2()
	if _, tr := z.StartReannounceLoop(ctx, time.Hour); tr != tr1 {
		t.Fatal("the no-op cancel stopped the running loop")
	}
	tr1 <- struct{}{} // the running loop's trigger still works
	cancel1()
	cancel1()
	cancel3, tr3 := z.StartReannounceLoop(ctx, time.Hour)
	if tr3 == tr1 {
		t.Fatal("no new loop after cancel")
	}
	cancel3()

	lctx, end := context.WithCancel(ctx)
	_, tr4 := z.StartReannounceLoop(lctx, time.Hour)
	end()
	deadline := time.Now().Add(5 * time.Second)
	for {
		cancel, tr := z.StartReannounceLoop(ctx, time.Hour)
		if tr != tr4 {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the loop whose ctx ended still counts as running")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
