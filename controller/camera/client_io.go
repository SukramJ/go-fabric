// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"slices"

	"github.com/SukramJ/go-fabric/clock"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
)

var _ = basicinformation.ClusterID

// avsmCommands are the AVSM command ids of the manager's command names.
var avsmCommands = map[string]uint32{
	cmdVideoStreamAllocate:      csdef.CmdVideoStreamAllocate,
	cmdVideoStreamDeallocate:    csdef.CmdVideoStreamDeallocate,
	cmdAudioStreamAllocate:      csdef.CmdAudioStreamAllocate,
	cmdAudioStreamDeallocate:    csdef.CmdAudioStreamDeallocate,
	cmdSnapshotStreamAllocate:   csdef.CmdSnapshotStreamAllocate,
	cmdSnapshotStreamDeallocate: csdef.CmdSnapshotStreamDeallocate,
	cmdCaptureSnapshot:          csdef.CmdCaptureSnapshot,
}

// providerCommands are the WebRtcTransportProvider command ids.
var providerCommands = map[string]uint32{
	callSolicitOffer:         wpdef.CmdSolicitOffer,
	callProvideOffer:         wpdef.CmdProvideOffer,
	callProvideAnswer:        wpdef.CmdProvideAnswer,
	callProvideIceCandidates: wpdef.CmdProvideIceCandidates,
	callEndSession:           wpdef.CmdEndSession,
}

// streamListOf is the attribute an allocate or deallocate changes, read
// again after the command: the camera answers before it reports the
// changed list. Mirrors matterjs-server
// packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:39-46 at
// 0bd06f2.
var streamListOf = map[string]uint32{
	cmdVideoStreamAllocate:      csdef.AttrAllocatedVideoStreams,
	cmdVideoStreamDeallocate:    csdef.AttrAllocatedVideoStreams,
	cmdAudioStreamAllocate:      csdef.AttrAllocatedAudioStreams,
	cmdAudioStreamDeallocate:    csdef.AttrAllocatedAudioStreams,
	cmdSnapshotStreamAllocate:   csdef.AttrAllocatedSnapshotStreams,
	cmdSnapshotStreamDeallocate: csdef.AttrAllocatedSnapshotStreams,
}

// clientIO is the manager's deviceIO, the session layer's providerNodes
// and the watch's watchIO over a controller's client nodes. Mirrors
// matterjs-server packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:152-283
// at 0bd06f2.
type clientIO struct {
	ctrl     *controller.Controller
	svc      *Service
	syncTime func(context.Context, *client.Node) error
	clk      clock.Clock
	log      *slog.Logger
	// send and reread reach the camera; nil is the client node's Invoke
	// and Read. A test of the routing replaces them.
	send   func(ctx context.Context, t target, cluster, command uint32, fields spec.Encodable) (any, error)
	reread func(ctx context.Context, t target, attr uint32) error
}

// call invokes one command on the camera and decodes the response.
func (io *clientIO) call(ctx context.Context, t target, cluster, command uint32, fields spec.Encodable) (any, error) {
	if io.send != nil {
		return io.send(ctx, t, cluster, command, fields)
	}
	n := io.node(t.nodeID)
	if n == nil {
		return nil, stackError("node 0x%016X is not in the registry", t.nodeID)
	}
	return invokeDecoded(ctx, n, t.endpointID, cluster, command, fields)
}

// node is the client node of a camera, nil when the registry holds none.
func (io *clientIO) node(nodeID uint64) *client.Node {
	return io.ctrl.Registry().GetByAddress(client.PeerAddress{FabricIndex: io.ctrl.Fabric().FabricIndex, NodeID: nodeID})
}

// cluster is a cluster of the camera's image, nil when absent.
func (io *clientIO) cluster(t target, id uint32) *client.Cluster {
	n := io.node(t.nodeID)
	if n == nil {
		return nil
	}
	ep := n.Endpoint(t.endpointID)
	if ep == nil {
		return nil
	}
	return ep.Cluster(id)
}

func (io *clientIO) readCameraState(_ context.Context, t target) (*cameraState, error) {
	c := io.cluster(t, csdef.ClusterID)
	if c == nil {
		return nil, nil
	}
	st, absent := csdef.StateFrom(c)
	return toCameraState(st, absent, csdef.Feature(c.FeatureMap())), nil
}

func (io *clientIO) missingCameraClusters(_ context.Context, t target) []uint32 {
	var missing []uint32
	if io.cluster(t, csdef.ClusterID) == nil {
		missing = append(missing, csdef.ClusterID)
	}
	if io.cluster(t, wpdef.ClusterID) == nil {
		missing = append(missing, wpdef.ClusterID)
	}
	return missing
}

// readWebRTCSessions takes the stream ids from the revision-2 lists and
// the deprecated revision-1 fields (§11.4.5.5). Mirrors matterjs-server
// packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:179-194 at
// 0bd06f2.
func (io *clientIO) readWebRTCSessions(_ context.Context, t target) ([]DeviceSession, bool) {
	c := io.cluster(t, wpdef.ClusterID)
	if c == nil {
		return nil, false
	}
	st, _ := wpdef.StateFrom(c)
	local := io.ctrl.Fabric().RootNodeID
	out := []DeviceSession{}
	for _, s := range st.CurrentSessions {
		video := resolveWebRTCSessionStreams(s.VideoStreams, s.VideoStreamId, nil)
		audio := resolveWebRTCSessionStreams(s.AudioStreams, s.AudioStreamId, nil)
		if video == nil {
			video = []uint16{}
		}
		if audio == nil {
			audio = []uint16{}
		}
		out = append(out, DeviceSession{ID: s.Id, PeerNodeID: s.PeerNodeId, PeerEndpoint: s.PeerEndpointId, StreamUsage: uint8(s.StreamUsage), VideoStreams: video, AudioStreams: audio, EstablishedByUs: s.PeerNodeId == local})
	}
	return out, true
}

// invoke sends an AVSM command and reads the changed stream list, or a
// provider command: an offer through the session layer, an EndSession
// dropping the local records. Mirrors matterjs-server
// packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:219-278 at
// 0bd06f2.
func (io *clientIO) invoke(ctx context.Context, call deviceCall) (any, error) {
	if call.cluster == clusterAVSM {
		resp, err := io.call(ctx, call.target, csdef.ClusterID, avsmCommands[call.command], call.fields)
		if err != nil {
			return nil, err
		}
		if attr, ok := streamListOf[call.command]; ok {
			io.refreshStreamList(ctx, call.target, attr)
		}
		return resp, nil
	}
	switch call.command {
	case callProvideOffer, callSolicitOffer:
		// The local requestor must hold the session: it answers NotFound
		// to the Answer and ICECandidates of a session it never stored.
		return io.svc.ps.establish(ctx, call.target, call.fields, call.establishing)
	case callEndSession:
		req, _ := call.fields.(wpdef.EndSessionRequest)
		id := req.WebRtcSessionId
		var resp any
		err := invokeEndSession(ctx, func(ctx context.Context) error {
			var err error
			resp, err = io.invokeProvider(ctx, call.target, call.command, call.fields)
			return err
		}, func(bool) { io.svc.ps.removeTracked(call.target, id) })
		return resp, err
	default:
		return io.invokeProvider(ctx, call.target, call.command, call.fields)
	}
}

// refreshStreamList reads a stream list now, fabric-filtered like the
// subscription; a failure is logged, the subscription catches up. Mirrors
// matterjs-server packages/ws-controller/src/camera/MatterCameraDeviceIo.ts:196-214
// at 0bd06f2.
func (io *clientIO) refreshStreamList(ctx context.Context, t target, attr uint32) {
	read := io.reread
	if read == nil {
		read = func(ctx context.Context, t target, attr uint32) error {
			n := io.node(t.nodeID)
			if n == nil {
				return stackError("node 0x%016X is not in the registry", t.nodeID)
			}
			var req client.ReadRequest
			req.Attributes = []im.ConcreteAttributePath{client.AttributeAt(t.endpointID, csdef.ClusterID, attr)}
			_, err := n.Read(ctx, req)
			return err
		}
	}
	if err := read(ctx, t, attr); err != nil {
		io.log.Warn("camera.read.stream_list_failed", slog.Uint64("node_id", t.nodeID), slog.Int("endpoint", int(t.endpointID)), slog.Any("err", err))
	}
}

func (io *clientIO) abandonSession(_ context.Context, t target, id uint16) error {
	io.svc.ps.abandon(t, id)
	return nil
}

// providerRevision is the provider's ClusterRevision; an endpoint without
// the cluster is an argument error.
func (io *clientIO) providerRevision(t target) (revision uint16, known bool, err error) {
	c := io.cluster(t, wpdef.ClusterID)
	if c == nil {
		return 0, false, invalidArguments("node 0x%016X endpoint %d has no WebRTC Transport Provider", t.nodeID, t.endpointID)
	}
	if !c.Ready() {
		return 0, false, nil
	}
	return c.Revision(), true, nil
}

func (io *clientIO) invokeProvider(ctx context.Context, t target, command string, fields spec.Encodable) (any, error) {
	return io.call(ctx, t, wpdef.ClusterID, providerCommands[command], fields)
}

// syncTime pushes this controller's time to the node: the host's push
// when Config.SyncTime is set, else SetUTCTime on the root endpoint.
func (io *clientIO) syncTimeOf(ctx context.Context, nodeID uint64) error {
	if io.syncTime != nil {
		n := io.node(nodeID)
		if n == nil {
			return stackError("node 0x%016X is not in the registry", nodeID)
		}
		return io.syncTime(ctx, n)
	}
	us := uint64(io.clk.Now().Sub(matterEpoch).Microseconds()) //nolint:gosec // G115: a clock after 2000
	_, err := io.call(ctx, target{nodeID: nodeID}, tsdef.ClusterID, tsdef.CmdSetUtcTime,
		tsdef.SetUtcTimeRequest{UtcTime: us, Granularity: tsdef.GranularityMillisecondsGranularity})
	return err
}

// invokeDecoded invokes one command and decodes its response by the
// command's generated response type.
func invokeDecoded(ctx context.Context, n *client.Node, endpoint uint16, cluster, command uint32, fields spec.Encodable) (any, error) {
	res, err := n.Invoke(ctx, client.CommandPath{Endpoint: endpoint, Cluster: cluster, Command: command}, fields, client.InvokeOptions{})
	if err != nil {
		return nil, err
	}
	if !res.HasFields {
		return nil, nil
	}
	switch {
	case cluster == csdef.ClusterID && res.Command == csdef.CmdVideoStreamAllocateResponse:
		return decodeAs[csdef.VideoStreamAllocateResponse](res.Fields)
	case cluster == csdef.ClusterID && res.Command == csdef.CmdAudioStreamAllocateResponse:
		return decodeAs[csdef.AudioStreamAllocateResponse](res.Fields)
	case cluster == csdef.ClusterID && res.Command == csdef.CmdSnapshotStreamAllocateResponse:
		return decodeAs[csdef.SnapshotStreamAllocateResponse](res.Fields)
	case cluster == csdef.ClusterID && res.Command == csdef.CmdCaptureSnapshotResponse:
		return decodeAs[csdef.CaptureSnapshotResponse](res.Fields)
	case cluster == wpdef.ClusterID && res.Command == wpdef.CmdProvideOfferResponse:
		return decodeAs[wpdef.ProvideOfferResponse](res.Fields)
	case cluster == wpdef.ClusterID && res.Command == wpdef.CmdSolicitOfferResponse:
		return decodeAs[wpdef.SolicitOfferResponse](res.Fields)
	}
	return nil, nil
}

// decodeAs decodes a response's fields as T.
func decodeAs[T any, P interface {
	*T
	DecodeTLV(spec.Node) error
}](n spec.Node) (any, error) {
	var v T
	if err := P(&v).DecodeTLV(n); err != nil {
		return nil, err
	}
	return v, nil
}

// requestor is the requestorTable over the controller's requestor.
type requestor struct {
	r *camera.Requestor
}

func (r requestor) endpointID() uint16                    { return controller.CameraControllerEndpoint }
func (r requestor) sessions() []wrdef.WebRTCSessionStruct { return r.r.Sessions() }
func (r requestor) upsert(s wrdef.WebRTCSessionStruct) error {
	return r.r.UpsertSession(s)
}
func (r requestor) remove(k wrdef.SessionKey) { r.r.RemoveSession(k) }

// The image reads of the watch.

func (io *clientIO) providerEndpoints(nodeID uint64) []uint16 {
	n := io.node(nodeID)
	if n == nil {
		return nil
	}
	var out []uint16
	for _, ep := range n.Endpoints() {
		if ep.Cluster(wpdef.ClusterID) != nil {
			out = append(out, ep.Number())
		}
	}
	slices.Sort(out)
	return out
}

func (io *clientIO) currentSessions(nodeID uint64, endpointID uint16) ([]sessionPeer, bool) {
	c := io.cluster(target{nodeID, endpointID}, wpdef.ClusterID)
	if c == nil {
		return nil, false
	}
	st, _ := wpdef.StateFrom(c)
	out := make([]sessionPeer, len(st.CurrentSessions))
	for i, s := range st.CurrentSessions {
		out[i] = sessionPeer{id: s.Id, peerNodeID: s.PeerNodeId}
	}
	return out, true
}

// providerNodes adapter: syncTime is named syncTimeOf on clientIO, so the
// interface is met by this wrapper.
type clientProviderNodes struct{ *clientIO }

func (n clientProviderNodes) syncTime(ctx context.Context, nodeID uint64) error {
	return n.syncTimeOf(ctx, nodeID)
}
