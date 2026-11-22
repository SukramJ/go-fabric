// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/core"
)

// rollbackInFlight expires gc's fail-safe through a session closer that
// names closed and blocks until release closes; it returns once the closer
// runs, the rollback in flight.
func rollbackInFlight(t *testing.T, gc *core.GeneralCommissioning, closed []uint16) (release, done chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	release, done = make(chan struct{}), make(chan struct{})
	gc.SetFailSafeSessionCloser(func(context.Context) []uint16 {
		close(entered)
		<-release
		return closed
	})
	if err := gc.ArmFailSafeFor(t.Context(), 60, 0); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		gc.ExpireFailSafe(context.Background())
	}()
	<-entered
	return release, done
}

// TestArmOnPaseSessionAfterTheRollbacksClose (R2-P2-comm-11, R2-P5-port-11):
// a PASE arm that waits for a rollback whose session closer closed other
// sessions arms its own; one whose session the closer named is refused; the
// older unnamed arm is refused by any close, as before.
func TestArmOnPaseSessionAfterTheRollbacksClose(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		arm     func(ctx context.Context, gc *core.GeneralCommissioning) error
		refused bool
	}{
		{"a session opened after the close", func(ctx context.Context, gc *core.GeneralCommissioning) error { return gc.ArmOnPaseSession(ctx, 7) }, false},
		{"a session the close named", func(ctx context.Context, gc *core.GeneralCommissioning) error { return gc.ArmOnPaseSession(ctx, 5) }, true},
		{"the unnamed arm", func(ctx context.Context, gc *core.GeneralCommissioning) error { return gc.ArmOnPaseEstablished(ctx) }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				gc := newGencomm(t, core.GeneralCommissioningConfig{})
				release, done := rollbackInFlight(t, gc, []uint16{5})
				armed := make(chan error, 1)
				go func() { armed <- c.arm(context.Background(), gc) }()
				// The arm waits for the rollback in flight.
				synctest.Wait()
				select {
				case err := <-armed:
					t.Fatalf("the arm returned (%v) while the rollback was in flight", err)
				default:
				}
				close(release)
				<-done
				err := <-armed
				if (err != nil) != c.refused {
					t.Fatalf("arm = %v, want refused %v", err, c.refused)
				}
				if gc.FailSafeArmed() == c.refused {
					t.Errorf("fail-safe armed %v after the arm, want %v", gc.FailSafeArmed(), !c.refused)
				}
				// The armed fail-safe's watch ends with it.
				gc.SetFailSafeSessionCloser(nil)
				gc.ExpireFailSafe(context.Background())
				synctest.Wait()
			})
		})
	}
}

// TestFailSafeExpiryNamesTheArmingAndTheAddedFabric (D-comm-4 (c),
// R2-P5-port-11): the expiry hook tells the fabric that armed the
// fail-safe from the one an AddNOC added, where the deprecated hook's one
// fabric is either.
func TestFailSafeExpiryNamesTheArmingAndTheAddedFabric(t *testing.T) {
	t.Parallel()
	gc := newGencomm(t, core.GeneralCommissioningConfig{})
	var got []core.FailSafeExpiry
	var old []uint8
	gc.SetOnFailSafeExpiry(func(_ context.Context, e core.FailSafeExpiry) { got = append(got, e) })
	gc.SetOnFailSafeExpired(func(_ context.Context, f uint8) { old = append(old, f) }) //nolint:staticcheck // the deprecated hook beside the new one
	if err := gc.ArmOnPaseSession(t.Context(), 9); err != nil {
		t.Fatal(err)
	}
	gc.SetCurrentFabric(3) // AddNOC installed fabric 3
	gc.ExpireFailSafe(t.Context())
	if len(got) != 1 || got[0] != (core.FailSafeExpiry{ArmedFabric: 0, AddedFabric: 3, OverPASE: true}) {
		t.Fatalf("SetOnFailSafeExpiry saw %+v, want armed over PASE, fabric 3 added", got)
	}
	if len(old) != 1 || old[0] != 3 {
		t.Errorf("the deprecated hook saw %v, want the one fabric 3", old)
	}

	if err := gc.ArmFailSafeFor(t.Context(), 60, 2); err != nil {
		t.Fatal(err)
	}
	gc.ExpireFailSafe(t.Context())
	if len(got) != 2 || got[1] != (core.FailSafeExpiry{ArmedFabric: 2}) {
		t.Errorf("an expiry armed over fabric 2's CASE session reported %+v, want ArmedFabric 2 only", got)
	}
}
