// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// paseCloseBridge is a bridge with only the session table wired — no node
// facade — holding one PASE session that AddNOC adopted onto fabric 1.
func paseCloseBridge(t *testing.T) (*Bridge, *operational.Manager, *clocktest.Fake) {
	t.Helper()
	mgr := operational.NewManager(nil)
	e, err := mgr.OpenFromPase(1, 2, 7, []byte("pase-shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	if err := mgr.AdoptFabricIndex(e.SessionID, 1); err != nil {
		t.Fatalf("AdoptFabricIndex: %v", err)
	}
	clk := clocktest.New(time.Unix(0, 0))
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: clk}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.AttachSessionRegistry(mgr)
	return b, mgr, clk
}

func openPASESessions(mgr *operational.Manager) int {
	count := 0
	for _, s := range mgr.Sessions() {
		if s.IsPASE {
			count++
		}
	}
	return count
}

func commissioningCompleteAnswer(code uint8) im.InvokeResponse {
	return im.InvokeResponse{Responses: []im.InvokeResponseEntry{{
		Path:        im.ConcreteCommandPath{Endpoint: 0, Cluster: generalCommissioningClusterID, Command: 0x04, HasEndpoint: true},
		HasResponse: true,
		Response:    core.CommissioningCompleteResponse{ErrorCode: code},
	}}}
}

// TestPASEClosedOnCommissioningCompleteParity pins Matter §11.10.6.6 step 4
// as matter.js packages/protocol/src/common/FailsafeContext.ts:156-157 at
// b25ed960 performs it (completeCommission → closePaseSession, awaited
// before the response is built, GeneralCommissioningServer.ts:255), in the
// bridge: the PASE session — the one AddNOC adopted onto the fabric
// included — is cleared at once, before the CommissioningComplete response
// goes out and with no timer, and a PASE session a new commissioning
// establishes afterwards stays when the response is sent and time passes.
func TestPASEClosedOnCommissioningCompleteParity(t *testing.T) {
	t.Parallel()
	b, mgr, clk := paseCloseBridge(t)
	after := &im.AfterResponse{}
	ctx := im.WithAfterResponse(context.Background(), after)

	b.closePASESessionsOnCommissioningComplete(ctx, commissioningCompleteAnswer(core.CommissioningErrorOK))
	if got := openPASESessions(mgr); got != 0 {
		t.Fatalf("PASE sessions before the response = %d, want 0 (cleared inside the command)", got)
	}
	if got := clk.Pending(); got != 0 {
		t.Errorf("timers armed by the close = %d, want 0", got)
	}
	// A new commissioning's PASE session is not this fail-safe context's:
	// neither the response nor time closes it.
	if _, err := mgr.OpenFromPase(1, 3, 8, []byte("next-pase-secret")); err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	after.Run()
	clk.Advance(time.Minute)
	if got := openPASESessions(mgr); got != 1 {
		t.Errorf("PASE sessions after CommissioningComplete = %d, want 1 (only the new commissioning's)", got)
	}
}

// TestPASEStaysOpenDuringCommissioning: nothing but a CommissioningComplete
// answered OK clears the PASE session — a refused CommissioningComplete
// (sent over PASE: InvalidAuthentication) and the commands of a running
// commissioning leave the channel they travel on alone.
func TestPASEStaysOpenDuringCommissioning(t *testing.T) {
	t.Parallel()
	answers := map[string]im.InvokeResponse{
		"CommissioningComplete refused":             commissioningCompleteAnswer(core.CommissioningErrorInvalidAuthentication),
		"CommissioningComplete without a fail-safe": commissioningCompleteAnswer(core.CommissioningErrorNoFailSafe),
		"ArmFailSafe": {Responses: []im.InvokeResponseEntry{{
			Path:        im.ConcreteCommandPath{Endpoint: 0, Cluster: generalCommissioningClusterID, Command: 0x00, HasEndpoint: true},
			HasResponse: true,
			Response:    core.ArmFailSafeResponse{ErrorCode: core.CommissioningErrorOK},
		}}},
		"AddNOC": {Responses: []im.InvokeResponseEntry{{
			Path:        im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003E, Command: 0x06, HasEndpoint: true},
			HasResponse: true,
			Response:    core.NOCResponse{StatusCode: core.NOCStatusOK, FabricIndex: 1},
		}}},
		"a status": {Responses: []im.InvokeResponseEntry{{
			Path:     im.ConcreteCommandPath{Endpoint: 0, Cluster: generalCommissioningClusterID, Command: 0x04, HasEndpoint: true},
			IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedAccess},
		}}},
	}
	for name, resp := range answers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, mgr, clk := paseCloseBridge(t)
			after := &im.AfterResponse{}
			b.closePASESessionsOnCommissioningComplete(im.WithAfterResponse(context.Background(), after), resp)
			after.Run()
			clk.Advance(time.Minute)
			if got := openPASESessions(mgr); got != 1 {
				t.Fatalf("PASE sessions = %d, want the commissioning's 1", got)
			}
		})
	}
}

// TestFailSafeExpiredClosesThePASESessionsParity pins the fail-safe
// rollback's step 1, matter.js
// packages/protocol/src/common/FailsafeContext.ts:281-283 at b25ed960
// (closePaseSession), on a bridge without the node facade: a timer expiry
// clears the PASE sessions at once, an expiry inside an interaction
// (ArmFailSafe with expiry 0) after its response.
func TestFailSafeExpiredClosesThePASESessionsParity(t *testing.T) {
	t.Parallel()
	t.Run("timer", func(t *testing.T) {
		t.Parallel()
		b, mgr, _ := paseCloseBridge(t)
		b.FailSafeExpired(context.Background())
		if got := openPASESessions(mgr); got != 0 {
			t.Errorf("PASE sessions after the fail-safe expired = %d, want 0", got)
		}
	})
	t.Run("in an interaction", func(t *testing.T) {
		t.Parallel()
		b, mgr, _ := paseCloseBridge(t)
		after := &im.AfterResponse{}
		b.FailSafeExpired(im.WithAfterResponse(context.Background(), after))
		if got := openPASESessions(mgr); got != 1 {
			t.Fatalf("PASE sessions before the response = %d, want 1", got)
		}
		after.Run()
		if got := openPASESessions(mgr); got != 0 {
			t.Errorf("PASE sessions after the response = %d, want 0", got)
		}
	})
	t.Run("a registry that lists no sessions", func(t *testing.T) {
		t.Parallel()
		b := newUnstartedBridge(t)
		reg := &fakeSessionRegistry{}
		b.AttachSessionRegistry(reg)
		b.FailSafeExpired(context.Background())
		if len(reg.closed) != 0 {
			t.Errorf("closed %v without knowing which sessions are PASE", reg.closed)
		}
	})
}
