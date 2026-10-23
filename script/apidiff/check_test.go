// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"reflect"
	"testing"
)

// report is apidiff's module-mode output as it prints it.
const report = `Ignoring internal package github.com/SukramJ/go-fabric/internal/udpseam
Incompatible changes:
- ./cluster/spec.Bool: removed
- ./controller/client.(*Cluster).Decoded: changed from func(uint32) (any, error) to func(uint32) any
- ./examples/quickstart-controller.Thing: removed
- ./script/apidiff.change: removed
- ./node/old: removed
`

func TestParse(t *testing.T) {
	got := parse(report)
	want := []change{
		{Line: "./cluster/spec.Bool: removed", Package: "cluster/spec", Symbol: "Bool"},
		{Line: "./controller/client.(*Cluster).Decoded: changed from func(uint32) (any, error) to func(uint32) any", Package: "controller/client", Symbol: "Cluster.Decoded"},
		{Line: "./node/old: removed", Package: "node/old", Symbol: "node/old"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parse:\n got %+v\nwant %+v", got, want)
	}
}

func TestSplitRoot(t *testing.T) {
	if c := split("..Version"); c.Package != "." || c.Symbol != "Version" {
		t.Errorf("root symbol: %+v", c)
	}
	if c := split("."); c.Symbol != module {
		t.Errorf("root package: %+v", c)
	}
}

const changelog = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- `spec.Bool`, new.\n\n" +
	"### Changed\n\n- `client.Cluster.Decoded` returns one value.\n\n### Removed\n\n- `node/old`: gone.\n\n" +
	"## [0.10.2] — 2026-10-23\n\n### Removed\n\n- `spec.Bool`.\n"

func TestUnannounced(t *testing.T) {
	text := unreleased(changelog)
	missing := unannounced(parse(report), text)
	if len(missing) != 1 || missing[0].Symbol != "Bool" {
		t.Errorf("missing %+v, want only Bool: Added does not announce a break, and an older release's line does not count", missing)
	}
	if unreleased("# no section\n") != "" {
		t.Error("a changelog without [Unreleased] announces something")
	}
}
