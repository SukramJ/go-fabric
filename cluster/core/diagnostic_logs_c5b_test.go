// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
)

// dlStartHoldA2 holds StartLogCollection until release is closed and
// records whether its context was cancelled by then.
type dlStartHoldA2 struct {
	*dlCollector
	entered  chan struct{}
	release  chan struct{}
	startErr chan error
}

func (c *dlStartHoldA2) StartLogCollection(ctx context.Context, intent uint8) (core.LogSession, core.LogTimestamps, error) {
	close(c.entered)
	<-c.release
	err := ctx.Err()
	c.startErr <- err
	if err != nil {
		return nil, core.LogTimestamps{}, err
	}
	return c.dlCollector.StartLogCollection(ctx, intent)
}

// TestDiagLogsBDX_QuiesceCancelsTheStartingCollection (review II,
// R4-conc-4155): a MatterQuiesce that lands while the collector starts
// the collection cancels the context the collector was called with — the
// transfer's — so the start ends cancelled, the command is Denied and the
// quiesce returns, instead of waiting for a whole transfer it never
// cancelled.
func TestDiagLogsBDX_QuiesceCancelsTheStartingCollection(t *testing.T) {
	t.Parallel()
	c := &dlStartHoldA2{
		dlCollector: &dlCollector{logs: make([]byte, 5*1024), ended: make(chan error, 1)},
		entered:     make(chan struct{}), release: make(chan struct{}), startErr: make(chan error, 1),
	}
	d := core.NewDiagnosticLogs()
	d.AttachCollector(c)
	d.AttachExchangeOpener(newDLOpener())

	ctx, after := invokeCtx()
	answered := make(chan core.RetrieveLogsResponse, 1)
	go func() {
		resp, _ := d.MatterInvoke(ctx, 0x00, bdxRequest("logs.txt"))
		r, _ := resp.(core.RetrieveLogsResponse)
		answered <- r
	}()
	<-c.entered

	header := make(chan string, 1)
	quiesced := make(chan struct{})
	go func() {
		defer close(quiesced)
		header <- goroutineHeaderA2()
		d.MatterQuiesce()
	}()
	waitParkedOrDoneA2(t, <-header, "MatterQuiesce", "sync.(*Cond).Wait", quiesced)
	close(c.release)

	if err := <-c.startErr; err == nil {
		t.Error("StartLogCollection's context not cancelled by the quiesce")
	}
	if r := <-answered; r.Status != core.LogStatusDenied {
		t.Errorf("request = %+v, want Denied", r)
	}
	after.Run()
	<-quiesced
}
