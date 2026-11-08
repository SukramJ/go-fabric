// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
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
// The host name is the Service's (the MAC-derived default the Build*
// helpers fill in, or [Responder.HostName]), not chosen again here, so a
// change never moves every interface at once as matter.js's host-name
// re-choice does (:331-333).

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

// interfacesChanged publishes on the interfaces that changed and arms the
// repeat (MdnsServer.ts #publishChange, :321-339 at db021da0).
func (r *Responder) interfacesChanged(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.order) == 0 || r.wire == nil {
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
	if r.repeatOn == nil {
		r.repeatOn = map[int]bool{}
	}
	for i := range changed {
		r.repeatOn[i] = true
	}
	r.stopChangeRepeatTimerLocked()
	// The timer's generation: a repeat that fired before the re-arm and
	// waits for r.mu is stale and must neither publish early nor drop the
	// new timer (MdnsServer.ts:337 stop().start() at db021da0).
	gen := r.changeRepeatGen
	r.changeRepeat = r.clock.Load().AfterFunc(changeRepeatInterval, func() { r.repeatChange(gen) })
	r.publishOnListedLocked(changed)
}

// repeatChange publishes once more on the interfaces of the changes since
// the last repeat (MdnsServer.ts #repeatChange, :341-349 at db021da0).
func (r *Responder) repeatChange(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.changeRepeatGen {
		return // a stale timer; the current one repeats
	}
	on := r.repeatOn
	r.repeatOn = nil
	r.changeRepeat = nil
	if r.closed || on == nil || r.wire == nil {
		return
	}
	r.publishOnListedLocked(on)
}

// publishOnListedLocked publishes every service on each listed interface
// in only (MdnsServer.ts #publishOnListedInterfaces, :254-284 at
// db021da0). Caller holds r.mu.
func (r *Responder) publishOnListedLocked(only map[int]bool) {
	for _, l := range r.listLocked() {
		if only[l.index] {
			r.publishLocked(l, nil, true)
		}
	}
}

// stopChangeRepeatLocked drops a waiting repeat (MdnsServer.ts:299-302 at
// db021da0). Caller holds r.mu.
func (r *Responder) stopChangeRepeatLocked() {
	r.stopChangeRepeatTimerLocked()
	r.repeatOn = nil
}

func (r *Responder) stopChangeRepeatTimerLocked() {
	r.changeRepeatGen++
	if r.changeRepeat != nil {
		r.changeRepeat.Stop()
		r.changeRepeat = nil
	}
}
