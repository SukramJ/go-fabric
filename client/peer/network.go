// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock"
)

// Network profiles: matter.js packages/protocol/src/peer/NetworkProfile.ts
// at 862ad75f. A network profile is a logical segment of nodes that share
// rate limits — a throttle (a [Semaphore]) and an MRP margin. matter.js
// selects a profile by the peer's medium (ForPeer) or by the id an
// interaction names (Select); a profile may carry a sub-profile for CASE
// establishment (connect) and one for address probes (probe).

// ErrUnknownNetworkProfile is a profile id neither configured nor a
// template — matter.js UnknownNetworkProfileError (NetworkProfile.ts:28).
var ErrUnknownNetworkProfile = errors.New("peer: unknown network profile")

// UnlimitedExchanges is the exchange limit of an unthrottled profile,
// matter.js's Infinity (NetworkProfile.ts:364-365).
const UnlimitedExchanges = math.MaxInt

// The template ids (NetworkProfile.ts:289-345). "thread:<channel>" and
// "group" are profiles ForPeer derives from the thread and fast templates.
const (
	NetworkFast         = "fast"
	NetworkWiFi         = "wifi"
	NetworkThread       = "thread"
	NetworkConservative = "conservative"
	NetworkUnknown      = "unknown"
	NetworkUnlimited    = "unlimited"
	NetworkICDLit       = "icdLit"
)

// ConcreteLimits throttle one profile — matter.js
// NetworkProfiles.ConcreteLimits (NetworkProfile.ts:237-257).
type ConcreteLimits struct {
	// Exchanges is the number of concurrent exchanges (below 1 is 1;
	// [UnlimitedExchanges] for none).
	Exchanges int
	// Delay is the cooldown between new exchanges; 0 is none.
	Delay time.Duration
	// Timeout releases an exchange's slot after this long in any case; 0
	// is none.
	Timeout time.Duration
	// AdditionalMrpDelay is the profile's MRP retransmission margin; nil
	// inherits the parent profile's, else 0.
	AdditionalMrpDelay *time.Duration
}

// Limits are a profile's limits with its BDX margin and its sub-profiles —
// matter.js NetworkProfiles.Limits (NetworkProfile.ts:262-284).
type Limits struct {
	ConcreteLimits
	// BDXAdditionalMrpDelay is the margin of bulk transfer exchanges; nil
	// is AdditionalMrpDelay. Values above roughly 7 s are inert: a BDX
	// retransmission interval is capped so the schedule fits the peer's
	// response budget.
	BDXAdditionalMrpDelay *time.Duration
	// Connect limits CASE session establishment; nil is the profile's.
	Connect *ConcreteLimits
	// ProbeAddress limits address probe reads; nil is the profile's.
	ProbeAddress *ConcreteLimits
}

// Templates are the standard profiles — matter.js NetworkProfiles.Templates
// (NetworkProfile.ts:289-345): Fast for Ethernet, WiFi with Fast's
// concurrency and a margin for Wi-Fi power save, Thread per channel,
// Conservative the base of constrained networks, Unknown for a peer whose
// medium is unknown, Unlimited only when an interaction names it, ICDLit
// the unthrottled profile of an await-mode LIT ICD's interactions (its
// MRP margin stays the medium's).
type Templates struct {
	Fast, WiFi, Thread, Conservative, Unknown, Unlimited, ICDLit Limits
}

func durationPtr(d time.Duration) *time.Duration { return &d }

// ConservativeLimits are matter.js NetworkProfiles.conservative
// (NetworkProfile.ts:347-361): 4 exchanges 100 ms apart, a 1.5 s MRP
// margin, CASE establishment 4 at a time each released after 10 s, address
// probes 2 at a time released after 15 s.
func ConservativeLimits() Limits {
	return Limits{
		ConcreteLimits: ConcreteLimits{Exchanges: 4, Delay: 100 * time.Millisecond, AdditionalMrpDelay: durationPtr(1500 * time.Millisecond)},
		Connect:        &ConcreteLimits{Exchanges: 4, Timeout: 10 * time.Second},
		ProbeAddress:   &ConcreteLimits{Exchanges: 2, Timeout: 15 * time.Second},
	}
}

// DefaultTemplates are matter.js NetworkProfiles.defaults
// (NetworkProfile.ts:363-371): unlimited and icdLit unthrottled without a
// margin, fast 200 exchanges without one, wifi 200 with 1 s, and thread,
// conservative and unknown the conservative limits.
func DefaultTemplates() Templates {
	return Templates{
		Unlimited:    Limits{ConcreteLimits: ConcreteLimits{Exchanges: UnlimitedExchanges, AdditionalMrpDelay: durationPtr(0)}},
		ICDLit:       Limits{ConcreteLimits: ConcreteLimits{Exchanges: UnlimitedExchanges, AdditionalMrpDelay: durationPtr(0)}},
		Fast:         Limits{ConcreteLimits: ConcreteLimits{Exchanges: 200, AdditionalMrpDelay: durationPtr(0)}},
		WiFi:         Limits{ConcreteLimits: ConcreteLimits{Exchanges: 200, AdditionalMrpDelay: durationPtr(time.Second)}},
		Thread:       ConservativeLimits(),
		Conservative: ConservativeLimits(),
		Unknown:      ConservativeLimits(),
	}
}

// template is the Templates field of id, false for none — matter.js's
// Object.hasOwn(defaults, id), which refuses a prototype key
// (NetworkProfile.ts:143).
func (t *Templates) template(id string) (*Limits, bool) {
	switch id {
	case NetworkFast:
		return &t.Fast, true
	case NetworkWiFi:
		return &t.WiFi, true
	case NetworkThread:
		return &t.Thread, true
	case NetworkConservative:
		return &t.Conservative, true
	case NetworkUnknown:
		return &t.Unknown, true
	case NetworkUnlimited:
		return &t.Unlimited, true
	case NetworkICDLit:
		return &t.ICDLit, true
	}
	return nil, false
}

// PartialConcreteLimits and PartialLimits override the fields they set —
// matter.js NetworkProfiles.PartialOptions (NetworkProfile.ts:230-235),
// the NetworkServer.ConcreteLimitsConfig and LimitsConfig of a node's
// configuration (packages/node/src/behavior/system/network/NetworkServer.ts:137-160).
type PartialConcreteLimits struct {
	Exchanges                          *int
	Delay, Timeout, AdditionalMrpDelay *time.Duration
}

// PartialLimits is a partial [Limits], as [PartialConcreteLimits] is a
// partial [ConcreteLimits].
type PartialLimits struct {
	PartialConcreteLimits
	BDXAdditionalMrpDelay *time.Duration
	Connect, ProbeAddress *PartialConcreteLimits
}

// ProfilesConfig overrides templates field-wise — matter.js
// NetworkServer.ProfilesConfig (NetworkServer.ts:162-183).
type ProfilesConfig struct {
	Fast, WiFi, Thread, Conservative, Unknown, Unlimited, ICDLit *PartialLimits
}

func (c *ProfilesConfig) each(fn func(id string, p *PartialLimits)) {
	for _, e := range []struct {
		id string
		p  *PartialLimits
	}{
		{NetworkFast, c.Fast},
		{NetworkWiFi, c.WiFi},
		{NetworkThread, c.Thread},
		{NetworkConservative, c.Conservative},
		{NetworkUnknown, c.Unknown},
		{NetworkUnlimited, c.Unlimited},
		{NetworkICDLit, c.ICDLit},
	} {
		if e.p != nil {
			fn(e.id, e.p)
		}
	}
}

func (c ConcreteLimits) merged(p PartialConcreteLimits) ConcreteLimits {
	if p.Exchanges != nil {
		c.Exchanges = *p.Exchanges
	}
	if p.Delay != nil {
		c.Delay = *p.Delay
	}
	if p.Timeout != nil {
		c.Timeout = *p.Timeout
	}
	if p.AdditionalMrpDelay != nil {
		c.AdditionalMrpDelay = durationPtr(*p.AdditionalMrpDelay)
	}
	return c
}

// NetworkProfile is one profile: its throttle and its MRP margins, with
// the connect and probe sub-profiles when its limits name them — matter.js
// ConcreteNetworkProfile and NetworkProfile (NetworkProfile.ts:30-76).
type NetworkProfile struct {
	// ID is the profile's id ("fast", "thread:15", "unknown:connect", …).
	ID string
	// Semaphore throttles the profile's exchanges.
	Semaphore *Semaphore
	// AdditionalMrpDelay is the profile's MRP margin.
	AdditionalMrpDelay time.Duration
	// Margins are the margins by traffic class: Messaging is
	// AdditionalMrpDelay, BDX the BDX margin (NetworkProfile.ts:155-158).
	Margins exchange.Margins
	// Connect and ProbeAddress are the sub-profiles; nil is none.
	Connect, ProbeAddress *NetworkProfile
}

// NetworkProfiles is the set of profiles of a [Set] — matter.js
// NetworkProfiles (NetworkProfile.ts:81-222). It is safe for concurrent
// use.
type NetworkProfiles struct {
	clock clock.Clock
	log   *slog.Logger

	mu       sync.Mutex
	networks map[string]*NetworkProfile
	defaults Templates
}

// NewNetworkProfiles returns the profiles over templates (nil is
// [DefaultTemplates]) on clk (nil is the system clock) — the constructor
// at NetworkProfile.ts:85-90.
func NewNetworkProfiles(clk clock.Clock, templates *Templates, log *slog.Logger) *NetworkProfiles {
	t := DefaultTemplates()
	if templates != nil {
		t = *templates
	}
	if log == nil {
		log = slog.Default()
	}
	return &NetworkProfiles{clock: clock.Or(clk), log: log, networks: map[string]*NetworkProfile{}, defaults: t}
}

// SetDefaults layers cfg onto the current templates and drops every
// configured profile, which derived from the previous ones; successive
// calls compose (NetworkProfile.ts:92-117).
func (n *NetworkProfiles) SetDefaults(cfg ProfilesConfig) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.networks = map[string]*NetworkProfile{}
	cfg.each(func(id string, p *PartialLimits) {
		base, _ := n.defaults.template(id)
		merged := *base
		merged.ConcreteLimits = merged.merged(p.PartialConcreteLimits)
		if p.BDXAdditionalMrpDelay != nil {
			merged.BDXAdditionalMrpDelay = durationPtr(*p.BDXAdditionalMrpDelay)
		}
		if p.Connect != nil {
			c := ConcreteLimits{}
			if merged.Connect != nil {
				c = *merged.Connect
			}
			c = c.merged(*p.Connect)
			merged.Connect = &c
		}
		if p.ProbeAddress != nil {
			c := ConcreteLimits{}
			if merged.ProbeAddress != nil {
				c = *merged.ProbeAddress
			}
			c = c.merged(*p.ProbeAddress)
			merged.ProbeAddress = &c
		}
		*base = merged
	})
}

// Select is the profile id names, or the peer's medium profile when id is
// "" (NetworkProfile.ts:123-129).
func (n *NetworkProfiles) Select(p *Peer, id string) (*NetworkProfile, error) {
	if id != "" {
		return n.Get(id)
	}
	return n.ForPeer(p), nil
}

// Get is the profile id: one configured before, else a template's
// (NetworkProfile.ts:136-148); any other id is [ErrUnknownNetworkProfile].
func (n *NetworkProfiles) Get(id string) (*NetworkProfile, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p, ok := n.networks[id]; ok {
		return p, nil
	}
	t, ok := n.defaults.template(id)
	if !ok {
		return nil, fmt.Errorf("%w: network profile %s is not configured", ErrUnknownNetworkProfile, id)
	}
	return n.configureLocked(id, *t, nil), nil
}

// Configure builds and registers profile id from limits: its margin is
// the limits', else parentDelay, else 0; its BDX margin the limits' BDX
// margin, else its margin; its connect and probe sub-profiles inherit its
// margin (NetworkProfile.ts:150-182).
func (n *NetworkProfiles) Configure(id string, limits Limits, parentDelay *time.Duration) *NetworkProfile {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.configureLocked(id, limits, parentDelay)
}

func (n *NetworkProfiles) configureLocked(id string, limits Limits, parentDelay *time.Duration) *NetworkProfile {
	delay := time.Duration(0)
	switch {
	case limits.AdditionalMrpDelay != nil:
		delay = *limits.AdditionalMrpDelay
	case parentDelay != nil:
		delay = *parentDelay
	}
	bdx := delay
	if limits.BDXAdditionalMrpDelay != nil {
		bdx = *limits.BDXAdditionalMrpDelay
	}
	p := &NetworkProfile{
		ID:                 id,
		Semaphore:          NewSemaphore(n.clock, "network semaphore "+id, limits.Exchanges, limits.Delay, limits.Timeout),
		AdditionalMrpDelay: delay,
		Margins:            exchange.Margins{Messaging: delay, BDX: bdx},
	}
	if limits.Connect != nil {
		p.Connect = n.configureLocked(id+":connect", Limits{ConcreteLimits: *limits.Connect}, &delay)
	}
	if limits.ProbeAddress != nil {
		p.ProbeAddress = n.configureLocked(id+":probe", Limits{ConcreteLimits: *limits.ProbeAddress}, &delay)
	}
	n.log.Debug("matter.controller.network.configure", slog.String("profile", id), slog.Int("exchanges", limits.Exchanges),
		slog.Duration("delay", limits.Delay), slog.Duration("additional_mrp_delay", delay))
	n.networks[id] = p
	return p
}

// Medium is the part of a peer's physical properties that selects its
// profile — matter.js PhysicalDeviceProperties's supports*, *Active,
// rootEndpointServerList and threadChannel
// (packages/protocol/src/peer/PhysicalDeviceProperties.ts:32-61), which a
// client node computes from the peer's attributes ([Peer.SetMedium]).
type Medium struct {
	SupportsThread, SupportsWifi, SupportsEthernet bool
	// RootEndpointServers is the length of the root endpoint's ServerList.
	RootEndpointServers int
	// ThreadActive, WifiActive and EthernetActive report an operational
	// interface; nil is unknown.
	ThreadActive, WifiActive, EthernetActive *bool
	// ThreadChannel is the Thread channel; 0 is unknown.
	ThreadChannel uint16
}

// ForPeer is the profile of p's medium (NetworkProfile.ts:184-221): a
// peer whose medium is unknown "unknown"; an active (or, without data, a
// supporting) Thread interface "thread:<channel>", or "thread" without a
// channel; an active Wi-Fi interface, or a Wi-Fi-capable peer without
// medium data, "wifi"; Ethernet support, or none of the three but a root
// server list (other means), "fast"; "unknown" otherwise. A group peer's
// "group" profile (fast) has no counterpart: this module has no group
// peers in the set.
func (n *NetworkProfiles) ForPeer(p *Peer) *NetworkProfile {
	var m *Medium
	if p != nil {
		m = p.medium()
	}
	return n.ForMedium(m)
}

// ForMedium is [NetworkProfiles.ForPeer] for a medium; nil is unknown.
func (n *NetworkProfiles) ForMedium(m *Medium) *NetworkProfile {
	n.mu.Lock()
	defer n.mu.Unlock()
	id, limits := NetworkUnknown, n.defaults.Unknown
	switch {
	case m == nil:
	case truth(m.ThreadActive) || (m.ThreadActive == nil && m.SupportsThread):
		id, limits = NetworkThread, n.defaults.Thread
		if m.ThreadChannel != 0 {
			id = NetworkThread + ":" + strconv.Itoa(int(m.ThreadChannel))
		}
	case truth(m.WifiActive) || (m.WifiActive == nil && m.EthernetActive == nil && m.SupportsWifi):
		id, limits = NetworkWiFi, n.defaults.WiFi
	case m.SupportsEthernet || (!m.SupportsWifi && !m.SupportsEthernet && !m.SupportsThread && m.RootEndpointServers > 0):
		id, limits = NetworkFast, n.defaults.Fast
	}
	if p, ok := n.networks[id]; ok {
		return p
	}
	return n.configureLocked(id, limits, nil)
}

func truth(b *bool) bool { return b != nil && *b }
