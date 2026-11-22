// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// unsealedSettings is a store whose stored Wi-Fi networks do not open
// (store.ErrUnseal) and that counts the writes of that setting.
type unsealedSettings struct {
	*memstore.Store
	writes atomic.Int32
}

func (u *unsealedSettings) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if key == wifiNetworksKey {
		return "", false, store.ErrUnseal
	}
	return u.Store.GetSetting(ctx, key)
}

func (u *unsealedSettings) SetSetting(ctx context.Context, key, value string) error {
	if key == wifiNetworksKey {
		u.writes.Add(1)
	}
	return u.Store.SetSetting(ctx, key, value)
}

// TestUnreadableStoredNetworksAreNeverOverwritten (review II, seal F5): a
// stored network list the store cannot read — a sealed value that does not
// open — is not taken as "no networks": a commissioning's StoreNetworks
// fails with the store's error (CommissioningComplete then answers
// FAILURE, nothing committed), and nothing writes the setting. Before, the
// unreadable list read as empty and the next commit overwrote it.
func TestUnreadableStoredNetworksAreNeverOverwritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &unsealedSettings{Store: memstore.New()}
	w := wifiOn(t, st, &simDriver{})
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.StoreNetworks(ctx); !errors.Is(err, store.ErrUnseal) {
		t.Fatalf("StoreNetworks over an unreadable stored list = %v, want store.ErrUnseal", err)
	}
	w.OnCommissioningComplete(ctx)
	w.OnFailSafeExpired()
	if n := st.writes.Load(); n != 0 {
		t.Errorf("the unreadable stored networks were written %d times", n)
	}
}
