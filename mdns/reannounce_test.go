// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"context"
	"testing"

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
