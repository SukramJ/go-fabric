// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/clockvar"
	"github.com/SukramJ/go-fabric/internal/netwatch"
)

// Responder is the production [Advertiser]: the module's own mDNS
// responder on UDP/5353, modelled on matter.js's MdnsServer and
// MdnsAdvertisement (packages/protocol/src/mdns/MdnsServer.ts,
// packages/protocol/src/advertisement/mdns/ at 862ad75f).
//
// For every published [Service] it holds the full record set — the
// service-type enumeration PTRs, the service and subtype PTRs (`_L`, `_S`,
// `_CM`, `_T`, `_V`, the fabric subtype), SRV, TXT and the host's A/AAAA
// records, each with a TTL of 120 s — and:
//
//   - answers every question about it, per interface, with additionals,
//     known-answer suppression, unicast responses, truncated queries, rate
//     limiting and duplicate-question suppression (answer.go);
//   - announces it on Publish and keeps announcing on matter.js's broadcast
//     schedule — 1 s, doubling, at most 90 s apart — until an operational
//     connection ends the run or two days pass (announce.go);
//   - says goodbye (TTL 0) to exactly the records peers may cache that no
//     published service holds any more — on Withdraw, Close and a changed
//     re-Publish, never to a host address or a shared pointer another
//     service still publishes — and retries a goodbye that did not leave
//     (publication.go);
//   - follows the host's interfaces while services are published: on an
//     address change it says goodbye to the lost addresses and announces
//     the current records on the changed interfaces, again a second later
//     (follow.go).
//
// It does not probe, as neither matter.js nor chip does. The multicast
// sockets are opened on the first Publish, so constructing a Responder
// binds nothing; Close releases them.
//
// The host's A/AAAA records are published for the host name the module
// owns — the MAC-derived default. A host name the OS responder owns, the
// [Responder.HostName] override or the OS host name kept on macOS
// (notes/parity/by_design.md L7-D03), is left to the OS: its address
// records are not published or answered here.
type Responder struct {
	// InterfaceFilter decides which host interfaces are kept out of the
	// advertised records: it reports true for an interface name whose
	// addresses must NOT be published, and nothing is announced or answered
	// on such an interface. [NewResponder] installs the container- and
	// virtualisation-bridge filter; nil falls back to it. Set it before the
	// first Publish.
	InterfaceFilter func(name string) bool
	// HostName, when set, is the SRV target of every record this
	// advertiser publishes, in place of the one the Service carries (the
	// MAC-derived default the Build* helpers fill in). It names a host the
	// OS responder publishes the address records of — a test host whose
	// LAN interface has no IPv6 sets the OS host name here, whose records
	// cover every interface. Set it before the first Publish.
	HostName string

	// log is the responder's logger, set by [Responder.SetLogger].
	log atomic.Pointer[slog.Logger]
	// clock paces the broadcast schedule and the answer windows.
	clock clockvar.Var
	// random32, when set, replaces the schedule's jitter source (tests).
	random32 func() uint32

	// mu guards everything below and serialises every send, so a goodbye
	// cannot overtake an announcement of the same records.
	mu       sync.Mutex
	closed   bool
	services map[string]*entry
	// order is the publish order of the service keys — matter.js's
	// generator Map iterates in insertion order.
	order []string
	links map[int]*linkRecords
	// linkStates is what peers may cache on each interface and its
	// goodbye retry (publication.go).
	linkStates map[int]*linkState
	answers    answerState
	// repeatOn are the interfaces the change repeat publishes on again,
	// nil when none waits; changeRepeat is its timer (follow.go).
	repeatOn     map[int]bool
	changeRepeat clock.Timer
	// changeRepeatGen tells a fired repeat timer from the current one.
	changeRepeatGen uint64
	// wire is the transport; nil until the first Publish opens the
	// sockets (or a test installs a fake).
	wire wire
	// socks are the real sockets behind wire, nil with a test wire.
	socks *sockets

	// watch compares the interfaces while services are published
	// (follow.go); followMu serialises starting and stopping it, outside
	// mu.
	watch    *netwatch.Watcher
	followMu sync.Mutex
}

// entry is one published service.
type entry struct {
	key string
	// svc is the service as Active reports it.
	svc   Service
	names serviceNames
	txt   []string
	// ownsHost is false when the OS responder owns the host name, whose
	// address records are then left out.
	ownsHost bool
	// fingerprint is the record set's identity: a re-Publish with the
	// same one only re-announces.
	fingerprint string
	run         *broadcastRun
	// gen, when set, replaces the record set derived from svc: the seam
	// the parity tests install matter.js's record-generator fixtures
	// through (MdnsServer.setRecordsGenerator). Nil in production.
	gen func() []dns.RR
}

// wire is the responder's transport: the interfaces it sends on, the
// addresses it advertises on each, and the two send paths.
type wire interface {
	// links lists the indices of the up, multicast-capable interfaces.
	links() []int
	// addrs is what the advertise policy publishes for the interface:
	// none for one it excludes (loopback, tunnel, container bridge, down).
	addrs(ifIndex int) []net.IP
	// multicast sends pkt to the mDNS groups on the interface and reports
	// how far it reached.
	multicast(pkt []byte, ifIndex int) reach
	// unicast sends pkt to dst through the interface and reports whether
	// it left.
	unicast(pkt []byte, ifIndex int, dst *net.UDPAddr) reach
}

// NewResponder returns the module's mDNS responder. It binds nothing until
// the first Publish; the caller closes it ([Responder.Close]).
func NewResponder() *Responder {
	r := &Responder{
		InterfaceFilter: isVirtualInterfaceName,
		services:        map[string]*entry{},
		links:           map[int]*linkRecords{},
		linkStates:      map[int]*linkState{},
		answers:         newAnswerState(),
	}
	r.watch = netwatch.New(r.clock.Load, r.interfaceSnapshot, r.interfacesChanged)
	return r
}

// SetClock sets the clock the broadcast schedule and the answer windows run
// on; nil is [clock.System].
func (r *Responder) SetClock(c clock.Clock) { r.clock.Store(c) }

// SetLogger sets the logger the responder's diagnostics go to; nil is
// slog.Default(). It is safe to call at any time.
func (r *Responder) SetLogger(l *slog.Logger) { r.log.Store(l) }

// logger is the responder's logger, slog.Default() until SetLogger gives
// it one.
func (r *Responder) logger() *slog.Logger {
	if l := r.log.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// Enabled implements [Enabled]: the responder publishes on the network.
func (r *Responder) Enabled() bool { return true }

// filter is InterfaceFilter or its default.
func (r *Responder) filter() func(string) bool {
	if r.InterfaceFilter != nil {
		return r.InterfaceFilter
	}
	return isVirtualInterfaceName
}

// ensureWireLocked opens the sockets on first use. Caller holds r.mu.
func (r *Responder) ensureWireLocked() error {
	if r.wire != nil {
		return nil
	}
	s, err := openSockets(r.filter(), r.logger(), &r.clock, r.receive)
	if err != nil {
		return err
	}
	r.socks = s
	r.wire = s
	return nil
}

// receive is the sockets' inbound path.
func (r *Responder) receive(buf []byte, ifIndex int, src *net.UDPAddr) {
	msg := new(dns.Msg)
	if err := msg.Unpack(buf); err != nil || msg.Response {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receiveLocked(msg, ifIndex, src)
}

// Publish implements [Advertiser]: it installs svc's record set, replacing
// one with the same instance and service type, and announces it on the
// broadcast schedule. A re-Publish of an unchanged record set only starts
// the schedule again when none runs (Advertisement.start); a changed one
// replaces the registration in place and starts the schedule afresh, whose
// first publication says goodbye to the records that left and announces
// the new ones in one message — matter.js registers the new generator
// under the service's name (MdnsServer.register, MdnsServer.ts:123-155 at
// db021da0) and its broadcast carries the goodbyes
// (InterfaceResponder.ts:271-302).
//
// The advertised addresses are the host's, chosen per interface by the
// advertise policy; the copy [Responder.Active] returns carries the union.
func (r *Responder) Publish(_ context.Context, svc Service) error {
	if err := svc.Validate(); err != nil {
		return err
	}
	defer r.follow()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("mdns: responder is closed")
	}
	if err := r.ensureWireLocked(); err != nil { //nolint:contextcheck // the sockets live until Close, not for the Publish call
		return fmt.Errorf("mdns: responder %s/%s: %w", svc.InstanceName, svc.ServiceType, err)
	}
	key := noopKey(svc.InstanceName, svc.ServiceType)
	host := svc.HostName
	if r.HostName != "" {
		host = r.HostName
	}
	if host == "" {
		host = defaultHostName()
	}
	ips := primaryHostIPs(r.filter())
	// The copy kept for Active carries the addresses the A/AAAA records
	// carry: the operator-facing diagnostics (Diagnose) inspect them.
	svc.Addresses = parseHostIPs(ips)
	txt := svc.MarshalTXT()
	fp := fmt.Sprintf("%s|%d|%s|%s|%s",
		strings.Join(txt, "\x1f"), svc.Port, host,
		strings.Join(ips, ","), strings.Join(svc.Subtypes, ","))
	prev, replacing := r.services[key]
	if replacing {
		if prev.fingerprint == fp {
			prev.svc = svc
			r.startLocked(prev, eventStartup)
			return nil
		}
		r.stopLocked(prev)
	}
	e := &entry{
		key:         key,
		svc:         svc,
		names:       namesOf(svc, host),
		txt:         txt,
		ownsHost:    r.HostName == "" && !strings.EqualFold(strings.TrimSuffix(strings.Trim(host, "."), ".local"), osHostName()),
		fingerprint: fp,
	}
	r.services[key] = e
	if !replacing {
		// A Map keeps a replaced key's position (MdnsServer.ts:142).
		r.order = append(r.order, key)
	}
	r.resetLocked()
	r.startLocked(e, eventStartup)
	return nil
}

// removeLocked stops e's run, drops it and publishes the goodbyes of the
// records no remaining service holds on every interface (MdnsServer.ts
// #withdraw, :165-178 at db021da0). Caller holds r.mu.
func (r *Responder) removeLocked(e *entry) {
	r.stopLocked(e)
	delete(r.services, e.key)
	r.order = slices.DeleteFunc(r.order, func(k string) bool { return k == e.key })
	r.resetLocked()
	r.publishEverywhereLocked(nil, false)
}

// resetLocked drops the cached record sets and the answer engine's memory
// after the records changed (matter.js #resetServices, MdnsServer.ts:282-291
// at 862ad75f). Caller holds r.mu.
func (r *Responder) resetLocked() {
	clear(r.links)
	r.answers.reset()
}

// Withdraw implements [Advertiser]: it stops answering for the service and
// says goodbye — TTL 0 — to its records that no remaining service
// publishes.
func (r *Responder) Withdraw(_ context.Context, instanceName, serviceType string) error {
	defer r.follow()
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.services[noopKey(instanceName, serviceType)]
	if !ok {
		return fmt.Errorf("%w: %s/%s", ErrServiceNotFound, instanceName, serviceType)
	}
	r.removeLocked(e)
	return nil
}

// Active implements [Advertiser].
func (r *Responder) Active() []Service {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Service, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.services[k].svc)
	}
	return out
}

// TriggerReannounce announces every published service now, outside the
// broadcast schedule, with the goodbyes of the records that left — for a
// host that observed a link change (interface up, address assigned). The
// interfaces' addresses are read again. It is matter.js
// MdnsServer.broadcast with no service named: one publication per
// interface (MdnsServer.ts:210-223 at db021da0). [Responder.CheckInterfaces]
// is the narrower call, which publishes only on the interfaces that
// changed and repeats it a second later.
func (r *Responder) TriggerReannounce(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.resetLocked()
	r.broadcastAllLocked()
}

// ServiceConnected tells the responder that a client connection to the
// service is established: its broadcast run ends, as matter.js's
// DefaultBroadcastSchedule has broadcastAfterConnection Instant
// (MdnsAdvertisement.ts:117-130, MdnsAdvertiser.ts:230-231 at 862ad75f).
// The service keeps answering queries. The node calls it for a fabric's
// operational service when a CASE session opens (DeviceAdvertiser.ts:114-129).
func (r *Responder) ServiceConnected(instanceName, serviceType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.services[noopKey(instanceName, serviceType)]; ok {
		r.stopLocked(e)
	}
}

// ServiceDisconnected tells the responder that no client connection to the
// service is left: the broadcast schedule runs again unless it runs
// already — matter.js serviceDisconnected starts the "reconnect" event
// (MdnsAdvertisement.ts:132-134, DeviceAdvertiser.ts:131-150 at 862ad75f).
func (r *Responder) ServiceDisconnected(instanceName, serviceType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.services[noopKey(instanceName, serviceType)]; ok {
		r.startLocked(e, eventReconnect)
	}
}

// ServiceRetransmission tells the responder that a message to a client of
// the service was retransmitted: one broadcast, unless a run is under way —
// matter.js's "retransmit" event and its RetransmissionBroadcastSchedule
// (DeviceAdvertiser.ts:164-172, MdnsAdvertiser.ts:234-242 at 862ad75f).
func (r *Responder) ServiceRetransmission(instanceName, serviceType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.services[noopKey(instanceName, serviceType)]; ok {
		r.startLocked(e, eventRetransmit)
	}
}

// Close implements [Advertiser]: every service is withdrawn — one goodbye
// per interface for every record peers may cache there — then the sockets
// close. No goodbye is retried after Close. Idempotent.
func (r *Responder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	for _, k := range r.order {
		r.stopLocked(r.services[k])
	}
	for _, t := range r.answers.truncated {
		t.timer.Stop()
	}
	clear(r.answers.truncated)
	clear(r.services)
	r.order = nil
	r.resetLocked()
	r.publishEverywhereLocked(nil, false)
	r.stopAllGoodbyeRetriesLocked()
	clear(r.linkStates)
	r.stopChangeRepeatLocked()
	r.closed = true
	socks := r.socks
	r.mu.Unlock()
	r.follow()
	// The receive loops take r.mu; they are waited for outside it.
	if socks != nil {
		return socks.close()
	}
	return nil
}

// osHostName is the OS host name without a ".local" suffix, "" if unknown.
func osHostName() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(h, "."), ".local")
}

var (
	_ Advertiser = (*Responder)(nil)
	_ Enabled    = (*Responder)(nil)
)
