// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gendiag "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiag "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/im"
)

// The reconnect probe (step 18.1): while the operational connection waits
// for the node, the commissioner asks the device over the still-open
// commissioning session what its network state is, logs it, and hands the
// routable addresses it reports to the connection. Ahead of matter.js and
// connectedhomeip, which wait for DNS-SD here; a node whose operational
// record a Thread border router never publishes is otherwise unreachable,
// and a failure says nothing of why. The report is the device's statement
// and nothing verifies its addresses: what the probe takes from it is
// bounded by what the attribute may hold — the interfaces the connection
// can use before any other (addInterfaces) — and filtered to addresses of
// one other host (routableAddr), and the connection takes at most
// peer.MaxHintedAddresses of them (peer.Peer.HintAddresses).
const (
	// reconnectProbeFirst is how long the transition runs before the
	// first probe: a reconnect discovery serves at once needs none, and
	// the commissioning channel stays quiet for it.
	reconnectProbeFirst = 2 * time.Second
	// reconnectProbeInterval is the time between two probes.
	reconnectProbeInterval = 10 * time.Second
	// reconnectProbeTimeout bounds one probe's read.
	reconnectProbeTimeout = 8 * time.Second
	// operationalPort is Matter's default operational port, where a probed
	// address is attempted. The node's real port is only in its SRV
	// record; once discovery delivers it, the discovered addresses take
	// over (peer.Peer.HintAddresses).
	operationalPort uint16 = 5540
)

// deviceNetworkReport is what one probe read of the device's network
// state: each field nil (or empty) when the device did not report it.
type deviceNetworkReport struct {
	networkingStatus *string // NetworkCommissioning LastNetworkingStatus, by name
	connectError     *int32  // LastConnectErrorValue
	connected        *bool   // any of the root's Networks Connected
	threadRole       *string // ThreadNetworkDiagnostics RoutingRole, by name
	networkName      *string
	channel          *uint16
	partitionID      *uint32
	// meshLocal is ThreadNetworkDiagnostics MeshLocalPrefix, invalid when
	// absent, null or malformed.
	meshLocal netip.Prefix
	// addresses are the addresses of the operational interfaces
	// (GeneralDiagnostics NetworkInterfaces with IsOperational); routable
	// are those the operational connection can use (hintable), meshLocal
	// counts the Thread mesh-local ones left out of it.
	addresses      []netip.Addr
	routable       []netip.Addr
	meshLocalCount int
	// interfacesKnown is NetworkInterfaces read at all.
	interfacesKnown bool
	// interfacesOver counts the operational interfaces with a usable
	// address past the attribute's list bound, which are not taken
	// (addInterfaces).
	interfacesOver int
}

// key is the report as one comparable string: a probe logs when it
// changed.
func (r *deviceNetworkReport) key() string {
	var b strings.Builder
	opt := func(name string, set bool, v any) {
		if set {
			fmt.Fprintf(&b, "%s=%v;", name, v)
		}
	}
	opt("status", r.networkingStatus != nil, deref(r.networkingStatus))
	opt("err", r.connectError != nil, deref(r.connectError))
	opt("connected", r.connected != nil, deref(r.connected))
	opt("role", r.threadRole != nil, deref(r.threadRole))
	opt("name", r.networkName != nil, deref(r.networkName))
	opt("channel", r.channel != nil, deref(r.channel))
	opt("partition", r.partitionID != nil, deref(r.partitionID))
	opt("mesh", r.meshLocal.IsValid(), r.meshLocal)
	opt("addresses", r.interfacesKnown, r.addresses)
	return b.String()
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// summary is the report as the Reconnect step's failure names it, e.g.
// `the device reports Thread role=Unassigned, last networking
// status=NetworkNotFound`. The network id (an SSID or an extended PAN id)
// is not part of it.
func (r *deviceNetworkReport) summary() string {
	var parts []string
	if r.threadRole != nil {
		s := "Thread role=" + *r.threadRole
		if r.networkName != nil && *r.networkName != "" {
			s += fmt.Sprintf(" on %q", *r.networkName)
		}
		if r.channel != nil {
			s += fmt.Sprintf(" channel %d", *r.channel)
		}
		parts = append(parts, s)
	}
	if r.networkingStatus != nil {
		s := "last networking status=" + *r.networkingStatus
		if r.connectError != nil {
			s += fmt.Sprintf(" (error %d)", *r.connectError)
		}
		parts = append(parts, s)
	}
	if r.connected != nil {
		parts = append(parts, fmt.Sprintf("network connected=%t", *r.connected))
	}
	if r.interfacesKnown {
		if len(r.routable) == 0 {
			parts = append(parts, "no routable address")
		} else {
			parts = append(parts, "addresses "+addrList(r.routable)+"; not reachable at them")
		}
		switch r.meshLocalCount {
		case 0:
		case 1:
			parts = append(parts, "1 mesh-local address omitted")
		default:
			parts = append(parts, fmt.Sprintf("%d mesh-local addresses omitted", r.meshLocalCount))
		}
	}
	if len(parts) == 0 {
		return "the device reports no network state"
	}
	return "the device reports " + strings.Join(parts, ", ")
}

func addrList(addrs []netip.Addr) string {
	s := make([]string, len(addrs))
	for i, a := range addrs {
		s[i] = a.String()
	}
	return "[" + strings.Join(s, ", ") + "]"
}

// The two IPv4 blocks net/netip does not classify and no datagram to a
// host is addressed to: 0.0.0.0/8 (RFC 1122 §3.2.1.3, "this network") and
// 240.0.0.0/4 (RFC 1112 §4, reserved) — the blocks, with the quoted text
// and their RFC 6890 registry entries, of client/peer's hint filter
// (hint.go, ipv4ThisNetwork and ipv4Reserved).
var (
	ipv4ThisNetwork = netip.MustParsePrefix("0.0.0.0/8")
	ipv4Reserved    = netip.MustParsePrefix("240.0.0.0/4")
)

// routableAddr reports whether a probed address can carry the operational
// connection. [netip.Addr.IsGlobalUnicast] refuses what names no peer —
// the zero Addr, the unspecified address, multicast, the IPv4 limited
// broadcast — and loopback and link-local unicast with them: a link-local
// address has no zone the controller could use across the border router.
// In IPv4 the two blocks the package does not classify are refused beside
// them, as peer.Peer.HintAddresses passes them over. An IPv4-mapped
// address is judged as its IPv4 address. A Thread mesh-local address is
// routable by this test but not from off the mesh (meshLocalAddr).
func routableAddr(a netip.Addr) bool {
	a = a.Unmap().WithZone("")
	return a.IsGlobalUnicast() && !ipv4ThisNetwork.Contains(a) && !ipv4Reserved.Contains(a)
}

// meshLocalAddr reports whether a is a Thread mesh-local address, which
// only the mesh reaches: inside the device's mesh-local prefix (the ML-EID,
// RLOC and ALOC; prefix invalid when the device did not report it), or —
// whatever the prefix — with the interface identifier 0000:00ff:fe00:xxxx
// Thread gives its routing and anycast locators (RLOC, ALOC).
func meshLocalAddr(a netip.Addr, prefix netip.Prefix) bool {
	if !a.Is6() || a.Is4In6() {
		return false
	}
	if prefix.IsValid() && prefix.Contains(a) {
		return true
	}
	b := a.As16()
	return b[8] == 0 && b[9] == 0 && b[10] == 0 && b[11] == 0xff && b[12] == 0xfe && b[13] == 0
}

// decodeIPv6Prefix decodes an ipv6pre: the prefix length in bits, then the
// prefix's bytes — as connectedhomeip encodes MeshLocalPrefix (one length
// byte, then the eight bytes of the prefix,
// src/app/clusters/thread-network-diagnostics-server/DirectThreadNetworkDiagnosticsProvider.cpp:167-174
// at connectedhomeip 6170af84). Invalid when malformed.
func decodeIPv6Prefix(b []byte) netip.Prefix {
	if len(b) < 1 || int(b[0]) > 128 || len(b)-1 < (int(b[0])+7)/8 || len(b)-1 > 16 {
		return netip.Prefix{}
	}
	var a [16]byte
	copy(a[:], b[1:])
	p, err := netip.AddrFrom16(a).Prefix(int(b[0]))
	if err != nil {
		return netip.Prefix{}
	}
	return p
}

// reconnectProbe is one Reconnect step's probing: run reads until stop.
type reconnectProbe struct {
	f      *flow
	ia     *interaction.Interaction
	hint   func([]netip.AddrPort)
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	last   *deviceNetworkReport // the last successful probe's report
	logged string               // the key of the last report logged
}

// startReconnectProbe starts probing the device over the commissioning
// interaction ia; stop ends it. Probing runs only while the session is
// open, and a probe that fails ends it: it never fails the step.
func (f *flow) startReconnectProbe(ctx context.Context, ia *interaction.Interaction) *reconnectProbe {
	pctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	p := &reconnectProbe{f: f, ia: ia, hint: f.hint, cancel: cancel, done: make(chan struct{})}
	// The first wait is armed before the transition starts.
	fired := make(chan struct{})
	first := f.c.ctx.Clock.AfterFunc(reconnectProbeFirst, func() { close(fired) })
	go p.run(pctx, fired, first)
	return p
}

// stop ends the probing, waits for it and returns the last report, nil
// when no probe succeeded (or p is nil). It may be called again.
func (p *reconnectProbe) stop() *deviceNetworkReport {
	if p == nil {
		return nil
	}
	p.cancel()
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func (p *reconnectProbe) run(ctx context.Context, first <-chan struct{}, firstTimer interface{ Stop() bool }) {
	defer close(p.done)
	select {
	case <-first:
	case <-ctx.Done():
		firstTimer.Stop()
		return
	}
	for p.once(ctx) {
		if !p.sleep(ctx, reconnectProbeInterval) {
			return
		}
	}
}

// sleep waits d on the flow's clock; false when the probing ended first.
func (p *reconnectProbe) sleep(ctx context.Context, d time.Duration) bool {
	fired := make(chan struct{})
	t := p.f.c.ctx.Clock.AfterFunc(d, func() { close(fired) })
	defer t.Stop()
	select {
	case <-fired:
		return true
	case <-ctx.Done():
		return false
	}
}

// once is one probe; false ends the probing.
func (p *reconnectProbe) once(probing context.Context) bool {
	if s := p.ia.Session(); s == nil || s.IsClosed() {
		p.f.log.Debug("matter.controller.commissioning.reconnect_probe_ended", slog.String("reason", "session closed"))
		return false
	}
	ctx, cancel := context.WithCancel(probing)
	defer cancel()
	t := p.f.c.ctx.Clock.AfterFunc(reconnectProbeTimeout, cancel)
	defer t.Stop()
	report, err := p.ia.Read(ctx, p.f.probePaths()...)
	if err != nil {
		if probing.Err() == nil {
			p.f.log.Debug("matter.controller.commissioning.reconnect_probe_ended", slog.Any("err", err))
		}
		return false
	}
	r := parseNetworkReport(report, sessionAddr(p.ia.Session()))
	p.mu.Lock()
	p.last = r
	changed := r.key() != p.logged
	p.logged = r.key()
	p.mu.Unlock()
	if changed {
		p.f.log.Info("matter.controller.commissioning.reconnect_probe", r.logAttrs()...)
		if r.interfacesOver > 0 {
			p.f.log.Warn("matter.controller.commissioning.reconnect_probe_interfaces_dropped",
				slog.Int("max", maxProbedInterfaces()), slog.Int("dropped", r.interfacesOver))
		}
	}
	if p.hint != nil && len(r.routable) > 0 {
		targets := make([]netip.AddrPort, len(r.routable))
		for i, a := range r.routable {
			targets[i] = netip.AddrPortFrom(a, operationalPort)
		}
		p.hint(targets)
	}
	return true
}

// sessionAddr is the address s runs on, without a zone; the zero Addr
// when it has none (a session over a commissioning channel that is not
// IP).
func sessionAddr(s *exchange.Session) netip.Addr {
	if s == nil {
		return netip.Addr{}
	}
	udp := s.Addr()
	if udp == nil {
		return netip.Addr{}
	}
	a, ok := netip.AddrFromSlice(udp.IP)
	if !ok {
		return netip.Addr{}
	}
	return a.Unmap()
}

// logAttrs are the report's log attributes, the ones it carries.
func (r *deviceNetworkReport) logAttrs() []any {
	var out []any
	if r.networkingStatus != nil {
		out = append(out, slog.String("networking_status", *r.networkingStatus))
	}
	if r.connectError != nil {
		out = append(out, slog.Int64("connect_error", int64(*r.connectError)))
	}
	if r.connected != nil {
		out = append(out, slog.Bool("connected", *r.connected))
	}
	if r.threadRole != nil {
		out = append(out, slog.String("role", *r.threadRole))
	}
	if r.networkName != nil {
		out = append(out, slog.String("network_name", *r.networkName))
	}
	if r.channel != nil {
		out = append(out, slog.Int("channel", int(*r.channel)))
	}
	if r.partitionID != nil {
		out = append(out, slog.Uint64("partition_id", uint64(*r.partitionID)))
	}
	if r.interfacesKnown {
		out = append(out, slog.String("addresses", addrList(r.routable)))
		if n := len(r.addresses) - len(r.routable); n > 0 {
			out = append(out, slog.Int("ignored", n))
		}
	}
	return out
}

// probePaths are the attributes one probe reads, in one read: the root
// NetworkCommissioning's state (the endpoint the NetworkCommissioning
// steps use), GeneralDiagnostics' NetworkInterfaces and the Thread
// diagnostics, each cluster only when the root's ServerList (step 0.1)
// names it or the list is unknown. Nine paths: a server has to accept
// nine per read (readTerms). A cluster or attribute the device lacks is
// a status in the report, not an error.
func (f *flow) probePaths() []im.ConcreteAttributePath {
	has := func(cluster uint32) bool {
		return f.data.rootServerList == nil || slices.Contains(f.data.rootServerList, cluster)
	}
	var paths []im.ConcreteAttributePath
	if has(netcomm.ClusterID) {
		paths = append(paths,
			rootAttr(netcomm.ClusterID, netcomm.AttrLastNetworkingStatus),
			rootAttr(netcomm.ClusterID, netcomm.AttrLastConnectErrorValue),
			rootAttr(netcomm.ClusterID, netcomm.AttrNetworks))
	}
	if has(gendiag.ClusterID) {
		paths = append(paths, rootAttr(gendiag.ClusterID, gendiag.AttrNetworkInterfaces))
	}
	if has(threaddiag.ClusterID) {
		paths = append(paths,
			rootAttr(threaddiag.ClusterID, threaddiag.AttrRoutingRole),
			rootAttr(threaddiag.ClusterID, threaddiag.AttrNetworkName),
			rootAttr(threaddiag.ClusterID, threaddiag.AttrChannel),
			rootAttr(threaddiag.ClusterID, threaddiag.AttrPartitionId),
			rootAttr(threaddiag.ClusterID, threaddiag.AttrMeshLocalPrefix))
	}
	return paths
}

// maxProbedInterfaces is how many interfaces the probe takes from one
// report: the list bound of GeneralDiagnostics NetworkInterfaces, read from
// the generated definition ("max 8"; matter.js packages/model/src/standard/
// elements/general-diagnostics.element.ts:27 at db021da0). Without a bound
// in the definition it takes none.
func maxProbedInterfaces() int {
	a := gendiag.Definition.Attribute(gendiag.AttrNetworkInterfaces)
	if a == nil || !a.Constraint.Max.IsInt() {
		return 0
	}
	return int(a.Constraint.Max.Int)
}

// addInterfaceList takes a NetworkInterfaces value (addInterfaces). The
// list itself is decoded whatever its length: connectedhomeip encodes
// every interface its platform lists
// (src/app/clusters/general-diagnostics-server/GeneralDiagnosticsCluster.cpp:437-446
// at 6170af84), so a device with more than the bound is not left out for
// it. An entry is decoded by the generated NetworkInterface, which holds
// its address lists to their bounds (8 IPv6, 4 IPv4, each of its exact
// length; connectedhomeip sizes them the same,
// src/include/platform/DiagnosticDataProvider.h:42-43): a value with an
// entry past them does not decode and is left out as a whole.
func (r *deviceNetworkReport) addInterfaceList(n spec.Node, proven netip.Addr) {
	if ifaces, err := spec.DecodeList(spec.DecodeStruct[gendiag.NetworkInterface])(n); err == nil {
		r.addInterfaces(ifaces, proven)
	}
}

// addInterfaces takes the addresses of at most maxProbedInterfaces
// operational interfaces, each address once, the routable ones marked.
// With the generated entry's own bounds that is at most
// peer.MaxHintedAddresses addresses per report: the report is the
// device's statement, and what the probe takes from it is logged, named
// in the step's failure and attempted.
//
// Which interfaces is judged before it is bounded: a device may list
// every interface of its platform, in the platform's order —
// connectedhomeip's Linux provider takes each link-layer entry of
// getifaddrs, none left out for its kind, operational when IFF_RUNNING
// (src/platform/Linux/DiagnosticDataProviderImpl.cpp:466-476 at 6170af84)
// — and the bound must not cost it the one it is reached on. The
// interfaces the connection can use — with an address that is routable
// and not mesh-local (usableAddr) — are taken first, in the device's
// order; the usable ones past the bound are counted (interfacesOver,
// which the probe logs) and not taken. Slots they leave go to the other
// operational interfaces, in the device's order, for what the log and
// the step's failure say of them (the ignored and the mesh-local
// addresses); nothing of those is attempted.
//
// Among usable interfaces nothing but the device's order tells one from
// another — every address is its statement — with one exception: proven,
// the address the commissioning session runs on (the zero Addr when it
// has none), is an address the device has answered at. The interface
// that names it is taken before any other, and it is the first routable
// address: the bound here does not drop it, and of this report's
// addresses it is the first to count against the connection's allowance
// (peer.MaxHintedAddresses, first come over a connection's probes) — an
// address the device merely states does not take its place. It counts
// only when it is usable by the test above: a link-local one is not, the
// report carrying no zone.
func (r *deviceNetworkReport) addInterfaces(ifaces []gendiag.NetworkInterface, proven netip.Addr) {
	r.interfacesKnown = true
	proven = proven.Unmap().WithZone("")
	if !r.usableAddr(proven) {
		proven = netip.Addr{}
	}
	// Judge: the interface that names the proven address, the usable
	// ones, the other operational ones.
	first := -1 // the interface that names proven
	var usable, others []int
	for i, iface := range ifaces {
		if !iface.IsOperational {
			continue
		}
		names, has := false, false
		for _, raw := range slices.Concat(iface.IPv6Addresses, iface.IPv4Addresses) {
			a, ok := netip.AddrFromSlice(raw)
			if a = a.Unmap(); !ok || !r.usableAddr(a) {
				continue
			}
			has = true
			names = names || a == proven
		}
		switch {
		case names && first < 0:
			first = i
		case has:
			usable = append(usable, i)
		default:
			others = append(others, i)
		}
	}
	if first >= 0 {
		usable = slices.Insert(usable, 0, first)
	}
	// Bound: the usable ones, then the others.
	limit := max(maxProbedInterfaces(), 0)
	kept := min(len(usable), limit)
	r.interfacesOver = len(usable) - kept
	take := slices.Concat(usable[:kept], others[:min(len(others), limit-kept)])
	// Take, in the device's order.
	slices.Sort(take)
	for _, i := range take {
		iface := ifaces[i]
		for _, raw := range slices.Concat(iface.IPv6Addresses, iface.IPv4Addresses) {
			a, ok := netip.AddrFromSlice(raw)
			if !ok {
				continue
			}
			a = a.Unmap()
			if slices.Contains(r.addresses, a) {
				continue
			}
			r.addresses = append(r.addresses, a)
			switch {
			case !routableAddr(a):
			case meshLocalAddr(a, r.meshLocal):
				r.meshLocalCount++
			default:
				r.routable = append(r.routable, a)
			}
		}
	}
	if i := slices.Index(r.routable, proven); i > 0 {
		r.routable = slices.Insert(slices.Delete(r.routable, i, i+1), 0, proven)
	}
}

// usableAddr reports whether the operational connection can use a probed
// address: routable (routableAddr) and not Thread mesh-local
// (meshLocalAddr, by the prefix the report carries).
func (r *deviceNetworkReport) usableAddr(a netip.Addr) bool {
	return routableAddr(a) && !meshLocalAddr(a, r.meshLocal)
}

// parseNetworkReport reads a probe's report. A value that does not decode
// is left out, as one the device did not report: the probe informs, it
// does not validate.
func parseNetworkReport(report *interaction.Report, proven netip.Addr) *deviceNetworkReport {
	r := &deviceNetworkReport{}
	value := func(cluster, attribute uint32) (spec.Node, bool) {
		return report.Value(rootEndpoint, cluster, attribute)
	}
	if n, ok := value(netcomm.ClusterID, netcomm.AttrLastNetworkingStatus); ok {
		if v, err := spec.DecodeNullable(spec.DecodeUint[uint8](8))(n); err == nil && !v.Null {
			name := enumName(netcomm.NetworkCommissioningStatusEnumDef, uint64(v.Value))
			r.networkingStatus = &name
		}
	}
	if n, ok := value(netcomm.ClusterID, netcomm.AttrLastConnectErrorValue); ok {
		if v, err := spec.DecodeNullable(spec.DecodeInt[int32](32))(n); err == nil && !v.Null {
			r.connectError = &v.Value
		}
	}
	if n, ok := value(netcomm.ClusterID, netcomm.AttrNetworks); ok {
		if nets, err := decodeNetworks(n); err == nil {
			connected := slices.ContainsFunc(nets, func(n netcomm.NetworkInfoStruct) bool { return n.Connected })
			r.connected = &connected
		}
	}
	if n, ok := value(threaddiag.ClusterID, threaddiag.AttrMeshLocalPrefix); ok {
		if v, err := spec.DecodeNullable(spec.DecodeBytes)(n); err == nil && !v.Null {
			r.meshLocal = decodeIPv6Prefix(v.Value)
		}
	}
	if n, ok := value(gendiag.ClusterID, gendiag.AttrNetworkInterfaces); ok {
		r.addInterfaceList(n, proven)
	}
	if n, ok := value(threaddiag.ClusterID, threaddiag.AttrRoutingRole); ok {
		if v, err := spec.DecodeNullable(spec.DecodeUint[uint8](8))(n); err == nil && !v.Null {
			name := enumName(threaddiag.RoutingRoleEnumDef, uint64(v.Value))
			r.threadRole = &name
		}
	}
	if n, ok := value(threaddiag.ClusterID, threaddiag.AttrNetworkName); ok {
		if v, err := spec.DecodeNullable(spec.DecodeString)(n); err == nil && !v.Null {
			r.networkName = &v.Value
		}
	}
	if n, ok := value(threaddiag.ClusterID, threaddiag.AttrChannel); ok {
		if v, err := spec.DecodeNullable(spec.DecodeUint[uint16](16))(n); err == nil && !v.Null {
			r.channel = &v.Value
		}
	}
	if n, ok := value(threaddiag.ClusterID, threaddiag.AttrPartitionId); ok {
		if v, err := spec.DecodeNullable(spec.DecodeUint[uint32](32))(n); err == nil && !v.Null {
			r.partitionID = &v.Value
		}
	}
	return r
}
