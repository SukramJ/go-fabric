// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"

	"github.com/miekg/dns"
)

// QueryConn is the multicast transport a querier runs over: it sends DNS
// messages to the mDNS groups (224.0.0.251 and ff02::fb, UDP/5353) and
// delivers every DNS message received there, with the index of the
// interface it arrived on. [OperationalResolver] resolves one instance over
// it; the controller's browser (client/discovery) keeps a record cache
// on top of it. Production joins the groups on every multicast interface
// ([OpenQueryConn]); tests substitute a fake responder.
//
// It is the socket half of matter.js's MdnsSocket
// (packages/general/src/net/dns-sd/MdnsSocket.ts): receive with the source
// interface, send to the groups. The relevance pre-filter and the
// query/answer splitting of MdnsSocket are not part of it.
type QueryConn interface {
	// Send multicasts one DNS message.
	Send(msg []byte) error
	// Responses delivers received DNS messages with the index of the
	// interface they arrived on (0 when unknown). A full queue drops a
	// message rather than blocking the socket; the querier's retry asks
	// again. The channel is not closed by Close.
	Responses() <-chan QueryResponse
	// Close leaves the groups and releases the sockets. Idempotent.
	Close() error
}

// QueryResponse is one DNS message received on the mDNS groups. Despite
// the name it may be a query — the groups carry both; [ParseDNSMessage]
// reports which.
type QueryResponse struct {
	// Msg is the raw DNS message.
	Msg []byte
	// IfIndex is the index of the receiving interface, 0 when unknown.
	IfIndex int
}

// OpenQueryConn joins the mDNS groups on every multicast interface,
// IPv4 and IPv6, and returns the connection. It fails only when neither
// family could join.
func OpenQueryConn() (QueryConn, error) {
	return openMulticastQueryConn()
}

// DNSType is a DNS resource-record type.
type DNSType uint16

// The record types DNS-SD discovery uses.
const (
	DNSTypeA    DNSType = DNSType(dns.TypeA)
	DNSTypePTR  DNSType = DNSType(dns.TypePTR)
	DNSTypeTXT  DNSType = DNSType(dns.TypeTXT)
	DNSTypeAAAA DNSType = DNSType(dns.TypeAAAA)
	DNSTypeSRV  DNSType = DNSType(dns.TypeSRV)
)

// String returns the record type's mnemonic ("A", "PTR", …).
func (t DNSType) String() string {
	if s, ok := dns.TypeToString[uint16(t)]; ok {
		return s
	}
	return fmt.Sprintf("TYPE%d", uint16(t))
}

// DNSSRV is the value of an SRV record.
type DNSSRV struct {
	Target   string
	Port     uint16
	Priority uint16
	Weight   uint16
}

// DNSRecord is one resource record in the shape matter.js's DnsCodec
// decodes it (packages/general/src/codec/DnsCodec.ts): names without the
// trailing root dot, the cache-flush bit split off the class, the value
// typed by the record type. A record of another type keeps its name, type
// and TTL and carries no value.
type DNSRecord struct {
	// Name is the owner name, without the trailing dot.
	Name string
	Type DNSType
	// TTL is the time to live in seconds; 0 is a goodbye.
	TTL uint32
	// CacheFlush is the top bit of the class (RFC 6762 §10.2).
	CacheFlush bool
	// IP is the address of an A or AAAA record.
	IP netip.Addr
	// PTR is the target of a PTR record, without the trailing dot.
	PTR string
	// SRV is the value of an SRV record; its target has no trailing dot,
	// and the root target "." decodes as "".
	SRV DNSSRV
	// TXT is the entries of a TXT record, as raw bytes.
	TXT [][]byte
}

// DNSQuestion is one question of a query.
type DNSQuestion struct {
	// Name is the queried name, with or without the trailing dot.
	Name string
	Type DNSType
}

// DNSMessage is a decoded DNS message.
type DNSMessage struct {
	// Response is the QR bit.
	Response    bool
	Questions   []DNSQuestion
	Answers     []DNSRecord
	Authorities []DNSRecord
	Additional  []DNSRecord
}

// ParseDNSMessage decodes msg. A record whose value does not parse is
// dropped and the others kept; the message fails when its header, a
// question or a record's name, fixed fields or length do not parse.
// Mirrors matter.js packages/general/src/codec/DnsCodec.ts:265-289 at
// 862ad75f (decodeRecord: the reader stands after the record before its
// value is decoded, so "a record we cannot read costs only itself") and
// :216-253 (decode, undefined on any other failure).
func ParseDNSMessage(msg []byte) (DNSMessage, error) {
	m := new(dns.Msg)
	if err := m.Unpack(msg); err != nil {
		var ok bool
		if m, ok = unpackReadable(msg); !ok {
			return DNSMessage{}, fmt.Errorf("mdns: parse DNS message: %w", err)
		}
	}
	out := DNSMessage{
		Response:    m.Response,
		Answers:     recordsOf(m.Answer),
		Authorities: recordsOf(m.Ns),
		Additional:  recordsOf(m.Extra),
	}
	for _, q := range m.Question {
		out.Questions = append(out.Questions, DNSQuestion{Name: trimName(q.Name), Type: DNSType(q.Qtype)})
	}
	return out, nil
}

// PackDNSQuery encodes an mDNS query (id 0, RFC 6762 §18.1) with the given
// questions and known answers. Known answers are written without the
// cache-flush bit, which means nothing outside a response (RFC 6762
// §18.12), whatever their CacheFlush says.
func PackDNSQuery(questions []DNSQuestion, knownAnswers []DNSRecord) ([]byte, error) {
	m := new(dns.Msg)
	m.Id = 0
	m.RecursionDesired = false
	for _, q := range questions {
		m.Question = append(m.Question, dns.Question{Name: fqdnOf(q.Name), Qtype: uint16(q.Type), Qclass: dns.ClassINET})
	}
	for _, r := range knownAnswers {
		if rr := rrOf(r); rr != nil {
			m.Answer = append(m.Answer, rr)
		}
	}
	out, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("mdns: pack DNS query: %w", err)
	}
	return out, nil
}

// unpackReadable walks msg record by record when miekg/dns refuses it
// whole: the header, the questions, then each record's name, fixed
// fields and RDLENGTH bound the next record's start, and a record whose
// value does not unpack is skipped. ok is false when the walk itself
// fails (a truncated header, question, name or value).
func unpackReadable(msg []byte) (m *dns.Msg, ok bool) {
	if len(msg) < 12 {
		return nil, false
	}
	m = new(dns.Msg)
	m.Id = binary.BigEndian.Uint16(msg)
	m.Response = msg[2]&0x80 != 0
	counts := [4]int{}
	for i := range counts {
		counts[i] = int(binary.BigEndian.Uint16(msg[4+2*i:]))
	}
	off := 12
	for range counts[0] {
		name, next, err := dns.UnpackDomainName(msg, off)
		if err != nil || next+4 > len(msg) {
			return nil, false
		}
		m.Question = append(m.Question, dns.Question{Name: name, Qtype: binary.BigEndian.Uint16(msg[next:]), Qclass: binary.BigEndian.Uint16(msg[next+2:])})
		off = next + 4
	}
	sections := [3]*[]dns.RR{&m.Answer, &m.Ns, &m.Extra}
	for si, sec := range sections {
		for range counts[si+1] {
			_, next, err := dns.UnpackDomainName(msg, off)
			if err != nil || next+10 > len(msg) {
				return nil, false
			}
			end := next + 10 + int(binary.BigEndian.Uint16(msg[next+8:]))
			if end > len(msg) {
				return nil, false
			}
			if rr, after, err := dns.UnpackRR(msg, off); err == nil && rr != nil && after == end {
				*sec = append(*sec, rr)
			}
			off = end
		}
	}
	return m, true
}

// MaxMessageSize is the largest mDNS message the module writes: an IPv6
// minimum MTU of 1280 bytes less the IPv6 and UDP headers. Mirrors
// matter.js packages/general/src/codec/DnsCodec.ts:26 at 862ad75f
// (MAX_MDNS_MESSAGE_SIZE).
const MaxMessageSize = 1232

// PackDNSQueries encodes an mDNS query as the messages it takes to stay
// within [MaxMessageSize]. Questions that do not fit one message are split
// across several, each without known answers (known-answer suppression
// cannot span the split); questions that fit carry their known answers,
// and answers past the limit spill into follow-up messages without the
// questions, every message but the last flagged truncated (RFC 6762
// §7.2). Mirrors matter.js
// packages/general/src/net/dns-sd/MdnsSocket.ts:102-178 at 862ad75f (send)
// and :208-238 (splitQueries).
func PackDNSQueries(questions []DNSQuestion, knownAnswers []DNSRecord) ([][]byte, error) {
	msgOf := func(qs []dns.Question, rrs []dns.RR, truncated bool) *dns.Msg {
		m := new(dns.Msg)
		m.Question, m.Answer, m.Truncated = qs, rrs, truncated
		return m
	}
	pack := func(m *dns.Msg) ([]byte, error) {
		out, err := m.Pack()
		if err != nil {
			return nil, fmt.Errorf("mdns: pack DNS query: %w", err)
		}
		return out, nil
	}
	const header = 12
	qs := make([]dns.Question, 0, len(questions))
	for _, q := range questions {
		qs = append(qs, dns.Question{Name: fqdnOf(q.Name), Qtype: uint16(q.Type), Qclass: dns.ClassINET})
	}
	var chunks [][]dns.Question
	var cur []dns.Question
	size := header
	for _, q := range qs {
		n := msgOf([]dns.Question{q}, nil, false).Len() - header
		if size+n > MaxMessageSize && len(cur) > 0 {
			chunks, cur, size = append(chunks, cur), nil, header
		}
		cur, size = append(cur, q), size+n
	}
	if len(cur) > 0 || len(chunks) == 0 {
		chunks = append(chunks, cur)
	}
	var out [][]byte
	if len(chunks) > 1 {
		for _, c := range chunks {
			b, err := pack(msgOf(c, nil, false))
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
		return out, nil
	}
	m := msgOf(chunks[0], nil, false)
	size = m.Len()
	for _, r := range knownAnswers {
		rr := rrOf(r)
		if rr == nil {
			continue
		}
		n := msgOf(nil, []dns.RR{rr}, false).Len() - header
		if size+n > MaxMessageSize {
			m.Truncated = true
			b, err := pack(m)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
			m = msgOf(nil, nil, false)
			size = header
		}
		m.Answer = append(m.Answer, rr)
		size += n
	}
	b, err := pack(m)
	if err != nil {
		return nil, err
	}
	return append(out, b), nil
}

func recordsOf(rrs []dns.RR) []DNSRecord {
	if len(rrs) == 0 {
		return nil
	}
	out := make([]DNSRecord, 0, len(rrs))
	for _, rr := range rrs {
		h := rr.Header()
		rec := DNSRecord{
			Name:       trimName(h.Name),
			Type:       DNSType(h.Rrtype),
			TTL:        h.Ttl,
			CacheFlush: h.Class&0x8000 != 0,
		}
		switch v := rr.(type) {
		case *dns.A:
			if a, ok := netip.AddrFromSlice(v.A.To4()); ok {
				rec.IP = a
			}
		case *dns.AAAA:
			if a, ok := netip.AddrFromSlice(v.AAAA.To16()); ok {
				rec.IP = a
			}
		case *dns.PTR:
			rec.PTR = trimName(v.Ptr)
		case *dns.SRV:
			rec.SRV = DNSSRV{Target: trimName(v.Target), Port: v.Port, Priority: v.Priority, Weight: v.Weight}
		case *dns.TXT:
			rec.TXT = make([][]byte, 0, len(v.Txt))
			for _, s := range v.Txt {
				rec.TXT = append(rec.TXT, unescapeText(s))
			}
		}
		out = append(out, rec)
	}
	return out
}

func rrOf(r DNSRecord) dns.RR {
	h := dns.RR_Header{Name: fqdnOf(r.Name), Rrtype: uint16(r.Type), Class: dns.ClassINET, Ttl: r.TTL}
	switch r.Type {
	case DNSTypeA:
		if !r.IP.Is4() {
			return nil
		}
		b := r.IP.As4()
		return &dns.A{Hdr: h, A: b[:]}
	case DNSTypeAAAA:
		if !r.IP.Is6() {
			return nil
		}
		b := r.IP.As16()
		return &dns.AAAA{Hdr: h, AAAA: b[:]}
	case DNSTypePTR:
		return &dns.PTR{Hdr: h, Ptr: fqdnOf(r.PTR)}
	case DNSTypeSRV:
		return &dns.SRV{Hdr: h, Priority: r.SRV.Priority, Weight: r.SRV.Weight, Port: r.SRV.Port, Target: fqdnOf(r.SRV.Target)}
	case DNSTypeTXT:
		// An empty TXT record is a single zero-length string (RFC 6763
		// §6.1), and an entry is at most 255 bytes, a longer one cut there.
		// Mirrors matter.js packages/general/src/codec/DnsCodec.ts:477-497
		// at 862ad75f (encodeTxtRecord).
		txt := make([]string, 0, max(len(r.TXT), 1))
		for _, e := range r.TXT {
			txt = append(txt, escapeText(e[:min(len(e), 255)]))
		}
		if len(txt) == 0 {
			txt = append(txt, "")
		}
		return &dns.TXT{Hdr: h, Txt: txt}
	}
	return nil
}

// trimName drops the trailing root dot and miekg/dns's presentation
// escapes, giving the name as matter.js's DnsCodec.decodeQName joins its
// labels; the root name "." becomes "".
func trimName(name string) string {
	return string(unescapeText(strings.TrimSuffix(name, ".")))
}

// fqdnOf is the inverse of trimName for the names a query carries.
func fqdnOf(name string) string {
	return dns.Fqdn(string(escapeName(name)))
}

// unescapeText reverses miekg/dns's presentation escapes (`\"`, `\\`,
// `\DDD`), recovering the raw bytes of a TXT string or a name.
func unescapeText(s string) []byte {
	if !strings.Contains(s, `\`) {
		return []byte(s)
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			out = append(out, c)
			continue
		}
		if i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			v := int(s[i+1]-'0')*100 + int(s[i+2]-'0')*10 + int(s[i+3]-'0')
			if v <= 0xFF {
				out = append(out, byte(v)) //nolint:gosec // v <= 0xFF checked above
				i += 3
				continue
			}
		}
		out = append(out, s[i+1])
		i++
	}
	return out
}

// escapeText escapes raw TXT bytes for miekg/dns's packer.
func escapeText(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < ' ' || c > '~':
			fmt.Fprintf(&sb, `\%03d`, c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// escapeName escapes the bytes of a dotted name that miekg/dns would
// otherwise read as presentation syntax; the dots stay label separators.
func escapeName(name string) []byte {
	out := make([]byte, 0, len(name))
	for i := range len(name) {
		c := name[i]
		switch {
		case c == '\\':
			out = append(out, '\\', '\\')
		case c < ' ' || c > '~':
			out = fmt.Appendf(out, `\%03d`, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
