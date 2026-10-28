// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command reference-controller is the reference controller with BLE through
// ESPHome Bluetooth proxies: examples/reference-controller's command line,
// plus pair --bluetooth-proxy host[:port][,key] (repeatable) and
// --bluetooth-proxy-discover with --bluetooth-proxy-key name=key
// (repeatable).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/examples/reference-controller/cli"

	"github.com/SukramJ/go-fabric/ble/esphome"
)

// discovering is a central over discovered proxies: closing it closes its
// DNS-SD cache too.
type discovering struct {
	*esphome.Central
	names *discovery.Names
}

func (d discovering) Close() error {
	err := d.Central.Close()
	return errors.Join(err, d.names.Close())
}

// proxies is the central over pair's proxy flags.
func proxies(flags cli.ProxyFlags, log *slog.Logger) (ble.Central, error) {
	ps := make([]esphome.Proxy, 0, len(flags.Proxies))
	for _, s := range flags.Proxies {
		p, err := esphome.ParseProxy(s)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	opts := esphome.Options{Logger: log, ClientInfo: "go-fabric reference-controller"}
	if !flags.Discover {
		return esphome.NewCentral(ps, opts)
	}
	keys := esphome.Keys{}
	for _, s := range flags.Keys {
		name, key, err := esphome.ParseKey(s)
		if err != nil {
			return nil, err
		}
		keys[name] = key
	}
	names, err := discovery.Open(log)
	if err != nil {
		return nil, err
	}
	opts.Discover = &esphome.DiscoverOptions{Browser: names, Keys: keys}
	c, err := esphome.NewCentral(ps, opts)
	if err != nil {
		_ = names.Close()
		return nil, err
	}
	return discovering{Central: c, names: names}, nil
}

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, cli.Options{Proxies: proxies}); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "reference-controller:", err)
		}
		os.Exit(1)
	}
}
