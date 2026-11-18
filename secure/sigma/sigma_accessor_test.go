// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"testing"
)

// TestResponder_PeerSessionID_BeforeSigma1 verifies that PeerSessionID
// returns 0 before any Sigma1 has been processed.
func TestResponder_PeerSessionID_BeforeSigma1(t *testing.T) {
	t.Parallel()
	r := &Responder{}
	if got := r.PeerSessionID(); got != 0 {
		t.Errorf("PeerSessionID before Sigma1 = %d, want 0", got)
	}
}

// TestResponder_PeerNodeID_BeforeSigma1 verifies that PeerNodeID
// returns 0 before any Sigma1 has been processed.
func TestResponder_PeerNodeID_BeforeSigma1(t *testing.T) {
	t.Parallel()
	r := &Responder{}
	if got := r.PeerNodeID(); got != 0 {
		t.Errorf("PeerNodeID before Sigma1 = %d, want 0", got)
	}
}

// TestResponder_SetSessionParameters_Nil verifies that nil detaches the
// parameters a responder held: SessionParameters returns nil afterwards
// (R2-P5-tassert-05, R2-P6-test-3107).
func TestResponder_SetSessionParameters_Nil(t *testing.T) {
	t.Parallel()
	r := &Responder{}
	r.SetSessionParameters(&SessionParameters{SessionIdleInterval: 1000})
	r.SetSessionParameters(nil)
	if got := r.SessionParameters(); got != nil {
		t.Fatalf("SessionParameters after SetSessionParameters(nil) = %+v, want nil", got)
	}
}

// TestResponder_SetSessionParameters_NonNil verifies that a non-nil
// SessionParameters is stored: SessionParameters returns the values set.
// That Sigma2 carries them as field 5 is held by
// secure/case_server_parity_test.go (R2-P5-tassert-05, R2-P6-test-3107).
func TestResponder_SetSessionParameters_NonNil(t *testing.T) {
	t.Parallel()
	r := &Responder{}
	if got := r.SessionParameters(); got != nil {
		t.Fatalf("SessionParameters on a new responder = %+v, want nil", got)
	}
	params := SessionParameters{
		SessionIdleInterval:    1000,
		SessionActiveInterval:  200,
		SessionActiveThreshold: 4000,
	}
	r.SetSessionParameters(&params)
	got := r.SessionParameters()
	if got == nil || *got != params {
		t.Fatalf("SessionParameters = %+v, want %+v", got, params)
	}
}

// TestInitiator_SessionKeys_BeforeComplete verifies that SessionKeys
// returns ok=false before Sigma2/Sigma3 have been processed.
func TestInitiator_SessionKeys_BeforeComplete(t *testing.T) {
	t.Parallel()
	ini := &Initiator{}
	_, ok := ini.SessionKeys()
	if ok {
		t.Error("expected ok=false from Initiator.SessionKeys before complete")
	}
}

// TestResponder_SessionIdentity_BeforeSigma1 verifies that SessionIdentity
// returns ok=false before any Sigma1 has been processed.
func TestResponder_SessionIdentity_BeforeSigma1(t *testing.T) {
	t.Parallel()
	r := &Responder{}
	_, _, ok := r.SessionIdentity()
	if ok {
		t.Error("SessionIdentity returned ok=true before Sigma1")
	}
}
