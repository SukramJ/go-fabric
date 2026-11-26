// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestProviderEndWithoutConnectorRemovesW2B6 (#620, decision 686): End on
// a provider no PeerConnector was attached to removes the session as it
// does when the requestor cannot be reached, and answers
// ErrNoPeerConnector — nothing was sent.
func TestProviderEndWithoutConnectorRemovesW2B6(t *testing.T) {
	t.Parallel()
	p, peers := signalProvider(t)
	p.AttachPeerConnector(nil)
	if err := p.End(context.Background(), 5, 0); !errors.Is(err, camera.ErrNoPeerConnector) {
		t.Fatalf("End without a connector: %v, want ErrNoPeerConnector", err)
	}
	if n := len(p.Sessions()); n != 0 {
		t.Errorf("End without a connector kept the session: %d sessions", n)
	}
	if l := readList[camera.ProviderSession](t, p, wpdef.AttrCurrentSessions); len(l) != 0 {
		t.Errorf("CurrentSessions = %+v after End without a connector", l)
	}
	peers.mu.Lock()
	defer peers.mu.Unlock()
	if len(peers.calls) != 0 {
		t.Errorf("End without a connector sent %d commands", len(peers.calls))
	}
}

// TestProviderEndReturnsSendAndRemovalErrorsW2B6 (#620, decision 686): a
// send that fails and a removal the table refuses are both returned; a
// refused removal is returned without a connector too.
func TestProviderEndReturnsSendAndRemovalErrorsW2B6(t *testing.T) {
	t.Parallel()
	refusal := errors.New("refused table")
	t.Run("failed send", func(t *testing.T) {
		t.Parallel()
		p, peers := signalProvider(t)
		peers.invokeErr = statusErr{im.StatusNotFound}
		camera.SetProviderCommitRefusalW2B6(p, func() error { return refusal })
		err := p.End(context.Background(), 5, 0)
		var se statusErr
		if !errors.As(err, &se) || se.code != im.StatusNotFound {
			t.Errorf("End: %v, want the send's NOT_FOUND", err)
		}
		if !errors.Is(err, refusal) {
			t.Errorf("End: %v, want the removal's refusal too", err)
		}
	})
	t.Run("no connector", func(t *testing.T) {
		t.Parallel()
		p, _ := signalProvider(t)
		p.AttachPeerConnector(nil)
		camera.SetProviderCommitRefusalW2B6(p, func() error { return refusal })
		err := p.End(context.Background(), 5, 0)
		if !errors.Is(err, camera.ErrNoPeerConnector) || !errors.Is(err, refusal) {
			t.Errorf("End: %v, want ErrNoPeerConnector and the removal's refusal", err)
		}
	})
}
