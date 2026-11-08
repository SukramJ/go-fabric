// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/clockvar"
)

// The responder's sockets: one IPv4 and one IPv6 UDP socket on port 5353,
// joined to 224.0.0.251 and ff02::fb on every multicast interface, with the
// interface of each received packet reported so the answer goes out where
// the query came in (matter.js answers per interface, MdnsServer.ts:187-197
// at 862ad75f). The wildcard multicast bind coexists with the OS responder
// (avahi, mDNSResponder) and with the module's own querier (query_conn.go).

var (
	mdnsGroup4 = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	mdnsGroup6 = &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}
)

// sockets is the production [wire].
type sockets struct {
	log *slog.Logger
	// clk paces the read-error backoff (the responder's clock).
	clk     *clockvar.Var
	exclude func(string) bool
	// handle receives every inbound packet with its interface and source.
	handle func(buf []byte, ifIndex int, src *net.UDPAddr)

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// mu guards pc4, pc6 and closed against the sends: close nils the
	// handles under the write lock.
	mu     sync.RWMutex
	pc4    *ipv4.PacketConn
	pc6    *ipv6.PacketConn
	closed bool
}

// openSockets binds both families and starts their receive loops. Either
// family alone suffices; the error says why neither bound.
func openSockets(exclude func(string) bool, log *slog.Logger, clk *clockvar.Var, handle func([]byte, int, *net.UDPAddr)) (*sockets, error) {
	s := &sockets{log: log, clk: clk, exclude: exclude, handle: handle}
	pc4, err4 := joinMcast4()
	if err4 != nil {
		log.Debug("mdns.responder.udp4_join_failed", slog.Any("err", err4))
	} else {
		s.pc4 = pc4
	}
	pc6, err6 := joinMcast6()
	if err6 != nil {
		log.Debug("mdns.responder.udp6_join_failed", slog.Any("err", err6))
	} else {
		s.pc6 = pc6
	}
	if s.pc4 == nil && s.pc6 == nil {
		return nil, fmt.Errorf("bind udp4 and udp6 port 5353: %w / %w", err4, err6)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if s.pc4 != nil {
		s.wg.Go(func() { s.serve(ctx, "v4", s.read4) })
	}
	if s.pc6 != nil {
		s.wg.Go(func() { s.serve(ctx, "v6", s.read6) })
	}
	log.Info("mdns.responder.started", slog.Bool("ipv4", s.pc4 != nil), slog.Bool("ipv6", s.pc6 != nil))
	return s, nil
}

// close ends the receive loops and releases both sockets. Idempotent.
func (s *sockets) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.pc4 != nil {
		err = errors.Join(err, s.pc4.Close())
		s.pc4 = nil
	}
	if s.pc6 != nil {
		err = errors.Join(err, s.pc6.Close())
		s.pc6 = nil
	}
	return err
}

// read4 / read6 read one packet with a one-second deadline, so a closed
// responder winds down without a packet to wake it. Raw time.Now: the
// kernel judges a socket deadline against the wall clock, so no injected
// clock can stand in (clock/noraw_test.go).
func (s *sockets) read4(buf []byte) (n, ifIndex int, src net.Addr, err error) {
	_ = s.pc4.SetReadDeadline(time.Now().Add(time.Second))
	n, cm, src, err := s.pc4.ReadFrom(buf)
	if cm != nil {
		ifIndex = cm.IfIndex
	}
	return n, ifIndex, src, err
}

func (s *sockets) read6(buf []byte) (n, ifIndex int, src net.Addr, err error) {
	_ = s.pc6.SetReadDeadline(time.Now().Add(time.Second))
	n, cm, src, err := s.pc6.ReadFrom(buf)
	if cm != nil {
		ifIndex = cm.IfIndex
	}
	return n, ifIndex, src, err
}

// serve is one receive loop. A packet without an interface is dropped: the
// answer could not be scoped to the link it came from.
func (s *sockets) serve(ctx context.Context, family string, read func([]byte) (int, int, net.Addr, error)) {
	buf := make([]byte, 9000)
	consecutive := 0
	for ctx.Err() == nil {
		n, ifIndex, src, err := read(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			if !s.survivesReadError(ctx, family, err, &consecutive) {
				return
			}
			continue
		}
		consecutive = 0
		from, _ := src.(*net.UDPAddr)
		if ifIndex <= 0 || from == nil {
			continue
		}
		s.handle(buf[:n], ifIndex, from)
	}
}

// survivesReadError decides whether a receive loop continues after a
// non-timeout read error: false once the socket is closed, the context
// is done, or the error repeated [maxConsecutiveReadErrors] times.
func (s *sockets) survivesReadError(ctx context.Context, family string, err error, consecutive *int) bool {
	if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
		return false
	}
	*consecutive++
	if *consecutive >= maxConsecutiveReadErrors {
		s.log.Error("mdns.responder.read_giving_up",
			slog.String("family", family),
			slog.Any("err", err),
			slog.Int("consecutive_errors", *consecutive),
			slog.String("hint", "mDNS answering stopped; commissioners will not discover this node until it restarts"))
		return false
	}
	s.log.Warn("mdns.responder.read_err",
		slog.String("family", family),
		slog.Any("err", err),
		slog.Int("consecutive_errors", *consecutive))
	select {
	case <-ctx.Done():
		return false
	case <-clock.After(s.clk.Load(), readErrorBackoff):
	}
	return true
}

// links implements [wire]: one listing of the host's interfaces, each
// up, multicast-capable one with what the advertise policy publishes
// there.
func (s *sockets) links() []link {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]link, 0, len(ifaces))
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		out = append(out, s.linkOf(ifi))
	}
	return out
}

// link implements [wire].
func (s *sockets) link(ifIndex int) link {
	ifi, err := net.InterfaceByIndex(ifIndex)
	if err != nil {
		return link{index: ifIndex}
	}
	l := s.linkOf(*ifi)
	l.listed = ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagMulticast != 0
	return l
}

// linkOf is ifi as a [link]: the advertise policy's addresses, IPv4
// dropped when no IPv4 socket bound (matter.js MdnsAdvertisement.ts:166-169
// supportsIpv4 at 862ad75f), and the families of all its addresses.
func (s *sockets) linkOf(ifi net.Interface) link {
	h := hostIfaceOf(ifi)
	l := link{index: ifi.Index, name: ifi.Name, listed: true}
	for _, ip := range h.ips {
		if ip.To4() != nil {
			l.has4 = true
		} else {
			l.has6 = true
		}
	}
	ips := parseHostIPs(filterPrimaryHostIPs([]hostIface{h}, s.exclude))
	s.mu.RLock()
	has4 := s.pc4 != nil
	s.mu.RUnlock()
	if !has4 {
		ips = slices.DeleteFunc(ips, func(ip net.IP) bool { return ip.To4() != nil })
	}
	l.addrs = ips
	return l
}

// multicast implements [wire]: the IPv4 group when the interface has an
// IPv4 address and an IPv4 socket bound, the IPv6 group likewise. It
// reports how far the datagram reached — matter.js UdpMulticastServer.send's
// reach (packages/general/src/net/dns-sd/MdnsSocket.ts:102-127 at
// db021da0): none when no family was usable, failed when every family it
// tried refused it, with the refusals joined.
func (s *sockets) multicast(pkt []byte, l link) (reach, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return reachNone, nil
	}
	tried, ok := 0, 0
	var errs []error
	if l.has4 && s.pc4 != nil {
		tried++
		if _, err := s.pc4.WriteTo(pkt, &ipv4.ControlMessage{IfIndex: l.index}, mdnsGroup4); err != nil {
			errs = append(errs, fmt.Errorf("%s (IPv4): %w", l.name, err))
		} else {
			ok++
		}
	}
	if l.has6 && s.pc6 != nil {
		tried++
		if _, err := s.pc6.WriteTo(pkt, &ipv6.ControlMessage{IfIndex: l.index}, mdnsGroup6); err != nil {
			errs = append(errs, fmt.Errorf("%s (IPv6): %w", l.name, err))
		} else {
			ok++
		}
	}
	return reachOf(tried, ok), errors.Join(errs...)
}

// unicast implements [wire]: the querier at its source address and port —
// 5353 for an mDNS querier, the legacy querier's own port otherwise
// (RFC 6762 §6.7).
func (s *sockets) unicast(pkt []byte, ifIndex int, dst *net.UDPAddr) (reach, error) {
	if dst == nil {
		return reachNone, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return reachNone, nil
	}
	var err error
	switch {
	case dst.IP.To4() != nil && s.pc4 != nil:
		_, err = s.pc4.WriteTo(pkt, &ipv4.ControlMessage{IfIndex: ifIndex}, dst)
	case dst.IP.To4() == nil && s.pc6 != nil:
		_, err = s.pc6.WriteTo(pkt, &ipv6.ControlMessage{IfIndex: ifIndex}, dst)
	default:
		return reachNone, nil
	}
	if err != nil {
		return reachFailed, fmt.Errorf("to %s: %w", dst, err)
	}
	return reachAll, nil
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := errors.AsType[net.Error](err); ok {
		return ne.Timeout()
	}
	return false
}

// maxConsecutiveReadErrors bounds how many back-to-back non-timeout read
// errors a receive loop tolerates before it gives up. A transient socket
// error must not end answering — the node would drop out of every
// commissioner's browse — but a socket that fails on every read would
// otherwise spin.
const maxConsecutiveReadErrors = 20

// readErrorBackoff paces the retry after a non-timeout read error so a
// permanently broken socket cannot burn a core before the give-up cap is
// reached.
const readErrorBackoff = 100 * time.Millisecond

// hostIface is the testable shape of one network interface: its name,
// the flags the filter consults, and its unicast IPs. Decoupled from
// net.Interface so filterPrimaryHostIPs can be unit-tested without
// real interfaces.
type hostIface struct {
	name         string
	up           bool
	multicast    bool
	loopback     bool
	pointToPoint bool
	ips          []net.IP
}

// primaryHostIPs returns the curated host-IP list the Matter mDNS
// records should publish (see filterPrimaryHostIPs for the policy).
// exclude may be nil; see filterPrimaryHostIPs.
func primaryHostIPs(exclude func(string) bool) []string {
	return filterPrimaryHostIPs(hostIfaces(), exclude)
}

// parseHostIPs turns the advertised address strings back into net.IP so a
// published [Service] reports the same addresses the A/AAAA records carry.
func parseHostIPs(ips []string) []net.IP {
	if len(ips) == 0 {
		return nil
	}
	out := make([]net.IP, 0, len(ips))
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// filterPrimaryHostIPs applies the advertise policy: the routable IPv4 +
// globally-routable IPv6 addresses (an interface's link-local IPv6 only when
// it has no routable one) from every non-loopback, non-tunnel,
// multicast-capable interface that is currently UP — excluding container /
// virtualisation bridges by interface name. Apple iOS Matter daemon
// iterates the published address list during resolve and aborts on
// unreachable addresses before trying the next — every IP on every
// interface (loopback, link-local duplicates, tunnels) made pairing
// silently time out. Container
// bridges (docker0, hassio, br-<hex>, veth*, …) pass the flag checks —
// they are UP and multicast-capable — but their addresses are unroutable
// from LAN peers, so they are dropped by exclude — nil means
// [DefaultInterfaceFilter], which holds the same policy the daemon's
// client-discovery advertiser applies.
//
// Collecting from all valid NICs (multi-homed hosts have multiple
// physical or VLAN interfaces) ensures the bridge is reachable from
// any network segment. Duplicates are deduplicated by string value.
//
// Returns string form, IPv4 before IPv6: the order of the
// [Service.Addresses] Active reports. The A/AAAA records order the
// addresses by selection preference (records.go addressRecords).
func filterPrimaryHostIPs(ifaces []hostIface, exclude func(string) bool) []string {
	if exclude == nil {
		exclude = DefaultInterfaceFilter
	}
	seen := make(map[string]struct{})
	var v4s, v6s []string
	for _, ifi := range ifaces {
		if !ifi.up || !ifi.multicast || ifi.loopback || ifi.pointToPoint {
			continue
		}
		if exclude(ifi.name) {
			continue
		}
		// An interface whose only IPv6 is link-local still publishes it:
		// Matter is IPv6-first — chip-tool resolves operational nodes over
		// IPv6 only — and chip and matter.js (NodeJsNetwork) publish every
		// IPv6 address of the interface. Where a routable IPv6 exists the
		// link-local one stays out, the policy above.
		keepLinkLocal6 := !hasRoutableIPv6(ifi.ips)
		for _, ip := range ifi.ips {
			if ip == nil || ip.IsLoopback() {
				continue
			}
			s := ip.String()
			if _, dup := seen[s]; dup {
				continue
			}
			if four := ip.To4(); four != nil {
				if !four.IsLinkLocalUnicast() {
					seen[s] = struct{}{}
					v4s = append(v4s, four.String())
				}
				continue
			}
			if ip.IsUnspecified() || (ip.IsLinkLocalUnicast() && !keepLinkLocal6) {
				continue
			}
			seen[s] = struct{}{}
			v6s = append(v6s, s)
		}
	}
	return append(v4s, v6s...)
}

// hasRoutableIPv6 reports whether ips holds an IPv6 address that is
// neither link-local, loopback nor unspecified.
func hasRoutableIPv6(ips []net.IP) bool {
	for _, ip := range ips {
		if ip == nil || ip.To4() != nil {
			continue
		}
		if !ip.IsLinkLocalUnicast() && !ip.IsLoopback() && !ip.IsUnspecified() {
			return true
		}
	}
	return false
}

// hostIfaceOf is ifi with its unicast addresses; an interface whose
// addresses cannot be read has none.
func hostIfaceOf(ifi net.Interface) hostIface {
	info := hostIface{
		name:         ifi.Name,
		up:           ifi.Flags&net.FlagUp != 0,
		multicast:    ifi.Flags&net.FlagMulticast != 0,
		loopback:     ifi.Flags&net.FlagLoopback != 0,
		pointToPoint: ifi.Flags&net.FlagPointToPoint != 0,
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return info
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			info.ips = append(info.ips, ipn.IP)
		}
	}
	return info
}

// hostAddrsFor returns the address source of the host-name answers: the
// addresses [filterPrimaryHostIPs] publishes for the one interface a query
// arrived on — none for an interface the policy excludes (loopback, tunnel,
// container bridge, down), so a query from such a link is not answered
// with addresses that are not reachable over it.
func hostAddrsFor(exclude func(string) bool) func(ifIndex int) []net.IP {
	return func(ifIndex int) []net.IP {
		ifi, err := net.InterfaceByIndex(ifIndex)
		if err != nil {
			return nil
		}
		return parseHostIPs(filterPrimaryHostIPs([]hostIface{hostIfaceOf(*ifi)}, exclude))
	}
}

// joinMcast4 / joinMcast6 bind a fresh UDP socket to port 5353 and join
// the mDNS group on every multicast interface. The multicast wildcard
// bind (SO_REUSEADDR) coexists with the OS responder and with the
// module's querier; bind + JoinGroup is the approach both Linux and macOS
// support.
func joinMcast4() (*ipv4.PacketConn, error) {
	addr := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 0), Port: 5353}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	pc := ipv4.NewPacketConn(conn)
	if err := pc.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		_ = conn.Close()
		return nil, err
	}
	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251)}
	joined := 0
	var primaryIface *net.Interface
	// One snapshot: the loop bound and the indexed slice must come from
	// the same enumeration. Re-reading the interface list per iteration
	// indexes a second, independently-taken snapshot — an interface going
	// down (container churn on the host) or a transient net.Interfaces()
	// error between the two syscalls then panics with index out of range
	// on the daemon's boot goroutine, where nothing recovers.
	ifaces := listMulticastInterfaces()
	for i := range ifaces {
		ifi := ifaces[i]
		if jerr := pc.JoinGroup(&ifi, group); jerr == nil {
			joined++
			// First non-loopback, non-tunnel interface with an IPv4
			// address wins. Used as the default outgoing interface
			// for multicast sends — without this, macOS rejects
			// `WriteTo(224.0.0.251:5353)` with `network unreachable`
			// because the wildcard listen socket has no implicit
			// route into a multicast group.
			if primaryIface == nil && isPrimaryV4(&ifi) {
				primaryIface = &ifi
			}
		}
	}
	if joined == 0 {
		_ = pc.Close()
		return nil, errors.New("mdns: no multicast interface accepted IPv4 join")
	}
	if primaryIface != nil {
		_ = pc.SetMulticastInterface(primaryIface)
	}
	return pc, nil
}

func joinMcast6() (*ipv6.PacketConn, error) {
	addr := &net.UDPAddr{IP: net.ParseIP("ff02::"), Port: 5353}
	conn, err := net.ListenUDP("udp6", addr)
	if err != nil {
		return nil, err
	}
	pc := ipv6.NewPacketConn(conn)
	if err := pc.SetControlMessage(ipv6.FlagInterface, true); err != nil {
		_ = conn.Close()
		return nil, err
	}
	group := &net.UDPAddr{IP: net.ParseIP("ff02::fb")}
	joined := 0
	var primaryIface *net.Interface
	// One snapshot — see joinMcast4 for why the list must not be
	// re-enumerated inside the loop.
	ifaces := listMulticastInterfaces()
	for i := range ifaces {
		ifi := ifaces[i]
		if jerr := pc.JoinGroup(&ifi, group); jerr == nil {
			joined++
			if primaryIface == nil && isPrimaryV6(&ifi) {
				primaryIface = &ifi
			}
		}
	}
	if joined == 0 {
		_ = pc.Close()
		return nil, errors.New("mdns: no multicast interface accepted IPv6 join")
	}
	if primaryIface != nil {
		_ = pc.SetMulticastInterface(primaryIface)
	}
	return pc, nil
}

// isPrimaryV4 reports whether the interface looks like the host's
// primary IPv4 multicast egress: not a loopback, has at least one
// non-loopback IPv4 address, not a point-to-point tunnel. The first
// one matched becomes the SetMulticastInterface target.
func isPrimaryV4(ifi *net.Interface) bool {
	if ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagPointToPoint != 0 {
		return false
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if v4 := ipn.IP.To4(); v4 != nil && !v4.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

func isPrimaryV6(ifi *net.Interface) bool {
	if ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagPointToPoint != 0 {
		return false
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if ipn.IP.To4() == nil && !ipn.IP.IsUnspecified() {
			return true
		}
	}
	return false
}

// listMulticastInterfaces returns the system interfaces that are up
// and multicast-capable.
func listMulticastInterfaces() []net.Interface {
	out := make([]net.Interface, 0, 4)
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifi := range ifaces {
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		if (ifi.Flags & net.FlagMulticast) == 0 {
			continue
		}
		out = append(out, ifi)
	}
	return out
}
