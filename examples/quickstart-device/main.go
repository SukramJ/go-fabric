// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command quickstart-device is the smallest Matter bridge on go-fabric: one
// on/off light under the node's Aggregator, its state in one SQLite file,
// advertised over DNS-SD. It names the facade only — node, device,
// contract and the light's cluster definition; the node opens the store
// and builds its own advertiser. docs/getting-started/device.md walks
// through it. It pairs with the CSA test identity and is not a product.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
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
	// The zero Network listens on :5540 and advertises over DNS-SD: a nil
	// Config.Advertiser is the node's own mDNS responder, which answers
	// every commissioner's discriminator browse.
	n, err := newNode(ctx, "quickstart-device.db", node.Network{})
	if err != nil {
		return err
	}
	payload, err := n.SetupPayload()
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	fmt.Printf("QR code: %s\nManual code: %s\n", payload.QR, payload.Manual)
	return n.Run(ctx) // Start, wait for ctx, Stop
}

// newNode builds the node on the SQLite file at path — the node opens it
// (foreign keys on, a new file created 0600), applies the schemas and
// closes it on Stop — and adds the light, unstarted.
func newNode(ctx context.Context, path string, network node.Network) (*node.Node, error) {
	n, err := node.New(ctx, node.Config{
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
		Store: node.Store{Path: path},
	})
	if err != nil {
		return nil, err
	}
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light-1"), Name: "Desk lamp"})
	if err == nil {
		light.OnOff().OnChanged(func(s onoffdef.State, c contract.Change) {
			if slices.Contains(c.Attributes, onoffdef.AttrOnOff) {
				slog.Info("light", slog.Bool("on", s.OnOff)) // drive the real lamp here
			}
		})
		err = n.Aggregator().Add(ctx, light)
	}
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, err
	}
	return n, nil
}
