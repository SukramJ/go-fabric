// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Address is one way to reach a service: matter.js ServerAddressIp
// (packages/general/src/net/ServerAddress.ts), an IP and port with the
// health and DNS priority the desirability order weighs. An IPv6
// link-local address carries the zone of the interface its AAAA arrived
// on.
type Address struct {
	IP   netip.Addr
	Port uint16
	// HealthyAt is the last successful use, zero when unknown.
	HealthyAt time.Time
	// UnhealthyAt is the last failed use, zero when unknown.
	UnhealthyAt time.Time
	// Priority is the DNS priority, nil when unknown.
	Priority *uint16
}

// AddrPort returns the address as a netip.AddrPort (zone included).
func (a Address) AddrPort() netip.AddrPort { return netip.AddrPortFrom(a.IP, a.Port) }

// UDPAddr returns the address as a *net.UDPAddr.
func (a Address) UDPAddr() *net.UDPAddr { return net.UDPAddrFromAddrPort(a.AddrPort()) }

// String formats the address as host:port, IPv6 in brackets.
func (a Address) String() string { return a.AddrPort().String() }

// Selection preferences, lower first. Mirrors matter.js 0ceb202d
// packages/general/src/net/ServerAddress.ts:170-176
// (ServerAddress.SelectionPreference).
const (
	PreferenceIPv6LinkLocal = iota
	PreferenceIPv6ULA
	PreferenceIPv6
	PreferenceIPv4
)

// SelectionPreference ranks an IP for connection attempts: IPv6
// link-local, then ULA (fc00::/7), then other IPv6, then IPv4. It
// classifies the address text as matter.js does — ULA by /^f[cd]/i,
// link-local by /^fe[89ab]/i, IPv6 by a ":" — so an IPv4-mapped IPv6
// address counts as IPv6. Mirrors matter.js 0ceb202d
// packages/general/src/net/ServerAddress.ts:178-209
// (selectionPreferenceOfIp).
func SelectionPreference(ip netip.Addr) int {
	s := strings.ToLower(ip.String())
	switch {
	case strings.HasPrefix(s, "fc"), strings.HasPrefix(s, "fd"):
		return PreferenceIPv6ULA
	case strings.HasPrefix(s, "fe8"), strings.HasPrefix(s, "fe9"), strings.HasPrefix(s, "fea"), strings.HasPrefix(s, "feb"):
		return PreferenceIPv6LinkLocal
	case strings.Contains(s, ":"):
		return PreferenceIPv6
	}
	return PreferenceIPv4
}

// CompareDesirability orders two addresses for communication, negative
// when a is preferred: selection preference first, then health, then —
// when both are known — the higher DNS priority value. Mirrors matter.js
// 0ceb202d packages/general/src/net/ServerAddressSet.ts:165-184
// (compareDesirability).
func CompareDesirability(a, b Address) int {
	if v := SelectionPreference(a.IP) - SelectionPreference(b.IP); v != 0 {
		return v
	}
	if h := compareHealth(a, b); h != 0 {
		return h
	}
	if a.Priority != nil && b.Priority != nil {
		return int(*b.Priority) - int(*a.Priority)
	}
	return 0
}

// healthOf drops an UnhealthyAt older than HealthyAt. Mirrors matter.js
// 0ceb202d ServerAddress.ts:149-161.
func healthOf(a Address) (healthyAt, unhealthyAt time.Time) {
	if a.UnhealthyAt.IsZero() {
		return a.HealthyAt, time.Time{}
	}
	if !a.HealthyAt.IsZero() && a.HealthyAt.After(a.UnhealthyAt) {
		return a.HealthyAt, time.Time{}
	}
	return a.HealthyAt, a.UnhealthyAt
}

// compareHealth: an unhealthy address after any other (the earlier
// failure first among two), then the most recently healthy first, then an
// unused one. Mirrors matter.js 0ceb202d ServerAddressSet.ts:191-229.
func compareHealth(a, b Address) int {
	ha, ua := healthOf(a)
	hb, ub := healthOf(b)
	if !ua.IsZero() {
		if !ub.IsZero() {
			return ua.Compare(ub)
		}
		return 1
	}
	if !ub.IsZero() {
		return -1
	}
	if !ha.IsZero() {
		if !hb.IsZero() {
			return hb.Compare(ha)
		}
		return -1
	}
	if !hb.IsZero() {
		return 1
	}
	return 0
}

// sortByDesirability orders addrs in place, stable for equals.
func sortByDesirability(addrs []Address) {
	slices.SortStableFunc(addrs, CompareDesirability)
}

// AddressChangeKind says whether an [AddressChange] adds or deletes.
type AddressChangeKind int

// Address change kinds.
const (
	AddressAdded AddressChangeKind = iota
	AddressDeleted
)

// AddressChange is one element of [Service.AddressChanges], matter.js
// IpService.AddressChange.
type AddressChange struct {
	Kind    AddressChangeKind
	Address Address
}

type changeObserver struct{ fn func() }

type srvEntry struct {
	name         *Name
	discoveredAt time.Time
	ttl          time.Duration
	port         uint16
	priority     uint16
	weight       uint16
	observer     *nameObserver
}

// Service is the live view of one DNS-SD service name: its SRV targets,
// the addresses they resolve to, and its TXT parameters, updated as the
// [Names] cache changes. It is matter.js IpService
// (packages/general/src/net/dns-sd/IpService.ts). Close it when done; an
// open Service keeps its name and SRV targets in the cache.
type Service struct {
	names *Names

	// Loop-owned state below.
	name         *Name
	nameObserver *nameObserver
	services     *omap[string, *srvEntry]
	addresses    *omap[netip.AddrPort, Address]
	changed      []*changeObserver
	notified     bool
	paramSig     string
	reachable    bool
	status       statusState
	closed       bool
}

// NewService returns the view of qname (without the trailing dot) on
// names, seeded from the records already cached.
func NewService(names *Names, qname string) *Service {
	var s *Service
	if err := names.loop.call(func() { s = newService(names, qname) }); err != nil {
		return &Service{names: names, name: newName(qname, names), services: newOmap[string, *srvEntry](), addresses: newOmap[netip.AddrPort, Address](), closed: true}
	}
	return s
}

// newService. Mirrors matter.js 0ceb202d IpService.ts:36-52.
func newService(names *Names, qname string) *Service {
	s := &Service{
		names:     names,
		services:  newOmap[string, *srvEntry](),
		addresses: newOmap[netip.AddrPort, Address](),
	}
	s.name = names.get(qname)
	s.nameObserver = &nameObserver{fn: s.onServiceChanged}
	s.name.on(s.nameObserver)
	for _, r := range s.name.records.values() {
		if r.Type == TypeSRV {
			s.updateService(r.TTL, r.SRV.Target, r.SRV.Port, r.SRV.Priority, r.SRV.Weight)
		}
	}
	s.paramSig = s.name.parameters().signature()
	return s
}

// Name returns the service's DNS-SD name.
func (s *Service) Name() *Name { return s.name }

// Addresses returns the known addresses, most desirable first
// ([CompareDesirability]).
func (s *Service) Addresses() []Address {
	var out []Address
	_ = s.names.loop.call(func() { out = s.orderedAddresses() })
	return out
}

// Parameters returns the service's TXT parameters.
func (s *Service) Parameters() Parameters {
	var out Parameters
	_ = s.names.loop.call(func() { out = s.name.parameters() })
	return out
}

// HasTXTRecord reports whether a TXT record of the service is known, even
// one without keys. Mirrors matter.js 0ceb202d IpService.ts:103-110.
func (s *Service) HasTXTRecord() bool {
	var out bool
	_ = s.names.loop.call(func() { out = s.hasTXTRecord() })
	return out
}

// Reachable reports whether the service is believed reachable: set when a
// new address appears (matter.js IpServiceStatus.isReachable, set by
// IpService.ts:324-325), and by [Status].
func (s *Service) Reachable() bool {
	var out bool
	_ = s.names.loop.call(func() { out = s.reachable })
	return out
}

// Close releases the service's observers, letting the cache drop its
// names once nothing else holds them, and stops an active resolution of
// its [Status], waiting for it to end (matter.js IpService.ts:85-88 with
// IpServiceStatus.ts:36-41). Idempotent.
func (s *Service) Close() {
	var resolving chan struct{}
	_ = s.names.loop.call(func() {
		s.close()
		s.stopResolving()
		resolving = s.status.resolving
	})
	if resolving != nil {
		select {
		case <-resolving:
		case <-s.names.loop.done:
		}
	}
}

// AddressChanges streams the service's addresses: first every known
// address as an add, then — whenever the set changes — deletions first,
// then additions in desirability order. The channel closes when ctx ends,
// the service closes or the cache closes. Mirrors matter.js 0ceb202d
// IpService.ts:137-200 (addressChanges), including its restart: when the
// set changes while changes are being delivered, the set is read again
// and an address already counted as known in the interrupted round is
// not delivered again.
func (s *Service) AddressChanges(ctx context.Context) <-chan AddressChange {
	out := make(chan AddressChange)
	dirty := make(chan struct{}, 1)
	signal := func() {
		select {
		case dirty <- struct{}{}:
		default:
		}
	}
	var obs *changeObserver
	if err := s.names.loop.call(func() { obs = s.onChanged(signal) }); err != nil {
		close(out)
		return out
	}
	go func() {
		defer close(out)
		defer func() { s.names.loop.post(func() { s.offChanged(obs) }) }()
		var known []netip.AddrPort
		for {
			// Clear the dirty flag before reading the set.
			select {
			case <-dirty:
			default:
			}
			var addrs []Address
			closed := false
			if err := s.names.loop.call(func() { addrs, closed = s.orderedAddresses(), s.closed }); err != nil || closed {
				return
			}
			changes, next := diffAddresses(known, addrs)
			known = next
			restart := false
			for _, c := range changes {
				select {
				case out <- c:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					return
				}
				select {
				case <-dirty:
					restart = true
				default:
				}
				if restart {
					break
				}
			}
			if restart {
				continue
			}
			select {
			case <-dirty:
			case <-ctx.Done():
				return
			case <-s.names.loop.done:
				return
			}
		}
	}()
	return out
}

// diffAddresses computes one round of addressChanges: the deletions (in the
// order the previous round knew them), then the additions (in addrs's
// order), and the new known set.
func diffAddresses(known []netip.AddrPort, addrs []Address) (changes []AddressChange, next []netip.AddrPort) {
	old := make(map[netip.AddrPort]bool, len(known))
	for _, k := range known {
		old[k] = true
	}
	var adds []AddressChange
	for _, a := range addrs {
		k := a.AddrPort()
		next = append(next, k)
		if old[k] {
			delete(old, k)
			continue
		}
		adds = append(adds, AddressChange{Kind: AddressAdded, Address: a})
	}
	for _, k := range known {
		if old[k] {
			changes = append(changes, AddressChange{Kind: AddressDeleted, Address: Address{IP: k.Addr(), Port: k.Port()}})
		}
	}
	return append(changes, adds...), next
}

// --- loop-side ---

func (s *Service) orderedAddresses() []Address {
	out := s.addresses.values()
	sortByDesirability(out)
	return out
}

func (s *Service) hasTXTRecord() bool {
	for _, r := range s.name.records.values() {
		if r.Type == TypeTXT {
			return true
		}
	}
	return false
}

func (s *Service) onChanged(fn func()) *changeObserver {
	o := &changeObserver{fn: fn}
	s.changed = append(s.changed, o)
	return o
}

func (s *Service) offChanged(o *changeObserver) {
	s.changed = slices.DeleteFunc(s.changed, func(x *changeObserver) bool { return x == o })
}

func (s *Service) close() {
	if s.closed {
		return
	}
	s.closed = true
	s.name.off(s.nameObserver)
	for _, e := range s.services.values() {
		e.name.off(e.observer)
	}
}

// onServiceChanged follows the service name's SRV records and detects a
// TXT change by its parameter signature. Mirrors matter.js 0ceb202d
// IpService.ts:202-238.
func (s *Service) onServiceChanged(c Changes) {
	if s.closed {
		return
	}
	txtUpdated, txtSuperseded := false, false
	for _, r := range c.Updated {
		switch r.Type {
		case TypeSRV:
			s.updateService(r.TTL, r.SRV.Target, r.SRV.Port, r.SRV.Priority, r.SRV.Weight)
		case TypeTXT:
			txtUpdated = true
		default:
		}
	}
	for _, r := range c.Deleted {
		switch r.Type {
		case TypeSRV:
			s.deleteService(r.SRV.Target, r.SRV.Port)
		case TypeTXT:
			txtSuperseded = txtSuperseded || s.isSuperseded(r)
		default:
		}
	}
	// When the newest TXT expires, the last known parameters stay the best
	// assumption; only a superseded one may change them.
	if txtUpdated || txtSuperseded {
		if sig := s.name.parameters().signature(); sig != s.paramSig {
			s.paramSig = sig
			s.notify()
		}
	}
}

// isSuperseded. Mirrors matter.js 0ceb202d IpService.ts:240-247.
func (s *Service) isSuperseded(deleted *Record) bool {
	for _, r := range s.name.records.values() {
		if r.Type == TypeTXT && r.InstalledAt.After(deleted.InstalledAt) {
			return true
		}
	}
	return false
}

func hostKey(target string, port uint16) string {
	return target + ":" + strconv.Itoa(int(port))
}

// updateService tracks one SRV target. Mirrors matter.js 0ceb202d
// IpService.ts:249-277.
func (s *Service) updateService(ttl time.Duration, target string, port, priority, weight uint16) {
	key := hostKey(target, port)
	if e, ok := s.services.get(key); ok {
		e.discoveredAt = s.names.clock.Now()
		e.ttl, e.port, e.priority, e.weight = ttl, port, priority, weight
		return
	}
	e := &srvEntry{
		name:         s.names.get(target),
		discoveredAt: s.names.clock.Now(),
		ttl:          ttl,
		port:         port,
		priority:     priority,
		weight:       weight,
	}
	e.observer = &nameObserver{fn: func(c Changes) { s.onAddressChanged(e, c) }}
	e.name.on(e.observer)
	s.onAddressChanged(e, Changes{Name: e.name, Updated: e.name.records.values()})
	s.services.set(key, e)
}

// deleteService. Mirrors matter.js 0ceb202d IpService.ts:279-294.
func (s *Service) deleteService(target string, port uint16) {
	key := hostKey(target, port)
	e, ok := s.services.get(key)
	if !ok {
		return
	}
	s.services.del(key)
	e.name.off(e.observer)
	s.onAddressChanged(e, Changes{Name: e.name, Deleted: e.name.records.values()})
}

// onAddressChanged. Mirrors matter.js 0ceb202d IpService.ts:296-313.
func (s *Service) onAddressChanged(e *srvEntry, c Changes) {
	if s.closed {
		return
	}
	for _, r := range c.Updated {
		if ip, ok := ipOf(r); ok {
			s.updateAddress(e, ip)
		}
	}
	for _, r := range c.Deleted {
		if ip, ok := ipOf(r); ok {
			s.deleteAddress(e, ip)
		}
	}
}

// ipOf gives an A record's address and an AAAA record's, an fe80::/10 one
// zoned to the interface it arrived on. Mirrors matter.js 0ceb202d
// IpService.ts:392-408.
func ipOf(r *Record) (netip.Addr, bool) {
	if r.Type == TypeA {
		return r.IP, r.IP.IsValid()
	}
	if r.Type != TypeAAAA || !r.IP.IsValid() {
		return netip.Addr{}, false
	}
	if r.SourceIntf != "" && r.IP.IsLinkLocalUnicast() && r.IP.Zone() == "" {
		return r.IP.WithZone(r.SourceIntf), true
	}
	return r.IP, true
}

// updateAddress adds {ip, port}; a new address marks the service
// reachable. Mirrors matter.js 0ceb202d IpService.ts:315-328.
func (s *Service) updateAddress(e *srvEntry, ip netip.Addr) {
	a := Address{IP: ip, Port: e.port}
	k := a.AddrPort()
	if s.addresses.has(k) {
		return
	}
	s.addresses.set(k, a)
	s.setReachable(true)
	s.notify()
}

// deleteAddress. Mirrors matter.js 0ceb202d IpService.ts:330-340.
func (s *Service) deleteAddress(e *srvEntry, ip netip.Addr) {
	k := netip.AddrPortFrom(ip, e.port)
	if !s.addresses.del(k) {
		return
	}
	s.notify()
}

// notify coalesces change notifications into one per macrotask. Mirrors
// matter.js 0ceb202d IpService.ts:342-355.
func (s *Service) notify() {
	if s.notified {
		return
	}
	s.notified = true
	s.names.loop.macrotask(func() {
		s.notified = false
		for _, o := range slices.Clone(s.changed) {
			o.fn()
		}
	})
}
