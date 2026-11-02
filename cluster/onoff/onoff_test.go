// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/onoff"
)

// TestRevisionIsWhatTheServerAnswers: the exported Revision is the
// ClusterRevision the server answers. That the answer is matter.js's is
// node's TestServedClusterRevisionAndFeatureMap (W6-test-10), which reads
// it off every constructed server.
func TestRevisionIsWhatTheServerAnswers(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); got != onoff.Revision() {
		t.Errorf("the server answers ClusterRevision %v, Revision() is %d", got, onoff.Revision())
	}
}

// TestLightingSetsAreOrderedAndComplete pins the two derived lists against
// matter.js conformance: the four 0x40xx attributes are "LT"-gated, and the
// command set is Off (M) plus On/Toggle (!OFFONLY) plus the three LT
// commands.
func TestLightingSetsAreOrderedAndComplete(t *testing.T) {
	t.Parallel()

	attrs := onoff.LightingAttributes()
	wantAttrs := []uint32{0x0000, 0x4000, 0x4001, 0x4002, 0x4003}
	if len(attrs) != len(wantAttrs) {
		t.Fatalf("attributes = %#v, want %#v", attrs, wantAttrs)
	}
	for i := range attrs {
		if attrs[i] != wantAttrs[i] {
			t.Errorf("attribute %d = %#04x, want %#04x", i, attrs[i], wantAttrs[i])
		}
	}

	cmds := onoff.LightingCommands()
	wantCmds := []uint32{0x00, 0x01, 0x02, 0x40, 0x41, 0x42}
	if len(cmds) != len(wantCmds) {
		t.Fatalf("commands = %#v, want %#v", cmds, wantCmds)
	}
	for i := range cmds {
		if cmds[i] != wantCmds[i] {
			t.Errorf("command %d = %#02x, want %#02x", i, cmds[i], wantCmds[i])
		}
	}
}
