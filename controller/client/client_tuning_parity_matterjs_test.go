// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The network-profile cases of matter.js
// packages/node/test/node/ClientTuningTest.ts at 862ad75f: a commissioned
// peer's profile follows its medium, its sessions carry the medium's MRP
// margin, configured profiles reach the controller's NetworkProfiles and
// the own profile sets the sender-side margin. The site's devices are
// Ethernet-attached (node's NetworkCommissioning default).
package client_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// onlyPeer is [...controller.env.get(PeerSet)][0].
func onlyPeer(t *testing.T, cp *clientPair) *peer.Peer {
	t.Helper()
	ps := cp.pair.Controller.Peers().Peers()
	if len(ps) != 1 {
		t.Fatalf("%d peers, want 1", len(ps))
	}
	return ps[0]
}

func TestClientTuningParity(t *testing.T) {
	t.Parallel()
	t.Run("ethernet-operational peers use the fast profile with no MRP margin", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		props := cp.node.Properties()
		if !props.SupportsEthernet {
			t.Fatalf("the device reports no Ethernet: %+v", props)
		}
		p := onlyPeer(t, cp)
		if n := p.Network(); n.ID != peer.NetworkFast || n.AdditionalMrpDelay != 0 {
			t.Errorf("profile %s margin %v, want fast 0", n.ID, n.AdditionalMrpDelay)
		}
		// ClientTuningTest.ts:82-91 with the fast medium: the session's
		// margin is the medium's.
		s := p.NewestSession()
		if s == nil || s.PeerMargins() == nil || s.PeerMargins().Messaging != 0 {
			t.Errorf("session margins %+v", s.PeerMargins())
		}
	})
	t.Run("ownNetworkProfileId sets the sender-side MRP margin", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{OwnNetworkProfile: peer.NetworkThread}})
		if d := cp.pair.Controller.Exchanges().LocalAdditionalMrpDelay(); d != 1500*time.Millisecond {
			t.Errorf("local margin %v, want 1.5s", d)
		}
	})
	t.Run("ownNetworkProfileId defaults to fast (no sender-side margin)", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if d := cp.pair.Controller.Exchanges().LocalAdditionalMrpDelay(); d != 0 {
			t.Errorf("local margin %v, want 0", d)
		}
	})
	t.Run("custom profiles propagate to NetworkProfiles", func(t *testing.T) {
		t.Parallel()
		one, three, hundred := 1, 3, 100
		five := 5 * time.Second
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Networks: &peer.ProfilesConfig{
			Conservative: &peer.PartialLimits{Connect: &peer.PartialConcreteLimits{Exchanges: &one, Timeout: &five}},
			Fast:         &peer.PartialLimits{PartialConcreteLimits: peer.PartialConcreteLimits{Exchanges: &hundred}, Connect: &peer.PartialConcreteLimits{Exchanges: &three}},
		}}})
		profiles := cp.pair.Controller.Peers().Networks()
		conservative, _ := profiles.Get(peer.NetworkConservative)
		fast, _ := profiles.Get(peer.NetworkFast)
		if conservative.Connect == nil || conservative.Connect.ID != "conservative:connect" {
			t.Errorf("conservative connect %+v", conservative.Connect)
		}
		if fast.ID != "fast" || fast.Connect == nil || fast.Connect.ID != "fast:connect" {
			t.Errorf("fast %s connect %+v", fast.ID, fast.Connect)
		}
		if fast.AdditionalMrpDelay != 0 || conservative.AdditionalMrpDelay != 1500*time.Millisecond {
			t.Errorf("margins fast %v conservative %v", fast.AdditionalMrpDelay, conservative.AdditionalMrpDelay)
		}
	})
	t.Run("a configured bdxAdditionalMrpDelay propagates to NetworkProfiles", func(t *testing.T) {
		t.Parallel()
		three, two := 3*time.Second, 2*time.Second
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Networks: &peer.ProfilesConfig{
			Thread: &peer.PartialLimits{BDXAdditionalMrpDelay: &three},
			WiFi:   &peer.PartialLimits{BDXAdditionalMrpDelay: &two},
		}}})
		profiles := cp.pair.Controller.Peers().Networks()
		thread, _ := profiles.Get(peer.NetworkThread)
		wifi, _ := profiles.Get(peer.NetworkWiFi)
		conservative, _ := profiles.Get(peer.NetworkConservative)
		if thread.Margins.BDX != 3*time.Second || thread.AdditionalMrpDelay != 1500*time.Millisecond {
			t.Errorf("thread %+v", thread.Margins)
		}
		if wifi.Margins.BDX != 2*time.Second || wifi.AdditionalMrpDelay != time.Second {
			t.Errorf("wifi %+v", wifi.Margins)
		}
		if conservative.Margins.BDX != 1500*time.Millisecond {
			t.Errorf("conservative %+v", conservative.Margins)
		}
	})
	t.Run("connect concurrency 1 serializes peer connections", func(t *testing.T) {
		t.Parallel()
		one := 1
		five := 5 * time.Second
		limit := &peer.PartialLimits{Connect: &peer.PartialConcreteLimits{Exchanges: &one, Timeout: &five}}
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Networks: &peer.ProfilesConfig{
			Fast: limit, Conservative: limit, Thread: limit,
		}}})
		profiles := cp.pair.Controller.Peers().Networks()
		for _, name := range []string{peer.NetworkFast, peer.NetworkConservative, peer.NetworkThread} {
			p, _ := profiles.Get(name)
			if p.Connect == nil || p.Connect.ID != name+":connect" {
				t.Errorf("%s connect %+v", name, p.Connect)
			}
		}
	})
}
