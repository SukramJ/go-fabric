// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/client"
)

// Provider is the raw route to one camera's WebRtcTransportProvider: the
// cluster's own commands for a host that allocates its streams itself, or
// re-offers a live session. A session it opens is registered with the
// controller's requestor, so the camera's signals arrive, on
// [Observer].Signal, since no [Session] holds it.
type Provider struct {
	s *Service
	t target
}

// Provider is the raw route to the camera endpoint.
func (s *Service) Provider(node *client.Node, endpoint uint16) (*Provider, error) {
	t, err := targetOf(node, endpoint)
	if err != nil {
		return nil, err
	}
	return &Provider{s: s, t: t}, nil
}

// Send sends ProvideOffer, SolicitOffer, ProvideAnswer or
// ProvideIceCandidates with payload, a JSON object of the command's
// fields as matter.js names them (keys matched with case and word
// separators ignored), checked against the cluster's model. An offer
// gets the controller's requestor endpoint as its originating endpoint,
// its stream fields reconciled with the camera's cluster revision, and
// its session registered with the requestor. The result is the
// generated response (wpdef.ProvideOfferResponse, wpdef.SolicitOfferResponse)
// or nil; notices are what the boundary forwarded unchanged that the
// camera may ignore. Mirrors matterjs-server's send_webrtc_provider_command
// (docs/websockets_api.md:1868-1900 of matterjs-server at 0bd06f2).
func (p *Provider) Send(ctx context.Context, command string, payload json.RawMessage) (result any, notices []string, err error) {
	if !isProviderCommandName(command) {
		return nil, nil, invalidArguments("command_name must be one of ProvideOffer, SolicitOffer, ProvideAnswer, ProvideIceCandidates")
	}
	name := providerCommandName(command)
	fields, err := toProviderCommandFields(name, payload, fmt.Sprintf("node 0x%016X endpoint %d", p.t.nodeID, p.t.endpointID), command+" payload")
	if err != nil {
		return nil, nil, err
	}
	req, err := fields.request(p.s.ps.requestor.endpointID())
	if err != nil {
		return nil, fields.notices, err
	}
	result, err = p.send(ctx, name, req)
	return result, fields.notices, err
}

func (p *Provider) send(ctx context.Context, name providerCommandName, req spec.Encodable) (any, error) {
	if establishesWebRTCSession(name) {
		// The camera handed this id to a session another route
		// established: a session still recorded under it is one the
		// camera lost.
		return p.s.ps.establish(ctx, p.t, req, func(id uint16) { p.s.m.sessions.issued(p.t.nodeID, p.t.endpointID, id) })
	}
	command := callProvideAnswer
	if name == cmdProvideIceCandidates {
		command = callProvideIceCandidates
	}
	return p.s.io.invokeProvider(ctx, p.t, command, req)
}

// EndSession is a host's own EndSession for a session of the camera (a
// generic device command): it is sent as given, its error passed on, and
// the controller's records for the session go with it, the end announced
// as [Service.StopSession] announces one. requestedBy is the owner
// asking. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2303-2321 at
// 0bd06f2.
func (p *Provider) EndSession(ctx context.Context, req wpdef.EndSessionRequest, requestedBy string) error {
	return p.s.m.endSessionForClient(ctx, p.t, req.WebRtcSessionId, requestedBy, func(ctx context.Context) error {
		_, err := p.s.io.invoke(ctx, deviceCall{target: p.t, cluster: clusterProvider, command: callEndSession, fields: req})
		return err
	})
}
