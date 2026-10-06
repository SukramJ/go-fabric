// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// vendorUpdatingStore adds the vendor-id capability to the fake store.
type vendorUpdatingStore struct{ *fakeStore }

func (s vendorUpdatingStore) UpdateFabricVendorID(_ context.Context, idx uint8, vid uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fabrics[idx]
	if !ok {
		return mstore.ErrFabricNotFound
	}
	f.VendorID = vid
	s.fabrics[idx] = f
	return nil
}

func vidStatusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sc interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &sc) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sc.MatterStatusCode()
}

// TestSetVidVerificationStatementParityMatterJS pins matter.js
// setVidVerificationStatement + Fabric.updateVendorVerificationData for the
// accessing fabric: an accessing fabric is required, at least one field,
// a valid VendorID (0..0xFFF4), a statement of 0 or 85 bytes (empty
// clears), a VVSC only without an ICAC; the stored statement and VVSC
// appear in Fabrics and NOCs and the VendorID in Fabrics.
func TestSetVidVerificationStatementParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	oc, err := core.NewOperationalCredentials(vendorUpdatingStore{fs}, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	_, _, fabricIndex := commissionTestFabric(ctx, t, oc)
	fctx := im.WithFabricFilter(ctx, true, fabricIndex)
	set := func(c context.Context, req core.SetVidVerificationStatementRequest) error {
		_, err := oc.MatterInvoke(c, 0x0C, req)
		return err
	}
	if got := vidStatusOf(t, set(ctx, core.SetVidVerificationStatementRequest{HasVendorID: true, VendorID: 1})); got != im.StatusUnsupportedAccess {
		t.Errorf("without an accessing fabric: %v, want UnsupportedAccess", got)
	}
	if got := vidStatusOf(t, set(fctx, core.SetVidVerificationStatementRequest{})); got != im.StatusInvalidCommand {
		t.Errorf("no field: %v, want InvalidCommand", got)
	}
	if got := vidStatusOf(t, set(fctx, core.SetVidVerificationStatementRequest{HasVendorID: true, VendorID: 0xFFF5})); got != im.StatusConstraintError {
		t.Errorf("VendorID 0xFFF5: %v, want ConstraintError", got)
	}
	if got := vidStatusOf(t, set(fctx, core.SetVidVerificationStatementRequest{HasVidVerificationStatement: true, VidVerificationStatement: make([]byte, 10)})); got != im.StatusConstraintError {
		t.Errorf("10-byte statement: %v, want ConstraintError", got)
	}
	statement := bytes.Repeat([]byte{0x21}, 85)
	if err := set(fctx, core.SetVidVerificationStatementRequest{
		HasVendorID: true, VendorID: 0xFFF2,
		HasVidVerificationStatement: true, VidVerificationStatement: statement,
	}); err != nil {
		t.Fatalf("valid statement: %v", err)
	}
	v, _ := oc.MatterReadFiltered(fctx, 0x0001)
	fab := v.([]core.FabricDescriptorStruct)
	if len(fab) != 1 || !bytes.Equal(fab[0].VidVerificationStatement, statement) || fab[0].VendorID != 0xFFF2 {
		t.Fatalf("Fabrics after the update: %+v", fab)
	}
	id, _ := fs.GetIdentity(ctx, fabricIndex)
	vvscReq := core.SetVidVerificationStatementRequest{HasVvsc: true, Vvsc: []byte{1, 2, 3}}
	if len(id.ICAC) > 0 {
		if got := vidStatusOf(t, set(fctx, vvscReq)); got != im.StatusInvalidCommand {
			t.Errorf("VVSC on a fabric with an ICAC: %v, want InvalidCommand", got)
		}
	} else {
		if err := set(fctx, vvscReq); err != nil {
			t.Fatalf("VVSC: %v", err)
		}
		v, _ := oc.MatterReadFiltered(fctx, 0x0000)
		if nocs := v.([]core.NOCStruct); len(nocs) != 1 || !bytes.Equal(nocs[0].Vvsc, []byte{1, 2, 3}) {
			t.Fatalf("NOCs after the VVSC: %+v", nocs)
		}
	}
	if err := set(fctx, core.SetVidVerificationStatementRequest{HasVidVerificationStatement: true}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	v, _ = oc.MatterReadFiltered(fctx, 0x0001)
	if fab := v.([]core.FabricDescriptorStruct); fab[0].VidVerificationStatement != nil {
		t.Fatalf("statement not cleared: %x", fab[0].VidVerificationStatement)
	}
	oc.NotifyFabricRemoved(fabricIndex)
}

// TestSignVidVerificationRequestParityMatterJS pins matter.js
// signVidVerificationRequest: an unknown fabric is ConstraintError; the
// response carries FabricBindingVersion 1 and an ECDSA-P256/SHA-256
// signature by the fabric's operational key over
// VendorIdVerification.dataToSign, with the invoking session's
// attestation challenge.
func TestSignVidVerificationRequestParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, fs := opcredsWithFakeStore(t)
	_, _, fabricIndex := commissionTestFabric(ctx, t, oc)
	clientChallenge := bytes.Repeat([]byte{0xC1}, 32)
	attChallenge := bytes.Repeat([]byte{0xA7}, 16)
	sctx := core.WithInvokeAttestationChallenge(im.WithFabricFilter(ctx, true, fabricIndex), attChallenge)

	if _, err := oc.MatterInvoke(sctx, 0x0D, core.SignVidVerificationRequest{FabricIndex: 99, ClientChallenge: clientChallenge}); vidStatusOf(t, err) != im.StatusConstraintError {
		t.Fatalf("unknown fabric: %v, want ConstraintError", err)
	}
	resp, err := oc.MatterInvoke(sctx, 0x0D, core.SignVidVerificationRequest{FabricIndex: fabricIndex, ClientChallenge: clientChallenge})
	if err != nil {
		t.Fatal(err)
	}
	r := resp.(core.SignVidVerificationResponse)
	if r.FabricIndex != fabricIndex || r.FabricBindingVersion != 1 || len(r.Signature) != 64 {
		t.Fatalf("response %+v", r)
	}
	fabric, _ := fs.GetFabric(ctx, fabricIndex)
	id, _ := fs.GetIdentity(ctx, fabricIndex)
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), id.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	tbs := []byte{1}
	tbs = append(tbs, clientChallenge...)
	tbs = append(tbs, attChallenge...)
	tbs = append(tbs, fabricIndex, 1)
	tbs = append(tbs, fabric.RootPublicKey...)
	tbs = binary.BigEndian.AppendUint64(tbs, fabric.FabricID)
	tbs = binary.BigEndian.AppendUint16(tbs, fabric.VendorID)
	digest := sha256.Sum256(tbs)
	rr, ss := new(big.Int).SetBytes(r.Signature[:32]), new(big.Int).SetBytes(r.Signature[32:])
	if !ecdsa.Verify(&priv.PublicKey, digest[:], rr, ss) {
		t.Fatal("the signature does not verify over dataToSign with the operational key")
	}
}
