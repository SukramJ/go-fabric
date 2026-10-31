// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// deviceTypeCamera is the Camera device type (schema/devicetypes.go: 0x0142).
const deviceTypeCamera = 0x0142

// refusingCameraStack is a camera's WebRTC stack that cannot produce an
// offer of its own (a relay that takes the client's offer only) and says
// so with the facade's status type.
type refusingCameraStack struct {
	cameraStack
	err error
}

func (r refusingCameraStack) SolicitOffer(context.Context, wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return wpdef.WebRTCSessionStruct{}, false, r.err
}

// liveViewStreams is a camera with one allocated live-view video stream,
// so an offer for LiveView passes the provider's checks and reaches the
// port.
type liveViewStreams struct{}

func (liveViewStreams) VideoStreams() []camera.VideoStream {
	return []camera.VideoStream{{VideoStreamId: 1, StreamUsage: camera.StreamUsage(wpdef.StreamUsageLiveView), KeyFrameInterval: 1000}}
}
func (liveViewStreams) AudioStreams() []camera.AudioStream { return nil }
func (liveViewStreams) StreamUsagePriorities() []camera.StreamUsage {
	return []camera.StreamUsage{camera.StreamUsage(wpdef.StreamUsageLiveView)}
}

func (liveViewStreams) SupportedStreamUsages() []camera.StreamUsage {
	return []camera.StreamUsage{camera.StreamUsage(wpdef.StreamUsageLiveView)}
}

func (liveViewStreams) PrivacyModes() (hard, softRecording, softLivestream bool) {
	return false, false, false
}

// TestCameraProviderPortStatusReachesController (#356): a ProviderPort's
// contract.StatusError reaches the controller as that status — the
// server passes it through and the dispatcher answers it — where before
// only an im.StatusCodeError did and the facade's status answered FAILURE.
// The provider's commands are large messages a controller sends over TCP
// only, so the site runs on loopback sockets with TCP on both sides (as
// cluster/camera's requestor tests do).
func TestCameraProviderPortStatusReachesController(t *testing.T) {
	holdSite(t)
	site := fabrictest.NewSite(t, fabrictest.Options{Loopback: true, Logger: slog.New(slog.DiscardHandler)})
	ctx := parityCtx(t)
	stack := refusingCameraStack{err: contract.StatusError{Status: uint8(im.StatusInvalidInState), Message: "the relay takes the client's offer only"}}
	prov, err := camera.NewProvider(camera.ProviderConfig{Port: stack, Streams: liveViewStreams{}})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{
			Bridged:   []node.Endpoint{fabrictest.Endpoint("cam", deviceTypeCamera, prov)},
			Configure: func(cfg *node.Config) { cfg.Network.TCP = node.TCPConfig{Incoming: true, Outgoing: true} },
		},
		Controller: fabrictest.ControllerOptions{TCP: controller.TCPConfig{Outgoing: true}},
	})
	ep, ok := pair.Device.EndpointOf("cam")
	if !ok {
		t.Fatal("no camera endpoint")
	}
	null := spec.NullOf[uint16]()
	_, err = fabrictest.Invoke(ctx, pair, client.CommandAt(ep, wpdef.ClusterID, wpdef.CmdSolicitOffer),
		wpdef.SolicitOfferRequest{StreamUsage: wpdef.StreamUsageLiveView, OriginatingEndpointId: 1, VideoStreamId: &null})
	var se *client.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusInvalidInState {
		t.Fatalf("SolicitOffer refused by the port with INVALID_IN_STATE: %v", err)
	}
	if n := len(prov.Sessions()); n != 0 {
		t.Fatalf("%d sessions stored after the port refused", n)
	}
}
