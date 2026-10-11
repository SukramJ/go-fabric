// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"testing"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
)

// TestParseTCPFlag mirrors matter.js NetworkServer.ts:261-272
// resolveTcpConfig: true → both, false / unset → none, else the roles.
func TestParseTCPFlag(t *testing.T) {
	cases := map[string]matterbridge.TCPConfig{
		"true":               {Incoming: true, Outgoing: true},
		"false":              {},
		"":                   {},
		"incoming":           {Incoming: true},
		"outgoing":           {Outgoing: true},
		"incoming,outgoing":  {Incoming: true, Outgoing: true},
		"outgoing, incoming": {Incoming: true, Outgoing: true},
	}
	for in, want := range cases {
		got, err := parseTCPFlag(in)
		if err != nil || got != want {
			t.Errorf("parseTCPFlag(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := parseTCPFlag("server"); err == nil {
		t.Error("parseTCPFlag accepted an unknown role")
	}
}
