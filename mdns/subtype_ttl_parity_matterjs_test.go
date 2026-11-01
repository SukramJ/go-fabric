// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// The subtype PTRs carry TTL 120 s, on a query answer and on an
// announcement: matter.js packages/general/src/codec/DnsCodec.ts:20
// DEFAULT_MDNS_TTL (the PTRs of
// packages/protocol/src/advertisement/mdns/CommissionableMdnsAdvertisement.ts:54-60),
// chip src/lib/dnssd/wire/records/ResourceRecord.h:35 kDefaultTtl. Review
// 2026-11 W2-wire-06.

import (
	"testing"

	"github.com/miekg/dns"
)

func TestSubtypePTRTTLParity(t *testing.T) {
	t.Parallel()
	const want = 120
	r := newTestResponder()
	qname := "_l3840._sub._matterc._udp.local."
	r.AddSubtype(qname, "inst._matterc._udp.local.")
	answers := r.matchAnswers([]dns.Question{{Name: qname, Qtype: dns.TypePTR}})
	if len(answers) != 1 || answers[0].Header().Ttl != want {
		t.Fatalf("query answer = %v, want one PTR with TTL %d", answers, want)
	}
	out, err := packSubtypePTRs(map[string]string{qname: "inst._matterc._udp.local."}, subtypePTRTTL)
	if err != nil {
		t.Fatal(err)
	}
	msg := new(dns.Msg)
	if err := msg.Unpack(out); err != nil {
		t.Fatal(err)
	}
	if len(msg.Answer) != 1 || msg.Answer[0].Header().Ttl != want {
		t.Errorf("announcement = %v, want one PTR with TTL %d", msg.Answer, want)
	}
}
