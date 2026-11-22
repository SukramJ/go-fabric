// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/node"
)

// TestSecretsComeFromAFileOrTheEnvironment holds R2-P6-sec-2615: the
// passcode takes @file and $GOFABRIC_PASSCODE when the flag is not given
// (the flag wins), the enable key takes @file, and the default salt is
// random per start, never a shared constant. Not parallel: it sets the
// environment.
func TestSecretsComeFromAFileOrTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "passcode")
	if err := os.WriteFile(file, []byte("34567812\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var v uint
	if err := (passcodeValue{&v}).Set("@" + file); err != nil || v != 34567812 {
		t.Fatalf("@file: %d %v", v, err)
	}
	if err := (passcodeValue{&v}).Set("not a number"); err == nil || strings.Contains(err.Error(), "not a number") {
		t.Fatalf("a malformed passcode: %v (the value must not be echoed)", err)
	}
	newSet := func() (*flag.FlagSet, *uint) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		p := new(uint)
		*p = 20202021
		fs.Var(passcodeValue{p}, "passcode", "")
		return fs, p
	}
	t.Setenv(passcodeEnv, "45678123")
	fs, p := newSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := passcodeFromEnv(fs); err != nil || *p != 45678123 {
		t.Fatalf("from $%s: %d %v", passcodeEnv, *p, err)
	}
	fs, p = newSet()
	if err := fs.Parse([]string{"--passcode", "56781234"}); err != nil {
		t.Fatal(err)
	}
	if err := passcodeFromEnv(fs); err != nil || *p != 56781234 {
		t.Fatalf("the flag over $%s: %d %v", passcodeEnv, *p, err)
	}
	keyFile := filepath.Join(dir, "enable-key")
	if err := os.WriteFile(keyFile, []byte("00112233445566778899aabbccddeeff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if key, err := enableKeyBytes("@" + keyFile); err != nil || len(key) != 16 {
		t.Fatalf("--enable-key @file: %x %v", key, err)
	}
	a, b := pbkdfSalt(""), pbkdfSalt("")
	if len(a) != node.PBKDFSaltMax || bytes.Equal(a, b) {
		t.Fatalf("default salts %x and %x: want two random %d-byte salts", a, b, node.PBKDFSaltMax)
	}
	if got := pbkdfSalt("go-fabric-ref-salt"); string(got) != "go-fabric-ref-salt" {
		t.Fatalf("--pbkdf-salt: %q", got)
	}
}
