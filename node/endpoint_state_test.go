// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// The store-error rule for the endpoint state (review II, D-store-4,
// R2-P5-apinode-11, R2-P5-port-16; store/doc.go): a change a controller
// causes is stored with the change, and a refused store answers FAILURE
// with the old value served.

const (
	bdbiCluster   = 0x0039
	bdbiNodeLabel = 0x0005
)

// sceneInvoke runs a ScenesManagement command on ep as fabric 1.
func sceneInvoke(t *testing.T, n *node.Node, ep uint16, cmd uint32, fields any) im.InvokeResult {
	t.Helper()
	ctx := im.WithFabricFilter(t.Context(), true, 1)
	return endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Invoke(ctx,
		im.ConcreteCommandPath{Endpoint: ep, Cluster: core.ScenesManagementClusterID, Command: cmd, HasEndpoint: true}, fields)
}

// sceneStatus is a scene command's answer: the IM status when it failed
// there, the response's status otherwise.
func sceneStatus(r im.InvokeResult) im.StatusCode {
	if r.Status != im.StatusSuccess {
		return r.Status
	}
	switch resp := r.Response.(type) {
	case core.SceneStatusResponse:
		return resp.Status
	case core.ViewSceneResponse:
		return resp.Status
	}
	return r.Status
}

// lightNode starts a node over st with one OnOffLight, whose device type
// mandates ScenesManagement, and returns its endpoint.
func lightNode(t *testing.T, st *refusingKeys) (n *node.Node, ep uint16) {
	t.Helper()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n = newNode(t, cfg)
	if err := n.Aggregator().Add(t.Context(), light(t, "lamp", nil)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ep, ok := endpointOf(t, n, "lamp")
	if !ok {
		t.Fatal("the light is not served")
	}
	return n, ep
}

const (
	scenesCmdViewScene  = 0x01
	scenesCmdStoreScene = 0x04
)

// TestRefusedSceneSaveAnswersFailureAndKeepsTheTable: a StoreScene whose
// table the store refuses answers FAILURE, and the table keeps what it
// held — the refused scene is not served, the stored one still is.
func TestRefusedSceneSaveAnswersFailureAndKeepsTheTable(t *testing.T) {
	t.Parallel()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"scenes."}}
	n, ep := lightNode(t, st)
	if s := sceneStatus(sceneInvoke(t, n, ep, scenesCmdStoreScene, core.SceneRef{SceneID: 1})); s != im.StatusSuccess {
		t.Fatalf("StoreScene 1 with the store working: %v", s)
	}
	stored, _, _ := st.GetSetting(t.Context(), "scenes.lamp")
	st.refuse.Store(true)
	if s := sceneStatus(sceneInvoke(t, n, ep, scenesCmdStoreScene, core.SceneRef{SceneID: 2})); s != im.StatusFailure {
		t.Fatalf("StoreScene 2 with the store refusing: %v, want FAILURE", s)
	}
	st.refuse.Store(false)
	if s := sceneStatus(sceneInvoke(t, n, ep, scenesCmdViewScene, core.SceneRef{SceneID: 2})); s != im.StatusNotFound {
		t.Errorf("ViewScene of the refused scene: %v, want NOT_FOUND", s)
	}
	if s := sceneStatus(sceneInvoke(t, n, ep, scenesCmdViewScene, core.SceneRef{SceneID: 1})); s != im.StatusSuccess {
		t.Errorf("ViewScene of the stored scene: %v, want SUCCESS", s)
	}
	if now, _, _ := st.GetSetting(t.Context(), "scenes.lamp"); now != stored {
		t.Errorf("stored table moved by the refused StoreScene: %q, want %q", now, stored)
	}
}

// TestRefusedBridgedNodeLabelSaveAnswersFailure: a controller's write of a
// bridged endpoint's NodeLabel the store refuses answers FAILURE, and the
// endpoint keeps serving the label it had.
func TestRefusedBridgedNodeLabelSaveAnswersFailure(t *testing.T) {
	t.Parallel()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"bridged_node_label."}}
	n, ep := lightNode(t, st)
	path := attrPath(ep, bdbiCluster, bdbiNodeLabel)
	if s := dispatchWrite(t, n, path, "kept"); s != im.StatusSuccess {
		t.Fatalf("NodeLabel write with the store working: %v", s)
	}
	st.refuse.Store(true)
	if s := dispatchWrite(t, n, path, "refused"); s != im.StatusFailure {
		t.Fatalf("NodeLabel write with the store refusing: %v, want FAILURE", s)
	}
	if got := dispatchRead(t, n, path); got != "kept" {
		t.Errorf("NodeLabel after the refused write = %s, want kept", got)
	}
	if v, _, _ := st.Port.GetSetting(t.Context(), "bridged_node_label.lamp"); v != "kept" {
		t.Errorf("stored NodeLabel %q, want kept", v)
	}
}

// TestFailedSceneLoadDoesNotOverwriteTheStoredTable: a scene table the
// store cannot read at the endpoint's first dispatch stays unloaded — a
// StoreScene answers FAILURE and stores nothing over the stored table —
// and once the store reads again the stored scenes are there and a new
// one joins them.
func TestFailedSceneLoadDoesNotOverwriteTheStoredTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"scenes."}}
	n, ep := lightNode(t, st)
	if s := sceneStatus(sceneInvoke(t, n, ep, scenesCmdStoreScene, core.SceneRef{SceneID: 1})); s != im.StatusSuccess {
		t.Fatalf("StoreScene 1: %v", s)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := st.Port.GetSetting(ctx, "scenes.lamp")

	st.refuseRead.Store(true)
	again, ep := lightNode(t, st)
	if s := sceneStatus(sceneInvoke(t, again, ep, scenesCmdStoreScene, core.SceneRef{SceneID: 2})); s != im.StatusFailure {
		t.Fatalf("StoreScene on a table the store could not load: %v, want FAILURE", s)
	}
	if now, _, _ := st.Port.GetSetting(ctx, "scenes.lamp"); now != stored {
		t.Fatalf("the stored table after the unloaded StoreScene = %q, want %q", now, stored)
	}
	st.refuseRead.Store(false)
	if s := sceneStatus(sceneInvoke(t, again, ep, scenesCmdViewScene, core.SceneRef{SceneID: 1})); s != im.StatusSuccess {
		t.Fatalf("ViewScene of the stored scene once the store reads again: %v", s)
	}
	if s := sceneStatus(sceneInvoke(t, again, ep, scenesCmdStoreScene, core.SceneRef{SceneID: 2})); s != im.StatusSuccess {
		t.Fatalf("StoreScene 2 once the store reads again: %v", s)
	}
	if s := sceneStatus(sceneInvoke(t, again, ep, scenesCmdViewScene, core.SceneRef{SceneID: 1})); s != im.StatusSuccess {
		t.Errorf("scene 1 after scene 2 joined it: %v", s)
	}
}
