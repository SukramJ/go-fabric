// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientNodeConfigurationVersionTest.ts
// at 862ad75f: the controller signals a peer's ConfigurationVersion bump —
// matter.js lifecycle.configurationVersionChanged, client.Hooks.ConfigurationVersionChanged.
package client_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

func TestClientNodeConfigurationVersionParity(t *testing.T) {
	t.Parallel()
	t.Run("emits configurationVersionChanged on the peer when the node bumps ConfigurationVersion", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var changed []uint16
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.ConfigurationVersionChanged = func(ep *client.Endpoint) {
				mu.Lock()
				changed = append(changed, ep.Number())
				mu.Unlock()
			}
		}}})
		if err := cp.pair.Device.Act(func(n *node.Node) error {
			n.Bridge().IncreaseNodeConfigurationVersion()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "configurationVersionChanged", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(changed) > 0
		})
		mu.Lock()
		defer mu.Unlock()
		if changed[0] != 0 {
			t.Fatalf("the change was signalled on endpoint %d, want the root", changed[0])
		}
	})
}
