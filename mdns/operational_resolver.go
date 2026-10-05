// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ErrOperationalNotResolved is returned when a peer's operational instance
// did not resolve to an address before the context ended.
var ErrOperationalNotResolved = errors.New("mdns: operational instance not resolved")

// OperationalInstanceQName returns the DNS-SD name of a node's operational
// instance, `<CompressedFabricID>-<NodeID>._matter._tcp.local.`, both ids as
// 16 upper-case hex digits. Mirrors matter.js
// packages/protocol/src/mdns/MdnsConsts.ts:getOperationalDeviceQname, and
// is the instance name [BuildOperationalService] advertises.
func OperationalInstanceQName(compressedFabricID [8]byte, nodeID uint64) string {
	return fmt.Sprintf("%s-%016X.%s.local.", strings.ToUpper(hex.EncodeToString(compressedFabricID[:])), nodeID, ServiceTypeOperational)
}

// Query retry schedule. matter.js DnssdSolicitor.DefaultRetries
// (packages/general/src/net/dns-sd/DnssdSolicitor.ts): the first query
// goes out immediately, the next after 1 s, each later one after twice the
// previous interval, every interval jittered by ±20 %, capped at one hour.
const (
	resolveInitialInterval = time.Second
	resolveBackoffFactor   = 2
	resolveJitterFactor    = 0.2
	resolveMaxInterval     = time.Hour
)

// OperationalResolver finds the address of one peer's operational
// instance by multicast DNS query. It is the minimum matter.js's own
// operational discovery does for a peer it wants to reach
// (packages/general/src/net/dns-sd/IpServiceResolution.ts): query SRV and
// TXT for the instance, query A/AAAA for an SRV target that came without
// addresses, and stop at the first address. It is not a browser — it
// resolves exactly the one instance it is asked for and keeps nothing
// between calls.
//
// go-fabric uses it only to re-establish former subscriptions after a
// restart (docs/adr/0008).
type OperationalResolver struct {
	logger *slog.Logger
	open   func() (queryConn, error)
}

// queryConn is the multicast transport a resolution runs over. Production
// joins the mDNS groups on UDP/5353; tests substitute a fake responder.
type queryConn interface {
	// Send multicasts one DNS message.
	Send(msg []byte) error
	// Responses delivers received DNS messages with the index of the
	// interface they arrived on (0 when unknown).
	Responses() <-chan received
	Close() error
}

type received struct {
	msg     []byte
	ifIndex int
}

// NewOperationalResolver returns a resolver on the host's multicast
// interfaces.
func NewOperationalResolver(logger *slog.Logger) *OperationalResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &OperationalResolver{logger: logger, open: openMulticastQueryConn}
}

// ResolveOperational resolves the operational instance of nodeID on the
// fabric with compressedFabricID and returns its addresses, most desirable
// first (IPv6 link-local, ULA, other IPv6, IPv4 — matter.js
// ServerAddress.selectionPreferenceOf). It returns as soon as the SRV
// record and at least one address of its target are known, or
// [ErrOperationalNotResolved] when ctx ends first.
func (r *OperationalResolver) ResolveOperational(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	conn, err := r.open()
	if err != nil {
		return nil, fmt.Errorf("mdns: operational resolve: %w", err)
	}
	defer func() { _ = conn.Close() }()

	res := newResolution(OperationalInstanceQName(compressedFabricID, nodeID))
	send := func() {
		if err := conn.Send(res.query()); err != nil {
			r.logger.Debug("matter.mdns.resolve.send", slog.String("instance", res.instance), slog.String("err", err.Error()))
		}
	}
	send()
	interval := resolveInitialInterval
	timer := time.NewTimer(jitter(interval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %s: %w", ErrOperationalNotResolved, res.instance, ctx.Err())
		case <-timer.C:
			send()
			interval = min(interval*resolveBackoffFactor, resolveMaxInterval)
			timer.Reset(jitter(interval))
		case in, ok := <-conn.Responses():
			if !ok {
				return nil, fmt.Errorf("%w: %s: transport closed", ErrOperationalNotResolved, res.instance)
			}
			hadTarget := res.target != ""
			if !res.absorb(in) {
				continue
			}
			if addrs := res.addresses(); len(addrs) > 0 {
				return addrs, nil
			}
			// An SRV target without addresses: ask for them right away
			// rather than at the next retry (IpServiceResolution starts a
			// host discovery the moment the SRV names a host with no IPs).
			if !hadTarget && res.target != "" {
				send()
			}
		}
	}
}

func jitter(d time.Duration) time.Duration {
	f := 1 + resolveJitterFactor*(2*rand.Float64()-1) //nolint:gosec // retry jitter, not a secret
	return time.Duration(float64(d) * f)
}

// resolution accumulates the records of one instance across responses.
type resolution struct {
	instance string
	target   string
	port     uint16
	hosts    map[string][]netip.Addr // lower-case host name → addresses
	zones    map[netip.Addr]string
}

func newResolution(instance string) *resolution {
	return &resolution{
		instance: instance,
		hosts:    make(map[string][]netip.Addr),
		zones:    make(map[netip.Addr]string),
	}
}

// query builds the DNS question set for the current state: SRV + TXT for
// the instance (TXT explicitly, as IpServiceResolution does, because some
// responders omit it from the additional section) and, once the target is
// known, A + AAAA for it.
func (r *resolution) query() []byte {
	m := new(dns.Msg)
	m.Id = 0 // mDNS queries carry id 0 (RFC 6762 §18.1)
	m.RecursionDesired = false
	m.Question = []dns.Question{
		{Name: r.instance, Qtype: dns.TypeSRV, Qclass: dns.ClassINET},
		{Name: r.instance, Qtype: dns.TypeTXT, Qclass: dns.ClassINET},
	}
	if r.target != "" && len(r.hosts[strings.ToLower(r.target)]) == 0 {
		m.Question = append(
			m.Question,
			dns.Question{Name: r.target, Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
			dns.Question{Name: r.target, Qtype: dns.TypeA, Qclass: dns.ClassINET},
		)
	}
	out, err := m.Pack()
	if err != nil {
		return nil
	}
	return out
}

// absorb folds one received message in and reports whether it was a
// response at all.
func (r *resolution) absorb(in received) bool {
	m := new(dns.Msg)
	if err := m.Unpack(in.msg); err != nil || !m.Response {
		return false
	}
	zone := ""
	if in.ifIndex > 0 {
		if ifi, err := net.InterfaceByIndex(in.ifIndex); err == nil {
			zone = ifi.Name
		}
	}
	records := slices.Concat(m.Answer, m.Ns, m.Extra)
	for _, rr := range records {
		srv, ok := rr.(*dns.SRV)
		if ok && strings.EqualFold(srv.Hdr.Name, r.instance) && srv.Hdr.Ttl > 0 {
			r.target, r.port = srv.Target, srv.Port
		}
	}
	for _, rr := range records {
		var ip net.IP
		switch v := rr.(type) {
		case *dns.AAAA:
			ip = v.AAAA
		case *dns.A:
			ip = v.A
		default:
			continue
		}
		if rr.Header().Ttl == 0 {
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		host := strings.ToLower(rr.Header().Name)
		if !slices.Contains(r.hosts[host], addr) {
			r.hosts[host] = append(r.hosts[host], addr)
		}
		if addr.Is6() && addr.IsLinkLocalUnicast() && zone != "" {
			r.zones[addr] = zone
		}
	}
	return true
}

// addresses returns the resolved addresses, most desirable first, or nil
// while the SRV record or its target's addresses are still missing.
func (r *resolution) addresses() []*net.UDPAddr {
	if r.target == "" || r.port == 0 {
		return nil
	}
	ips := slices.Clone(r.hosts[strings.ToLower(r.target)])
	slices.SortStableFunc(ips, func(a, b netip.Addr) int {
		return cmp.Compare(SelectionPreference(a), SelectionPreference(b))
	})
	out := make([]*net.UDPAddr, 0, len(ips))
	for _, ip := range ips {
		zone := r.zones[ip]
		if ip.Is6() && ip.IsLinkLocalUnicast() && zone == "" {
			continue // unroutable without its interface
		}
		out = append(out, &net.UDPAddr{IP: ip.AsSlice(), Port: int(r.port), Zone: zone})
	}
	return out
}

// SelectionPreference ranks an address for connection attempts, lower
// first: IPv6 link-local 0, IPv6 ULA 1, other IPv6 2, IPv4 3. Mirrors
// matter.js packages/general/src/net/ServerAddress.ts:selectionPreferenceOfIp.
func SelectionPreference(a netip.Addr) int {
	a = a.Unmap()
	switch {
	case a.Is4():
		return 3
	case a.IsLinkLocalUnicast():
		return 0
	case a.As16()[0]&0xFE == 0xFC:
		return 1
	default:
		return 2
	}
}

// multicastQueryConn is the production [queryConn]: the mDNS groups on
// every multicast interface, UDP/5353, shared with the advertiser.
type multicastQueryConn struct {
	pc4  *ipv4.PacketConn
	pc6  *ipv6.PacketConn
	out  chan received
	once sync.Once
	wg   sync.WaitGroup
}

func openMulticastQueryConn() (queryConn, error) {
	c := &multicastQueryConn{out: make(chan received, 16)}
	pc4, err4 := joinMcast4()
	if err4 == nil {
		c.pc4 = pc4
		c.wg.Go(func() { c.read4() })
	}
	pc6, err6 := joinMcast6()
	if err6 == nil {
		c.pc6 = pc6
		c.wg.Go(func() { c.read6() })
	}
	if c.pc4 == nil && c.pc6 == nil {
		return nil, fmt.Errorf("join mDNS groups: %w / %w", err4, err6)
	}
	return c, nil
}

func (c *multicastQueryConn) Send(msg []byte) error {
	var errs []error
	sent := false
	if c.pc4 != nil {
		if _, err := c.pc4.WriteTo(msg, nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}); err != nil {
			errs = append(errs, err)
		} else {
			sent = true
		}
	}
	if c.pc6 != nil {
		if _, err := c.pc6.WriteTo(msg, nil, &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}); err != nil {
			errs = append(errs, err)
		} else {
			sent = true
		}
	}
	if sent {
		return nil
	}
	return errors.Join(errs...)
}

func (c *multicastQueryConn) deliver(buf []byte, ifIndex int) {
	msg := append([]byte(nil), buf...)
	select {
	case c.out <- received{msg: msg, ifIndex: ifIndex}:
	default: // a full queue drops; the next retry asks again
	}
}

func (c *multicastQueryConn) read4() {
	buf := make([]byte, 9000)
	for {
		n, cm, _, err := c.pc4.ReadFrom(buf)
		if err != nil {
			return
		}
		ifIndex := 0
		if cm != nil {
			ifIndex = cm.IfIndex
		}
		c.deliver(buf[:n], ifIndex)
	}
}

func (c *multicastQueryConn) read6() {
	buf := make([]byte, 9000)
	for {
		n, cm, _, err := c.pc6.ReadFrom(buf)
		if err != nil {
			return
		}
		ifIndex := 0
		if cm != nil {
			ifIndex = cm.IfIndex
		}
		c.deliver(buf[:n], ifIndex)
	}
}

func (c *multicastQueryConn) Responses() <-chan received { return c.out }

func (c *multicastQueryConn) Close() error {
	c.once.Do(func() {
		if c.pc4 != nil {
			_ = c.pc4.Close()
		}
		if c.pc6 != nil {
			_ = c.pc6.Close()
		}
		c.wg.Wait()
	})
	return nil
}
