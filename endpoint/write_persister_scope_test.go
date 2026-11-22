// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// refusingPersister refuses to store one attribute and stores the others.
type refusingPersister struct{ refuse uint32 }

func (p refusingPersister) PersistWrite(_ context.Context, _ *Endpoint, _ contract.ClusterServer, attrID uint32, _ im.AttributeValue) (func(bool), error) {
	if attrID == p.refuse {
		return nil, errors.New("disk")
	}
	return func(bool) {}, nil
}

// TestRefusedStoreWriteFailsOnlyItsPath: a store that refuses the write of
// one nonvolatile attribute answers FAILURE for that path, and the server
// never sees the value; the write of another path is stored and applied —
// the request's other paths are answered as their own writes go. The scope
// is this module's choice: matter.js ends the whole interaction with a
// StatusResponse FAILURE (owner decision 206; review II, R2-P6-par-2903,
// BD-Matter-NonVolatileWrite-StoredBeforeApplied).
func TestRefusedStoreWriteFailsOnlyItsPath(t *testing.T) {
	t.Parallel()
	const onTime, offWaitTime = 0x4001, 0x4002
	srv := &recordingServer{id: 0x0006, attrs: []uint32{0x0000, onTime, offWaitTime}}
	ep := &Endpoint{ID: 2, Source: recordingSource{srv: srv}}
	topo := makeTopology(ep)
	topo.Persister = refusingPersister{refuse: onTime}
	d := NewTopologyDispatcher(topo)

	refused := d.Write(context.Background(), concreteAttrPath(2, 0x0006, onTime), im.AttributeValue{Value: uint64(10)})
	stored := d.Write(context.Background(), concreteAttrPath(2, 0x0006, offWaitTime), im.AttributeValue{Value: uint64(20)})
	if len(refused) != 1 || refused[0].Status != im.StatusFailure {
		t.Fatalf("the refused store write answered %+v, want one FAILURE", refused)
	}
	if len(stored) != 1 || stored[0].Status != im.StatusSuccess {
		t.Fatalf("the other path answered %+v, want one SUCCESS", stored)
	}
	if recordedWrite(srv.writeCalls, onTime) || !recordedWrite(srv.writeCalls, offWaitTime) {
		t.Fatalf("the server saw %v, want only OffWaitTime", srv.writeCalls)
	}
}
