// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command ble-wifi-light is a Matter node on Linux that is commissioned over
// Bluetooth LE and joins the Wi-Fi network its commissioner provisions: one
// on/off light under the node's Aggregator, its state in one SQLite file.
// It composes the two radio modules — ble/bluez as node.Network.BLE and
// wifi/wpasupplicant as node.Network.WiFi's Driver — which neither the main
// module nor either library module imports. It pairs with the CSA test
// identity and is not a product.
//
// The host runs bluetoothd and wpa_supplicant (with its D-Bus interface),
// leaves the Wi-Fi interface unmanaged by NetworkManager, and lets the
// process use both services on the system bus (docs/guides/operations.md,
// "BLE commissioning" and "Wi-Fi provisioning").
//
//	ble-wifi-light --adapter hci0 --wifi-interface wlan0 --storage light.db
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/SukramJ/go-fabric/ble/bluez"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/wifi/wpasupplicant"
)

// options are the command's flags.
type options struct {
	adapter       string
	wifiInterface string
	noWiFi        bool
	storage       string
	passcode      uint
	discriminator uint
	logLevel      slog.Level
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("ble-wifi-light", flag.ContinueOnError)
	fs.StringVar(&o.adapter, "adapter", "hci0", "the Bluetooth adapter (BlueZ)")
	fs.StringVar(&o.wifiInterface, "wifi-interface", wpasupplicant.DefaultInterface, "the Wi-Fi interface wpa_supplicant manages")
	fs.BoolVar(&o.noWiFi, "no-wifi", false, "no Wi-Fi: the node serves the Ethernet NetworkCommissioning and is reached over the host's present network (a test without a Wi-Fi radio)")
	fs.StringVar(&o.storage, "storage", "ble-wifi-light.db", "the SQLite file of the node's state")
	fs.UintVar(&o.passcode, "passcode", 20202021, "the setup passcode")
	fs.UintVar(&o.discriminator, "discriminator", 3840, "the 12-bit discriminator")
	level := fs.String("log-level", "info", "debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if err := o.logLevel.UnmarshalText([]byte(*level)); err != nil {
		return options{}, fmt.Errorf("--log-level: %w", err)
	}
	if o.passcode > 99999998 {
		return options{}, fmt.Errorf("--passcode %d is not an 8-digit passcode", o.passcode)
	}
	if o.discriminator > 0xFFF {
		return options{}, fmt.Errorf("--discriminator %d is wider than 12 bits", o.discriminator)
	}
	return o, nil
}

func main() {
	o, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: o.logLevel})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, o, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run opens the two radios, builds the node on them and runs it until ctx
// ends.
func run(ctx context.Context, o options, out io.Writer) error {
	radio, err := bluez.NewPeripheral(o.adapter) //nolint:contextcheck // the constructor takes no context; the radio lives until Close
	if err != nil {
		return fmt.Errorf("bluetooth adapter %s: %w", o.adapter, err)
	}
	if c, ok := radio.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	var wifi contract.WiFiDriver
	if !o.noWiFi {
		d, err := wpasupplicant.New(ctx, wpasupplicant.Options{Interface: o.wifiInterface})
		if err != nil {
			return fmt.Errorf("wi-fi interface %s: %w", o.wifiInterface, err)
		}
		defer func() { _ = d.Close() }()
		wifi = d
	}

	// The zero Network listens on :5540 on every interface and advertises
	// over DNS-SD on the interfaces as they come; the Wi-Fi interface's
	// addresses join once the node is on the network.
	n, err := newNode(ctx, o, node.Network{}, radio, wifi)
	if err != nil {
		return err
	}
	payload, err := n.SetupPayload()
	if err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	_, _ = fmt.Fprintf(out, "QR code: %s\nManual code: %s\n", payload.QR, payload.Manual)
	return n.Run(ctx) // Start, wait for ctx, Stop
}

// newNode builds the node on o's storage with the BLE radio and the Wi-Fi
// driver (none: the Ethernet NetworkCommissioning), and adds the light,
// unstarted.
func newNode(ctx context.Context, o options, network node.Network, radio contract.BLEPeripheral, wifi contract.WiFiDriver) (*node.Node, error) {
	network.BLE = radio
	if wifi != nil {
		network.WiFi = &node.WiFi{Driver: wifi}
	}
	n, err := node.New(ctx, node.Config{
		Network: network,
		Commissioning: node.Commissioning{
			Passcode: uint32(o.passcode), Discriminator: uint16(o.discriminator), //nolint:gosec // parseFlags bounds both
			PBKDFIterations: 1000, Salt: []byte("go-fabric-ble-wifi-light"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001}, // CSA test vendor
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "ble-wifi-light",
			NodeLabel: "BLE Wi-Fi light", SerialNumber: "BLE-WIFI-LIGHT-0001",
		},
		Store: node.Store{Path: o.storage},
	})
	if err != nil {
		return nil, err
	}
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light-1"), Name: "Light"})
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
