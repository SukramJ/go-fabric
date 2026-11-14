// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/surface"
)

// TestNoAttributeReadableBelowItsReadPrivilege walks every attribute every
// server of a node lists — the root with ICD and the terms and conditions
// configured, and a bridged endpoint for every device type the device kit
// builds with its defaults — and holds the read privilege the dispatcher
// gates a read with (endpoint.TopologyDispatcher.MinReadPrivilege, the
// im.AttributeReadPrivilegeProvider every read and subscription report
// asks) to the attribute's read privilege in parity/schema.json, matter.js
// at the pin: no attribute is readable below it, whatever its server
// forwards (review II, R2-P3-admin-02).
func TestNoAttributeReadableBelowItsReadPrivilege(t *testing.T) {
	t.Parallel()
	level := map[string]uint8{"V": 1, "O": 3, "M": 4, "A": 5}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	cfg.ICD = &node.ICDConfig{
		IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
		ActiveModeDuration: 2 * time.Second, ActiveModeThreshold: time.Second,
		ClientsSupportedPerFabric: 2,
	}
	// The terms and conditions are an "Ahead of matter.js" row a host
	// enables (ADR 0030); their attributes read at Administer.
	cfg.Surface = surface.Policies{Extensions: surface.NewExtensions("BD-chip-GeneralCommissioning-TermsAndConditions")}
	cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 3, Acknowledgements: 1}
	n := newNode(t, cfg)
	ctx := t.Context()
	added, refused := 0, 0
	for _, ty := range device.Types() {
		dev, err := device.New(ty, device.Options{StableKey: endpoint.StringKey("read:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler), Surface: cfg.Surface})
		if err != nil {
			refused++ // a type whose defaults need the host's declarations
			continue
		}
		if err := n.Aggregator().Add(ctx, dev); err != nil {
			refused++ // a type the Aggregator does not bridge
			continue
		}
		added++
	}
	if added < 40 {
		t.Fatalf("only %d device types were bridged (%d refused)", added, refused)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	topo := n.Bridge().Topology()
	d := endpoint.NewTopologyDispatcher(topo)

	checked, elevated := 0, map[string]bool{}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			id := srv.MatterClusterID()
			js := paritytest.ClusterSnapshot(t, id)
			want := map[uint32]uint8{}
			for _, a := range js.Attributes {
				if a.Effective == nil || a.Effective.Access == nil {
					t.Fatalf("%s (0x%04X) attribute %s carries no effective access in the snapshot", js.Name, id, a.Name)
				}
				p, ok := level[a.Effective.Access.ReadPriv]
				if !ok {
					t.Fatalf("%s (0x%04X) attribute %s: read privilege %q", js.Name, id, a.Name, a.Effective.Access.ReadPriv)
				}
				want[a.ID] = p
			}
			listed := srv.MatterReportable()
			if lister, ok := srv.(contract.ClusterAttributeLister); ok {
				listed = lister.MatterAttributes()
			}
			for _, attr := range listed {
				p, known := want[attr]
				if !known {
					continue // a global attribute, read at View
				}
				checked++
				if p > 1 {
					elevated[fmt.Sprintf("0x%04X/0x%04X", id, attr)] = true
				}
				if got := d.MinReadPrivilege(ep.ID, id, attr); got < p {
					t.Errorf("endpoint %d %s (0x%04X) attribute 0x%04X is read at privilege %d, the data model's read privilege is %d", ep.ID, js.Name, id, attr, got, p)
				}
			}
		}
	}
	// The attributes the finding named are among those walked.
	for _, key := range []string{"0x0046/0x0003", "0x0046/0x0004", "0x0030/0x0005", "0x0030/0x0009", "0x001F/0x0000"} {
		if !elevated[key] {
			t.Errorf("attribute %s above View was not walked", key)
		}
	}
	t.Logf("%d attributes walked, %d above View (%d device types bridged, %d refused)", checked, len(elevated), added, refused)
}
