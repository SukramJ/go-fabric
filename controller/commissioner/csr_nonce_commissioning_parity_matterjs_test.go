// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// Ports of matter.js at b25ed960, on a scripted device that answers the
// attestation and CSR commands with a real test chain:
// packages/node/test/node/CsrNonceCommissioningTest.ts (a device that signs
// NOCSRElements over another CSRNonce stops the commissioning with the
// NocsrError CsrNonceMismatch), and the describe "BasicInformation VendorID
// that does not match the CD" of
// packages/node/test/node/ClientAttestationTest.ts:549-591, which matter.js
// runs through a commissioning on its MockSite and this port runs through
// the flow's attestation step.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"regexp"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	opcredsdef "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/im"
	secattestation "thingfabric.dev/go-fabric/secure/attestation"
	"thingfabric.dev/go-fabric/tlv"
)

// attestingDevice answers CertificateChainRequest, AttestationRequest and
// CSRRequest as a device with a CSA test chain of 0xFFF1/0x8000 does,
// signing with its DAC key over the session's challenge. wrongCSRNonce
// signs NOCSRElements over the sent nonce with every byte inverted
// (WrongCsrNonceServer, CsrNonceCommissioningTest.ts:15-20).
// AddTrustedRootCertificate is answered with a FAILURE status and
// recorded, so a test sees whether the flow got past the CSR.
type attestingDevice struct {
	chain         *secattestation.Chain
	cd            []byte
	challenge     []byte
	wrongCSRNonce bool

	mu        sync.Mutex
	addedRoot bool
}

func newAttestingDevice(t *testing.T) *attestingDevice {
	t.Helper()
	chain, err := secattestation.BuildTestChain(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	return &attestingDevice{chain: chain, cd: cd}
}

// sign is the raw r‖s ECDSA signature of the DAC key over elements ‖
// challenge.
func (d *attestingDevice) sign(t *testing.T, elements []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(slices.Concat(elements, d.challenge))
	r, s, err := ecdsa.Sign(rand.Reader, d.chain.DACKey, digest[:])
	if err != nil {
		t.Error(err)
		return nil
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out
}

func (d *attestingDevice) rootAdded() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.addedRoot
}

func (d *attestingDevice) handler(t *testing.T) func(imRequest) (uint8, []byte, bool) {
	t.Helper()
	return func(req imRequest) (uint8, []byte, bool) {
		path, fields, err := decodeInvoke(req.payload)
		if req.opcode != im.OpcodeInvokeRequest || err != nil {
			return 0, nil, false
		}
		switch path.Command {
		case opcredsdef.CmdCertificateChainRequest:
			r, err := spec.DecodeStruct[opcredsdef.CertificateChainRequestRequest](fields)
			if err != nil {
				return 0, nil, false
			}
			cert := d.chain.DAC
			if r.CertificateType == opcredsdef.CertificateChainTypePaiCertificate {
				cert = d.chain.PAI
			}
			path.Command = opcredsdef.CmdCertificateChainResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, opcredsdef.CertificateChainResponse{Certificate: cert}), true
		case opcredsdef.CmdAttestationRequest:
			r, err := spec.DecodeStruct[opcredsdef.AttestationRequestRequest](fields)
			if err != nil {
				return 0, nil, false
			}
			elements := encodeTLV(func(e *tlv.Encoder) {
				e.StartStruct(tlv.AnonymousTag())
				e.PutOctets(tlv.ContextTag(1), d.cd)
				e.PutOctets(tlv.ContextTag(2), r.AttestationNonce)
				e.PutUint32(tlv.ContextTag(3), 0)
				_ = e.EndContainer()
			})
			path.Command = opcredsdef.CmdAttestationResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, rawSignedValue{elements, d.sign(t, elements)}), true
		case opcredsdef.CmdCsrRequest:
			r, err := spec.DecodeStruct[opcredsdef.CsrRequestRequest](fields)
			if err != nil {
				return 0, nil, false
			}
			nonce := append([]byte(nil), r.CsrNonce...)
			if d.wrongCSRNonce {
				for i := range nonce {
					nonce[i] ^= 0xff
				}
			}
			opKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return 0, nil, false
			}
			csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{"CSR"}}}, opKey)
			if err != nil {
				return 0, nil, false
			}
			elements := encodeTLV(func(e *tlv.Encoder) {
				e.StartStruct(tlv.AnonymousTag())
				e.PutOctets(tlv.ContextTag(1), csr)
				e.PutOctets(tlv.ContextTag(2), nonce)
				_ = e.EndContainer()
			})
			path.Command = opcredsdef.CmdCsrResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, rawSignedValue{elements, d.sign(t, elements)}), true
		case opcredsdef.CmdAddTrustedRootCertificate:
			d.mu.Lock()
			d.addedRoot = true
			d.mu.Unlock()
			return im.OpcodeInvokeResponse, invokeStatusOf(path, im.StatusFailure), true
		}
		return 0, nil, false
	}
}

// rawSignedValue is an AttestationResponse or CSRResponse with the given
// elements and signature.
type rawSignedValue struct{ elements, signature []byte }

func (v rawSignedValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartStruct(tag)
	e.PutOctets(tlv.ContextTag(0), v.elements)
	e.PutOctets(tlv.ContextTag(1), v.signature)
	_ = e.EndContainer()
}

// attestingFlow is a flow over d, BasicInformation reporting 0xFFF1/0x8000
// and no trust store.
func attestingFlow(t *testing.T, d *attestingDevice, policy attestation.Policy) *flow {
	t.Helper()
	f, _ := newTestFlow(t, d.handler(t))
	d.challenge = f.challenge
	f.data.vendorID, f.data.productID = new(uint16(0xFFF1)), new(uint16(0x8000))
	f.opts.OnAttestationFailure = policy
	return f
}

// attestAndCertify runs the attestation step (10.1), then the CSR step
// (11.1), as the flow orders them — without execute, whose disarm after a
// failure this device does not answer.
func attestAndCertify(t *testing.T, f *flow) error {
	t.Helper()
	ctx := context.Background()
	if _, err := f.deviceAttestation(ctx); err != nil {
		t.Fatalf("deviceAttestation: %v", err)
	}
	_, err := f.certificates(ctx)
	return err
}

// TestCsrNonceCommissioningParity is the describe "CSR nonce check during
// commissioning" (CsrNonceCommissioningTest.ts:22).
func TestCsrNonceCommissioningParity(t *testing.T) {
	t.Parallel()
	t.Run("stops commissioning when the CSRResponse carries another nonce", func(t *testing.T) {
		t.Parallel()
		d := newAttestingDevice(t)
		d.wrongCSRNonce = true
		err := attestAndCertify(t, attestingFlow(t, d, attestation.Accept()))
		var ne *attestation.NOCSRError
		if !errors.Is(err, ErrCommissioning) || !errors.As(err, &ne) || ne.Check != attestation.NOCSRCSRNonceMismatch {
			t.Fatalf("certificates = %v, want the NocsrError CsrNonceMismatch as a commissioning error", err)
		}
		if d.rootAdded() {
			t.Fatal("the flow went on to AddTrustedRootCertificate")
		}
	})
	// The control: the same device echoing the nonce gets past the CSR.
	t.Run("goes on past the CSR when the CSRResponse carries the nonce sent", func(t *testing.T) {
		t.Parallel()
		d := newAttestingDevice(t)
		err := attestAndCertify(t, attestingFlow(t, d, attestation.Accept()))
		var ne *attestation.NOCSRError
		if errors.As(err, &ne) || !d.rootAdded() {
			t.Fatalf("certificates = %v, root added %v: want the flow past the CSR", err, d.rootAdded())
		}
	})
}

// TestCertificatesRequiresTheDACKey: the CSR step without the DAC key the
// attestation step keeps is the flow's own defect, not a commissioning
// error, and no CSRResponse is validated without the key
// (ControllerCommissioningFlow.ts:1450-1452 at b25ed960, an InternalError).
func TestCertificatesRequiresTheDACKey(t *testing.T) {
	t.Parallel()
	d := newAttestingDevice(t)
	f := attestingFlow(t, d, attestation.Accept())
	_, err := f.certificates(context.Background())
	if !errors.Is(err, errNoDACKey) || errors.Is(err, ErrCommissioning) {
		t.Fatalf("certificates without a DAC key = %v, want errNoDACKey", err)
	}
	if d.rootAdded() {
		t.Fatal("the flow went on to AddTrustedRootCertificate")
	}
}

// TestClientAttestationBasicInformationVendorIDParity is the describe
// "BasicInformation VendorID that does not match the CD"
// (ClientAttestationTest.ts:549-591 at b25ed960): the device reports
// VendorID 0xFFF2 while its CD names 0xFFF1.
func TestClientAttestationBasicInformationVendorIDParity(t *testing.T) {
	t.Parallel()
	mismatch := regexp.MustCompile(`CD vendor_id 0xFFF1 does not match BasicInformation VendorID 0xFFF2`)
	run := func(t *testing.T, store attestation.PAAStore, policy attestation.Policy) (*flow, *attestingDevice, error) {
		t.Helper()
		d := newAttestingDevice(t)
		f := attestingFlow(t, d, policy)
		f.data.vendorID = new(uint16(0xFFF2))
		f.c.ctx.Attestation = store
		_, err := f.deviceAttestation(context.Background())
		return f, d, err
	}
	trusted := func() attestation.PAAStore { return attestation.TestStore() }

	t.Run("reports CertificationDeclarationFieldMismatch, callback rejects", func(t *testing.T) {
		t.Parallel()
		var judged []attestation.Finding
		_, _, err := run(t, trusted(), attestation.Policy{Decide: func(fs []attestation.Finding) (bool, string, error) {
			judged = fs
			return false, "", nil
		}})
		var ce *CommissioningError
		if !errors.As(err, &ce) || !regexp.MustCompile(`^Device attestation rejected by policy: CD vendor_id 0xFFF1 does not match`).MatchString(ce.Message) {
			t.Fatalf("deviceAttestation = %v, want the rejection naming the mismatch", err)
		}
		var errs []attestation.Finding
		for _, fd := range judged {
			if fd.Level == attestation.LevelError {
				errs = append(errs, fd)
			}
		}
		if len(errs) != 1 || errs[0].Check != attestation.CertificationDeclarationFieldMismatch || !mismatch.MatchString(errs[0].Message) {
			t.Fatalf("error findings %+v, want one CertificationDeclarationFieldMismatch", errs)
		}
	})
	t.Run("reports CertificationDeclarationFieldMismatch, callback accepts → commissioning proceeds", func(t *testing.T) {
		t.Parallel()
		f, _, err := run(t, trusted(), attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return true, "", nil }})
		if err != nil {
			t.Fatalf("deviceAttestation = %v, want it accepted", err)
		}
		// The step proceeds with the validator's DAC key; the pair is not
		// attested, as an error finding was accepted.
		if f.dacKey == nil || f.attested != nil {
			t.Fatalf("dacKey %v, attested %v", f.dacKey, f.attested)
		}
	})
	t.Run("names every error finding when the policy rejects", func(t *testing.T) {
		t.Parallel()
		testOnly := &attestation.StaticStore{PAAs: attestation.TestStore().PAAs} // paaAsTestOnly: the PAAs are test roots, test certificates refused
		_, _, err := run(t, testOnly, attestation.Reject())
		var ce *CommissioningError
		if !errors.As(err, &ce) || !errors.Is(err, ErrAttestationRejected) {
			t.Fatalf("deviceAttestation = %v, want a CommissioningError", err)
		}
		if !regexp.MustCompile(`is a test/development certificate`).MatchString(ce.Message) || !mismatch.MatchString(ce.Message) {
			t.Fatalf("message %q, want both error findings named", ce.Message)
		}
	})
}

// onlySignature is a CSRResponse without NOCSRElements.
type onlySignature struct{}

func (onlySignature) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartStruct(tag)
	e.PutOctets(tlv.ContextTag(1), make([]byte, 64))
	_ = e.EndContainer()
}

// TestCertificatesMissingNOCSRElements: a CSRResponse that leaves out
// NOCSRElements decodes, and is the NOCSR check FieldMissing as a
// commissioning error — not unexpected data (NocsrValidator.ts:82-84 at
// db91886c; matter.js decodes a command response without a check against
// its schema).
func TestCertificatesMissingNOCSRElements(t *testing.T) {
	t.Parallel()
	dev := func(req imRequest) (uint8, []byte, bool) {
		path, _, err := decodeInvoke(req.payload)
		if req.opcode != im.OpcodeInvokeRequest || err != nil || path.Command != opcredsdef.CmdCsrRequest {
			return 0, nil, false
		}
		path.Command = opcredsdef.CmdCsrResponse
		return im.OpcodeInvokeResponse, invokeResponseOf(path, onlySignature{}), true
	}
	f, _ := newTestFlow(t, dev)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f.dacKey = &key.PublicKey
	_, err := f.certificates(context.Background())
	var ne *attestation.NOCSRError
	if !errors.Is(err, ErrCommissioning) || !errors.As(err, &ne) || ne.Check != attestation.NOCSRFieldMissing {
		t.Fatalf("certificates = %v, want the NOCSR check FieldMissing as a commissioning error", err)
	}
}
