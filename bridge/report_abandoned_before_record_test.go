// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestReportAbandonedBeforeItsRecordIsAFailedUpdate: a report whose last
// message ran out of retransmissions before the reporter stored its record
// is one failed update all the same — its data is owed again and the
// failure is counted, as for a report abandoned after the store. matter.js
// awaits the report's delivery inside the update and requeues on failure
// (packages/node/src/node/server/ServerSubscription.ts:553-603 at
// db021da0), so the order of the two steps is no question there (review
// II, R2-P4-nv-07).
func TestReportAbandonedBeforeItsRecordIsAFailedUpdate(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1601)
	raw, ok := r.dev.br.routing.subTargets.Load(r.sub.ID)
	if !ok {
		t.Fatal("no routing target for the subscription")
	}
	target, ok := raw.(subTarget)
	if !ok {
		t.Fatalf("routing target %T", raw)
	}
	report := im.ReportData{SubscriptionID: r.sub.ID, HasSubscription: true}
	report.Reports = []im.AttributeReport{{Path: undeliveredReportPath, Value: im.AttributeValue{Value: "x"}}}
	counters, _, err := r.dev.br.sendInitiatedReport(target, report)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(counters) == 0 {
		t.Fatal("the report's message was not tracked")
	}
	// The message is abandoned before the record exists: the pump finds
	// nothing to fail.
	r.exhaust()
	if n := r.outbound.Pending(); n != 0 {
		t.Fatalf("%d entries pending after the exhaustion", n)
	}
	owed := &owedReport{subID: r.sub.ID, paths: []im.ConcreteAttributePath{undeliveredReportPath}}
	r.dev.br.recordOngoingReport(r.reportSID, counters, owed)
	if !owed.settled.Load() {
		t.Error("the abandoned report's update is not settled")
	}

	if _, held := r.dev.br.reportCounterOwner.Load(reportCounterKey(r.reportSID, counters[len(counters)-1])); held {
		t.Error("the abandoned report's record is still held")
	}
	failures, counted := r.dev.br.subSendErrorCount.Load(r.sub.ID)
	if !counted || failures != 1 {
		t.Fatalf("failed updates = %v (counted %v), want 1: a report abandoned before its record is a failed update", failures, counted)
	}
	// What it carried is owed again, and the next update carries it — a
	// change starts it (F-db021da0-3).
	r.dev.subs.OnAttributeChanged(undeliveredReportPath)
	r.pending(t)
	r.sessionsKept(t, "after the failed update")
}

// TestOutboundReliableRemembersAnAbandonedMessage: the tracker tells an
// abandoned message of a secure session from an acknowledged one after it
// left the pending set, for as long again as it was pending, and forgets
// it when its session is dropped (review II, R2-P4-nv-07).
func TestOutboundReliableRemembersAnAbandonedMessage(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, nil)
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	start := time.Now()
	send := func(net.Addr, []byte) error { return nil }
	tr.Track(7, 0x10, 1, []byte{1}, dest, start)
	tr.Track(8, 0x10, 2, []byte{2}, dest, start)
	if !tr.Ack(0x10, 8) {
		t.Fatal("the acknowledgement found no entry")
	}
	var abandonedAt time.Time
	for i := 1; i <= mrp.MaxRetransmissions+1 && tr.Pending() > 0; i++ {
		abandonedAt = start.Add(time.Duration(i) * time.Hour)
		tr.Tick(abandonedAt, send)
	}
	if tr.Pending() != 0 {
		t.Fatal("the message was not abandoned")
	}
	if !tr.Abandoned(0x10, 7) {
		t.Fatal("an abandoned message is not reported abandoned")
	}
	if tr.Abandoned(0x10, 8) {
		t.Error("an acknowledged message is reported abandoned")
	}
	tr.Tick(abandonedAt.Add(abandonedAt.Sub(start)-time.Nanosecond), send)
	if !tr.Abandoned(0x10, 7) {
		t.Error("forgotten before as long again as it was pending")
	}
	tr.DropSession(0x10)
	if tr.Abandoned(0x10, 7) {
		t.Error("remembered after its session was dropped")
	}
	tr.Track(9, 0x11, 3, []byte{3}, dest, start)
	for i := 1; i <= mrp.MaxRetransmissions+1 && tr.Pending() > 0; i++ {
		abandonedAt = start.Add(time.Duration(i) * time.Hour)
		tr.Tick(abandonedAt, send)
	}
	tr.Tick(abandonedAt.Add(abandonedAt.Sub(start)), send)
	if tr.Abandoned(0x11, 9) {
		t.Error("remembered after as long again as it was pending")
	}
}
