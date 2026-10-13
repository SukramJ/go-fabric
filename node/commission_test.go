// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// The facade after AddNOC, over the wire: a bridgetest.Commissioner
// commissions a fresh node (PASE, the commissioning commands, CASE,
// CommissioningComplete) and every assertion here crossed the wire both
// ways on a CASE session — the ACL, the CASE responder and initiator, the
// fabric hooks and the persisted subscriptions the node wires.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	testFabricID   uint64 = 0x0000_0000_0000_0F0B
	testNodeID     uint64 = 0x0000_0000_0000_1234
	otherAdminNode uint64 = 0x0000_0000_0000_2222

	clusterBasicInformation uint32 = 0x0028
	attrNodeLabel           uint32 = 0x0005
	clusterAccessControl    uint32 = 0x001F
	attrACL                 uint32 = 0x0000
	clusterDescriptor       uint32 = 0x001D
	attrPartsList           uint32 = 0x0003

	privilegeView       = 1
	privilegeAdminister = 5
	authModeCASE        = 2
)

func attr(endpoint uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: endpoint, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func commissionerOptions() bridgetest.Options {
	return bridgetest.Options{Passcode: testPasscode, Discriminator: 3840, FabricID: testFabricID, NodeID: testNodeID, Seed: 7}
}

// commissioned starts a node on db and commissions it.
func commissioned(t *testing.T, cfg node.Config) (*node.Node, *bridgetest.Commissioner, *bridgetest.Session) {
	t.Helper()
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	s, err := c.Commission(context.Background())
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	return n, c, s
}

// TestAddNOCCommissionsFreshNode: the commissioning commands install the
// fabric — Fabrics lists it with the node id the NOC assigned, the
// commissioning window closes on CommissioningComplete, and the node
// answers CASE as that node id.
func TestAddNOCCommissionsFreshNode(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	opts := commissionerOptions()
	opts.Advertiser = adv // PASE and CASE find the node through its records
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	s, err := c.Commission(context.Background())
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	fabrics, err := n.Fabrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fabrics) != 1 || fabrics[0].FabricID != testFabricID || fabrics[0].NodeID != testNodeID || fabrics[0].FabricIndex != c.FabricIndex() {
		t.Fatalf("Fabrics() = %+v, want fabric 0x%X node 0x%X index %d", fabrics, testFabricID, testNodeID, c.FabricIndex())
	}
	if fabrics[0].CompressedID != c.CompressedFabricID() {
		t.Errorf("compressed fabric id %X, commissioner derived %X", fabrics[0].CompressedID, c.CompressedFabricID())
	}
	if n.CommissioningWindow().IsOpen() {
		t.Error("the commissioning window is still open after CommissioningComplete")
	}
	if got := s.PeerNodeID(); got != testNodeID {
		t.Errorf("CASE peer node id 0x%X, want 0x%X", got, testNodeID)
	}
	report, err := s.Read(context.Background(), attr(0, clusterBasicInformation, attrNodeLabel))
	if err != nil {
		t.Fatalf("CASE read: %v", err)
	}
	if v, ok := report.Value(0, clusterBasicInformation, attrNodeLabel); !ok || v.String != "test node" {
		t.Errorf("NodeLabel over CASE = %+v (%v)", v, ok)
	}
}

// TestCASEReadsAdministratorACL: the ACL AddNOC installed — one
// Administer entry, CASE, the CaseAdminSubject, on the new fabric — read
// over CASE.
func TestCASEReadsAdministratorACL(t *testing.T) {
	t.Parallel()
	_, c, s := commissioned(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	report, err := s.Read(context.Background(), attr(0, clusterAccessControl, attrACL))
	if err != nil {
		t.Fatalf("read ACL: %v", err)
	}
	acl, ok := report.Value(0, clusterAccessControl, attrACL)
	if !ok || len(acl.Children) != 1 {
		t.Fatalf("ACL = %+v (%v), want one entry", acl, ok)
	}
	entry := acl.Children[0]
	privilege, _ := entry.Child(1)
	authMode, _ := entry.Child(2)
	subjects, _ := entry.Child(3)
	targets, _ := entry.Child(4)
	fabric, _ := entry.Child(254)
	if privilege.Uint != privilegeAdminister || authMode.Uint != authModeCASE {
		t.Errorf("entry privilege %d auth mode %d, want Administer (5) over CASE (2)", privilege.Uint, authMode.Uint)
	}
	if len(subjects.Children) != 1 || subjects.Children[0].Uint != bridgetest.DefaultControllerNodeID {
		t.Errorf("entry subjects %+v, want [%d]", subjects.Children, bridgetest.DefaultControllerNodeID)
	}
	if !targets.IsNull {
		t.Errorf("entry targets %+v, want null", targets)
	}
	if fabric.Uint != uint64(c.FabricIndex()) {
		t.Errorf("entry fabric index %d, want %d", fabric.Uint, c.FabricIndex())
	}
}

// aclEntry encodes one AccessControlEntryStruct for a write (privilege,
// CASE, one subject, all targets).
func aclEntry(e *tlv.Encoder, privilege uint8, subject uint64) {
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(1), uint64(privilege))
	e.PutUint(tlv.ContextTag(2), authModeCASE)
	e.StartArray(tlv.ContextTag(3))
	e.PutUint(tlv.AnonymousTag(), subject)
	_ = e.EndContainer()
	e.PutNull(tlv.ContextTag(4))
	_ = e.EndContainer()
}

// TestWriteWithoutPrivilegeIsUnsupportedAccess: the administrator grants a
// second controller View; that controller reads, but its write of
// NodeLabel (Manage) is UNSUPPORTED_ACCESS, while the administrator's
// succeeds.
func TestWriteWithoutPrivilegeIsUnsupportedAccess(t *testing.T) {
	t.Parallel()
	_, c, admin := commissioned(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	status, err := admin.Write(ctx, attr(0, clusterAccessControl, attrACL), func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartArray(tag)
		aclEntry(e, privilegeAdminister, bridgetest.DefaultControllerNodeID)
		aclEntry(e, privilegeView, otherAdminNode)
		_ = e.EndContainer()
	})
	if err != nil || status != im.StatusSuccess {
		t.Fatalf("ACL write = %s, %v", status, err)
	}
	viewer, err := c.CASEAs(ctx, otherAdminNode)
	if err != nil {
		t.Fatalf("CASE as the viewer: %v", err)
	}
	report, err := viewer.Read(ctx, attr(0, clusterBasicInformation, attrNodeLabel))
	if err != nil {
		t.Fatalf("viewer read: %v", err)
	}
	if _, ok := report.Value(0, clusterBasicInformation, attrNodeLabel); !ok {
		t.Fatalf("viewer read NodeLabel: statuses %+v", report.Statuses)
	}
	label := func(s string) func(e *tlv.Encoder, tag tlv.Tag) {
		return func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, s) }
	}
	if status, err := viewer.Write(ctx, attr(0, clusterBasicInformation, attrNodeLabel), label("viewer")); err != nil || status != im.StatusUnsupportedAccess {
		t.Errorf("viewer NodeLabel write = %s, %v; want UNSUPPORTED_ACCESS", status, err)
	}
	if status, err := admin.Write(ctx, attr(0, clusterBasicInformation, attrNodeLabel), label("admin")); err != nil || status != im.StatusSuccess {
		t.Errorf("administrator NodeLabel write = %s, %v; want SUCCESS", status, err)
	}
}

// TestSubscriptionSurvivesRestart: a CASE subscription outlives Stop and a
// new node on the same database. The new node re-establishes it (ADR
// 0008): it opens CASE to the controller and sends the priming report
// under the old SubscriptionID, and later changes arrive on it.
func TestSubscriptionSurvivesRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	first, c, s := commissioned(t, minimalConfig(openDB(t, path)))
	if err := first.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	sub, err := s.Subscribe(ctx, 0, 60, attr(1, clusterDescriptor, attrPartsList))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("SubscribeResponse without a SubscriptionID")
	}
	waitPersisted(t, first)
	if err := first.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	second, err := node.New(minimalConfig(openDB(t, path)))
	if err != nil {
		t.Fatalf("New on the same database: %v", err)
	}
	t.Cleanup(func() { _ = second.Stop(context.Background()) })
	// The node finds its controllers over DNS-SD; here the commissioner
	// answers for itself.
	second.Bridge().AttachOperationalResolver(c)
	if err := second.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	resumed, err := c.Accept(ctx)
	if err != nil {
		t.Fatalf("the restarted node opened no CASE session: %v", err)
	}
	priming, err := resumed.NextReport(ctx)
	if err != nil {
		t.Fatalf("no priming report: %v", err)
	}
	if priming.SubscriptionID != sub.ID {
		t.Fatalf("priming report on subscription %d, want %d", priming.SubscriptionID, sub.ID)
	}
	plugEP, ok := endpointOf(t, second, "plug:1")
	if !ok {
		t.Fatal("plug:1 not in the topology")
	}
	if v, ok := priming.Value(1, clusterDescriptor, attrPartsList); !ok || len(v.Children) != 1 || v.Children[0].Uint != uint64(plugEP) {
		t.Errorf("priming PartsList %+v (%v), want [%d]", v, ok, plugEP)
	}
	if err := second.Aggregator().Add(ctx, newPlug(t, "plug:2")); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	report, err := resumed.NextReport(deadline)
	if err != nil {
		t.Fatalf("no report after the Aggregator changed: %v", err)
	}
	if report.SubscriptionID != sub.ID {
		t.Errorf("report on subscription %d, want %d", report.SubscriptionID, sub.ID)
	}
	if v, ok := report.Value(1, clusterDescriptor, attrPartsList); !ok || len(v.Children) != 2 {
		t.Errorf("reported PartsList %+v (%v), want two endpoints", v, ok)
	}
}

// TestRemoveFabricDropsSession: RemoveFabric over CASE answers OK, the
// fabric is gone from Fabrics, and the session it arrived on is dropped —
// a further read on it is not answered.
func TestRemoveFabricDropsSession(t *testing.T) {
	t.Parallel()
	n, c, s := commissioned(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	status, err := c.RemoveFabric(ctx, s)
	if err != nil || status != 0 {
		t.Fatalf("RemoveFabric = %d, %v; want NOCResponse OK", status, err)
	}
	fabrics, err := n.Fabrics(ctx)
	if err != nil || len(fabrics) != 0 {
		t.Fatalf("Fabrics() after RemoveFabric = %+v, %v", fabrics, err)
	}
	short, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if _, err := s.Read(short, attr(0, clusterBasicInformation, attrNodeLabel)); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("read on the removed fabric's session = %v, want no answer", err)
	}
	if !n.CommissioningWindow().IsOpen() {
		t.Error("the node did not reopen its window after its last fabric was removed")
	}
}

// TestRemoveFabricClosesSessionsWithCloseSession: RemoveFabric tells every
// other session of the fabric with a CloseSession before the NOCResponse,
// and closes the session that carried it after the response without one —
// matter.js Fabric.leave (Fabric.ts:423-432): the other sessions close at
// once and their close sends CloseSession (NodeSession.ts:384-393,
// ExchangeManager.ts:693, :716-726); the invoking session's close waits for
// its exchange and is then a force close, which sends nothing
// (NodeSession.ts:337-352, Fabric.ts:461-465). The fabric and its ACL are
// gone afterwards, as before.
func TestRemoveFabricClosesSessionsWithCloseSession(t *testing.T) {
	t.Parallel()
	n, c, s := commissioned(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	ctx := context.Background()
	fabricIndex := c.FabricIndex()
	if acl, err := n.Store().ListACL(ctx, fabricIndex); err != nil || len(acl) == 0 {
		t.Fatalf("ACL of the commissioned fabric = %+v, %v; want the commissioner's entry", acl, err)
	}
	second, err := c.CASEAs(ctx, otherAdminNode)
	if err != nil {
		t.Fatalf("second CASE session: %v", err)
	}
	// One message on it, so the device has a route to tell it on.
	first, cancelFirst := context.WithTimeout(ctx, 5*time.Second)
	_, _ = second.Read(first, attr(0, clusterBasicInformation, attrNodeLabel))
	cancelFirst()

	status, err := c.RemoveFabric(ctx, s)
	if err != nil || status != 0 {
		t.Fatalf("RemoveFabric = %d, %v; want NOCResponse OK", status, err)
	}
	// The other session's CloseSession went out before the response.
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_, err = second.Read(short, attr(0, clusterBasicInformation, attrNodeLabel))
	cancel()
	if !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Errorf("read on the fabric's other session after RemoveFabric = %v, want ErrSessionClosed (a CloseSession)", err)
	}
	// The invoking session: closed after its response, without a CloseSession.
	time.Sleep(300 * time.Millisecond)
	short, cancel = context.WithTimeout(ctx, 1500*time.Millisecond)
	_, err = s.Read(short, attr(0, clusterBasicInformation, attrNodeLabel))
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("read on the invoking session after RemoveFabric = %v, want no answer and no CloseSession", err)
	}
	fabrics, err := n.Fabrics(ctx)
	if err != nil || len(fabrics) != 0 {
		t.Fatalf("Fabrics() after RemoveFabric = %+v, %v", fabrics, err)
	}
	if acl, err := n.Store().ListACL(ctx, fabricIndex); err != nil || len(acl) != 0 {
		t.Errorf("ACL of the removed fabric = %+v, %v; want none", acl, err)
	}
}

// waitPersisted waits until n has stored its subscription for
// re-establishment, which it does after it sent the SubscribeResponse: a
// Stop before that ends the subscription with the run.
func waitPersisted(t *testing.T, n *node.Node) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, err := n.Store().LoadServerSubscriptions(context.Background())
		if err == nil && len(rows) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the subscription was not persisted: %d rows, %v", len(rows), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
