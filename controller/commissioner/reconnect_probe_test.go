// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gendiag "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiag "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// probeState is what a probedDevice reports on one read: nil fields are
// not reported (the attribute is absent).
type probeState struct {
	status     *netcomm.NetworkCommissioningStatusEnum
	connected  bool
	role       *threaddiag.RoutingRoleEnum
	name       string
	channel    uint16
	interfaces []gendiag.NetworkInterface
	// meshPrefix is MeshLocalPrefix's ipv6pre bytes, nil for null.
	meshPrefix []byte
	// unsupported are attributes answered with UNSUPPORTED_ATTRIBUTE.
	unsupported []uint32
}

// probedDevice answers the reconnect probe's reads with its states in
// turn, the last one repeated; silent answers no read.
type probedDevice struct {
	silent bool
	states []probeState

	mu    sync.Mutex
	reads int
	paths []im.ConcreteAttributePath
}

func (d *probedDevice) readCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

func (d *probedDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	if req.opcode != im.OpcodeReadRequest {
		return 0, nil, false
	}
	d.mu.Lock()
	n := d.reads
	d.reads++
	if r, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload)); err == nil {
		d.paths = append(d.paths, r.AttributeRequests...)
	}
	d.mu.Unlock()
	if d.silent {
		return 0, nil, false
	}
	st := d.states[min(n, len(d.states)-1)]
	type value struct {
		cluster, attribute uint32
		v                  spec.Encodable
	}
	values := []value{
		{netcomm.ClusterID, netcomm.AttrLastConnectErrorValue, termsValue(func(e *tlv.Encoder, tag tlv.Tag) { e.PutNull(tag) })},
		{netcomm.ClusterID, netcomm.AttrNetworks, spec.List[netcomm.NetworkInfoStruct]{{NetworkId: []byte("net"), Connected: st.connected}}},
		{gendiag.ClusterID, gendiag.AttrNetworkInterfaces, spec.List[gendiag.NetworkInterface](st.interfaces)},
		{threaddiag.ClusterID, threaddiag.AttrNetworkName, termsValue(func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, st.name) })},
		{threaddiag.ClusterID, threaddiag.AttrChannel, uintValue(st.channel)},
	}
	if st.status != nil {
		values = append(values, value{netcomm.ClusterID, netcomm.AttrLastNetworkingStatus, uintValue(*st.status)})
	}
	if st.role != nil {
		values = append(values, value{threaddiag.ClusterID, threaddiag.AttrRoutingRole, uintValue(*st.role)})
	}
	values = append(values, value{threaddiag.ClusterID, threaddiag.AttrMeshLocalPrefix, termsValue(func(e *tlv.Encoder, tag tlv.Tag) {
		if st.meshPrefix == nil {
			e.PutNull(tag)
			return
		}
		e.PutOctets(tag, st.meshPrefix)
	})})
	return im.OpcodeReportData, encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		for _, v := range values {
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(0), 1)
			im.ConcreteAttributePath{Cluster: v.cluster, Attribute: v.attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
			v.v.EncodeTLV(e, tlv.ContextTag(2))
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		for _, a := range st.unsupported {
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(0))
			im.ConcreteAttributePath{Cluster: threaddiag.ClusterID, Attribute: a, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(0))
			im.StatusIB{Status: im.StatusUnsupportedAttribute}.MarshalTLV(e, tlv.ContextTag(1))
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		_ = e.EndContainer()
	}), true
}

// probeLog records the reconnect probe's Info lines.
type probeLog struct {
	mu      sync.Mutex
	records []map[string]string
}

func (l *probeLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *probeLog) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "matter.controller.commissioning.reconnect_probe") {
		return nil
	}
	m := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, m)
	return nil
}

func (l *probeLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *probeLog) WithGroup(string) slog.Handler      { return l }

// all are the report lines.
func (l *probeLog) all() []map[string]string {
	return l.named("matter.controller.commissioning.reconnect_probe")
}

// named are the lines of msg.
func (l *probeLog) named(msg string) []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]string
	for _, m := range l.records {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// probeRig is a flow at its Reconnect step over a probedDevice: the
// transition blocks until release, the hints are recorded.
type probeRig struct {
	f     *flow
	clock *fakeClock
	log   *probeLog
	dev   *probedDevice

	release chan error
	result  chan error
	entered chan struct{}

	mu    sync.Mutex
	hints [][]netip.AddrPort
}

func newProbeRig(t *testing.T, dev *probedDevice, concurrent bool) *probeRig {
	t.Helper()
	r := &probeRig{dev: dev, log: &probeLog{}, release: make(chan error, 1), result: make(chan error, 1), entered: make(chan struct{})}
	r.f, r.clock = newTestFlow(t, dev.handle)
	r.f.log = slog.New(r.log)
	r.f.data.supportsConcurrentConnection = &concurrent
	// A failsafe long enough: the step arms nothing.
	r.f.setFailSafeEnd(r.clock.Now().Add(time.Hour))
	r.f.transition = func(context.Context, handshake.PeerAddress, bool) (*interaction.Interaction, error) {
		close(r.entered)
		if err := <-r.release; err != nil {
			return nil, err
		}
		return r.f.interaction(), nil
	}
	r.f.hint = func(addrs []netip.AddrPort) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.hints = append(r.hints, slices.Clone(addrs))
	}
	go func() {
		_, err := r.f.reconnectWithDevice(context.Background())
		r.result <- err
	}()
	return r
}

func (r *probeRig) hinted() [][]netip.AddrPort {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.hints)
}

// armed waits until a timer of d is armed on the fake clock.
func (r *probeRig) armed(t *testing.T, d time.Duration) {
	t.Helper()
	eventually(t, "a timer of "+d.String(), func() bool { return slices.Contains(r.clock.Armed(), d) })
}

// probe runs one probe: the next probe's wait, then the read answered and
// the probe asleep again.
func (r *probeRig) probe(t *testing.T, wait time.Duration) {
	t.Helper()
	r.armed(t, wait)
	r.clock.Advance(wait)
	r.armed(t, reconnectProbeInterval)
}

func (r *probeRig) finish(t *testing.T, err error) error {
	t.Helper()
	r.release <- err
	var got error
	eventually(t, "the Reconnect step to return", func() bool {
		select {
		case got = <-r.result:
			return true
		default:
			return false
		}
	})
	return got
}

func ptr[T any](v T) *T { return &v }

func iface(typ gendiag.InterfaceTypeEnum, operational bool, addrs ...string) gendiag.NetworkInterface {
	n := gendiag.NetworkInterface{
		Name: "if", IsOperational: operational, HardwareAddress: make([]byte, 8), Type: typ,
		IPv4Addresses: [][]byte{}, IPv6Addresses: [][]byte{},
	}
	for _, a := range addrs {
		ip := netip.MustParseAddr(a)
		if ip.Is4() {
			b := ip.As4()
			n.IPv4Addresses = append(n.IPv4Addresses, b[:])
		} else {
			b := ip.As16()
			n.IPv6Addresses = append(n.IPv6Addresses, b[:])
		}
	}
	return n
}

// TestReconnectProbeHintsRoutableAddresses: a concurrent device is probed
// over the commissioning session once the transition ran
// reconnectProbeFirst, then every reconnectProbeInterval, in one read of
// nine root paths. The probe logs at Info when the report changed, not on
// every probe; the operational interfaces' routable addresses — not the
// link-local one, not a non-operational interface's — are handed to the
// connection at port 5540 on every probe. The step succeeds with the
// transition, and the probing ends with it.
func TestReconnectProbeHintsRoutableAddresses(t *testing.T) {
	t.Parallel()
	detached := probeState{
		status: ptr(netcomm.NetworkCommissioningStatusSuccess), role: ptr(threaddiag.RoutingRoleUnassigned), name: "lab", channel: 15,
		interfaces: []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeThread, true, "fe80::1")},
	}
	attached := detached
	attached.connected, attached.role = true, ptr(threaddiag.RoutingRoleEndDevice)
	attached.interfaces = []gendiag.NetworkInterface{
		iface(gendiag.InterfaceTypeThread, true, "fe80::1", "fd11::5", "2001:db8::5"),
		iface(gendiag.InterfaceTypeWiFi, false, "fd22::9"),
	}
	dev := &probedDevice{states: []probeState{detached, detached, attached, attached}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	r.probe(t, reconnectProbeInterval)
	if got := r.log.all(); len(got) != 1 {
		t.Fatalf("after two equal reports %d log lines, want 1: %v", len(got), got)
	}
	if h := r.hinted(); len(h) != 0 {
		t.Fatalf("a link-local-only report hinted %v", h)
	}
	r.probe(t, reconnectProbeInterval)
	r.probe(t, reconnectProbeInterval)
	logs := r.log.all()
	if len(logs) != 2 {
		t.Fatalf("%d log lines, want one per change: %v", len(logs), logs)
	}
	first, second := logs[0], logs[1]
	if first["level"] != "INFO" || first["role"] != "Unassigned" || first["networking_status"] != "Success" || first["connected"] != "false" ||
		first["network_name"] != "lab" || first["channel"] != "15" || first["addresses"] != "[]" || first["ignored"] != "1" {
		t.Fatalf("first line %v", first)
	}
	if second["role"] != "EndDevice" || second["connected"] != "true" || second["addresses"] != "[fd11::5, 2001:db8::5]" || second["ignored"] != "1" {
		t.Fatalf("second line %v", second)
	}
	want := []netip.AddrPort{netip.MustParseAddrPort("[fd11::5]:5540"), netip.MustParseAddrPort("[2001:db8::5]:5540")}
	h := r.hinted()
	if len(h) != 2 || !slices.Equal(h[0], want) || !slices.Equal(h[1], want) {
		t.Fatalf("hints %v, want %v on each of the two attached probes", h, want)
	}
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
	reads := dev.readCount()
	r.clock.Advance(time.Hour)
	if dev.readCount() != reads || reads != 4 {
		t.Fatalf("reads %d then %d: the probing outlived the transition", reads, dev.readCount())
	}
	dev.mu.Lock()
	paths := slices.Clone(dev.paths[:9])
	dev.mu.Unlock()
	for _, p := range paths {
		if !p.HasEndpoint || p.Endpoint != rootEndpoint || !p.HasCluster || !p.HasAttribute {
			t.Fatalf("a probe path is not a concrete root path: %+v", p)
		}
	}
}

// interfacesReport is a probe's report whose NetworkInterfaces is ifaces,
// through the wire encoding and the probe's own parser.
func interfacesReport(t *testing.T, ifaces []gendiag.NetworkInterface) *deviceNetworkReport {
	t.Helper()
	b := encodeTLV(func(e *tlv.Encoder) { spec.List[gendiag.NetworkInterface](ifaces).EncodeTLV(e, tlv.AnonymousTag()) })
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	r := &deviceNetworkReport{}
	r.addInterfaceList(n)
	return r
}

// TestReconnectProbeBoundsReportedAddresses (review II, R2-P0-bound-E01):
// the probe takes what one NetworkInterfaces attribute may hold and no
// more — 8 interfaces of 8 IPv6 and 4 IPv4 addresses, the numbers of the
// generated definition, which peer.MaxHintedAddresses multiplies out. A
// device that lists more interfaces (connectedhomeip encodes its whole
// interface list, GeneralDiagnosticsCluster.cpp:437-446 at 6170af84) has
// its first 8 operational ones taken; an interface with more addresses
// than its lists allow does not decode, and the attribute is left out.
func TestReconnectProbeBoundsReportedAddresses(t *testing.T) {
	t.Parallel()
	def := gendiag.Definition.Attribute(gendiag.AttrNetworkInterfaces)
	listMax := func(name string) int {
		for _, f := range gendiag.NetworkInterfaceDef.Fields {
			if f.Name == name && f.Constraint.Max.IsInt() {
				return int(f.Constraint.Max.Int)
			}
		}
		t.Fatalf("the generated NetworkInterface has no list bound for %s", name)
		return 0
	}
	if def == nil || !def.Constraint.Max.IsInt() {
		t.Fatal("the generated NetworkInterfaces has no list bound")
	}
	v4, v6 := listMax("IPv4Addresses"), listMax("IPv6Addresses")
	if maxProbedInterfaces() != 8 || v4 != 4 || v6 != 8 {
		t.Fatalf("bounds %d interfaces, %d IPv4, %d IPv6; want 8, 4, 8", maxProbedInterfaces(), v4, v6)
	}
	if got := maxProbedInterfaces() * (v4 + v6); got != peer.MaxHintedAddresses {
		t.Fatalf("peer.MaxHintedAddresses is %d, the definition allows %d", peer.MaxHintedAddresses, got)
	}

	// 12 interfaces, each full: the 2nd is not operational, so the first
	// 8 operational ones are 1 and 3 to 9.
	full := func(i int, operational bool) gendiag.NetworkInterface {
		var addrs []string
		for j := range v6 {
			addrs = append(addrs, fmt.Sprintf("fd%02x::%x", i, j+1))
		}
		for j := range v4 {
			addrs = append(addrs, fmt.Sprintf("10.%d.0.%d", i, j+1))
		}
		return iface(gendiag.InterfaceTypeWiFi, operational, addrs...)
	}
	var ifaces []gendiag.NetworkInterface
	for i := 1; i <= 12; i++ {
		ifaces = append(ifaces, full(i, i != 2))
	}
	r := interfacesReport(t, ifaces)
	if !r.interfacesKnown || len(r.addresses) != peer.MaxHintedAddresses || len(r.routable) != peer.MaxHintedAddresses || r.interfacesOver != 3 {
		t.Fatalf("%d addresses, %d routable, %d interfaces passed over; want %d, %d and 3",
			len(r.addresses), len(r.routable), r.interfacesOver, peer.MaxHintedAddresses, peer.MaxHintedAddresses)
	}
	if slices.Contains(r.addresses, netip.MustParseAddr("fd02::1")) || !slices.Contains(r.addresses, netip.MustParseAddr("fd09::1")) ||
		slices.Contains(r.addresses, netip.MustParseAddr("fd0a::1")) {
		t.Fatalf("not the first 8 operational interfaces: %v", r.addresses)
	}

	// One address more than a list allows: the attribute does not decode.
	over := full(1, true)
	extra := netip.MustParseAddr("fd01::ffff").As16()
	over.IPv6Addresses = append(over.IPv6Addresses, extra[:])
	if r := interfacesReport(t, []gendiag.NetworkInterface{over}); r.interfacesKnown || len(r.addresses) != 0 {
		t.Fatalf("an interface with %d IPv6 addresses was taken: %v", len(over.IPv6Addresses), r.addresses)
	}
}

// TestReconnectProbeRoutableIsUnicast (review II, R2-P0-bound-E01): what
// is not a unicast address of another host is not handed to the
// connection — beside link-local, loopback, unspecified and multicast,
// the IPv4 limited broadcast with the reserved range around it and IPv4
// "this network".
func TestReconnectProbeRoutableIsUnicast(t *testing.T) {
	t.Parallel()
	r := interfacesReport(t, []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeWiFi, true,
		"fe80::1", "::1", "::", "ff02::1", "fd11::5",
		"255.255.255.255", "240.0.0.1", "0.1.2.3", "192.0.2.7")})
	want := []netip.Addr{netip.MustParseAddr("fd11::5"), netip.MustParseAddr("192.0.2.7")}
	if !slices.Equal(r.routable, want) {
		t.Fatalf("routable %v, want %v", r.routable, want)
	}
}

// TestRoutableAddr holds routableAddr's classes one by one (review II,
// item 5): net/netip's where it has them, RFC 1122 §3.2.1.3 for 0.0.0.0/8
// and RFC 1112 §4 for 240.0.0.0/4.
func TestRoutableAddr(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		addr string
		want bool
		by   string
	}{
		{"0.0.0.0", false, "netip: unspecified"},
		{"::", false, "netip: unspecified"},
		{"224.0.0.251", false, "netip: multicast"},
		{"ff02::1", false, "netip: multicast"},
		{"255.255.255.255", false, "netip: limited broadcast"},
		{"127.0.0.1", false, "netip: loopback"},
		{"::1", false, "netip: loopback"},
		{"169.254.1.2", false, "netip: link-local"},
		{"fe80::1", false, "netip: link-local"},
		{"fe80::1%eth0", false, "netip: link-local"},
		{"0.1.2.3", false, "RFC 1122 §3.2.1.3 (b)"},
		{"0.255.255.255", false, "RFC 1122 §3.2.1.3 (b)"},
		{"240.0.0.1", false, "RFC 1112 §4"},
		{"255.255.255.254", false, "RFC 1112 §4"},
		{"::ffff:0.1.2.3", false, "RFC 1122 §3.2.1.3 (b), IPv4-mapped"},
		{"::ffff:240.0.0.1", false, "RFC 1112 §4, IPv4-mapped"},
		{"::ffff:127.0.0.1", false, "netip: loopback, IPv4-mapped"},
		{"1.0.0.0", true, "above 0.0.0.0/8"},
		{"223.255.255.255", true, "below 224.0.0.0/4"},
		{"192.0.2.7", true, "unicast"},
		{"10.1.2.3", true, "unicast (private)"},
		{"fd11::5", true, "unicast (unique local)"},
		{"2001:db8::1", true, "unicast"},
		{"::ffff:192.0.2.7", true, "unicast (IPv4-mapped)"},
	} {
		if got := routableAddr(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("routableAddr(%s) = %v, want %v (%s)", tc.addr, got, tc.want, tc.by)
		}
	}
	if routableAddr(netip.Addr{}) {
		t.Error("the zero Addr is routable")
	}
}

// TestReconnectProbeFailureNamesReport: a device that reports it is not
// attached and never becomes reachable fails the step with
// ErrOperativeConnectionFailed, its message naming the last report.
func TestReconnectProbeFailureNamesReport(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{
		status: ptr(netcomm.NetworkCommissioningStatusNetworkNotFound), role: ptr(threaddiag.RoutingRoleUnassigned), name: "lab", channel: 15,
		interfaces: []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeThread, true, "fe80::1")},
	}}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	err := r.finish(t, errors.New("peer: unreachable for 4m15s"))
	var ce *CommissioningError
	if !errors.Is(err, ErrOperativeConnectionFailed) || !errors.As(err, &ce) {
		t.Fatalf("error %v", err)
	}
	const want = `commissioner: Operative reconnection with device failed (the device reports Thread role=Unassigned on "lab" channel 15, last networking status=NetworkNotFound, network connected=false, no routable address): peer: unreachable for 4m15s`
	if err.Error() != want {
		t.Fatalf("error\n%s\nwant\n%s", err, want)
	}
	if h := r.hinted(); len(h) != 0 {
		t.Fatalf("hints %v", h)
	}
}

// TestReconnectProbeFailureNamesAddresses: the failure names the routable
// addresses the device reported and the connection did not reach.
func TestReconnectProbeFailureNamesAddresses(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{
		status: ptr(netcomm.NetworkCommissioningStatusSuccess), connected: true, role: ptr(threaddiag.RoutingRoleEndDevice), name: "lab", channel: 15,
		interfaces: []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeThread, true, "fd11::5")},
	}}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	err := r.finish(t, errors.New("peer: unreachable"))
	if !errors.Is(err, ErrOperativeConnectionFailed) ||
		!strings.Contains(err.Error(), `(the device reports Thread role=EndDevice on "lab" channel 15, last networking status=Success, network connected=true, addresses [fd11::5]; not reachable at them)`) {
		t.Fatalf("error %v", err)
	}
}

// TestReconnectProbeSilentDevice: a device that answers no probe read
// leaves the step as it was — the read ends at reconnectProbeTimeout,
// the probing with it (no further probe), and the failure is the
// transition's alone.
func TestReconnectProbeSilentDevice(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{silent: true}
	r := newProbeRig(t, dev, true)
	r.armed(t, reconnectProbeFirst)
	r.clock.Advance(reconnectProbeFirst)
	r.armed(t, reconnectProbeTimeout)
	eventually(t, "the read", func() bool { return dev.readCount() > 0 })
	r.clock.Advance(reconnectProbeTimeout)
	eventually(t, "the probing to end", func() bool {
		return len(r.log.named("matter.controller.commissioning.reconnect_probe_ended")) == 1
	})
	if ended := r.log.named("matter.controller.commissioning.reconnect_probe_ended")[0]; ended["level"] != "DEBUG" || ended["err"] == "" {
		t.Fatalf("end line %v", ended)
	}
	if slices.Contains(r.clock.Armed(), reconnectProbeInterval) {
		t.Fatal("a probe follows the one that timed out")
	}
	err := r.finish(t, errors.New("peer: unreachable"))
	if err == nil || err.Error() != "commissioner: Operative reconnection with device failed: peer: unreachable" {
		t.Fatalf("error %v", err)
	}
	if len(r.log.all()) != 0 || len(r.hinted()) != 0 {
		t.Fatal("a silent device was logged or hinted")
	}
}

// TestReconnectProbeSessionClosed: with the commissioning session closed
// the probe sends nothing, and the failure is the transition's alone.
func TestReconnectProbeSessionClosed(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{}}}
	r := newProbeRig(t, dev, true)
	r.f.interaction().Session().Close()
	r.armed(t, reconnectProbeFirst)
	r.clock.Advance(reconnectProbeFirst)
	eventually(t, "the probing to end", func() bool {
		return len(r.log.named("matter.controller.commissioning.reconnect_probe_ended")) == 1
	})
	err := r.finish(t, errors.New("peer: unreachable"))
	if dev.readCount() != 0 || err.Error() != "commissioner: Operative reconnection with device failed: peer: unreachable" {
		t.Fatalf("reads %d, error %v", dev.readCount(), err)
	}
}

// TestReconnectProbeNotConcurrent: a device without concurrent connection
// is not probed: its commissioning session closed with the transition.
func TestReconnectProbeNotConcurrent(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{}}}
	r := newProbeRig(t, dev, false)
	<-r.entered
	if slices.Contains(r.clock.Armed(), reconnectProbeFirst) {
		t.Fatal("a probe is armed for a non-concurrent device")
	}
	r.clock.Advance(reconnectProbeFirst + 3*reconnectProbeInterval)
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
	if dev.readCount() != 0 {
		t.Fatalf("%d probe reads of a non-concurrent device", dev.readCount())
	}
}

// TestReconnectProbePathsFollowServerList: the probe reads a diagnostics
// cluster only when the root's ServerList names it.
func TestReconnectProbePathsFollowServerList(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, nil)
	if n := len(f.probePaths()); n != 9 {
		t.Fatalf("%d paths without a ServerList, want 9", n)
	}
	f.data.rootServerList = []uint32{netcomm.ClusterID, gendiag.ClusterID}
	paths := f.probePaths()
	if len(paths) != 4 || slices.ContainsFunc(paths, func(p im.ConcreteAttributePath) bool { return p.Cluster == threaddiag.ClusterID }) {
		t.Fatalf("paths %v", paths)
	}
}

// TestReconnectProbeArmedWithTransition: a concurrent device's first probe
// is armed by the time the transition runs.
func TestReconnectProbeArmedWithTransition(t *testing.T) {
	t.Parallel()
	r := newProbeRig(t, &probedDevice{states: []probeState{{}}}, true)
	<-r.entered
	if !slices.Contains(r.clock.Armed(), reconnectProbeFirst) {
		t.Fatal("no probe armed when the transition started")
	}
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestReconnectProbeMeshLocalOmitted: the mesh-local addresses — inside
// the reported MeshLocalPrefix (the ML-EID) or, whatever the prefix, with
// the RLOC/ALOC interface identifier 0000:00ff:fe00:xxxx — are not hinted,
// not logged as addresses and not named as unreachable; the failure counts
// them.
func TestReconnectProbeMeshLocalOmitted(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{
		status: ptr(netcomm.NetworkCommissioningStatusSuccess), connected: true, role: ptr(threaddiag.RoutingRoleEndDevice), name: "lab", channel: 15,
		meshPrefix: []byte{64, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00},
		interfaces: []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeThread, true,
			"fe80::1", "fdde:ad00:beef:0:1234:5678:9abc:def0", "fdde:ad00:beef:0:0:ff:fe00:4c01", "fd11::5")},
	}}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	want := []netip.AddrPort{netip.MustParseAddrPort("[fd11::5]:5540")}
	if h := r.hinted(); len(h) != 1 || !slices.Equal(h[0], want) {
		t.Fatalf("hints %v, want %v", h, want)
	}
	if l := r.log.all(); len(l) != 1 || l[0]["addresses"] != "[fd11::5]" || l[0]["ignored"] != "3" {
		t.Fatalf("log %v", l)
	}
	err := r.finish(t, errors.New("peer: unreachable"))
	if !strings.Contains(err.Error(), "addresses [fd11::5]; not reachable at them, 2 mesh-local addresses omitted)") {
		t.Fatalf("error %v", err)
	}
}

// TestReconnectProbeRLOCWithoutPrefix: without a MeshLocalPrefix (null)
// only the RLOC/ALOC rule applies — the ML-EID passes as a routable ULA.
func TestReconnectProbeRLOCWithoutPrefix(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{
		role: ptr(threaddiag.RoutingRoleEndDevice),
		interfaces: []gendiag.NetworkInterface{iface(gendiag.InterfaceTypeThread, true,
			"fdde:ad00:beef:0:1234:5678:9abc:def0", "fdde:ad00:beef:0:0:ff:fe00:fc00")},
	}}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	want := []netip.AddrPort{netip.MustParseAddrPort("[fdde:ad00:beef:0:1234:5678:9abc:def0]:5540")}
	if h := r.hinted(); len(h) != 1 || !slices.Equal(h[0], want) {
		t.Fatalf("hints %v, want %v", h, want)
	}
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestDecodeIPv6Prefix: an ipv6pre is the length in bits, then the
// prefix's bytes; a malformed one is invalid.
func TestDecodeIPv6Prefix(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   []byte
		want string
	}{
		{[]byte{64, 0xfd, 0xde, 0xad, 0, 0xbe, 0xef, 0, 0}, "fdde:ad00:beef::/64"},
		{[]byte{48, 0xfd, 0xde, 0xad, 0, 0xbe, 0xef}, "fdde:ad00:beef::/48"},
		{[]byte{64, 0xfd}, "invalid Prefix"},
		{[]byte{129}, "invalid Prefix"},
		{nil, "invalid Prefix"},
	} {
		if got := decodeIPv6Prefix(c.in).String(); got != c.want {
			t.Errorf("decodeIPv6Prefix(%x) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestReconnectProbeUnsupportedAttribute: an attribute answered with a
// status (UNSUPPORTED_ATTRIBUTE) is not reported, and the rest of the
// probe stands.
func TestReconnectProbeUnsupportedAttribute(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{states: []probeState{{
		status: ptr(netcomm.NetworkCommissioningStatusNetworkNotFound), name: "lab", channel: 15,
		interfaces: []gendiag.NetworkInterface{}, unsupported: []uint32{threaddiag.AttrRoutingRole},
	}}}
	r := newProbeRig(t, dev, true)
	r.probe(t, reconnectProbeFirst)
	l := r.log.all()
	if len(l) != 1 || l[0]["network_name"] != "lab" || l[0]["networking_status"] != "NetworkNotFound" {
		t.Fatalf("log %v", l)
	}
	dev.mu.Lock()
	n := dev.reads
	dev.mu.Unlock()
	if _, ok := l[0]["role"]; ok || n != 1 {
		t.Fatalf("an unsupported attribute was reported: %v", l[0])
	}
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestReconnectProbeReadInFlightAtSuccess: a transition that succeeds
// while a probe read is in flight returns at once — no clock advance — and
// the read is cancelled: its timeout timer is gone and no end is logged.
func TestReconnectProbeReadInFlightAtSuccess(t *testing.T) {
	t.Parallel()
	dev := &probedDevice{silent: true}
	r := newProbeRig(t, dev, true)
	r.armed(t, reconnectProbeFirst)
	r.clock.Advance(reconnectProbeFirst)
	r.armed(t, reconnectProbeTimeout)
	eventually(t, "the read", func() bool { return dev.readCount() > 0 })
	if err := r.finish(t, nil); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.clock.Armed(), reconnectProbeTimeout) || slices.Contains(r.clock.Armed(), reconnectProbeInterval) {
		t.Fatalf("probe timers left armed: %v", r.clock.Armed())
	}
	if n := len(r.log.named("matter.controller.commissioning.reconnect_probe_ended")); n != 0 {
		t.Fatalf("%d end lines for a cancelled read", n)
	}
}
