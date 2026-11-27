// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	_ "modernc.org/sqlite"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "p.db"))
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
	synctest.Test(t, func(t *testing.T) {
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
			keepOperationalTime(kctx, nil, st, g, time.Millisecond, slog.New(slog.DiscardHandler))
			close(done)
		}()
		time.Sleep(20 * time.Millisecond) // twenty ticks of the bubble's virtual clock
		synctest.Wait()
		cancel()
		<-done
	})
}

// TestPersistedKeys: labels, versions and scene tables live under the
// daemon's keys, for StringKey stable keys only.
func TestPersistedKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := persisted{st: testStore(t), logger: slog.New(slog.DiscardHandler)}
	// Read through the deprecated port's adapter, which the facade's own
	// state backs (NewEndpointPersistence).
	l := legacyEndpoints{p}
	k := endpoint.StringKey("dev:1")
	l.OnNodeLabelWritten(k, "Hall")
	if got := l.NodeLabel(ctx, k); got != "Hall" {
		t.Errorf("label %q", got)
	}
	l.SaveScenes(k, []byte("table"))
	if got := string(l.LoadScenes(k)); got != "table" {
		t.Errorf("scenes %q", got)
	}
	// A host's own key type is persisted under its String() (issue #87).
	var other otherKey
	l.OnNodeLabelWritten(other, "x")
	l.SaveScenes(other, []byte("y"))
	if l.NodeLabel(ctx, other) != "x" || string(l.LoadScenes(other)) != "y" {
		t.Error("a non-StringKey was not persisted under its String()")
	}
	if v, ok, _ := p.st.GetSetting(ctx, endpointLabelKeyRoot+"other"); !ok || v != "x" {
		t.Errorf("label key for a host key: %q (%v)", v, ok)
	}
	l.OnNodeLabelWritten(nil, "x")
	if v, err := p.configVersion(ctx, nil); l.NodeLabel(ctx, nil) != "" || l.LoadScenes(nil) != nil || v != 0 || err != nil {
		t.Error("a nil key was persisted")
	}
	if v, err := p.configVersion(ctx, k); l.LoadScenes(endpoint.StringKey("none")) != nil || v != 0 || err != nil {
		t.Error("absent state read back")
	}
	if err := p.st.SetSetting(ctx, endpointConfigVersionKey+"dev:1", "x"); err != nil {
		t.Fatal(err)
	}
	if v, err := p.configVersion(ctx, k); v != 0 || err != nil {
		t.Errorf("a corrupt version read as %d, %v; want 0, nil (logged)", v, err)
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
	ring.Record(diagevent.Event{Kind: "k", Message: "m", Detail: map[string]string{"max_errors": "2", "lockout_duration": "1"}})
	if b, _ := r.Logs(context.Background(), 0); !strings.Contains(string(b), "k m lockout_duration=1 max_errors=2") {
		t.Errorf("ring %q", b)
	}
}

// TestRingLogsCarryNoPeerDetail (review II, R2-C2-sec-2451): the log a
// controller retrieves with RetrieveLogsRequest — Operate on any fabric —
// carries no peer address and no session id. The ring records every
// fabric's and every PASE peer's sessions and pairing attempts and its
// events name no fabric, so those details would show one fabric's
// subject another fabric's controllers and commissioners on no fabric.
// The event's time, kind and message stay, and so do the node-scoped
// details (the PASE lockout's).
func TestRingLogsCarryNoPeerDetail(t *testing.T) {
	t.Parallel()
	ring := diagevent.NewRing(8)
	r := ringLogs{ring}
	const addr = "[fd00::b]:5540"
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "A controller completed a secure-session handshake with the bridge.", Detail: map[string]string{"peer": addr}})
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "A controller closed its secure session with the bridge.", Detail: map[string]string{"peer": addr, "session_id": "4711"}})
	ring.Record(diagevent.Event{Kind: diagevent.KindPairing, Message: "A commissioner tried to pair.", Detail: map[string]string{"peer": addr}})
	ring.Record(diagevent.Event{Kind: diagevent.KindPairing, Message: "Pairing was locked.", Detail: map[string]string{"max_errors": "20", "lockout_duration": "1m0s"}})
	b, err := r.Logs(context.Background(), mattercore.IntentEndUserSupport)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, leak := range []string{addr, "peer=", "session_id=", "4711"} {
		if strings.Contains(out, leak) {
			t.Errorf("the retrieved log carries %q:\n%s", leak, out)
		}
	}
	for _, kept := range []string{"closed its secure session", "lockout_duration=1m0s max_errors=20"} {
		if !strings.Contains(out, kept) {
			t.Errorf("the retrieved log lost %q:\n%s", kept, out)
		}
	}
}

// TestRingLogsServeTheAccessingFabricsOwnSessions (review II,
// R2-C2-sec-2451, owner decision 524): a session event the bridge stamped
// with its fabric ("fabric_index") shows the accessing fabric's own
// controller its peer address and session id; another fabric's event, an
// event with no fabric (a PASE commissioner's) and every event read with
// no accessing fabric or fabric 0 (a PASE session) keep them out.
func TestRingLogsServeTheAccessingFabricsOwnSessions(t *testing.T) {
	t.Parallel()
	ring := diagevent.NewRing(8)
	r := ringLogs{ring}
	const own, other, pase = "[fd00::1]:5540", "[fd00::2]:5540", "[fd00::3]:5540"
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "opened", Detail: map[string]string{"peer": own, "fabric_index": "1"}})
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "closed", Detail: map[string]string{"peer": own, "session_id": "11", "fabric_index": "1"}})
	ring.Record(diagevent.Event{Kind: diagevent.KindSession, Message: "closed", Detail: map[string]string{"peer": other, "session_id": "22", "fabric_index": "2"}})
	ring.Record(diagevent.Event{Kind: diagevent.KindPairing, Message: "busy", Detail: map[string]string{"peer": pase}})
	read := func(ctx context.Context) string {
		b, err := r.Logs(ctx, mattercore.IntentEndUserSupport)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	out := read(contract.WithAccessingFabric(context.Background(), 1, false))
	for _, kept := range []string{"peer=" + own, "session_id=11"} {
		if !strings.Contains(out, kept) {
			t.Errorf("fabric 1's log lost its own %q:\n%s", kept, out)
		}
	}
	for _, leak := range []string{other, "session_id=22", pase} {
		if strings.Contains(out, leak) {
			t.Errorf("fabric 1's log carries %q:\n%s", leak, out)
		}
	}
	for name, ctx := range map[string]context.Context{
		"no accessing fabric": context.Background(),
		"fabric 0":            contract.WithAccessingFabric(context.Background(), 0, false),
		"fabric 3":            contract.WithAccessingFabric(context.Background(), 3, false),
	} {
		out := read(ctx)
		for _, leak := range []string{"peer=", "session_id="} {
			if strings.Contains(out, leak) {
				t.Errorf("%s: the log carries %q:\n%s", name, leak, out)
			}
		}
	}
}

// TestUniqueIDRotationOnOffOn (issue #122): over one store, a boot with
// rotation on serves a salted per-boot value and writes nothing, and the
// next boot with rotation off serves the value pinned before it, never a
// salted one; a value flagged by the deprecated marker is migrated once — matter.js creates uniqueId once and persists it
// (BasicInformationServer.ts:89-91 at db91886c); rotation is this
// module's dev knob (bootid) beside that rule.
func TestUniqueIDRotationOnOffOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	var salt [16]byte
	p := persisted{st: st, logger: slog.New(slog.DiscardHandler), salt: func() [16]byte { return salt }}
	derive := func() string { return "id-" + strconv.Itoa(int(salt[0])) }
	boot := func(rotation byte) string {
		t.Helper()
		salt = [16]byte{rotation}
		id, err := p.uniqueID(ctx, derive)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	stored := func() (string, bool) {
		v, ok, err := st.GetSetting(ctx, store.SettingUniqueID)
		if err != nil {
			t.Fatal(err)
		}
		return v, ok
	}

	// A fresh store booted with rotation on: a salted value, nothing pinned.
	if got := boot(7); got != "id-7" {
		t.Fatalf("rotated boot of a fresh store served %q", got)
	}
	if v, ok := stored(); ok {
		t.Fatalf("rotation pinned %q", v)
	}
	// Off: derived without salt and pinned.
	pinned := boot(0)
	if pinned != "id-0" {
		t.Fatalf("first boot without rotation served %q", pinned)
	}
	// On / off / on / off: every rotated boot serves its salted value and
	// writes nothing; every boot without rotation serves the pin.
	for i, rotation := range []byte{9, 0, 11, 0} {
		got := boot(rotation)
		want := pinned
		if rotation != 0 {
			want = "id-" + strconv.Itoa(int(rotation))
		}
		if got != want {
			t.Errorf("boot %d (salt %d) served %q, want %q", i, rotation, got, want)
		}
		if v, ok := stored(); !ok || v != pinned {
			t.Errorf("boot %d (salt %d): stored %q (%v), want the pin %q", i, rotation, v, ok, pinned)
		}
	}
}
