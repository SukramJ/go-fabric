// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import "testing"

// TestSigma_Resume_RecordOfAnotherFabricWithoutFabricLookupIsAMiss: when
// the identity is not resolved by the record's fabric index — no
// resolver, or one without [FabricIndexResolver] — a record whose
// FabricIndex is set and differs from the baseline identity's is a miss
// (Full Sigma): resuming it would report the baseline's fabric for the
// other fabric's peer. R2-C2-sec-0101. A stamped record under an
// unstamped baseline (fabric index 0) is a miss too — the baseline's
// caller would put the record's peer onto its default fabric with no proof
// of fabric (decision 517), and so is one under a nil baseline; an
// unstamped record stays comparable with any baseline.
func TestSigma_Resume_RecordOfAnotherFabricWithoutFabricLookupIsAMiss(t *testing.T) {
	for _, resolver := range []string{"none", "identity-only"} {
		for _, tc := range []struct {
			name       string
			baseline   uint8
			record     uint8
			wantResume bool
		}{
			{"same fabric", 1, 1, true},
			{"record without fabric index", 1, 0, true},
			{"another fabric", 1, 2, false},
			{"unstamped baseline, record without fabric index", 0, 0, true},
			{"unstamped baseline, stamped record", 0, 1, false},
		} {
			baseline := tc.baseline
			t.Run(resolver+"/"+tc.name, func(t *testing.T) {
				sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
				store := newStubStore()
				store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid, FabricIndex: tc.record})

				respID := newTestIdentity(t, 0xBBBB, 1, fabricIPK())
				respID.FabricIndex = baseline
				v := testVerifier{}
				responder := NewResponder(respID, v, 0x2001)
				responder.SetResumptionStore(store)
				if resolver == "identity-only" {
					responder.SetIdentityResolver(stubIdentityOnlyResolver{identity: respID, verifier: v})
				}

				result, err := responder.ProcessSigma1WithResume(sigma1Bytes)
				if err != nil {
					t.Fatalf("ProcessSigma1WithResume: %v", err)
				}
				if result.IsResume() != tc.wantResume {
					t.Fatalf("resumed=%v, want %v", result.IsResume(), tc.wantResume)
				}
				if !tc.wantResume && result.Sigma2 == nil {
					t.Fatal("the miss must fall through to Full Sigma (Sigma2)")
				}
				if tc.wantResume {
					if fi, _, ok := responder.SessionIdentity(); !ok || fi != baseline {
						t.Fatalf("SessionIdentity() fabricIndex=%d ok=%v, want %d", fi, ok, baseline)
					}
				}
			})
		}
	}

	// A nil baseline (NewResponder(nil, …)) with an identity-only resolver
	// is a working configuration — the full path takes the identity from
	// the resolver — but proves no fabric for a stamped record: resuming it
	// would commit a nil identity and SessionIdentity would report none, so
	// its caller keeps its default fabric (decision 517). A miss.
	t.Run("identity-only/nil baseline, stamped record", func(t *testing.T) {
		sigma1Bytes, sharedSecret, rid := buildValidSigma1WithResume(t)
		store := newStubStore()
		store.put(&ResumptionRecord{SharedSecret: sharedSecret, ResumptionID: rid, FabricIndex: 1})

		respID := newTestIdentity(t, 0xBBBB, 1, fabricIPK())
		respID.FabricIndex = 1
		v := testVerifier{}
		responder := NewResponder(nil, v, 0x2001)
		responder.SetResumptionStore(store)
		responder.SetIdentityResolver(stubIdentityOnlyResolver{identity: respID, verifier: v})

		result, err := responder.ProcessSigma1WithResume(sigma1Bytes)
		if err != nil {
			t.Fatalf("ProcessSigma1WithResume: %v", err)
		}
		if result.IsResume() {
			t.Fatal("a stamped record must not resume under a nil baseline")
		}
		if result.Sigma2 == nil {
			t.Fatal("the miss must fall through to Full Sigma (Sigma2)")
		}
	})
}
