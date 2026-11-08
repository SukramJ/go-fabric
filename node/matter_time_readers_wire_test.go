// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	diaglogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// switchedFaults is a fault port whose hardware list a test sets.
type switchedFaults struct {
	mu  sync.Mutex
	hw  []uint8
	cbs []func()
}

func (f *switchedFaults) MatterActiveFaults() contract.Faults {
	f.mu.Lock()
	defer f.mu.Unlock()
	return contract.Faults{Hardware: append([]uint8(nil), f.hw...)}
}

func (f *switchedFaults) OnMatterValueChanged(cb func()) func() {
	f.mu.Lock()
	f.cbs = append(f.cbs, cb)
	f.mu.Unlock()
	return func() {}
}

func (f *switchedFaults) set(hw ...uint8) {
	f.mu.Lock()
	f.hw = hw
	cbs := append([]func(){}, f.cbs...)
	f.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestRootReadersFollowTheNodesUTC (issue 445): after SetUTCTime moves the
// root's UTCTime an hour away from the host clock, the root's other
// readers of real time follow it — an event's EpochTimestamp (chip
// EventManagement.cpp:433-439 at 6170af84, GetClock_RealTimeMS) and
// DiagnosticLogs' UTCTimeStamp — not the host clock, as connectedhomeip's
// servers read the one system clock SetUTCTime sets
// (TimeSynchronizationCluster.cpp:856-872).
func TestRootReadersFollowTheNodesUTC(t *testing.T) {
	t.Parallel()
	faults := &switchedFaults{}
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(cfg *node.Config) { cfg.Faults = faults },
	}})
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	set := time.Now().Add(time.Hour)
	res, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: tsdef.ClusterID, Command: tsdef.CmdSetUtcTime},
		tsdef.SetUtcTimeRequest{UtcTime: uint64(set.UnixMicro() - 946684800_000_000), Granularity: tsdef.GranularityMillisecondsGranularity}) //nolint:gosec // after the epoch
	if err != nil || res.Status != im.StatusSuccess {
		t.Fatalf("SetUTCTime: %+v %v", res, err)
	}
	nearSet := func(unixMS int64) bool {
		d := unixMS - set.UnixMilli()
		return d > -60_000 && d < 60_000
	}

	// The event log's stamp.
	// The server re-reads the faults on its own goroutine: read until the
	// event is there.
	faults.set(uint8(gendiagdef.HardwareFaultSensor))
	var events []fabrictest.EventData
	for range 100 {
		var err error
		events, err = fabrictest.Events(ctx, pair, im.ConcreteEventPath{Endpoint: 0, Cluster: gendiagdef.ClusterID, Event: gendiagdef.EventHardwareFaultChange, HasEndpoint: true, HasCluster: true, HasEvent: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 {
			break
		}
	}
	if len(events) == 0 {
		t.Fatal("no HardwareFaultChange event")
	}
	if ms := int64(events[len(events)-1].Timestamp); !nearSet(ms) { //nolint:gosec // milliseconds of this century
		t.Errorf("event EpochTimestamp %d ms, want the node's UTC near %d, not the host clock's %d", ms, set.UnixMilli(), time.Now().UnixMilli())
	}

	// DiagnosticLogs' UTCTimeStamp.
	res, err = fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: 0, Cluster: diaglogsdef.ClusterID, Command: diaglogsdef.CmdRetrieveLogsRequest},
		diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEndUserSupport, RequestedProtocol: diaglogsdef.TransferProtocolResponsePayload})
	if err != nil || res.Status != im.StatusSuccess || !res.HasFields {
		t.Fatalf("RetrieveLogs: %+v %v", res, err)
	}
	var resp diaglogsdef.RetrieveLogsResponse
	if err := resp.DecodeTLV(res.Fields); err != nil {
		t.Fatal(err)
	}
	if resp.UtcTimeStamp == nil {
		t.Fatalf("RetrieveLogs %+v carries no UTCTimeStamp", resp)
	}
	if ms := int64(*resp.UtcTimeStamp/1000) + 946684800_000; !nearSet(ms) { //nolint:gosec // microseconds of this century
		t.Errorf("UTCTimeStamp at %d ms, want the node's UTC near %d", ms, set.UnixMilli())
	}
}
