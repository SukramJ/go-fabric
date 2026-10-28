// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// The node's CommissionerReach (brief 27-B §7): an administrator of the kit
// whose declaration names no reach announces itself to another one, whose
// node reads the announcer back over its own client stack — the
// AdministratorFabricIndex, Fabrics, TrustedRootCertificates and NOCs —
// and has it sign the Vendor ID Verification, as connectedhomeip's
// JCMCommissionee does over the invoking session (JCMCommissionee.cpp:
// 46-69, :297-322 at 6170af84). Without the announcer's grant the reads
// fail and the announcement answers FAILURE.
func TestAnnounceThroughTheNodesCommissionerReach(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	kit := func(key string) (*device.JointFabricAdministrator, *jointfabric.Administrator) {
		jfa, err := device.NewJointFabricAdministrator(device.Options{StableKey: endpoint.StringKey(key), Name: key},
			dsdef.Declare{Sync: nopSync{}}, jfadef.Declare{AdministratorPort: newKeyPort(t)})
		if err != nil {
			t.Fatal(err)
		}
		admin, _ := jfa.Server(jfadef.ClusterID).(*jointfabric.Administrator)
		return jfa, admin
	}
	withClient := func(cfg *node.Config) { cfg.Client.Enabled = true }
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	announcerKit, announcerAdmin := kit("announcer")
	targetKit, targetAdmin := kit("target")
	announcer := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{announcerKit}, Configure: withClient})
	target := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{targetKit}, Configure: withClient})

	// Fabric B: the announcer is its administrator (the administrator and
	// anchor CATs on its NOC, an ICAC), the target an ordinary member.
	b := site.AddController(fabrictest.ControllerOptions{FabricID: 0x0B, VendorID: 0xFFF2, IntermediateCA: true})
	pairA, err := b.Commission(ctx, announcer, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) { o.CATs = []uint32{adminCAT, anchorCAT} }})
	if err != nil {
		t.Fatalf("commission the announcer: %v", err)
	}
	pairT, err := b.Commission(ctx, target, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the target: %v", err)
	}
	if err := site.Eventually(ctx, "the announcer's administrator fabric", func() bool {
		idx, _ := announcerAdmin.MatterRead(jfadef.AttrAdministratorFabricIndex)
		i, ok := idx.(uint8)
		return ok && i != 0
	}); err != nil {
		t.Fatal(err)
	}
	grant := func(d *fabrictest.Device, subjects ...uint64) {
		t.Helper()
		if err := d.Store().ReplaceACL(ctx, 1, []store.ACLEntry{{
			FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
			Subjects: append([]uint64{b.Fabric().RootNodeID}, subjects...),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	announcerEP, ok := announcer.EndpointOf("announcer")
	if !ok {
		t.Fatal("no announcer endpoint")
	}
	targetEP, ok := target.EndpointOf("target")
	if !ok {
		t.Fatal("no target endpoint")
	}
	announce := func() im.StatusCode {
		t.Helper()
		var status im.StatusCode
		err := site.Run(ctx, func(ctx context.Context) error {
			p, err := announcer.Peers().For(handshake.PeerAddress{FabricIndex: 1, NodeID: pairT.Address.NodeID})
			if err != nil {
				return err
			}
			if _, err := p.Connect(ctx, peer.ConnectOptions{}); err != nil {
				return err
			}
			res, err := interaction.ForPeer(p, interaction.Config{}).Invoke(ctx,
				im.ConcreteCommandPath{Endpoint: targetEP, Cluster: jfadef.ClusterID, Command: jfadef.CmdAnnounceJointFabricAdministrator},
				jfadef.AnnounceJointFabricAdministratorRequest{EndpointId: announcerEP}, interaction.InvokeOptions{})
			status = res.Status
			if err != nil && status == im.StatusSuccess {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return status
	}

	// The target may be invoked by the announcer; the announcer does not
	// yet let the target read it.
	grant(target, pairA.Address.NodeID)
	if got := announce(); got != im.StatusFailure {
		t.Fatalf("announcement without the announcer's grant answered %v, want Failure", got)
	}
	grant(announcer, pairT.Address.NodeID)
	if got := announce(); got != im.StatusSuccess {
		t.Fatalf("announcement answered %v, want Success", got)
	}
	if got := targetAdmin.VIDVerifiedFabric(); got != 1 {
		t.Errorf("VID verified for fabric %d, want 1", got)
	}
}
