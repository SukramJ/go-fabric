// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
)

// TestClientIOTimePush: without a host push, the time pushed to a camera
// that refused an offer for its clock is SetUTCTime on its root endpoint,
// the controller's clock in microseconds since the Matter epoch.
func TestClientIOTimePush(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	var got tsdef.SetUtcTimeRequest
	var where target
	io := &clientIO{clk: clocktest.New(at), send: func(_ context.Context, t target, cluster, command uint32, fields spec.Encodable) (any, error) {
		if cluster == tsdef.ClusterID && command == tsdef.CmdSetUtcTime {
			got, where = fields.(tsdef.SetUtcTimeRequest), t
		}
		return nil, nil
	}}
	if err := io.syncTimeOf(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if where != (target{nodeID: 5}) || got.UtcTime != uint64(at.Sub(matterEpoch).Microseconds()) || got.Granularity != tsdef.GranularityMillisecondsGranularity {
		t.Errorf("pushed %+v to %+v", got, where)
	}
}

// TestClientIOAbandon: a session given up and not ended leaves the
// requestor's table, and the next batch end sweeps its endpoint again.
func TestClientIOAbandon(t *testing.T) {
	f := newPSFixture()
	f.batch()
	_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 9 }))
	io := &clientIO{svc: f.svc}
	if err := io.abandonSession(context.Background(), target{psCamera, psEndpoint}, 9); err != nil {
		t.Fatal(err)
	}
	if len(f.sessions()) != 0 {
		t.Errorf("requestor %+v", f.sessions())
	}
}
