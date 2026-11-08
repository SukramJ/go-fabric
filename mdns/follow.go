// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"cmp"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/internal/netwatch"
)

// Following the host's interfaces. While services are published the
// responder compares the advertised addresses of every interface at the
// interface check interval — matter.js MdnsServer observes
// Network.interfacesChanged while services are registered (#followInterfaces,
// MdnsServer.ts:291-304 at db021da0), and the Network checks every 60 s
// while observed (Network.ts:91-95, :142-172). On a change the records of
// the changed interfaces are built again and published there: the
// publication says goodbye to the addresses an interface lost and
// announces the current records, and one second later they are announced
// once more (RFC 6762 §8.3/§8.4; #publishChange and #repeatChange,
// :321-349; CHANGE_REPEAT_INTERVAL, :32-33).
//
// The MAC-derived host name is the Service's (the default the Build*
// helpers fill in, or [Responder.HostName]) and not chosen again here. On
// macOS the verdict on the OS host name of a defaulted Service is taken
// again on every change ([hostNamePolicy]); when it moves the host name,
// every service is published on every interface and repeated on every
// interface, as matter.js does when its host name changed (:331-338;
// #updateHost, :352-355). The publication carries the goodbyes of the old
// host name's records, which no service holds any more (publication.go).

// changeRepeatInterval is RFC 6762 §8.3's second announcement of a change,
// matter.js CHANGE_REPEAT_INTERVAL (MdnsServer.ts:32-33 at db021da0).
const changeRepeatInterval = time.Second

// SetInterfaceCheckInterval sets how often the responder compares the
// host's interfaces while services are published; zero or negative
// disables the periodic comparison ([Responder.CheckInterfaces] still
// runs one). The default is 60 s, matter.js INTERFACE_CHECK_INTERVAL
// (Network.ts:91-95 at db021da0).
func (r *Responder) SetInterfaceCheckInterval(d time.Duration) {
	r.watch.SetInterval(d)
}

// CheckInterfaces compares the host's interfaces now and publishes the
// changes as the periodic check does — for a host that learns of a change
// before the next check (matter.js Network.checkInterfaces,
// Network.ts:174-186 at db021da0). It does nothing while no service is
// published.
func (r *Responder) CheckInterfaces() {
	r.watch.Check()
}

// follow starts the interface comparison while services are published and
// the responder is open, and stops it otherwise. It runs outside r.mu,
// after the change that may move it; followMu orders concurrent calls, so
// the last one applies the current state.
func (r *Responder) follow() {
	r.followMu.Lock()
	defer r.followMu.Unlock()
	r.mu.Lock()
	want := !r.closed && len(r.order) > 0
	if !want {
		r.stopChangeRepeatLocked()
	}
	r.mu.Unlock()
	if want {
		r.watch.Start()
	} else {
		r.watch.Stop()
	}
}

// interfaceSnapshot describes each interface the responder sends on by
// the addresses the advertise policy publishes there — the inputs of its
// records. nil (no wire yet) keeps the previous snapshot.
func (r *Responder) interfaceSnapshot() map[string]string {
	r.mu.Lock()
	w := r.wire
	r.mu.Unlock()
	if w == nil {
		return nil
	}
	out := map[string]string{}
	for _, l := range w.links() {
		ips := make([]string, 0, len(l.addrs))
		for _, ip := range l.addrs {
			ips = append(ips, ip.String())
		}
		out[strconv.Itoa(l.index)] = netwatch.Describe("", ips)
	}
	return out
}

// interfacesChanged takes the host-name verdict again, then publishes on
// the interfaces that changed — on every interface when the host name
// moved — and arms the repeat (MdnsServer.ts #publishChange, :321-339 at
// db021da0: previousHostname, #updateHost, "all" when the name changed).
func (r *Responder) interfacesChanged(names []string) {
	kept, retaken := r.retakeHostName()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.order) == 0 || r.wire == nil {
		return
	}
	if retaken && r.moveHostLocked(kept) {
		r.repeatAll, r.repeatOn = true, nil
		r.armChangeRepeatLocked()
		r.publishEverywhereLocked(nil, true)
		return
	}
	changed := map[int]bool{}
	for _, name := range names {
		ifIndex, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		changed[ifIndex] = true
		// The interface's records are built again from its addresses.
		delete(r.links, ifIndex)
	}
	if !r.repeatAll {
		if r.repeatOn == nil {
			r.repeatOn = map[int]bool{}
		}
		for i := range changed {
			r.repeatOn[i] = true
		}
	}
	r.armChangeRepeatLocked()
	r.publishOnListedLocked(changed)
}

// armChangeRepeatLocked (re)starts the change repeat's timer. Caller holds
// r.mu.
func (r *Responder) armChangeRepeatLocked() {
	r.stopChangeRepeatTimerLocked()
	// The timer's generation: a repeat that fired before the re-arm and
	// waits for r.mu is stale and must neither publish early nor drop the
	// new timer (MdnsServer.ts:337 stop().start() at db021da0).
	gen := r.changeRepeatGen
	r.changeRepeat = r.clock.Load().AfterFunc(changeRepeatInterval, func() { r.repeatChange(gen) })
}

// retakeHostName takes the host-name verdict again, outside r.mu — the
// lookup may take up to osHostNameLookupTimeout — when no override is set
// and a published service carries a defaulted host name. kept is the OS
// host name kept, "" when each service's own (MAC-derived) name applies;
// retaken is false when there was nothing to take.
func (r *Responder) retakeHostName() (kept string, retaken bool) {
	if r.HostName != "" {
		return "", false
	}
	r.mu.Lock()
	var fallback string
	for _, k := range r.order {
		if e := r.services[k]; e.svc.hostNameDefaulted {
			fallback = e.svc.HostName
			break
		}
	}
	r.mu.Unlock()
	if fallback == "" {
		return "", false
	}
	p := r.hostPolicy()
	p.retake()
	return p.keptHostName(r.logger(), fallback), true
}

// moveHostLocked gives every service with a defaulted host name the host
// name of the verdict — kept, or its own MAC-derived one — and reports
// whether one moved. A moved service keeps its place and its broadcast
// run, as a matter.js registration outlives a host-name change; its
// records, ownership of the address records and fingerprint follow the new
// name. Caller holds r.mu.
func (r *Responder) moveHostLocked(kept string) bool {
	moved := false
	for _, k := range r.order {
		e := r.services[k]
		if e.gen != nil || !e.svc.hostNameDefaulted {
			continue
		}
		host := cmp.Or(kept, e.svc.HostName)
		if host == e.host {
			continue
		}
		e.host = host
		e.names = namesOf(e.svc, host)
		e.ownsHost = r.ownsHostName(host)
		e.fingerprint = fingerprintOf(e.txt, e.svc, host, e.ips)
		moved = true
	}
	if moved {
		r.resetLocked()
	}
	return moved
}

// repeatChange publishes once more on the interfaces of the changes since
// the last repeat (MdnsServer.ts #repeatChange, :341-349 at db021da0).
func (r *Responder) repeatChange(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.changeRepeatGen {
		return // a stale timer; the current one repeats
	}
	on, all := r.repeatOn, r.repeatAll
	r.repeatOn, r.repeatAll = nil, false
	r.changeRepeat = nil
	if r.closed || (on == nil && !all) || r.wire == nil {
		return
	}
	if all {
		on = nil
	}
	r.publishOnListedLocked(on)
}

// publishOnListedLocked publishes every service on each listed interface
// in only, on every one when only is nil (MdnsServer.ts
// #publishOnListedInterfaces, :254-284 at db021da0). Caller holds r.mu.
func (r *Responder) publishOnListedLocked(only map[int]bool) {
	for _, l := range r.listLocked() {
		if only == nil || only[l.index] {
			r.publishLocked(l, nil, true)
		}
	}
}

// stopChangeRepeatLocked drops a waiting repeat (MdnsServer.ts:299-302 at
// db021da0). Caller holds r.mu.
func (r *Responder) stopChangeRepeatLocked() {
	r.stopChangeRepeatTimerLocked()
	r.repeatOn, r.repeatAll = nil, false
}

func (r *Responder) stopChangeRepeatTimerLocked() {
	r.changeRepeatGen++
	if r.changeRepeat != nil {
		r.changeRepeat.Stop()
		r.changeRepeat = nil
	}
}
