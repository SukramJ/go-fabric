// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// The application cluster servers (cluster/alarm, cluster/fan,
// cluster/pump) over the real wire path: a bridge assembled from host
// devices, a CASE session into it, and every command, write and event
// those servers own crossing InvokeRequest / WriteRequest / ReadRequest
// encoding and decoding — commandFieldsReader, the attribute value
// reader, the dispatcher and the value writer — rather than a direct
// MatterInvoke call.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	matteralarm "github.com/SukramJ/go-fabric/cluster/alarm"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// appDevice is a bridged host device: a device type and the long-lived
// cluster servers it returns on every call.
type appDevice struct {
	deviceType uint16
	servers    []contract.ClusterServer
}

func (d appDevice) MatterDeviceType() uint16                       { return d.deviceType }
func (d appDevice) MatterClusterServers() []contract.ClusterServer { return d.servers }

// appHarness is a secure harness over a bridge of the given devices;
// endpoints maps each device type to the endpoint the assembler gave it.
type appHarness struct {
	*secureHarness
	endpoints map[uint16]uint16
}

func newAppHarness(t *testing.T, devices ...appDevice) *appHarness {
	t.Helper()
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		specs := make([]endpoint.Spec, len(devices))
		for i, d := range devices {
			specs[i] = endpoint.Spec{
				StableKey: endpoint.StringKey(fmt.Sprintf("dev-%d", i)), DeviceType: d.deviceType,
				FriendlyName: fmt.Sprintf("Device %d", i), Source: d,
			}
		}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: specs, ModelComplete: true}})
		}, nil
	})
	h.allowAll()
	ah := &appHarness{secureHarness: h, endpoints: map[uint16]uint16{}}
	for _, ep := range h.bridge.Topology().Bridged() {
		ah.endpoints[ep.DeviceType] = ep.ID
	}
	if len(ah.endpoints) != len(devices) {
		t.Fatalf("assembled %d endpoints, want %d", len(ah.endpoints), len(devices))
	}
	return ah
}

// readAttribute reads one concrete attribute and returns its data node,
// or the status when the report carries one.
func (h *secureHarness) readAttribute(endpointID uint16, cluster, attribute uint32) (data tlvNode, status im.StatusCode, isStatus bool) {
	h.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(0))
	enc.StartList(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(2), uint64(endpointID))
	enc.PutUint(tlv.ContextTag(3), uint64(cluster))
	enc.PutUint(tlv.ContextTag(4), uint64(attribute))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), true)
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		h.t.Fatal(err)
	}
	op, payload, ok := h.exchange(im.OpcodeReadRequest, body)
	if !ok || op != im.OpcodeReportData {
		h.t.Fatalf("Read answered opcode 0x%02X (ok=%v)", op, ok)
	}
	reports := decodeTLVTree(h.t, payload).mustChild(h.t, 1).Children
	if len(reports) != 1 {
		h.t.Fatalf("ReportData carries %d attribute reports, want 1", len(reports))
	}
	if st, ok := reports[0].child(0); ok {
		return tlvNode{}, im.StatusCode(st.mustChild(h.t, 1).mustChild(h.t, 0).El.Uint), true
	}
	return reports[0].mustChild(h.t, 1).mustChild(h.t, 2), 0, false
}

// readEvents reads every event of one cluster on one endpoint and
// returns (event id, data node) in event-number order.
func (h *secureHarness) readEvents(endpointID uint16, cluster uint32) []struct {
	id   uint64
	data tlvNode
} {
	h.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(1)) // EventRequests
	enc.StartList(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), uint64(endpointID))
	enc.PutUint(tlv.ContextTag(2), uint64(cluster))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), false)
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		h.t.Fatal(err)
	}
	op, payload, ok := h.exchange(im.OpcodeReadRequest, body)
	if !ok || op != im.OpcodeReportData {
		h.t.Fatalf("event Read answered opcode 0x%02X (ok=%v)", op, ok)
	}
	report := decodeTLVTree(h.t, payload)
	events, ok := report.child(2)
	if !ok {
		return nil
	}
	var out []struct {
		id   uint64
		data tlvNode
	}
	for _, ev := range events.Children {
		data := ev.mustChild(h.t, 1)
		path := data.mustChild(h.t, 0)
		out = append(out, struct {
			id   uint64
			data tlvNode
		}{path.mustChild(h.t, 3).El.Uint, data.mustChild(h.t, 7)})
	}
	return out
}

// smokeDevice is a host smoke/CO alarm.
type smokeDevice struct {
	mu        sync.Mutex
	st        matteralarm.State
	selfTests int
}

func (d *smokeDevice) SmokeCOState() matteralarm.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *smokeDevice) SelfTest(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.selfTests++
	d.st.TestInProgress = true
	return nil
}

func (d *smokeDevice) SetSmokeSensitivityLevel(_ context.Context, level matteralarm.Sensitivity) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.st.SmokeSensitivityLevel = level
	return nil
}

func TestSmokeCoAlarmOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &smokeDevice{st: matteralarm.State{SmokeSensitivityLevel: matteralarm.SensitivityStandard}}
	srv, err := matteralarm.NewServer(matteralarm.Config{
		Source:   dev,
		Features: matteralarm.FeatureSmokeAlarm | matteralarm.FeatureCOAlarm,
		Optional: matteralarm.OptionalSmokeSensitivityLevel,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: matteralarm.DeviceTypeSmokeCoAlarm, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[matteralarm.DeviceTypeSmokeCoAlarm]

	// SelfTestRequest: accepted, reaches the device, then Testing.
	if _, _, status, isStatus := invokeResult(t, h.invoke(ep, matteralarm.ClusterID, matteralarm.CmdSelfTestRequest, nil)); !isStatus || status != im.StatusSuccess {
		t.Fatalf("SelfTestRequest = %v (status=%v)", status, isStatus)
	}
	if dev.selfTests != 1 {
		t.Fatalf("device saw %d self-tests, want 1", dev.selfTests)
	}
	if data, _, _ := h.readAttribute(ep, matteralarm.ClusterID, matteralarm.AttrExpressedState); data.El.Uint != uint64(matteralarm.ExpressedTesting) {
		t.Errorf("ExpressedState over the wire = %d, want Testing", data.El.Uint)
	}
	// A second request while testing: Busy.
	if _, _, status, _ := invokeResult(t, h.invoke(ep, matteralarm.ClusterID, matteralarm.CmdSelfTestRequest, nil)); status != im.StatusBusy {
		t.Errorf("SelfTestRequest while testing = %v, want Busy", status)
	}

	// SmokeSensitivityLevel: a write lands, an out-of-enum one does not.
	if st := h.writeAttribute(ep, matteralarm.ClusterID, matteralarm.AttrSmokeSensitivityLevel, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, uint64(matteralarm.SensitivityHigh))
	}); st != 0 {
		t.Fatalf("SmokeSensitivityLevel write status 0x%02X", st)
	}
	if data, _, _ := h.readAttribute(ep, matteralarm.ClusterID, matteralarm.AttrSmokeSensitivityLevel); data.El.Uint != uint64(matteralarm.SensitivityHigh) {
		t.Errorf("SmokeSensitivityLevel read back %d", data.El.Uint)
	}
	if st := h.writeAttribute(ep, matteralarm.ClusterID, matteralarm.AttrSmokeSensitivityLevel, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 7)
	}); st != uint64(im.StatusConstraintError) {
		t.Errorf("out-of-enum write status 0x%02X, want ConstraintError", st)
	}
	if st := h.writeAttribute(ep, matteralarm.ClusterID, matteralarm.AttrExpressedState, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 0)
	}); st != uint64(im.StatusUnsupportedWrite) {
		t.Errorf("ExpressedState write status 0x%02X, want UnsupportedWrite", st)
	}

	// Events: the test ends and smoke is detected; the bridge's emitter
	// was wired into the host's instance at reassembly.
	dev.mu.Lock()
	dev.st.TestInProgress = false
	dev.st.SmokeState = matteralarm.AlarmCritical
	dev.mu.Unlock()
	srv.Refresh()
	events := h.readEvents(ep, matteralarm.ClusterID)
	var gotSmoke, gotComplete bool
	for _, ev := range events {
		switch uint32(ev.id) {
		case matteralarm.EventSmokeAlarm:
			gotSmoke = true
			if ev.data.El.Type != tlv.TypeStructure || ev.data.mustChild(t, 0).El.Uint != uint64(matteralarm.AlarmCritical) {
				t.Errorf("SmokeAlarm data = %+v, want {0: Critical}", ev.data)
			}
		case matteralarm.EventSelfTestComplete:
			gotComplete = true
			if ev.data.El.Type != tlv.TypeStructure || len(ev.data.Children) != 0 {
				t.Errorf("SelfTestComplete data = %+v, want an empty structure", ev.data)
			}
		}
	}
	if !gotSmoke || !gotComplete {
		t.Errorf("events read back %+v, want SmokeAlarm and SelfTestComplete", events)
	}
}

// TestApplicationValueWriterEncodesEventPayloads pins the two event
// payload shapes the value writer carries for the application clusters.
func TestApplicationValueWriterEncodesEventPayloads(t *testing.T) {
	t.Parallel()
	encode := func(v any) tlvNode {
		enc := tlv.NewEncoder()
		defaultAttributeValueWriter(enc, tlv.AnonymousTag(), im.AttributeValue{Value: v})
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return decodeTLVTree(t, b)
	}
	n := encode(clusterwire.FieldlessEvent{})
	if n.El.Type != tlv.TypeStructure || len(n.Children) != 0 {
		t.Errorf("FieldlessEvent = %+v, want an empty structure", n)
	}
	n = encode(matteralarm.AlarmSeverityEvent{AlarmSeverityLevel: matteralarm.AlarmWarning})
	if n.El.Type != tlv.TypeStructure || len(n.Children) != 1 || n.mustChild(t, 0).El.Uint != 1 {
		t.Errorf("AlarmSeverityEvent = %+v, want {0: 1}", n)
	}
}
