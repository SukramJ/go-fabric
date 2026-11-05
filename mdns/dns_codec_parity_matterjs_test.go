// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// Parity with matter.js packages/general/test/codec/DnsCodecTest.ts at
// 862ad75f. matter.js's DnsCodec is its own DNS reader and writer; the
// module reads a message with [mdns.ParseDNSMessage] and writes a query
// with [mdns.PackDNSQuery], both over miekg/dns. The message vectors are
// the file's RESULT (:45-47), ENCODED (:49-51) and RESULT_WITH_PRIVATE_TYPE
// (:92-94).

const (
	dnsCodecResult = "000084000000000400000004095f7365727669636573075f646e732d7364045f756470056c6f63616c00000c00010000" +
		"00780014075f6d6174746572045f746370056c6f63616c00095f7365727669636573075f646e732d7364045f75647005" +
		"6c6f63616c00000c000100000078002c125f4944353539414633363135343941394132045f737562075f6d6174746572" +
		"045f746370056c6f63616c00075f6d6174746572045f746370056c6f63616c00000c0001000000780036214435353941" +
		"46333631353439413941322d30303030303030303030303030303039075f6d6174746572045f746370056c6f63616c00" +
		"125f4944353539414633363135343941394132045f737562075f6d6174746572045f746370056c6f63616c00000c0001" +
		"00000078003621443535394146333631353439413941322d30303030303030303030303030303039075f6d6174746572" +
		"045f746370056c6f63616c001044434136333241303239354630303030056c6f63616c0000010001000000780004c0a8" +
		"c82e1044434136333241303239354630303030056c6f63616c00001c0001000000780010fe800000000000009580b733" +
		"6f549f4321443535394146333631353439413941322d30303030303030303030303030303039075f6d6174746572045f" +
		"746370056c6f63616c000021000100000078001e0000000015a41044434136333241303239354630303030056c6f6361" +
		"6c0021443535394146333631353439413941322d30303030303030303030303030303039075f6d6174746572045f7463" +
		"70056c6f63616c0000100001000000780015085349493d35303030075341493d33303003543d31"
	dnsCodecEncoded = "000000000003000200000001026c62075f646e732d7364045f756470056c6f63616c00000c00010f5f636f6d70616e69" +
		"6f6e2d6c696e6b045f746370c01c000c0001085f686f6d656b6974c037000c0001c027000c000100001194000a074b69" +
		"746368656ec027c042000c00010000119400272441423645433741312d333837422d353235332d413835342d39444135" +
		"3236333535363746c04200002905a00000119400120004000e0099929387b033db4275a6a31b2d"
	dnsCodecPrivate = "0000000000010000000300001033424530464344363346373945454332085f6d617474657263045f756470056c6f6361" +
		"6c0000ff0001c00c001000010000119400720a56503d343837342b38330644543d3236360d444e3d45766520456e6572" +
		"6779075349493d383030075341493d38303003543d3006443d3233393104434d3d322752493d30393030363639454434" +
		"46334338303433464344373741333945454244393646363732410550483d33360350493dc00cff2b0001000000780004" +
		"00000000c00c002100010000007800190000000015a41045454436323032303244373033313633c02b"
)

func mustDNS(t *testing.T, h string) mdns.DNSMessage {
	t.Helper()
	raw, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mdns.ParseDNSMessage(raw)
	if err != nil {
		t.Fatalf("ParseDNSMessage: %v", err)
	}
	return m
}

func names(rs []mdns.DNSRecord) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestDnsCodecDecodeParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("decodes a message", func(t *testing.T) {
		t.Parallel()
		m := mustDNS(t, dnsCodecEncoded)
		qs := make([]string, 0, len(m.Questions))
		for _, q := range m.Questions {
			qs = append(qs, q.Name)
		}
		if want := []string{"lb._dns-sd._udp.local", "_companion-link._tcp.local", "_homekit._tcp.local"}; !slices.Equal(qs, want) {
			t.Errorf("questions %v, want %v", qs, want)
		}
		if len(m.Answers) != 2 || m.Answers[0].PTR != "Kitchen._companion-link._tcp.local" ||
			m.Answers[1].PTR != "AB6EC7A1-387B-5253-A854-9DA52635567F._homekit._tcp.local" || m.Answers[0].TTL != 4500 {
			t.Errorf("answers %+v", m.Answers)
		}
	})
	t.Run("keeps the readable records of a message that holds an unreadable one", func(t *testing.T) {
		t.Parallel()
		// A TXT record whose string length overruns its RDATA, then a valid A record.
		m := mustDNS(t, "000084000000000200000000"+"0162056c6f63616c00"+"0010"+"0001"+"0000003c"+"0003"+"054142"+
			"0161056c6f63616c00"+"0001"+"0001"+"0000003c"+"0004"+"c0a80101")
		if len(m.Answers) != 1 || m.Answers[0].Name != "a.local" || m.Answers[0].IP != netip.MustParseAddr("192.168.1.1") {
			t.Errorf("answers %+v, want the A record of a.local alone", m.Answers)
		}
	})
	t.Run("keeps the readable records of a message whose record value has a bad name pointer", func(t *testing.T) {
		t.Parallel()
		// A PTR record whose RDATA is a compression pointer past the end of the message, then a valid A record.
		m := mustDNS(t, "000084000000000200000000"+"0162056c6f63616c00"+"000c"+"0001"+"0000003c"+"0002"+"c0ff"+
			"0161056c6f63616c00"+"0001"+"0001"+"0000003c"+"0004"+"c0a80101")
		if len(m.Answers) != 1 || m.Answers[0].Name != "a.local" {
			t.Errorf("answers %+v, want the A record of a.local alone", m.Answers)
		}
	})
	t.Run("decodes message with private record type", func(t *testing.T) {
		t.Parallel()
		m := mustDNS(t, dnsCodecPrivate)
		if len(m.Questions) != 1 || m.Questions[0].Name != "3BE0FCD63F79EEC2._matterc._udp.local" || m.Questions[0].Type != mdns.DNSType(255) {
			t.Fatalf("questions %+v", m.Questions)
		}
		if len(m.Authorities) != 3 {
			t.Fatalf("authorities %+v, want TXT, the private type 65323 and SRV", m.Authorities)
		}
		txt, priv, srv := m.Authorities[0], m.Authorities[1], m.Authorities[2]
		if txt.Type != mdns.DNSTypeTXT || len(txt.TXT) != 11 || string(txt.TXT[2]) != "DN=Eve Energy" || string(txt.TXT[10]) != "PI=" {
			t.Errorf("TXT %+v", txt)
		}
		if priv.Type != mdns.DNSType(65323) || priv.TTL != 120 {
			t.Errorf("private record %+v", priv)
		}
		if srv.Type != mdns.DNSTypeSRV || srv.SRV != (mdns.DNSSRV{Target: "EED620202D703163.local", Port: 5540}) {
			t.Errorf("SRV %+v", srv)
		}
	})
}

func TestDnsCodecEncodeParityMatterJS(t *testing.T) {
	t.Parallel()
	// The round trip of a decoded message through the query writer: the
	// records it carries come back as they went in.
	roundTrip := func(t *testing.T, m mdns.DNSMessage, records []mdns.DNSRecord) {
		t.Helper()
		wire, err := mdns.PackDNSQuery(m.Questions, records)
		if err != nil {
			t.Fatal(err)
		}
		back, err := mdns.ParseDNSMessage(wire)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(names(back.Answers), names(records)) || !slices.EqualFunc(back.Questions, m.Questions, func(a, b mdns.DNSQuestion) bool { return a == b }) {
			t.Errorf("round trip %+v, want questions %+v and records %v", back, m.Questions, names(records))
		}
	}
	t.Run("encodes a message and verify with decoding again", func(t *testing.T) {
		t.Parallel()
		m := mustDNS(t, dnsCodecResult)
		if want := []string{"_services._dns-sd._udp.local", "_services._dns-sd._udp.local", "_matter._tcp.local", "_ID559AF361549A9A2._sub._matter._tcp.local"}; !slices.Equal(names(m.Answers), want) {
			t.Fatalf("answers %v, want %v", names(m.Answers), want)
		}
		if len(m.Additional) != 4 || m.Additional[0].IP != netip.MustParseAddr("192.168.200.46") ||
			m.Additional[1].IP != netip.MustParseAddr("fe80::9580:b733:6f54:9f43") || m.Additional[2].SRV.Port != 5540 ||
			len(m.Additional[3].TXT) != 3 || string(m.Additional[3].TXT[0]) != "SII=5000" {
			t.Fatalf("additional %+v", m.Additional)
		}
		roundTrip(t, m, append(append([]mdns.DNSRecord(nil), m.Answers...), m.Additional...))
	})
	t.Run("encodes second message and verify with decoding again", func(t *testing.T) {
		t.Parallel()
		m := mustDNS(t, dnsCodecEncoded)
		roundTrip(t, m, m.Answers)
	})
}

// TestDnsCodecQNameParityMatterJS mirrors DnsCodecTest.ts:154-217 and
// :310-326 (decode QName) at 862ad75f by meaning: names are read from a
// message's question, a compressed name follows its pointer, and a name
// cut short or pointing at itself fails the message.
func TestDnsCodecQNameParityMatterJS(t *testing.T) {
	t.Parallel()
	question := func(name string) string { return "000000000001000000000000" + name + "00010001" }
	for _, c := range []struct {
		name, wire, want string
	}{
		{"one item", question("047465737400"), "test"},
		{"two items", question("047465737403636f6d00"), "test.com"},
		{"multiple items", question("0474686973026973016104746573740000"[:len("0474686973026973016104746573740000")-2]), "this.is.a.test"},
		{"a compressed reference to the first question", "000000000002000000000000" + "047465737400" + "00010001" + "c00c" + "00010001", "test"},
	} {
		t.Run("decodes "+c.name, func(t *testing.T) {
			t.Parallel()
			m := mustDNS(t, c.wire)
			if got := m.Questions[len(m.Questions)-1].Name; got != c.want {
				t.Errorf("name %q, want %q", got, c.want)
			}
		})
	}
	for _, c := range []struct{ name, wire string }{
		{"data truncated before the end", question("0474657374")[:len(question("0474657374"))-8]},
		{"a pointer past the remaining bytes", "000000000001000000000000" + "c0"},
		{"infinite recursion by referencing own element at the start", "000000000001000000000000" + "c00c" + "00010001"},
		{"infinite recursion inside the stream", "000000000001000000000000" + "0474657374c00c" + "00010001"},
	} {
		t.Run("refuses "+c.name, func(t *testing.T) {
			t.Parallel()
			raw, _ := hex.DecodeString(c.wire)
			if _, err := mdns.ParseDNSMessage(raw); err == nil {
				t.Errorf("ParseDNSMessage(%s) accepted", c.wire)
			}
		})
	}
}

func TestDnsCodecTXTParityMatterJS(t *testing.T) {
	t.Parallel()
	pack := func(t *testing.T, entries [][]byte) []byte {
		t.Helper()
		wire, err := mdns.PackDNSQuery(nil, []mdns.DNSRecord{{Name: "x.local", Type: mdns.DNSTypeTXT, TTL: 1, TXT: entries}})
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	t.Run("round-trips a TXT entry containing high bytes losslessly", func(t *testing.T) {
		t.Parallel()
		raw, _ := hex.DecodeString("5aaf359c0501a1b0")
		entry := append([]byte("xa="), raw...)
		m, err := mdns.ParseDNSMessage(pack(t, [][]byte{entry}))
		if err != nil || len(m.Answers) != 1 || len(m.Answers[0].TXT) != 1 || !bytes.Equal(m.Answers[0].TXT[0], entry) {
			t.Fatalf("TXT %+v, %v; want %x", m.Answers, err, entry)
		}
	})
	t.Run("encodes an empty TXT record as a single zero byte per RFC 6763 §6.1", func(t *testing.T) {
		t.Parallel()
		wire := pack(t, nil)
		// The record's RDLENGTH and RDATA close the message.
		if !bytes.HasSuffix(wire, []byte{0x00, 0x01, 0x00}) {
			t.Fatalf("wire %x does not end in RDLENGTH 1 and a zero byte", wire)
		}
		m, err := mdns.ParseDNSMessage(wire)
		if err != nil || len(m.Answers) != 1 || len(m.Answers[0].TXT) != 1 || len(m.Answers[0].TXT[0]) != 0 {
			t.Fatalf("TXT %+v, %v; want one empty string", m.Answers, err)
		}
	})
	t.Run("truncates entries longer than 255 bytes per RFC 6763 §6.1", func(t *testing.T) {
		t.Parallel()
		wire := pack(t, [][]byte{bytes.Repeat([]byte{0x41}, 300)})
		// RDLENGTH 256: one length byte of 0xFF and 255 bytes.
		if tail := wire[len(wire)-258:]; tail[0] != 0x01 || tail[1] != 0x00 || tail[2] != 0xff {
			t.Fatalf("wire tail %x, want RDLENGTH 0x0100 and a string of 255", tail[:3])
		}
	})
}

func TestDnsCodecAAAAParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, addr, wire string }{
		{"encodes and decodes AAAA fe80 record", "fe80::9580:b733:6f54:9f43", "fe800000000000009580b7336f549f43"},
		{"encodes and decodes AAAA record ::1", "::1", "00000000000000000000000000000001"},
		{"encodes and decodes AAAA record ::", "::", "00000000000000000000000000000000"},
		{"encodes and decodes AAAA record with more cutted 0s", "fe80::1", "fe800000000000000000000000000001"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			addr := netip.MustParseAddr(c.addr)
			wire, err := mdns.PackDNSQuery(nil, []mdns.DNSRecord{{Name: "h.local", Type: mdns.DNSTypeAAAA, TTL: 1, IP: addr}})
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(wire[len(wire)-16:]); got != c.wire {
				t.Fatalf("RDATA %s, want %s", got, c.wire)
			}
			m, err := mdns.ParseDNSMessage(wire)
			if err != nil || len(m.Answers) != 1 || m.Answers[0].IP != addr || m.Answers[0].IP.String() != c.addr {
				t.Fatalf("decoded %+v, %v; want %s", m.Answers, err, c.addr)
			}
		})
	}
}
