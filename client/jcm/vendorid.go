// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"

	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// FabricBindingVersion1 is the only fabric binding version
// (CHIPCryptoPAL.cpp:936-943, kVersion1).
const FabricBindingVersion1 uint8 = 1

// ClientChallengeSize is the SignVIDVerificationRequest client challenge
// (VendorIdVerificationClient.cpp:135, kVendorIdVerificationClientChallengeSize).
const ClientChallengeSize = 32

// attestationChallengeSize is a session's AttestationChallenge
// (CHIPCryptoPAL.cpp:964, CHIP_CRYPTO_SYMMETRIC_KEY_LENGTH_BYTES).
const attestationChallengeSize = 16

// Errors of the Vendor ID Verification procedure.
var (
	// ErrNoSession is a signer without the session to sign on — chip's
	// CHIP_ERROR_INCORRECT_STATE (VendorIdVerificationClient.cpp:152-156,
	// :169-174).
	ErrNoSession = errors.New("jcm: no session to the peer")
	// ErrNoTrustAnchor is a lookup that found no operational trust anchor
	// (CHIP_ERROR_CERT_NOT_FOUND of the delegate).
	ErrNoTrustAnchor = errors.New("jcm: no operational trust anchor")
	// ErrSignature is a signature that does not verify — chip's
	// CHIP_ERROR_INVALID_SIGNATURE (:95-96).
	ErrSignature = errors.New("jcm: VID verification signature does not verify")
	// ErrBindingInput is a fabric binding input outside its form: a root
	// public key, challenge or binding version chip refuses
	// (CHIP_ERROR_INVALID_ARGUMENT, CHIPCryptoPAL.cpp:936-943, :964-971).
	ErrBindingInput = errors.New("jcm: VID verification input out of form")
)

// Signer sends SignVIDVerificationRequest to the peer and returns its
// response with the AttestationChallenge of the session it was sent on —
// chip's InvokeCommandRequest and the session getter of VerifyVendorId
// (VendorIdVerificationClient.cpp:127-187).
type Signer interface {
	SignVIDVerification(ctx context.Context, fabricIndex uint8, clientChallenge []byte) (resp opcreds.SignVidVerificationResponse, attestationChallenge []byte, err error)
}

// TrustAnchorLookup finds the globally trusted root of vendorID whose
// SubjectKeyID is subjectKeyID (the Operational Trust Anchors schema of
// the DCL) — chip's OnLookupOperationalTrustAnchor
// (VendorIdVerificationClient.h:66-67).
type TrustAnchorLookup func(ctx context.Context, vendorID uint16, subjectKeyID []byte) (globallyTrustedRoot []byte, err error)

// VerifyVendorID runs the Vendor ID Verification procedure on info's
// administrator fabric: a random 32-byte client challenge, the peer's
// SignVIDVerificationResponse, then [Verify]. random nil is crypto/rand.
// Mirrors VendorIdVerificationClient.cpp:127-187 (VerifyVendorId) and the
// response callback :145-162.
func VerifyVendorID(ctx context.Context, signer Signer, info *Info, lookup TrustAnchorLookup, random io.Reader) error {
	if signer == nil {
		return ErrNoSession // :169-174
	}
	if random == nil {
		random = rand.Reader
	}
	challenge := make([]byte, ClientChallengeSize) // :135-137
	if _, err := io.ReadFull(random, challenge); err != nil {
		return fmt.Errorf("jcm: client challenge: %w", err)
	}
	resp, attestation, err := signer.SignVIDVerification(ctx, info.AdminFabricIndex, challenge) // :142-143, :178-179
	if err != nil {
		return err // :164-167
	}
	return Verify(ctx, info, challenge, attestation, resp, lookup) // :158-161
}

// Verify checks a SignVIDVerificationResponse against info: the fabric
// binding message over the RCAC's public key, fabric id and vendor id; the
// signature by the NOC's key over the to-be-signed message; the NOC chain
// against the fabric's RCAC; and the chain against the globally trusted
// root lookup finds for the vendor id and the RCAC's SubjectKeyID. A
// VIDVerificationStatement is not supported (step 12a): the statement is
// empty. Mirrors VendorIdVerificationClient.cpp:51-125 (Verify), :28-49
// (VerifyNOCCertificateChain), CHIPCryptoPAL.cpp:933-958
// (GenerateVendorFabricBindingMessage) and :960-990
// (GenerateVendorIdVerificationToBeSigned).
func Verify(ctx context.Context, info *Info, clientChallenge, attestationChallenge []byte, resp opcreds.SignVidVerificationResponse, lookup TrustAnchorLookup) error {
	rcac, err := mattercert.Decode(info.AdminRCAC) // :64-66
	if err != nil {
		return fmt.Errorf("jcm: RCAC: %w", err)
	}
	tbs, err := toBeSigned(info.AdminFabricIndex, clientChallenge, attestationChallenge, rcac.PublicKey, info.AdminFabricID, info.AdminVendorID) // :68-82
	if err != nil {
		return err
	}
	noc, err := mattercert.Decode(info.AdminNOC) // :84-87
	if err != nil {
		return fmt.Errorf("jcm: NOC: %w", err)
	}
	nocKey, err := noc.PublicKeyECDSA()
	if err != nil {
		return fmt.Errorf("jcm: NOC: %w", err)
	}
	if !verifyRaw(nocKey, tbs, resp.Signature) { // :89-96
		return ErrSignature
	}
	if err := verifyChain(info.AdminNOC, info.AdminICAC, rcac.PublicKey); err != nil { // :98-101
		return err
	}
	if lookup == nil {
		return ErrNoTrustAnchor
	}
	root, err := lookup(ctx, info.AdminVendorID, rcac.Extensions.SubjectKeyID) // :110-114
	if err != nil {
		return err
	}
	trusted, err := mattercert.Decode(root)
	if err != nil {
		return fmt.Errorf("jcm: globally trusted root: %w", err)
	}
	return verifyChain(info.AdminNOC, info.AdminICAC, trusted.PublicKey) // :116-120
}

// toBeSigned is vendor_id_verification_tbs := version || client_challenge
// || attestation_challenge || fabric_index || vendor_fabric_binding_message,
// the binding message version || root_public_key || fabric_id || vendor_id,
// big endian, without a statement (CHIPCryptoPAL.cpp:946-950, :973-981).
func toBeSigned(fabricIndex uint8, clientChallenge, attestationChallenge, rootPublicKey []byte, fabricID uint64, vendorID uint16) ([]byte, error) {
	if len(clientChallenge) != ClientChallengeSize || len(attestationChallenge) != attestationChallengeSize || len(rootPublicKey) != 65 { // :964-967
		return nil, ErrBindingInput
	}
	out := make([]byte, 0, 1+ClientChallengeSize+attestationChallengeSize+1+1+65+8+2)
	out = append(out, FabricBindingVersion1)
	out = append(out, clientChallenge...)
	out = append(out, attestationChallenge...)
	out = append(out, fabricIndex, FabricBindingVersion1)
	out = append(out, rootPublicKey...)
	out = binary.BigEndian.AppendUint64(out, fabricID)
	return binary.BigEndian.AppendUint16(out, vendorID), nil
}

// ToBeSigned is the message a fabric's operational key signs for a
// SignVIDVerificationRequest without a statement — what a peer under test
// signs; see [Verify].
func ToBeSigned(fabricIndex uint8, clientChallenge, attestationChallenge, rootPublicKey []byte, fabricID uint64, vendorID uint16) ([]byte, error) {
	return toBeSigned(fabricIndex, clientChallenge, attestationChallenge, rootPublicKey, fabricID, vendorID)
}

// verifyRaw verifies a 64-byte r||s P-256 signature over SHA-256(msg)
// (P256PublicKey::ECDSA_validate_msg_signature).
func verifyRaw(pub *ecdsa.PublicKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	digest := sha256.Sum256(msg)
	return ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

// verifyChain validates the NOC (and ICAC, if any) against the root
// public key: the operational certificate checks and the signatures up to
// the root — chip's ValidateCert with kDigitalSignature and kClientAuth
// (VendorIdVerificationClient.cpp:28-49).
func verifyChain(noc, icac, rootPublicKey []byte) error {
	v, err := mattercert.NewVerifier(rootPublicKey, nil)
	if err != nil {
		return fmt.Errorf("jcm: root: %w", err)
	}
	if len(icac) == 0 {
		icac = nil
	}
	if _, err := v.VerifyAndExtractPubKey(noc, icac); err != nil {
		return fmt.Errorf("jcm: NOC chain: %w", err)
	}
	return nil
}
