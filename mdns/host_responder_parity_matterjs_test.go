// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The host-name answerer, case by case against matter.js
// packages/protocol/test/mdns/MdnsServerTest.ts (at the schema pin): the
// same queries, the same answers, the same unicast / multicast decisions.

const testHost = "AABBCCDDEEFF0000.local."

// fakeClock is a settable clock (matter.js MockTime).
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestAnswerer answers for testHost with addrs on interface 1 and none
// elsewhere — interface 2 stands for an interface the policy excludes.
func newTestAnswerer(addrs ...string) (*hostAnswerer, *fakeClock) {
	h := newHostAnswerer()
	// More than two minutes past the zero time, as MdnsServerTest's
	// beforeEach advances MockTime past the 120 s TTL.
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	h.now = clock.now
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, net.ParseIP(a))
	}
	h.set(testHost, func(ifIndex int) []net.IP {
		if ifIndex == 1 {
			return ips
		}
		return nil
	})
	return h, clock
}

var querier = &net.UDPAddr{IP: net.ParseIP("192.168.200.2"), Port: 5353}

func hostQuery(name string, qtype uint16, qu bool, known ...dns.RR) *dns.Msg {
	m := new(dns.Msg)
	q := dns.Question{Name: name, Qtype: qtype, Qclass: dns.ClassINET}
	if qu {
		q.Qclass |= classCacheFlush
	}
	m.Question = []dns.Question{q}
	m.Answer = known
	return m
}

func aRecord(ip string, ttl uint32, flush bool) dns.RR {
	class := uint16(dns.ClassINET)
	if flush {
		class |= classCacheFlush
	}
	parsed := net.ParseIP(ip)
	if v4 := parsed.To4(); v4 != nil {
		return &dns.A{Hdr: dns.RR_Header{Name: testHost, Rrtype: dns.TypeA, Class: class, Ttl: ttl}, A: v4}
	}
	return &dns.AAAA{Hdr: dns.RR_Header{Name: testHost, Rrtype: dns.TypeAAAA, Class: class, Ttl: ttl}, AAAA: parsed}
}

func rrStrings(rrs []dns.RR) []string {
	out := make([]string, 0, len(rrs))
	for _, rr := range rrs {
		out = append(out, rr.String()+" class="+itoaClass(rr.Header().Class))
	}
	return out
}

func itoaClass(c uint16) string {
	if c&classCacheFlush != 0 {
		return "IN+flush"
	}
	return "IN"
}

func mustAnswer(t *testing.T, h *hostAnswerer, q *dns.Msg, ifIndex int) hostReply {
	t.Helper()
	r, ok := h.answer(q, ifIndex, querier)
	if !ok {
		t.Fatalf("no answer to %v", q.Question)
	}
	return r
}

func expectRRs(t *testing.T, what string, got []dns.RR, want ...dns.RR) {
	t.Helper()
	if !slices.Equal(rrStrings(got), rrStrings(want)) {
		t.Errorf("%s:\n got  %v\n want %v", what, rrStrings(got), rrStrings(want))
	}
}

// MdnsServerTest.ts "server responds to an A query with A record only":
// the A records alone, no additionals, multicast on the query's interface.
func TestHostAnswer_AQueryAnswersARecordsOnly(t *testing.T) {
	h, _ := newTestAnswerer("1.2.3.4", "2001:db8::1")
	r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
	if r.unicast || r.ifIndex != 1 {
		t.Errorf("reply unicast=%v ifIndex=%d, want multicast on 1", r.unicast, r.ifIndex)
	}
	expectRRs(t, "answers", r.msg.Answer, aRecord("1.2.3.4", 120, true))
	expectRRs(t, "additionals", r.msg.Extra)
	if len(r.msg.Question) != 0 || !r.msg.Response || !r.msg.Authoritative {
		t.Errorf("response framing: questions=%d response=%v aa=%v", len(r.msg.Question), r.msg.Response, r.msg.Authoritative)
	}
}

// The case TC-SC-4.1 / TC-SC-4.3 fail on: a bare-host-name AAAA question.
// IPv6 records come in matter.js selection-preference order, each flagged
// cache-flush with the 120 s TTL (MdnsAdvertisement.ts #recordsFor).
func TestHostAnswer_AAAAQueryInSelectionPreferenceOrder(t *testing.T) {
	h, _ := newTestAnswerer("2001:db8::1", "1.2.3.4", "fe80::1", "fd00::1")
	r := mustAnswer(t, h, hostQuery(strings.ToLower(testHost), dns.TypeAAAA, false), 1)
	expectRRs(t, "answers", r.msg.Answer,
		aRecord("fe80::1", 120, true), aRecord("fd00::1", 120, true), aRecord("2001:db8::1", 120, true))
	expectRRs(t, "additionals", r.msg.Extra)
}

// An ANY question for the host answers every address record.
func TestHostAnswer_ANYQueryAnswersAll(t *testing.T) {
	h, _ := newTestAnswerer("1.2.3.4", "2001:db8::1")
	r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
	expectRRs(t, "answers", r.msg.Answer, aRecord("2001:db8::1", 120, true), aRecord("1.2.3.4", 120, true))
}

// A mixed query (host AAAA plus a question the host set does not answer)
// carries the rest of the set as additionals (matter.js: additionals unless
// every question is A/AAAA).
func TestHostAnswer_MixedQueryAddsTheRestAsAdditionals(t *testing.T) {
	h, _ := newTestAnswerer("1.2.3.4", "2001:db8::1")
	q := hostQuery(testHost, dns.TypeAAAA, false)
	q.Question = append(q.Question, dns.Question{Name: "other.local.", Qtype: dns.TypeSRV, Qclass: dns.ClassINET})
	r := mustAnswer(t, h, q, 1)
	expectRRs(t, "answers", r.msg.Answer, aRecord("2001:db8::1", 120, true))
	expectRRs(t, "additionals", r.msg.Extra, aRecord("1.2.3.4", 120, true))
}

// MdnsServerTest.ts "does not respond to queries for names we do not
// serve", and the per-interface record set: an interface without records
// gets no answer, nor does a stopped answerer.
func TestHostAnswer_UnknownNameOtherInterfaceOrStopped(t *testing.T) {
	h, _ := newTestAnswerer("1.2.3.4")
	if _, ok := h.answer(hostQuery("someone-else.local.", dns.TypeA, false), 1, querier); ok {
		t.Error("answered a name it does not own")
	}
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 2, querier); ok {
		t.Error("answered on an interface whose addresses are not advertised")
	}
	if _, ok := h.answer(hostQuery(testHost, dns.TypeTXT, false), 1, querier); ok {
		t.Error("answered a record type the host set does not hold")
	}
	resp := hostQuery(testHost, dns.TypeA, false)
	resp.Response = true
	if _, ok := h.answer(resp, 1, querier); ok {
		t.Error("answered a response")
	}
	h.set("", nil)
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 1, querier); ok {
		t.Error("answered after the host was cleared")
	}
}

// MdnsServerTest.ts "suppress full answer in an A query" and "drops the
// bit when a known answer leaves the record set incomplete" / "keeps the
// bit when the whole record set is sent".
func TestHostAnswer_KnownAnswerSuppressionAndCacheFlush(t *testing.T) {
	h, _ := newTestAnswerer("1.2.3.4", "5.6.7.8")
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false, aRecord("1.2.3.4", 120, false), aRecord("5.6.7.8", 120, false)), 1, querier); ok {
		t.Error("answered although the querier knows every record")
	}
	h, _ = newTestAnswerer("1.2.3.4", "5.6.7.8")
	r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false, aRecord("1.2.3.4", 120, true)), 1)
	expectRRs(t, "incomplete set", r.msg.Answer, aRecord("5.6.7.8", 120, false))
	h, _ = newTestAnswerer("1.2.3.4", "5.6.7.8")
	r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
	expectRRs(t, "whole set", r.msg.Answer, aRecord("1.2.3.4", 120, true), aRecord("5.6.7.8", 120, true))
}

// MdnsServerTest.ts "suppresses only while more than half the known
// answer's lifetime remains": 61 s of 120 s suppresses, 60 s does not.
func TestHostAnswer_KnownAnswerNeedsMoreThanHalfTTL(t *testing.T) {
	for _, tc := range []struct {
		remaining  uint32
		suppressed bool
	}{{61, true}, {60, false}, {59, false}} {
		h, _ := newTestAnswerer("1.2.3.4")
		_, ok := h.answer(hostQuery(testHost, dns.TypeA, false, aRecord("1.2.3.4", tc.remaining, false)), 1, querier)
		if ok == tc.suppressed {
			t.Errorf("known answer with %d s left: answered=%v, want %v", tc.remaining, ok, !tc.suppressed)
		}
	}
}

// MdnsServerTest.ts "Server responds as unicast (if allowed)": a QU
// question is answered by multicast until the records went out by
// multicast within a quarter of their TTL; a unicast reply carries no
// cache-flush bit ("omits the bit from a unicast response").
func TestHostAnswer_UnicastOnlyAfterRecentMulticast(t *testing.T) {
	h, clock := newTestAnswerer("1.2.3.4")
	r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1)
	if r.unicast {
		t.Fatal("QU question for never-multicast records was answered by unicast")
	}
	clock.advance(29 * time.Second) // within a quarter of 120 s
	r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1)
	if !r.unicast || !r.dst.IP.Equal(querier.IP) || r.dst.Port != querier.Port {
		t.Fatalf("QU question after a recent multicast: unicast=%v dst=%v, want unicast to %v", r.unicast, r.dst, querier)
	}
	expectRRs(t, "unicast answers", r.msg.Answer, aRecord("1.2.3.4", 120, false))

	h, clock = newTestAnswerer("1.2.3.4")
	mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
	clock.advance(20 * time.Second)
	if r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1); !r.unicast {
		t.Error("QU 20 s after a multicast: want unicast")
	}
	clock.advance(11 * time.Second) // 31 s since the multicast: more than a quarter
	if r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1); r.unicast {
		t.Error("QU 31 s after the last multicast: want multicast")
	}
}

// MdnsServerTest.ts "Handle duplicate messages" and "Duplicate question
// suppression (RFC 6762 §7.3)": the same question within 999 ms is not
// answered twice by multicast; after the window it is.
func TestHostAnswer_DuplicateQuestionSuppression(t *testing.T) {
	h, clock := newTestAnswerer("1.2.3.4")
	mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 1, querier); ok {
		t.Error("answered the same question again at once")
	}
	clock.advance(500 * time.Millisecond)
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 1, querier); ok {
		t.Error("answered the same question again within the window")
	}
	clock.advance(questionSuppressionWindow + 2*time.Millisecond)
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 1, querier); !ok {
		t.Error("did not answer after the suppression window")
	}
}

// The python-zeroconf resolver the CHIP cases use sends a QU and then a QM
// copy of the same question back to back; the link gets one multicast
// answer.
func TestHostAnswer_QUThenQMGetsOneMulticast(t *testing.T) {
	h, _ := newTestAnswerer("2001:db8::1")
	r := mustAnswer(t, h, hostQuery(testHost, dns.TypeAAAA, true), 1)
	if r.unicast {
		t.Fatal("first QU answered by unicast")
	}
	if _, ok := h.answer(hostQuery(testHost, dns.TypeAAAA, false), 1, querier); ok {
		t.Error("the QM copy was answered again within the rate limit")
	}
}

type sent struct {
	pkt     []byte
	ifIndex int
	dst     *net.UDPAddr
}

type fakeHostSender struct {
	mu         sync.Mutex
	multicasts []sent
	unicasts   []sent
}

func (f *fakeHostSender) multicast(pkt []byte, ifIndex int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.multicasts = append(f.multicasts, sent{pkt: pkt, ifIndex: ifIndex})
}

func (f *fakeHostSender) unicast(pkt []byte, ifIndex int, dst *net.UDPAddr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unicasts = append(f.unicasts, sent{pkt: pkt, ifIndex: ifIndex, dst: dst})
}

// The responder's receive path hands host questions to the answerer and its
// replies to the transport, on the interface the query arrived on; subtype
// questions keep their own path.
func TestSubtypeResponder_AnswersHostThroughTransport(t *testing.T) {
	r := newTestResponder()
	fake := &fakeHostSender{}
	r.host = newHostAnswerer()
	r.hostSend = fake
	r.SetHost(strings.TrimSuffix(testHost, "."), func(ifIndex int) []net.IP {
		if ifIndex == 3 {
			return []net.IP{net.ParseIP("2001:db8::7")}
		}
		return nil
	})
	q, err := hostQuery(testHost, dns.TypeAAAA, false).Pack()
	if err != nil {
		t.Fatal(err)
	}
	r.answerHost(q, 4, querier) // not an advertised interface
	r.answerHost(q, 3, querier)
	if len(fake.multicasts) != 1 || fake.multicasts[0].ifIndex != 3 || len(fake.unicasts) != 0 {
		t.Fatalf("sent %d multicasts %v and %d unicasts, want one multicast on interface 3", len(fake.multicasts), fake.multicasts, len(fake.unicasts))
	}
	resp := new(dns.Msg)
	if err := resp.Unpack(fake.multicasts[0].pkt); err != nil {
		t.Fatal(err)
	}
	expectRRs(t, "wire answer", resp.Answer, aRecord("2001:db8::7", 120, true))

	r.SetHost("", nil)
	r.answerHost(q, 3, querier)
	if len(fake.multicasts) != 1 {
		t.Error("answered after SetHost(\"\")")
	}
}

// Zeroconf hands the responder the MAC-derived SRV target it publishes and
// keeps the OS host name (and the HostName override) to the OS responder.
func TestZeroconf_HandsHostNameToResponder(t *testing.T) {
	z := NewZeroconf()
	r := newTestResponder()
	r.host = newHostAnswerer()
	z.responder = r
	z.setResponderHostLocked("AABBCCDDEEFF0000", "local.")
	if got := r.host.hostName(); got != testHost {
		t.Errorf("host name %q, want %q", got, testHost)
	}
	if h := osHostName(); h != "" {
		z.setResponderHostLocked(h, "local.")
		if got := r.host.hostName(); got != "" {
			t.Errorf("the OS host name %q was taken over (%q)", h, got)
		}
	}
	z.setResponderHostLocked("AABBCCDDEEFF0000", "local.")
	z.HostName = "override"
	z.setResponderHostLocked("override", "local.")
	if got := r.host.hostName(); got != "" {
		t.Errorf("an overridden host name was answered for: %q", got)
	}
}

// hostAddrsFor applies the advertise policy to the one interface: loopback
// yields nothing, an unknown index nothing.
func TestHostAddrsFor_Policy(t *testing.T) {
	f := hostAddrsFor(nil)
	if got := f(1 << 30); got != nil {
		t.Errorf("unknown interface: %v", got)
	}
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagLoopback != 0 {
			if got := f(ifi.Index); len(got) != 0 {
				t.Errorf("loopback %s yields %v", ifi.Name, got)
			}
		}
	}
}
