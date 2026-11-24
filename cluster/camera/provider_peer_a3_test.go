// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/im"
)

// blockingPeersA3 is a PeerConnector whose ConnectPeer (blockConnect) or
// Invoke blocks until release is closed; entered is closed when the call
// blocks, and the blocked call then answers err.
type blockingPeersA3 struct {
	blockConnect bool
	entered      chan struct{}
	release      chan struct{}
	err          error
}

func newBlockingPeersA3(blockConnect bool, err error) *blockingPeersA3 {
	return &blockingPeersA3{blockConnect: blockConnect, entered: make(chan struct{}), release: make(chan struct{}), err: err}
}

func (b *blockingPeersA3) ConnectPeer(context.Context, uint8, uint64) (camera.PeerSession, error) {
	if b.blockConnect {
		close(b.entered)
		<-b.release
		return nil, b.err
	}
	return b, nil
}

func (b *blockingPeersA3) Invoke(context.Context, uint16, uint32, uint32, spec.Encodable) (*spec.Node, error) {
	close(b.entered)
	<-b.release
	return nil, b.err
}

// TestProviderEndRemovesBeforeTheSendA3: End takes the session out of
// CurrentSessions before it sends the requestor's End, so the removal
// does not wait on the requestor's response — the session is gone while
// the End is in flight, whether the send is still reaching the requestor
// or already waiting for its answer — and the send's error is still
// returned (issue #596 A, decisions 654 and 663).
func TestProviderEndRemovesBeforeTheSendA3(t *testing.T) {
	for _, tc := range []struct {
		name         string
		blockConnect bool
		err          error
	}{
		{"invoke in flight", false, statusErr{im.StatusNotFound}},
		{"connect in flight, requestor unreachable", true, errors.New("no route")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newProvider(t, nil)
			peers := newBlockingPeersA3(tc.blockConnect, tc.err)
			p.AttachPeerConnector(peers)
			if err := p.UpsertSession(camera.ProviderSession{
				Id: 5, PeerNodeId: sigPeerNode, PeerEndpointId: sigPeerEndpoint, FabricIndex: sigPeerFabric, StreamUsage: wpdef.StreamUsageLiveView,
				VideoStreams: &[]uint16{1},
			}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- p.End(context.Background(), 5, uint8(wrdef.WebRTCEndReasonPrivacyMode)) }()
			<-peers.entered
			if n := len(p.Sessions()); n != 0 {
				close(peers.release)
				<-done
				t.Fatalf("%d sessions while the End is in flight, want 0", n)
			}
			close(peers.release)
			if err := <-done; !errors.Is(err, tc.err) {
				t.Fatalf("End: %v, want the send's error %v", err, tc.err)
			}
			if n := len(p.Sessions()); n != 0 {
				t.Fatalf("%d sessions after End", n)
			}
		})
	}
}
