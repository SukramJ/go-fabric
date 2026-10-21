// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

func encode(t *testing.T, f func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	f(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tlvNode(f func(e *tlv.Encoder, tag tlv.Tag)) spec.Node {
	e := tlv.NewEncoder()
	f(e, tlv.AnonymousTag())
	b, _ := e.Bytes()
	n, _ := spec.ReadElement(tlv.NewDecoder(b))
	return n
}

func listN(vs ...uint64) spec.Node {
	return tlvNode(func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartArray(tag)
		for _, v := range vs {
			e.PutUint(tlv.AnonymousTag(), v)
		}
		_ = e.EndContainer()
	})
}

func attrPath(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// dataReport encodes a ReportData of values (each a spec.Node) and events.
func dataReport(t *testing.T, subscription uint32, reports []im.AttributeReport, events ...im.EventReport) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) {
		im.ReportData{SubscriptionID: subscription, HasSubscription: subscription != 0, Reports: reports, EventReports: events, SuppressResponse: true}.MarshalTLV(e,
			func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
				interaction.Raw(v.Value.(spec.Node)).EncodeTLV(e, tag)
			})
	})
}

func value(ep uint16, cluster, attr, version uint32, v spec.Node) im.AttributeReport {
	return im.AttributeReport{Path: attrPath(ep, cluster, attr), DataVersion: version, Value: im.AttributeValue{Value: v}}
}

// image is a node's report: a root with Descriptor and BasicInformation, a
// light on endpoint 1 with OnOff at version onOffVersion.
func image(onOffVersion uint32) []im.AttributeReport {
	dts := func(dt uint64) spec.Node {
		return tlvNode(func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartArray(tag)
			e.StartStruct(tlv.AnonymousTag())
			e.PutUint(tlv.ContextTag(0), dt)
			e.PutUint(tlv.ContextTag(1), 1)
			_ = e.EndContainer()
			_ = e.EndContainer()
		})
	}
	return []im.AttributeReport{
		value(0, descriptor.ClusterID, descriptor.AttrDeviceTypeList, 2, dts(0x0016)),
		value(0, descriptor.ClusterID, descriptor.AttrServerList, 2, listN(uint64(descriptor.ClusterID), uint64(basicinfo.ClusterID))),
		value(0, descriptor.ClusterID, descriptor.AttrPartsList, 2, listN(1)),
		value(0, descriptor.ClusterID, spec.AttrAttributeList, 2, listN(0, 1, 3, 0xFFFB)),
		value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel, 3, tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, "n") })),
		value(1, descriptor.ClusterID, descriptor.AttrDeviceTypeList, 4, dts(0x0100)),
		value(1, descriptor.ClusterID, descriptor.AttrServerList, 4, listN(uint64(descriptor.ClusterID), uint64(onoff.ClusterID))),
		value(1, descriptor.ClusterID, descriptor.AttrPartsList, 4, listN()),
		value(1, onoff.ClusterID, onoff.AttrOnOff, onOffVersion, tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, false) })),
	}
}

func startUp(number uint64) im.EventReport {
	return im.EventReport{
		Path:   im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: basicinfo.EventStartUp, HasEndpoint: true, HasCluster: true, HasEvent: true},
		Number: number, Priority: im.EventPriorityCritical, Timestamp: 1,
		Data: im.AttributeValue{Value: tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { basicinfo.StartUpEvent{SoftwareVersion: 3}.EncodeTLV(e, tag) })},
	}
}

func subscribeResponse(t *testing.T, id uint32) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) { im.SubscribeResponse{SubscriptionID: id, MaxInterval: 60}.MarshalTLV(e) })
}

func readRequestOf(t *testing.T, m msg) im.ReadRequest {
	t.Helper()
	if m.proto.Opcode != im.OpcodeReadRequest {
		t.Fatalf("opcode 0x%02X, want a ReadRequest", m.proto.Opcode)
	}
	r, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(m.payload))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func subscribeRequestOf(t *testing.T, m msg) im.SubscribeRequest {
	t.Helper()
	if m.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X, want a SubscribeRequest", m.proto.Opcode)
	}
	r, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(m.payload))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hasFilter(fs []im.DataVersionFilter, ep uint16, cluster, version uint32) bool {
	return slices.ContainsFunc(fs, func(f im.DataVersionFilter) bool {
		return f.Endpoint == ep && f.Cluster == cluster && f.DataVersion == version
	})
}

// hookLog records the node hooks of the network half.
type hookLog struct {
	mu     sync.Mutex
	states []client.ConnectionState
	events []any
	alive  int
	status []bool
	seeded int
	state  chan client.ConnectionState
}

func newHookLog() *hookLog { return &hookLog{state: make(chan client.ConnectionState, 32)} }

func (h *hookLog) hooks() client.Hooks {
	return client.Hooks{
		ConnectionStateChanged: func(s client.ConnectionState) {
			h.mu.Lock()
			h.states = append(h.states, s)
			h.mu.Unlock()
			h.state <- s
		},
		Event: func(_ uint16, _, _ uint32, _ interaction.EventData, v any) {
			h.mu.Lock()
			h.events = append(h.events, v)
			h.mu.Unlock()
		},
		SubscriptionAlive: func() {
			h.mu.Lock()
			h.alive++
			h.mu.Unlock()
		},
		SubscriptionStatusChanged: func(a bool) {
			h.mu.Lock()
			h.status = append(h.status, a)
			h.mu.Unlock()
		},
		Seeded: func() {
			h.mu.Lock()
			h.seeded++
			h.mu.Unlock()
		},
	}
}

func (h *hookLog) wait(t *testing.T, want client.ConnectionState) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-h.state:
			if s == want {
				return
			}
		case <-deadline:
			t.Fatalf("no transition to %v", want)
		}
	}
}

// TestDefaultSubscription: a newly commissioned node reads first
// (NetworkClient.ts:137-143) and then subscribes, sustained, with the
// versions the read brought as filters and the event minimum
// (:149-156, ClientNodeInteraction.ts:128); a re-subscription after the
// loss carries the refreshed filters and the event minimum past the
// highest event seen (ClientNodeInteraction.ts:148-155). The next node on
// the store bootstraps with a read carrying them (SustainedSubscription.ts:187-204).
func TestDefaultSubscription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &client.MemoryStore{}
	hl := newHookLog()
	sn := newScriptedNode(t, client.Config{ID: "peer1", Store: st, AutoSubscribe: true, NewlyCommissioned: true, Hooks: hl.hooks()})
	var mu sync.Mutex
	var observed []interaction.SubscribeRequest
	client.ObserveRequests(sn.node, func(r interaction.SubscribeRequest) {
		mu.Lock()
		observed = append(observed, r)
		mu.Unlock()
	})
	started := make(chan error, 1)
	go func() { started <- sn.node.Start(ctx) }()

	read := sn.next(t)
	rr := readRequestOf(t, read)
	if len(rr.DataVersionFilters) != 0 || len(rr.EventFilters) != 0 || len(rr.AttributeRequests) != 1 || rr.AttributeRequests[0].HasEndpoint ||
		len(rr.EventRequests) != 1 || !rr.EventRequests[0].IsUrgent || !rr.FabricFiltered {
		t.Fatalf("initial read %+v", rr)
	}
	sn.reply(t, read, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, image(5)))
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if !sn.node.Seeded() {
		t.Fatal("not seeded after the initial read")
	}

	sub := sn.next(t)
	sr := subscribeRequestOf(t, sub)
	if !hasFilter(sr.DataVersionFilters, 1, onoff.ClusterID, 5) || !hasFilter(sr.DataVersionFilters, 0, basicinfo.ClusterID, 3) ||
		len(sr.EventFilters) != 1 || sr.EventFilters[0].EventMin != 1 || sr.KeepSubscriptions || sr.MinIntervalFloor != 0 || sr.MaxIntervalCeiling != 60 {
		t.Fatalf("default subscription %+v", sr)
	}
	sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 33, []im.AttributeReport{value(1, onoff.ClusterID, onoff.AttrOnOff, 6, tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) }))}, startUp(7)))
	sn.reply(t, sub, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 33))
	hl.wait(t, client.Connected)
	if !sn.node.SubscriptionActive() {
		t.Fatal("not active")
	}
	if v, _ := sn.node.Endpoint(1).Cluster(onoff.ClusterID).Decoded(onoff.AttrOnOff); v != true {
		t.Fatal("the priming report was not applied")
	}
	hl.mu.Lock()
	if len(hl.events) != 1 || hl.events[0].(basicinfo.StartUpEvent).SoftwareVersion != 3 || hl.seeded != 1 || hl.alive == 0 || !slices.Equal(hl.status, []bool{true}) {
		t.Fatalf("events %v seeded %d alive %d status %v", hl.events, hl.seeded, hl.alive, hl.status)
	}
	hl.mu.Unlock()

	// The underlying subscription is lost: the replacement is refreshed.
	sn.reg.CloseForPeer(sn.reg.Active()[0].Peer())
	hl.wait(t, client.Reconnecting)
	again := sn.next(t)
	ar := subscribeRequestOf(t, again)
	if !hasFilter(ar.DataVersionFilters, 1, onoff.ClusterID, 6) || len(ar.EventFilters) != 1 || ar.EventFilters[0].EventMin != 8 {
		t.Fatalf("re-subscription %+v", ar)
	}
	sn.reply(t, again, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 34, nil))
	sn.reply(t, again, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 34))
	hl.wait(t, client.Connected)
	mu.Lock()
	if len(observed) != 2 || len(observed[1].EventFilters) != 1 || observed[1].EventFilters[0].EventMin != 8 {
		t.Fatalf("observed requests %d", len(observed))
	}
	mu.Unlock()
	if err := sn.node.Close(ctx); err != nil {
		t.Fatal(err)
	}
	hl.wait(t, client.Disconnected)

	// The next node on the store: autoSubscribe persisted, a bootstrap read
	// with the cached versions and the event minimum first.
	hl2 := newHookLog()
	sn2 := newScriptedNode(t, client.Config{ID: "peer1", Store: st, Hooks: hl2.hooks()})
	if err := sn2.node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	boot := sn2.next(t)
	br := readRequestOf(t, boot)
	if !hasFilter(br.DataVersionFilters, 1, onoff.ClusterID, 6) || len(br.EventFilters) != 1 || br.EventFilters[0].EventMin != 8 {
		t.Fatalf("bootstrap read %+v", br)
	}
	sn2.reply(t, boot, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, []im.AttributeReport{value(1, onoff.ClusterID, onoff.AttrOnOff, 9, tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, false) }))}))
	after := subscribeRequestOf(t, sn2.next(t))
	if !hasFilter(after.DataVersionFilters, 1, onoff.ClusterID, 9) || after.EventFilters[0].EventMin != 8 {
		t.Fatalf("subscription after the bootstrap read %+v", after)
	}
}

// TestIncludeKnownVersions: a read carries the cached versions unless
// IncludeKnownVersions (ClientNodeInteraction.ts:103-108), and not when
// its fabric filter differs from the default subscription's.
func TestIncludeKnownVersions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sn := newScriptedNode(t, client.Config{ID: "peer1"})
	wildcard := interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}}
	read := func(req client.ReadRequest) im.ReadRequest {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := sn.node.Read(ctx, req)
			done <- err
		}()
		m := sn.next(t)
		rr := readRequestOf(t, m)
		sn.reply(t, m, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, image(5)))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		return rr
	}
	if rr := read(client.ReadRequest{ReadRequest: wildcard}); len(rr.DataVersionFilters) != 0 {
		t.Fatal("filters on an empty image")
	}
	if rr := read(client.ReadRequest{ReadRequest: wildcard}); !hasFilter(rr.DataVersionFilters, 1, onoff.ClusterID, 5) {
		t.Fatalf("no filters %+v", rr.DataVersionFilters)
	}
	if rr := read(client.ReadRequest{ReadRequest: wildcard, IncludeKnownVersions: true}); len(rr.DataVersionFilters) != 0 {
		t.Fatal("IncludeKnownVersions sent filters")
	}
	unfiltered := wildcard
	unfiltered.NoFabricFilter = true
	if rr := read(client.ReadRequest{ReadRequest: unfiltered}); len(rr.DataVersionFilters) != 0 {
		t.Fatal("an unfiltered read sent filters")
	}
	// Errors pass through.
	ctxDone, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := sn.node.Read(ctxDone, client.ReadRequest{ReadRequest: wildcard}); err == nil {
		t.Fatal("a cancelled read succeeded")
	}
}

// TestConnectionStateTable is NetworkClient.ts:223-235.
func TestConnectionStateTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		active, session, offline, latch bool
		want                            client.ConnectionState
	}{
		{true, true, false, false, client.Connected},
		{true, true, true, true, client.Connected},
		{true, false, false, false, client.Reconnecting},
		{false, true, false, false, client.Reconnecting},
		{false, true, true, true, client.Disconnected},
		{true, false, true, false, client.Disconnected},
		{false, false, false, true, client.WaitingForDeviceDiscovery},
		{true, false, false, true, client.WaitingForDeviceDiscovery},
	} {
		if got := client.ConnectionStateFor(tc.active, tc.session, tc.offline, tc.latch); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

// TestWriteAndInvokeForward: Write and Invoke go to the peer
// (ClientNodeInteraction.ts:175-186, :197-210); the image is not touched.
func TestWriteAndInvokeForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sn := newScriptedNode(t, client.Config{ID: "peer1"})
	path := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	wrote := make(chan error, 1)
	go func() {
		_, err := sn.node.Write(ctx, []interaction.WriteRequest{{Path: path, Value: interaction.Raw(tlvNode(func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, "x") }))}}, interaction.WriteOptions{})
		wrote <- err
	}()
	w := sn.next(t)
	if w.proto.Opcode != im.OpcodeWriteRequest {
		t.Fatalf("opcode 0x%02X", w.proto.Opcode)
	}
	sn.reply(t, w, im.InteractionModelProtocolID, im.OpcodeWriteResponse, encode(t, func(e *tlv.Encoder) {
		im.WriteResponse{Responses: []im.AttributeStatus{{Path: path, Status: im.StatusIB{Status: im.StatusSuccess}}}}.MarshalTLV(e)
	}))
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
	invoked := make(chan error, 1)
	go func() {
		_, err := sn.node.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 1, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{})
		invoked <- err
	}()
	inv := sn.next(t)
	if inv.proto.Opcode != im.OpcodeInvokeRequest {
		t.Fatalf("opcode 0x%02X", inv.proto.Opcode)
	}
	sn.reply(t, inv, im.InteractionModelProtocolID, im.OpcodeStatusResponse, encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: im.StatusUnsupportedCommand}.MarshalTLV(e) }))
	if err := <-invoked; err == nil {
		t.Fatal("the refused invoke succeeded")
	}
	if len(sn.node.Endpoints()) != 1 {
		t.Fatal("the image changed")
	}
}
