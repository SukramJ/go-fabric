// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"net/netip"
	"strings"

	"github.com/SukramJ/go-fabric/mdns"
)

// OperationalQName is the DNS-SD name of a node's operational instance,
// `<CFID>-<NODEID>._matter._tcp.local`, both ids as 16 upper-case hex
// digits, without the trailing dot. Mirrors matter.js 0ceb202d
// packages/protocol/src/mdns/MdnsConsts.ts:36-37
// (getOperationalDeviceQname); it is [mdns.OperationalInstanceQName]
// without the root dot.
func OperationalQName(compressedFabricID [8]byte, nodeID uint64) string {
	return strings.TrimSuffix(mdns.OperationalInstanceQName(compressedFabricID, nodeID), ".")
}

// Service returns the operational service of nodeID on the fabric with
// compressedFabricID — the view a matter.js Peer owns for its node
// (packages/protocol/src/peer/Peer.ts:108-117). Close it when done.
func (n *Names) Service(compressedFabricID [8]byte, nodeID uint64) *Service {
	return NewService(n, OperationalQName(compressedFabricID, nodeID))
}

// Resolve queries for the service until an address it did not know when
// Resolve started appears, and returns the addresses then known, most
// desirable first. It asks for SRV and TXT of the service name — with its
// SRV targets' records as known answers — and for A and AAAA of every SRV
// target that has no address yet, on the solicitor's default schedule,
// following the SRV targets as they change. It returns ctx's error when
// ctx ends first and [ErrClosed] when the cache closes. Mirrors matter.js
// 0ceb202d packages/general/src/net/dns-sd/IpServiceResolution.ts:26-134.
func (s *Service) Resolve(ctx context.Context) ([]Address, error) {
	found := make(chan struct{})
	var stop func()
	if err := s.names.loop.call(func() { stop = s.startResolution(found) }); err != nil {
		return nil, err
	}
	var err error
	select {
	case <-found:
	case <-ctx.Done():
		err = ctx.Err()
	case <-s.names.loop.done:
		return nil, ErrClosed
	}
	var addrs []Address
	if cerr := s.names.loop.call(func() {
		stop()
		addrs = s.orderedAddresses()
	}); cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, err
	}
	return addrs, nil
}

// startResolution runs IpServiceResolution on the loop; found is closed
// when a new address appears. The returned function stops it (idempotent).
func (s *Service) startResolution(found chan struct{}) func() {
	hosts := newOmap[*Name, struct{}]()
	resolvers := make(map[*Name]func())
	stopped := false
	var mainCancel func()
	var observer *changeObserver

	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if mainCancel != nil {
			mainCancel()
		}
		for nm, cancel := range resolvers {
			cancel()
			delete(resolvers, nm)
		}
		if observer != nil {
			s.offChanged(observer)
		}
		close(found)
	}

	// A host with no A/AAAA gets its own discovery (:39-64).
	addHost := func(nm *Name) {
		if stopped || hosts.has(nm) {
			return
		}
		hosts.set(nm, struct{}{})
		for _, r := range nm.records.values() {
			if r.Type == TypeA || r.Type == TypeAAAA {
				return
			}
		}
		cancel, _ := s.names.solicitor.discover(&solicitation{name: nm, types: []mdns.DNSType{TypeA, TypeAAAA}}, nil)
		resolvers[nm] = cancel
	}
	// A host no longer targeted stops resolving (:66-75).
	deleteHost := func(nm *Name) {
		hosts.del(nm)
		if cancel, ok := resolvers[nm]; ok {
			delete(resolvers, nm)
			cancel()
		}
	}

	// Initial SRV targets (:77-84).
	for _, r := range s.name.records.values() {
		if r.Type == TypeSRV {
			addHost(s.names.get(r.SRV.Target))
		}
	}

	// A new address ends the resolution; SRV changes update the hosts
	// (:86-117).
	existing := make(map[netip.AddrPort]bool, s.addresses.len())
	for _, a := range s.addresses.values() {
		existing[a.AddrPort()] = true
	}
	observer = s.onChanged(func() {
		if stopped {
			return
		}
		for _, a := range s.addresses.values() {
			if !existing[a.AddrPort()] {
				stop()
				return
			}
		}
		targets := make(map[string]bool)
		for _, r := range s.name.records.values() {
			if r.Type == TypeSRV {
				targets[r.SRV.Target] = true
				addHost(s.names.get(r.SRV.Target))
			}
		}
		for _, nm := range hosts.snapshotKeys() {
			if !targets[nm.qname] {
				deleteHost(nm)
			}
		}
	})

	// SRV and TXT of the service, TXT explicitly because some responders
	// leave it out of the additional records (:119-130).
	mainCancel, _ = s.names.solicitor.discover(&solicitation{
		name:       s.name,
		types:      []mdns.DNSType{TypeSRV, TypeTXT},
		associated: &associated{fn: hosts.snapshotKeys},
	}, nil)
	return stop
}
