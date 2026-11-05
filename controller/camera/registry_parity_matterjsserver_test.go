// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"reflect"
	"slices"
	"testing"
)

// The cases of matterjs-server's
// packages/ws-controller/test/SessionRegistryClientKeyTest.ts:34-286 at
// 0bd06f2, ported by meaning: a promise the test awaits is a wait the test
// calls, and new Set(ids) is a map of ids.

const (
	regNode          = uint64(5)
	regEndpoint      = uint16(1)
	regOtherNode     = uint64(6)
	regOtherEndpoint = uint16(2)
)

// sessionOf is the fixture's sessionOf (SessionRegistryClientKeyTest.ts:16-32,
// matterjs-server at 0bd06f2).
func sessionOf(id uint16, connectionID, key string, node uint64, endpoint uint16) managedSession {
	return managedSession{webRTCSessionID: id, nodeID: node, endpointID: endpoint, connectionID: connectionID, clientSessionKey: key}
}

func quietRegistry() *sessionRegistry {
	return newSessionRegistry(func(cameraSessionEnded) {}, func(managedSession, sessionEndCause) {})
}

func ids(set ...uint16) map[uint16]bool {
	m := make(map[uint16]bool, len(set))
	for _, id := range set {
		m[id] = true
	}
	return m
}

// TestSessionRegistryClientKeysParityMatterjsServer is the top-level cases of
// describe("CameraSessionRegistry client session keys")
// (SessionRegistryClientKeyTest.ts:35-147, matterjs-server at 0bd06f2).
func TestSessionRegistryClientKeysParityMatterjsServer(t *testing.T) {
	ctx := context.Background()
	t.Run("finds the tracked entry, not its registration, between track and finish", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
		r.establishing(p, 42)
		session := sessionOf(42, "conn-1", "key-a", regNode, regEndpoint)
		if got := r.track(p, session); got != trackAccepted {
			t.Fatalf("track = %v", got)
		}
		for _, when := range []string{"before finish", "after finish"} {
			got, ok := r.byClientKey(regNode, regEndpoint, "conn-1", "key-a")
			if !ok || got.pending != nil || got.session == nil || !reflect.DeepEqual(*got.session, session) {
				t.Errorf("%s: byClientKey = %+v, %v; want the tracked session", when, got, ok)
			}
			r.finish(p)
		}
	})
	t.Run("keeps a release claim when the owner also stops the start", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
		claimed := r.claim(func(sessionScope) bool { return true })
		stopped := r.cancel(p)
		if got := r.claimRefusal(p); got != refusalClaimed {
			t.Errorf("claimRefusal = %v, want claimed", got)
		}
		r.finish(p)
		for _, c := range claimed.starting {
			<-c
		}
		if got, err := stopped(ctx); got || err != nil {
			t.Errorf("cancel = %v, %v; want false", got, err)
		}
	})
	t.Run("answers with the EndSession outcome of the cancelled start once it finished", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
		stopped := r.cancel(p)
		r.establishing(p, 42)
		if got := r.track(p, sessionOf(42, "conn-1", "key-a", regNode, regEndpoint)); got != refusalStopped {
			t.Errorf("track = %v, want stopped", got)
		}
		r.ending(p, resolvedOutcome(true, nil))
		r.finish(p)
		if got, err := stopped(ctx); !got || err != nil {
			t.Errorf("cancel = %v, %v; want true", got, err)
		}
		if r.clientSessionKeyInUse("conn-1", "key-a") {
			t.Error("the key is still in use")
		}
	})
	t.Run("answers false for a cancelled start that ended no session", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
		stopped := r.cancel(p)
		r.establishing(p, 42)
		r.finish(p)
		if got, err := stopped(ctx); got || err != nil {
			t.Errorf("cancel = %v, %v; want false", got, err)
		}
	})
	t.Run("announces a reissued id's old entry with the old key", func(t *testing.T) {
		var announced []cameraSessionEnded
		r := newSessionRegistry(func(e cameraSessionEnded) { announced = append(announced, e) }, func(managedSession, sessionEndCause) {})
		old := r.begin(regNode, regEndpoint, "conn-1", "key-old")
		r.establishing(old, 42)
		r.track(old, sessionOf(42, "conn-1", "key-old", regNode, regEndpoint))
		r.finish(old)

		next := r.begin(regNode, regEndpoint, "conn-1", "key-new")
		r.establishing(next, 42)

		if len(announced) != 1 || announced[0].cause != endCameraRestarted || announced[0].clientSessionKey != "key-old" {
			t.Errorf("announced = %+v, want one camera_restarted of key-old", announced)
		}
		if got, ok := r.signallingOwner(regNode, regEndpoint, 42); !ok || got != (sessionOwner{connectionID: "conn-1", clientSessionKey: "key-new"}) {
			t.Errorf("signallingOwner = %+v, %v", got, ok)
		}
	})
	t.Run("finds a key only on the node and endpoint it was used on", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
		if got, ok := r.byClientKey(regNode, regEndpoint, "conn-1", "key-a"); !ok || got.pending != p {
			t.Errorf("byClientKey = %+v, %v; want the registration", got, ok)
		}
		checkOtherScopes := func() {
			if _, ok := r.byClientKey(regOtherNode, regEndpoint, "conn-1", "key-a"); ok {
				t.Error("found on another node")
			}
			if _, ok := r.byClientKey(regNode, regOtherEndpoint, "conn-1", "key-a"); ok {
				t.Error("found on another endpoint")
			}
		}
		checkOtherScopes()
		r.establishing(p, 42)
		session := sessionOf(42, "conn-1", "key-a", regNode, regEndpoint)
		r.track(p, session)
		if got, ok := r.byClientKey(regNode, regEndpoint, "conn-1", "key-a"); !ok || got.session == nil || !reflect.DeepEqual(*got.session, session) {
			t.Errorf("byClientKey = %+v, %v; want the session", got, ok)
		}
		checkOtherScopes()
	})
	t.Run("refuses a claim check for a registration that is already finished", func(t *testing.T) {
		r := quietRegistry()
		p := r.begin(regNode, regEndpoint, "conn-1", "")
		if got := r.claimRefusal(p); got != trackAccepted {
			t.Errorf("claimRefusal = %v, want none", got)
		}
		r.finish(p)
		if got := r.claimRefusal(p); got != refusalClaimed {
			t.Errorf("claimRefusal = %v, want claimed", got)
		}
	})
}

// TestSessionRegistryIssuedParityMatterjsServer is describe("an id issued to
// another route") (SessionRegistryClientKeyTest.ts:149-206, matterjs-server at
// 0bd06f2).
func TestSessionRegistryIssuedParityMatterjsServer(t *testing.T) {
	t.Run("supersedes the registration holding the id on the same node and endpoint", func(t *testing.T) {
		r := quietRegistry()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.issued(regNode, regEndpoint, 42)
		if _, ok := r.signallingOwner(regNode, regEndpoint, 42); ok {
			t.Error("a superseded registration still owns the signalling")
		}
		if got := r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalSuperseded {
			t.Errorf("track = %v, want superseded", got)
		}
	})
	t.Run("keeps a superseded registration superseded when it is also claimed, ended by peer or report, or its node removed", func(t *testing.T) {
		r := quietRegistry()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.issued(regNode, regEndpoint, 42)
		r.peerEnded(regNode, regEndpoint, 42, 0, false)
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		r.forgetNode(regNode)
		claimed := r.claim(func(sessionScope) bool { return true })
		if got := r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalSuperseded {
			t.Errorf("track = %v, want superseded", got)
		}
		r.finish(waiting)
		for _, c := range claimed.starting {
			<-c
		}
	})
	for _, c := range []struct {
		name     string
		node     uint64
		endpoint uint16
	}{
		{"leaves a registration on another endpoint holding the same id", regNode, regOtherEndpoint},
		{"leaves a registration on another node holding the same id", regOtherNode, regEndpoint},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := quietRegistry()
			waiting := r.begin(c.node, c.endpoint, "conn-1", "")
			r.establishing(waiting, 42)
			r.issued(regNode, regEndpoint, 42)
			if got := r.track(waiting, sessionOf(42, "conn-1", "", c.node, c.endpoint)); got != trackAccepted {
				t.Errorf("track = %v, want accepted", got)
			}
		})
	}
}

// TestSessionRegistryReportDuringEstablishmentParityMatterjsServer is
// describe("a CurrentSessions report during establishment")
// (SessionRegistryClientKeyTest.ts:208-267, matterjs-server at 0bd06f2).
func TestSessionRegistryReportDuringEstablishmentParityMatterjsServer(t *testing.T) {
	vanishing := func() (*sessionRegistry, *[]uint16) {
		var vanished []uint16
		r := newSessionRegistry(func(cameraSessionEnded) {}, func(s managedSession, _ sessionEndCause) {
			vanished = append(vanished, s.webRTCSessionID)
		})
		return r, &vanished
	}
	t.Run("counts a listing between establishing and track, so a later report without the id ends it", func(t *testing.T) {
		r, vanished := vanishing()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.reconcile(regNode, regEndpoint, ids(42), endCameraDropped)
		r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint))
		if len(*vanished) != 0 {
			t.Fatalf("vanished = %v before the report without the id", *vanished)
		}
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		if !slices.Equal(*vanished, []uint16{42}) {
			t.Errorf("vanished = %v, want [42]", *vanished)
		}
	})
	t.Run("ignores a listing on another endpoint or node", func(t *testing.T) {
		r, vanished := vanishing()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.reconcile(regNode, regOtherEndpoint, ids(42), endCameraDropped)
		r.reconcile(regOtherNode, regEndpoint, ids(42), endCameraDropped)
		r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint))
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		if len(*vanished) != 0 {
			t.Errorf("vanished = %v, want none", *vanished)
		}
	})
	t.Run("ignores a listing before the camera gave the registration its id", func(t *testing.T) {
		r, vanished := vanishing()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.reconcile(regNode, regEndpoint, ids(42), endCameraDropped)
		r.establishing(waiting, 42)
		r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint))
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		if len(*vanished) != 0 {
			t.Errorf("vanished = %v, want none", *vanished)
		}
	})
	t.Run("refuses to track a registration a report listed and a later one no longer did", func(t *testing.T) {
		r, _ := vanishing()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.reconcile(regNode, regEndpoint, ids(42), endCameraDropped)
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		if got := r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalAlreadyEnded {
			t.Errorf("track = %v, want already_ended", got)
		}
		if _, ok := r.get(regNode, regEndpoint, 42); ok {
			t.Error("the ended registration is tracked")
		}
	})
	t.Run("keeps superseded ahead of an end a report recorded", func(t *testing.T) {
		r, _ := vanishing()
		waiting := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(waiting, 42)
		r.reconcile(regNode, regEndpoint, ids(42), endCameraDropped)
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		r.issued(regNode, regEndpoint, 42)
		if got := r.track(waiting, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalSuperseded {
			t.Errorf("track = %v, want superseded", got)
		}
	})
}

// TestSessionRegistryForgetNodeParityMatterjsServer is describe("forgetNode")
// (SessionRegistryClientKeyTest.ts:269-286, matterjs-server at 0bd06f2).
func TestSessionRegistryForgetNodeParityMatterjsServer(t *testing.T) {
	t.Run("ends the registrations of the removed node only", func(t *testing.T) {
		r := quietRegistry()
		removed := r.begin(regNode, regEndpoint, "conn-1", "")
		r.establishing(removed, 42)
		kept := r.begin(regOtherNode, regEndpoint, "conn-1", "")
		r.establishing(kept, 43)

		r.forgetNode(regNode)

		if got := r.track(removed, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalAlreadyEnded {
			t.Errorf("removed: track = %v, want already_ended", got)
		}
		if got := r.track(kept, sessionOf(43, "conn-1", "", regOtherNode, regEndpoint)); got != trackAccepted {
			t.Errorf("kept: track = %v, want accepted", got)
		}
	})
}
