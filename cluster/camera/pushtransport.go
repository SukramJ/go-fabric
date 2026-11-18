// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/internal/deepcopy"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// PushTransportClusterID is the PushAvStreamTransport cluster id.
const PushTransportClusterID = padef.ClusterID

// Generated types the push transport speaks.
type (
	// PushFeature is a FeatureMap bit of PushAvStreamTransport.
	PushFeature = padef.Feature
	// TransportConfiguration is an entry of CurrentConnections.
	TransportConfiguration = padef.TransportConfigurationStruct
	// TransportOptions are a transport's options.
	TransportOptions = padef.TransportOptionsStruct
	// PushTransportPort is the camera's push uploader.
	PushTransportPort = padef.PushTransportPort
	// TLSCredentials are what a transport uploads with.
	TLSCredentials = padef.TLSCredentials
)

// The features.
const (
	FeaturePushPerZoneSensitivity = padef.FeaturePerZoneSensitivity
	FeaturePushMetadata           = padef.FeatureMetadata
)

// The spec's bounds (connectedhomeip push-av-stream-transport-server/
// constants.h:37-45, PushAVStreamTransportLogic.cpp:40-41) and chip's
// CHIP_CONFIG_MAX_NUM_PUSH_TRANSPORTS (src/lib/core/CHIPConfig.h:2099).
const (
	minURLLength                   = 13
	maxURLLength                   = 2000
	minSegmentDuration             = 500
	maxSegmentDuration             = 65500
	minStreamNameLength            = 1
	maxStreamNameLength            = 16
	maxPushStreams                 = 16
	maxConnectionID         uint16 = 65535
	maxTLSEndpointID        uint16 = 65534
	DefaultMaxPushPerFabric        = 4
)

// The enum bounds of the model's values.
const (
	maxTriggerType      = padef.TransportTriggerTypeContinuous
	maxActivationReason = padef.TriggerActivationReasonDoorbellPressed
	maxTransportStatus  = padef.TransportStatusInactive
	maxCMAFInterface    = padef.CMAFInterfaceInterface2Hls
)

// ErrNoPushTransportPort reports a PushAvStreamTransport built without
// its port.
var ErrNoPushTransportPort = errors.New("camera: PushAvStreamTransport needs a PushTransportPort")

// ZoneSource is the camera's zones a motion trigger names: [*Zones] is
// one. chip's delegate answers ValidateZoneId and
// ValidateMotionZoneListSize from the ZoneManagement server
// (push-av-stream-manager.cpp:530-562).
type ZoneSource interface {
	ZoneList() []Zone
	MaxZones() uint8
}

var _ ZoneSource = (*Zones)(nil)

// PushTransportConfig carries the construction parameters of a
// [PushTransport].
type PushTransportConfig struct {
	// Features is the FeatureMap selection.
	Features PushFeature
	// Port is the camera's push uploader; required.
	Port PushTransportPort
	// Streams is the camera's CameraAvStreamManagement server and Zones
	// its ZoneManagement server; nil is a camera without either.
	Streams StreamSource
	Zones   ZoneSource
	// TLSEndpoints and TLSCertificates are the node's TlsClientManagement
	// and TlsCertificateManagement servers (CodegenIntegration.cpp:124,
	// :133).
	TLSEndpoints    padef.TLSEndpoints
	TLSCertificates padef.TLSCertificates
	// Connections are the transports the host persisted (chip's
	// LoadCurrentConnections, :2067-2131).
	Connections []TransportConfiguration
	// MaxPerFabric bounds a fabric's transports; 0 is chip's 4.
	MaxPerFabric int
	// Clock runs the expiry timers; nil is the system clock.
	Clock clock.Clock
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// PushTransport implements [contract.ClusterServer] for
// PushAvStreamTransport: connectedhomeip's server logic over the generated
// server — the transport options' validation, the TLS endpoint, the URL,
// the streams and the motion zones checked before the [PushTransportPort]
// allocates a transport, the per-fabric connection table with its expiry
// timers, and the PushTransportBegin and PushTransportEnd events the host
// reports. Mirrors PushAVStreamTransportLogic.cpp at 6170af84 (matter.js's
// server is the generated behaviour).
type PushTransport struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	port  PushTransportPort
	cfg   PushTransportConfig
	view  streamView
	clock clock.Clock

	// op serialises the commands, the expiries and the fabric removal
	// from their checks through the port to their commit ([serial]); pub
	// serialises each change of the connection table with its
	// publication; mu guards the state below.
	op       sync.Mutex
	pub      sync.Mutex
	mu       sync.Mutex
	endpoint uint16
	conns    []TransportConfiguration
	timers   map[uint16]*expiry
	// quiesced is set by MatterQuiesce, which stopped the expiries;
	// SetEndpoint arms them again for the time left.
	quiesced bool
	lastID   uint16
	tlsEP    padef.TLSEndpoints
	tlsCerts padef.TLSCertificates
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*PushTransport)(nil)
	_ contract.ClusterDataVersion             = (*PushTransport)(nil)
	_ contract.ClusterAttributeLister         = (*PushTransport)(nil)
	_ contract.ClusterCommandLister           = (*PushTransport)(nil)
	_ contract.ClusterEventLister             = (*PushTransport)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*PushTransport)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*PushTransport)(nil)
	_ contract.AttributeChangeNotifier        = (*PushTransport)(nil)
	_ contract.EventReceiver                  = (*PushTransport)(nil)
	_ contract.FabricScopedReader             = (*PushTransport)(nil)
	_ spec.CommandRegistrar                   = (*PushTransport)(nil)
	_ padef.Host                              = (*PushTransport)(nil)
)

// NewPushTransport builds a PushAvStreamTransport server on cfg.Port with
// the connections the host persisted; SupportedFormats is chip's CMAF over
// CMAF ingest (Logic.cpp:89-96).
func NewPushTransport(cfg PushTransportConfig) (*PushTransport, error) {
	if cfg.Port == nil {
		return nil, ErrNoPushTransportPort
	}
	if cfg.MaxPerFabric <= 0 {
		cfg.MaxPerFabric = DefaultMaxPushPerFabric
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.System{}
	}
	p := &PushTransport{
		port: cfg.Port, cfg: cfg, view: streamView{cfg.Streams}, clock: clk, timers: map[uint16]*expiry{},
		tlsEP: cfg.TLSEndpoints, tlsCerts: cfg.TLSCertificates,
	}
	p.conns = deepcopy.Each(cfg.Connections)
	handlers := map[uint32]spec.CommandHandler{
		padef.CmdAllocatePushTransport:    serial(&p.op, p.allocate),
		padef.CmdDeallocatePushTransport:  serial(&p.op, p.deallocate),
		padef.CmdModifyPushTransport:      serial(&p.op, p.modify),
		padef.CmdSetTransportStatus:       serial(&p.op, p.setStatus),
		padef.CmdManuallyTriggerTransport: serial(&p.op, p.trigger),
		padef.CmdFindTransport:            serial(&p.op, p.find),
	}
	srv, err := spec.NewServer(padef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	p.srv, p.Instance, p.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{
		padef.AttrSupportedFormats: spec.List[padef.SupportedFormatStruct]{{
			ContainerFormat: padef.ContainerFormatCmaf, IngestMethod: padef.IngestMethodsCmafIngest, // :91-92
		}},
		padef.AttrCurrentConnections: spec.List[TransportConfiguration](nonNil(p.conns)),
	}); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return p, nil
}

// Connections returns CurrentConnections: a deep copy, which the caller
// may change.
func (p *PushTransport) Connections() []TransportConfiguration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return deepcopy.Each(p.conns)
}

// AttachTLS hands the server the node's TLS servers: each lookup the
// configuration left nil is set, one the host set stays. The node calls
// it for every PushAvStreamTransport of its topology when it mounts the
// TLS servers on endpoint 0 (node.Config.TLS); chip wires the same
// delegates (CodegenIntegration.cpp:124, :133).
func (p *PushTransport) AttachTLS(endpoints padef.TLSEndpoints, certificates padef.TLSCertificates) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tlsEP == nil {
		p.tlsEP = endpoints
	}
	if p.tlsCerts == nil {
		p.tlsCerts = certificates
	}
}

// expiry is a connection's armed deallocation: its timer and its
// deadline on the server's clock, which a remount re-arms from.
type expiry struct {
	timer clock.Timer
	at    time.Time
	ctx   context.Context //nolint:containedctx // the allocating command's values, without its cancel, for the port call
}

// Close stops the expiry timers (chip's Shutdown, :105-112) as
// [PushTransport.MatterQuiesce] does.
func (p *PushTransport) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.timers {
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
	}
	p.quiesced = true
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the bridge calls it
// when the server leaves the topology or stops. Every connection's expiry
// timer stops, so none reaches the port or changes CurrentConnections
// afterwards; the deadlines are kept, and the [PushTransport.SetEndpoint]
// of a reassembly that puts the same server back arms each again for the
// time left — at once for one that passed meanwhile — as EvseServer does
// (review II, R2-P6-conc-1606).
func (p *PushTransport) MatterQuiesce() {
	p.Close()
	p.srv.MatterQuiesce()
}

// change applies edit to a copy of the connection table and, when edit
// reports a change, publishes the result (a command's change, as each one
// is) and keeps it — both under pub; a
// table the schema refuses is neither published nor kept (review II,
// R2-P6-err-1611).
func (p *PushTransport) change(edit func([]TransportConfiguration) ([]TransportConfiguration, bool)) error {
	p.pub.Lock()
	defer p.pub.Unlock()
	next, changed := edit(p.Connections())
	if !changed {
		return nil
	}
	if err := p.srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{padef.AttrCurrentConnections: spec.List[TransportConfiguration](nonNil(next))}); err != nil {
		return fmt.Errorf("camera: CurrentConnections: %w", err)
	}
	p.mu.Lock()
	p.conns = next
	p.mu.Unlock()
	return nil
}

// commitError answers a command whose change the store refused.
func commitError(err error) error {
	return spec.Errorf(im.StatusFailure, "%v", err)
}

// indexLocked returns the index of the fabric's connection of the id (chip's
// FindStreamTransportConnectionWithinFabric, :232-246); the caller holds
// mu.
func (p *PushTransport) indexLocked(id uint16, fabric uint8, anyFabric bool) int {
	return slices.IndexFunc(p.conns, func(c TransportConfiguration) bool {
		return c.ConnectionId == id && (anyFabric || c.FabricIndex == fabric)
	})
}

// pushStatus is a PushAvStreamTransport cluster status: FAILURE with the
// code (chip's AddClusterSpecificFailure).
type pushStatus struct {
	code padef.StatusCodeEnum
	msg  string
}

func (e pushStatus) Error() string                 { return e.msg }
func (pushStatus) MatterStatusCode() im.StatusCode { return im.StatusFailure }
func (e pushStatus) MatterClusterStatus() uint8    { return uint8(e.code) }

var _ im.MatterClusterStatusError = pushStatus{}

func pushErr(code padef.StatusCodeEnum, format string, args ...any) error {
	return pushStatus{code, fmt.Sprintf("PushAvStreamTransport: "+format, args...)}
}

func pushf(status im.StatusCode, format string, args ...any) error {
	return spec.Errorf(status, "PushAvStreamTransport: "+format, args...)
}

// validateOptions is chip's ValidateIncomingTransportOptions
// (:387-664).
func (p *PushTransport) validateOptions(o TransportOptions) error {
	if uint8(o.StreamUsage) > usageLiveView { // :391-392
		return pushf(im.StatusConstraintError, "StreamUsage %d", o.StreamUsage)
	}
	if o.VideoStreams != nil {
		if len(*o.VideoStreams) > maxPushStreams { // :405
			return pushf(im.StatusConstraintError, "%d video streams", len(*o.VideoStreams))
		}
		for _, s := range *o.VideoStreams {
			if len(s.VideoStreamName) < minStreamNameLength || len(s.VideoStreamName) > maxStreamNameLength { // :413-415
				return pushf(im.StatusConstraintError, "video stream name %q", s.VideoStreamName)
			}
		}
	}
	if o.AudioStreams != nil {
		if len(*o.AudioStreams) > maxPushStreams { // :433
			return pushf(im.StatusConstraintError, "%d audio streams", len(*o.AudioStreams))
		}
		for _, s := range *o.AudioStreams {
			if len(s.AudioStreamName) < minStreamNameLength || len(s.AudioStreamName) > maxStreamNameLength { // :441-443
				return pushf(im.StatusConstraintError, "audio stream name %q", s.AudioStreamName)
			}
		}
	}
	if o.TlsEndpointId > maxTLSEndpointID { // :451
		return pushf(im.StatusConstraintError, "TLSEndpointID %d", o.TlsEndpointId)
	}
	if len(o.Url) < minURLLength || len(o.Url) > maxURLLength { // :456-457
		return pushf(im.StatusConstraintError, "URL of %d characters", len(o.Url))
	}
	if err := p.validateTrigger(o.TriggerOptions); err != nil {
		return err
	}
	if o.IngestMethod != padef.IngestMethodsCmafIngest { // :600-601
		return pushf(im.StatusConstraintError, "IngestMethod %d", o.IngestMethod)
	}
	return p.validateContainer(o.ContainerOptions)
}

// validateTrigger is the trigger part of ValidateIncomingTransportOptions
// (:463-596). The model has no AmbientContext trigger (chip's :533-539).
func (p *PushTransport) validateTrigger(t padef.TransportTriggerOptionsStruct) error {
	perZone := p.HasFeature("PERZONESENS")
	if t.TriggerType > maxTriggerType { // :465-466
		return pushf(im.StatusConstraintError, "TriggerType %d", t.TriggerType)
	}
	if t.TriggerType == padef.TransportTriggerTypeMotion {
		if t.MotionZones == nil { // :471-472
			return pushf(im.StatusInvalidCommand, "a motion trigger without MotionZones")
		}
		if !t.MotionZones.Null {
			for _, z := range t.MotionZones.Value {
				switch {
				case perZone && z.Sensitivity == nil: // :486-487
					return pushf(im.StatusInvalidCommand, "a zone without its sensitivity")
				case perZone && (*z.Sensitivity < 1 || *z.Sensitivity > 10): // :491-493
					return pushf(im.StatusConstraintError, "zone sensitivity %d", *z.Sensitivity)
				case !perZone && z.Sensitivity != nil: // :500
					return pushf(im.StatusInvalidCommand, "a zone sensitivity without PERZONESENS")
				}
			}
		}
		switch {
		case t.MotionTimeControl == nil: // :516
			return pushf(im.StatusInvalidCommand, "a motion trigger without MotionTimeControl")
		case t.MotionTimeControl.InitialDuration < 1: // :521-522
			return pushf(im.StatusConstraintError, "InitialDuration 0")
		case t.MotionTimeControl.MaxDuration < 1: // :528-529
			return pushf(im.StatusConstraintError, "MaxDuration 0")
		}
	} else {
		switch {
		case t.MotionZones != nil: // :545
			return pushf(im.StatusInvalidCommand, "MotionZones without a motion trigger")
		case t.MotionTimeControl != nil: // :550
			return pushf(im.StatusInvalidCommand, "MotionTimeControl without a motion trigger")
		}
	}
	if t.TriggerType == padef.TransportTriggerTypeMotion && !perZone {
		if t.MotionSensitivity == nil { // :558-559
			return pushf(im.StatusInvalidCommand, "a motion trigger without MotionSensitivity")
		}
		if v := t.MotionSensitivity; !v.Null && (v.Value < 1 || v.Value > 10) { // :564-566
			return pushf(im.StatusConstraintError, "MotionSensitivity %d", v.Value)
		}
	} else if t.MotionSensitivity != nil { // :573-574
		return pushf(im.StatusInvalidCommand, "an unexpected MotionSensitivity")
	}
	if t.TriggerType == padef.TransportTriggerTypeMotion || t.TriggerType == padef.TransportTriggerTypeCommand {
		if t.MaxPreRollLen == nil { // :584
			return pushf(im.StatusInvalidCommand, "a trigger without MaxPreRollLen")
		}
	} else if t.MaxPreRollLen != nil { // :591-592
		return pushf(im.StatusInvalidCommand, "an unexpected MaxPreRollLen")
	}
	return nil
}

// validateContainer is the container part of
// ValidateIncomingTransportOptions (:604-660).
func (p *PushTransport) validateContainer(c padef.ContainerOptionsStruct) error {
	if c.ContainerType != padef.ContainerFormatCmaf { // :606-607
		return pushf(im.StatusConstraintError, "ContainerType %d", c.ContainerType)
	}
	o := c.CmafContainerOptions
	switch {
	case o == nil: // :612
		return pushf(im.StatusInvalidCommand, "CMAF without CMAFContainerOptions")
	case o.CmafInterface > maxCMAFInterface: // :617-622
		return pushf(im.StatusConstraintError, "CMAFInterface %d", o.CmafInterface)
	case o.SegmentDuration < minSegmentDuration || o.SegmentDuration > maxSegmentDuration: // :627-630
		return pushf(im.StatusConstraintError, "SegmentDuration %d", o.SegmentDuration)
	case o.ChunkDuration > o.SegmentDuration/2: // :638
		return pushf(im.StatusConstraintError, "ChunkDuration %d", o.ChunkDuration)
	case !p.HasFeature("METADATA") && o.MetadataEnabled != nil: // :644-646
		return pushf(im.StatusInvalidCommand, "MetadataEnabled without METADATA")
	}
	return nil
}

// validateStreams is chip's ValidateStreamParameters (:665-879): the
// stream lists or the deprecated ids, never both and one at least; the
// lists free of duplicate names and ids and naming allocated streams; a
// null deprecated id selects the usage's stream (INVALID_IN_STATE without
// one), an id must be allocated (InvalidStream). It returns the options to
// store: the deprecated ids become one-stream lists named "video" and
// "audio" (:822-872), and an empty list is stored absent
// (push-av-stream-transport-storage.h:436-462, :501-511, :545-556 at
// 6170af84: TransportOptionsStorage copies a list stream by stream and
// clears one with no stream).
func (p *PushTransport) validateStreams(o TransportOptions) (TransportOptions, error) {
	out := o
	if out.VideoStreams != nil && len(*out.VideoStreams) == 0 {
		out.VideoStreams = nil
	}
	if out.AudioStreams != nil && len(*out.AudioStreams) == 0 {
		out.AudioStreams = nil
	}
	hasNew := (o.VideoStreams != nil && len(*o.VideoStreams) > 0) || (o.AudioStreams != nil && len(*o.AudioStreams) > 0) // :675-677
	hasOld := o.VideoStreamId != nil || o.AudioStreamId != nil
	switch {
	case hasNew && hasOld: // :681
		return out, pushf(im.StatusInvalidCommand, "the stream lists and the stream ids together")
	case !hasNew && !hasOld: // :684
		return out, pushf(im.StatusInvalidCommand, "no stream")
	}
	if hasNew {
		return out, p.validateStreamLists(o)
	}
	if o.VideoStreamId != nil {
		id, err := p.oldStream(o.StreamUsage, *o.VideoStreamId, true)
		if err != nil {
			return out, err
		}
		v := spec.ValueOf(id)
		out.VideoStreamId = &v
		out.VideoStreams = &[]padef.VideoStreamStruct{{VideoStreamName: "video", VideoStreamId: id}} // :823-829
	}
	if o.AudioStreamId != nil {
		id, err := p.oldStream(o.StreamUsage, *o.AudioStreamId, false)
		if err != nil {
			return out, err
		}
		v := spec.ValueOf(id)
		out.AudioStreamId = &v
		out.AudioStreams = &[]padef.AudioStreamStruct{{AudioStreamName: "audio", AudioStreamId: id}} // :866-872
	}
	return out, nil
}

// validateStreamLists is the list part of ValidateStreamParameters
// (:686-782): no duplicate names or ids, the lists' streams allocated, an
// audio name no video stream's, one stream at least.
func (p *PushTransport) validateStreamLists(o TransportOptions) error {
	videoNames := map[string]bool{}
	count := 0
	if o.VideoStreams != nil {
		ids := map[uint16]bool{}
		for _, s := range *o.VideoStreams {
			videoNames[s.VideoStreamName], ids[s.VideoStreamId] = true, true
		}
		if len(videoNames) != len(*o.VideoStreams) || len(ids) != len(*o.VideoStreams) { // :706-713
			return pushErr(padef.StatusCodeDuplicateStreamValues, "a duplicate video stream name or id")
		}
		for _, s := range *o.VideoStreams {
			if !p.view.allocated(s.VideoStreamId, true) { // :718-723, SetVideoStream
				return pushf(im.StatusInvalidCommand, "video stream %d is not allocated", s.VideoStreamId)
			}
		}
		count += len(*o.VideoStreams)
	}
	if o.AudioStreams != nil {
		names, ids := map[string]bool{}, map[uint16]bool{}
		for _, s := range *o.AudioStreams {
			names[s.AudioStreamName], ids[s.AudioStreamId] = true, true
		}
		if len(names) != len(*o.AudioStreams) || len(ids) != len(*o.AudioStreams) { // :743-750
			return pushErr(padef.StatusCodeDuplicateStreamValues, "a duplicate audio stream name or id")
		}
		for _, s := range *o.AudioStreams {
			if videoNames[s.AudioStreamName] { // :756-764
				return pushErr(padef.StatusCodeInvalidStream, "audio stream name %q is a video stream's", s.AudioStreamName)
			}
			if !p.view.allocated(s.AudioStreamId, false) { // :766-771, SetAudioStream
				return pushf(im.StatusInvalidCommand, "audio stream %d is not allocated", s.AudioStreamId)
			}
		}
		count += len(*o.AudioStreams)
	}
	if count == 0 { // :776-781
		return pushErr(padef.StatusCodeInvalidStream, "no stream")
	}
	return nil
}

// oldStream resolves a deprecated stream id: null is the camera app's
// SelectVideoStream/SelectAudioStream, the first allocated stream of the
// usage (push-av-stream-manager.cpp:474-528), any refusal INVALID_IN_STATE
// (:794-802, :837-845); an id is SetVideoStream/SetAudioStream, an
// allocated stream (:564-605), InvalidStream otherwise (:807-818,
// :850-861).
func (p *PushTransport) oldStream(usage padef.StreamUsageEnum, id spec.Nullable[uint16], video bool) (uint16, error) {
	if id.Null {
		if video {
			for _, s := range p.view.videos() {
				if uint8(s.StreamUsage) == uint8(usage) {
					return s.VideoStreamId, nil
				}
			}
		} else {
			for _, s := range p.view.audios() {
				if uint8(s.StreamUsage) == uint8(usage) {
					return s.AudioStreamId, nil
				}
			}
		}
		return 0, pushf(im.StatusInvalidInState, "no allocated stream of StreamUsage %d", usage)
	}
	if !p.view.allocated(id.Value, video) {
		return 0, pushErr(padef.StatusCodeInvalidStream, "stream %d is not allocated", id.Value)
	}
	return id.Value, nil
}

func pushRequest[T any](fields any, name string) (T, error) {
	req, ok := spec.RequestOf[T](fields)
	if !ok {
		return req, pushf(im.StatusInvalidCommand, "%s payload %T", name, fields)
	}
	return req, nil
}

// credentials is chip's TLS endpoint check of AllocatePushTransport
// (:920-972): the accessing fabric's provisioned endpoint with a client
// certificate, and — with the certificate server — its root and client
// certificates; InvalidTLSEndpoint otherwise, and always without the
// endpoint server (:966-972).
func (p *PushTransport) credentials(fabric uint8, endpointID uint16) (TLSCredentials, error) {
	p.mu.Lock()
	endpoints, certificates := p.tlsEP, p.tlsCerts
	p.mu.Unlock()
	if endpoints == nil {
		return TLSCredentials{}, pushErr(padef.StatusCodeInvalidTlsEndpoint, "no TLS client management")
	}
	ep, ok := endpoints.ProvisionedEndpoint(fabric, endpointID)
	if !ok || ep.Ccdid.Null { // :924-933
		return TLSCredentials{}, pushErr(padef.StatusCodeInvalidTlsEndpoint, "TLS endpoint %d is not provisioned with a client certificate", endpointID)
	}
	cred := TLSCredentials{Endpoint: ep}
	if certificates == nil { // :951-954
		return cred, nil
	}
	client, okc := certificates.ClientCertificate(fabric, ep.Ccdid.Value) // :944-945
	root, okr := certificates.RootCertificate(fabric, ep.Caid)            // :946-947
	if !okc || !okr {
		return TLSCredentials{}, pushErr(padef.StatusCodeInvalidTlsEndpoint, "TLS endpoint %d's certificates are not provisioned", endpointID)
	}
	if root.Certificate != nil {
		cred.RootCertificate = *root.Certificate
	}
	if client.ClientCertificate != nil && !client.ClientCertificate.Null {
		cred.ClientCertificate = client.ClientCertificate.Value
	}
	if client.IntermediateCertificates != nil {
		cred.IntermediateCertificates = *client.IntermediateCertificates
	}
	return cred, nil
}

// checkZones is the motion-zone part of AllocatePushTransport
// (:1032-1098): no zone twice and one null zone at most
// (ALREADY_EXISTS), the list within MaxZones (DYNAMIC_CONSTRAINT_ERROR),
// each zone a zone of the camera (InvalidZone).
func (p *PushTransport) checkZones(t padef.TransportTriggerOptionsStruct) error {
	if t.TriggerType != padef.TransportTriggerTypeMotion || t.MotionZones == nil || t.MotionZones.Null {
		return nil
	}
	zones := t.MotionZones.Value
	seen, null := map[uint16]bool{}, false
	for _, z := range zones {
		if z.Zone.Null {
			if null { // :1063-1068
				return pushf(im.StatusAlreadyExists, "a second null zone")
			}
			null = true
			continue
		}
		if seen[z.Zone.Value] { // :1052-1057
			return pushf(im.StatusAlreadyExists, "zone %d twice", z.Zone.Value)
		}
		seen[z.Zone.Value] = true
	}
	var known []Zone
	maxZones := 0
	if p.cfg.Zones != nil {
		known, maxZones = p.cfg.Zones.ZoneList(), int(p.cfg.Zones.MaxZones())
	}
	if len(zones) > maxZones { // :1074-1078, push-av-stream-manager.cpp:549-562
		return pushf(im.StatusDynamicConstraintError, "%d motion zones", len(zones))
	}
	for _, z := range zones {
		if !z.Zone.Null && !slices.ContainsFunc(known, func(k Zone) bool { return k.ZoneId == z.Zone.Value }) { // :1082-1095
			return pushErr(padef.StatusCodeInvalidZone, "zone %d is not the camera's", z.Zone.Value)
		}
	}
	return nil
}

// segmentDuration is the camera app's ValidateSegmentDuration
// (push-av-stream-manager.cpp:397-434): the segment duration a multiple
// of the deprecated video stream's key-frame interval (InvalidOptions,
// Logic.cpp:1119-1132).
func (p *PushTransport) segmentDuration(o TransportOptions) error {
	if o.VideoStreamId == nil || o.VideoStreamId.Null {
		return nil
	}
	s, ok := p.view.video(o.VideoStreamId.Value)
	if seg := o.ContainerOptions.CmafContainerOptions; seg != nil {
		if !ok || s.KeyFrameInterval == 0 || seg.SegmentDuration%s.KeyFrameInterval != 0 {
			return pushErr(padef.StatusCodeInvalidOptions, "SegmentDuration %d and the key-frame interval", seg.SegmentDuration)
		}
	}
	return nil
}

// allocate carries out AllocatePushTransport. Mirrors chip
// HandleAllocatePushTransport (:880-1228).
func (p *PushTransport) allocate(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.AllocatePushTransportRequest](fields, "AllocatePushTransport")
	if err != nil {
		return nil, err
	}
	fabric := accessingFabricOf(ctx)
	p.mu.Lock()
	n := 0
	for _, c := range p.conns {
		if c.FabricIndex == fabric {
			n++
		}
	}
	p.mu.Unlock()
	if n >= p.cfg.MaxPerFabric { // :889-905
		return nil, pushf(im.StatusResourceExhausted, "%d transports", n)
	}
	o := req.TransportOptions
	if err := p.validateOptions(o); err != nil { // :910-917
		return nil, err
	}
	cred, err := p.credentials(fabric, o.TlsEndpointId) // :920-972
	if err != nil {
		return nil, err
	}
	if o.IngestMethod != padef.IngestMethodsCmafIngest || o.ContainerOptions.ContainerType != padef.ContainerFormatCmaf { // :974-995
		return nil, pushErr(padef.StatusCodeInvalidCombination, "the ingest method and container format")
	}
	if o.ContainerOptions.CmafContainerOptions != nil && !ValidPushURL(o.Url) { // :998-1012
		return nil, pushErr(padef.StatusCodeInvalidUrl, "URL %q", o.Url)
	}
	if !p.view.usageSupported(uint8(o.StreamUsage)) { // :1015-1022
		return nil, pushErr(padef.StatusCodeInvalidStreamUsage, "StreamUsage %d", o.StreamUsage)
	}
	if err := p.checkZones(o.TriggerOptions); err != nil {
		return nil, err
	}
	stored, err := p.validateStreams(o) // :1108-1115
	if err != nil {
		return nil, err
	}
	if o.ContainerOptions.CmafContainerOptions != nil { // :1117-1133
		if err := p.segmentDuration(stored); err != nil {
			return nil, err
		}
	}
	var firstVideo, firstAudio *uint16 // :1135-1155
	if o.VideoStreams != nil && len(*o.VideoStreams) > 0 {
		id := (*o.VideoStreams)[0].VideoStreamId
		firstVideo, stored.VideoStreamId = &id, nvalue(id)
	}
	if o.AudioStreams != nil && len(*o.AudioStreams) > 0 {
		id := (*o.AudioStreams)[0].AudioStreamId
		firstAudio, stored.AudioStreamId = &id, nvalue(id)
	}
	if err := p.port.ValidateBandwidthLimit(ctx, o.StreamUsage, firstVideo, firstAudio); err != nil { // :1157-1165
		return nil, portError("ValidateBandwidthLimit", err)
	}
	// ValidateMaxPreRollLength (push-av-stream-manager.cpp:436-472): a
	// non-zero pre-roll at least the key-frame interval.
	if stored.VideoStreamId != nil && !stored.VideoStreamId.Null && o.TriggerOptions.MaxPreRollLen != nil && *o.TriggerOptions.MaxPreRollLen != 0 { // :1167-1181
		s, ok := p.view.video(stored.VideoStreamId.Value)
		if !ok || *o.TriggerOptions.MaxPreRollLen < s.KeyFrameInterval {
			return nil, pushErr(padef.StatusCodeInvalidPreRollLength, "MaxPreRollLen %d", *o.TriggerOptions.MaxPreRollLen)
		}
	}
	id, ok := p.generateID() // :1183-1190
	if !ok {
		return nil, pushf(im.StatusResourceExhausted, "every connection id is in use")
	}
	if err := p.port.AllocatePushTransport(ctx, id, fabric, stored, cred); err != nil { // :1193-1194
		return nil, portError("AllocatePushTransport", err)
	}
	opts := stored
	conn := TransportConfiguration{ConnectionId: id, TransportStatus: padef.TransportStatusInactive, TransportOptions: &opts, FabricIndex: fabric} // :1198-1200
	if err := p.commitAllocation(ctx, conn, o.ExpiryTime); err != nil {
		return nil, err
	}
	return padef.AllocatePushTransportResponse{TransportConfiguration: conn}, nil
}

// commitAllocation stores an allocated connection and arms its expiry
// (:1198-1217).
func (p *PushTransport) commitAllocation(ctx context.Context, conn TransportConfiguration, expirySeconds *uint32) error {
	if err := p.change(func(next []TransportConfiguration) ([]TransportConfiguration, bool) {
		return append(next, conn.Clone()), true
	}); err != nil {
		return commitError(err)
	}
	if expirySeconds != nil {
		p.mu.Lock()
		p.scheduleLocked(ctx, conn.ConnectionId, time.Duration(*expirySeconds)*time.Second)
		p.mu.Unlock()
	}
	return nil
}

func nvalue(id uint16) *spec.Nullable[uint16] {
	v := spec.ValueOf(id)
	return &v
}

// generateID is chip's GenerateConnectionID (:247-262): the next id after
// the last one handed out that no connection holds, from 1, wrapping
// within 0..65534.
func (p *PushTransport) generateID() (uint16, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range uint32(maxConnectionID) {
		cand := uint16((uint32(p.lastID) + i + 1) % uint32(maxConnectionID))
		if p.indexLocked(cand, 0, true) < 0 {
			p.lastID = cand
			return cand, true
		}
	}
	return 0, false
}

// scheduleLocked arms the expiry of a connection. chip's
// ScheduleTransportDeallocate (:357-385) takes ExpiryTime as the seconds
// until the deallocation (:1216); the timer runs on the server's clock.
func (p *PushTransport) scheduleLocked(ctx context.Context, id uint16, after time.Duration) {
	if e, ok := p.timers[id]; ok && e.timer != nil {
		e.timer.Stop()
	}
	e := &expiry{at: p.clock.Now().Add(after), ctx: context.WithoutCancel(ctx)}
	e.timer = p.clock.AfterFunc(after, func() { p.expire(id, e) })
	p.timers[id] = e
}

// expire is chip's PushAVStreamTransportDeallocateCallback (:262-292):
// the port deallocates, and on success the connection leaves. An expiry
// a quiesce, a removal or a later schedule replaced does nothing.
func (p *PushTransport) expire(id uint16, e *expiry) {
	p.op.Lock()
	defer p.op.Unlock()
	p.mu.Lock()
	current := p.timers[id] == e && e.timer != nil
	p.mu.Unlock()
	if !current {
		return
	}
	if err := p.port.DeallocatePushTransport(e.ctx, id); err != nil {
		return
	}
	_ = p.remove(id) // a shorter table the store held before cannot be refused
}

// remove drops a connection and its expiry; the caller holds op.
func (p *PushTransport) remove(id uint16) error {
	p.mu.Lock()
	if e, ok := p.timers[id]; ok { // RemoveTimerAppState, :188-202
		if e.timer != nil {
			e.timer.Stop()
		}
		delete(p.timers, id)
	}
	p.mu.Unlock()
	return p.change(func(next []TransportConfiguration) ([]TransportConfiguration, bool) {
		i := slices.IndexFunc(next, func(c TransportConfiguration) bool { return c.ConnectionId == id })
		if i < 0 {
			return next, false
		}
		return slices.Delete(next, i, i+1), true
	})
}

// deallocate carries out DeallocatePushTransport. Mirrors chip
// HandleDeallocatePushTransport (:1229-1276).
func (p *PushTransport) deallocate(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.DeallocatePushTransportRequest](fields, "DeallocatePushTransport")
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	found := p.indexLocked(req.ConnectionId, accessingFabricOf(ctx), false) >= 0
	p.mu.Unlock()
	switch {
	case !found: // :1245
		return nil, pushf(im.StatusNotFound, "connection %d not found", req.ConnectionId)
	case p.port.TransportBusy(req.ConnectionId): // :1250-1252
		return nil, pushf(im.StatusBusy, "connection %d is busy", req.ConnectionId)
	}
	if err := p.port.DeallocatePushTransport(ctx, req.ConnectionId); err != nil { // :1256-1273
		return nil, portError("DeallocatePushTransport", err)
	}
	if err := p.remove(req.ConnectionId); err != nil {
		return nil, commitError(err)
	}
	return nil, nil
}

// modify carries out ModifyPushTransport. Mirrors chip
// HandleModifyPushTransport (:1277-1360): the options' validation, the
// fabric's connection, not busy, the streams; then the port, and the
// connection takes the options.
func (p *PushTransport) modify(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.ModifyPushTransportRequest](fields, "ModifyPushTransport")
	if err != nil {
		return nil, err
	}
	if err := p.validateOptions(req.TransportOptions); err != nil { // :1290-1297
		return nil, err
	}
	fabric := accessingFabricOf(ctx)
	p.mu.Lock()
	found := p.indexLocked(req.ConnectionId, fabric, false) >= 0
	p.mu.Unlock()
	switch {
	case !found: // :1306
		return nil, pushf(im.StatusNotFound, "connection %d not found", req.ConnectionId)
	case p.port.TransportBusy(req.ConnectionId): // :1311-1313
		return nil, pushf(im.StatusBusy, "connection %d is busy", req.ConnectionId)
	}
	stored, err := p.validateStreams(req.TransportOptions) // :1326-1332
	if err != nil {
		return nil, err
	}
	if err := p.port.ModifyPushTransport(ctx, req.ConnectionId, stored); err != nil { // :1334
		return nil, portError("ModifyPushTransport", err)
	}
	if err := p.change(func(next []TransportConfiguration) ([]TransportConfiguration, bool) {
		i := slices.IndexFunc(next, func(c TransportConfiguration) bool { // :1337-1352
			return c.ConnectionId == req.ConnectionId && c.FabricIndex == fabric
		})
		if i < 0 {
			return next, false
		}
		opts := stored.Clone()
		next[i].TransportOptions = &opts
		return next, true
	}); err != nil {
		return nil, commitError(err)
	}
	return nil, nil
}

// setStatus carries out SetTransportStatus. Mirrors chip
// HandleSetTransportStatus (:1361-1439): a null id is every connection of
// the fabric.
func (p *PushTransport) setStatus(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.SetTransportStatusRequest](fields, "SetTransportStatus")
	if err != nil {
		return nil, err
	}
	if req.TransportStatus > maxTransportStatus { // :1374-1377
		return nil, pushf(im.StatusConstraintError, "TransportStatus %d", req.TransportStatus)
	}
	fabric := accessingFabricOf(ctx)
	var ids []uint16
	p.mu.Lock()
	if req.ConnectionId.Null { // :1381-1390
		for _, c := range p.conns {
			if c.FabricIndex == fabric {
				ids = append(ids, c.ConnectionId)
			}
		}
	} else if p.indexLocked(req.ConnectionId.Value, fabric, false) >= 0 {
		ids = []uint16{req.ConnectionId.Value}
	}
	p.mu.Unlock()
	if !req.ConnectionId.Null && len(ids) == 0 { // :1398-1402
		return nil, pushf(im.StatusNotFound, "connection %d not found", req.ConnectionId.Value)
	}
	if err := p.port.SetTransportStatus(ctx, slices.Clone(ids), req.TransportStatus); err != nil { // :1406
		return nil, portError("SetTransportStatus", err)
	}
	if err := p.change(func(next []TransportConfiguration) ([]TransportConfiguration, bool) {
		for i := range next { // :1410-1419
			if slices.Contains(ids, next[i].ConnectionId) {
				next[i].TransportStatus = req.TransportStatus
			}
		}
		return next, true
	}); err != nil {
		return nil, commitError(err)
	}
	return nil, nil
}

// trigger carries out ManuallyTriggerTransport. Mirrors chip
// HandleManuallyTriggerTransport (:1440-1542): the reason and the time
// control, the fabric's connection, the privacy modes of its usage, not
// busy, an active transport (InvalidTransportStatus), not continuous
// (InvalidTriggerType), a Command trigger needs a time control
// (DYNAMIC_CONSTRAINT_ERROR); without one the transport's motion time
// control goes to the port.
func (p *PushTransport) trigger(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.ManuallyTriggerTransportRequest](fields, "ManuallyTriggerTransport")
	if err != nil {
		return nil, err
	}
	if req.ActivationReason > maxActivationReason { // :1454-1458
		return nil, pushf(im.StatusConstraintError, "ActivationReason %d", req.ActivationReason)
	}
	if tc := req.TimeControl; tc != nil {
		if tc.InitialDuration < 1 { // :1463-1469
			return nil, pushf(im.StatusConstraintError, "InitialDuration 0")
		}
		if tc.MaxDuration < 1 { // :1470-1476
			return nil, pushf(im.StatusConstraintError, "MaxDuration 0")
		}
	}
	p.mu.Lock()
	var conn TransportConfiguration
	i := p.indexLocked(req.ConnectionId, accessingFabricOf(ctx), false)
	if i >= 0 {
		conn = p.conns[i]
	}
	p.mu.Unlock()
	if i < 0 || conn.TransportOptions == nil { // :1481-1487
		return nil, pushf(im.StatusNotFound, "connection %d not found", req.ConnectionId)
	}
	opts := conn.TransportOptions
	if err := p.view.checkPrivacy("PushAvStreamTransport", uint8(opts.StreamUsage)); err != nil { // :1489-1494
		return nil, err
	}
	switch {
	case p.port.TransportBusy(req.ConnectionId): // :1496-1502
		return nil, pushf(im.StatusBusy, "connection %d is busy", req.ConnectionId)
	case conn.TransportStatus == padef.TransportStatusInactive: // :1504-1510
		return nil, pushErr(padef.StatusCodeInvalidTransportStatus, "connection %d is inactive", req.ConnectionId)
	case opts.TriggerOptions.TriggerType == padef.TransportTriggerTypeContinuous: // :1512-1518
		return nil, pushErr(padef.StatusCodeInvalidTriggerType, "connection %d is continuous", req.ConnectionId)
	case opts.TriggerOptions.TriggerType == padef.TransportTriggerTypeCommand && req.TimeControl == nil: // :1520-1526
		return nil, pushf(im.StatusDynamicConstraintError, "a command trigger without TimeControl")
	}
	tc := req.TimeControl
	if tc == nil { // :1528-1531
		tc = opts.TriggerOptions.MotionTimeControl
	}
	if err := p.port.ManuallyTriggerTransport(ctx, req.ConnectionId, req.ActivationReason, tc); err != nil { // :1533
		return nil, portError("ManuallyTriggerTransport", err)
	}
	return nil, nil
}

// find carries out FindTransport. Mirrors chip HandleFindTransport
// (:1543-1603): a null id answers the fabric's connections, NOT_FOUND
// without any (:1558-1565, :1590-1594).
func (p *PushTransport) find(ctx context.Context, fields any) (any, error) {
	req, err := pushRequest[padef.FindTransportRequest](fields, "FindTransport")
	if err != nil {
		return nil, err
	}
	fabric := accessingFabricOf(ctx)
	var out []TransportConfiguration
	for _, c := range p.Connections() {
		if c.FabricIndex == fabric && (req.ConnectionId.Null || c.ConnectionId == req.ConnectionId.Value) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, pushf(im.StatusNotFound, "no connection")
	}
	return padef.FindTransportResponse{TransportConfigurations: out}, nil
}

// TransportStarted implements [padef.Host]. Mirrors chip
// NotifyTransportStarted (:1978-2015) and GeneratePushTransportBeginEvent
// (:1903-1926): the container type, and for CMAF the port's session
// number.
func (p *PushTransport) TransportStarted(connectionID uint16, triggerType padef.TransportTriggerTypeEnum, activationReason *padef.TriggerActivationReasonEnum) error {
	container, number, err := p.eventFields(connectionID)
	if err != nil {
		return err
	}
	return p.srv.EmitEvent(padef.EventPushTransportBegin, padef.PushTransportBeginEvent{
		ConnectionId: connectionID, TriggerType: triggerType, ActivationReason: activationReason,
		ContainerType: &container, CmafSessionNumber: number,
	})
}

// TransportStopped implements [padef.Host]. Mirrors chip
// NotifyTransportStopped (:2016-2031) and GeneratePushTransportEndEvent
// (:1927-1976).
func (p *PushTransport) TransportStopped(connectionID uint16) error {
	container, number, err := p.eventFields(connectionID)
	if err != nil {
		return err
	}
	return p.srv.EmitEvent(padef.EventPushTransportEnd, padef.PushTransportEndEvent{
		ConnectionId: connectionID, ContainerType: &container, CmafSessionNumber: number,
	})
}

// eventFields are an event's container type and CMAF session number; an
// unknown connection is NOT_FOUND (:1985-1990, :2022-2027).
func (p *PushTransport) eventFields(connectionID uint16) (padef.ContainerFormatEnum, *uint64, error) {
	p.mu.Lock()
	i := p.indexLocked(connectionID, 0, true)
	var conn TransportConfiguration
	if i >= 0 {
		conn = p.conns[i]
	}
	p.mu.Unlock()
	if i < 0 || conn.TransportOptions == nil {
		return 0, nil, pushf(im.StatusNotFound, "connection %d not found", connectionID)
	}
	container := conn.TransportOptions.ContainerOptions.ContainerType
	var number *uint64
	if container == padef.ContainerFormatCmaf {
		if n, ok := p.port.CMAFSessionNumber(connectionID); ok {
			number = &n
		}
	}
	return container, number, nil
}

// accessingFabricOf is the invoking fabric; 0 without one.
func accessingFabricOf(ctx context.Context) uint8 {
	_, fabric, _ := im.LookupFabricFilter(ctx)
	return fabric
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (p *PushTransport) MatterDataVersion() uint32 { return p.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier]; the
// host persists CurrentConnections from it.
func (p *PushTransport) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return p.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute for a local caller: CurrentConnections
// holds every fabric's connections.
func (p *PushTransport) MatterRead(attrID uint32) (any, bool) { return p.srv.MatterRead(attrID) }

// MatterReadFiltered implements [contract.FabricScopedReader]:
// CurrentConnections is fabric-sensitive ("R S V"), so a peer's read sees
// its own fabric's connections only.
func (p *PushTransport) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if attrID != padef.AttrCurrentConnections || !remote {
		return p.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
	}
	out := spec.List[TransportConfiguration]{}
	for _, c := range p.Connections() {
		if fabric != 0 && c.FabricIndex == fabric {
			out = append(out, c)
		}
	}
	return out, true
}

// MatterWrite answers every write by the definition: the cluster has no
// writable attribute.
func (p *PushTransport) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return p.srv.MatterWrite(ctx, attrID, value)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (p *PushTransport) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return p.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (p *PushTransport) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &p.SelfHeld, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (p *PushTransport) SetMatterEventEmitter(emitter contract.EventEmitter) {
	p.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly, for every server of the new topology. A server
// [PushTransport.MatterQuiesce] stopped comes back here, and each kept
// expiry is armed again for the time left (review II, R2-P6-conc-1606).
func (p *PushTransport) SetEndpoint(endpoint uint16) {
	// The store is stamped first: an expiry re-armed below that fires at
	// once publishes on the new endpoint (review II, appinfra round 3).
	p.srv.SetEndpoint(endpoint) // the store emits on it (EmitEvent, the typed Record<Event>)
	p.mu.Lock()
	p.endpoint = endpoint
	if p.quiesced {
		p.quiesced = false
		now := p.clock.Now()
		for id, e := range p.timers {
			p.scheduleLocked(e.ctx, id, max(e.at.Sub(now), 0))
		}
	}
	p.mu.Unlock()
}
