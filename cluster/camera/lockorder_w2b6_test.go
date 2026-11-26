// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
)

// lockOrderRoundsW2B6 bounds each goroutine of the lock-order tests.
const lockOrderRoundsW2B6 = 200

// TestProviderKeepUnderServerLockW2B6 (N1, decision 689): the table's keep
// runs under the spec server's lock and takes the provider's mu. Every
// holder of mu — Sessions, MatterHeldFabrics, AttachPeerConnector, the
// signals' target lookup (End) — runs beside changes of the table, reads
// of CurrentSessions through the spec server and a listener that reads
// Sessions; none waits on the server under mu, so the run ends. A
// lock-order inversion hangs it until the test binary's timeout.
func TestProviderKeepUnderServerLockW2B6(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	p.AttachPeerConnector(&fakePeers{})
	unsub := p.OnAttributeChange(func(contract.Change) { _ = p.Sessions() })
	defer unsub()
	session := func(id uint16) camera.ProviderSession {
		return camera.ProviderSession{
			Id: id, PeerNodeId: sigPeerNode, PeerEndpointId: sigPeerEndpoint, FabricIndex: sigPeerFabric,
			StreamUsage: wpdef.StreamUsageLiveView, VideoStreams: &[]uint16{1},
		}
	}
	var wg sync.WaitGroup
	run := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range lockOrderRoundsW2B6 {
				fn(i)
			}
		}()
	}
	run(func(i int) {
		if err := p.UpsertSession(session(uint16(i % 4))); err != nil {
			t.Error(err)
		}
	})
	run(func(i int) { p.RemoveSession(uint16(i % 4)) })
	run(func(int) { _ = p.Sessions() })
	run(func(int) { _ = p.MatterHeldFabrics() })
	run(func(int) { p.AttachPeerConnector(&fakePeers{}) })
	run(func(i int) { _ = p.End(context.Background(), uint16(i%4), 0) })
	run(func(int) { _, _ = p.MatterRead(wpdef.AttrCurrentSessions) })
	wg.Wait()
}

// TestRequestorKeepUnderServerLockW2B6 is the provider's case for the
// requestor: Sessions and MatterHeldFabrics hold mu beside changes of the
// table and reads of CurrentSessions through the spec server.
func TestRequestorKeepUnderServerLockW2B6(t *testing.T) {
	t.Parallel()
	r := newRequestor(t, nil)
	unsub := r.OnAttributeChange(func(contract.Change) { _ = r.Sessions() })
	defer unsub()
	var wg sync.WaitGroup
	run := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range lockOrderRoundsW2B6 {
				fn(i)
			}
		}()
	}
	run(func(i int) {
		if err := r.UpsertSession(makeSession(withID(uint16(i%4 + 1)))); err != nil {
			t.Error(err)
		}
	})
	run(func(i int) {
		r.RemoveSession(wrdef.KeyOf(makeSession(withID(uint16(i%4 + 1)))))
	})
	run(func(int) { _ = r.Sessions() })
	run(func(int) { _ = r.MatterHeldFabrics() })
	run(func(int) { _, _ = r.MatterRead(wrdef.AttrCurrentSessions) })
	wg.Wait()
}
