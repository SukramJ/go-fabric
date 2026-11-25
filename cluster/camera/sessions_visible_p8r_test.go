// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/contract"
)

// sessionSeenP8R drives the shape of the reference daemon's Answer wait
// (examples/reference-bridge/camera_webrtc.go answerOffer): a waiter woken
// through a one-slot channel by a change listener re-reads the session
// table and waits again while the session is missing. A second listener,
// registered after the waiter's, holds the change's setter until the
// waiter saw the session, bounded by two seconds. Reports whether the
// waiter saw it: false when the listeners ran before the table showed the
// change — the waiter consumed the only wake-up and read the old table
// (PR 1 review M1, decision 682).
func sessionSeenP8R(t *testing.T, onChange func(func(contract.Change)) func(), has func() bool, change func() error) bool {
	t.Helper()
	changed := make(chan struct{}, 1)
	unsubWaiter := onChange(func(contract.Change) {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer unsubWaiter()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	seen := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !has() {
			select {
			case <-changed:
			case <-ctx.Done():
				return
			}
		}
		close(seen)
	}()
	unsubHold := onChange(func(contract.Change) {
		select {
		case <-seen:
		case <-ctx.Done():
		}
	})
	defer unsubHold()
	if err := change(); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	select {
	case <-seen:
		return true
	default:
		return false
	}
}

// TestProviderSessionsShowTheChangeBeforeListenersRun: a listener of a
// CurrentSessions change, and a goroutine it wakes, read the new table
// through Sessions() — stored, visible, then told.
func TestProviderSessionsShowTheChangeBeforeListenersRun(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	video := []uint16{1}
	s := camera.ProviderSession{Id: 5, PeerNodeId: 7, FabricIndex: 1, VideoStreams: &video}
	has := func() bool {
		return slices.ContainsFunc(p.Sessions(), func(x camera.ProviderSession) bool { return x.Id == s.Id })
	}
	if !sessionSeenP8R(t, p.OnAttributeChange, has, func() error { return p.UpsertSession(s) }) {
		t.Fatal("the woken waiter read CurrentSessions without the upserted session: listeners ran before Sessions() showed the change")
	}
}

// TestRequestorSessionsShowTheChangeBeforeListenersRun: as the provider's,
// for the requestor's table.
func TestRequestorSessionsShowTheChangeBeforeListenersRun(t *testing.T) {
	t.Parallel()
	r := newRequestor(t, &recorder{})
	s := makeSession(withID(5))
	has := func() bool {
		return slices.ContainsFunc(r.Sessions(), func(x camera.WebRTCSession) bool { return x.Id == s.Id })
	}
	if !sessionSeenP8R(t, r.OnAttributeChange, has, func() error { return r.UpsertSession(s) }) {
		t.Fatal("the woken waiter read CurrentSessions without the upserted session: listeners ran before Sessions() showed the change")
	}
}
