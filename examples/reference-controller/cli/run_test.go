// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import "io"

// run is Run as the build without BLE runs it.
func run(args []string, stdin io.Reader, stdout io.Writer) error {
	return Run(args, stdin, stdout, Options{})
}
