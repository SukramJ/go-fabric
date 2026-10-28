// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientInvokeTest.ts at
// 862ad75f: the controller's invokes through a client node, on fabrictest
// (the Go device's MaxPathsPerInvoke is 10). The batcher's own cases —
// its bypasses, per-request options, lanes per network, close and abort —
// run on the scripted rig in client/interaction
// (TestInvokeBatcherParity); here the commands reach a real device
// through it.
package client_test

import (
	"context"
	"sync"
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
	for _, row := range []struct {
		name string
		cmds []uint32
		want bool
	}{
		// ClientInvokeTest.ts:382-415: on and toggle share a batch, the
		// second on goes into one of its own.
		{"batches different commands together while splitting duplicates", []uint32{onoff.CmdOn, onoff.CmdToggle, onoff.CmdOn}, true},
		// :973-1003: two toggles of one tick — one path twice — leave the
		// light as it was.
		{"splits duplicate command paths into separate batches", []uint32{onoff.CmdToggle, onoff.CmdToggle}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			cp := newClientPair(t, clientPairOptions{})
			path := cp.lightPath(onoff.ClusterID, onoff.AttrOnOff)
			cp.run(t, func(ctx context.Context) error {
				var wg sync.WaitGroup
				errs := make([]error, len(row.cmds))
				for i, cmd := range row.cmds {
					wg.Go(func() {
						_, errs[i] = cp.node.Invoke(ctx, im.ConcreteCommandPath{Endpoint: cp.lightEP, Cluster: onoff.ClusterID, Command: cmd}, nil, interaction.InvokeOptions{})
					})
				}
				wg.Wait()
				for _, err := range errs {
					if err != nil {
						return err
					}
				}
				return nil
			})
			// The on/toggle/on order is the batches' order only when they
			// share a tick; the duplicate case holds whatever the order.
			if v := deviceValue(t, cp, path); row.cmds[0] == onoff.CmdToggle && v.El.Bool != row.want {
				t.Fatalf("device OnOff %v, want %v", v.El.Bool, row.want)
			}
		})
	}
}
