// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Joint Commissioning: a port of connectedhomeip
// src/controller/tests/jcm/TestJCMCommissioner.cpp at 6170af84 (7
// TEST_F), by meaning. chip's MockClusterStateCache (:106-224) is a read
// report of a device that administers the JF-B fabric (a 3-tier fabric of
// controller/ca, its NOC with the administrator CAT, on endpoint 1); its
// TestVendorIDVerificationDataModel (:261-329) is jfbSigner, the device's
// SignVIDVerificationRequest; MockTrustVerificationDelegate (:67-104) is
// the JointFabricOptions' Progress and Consent with an anchor lookup that
// returns the fabric's root.
package commissioner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
)

const jfbAdminCAT uint32 = 0xFFFF_0001

// jfbFabric is the device's JF-B fabric (SetupForJFTests).
type jfbFabric struct {
	index            uint8
	fabricID, nodeID uint64
	vendorID         uint16
	rcac, icac, noc  []byte
	rootPub          []byte
	key              *ecdsa.PrivateKey
}

func newJFBFabric(t *testing.T) *jfbFabric {
	t.Helper()
	c, err := ca.New(ca.Config{IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	f := &jfbFabric{index: 2, fabricID: 0x0B0B, nodeID: 0x2222, vendorID: 0xFFF1, rcac: c.RootCert(), icac: c.ICACCert(), rootPub: c.RootPublicKey(), key: key}
	if f.noc, err = c.GenerateNOC(pub, f.fabricID, f.nodeID, []uint32{jfbAdminCAT}); err != nil {
		t.Fatal(err)
	}
	return f
}

func nodeOf(t *testing.T, fn func(e *tlv.Encoder)) spec.Node {
	t.Helper()
	e := tlv.NewEncoder()
	fn(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// report is MockClusterStateCache::SetUp (:117-178).
func (f *jfbFabric) report(t *testing.T) *interaction.Report {
	t.Helper()
	anon := tlv.AnonymousTag()
	attr := func(ep uint16, cluster, id uint32, n spec.Node) interaction.AttributeData {
		return interaction.AttributeData{Path: im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: id, HasEndpoint: true, HasCluster: true, HasAttribute: true}, Value: n}
	}
	noc := opcredsdef.NOCStruct{Noc: f.noc, FabricIndex: f.index}
	noc.Icac.Value = f.icac
	return &interaction.Report{Attributes: []interaction.AttributeData{
		attr(1, jfadef.ClusterID, jfadef.AttrAdministratorFabricIndex, nodeOf(t, func(e *tlv.Encoder) { e.PutUint(anon, uint64(f.index)) })),
		attr(0, opCredsCluster, opcredsdef.AttrTrustedRootCertificates, nodeOf(t, func(e *tlv.Encoder) {
			e.StartArray(anon)
			e.PutOctets(anon, f.rcac)
			_ = e.EndContainer()
		})),
		attr(0, opCredsCluster, opcredsdef.AttrFabrics, nodeOf(t, func(e *tlv.Encoder) {
			spec.List[opcredsdef.FabricDescriptorStruct]{{RootPublicKey: f.rootPub, VendorId: f.vendorID, FabricId: f.fabricID, NodeId: f.nodeID, FabricIndex: f.index}}.EncodeTLV(e, anon)
		})),
		attr(0, opCredsCluster, opcredsdef.AttrNocs, nodeOf(t, func(e *tlv.Encoder) { spec.List[opcredsdef.NOCStruct]{noc}.EncodeTLV(e, anon) })),
	}}
}

// jfbSigner is the device's SignVIDVerificationRequest on JF-B (:275-325)
// over the commissioning session's challenge.
type jfbSigner struct {
	f         *jfbFabric
	challenge []byte
}

func (s jfbSigner) SignVIDVerification(_ context.Context, fabricIndex uint8, client []byte) (opcredsdef.SignVidVerificationResponse, []byte, error) {
	rcac, err := mattercert.Decode(s.f.rcac)
	if err != nil {
		return opcredsdef.SignVidVerificationResponse{}, nil, err
	}
	tbs, err := jcm.ToBeSigned(fabricIndex, client, s.challenge, rcac.PublicKey, s.f.fabricID, s.f.vendorID)
	if err != nil {
		return opcredsdef.SignVidVerificationResponse{}, nil, err
	}
	d := sha256.Sum256(tbs)
	r, ss, err := ecdsa.Sign(rand.Reader, s.f.key, d[:])
	if err != nil {
		return opcredsdef.SignVidVerificationResponse{}, nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return opcredsdef.SignVidVerificationResponse{FabricIndex: fabricIndex, FabricBindingVersion: 1, Signature: sig}, s.challenge, nil
}

// delegate is MockTrustVerificationDelegate (:67-104).
type delegate struct {
	progress     int
	lastStage    jcm.Stage
	lastResult   jcm.Result
	asked        bool
	lookedUp     bool
	consent      bool
	lastVendorID uint16
}

func (d *delegate) options() *JointFabricOptions {
	return &JointFabricOptions{
		Progress: func(s jcm.Stage, r jcm.Result) { d.progress++; d.lastStage, d.lastResult = s, r },
		Consent: func(info TrustInfo) bool {
			d.asked, d.lastVendorID = true, info.AdminVendorID
			return d.consent
		},
		TrustAnchors: func(context.Context, uint16, []byte) ([]byte, error) { d.lookedUp = true; return nil, errLookup },
	}
}

var errLookup = jcm.ErrNoTrustAnchor

// newTrust is the fixture of :383-412: a commissioner trust verification
// over the JF-B device, the anchor lookup answering the fabric's root.
func newTrust(t *testing.T, d *delegate) (*jcmTrust, *jfbFabric) {
	t.Helper()
	f := newJFBFabric(t)
	challenge := make([]byte, 16)
	_, _ = rand.Read(challenge)
	opts := d.options()
	opts.TrustAnchors = func(context.Context, uint16, []byte) ([]byte, error) { d.lookedUp = true; return f.rcac, nil }
	return &jcmTrust{opts: opts, info: jcm.NewInfo(), challenge: challenge, signer: jfbSigner{f: f, challenge: challenge}}, f
}

// TestTrustVerificationStageFinishedProgressesThroughStages is :444-468.
func TestTrustVerificationStageFinishedProgressesThroughStages(t *testing.T) {
	d := &delegate{consent: true}
	tr, f := newTrust(t, d)
	if err := tr.parse(f.report(t)); err != nil {
		t.Fatal(err)
	}
	tr.start(t.Context(), jcm.StageIdle)
	if d.progress != 5 || d.lastStage != jcm.StageComplete || d.lastResult != jcm.Success || !d.asked || !d.lookedUp || d.lastVendorID != 0xFFF1 {
		t.Fatalf("delegate %+v", d)
	}
	if !tr.done || tr.failure.Result != jcm.Success {
		t.Fatalf("completion %v %+v", tr.done, tr.failure)
	}
}

// TestTrustVerificationStageFinishedHandlesUserConsent is :470-487.
func TestTrustVerificationStageFinishedHandlesUserConsent(t *testing.T) {
	d := &delegate{consent: false}
	tr, _ := newTrust(t, d)
	tr.info.AdminFabricIndex, tr.info.AdminEndpointID = 1, 1
	tr.start(t.Context(), jcm.StagePerformingVendorIDVerification)
	if !d.asked || d.progress != 2 || d.lastStage != jcm.StageAskingUserForConsent || d.lastResult != jcm.UserDeniedConsent {
		t.Fatalf("delegate %+v", d)
	}
}

// TestTrustVerificationStageFinishedHandlesError is :489-506: without the
// parsed information the vendor id verification fails.
func TestTrustVerificationStageFinishedHandlesError(t *testing.T) {
	d := &delegate{consent: true}
	tr, _ := newTrust(t, d)
	tr.start(t.Context(), jcm.StageVerifyingAdministratorInformation)
	if d.progress != 2 || d.lastStage != jcm.StagePerformingVendorIDVerification || d.lastResult != jcm.VendorIDVerificationFailed {
		t.Fatalf("delegate %+v", d)
	}
}

// TestParseAdminFabricIndexAndEndpointId is :508-517.
func TestParseAdminFabricIndexAndEndpointId(t *testing.T) {
	tr, f := newTrust(t, &delegate{})
	if err := tr.parseAdminFabricIndexAndEndpointID(f.report(t)); err != nil {
		t.Fatal(err)
	}
	if tr.info.AdminFabricIndex != f.index || tr.info.AdminEndpointID != 1 {
		t.Fatalf("info %+v", tr.info)
	}
}

// TestParseOperationalCredentials is :519-540.
func TestParseOperationalCredentials(t *testing.T) {
	tr, f := newTrust(t, &delegate{})
	r := f.report(t)
	if err := tr.parseAdminFabricIndexAndEndpointID(r); err != nil {
		t.Fatal(err)
	}
	if err := tr.parseOperationalCredentials(r); err != nil {
		t.Fatal(err)
	}
	if len(tr.info.RootPublicKey) != 65 || tr.info.AdminVendorID != f.vendorID || tr.info.AdminFabricID != f.fabricID ||
		len(tr.info.AdminNOC) != len(f.noc) || len(tr.info.AdminICAC) != len(f.icac) {
		t.Fatalf("info %+v", tr.info)
	}
}

// TestParseTrustedRoot is :542-568.
func TestParseTrustedRoot(t *testing.T) {
	tr, f := newTrust(t, &delegate{})
	r := f.report(t)
	if err := tr.parseAdminFabricIndexAndEndpointID(r); err != nil {
		t.Fatal(err)
	}
	if err := tr.parseOperationalCredentials(r); err != nil {
		t.Fatal(err)
	}
	if err := tr.parseTrustedRoot(r); err != nil {
		t.Fatal(err)
	}
	if len(tr.info.AdminRCAC) != len(f.rcac) {
		t.Fatalf("RCAC of %d bytes", len(tr.info.AdminRCAC))
	}
}

// TestParseExtraCommissioningInfo is :570-590.
func TestParseExtraCommissioningInfo(t *testing.T) {
	tr, f := newTrust(t, &delegate{})
	if err := tr.parse(f.report(t)); err != nil {
		t.Fatal(err)
	}
	if tr.info.AdminFabricIndex != f.index || tr.info.AdminEndpointID != 1 || len(tr.info.RootPublicKey) != 65 ||
		tr.info.AdminVendorID != f.vendorID || tr.info.AdminFabricID != f.fabricID || len(tr.info.AdminNOC) != len(f.noc) ||
		len(tr.info.AdminICAC) != len(f.icac) || len(tr.info.AdminRCAC) != len(f.rcac) {
		t.Fatalf("info %+v", tr.info)
	}
}
