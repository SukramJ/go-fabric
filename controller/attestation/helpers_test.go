// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // key identifiers are SHA-1 of the key, as the device's builder derives them
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/tlv"
)

var (
	oidVID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidPID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
	oidCN  = asn1.ObjectIdentifier{2, 5, 4, 3}
)

// chainSpec describes a PAI and DAC under the CSA test PAA (VID 0xFFF1).
type chainSpec struct {
	paiVID, dacVID, dacPID uint16
	paiNoVID               bool
	paiNotBefore           time.Time
	dacNotBefore           time.Time
	// dacSigner signs the DAC in place of the PAI key.
	dacSigner *ecdsa.PrivateKey
	// paiSigner signs the PAI in place of the PAA key.
	paiSigner *ecdsa.PrivateKey
	// paiAKID overrides the PAI's authority key identifier.
	paiAKID []byte
}

func defaultSpec() chainSpec {
	return chainSpec{
		paiVID: 0xFFF1, dacVID: 0xFFF1, dacPID: 0x8000,
		paiNotBefore: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		dacNotBefore: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func skid(pub *ecdsa.PublicKey) []byte {
	b, _ := pub.Bytes()
	s := sha1.Sum(b) //nolint:gosec // see the import
	return s[:]
}

func name(cn string, vid uint16, withVID bool, pid uint16, withPID bool) pkix.Name {
	extra := []pkix.AttributeTypeAndValue{{Type: oidCN, Value: cn}}
	if withVID {
		extra = append(extra, pkix.AttributeTypeAndValue{Type: oidVID, Value: fmt.Sprintf("%04X", vid)})
	}
	if withPID {
		extra = append(extra, pkix.AttributeTypeAndValue{Type: oidPID, Value: fmt.Sprintf("%04X", pid)})
	}
	return pkix.Name{ExtraNames: extra}
}

// buildChain mints a PAI and DAC per s and returns their DER and the DAC key.
func buildChain(t *testing.T, s chainSpec) (dac, pai []byte, dacKey *ecdsa.PrivateKey) {
	t.Helper()
	paa, err := x509.ParseCertificate(secattestation.TestPAAFFF1Cert)
	if err != nil {
		t.Fatal(err)
	}
	paiKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	dacKey, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	paiSigner := s.paiSigner
	if paiSigner == nil {
		paiSigner = secattestation.TestPAAFFF1PrivateKey
	}
	akid := s.paiAKID
	if akid == nil {
		akid = paa.SubjectKeyId
	}
	paiTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(0x1234),
		Subject:               name("Test PAI", s.paiVID, !s.paiNoVID, 0, false),
		NotBefore:             s.paiNotBefore,
		NotAfter:              time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          skid(&paiKey.PublicKey),
		AuthorityKeyId:        akid,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	// crypto/x509 takes the AKID from the parent's SKID and checks the
	// signer against the parent's key: a parent copy carries the overrides.
	paaParent := *paa
	paaParent.SubjectKeyId = akid
	paaParent.PublicKey = paiSigner.Public()
	pai, err = x509.CreateCertificate(rand.Reader, paiTmpl, &paaParent, &paiKey.PublicKey, paiSigner)
	if err != nil {
		t.Fatal(err)
	}
	paiCert, _ := x509.ParseCertificate(pai)
	dacSigner := s.dacSigner
	if dacSigner == nil {
		dacSigner = paiKey
	}
	dacTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(0x5678),
		Subject:               name("Test DAC", s.dacVID, true, s.dacPID, true),
		NotBefore:             s.dacNotBefore,
		NotAfter:              time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		SubjectKeyId:          skid(&dacKey.PublicKey),
		AuthorityKeyId:        paiCert.SubjectKeyId,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	paiParent := *paiCert
	paiParent.PublicKey = dacSigner.Public()
	dac, err = x509.CreateCertificate(rand.Reader, dacTmpl, &paiParent, &dacKey.PublicKey, dacSigner)
	if err != nil {
		t.Fatal(err)
	}
	return dac, pai, dacKey
}

// cdSpec describes a Certification Declaration.
type cdSpec struct {
	vid                    uint16
	pids                   []uint16
	certType               uint8
	originVID, originPID   *uint16
	authorizedPAAs         [][]byte
	signer                 *ecdsa.PrivateKey
	signerSKID             []byte
	corruptTLV, corruptCMS bool
}

func defaultCD() cdSpec {
	return cdSpec{vid: 0xFFF1, pids: []uint16{0x8000}}
}

// buildCD encodes the CD TLV (certification-declaration.ts:32-48) and wraps
// it in CMS SignedData as secure/attestation/cd.go does.
func buildCD(t *testing.T, s cdSpec) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(0), 1)
	e.PutUint(tlv.ContextTag(1), uint64(s.vid))
	e.StartArray(tlv.ContextTag(2))
	for _, p := range s.pids {
		e.PutUint(tlv.AnonymousTag(), uint64(p))
	}
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(3), 22)
	e.PutUTF8(tlv.ContextTag(4), "CSA00000SWC00000-00")
	e.PutUint(tlv.ContextTag(5), 0)
	e.PutUint(tlv.ContextTag(6), 0)
	e.PutUint(tlv.ContextTag(7), 1)
	e.PutUint(tlv.ContextTag(8), uint64(s.certType))
	if s.originVID != nil {
		e.PutUint(tlv.ContextTag(9), uint64(*s.originVID))
	}
	if s.originPID != nil {
		e.PutUint(tlv.ContextTag(10), uint64(*s.originPID))
	}
	if s.authorizedPAAs != nil {
		e.StartArray(tlv.ContextTag(11))
		for _, p := range s.authorizedPAAs {
			e.PutOctets(tlv.AnonymousTag(), p)
		}
		_ = e.EndContainer()
	}
	_ = e.EndContainer()
	content, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if s.corruptTLV {
		content = content[:len(content)-3]
	}
	signer, id := s.signer, s.signerSKID
	if signer == nil {
		signer = secattestation.TestCMSSignerPrivateKey
	}
	if id == nil {
		id = secattestation.TestCMSSignerSKID
	}
	digest := sha256.Sum256(content)
	sig, err := ecdsa.SignASN1(rand.Reader, signer, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	type algo struct {
		Algorithm  asn1.ObjectIdentifier
		Parameters asn1.RawValue `asn1:"optional"`
	}
	type signerInfo struct {
		Version            int
		SID                asn1.RawValue
		DigestAlgorithm    algo
		SignatureAlgorithm algo
		Signature          []byte
	}
	type encap struct {
		EContentType asn1.ObjectIdentifier
		EContent     asn1.RawValue `asn1:"explicit,tag:0,optional"`
	}
	type signedData struct {
		Version          int
		DigestAlgorithms []algo `asn1:"set"`
		EncapContentInfo encap
		SignerInfos      []signerInfo `asn1:"set"`
	}
	type contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	sha := asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	ecdsaSHA := asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	octets, _ := asn1.Marshal(content)
	sd, err := asn1.Marshal(signedData{
		Version:          3,
		DigestAlgorithms: []algo{{Algorithm: sha}},
		EncapContentInfo: encap{EContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}, EContent: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: octets}},
		SignerInfos: []signerInfo{{
			Version: 3, SID: asn1.RawValue{Class: 2, Tag: 0, Bytes: id},
			DigestAlgorithm: algo{Algorithm: sha}, SignatureAlgorithm: algo{Algorithm: ecdsaSHA}, Signature: sig,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := asn1.Marshal(contentInfo{ContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}, Content: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: sd}})
	if err != nil {
		t.Fatal(err)
	}
	if s.corruptCMS {
		out = out[:10]
	}
	return out
}

// elements encodes AttestationElements as the device does
// (cluster/core/operational_credentials.go:1323-1343).
func elements(t *testing.T, cd, nonce []byte) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(1), cd)
	e.PutOctets(tlv.ContextTag(2), nonce)
	e.PutUint32(tlv.ContextTag(3), 0)
	_ = e.EndContainer()
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sign is the device's signature over elements ‖ challenge, raw r‖s.
func sign(t *testing.T, key *ecdsa.PrivateKey, elems, challenge []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(append(append([]byte(nil), elems...), challenge...))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out
}

// fixture is one attestation exchange.
type fixture struct {
	ctx  attestation.Context
	data attestation.Data
}

func newFixture(t *testing.T, s chainSpec, cd cdSpec) fixture {
	t.Helper()
	dac, pai, key := buildChain(t, s)
	challenge := make([]byte, 16)
	nonce := make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	el := elements(t, buildCD(t, cd), nonce)
	return fixture{
		ctx: attestation.Context{Challenge: challenge, Store: attestation.TestStore()},
		data: attestation.Data{
			DAC: dac, PAI: pai, Elements: el, Signature: sign(t, key, el, challenge), Nonce: nonce,
			VendorID: 0xFFF1, ProductID: 0x8000,
		},
	}
}
