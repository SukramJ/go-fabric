// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command host-view is a Matter bridge as a host sees go-fabric: two
// devices declared once, a node, the changes a controller makes told
// apart from the host's own. docs/guides/host-view.md explains every
// line. It pairs with the CSA test identity and is not a product.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
)

// key is the host's stable key of a device: the device keeps its endpoint
// number under it across restarts.
type key string

func (k key) String() string { return string(k) }

// bridge is the host's two devices.
type bridge struct {
	light *device.DimmableLight
	door  *device.ContactSensor
}

// newBridge declares the devices. switched hears every change of the
// light's OnOff that a controller made — not the host's own.
func newBridge(switched func(on bool)) (*bridge, error) {
	light, err := device.NewDimmableLight(device.Options{StableKey: key("light"), Name: "Kitchen light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}},
		levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(128))}})
	if err != nil {
		return nil, err
	}
	door, err := device.NewContactSensor(device.Options{StableKey: key("door"), Name: "Front door"},
		booleanstatedef.Declare{Initial: booleanstatedef.Patch{StateValue: new(true)}})
	if err != nil {
		return nil, err
	}
	light.OnOff().OnChanged(func(s onoffdef.State, c contract.Change) {
		if c.Origin != contract.OriginHostSet {
			switched(s.OnOff) // drive the real lamp here
		}
	})
	return &bridge{light: light, door: door}, nil
}

// doorMoved reports the door sensor's new state: the host's own change.
func (b *bridge) doorMoved(closed bool) error {
	return b.door.BooleanState().Set(booleanstatedef.Patch{StateValue: &closed})
}

// newNode builds the node with both devices under its Aggregator.
func newNode(ctx context.Context, cfg node.Config, b *bridge) (*node.Node, error) {
	n, err := node.New(cfg) //nolint:contextcheck // the node's constructor takes no context
	if err != nil {
		return nil, err
	}
	for _, d := range []node.Endpoint{b.light, b.door} {
		if err := n.Aggregator().Add(ctx, d); err != nil {
			_ = n.Stop(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	n.Aggregator().MarkComplete() //nolint:contextcheck // the node's own: every device is added
	return n, nil
}

// config is the node's identity and its SQLite file; change the
// commissioning values for anything real.
func config(path string) node.Config {
	return node.Config{
		Commissioning: node.Commissioning{
			Passcode: 20202021, Discriminator: 3840,
			PBKDFIterations: 1000, Salt: []byte("go-fabric-host-view"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8002}, // CSA test vendor
		BasicInformation: node.BasicInformation{
			VendorName: "host-view", ProductName: "host-view",
			NodeLabel: "Host view bridge", SerialNumber: "HOSTVIEW-0001",
		},
		Store: node.Store{Path: path},
	}
}

func run(ctx context.Context) error {
	b, err := newBridge(func(on bool) { slog.Info("a controller switched the light", slog.Bool("on", on)) })
	if err != nil {
		return err
	}
	n, err := newNode(ctx, config("host-view.db"), b)
	if err != nil {
		return err
	}
	if err := n.Start(ctx); err != nil {
		return err
	}
	for _, d := range n.Topology().Devices {
		slog.Info("bridged", slog.String("name", d.Name), slog.Int("endpoint", int(d.Endpoint)))
	}
	b.light.Rename(ctx, "Kitchen ceiling")
	b.door.SetReachable(ctx, true)
	if err := b.doorMoved(false); err != nil {
		slog.Warn("door", slog.Any("err", err))
	}
	if qr, manual, err := n.PairingCodes(); err == nil {
		fmt.Printf("QR code: %s\nManual code: %s\n", qr, manual)
	}
	<-ctx.Done()
	return n.Stop(context.WithoutCancel(ctx))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
