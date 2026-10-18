// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// OpenWindow opens a basic commissioning window on the device with its own
// passcode and discriminator ([Device.Passcode], [Device.Discriminator]),
// for timeout (0 is the node's default) — matter.js's
// ServerNode.openCommissioningWindow, which a test calls to commission a
// device a second time onto another controller's fabric. It is a local
// action ([Device.Act], node.Node.OpenBasicWindow); the window a
// controller opens over the wire is AdministratorCommissioning's.
func (d *Device) OpenWindow(ctx context.Context, timeout time.Duration) error {
	return d.Act(func(n *node.Node) error {
		return n.OpenBasicWindow(ctx, node.WindowCredentials{
			Discriminator: d.Discriminator(), Passcode: d.Passcode(),
			Iterations: DefaultPBKDFIterations, Salt: defaultSalt, Timeout: timeout,
		})
	})
}
