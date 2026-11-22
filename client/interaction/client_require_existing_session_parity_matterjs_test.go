// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/node/test/node/ClientRequireExistingSessionTest.ts
// at b25ed960 (#4928): ClientRequest.requireExistingSession on the
// controller's interaction with a commissioned peer — a read over the
// session that exists, a read that fails instead of connecting when the
// peer has none, and the refusal for a sustained subscription.

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/im"
)

// vendorNameRead is the test's vendorName read (:20-23): BasicInformation
// VendorName on the root endpoint, over an existing session only.
func vendorNameRead() interaction.ReadRequest {
	return interaction.ReadRequest{
		Attributes:    []im.ConcreteAttributePath{rootAttr(basicinfo.ClusterID, basicinfo.AttrVendorName)},
		ClientRequest: interaction.ClientRequest{RequireExistingSession: true},
	}
}

func TestClientRequireExistingSession(t *testing.T) {
	t.Run("reads over the existing session", func(t *testing.T) {
		lv := newLive(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var rep *interaction.Report
		if err := lv.run(ctx, func(ctx context.Context) error {
			var err error
			rep, err = lv.c.ReadWith(ctx, vendorNameRead())
			return err
		}); err != nil {
			t.Fatalf("read over the existing session: %v", err)
		}
		if _, ok := rep.Value(0, basicinfo.ClusterID, basicinfo.AttrVendorName); !ok {
			t.Fatal("the report carries no VendorName")
		}
	})

	t.Run("fails without a session instead of establishing one", func(t *testing.T) {
		lv := newLive(t)
		// :48-50: every session of the controller closed.
		for _, s := range lv.peer.Sessions() {
			s.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := lv.run(ctx, func(ctx context.Context) error {
			_, err := lv.c.ReadWith(ctx, vendorNameRead())
			return err
		})
		// matter.js's SessionClosedError "No existing session to <address>"
		// (PeerExchangeProvider.ts:116-118 at b25ed960) is ErrNoSession.
		if !errors.Is(err, interaction.ErrNoSession) {
			t.Fatalf("read without a session: %v, want ErrNoSession", err)
		}
		if s := lv.peer.NewestSession(); s != nil && !s.IsClosed() {
			t.Fatal("the read established a session")
		}
	})

	t.Run("is refused for a sustained subscription", func(t *testing.T) {
		lv := newLive(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// ImplementationError (ClientInteraction.ts:1026-1030 at b25ed960) is
		// ErrMalformedRequest, as the subscription's other request checks.
		sub, err := lv.c.Subscribe(ctx, interaction.SubscribeRequest{
			Attributes:    []im.ConcreteAttributePath{{Endpoint: 0, HasEndpoint: true}},
			Sustain:       true,
			ClientRequest: interaction.ClientRequest{RequireExistingSession: true},
		})
		if !errors.Is(err, interaction.ErrMalformedRequest) {
			if sub != nil {
				sub.Close()
			}
			t.Fatalf("sustained subscription with RequireExistingSession: %v, want ErrMalformedRequest", err)
		}
	})
}
