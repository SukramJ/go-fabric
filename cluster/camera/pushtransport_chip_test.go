// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// PushAvStreamTransport: a port of connectedhomeip
// src/app/clusters/push-av-stream-transport-server/tests/
// TestPushAVStreamTransportCluster.cpp and TestURLParsing.cpp at 6170af84,
// by meaning. chip's TestPushAVStreamTransportDelegateImpl (:153-357) is
// mockPush; its stream and zone answers (all success) are fakeStreams and
// fakeZones holding the ids the cases name; TestTLSClientManagementDelegate
// (:359-423) is fakeTLS. The server is built with PERZONESENS
// (BitFlags<Feature>(1), :445). The two UpdateMotionZoneOptions cases
// (:1761, :2069) are left out: the command is not in the matter.js model.
package camera_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	tcldef "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/im"
)

// mockPush is chip's TestPushAVStreamTransportDelegateImpl: every call
// succeeds, a manual trigger makes the transport busy, the CMAF session
// number is the id plus 2000 (:340-345).
type mockPush struct {
	mu          sync.Mutex
	allocated   map[uint16]padef.TransportOptionsStruct
	credentials []camera.TLSCredentials
	busy        map[uint16]bool
	statuses    map[uint16]padef.TransportStatusEnum
	triggers    []*padef.TransportMotionTriggerTimeControlStruct
	deallocated []uint16
	err         error
}

func newMockPush() *mockPush {
	return &mockPush{allocated: map[uint16]padef.TransportOptionsStruct{}, busy: map[uint16]bool{}, statuses: map[uint16]padef.TransportStatusEnum{}}
}

func (m *mockPush) AllocatePushTransport(_ context.Context, id uint16, _ uint8, o padef.TransportOptionsStruct, c camera.TLSCredentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.allocated[id] = o
	m.credentials = append(m.credentials, c)
	return nil
}

func (m *mockPush) DeallocatePushTransport(_ context.Context, id uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.allocated, id)
	m.deallocated = append(m.deallocated, id)
	return nil
}

func (m *mockPush) ModifyPushTransport(_ context.Context, id uint16, o padef.TransportOptionsStruct) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.allocated[id]; !ok {
		return spec.Errorf(im.StatusNotFound, "no stream")
	}
	m.allocated[id] = o
	return nil
}

func (m *mockPush) SetTransportStatus(_ context.Context, ids []uint16, s padef.TransportStatusEnum) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.statuses[id] = s
	}
	return nil
}

func (m *mockPush) ManuallyTriggerTransport(_ context.Context, id uint16, _ padef.TriggerActivationReasonEnum, tc *padef.TransportMotionTriggerTimeControlStruct) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy[id] = true
	m.triggers = append(m.triggers, tc)
	return nil
}

func (m *mockPush) TransportBusy(id uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[id]
}

func (m *mockPush) ValidateBandwidthLimit(context.Context, padef.StreamUsageEnum, *uint16, *uint16) error {
	return nil
}

func (m *mockPush) CMAFSessionNumber(id uint16) (uint64, bool) { return uint64(id) + 2000, true }

// fakeTLS is chip's TestTLSClientManagementDelegate: every endpoint is
// provisioned (:376-380); nullCCDID is
// TestTLSClientManagementDelegateNullClientCertId (:411-423).
type fakeTLS struct{ nullCCDID bool }

func (f fakeTLS) ProvisionedEndpoint(fabric uint8, id uint16) (tcldef.TLSEndpointStruct, bool) {
	ccdid := spec.ValueOf[uint16](4)
	if f.nullCCDID {
		ccdid = spec.NullOf[uint16]()
	}
	return tcldef.TLSEndpointStruct{EndpointId: id, Hostname: []byte("camera.example"), Port: 443, Caid: 3, Ccdid: ccdid, FabricIndex: fabric}, true
}

// fakeCerts holds the endpoint's certificates.
type fakeCerts struct{}

func (fakeCerts) RootCertificate(fabric uint8, caid uint16) (tcmdef.TLSCertStruct, bool) {
	c := []byte("root")
	return tcmdef.TLSCertStruct{Caid: caid, Certificate: &c, FabricIndex: fabric}, true
}

func (fakeCerts) ClientCertificate(fabric uint8, ccdid uint16) (tcmdef.TLSClientCertificateDetailStruct, bool) {
	c := spec.ValueOf([]byte("client"))
	return tcmdef.TLSClientCertificateDetailStruct{Ccdid: ccdid, ClientCertificate: &c, FabricIndex: fabric}, true
}

// fakeZones holds the zones the cases name.
type fakeZones struct{ zones []camera.Zone }

func (f fakeZones) ZoneList() []camera.Zone { return f.zones }
func (fakeZones) MaxZones() uint8           { return 8 }

const analysis = csdef.StreamUsageAnalysis

// pushStreams holds the streams the cases name: video 1 and 11, audio 2
// and 22, of the Analysis usage, key frames every 500 ms.
func pushStreams() *fakeStreams {
	return &fakeStreams{
		videos: []camera.VideoStream{
			{VideoStreamId: 1, StreamUsage: analysis, KeyFrameInterval: 500},
			{VideoStreamId: 11, StreamUsage: analysis, KeyFrameInterval: 500},
		},
		audios:     []camera.AudioStream{{AudioStreamId: 2, StreamUsage: analysis}, {AudioStreamId: 22, StreamUsage: analysis}},
		priorities: []camera.StreamUsage{analysis, liveView},
		supported:  []camera.StreamUsage{analysis, liveView, recording},
	}
}

func pushZones() fakeZones {
	return fakeZones{zones: []camera.Zone{{ZoneId: 1, ZoneSource: zmdef.ZoneSourceUser}, {ZoneId: 2}, {ZoneId: 7}, {ZoneId: 9}}}
}

func newPush(t *testing.T, edit ...func(*camera.PushTransportConfig)) (*camera.PushTransport, *mockPush) {
	t.Helper()
	m := newMockPush()
	cfg := camera.PushTransportConfig{
		Features: camera.FeaturePushPerZoneSensitivity, Port: m, Streams: pushStreams(), Zones: pushZones(),
		TLSEndpoints: fakeTLS{}, TLSCertificates: fakeCerts{},
	}
	for _, e := range edit {
		e(&cfg)
	}
	p, err := camera.NewPushTransport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, m
}

func u8(v uint8) *uint8    { return &v }
func u16(v uint16) *uint16 { return &v }

// motionOptions are the cases' transport options (:810-875): Analysis,
// the deprecated stream ids 1 and 2, a Motion trigger on zones 1 (5) and
// 2 (10), CMAF of 1000 ms segments.
func motionOptions() padef.TransportOptionsStruct {
	track := "video"
	zones := spec.ValueOf([]padef.TransportZoneOptionsStruct{
		{Zone: spec.ValueOf[uint16](1), Sensitivity: u8(5)},
		{Zone: spec.ValueOf[uint16](2), Sensitivity: u8(10)},
	})
	return padef.TransportOptionsStruct{
		StreamUsage: padef.StreamUsageAnalysis, VideoStreamId: nv(1), AudioStreamId: nv(2), TlsEndpointId: 1,
		Url: "https://192.168.1.100:554/stream/",
		TriggerOptions: padef.TransportTriggerOptionsStruct{
			TriggerType: padef.TransportTriggerTypeMotion, MotionZones: &zones,
			MotionTimeControl: &padef.TransportMotionTriggerTimeControlStruct{InitialDuration: 5000, AugmentationDuration: 2000, MaxDuration: 30000, BlindDuration: 1000},
			MaxPreRollLen:     u16(1000),
		},
		IngestMethod: padef.IngestMethodsCmafIngest,
		ContainerOptions: padef.ContainerOptionsStruct{
			ContainerType:        padef.ContainerFormatCmaf,
			CmafContainerOptions: &padef.CMAFContainerOptionsStruct{SegmentDuration: 1000, ChunkDuration: 500, TrackName: &track},
		},
	}
}

func allocatePush(t *testing.T, p *camera.PushTransport, o padef.TransportOptionsStruct) padef.TransportConfigurationStruct {
	t.Helper()
	resp, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
	wantCode(t, "AllocatePushTransport", statusOf(err), im.StatusSuccess)
	return resp.(padef.AllocatePushTransportResponse).TransportConfiguration
}

func pushCluster(err error) (uint8, bool) {
	var cs im.MatterClusterStatusError
	if !errors.As(err, &cs) {
		return 0, false
	}
	return cs.MatterClusterStatus(), true
}

// AllocatePushTransportRejectsEndpointWithNullClientCertificateId
// (:447-503): a TLS endpoint without a client certificate is
// InvalidTLSEndpoint and allocates nothing.
func TestPushAllocateRejectsNullClientCertificate(t *testing.T) {
	p, _ := newPush(t, func(c *camera.PushTransportConfig) { c.TLSEndpoints = fakeTLS{nullCCDID: true} })
	o := motionOptions()
	o.TriggerOptions = padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous}
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
	if code, ok := pushCluster(err); !ok || code != uint8(padef.StatusCodeInvalidTlsEndpoint) {
		t.Fatalf("status %v", err)
	}
	if len(p.Connections()) != 0 {
		t.Fatal("a connection was allocated")
	}
}

// TestTransportOptionsConstraints (:505-608): a motion trigger without
// MotionZones is INVALID_COMMAND, without a SegmentDuration
// CONSTRAINT_ERROR, then valid.
func TestPushTransportOptionsConstraints(t *testing.T) {
	p, _ := newPush(t)
	invoke := func(o padef.TransportOptionsStruct) error {
		_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
		return err
	}
	o := motionOptions()
	o.TriggerOptions.MotionZones = nil
	o.ContainerOptions.CmafContainerOptions = &padef.CMAFContainerOptionsStruct{ChunkDuration: 1000}
	wantCode(t, "no zones", statusOf(invoke(o)), im.StatusInvalidCommand)
	o = motionOptions()
	o.ContainerOptions.CmafContainerOptions = &padef.CMAFContainerOptionsStruct{ChunkDuration: 1000}
	wantCode(t, "no segment duration", statusOf(invoke(o)), im.StatusConstraintError)
	wantCode(t, "valid", statusOf(invoke(motionOptions())), im.StatusSuccess)
}

// TestValidateIncomingTransportOptions_StreamCountConstraint (:610-771):
// 17 video or 17 audio streams are CONSTRAINT_ERROR.
func TestPushStreamCountConstraint(t *testing.T) {
	p, _ := newPush(t)
	o := motionOptions()
	o.VideoStreamId, o.AudioStreamId = nil, nil
	empty := spec.ValueOf([]padef.TransportZoneOptionsStruct{})
	o.TriggerOptions.MotionZones = &empty
	videos := make([]padef.VideoStreamStruct, 17)
	for i := range videos {
		videos[i] = padef.VideoStreamStruct{VideoStreamName: "Stream" + string(rune('a'+i)), VideoStreamId: uint16(i)}
	}
	o.VideoStreams = &videos
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
	wantCode(t, "17 video streams", statusOf(err), im.StatusConstraintError)
	o.VideoStreams = nil
	audios := make([]padef.AudioStreamStruct, 17)
	for i := range audios {
		audios[i] = padef.AudioStreamStruct{AudioStreamName: "Audio" + string(rune('a'+i)), AudioStreamId: uint16(i)}
	}
	o.AudioStreams = &audios
	_, err = p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
	wantCode(t, "17 audio streams", statusOf(err), im.StatusConstraintError)
}

// Test_AllocateTransport_AllocateTransportResponse_ReadAttribute_DeallocateTransport
// (:785-1118): the response and CurrentConnections carry the options as
// sent, the transport inactive on fabric 1; DeallocatePushTransport
// removes it.
func TestPushAllocateReadDeallocate(t *testing.T) {
	p, m := newPush(t)
	conn := allocatePush(t, p, motionOptions())
	check := func(what string, c padef.TransportConfigurationStruct) {
		t.Helper()
		o := c.TransportOptions
		zones := o.TriggerOptions.MotionZones.Value
		switch {
		case c.FabricIndex != 1 || c.TransportStatus != padef.TransportStatusInactive:
			t.Fatalf("%s: %+v", what, c)
		case o.StreamUsage != padef.StreamUsageAnalysis || o.VideoStreamId.Value != 1 || o.AudioStreamId.Value != 2 || o.TlsEndpointId != 1:
			t.Fatalf("%s: options %+v", what, o)
		case o.Url != "https://192.168.1.100:554/stream/" || o.TriggerOptions.TriggerType != padef.TransportTriggerTypeMotion:
			t.Fatalf("%s: options %+v", what, o)
		case len(zones) != 2 || zones[0].Zone.Value != 1 || *zones[0].Sensitivity != 5 || zones[1].Zone.Value != 2 || *zones[1].Sensitivity != 10:
			t.Fatalf("%s: zones %+v", what, zones)
		case o.TriggerOptions.MotionSensitivity != nil || *o.TriggerOptions.MaxPreRollLen != 1000 || o.TriggerOptions.MotionTimeControl.MaxDuration != 30000:
			t.Fatalf("%s: trigger %+v", what, o.TriggerOptions)
		case o.ContainerOptions.CmafContainerOptions.SegmentDuration != 1000 || *o.ContainerOptions.CmafContainerOptions.TrackName != "video":
			t.Fatalf("%s: container %+v", what, o.ContainerOptions)
		}
	}
	check("response", conn)
	v, _ := p.MatterRead(padef.AttrCurrentConnections)
	var conns []padef.TransportConfigurationStruct
	if !spec.Assign(&conns, v) || len(conns) != 1 || conns[0].ConnectionId != conn.ConnectionId {
		t.Fatalf("CurrentConnections %v", v)
	}
	check("read", conns[0])
	if cred := m.credentials[0]; string(cred.RootCertificate) != "root" || string(cred.ClientCertificate) != "client" {
		t.Fatalf("credentials %+v", cred)
	}
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdDeallocatePushTransport, padef.DeallocatePushTransportRequest{ConnectionId: conn.ConnectionId})
	wantCode(t, "DeallocatePushTransport", statusOf(err), im.StatusSuccess)
	if len(p.Connections()) != 0 {
		t.Fatal("the connection survived")
	}
}

// Test_AllocateTransport_Persistence_DeallocateTransport (:1120-1333):
// the connections the host persisted come back with a new server (chip's
// SafeAttributePersistenceProvider is the host's here).
func TestPushPersistence(t *testing.T) {
	p, _ := newPush(t)
	conn := allocatePush(t, p, motionOptions())
	q, _ := newPush(t, func(c *camera.PushTransportConfig) { c.Connections = p.Connections() })
	got := q.Connections()
	if len(got) != 1 || got[0].ConnectionId != conn.ConnectionId || got[0].TransportStatus != padef.TransportStatusInactive ||
		got[0].TransportOptions.Url != conn.TransportOptions.Url || len(got[0].TransportOptions.TriggerOptions.MotionZones.Value) != 2 {
		t.Fatalf("restored %+v", got)
	}
	_, err := q.MatterInvoke(peer(1, 0x55), padef.CmdDeallocatePushTransport, padef.DeallocatePushTransportRequest{ConnectionId: conn.ConnectionId})
	wantCode(t, "DeallocatePushTransport", statusOf(err), im.StatusSuccess)
}

// Test_AllocateTransport_ModifyTransport_FindTransport_FindTransportResponse
// (:1335-1625): the modified options are what FindTransport answers.
func TestPushModifyFind(t *testing.T) {
	p, _ := newPush(t)
	conn := allocatePush(t, p, motionOptions())
	o := motionOptions()
	o.VideoStreamId, o.AudioStreamId = nv(11), nv(22)
	o.Url = "https://192.168.1.100:554/modify-stream/"
	zones := spec.ValueOf([]padef.TransportZoneOptionsStruct{{Zone: spec.ValueOf[uint16](7), Sensitivity: u8(8)}, {Zone: spec.ValueOf[uint16](9), Sensitivity: u8(6)}})
	o.TriggerOptions.MotionZones = &zones
	o.TriggerOptions.MotionTimeControl = &padef.TransportMotionTriggerTimeControlStruct{InitialDuration: 1000, AugmentationDuration: 1000, MaxDuration: 10000, BlindDuration: 1000}
	o.ContainerOptions.CmafContainerOptions = &padef.CMAFContainerOptionsStruct{SegmentDuration: 30000, ChunkDuration: 1000}
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdModifyPushTransport, padef.ModifyPushTransportRequest{ConnectionId: conn.ConnectionId, TransportOptions: o})
	wantCode(t, "ModifyPushTransport", statusOf(err), im.StatusSuccess)
	resp, err := p.MatterInvoke(peer(1, 0x55), padef.CmdFindTransport, padef.FindTransportRequest{ConnectionId: spec.ValueOf(conn.ConnectionId)})
	wantCode(t, "FindTransport", statusOf(err), im.StatusSuccess)
	found := resp.(padef.FindTransportResponse).TransportConfigurations
	if len(found) != 1 {
		t.Fatalf("found %+v", found)
	}
	f := found[0].TransportOptions
	z := f.TriggerOptions.MotionZones.Value
	if f.VideoStreamId.Value != 11 || f.AudioStreamId.Value != 22 || f.Url != o.Url || z[0].Zone.Value != 7 || *z[1].Sensitivity != 6 ||
		f.TriggerOptions.MotionTimeControl.MaxDuration != 10000 || f.ContainerOptions.CmafContainerOptions.SegmentDuration != 30000 {
		t.Fatalf("found %+v", f)
	}
}

// Test_AllocateTransport_SetTransportStatus_ManuallyTriggerTransport
// (:1627-1759): the transport turns active and takes a manual trigger
// with its motion time control.
func TestPushSetStatusTrigger(t *testing.T) {
	p, m := newPush(t)
	conn := allocatePush(t, p, motionOptions())
	_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdSetTransportStatus, padef.SetTransportStatusRequest{ConnectionId: spec.ValueOf(conn.ConnectionId), TransportStatus: padef.TransportStatusActive})
	wantCode(t, "SetTransportStatus", statusOf(err), im.StatusSuccess)
	if p.Connections()[0].TransportStatus != padef.TransportStatusActive {
		t.Fatal("the transport is not active")
	}
	_, err = p.MatterInvoke(peer(1, 0x55), padef.CmdManuallyTriggerTransport, padef.ManuallyTriggerTransportRequest{ConnectionId: conn.ConnectionId, ActivationReason: padef.TriggerActivationReasonUserInitiated})
	wantCode(t, "ManuallyTriggerTransport", statusOf(err), im.StatusSuccess)
	if len(m.triggers) != 1 || m.triggers[0] == nil || m.triggers[0].MaxDuration != 30000 {
		t.Fatalf("triggers %+v", m.triggers)
	}
}

// TestURLParsing.cpp's TestValidateUrl cases (:479-660): the URL check of
// AllocatePushTransport. The TestUriExtractionHelpers cases (:245-477)
// test uriparser's own helpers and are left out.
func TestPushValidateURL(t *testing.T) {
	cases := map[string][]struct {
		url  string
		want bool
	}{
		"ValidHttpsUrl": {
			{"https://192.168.1.100:554/stream/", true},
			{"https://example.com/", true},
			{"https://example.com/path/", true},
			{"https://subdomain.example.com/", true},
			{"https://example.com:443/", true},
			{"https://192.168.1.1/", true},
		},
		"CaseInsensitiveScheme": {{"HTTPS://example.com/", true}, {"Https://example.com/", true}, {"HtTpS://example.com/", true}},
		"InvalidScheme":         {{"http://example.com/", false}, {"ftp://example.com/", false}, {"file://example.com/", false}, {"ws://example.com/", false}, {"wss://example.com/", false}},
		"MissingScheme":         {{"example.com/path/", false}, {"//example.com/", false}},
		"MissingHost":           {{"https:///path/", false}, {"https:///long/path/", false}},
		"WithFragment":          {{"https://example.com/#fragment", false}, {"https://example.com/path/#fragment", false}, {"https://example.com/#", false}},
		"WithQuery":             {{"https://example.com/?query=value", false}, {"https://example.com/path/?query=value", false}, {"https://example.com/?", false}},
		"NoTrailingSlash":       {{"https://example.com", false}, {"https://example.com/path", false}},
		"MalformedUrl": {
			{"https:/example.com/", false},
			{"https:example.com/", false},
			{"://example.com/", false},
			{"https//example.com/", false},
			{"https/example.com/", false},
			{"https::://example.com/", false},
			{"https://example.com::443/", false},
			{"https:///example.com/", false},
		},
		"ComplexValidPaths": {{"https://example.com/api/v1/", true}, {"https://example.com/path/to/resource/", true}, {"https://example.com/very/long/path/with/many/segments/", true}},
		"IPv6Addresses":     {{"https://[2001:0db8:85a3:0000:0000:8a2e:0370:7334]/", true}, {"https://[::1]/", true}, {"https://[2001:db8::1]/", true}},
		"PortNumbers":       {{"https://example.com:443/", true}, {"https://example.com:8080/", true}, {"https://example.com:8443/", true}},
		"InvalidIPv6Addresses": {
			{"https://2001:0db8::1/", false},
			{"https://::12/", false},
			{"https://[2001::db8::1]/", false},
			{"https://[::1::2]/", false},
			{"https://[:::/", false},
			{"https://[2001:::1]/", false},
			{"https://[1:2:3:4:5:6:7:8:9]/", false},
			{"https://[::1:2:3:4:5:6:7:8]/", false},
			{"https://[g:0:0:0:0:0:0:1]/", false},
			{"https://[2001:0db8:g:1]/", false},
			{"https://[::ffff:256.1.1.1]/", false},
			{"https://[::ffff:1.2.3.256]/", false},
			{"https://[::ffff:1.2.3]/", false},
			{"https://[::ffff:192.168.1.1]/", true},
		},
		"PercentEncodingEdgeCases": {
			{"https://example.com/path%20with%20spaces/", true},
			{"https://example.com/path%2Fwith%2Fslashes/", true},
			{"https://example.com/path%/", false},
			{"https://example.com/path%2/", false},
			{"https://example.com/path%g2/", false},
			{"https://example.com/path%2g/", false},
		},
		"InvalidPort": {{"https://example.com:port/", false}, {"https://example.com:-1/", false}, {"https://example.com:12abc/", false}},
		"VerifyPath": {
			{"https://example.com/path-with-dashes/", true},
			{"https://example.com/path_with_underscores/", true},
			{"https://example.com/path.with.dots/", true},
			{"https://example.com/123/", true},
			{"https://user@pass@example.com/", false},
			{"https://example.com/path with spaces/", false},
			{"https://example.com/path\nwith\nnewlines/", false},
			{"https://example.com//", true},
			{"https://example.com///", true},
			{"https://example.com/api%2Fv1%2Fendpoint/", true},
		},
		"HostnameEdgeCases": {{"https://example .com/", false}, {"https://example\n.com/", false}, {"https://example\t.com/", false}},
	}
	for name, urls := range cases {
		t.Run(name, func(t *testing.T) {
			for _, c := range urls {
				p, _ := newPush(t)
				o := motionOptions()
				o.Url = c.url
				o.TriggerOptions = padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeCommand, MaxPreRollLen: u16(1000)}
				o.ContainerOptions.CmafContainerOptions = &padef.CMAFContainerOptionsStruct{SegmentDuration: 4000}
				_, err := p.MatterInvoke(peer(1, 0x55), padef.CmdAllocatePushTransport, padef.AllocatePushTransportRequest{TransportOptions: o})
				code, isURL := pushCluster(err)
				rejected := isURL && code == uint8(padef.StatusCodeInvalidUrl)
				if len(c.url) < 13 {
					rejected = statusOf(err) == im.StatusConstraintError
				}
				if c.want == rejected || (c.want && err != nil) {
					t.Errorf("%q: %v", c.url, err)
				}
			}
		})
	}
}
