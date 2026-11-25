// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// aclReadingEmitterP8G is an event emitter whose hook reads the Acl list
// on the emitter's goroutine, as a synchronous host observer may
// (node.Observers.OnEvent runs on the emitter's goroutine).
type aclReadingEmitterP8G struct {
	a     *AccessControl
	ctx   context.Context
	reads atomic.Int32
}

func (e *aclReadingEmitterP8G) MatterEmitEvent(_ uint16, _, _ uint32, _ any, _ contract.EventPriority) {
	if _, ok := e.a.MatterReadFiltered(e.ctx, accessControlAttrACL); ok {
		e.reads.Add(1)
	}
}

// TestAccessControlEventHookReadsAclDuringAWriteP8G (decision 681, gate
// a-B1 of R5-conc-5004): an Acl write emits AccessControlEntryChanged
// while it holds writeMu; a hook that reads Acl from inside that emit
// returns, and the write with it. Before, the read took writeMu on the
// goroutine that held it and the write never returned. Not parallel:
// WaitParkedOnMutexA1 reads every goroutine's stack.
func TestAccessControlEventHookReadsAclDuringAWriteP8G(t *testing.T) {
	ctx := context.Background()
	const f uint8 = 3
	st := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}, fabrics: []uint8{f}}
	a, err := NewAccessControlWith(ctx, AccessControlConfig{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	wctx := im.WithFabricFilter(ctx, true, f)
	em := &aclReadingEmitterP8G{a: a, ctx: wctx}
	a.SetEndpoint(0)
	a.SetMatterEventEmitter(em)

	rows := []AccessControlEntryStruct{
		{Privilege: 5, AuthMode: 2, Subjects: []uint64{0x1003}, FabricIndex: f},
		{Privilege: 3, AuthMode: 2, Subjects: []uint64{0x2003}, FabricIndex: f},
	}
	var werr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		werr = a.MatterWrite(wctx, accessControlAttrACL, rows)
	}()
	if WaitParkedOnMutexA1(t, done, "listACLMarking") {
		t.Fatal("the hook's Acl read waits for writeMu, which the emitting write holds: the write never returns")
	}
	<-done
	if werr != nil {
		t.Fatal(werr)
	}
	if n := em.reads.Load(); n == 0 {
		t.Error("the hook read nothing: the write emitted no event")
	}
}
