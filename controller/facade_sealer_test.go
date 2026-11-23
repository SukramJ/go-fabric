// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/store"
)

// The controller facade's sealing errors are the store's values (decision
// 551, #584), for errors.Is.
func TestFacadeSealErrorsAreTheStores(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name         string
		facade, base error
	}{
		{"Unseal", controller.ErrUnseal, store.ErrUnseal},
		{"NoSealer", controller.ErrNoSealer, store.ErrNoSealer},
	} {
		if c.facade == nil || !errors.Is(c.facade, c.base) || !errors.Is(c.base, c.facade) {
			t.Errorf("%s: controller's %v is not the store's %v", c.name, c.facade, c.base)
		}
	}
}

// controller.NewAESGCMSealer is the store's sealer (decisions 551, 561,
// #584): what it seals the store's own sealer opens, and a key of the
// wrong size is refused.
func TestFacadeAESGCMSealerIsTheStores(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 32)
	s, err := controller.NewAESGCMSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	var _ store.Sealer = s
	base, err := store.NewAESGCMSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := base.Open(sealed, []byte("aad")); err != nil || string(plain) != "secret" {
		t.Fatalf("the store's sealer opens the facade's value: %q, %v", plain, err)
	}
	if _, err := controller.NewAESGCMSealer(key[:16]); err == nil {
		t.Fatal("a 16-byte key was accepted")
	}
}

// TestFacadeSealedControllerDatabase is #584's acceptance on the
// controller's side, through the controller facade alone (no store
// import in the steps): a database sealed with controller.NewAESGCMSealer
// reopens under the same key, refuses another key with
// controller.ErrUnseal and a missing sealer with controller.ErrNoSealer,
// and controller.Reseal rotates the key — after it the new key opens and
// the old one is controller.ErrUnseal.
func TestFacadeSealedControllerDatabase(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	first := site.ControllerConfig(fabrictest.ControllerOptions{})
	oldKey, err := controller.NewAESGCMSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := controller.NewAESGCMSealer(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	// open runs New and Close on the database under the key k names
	// (0 none, 1 old, 2 new).
	open := func(k int) error {
		t.Helper()
		cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
		cfg.DB = first.DB
		switch k {
		case 1:
			cfg.Sealer = oldKey
		case 2:
			cfg.Sealer = newKey
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return site.Run(ctx, func(ctx context.Context) error {
			c, err := controller.New(ctx, cfg)
			if err != nil {
				return err
			}
			return c.Close(ctx)
		})
	}
	const none, old, next = 0, 1, 2
	if err := open(old); err != nil {
		t.Fatalf("New sealing a fresh database: %v", err)
	}
	if err := open(old); err != nil {
		t.Fatalf("New again under the same key: %v", err)
	}
	if err := open(next); !errors.Is(err, controller.ErrUnseal) {
		t.Fatalf("New under another key: %v, want controller.ErrUnseal", err)
	}
	if err := open(none); !errors.Is(err, controller.ErrNoSealer) {
		t.Fatalf("New without a sealer: %v, want controller.ErrNoSealer", err)
	}
	if err := controller.Reseal(t.Context(), first.DB, oldKey, newKey); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if err := open(next); err != nil {
		t.Fatalf("New under the new key after Reseal: %v", err)
	}
	if err := open(old); !errors.Is(err, controller.ErrUnseal) {
		t.Fatalf("New under the old key after Reseal: %v, want controller.ErrUnseal", err)
	}
}
