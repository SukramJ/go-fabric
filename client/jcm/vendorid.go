// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm

import (
	"bytes"
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
	// ErrStatement is a VID Verification Statement that does not hold: not
	// version 1 of its 85-byte form, no VVSC to check it with, a signer
	// key identifier other than the VVSC's, or a VVSC that fails its
	// profile (matter.js VendorIdVerification.ts:230-293 at db021da0).
	ErrStatement = errors.New("jcm: VID Verification Statement does not verify")
)

// Statement layout, version 1 — VIDVerificationStatement := statement_version
// || vid_verification_signer_skid || vid_verification_statement_signature
// (chip CHIPCryptoPAL.h:127-141 at 6170af84, kVendorIdVerificationStatementV1Size
// == 85; matter.js VERIFICATION_STATEMENT_VERSION 0x21,
// VendorIdVerification.ts:36, VERIFICATION_STATEMENT_SIZE :44).
const (
	statementVersion1    = 0x21
	statementSize        = 1 + 20 + 64
	fabricBindingSize    = 1 + 65 + 8 + 2
	tbsBindingMessageOff = 1 + ClientChallengeSize + attestationChallengeSize + 1
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
// against the fabric's RCAC; and — without a VID Verification Statement —
// the chain against the globally trusted root lookup finds for the vendor
// id and the RCAC's SubjectKeyID (step 12b). Mirrors
// VendorIdVerificationClient.cpp:51-125 (Verify), :28-49
// (VerifyNOCCertificateChain), CHIPCryptoPAL.cpp:933-958
// (GenerateVendorFabricBindingMessage) and :960-990
// (GenerateVendorIdVerificationToBeSigned). With a statement in info
// (step 12a, which chip does not support, :103-104) the to-be-signed
// message carries it, and [verifyStatement] checks it.
func Verify(ctx context.Context, info *Info, clientChallenge, attestationChallenge []byte, resp opcreds.SignVidVerificationResponse, lookup TrustAnchorLookup) error {
	rcac, err := mattercert.Decode(info.AdminRCAC) // :64-66
	if err != nil {
		return fmt.Errorf("jcm: RCAC: %w", err)
	}
	tbs, err := toBeSigned(info.AdminFabricIndex, clientChallenge, attestationChallenge, rcac.PublicKey, info.AdminFabricID, info.AdminVendorID) // :68-82
	if err != nil {
		return err
	}
	statement := info.AdminVIDVerificationStatement
	if len(statement) > 0 {
		// vendor_id_verification_tbs ends with the statement
		// (CHIPCryptoPAL.h:150-155; VendorIdVerification.ts:89-91).
		tbs = append(tbs, statement...)
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
	if len(statement) > 0 {
		return verifyStatement(ctx, info, tbs[tbsBindingMessageOff:tbsBindingMessageOff+fabricBindingSize], lookup)
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

// verifyStatement is step 12a, the VID Verification Statement path: the
// statement is version 1 of its 85-byte form; the administrator's VVSC
// holds the VVSC profile (mattercert.DecodeVVSC, Vvsc.ts at db021da0); its
// SubjectKeyID is the statement's signer key identifier; its key signed
// statement_version || vendor_fabric_binding_message || signer_skid; and
// a trust anchor of the vendor — lookup for the vendor id and the VVSC's
// AuthorityKeyID — signed the VVSC. Mirrors matter.js
// VendorIdVerification.verifyData (VendorIdVerification.ts:230-293 at
// db021da0) with three differences, each failing closed where matter.js
// does not: a statement without a VVSC fails (matter.js returns true,
// "TODO Fail if not available!", :263-271); the VVSC's issuer is checked
// against a trust anchor (a TODO at :232-240); and the statement signature
// covers the 76-byte fabric binding message, as createStatementBytes
// names its input (:311-322), where matter.js passes the whole
// to-be-signed message (:282-286) — a message that carries the client
// and attestation challenges of one request, which a statement set once
// per fabric (SetVIDVerificationStatement, FabricDescriptorStruct field
// 6) cannot have been signed over. A VVSC under an intermediate
// certificate is refused: lookup's answer must have signed it.
func verifyStatement(ctx context.Context, info *Info, binding []byte, lookup TrustAnchorLookup) error {
	s := info.AdminVIDVerificationStatement
	if len(s) != statementSize {
		return fmt.Errorf("%w: %d bytes, want %d", ErrStatement, len(s), statementSize)
	}
	if s[0] != statementVersion1 {
		return fmt.Errorf("%w: version %#x", ErrStatement, s[0])
	}
	skid, sig := s[1:21], s[21:]
	if len(info.AdminVVSC) == 0 {
		return fmt.Errorf("%w: no VVSC", ErrStatement)
	}
	vvsc, err := mattercert.DecodeVVSC(info.AdminVVSC)
	if err != nil {
		return fmt.Errorf("%w: VVSC: %w", ErrStatement, err)
	}
	if !bytes.Equal(vvsc.Extensions.SubjectKeyID, skid) {
		return fmt.Errorf("%w: the signer key identifier is not the VVSC's", ErrStatement)
	}
	key, err := vvsc.PublicKeyECDSA()
	if err != nil {
		return fmt.Errorf("%w: VVSC: %w", ErrStatement, err)
	}
	msg := make([]byte, 0, 1+fabricBindingSize+len(skid))
	msg = append(msg, statementVersion1)
	msg = append(msg, binding...)
	msg = append(msg, skid...)
	if !verifyRaw(key, msg, sig) {
		return ErrSignature
	}
	if lookup == nil {
		return ErrNoTrustAnchor
	}
	anchor, err := lookup(ctx, info.AdminVendorID, vvsc.Extensions.AuthorityKeyID)
	if err != nil {
		return err
	}
	root, err := mattercert.Decode(anchor)
	if err != nil {
		return fmt.Errorf("jcm: VVSC trust anchor: %w", err)
	}
	if err := mattercert.VerifyVVSCIssuedBy(vvsc, root.PublicKey); err != nil {
		return fmt.Errorf("%w: VVSC issuer: %w", ErrStatement, err)
	}
	return nil
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
// (VendorIdVerificationClient.cpp:28-49). mattercert's NOC check requires
// serverAuth since the db021da0 lift, as matter.js's VendorIdVerification
// verifies through Noc.verify (VendorIdVerification.ts:228, Noc.ts:148-154
// at db021da0, which notes chip's clientAuth there).
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
