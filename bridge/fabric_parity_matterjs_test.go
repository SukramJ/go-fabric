// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A port of matter.js packages/protocol/test/fabric/FabricTest.ts at
// 862ad75f: removing a fabric ends its sessions.
//
// matter.js calls Fabric.delete(currentExchange); the Go removal inside the
// RemoveFabric command is Bridge.EmitFabricRemovedContext, the session
// registry being the operational manager. The CloseSession each other
// session is told with is TestEmitFabricRemovedContext_InvokingSessionLast's.

import (
	"context"
	"errors"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
)

func TestFabricRemoveFromSession(t *testing.T) {
	t.Parallel()

	t.Run("removes all sessions when removing fabric", func(t *testing.T) {
		t.Parallel()
		dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
		const (
			session1 = uint16(1) // FabricTest.ts:149-160
			session2 = uint16(2) // FabricTest.ts:163-174, the exchange's session
		)
		for _, sid := range []uint16{session1, session2} {
			if _, err := dev.sessions.OpenFromSigmaWithID(sid, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x8d4b, nil, peerLossKeys(t)); err != nil {
				t.Fatal(err)
			}
		}
		deleted := false
		dev.br.SetOnFabricRemoved(func(idx uint8) { deleted = idx == resumeFabricIndex })

		after := &im.AfterResponse{}
		ctx := mattercore.WithInvokeSessionID(im.WithAfterResponse(context.Background(), after), session2)
		dev.br.EmitFabricRemovedContext(ctx, resumeFabricIndex)

		if _, err := dev.sessions.Get(session1); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session 1: %v, want deleted", err)
		}
		// Not destroyed directly: the session of the fabric removal closes
		// once its exchange ends (FabricTest.ts:199-201).
		if _, err := dev.sessions.Get(session2); err != nil {
			t.Errorf("session 2 before its exchange ended: %v, want open", err)
		}
		if !deleted {
			t.Error("fabric deleted event not emitted")
		}
		after.Run()
		if _, err := dev.sessions.Get(session2); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session 2 after its exchange: %v, want closed", err)
		}
	})
}
