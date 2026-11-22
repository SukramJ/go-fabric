// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/controller/ca"
	"thingfabric.dev/go-fabric/secure/setup"
	"thingfabric.dev/go-fabric/store"
)

// TestReferenceControllerSealKey holds R2-P6-sec-2604 and the sealed half
// of R2-P6-store-2603: --seal-key file: creates the key file (0600) on
// first use, seals the CA a clear database held, and the tool pairs, reads
// and creates a group with it; the group's epoch key is nowhere in the
// database file in clear; without the flag, or under another key, the
// database does not open. Not parallel.
func TestReferenceControllerSealKey(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	tl.run("nodes") // a clear database: the CA with its root key
	keyPath := filepath.Join(t.TempDir(), "seal.key")
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	tl.run("--seal-key", "file:"+keyPath, "pair", "--code", manual, "--node-id", "0x4321")
	if fi, err := os.Stat(keyPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", fi, err)
	}
	if out := tl.run("--seal-key", "file:"+keyPath, "read", "0x4321", "0/BasicInformation/VendorID"); !strings.Contains(out, "65521") {
		t.Fatalf("read:\n%s", out)
	}
	const epochHex = "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf"
	if out := tl.run("--seal-key", "file:"+keyPath, "group", "create", "300", "--key", epochHex); !strings.Contains(out, "group 300 created") {
		t.Fatalf("group create:\n%s", out)
	}

	db, err := store.OpenSQLite(tl.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.New(db).GetSetting(context.Background(), ca.DefaultSettingKey); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("the CA read without the sealer: %v, want store.ErrNoSealer", err)
	}
	_ = db.Close()
	epoch, _ := hex.DecodeString(epochHex)
	for _, f := range []string{tl.db, tl.db + "-wal"} {
		b, err := os.ReadFile(f)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, epoch) || bytes.Contains(b, []byte(epochHex)) {
			t.Errorf("%s holds the group's epoch key in clear", filepath.Base(f))
		}
	}

	if _, err := tl.runIn("", "nodes"); err == nil {
		t.Error("the sealed database opened without --seal-key")
	}
	other := filepath.Join(t.TempDir(), "other.key")
	if err := os.WriteFile(other, []byte(strings.Repeat("11", sealKeySize)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tl.runIn("", "--seal-key", "file:"+other, "nodes"); err == nil {
		t.Error("the sealed database opened under another key")
	}
	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{keyPath, "file:", "file:" + short} {
		if _, err := openSealKey(bad); err == nil {
			t.Errorf("--seal-key %q accepted", bad)
		}
	}
}
