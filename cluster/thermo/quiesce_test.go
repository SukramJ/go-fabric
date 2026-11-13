// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestThermostatMatterQuiesceStopsItsTimers: the bridge quiesces a server
// that leaves the topology through contract.ClusterQuiescer
// (bridge.quiesceServers). Afterwards neither the ThermostatSuggestions
// timer nor an atomic write's timeout is armed, and advancing the clock
// past both changes nothing: no preset is activated and no change is
// reported for a device the bridge no longer serves. The server stays
// usable: a later re-evaluation arms the suggestion timer again (review
// II, R2-P1-clu-02).
func TestThermostatMatterQuiesceStopsItsTimers(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	at := clk.epoch() + 600
	if _, err := addSuggestion(srv, handleB, &at, 30); err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	ctx = im.WithSubject(ctx, 1, nil)
	ctx = im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
	ctx = im.WithCommandPath(ctx, im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdAtomicRequest, HasEndpoint: true})
	timeout := uint16(5000)
	resp, err := srv.MatterInvoke(ctx, tdef.CmdAtomicRequest, tdef.AtomicRequestRequest{
		RequestType: 0, AttributeRequests: []uint32{tdef.AttrPresets}, Timeout: &timeout, // BeginWrite
	})
	if err != nil || resp.(tdef.AtomicResponse).StatusCode != tdef.StatusSuccess {
		t.Fatalf("BeginWrite: %+v, %v", resp, err)
	}
	if n := clk.Pending(); n != 2 {
		t.Fatalf("%d timers armed, want the suggestion's and the atomic write's", n)
	}
	changes := recordChanges(srv)

	q, ok := any(srv).(contract.ClusterQuiescer)
	if !ok {
		t.Fatal("ThermostatServer is not a contract.ClusterQuiescer: the bridge cannot stop its timers")
	}
	q.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce, want 0", n)
	}
	clk.Advance(601 * time.Second) // past the suggestion's EffectiveTime and the write's timeout
	if got := srv.ActivePresetHandle(); got != nil {
		t.Errorf("ActivePresetHandle %q after MatterQuiesce: the suggestion timer fired", got)
	}
	if got := changes(); len(got) != 0 {
		t.Errorf("changes %v reported after MatterQuiesce", got)
	}

	// Usable again: a moved Matter time re-evaluates and arms anew.
	srv.MatterTimeChanged()
	clk.Advance(0)
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q after a re-evaluation following MatterQuiesce, want %q", got, handleB)
	}
}

// TestThermostatReAddedAfterMatterQuiesceArmsItsSuggestion (review II,
// R2-P1-dr-rest-03): a reassembly drops the server (MatterQuiesce) and a
// later one puts the same object back, stamping its endpoint
// (bridge.Reassemble → SetEndpoint). The suggestion it held becomes current
// at its EffectiveTime without waiting for a command or a moved time.
func TestThermostatReAddedAfterMatterQuiesceArmsItsSuggestion(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	at := clk.epoch() + 600
	if _, err := addSuggestion(srv, handleB, &at, 30); err != nil {
		t.Fatal(err)
	}
	srv.MatterQuiesce()
	clk.Advance(10 * time.Second)
	srv.SetEndpoint(1) // the later reassembly
	clk.Advance(0)
	clk.Advance(600 * time.Second)
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q past the EffectiveTime after the re-add, want %q", got, handleB)
	}
}
