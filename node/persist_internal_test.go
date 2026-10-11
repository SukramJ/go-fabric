// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "p.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

// TestCountBootAndOperationalTime: RebootCount counts boots from 0, the
// operational time round-trips in milliseconds and the legacy hour count
// is read when the millisecond key is absent (the daemon's rules,
// wiring.go:951-1036).
func TestCountBootAndOperationalTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	for want := range uint16(3) {
		got, err := countBoot(ctx, st)
		if err != nil || got != want {
			t.Fatalf("boot %d: %d %v", want, got, err)
		}
	}
	if err := st.SetSetting(ctx, rebootCountSetting, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := countBoot(ctx, st); err == nil {
		t.Error("a corrupt RebootCount was accepted")
	}

	if d, err := loadOperationalTime(ctx, st); err != nil || d != 0 {
		t.Errorf("fresh store: %v %v", d, err)
	}
	if err := st.SetSetting(ctx, legacyOperationalHoursSetting, "3"); err != nil {
		t.Fatal(err)
	}
	if d, err := loadOperationalTime(ctx, st); err != nil || d != 3*time.Hour {
		t.Errorf("legacy hours: %v %v", d, err)
	}
	if err := st.SetSetting(ctx, legacyOperationalHoursSetting, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOperationalTime(ctx, st); err == nil {
		t.Error("a corrupt legacy count was accepted")
	}
	g := mattercore.NewGeneralDiagnostics(mattercore.BootReasonPowerOnReboot)
	g.SetPersistedOperationalTime(90 * time.Second)
	if err := storeOperationalTime(ctx, st, g); err != nil {
		t.Fatal(err)
	}
	if d, err := loadOperationalTime(ctx, st); err != nil || d < 90*time.Second {
		t.Errorf("stored time: %v %v", d, err)
	}
	if err := st.SetSetting(ctx, operationalTimeSetting, "-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOperationalTime(ctx, st); err == nil {
		t.Error("a corrupt millisecond count was accepted")
	}

	kctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		keepOperationalTime(kctx, st, g, time.Millisecond, slog.New(slog.DiscardHandler))
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
}

// TestPersistedKeys: labels, versions and scene tables live under the
// daemon's keys, for StringKey stable keys only.
func TestPersistedKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := persisted{st: testStore(t), logger: slog.New(slog.DiscardHandler)}
	k := endpoint.StringKey("dev:1")
	p.storeLabel(k, "Hall")
	if got := p.label(ctx, k); got != "Hall" {
		t.Errorf("label %q", got)
	}
	p.SaveScenes(k, []byte("table"))
	if got := string(p.LoadScenes(k)); got != "table" {
		t.Errorf("scenes %q", got)
	}
	var other otherKey
	p.storeLabel(other, "x")
	p.SaveScenes(other, []byte("x"))
	if p.label(ctx, other) != "" || p.LoadScenes(other) != nil || p.configVersion(ctx, other) != 0 {
		t.Error("a non-StringKey was persisted")
	}
	if p.LoadScenes(endpoint.StringKey("none")) != nil || p.configVersion(ctx, k) != 0 {
		t.Error("absent state read back")
	}
	if err := p.st.SetSetting(ctx, endpointConfigVersionKey+"dev:1", "x"); err != nil {
		t.Fatal(err)
	}
	if p.configVersion(ctx, k) != 0 {
		t.Error("a corrupt version was read")
	}
}

type otherKey struct{}

func (otherKey) String() string { return "other" }

// TestRingLogs: one line per event, NoLogs for CrashLogs, a line for an
// empty ring.
func TestRingLogs(t *testing.T) {
	t.Parallel()
	ring := diagevent.NewRing(4)
	r := ringLogs{ring}
	if b, _ := r.Logs(context.Background(), mattercore.IntentCrashLogs); b != nil {
		t.Errorf("crash logs %q", b)
	}
	if b, _ := r.Logs(context.Background(), 0); !strings.Contains(string(b), "no diagnostic events") {
		t.Errorf("empty ring %q", b)
	}
	ring.Record(diagevent.Event{Kind: "k", Message: "m", Detail: map[string]string{"b": "2", "a": "1"}})
	if b, _ := r.Logs(context.Background(), 0); !strings.Contains(string(b), "k m a=1 b=2") {
		t.Errorf("ring %q", b)
	}
}
