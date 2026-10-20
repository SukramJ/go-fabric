// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package booleanstate_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	"github.com/SukramJ/go-fabric/contract"
)

type emitted struct {
	endpoint           uint16
	clusterID, eventID uint32
	data               any
	priority           contract.EventPriority
}

type recorder struct{ events []emitted }

func (r *recorder) MatterEmitEvent(endpoint uint16, clusterID, eventID uint32, data any, priority contract.EventPriority) {
	r.events = append(r.events, emitted{endpoint, clusterID, eventID, data, priority})
}

// TestTypedEmit: the typed event goes out through the server's Emit with
// the definition's priority; an event outside the EventList is refused.
// (LevelControl, the cluster of the other hand-written typed test,
// defines no event at the matter.js pin.)
func TestTypedEmit(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(booleanstatedef.Definition, spec.Options{Features: uint32(booleanstatedef.FeatureChangeEvent)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	s := booleanstatedef.Wrap(srv)
	if err := s.EmitStateChange(3, booleanstatedef.StateChangeEvent{StateValue: true}); err != nil {
		t.Fatal(err)
	}
	want := emitted{3, booleanstatedef.ClusterID, booleanstatedef.EventStateChange, booleanstatedef.StateChangeEvent{StateValue: true}, srv.EventPriority(booleanstatedef.EventStateChange)}
	if len(rec.events) != 1 || rec.events[0] != want {
		t.Errorf("emitted %+v, want %+v", rec.events, want)
	}

	plain, err := spec.NewServer(booleanstatedef.Definition, spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := booleanstatedef.Wrap(plain).EmitStateChange(3, booleanstatedef.StateChangeEvent{}); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event outside the EventList: %v", err)
	}
	if err := booleanstatedef.Wrap(struct{ contract.ClusterServer }{srv}).EmitStateChange(3, booleanstatedef.StateChangeEvent{}); !errors.Is(err, spec.ErrNoEvents) {
		t.Errorf("a server without Emit: %v", err)
	}
}
