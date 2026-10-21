// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// TestControllerDiscover: the controller finds the uncommissioned device
// by its long discriminator, and nothing for another one once the timeout
// has passed on the site's clock.
func TestControllerDiscover(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{})
	found, err := p.Controller.Discover(ctx, discovery.ByLongDiscriminator(p.Device.Discriminator()), 90*time.Second)
	if err != nil || len(found) != 1 || found[0].D != p.Device.Discriminator() {
		t.Fatalf("discovered %+v, %v", found, err)
	}
	start := site.Clock().Now()
	none, err := p.Controller.Discover(ctx, discovery.ByLongDiscriminator(p.Device.Discriminator()^0x1), 5*time.Second)
	if err != nil || len(none) != 0 {
		t.Fatalf("discovered %+v, %v for another discriminator", none, err)
	}
	if elapsed := site.Clock().Now().Sub(start); elapsed < 5*time.Second {
		t.Fatalf("the search ended after %v, before its timeout", elapsed)
	}
}

// TestControllerCommissionNode: the registry's node of the discovered
// device is commissioned, started and subscribed.
func TestControllerCommissionNode(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{})
	n, err := p.Controller.CommissionNode(ctx, p.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !n.IsCommissioned() || p.Controller.Registry().Size() != 1 {
		t.Fatalf("commissioned %v, registry size %d", n.IsCommissioned(), p.Controller.Registry().Size())
	}
	if err := site.Eventually(ctx, "the subscription", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
}

// TestControllerAttestationStore: without a trust store the attestation
// carries the DclServiceUnavailable warning; a store of another root does
// not trust the device's chain.
func TestControllerAttestationStore(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		opts  fabrictest.ControllerOptions
		check attestation.Check
	}{
		{"no store", fabrictest.ControllerOptions{NoAttestationStore: true}, attestation.DclServiceUnavailable},
		{"another root", fabrictest.ControllerOptions{Attestation: &attestation.StaticStore{PAAs: []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert}}, AcceptTest: true}}, attestation.PaaNotTrusted},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			site := fabrictest.NewSite(t, fabrictest.Options{})
			var seen []attestation.Finding
			_ = site.CommissionedPair(fabrictest.PairOptions{Controller: c.opts, Commission: fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
				o.OnAttestationFailure = attestation.Policy{Decide: func(f []attestation.Finding) (bool, string, error) {
					seen = f
					return true, "", nil
				}}
			}}})
			if !slices.ContainsFunc(seen, func(f attestation.Finding) bool { return f.Check == c.check }) {
				t.Fatalf("findings %+v, want %s", seen, c.check)
			}
		})
	}
}
