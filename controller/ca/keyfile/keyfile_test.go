// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package keyfile

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

func TestCreateOpenSign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.key")
	s, err := create(path, []byte("secret"), nil, minIterations)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
	if _, err := create(path, []byte("secret"), nil, minIterations); err == nil {
		t.Fatal("an existing file was overwritten")
	}
	o, err := Open(path, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Public().(*ecdsa.PublicKey).Equal(s.Public()) {
		t.Fatal("opened key differs")
	}
	if ref := ca.KeyRefOf(o); ref != "file:"+path {
		t.Fatalf("KeyRef %q", ref)
	}
	digest := sha256.Sum256([]byte("tbs"))
	sig, err := o.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil || !ecdsa.VerifyASN1(s.Public().(*ecdsa.PublicKey), digest[:], sig) {
		t.Fatalf("signature does not verify: %v", err)
	}
	if _, err := ca.P256PublicKey(o); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "privateKey") {
		t.Fatal("the file names a private key in clear")
	}
}

// TestCreateDefaultIterations: Create writes the file at Iterations, and
// it opens.
func TestCreateDefaultIterations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op.key")
	if _, err := Create(path, []byte("secret"), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil || f.Iterations != Iterations || len(f.Salt) != 32 {
		t.Fatalf("header %+v %v", f, err)
	}
	if _, err := Open(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.key")
	if _, err := create(path, []byte("secret"), nil, minIterations); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, []byte("wrong")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	edit := func(name string, change func(*file)) string {
		g := f
		change(&g)
		out, err := json.Marshal(&g)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, out, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A header edit is caught by the GCM additional data.
	other, err := create(filepath.Join(dir, "other.key"), []byte("x"), nil, minIterations)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, err := ca.P256PublicKey(other)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*file){
		"pub.key":    func(g *file) { g.PublicKey = otherPub },
		"iter.key":   func(g *file) { g.Iterations = minIterations + 1 },
		"weak.key":   func(g *file) { g.Iterations = 1000 },
		"format.key": func(g *file) { g.Format = 2 },
		"ct.key":     func(g *file) { g.Ciphertext = append([]byte{g.Ciphertext[0] ^ 1}, g.Ciphertext[1:]...) },
	} {
		if _, err := Open(edit(name, change), []byte("secret")); err == nil {
			t.Errorf("%s opened", name)
		}
	}
	if _, err := create(filepath.Join(dir, "empty.key"), nil, nil, minIterations); err == nil {
		t.Fatal("empty passphrase accepted")
	}
}
