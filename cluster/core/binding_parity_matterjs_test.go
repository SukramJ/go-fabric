// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/binding/BindingServerTest.ts
// at 862ad75f: the case the register lists open. The replay of stored
// entries, the write diff, the close and the entry written while nothing
// observes are binding:TestAttachDetachClose, binding:TestDormantThenWoken
// and TestBindingPersist.
//
// matter.js adds an OnOffLightSwitch whose BindingServer starts with a
// stored self-binding and observes established only after its initialize
// registered the entries; the port restores the stored list into a
// Binding server (Persist), attaches it to a binding manager over the
// switch's node — our node 100 on fabric 1, endpoint 1 with the device
// type's mandatory client clusters — and observes established once the
// entry registered and went dormant.
package core_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/schema"
)

// switchTopology is the node of the case: our node 100 on fabric 1 and
// the OnOffLightSwitch on endpoint 1, serving Identify, Descriptor and
// Binding, declaring the device type's mandatory client clusters.
type switchTopology struct{ clients []uint32 }

func (switchTopology) NodeID(fabricIndex uint8) (uint64, bool) { return 100, fabricIndex == 1 }
func (switchTopology) HasEndpoint(ep uint16) bool              { return ep <= 1 }
func (switchTopology) HasServer(ep uint16, cluster uint32) bool {
	return ep == 1 && slices.Contains([]uint32{0x0003, 0x001D, 0x001E}, cluster)
}

func (s switchTopology) ClientClusters(ep uint16) []uint32 {
	if ep != 1 {
		return nil
	}
	return s.clients
}
func (switchTopology) HasGroupKey(uint8, uint16) bool { return false }

// dormantLog signals the manager's "binding.entry.dormant" record.
type dormantLog struct{ dormant chan struct{} }

func (dormantLog) Enabled(context.Context, slog.Level) bool { return true }

func (d dormantLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "binding.entry.dormant" {
		select {
		case d.dormant <- struct{}{}:
		default:
		}
	}
	return nil
}
func (d dormantLog) WithAttrs([]slog.Attr) slog.Handler { return d }
func (d dormantLog) WithGroup(string) slog.Handler      { return d }

// TestBindingServerParityMatterJS ports the open case of the describe
// "BindingServer" (BindingServerTest.ts:16-206).
func TestBindingServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :191-206.
	t.Run("resolves a stored entry for a behavior that observes established after the entries registered", func(t *testing.T) {
		t.Parallel()
		def, ok := schema.DeviceTypeDefinitionOf(0x0103) // OnOffLightSwitch
		if !ok {
			t.Fatal("no OnOffLightSwitch in the schema")
		}
		var clients []uint32
		for i := range def.Requirements {
			r := &def.Requirements[i]
			if r.Element == schema.RequirementClientCluster && r.HasID && r.Conformance.Op == "M" {
				clients = append(clients, r.ID)
			}
		}
		log := dormantLog{dormant: make(chan struct{}, 1)}
		m, err := binding.New(binding.Config{Topology: switchTopology{clients: clients}, Logger: slog.New(log)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		b := core.NewBinding()
		stored := []core.TargetStruct{{FabricIndex: 1, Node: 100, Endpoint: 1}}
		if err := b.Persist(core.BindingConfig{
			Load: func() ([]core.TargetStruct, error) { return slices.Clone(stored), nil },
			Save: func([]core.TargetStruct) error { return nil },
		}); err != nil {
			t.Fatal(err)
		}
		m.Attach(1, b)
		select {
		case <-log.dormant:
		case <-time.After(10 * time.Second):
			t.Fatal("the stored entry did not register")
		}
		established := make(chan binding.Resolution, 2)
		m.OnEstablished(func(r binding.Resolution) { established <- r })
		select {
		case r := <-established:
			if r.Kind != binding.KindServer || r.Endpoint != 1 {
				t.Fatalf("established %+v, want a server resolution on endpoint 1", r)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the stored entry did not resolve once observed")
		}
	})
}
