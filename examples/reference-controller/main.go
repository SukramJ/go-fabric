// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command reference-controller is the module's controller role as a
// command-line tool (package cli beside this file), built without BLE:
// pair --ble answers that this build has none. The BLE build is
// ble/bluez/cmd/reference-controller.
//
// It is an example, not API (README.md §API stability): copy from it, do
// not build against it. README.md beside this file lists every command.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/SukramJ/go-fabric/examples/reference-controller/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, cli.Options{}); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "reference-controller:", err)
		}
		os.Exit(1)
	}
}
