// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for five C4 survivors first registered as equivalents and
// reclassed test gaps by the Fable read of w2-b2b (phase 8 wave 2b,
// #602, decision 692): subscribe_dispatch.go :241 (the second && → ||),
// :483, :683, :761 and ble.go:442.

package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// logBufferW2B2b is a log sink safe for the bridge's goroutines.
type logBufferW2B2b struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBufferW2B2b) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines returns the logged lines that contain every one of parts.
func (l *logBufferW2B2b) lines(parts ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all && line != "" {
			out = append(out, line)
		}
	}
	return out
}

// loggedBridgeW2B2b is a started bridge logging at debug level into the
// returned buffer.
func loggedBridgeW2B2b(t *testing.T) (*Bridge, *logBufferW2B2b) {
	t.Helper()
	logs := &logBufferW2B2b{}
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"},
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	return b, logs
}

// TestRefusedSubscribeIsLoggedAtWarnW2B2b: a Subscribe the manager
// refuses for another reason than the fabric quota is logged
// matter.rx.im.subscribe.manager at WARN, the level the CHANGELOG
// publishes for a refused request. Kills subscribe_dispatch.go:483
// (&& → ||).
func TestRefusedSubscribeIsLoggedAtWarnW2B2b(t *testing.T) {
	t.Parallel()
	b, logs := loggedBridgeW2B2b(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	mgr.SetReportSender(nil) // every registration fails: ErrNoReportSender
	c, err := subscribeOnConnW2B2b(t, b, 0x5600, im.SubscribeRequest{AttributeRequests: noMatchPathsW2B2b(1), MaxIntervalCeiling: 60})
	if err != nil {
		t.Fatalf("handleSubscribeRequest: %v", err)
	}
	if got := onlyStatusW2B2b(t, c); got != im.StatusFailure {
		t.Fatalf("answered %v, want Failure (the manager's refusal)", got)
	}
	if got := logs.lines("level=WARN", "msg=matter.rx.im.subscribe.manager"); len(got) != 1 {
		t.Fatalf("%d WARN lines matter.rx.im.subscribe.manager, want 1; manager lines: %q", len(got), logs.lines("matter.rx.im.subscribe.manager"))
	}
}

// TestStopBLEWithNothingToStopLogsNothingW2B2b: StopBLE on a bridge whose
// radio never started does not log matter.ble.stopped — the line the
// chip-tool BLE-Wi-Fi leg takes as proof the node left the radio
// (internal/chiptool/blewifi_test.go). Kills ble.go:442 (guard off).
func TestStopBLEWithNothingToStopLogsNothingW2B2b(t *testing.T) {
	t.Parallel()
	b, logs := loggedBridgeW2B2b(t)
	if err := b.StopBLE(context.Background()); err != nil {
		t.Fatalf("StopBLE: %v", err)
	}
	if got := logs.lines("matter.ble.stopped"); len(got) != 0 {
		t.Fatalf("StopBLE with nothing to stop logged %q", got)
	}
}

// versionedStatusDispatcherW2B2b answers every read with a failure that
// carries a DataVersion — a host dispatcher may: im.Dispatcher does not
// restrict HasDataVersion to success.
type versionedStatusDispatcherW2B2b struct{ im.Dispatcher }

func (versionedStatusDispatcherW2B2b) Read(_ context.Context, path im.ConcreteAttributePath) []im.ReadResult {
	return []im.ReadResult{{Path: path, Status: im.StatusFailure, DataVersion: 42, HasDataVersion: true}}
}

// TestPrimingReportsAStatusWhateverItsVersionW2B2b: a status result is
// reported even when it carries the DataVersion its cluster's filter
// names; only a value at the filter's version is left out. Kills
// subscribe_dispatch.go:241 (the second && → ||). Defensive: the
// TopologyDispatcher sets a version on success results only.
func TestPrimingReportsAStatusWhateverItsVersionW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	path := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 1, HasCluster: true, Cluster: 6, HasAttribute: true, Attribute: 0}
	src := b.newPrimingSource(im.WithAuthModePASE(context.Background()), versionedStatusDispatcherW2B2b{}, im.SubscribeRequest{
		AttributeRequests:  []im.ConcreteAttributePath{path},
		DataVersionFilters: []im.DataVersionFilter{{Endpoint: 1, Cluster: 6, DataVersion: 42}},
	})
	rep, ok := src.next()
	if !ok || !rep.IsStatus || rep.Status.Status != im.StatusFailure {
		t.Fatalf("first report = %+v (ok=%v), want the Failure status", rep, ok)
	}
}

// openContainersW2B2b is a host attribute value whose EncodeTLV leaves
// two structures open: the report it is in cannot be encoded
// (tlv.Encoder.Bytes refuses an open container).
type openContainersW2B2b struct{}

func (openContainersW2B2b) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.StartStruct(tlv.ContextTag(0))
}

// valuesServerW2B2b serves fixed attribute values.
type valuesServerW2B2b struct {
	id     uint32
	values map[uint32]any
}

func (s valuesServerW2B2b) MatterClusterID() uint32 { return s.id }

func (s valuesServerW2B2b) MatterRead(attrID uint32) (any, bool) {
	v, ok := s.values[attrID]
	return v, ok
}

func (valuesServerW2B2b) MatterWrite(context.Context, uint32, any) error         { return nil }
func (valuesServerW2B2b) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (valuesServerW2B2b) MatterReportable() []uint32                             { return nil }

// udpLikeConnW2B2b is a connection with a UDP peer address — the priming
// report is cut at the UDP budget — that answers every send with the
// peer's StatusResponse(Success) on exchange.
type udpLikeConnW2B2b struct {
	fakeConn
	b        *Bridge
	exchange uint16
}

func (*udpLikeConnW2B2b) Remote() net.Addr { return &net.UDPAddr{IP: net.IPv6loopback, Port: 5540} }

func (c *udpLikeConnW2B2b) Send(msg []byte) error {
	_ = c.fakeConn.Send(msg)
	c.b.signalStatusResponseRX(paseTestSessionID, c.exchange, false, im.StatusSuccess)
	return nil
}

func (c *udpLikeConnW2B2b) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// TestPrimingReportThatCannotBeEncodedIsNoSubscriptionW2B2b: a priming
// report one of whose values cannot be encoded — in the first chunk or in
// a later one — fails the Subscribe; no subscription is established and
// no SubscribeResponse goes out. Kills subscribe_dispatch.go:683 and
// :761 (guard off (err)).
func TestPrimingReportThatCannotBeEncodedIsNoSubscriptionW2B2b(t *testing.T) {
	t.Parallel()
	const cluster = uint32(0xFFF6)
	big := strings.Repeat("x", 900)
	srv := valuesServerW2B2b{id: cluster, values: map[uint32]any{0: big, 1: big, 2: openContainersW2B2b{}}}
	src := notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}, servers: []contract.ClusterServer{srv}}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b := newStartedBridgeWithSnapshotter(t, func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: []endpoint.Spec{{StableKey: endpoint.StringKey("v"), DeviceType: src.MatterDeviceType(), FriendlyName: "V", Source: src}}}})
	})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)
	ep := b.Topology().Bridged()[0].ID
	attr := func(a uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{HasEndpoint: true, Endpoint: ep, HasCluster: true, Cluster: cluster, HasAttribute: true, Attribute: a}
	}
	for i, tc := range []struct {
		paths  []im.ConcreteAttributePath
		chunks int // priming chunks sent before the failure
	}{
		{[]im.ConcreteAttributePath{attr(2)}, 0},                   // the first chunk fails (:683)
		{[]im.ConcreteAttributePath{attr(0), attr(1), attr(2)}, 1}, // the first goes out, the next fails (:761)
	} {
		exchange := uint16(0x5700 + i)
		c := &udpLikeConnW2B2b{b: b, exchange: exchange}
		hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: uint32(300 + i)}
		proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: exchange, Initiator: true}
		err := b.handleSubscribeRequest(context.Background(), &connRoute{conn: c}, hdr, proto, im.SubscribeRequest{AttributeRequests: tc.paths, MaxIntervalCeiling: 60})
		if err == nil {
			t.Errorf("case %d: a priming report that cannot be encoded returned no error", i)
		}
		if n := mgr.Active(); n != 0 {
			t.Errorf("case %d: mgr.Active() = %d, want 0", i, n)
		}
		if got := c.sentCount(); got != tc.chunks {
			t.Errorf("case %d: %d datagrams sent, want %d priming chunk(s) and no SubscribeResponse", i, got, tc.chunks)
		}
	}
}
