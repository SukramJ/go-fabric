// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// fabricClosingRegistry is a session registry that closes by fabric.
type fabricClosingRegistry struct {
	fakeSessionRegistry
	mu     sync.Mutex
	closed []uint8
}

func (r *fabricClosingRegistry) CloseFabric(fabricIndex uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = append(r.closed, fabricIndex)
}

func (r *fabricClosingRegistry) closedFabrics() []uint8 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint8(nil), r.closed...)
}

// TestEmitFabricRemovedContextClosesTheFabricsSessions pins matter.js
// Fabric.remove: a removed fabric's sessions end — inside the RemoveFabric
// command only once its response is out, outside a command at once
// (TC-CADMIN-1.15 step 12).
func TestEmitFabricRemovedContextClosesTheFabricsSessions(t *testing.T) {
	t.Parallel()
	b := newUnstartedBridge(t)
	reg := &fabricClosingRegistry{}
	b.AttachSessionRegistry(reg)

	after := &im.AfterResponse{}
	b.EmitFabricRemovedContext(im.WithAfterResponse(context.Background(), after), 2)
	if got := reg.closedFabrics(); len(got) != 0 {
		t.Fatalf("sessions closed before the response: %v", got)
	}
	after.Run()
	if got := reg.closedFabrics(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("closed %v after the response, want [2]", got)
	}
	b.EmitFabricRemovedContext(context.Background(), 3)
	if got := reg.closedFabrics(); len(got) != 2 || got[1] != 3 {
		t.Fatalf("closed %v outside a command, want fabric 3 at once", got)
	}
}

// adoptingRegistry records AdoptFabricIndex calls.
type adoptingRegistry struct {
	fakeSessionRegistry
	mu      sync.Mutex
	adopted map[uint16]uint8
}

func (r *adoptingRegistry) AdoptFabricIndex(sessionID uint16, fabricIndex uint8) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adopted == nil {
		r.adopted = map[uint16]uint8{}
	}
	r.adopted[sessionID] = fabricIndex
	return nil
}

// TestAddNOCOverPASEAdoptsTheSession pins matter.js
// OperationalCredentialsServer.addNoc (`session.fabric = fabric` for a
// PASE session): a successful AddNOC moves its PASE session onto the new
// fabric; a CASE session, a failed AddNOC and other commands do not
// (TC-CGEN-2.4).
func TestAddNOCOverPASEAdoptsTheSession(t *testing.T) {
	t.Parallel()
	b := newUnstartedBridge(t)
	reg := &adoptingRegistry{}
	b.AttachSessionRegistry(reg)
	b.AttachSessionLookup(NewOperationalSessionLookup(nil).WithPASEResolver(func(id uint16) (bool, bool) {
		return id == 5, true
	}))
	nocResp := func(status, idx uint8) im.InvokeResponse {
		return im.InvokeResponse{Responses: []im.InvokeResponseEntry{{
			Path:        im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003E, Command: 0x08},
			Response:    core.NOCResponse{StatusCode: status, FabricIndex: idx},
			HasResponse: true,
		}}}
	}
	b.adoptPASESessionOnAddNOC(6, nocResp(core.NOCStatusOK, 3))
	b.adoptPASESessionOnAddNOC(5, nocResp(core.NOCStatusInvalidNOC, 3))
	if len(reg.adopted) != 0 {
		t.Fatalf("adopted %v from a CASE session or a failed AddNOC", reg.adopted)
	}
	b.adoptPASESessionOnAddNOC(5, nocResp(core.NOCStatusOK, 3))
	if reg.adopted[5] != 3 {
		t.Fatalf("adopted %v, want PASE session 5 on fabric 3", reg.adopted)
	}
}
