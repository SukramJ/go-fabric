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
	var verr error
	if err := (passcodeValue{&v, &verr}).Set("@" + file); err != nil || verr != nil || v != 34567812 {
		t.Fatalf("@file: %d %v %v", v, err, verr)
	}
	newSet := func() (*flag.FlagSet, *uint) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		p := new(uint)
		*p = 20202021
		fs.Var(passcodeValue{p, new(error)}, "passcode", "")
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

// TestMalformedPasscodeIsNotEchoed holds R2-C2-sec-2601: a --passcode
// that is not a decimal number is refused after the parse with a message
// that does not carry it — package flag, which prints a failing Set's
// value, sees no failure — and $GOFABRIC_PASSCODE is refused the same way.
// Not parallel: it sets the environment.
func TestMalformedPasscodeIsNotEchoed(t *testing.T) {
	const secret = "2468135x"
	var out bytes.Buffer
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(&out)
	f := flags{passcode: 20202021, discriminator: 3840, iterations: node.PBKDFIterationsMin}
	fs.Var(passcodeValue{&f.passcode, &f.passcodeErr}, "passcode", "")
	if err := fs.Parse([]string{"--passcode", secret}); err != nil {
		t.Fatalf("the parse failed: %v", err)
	}
	_, _, err := checkFlags(f)
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(out.String(), secret) || !strings.Contains(err.Error(), "--passcode") {
		t.Fatalf("checkFlags = %v, output %q: want a refusal naming the flag, not the value", err, out.String())
	}
	out.Reset()
	t.Setenv(passcodeEnv, secret)
	fs = flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(&out)
	fs.Var(passcodeValue{new(uint), new(error)}, "passcode", "")
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := passcodeFromEnv(fs); err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), passcodeEnv) {
		t.Fatalf("from $%s: %v", passcodeEnv, err)
	}
}
