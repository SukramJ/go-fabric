// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/store"
)

// TestRefusedStartStoresNoCredentialKey (R4-store-4201): with the
// credential key setting lost and the records kept, New refuses with
// ErrConfig (decision 569) and leaves no credential key in the store — a
// key stored by a refused start would be the key the next start loads,
// under which the kept credentials never decrypt.
func TestRefusedStartStoresNoCredentialKey(t *testing.T) {
	t.Parallel()
	st, _ := openStore(t)
	ctx := withPairFabric()
	newLock := func() (*lock.DoorLockServer, error) {
		cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		cfg.Auth, cfg.AuthKey = st, "lock:front"
		return lock.New(cfg)
	}
	srv, err := newLock()
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.MatterInvoke(ctx, lockdef.CmdSetCredential, addPin(2, "4711", user1))
	if err != nil || res.(lockdef.SetCredentialResponse).Status != lockdef.StatusSuccess {
		t.Fatalf("SetCredential: %+v, %v", res, err)
	}
	// The key setting is lost, the records are kept.
	if err := st.DeleteSetting(context.Background(), store.SettingDoorLockCredentialKey); err != nil {
		t.Fatal(err)
	}
	if _, err := newLock(); !errors.Is(err, lock.ErrConfig) {
		t.Fatalf("New with saved credentials and no credential key: %v, want ErrConfig", err)
	}
	if _, ok, err := st.GetSetting(context.Background(), store.SettingDoorLockCredentialKey); err != nil || ok {
		t.Fatalf("credential key after the refused start: stored %v, %v; want none", ok, err)
	}
}
