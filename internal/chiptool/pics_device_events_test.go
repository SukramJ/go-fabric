// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// deviceTypeEvents is the events each device type makes mandatory on a
// server cluster it lists — matter.js's device type element requirements
// (schema.DeviceTypeDefinition, nested "event" requirements with
// conformance "M" that resolve and are not provisional), keyed by device
// type id and cluster id as decimal strings. testdata/gen_pics.py reads it
// to set an event's PICS code where the endpoint's device type, not only
// the cluster's own conformance, requires it: CHIP's spec parser carries
// no event overrides for device types (matter/testing/spec_parsing.py
// XmlDeviceTypeClusterRequirements, feature / attribute / command only, at
// the harness pin 6170af8461b10b1766044122ac83332c6d00ab20), so
// derive_base_pics_facts_from_device_wildcard's mandatory_events_by_cluster
// (matter/testing/pics.py:276-300) misses OperationCompletion on a
// LaundryWasher or RoboticVacuumCleaner.
func deviceTypeEvents() map[string]map[string][]uint32 {
	out := map[string]map[string][]uint32{}
	for id := range uint32(0x10000) {
		dt, ok := schema.DeviceTypeDefinitionOf(schema.DeviceTypeID(id))
		if !ok {
			continue
		}
		for i := range dt.Requirements {
			r := &dt.Requirements[i]
			if r.Element != schema.RequirementServerCluster || !r.HasID {
				continue
			}
			for j := range r.Requirements {
				n := &r.Requirements[j]
				if n.Element != schema.RequirementEvent || n.Conformance.Text != "M" || !n.Referent.Resolved || n.Referent.Provisional {
					continue
				}
				dk, ck := strconv.FormatUint(uint64(id), 10), strconv.FormatUint(uint64(r.ID), 10)
				if out[dk] == nil {
					out[dk] = map[string][]uint32{}
				}
				if !slices.Contains(out[dk][ck], n.Referent.ID) {
					out[dk][ck] = append(out[dk][ck], n.Referent.ID)
				}
			}
		}
	}
	return out
}

// deviceTypeEventsJSON is [deviceTypeEvents] as gen_pics.py reads it.
func deviceTypeEventsJSON() ([]byte, error) { return json.Marshal(deviceTypeEvents()) }

// TestDeviceTypeEvents: the device types whose endpoints the reference
// daemon serves with an event their cluster leaves optional —
// OperationCompletion (1) on OperationalState (0x0060) for LaundryWasher
// (0x0073) and on RvcOperationalState (0x0061) for RoboticVacuumCleaner
// (0x0074) — are in the table gen_pics.py reads.
func TestDeviceTypeEvents(t *testing.T) {
	t.Parallel()
	got := deviceTypeEvents()
	for _, c := range []struct {
		dt, cluster string
		event       uint32
	}{{"115", "96", 1}, {"116", "97", 1}} {
		if !slices.Contains(got[c.dt][c.cluster], c.event) {
			t.Errorf("device type %s cluster %s: events %v, want %d", c.dt, c.cluster, got[c.dt][c.cluster], c.event)
		}
	}
	if _, err := deviceTypeEventsJSON(); err != nil {
		t.Fatal(err)
	}
}
