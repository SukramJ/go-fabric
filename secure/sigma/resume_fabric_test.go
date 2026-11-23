// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import "testing"

// TestSigma_Resume_RecordOfAnotherFabricWithoutFabricLookupIsAMiss: when
// the identity is not resolved by the record's fabric index — no
// resolver, or one without [FabricIndexResolver] — a record whose
// FabricIndex is set and differs from the baseline identity's is a miss
// (Full Sigma): resuming it would report the baseline's fabric for the
// other fabric's peer. R2-C2-sec-0101.
func TestSigma_Resume_RecordOfAnotherFabricWithoutFabricLookupIsAMiss(t *testing.T) {
	const baseline uint8 = 1
	for _, resolver := range []string{"none", "identity-only"} {
		for _, tc := range []struct {
			name       string
			record     uint8
			wantResume bool
		}{
			{"same fabric", baseline, true},
			{"record without fabric index", 0, true},
			{"another fabric", 2, false},
		} {
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
}
