// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"crypto"
	"io"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ca"
)

// readingSigner is a host signer (an HSM, say) that reads the CA while it
// signs, as one that logs the certificate it signs under would.
type readingSigner struct {
	*refSigner
	ca *ca.CA
}

func (s *readingSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if s.ca != nil {
		_ = s.ca.RootCert()
	}
	return s.refSigner.Sign(r, digest, opts)
}

// TestGenerateNOCSignsWithoutTheLock pins G1-16 (ADR 0026 P6): the NOC is
// signed — by a host's signer — and the counter saved with the CA's
// state lock released, so a signer that reads the CA does not deadlock.
func TestGenerateNOCSignsWithoutTheLock(t *testing.T) {
	t.Parallel()
	root := &readingSigner{refSigner: newRefSigner(t, "pkcs11:token=ctl;object=root")}
	c, err := ca.New(ca.Config{Store: ca.SettingStore{Settings: &mapSettings{}}, RootSigner: root, Clock: clocktest.New(issuedAt)})
	if err != nil {
		t.Fatal(err)
	}
	root.ca = c
	done := make(chan error, 1)
	go func() {
		_, err := c.GenerateNOC(newKey(t), 1, 2, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("GenerateNOC holds the CA's lock while the host's signer signs")
	}
}
