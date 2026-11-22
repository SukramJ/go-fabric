// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
	"thingfabric.dev/go-fabric/store"

	_ "modernc.org/sqlite"
)

func openStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "matter.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return store.New(db), db
}

// TestServerWithEveryFeatureMatchesTheDefinition holds the full selection
// the server serves against the generated definition.
func TestServerWithEveryFeatureMatchesTheDefinition(t *testing.T) {
	t.Parallel()
	cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
	cfg.Features |= lock.FeatureRID | lock.FeatureFGP | lock.FeatureFACE | lock.FeatureDPS
	cfg.NumberOfRfidUsersSupported, cfg.MinRfidCodeLength, cfg.MaxRfidCodeLength = 10, 4, 16
	srv, err := lock.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, lockdef.Definition, uint32(cfg.Features))
}

// TestNewRefusesAFeatureItDoesNotServe: ALIRO and ALBU stay out (FACE is
// served since phase 36, as matter.js's base server serves it).
func TestNewRefusesAFeatureItDoesNotServe(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]lock.Feature{
		"ALIRO":      lockdef.FeatureAliroProvisioning,
		"ALIRO+ALBU": lockdef.FeatureAliroProvisioning | lockdef.FeatureAliroBleuwb,
	} {
		cfg := lockState()
		cfg.Features |= f
		if _, err := lock.New(cfg); !errors.Is(err, lock.ErrConfig) {
			t.Errorf("%s: %v, want ErrConfig", name, err)
		}
	}
}

// TestRecordsPersistEncrypted: a lock on a store keeps its users and
// credentials across a new server on the same store, under the node's
// credential key, with no credential stored in clear (matter.js
// DoorLockServer.ts:153-164 and LockAuth.ts:129-278).
func TestRecordsPersistEncrypted(t *testing.T) {
	t.Parallel()
	st, db := openStore(t)
	ctx := withPairFabric()
	newLock := func() *lock.DoorLockServer {
		cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		cfg.Auth, cfg.AuthKey = st, "lock:front"
		srv, err := lock.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}

	first := newLock()
	if _, ok, _ := st.GetSetting(context.Background(), store.SettingDoorLockCredentialKey); !ok {
		t.Fatal("no credential key stored")
	}
	res, err := first.MatterInvoke(ctx, lockdef.CmdSetCredential, addPin(2, "4711", user1))
	if err != nil || res.(lockdef.SetCredentialResponse).Status != lockdef.StatusSuccess {
		t.Fatalf("SetCredential: %+v, %v", res, err)
	}
	name := "front"
	if _, err := first.MatterInvoke(ctx, lockdef.CmdSetUser, lockdef.SetUserRequest{
		OperationType: lockdef.DataOperationTypeModify, UserIndex: 1, UserName: spec.ValueOf(name),
		UserUniqueId: spec.NullOf[uint32](), UserStatus: spec.NullOf[lockdef.UserStatusEnum](),
		UserType: spec.NullOf[lockdef.UserTypeEnum](), CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT credential_data FROM matter_doorlock_credentials`)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("4711")) || bytes.Contains(data, []byte("1234")) {
			t.Errorf("credential stored in clear: %x", data)
		}
		n++
	}
	_ = rows.Close()
	if n != 2 {
		t.Fatalf("%d stored credentials, want 2", n)
	}

	second := newLock()
	if u, ok := second.User(1); !ok || u.UserName != "front" || len(u.Credentials) != 2 {
		t.Errorf("user 1 after the restart %+v (%v), want front with two credentials", u, ok)
	}
	for _, code := range []string{"1234", "4711"} {
		p := pin(code)
		if _, err := second.MatterInvoke(ctx, lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{PinCode: &p}); err != nil {
			t.Errorf("unlock with %s after the restart: %v", code, err)
		}
	}
}

// TestEmptySelectionIsNone: a configuration that names no feature serves
// none — matter.js's DoorLockServer without features — and accepts
// UnlockWithTimeout, as every selection does (brief BU; before v0.10.0 it
// served Unbolting without UnlockWithTimeout).
func TestEmptySelectionIsNone(t *testing.T) {
	t.Parallel()
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: &stubSourceFull{}})
	if fm, _ := srv.MatterRead(0xFFFC); fm != uint32(0) {
		t.Errorf("FeatureMap %v, want 0", fm)
	}
	if !slices.Contains(srv.MatterAcceptedCommands(), lockdef.CmdUnlockWithTimeout) {
		t.Error("UnlockWithTimeout not accepted by the empty selection")
	}
	if slices.Contains(srv.MatterAcceptedCommands(), lockdef.CmdUnboltDoor) {
		t.Error("UnboltDoor accepted without Unbolting")
	}
}

// stubSourceFull is a locked, observed StateSource.
type stubSourceFull struct{}

func (stubSourceFull) IsJammed() bool                           { return false }
func (stubSourceFull) IsLocked() (locked, observed bool)        { return true, true }
func (stubSourceFull) LockInvoke(context.Context, uint32) error { return nil }

// TestLockCommandPinIsCheckedWithoutCOTA holds that the PINCode of a lock
// command is authorized whatever the features: on a USR|PIN lock without
// COTA a wrong PIN is refused and counted, a right one names its user.
// Mirrors matter.js packages/node/src/behaviors/door-lock/DoorLockServer.ts
// at db021da0: unlockDoor passes request.pinCode to #actuate (:198-201),
// #authorize identifies it without a feature test (:1049-1080), an unknown
// PIN is #refuseWrongCode (:1175-1190), a known one runs #authorizeUser.
// matter.js's base fixture carries no COTA either (DoorLockServerTest.ts:62).
func TestLockCommandPinIsCheckedWithoutCOTA(t *testing.T) {
	t.Parallel()
	f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) { c.Features &^= lock.FeatureCOTA },
		scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))

	f.markEvents()
	if err := f.unlock("9999"); err == nil {
		t.Fatal("UnlockDoor with a wrong PIN succeeded, want FAILURE")
	}
	if got := f.lockStateNow(); got != stateLocked {
		t.Errorf("LockState after a wrong PIN = %v, want Locked", got)
	}
	ev := f.events()
	if len(ev.errors) != 1 || ev.errors[0].OperationError != lockdef.OperationErrorInvalidCredential {
		t.Errorf("LockOperationError after a wrong PIN = %+v, want one InvalidCredential", ev.errors)
	}

	f.markEvents()
	if err := f.unlock("1234"); err != nil {
		t.Fatalf("UnlockDoor with the right PIN: %v", err)
	}
	ev = f.events()
	if len(ev.operations) != 1 || ev.operations[0].UserIndex.Null || ev.operations[0].UserIndex.Value != 1 {
		t.Errorf("LockOperation after the right PIN = %+v, want UserIndex 1", ev.operations)
	}
}
