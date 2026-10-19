// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/store"

	_ "modernc.org/sqlite"
)

func openStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "matter.db"))
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
	cfg.Features |= lock.FeatureRID | lock.FeatureFGP | lock.FeatureDPS
	cfg.NumberOfRfidUsersSupported, cfg.MinRfidCodeLength, cfg.MaxRfidCodeLength = 10, 4, 16
	srv, err := lock.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, lockdef.Definition, uint32(cfg.Features))
}

// TestNewRefusesAFeatureItDoesNotServe: ALIRO, ALBU and FACE stay out.
func TestNewRefusesAFeatureItDoesNotServe(t *testing.T) {
	t.Parallel()
	cfg := lockState()
	cfg.Features |= lockdef.FeatureFaceCredentials
	if _, err := lock.New(cfg); !errors.Is(err, lock.ErrConfig) {
		t.Errorf("FACE: %v, want ErrConfig", err)
	}
}

// TestRecordsPersistEncrypted: a lock on a store keeps its users and
// credentials across a new server on the same store, under the node's
// credential key, with no credential stored in clear (matter.js
// DoorLockServer.ts:153-164, LockAuth.Store :129-278).
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

// TestDefaultSelectionServesAsBefore: a configuration that names no
// feature serves the former selection, Unbolting without UnlockWithTimeout.
func TestDefaultSelectionServesAsBefore(t *testing.T) {
	t.Parallel()
	srv := lock.NewDoorLockServer(lock.DoorLockConfig{Source: &stubSourceFull{}})
	if fm, _ := srv.MatterRead(0xFFFC); fm != uint32(lockdef.FeatureUnbolting) {
		t.Errorf("FeatureMap %v, want Unbolting", fm)
	}
	for _, id := range srv.MatterAcceptedCommands() {
		if id == lockdef.CmdUnlockWithTimeout {
			t.Error("UnlockWithTimeout accepted by the default selection")
		}
	}
}

// stubSourceFull is a locked, observed StateSource.
type stubSourceFull struct{}

func (stubSourceFull) IsJammed() bool                           { return false }
func (stubSourceFull) IsLocked() (locked, observed bool)        { return true, true }
func (stubSourceFull) LockInvoke(context.Context, uint32) error { return nil }
