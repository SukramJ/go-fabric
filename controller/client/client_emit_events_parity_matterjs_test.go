// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
)

// TestHostReadDoesNotEmitEvents (W4-ctlim-01): a host's Node.Read with an
// event path returns the events in its Report, but neither calls
// Hooks.Event nor raises the persisted event cursor — only the default
// subscription while current and the newly-commissioned read emit.
// Mirrors matter.js ClientStructure.ts:412 (emitEvents defaults to false),
// :1757-1770 (#emitOccurrence), ClientNodeInteraction.ts:97-98 (a read
// passes the request's own emitEvents) and NetworkClient.ts:171-173,
// :303-308 (the two interactions that set it).
func TestHostReadDoesNotEmitEvents(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, seedCommissioned(3), nil)
	n := sr.reg.Get("peer1")
	if err := n.Start(context.Background()); err != nil { // no autoSubscribe
		t.Fatal(err)
	}
	type result struct {
		rep *client.Report
		err error
	}
	read := make(chan result, 1)
	go func() {
		rep, err := n.Read(context.Background(), client.ReadRequest{ReadRequest: interaction.ReadRequest{Events: []im.ConcreteEventPath{{}}}})
		read <- result{rep, err}
	}()
	m := sr.next(t)
	sr.reply(t, m, im.InteractionModelProtocolID, im.OpcodeReportData, dataReport(t, 0, nil,
		eventReport(uint64(basicinfo.EventStartUp), 500)))
	r := <-read
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.rep.Events) != 1 || r.rep.Events[0].Number != 500 {
		t.Fatalf("report events %+v, want event 500", r.rep.Events)
	}
	client.WaitRegistry(sr.reg)
	sr.hooks.mu.Lock()
	hooked := len(sr.hooks.events)
	sr.hooks.mu.Unlock()
	if hooked != 0 {
		t.Errorf("Hooks.Event called %d times for a host read", hooked)
	}
	if got := client.MaxEventNumber(n); got != 0 {
		t.Errorf("event cursor %d after a host read, want 0", got)
	}
}
