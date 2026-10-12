// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// The commissioner's certificate authority: a fabric root (RCAC) and the
// node operational certificates (NOCs) it signs, as Matter TLV
// certificates. Every key, identifier and signature derives from
// [Options.Seed], so two runs with the same seed mint the same bytes.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/sha1" //nolint:gosec // Matter key identifiers are SHA-1 of the public key (matter.js CertificateAuthority.ts:170, :314)
	"crypto/sha256"
	"encoding/asn1"
	"encoding/binary"
	"math/big"
	"time"

	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
)

// Matter TLV certificate tags (secure/mattercert/decode.go:20-47) and the
// extension tags inside tag 10 (decode.go:79-86).
const (
	certTagSerial        = 1
	certTagSigAlgo       = 2
	certTagIssuer        = 3
	certTagNotBefore     = 4
	certTagNotAfter      = 5
	certTagSubject       = 6
	certTagPubKeyAlgo    = 7
	certTagCurve         = 8
	certTagPublicKey     = 9
	certTagExtensions    = 10
	certTagSignature     = 11
	dnTagNodeID          = 17
	dnTagRCACID          = 20
	dnTagFabricID        = 21
	dnTagCAT             = 22
	extTagBasicConstr    = 1
	extTagKeyUsage       = 2
	extTagExtKeyUsage    = 3
	extTagSubjectKeyID   = 4
	extTagAuthorityKeyID = 5

	// keyUsage bits as the TLV certificate carries them
	// (secure/mattercert/verify.go:332-338).
	keyUsageDigitalSignature = 0x0001
	keyUsageKeyCertSign      = 0x0020
	keyUsageCRLSign          = 0x0040
)

// rootCertID is the RCAC id: matter.js CertificateAuthority starts its
// root certificate id at 0 (CertificateAuthority.ts:52) and its NOC ids
// at 1 (:56).
const rootCertID = 0

// certNotBefore is the validity start of every certificate this CA mints,
// in Matter epoch seconds. matter.js stamps "now minus one year"
// (CertificateAuthority.ts:233, :304); a fixed instant keeps the minted
// bytes a function of the seed alone. NotAfter is 0, Matter's "no
// well-defined expiration date".
var certNotBefore = uint64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) / time.Second)

// testCA is one fabric's root of trust.
type testCA struct {
	seed     uint64
	fabricID uint64
	rootKey  *ecdsa.PrivateKey
	rootPub  []byte // 65-byte uncompressed point
	rootKID  []byte // SHA-1 of rootPub
	rootCert []byte // Matter TLV RCAC
	ipk      []byte // the raw epoch key AddNOC carries (IPKValue)
	nextID   uint64
}

// newTestCA derives the fabric root from seed.
func newTestCA(seed, fabricID uint64) *testCA {
	ca := &testCA{seed: seed, fabricID: fabricID, nextID: 1}
	ca.rootKey = deriveKey(seed, "root", 0)
	ca.rootPub = publicKeyBytes(&ca.rootKey.PublicKey)
	ca.rootKID = keyIdentifier(ca.rootPub)
	ca.ipk = deriveBytes(seed, "ipk", 0, 16)
	// Mirrors matter.js packages/protocol/src/certificate/
	// CertificateAuthority.ts:225-249 #generateRootCert: issuer and subject
	// carry the rcacId, the root is a CA with keyCertSign and cRLSign, and
	// both key identifiers are the root key's SHA-1.
	ca.rootCert = ca.sign(certFields{
		serial:  rootCertID,
		subject: func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(dnTagRCACID), rootCertID) },
		pub:     ca.rootPub,
		isCA:    true,
	})
	return ca
}

// compressedFabricID derives the compressed fabric id: HKDF-SHA256 over
// the root public key without its 0x04 prefix, salted with the fabric id
// big-endian, info "CompressedFabric", 8 bytes — the derivation the device
// runs at AddNOC (cluster/core/operational_credentials.go:1507-1535).
func (ca *testCA) compressedFabricID() [8]byte {
	var salt [8]byte
	binary.BigEndian.PutUint64(salt[:], ca.fabricID)
	out, err := hkdf.Key(sha256.New, ca.rootPub[1:], salt[:], "CompressedFabric", 8)
	mustNot(err)
	var id [8]byte
	copy(id[:], out)
	return id
}

// noc mints a NOC for nodeID over pub.
//
// Mirrors matter.js packages/protocol/src/certificate/
// CertificateAuthority.ts:287-320 generateNoc: subject {fabricId, nodeId,
// caseAuthenticatedTags}, not a CA, keyUsage digitalSignature,
// extendedKeyUsage [clientAuth, serverAuth], SHA-1 subject key identifier,
// the root's key identifier as authority key identifier.
func (ca *testCA) noc(pub []byte, nodeID uint64, cats []uint32) []byte {
	id := ca.nextID
	ca.nextID++
	return ca.sign(certFields{
		serial: id,
		subject: func(e *tlv.Encoder) {
			e.PutUint(tlv.ContextTag(dnTagNodeID), nodeID)
			e.PutUint(tlv.ContextTag(dnTagFabricID), ca.fabricID)
			for _, cat := range cats {
				e.PutUint(tlv.ContextTag(dnTagCAT), uint64(cat))
			}
		},
		pub: pub,
	})
}

// certFields are the parts of a certificate that differ between the RCAC
// and a NOC; the issuer is always this CA's root.
type certFields struct {
	serial  uint64
	subject func(e *tlv.Encoder)
	pub     []byte
	isCA    bool
}

// sign encodes f and signs it with the root key: the signature covers the
// X.509 DER form of the TBS certificate, which is what a verifier hashes
// (secure/mattercert/tbs_der.go:95 TBSToDER). ECDSA runs deterministically
// (RFC 6979, crypto/ecdsa with a nil random source).
func (ca *testCA) sign(f certFields) []byte {
	probe, err := mattercert.Decode(ca.encode(f, make([]byte, 64)))
	mustNot(err)
	der, err := mattercert.TBSToDER(probe)
	mustNot(err)
	digest := sha256.Sum256(der)
	asn, err := ca.rootKey.Sign(nil, digest[:], crypto.SHA256)
	mustNot(err)
	return ca.encode(f, rawSignature(asn))
}

// encode writes the certificate TLV with sig as its signature.
func (ca *testCA) encode(f certFields, sig []byte) []byte {
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(certTagSerial), serialBytes(f.serial))
	e.PutUint(tlv.ContextTag(certTagSigAlgo), mattercert.SigAlgoECDSAWithSHA256)
	e.StartList(tlv.ContextTag(certTagIssuer))
	e.PutUint(tlv.ContextTag(dnTagRCACID), rootCertID)
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(certTagNotBefore), certNotBefore)
	e.PutUint(tlv.ContextTag(certTagNotAfter), 0)
	e.StartList(tlv.ContextTag(certTagSubject))
	f.subject(e)
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(certTagPubKeyAlgo), mattercert.PubKeyAlgoEC)
	e.PutUint(tlv.ContextTag(certTagCurve), mattercert.CurvePrime256v1)
	e.PutOctets(tlv.ContextTag(certTagPublicKey), f.pub)
	e.StartList(tlv.ContextTag(certTagExtensions))
	e.StartStruct(tlv.ContextTag(extTagBasicConstr))
	e.PutBool(tlv.ContextTag(1), f.isCA)
	_ = e.EndContainer()
	if f.isCA {
		e.PutUint(tlv.ContextTag(extTagKeyUsage), keyUsageKeyCertSign|keyUsageCRLSign)
	} else {
		e.PutUint(tlv.ContextTag(extTagKeyUsage), keyUsageDigitalSignature)
		e.StartArray(tlv.ContextTag(extTagExtKeyUsage))
		e.PutUint(tlv.AnonymousTag(), 2) // clientAuth
		e.PutUint(tlv.AnonymousTag(), 1) // serverAuth
		_ = e.EndContainer()
	}
	e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), keyIdentifier(f.pub))
	e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), ca.rootKID)
	_ = e.EndContainer()
	e.PutOctets(tlv.ContextTag(certTagSignature), sig)
	_ = e.EndContainer()
	return mustBytes(e)
}

// serialBytes is the certificate id big-endian without leading zero
// bytes, one byte at least — matter.js Bytes.fromHex(toHex(id)).
func serialBytes(id uint64) []byte {
	b := new(big.Int).SetUint64(id).Bytes()
	if len(b) == 0 {
		return []byte{0}
	}
	return b
}

// rawSignature turns an ASN.1 ECDSA signature into the 64-byte r||s form
// Matter certificates and attestation signatures carry.
func rawSignature(asn []byte) []byte {
	var sig struct{ R, S *big.Int }
	_, err := asn1.Unmarshal(asn, &sig)
	mustNot(err)
	out := make([]byte, 64)
	sig.R.FillBytes(out[:32])
	sig.S.FillBytes(out[32:])
	return out
}

// deriveKey returns the P-256 key named by (seed, label, n).
func deriveKey(seed uint64, label string, n uint64) *ecdsa.PrivateKey {
	for ctr := uint64(0); ; ctr++ {
		// A scalar outside [1, N-1] is rejected; the next counter value
		// draws another (probability about 2^-32 per draw).
		if k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), deriveBytes(seed, label, n<<8|ctr, 32)); err == nil {
			return k
		}
	}
}

// deriveBytes returns size bytes named by (seed, label, n).
func deriveBytes(seed uint64, label string, n uint64, size int) []byte {
	var in [16]byte
	binary.BigEndian.PutUint64(in[:8], seed)
	binary.BigEndian.PutUint64(in[8:], n)
	out, err := hkdf.Key(sha256.New, in[:], nil, "go-fabric bridgetest "+label, size)
	mustNot(err)
	return out
}

// publicKeyBytes is the 65-byte uncompressed encoding of pub.
func publicKeyBytes(pub *ecdsa.PublicKey) []byte {
	b, err := pub.Bytes()
	mustNot(err)
	return b
}

// keyIdentifier is the SHA-1 key identifier matter.js derives
// (CertificateAuthority.ts:170, :314).
func keyIdentifier(pub []byte) []byte {
	sum := sha1.Sum(pub) //nolint:gosec // see the import
	return sum[:]
}

// mustBytes returns the encoder's bytes. An error there is an unbalanced
// container in this package's own encoding, never something the device
// sent.
func mustBytes(e *tlv.Encoder) []byte {
	b, err := e.Bytes()
	mustNot(err)
	return b
}

// mustNot panics on an error that only a defect in this package can
// produce (an encoder or a key derivation over constant inputs).
func mustNot(err error) {
	if err != nil {
		panic("bridgetest: " + err.Error())
	}
}
