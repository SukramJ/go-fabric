// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box test for TC-EEVSE-2.3 step 19 on the wire: the generated
// decoder no longer refuses the eleventh ChargingTarget of a day at the
// model's `max 10`, so the EnergyEvse server's own check answers, as
// chip's handler does (EnergyEvseCluster.cpp:564-565, ResourceExhausted)
// — a deliberate divergence from matter.js, which validates the request
// first (CommandInvokeResponse.ts:484-487) and answers ConstraintError
// (notes/parity/by_design.md BD-chip-EVSE-TargetsServerHeld).

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

func TestSetTargetsEleventhTargetIsServerJudged(t *testing.T) {
	t.Parallel()
	added := int64(25000000)
	for name, c := range map[string]struct {
		targets int
		status  im.StatusCode
	}{
		"eleven targets: the server's RESOURCE_EXHAUSTED": {11, im.StatusResourceExhausted},
		"ten targets: applied":                            {10, im.StatusSuccess},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			targets := make([]energy.ChargingTarget, 0, c.targets)
			for i := range c.targets {
				targets = append(targets, energy.ChargingTarget{TargetTimeMinutesPastMidnight: uint16(60 + 120*i), AddedEnergy: &added})
			}
			req := evse.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{{DayOfWeekForSequence: 0x40, ChargingTargets: slices.Clip(targets)}}}
			enc := tlv.NewEncoder()
			req.EncodeTLV(enc, tlv.AnonymousTag())
			raw, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			dec := tlv.NewDecoder(raw)
			el, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			fields, ok, err := decodeByDefinition(im.ConcreteCommandPath{Cluster: evse.ClusterID, Command: evse.CmdSetTargets}, dec, el)
			if !ok || err != nil {
				t.Fatalf("decodeByDefinition: ok %t, %v; want the request decoded for the server", ok, err)
			}
			srv, err := energy.NewEnergyEvse(energy.EvseConfig{Features: energy.EvseFeatureChargingPreferences})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(srv.Close)
			_, err = srv.MatterInvoke(context.Background(), evse.CmdSetTargets, fields)
			if c.status == im.StatusSuccess {
				if err != nil {
					t.Fatalf("SetTargets: %v; want applied", err)
				}
				return
			}
			var sce im.StatusCodeError
			if !errors.As(err, &sce) || sce.MatterStatusCode() != c.status {
				t.Errorf("SetTargets: %v; want status 0x%02X", err, c.status)
			}
		})
	}
}
