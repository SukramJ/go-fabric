// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "reference-bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

// TestCameraStateSurvivesARestart: the chime's selection and the stream
// priorities the camera keeps in the settings come back on a new camera
// on the same database (camera.go cameraStateSetting).
func TestCameraStateSurvivesARestart(t *testing.T) {
	t.Parallel()
	st := openTestStore(t)
	first := newDemoCamera(surface.Policies{}, cameraOptions{}, slog.Default())
	first.attach(context.Background(), st)
	if _, err := first.devices(); err != nil {
		t.Fatal(err)
	}
	two := uint8(2)
	if err := first.chime.Chime().Set(chimedef.Patch{SelectedChime: &two}); err != nil {
		t.Fatal(err)
	}
	first.persist()

	second := newDemoCamera(surface.Policies{}, cameraOptions{}, slog.Default())
	second.attach(context.Background(), st)
	if _, err := second.devices(); err != nil {
		t.Fatal(err)
	}
	if got := second.chime.Chime().Get().SelectedChime; got != 2 {
		t.Fatalf("SelectedChime after a restart = %d, want 2", got)
	}
}

// TestCameraVideoStreamCompatibility: the camera-app's available streams
// decide an allocation (camera-device-interface.h:51-62).
func TestCameraVideoStreamCompatibility(t *testing.T) {
	t.Parallel()
	c := newDemoCamera(surface.Policies{}, cameraOptions{}, slog.Default())
	if _, err := c.devices(); err != nil {
		t.Fatal(err)
	}
	port := camVideoPort{c}
	ok := camVideo(0, 30, 30, 640, 480, 1280, 720)
	id, err := port.VideoStreamAllocate(context.Background(), ok)
	if err != nil || id != 1 {
		t.Fatalf("a 720p allocation = %d, %v; want the camera-app's stream 1", id, err)
	}
	bad := ok
	bad.VideoCodec = csdef.VideoCodecAv1
	if _, err := port.VideoStreamAllocate(context.Background(), bad); err == nil {
		t.Fatal("an AV1 stream was allocated; the camera-app has none")
	}
	if _, err := (camCapturePort{c}).CaptureSnapshot(context.Background(), nil, csdef.VideoResolutionStruct{Width: 640, Height: 480}); err == nil {
		t.Fatal("a snapshot without --camera-test-videosrc succeeded")
	}
	c.opts.testVideoSource = true
	if snap, err := (camCapturePort{c}).CaptureSnapshot(context.Background(), nil, csdef.VideoResolutionStruct{Width: 640, Height: 480}); err != nil || len(snap.Data) == 0 {
		t.Fatalf("a snapshot with --camera-test-videosrc = %v", err)
	}
}

// TestTLSKeysMatchTheirCertificate: a client key made at ClientCSR signs
// the nonce, matches a certificate over its public key after a restart,
// and no other.
func TestTLSKeysMatchTheirCertificate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)
	keys := newTLSKeys(slog.Default())
	keys.attach(ctx, st)
	nonce := make([]byte, 32)
	csrDER, sig, err := keys.GenerateCSR(ctx, 1, 7, true, nonce)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil || csr.CheckSignature() != nil {
		t.Fatalf("the CSR does not verify: %v", err)
	}
	pub := csr.PublicKey.(*ecdsa.PublicKey)
	digest := sha256.Sum256(nonce)
	if len(sig) != 64 || !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("the nonce signature does not verify")
	}
	ca, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "client"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	cert, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, ca)
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTLSKeys(slog.Default())
	restarted.attach(ctx, st)
	if !restarted.ClientKeyMatches(ctx, 1, 7, cert) || !restarted.CertificateValid(ctx, cert) {
		t.Fatal("the kept key does not match its certificate after a restart")
	}
	if restarted.ClientKeyMatches(ctx, 1, 8, cert) {
		t.Fatal("another CCDID's key matches")
	}
	restarted.RemoveClientKey(ctx, 1, 7)
	if restarted.ClientKeyMatches(ctx, 1, 7, cert) {
		t.Fatal("a removed key still matches")
	}
}
