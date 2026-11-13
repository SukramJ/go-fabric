// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"net/netip"
	"testing"
)

// TestUnicastDestination holds the hint filter's classes one by one
// (review II, item 5): what net/netip classifies is judged by net/netip,
// and the two IPv4 blocks it does not classify by the RFC that defines
// each — the "by" column names which.
func TestUnicastDestination(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		addr string
		want bool
		by   string
	}{
		// net/netip: the unspecified address of either family.
		{"0.0.0.0", false, "netip: IsGlobalUnicast (unspecified)"},
		{"::", false, "netip: IsGlobalUnicast (unspecified)"},
		{"::%eth0", false, "netip: IsGlobalUnicast (unspecified), the zone set aside"},
		{"::ffff:0.0.0.0", false, "netip: IsGlobalUnicast (unspecified, IPv4-mapped)"},
		// net/netip: multicast.
		{"224.0.0.251", false, "netip: IsMulticast"},
		{"239.255.255.255", false, "netip: IsMulticast"},
		{"ff02::1", false, "netip: IsMulticast"},
		{"ff05::fb", false, "netip: IsMulticast"},
		{"::ffff:224.0.0.251", false, "netip: IsMulticast (IPv4-mapped)"},
		// net/netip: the IPv4 limited broadcast.
		{"255.255.255.255", false, "netip: IsGlobalUnicast (limited broadcast)"},
		{"::ffff:255.255.255.255", false, "netip: IsGlobalUnicast (limited broadcast, IPv4-mapped)"},
		// RFC 1122 §3.2.1.3 (b): 0.0.0.0/8.
		{"0.0.0.1", false, "RFC 1122 §3.2.1.3 (b)"},
		{"0.1.2.3", false, "RFC 1122 §3.2.1.3 (b)"},
		{"0.255.255.255", false, "RFC 1122 §3.2.1.3 (b)"},
		{"::ffff:0.1.2.3", false, "RFC 1122 §3.2.1.3 (b), IPv4-mapped"},
		// RFC 1112 §4: 240.0.0.0/4.
		{"240.0.0.0", false, "RFC 1112 §4"},
		{"240.0.0.1", false, "RFC 1112 §4"},
		{"255.255.255.254", false, "RFC 1112 §4"},
		{"::ffff:240.0.0.1", false, "RFC 1112 §4, IPv4-mapped"},
		// The neighbours of each block, and the unicast classes.
		{"1.0.0.0", true, "above 0.0.0.0/8"},
		{"223.255.255.255", true, "below 224.0.0.0/4"},
		{"192.0.2.7", true, "unicast"},
		{"10.1.2.3", true, "unicast (private)"},
		{"127.0.0.1", true, "unicast (loopback)"},
		{"169.254.1.2", true, "unicast (link-local)"},
		{"::1", true, "unicast (loopback)"},
		{"fe80::1%eth0", true, "unicast (link-local; HintAddresses asks for the zone)"},
		{"fe80::1", true, "unicast (link-local; HintAddresses asks for the zone)"},
		{"fd11::5", true, "unicast (unique local)"},
		{"2001:db8::1", true, "unicast"},
		{"feff::1", true, "below ff00::/8"},
		{"::ffff:192.0.2.7", true, "unicast (IPv4-mapped)"},
	} {
		if got := unicastDestination(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("unicastDestination(%s) = %v, want %v (%s)", tc.addr, got, tc.want, tc.by)
		}
	}
	if unicastDestination(netip.Addr{}) {
		t.Error("the zero Addr is a unicast destination")
	}
}
