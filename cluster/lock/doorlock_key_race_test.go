// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/lock"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	"thingfabric.dev/go-fabric/store"
)

// keyGatedStore holds every answer to a read of the credential key until
// release closes, so that two servers starting together both have read it
// before either stores one.
type keyGatedStore struct {
	lock.AuthStore
	release chan struct{}
}

func (g keyGatedStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	value, ok, err = g.AuthStore.GetSetting(ctx, key)
	if key == store.SettingDoorLockCredentialKey {
		<-g.release
	}
	return value, ok, err
}

// TestConcurrentLocksOnOneStoreShareOneCredentialKey (R3-store-3254): two
// DoorLock servers that start together on one AuthStore before any
// credential key exists end on one key, so the credentials each stores
// still match after a restart. Before the fix each read no key, each
// generated one, the later store won, and the other server's credentials
// no longer decrypted after the restart: its PIN was refused.
func TestConcurrentLocksOnOneStoreShareOneCredentialKey(t *testing.T) {
	t.Parallel()
	st, _ := openStore(t)
	ctx := withPairFabric()
	newLock := func(as lock.AuthStore, key string) (*lock.DoorLockServer, error) {
		cfg := scheduledLockState(scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		cfg.Auth, cfg.AuthKey = as, key
		return lock.New(cfg)
	}
	keys := []string{"lock:front", "lock:back"}
	servers := make([]*lock.DoorLockServer, len(keys))
	synctest.Test(t, func(t *testing.T) {
		gated := keyGatedStore{AuthStore: st, release: make(chan struct{})}
		errs := make(chan error, len(keys))
		for i, key := range keys {
			go func() {
				var err error
				servers[i], err = newLock(gated, key)
				errs <- err
			}()
		}
		synctest.Wait() // both read the key, or the second waits behind the first
		close(gated.release)
		for range keys {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
	})
	for i, srv := range servers {
		res, err := srv.MatterInvoke(ctx, lockdef.CmdSetCredential, addPin(2, "4711", user1))
		if err != nil || res.(lockdef.SetCredentialResponse).Status != lockdef.StatusSuccess {
			t.Fatalf("%s SetCredential: %+v, %v", keys[i], res, err)
		}
	}
	for _, key := range keys {
		restarted, err := newLock(st, key)
		if err != nil {
			t.Fatal(err)
		}
		p := pin("4711")
		if _, err := restarted.MatterInvoke(ctx, lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{PinCode: &p}); err != nil {
			t.Errorf("%s: unlock with the PIN it stored, after a restart: %v", key, err)
		}
	}
}
