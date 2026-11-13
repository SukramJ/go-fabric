// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestOpcredsSignaturesNeedTheSessionChallenge pins the one source of the
// attestation challenge the three signing commands have: the session the
// invoke arrived on. matter.js signs with the session and nothing else —
// packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:113
// and :148 (`certification.sign(session, …)`), :491
// (`this.context.session.attestationChallengeKey`) and
// packages/protocol/src/certificate/DeviceCertification.ts:149 at db021da0;
// a context without a secure session throws (`NodeSession.assert`, :105,
// :126, :482). Without a stamped challenge AttestationRequest, CSRRequest
// and SignVIDVerificationRequest fail; with one they verify against it.
func TestOpcredsSignaturesNeedTheSessionChallenge(t *testing.T) {
	t.Parallel()
	bg := context.Background()
	challenge := bytes.Repeat([]byte{0xA7}, 16)
	stamped := core.WithInvokeAttestationChallenge(bg, challenge)

	verifyDAC := func(t *testing.T, dac *ecdsa.PrivateKey, elements, sig []byte) {
		t.Helper()
		if len(sig) != 64 {
			t.Fatalf("signature length = %d, want 64", len(sig))
		}
		h := sha256.New()
		h.Write(elements)
		h.Write(challenge)
		r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(&dac.PublicKey, h.Sum(nil), r, s) {
			t.Fatal("the signature does not verify over elements || the session's challenge")
		}
	}

	t.Run("AttestationRequest", func(t *testing.T) {
		t.Parallel()
		oc, dac := newOpcredsWithDAC(t)
		req := core.AttestationRequest{AttestationNonce: bytes.Repeat([]byte{0x42}, 32)}
		if resp, err := oc.MatterInvoke(bg, 0x00, req); err == nil {
			t.Fatalf("AttestationRequest without a session challenge answered %T, want FAILURE", resp)
		} else if got := vidStatusOf(t, err); got != im.StatusFailure {
			t.Fatalf("AttestationRequest without a session challenge: status %v, want FAILURE", got)
		}
		resp, err := oc.MatterInvoke(stamped, 0x00, req)
		if err != nil {
			t.Fatalf("AttestationRequest with the session challenge: %v", err)
		}
		r := resp.(core.AttestationResponse)
		verifyDAC(t, dac, r.AttestationElements, r.AttestationSignature)
	})

	t.Run("CSRRequest", func(t *testing.T) {
		t.Parallel()
		oc, dac := newOpcredsWithDAC(t)
		req := core.CSRRequest{CSRNonce: bytes.Repeat([]byte{0x42}, 32)}
		if resp, err := oc.MatterInvoke(bg, 0x04, req); err == nil {
			t.Fatalf("CSRRequest without a session challenge answered %T, want FAILURE", resp)
		} else if got := vidStatusOf(t, err); got != im.StatusFailure {
			t.Fatalf("CSRRequest without a session challenge: status %v, want FAILURE", got)
		}
		resp, err := oc.MatterInvoke(stamped, 0x04, req)
		if err != nil {
			t.Fatalf("CSRRequest with the session challenge: %v", err)
		}
		r := resp.(core.CSRResponse)
		verifyDAC(t, dac, r.NOCSRElements, r.AttestationSignature)
	})

	t.Run("SignVIDVerificationRequest", func(t *testing.T) {
		t.Parallel()
		oc, fs := opcredsWithFakeStore(t)
		_, _, fabricIndex := commissionTestFabric(stamped, t, oc)
		clientChallenge := bytes.Repeat([]byte{0xC1}, 32)
		req := core.SignVidVerificationRequest{FabricIndex: fabricIndex, ClientChallenge: clientChallenge}
		if resp, err := oc.MatterInvoke(im.WithFabricFilter(bg, true, fabricIndex), 0x0D, req); err == nil {
			t.Fatalf("SignVIDVerificationRequest without a session challenge answered %T, want FAILURE", resp)
		} else if got := vidStatusOf(t, err); got != im.StatusFailure {
			t.Fatalf("SignVIDVerificationRequest without a session challenge: status %v, want FAILURE", got)
		}
		resp, err := oc.MatterInvoke(im.WithFabricFilter(stamped, true, fabricIndex), 0x0D, req)
		if err != nil {
			t.Fatalf("SignVIDVerificationRequest with the session challenge: %v", err)
		}
		r := resp.(core.SignVidVerificationResponse)
		fabric, _ := fs.GetFabric(bg, fabricIndex)
		id, _ := fs.GetIdentity(bg, fabricIndex)
		priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), id.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		tbs := []byte{1}
		tbs = append(tbs, clientChallenge...)
		tbs = append(tbs, challenge...)
		tbs = append(tbs, fabricIndex, 1)
		tbs = append(tbs, fabric.RootPublicKey...)
		tbs = binary.BigEndian.AppendUint64(tbs, fabric.FabricID)
		tbs = binary.BigEndian.AppendUint16(tbs, fabric.VendorID)
		digest := sha256.Sum256(tbs)
		rr, ss := new(big.Int).SetBytes(r.Signature[:32]), new(big.Int).SetBytes(r.Signature[32:])
		if !ecdsa.Verify(&priv.PublicKey, digest[:], rr, ss) {
			t.Fatal("the signature does not verify over dataToSign with the session's challenge")
		}
	})
}
