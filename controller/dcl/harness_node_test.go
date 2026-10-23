// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

func init() { dcl.SetHarnessNode(startNode) }

// startNode starts a node.Node on loopback with chain (nil: the CSA test
// chain) for the commission tests of commission_test.go.
func startNode(t *testing.T, chain *secattestation.Chain) *net.UDPAddr {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "node.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Advertiser:         mdns.NewNoop(),
		Commissioning:      node.Commissioning{Passcode: 20202021, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "dcl test", NodeLabel: "dcl", SerialNumber: "DCL-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Attestation: node.Attestation{Chain: chain},
		Store:       node.Store{DB: db},
		Logger:      slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return addr
}
