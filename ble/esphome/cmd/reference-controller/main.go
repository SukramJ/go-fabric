// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command reference-controller is the reference controller with BLE through
// ESPHome Bluetooth proxies: examples/reference-controller's command line,
// plus pair --bluetooth-proxy host[:port][,key] (repeatable).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/examples/reference-controller/cli"

	"github.com/SukramJ/go-fabric/ble/esphome"
)

// proxies is the central over the --bluetooth-proxy specs.
func proxies(specs []string, log *slog.Logger) (ble.Central, error) {
	ps := make([]esphome.Proxy, 0, len(specs))
	for _, s := range specs {
		p, err := esphome.ParseProxy(s)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return esphome.NewCentral(ps, esphome.Options{Logger: log, ClientInfo: "go-fabric reference-controller"})
}

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, cli.Options{Proxies: proxies}); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "reference-controller:", err)
		}
		os.Exit(1)
	}
}
