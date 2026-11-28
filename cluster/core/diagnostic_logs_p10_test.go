// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"thingfabric.dev/go-fabric/bdx"
	diaglogsdef "thingfabric.dev/go-fabric/cluster/spec/diagnosticlogs"
)

// quiesceCollectorP10 has a log too large for the response; a collection
// it is asked to start fails.
type quiesceCollectorP10 struct{}

func (quiesceCollectorP10) GetSizeForIntent(context.Context, uint8) (uint64, error) {
	return 2 * MatterDiagnosticLogsInlineCap, nil
}

func (quiesceCollectorP10) GetLogForIntent(context.Context, uint8, int) ([]byte, LogTimestamps, error) {
	return nil, LogTimestamps{}, ErrNoLogs
}

func (quiesceCollectorP10) StartLogCollection(context.Context, uint8) (LogSession, LogTimestamps, error) {
	return nil, LogTimestamps{}, errors.New("no collection")
}

type quiesceOpenerP10 struct{}

func (quiesceOpenerP10) OpenSessionExchange(context.Context, uint16) (bdx.Exchange, error) {
	return nil, errors.New("no exchange")
}

// TestDiagLogsRequestWhileQuiescingIsBusy (review II, P10-test-6802): a
// RetrieveLogsRequest for BDX that arrives while MatterQuiesce waits — the
// transfer it cancelled has ended (busy fell) but the quiesce has not
// returned — answers Busy and starts no transfer behind the quiesce's
// cancel (R4-conc-4155). The transfer's end is set without its signal, so
// the quiesce stays parked while the request is served.
func TestDiagLogsRequestWhileQuiescingIsBusy(t *testing.T) {
	t.Parallel()
	d := NewDiagnosticLogs()
	d.AttachCollector(quiesceCollectorP10{})
	d.AttachExchangeOpener(quiesceOpenerP10{})
	d.mu.Lock()
	d.busy = true // a transfer runs
	d.mu.Unlock()

	quiesced := make(chan struct{})
	go func() {
		defer close(quiesced)
		d.MatterQuiesce()
	}()
	// quiescing is set under mu, which MatterQuiesce holds until idle.Wait
	// releases it: seen true, the quiesce waits.
	for {
		d.mu.Lock()
		q := d.quiescing
		if q {
			d.busy = false // the transfer ended; its signal is withheld
		}
		d.mu.Unlock()
		if q {
			break
		}
		runtime.Gosched()
	}

	designator := "logs.txt"
	resp, err := d.MatterInvoke(context.Background(), diaglogsCmdRetrieveLogsRequest, diaglogsdef.RetrieveLogsRequestRequest{
		Intent: diaglogsdef.IntentEndUserSupport, RequestedProtocol: diaglogsdef.TransferProtocolBdx, TransferFileDesignator: &designator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := resp.(RetrieveLogsResponse); !ok || r.Status != LogStatusBusy {
		t.Errorf("request while the quiesce waits = %+v, want Busy", resp)
	}
	d.done()
	<-quiesced
}
