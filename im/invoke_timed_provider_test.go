// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// timedProviderDispatcher answers IsTimedCommand from a fixed set, so a
// test can tell the provider's answer from the schema table's.
type timedProviderDispatcher struct {
	invokeCountingDispatcher
	timed map[uint32]bool
	asked int
}

func (d *timedProviderDispatcher) IsTimedCommand(_ uint16, _, cmdID uint32) bool {
	d.asked++
	return d.timed[cmdID]
}

var _ TimedCommandProvider = (*timedProviderDispatcher)(nil)

// TestHandleInvokeRequest_TimedFromProvider: the IM's timed gate asks the
// dispatcher's [TimedCommandProvider] (the resolved server's model) instead
// of the schema table — matter.js reads `limits.timed` from the command's
// model, CommandInvokeResponse.ts:292-295. OnOff.On (0x0006/0x01) is not
// timed in the schema; a provider that says it is wins, and a timed
// interaction passes the same command through to the server.
func TestHandleInvokeRequest_TimedFromProvider(t *testing.T) {
	t.Parallel()
	path := ConcreteCommandPath{Endpoint: 1, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}
	req := InvokeRequest{Invokes: []CommandInvocation{{Path: path}}}

	d := &timedProviderDispatcher{timed: map[uint32]bool{0x01: true}}
	resp := HandleInvokeRequest(WithTimedInteraction(context.Background(), false), d, req)
	if len(resp.Responses) != 1 || !resp.Responses[0].IsStatus || resp.Responses[0].Status.Status != StatusNeedsTimedInteraction {
		t.Fatalf("untimed invoke of a provider-timed command: %+v, want NEEDS_TIMED_INTERACTION", resp.Responses)
	}
	if d.asked != 1 || d.invokes != 0 {
		t.Errorf("provider asked %d times, server invoked %d times; want 1 and 0", d.asked, d.invokes)
	}

	resp = HandleInvokeRequest(WithTimedInteraction(context.Background(), true), d, req)
	if len(resp.Responses) != 1 || (resp.Responses[0].IsStatus && resp.Responses[0].Status.Status == StatusNeedsTimedInteraction) {
		t.Fatalf("timed invoke: %+v, want the command dispatched", resp.Responses)
	}
	if d.invokes != 1 {
		t.Errorf("timed invoke reached the server %d times, want 1", d.invokes)
	}

	// A provider that says "not timed" wins over the schema table too:
	// AdministratorCommissioning.OpenCommissioningWindow is timed in the
	// schema, but the gate takes the resolved server's answer.
	acPath := ConcreteCommandPath{Endpoint: 0, Cluster: 0x003C, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true}
	resp = HandleInvokeRequest(WithTimedInteraction(context.Background(), false), d, InvokeRequest{Invokes: []CommandInvocation{{Path: acPath}}})
	if len(resp.Responses) == 1 && resp.Responses[0].IsStatus && resp.Responses[0].Status.Status == StatusNeedsTimedInteraction {
		t.Errorf("provider answered not-timed, gate still answered NEEDS_TIMED_INTERACTION")
	}
}

// TestHandleInvokeRequest_TimedSchemaFallback: a dispatcher without the
// provider is answered by schema.IsTimedInvoke — EnergyEvse.EnableCharging
// (0x0099/0x02) is "T" in the snapshot.
func TestHandleInvokeRequest_TimedSchemaFallback(t *testing.T) {
	t.Parallel()
	path := ConcreteCommandPath{Endpoint: 1, Cluster: 0x0099, Command: 0x02, HasEndpoint: true, HasCluster: true, HasCommand: true}
	d := &invokeCountingDispatcher{}
	resp := HandleInvokeRequest(WithTimedInteraction(context.Background(), false), d, InvokeRequest{Invokes: []CommandInvocation{{Path: path}}})
	if len(resp.Responses) != 1 || !resp.Responses[0].IsStatus || resp.Responses[0].Status.Status != StatusNeedsTimedInteraction {
		t.Fatalf("untimed EnableCharging: %+v, want NEEDS_TIMED_INTERACTION", resp.Responses)
	}
	if d.invokes != 0 {
		t.Errorf("untimed EnableCharging reached the server")
	}
}
