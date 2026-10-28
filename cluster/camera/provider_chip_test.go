// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// WebRtcTransportProvider: a port of connectedhomeip
// src/app/clusters/webrtc-transport-provider-server/tests/
// TestWebRTCTransportProviderCluster.cpp at 6170af84, by meaning: the
// attribute and session-table cases. chip has no handler tests; the
// handler paths are provider_rules_test.go. MockWebRTCTransportProviderDelegate
// (:39-128) is mockProvider.
package camera_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// mockProvider is the ProviderPort: it builds the session the server
// stores from the arguments, and records the signals.
type mockProvider struct {
	mu       sync.Mutex
	offers   []camera.OfferArgs
	answers  []string
	ice      int
	ended    []uint16
	deferred bool
	err      error
}

func (m *mockProvider) session(args camera.OfferArgs) camera.ProviderSession {
	s := camera.ProviderSession{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, FabricIndex: args.FabricIndex, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams,
	}
	if args.VideoStreams != nil && len(*args.VideoStreams) > 0 {
		v := spec.ValueOf((*args.VideoStreams)[0])
		s.VideoStreamId = &v
	}
	return s
}

func (m *mockProvider) SolicitOffer(_ context.Context, args camera.OfferArgs) (camera.ProviderSession, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return camera.ProviderSession{}, false, m.err
	}
	m.offers = append(m.offers, args)
	return m.session(args), m.deferred, nil
}

func (m *mockProvider) ProvideOffer(_ context.Context, args camera.OfferArgs) (camera.ProviderSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return camera.ProviderSession{}, m.err
	}
	m.offers = append(m.offers, args)
	return m.session(args), nil
}

func (m *mockProvider) ProvideAnswer(_ context.Context, _ uint16, sdp string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.answers = append(m.answers, sdp)
	return m.err
}

func (m *mockProvider) ProvideIceCandidates(context.Context, uint16, []wpdef.ICECandidateStruct) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ice++
	return m.err
}

func (m *mockProvider) EndSession(_ context.Context, id uint16, _ uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ended = append(m.ended, id)
	return m.err
}

func newProvider(t *testing.T, streams camera.StreamSource, edit ...func(*camera.ProviderConfig)) (*camera.Provider, *mockProvider) {
	t.Helper()
	m := &mockProvider{}
	cfg := camera.ProviderConfig{Port: m, Streams: streams}
	for _, e := range edit {
		e(&cfg)
	}
	p, err := camera.NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

// TestProviderAttributes (:131-137): CurrentSessions and the globals.
func TestProviderAttributes(t *testing.T) {
	p, _ := newProvider(t, nil)
	if !slices.Contains(p.MatterAttributes(), wpdef.AttrCurrentSessions) {
		t.Fatalf("AttributeList %v", p.MatterAttributes())
	}
}

// TestProviderCommands (:139-152): the five commands; the two responses
// generated.
func TestProviderCommands(t *testing.T) {
	p, _ := newProvider(t, nil)
	want := []uint32{wpdef.CmdSolicitOffer, wpdef.CmdProvideOffer, wpdef.CmdProvideAnswer, wpdef.CmdProvideIceCandidates, wpdef.CmdEndSession}
	if got := slices.Sorted(slices.Values(p.MatterAcceptedCommands())); !slices.Equal(got, want) {
		t.Fatalf("AcceptedCommandList %v", got)
	}
	if got := slices.Sorted(slices.Values(p.MatterGeneratedCommands())); !slices.Equal(got, []uint32{wpdef.CmdSolicitOfferResponse, wpdef.CmdProvideOfferResponse}) {
		t.Fatalf("GeneratedCommandList %v", got)
	}
}

// TestProviderCurrentSessionsAttribute (:154-162).
func TestProviderCurrentSessionsAttribute(t *testing.T) {
	p, _ := newProvider(t, nil)
	if len(p.Sessions()) != 0 {
		t.Fatal("a new provider holds sessions")
	}
}

// TestProviderSessionManagement (:164-177): removing an unknown session
// is safe.
func TestProviderSessionManagement(t *testing.T) {
	p, _ := newProvider(t, nil)
	p.RemoveSession(999)
	if len(p.Sessions()) != 0 {
		t.Fatal("RemoveSession made a session")
	}
}

// TestProviderReadCurrentSessions (:179-197).
func TestProviderReadCurrentSessions(t *testing.T) {
	p, _ := newProvider(t, nil)
	v, ok := p.MatterRead(wpdef.AttrCurrentSessions)
	var got []camera.ProviderSession
	if !ok || !spec.Assign(&got, v) || len(got) != 0 {
		t.Fatalf("CurrentSessions %v %v", v, ok)
	}
}

// TestProviderReadClusterRevision (:199-215).
func TestProviderReadClusterRevision(t *testing.T) {
	p, _ := newProvider(t, nil)
	v, ok := p.MatterRead(0xFFFD)
	var got uint16
	if !ok || !spec.Assign(&got, v) || got != wpdef.Revision {
		t.Fatalf("ClusterRevision %v", v)
	}
}

// TestProviderReadUnsupportedAttribute (:217-233).
func TestProviderReadUnsupportedAttribute(t *testing.T) {
	p, _ := newProvider(t, nil)
	if _, ok := p.MatterRead(0xFFFF); ok {
		t.Fatal("attribute 0xFFFF read")
	}
}
