// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/store"
)

// TestNewRefusesCredentialsTheLoadedKeyDoesNotOpen (R3-store-3254,
// decision 569): when the AuthStore holds credentials that do not decrypt
// under the credential key it holds — the key and the records live in two
// areas that can drift — New refuses with ErrConfig instead of starting a
// server that would refuse every such PIN later.
func TestNewRefusesCredentialsTheLoadedKeyDoesNotOpen(t *testing.T) {
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
	// The same key reopens the records.
	if _, err := newLock(); err != nil {
		t.Fatalf("restart on the key the credentials were stored under: %v", err)
	}
	// Another valid key in place of the one the credentials were stored under.
	other := make([]byte, 16)
	for i := range other {
		other[i] = byte(i + 1)
	}
	if err := st.SetSetting(context.Background(), store.SettingDoorLockCredentialKey, hex.EncodeToString(other)); err != nil {
		t.Fatal(err)
	}
	if _, err := newLock(); !errors.Is(err, lock.ErrConfig) {
		t.Fatalf("New on a key the stored credentials do not decrypt under: %v, want ErrConfig", err)
	}
}
