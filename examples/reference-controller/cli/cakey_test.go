// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
)

// caSetting is the CA's record in the tool's database.
func caSetting(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	v, _, err := store.New(db).GetSetting(context.Background(), ca.DefaultSettingKey)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestReferenceControllerCAKey: --ca-key file: on a database whose CA
// holds its root key moves the key into a new key file and out of the
// database; the tool pairs and reaches the node with it; without it the
// CA does not open; a wrong passphrase is refused. Not parallel.
func TestReferenceControllerCAKey(t *testing.T) {
	h := startHarness(t, nil)
	tl := newTool(t, h)
	tl.run("nodes") // creates the CA with its key in the database
	if !strings.Contains(caSetting(t, tl.db), "privateKey") {
		t.Fatal("the default path does not keep the root key in the database")
	}
	keyPath := filepath.Join(t.TempDir(), "ca.key")
	t.Setenv(passphraseEnv, "correct horse")
	manual, err := setup.ManualCode(harnessDiscriminator, harnessPasscode)
	if err != nil {
		t.Fatal(err)
	}
	tl.run("--ca-key", "file:"+keyPath, "pair", "--code", manual, "--node-id", "0x4321")
	js := caSetting(t, tl.db)
	if strings.Contains(js, "privateKey") || !strings.Contains(js, `"rootKeyRef":"file:`+keyPath+`"`) {
		t.Fatalf("CA record %s", js)
	}
	if out := tl.run("--ca-key", "file:"+keyPath, "read", "0x4321", "0/BasicInformation/VendorID"); !strings.Contains(out, "65521") {
		t.Fatalf("read:\n%s", out)
	}
	if _, err := tl.runIn("", "nodes"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("without --ca-key: %v", err)
	}
	t.Setenv(passphraseEnv, "wrong")
	if _, err := tl.runIn("", "--ca-key", "file:"+keyPath, "nodes"); err == nil {
		t.Fatal("a wrong passphrase opened the key file")
	}
}
