// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The reference daemon's guards over the wiring the node took over from it
// (brief R): moved here with the code they test, assertions unchanged.
package node

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestCaseInitiatorProviderNeedsALoadedIdentity: the provider the bridge
// dials former-subscription peers with refuses a fabric whose CASE
// identity is not loaded, and leaves no session id reserved behind.
func TestCaseInitiatorProviderNeedsALoadedIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	sessions := operational.NewManager(st)
	before := sessions.Occupancy()
	provider := caseInitiatorProvider(st, sessions, newCaseIdentities(slog.Default(), nil), slog.Default())
	if init, err := provider(ctx, 1, 0xC0FFEE); err == nil || init != nil {
		t.Fatalf("provider(unknown fabric) = %v, %v; want an error", init, err)
	}
	if after := sessions.Occupancy(); after != before {
		t.Fatalf("session table occupancy %+v → %+v: a refused initiation must not reserve an id", before, after)
	}
}

// TestCaseIdentitiesResolveAndForget pins the per-fabric identity table the
// CASE responder resolves through: a resumed session answers as the fabric
// its record names, and a forgotten fabric is no longer resolvable — by
// index or as the latest identity.
func TestCaseIdentitiesResolveAndForget(t *testing.T) {
	t.Parallel()
	c := newCaseIdentities(nil, nil)
	one := &caseFabric{identity: &sigma.Identity{FabricIndex: 1, NodeID: 11}}
	two := &caseFabric{identity: &sigma.Identity{FabricIndex: 2, NodeID: 22}}
	c.byIdx[1], c.byIdx[2], c.latest = one, two, two
	if id, _, ok := c.ResolveFabricIndex(1); !ok || id.NodeID != 11 {
		t.Fatalf("ResolveFabricIndex(1) = %+v, %v; want fabric 1's identity", id, ok)
	}
	if _, nodeID, ok := c.forget(2); !ok || nodeID != 22 {
		t.Fatalf("forget(2) = %d, %v", nodeID, ok)
	}
	if _, _, ok := c.ResolveFabricIndex(2); ok {
		t.Fatal("a forgotten fabric still resolves")
	}
	if cur := c.current(); cur != one {
		t.Fatalf("latest after forgetting it: %+v, want fabric 1", cur)
	}
	if _, _, ok := c.forget(9); ok {
		t.Fatal("forgetting an unknown fabric reported one")
	}
}

// TestOperationalHoursSurviveARestart: the operational time is seeded from
// the settings table and stored back in milliseconds, so a reboot does not
// reset it (TC-DGGEN-2.1 step 10c) and a run shorter than an hour still
// counts (matter.js totalOperationalHoursCounter). A database of an earlier
// release, which kept whole hours, is migrated.
func TestOperationalHoursSurviveARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	if d, err := loadOperationalTime(ctx, st); err != nil || d != 0 {
		t.Fatalf("operational time on a new database = %v, %v", d, err)
	}

	// An earlier release's whole hours migrate to milliseconds.
	if err := st.SetSetting(ctx, legacyOperationalHoursSetting, "3"); err != nil {
		t.Fatal(err)
	}
	d, err := loadOperationalTime(ctx, st)
	if err != nil || d != 3*time.Hour {
		t.Fatalf("legacy hours loaded as %v, %v; want 3h", d, err)
	}
	g := mattercore.NewGeneralDiagnostics(mattercore.BootReasonPowerOnReboot)
	g.SetPersistedOperationalTime(d + 59*time.Minute)
	if err := storeOperationalTime(ctx, st, g); err != nil {
		t.Fatal(err)
	}
	if raw, _, _ := st.GetSetting(ctx, operationalTimeSetting); !strings.HasPrefix(raw, "14340") {
		t.Fatalf("stored %q ms; want 3h59m = 14340000 ms and change", raw)
	}
	// The part-hour survives: a restart starting at 3h59m reads 3 hours,
	// and the next minute makes it 4.
	d, err = loadOperationalTime(ctx, st)
	if err != nil || d < 3*time.Hour+59*time.Minute || d > 4*time.Hour {
		t.Fatalf("operational time after a store = %v, %v; want 3h59m", d, err)
	}
	g2 := mattercore.NewGeneralDiagnostics(mattercore.BootReasonPowerOnReboot)
	g2.SetPersistedOperationalTime(d + time.Minute)
	if v, _ := g2.MatterRead(0x0003); v != uint32(4) {
		t.Fatalf("TotalOperationalHours = %v after 3h59m + 1m; want 4", v)
	}

	if err := st.SetSetting(ctx, operationalTimeSetting, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOperationalTime(ctx, st); err == nil {
		t.Fatal("a corrupt millisecond count was accepted")
	}
	if err := st.SetSetting(ctx, operationalTimeSetting, "1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, legacyOperationalHoursSetting, "x"); err != nil {
		t.Fatal(err)
	}
	if d, err := loadOperationalTime(ctx, st); err != nil || d != time.Millisecond {
		t.Fatalf("the millisecond count must win over the legacy hours: %v, %v", d, err)
	}
}

// TestRuntimeHeapAndUnknownEthernet: SoftwareDiagnostics reads the Go heap;
// the Ethernet reporter knows nothing.
func TestRuntimeHeapAndUnknownEthernet(t *testing.T) {
	t.Parallel()
	if used, ok := (runtimeHeap{}).CurrentHeapUsed(); !ok || used == 0 {
		t.Errorf("heap used %d", used)
	}
	if _, ok := (runtimeHeap{}).CurrentHeapFree(); !ok {
		t.Error("heap free unknown")
	}
	e := unknownEthernet{}
	if _, ok := e.TimeSinceReset(); ok {
		t.Error("TimeSinceReset known")
	}
}
