// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// The responder's tests run it over a fake transport and the fake clock —
// matter.js's MdnsServerTest drives MdnsServer through a simulated
// UdpMulticastServer under MockTime (packages/protocol/test/mdns/
// MdnsServerTest.ts:42-79 at 862ad75f). No socket, no sleep.

// sentPacket is one packet the responder sent.
type sentPacket struct {
	msg     *dns.Msg
	ifIndex int
	// dst is the unicast target; nil for multicast.
	dst *net.UDPAddr
}

// fakeWire is a [wire] that records what is sent. Interface 1 is the one
// the tests query on; addrsByLink names the advertised addresses of each
// interface (an interface without any is one the policy excludes).
type fakeWire struct {
	mu          sync.Mutex
	addrsByLink map[int][]net.IP
	sent        []sentPacket
	// fail, when set, decides how far a multicast reaches: reachAll sends
	// it, reachFailed and reachNone drop it, reachSome records it as sent
	// on some families only (matter.js MdnsServerTransportTest's failing
	// socket).
	fail func(m *dns.Msg) reach
	// failUnicast, when set, decides how far a unicast datagram reaches.
	failUnicast func(m *dns.Msg) reach
	// turn lets the clock's due timers run — the announcement of this
	// turn — before take reads the wire (newWiredResponder sets it).
	turn func()
}

func newFakeWire(addrsByLink map[int][]string) *fakeWire {
	w := &fakeWire{addrsByLink: map[int][]net.IP{}}
	for i, as := range addrsByLink {
		for _, a := range as {
			w.addrsByLink[i] = append(w.addrsByLink[i], net.ParseIP(a))
		}
	}
	return w
}

func (w *fakeWire) links() []link {
	w.mu.Lock()
	defer w.mu.Unlock()
	idx := make([]int, 0, len(w.addrsByLink))
	for i := range w.addrsByLink {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	out := make([]link, 0, len(idx))
	for _, i := range idx {
		out = append(out, w.linkLocked(i))
	}
	return out
}

func (w *fakeWire) link(ifIndex int) link {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.linkLocked(ifIndex)
}

func (w *fakeWire) linkLocked(ifIndex int) link {
	addrs, listed := w.addrsByLink[ifIndex]
	l := link{index: ifIndex, name: "fake" + strconv.Itoa(ifIndex), addrs: addrs, listed: listed, has4: true, has6: true}
	return l
}

// addrs is the advertised addresses of an interface.
func (w *fakeWire) addrs(ifIndex int) []net.IP { return w.link(ifIndex).addrs }

func (w *fakeWire) record(pkt []byte, ifIndex int, dst *net.UDPAddr, fail func(*dns.Msg) reach) (reach, error) {
	m := new(dns.Msg)
	if err := m.Unpack(pkt); err != nil {
		panic(err)
	}
	if len(pkt) > maxMessageSize {
		panic("packet exceeds the mDNS message size")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	rc := reachAll
	if fail != nil {
		rc = fail(m)
	}
	var err error
	switch rc {
	case reachNone:
	case reachFailed:
		err = errSendFailed
	case reachSome:
		err = errSendFailed
		w.sent = append(w.sent, sentPacket{msg: m, ifIndex: ifIndex, dst: dst})
	default:
		w.sent = append(w.sent, sentPacket{msg: m, ifIndex: ifIndex, dst: dst})
	}
	return rc, err
}

// errSendFailed is the fake wire's refused send.
var errSendFailed = errors.New("send ENETUNREACH")

func (w *fakeWire) multicast(pkt []byte, l link) (reach, error) {
	w.mu.Lock()
	fail := w.fail
	w.mu.Unlock()
	return w.record(pkt, l.index, nil, fail)
}

func (w *fakeWire) unicast(pkt []byte, ifIndex int, dst *net.UDPAddr) (reach, error) {
	w.mu.Lock()
	fail := w.failUnicast
	w.mu.Unlock()
	return w.record(pkt, ifIndex, dst, fail)
}

// setFail installs the multicast failure rule (nil: every send leaves).
func (w *fakeWire) setFail(f func(m *dns.Msg) reach) {
	w.mu.Lock()
	w.fail = f
	w.mu.Unlock()
}

// setAddrs replaces the advertised addresses of an interface (none
// removes it from the links).
func (w *fakeWire) setAddrs(ifIndex int, addrs ...string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(addrs) == 0 {
		delete(w.addrsByLink, ifIndex)
		return
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, net.ParseIP(a))
	}
	w.addrsByLink[ifIndex] = ips
}

// take returns the packets sent since the last take, once the turn's
// announcement went out.
func (w *fakeWire) take() []sentPacket {
	if w.turn != nil {
		w.turn()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.sent
	w.sent = nil
	return out
}

// fakeClock is the test clock, a [clocktest.Fake] (matter.js MockTime).
type fakeClock = clocktest.Fake

// testEpoch is more than two minutes past the zero time, as
// MdnsServerTest's beforeEach advances MockTime past the 120 s TTL (:69).
var testEpoch = time.Unix(1_000_000, 0)

// newWiredResponder is a Responder on a fake wire and the fake clock, with
// no schedule jitter.
func newWiredResponder(addrsByLink map[int][]string) (*Responder, *fakeWire, *clocktest.Fake) {
	r := NewResponder()
	w := newFakeWire(addrsByLink)
	clk := clocktest.New(testEpoch)
	r.SetClock(clk)
	r.wire = w
	r.random32 = func() uint32 { return 0 }
	w.turn = func() { clk.Advance(0) }
	return r, w, clk
}

// setRecords installs a record-generator fixture under key, as matter.js
// MdnsServer.setRecordsGenerator does (MdnsServer.ts:277-280).
func setRecords(r *Responder, key string, rrs ...dns.RR) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.services[key]; !ok {
		r.order = append(r.order, key)
	}
	r.services[key] = &entry{key: key, gen: func() []dns.RR { return rrs }}
	r.resetLocked()
}

// deliver hands the responder a packet as received on ifIndex from src.
func deliver(t *testing.T, r *Responder, m *dns.Msg, ifIndex int, src *net.UDPAddr) {
	t.Helper()
	pkt, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	r.receive(pkt, ifIndex, src)
}

func question(name string, qtype uint16, qu bool) dns.Question {
	q := dns.Question{Name: dns.Fqdn(name), Qtype: qtype, Qclass: dns.ClassINET}
	if qu {
		q.Qclass |= classCacheFlush
	}
	return q
}

func queryMsg(qs []dns.Question, known ...dns.RR) *dns.Msg {
	m := new(dns.Msg)
	m.Question = qs
	m.Answer = known
	return m
}

// Record builders after matter.js's (packages/general/src/codec/
// DnsCodec.ts:28-95): default TTL 120 s, no cache-flush bit.

func ptrRR(name, target string) dns.RR {
	return &dns.PTR{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: recordTTL}, Ptr: dns.Fqdn(target)}
}

func srvRec(name string, port uint16, target string) dns.RR {
	return &dns.SRV{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: recordTTL}, Port: port, Target: dns.Fqdn(target)}
}

func txtRR(name string, entries ...string) dns.RR {
	return &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: recordTTL}, Txt: entries}
}

func addrRR(name, ip string) dns.RR {
	parsed := net.ParseIP(ip)
	if v4 := parsed.To4(); v4 != nil {
		return &dns.A{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: recordTTL}, A: v4}
	}
	return &dns.AAAA{Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: recordTTL}, AAAA: parsed}
}

// flushed is matter.js's `{ ...record, flushCache: true }`.
func flushed(rr dns.RR) dns.RR {
	c := dns.Copy(rr)
	c.Header().Class |= classCacheFlush
	return c
}

// ttl returns a copy of rr with the TTL.
func ttl(rr dns.RR, seconds uint32) dns.RR {
	c := dns.Copy(rr)
	c.Header().Ttl = seconds
	return c
}

// rrStrings renders records for comparison: the record with its TTL and
// whether the cache-flush bit is set.
func rrStrings(rrs []dns.RR) []string {
	out := make([]string, 0, len(rrs))
	for _, rr := range rrs {
		c := dns.Copy(rr)
		flush := c.Header().Class&classCacheFlush != 0
		c.Header().Class &^= classCacheFlush
		s := c.String()
		if flush {
			s += " +flush"
		}
		out = append(out, s)
	}
	return out
}

// sortedRRStrings is rrStrings in a canonical order — matter.js MdnsTest
// compares announcements as sets (MdnsTest.ts:1001-1024 expectMessage).
func sortedRRStrings(rrs []dns.RR) []string {
	out := rrStrings(rrs)
	slices.Sort(out)
	return out
}

func expectRRs(t *testing.T, what string, got []dns.RR, want ...dns.RR) {
	t.Helper()
	if g, w := rrStrings(got), rrStrings(want); !slices.Equal(g, w) {
		t.Errorf("%s:\n got  %s\n want %s", what, strings.Join(g, "\n      "), strings.Join(w, "\n      "))
	}
}

// expectResponse checks one sent packet: a response with no question, the
// transaction id, the interface, the unicast target (nil for multicast),
// and the answer and additional sections in order.
func expectResponse(t *testing.T, p sentPacket, ifIndex int, dst *net.UDPAddr, answers, additional []dns.RR) {
	t.Helper()
	if !p.msg.Response || !p.msg.Authoritative || len(p.msg.Question) != 0 || len(p.msg.Ns) != 0 || p.msg.Truncated {
		t.Errorf("response header: response=%v aa=%v questions=%d authorities=%d tc=%v",
			p.msg.Response, p.msg.Authoritative, len(p.msg.Question), len(p.msg.Ns), p.msg.Truncated)
	}
	if p.ifIndex != ifIndex {
		t.Errorf("sent on interface %d, want %d", p.ifIndex, ifIndex)
	}
	switch {
	case dst == nil && p.dst != nil:
		t.Errorf("unicast to %v, want multicast", p.dst)
	case dst != nil && (p.dst == nil || !p.dst.IP.Equal(dst.IP) || p.dst.Port != dst.Port):
		t.Errorf("sent to %v, want unicast to %v", p.dst, dst)
	}
	expectRRs(t, "answers", p.msg.Answer, answers...)
	expectRRs(t, "additionals", p.msg.Extra, additional...)
}

func expectSent(t *testing.T, got []sentPacket, n int) {
	t.Helper()
	if len(got) != n {
		var b strings.Builder
		for _, p := range got {
			b.WriteString("\n  ")
			b.WriteString(strings.Join(rrStrings(append(slices.Clone(p.msg.Answer), p.msg.Extra...)), " | "))
		}
		t.Fatalf("%d packets sent, want %d:%s", len(got), n, b.String())
	}
}

// goodbyeRecords returns copies of rrs with TTL 0, the cache-flush bits
// kept — the goodbye of a record set whose unique records carry the bit
// (MdnsTest.ts:359 at db021da0: "unique records keep the cache-flush
// bit").
func goodbyeRecords(rrs []dns.RR) []dns.RR {
	out := make([]dns.RR, len(rrs))
	for i, rr := range rrs {
		c := dns.Copy(rr)
		c.Header().Ttl = 0
		out[i] = c
	}
	return out
}

// goodbyeOf is the goodbye the responder sends for rrs once no published
// record is left of their sets: TTL 0, the cache-flush bit on the unique
// types only (InterfaceResponder.ts:503-507 at db021da0).
func goodbyeOf(rrs []dns.RR) []dns.RR {
	out := goodbyeRecords(rrs)
	for _, rr := range out {
		rr.Header().Class &^= classCacheFlush
		if isUniqueType(rr.Header().Rrtype) {
			rr.Header().Class |= classCacheFlush
		}
	}
	return out
}
