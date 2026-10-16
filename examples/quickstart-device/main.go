// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command quickstart-device is the smallest Matter bridge on go-fabric: one
// on/off light under the node's Aggregator, its state in one SQLite file,
// advertised over DNS-SD. docs/getting-started/device.md walks through it.
// It pairs with the CSA test identity and is not a product.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	_ "modernc.org/sqlite" // the host picks the SQLite driver

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	db, err := openDB("quickstart-device.db")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// DNS-SD: the subtype responder answers the discriminator-filtered
	// browse every commissioner sends for a QR or manual code.
	zc := mdns.NewZeroconf()
	if r, err := mdns.NewSubtypeResponder(slog.Default()); err == nil {
		r.Start(ctx)
		zc.AttachSubtypeResponder(r)
		defer func() { _ = r.Close() }()
	}

	n, err := newNode(ctx, db, node.Network{}, zc)
	if err != nil {
		return err
	}
	qr, manual, err := n.PairingCodes()
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	fmt.Printf("QR code: %s\nManual code: %s\n", qr, manual)
	return n.Run(ctx) // Start, wait for ctx, Stop
}

// openDB opens the SQLite file; foreign_keys carries the store's
// cascade-delete on fabric removal. node.New applies the schemas.
func openDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
}

// newNode builds the node and adds the light, unstarted.
func newNode(ctx context.Context, db *sql.DB, network node.Network, adv mdns.Advertiser) (*node.Node, error) {
	n, err := node.New(node.Config{ //nolint:contextcheck // the node's constructor takes no context
		Network: network,
		Commissioning: node.Commissioning{ // change all four for anything real
			Passcode: 20202021, Discriminator: 3840,
			PBKDFIterations: 1000, Salt: []byte("go-fabric-quickstart"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001}, // CSA test vendor
		BasicInformation: node.BasicInformation{
			VendorName: "quickstart", ProductName: "quickstart-device",
			NodeLabel: "Quickstart bridge", SerialNumber: "QUICKSTART-0001",
		},
		Store:      node.Store{DB: db},
		Advertiser: adv,
	})
	if err != nil {
		return nil, err
	}
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light-1"), Name: "Desk lamp"})
	if err == nil {
		light.OnChanged(onoff.ClusterID, func(attrs []uint32) {
			if slices.Contains(attrs, onoff.AttrOnOff) {
				slog.Info("light", slog.Bool("on", light.OnOff().OnOff())) // drive the real lamp here
			}
		})
		err = n.Aggregator().Add(ctx, light)
	}
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, err
	}
	n.Aggregator().MarkComplete() //nolint:contextcheck // the node's own; every device is added: stale endpoint numbers may go
	return n, nil
}
