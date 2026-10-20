// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"github.com/SukramJ/go-fabric/internal/nodeseam"
	"github.com/SukramJ/go-fabric/transport/udp"
)

func init() {
	nodeseam.UseClientListener = func(cfg any, open func() (*udp.Listener, error)) bool {
		c, ok := cfg.(*Config)
		if !ok || c == nil || open == nil {
			return false
		}
		c.Client.open = open
		return true
	}
}
