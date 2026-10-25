// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestHooksFor: Config.HooksFor gives every node its own hooks; the
// paired node's default subscription signals through the hooks of its id.
func TestHooksFor(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	alive := map[string]int{}
	site, _, _, n := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.HooksFor = func(id string) client.Hooks {
			return client.Hooks{SubscriptionAlive: func() {
				mu.Lock()
				alive[id]++
				mu.Unlock()
			}}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := site.Eventually(ctx, "the node's subscription is alive", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return alive[n.ID()] > 0
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(alive) != 1 {
		t.Fatalf("SubscriptionAlive by node %v, want node %s's", alive, n.ID())
	}
}

// TestDiscover: Controller.Discover finds the site's commissionable device
// by its long discriminator, and nothing for another one, the timeout on
// the site's clock.
func TestDiscover(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		id   discovery.Identifier
		want int
	}{
		{discovery.ByLongDiscriminator(dev.Discriminator()), 1},
		{discovery.ByLongDiscriminator(dev.Discriminator() + 1), 0},
	} {
		var found []client.Device
		if err := site.Run(ctx, func(ctx context.Context) (err error) {
			found, err = c.Discover(ctx, tc.id, 5*time.Second)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(found) != tc.want {
			t.Fatalf("Discover(%v) found %d, want %d", tc.id, len(found), tc.want)
		}
		if tc.want == 1 && found[0].D != dev.Discriminator() {
			t.Fatalf("found discriminator %d, want %d", found[0].D, dev.Discriminator())
		}
	}
}

// TestParsePairingCode decodes a QR and a manual code and refuses a code
// with a broken check digit, as Pair does.
func TestParsePairingCode(t *testing.T) {
	t.Parallel()
	qr, err := setup.QRCode(setup.Payload{VendorID: 0xFFF1, ProductID: 0x8000, DiscoveryCaps: setup.DiscoveryOnIP, Discriminator: 3840, Passcode: 20202021})
	if err != nil {
		t.Fatal(err)
	}
	p, err := controller.ParsePairingCode(qr)
	if err != nil {
		t.Fatal(err)
	}
	if p.Manual || p.VendorID != 0xFFF1 || p.ProductID != 0x8000 || p.Capabilities != 4 || p.Discriminator != 3840 || p.Passcode != 20202021 || p.HasExtra {
		t.Fatalf("QR code: %+v", p)
	}
	manual, err := setup.ManualCode(3840, 20202021)
	if err != nil {
		t.Fatal(err)
	}
	m, err := controller.ParsePairingCode(manual)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Manual || m.Discriminator != 3840>>8 || m.Passcode != 20202021 || m.VendorID != 0 {
		t.Fatalf("manual code: %+v", m)
	}
	bad := manual[:len(manual)-1] + string(rune('0'+(manual[len(manual)-1]-'0'+1)%10))
	if _, err := controller.ParsePairingCode(bad); !errors.Is(err, controller.ErrPairingCode) {
		t.Fatalf("a broken check digit: %v, want ErrPairingCode", err)
	}
	_, _, c := siteController(t, fabrictest.Topology{}, nil)
	if _, err := c.Pair(context.Background(), bad, controller.PairOptions{}); !errors.Is(err, controller.ErrPairingCode) {
		t.Fatalf("Pair with a broken code: %v, want ErrPairingCode", err)
	}
}
