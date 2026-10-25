// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	acdef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	ocdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestDecodeByDefinitionSkipsHandWrittenServers: a linked definition-only
// definition (ADR 0013, amendment of 2026-10-25) never decodes a request
// for the hand-written cluster/core server of its cluster.
func TestDecodeByDefinitionSkipsHandWrittenServers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cluster, cmd uint32
	}{
		{acdef.ClusterID, acdef.CmdReviewFabricRestrictions},
		{ocdef.ClusterID, ocdef.CmdAttestationRequest},
		{gkmdef.ClusterID, gkmdef.CmdKeySetWrite},
	} {
		if spec.Lookup(c.cluster) == nil {
			t.Fatalf("0x%04X: definition not registered", c.cluster)
		}
		if !handWrittenServers[c.cluster] {
			t.Errorf("0x%04X: not among the hand-written servers", c.cluster)
		}
		if _, ok, err := decodeByDefinition(im.ConcreteCommandPath{Cluster: c.cluster, Command: c.cmd}, nil, tlv.Element{}); ok || err != nil {
			t.Errorf("0x%04X/0x%02X: decoded by the definition (%v, %v)", c.cluster, c.cmd, ok, err)
		}
	}
}
