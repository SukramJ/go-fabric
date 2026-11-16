// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
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

// TestSealerSealsTheControllersStore: with Config.Sealer the controller's
// store is sealed — a CASE resumption record saved through the store the
// peers resume with is sealed in the file (a store without the sealer reads
// it as store.ErrNoSealer) and reads back through the controller's store,
// also after a restart under the same key; New under another key fails
// with an error that wraps store.ErrUnseal.
func TestSealerSealsTheControllersStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sealer, err := store.NewAESGCMSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(ctx, sealerConfig(db, sealer))
	if err != nil {
		t.Fatal(err)
	}
	peer := handshake.PeerAddress{FabricIndex: c.fabricIndex(), NodeID: 0xABCD}
	rec := handshake.ResumptionRecord{Peer: peer, ResumptionID: bytes.Repeat([]byte{1}, 16), SharedSecret: bytes.Repeat([]byte{2}, 32)}
	if err := handshake.NewStoreResumption(c.st).SaveResumptionRecord(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(db).GetResumptionByPeer(ctx, peer.FabricIndex, peer.NodeID); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a store without the sealer reads the record: %v, want store.ErrNoSealer", err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT shared_secret FROM matter_resumption`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, rec.SharedSecret) {
		t.Errorf("the shared secret is in the file in clear: %x", raw)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}

	again, err := New(ctx, sealerConfig(db, sealer))
	if err != nil {
		t.Fatalf("New under the same key: %v", err)
	}
	got, ok := handshake.NewStoreResumption(again.st).FindResumptionRecord(peer)
	if !ok || !bytes.Equal(got.SharedSecret, rec.SharedSecret) || !bytes.Equal(got.ResumptionID, rec.ResumptionID) {
		t.Errorf("the record after a restart: %+v, %v; want it back", got, ok)
	}
	if err := again.Close(ctx); err != nil {
		t.Fatal(err)
	}

	other, err := store.NewAESGCMSealer(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if wrong, err := New(ctx, sealerConfig(db, other)); !errors.Is(err, store.ErrUnseal) {
		if wrong != nil {
			_ = wrong.Close(ctx)
		}
		t.Errorf("New under another key: %v, want an error wrapping store.ErrUnseal", err)
	}
}
