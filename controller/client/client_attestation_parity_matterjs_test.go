// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientAttestationTest.ts at
// 862ad75f: device attestation while the controller's registry commissions
// a device — without a trust store, with the device's PAA trusted, trusted
// as a test root only, or another PAA trusted, under each form of
// onAttestationFailure (undefined, true, false, a callback that accepts,
// rejects, returns a reason or fails).
//
// matter.js's trust store is a DclCertificateService fed by a mocked DCL;
// the port gives the commissioner an attestation.StaticStore with the
// roots the case names (fabrictest.ControllerOptions.Attestation, and
// NoAttestationStore for "no DclCertificateService"): the device's chain
// ends in the VID-less CSA test PAA as MockServerNode's does, so "correct
// PAA" is that root (TestCert_PAA_NoVID) and "wrong PAA" the FFF1 one.
// onAttestationFailure is attestation.Policy
// (OnFailure for the boolean, Decide for the callback); a matter.js
// CommissioningError is commissioner.ErrCommissioning, a
// DeviceAttestationError an *attestation.Error.
package client_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// attestationStore is a trust store of the given roots with the CSA test
// CD signer (matter.js's DCL with CertificationDeclaration.testSignerCertificate).
func attestationStore(acceptTest bool, roots ...[]byte) *attestation.StaticStore {
	s := &attestation.StaticStore{
		CDSigners: map[string]*ecdsa.PublicKey{
			strings.ToUpper(hex.EncodeToString(secattestation.TestCMSSignerSKID)): &secattestation.TestCMSSignerPrivateKey.PublicKey,
		},
		AcceptTest: acceptTest,
	}
	for _, r := range roots {
		s.PAAs = append(s.PAAs, attestation.PAA{DER: r})
	}
	return s
}

// attestationRun is ClientAttestationTest.ts:172-318 runAttestationTest:
// an uncommissioned pair whose controller uses store (nil: no store),
// the device commissioned through the registry under policy, the findings
// a Decide saw.
type attestationRun struct {
	err      error
	findings []attestation.Finding
	invoked  int
	device   *fabrictest.Device
	size     int
}

func runAttestation(t *testing.T, store attestation.PAAStore, policy attestation.Policy) *attestationRun {
	t.Helper()
	site := quietSite(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{Attestation: store, NoAttestationStore: store == nil}})
	run := &attestationRun{device: p.Device}
	var mu sync.Mutex
	if decide := policy.Decide; decide != nil {
		policy.Decide = func(f []attestation.Finding) (bool, string, error) {
			mu.Lock()
			run.findings, run.invoked = f, run.invoked+1
			mu.Unlock()
			return decide(f)
		}
	}
	found, err := p.Controller.Discover(parityCtx(t), discovery.ByLongDiscriminator(p.Device.Discriminator()), 90*time.Second)
	if err != nil || len(found) != 1 {
		t.Fatalf("discovered %+v, %v", found, err)
	}
	reg := p.Controller.Registry()
	n, err := reg.ForDevice(parityCtx(t), found[0])
	if err != nil {
		t.Fatal(err)
	}
	o := client.CommissionOptions{}
	o.Passcode, o.OnAttestationFailure = p.Device.Passcode(), policy
	if err := site.Run(parityCtx(t), func(ctx context.Context) error {
		run.err = reg.Commission(ctx, n, o)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if run.err == nil {
		// matter.js counts the commissioned peers; a failed attempt leaves
		// the discovered node, which the registry counts too.
		run.size = len(reg.Commissioned())
	}
	return run
}

// expectCommissioned is the matter.js assertResult of a success:
// the device commissioned, one peer.
func (r *attestationRun) expectCommissioned(t *testing.T) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("commission: %v", r.err)
	}
	f, err := r.device.Node().FabricInfos(parityCtx(t))
	if err != nil || len(f) != 1 || r.size != 1 {
		t.Fatalf("device fabrics %d (%v), commissioned peers %d; want 1 and 1", len(f), err, r.size)
	}
}

// expectRejected asserts the commission failed with a message matching re.
func (r *attestationRun) expectRejected(t *testing.T, re string) {
	t.Helper()
	if r.err == nil || !regexp.MustCompile(re).MatchString(r.err.Error()) {
		t.Fatalf("commission: %v, want an error matching %q", r.err, re)
	}
}

// has reports whether the findings carry check, and its level.
func (r *attestationRun) has(check attestation.Check) (attestation.Level, bool) {
	for _, f := range r.findings {
		if f.Check == check {
			return f.Level, true
		}
	}
	return "", false
}

var (
	accept = func([]attestation.Finding) (bool, string, error) { return true, "", nil }
	reject = func([]attestation.Finding) (bool, string, error) { return false, "", nil }
)

const rejectedByPolicy = `finding\(s\) and was rejected by policy`

func TestClientAttestationNoTrustStoreParity(t *testing.T) {
	t.Parallel()
	t.Run("commissions with default policy (undefined)", func(t *testing.T) {
		t.Parallel()
		runAttestation(t, nil, attestation.Policy{}).expectCommissioned(t)
	})
	t.Run("commissions with onAttestationFailure=true", func(t *testing.T) {
		t.Parallel()
		runAttestation(t, nil, attestation.Accept()).expectCommissioned(t)
	})
	t.Run("rejects with onAttestationFailure=false", func(t *testing.T) {
		t.Parallel()
		runAttestation(t, nil, attestation.Reject()).expectRejected(t, rejectedByPolicy)
	})
	t.Run("custom callback receives DclServiceUnavailable warning and other findings", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, nil, attestation.Policy{Decide: accept})
		r.expectCommissioned(t)
		if lvl, ok := r.has(attestation.DclServiceUnavailable); !ok || lvl != attestation.LevelWarning {
			t.Fatalf("findings %+v: want DclServiceUnavailable as a warning", r.findings)
		}
		if _, ok := r.has(attestation.CertificationTypeTest); !ok {
			t.Fatalf("findings %+v: want CertificationTypeTest", r.findings)
		}
	})
	t.Run("custom callback receives findings and rejects", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, nil, attestation.Policy{Decide: reject})
		r.expectRejected(t, rejectedByPolicy)
		if _, ok := r.has(attestation.DclServiceUnavailable); !ok {
			t.Fatalf("findings %+v: want DclServiceUnavailable", r.findings)
		}
	})
}

func TestClientAttestationTrustStoreParity(t *testing.T) {
	t.Parallel()
	correct := func() attestation.PAAStore { return attestationStore(true, secattestation.TestPAANoVIDCert) }
	testOnly := func() attestation.PAAStore { return attestationStore(false, secattestation.TestPAANoVIDCert) }
	wrong := func() attestation.PAAStore { return attestationStore(true, secattestation.TestPAAFFF1Cert) }

	t.Run("commissions with default policy", func(t *testing.T) {
		t.Parallel()
		runAttestation(t, correct(), attestation.Policy{}).expectCommissioned(t)
	})
	t.Run("callback receives CertificationTypeTest and other expected findings", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, correct(), attestation.Policy{Decide: accept})
		r.expectCommissioned(t)
		if lvl, ok := r.has(attestation.CertificationTypeTest); !ok || lvl != attestation.LevelInfo {
			t.Fatalf("findings %+v: want CertificationTypeTest as info", r.findings)
		}
		if _, ok := r.has(attestation.CdSignerVerificationSkipped); ok {
			t.Fatalf("findings %+v: the CD signer was not verified", r.findings)
		}
	})
	t.Run("rejects with onAttestationFailure=false despite valid chain", func(t *testing.T) {
		t.Parallel()
		runAttestation(t, correct(), attestation.Reject()).expectRejected(t, rejectedByPolicy)
	})
	t.Run("emits TrustedAsTestCertificate finding, callback rejects", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, testOnly(), attestation.Policy{Decide: reject})
		r.expectRejected(t, rejectedByPolicy)
		if lvl, ok := r.has(attestation.TrustedAsTestCertificate); !ok || lvl != attestation.LevelError {
			t.Fatalf("findings %+v: want TrustedAsTestCertificate as an error", r.findings)
		}
	})
	t.Run("emits TrustedAsTestCertificate finding, callback accepts → commissioning proceeds", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, testOnly(), attestation.Policy{Decide: accept})
		r.expectCommissioned(t)
		if _, ok := r.has(attestation.TrustedAsTestCertificate); !ok {
			t.Fatalf("findings %+v: want TrustedAsTestCertificate", r.findings)
		}
	})
	paaNotTrusted := func(t *testing.T, r *attestationRun) {
		t.Helper()
		if len(r.findings) != 1 || r.findings[0].Check != attestation.PaaNotTrusted || r.findings[0].Level != attestation.LevelError {
			t.Fatalf("findings %+v: want one PaaNotTrusted error", r.findings)
		}
	}
	t.Run("PaaNotTrusted error finding, callback rejects", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, wrong(), attestation.Policy{Decide: reject})
		r.expectRejected(t, `(?i)PAA`)
		paaNotTrusted(t, r)
	})
	t.Run("PaaNotTrusted error finding, callback accepts → commissioning proceeds", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, wrong(), attestation.Policy{Decide: accept})
		paaNotTrusted(t, r)
		r.expectCommissioned(t)
	})
}

// reasonError is the CommissioningError a reason returned by the callback
// makes, its Message the reason verbatim.
func reasonError(t *testing.T, r *attestationRun, reason string) *commissioner.CommissioningError {
	t.Helper()
	var ce *commissioner.CommissioningError
	if !errors.As(r.err, &ce) || ce.Message != reason {
		t.Fatalf("commission: %v, want a CommissioningError %q", r.err, reason)
	}
	return ce
}

// customRejection is ClientAttestationTest.ts:621-634 CustomRejection.
type customRejection struct{}

func (customRejection) Error() string { return "custom rejection" }

func TestClientAttestationCallbackContractParity(t *testing.T) {
	t.Parallel()
	correct := func() attestation.PAAStore { return attestationStore(true, secattestation.TestPAANoVIDCert) }
	wrong := func() attestation.PAAStore { return attestationStore(true, secattestation.TestPAAFFF1Cert) }
	reason := func(msg string) attestation.Policy {
		return attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return false, msg, nil }}
	}

	t.Run("string return on success-path findings wraps the synthesized error as cause", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, correct(), reason("operator declined: test attestation"))
		ce := reasonError(t, r, "operator declined: test attestation")
		var cause *commissioner.CommissioningError
		if !errors.As(ce.Cause, &cause) || !regexp.MustCompile(rejectedByPolicy).MatchString(cause.Message) {
			t.Fatalf("cause %v, want the synthesized rejection", ce.Cause)
		}
	})
	t.Run("string return on hard-error finding wraps the original DeviceAttestationError as cause", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, wrong(), reason("operator declined: untrusted PAA"))
		ce := reasonError(t, r, "operator declined: untrusted PAA")
		var ae *attestation.Error
		if !errors.As(ce.Cause, &ae) || ae.Check != attestation.PaaNotTrusted {
			t.Fatalf("cause %v: want the PaaNotTrusted attestation error", ce.Cause)
		}
	})
	t.Run("callback throw propagates verbatim (no wrapping)", func(t *testing.T) {
		t.Parallel()
		r := runAttestation(t, wrong(), attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return false, "", customRejection{} }})
		if r.err == nil || r.err.Error() != "custom rejection" || errors.Unwrap(r.err) != nil {
			t.Fatalf("commission: %v, want the callback's error verbatim", r.err)
		}
	})
	fromCallback := func(store attestation.PAAStore) func(t *testing.T) {
		return func(t *testing.T) {
			t.Helper()
			t.Parallel()
			r := runAttestation(t, store, attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) {
				return false, "", &attestation.Error{Check: attestation.AttestationSignatureInvalid, Message: "from callback"}
			}})
			var ae *attestation.Error
			if !errors.As(r.err, &ae) || ae.Check != attestation.AttestationSignatureInvalid || ae.Message != "from callback" || r.invoked != 1 {
				t.Fatalf("commission: %v after %d invocations, want the callback's attestation error once", r.err, r.invoked)
			}
		}
	}
	t.Run("callback throwing DeviceAttestationError on success-path findings propagates without re-invocation", fromCallback(correct()))
	t.Run("callback throwing DeviceAttestationError on hard-error finding propagates without re-invocation", fromCallback(wrong()))
}
