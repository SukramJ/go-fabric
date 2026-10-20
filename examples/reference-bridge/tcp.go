// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"strings"

	"github.com/SukramJ/go-fabric/node"
)

// parseTCPFlag resolves the --tcp flag the way matter.js resolves its
// `network.tcp` setting (packages/node/src/behavior/system/network/
// NetworkServer.ts:261-272 resolveTcpConfig): "true" turns both roles
// on, "false" (or nothing) turns both off, and otherwise each named role
// is on — "incoming" (accept TCP connections, the tcpServer bit),
// "outgoing" (open them, the tcpClient bit), comma-separated.
func parseTCPFlag(v string) (node.TCPConfig, error) {
	switch strings.TrimSpace(v) {
	case "true":
		return node.TCPConfig{Incoming: true, Outgoing: true}, nil
	case "false", "":
		return node.TCPConfig{}, nil
	}
	var cfg node.TCPConfig
	for _, role := range strings.Split(v, ",") {
		switch strings.TrimSpace(role) {
		case "incoming":
			cfg.Incoming = true
		case "outgoing":
			cfg.Outgoing = true
		default:
			return node.TCPConfig{}, fmt.Errorf("--tcp %q: want true, false, or incoming / outgoing (comma-separated)", v)
		}
	}
	return cfg, nil
}
