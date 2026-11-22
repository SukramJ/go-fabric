// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/controller/client/sqlstore"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/store"
)

// quietConn is a DNS-SD socket on which nothing answers.
type quietConn struct{ out chan mdns.QueryResponse }

func (c quietConn) Send([]byte) error                    { return nil }
func (c quietConn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c quietConn) Close() error                         { return nil }

func sealerConfig(db *sql.DB, s store.Sealer) Config {
	return Config{
		DB: db, Attestation: attestation.TestStore(), Listen: "127.0.0.1:0", Label: "test",
		Logger: slog.New(slog.DiscardHandler), Sealer: s,
		Names: func(log *slog.Logger) (*discovery.Names, error) {
			return discovery.NewNames(quietConn{out: make(chan mdns.QueryResponse)}, log), nil
		},
	}
}

// openSealerDB opens a controller database file in WAL mode.
func openSealerDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func aesSealer(t *testing.T, b byte) store.Sealer {
	t.Helper()
	s, err := store.NewAESGCMSealer(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// noClearSecret fails when the database file or its -wal holds one of the
// clear secrets: the CA's and the fabric's private keys (their JSON field
// "privateKey"), or the given values.
func noClearSecret(t *testing.T, path, what string, plain ...[]byte) {
	t.Helper()
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(`"privateKey"`)) {
			t.Errorf("%s: %s holds a private key in clear", what, filepath.Base(f))
		}
		for _, p := range plain {
			if bytes.Contains(b, p) {
				t.Errorf("%s: %s holds %q in clear", what, filepath.Base(f), p)
			}
		}
	}
}

const (
	onboardingCodeCluster = 0xFFFF_FFFC // controller/client onboarding.go
	keptCode              = "MT:Y.K90-Q000KA0648G00"
)

// TestSealerSealsTheControllersStore: with Config.Sealer the controller's
// secrets are sealed in the file — the CA with its root key and the
// controller's fabric with its operational key (controller.ca,
// controller.ca.fabric), a CASE resumption record saved through the store
// the peers resume with, and a kept onboarding code saved through the
// registry's store; each reads back through the controller, also after a
// restart under the same key, and a store without the sealer reads it as
// store.ErrNoSealer. New under another key fails with an error that wraps
// store.ErrUnseal, and New without a sealer with one that wraps
// store.ErrNoSealer.
func TestSealerSealsTheControllersStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	db := openSealerDB(t, path)
	sealer := aesSealer(t, 7)
	c, err := New(ctx, sealerConfig(db, sealer))
	if err != nil {
		t.Fatal(err)
	}
	fabricID, rootNode := c.Fabric().FabricID, c.Fabric().RootNodeID
	peer := handshake.PeerAddress{FabricIndex: c.fabricIndex(), NodeID: 0xABCD}
	rec := handshake.ResumptionRecord{Peer: peer, ResumptionID: bytes.Repeat([]byte{1}, 16), SharedSecret: bytes.Repeat([]byte{2}, 32)}
	if err := handshake.NewStoreResumption(c.st).SaveResumptionRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := c.nodeStore.Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	plain := store.New(db)
	if _, err := plain.GetResumptionByPeer(ctx, peer.FabricIndex, peer.NodeID); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a store without the sealer reads the resumption record: %v, want store.ErrNoSealer", err)
	}
	for _, key := range []string{"controller.ca", "controller.ca.fabric"} {
		if _, _, err := plain.GetSetting(ctx, key); !errors.Is(err, store.ErrNoSealer) {
			t.Errorf("a store without the sealer reads %s: %v, want store.ErrNoSealer", key, err)
		}
	}
	if _, err := sqlstore.New(db).Load("n"); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a registry without the sealer reads the kept code: %v, want store.ErrNoSealer", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	noClearSecret(t, path, "sealed controller", rec.SharedSecret, []byte(keptCode))

	again, err := New(ctx, sealerConfig(db, sealer))
	if err != nil {
		t.Fatalf("New under the same key: %v", err)
	}
	if f := again.Fabric(); f.FabricID != fabricID || f.RootNodeID != rootNode {
		t.Errorf("the fabric after a restart: %x/%x, want %x/%x", f.FabricID, f.RootNodeID, fabricID, rootNode)
	}
	got, ok := handshake.NewStoreResumption(again.st).FindResumptionRecord(peer)
	if !ok || !bytes.Equal(got.SharedSecret, rec.SharedSecret) {
		t.Errorf("the resumption record after a restart: %+v, %v", got, ok)
	}
	eps, err := again.nodeStore.Load("n")
	if err != nil || len(eps) != 1 || string(eps[0].Clusters[0].Values[0]) != keptCode {
		t.Errorf("the kept code after a restart: %+v, %v", eps, err)
	}
	if err := again.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if wrong, err := New(ctx, sealerConfig(db, aesSealer(t, 8))); !errors.Is(err, store.ErrUnseal) {
		if wrong != nil {
			_ = wrong.Close(ctx)
		}
		t.Errorf("New under another key: %v, want an error wrapping store.ErrUnseal", err)
	}
	if none, err := New(ctx, sealerConfig(db, nil)); !errors.Is(err, store.ErrNoSealer) {
		if none != nil {
			_ = none.Close(ctx)
		}
		t.Errorf("New without the sealer: %v, want an error wrapping store.ErrNoSealer", err)
	}
}

// TestSealerSealsAClearControllerDatabase: a controller database written
// without a sealer — its CA, its fabric and a kept onboarding code in
// clear — opens under Config.Sealer as a node's clear store does under
// node.Store.Sealer (store.NewSealed): every clear secret is sealed on the
// way in and the file scrubbed, so the clear values are gone from it and
// its -wal, and the controller keeps its fabric.
func TestSealerSealsAClearControllerDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	db := openSealerDB(t, path)
	c, err := New(ctx, sealerConfig(db, nil))
	if err != nil {
		t.Fatal(err)
	}
	fabricID := c.Fabric().FabricID
	if err := c.nodeStore.Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := store.New(db).GetSetting(ctx, "controller.ca"); err != nil || !ok || !bytes.Contains([]byte(v), []byte(`"privateKey"`)) {
		t.Fatalf("the clear database's CA: ok %v, %v; want the CA with its key in clear", ok, err)
	}

	sealed, err := New(ctx, sealerConfig(db, aesSealer(t, 7)))
	if err != nil {
		t.Fatalf("New under a sealer on a clear database: %v", err)
	}
	if f := sealed.Fabric(); f.FabricID != fabricID {
		t.Errorf("the fabric under the sealer: %x, want %x", f.FabricID, fabricID)
	}
	if err := sealed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	noClearSecret(t, path, "a clear database sealed on the way in", []byte(keptCode))
	if _, _, err := store.New(db).GetSetting(ctx, "controller.ca"); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("the CA after the sealing: %v, want store.ErrNoSealer without the sealer", err)
	}
}
