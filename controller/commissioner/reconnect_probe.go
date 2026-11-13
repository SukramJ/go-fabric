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
// bounded by what the attribute may hold (addInterfaces) and filtered to
// addresses of one other host (routableAddr), and the connection takes at
// most peer.MaxHintedAddresses of them (peer.Peer.HintAddresses).
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
	// interfacesOver counts the operational interfaces past the
	// attribute's list bound, which are not taken (addInterfaces).
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

// routableAddr reports whether a probed address can carry the operational
// connection: a link-local address has no zone the controller could use
// across the border router, and the others name no peer — loopback, the
// unspecified address, multicast, and in IPv4 "this network" (0.0.0.0/8)
// and the reserved 240.0.0.0/4 with the limited broadcast, which
// peer.Peer.HintAddresses passes over as well. A Thread mesh-local address
// is routable by this test but not from off the mesh (meshLocalAddr).
func routableAddr(a netip.Addr) bool {
	if !a.IsValid() || a.IsLinkLocalUnicast() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() {
		return false
	}
	if a.Is4() {
		first := a.As4()[0]
		return first != 0 && first < 240
	}
	return true
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
	r := parseNetworkReport(report)
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
func (r *deviceNetworkReport) addInterfaceList(n spec.Node) {
	if ifaces, err := spec.DecodeList(spec.DecodeStruct[gendiag.NetworkInterface])(n); err == nil {
		r.addInterfaces(ifaces)
	}
}

// addInterfaces takes the addresses of the operational interfaces, each
// once, the routable ones marked — of the first maxProbedInterfaces
// operational interfaces, in the device's order; the ones after them are
// counted (interfacesOver, which the probe logs) and not taken. With the
// generated entry's own bounds that is at most peer.MaxHintedAddresses
// addresses per report: the report is the device's statement, and what
// the probe takes from it is logged, named in the step's failure and
// attempted.
func (r *deviceNetworkReport) addInterfaces(ifaces []gendiag.NetworkInterface) {
	r.interfacesKnown = true
	limit, taken := maxProbedInterfaces(), 0
	for _, iface := range ifaces {
		if !iface.IsOperational {
			continue
		}
		if taken >= limit {
			r.interfacesOver++
			continue
		}
		taken++
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
}

// parseNetworkReport reads a probe's report. A value that does not decode
// is left out, as one the device did not report: the probe informs, it
// does not validate.
func parseNetworkReport(report *interaction.Report) *deviceNetworkReport {
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
		r.addInterfaceList(n)
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
