// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
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

func (d *probedDevice) handle(req imRequest) (uint8, []byte, bool) {
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

	mu    sync.Mutex
	hints [][]netip.AddrPort
}

func newProbeRig(t *testing.T, dev *probedDevice, concurrent bool) *probeRig {
	t.Helper()
	r := &probeRig{dev: dev, log: &probeLog{}, release: make(chan error, 1), result: make(chan error, 1)}
	r.f, r.clock = newTestFlow(t, dev.handle)
	r.f.log = slog.New(r.log)
	r.f.data.supportsConcurrentConnection = &concurrent
	// A failsafe long enough: the step arms nothing.
	r.f.setFailSafeEnd(r.clock.Now().Add(time.Hour))
	r.f.transition = func(context.Context, handshake.PeerAddress, bool) (*interaction.Interaction, error) {
		if err := <-r.release; err != nil {
			return nil, err
		}
		return r.f.interaction(), nil
	}
	r.f.hint = func(addrs []netip.AddrPort) int {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.hints = append(r.hints, slices.Clone(addrs))
		return len(addrs)
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
	select {
	case got := <-r.result:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("the Reconnect step did not return")
		return nil
	}
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
// eight root paths. The probe logs at Info when the report changed, not on
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
		first["network_name"] != "lab" || first["channel"] != "15" || first["addresses"] != "[fe80::1]" {
		t.Fatalf("first line %v", first)
	}
	if second["role"] != "EndDevice" || second["connected"] != "true" || second["addresses"] != "[fe80::1, fd11::5, 2001:db8::5]" {
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
	paths := slices.Clone(dev.paths[:8])
	dev.mu.Unlock()
	for _, p := range paths {
		if !p.HasEndpoint || p.Endpoint != rootEndpoint || !p.HasCluster || !p.HasAttribute {
			t.Fatalf("a probe path is not a concrete root path: %+v", p)
		}
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
	if n := len(f.probePaths()); n != 8 {
		t.Fatalf("%d paths without a ServerList, want 8", n)
	}
	f.data.rootServerList = []uint32{netcomm.ClusterID, gendiag.ClusterID}
	paths := f.probePaths()
	if len(paths) != 4 || slices.ContainsFunc(paths, func(p im.ConcreteAttributePath) bool { return p.Cluster == threaddiag.ClusterID }) {
		t.Fatalf("paths %v", paths)
	}
}
