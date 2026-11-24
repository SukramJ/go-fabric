// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"testing"

	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/im"
)

// TestThreadNetworkCommandAfterTheWindowExpired (review II, R3-fsm-3055):
// a fail-safe that expires between the gate's check and the command's
// write — the port runs the expiry before it answers armed — fails the
// command with FAILSAFE_REQUIRED: no backup is taken that no expiry would
// revert, and the staged network stays.
func TestThreadNetworkCommandAfterTheWindowExpired(t *testing.T) {
	t.Parallel()
	for _, cmd := range []struct {
		name   string
		active []byte
		id     uint32
		req    any
	}{
		{"AddOrUpdateThreadNetwork", nil, netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset}},
		{"RemoveNetwork", threadDataset, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: threadExtPANID}},
		{"ConnectNetwork", threadDataset, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID}},
	} {
		t.Run(cmd.name, func(t *testing.T) {
			t.Parallel()
			br := &simBorderRouter{active: bytes.Clone(cmd.active), attached: cmd.active != nil, enabled: cmd.active != nil}
			armed, crumbs := false, []uint64{}
			n := newThreadNC(t, br, &armed, &crumbs)
			expired := false
			n.AttachFailSafe(func() (uint8, bool) {
				if !expired {
					expired = true
					n.OnFailSafeExpired(context.Background())
				}
				return 1, true
			}, nil)
			br.mu.Lock()
			activations := len(br.activated)
			br.mu.Unlock()

			_, err := n.MatterInvoke(im.WithFabricFilter(context.Background(), false, 1), cmd.id, cmd.req)
			if got := threadStatusOf(t, err); got != im.StatusFailsafeRequired {
				t.Fatalf("%s after the window expired = %v, want FAILSAFE_REQUIRED", cmd.name, err)
			}
			n.mu.Lock()
			backup, staging := n.backup, n.staging
			n.mu.Unlock()
			if backup != nil {
				t.Errorf("backup %x taken after the expiry", backup)
			}
			if !bytes.Equal(staging, cmd.active) {
				t.Errorf("staged network %x, want %x", staging, cmd.active)
			}
			br.mu.Lock()
			defer br.mu.Unlock()
			if len(br.activated) != activations {
				t.Errorf("border router attached %x after the expiry", br.activated[activations:])
			}
		})
	}
}
