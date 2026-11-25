// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/camera"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
)

// TestCameraSimAnswerSurvivesASlowLaterListener (PR 1 review M1, decision
// 682): the Answer goroutine of answerOffer waits for its session through
// a change listener and Provider.Sessions(). A change listener registered
// after its own that does not return until the Answer was sent — the
// widest form of the window between the notification and the table the
// goroutine reads — must not cost the Answer: the table shows the session
// before any listener is told. Before the fix the goroutine read the old
// table, consumed the only wake-up and sent nothing (the CI flake of
// TestCameraSimAnswersOfferAndEndsOnPrivacy).
func TestCameraSimAnswerSurvivesASlowLaterListener(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	provider := c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider)
	signals := &simSignals{}
	provider.AttachPeerConnector(signals)
	video, audio := simStreams(t, c)
	id := simProvideOffer(t, c, video, audio)
	waitFor(t, "the first Answer", func() bool { a, _ := signals.sent(); return len(a) > 0 })
	i := slices.IndexFunc(provider.Sessions(), func(s camera.ProviderSession) bool { return s.Id == id })
	if i < 0 {
		t.Fatalf("session %d not in CurrentSessions after its Answer", id)
	}
	s := provider.Sessions()[i]
	s.Id = id + 1
	answered := func() bool {
		a, _ := signals.sent()
		return slices.ContainsFunc(a, func(r wrdef.AnswerRequest) bool { return r.WebRtcSessionId == s.Id })
	}
	c.answerOffer(context.Background(), s.Id, simTHOffer)
	unsubscribe := provider.OnAttributeChange(func(contract.Change) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for !answered() && ctx.Err() == nil {
			runtime.Gosched()
		}
	})
	defer unsubscribe()
	if err := provider.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if !answered() {
		t.Fatalf("no Answer for session %d while a later change listener held the setter: the goroutine read CurrentSessions before it showed the session", s.Id)
	}
}
