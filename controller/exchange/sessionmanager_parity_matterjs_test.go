// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange_test

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts at
// 862ad75f: the local session parameters a manager is built with and their
// merge (describes "active threshold validation", "max paths per invoke
// validation", "session parameter setter"). The session table cases live
// in secure/operational, the group counter cases in groups.

import (
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

type sessionManagerConn struct{}

func (sessionManagerConn) Send(*net.UDPAddr, []byte) error { return nil }

func sessionManagerWith(t *testing.T, p sessionparams.Parameters) *exchange.Manager {
	t.Helper()
	m, err := exchange.NewManager(sessionManagerConn{}, exchange.Config{LocalParameters: &p})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestSessionManagerActiveThresholdValidation(t *testing.T) {
	t.Parallel()
	t.Run("accepts a local active threshold of exactly 65535 milliseconds", func(t *testing.T) {
		t.Parallel()
		m := sessionManagerWith(t, sessionparams.Parameters{SessionActiveThreshold: 65535})
		if got := m.LocalParameters().SessionActiveThreshold; got != 65535 {
			t.Fatalf("SessionActiveThreshold = %d, want 65535", got)
		}
	})
}

func TestSessionManagerMaxPathsPerInvokeValidation(t *testing.T) {
	t.Parallel()
	t.Run("accepts a local max paths per invoke of one", func(t *testing.T) {
		t.Parallel()
		m := sessionManagerWith(t, sessionparams.Parameters{MaxPathsPerInvoke: 1})
		if got := m.LocalParameters().MaxPathsPerInvoke; got != 1 {
			t.Fatalf("MaxPathsPerInvoke = %d, want 1", got)
		}
	})
}

// The Go manager has no parameter setter; exchange.Overlay is the merge the
// setter performs (SessionManager.ts:368-380), with a zero field the
// "undefined" a Go Parameters cannot otherwise express.
func TestSessionManagerSessionParameterSetter(t *testing.T) {
	t.Parallel()
	t.Run("ignores undefined values and retains current values", func(t *testing.T) {
		t.Parallel()
		current := sessionManagerWith(t, sessionparams.Parameters{SessionIdleInterval: 1000, SessionActiveInterval: 700}).LocalParameters()
		got := exchange.Overlay(current, &sessionparams.Parameters{SessionActiveInterval: 600})
		if got.SessionIdleInterval != 1000 || got.SessionActiveInterval != 600 {
			t.Fatalf("idle/active = %d/%d, want 1000/600", got.SessionIdleInterval, got.SessionActiveInterval)
		}
	})
}
