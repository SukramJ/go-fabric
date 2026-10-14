// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/store"
)

// TestNewFabricLoadsOrMints: with a Store, the first NewFabric mints and
// saves; the second loads the same fabric id, root node id, IPK, key and
// NOC; a record without key and NOC mints a NOC under the stored ids; a
// conflicting configuration or another root is ErrFabricMismatch.
func TestNewFabricLoadsOrMints(t *testing.T) {
	t.Parallel()
	mem := &ca.MemoryStore{}
	authority, err := ca.New(ca.Config{Store: mem})
	if err != nil {
		t.Fatal(err)
	}
	cfg := FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1, Label: fabricLabel, Store: mem}
	first, err := NewFabric(authority, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFabric(authority, FabricConfig{Store: mem})
	if err != nil {
		t.Fatal(err)
	}
	if second.FabricID != first.FabricID || second.RootNodeID != first.RootNodeID || second.RootVendorID != 0xFFF1 ||
		second.Label != fabricLabel || !bytes.Equal(second.IPK, first.IPK) || !bytes.Equal(second.Identity.NOC, first.Identity.NOC) ||
		!second.Identity.PrivateKey.Equal(first.Identity.PrivateKey) || second.Identity.IPK != first.Identity.IPK {
		t.Fatal("the loaded fabric differs from the minted one")
	}
	for _, bad := range []FabricConfig{
		{FabricID: testFabricID + 1, Store: mem},
		{RootNodeID: controllerNodeID + 1, Store: mem},
	} {
		if _, err := NewFabric(authority, bad); !errors.Is(err, ErrFabricMismatch) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	other, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFabric(other, FabricConfig{Store: mem}); !errors.Is(err, ErrFabricMismatch) {
		t.Fatalf("another root: %v", err)
	}

	// A record of ids only: NewFabric mints a key and NOC and saves them.
	idsOnly := &ca.MemoryStore{}
	ipk := bytes.Repeat([]byte{3}, 16)
	_ = idsOnly.SaveFabric(ca.FabricRecord{FabricID: 9, RootNodeID: 7, IPK: ipk})
	f, err := NewFabric(authority, FabricConfig{Store: idsOnly})
	if err != nil {
		t.Fatal(err)
	}
	if f.FabricID != 9 || f.RootNodeID != 7 || f.FabricIndex != 1 || !bytes.Equal(f.IPK, ipk) {
		t.Fatalf("fabric %+v", f)
	}
	if rec, _, _ := idsOnly.LoadFabric(); rec.KeyPair == nil || !bytes.Equal(rec.NOC, f.Identity.NOC) {
		t.Fatal("the minted key and NOC were not saved")
	}
	_ = idsOnly.SaveFabric(ca.FabricRecord{FabricID: 9, RootNodeID: 7})
	if _, err := NewFabric(authority, FabricConfig{Store: idsOnly}); !errors.Is(err, ErrFabricMismatch) {
		t.Fatalf("incomplete record: %v", err)
	}
	_ = idsOnly.SaveFabric(ca.FabricRecord{FabricID: 9, RootNodeID: 7, IPK: ipk, KeyPair: &ca.KeyPair{PrivateKey: []byte{1}}, NOC: []byte{1}})
	if _, err := NewFabric(authority, FabricConfig{Store: idsOnly}); !errors.Is(err, ErrFabricMismatch) {
		t.Fatalf("bad key: %v", err)
	}
}

// TestFabricPersistsAcrossRestart: a controller whose CA and fabric live
// in a sqlite database (store.Store settings) commissions the node, is
// closed, is opened again from the same database, and reads from the node
// over CASE without commissioning it again — the node still trusts the
// same root and root node id. Not parallel (brief AT §2).
func TestFabricPersistsAcrossRestart(t *testing.T) {
	_, addr := startNode(t, nil, "127.0.0.1:0")
	path := filepath.Join(t.TempDir(), "controller.db")
	open := func() (*controller, func()) {
		db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Apply(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		cs := ca.SettingStore{Settings: store.New(db)}
		authority, err := ca.New(ca.Config{Store: cs})
		if err != nil {
			t.Fatal(err)
		}
		conn := newFakeMDNS()
		c, closeStack := newControllerWith(t, discovery.NewNames(conn, discard()), "127.0.0.1:0", authority,
			FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1, Label: fabricLabel, Store: cs})
		c.announceNode(t, conn, addr.Port, commissionedNode)
		return c, func() { closeStack(); _ = db.Close() }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, closeFirst := open()
	dev := c.find(t)
	res, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	rootKey := c.fabric.RootPublicKey
	ipk := c.fabric.IPK
	closeFirst()

	c2, closeSecond := open()
	defer closeSecond()
	if !bytes.Equal(c2.fabric.RootPublicKey, rootKey) || !bytes.Equal(c2.fabric.IPK, ipk) ||
		c2.fabric.FabricID != testFabricID || c2.fabric.RootNodeID != controllerNodeID {
		t.Fatal("the reopened fabric is not the same fabric")
	}
	p, err := c2.set.For(res.Address)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.Connect(ctx, peer.ConnectOptions{})
	if err != nil {
		t.Fatalf("CASE after the restart: %v", err)
	}
	report, err := interaction.New(c2.mgr, s, nil).Read(ctx, rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel))
	if err != nil {
		t.Fatalf("Read over CASE: %v", err)
	}
	v, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	if label, _ := spec.DecodeString(v); !ok || label != "commissioned node" {
		t.Fatalf("NodeLabel %q", label)
	}
}
