// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/camera"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/store"
)

// The controller's own endpoints — what it serves to the nodes of its
// fabric under its one operational identity, RootNodeID.
//
// matter.js's controller is a ServerNode with the ControllerBehavior
// (matterjs-server ControllerNode.ts:33 at 0bd06f2; matter.js
// ControllerBehavior.ts:43-50, Peers.ts:109,:549-552,:574-579 at
// 862ad75f): the endpoints it adds — the OTA provider
// (ControllerNode.ts:140-141), the camera controller with its
// WebRtcTransportRequestor (:147) — are served by the node's
// InteractionServer on the same sessions the controller opens, and a
// camera that stores the invoker as its PeerNodeID calls the controller
// back on them. This module's controller composes the same tree from the
// layers below the roles (endpoint, cluster/core, cluster/camera) and
// installs it into the client stack's Interaction Model server
// (client/interaction, [interaction.Subscriptions.Serve]); the endpoints
// are bare ([endpoint.Endpoint.IsBare]), listed here by hand. There is no
// second node, no second socket and no second identity (ADR 0026
// principles 1, 2 and 12; concept of issue 410).

// CameraConfig turns on the controller side of Matter cameras: endpoint
// [CameraControllerEndpoint] with the Camera Controller device type and a
// WebRtcTransportRequestor the cameras of the fabric reach (matter.js
// camera-controller.ts:133 mandates the requestor; the entry that lets a
// camera reach it mirrors WebRtcTransportRequestorServer.ts:45-94). The
// stream management over it is the camera service (controller/camera).
type CameraConfig struct{}

// Endpoint numbers of the controller's tree. The root is 0; the camera
// controller sits at 1 and the OTA provider at 2, as matterjs-server adds
// them (ControllerNode.ts:140-147, the provider first when enabled, the
// camera controller after it).
const (
	CameraControllerEndpoint uint16 = 1
	OTAProviderEndpoint      uint16 = 2
)

// CameraSignalKind names a signal the camera sent to the requestor.
type CameraSignalKind uint8

// The requestor's signals (WebRtcTransportRequestor's commands: Offer,
// Answer, ICECandidates, End).
const (
	CameraSignalOffer CameraSignalKind = iota + 1
	CameraSignalAnswer
	CameraSignalICECandidates
	CameraSignalEnd
)

// CameraSignal is one command a camera invoked on the controller's
// WebRtcTransportRequestor, with the session it names.
type CameraSignal struct {
	Kind    CameraSignalKind
	Session camera.WebRTCSession
	// SDP is the offer's or answer's SDP.
	SDP string
	// Offer carries the Offer command's remaining fields.
	Offer wrdef.OfferRequest
	// Candidates are the ICECandidates command's candidates.
	Candidates []camera.ICECandidate
	// Reason is the End command's reason.
	Reason uint8
}

// served is the controller's own endpoint tree and what it hangs on it.
type served struct {
	topology   *endpoint.Topology
	dispatcher *endpoint.TopologyDispatcher
	requestor  *camera.Requestor

	mu        sync.Mutex
	listeners map[uint64]func(CameraSignal)
	nextID    uint64
	withdraw  func(context.Context) error
}

// servesEndpoints reports whether cfg composes a tree at all.
func servesEndpoints(cfg Config) bool { return cfg.Camera != nil }

// serveEndpoints composes the tree cfg asks for and installs it into the
// client stack's Interaction Model server; nothing is composed without
// [Config.Camera].
func (c *Controller) serveEndpoints(ctx context.Context, cfg Config, log *slog.Logger) error {
	if !servesEndpoints(cfg) {
		return nil
	}
	if err := c.composeTree(cfg, log); err != nil {
		return err
	}
	return c.advertiseOperational(ctx, cfg)
}

// composeTree builds the tree and installs it.
func (c *Controller) composeTree(cfg Config, log *slog.Logger) error {
	s := &served{listeners: map[uint64]func(CameraSignal){}}
	var endpoints []*endpoint.Endpoint
	parts := []uint16{}
	if cfg.Camera != nil {
		requestor, err := camera.NewRequestor(camera.RequestorConfig{Port: cameraPort{s: s}})
		if err != nil {
			return err
		}
		s.requestor = requestor
		ep, err := bareEndpoint(CameraControllerEndpoint, schema.DeviceTypeID(0x0147), requestor)
		if err != nil {
			return err
		}
		endpoints = append(endpoints, ep)
		parts = append(parts, CameraControllerEndpoint)
	}
	root, err := bareEndpoint(0, schema.DeviceTypeID(0x0016))
	if err != nil {
		return err
	}
	rootDescriptor(root).SetPartsList(parts)
	s.topology = &endpoint.Topology{Endpoints: append([]*endpoint.Endpoint{root}, endpoints...), VendorID: c.fabric.RootVendorID}
	s.dispatcher = endpoint.NewTopologyDispatcher(s.topology)
	s.dispatcher.SetACLLister(servedACL{c: c, s: s})
	c.subs.Serve(interaction.ServerConfig{Dispatcher: s.dispatcher, Qualities: servedQualities{}})
	c.served = s
	log.Info("controller.serve.endpoints", slog.Int("endpoints", len(s.topology.Endpoints)), slog.Bool("camera", cfg.Camera != nil))
	return nil
}

// bareEndpoint lists one endpoint of the tree: its Descriptor (device
// type at the snapshot's revision, the server list of what it serves) and
// the servers themselves, published as its attached set.
func bareEndpoint(id uint16, deviceType schema.DeviceTypeID, servers ...contract.ClusterServer) (*endpoint.Endpoint, error) {
	rev, ok := schema.DeviceTypeRevision(deviceType)
	if !ok {
		return nil, fmt.Errorf("controller: device type 0x%04X is not in the snapshot", uint32(deviceType))
	}
	serverList := []uint32{descdef.ClusterID}
	for _, srv := range servers {
		serverList = append(serverList, srv.MatterClusterID())
	}
	desc, err := mattercore.NewDescriptor([]mattercore.DeviceTypeStruct{{DeviceType: uint32(deviceType), Revision: rev}}, serverList, nil, nil)
	if err != nil {
		return nil, err
	}
	ep := &endpoint.Endpoint{ID: id, DeviceType: uint16(deviceType), Reachable: true} //nolint:gosec // a device type id of the snapshot
	ep.PublishClusterServers(append([]contract.ClusterServer{desc}, servers...))
	return ep, nil
}

func rootDescriptor(root *endpoint.Endpoint) *mattercore.Descriptor {
	for _, srv := range root.AttachedClusterServers() {
		if d, ok := srv.(*mattercore.Descriptor); ok {
			return d
		}
	}
	return nil
}

// servedACL is the ACL the tree is served under: on our fabric, the
// administrator is RootNodeID — ourselves — and every node of the fabric
// may operate the requestor over CASE, the entry matter.js's requestor
// adds for itself (WebRtcTransportRequestorServer.ts:81-91: Operate, CASE,
// no subject, the requestor's endpoint and cluster) and node/ adds on the
// device side (node/webrtc_requestor_acl.go). Another fabric holds nothing.
type servedACL struct {
	c *Controller
	s *served
}

func (a servedACL) ListACL(_ context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	if fabricIndex != a.c.fabricIndex() {
		return nil, nil
	}
	entries := []store.ACLEntry{{
		FabricIndex: fabricIndex, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{a.c.fabric.RootNodeID},
	}}
	if a.s.requestor != nil {
		cluster, ep := wrdef.ClusterID, CameraControllerEndpoint
		entries = append(entries, store.ACLEntry{
			FabricIndex: fabricIndex, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeCASE,
			Targets: []store.ACLTarget{{Cluster: &cluster, Endpoint: &ep}},
		})
	}
	return entries, nil
}

// servedQualities answers the wildcard-read qualities from the generated
// definitions, as the bridge does (bridge/element_qualities.go).
type servedQualities struct{}

func (servedQualities) DiagnosticsCluster(uint32) bool { return false }

func (servedQualities) AttributeFixed(cluster, attribute uint32) bool {
	def := spec.Lookup(cluster)
	if def == nil {
		return false
	}
	a := def.Attribute(attribute)
	return a != nil && a.Quality.Fixed
}

// cameraPort is the requestor's port: every signal fans out to the
// listeners of [Controller.OnCameraSignal].
type cameraPort struct{ s *served }

func (p cameraPort) Offer(_ context.Context, session camera.WebRTCSession, req wrdef.OfferRequest) {
	p.s.emit(CameraSignal{Kind: CameraSignalOffer, Session: session, SDP: req.Sdp, Offer: req})
}

func (p cameraPort) Answer(_ context.Context, session camera.WebRTCSession, sdp string) {
	p.s.emit(CameraSignal{Kind: CameraSignalAnswer, Session: session, SDP: sdp})
}

func (p cameraPort) IceCandidates(_ context.Context, session camera.WebRTCSession, candidates []camera.ICECandidate) {
	p.s.emit(CameraSignal{Kind: CameraSignalICECandidates, Session: session, Candidates: candidates})
}

func (p cameraPort) End(_ context.Context, session camera.WebRTCSession, reason uint8) {
	p.s.emit(CameraSignal{Kind: CameraSignalEnd, Session: session, Reason: reason})
}

func (s *served) emit(sig CameraSignal) {
	s.mu.Lock()
	fns := make([]func(CameraSignal), 0, len(s.listeners))
	for _, fn := range s.listeners {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(sig)
	}
}

// CameraRequestor is the WebRtcTransportRequestor the controller serves
// at [CameraControllerEndpoint], nil without [Config.Camera].
func (c *Controller) CameraRequestor() *camera.Requestor {
	if c.served == nil {
		return nil
	}
	return c.served.requestor
}

// OnCameraSignal calls fn for every command a camera invokes on the
// requestor, on the serving goroutine; the camera service subscribes
// here. The returned function unsubscribes. Without [Config.Camera] fn is
// never called.
func (c *Controller) OnCameraSignal(fn func(CameraSignal)) (unsubscribe func()) {
	s := c.served
	if s == nil || fn == nil {
		return func() {}
	}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.listeners[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// ErrNoAdvertiser is an operational record asked for without an
// advertiser.
var ErrNoAdvertiser = errors.New("controller: no mDNS advertiser")

// advertiseOperational publishes the operational record of RootNodeID
// (`_matter._tcp`, Matter §4.3.1), so that a node of the fabric can open
// CASE to the controller when no session of ours is alive — chip's
// camera-app falls back to it (OperationalSessionSetup.cpp:119,:138-142
// at 6170af84). matter.js's ServerNode advertises it for every fabric it
// holds (OperationalMdnsAdvertisement); the controller holds one. The
// record is the bridge's (bridge.AnnounceFabric) with the controller's
// port, TCP roles and session intervals.
func (c *Controller) advertiseOperational(ctx context.Context, cfg Config) error {
	if c.served == nil {
		return nil
	}
	adv := c.openAdvertiser(cfg.Advertiser)
	f, ok := c.fabric.Lookup(c.fabric.AddressOf(c.fabric.RootNodeID))
	if !ok || f.Identity == nil {
		return errors.New("controller: the fabric's identity is not loaded")
	}
	opCfg := mdns.OperationalServiceConfig{
		Logger:             c.log,
		CompressedFabricID: f.Identity.CompressedFabricID,
		NodeID:             c.fabric.RootNodeID,
		Port:               uint16(c.udp.LocalAddr().Port), //nolint:gosec // G115: a UDP port
		TCPClient:          cfg.TCP.Outgoing,
		TCPServer:          cfg.TCP.Incoming,
	}
	if s := cfg.Sessions; s != nil {
		opCfg.SessionIdleInterval = uint32(s.Idle.Milliseconds())               //nolint:gosec // G115: validated by Sessions.validate
		opCfg.SessionActiveInterval = uint32(s.Active.Milliseconds())           //nolint:gosec // G115: validated by Sessions.validate
		opCfg.SessionActiveThreshold = uint32(s.ActiveThreshold.Milliseconds()) //nolint:gosec // G115: validated by Sessions.validate
	}
	svc := mdns.BuildOperationalService(opCfg)
	if err := adv.Publish(ctx, svc); err != nil {
		return fmt.Errorf("controller: operational advertisement: %w", err)
	}
	c.served.withdraw = func(ctx context.Context) error {
		return adv.Withdraw(ctx, svc.InstanceName, mdns.ServiceTypeOperational)
	}
	c.log.Info("controller.serve.advertised", slog.String("instance", svc.InstanceName), slog.Int("port", int(opCfg.Port)))
	return nil
}
