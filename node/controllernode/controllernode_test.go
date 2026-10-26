// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllernode_test

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node/controllernode"
)

// TestStartRunsTheOwnNode: Start runs a node with the OTA provider
// endpoint and its BDX responder on loopback, as the controller's port
// sees it; a fresh node holds no fabric; Node unwraps it.
func TestStartRunsTheOwnNode(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "c.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mgr, err := softwareupdate.New(softwareupdate.Config{Registry: &client.Registry{}, Images: softwareupdate.NewImages(softwareupdate.ImagesConfig{})})
	if err != nil {
		t.Fatal(err)
	}
	spec := controller.OwnNodeSpec{
		NodeID: 0x42, Listen: "[::1]:0", Advertiser: mdns.NewNoop(),
		Passcode: 20202021, Discriminator: 3840, Salt: make([]byte, 32), Iterations: 1000,
		VendorID: 0xFFF1, ProductID: 0x8000, SerialNumber: "0000000000000042", DB: db,
		Provider: mgr.ProviderConfig(), Logger: slog.New(slog.DiscardHandler),
	}
	ctx := context.Background()
	own, addr, err := controllernode.Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = own.Stop(ctx) })
	if addr == nil || addr.IP.IsUnspecified() || addr.Port == 0 {
		t.Errorf("address %v, want a loopback socket", addr)
	}
	if _, ok := own.OTAProviderEndpoint(); !ok || own.BDX() == nil {
		t.Error("no OTA provider endpoint or BDX responder")
	}
	if controllernode.Node(own) == nil {
		t.Error("Node does not unwrap the own node")
	}
	if held, err := own.HoldsFabric(ctx, 1, 0x42, []byte{4}); err != nil || held {
		t.Errorf("HoldsFabric = %v, %v; want false on a fresh node", held, err)
	}
}
