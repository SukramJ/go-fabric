// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/mdns"
)

// Instance is one instance of a DNS-SD service type that [Names.Browse]
// found: its name, the addresses its SRV targets resolve to and its TXT
// parameters.
type Instance struct {
	// Name is the instance label, QName without the service type
	// ("kitchen-proxy").
	Name string
	// QName is the instance's service name without the trailing dot
	// ("kitchen-proxy._esphomelib._tcp.local").
	QName string
	// Addresses are the instance's addresses, most desirable first
	// ([CompareDesirability]); each carries its SRV port.
	Addresses []Address
	// Parameters are the instance's TXT entries.
	Parameters Parameters
}

// errServiceType is a Browse without a service type.
var errServiceType = errors.New("discovery: browse: empty service type")

// Browse finds the instances of one DNS-SD service type — any type, not
// only Matter's, e.g. "_esphomelib._tcp.local" — until ctx ends, and
// returns those advertised and resolved to an address by then, ordered by
// QName. It queries the type's PTR on the solicitor's retry schedule, asks
// for the SRV and TXT an instance lacks and the A and AAAA of every SRV
// target, as [Scanner] does for commissionable nodes. While it runs the
// cache admits the type's records, and the instances already cached from
// an earlier browse count at once. It returns [ErrClosed] when the cache
// closes.
//
// matter.js browses no foreign service type: its DnssdNames
// (packages/general/src/net/dns-sd/DnssdNames.ts at 862ad75f) is generic,
// and MdnsService narrows it to Matter's types
// (packages/protocol/src/mdns/MdnsService.ts:27, :90-94). Browse widens
// the same cache for one type by an ingress filter, as the commissionable
// scanner does (CommissionableMdnsScanner.ts:100-105).
func (n *Names) Browse(ctx context.Context, serviceType string) ([]Instance, error) {
	st := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(serviceType)), ".")
	if st == "" {
		return nil, errServiceType
	}
	b := &browse{n: n, st: st, instances: newOmap[string, *browsed](), hosts: map[string]bool{}}
	if err := n.loop.call(b.start); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
	case <-n.loop.done:
		return nil, ErrClosed
	}
	var out []Instance
	if err := n.loop.call(func() {
		out = b.collect()
		b.stop()
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// browse is one running [Names.Browse]; loop-owned.
type browse struct {
	n         *Names
	st        string
	filter    *ingressFilter
	ptr       *Name
	ptrObs    *nameObserver
	instances *omap[string, *browsed]
	hosts     map[string]bool
	cancel    func()
}

// browsed is one instance a browse follows.
type browsed struct {
	svc *Service
	obs *nameObserver
}

func (b *browse) matches(r *Record) bool {
	l := strings.ToLower(strings.TrimSuffix(r.Name, "."))
	return l == b.st || strings.HasSuffix(l, "."+b.st)
}

func (b *browse) start() {
	b.filter = b.n.addFilter(b.matches)
	b.ptr = b.n.get(b.st)
	b.ptrObs = &nameObserver{fn: func(Changes) { b.follow() }}
	b.ptr.on(b.ptrObs)
	b.follow()
	b.cancel, _ = b.n.solicitor.discover(&solicitation{name: b.ptr, types: []mdns.DNSType{TypePTR}}, nil)
}

// follow opens a view of every instance the type's PTR records name.
func (b *browse) follow() {
	for _, r := range b.ptr.records.values() {
		if r.Type != TypePTR || !b.matches(&Record{Name: r.PTR}) {
			continue
		}
		b.track(strings.TrimSuffix(r.PTR, "."))
	}
}

// track follows one instance: its SRV and TXT asked for when missing, the
// address of each SRV target once.
func (b *browse) track(qname string) {
	key := strings.ToLower(qname)
	if b.instances.has(key) {
		return
	}
	in := &browsed{svc: newService(b.n, qname)}
	nm := in.svc.name
	in.obs = &nameObserver{fn: func(c Changes) { b.resolveHosts(c.Name) }}
	nm.on(in.obs)
	b.instances.set(key, in)
	hasSRV, hasTXT := false, false
	for _, r := range nm.records.values() {
		hasSRV = hasSRV || r.Type == TypeSRV
		hasTXT = hasTXT || r.Type == TypeTXT
	}
	var types []mdns.DNSType
	if !hasSRV {
		types = append(types, TypeSRV)
	}
	if !hasTXT {
		types = append(types, TypeTXT)
	}
	if len(types) > 0 {
		b.n.solicitor.solicit(&solicitation{name: nm, types: types})
	}
	b.resolveHosts(nm)
}

// resolveHosts asks once for the addresses of an SRV target that has none.
func (b *browse) resolveHosts(nm *Name) {
	for _, r := range nm.records.values() {
		if r.Type != TypeSRV {
			continue
		}
		host := strings.ToLower(strings.TrimSuffix(r.SRV.Target, "."))
		if b.hosts[host] {
			continue
		}
		b.hosts[host] = true
		h := b.n.get(r.SRV.Target)
		resolved := false
		for _, hr := range h.records.values() {
			resolved = resolved || hr.Type == TypeA || hr.Type == TypeAAAA
		}
		if !resolved {
			b.n.solicitor.solicit(&solicitation{name: h, types: []mdns.DNSType{TypeA, TypeAAAA}})
		}
	}
}

// collect is the advertised instances with an address.
func (b *browse) collect() []Instance {
	advertised := map[string]bool{}
	for _, r := range b.ptr.records.values() {
		if r.Type == TypePTR {
			advertised[strings.ToLower(strings.TrimSuffix(r.PTR, "."))] = true
		}
	}
	var out []Instance
	for _, key := range b.instances.snapshotKeys() {
		in, _ := b.instances.get(key)
		if !advertised[key] {
			continue
		}
		addrs := in.svc.orderedAddresses()
		if len(addrs) == 0 {
			continue
		}
		q := in.svc.name.qname
		label := q
		if len(q) > len(b.st)+1 {
			label = q[:len(q)-len(b.st)-1]
		}
		out = append(out, Instance{Name: label, QName: q, Addresses: addrs, Parameters: in.svc.name.parameters()})
	}
	slices.SortFunc(out, func(x, y Instance) int {
		return cmp.Compare(strings.ToLower(x.QName), strings.ToLower(y.QName))
	})
	return out
}

func (b *browse) stop() {
	b.cancel()
	b.n.deleteFilter(b.filter)
	for _, in := range b.instances.values() {
		in.svc.name.off(in.obs)
		in.svc.close()
	}
	b.ptr.off(b.ptrObs)
}
