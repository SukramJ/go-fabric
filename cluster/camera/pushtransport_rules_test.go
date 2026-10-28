// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// PushAvStreamTransport rules no chip case reaches, each against the
// connectedhomeip line it checks (PushAVStreamTransportLogic.cpp at
// 6170af84; the stream and zone answers are the camera app's,
// push-av-stream-manager.cpp).
package camera_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

func pushInvoke(p *camera.PushTransport, cmd uint32, req any) (any, error) {
	return p.MatterInvoke(peer(1, 0x55), cmd, req)
}

func wantPushCluster(t *testing.T, what string, err error, code padef.StatusCodeEnum) {
	t.Helper()
	if got, ok := pushCluster(err); !ok || got != uint8(code) {
		t.Fatalf("%s: %v, want cluster status %d", what, err, code)
	}
}

func TestPushConfig(t *testing.T) {
	if _, err := camera.NewPushTransport(camera.PushTransportConfig{}); !errors.Is(err, camera.ErrNoPushTransportPort) {
		t.Fatal(err)
	}
}

func TestPushOptionRules(t *testing.T) {
	str := func(n int) string { return strings.Repeat("s", n) }
	cases := []struct {
		name     string
		features camera.PushFeature
		edit     func(*padef.TransportOptionsStruct)
		want     im.StatusCode
	}{
		{"usage", 1, func(o *padef.TransportOptionsStruct) { o.StreamUsage = 4 }, im.StatusConstraintError}, // :391-392
		{"video name", 1, func(o *padef.TransportOptionsStruct) {
			o.VideoStreams = &[]padef.VideoStreamStruct{{VideoStreamName: str(17)}}
		}, im.StatusConstraintError}, // :413-415
		{"audio name", 1, func(o *padef.TransportOptionsStruct) {
			o.AudioStreams = &[]padef.AudioStreamStruct{{AudioStreamName: ""}}
		}, im.StatusConstraintError}, // :441-443
		{"TLS endpoint id", 1, func(o *padef.TransportOptionsStruct) { o.TlsEndpointId = 65535 }, im.StatusConstraintError},                                         // :451
		{"short URL", 1, func(o *padef.TransportOptionsStruct) { o.Url = "https://a/" }, im.StatusConstraintError},                                                  // :456-457
		{"trigger type", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.TriggerType = 3 }, im.StatusConstraintError},                                   // :465-466
		{"zone without sensitivity", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MotionZones.Value[0].Sensitivity = nil }, im.StatusInvalidCommand}, // :486-487
		{"zone sensitivity 11", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MotionZones.Value[0].Sensitivity = u8(11) }, im.StatusConstraintError},  // :491-493
		{"zone sensitivity without PERZONESENS", 0, func(o *padef.TransportOptionsStruct) {}, im.StatusInvalidCommand},                                              // :500
		{"no time control", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MotionTimeControl = nil }, im.StatusInvalidCommand},                         // :516
		{"InitialDuration 0", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MotionTimeControl.InitialDuration = 0 }, im.StatusConstraintError},        // :521-522
		{"MaxDuration 0", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MotionTimeControl.MaxDuration = 0 }, im.StatusConstraintError},                // :528-529
		{"zones on a command trigger", 1, func(o *padef.TransportOptionsStruct) {
			o.TriggerOptions.TriggerType = padef.TransportTriggerTypeCommand
		}, im.StatusInvalidCommand}, // :545
		{"time control on a command trigger", 1, func(o *padef.TransportOptionsStruct) {
			o.TriggerOptions.TriggerType, o.TriggerOptions.MotionZones = padef.TransportTriggerTypeCommand, nil
		}, im.StatusInvalidCommand}, // :550
		{"no motion sensitivity without PERZONESENS", 0, func(o *padef.TransportOptionsStruct) { zonesNoSens(o) }, im.StatusInvalidCommand}, // :558-559
		{"motion sensitivity 11", 0, func(o *padef.TransportOptionsStruct) {
			zonesNoSens(o)
			v := spec.ValueOf[uint8](11)
			o.TriggerOptions.MotionSensitivity = &v
		}, im.StatusConstraintError}, // :564-566
		{"motion sensitivity with PERZONESENS", 1, func(o *padef.TransportOptionsStruct) {
			v := spec.ValueOf[uint8](3)
			o.TriggerOptions.MotionSensitivity = &v
		}, im.StatusInvalidCommand}, // :573-574
		{"no pre-roll", 1, func(o *padef.TransportOptionsStruct) { o.TriggerOptions.MaxPreRollLen = nil }, im.StatusInvalidCommand}, // :584
		{"pre-roll on a continuous trigger", 1, func(o *padef.TransportOptionsStruct) {
			o.TriggerOptions = padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous, MaxPreRollLen: u16(1)}
		}, im.StatusInvalidCommand}, // :591-592
		{"ingest method", 1, func(o *padef.TransportOptionsStruct) { o.IngestMethod = 1 }, im.StatusConstraintError},                                              // :600-601
		{"container type", 1, func(o *padef.TransportOptionsStruct) { o.ContainerOptions.ContainerType = 1 }, im.StatusConstraintError},                           // :606-607
		{"no CMAF options", 1, func(o *padef.TransportOptionsStruct) { o.ContainerOptions.CmafContainerOptions = nil }, im.StatusInvalidCommand},                  // :612
		{"CMAF interface", 1, func(o *padef.TransportOptionsStruct) { o.ContainerOptions.CmafContainerOptions.CmafInterface = 3 }, im.StatusConstraintError},      // :617-622
		{"segment 65501", 1, func(o *padef.TransportOptionsStruct) { o.ContainerOptions.CmafContainerOptions.SegmentDuration = 65501 }, im.StatusConstraintError}, // :627-630
		{"chunk above half", 1, func(o *padef.TransportOptionsStruct) { o.ContainerOptions.CmafContainerOptions.ChunkDuration = 501 }, im.StatusConstraintError},  // :638
		{"metadata without METADATA", 1, func(o *padef.TransportOptionsStruct) {
			b := true
			o.ContainerOptions.CmafContainerOptions.MetadataEnabled = &b
		}, im.StatusInvalidCommand}, // :644-646
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Features = tc.features })
			o := motionOptions()
			tc.edit(&o)
			_, err := pushInvoke(p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
			wantCode(t, tc.name, statusOf(err), tc.want)
			_, err = pushInvoke(p, padef.CmdModifyPushTransport, padef.ModifyPushTransportRequest{ConnectionId: 1, TransportOptions: o})
			wantCode(t, tc.name+" (modify)", statusOf(err), tc.want) // :1290-1297
		})
	}
}

// zonesNoSens strips the per-zone sensitivities.
func zonesNoSens(o *padef.TransportOptionsStruct) {
	for i := range o.TriggerOptions.MotionZones.Value {
		o.TriggerOptions.MotionZones.Value[i].Sensitivity = nil
	}
}

func TestPushAllocateRules(t *testing.T) {
	alloc := func(p *camera.PushTransport, o padef.TransportOptionsStruct) error {
		_, err := pushInvoke(p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		return err
	}
	p, m := newPush(t)
	for range 4 {
		allocatePush(t, p, motionOptions())
	}
	wantCode(t, "capacity", statusOf(alloc(p, motionOptions())), im.StatusResourceExhausted) // :889-905
	if _, err := p.MatterInvoke(peer(2, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: motionOptions()}); err != nil {
		t.Fatalf("the capacity is per fabric: %v", err)
	}

	q, _ := newPush(t, func(c *camera.PushTransportConfig) { c.TLSEndpoints = nil })
	wantPushCluster(t, "no TLS client management", alloc(q, motionOptions()), padef.StatusCodeInvalidTlsEndpoint) // :966-972
	q, qm := newPush(t, func(c *camera.PushTransportConfig) { c.TLSCertificates = nil })
	if err := alloc(q, motionOptions()); err != nil || qm.credentials[0].RootCertificate != nil { // :951-954
		t.Fatalf("without certificates: %v %+v", err, qm.credentials)
	}
	o := motionOptions()
	o.Url = "http://example.com/stream/"
	q, _ = newPush(t)
	wantPushCluster(t, "URL", alloc(q, o), padef.StatusCodeInvalidUrl) // :998-1012

	q, _ = newPush(t, func(c *camera.PushTransportConfig) { s := pushStreams(); s.supported = nil; c.Streams = s })
	wantPushCluster(t, "usage", alloc(q, motionOptions()), padef.StatusCodeInvalidStreamUsage) // :1015-1022

	zones := func(z ...padef.TransportZoneOptionsStruct) padef.TransportOptionsStruct {
		o := motionOptions()
		v := spec.ValueOf(z)
		o.TriggerOptions.MotionZones = &v
		return o
	}
	zone := func(id uint16) padef.TransportZoneOptionsStruct {
		return padef.TransportZoneOptionsStruct{Zone: spec.ValueOf(id), Sensitivity: u8(3)}
	}
	null := padef.TransportZoneOptionsStruct{Zone: spec.NullOf[uint16](), Sensitivity: u8(3)}
	q, _ = newPush(t)
	wantCode(t, "zone twice", statusOf(alloc(q, zones(zone(1), zone(1)))), im.StatusAlreadyExists) // :1052-1057
	wantCode(t, "null zone twice", statusOf(alloc(q, zones(null, null))), im.StatusAlreadyExists)  // :1063-1068
	many := make([]padef.TransportZoneOptionsStruct, 9)
	for i := range many {
		many[i] = zone(uint16(100 + i))
	}
	wantCode(t, "beyond MaxZones", statusOf(alloc(q, zones(many...))), im.StatusDynamicConstraintError) // :1074-1078
	wantPushCluster(t, "unknown zone", alloc(q, zones(zone(5))), padef.StatusCodeInvalidZone)           // :1082-1095
	r, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Zones = nil })
	wantCode(t, "no zones server", statusOf(alloc(r, zones(zone(1)))), im.StatusDynamicConstraintError)

	o = motionOptions()
	o.ContainerOptions.CmafContainerOptions.SegmentDuration = 1250
	o.ContainerOptions.CmafContainerOptions.ChunkDuration = 600
	wantPushCluster(t, "segment duration", alloc(q, o), padef.StatusCodeInvalidOptions) // :1119-1132
	o = motionOptions()
	o.TriggerOptions.MaxPreRollLen = u16(300)
	wantPushCluster(t, "pre-roll", alloc(q, o), padef.StatusCodeInvalidPreRollLength) // :1167-1181
	o.TriggerOptions.MaxPreRollLen = u16(0)
	if err := alloc(q, o); err != nil { // a zero pre-roll is not checked
		t.Fatal(err)
	}
	m.err = spec.Errorf(im.StatusResourceExhausted, "no uploader")
	if err := alloc(p, motionOptions()); statusOf(err) != im.StatusResourceExhausted { // capacity first
		t.Fatal(err)
	}
	s, sm := newPush(t)
	sm.err = errors.New("uploader down")
	wantCode(t, "port error", statusOf(alloc(s, motionOptions())), im.StatusFailure) // :1193-1210
	if len(s.Connections()) != 0 {
		t.Fatal("a refused transport was stored")
	}
}

func TestPushStreamRules(t *testing.T) {
	alloc := func(o padef.TransportOptionsStruct) error {
		p, _ := newPush(t)
		_, err := pushInvoke(p, padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		return err
	}
	withLists := func(v []padef.VideoStreamStruct, a []padef.AudioStreamStruct) padef.TransportOptionsStruct {
		o := motionOptions()
		o.VideoStreamId, o.AudioStreamId = nil, nil
		if v != nil {
			o.VideoStreams = &v
		}
		if a != nil {
			o.AudioStreams = &a
		}
		return o
	}
	o := motionOptions()
	o.VideoStreams = &[]padef.VideoStreamStruct{{VideoStreamName: "v", VideoStreamId: 1}}
	wantCode(t, "both", statusOf(alloc(o)), im.StatusInvalidCommand)                                                                                                                                                                      // :681
	wantCode(t, "neither", statusOf(alloc(withLists(nil, nil))), im.StatusInvalidCommand)                                                                                                                                                 // :684
	wantPushCluster(t, "duplicate video", alloc(withLists([]padef.VideoStreamStruct{{VideoStreamName: "v", VideoStreamId: 1}, {VideoStreamName: "v", VideoStreamId: 11}}, nil)), padef.StatusCodeDuplicateStreamValues)                   // :706-713
	wantPushCluster(t, "duplicate audio", alloc(withLists(nil, []padef.AudioStreamStruct{{AudioStreamName: "a", AudioStreamId: 2}, {AudioStreamName: "b", AudioStreamId: 2}})), padef.StatusCodeDuplicateStreamValues)                    // :743-750
	wantPushCluster(t, "audio named as video", alloc(withLists([]padef.VideoStreamStruct{{VideoStreamName: "x", VideoStreamId: 1}}, []padef.AudioStreamStruct{{AudioStreamName: "x", AudioStreamId: 2}})), padef.StatusCodeInvalidStream) // :756-764
	wantCode(t, "unknown listed video", statusOf(alloc(withLists([]padef.VideoStreamStruct{{VideoStreamName: "v", VideoStreamId: 5}}, nil))), im.StatusInvalidCommand)                                                                    // :718-723
	wantCode(t, "unknown listed audio", statusOf(alloc(withLists(nil, []padef.AudioStreamStruct{{AudioStreamName: "a", AudioStreamId: 5}}))), im.StatusInvalidCommand)                                                                    // :766-771
	o = motionOptions()
	o.VideoStreamId = nv(5)
	wantPushCluster(t, "unknown video id", alloc(o), padef.StatusCodeInvalidStream) // :807-818
	o = motionOptions()
	o.AudioStreamId = nv(5)
	wantPushCluster(t, "unknown audio id", alloc(o), padef.StatusCodeInvalidStream) // :850-861
	o = motionOptions()
	o.StreamUsage = padef.StreamUsageRecording
	o.VideoStreamId = nnull()
	wantCode(t, "no stream of the usage", statusOf(alloc(o)), im.StatusInvalidInState) // :794-802
	o = motionOptions()
	o.VideoStreamId, o.AudioStreamId = nnull(), nnull()
	p, m := newPush(t)
	conn := allocatePush(t, p, o)
	if got := conn.TransportOptions; got.VideoStreamId.Value != 1 || (*got.VideoStreams)[0].VideoStreamName != "video" || (*got.AudioStreams)[0].AudioStreamId != 2 {
		t.Fatalf("selected %+v", got)
	}
	// The lists' first ids become the deprecated ids (:1135-1155).
	p2, _ := newPush(t)
	conn = allocatePush(t, p2, withLists([]padef.VideoStreamStruct{{VideoStreamName: "main", VideoStreamId: 11}}, []padef.AudioStreamStruct{{AudioStreamName: "mic", AudioStreamId: 22}}))
	if got := conn.TransportOptions; got.VideoStreamId.Value != 11 || got.AudioStreamId.Value != 22 {
		t.Fatalf("first ids %+v", got)
	}
	_ = m
}

func TestPushConnectionRules(t *testing.T) {
	p, m := newPush(t)
	conn := allocatePush(t, p, motionOptions())
	id := conn.ConnectionId
	if id != 1 { // GenerateConnectionID starts after 0 (:247-262)
		t.Fatalf("first connection id %d", id)
	}
	other := peer(2, 0x55)
	_, err := p.MatterInvoke(other, padef.CmdDeallocatePushTransport, padef.DeallocatePushTransportRequest{ConnectionId: id})
	wantCode(t, "dealloc other fabric", statusOf(err), im.StatusNotFound) // :1245
	_, err = p.MatterInvoke(other, padef.CmdModifyPushTransport, padef.ModifyPushTransportRequest{ConnectionId: id, TransportOptions: motionOptions()})
	wantCode(t, "modify other fabric", statusOf(err), im.StatusNotFound) // :1306
	_, err = p.MatterInvoke(other, padef.CmdSetTransportStatus, padef.SetTransportStatusRequest{ConnectionId: spec.ValueOf(id), TransportStatus: padef.TransportStatusActive})
	wantCode(t, "status other fabric", statusOf(err), im.StatusNotFound) // :1398-1402
	_, err = pushInvoke(p, padef.CmdSetTransportStatus, padef.SetTransportStatusRequest{ConnectionId: spec.ValueOf(id), TransportStatus: 2})
	wantCode(t, "status 2", statusOf(err), im.StatusConstraintError) // :1374-1377
	_, err = p.MatterInvoke(other, padef.CmdFindTransport, padef.FindTransportRequest{ConnectionId: spec.NullOf[uint16]()})
	wantCode(t, "find none", statusOf(err), im.StatusNotFound) // :1590-1594
	_, err = pushInvoke(p, padef.CmdFindTransport, padef.FindTransportRequest{ConnectionId: spec.ValueOf[uint16](77)})
	wantCode(t, "find unknown", statusOf(err), im.StatusNotFound) // :1580-1585
	if v, _ := p.MatterReadFiltered(other, padef.AttrCurrentConnections); len(v.(spec.List[padef.TransportConfigurationStruct])) != 0 {
		t.Fatal("another fabric reads the connection")
	}

	trig := func(req padef.ManuallyTriggerTransportRequest) error {
		_, err := pushInvoke(p, padef.CmdManuallyTriggerTransport, req)
		return err
	}
	base := padef.ManuallyTriggerTransportRequest{ConnectionId: id, ActivationReason: padef.TriggerActivationReasonUserInitiated}
	r := base
	r.ActivationReason = 4
	wantCode(t, "reason", statusOf(trig(r)), im.StatusConstraintError) // :1454-1458
	r = base
	r.TimeControl = &padef.TransportMotionTriggerTimeControlStruct{MaxDuration: 1}
	wantCode(t, "InitialDuration 0", statusOf(trig(r)), im.StatusConstraintError) // :1463-1469
	r.TimeControl = &padef.TransportMotionTriggerTimeControlStruct{InitialDuration: 1}
	wantCode(t, "MaxDuration 0", statusOf(trig(r)), im.StatusConstraintError) // :1470-1476
	r = base
	r.ConnectionId = 77
	wantCode(t, "trigger unknown", statusOf(trig(r)), im.StatusNotFound)               // :1481-1487
	wantPushCluster(t, "inactive", trig(base), padef.StatusCodeInvalidTransportStatus) // :1504-1510
	// SetTransportStatus with a null id sets every connection of the fabric.
	allocatePush(t, p, motionOptions())
	_, err = pushInvoke(p, padef.CmdSetTransportStatus, padef.SetTransportStatusRequest{ConnectionId: spec.NullOf[uint16](), TransportStatus: padef.TransportStatusActive})
	wantCode(t, "status all", statusOf(err), im.StatusSuccess) // :1381-1390
	for _, c := range p.Connections() {
		if c.TransportStatus != padef.TransportStatusActive {
			t.Fatalf("connection %d inactive", c.ConnectionId)
		}
	}
	wantCode(t, "trigger", statusOf(trig(base)), im.StatusSuccess)
	wantCode(t, "trigger busy", statusOf(trig(base)), im.StatusBusy) // :1496-1502
	_, err = pushInvoke(p, padef.CmdDeallocatePushTransport, padef.DeallocatePushTransportRequest{ConnectionId: id})
	wantCode(t, "dealloc busy", statusOf(err), im.StatusBusy) // :1250-1252
	_, err = pushInvoke(p, padef.CmdModifyPushTransport, padef.ModifyPushTransportRequest{ConnectionId: id, TransportOptions: motionOptions()})
	wantCode(t, "modify busy", statusOf(err), im.StatusBusy) // :1311-1313
	_ = m
}

func TestPushTriggerKinds(t *testing.T) {
	p, _ := newPush(t)
	cont := motionOptions()
	cont.TriggerOptions = padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous}
	c1 := allocatePush(t, p, cont)
	cmd := motionOptions()
	cmd.TriggerOptions = padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeCommand, MaxPreRollLen: u16(1000)}
	c2 := allocatePush(t, p, cmd)
	_, err := pushInvoke(p, padef.CmdSetTransportStatus, padef.SetTransportStatusRequest{ConnectionId: spec.NullOf[uint16](), TransportStatus: padef.TransportStatusActive})
	wantCode(t, "activate", statusOf(err), im.StatusSuccess)
	_, err = pushInvoke(p, padef.CmdManuallyTriggerTransport, padef.ManuallyTriggerTransportRequest{ConnectionId: c1.ConnectionId})
	wantPushCluster(t, "continuous", err, padef.StatusCodeInvalidTriggerType) // :1512-1518
	_, err = pushInvoke(p, padef.CmdManuallyTriggerTransport, padef.ManuallyTriggerTransportRequest{ConnectionId: c2.ConnectionId})
	wantCode(t, "command without time control", statusOf(err), im.StatusDynamicConstraintError) // :1520-1526

	f := pushStreams()
	f.softRec = true
	q, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Streams = f })
	o := motionOptions()
	o.StreamUsage = padef.StreamUsageAnalysis
	conn := allocatePush(t, q, o)
	_, err = pushInvoke(q, padef.CmdManuallyTriggerTransport, padef.ManuallyTriggerTransportRequest{ConnectionId: conn.ConnectionId})
	wantCode(t, "privacy", statusOf(err), im.StatusInvalidInState) // :1489-1494
}

// eventLog records the emitted events.
type eventLog struct {
	mu     sync.Mutex
	events []any
}

func (l *eventLog) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	l.mu.Lock()
	l.events = append(l.events, data)
	l.mu.Unlock()
}

func TestPushEventsAndExpiry(t *testing.T) {
	clk := clocktest.New(time.Unix(1_000_000, 0))
	p, m := newPush(t, func(c *camera.PushTransportConfig) { c.Clock = clk })
	log := &eventLog{}
	p.SetMatterEventEmitter(log)
	p.SetEndpoint(3)
	o := motionOptions()
	expiry := uint32(30)
	o.ExpiryTime = &expiry
	conn := allocatePush(t, p, o)
	reason := padef.TriggerActivationReasonUserInitiated
	if err := p.TransportStarted(conn.ConnectionId, padef.TransportTriggerTypeCommand, &reason); err != nil {
		t.Fatal(err)
	}
	if err := p.TransportStopped(conn.ConnectionId); err != nil {
		t.Fatal(err)
	}
	if statusOf(p.TransportStarted(99, padef.TransportTriggerTypeMotion, nil)) != im.StatusNotFound || statusOf(p.TransportStopped(99)) != im.StatusNotFound {
		t.Fatal("an unknown connection's event") // :1985-1990, :2022-2027
	}
	if len(log.events) != 2 {
		t.Fatalf("events %+v", log.events)
	}
	begin := log.events[0].(padef.PushTransportBeginEvent)
	end := log.events[1].(padef.PushTransportEndEvent)
	if *begin.CmafSessionNumber != uint64(conn.ConnectionId)+2000 || *begin.ContainerType != padef.ContainerFormatCmaf || *end.CmafSessionNumber != uint64(conn.ConnectionId)+2000 {
		t.Fatalf("events %+v %+v", begin, end)
	}
	// ExpiryTime is the seconds until the deallocation (chip :1216).
	clk.Advance(29 * time.Second)
	if len(p.Connections()) != 1 {
		t.Fatal("expired early")
	}
	clk.Advance(time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for len(p.Connections()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(p.Connections()) != 0 || len(m.deallocated) != 1 {
		t.Fatalf("not expired: %+v", p.Connections())
	}
	var _ padef.Host = p
}
