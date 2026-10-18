// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command reference-controller is the reference controller with BLE over
// BlueZ: examples/reference-controller's command line, plus pair --ble.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/SukramJ/go-fabric/ble/bluez"
	"github.com/SukramJ/go-fabric/examples/reference-controller/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, cli.Options{Central: bluez.NewCentral}); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "reference-controller:", err)
		}
		os.Exit(1)
	}
}
