// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestFabricUpdateAbortsPendingInitiation: UpdateNOC's session teardown
// ([Bridge.CloseFabricSessionsAfterUpdate]) ends a CASE handshake this
// node opened on the updated fabric and that has not finished — the peer
// here never answers its Sigma1 — while an update of another fabric
// leaves it running. connectedhomeip aborts a pending establishment on the
// updated fabric in either role (src/protocols/secure_channel/CASESession.cpp:476,
// :529, :423-433 at 6170af84). Review II, R2-P6-sec-2412, owner decision
// 233.
func TestFabricUpdateAbortsPendingInitiation(t *testing.T) {
	t.Parallel()
	fab := newResumeFabric(t)
	silent, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: silent.LocalAddr().(*net.UDPAddr)})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := dev.br.ConnectPeer(ctx, resumeFabricIndex, resumeCtrlNode)
		result <- err
	}()
	initiating := func() int {
		dev.br.resumption.mu.Lock()
		defer dev.br.resumption.mu.Unlock()
		return len(dev.br.resumption.initiating)
	}
	for deadline := time.Now().Add(5 * time.Second); initiating() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the initiation never began")
		}
		time.Sleep(5 * time.Millisecond)
	}
	dev.br.CloseFabricSessionsAfterUpdate(context.Background(), resumeFabricIndex+1)
	select {
	case err := <-result:
		t.Fatalf("an update of another fabric ended the initiation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	dev.br.CloseFabricSessionsAfterUpdate(context.Background(), resumeFabricIndex)
	select {
	case err := <-result:
		if !errors.Is(err, errFabricUpdated) {
			t.Fatalf("the initiation ended with %v, want errFabricUpdated", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the initiation on the updated fabric went on")
	}
	if n := initiating(); n != 0 {
		t.Fatalf("%d initiations still registered", n)
	}
}
