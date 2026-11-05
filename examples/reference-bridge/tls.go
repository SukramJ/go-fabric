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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// --tls-sim (TESTING ONLY): TlsCertificateManagement and TlsClientManagement
// on the root (node.Config.TLS) over a software key store, for the TLSCERT
// and TLSCLIENT certification families and for the camera's
// PushAvStreamTransport, which reaches its TLS endpoints through the node.
// connectedhomeip serves both clusters on the root of its all-clusters and
// camera apps (camera-app.matter's endpoint 0 at the harness pin 6170af84);
// its certificate table makes a P-256 key pair per client certificate
// (CertificateTableImpl.cpp PrepareClientCertificate: NewCertificateSigningRequest,
// ECDSA_sign_msg of the nonce) and matches a provisioned certificate's
// public key against it (UpdateClientCertificateEntry: ExtractPubkeyFromX509Cert).
// tlsKeys does the same with crypto/ecdsa. The keys live in the daemon's
// settings (tlsKeysSetting), so a daemon restarted on the same --db still
// holds the key of every certificate its tables (the node's store) list.

// tlsKeysSetting is the settings key the client keys are kept under.
const tlsKeysSetting = "tlssim.keys"

// tlsKeys is the CertificatePort of --tls-sim.
type tlsKeys struct {
	mu     sync.Mutex
	keys   map[string]*ecdsa.PrivateKey
	st     settings
	logger *slog.Logger
	now    func() time.Time
}

// newTLSKeys returns the key store; attach binds it to the node's settings
// once the node exists.
func newTLSKeys(logger *slog.Logger) *tlsKeys {
	return &tlsKeys{keys: map[string]*ecdsa.PrivateKey{}, logger: logger, now: time.Now}
}

// tlsConfig is the node's TLS configuration over the key store.
func (k *tlsKeys) tlsConfig() *node.TLSConfig {
	return &node.TLSConfig{CertificatePort: k}
}

// attach loads the persisted keys from st and persists every later change.
func (k *tlsKeys) attach(ctx context.Context, st settings) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.st = st
	v, ok, err := st.GetSetting(ctx, tlsKeysSetting)
	if err != nil || !ok {
		return
	}
	var stored map[string]string
	if err := json.Unmarshal([]byte(v), &stored); err != nil {
		k.logger.Warn("tlssim.load", slog.Any("err", err))
		return
	}
	for id, b64 := range stored {
		der, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		if key, err := x509.ParseECPrivateKey(der); err == nil {
			k.keys[id] = key
		}
	}
}

// persistLocked writes the key set; k.mu is held.
func (k *tlsKeys) persistLocked(ctx context.Context) {
	if k.st == nil {
		return
	}
	stored := make(map[string]string, len(k.keys))
	for id, key := range k.keys {
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			continue
		}
		stored[id] = base64.StdEncoding.EncodeToString(der)
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return
	}
	if err := k.st.SetSetting(context.WithoutCancel(ctx), tlsKeysSetting, string(data)); err != nil {
		k.logger.Warn("tlssim.persist", slog.Any("err", err))
	}
}

func tlsKeyID(fabricIndex uint8, ccdid uint16) string {
	return fmt.Sprintf("%d/%d", fabricIndex, ccdid)
}

// GenerateCSR makes (or reuses) ccdid's P-256 key and returns its DER
// PKCS#10 request, signed ecdsa-with-SHA256, and the raw r‖s signature of
// the nonce, as chip's P256Keypair::ECDSA_sign_msg returns it.
func (k *tlsKeys) GenerateCSR(ctx context.Context, fabricIndex uint8, ccdid uint16, newKey bool, nonce []byte) (csr, nonceSignature []byte, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	id := tlsKeyID(fabricIndex, ccdid)
	key := k.keys[id]
	if key == nil || newKey {
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return nil, nil, err
		}
		k.keys[id] = key
		k.persistLocked(ctx)
	}
	csr, err = x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{SignatureAlgorithm: x509.ECDSAWithSHA256}, key)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(nonce)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return nil, nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return csr, sig, nil
}

// CertificateValid reports a DER X.509 certificate valid at the current
// time (chip's Crypto::IsCertificateValidAtCurrentTime).
func (k *tlsKeys) CertificateValid(_ context.Context, certificate []byte) bool {
	cert, err := x509.ParseCertificate(certificate)
	if err != nil {
		return false
	}
	now := k.now()
	return !now.Before(cert.NotBefore) && !now.After(cert.NotAfter)
}

// ClientKeyMatches reports that the certificate carries ccdid's public key.
func (k *tlsKeys) ClientKeyMatches(_ context.Context, fabricIndex uint8, ccdid uint16, certificate []byte) bool {
	cert, err := x509.ParseCertificate(certificate)
	if err != nil {
		return false
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return false
	}
	k.mu.Lock()
	key := k.keys[tlsKeyID(fabricIndex, ccdid)]
	k.mu.Unlock()
	return key != nil && key.PublicKey.Equal(pub)
}

// RemoveClientKey drops ccdid's key.
func (k *tlsKeys) RemoveClientKey(ctx context.Context, fabricIndex uint8, ccdid uint16) {
	k.mu.Lock()
	defer k.mu.Unlock()
	id := tlsKeyID(fabricIndex, ccdid)
	if _, ok := k.keys[id]; ok {
		delete(k.keys, id)
		k.persistLocked(ctx)
	}
}
