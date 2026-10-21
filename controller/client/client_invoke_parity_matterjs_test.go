// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientInvokeTest.ts at
// 862ad75f: the controller's invokes through a client node. matter.js
// batches commands up to the peer's maxPathsPerInvoke and propagates
// network and per-request options through the batcher; the Go client
// sends one command per InvokeRequest (client/interaction
// Interaction.Invoke), so the batching, splitting, commandRef,
// suppressResponse, network-profile and batch-exchange cases are missing
// with that reason in the register, and the cases about the commands
// themselves run on fabrictest here.
package client_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/im"
)

func TestClientInvokeParity(t *testing.T) {
	t.Parallel()
	t.Run("executes multiple commands sequentially", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := cp.lightPath(onoff.ClusterID, onoff.AttrOnOff)
		for _, c := range []struct {
			cmd  uint32
			want bool
		}{{onoff.CmdOff, false}, {onoff.CmdOn, true}, {onoff.CmdToggle, false}} {
			cp.run(t, func(ctx context.Context) error {
				_, err := cp.node.Invoke(ctx, im.ConcreteCommandPath{Endpoint: cp.lightEP, Cluster: onoff.ClusterID, Command: c.cmd}, nil, interaction.InvokeOptions{})
				return err
			})
			if v := deviceValue(t, cp, path); v.El.Bool != c.want {
				t.Fatalf("device OnOff %v after command %d, want %v", v.El.Bool, c.cmd, c.want)
			}
		}
	})
}
