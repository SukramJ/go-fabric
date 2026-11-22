// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/cluster/jointfabric"
	"thingfabric.dev/go-fabric/cluster/spec"
	jfdsdef "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/surface"
)

// The --jointfabric-sim administrator (jointfabric.go) after
// connectedhomeip's jf-admin-app at the harness pin 6170af84.

// csrKey parses a PKCS#10 request, checks its signature and returns its
// P-256 key.
func csrKey(t *testing.T, der []byte) *ecdsa.PublicKey {
	t.Helper()
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil || csr.CheckSignature() != nil {
		t.Fatalf("the ICAC CSR does not verify: %v", err)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		t.Fatalf("the ICAC CSR's key is %T, want P-256", csr.PublicKey)
	}
	return pub
}

// TestJointFabricSimKeepsItsAdministratorKey: the administrator hands the
// CSR of one P-256 key, kept in the settings across a restart — the key
// a joint-fabric controller checks the CSR against
// (examples/jf-admin-app/linux/JFAManager.cpp:393-406 at 6170af84,
// OnSendICACSRRequestResponse: VerifyCertificateSigningRequest, then the
// key must match the peer administrator's ICAC key).
func TestJointFabricSimKeepsItsAdministratorKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	cfg := node.Config{}
	applyJointFabric(&cfg)
	if cfg.Client == nil {
		t.Fatal("--jointfabric-sim left the node's client stack off")
	}
	client := cfg.Client
	applyJointFabric(&cfg)
	if cfg.Client != client {
		t.Fatal("applyJointFabric replaced a configured client stack")
	}

	j := newDemoJointFabric(surface.Policies{}, logger)
	j.attach(st)
	eps, err := j.devices()
	if err != nil || len(eps) != 1 {
		t.Fatalf("devices = %d endpoints, %v; want the administrator endpoint", len(eps), err)
	}
	if again, _ := j.devices(); again[0] != eps[0] {
		t.Fatal("a second devices call built a second endpoint")
	}
	first, err := j.IcacCsr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := csrKey(t, first)
	second, err := j.IcacCsr(ctx)
	if err != nil || !csrKey(t, second).Equal(key) {
		t.Fatalf("a second CSR = %v; want the same key", err)
	}

	restarted := newDemoJointFabric(surface.Policies{}, logger)
	restarted.attach(st)
	csr, err := restarted.IcacCsr(ctx)
	if err != nil || !csrKey(t, csr).Equal(key) {
		t.Fatalf("the CSR after a restart = %v; want the kept key", err)
	}

	// A broken setting is a new key, and without settings the key lives
	// as long as the daemon.
	if err := st.SetSetting(ctx, jfKeySetting, "not base64"); err != nil {
		t.Fatal(err)
	}
	broken := newDemoJointFabric(surface.Policies{}, logger)
	broken.attach(st)
	if csr, err := broken.IcacCsr(ctx); err != nil || csrKey(t, csr).Equal(key) {
		t.Fatalf("the CSR over a broken setting = %v; want a new key", err)
	}
	volatile := newDemoJointFabric(surface.Policies{}, logger)
	if _, err := volatile.devices(); err != nil {
		t.Fatal(err)
	}
	if csr, err := volatile.IcacCsr(ctx); err != nil || csrKey(t, csr) == nil {
		t.Fatalf("a CSR without settings: %v", err)
	}
}

// TestJointFabricSimSyncReachesNoNode: the datastore's member-node
// synchronisation reaches no node — each request succeeds and each fetch
// is empty, so a RefreshNode through the datastore server finds a node
// that exposes nothing.
func TestJointFabricSimSyncReachesNoNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	j := newDemoJointFabric(surface.Policies{}, slog.New(slog.DiscardHandler))
	j.attach(openTestStore(t))
	if _, err := j.devices(); err != nil {
		t.Fatal(err)
	}
	ds := j.dev.JointFabricDatastore().Underlying()
	// A request reaches the datastore from its anchor fabric only (owner
	// decision 129); the demo is commissioned by no anchor, so the test
	// records fabric 1 as the host would.
	if d, ok := ds.(*jointfabric.Datastore); !ok {
		t.Fatalf("the datastore server is %T", ds)
	} else {
		d.AttachNode(oneFabric{store.FabricRecord{FabricIndex: 1, FabricID: 1, RootPublicKey: []byte{4, 1}}}, nil)
		if err := d.SetAnchorFabricIndex(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(cmdID uint32, req any) im.StatusCode {
		_, err := ds.MatterInvoke(im.WithFabricFilter(ctx, true, 1), cmdID, req)
		return spec.StatusOf(err)
	}
	if s := invoke(jfdsdef.CmdAddPendingNode, jfdsdef.AddPendingNodeRequest{NodeId: 7, FriendlyName: "member"}); s != im.StatusSuccess {
		t.Fatalf("AddPendingNode: %v", s)
	}
	if s := invoke(jfdsdef.CmdRefreshNode, jfdsdef.RefreshNodeRequest{NodeId: 7}); s != im.StatusSuccess {
		t.Fatalf("RefreshNode: %v", s)
	}
	st := j.dev.JointFabricDatastore().Get()
	if len(st.NodeList) != 1 || len(st.NodeEndpointList) != 0 || len(st.NodeAclList) != 0 || len(st.NodeKeySetList) != 0 {
		t.Fatalf("after RefreshNode: %d nodes, %d endpoints, %d ACLs, %d key sets; want the node and nothing else",
			len(st.NodeList), len(st.NodeEndpointList), len(st.NodeAclList), len(st.NodeKeySetList))
	}

	s := jfSync{logger: slog.New(slog.DiscardHandler)}
	for name, err := range map[string]error{
		"SyncEndpointGroupID":  s.SyncEndpointGroupID(ctx, 7, jfdsdef.DatastoreEndpointGroupIDEntryStruct{}),
		"SyncNodeKeySet":       s.SyncNodeKeySet(ctx, 7, jfdsdef.DatastoreNodeKeySetEntryStruct{}),
		"SyncBindings":         s.SyncBindings(ctx, 7, nil),
		"SyncEndpointBindings": s.SyncEndpointBindings(ctx, 7, 1, nil),
		"SyncACL":              s.SyncACL(ctx, 7, jfdsdef.DatastoreACLEntryStruct{}),
		"SyncACLs":             s.SyncACLs(ctx, 7, nil),
		"SyncGroupKeySet":      s.SyncGroupKeySet(ctx, 7, jfdsdef.DatastoreGroupKeySetStruct{}),
	} {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if ks, err := s.FetchGroupKeySet(ctx, 7, 1); err != nil || ks.GroupKeySetId != 0 {
		t.Fatalf("FetchGroupKeySet = %+v, %v; want nothing", ks, err)
	}
}

// oneFabric is a fabric table of one fabric.
type oneFabric struct{ f store.FabricRecord }

func (o oneFabric) GetFabric(_ context.Context, idx uint8) (store.FabricRecord, error) {
	if idx == o.f.FabricIndex {
		return o.f, nil
	}
	return store.FabricRecord{}, store.ErrFabricNotFound
}

func (o oneFabric) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	return []store.FabricRecord{o.f}, nil
}

func (oneFabric) GetIdentity(context.Context, uint8) (store.IdentityRecord, error) {
	return store.IdentityRecord{}, store.ErrIdentityNotFound
}
