// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"strings"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
)

// parseTCPFlag resolves the --tcp flag the way matter.js resolves its
// `network.tcp` setting (packages/node/src/behavior/system/network/
// NetworkServer.ts:261-272 resolveTcpConfig): "true" turns both roles
// on, "false" (or nothing) turns both off, and otherwise each named role
// is on — "incoming" (accept TCP connections, the tcpServer bit),
// "outgoing" (open them, the tcpClient bit), comma-separated.
func parseTCPFlag(v string) (matterbridge.TCPConfig, error) {
	switch strings.TrimSpace(v) {
	case "true":
		return matterbridge.TCPConfig{Incoming: true, Outgoing: true}, nil
	case "false", "":
		return matterbridge.TCPConfig{}, nil
	}
	var cfg matterbridge.TCPConfig
	for _, role := range strings.Split(v, ",") {
		switch strings.TrimSpace(role) {
		case "incoming":
			cfg.Incoming = true
		case "outgoing":
			cfg.Outgoing = true
		default:
			return matterbridge.TCPConfig{}, fmt.Errorf("--tcp %q: want true, false, or incoming / outgoing (comma-separated)", v)
		}
	}
	return cfg, nil
}
