// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The LIT ICD part of the port of matter.js
// packages/node/test/node/ServerSubscriptionTest.ts at 862ad75f (:157-183):
// a LIT-capable ICD publisher grants its IdleModeDuration as the
// subscription's MaxInterval, above or below the ceiling the controller
// requests (ServerSubscription.ts:302-312). The device is a fabrictest node
// with the test's LIT_CONFIG (icd-helpers.ts) as node.Config.ICD; matter.js's
// ServerSubscription with a mocked exchange is a subscription over the wire.
package node_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

func TestServerSubscriptionLITParity(t *testing.T) {
	t.Parallel()
	negotiated := func(t *testing.T, ceiling time.Duration) time.Duration {
		t.Helper()
		_, pair := icdPair(t, icdSetup{cfg: litConfig})
		sub, err := fabrictest.Subscribe(t.Context(), pair, interaction.SubscribeRequest{
			Attributes:       []im.ConcreteAttributePath{attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)},
			MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(ceiling),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(sub.Close)
		return sub.MaxInterval()
	}
	t.Run("grants LIT ICD publisher maxInterval === idleModeDuration when the client requests a higher ceiling", func(t *testing.T) {
		t.Parallel()
		if got := negotiated(t, 7200*time.Second); got != litConfig.IdleModeDuration {
			t.Fatalf("MaxInterval %v, want %v", got, litConfig.IdleModeDuration)
		}
	})
	t.Run("grants LIT ICD publisher maxInterval === idleModeDuration when the client requests a lower ceiling", func(t *testing.T) {
		t.Parallel()
		if got := negotiated(t, 1800*time.Second); got != litConfig.IdleModeDuration {
			t.Fatalf("MaxInterval %v, want %v", got, litConfig.IdleModeDuration)
		}
	})
}
