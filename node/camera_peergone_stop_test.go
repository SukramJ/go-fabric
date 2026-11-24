// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// R5-conc-5301: the peer-gone keeper is a goroutine Stop waits for. Its
// wait for a camera provider's command serialisation ends with the node,
// and once the node's life ended no lost requestor's session ends any
// more — not a pair already dequeued, not one whose wait a port call held.

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
	waitGoroutineBlockedIn(t, keeper, "camera.(*Provider).PeerGone")

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
			keep = !strings.HasPrefix(gr, keeper+"[runn") && strings.Contains(gr, "camera.(*Provider).PeerGone")
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
