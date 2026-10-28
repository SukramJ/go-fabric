// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
)

// TestMachineRules: TrustVerification.cpp:45-76 — Progress hears every
// finished stage, a failure completes with it, a next stage of Error
// completes with InternalError, a Finished after the end is ignored.
func TestMachineRules(t *testing.T) {
	order := []jcm.Stage{jcm.StageStoringEndpointID, jcm.StageComplete}
	next := func(s jcm.Stage) jcm.Stage {
		for i, o := range order {
			if o == s && i+1 < len(order) {
				return order[i+1]
			}
		}
		if s == jcm.StageIdle {
			return order[0]
		}
		return jcm.StageError
	}
	t.Run("success", func(t *testing.T) {
		var progress []jcm.Stage
		var done []jcm.Result
		m := &jcm.Machine{Next: next, Progress: func(s jcm.Stage, _ jcm.Result) { progress = append(progress, s) }, Complete: func(r jcm.Result) { done = append(done, r) }}
		m.Perform = func(s jcm.Stage) { m.Run(s, func() jcm.Result { return jcm.Success }) }
		m.Start()
		if len(progress) != 2 || progress[1] != jcm.StageComplete || len(done) != 1 || done[0] != jcm.Success {
			t.Fatalf("progress %v, done %v", progress, done)
		}
		m.Finished(jcm.StageComplete, jcm.InternalError)
		if len(done) != 1 {
			t.Fatalf("a Finished after the end completed again: %v", done)
		}
	})
	t.Run("failure", func(t *testing.T) {
		var done []jcm.Result
		m := &jcm.Machine{Next: next, Complete: func(r jcm.Result) { done = append(done, r) }}
		m.Perform = func(s jcm.Stage) { m.Run(s, func() jcm.Result { return jcm.InvalidAdministratorEndpointID }) }
		m.Start()
		if len(done) != 1 || done[0] != jcm.InvalidAdministratorEndpointID {
			t.Fatalf("done %v", done)
		}
	})
	t.Run("next is Error", func(t *testing.T) {
		var done []jcm.Result
		m := &jcm.Machine{Next: func(jcm.Stage) jcm.Stage { return jcm.StageStoringEndpointID }, Complete: func(r jcm.Result) { done = append(done, r) }}
		m.Next = func(s jcm.Stage) jcm.Stage {
			if s == jcm.StageIdle {
				return jcm.StageStoringEndpointID
			}
			return jcm.StageError
		}
		m.Perform = func(s jcm.Stage) { m.Run(s, func() jcm.Result { return jcm.Success }) }
		m.Start()
		if len(done) != 1 || done[0] != jcm.InternalError {
			t.Fatalf("done %v", done)
		}
	})
	t.Run("async", func(t *testing.T) {
		var done []jcm.Result
		m := &jcm.Machine{Next: next, Complete: func(r jcm.Result) { done = append(done, r) }}
		m.Perform = func(s jcm.Stage) { m.Run(s, func() jcm.Result { return jcm.Async }) }
		m.Start()
		if len(done) != 0 {
			t.Fatalf("an Async stage completed: %v", done)
		}
		m.Finished(jcm.StageStoringEndpointID, jcm.UserDeniedConsent)
		if len(done) != 1 || done[0] != jcm.UserDeniedConsent {
			t.Fatalf("done %v", done)
		}
	})
	if f := (jcm.Failure{Stage: jcm.StageAskingUserForConsent, Result: jcm.UserDeniedConsent}); f.Error() != "jcm: trust verification failed at ASKING_USER_FOR_CONSENT: USER_DENIED_CONSENT" {
		t.Errorf("Failure = %q", f.Error())
	}
}
