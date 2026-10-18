// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// A port of matter.js packages/protocol/test/mdns/MdnsServerTest.ts at
// 862ad75f: the host-name answerer's cache-flush, suppression, duplicate and
// unicast decisions, and the MAC-derived host-name choice.

import (
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// The host-name answerer, case by case against matter.js
// packages/protocol/test/mdns/MdnsServerTest.ts (at the schema pin): the
// same queries, the same answers, the same unicast / multicast decisions.

const testHost = "AABBCCDDEEFF0000.local."

// fakeClock is the test clock, a [clocktest.Fake] (matter.js MockTime).
type fakeClock = clocktest.Fake

// newTestAnswerer answers for testHost with addrs on interface 1 and none
// elsewhere — interface 2 stands for an interface the policy excludes.
func newTestAnswerer(addrs ...string) (ans *hostAnswerer, clk *fakeClock) {
	h := newHostAnswerer()
	// More than two minutes past the zero time, as MdnsServerTest's
	// beforeEach advances MockTime past the 120 s TTL.
	clock := clocktest.New(time.Unix(1_000_000, 0))
	h.clock.Store(clock)
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
	clock.Advance(29 * time.Second) // within a quarter of 120 s
	r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1)
	if !r.unicast || !r.dst.IP.Equal(querier.IP) || r.dst.Port != querier.Port {
		t.Fatalf("QU question after a recent multicast: unicast=%v dst=%v, want unicast to %v", r.unicast, r.dst, querier)
	}
	expectRRs(t, "unicast answers", r.msg.Answer, aRecord("1.2.3.4", 120, false))

	h, clock = newTestAnswerer("1.2.3.4")
	mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
	clock.Advance(20 * time.Second)
	if r = mustAnswer(t, h, hostQuery(testHost, dns.TypeA, true), 1); !r.unicast {
		t.Error("QU 20 s after a multicast: want unicast")
	}
	clock.Advance(11 * time.Second) // 31 s since the multicast: more than a quarter
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
	clock.Advance(500 * time.Millisecond)
	if _, ok := h.answer(hostQuery(testHost, dns.TypeA, false), 1, querier); ok {
		t.Error("answered the same question again within the window")
	}
	clock.Advance(questionSuppressionWindow + 2*time.Millisecond)
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

// The MdnsServerTest.ts cases below run against the host-name answerer, the
// go-fabric code that answers queries itself (grandcat/zeroconf answers the
// service records). The matter.js record set is a PTR, SRV, TXT and the
// host's A; here it is the host's A/AAAA set, so the cases test the same
// rule on the records the answerer holds. Where the matter.js case needs
// additionals it uses a mixed query: matter.js adds additionals unless
// every question is A/AAAA (MdnsServer.ts:130-135), and a host ANY answers
// the whole set.

// mixedQuery asks qtype of the host plus an unrelated SRV, so the rest of
// the host's set comes back as additionals.
func mixedQuery(qtype uint16, known ...dns.RR) *dns.Msg {
	q := hostQuery(testHost, qtype, false, known...)
	q.Question = append(q.Question, dns.Question{Name: "other.local.", Qtype: dns.TypeSRV, Qclass: dns.ClassINET})
	return q
}

func noAnswer(t *testing.T, h *hostAnswerer, q *dns.Msg) {
	t.Helper()
	if r, ok := h.answer(q, 1, querier); ok {
		t.Errorf("answered %v with %v, want nothing", q.Question, rrStrings(r.msg.Answer))
	}
}

func TestMdnsServerRespondsToStandardQueries(t *testing.T) {
	t.Parallel()
	t.Run("server responds to an A query with A record only", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
		if r.unicast || r.ifIndex != 1 || r.msg.Id != 0 || len(r.msg.Question) != 0 {
			t.Errorf("reply unicast=%v ifIndex=%d id=%d questions=%d", r.unicast, r.ifIndex, r.msg.Id, len(r.msg.Question))
		}
		expectRRs(t, "answers", r.msg.Answer, aRecord("1.2.3.4", 120, true))
		expectRRs(t, "additionals", r.msg.Extra)
	})
}

func TestMdnsServerCacheFlushBit(t *testing.T) {
	t.Parallel()
	t.Run("suppresses a record the querier knows even though we flag it cache-flush", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "fe80::1")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false, aRecord("1.2.3.4", 120, false)), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("fe80::1", 120, true))
	})
	t.Run("drops the bit on an additional record whose set a known answer pruned", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "5.6.7.8", "fe80::1")
		r := mustAnswer(t, h, mixedQuery(dns.TypeAAAA, aRecord("1.2.3.4", 120, false)), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("fe80::1", 120, true))
		expectRRs(t, "additionals", r.msg.Extra, aRecord("5.6.7.8", 120, false))
	})
	t.Run("omits the bit from a unicast response", func(t *testing.T) {
		t.Parallel()
		h, clock := newTestAnswerer("1.2.3.4", "fe80::1")
		mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
		clock.Advance(time.Second)
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, true), 1)
		if !r.unicast || len(r.msg.Answer) == 0 {
			t.Fatalf("unicast=%v answers=%d, want a unicast answer", r.unicast, len(r.msg.Answer))
		}
		for _, rr := range r.msg.Answer {
			if rr.Header().Class&classCacheFlush != 0 {
				t.Errorf("unicast answer %v carries the cache-flush bit", rr)
			}
		}
	})
	t.Run("drops the bit when a known answer leaves the record set incomplete", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "5.6.7.8")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false, aRecord("1.2.3.4", 120, false)), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("5.6.7.8", 120, false))
	})
	t.Run("keeps the bit when the whole record set is sent", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "5.6.7.8")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("1.2.3.4", 120, true), aRecord("5.6.7.8", 120, true))
	})
}

func TestMdnsServerAnswerSuppression(t *testing.T) {
	t.Parallel()
	t.Run("suppress some answers in an ANY query", func(t *testing.T) {
		t.Parallel()
		// Two of three answers known, as the TXT and PTR of :521.
		h, _ := newTestAnswerer("fe80::1", "fd00::1", "1.2.3.4")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false, aRecord("fe80::1", 120, false), aRecord("fd00::1", 120, false)), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("1.2.3.4", 120, true))
	})
	t.Run("suppress full answer in an A query", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4")
		noAnswer(t, h, hostQuery(testHost, dns.TypeA, false, aRecord("1.2.3.4", 120, false)))
	})
}

func TestMdnsServerHandleDuplicateMessages(t *testing.T) {
	t.Parallel()
	t.Run("server suppress response to same ANY query when 0ms delay", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
		expectRRs(t, "first", r.msg.Answer, aRecord("1.2.3.4", 120, true))
		noAnswer(t, h, hostQuery(testHost, dns.TypeANY, false))
	})
	t.Run("server do not suppress response from same ANY query when > 1s delay", func(t *testing.T) {
		t.Parallel()
		h, clock := newTestAnswerer("1.2.3.4")
		mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
		clock.Advance(questionSuppressionWindow + 2*time.Millisecond)
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
		expectRRs(t, "second", r.msg.Answer, aRecord("1.2.3.4", 120, true))
	})
	t.Run("server also suppress response to different query with same records when 0ms delay", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4")
		mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1)
		noAnswer(t, h, hostQuery(testHost, dns.TypeA, false))
	})
	t.Run("server do not suppress additional records to different query when 0ms delay", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "fe80::1")
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeA, false), 1)
		expectRRs(t, "first answers", r.msg.Answer, aRecord("1.2.3.4", 120, true))
		expectRRs(t, "first additionals", r.msg.Extra)
		r = mustAnswer(t, h, mixedQuery(dns.TypeAAAA), 1)
		expectRRs(t, "second answers", r.msg.Answer, aRecord("fe80::1", 120, true))
		expectRRs(t, "second additionals", r.msg.Extra, aRecord("1.2.3.4", 120, true))
	})
}

func TestMdnsServerRespondsAsUnicast(t *testing.T) {
	t.Parallel()
	anyQU := func() *dns.Msg { return hostQuery(testHost, dns.TypeANY, true) }
	expect := func(t *testing.T, r hostReply, unicast bool) {
		t.Helper()
		if r.unicast != unicast {
			t.Errorf("unicast=%v, want %v", r.unicast, unicast)
		}
		if r.unicast && (!r.dst.IP.Equal(querier.IP) || r.dst.Port != querier.Port) {
			t.Errorf("unicast to %v, want %v", r.dst, querier)
		}
		if len(r.msg.Answer) != 1 || r.msg.Answer[0].Header().Rrtype != dns.TypeA {
			t.Errorf("answers %v", rrStrings(r.msg.Answer))
		}
	}
	t.Run("server responds to an ANY query as multicast when unicast is requested after a previous unicast query with less than 1/4 of TTL remaining", func(t *testing.T) {
		t.Parallel()
		h, clock := newTestAnswerer("1.2.3.4")
		expect(t, mustAnswer(t, h, anyQU(), 1), false)
		clock.Advance(29 * time.Second)
		expect(t, mustAnswer(t, h, anyQU(), 1), true)
	})
	t.Run("server responds to an ANY query as multicast even if requested as unicast and after a multicast query with more than 1/4 of ttl but never sent as multicast", func(t *testing.T) {
		t.Parallel()
		h, clock := newTestAnswerer("1.2.3.4")
		expect(t, mustAnswer(t, h, anyQU(), 1), false)
		clock.Advance(31 * time.Second)
		expect(t, mustAnswer(t, h, anyQU(), 1), false)
	})
	t.Run("server responds to an ANY query as multicast even if requested as unicast and after a multicast query with more than 1/4 of ttl when earlier sent as multicast", func(t *testing.T) {
		t.Parallel()
		h, clock := newTestAnswerer("1.2.3.4")
		expect(t, mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false), 1), false)
		clock.Advance(20 * time.Second)
		expect(t, mustAnswer(t, h, anyQU(), 1), true)
		clock.Advance(11 * time.Second)
		expect(t, mustAnswer(t, h, anyQU(), 1), false)
	})
}

func TestMdnsServerUnknownNameQueryRejection(t *testing.T) {
	t.Parallel()
	t.Run("suppresses known-answer records supplied with a different case (RFC 6762 §16)", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAnswerer("1.2.3.4", "fe80::1")
		known := aRecord("1.2.3.4", 120, false)
		known.Header().Name = strings.ToUpper(testHost)
		r := mustAnswer(t, h, hostQuery(testHost, dns.TypeANY, false, known), 1)
		expectRRs(t, "answers", r.msg.Answer, aRecord("fe80::1", 120, true))
	})
}

// The Host name group (MdnsServerTest.ts:1670-1836) runs against the
// host-name choice of [defaultHostName]: one name, the SRV target of every
// service on every interface. Every matter.js MockNetwork interface is up
// and multicast-capable; "lo" is excluded by its zero MAC, as there.
func TestMdnsServerHostName(t *testing.T) {
	t.Parallel()
	up := net.FlagUp | net.FlagMulticast
	iface := func(mac string, addrs int) hostNameIface {
		hw, err := net.ParseMAC(mac)
		if err != nil {
			t.Fatal(err)
		}
		return hostNameIface{flags: up, mac: hw, addrs: addrs}
	}
	// beforeEach, :1749-1754.
	lo, eth0, docker0 := iface("00:00:00:00:00:00", 1), iface("aa:bb:cc:dd:ee:01", 1), iface("aa:bb:cc:dd:ee:02", 1)
	standard := func() []hostNameIface { return []hostNameIface{lo, eth0, docker0} }

	t.Run("names one host on every interface", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		anpi0 := iface("aa:bb:cc:dd:ee:00", 0)
		if got := c.choose(append([]hostNameIface{anpi0}, standard()...), true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
	t.Run("keeps the name while its MAC is present, even when another interface lists first", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		c.choose(standard(), true)
		if got := c.choose([]hostNameIface{docker0, eth0}, true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
	t.Run("follows a rotated MAC", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		c.choose(standard(), true)
		if got := c.choose([]hostNameIface{lo, iface("aa:bb:cc:dd:ee:03", 1), docker0}, true); got != "AABBCCDDEE030000" {
			t.Errorf("host = %q, want AABBCCDDEE030000", got)
		}
	})
	t.Run("skips an interface whose details cannot be read", func(t *testing.T) {
		t.Parallel()
		var c hostNameChooser
		gone := iface("aa:bb:cc:dd:ee:09", 1)
		gone.err = errors.New("interface gone is gone")
		if got := c.choose(append([]hostNameIface{gone}, standard()...), true); got != "AABBCCDDEE010000" {
			t.Errorf("host = %q, want AABBCCDDEE010000", got)
		}
	})
}
