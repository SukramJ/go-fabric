// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ReceivedStatusTest.ts at
// 862ad75f: a status the device sent, as the controller surfaces it.
//
// matter.js distinguishes a ReceivedStatusResponseError (the device sent
// it) from a StatusResponseError raised locally; in Go every
// *interaction.StatusError is a status the peer sent, and a local refusal
// is another error. A read's per-path statuses are the report's
// Statuses, where matter.js's state getter throws an
// EndpointReadFailedError of them.
package client_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

func TestReceivedStatusParity(t *testing.T) {
	t.Parallel()

	t.Run("keeps the origin of a device status in EndpointReadFailedError", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)}})
		if err := cp.pair.Device.Node().Aggregator().Remove(parityCtx(t), endpoint.StringKey("light:1")); err != nil {
			t.Fatal(err)
		}
		if cp.node.Endpoint(cp.lightEP) == nil {
			t.Fatal("the image lost the light without a subscription")
		}
		var rep *interaction.Report
		cp.run(t, func(ctx context.Context) error {
			var err error
			rep, err = cp.node.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{cp.lightPath(onoff.ClusterID, onoff.AttrOnOff)}}})
			return err
		})
		if len(rep.Statuses) == 0 {
			t.Fatal("the read reported no status for the removed endpoint")
		}
		for _, s := range rep.Statuses {
			if s.Status != im.StatusUnsupportedEndpoint {
				t.Fatalf("status %+v, want UNSUPPORTED_ENDPOINT", s)
			}
		}
	})
}
