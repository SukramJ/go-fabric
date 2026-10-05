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
	"errors"
	"fmt"
	"sync"
	"testing"

	matteralarm "github.com/SukramJ/go-fabric/cluster/alarm"
	matterfan "github.com/SukramJ/go-fabric/cluster/fan"
	matterpump "github.com/SukramJ/go-fabric/cluster/pump"
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

// fanDevice is a host fan with a 0..4 speed range.
type fanDevice struct {
	mu      sync.Mutex
	st      matterfan.State
	applied []matterfan.Settings
}

func (d *fanDevice) FanState() matterfan.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *fanDevice) ApplyFanSettings(_ context.Context, s matterfan.Settings) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.applied = append(d.applied, s)
	if s.FanMode != nil {
		d.st.FanMode = *s.FanMode
	}
	if p := s.PercentSetting; p != nil {
		d.st.PercentSetting = nil
		if !p.Null {
			v := p.Value
			d.st.PercentSetting = &v
		}
	}
	if v := s.SpeedSetting; v != nil {
		d.st.SpeedSetting = nil
		if !v.Null {
			n := v.Value
			d.st.SpeedSetting = &n
		}
	}
	return nil
}

func TestFanControlOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &fanDevice{}
	srv, err := matterfan.NewServer(matterfan.Config{
		Source:   dev,
		Features: matterfan.FeatureMultiSpeed | matterfan.FeatureAuto | matterfan.FeatureStep,
		Sequence: matterfan.SequenceOffLowMedHighAuto, SpeedMax: 4,
		DeviceType: matterfan.DeviceTypeFan,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: matterfan.DeviceTypeFan, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[matterfan.DeviceTypeFan]

	// PercentSetting 50 → SpeedSetting ceil(4 × 0.5) = 2, read back.
	if st := h.writeAttribute(ep, matterfan.ClusterID, matterfan.AttrPercentSetting, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 50)
	}); st != 0 {
		t.Fatalf("PercentSetting write status 0x%02X", st)
	}
	if data, _, _ := h.readAttribute(ep, matterfan.ClusterID, matterfan.AttrSpeedSetting); data.El.Uint != 2 {
		t.Errorf("SpeedSetting after PercentSetting 50 = %d, want 2", data.El.Uint)
	}
	// A null PercentSetting write succeeds and changes nothing.
	n := len(dev.applied)
	if st := h.writeAttribute(ep, matterfan.ClusterID, matterfan.AttrPercentSetting, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutNull(tag)
	}); st != 0 || len(dev.applied) != n {
		t.Errorf("null PercentSetting write status 0x%02X, applied %d → %d", st, n, len(dev.applied))
	}
	// FanMode Auto → both settings null on the wire.
	if st := h.writeAttribute(ep, matterfan.ClusterID, matterfan.AttrFanMode, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, uint64(matterfan.FanModeAuto))
	}); st != 0 {
		t.Fatalf("FanMode write status 0x%02X", st)
	}
	if data, _, _ := h.readAttribute(ep, matterfan.ClusterID, matterfan.AttrPercentSetting); !data.El.IsNull {
		t.Errorf("PercentSetting in Auto = %+v, want null", data.El)
	}
	// Out-of-range writes.
	if st := h.writeAttribute(ep, matterfan.ClusterID, matterfan.AttrSpeedSetting, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 5)
	}); st != uint64(im.StatusConstraintError) {
		t.Errorf("SpeedSetting 5 > SpeedMax status 0x%02X", st)
	}
	if st := h.writeAttribute(ep, matterfan.ClusterID, matterfan.AttrSpeedMax, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, 3)
	}); st != uint64(im.StatusUnsupportedWrite) {
		t.Errorf("SpeedMax write status 0x%02X, want UnsupportedWrite", st)
	}

	// Step crosses the wire with its fields decoded: Decrease from speed
	// 0 with Wrap and LowestOff wraps to SpeedMax.
	dev.mu.Lock()
	dev.st.SpeedSetting = new(uint8)
	dev.mu.Unlock()
	if _, _, status, isStatus := invokeResult(t, h.invoke(ep, matterfan.ClusterID, matterfan.CmdStep, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(matterfan.StepDecrease))
		enc.PutBool(tlv.ContextTag(1), true)
	})); !isStatus || status != im.StatusSuccess {
		t.Fatalf("Step = %v", status)
	}
	if data, _, _ := h.readAttribute(ep, matterfan.ClusterID, matterfan.AttrSpeedSetting); data.El.Uint != 4 {
		t.Errorf("SpeedSetting after a wrapping Step down = %d, want 4", data.El.Uint)
	}
	// A Step without its mandatory Direction: InvalidCommand.
	if _, _, status, _ := invokeResult(t, h.invoke(ep, matterfan.ClusterID, matterfan.CmdStep, func(enc *tlv.Encoder) {
		enc.PutBool(tlv.ContextTag(1), true)
	})); status != im.StatusInvalidCommand {
		t.Errorf("Step without Direction = %v, want InvalidCommand", status)
	}
	// An undefined Direction: ConstraintError.
	if _, _, status, _ := invokeResult(t, h.invoke(ep, matterfan.ClusterID, matterfan.CmdStep, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 2)
	})); status != im.StatusConstraintError {
		t.Errorf("Step Direction 2 = %v, want ConstraintError", status)
	}
}

// TestFanStepDecoderRejectsMalformedFields covers the decoder's own
// rejections, which a controller can reach with any payload.
func TestFanStepDecoderRejectsMalformedFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		fields func(enc *tlv.Encoder)
		want   im.StatusCode
	}{
		{"direction not an integer", func(enc *tlv.Encoder) { enc.PutUTF8(tlv.ContextTag(0), "up") }, im.StatusInvalidCommand},
		{"direction wider than enum8", func(enc *tlv.Encoder) { enc.PutUint16(tlv.ContextTag(0), 0x100) }, im.StatusConstraintError},
		{"wrap not a bool", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 0)
			enc.PutUint(tlv.ContextTag(1), 1)
		}, im.StatusInvalidCommand},
		{"lowestOff a struct", func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 0)
			enc.StartStruct(tlv.ContextTag(2))
			_ = enc.EndContainer()
		}, im.StatusInvalidCommand},
	}
	for _, tc := range cases {
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		tc.fields(enc)
		_ = enc.EndContainer()
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		open, _ := dec.Next()
		_, err = commandFieldsReader(im.ConcreteCommandPath{Cluster: clusterwire.FanControlClusterID, Command: clusterwire.FanControlCmdStep}, dec, open)
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != tc.want {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// Unknown context and non-context fields are skipped.
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 1)
	enc.StartArray(tlv.ContextTag(9))
	_ = enc.EndContainer()
	enc.PutUint(tlv.FullyQualifiedTag(0xFFF1, 1, 1), 3)
	_ = enc.EndContainer()
	b, _ := enc.Bytes()
	dec := tlv.NewDecoder(b)
	open, _ := dec.Next()
	got, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: clusterwire.FanControlClusterID, Command: clusterwire.FanControlCmdStep}, dec, open)
	if err != nil || got != (clusterwire.FanStepRequest{Direction: 1, LowestOff: true}) {
		t.Errorf("Step with extra fields = %+v, %v", got, err)
	}
}

// pumpDevice is a host pump.
type pumpDevice struct {
	mu sync.Mutex
	st matterpump.State
}

func (d *pumpDevice) PumpState() matterpump.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *pumpDevice) SetOperationMode(_ context.Context, m matterpump.OperationMode) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.st.OperationMode = m
	return nil
}

// hostOnOff is the OnOff server the Pump device type mandates; OnOff
// servers are host-side in this module.
type hostOnOff struct{}

func (hostOnOff) MatterClusterID() uint32                                { return 0x0006 }
func (hostOnOff) MatterRead(attrID uint32) (any, bool)                   { return false, attrID == 0 }
func (hostOnOff) MatterWrite(context.Context, uint32, any) error         { return nil }
func (hostOnOff) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (hostOnOff) MatterReportable() []uint32                             { return []uint32{0} }

func TestPumpOverTheWire(t *testing.T) {
	t.Parallel()
	dev := &pumpDevice{}
	srv, err := matterpump.NewServer(matterpump.Config{
		Source:   dev,
		Features: matterpump.FeatureConstantSpeed,
		Limits:   matterpump.Limits{MaxSpeed: new(uint16)},
		Events:   []uint32{matterpump.EventDryRunning},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: matterpump.DeviceTypePump, servers: []contract.ClusterServer{hostOnOff{}, srv}})
	ep := h.endpoints[matterpump.DeviceTypePump]

	// OperationMode Maximum (SPD) lands; Local (no LOCAL) does not.
	if st := h.writeAttribute(ep, matterpump.ClusterID, matterpump.AttrOperationMode, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, uint64(matterpump.OperationMaximum))
	}); st != 0 {
		t.Fatalf("OperationMode write status 0x%02X", st)
	}
	if data, _, _ := h.readAttribute(ep, matterpump.ClusterID, matterpump.AttrOperationMode); data.El.Uint != uint64(matterpump.OperationMaximum) {
		t.Errorf("OperationMode read back %d", data.El.Uint)
	}
	if st := h.writeAttribute(ep, matterpump.ClusterID, matterpump.AttrOperationMode, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, uint64(matterpump.OperationLocal))
	}); st != uint64(im.StatusConstraintError) {
		t.Errorf("OperationMode Local status 0x%02X, want ConstraintError", st)
	}
	// Capacity unknown: null on the wire. MaxSpeed 0 as a uint16.
	if data, _, _ := h.readAttribute(ep, matterpump.ClusterID, matterpump.AttrCapacity); !data.El.IsNull {
		t.Errorf("Capacity = %+v, want null", data.El)
	}
	if data, _, _ := h.readAttribute(ep, matterpump.ClusterID, matterpump.AttrMaxSpeed); data.El.Type != tlv.TypeUnsignedInt2 {
		t.Errorf("MaxSpeed TLV type 0x%02X, want a two-byte unsigned integer", data.El.Type)
	}
	// The host raises DryRunning; it reads back as an empty structure.
	if err := srv.Emit(matterpump.EventDryRunning); err != nil {
		t.Fatal(err)
	}
	events := h.readEvents(ep, matterpump.ClusterID)
	if len(events) != 1 || events[0].id != uint64(matterpump.EventDryRunning) ||
		events[0].data.El.Type != tlv.TypeStructure || len(events[0].data.Children) != 0 {
		t.Errorf("events = %+v, want one DryRunning with an empty structure", events)
	}

	// OperationMode is "RW VM": an Operate-only subject is refused.
	if err := h.store.ReplaceACL(context.Background(), h.fabric, []store.ACLEntry{{
		FabricIndex: h.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{harnessControllerNodeID},
	}}); err != nil {
		t.Fatal(err)
	}
	if st := h.writeAttribute(ep, matterpump.ClusterID, matterpump.AttrOperationMode, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.PutUint(tag, uint64(matterpump.OperationNormal))
	}); st != uint64(im.StatusUnsupportedAccess) {
		t.Errorf("Operate-only OperationMode write status 0x%02X, want UnsupportedAccess", st)
	}
}
