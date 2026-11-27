// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	provdef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestOTAProvider_InProgressBoundedPerFabric (review II, R2-P6-bound-1206,
// decision 134): ApplyUpdateRequests with tokens the provider never issued
// still answer as matter.js does (an unknown token with a version above 0
// is allowed, SoftwareUpdateManager.ts:1932-1935 at db91886c), each
// replacing the requestor's answered attempt before it; and the requestors
// of one fabric hold at most otaAttemptsPerFabric running attempts, the
// one active longest ago ending first.
func TestOTAProvider_InProgressBoundedPerFabric(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query %+v", r)
	}
	for i := range 500 {
		token := make([]byte, 8)
		binary.BigEndian.PutUint64(token, uint64(i)+1)
		if a := f.apply(t, token, 2); a.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply %d: %+v", i, a)
		}
	}
	if n := f.attempts(); n != 1 {
		t.Errorf("%d running attempts after 500 unknown tokens of one requestor, want 1", n)
	}
	m := f.p.mgr.(*otaStandaloneManager)
	for i := range 3 * otaAttemptsPerFabric {
		peer := OTAPeer{FabricIndex: otaTestFabric, NodeID: uint64(100 + i)}
		m.ApplyUpdate(context.Background(), peer, []byte{byte(i)}, 2, 0)
		f.clock.Advance(time.Second)
	}
	m.ApplyUpdate(context.Background(), OTAPeer{FabricIndex: otaTestFabric + 1, NodeID: 1}, []byte{1}, 2, 0)
	m.mu.Lock()
	n, other := 0, 0
	for _, a := range m.set.All() {
		if a.Peer().FabricIndex == otaTestFabric {
			n++
		} else {
			other++
		}
	}
	oldest := m.set.Of(OTAPeer{FabricIndex: otaTestFabric, NodeID: uint64(100 + 2*otaAttemptsPerFabric)})
	m.mu.Unlock()
	if n != otaAttemptsPerFabric || other != 1 || len(oldest) != 1 {
		t.Errorf("fabric holds %d attempts (want %d), other fabric %d (want 1), oldest kept %d (want 1)", n, otaAttemptsPerFabric, other, len(oldest))
	}
}

// TestOTAProvider_GroupMessageIsNotCASE (decision 158 brief: the CASE check
// is internal/caseauth's): a group message carries a fabric and is not
// PASE, and is still refused.
func TestOTAProvider_GroupMessageIsNotCASE(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	ctx := im.WithGroupSubject(otaCtx(7), im.GroupSubject{GroupID: 1})
	_, err := f.p.MatterInvoke(ctx, provdef.CmdQueryImage, bdxQuery())
	var se interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("group QueryImage: %v", err)
	}
}
