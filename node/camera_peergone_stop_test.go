// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// R5-conc-5301: the peer-gone keeper is a goroutine Stop waits for. Its
// wait for a camera provider's command serialisation ends with the node,
// and once Stop began no lost requestor's session ends any more — not a
// pair already dequeued, not one whose wait a port call held, not one
// whose last session Stop itself closed before the node's life ended.

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestCameraStopEndsThePeerGoneWait: a provider port call does not
// return while a lost peer's end waits for the provider. Stop returns
// without the port call, and nothing ends after the call is released.
func TestCameraStopEndsThePeerGoneWait(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.port.hold, g.port.entered, g.port.release = true, make(chan struct{}), make(chan struct{})
	var release sync.Once
	releasePort := func() { release.Do(func() { close(g.port.release) }) }
	t.Cleanup(releasePort)
	answered := make(chan error, 1)
	go func() {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, pgFabric), pgPeerB, nil)
		_, err := g.prov.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: 2, Sdp: "a"})
		answered <- err
	}()
	<-g.port.entered
	queue := make(chan string, 1)
	g.n.sec.peerGone.testBeforeEnd = func(addr exchange.PeerAddress) {
		if addr == pgAddrA {
			queue <- goroutineHeader()
		}
	}
	g.openBridge(t, pgPeerA)
	g.lose(pgPeerA)
	keeper := <-queue
	waitGoroutineBlockedIn(t, keeper, "camera.(*Provider).EndLostPeer")

	stopHeader := make(chan string, 1)
	stopped := make(chan error, 1)
	go func() {
		stopHeader <- goroutineHeader()
		stopped <- g.n.Stop(context.Background())
	}()
	stopper := <-stopHeader
	buf := make([]byte, 1<<20)
	done := false
	for range 1_000_000 {
		select {
		case err := <-stopped:
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			done = true
		default:
		}
		if done {
			break
		}
		// Stop parked in its wait for the keepers while the keeper stays
		// parked in PeerGone: only the port call's return would end that.
		if parkedIn(string(buf[:runtime.Stack(buf, true)]), stopper, keeper) {
			t.Fatal("Stop waits for the peer-gone keeper, which waits for the provider behind a held port call")
		}
		runtime.Gosched()
	}
	if !done {
		t.Fatal("Stop did not return while the port call was held")
	}
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v during Stop, want none", got)
	}
	releasePort()
	<-answered
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v after the release, want none", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("sessions %v, want [1 2 3]", got)
	}
}

// parkedIn reports whether, in stacks, the goroutine stopper waits in
// stopLocked's own wait for the keepers (a wait group's Wait called by
// stopLocked itself, not by a part it stops) and the goroutine keeper is
// parked (neither running nor runnable) inside Provider.PeerGone.
func parkedIn(stacks, stopper, keeper string) bool {
	var stop, keep bool
	for _, gr := range strings.Split(stacks, "\n\n") {
		switch {
		case strings.HasPrefix(gr, stopper+"["):
			lines := strings.Split(gr, "\n")
			for i := 1; i+2 < len(lines); i++ {
				if strings.HasPrefix(lines[i], "sync.(*WaitGroup).Wait(") &&
					strings.HasPrefix(lines[i+2], "thingfabric.dev/go-fabric/node.(*Node).stopLocked(") {
					stop = !strings.HasPrefix(gr, stopper+"[runn")
				}
			}
		case strings.HasPrefix(gr, keeper+"["):
			keep = !strings.HasPrefix(gr, keeper+"[runn") && strings.Contains(gr, "camera.(*Provider).EndLostPeer")
		}
	}
	return stop && keep
}

// TestCameraPeerLossAfterStopBeganEndsNothing: a pair the keeper works
// off once the node's life ended ends nothing, although the node holds no
// session with the peer — Stop closed them all.
func TestCameraPeerLossAfterStopBeganEndsNothing(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.n.cancel()
	g.n.endLostPeer(pgAddrA)
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v, want none", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("sessions %v, want [1 2 3]", got)
	}
}

// TestCameraPeerLossInStopsWindowEndsNothing (fable-diff-h4): between the
// moment Stop closed every session (br.Stop) and the end of the node's
// life (n.cancel) the node holds no session with any peer, so "no session
// left" is no evidence. A lost requestor whose new session Stop closed in
// that window keeps its WebRTC sessions: the keeper parked in PeerGone,
// released while Stop waits for the node's callbacks, ends nothing.
func TestCameraPeerLossInStopsWindowEndsNothing(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.port.hold, g.port.entered, g.port.release = true, make(chan struct{}), make(chan struct{})
	var release sync.Once
	releasePort := func() { release.Do(func() { close(g.port.release) }) }
	t.Cleanup(releasePort)
	answered := make(chan error, 1)
	go func() {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, pgFabric), pgPeerB, nil)
		_, err := g.prov.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: 2, Sdp: "a"})
		answered <- err
	}()
	<-g.port.entered
	queue := make(chan string, 1)
	g.n.sec.peerGone.testBeforeEnd = func(addr exchange.PeerAddress) {
		if addr == pgAddrA {
			queue <- goroutineHeader()
		}
	}
	g.openBridge(t, pgPeerA)
	g.lose(pgPeerA)
	waitGoroutineBlockedIn(t, <-queue, "camera.(*Provider).EndLostPeer")
	// A is live again: a new session before Stop.
	g.openBridge(t, pgPeerA)
	if !g.n.holdsPeerSession(pgAddrA) {
		t.Fatal("the node holds no session with A after A's new one")
	}
	// Stop parks at the callbacks' drain, after br.Stop and before the
	// node's life ends.
	hold := make(chan struct{})
	var releaseHold sync.Once
	releaseCallbacks := func() { releaseHold.Do(func() { close(hold) }) }
	t.Cleanup(releaseCallbacks)
	g.n.callbacks.Enqueue(func() { <-hold })
	stopped := make(chan error, 1)
	go func() { stopped <- g.n.Stop(context.Background()) }()
	for i := 0; g.n.holdsPeerSession(pgAddrA); i++ {
		if i == 1_000_000 {
			t.Fatal("Stop did not close A's session")
		}
		runtime.Gosched()
	}
	if !g.n.stopBegan() || g.n.life.Err() != nil {
		t.Fatalf("not in the window: stopBegan=%v life=%v", g.n.stopBegan(), g.n.life.Err())
	}
	releasePort()
	if err := <-answered; err != nil {
		t.Fatalf("answer: %v", err)
	}
	if d := <-g.done; d != (pgDone{pgAddrA, 0, false}) {
		t.Errorf("worked off %+v, want A neither ended nor cancelled", d)
	}
	releaseCallbacks()
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v, want none", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("sessions %v, want [1 2 3]", got)
	}
}

// TestCameraPeerLossStopBetweenTheReadsEndsNothing (fable-diff-h4-r2 §2,
// decision 650): Stop begins between the gone predicate's reads and closes
// the session A opened since its loss. "No session" read after Stop began
// is Stop's doing; the predicate reads the session table first, so A,
// live again when the predicate began, keeps its WebRTC sessions.
func TestCameraPeerLossStopBetweenTheReadsEndsNothing(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.port.hold, g.port.entered, g.port.release = true, make(chan struct{}), make(chan struct{})
	var release sync.Once
	releasePort := func() { release.Do(func() { close(g.port.release) }) }
	t.Cleanup(releasePort)
	answered := make(chan error, 1)
	go func() {
		ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, pgFabric), pgPeerB, nil)
		_, err := g.prov.MatterInvoke(ctx, wpdef.CmdProvideAnswer, wpdef.ProvideAnswerRequest{WebRtcSessionId: 2, Sdp: "a"})
		answered <- err
	}()
	<-g.port.entered
	queue := make(chan string, 1)
	g.n.sec.peerGone.testBeforeEnd = func(addr exchange.PeerAddress) {
		if addr == pgAddrA {
			queue <- goroutineHeader()
		}
	}
	g.openBridge(t, pgPeerA)
	g.lose(pgPeerA)
	waitGoroutineBlockedIn(t, <-queue, "camera.(*Provider).EndLostPeer")
	// A is live again: a new session before the predicate runs.
	g.openBridge(t, pgPeerA)
	if !g.n.holdsPeerSession(pgAddrA) {
		t.Fatal("the node holds no session with A after A's new one")
	}
	// Between the predicate's reads Stop begins and closes A's session;
	// Stop then waits for the node's callbacks, before the node's life ends.
	hold := make(chan struct{})
	var releaseHold sync.Once
	releaseCallbacks := func() { releaseHold.Do(func() { close(hold) }) }
	t.Cleanup(releaseCallbacks)
	g.n.callbacks.Enqueue(func() { <-hold })
	stopped := make(chan error, 1)
	var stopOnce sync.Once
	startStop := func() {
		stopOnce.Do(func() { go func() { stopped <- g.n.Stop(context.Background()) }() })
	}
	g.n.sec.peerGone.testBetweenReads = func(addr exchange.PeerAddress) {
		if addr != pgAddrA {
			return
		}
		startStop()
		for i := 0; g.n.holdsPeerSession(pgAddrA); i++ {
			if i == 1_000_000 {
				t.Error("Stop did not close A's session")
				return
			}
			runtime.Gosched()
		}
	}
	releasePort()
	if err := <-answered; err != nil {
		t.Fatalf("answer: %v", err)
	}
	d := <-g.done
	startStop() // the predicate may have answered before its second read
	releaseCallbacks()
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if d.addr != pgAddrA || d.ended != 0 {
		t.Errorf("worked off %+v, want A not ended", d)
	}
	if got := g.port.endedIDs(); len(got) != 0 {
		t.Errorf("port ended %v, want none", got)
	}
	if got := g.sessions(); !slices.Equal(got, []uint16{1, 2, 3}) {
		t.Errorf("sessions %v, want [1 2 3]", got)
	}
}

// TestCameraPeerGoneTakesNoKeeperLock (decision 650): the gone predicate
// runs under a camera provider's command serialisation, and a reopen holds
// keeperMu across the host's synchronous window hooks. The predicate and
// the stop flag it reads take no keeperMu: a lost requestor's end runs to
// completion while keeperMu is held.
func TestCameraPeerGoneTakesNoKeeperLock(t *testing.T) {
	t.Parallel()
	g := newPGNode(t)
	g.n.keeperMu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			g.n.keeperMu.Unlock()
		}
	}
	defer unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.n.endLostPeer(pgAddrA)
	}()
	finished := false
	for range 1_000_000 {
		select {
		case <-done:
			finished = true
		default:
		}
		if finished {
			break
		}
		runtime.Gosched()
	}
	unlock()
	if !finished {
		<-done
		t.Fatal("a lost requestor's end waited for keeperMu")
	}
	if d := <-g.done; d != (pgDone{pgAddrA, 1, false}) {
		t.Errorf("worked off %+v, want A ended on one provider", d)
	}
	if got := g.port.endedIDs(); !slices.Equal(got, []uint16{1, 3}) {
		t.Errorf("port ended %v, want [1 3]", got)
	}
}
