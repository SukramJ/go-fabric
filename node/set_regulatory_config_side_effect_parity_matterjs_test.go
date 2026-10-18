// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/SetRegulatoryConfigSideEffectTest.ts
// at 862ad75f: a SetRegulatoryConfig that fails validation — Outdoor on an
// Indoor-only node — answers ValueOutsideRange and leaves
// BasicInformation.Location as it was.
//
// matter.js invokes on a MockServerNode in a command context without
// fabric; the port invokes over a PASE session (fabrictest
// Controller.PASE) to an uncommissioned node, whose LocationCapability is
// Indoor (node.Commissioning.LocationCapability's zero value).
package node_test

import (
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/fabrictest"
)

func TestSetRegulatoryConfigSideEffectParity(t *testing.T) {
	t.Parallel()
	t.Run("does not write BasicInformation.location when newRegulatoryConfig validation fails", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		ctx := parityCtx(t)
		p, err := u.Controller.PASE(ctx, u.Device)
		if err != nil {
			t.Fatal(err)
		}
		// FAILSAFE_LENGTH_S (packages/node/src/testing/node-helpers.ts:104).
		if _, err := fabrictest.Invoke(ctx, p, cmdPath(0, gencomm.ClusterID, gencomm.CmdArmFailSafe), gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1}); err != nil {
			t.Fatal(err)
		}
		location := attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocation)
		before, err := fabrictest.Read(ctx, p, location)
		if err != nil {
			t.Fatal(err)
		}
		// Outdoor is invalid for an Indoor-only capability; DE differs from
		// the default to exercise the location write.
		res, err := fabrictest.Invoke(ctx, p, cmdPath(0, gencomm.ClusterID, gencomm.CmdSetRegulatoryConfig), gencomm.SetRegulatoryConfigRequest{
			NewRegulatoryConfig: gencomm.RegulatoryLocationTypeOutdoor, CountryCode: "DE", Breadcrumb: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		var resp gencomm.SetRegulatoryConfigResponse
		if err := resp.DecodeTLV(res.Fields); err != nil || resp.ErrorCode != gencomm.CommissioningErrorValueOutsideRange {
			t.Fatalf("SetRegulatoryConfig response %+v, %v, want ValueOutsideRange", resp, err)
		}
		after, err := fabrictest.Read(ctx, p, location)
		if err != nil {
			t.Fatal(err)
		}
		if after.Value.El.String != before.Value.El.String {
			t.Fatalf("Location %q after the refused SetRegulatoryConfig, was %q", after.Value.El.String, before.Value.El.String)
		}
	})
}
