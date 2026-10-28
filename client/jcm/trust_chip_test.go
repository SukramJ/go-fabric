// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The trust verification's types: a port of connectedhomeip
// src/credentials/tests/jcm/TestTrustVerification.cpp at 6170af84 (3
// TEST_F), by meaning.
package jcm_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
)

// TestEnumToStringError is :55-69: each result's name.
func TestEnumToStringError(t *testing.T) {
	for r, want := range map[jcm.Result]string{
		jcm.Success:                         "SUCCESS",
		jcm.Async:                           "ASYNC_OPERATION",
		jcm.InvalidAdministratorEndpointID:  "INVALID_ADMINISTRATOR_ENDPOINT_ID",
		jcm.InvalidAdministratorFabricIndex: "INVALID_ADMINISTRATOR_FABRIC_INDEX",
		jcm.InvalidAdministratorCAT:         "INVALID_ADMINISTRATOR_CAT",
		jcm.TrustVerificationDelegateNotSet: "TRUST_VERIFICATION_DELEGATE_NOT_SET",
		jcm.UserDeniedConsent:               "USER_DENIED_CONSENT",
		jcm.VendorIDVerificationFailed:      "VENDOR_ID_VERIFICATION_FAILED",
		jcm.InternalError:                   "INTERNAL_ERROR",
	} {
		if got := r.String(); got != want {
			t.Errorf("Result(%d) = %q, want %q", r, got, want)
		}
	}
}

// TestEnumToStringStage is :71-79: each stage's name.
func TestEnumToStringStage(t *testing.T) {
	for s, want := range map[jcm.Stage]string{
		jcm.StageIdle:                              "IDLE",
		jcm.StageVerifyingAdministratorInformation: "VERIFYING_ADMINISTRATOR_INFORMATION",
		jcm.StagePerformingVendorIDVerification:    "PERFORMING_VENDOR_ID_VERIFICATION_PROCEDURE",
		jcm.StageAskingUserForConsent:              "ASKING_USER_FOR_CONSENT",
		jcm.StageComplete:                          "COMPLETE",
		jcm.StageError:                             "ERROR",
	} {
		if got := s.String(); got != want {
			t.Errorf("Stage(%d) = %q, want %q", s, got, want)
		}
	}
}

// TestTrustVerificationInfoCleanup is :81-115: Cleanup restores the
// defaults and drops every buffer.
func TestTrustVerificationInfoCleanup(t *testing.T) {
	info := jcm.Info{
		AdminEndpointID: 5, AdminFabricIndex: 7, AdminVendorID: 0xFFF1, AdminFabricID: 0xdeadbeef,
		RootPublicKey: make([]byte, 16), AdminRCAC: make([]byte, 8), AdminICAC: make([]byte, 12), AdminNOC: make([]byte, 20),
	}
	info.Cleanup()
	if info.AdminEndpointID != jcm.InvalidEndpointID || info.AdminFabricIndex != 0 || info.AdminVendorID != 0 || info.AdminFabricID != 0 {
		t.Errorf("Cleanup kept identifiers: %+v", info)
	}
	if len(info.RootPublicKey)+len(info.AdminRCAC)+len(info.AdminICAC)+len(info.AdminNOC) != 0 {
		t.Errorf("Cleanup kept buffers: %+v", info)
	}
}
