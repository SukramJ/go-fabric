// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// White-box tests for the responder's socket helpers and the advertise
// address policy (socket.go).

package mdns

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"testing"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/internal/clockvar"
)

func TestIsTimeout_Nil(t *testing.T) {
	t.Parallel()
	if isTimeout(nil) {
		t.Error("isTimeout(nil) = true, want false")
	}
}

func TestIsTimeout_NonNetError(t *testing.T) {
	t.Parallel()
	err := dns.ErrBuf // arbitrary non-net.Error
	if isTimeout(err) {
		t.Errorf("isTimeout(%T) = true, want false", err)
	}
}

func TestIsPrimaryV4_Loopback(t *testing.T) {
	t.Parallel()
	lo := &net.Interface{
		Flags: net.FlagUp | net.FlagLoopback | net.FlagMulticast,
	}
	if isPrimaryV4(lo) {
		t.Error("isPrimaryV4(loopback) = true, want false")
	}
}

func TestIsPrimaryV4_PointToPoint(t *testing.T) {
	t.Parallel()
	ptp := &net.Interface{
		Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast,
	}
	if isPrimaryV4(ptp) {
		t.Error("isPrimaryV4(point-to-point) = true, want false")
	}
}

func TestIsPrimaryV6_Loopback(t *testing.T) {
	t.Parallel()
	lo := &net.Interface{
		Flags: net.FlagUp | net.FlagLoopback | net.FlagMulticast,
	}
	if isPrimaryV6(lo) {
		t.Error("isPrimaryV6(loopback) = true, want false")
	}
}

func TestIsPrimaryV6_PointToPoint(t *testing.T) {
	t.Parallel()
	ptp := &net.Interface{
		Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast,
	}
	if isPrimaryV6(ptp) {
		t.Error("isPrimaryV6(point-to-point) = true, want false")
	}
}

func TestListMulticastInterfaces_ReturnsNonNilOnAnyOS(t *testing.T) {
	t.Parallel()
	// May be empty in a sandbox but must not panic.
	ifaces := listMulticastInterfaces()
	// All returned entries must have FlagUp + FlagMulticast set.
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 {
			t.Errorf("interface %q: FlagUp not set", ifi.Name)
		}
		if ifi.Flags&net.FlagMulticast == 0 {
			t.Errorf("interface %q: FlagMulticast not set", ifi.Name)
		}
	}
}

// TestIsPrimaryV4_AllInterfaces exercises isPrimaryV4 against every
// interface on the host so the function body is exercised regardless
// of which flag combinations the test runner has. We only check that
// the call does not panic — correctness of the "true" case depends on
// the host having a non-loopback IPv4 interface.
func TestIsPrimaryV4_AllSystemInterfaces(t *testing.T) {
	t.Parallel()
	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		ifi := &ifaces[i]
		// Must not panic.
		_ = isPrimaryV4(ifi)
	}
}

func TestIsPrimaryV6_AllSystemInterfaces(t *testing.T) {
	t.Parallel()
	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		ifi := &ifaces[i]
		_ = isPrimaryV6(ifi)
	}
}

// TestSurvivesReadError pins the receive-loop policy: a transient read
// error keeps the loop alive, a closed socket or a cancelled context
// stops it, and a socket that fails on every read is abandoned after the
// cap rather than spun on. Ending the loop on the first transient error
// silently stops answering for the process lifetime — the node drops out
// of every commissioner's browse.
func TestSurvivesReadError(t *testing.T) {
	t.Parallel()
	r := &sockets{log: slog.New(slog.DiscardHandler), clk: new(clockvar.Var)}
	transient := errors.New("recvmsg: interrupted system call")

	consecutive := 0
	if !r.survivesReadError(context.Background(), "v4", transient, &consecutive) {
		t.Fatal("a single transient read error ended the loop")
	}
	if consecutive != 1 {
		t.Errorf("consecutive = %d, want 1", consecutive)
	}

	// A closed socket is terminal regardless of the counter.
	consecutive = 0
	if r.survivesReadError(context.Background(), "v4", net.ErrClosed, &consecutive) {
		t.Error("loop continued after net.ErrClosed")
	}

	// A cancelled context is terminal too.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	consecutive = 0
	if r.survivesReadError(ctx, "v6", transient, &consecutive) {
		t.Error("loop continued after the context was cancelled")
	}

	// Persistent failure gives up at the cap instead of spinning.
	consecutive = maxConsecutiveReadErrors - 1
	if r.survivesReadError(context.Background(), "v6", transient, &consecutive) {
		t.Error("loop continued past maxConsecutiveReadErrors")
	}
}

// TestPrimaryHostIPs_ReturnsNonNilSlice verifies primaryHostIPs does not
// panic and returns either nil (no suitable interface) or at least one
// address. We cannot assert the exact result because the host's network
// configuration varies.
func TestPrimaryHostIPs_ReturnsNonNilSlice(t *testing.T) {
	t.Parallel()
	ips := primaryHostIPs(nil)
	// Must not panic. Result may be nil on a loopback-only environment.
	for _, ip := range ips {
		if ip == "" {
			t.Error("primaryHostIPs returned empty string IP")
		}
	}
}

// TestFilterPrimaryHostIPs verifies the advertise policy documented on
// filterPrimaryHostIPs with the default (nil) filter: container/
// virtualisation bridges are dropped by name, down/non-multicast/loopback/point-to-point interfaces are dropped
// by flag, IPv4 sorts before IPv6, duplicates are deduplicated, and
// link-local addresses are excluded (IPv6 ones only where the interface
// has a routable IPv6) while global IPv6 survives.
func TestFilterPrimaryHostIPs(t *testing.T) {
	t.Parallel()
	ip := net.ParseIP

	tests := []struct {
		name   string
		ifaces []hostIface
		want   []string
	}{
		{
			name: "container bridges dropped, real LAN interface kept",
			ifaces: []hostIface{
				{name: "docker0", up: true, multicast: true, ips: []net.IP{ip("172.17.0.1")}},
				{name: "hassio", up: true, multicast: true, ips: []net.IP{ip("172.30.32.1")}},
				{name: "br-1a2b3c4d", up: true, multicast: true, ips: []net.IP{ip("172.18.0.1")}},
				{name: "eth0", up: true, multicast: true, ips: []net.IP{ip("192.168.1.10")}},
			},
			want: []string{"192.168.1.10"},
		},
		{
			name: "down interface dropped",
			ifaces: []hostIface{
				{name: "eth0", up: false, multicast: true, ips: []net.IP{ip("192.168.1.10")}},
			},
			want: nil,
		},
		{
			name: "non-multicast interface dropped",
			ifaces: []hostIface{
				{name: "eth0", up: true, multicast: false, ips: []net.IP{ip("192.168.1.10")}},
			},
			want: nil,
		},
		{
			name: "loopback interface dropped",
			ifaces: []hostIface{
				{name: "lo", up: true, multicast: true, loopback: true, ips: []net.IP{ip("127.0.0.1")}},
			},
			want: nil,
		},
		{
			name: "point-to-point interface dropped",
			ifaces: []hostIface{
				{name: "tun0", up: true, multicast: true, pointToPoint: true, ips: []net.IP{ip("10.8.0.2")}},
			},
			want: nil,
		},
		{
			name: "IPv4 sorted before IPv6",
			ifaces: []hostIface{
				{name: "eth0", up: true, multicast: true, ips: []net.IP{ip("2001:db8::1"), ip("192.168.1.10")}},
			},
			want: []string{"192.168.1.10", "2001:db8::1"},
		},
		{
			name: "duplicate IPs across interfaces are deduplicated",
			ifaces: []hostIface{
				{name: "eth0", up: true, multicast: true, ips: []net.IP{ip("192.168.1.10")}},
				{name: "eth0:1", up: true, multicast: true, ips: []net.IP{ip("192.168.1.10")}},
			},
			want: []string{"192.168.1.10"},
		},
		{
			name: "IPv4 and IPv6 link-local dropped, global IPv6 kept",
			ifaces: []hostIface{
				{name: "eth0", up: true, multicast: true, ips: []net.IP{
					ip("169.254.1.1"),
					ip("fe80::1"),
					ip("192.168.1.10"),
					ip("2001:db8::1"),
				}},
			},
			want: []string{"192.168.1.10", "2001:db8::1"},
		},
		{
			// An interface with only a link-local IPv6 still advertises
			// it — an IPv6-only resolver (chip-tool) has nothing else to
			// reach the node over; chip and matter.js publish every IPv6.
			name: "IPv6 link-local kept where the interface has no routable IPv6",
			ifaces: []hostIface{
				{name: "eth0", up: true, multicast: true, ips: []net.IP{
					ip("10.1.0.4"),
					ip("fe80::20d:3aff:fe1b:2c3d"),
				}},
				{name: "eth1", up: true, multicast: true, ips: []net.IP{
					ip("fe80::1"),
					ip("2001:db8::2"),
				}},
			},
			want: []string{"10.1.0.4", "fe80::20d:3aff:fe1b:2c3d", "2001:db8::2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterPrimaryHostIPs(tc.ifaces, nil)
			if !slices.Equal(got, tc.want) {
				t.Errorf("filterPrimaryHostIPs() = %v, want %v", got, tc.want)
			}
		})
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
