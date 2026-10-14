// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
)

// TestLifecycle: New needs an interaction; a disabled node does not start
// (ClientNetworkRuntime.ts:41-43); Enable starts it again
// (ClientNode.ts:257-264); a closed node does not start.
func TestLifecycle(t *testing.T) {
	t.Parallel()
	if _, err := client.New(client.Config{ID: "peer1"}); !errors.Is(err, client.ErrConfig) {
		t.Fatalf("New without an interaction: %v", err)
	}
	ctx := context.Background()
	n := newScriptedNode(t, client.Config{ID: "peer1", Hooks: client.Hooks{ConnectionStateChanged: func(client.ConnectionState) {}}}).node
	if n.ID() != "peer1" || n.ConnectionState() != client.Disconnected || n.ConnectionState().String() != "Disconnected" {
		t.Fatalf("fresh node %q %v", n.ID(), n.ConnectionState())
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Disable(ctx); err != nil || !n.Disabled() {
		t.Fatalf("Disable: %v", err)
	}
	if err := n.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); !errors.Is(err, client.ErrDisabled) {
		t.Fatalf("Start of a disabled node: %v", err)
	}
	if err := n.Enable(ctx); err != nil || n.Disabled() {
		t.Fatalf("Enable: %v", err)
	}
	if err := n.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); !errors.Is(err, client.ErrClosed) {
		t.Fatalf("Start of a closed node: %v", err)
	}
	for s, want := range map[client.ConnectionState]string{
		client.Connected: "Connected", client.Reconnecting: "Reconnecting",
		client.WaitingForDeviceDiscovery: "WaitingForDeviceDiscovery", 9: "ConnectionState(9)",
	} {
		if s.String() != want {
			t.Fatalf("%d is %q", s, s.String())
		}
	}
}

// TestSettingsPersisted: disabled, autoSubscribe and the default
// subscription are stored with the image (NetworkClient.ts:331-372) and
// read back by the next node on the store; Erase removes them.
func TestSettingsPersisted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &client.MemoryStore{}
	first := newScriptedNode(t, client.Config{ID: "peer1", Store: st, AutoSubscribe: true, DefaultSubscription: &client.SubscribeOptions{NoFabricFilter: true}}).node
	if err := first.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	second := newScriptedNode(t, client.Config{ID: "peer1", Store: st}).node
	if !second.Disabled() {
		t.Fatal("disabled was not persisted")
	}
	if err := second.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if eps, _ := st.Load("peer1"); len(eps) != 0 {
		t.Fatalf("Erase left %+v", eps)
	}
	bad := &client.MemoryStore{}
	_ = bad.Save("peer1", 0, 0xFFFF_FFFF, map[uint32][]byte{0: {0x04, 0x01}}, nil)
	s := newScripted(t)
	if _, err := client.New(client.Config{ID: "peer1", Store: bad, Interaction: interaction.New(s.mgr, s.session, nil)}); err == nil {
		t.Fatal("New loaded malformed settings")
	}
}
