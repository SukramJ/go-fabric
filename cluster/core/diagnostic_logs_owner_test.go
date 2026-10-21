// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestDiagLogsBDX_QuiesceEndsTheTransfer pins DiagnosticLogs as the owner
// of its transfer goroutine (G1-14, ADR 0026 P5): MatterQuiesce cancels a
// transfer still streaming and returns after it ended — the collection
// is ended by then — and the next request is not Busy.
func TestDiagLogsBDX_QuiesceEndsTheTransfer(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{}) // never opened: the collection blocks until cancelled
	c := &dlCollector{logs: make([]byte, 5*1024), ended: make(chan error, 1), gate: gate}
	opener := newDLOpener()
	d := core.NewDiagnosticLogs()
	d.AttachCollector(c)
	d.AttachExchangeOpener(opener)

	ctx, after := invokeCtx()
	answered := make(chan struct{})
	go func() {
		_, _ = d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt"))
		close(answered)
	}()
	_, _, _ = opener.receive(t, false)
	<-answered
	after.Run() // the transfer starts and waits in CollectLog

	d.MatterQuiesce()
	select {
	case err := <-c.ended:
		if err == nil {
			t.Fatal("EndLogCollection(nil) for a cancelled transfer")
		}
	default:
		t.Fatal("MatterQuiesce returned before the transfer ended its collection")
	}
	// The next request starts a transfer (the receiver refuses it, so it
	// is Denied) instead of finding the server Busy.
	ctx2, _ := invokeCtx()
	second := make(chan core.RetrieveLogsResponse, 1)
	go func() {
		resp, _ := d.MatterInvoke(ctx2, 0x00, bdxRequest("logs2.txt"))
		r, _ := resp.(core.RetrieveLogsResponse)
		second <- r
	}()
	_, _, _ = opener.receive(t, true)
	if r := <-second; r.Status != core.LogStatusDenied {
		t.Fatalf("request after the quiesce: %+v, want Denied by the receiver, not Busy", r)
	}
}
