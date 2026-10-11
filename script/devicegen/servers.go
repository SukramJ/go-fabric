// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// serverType is the Go type of a cluster's default server, which the
// generated accessor returns, and the package that declares it.
type serverType struct {
	// Type is the Go type as the generated file spells it.
	Type string
	// Import is the declaring package's import path.
	Import string
}

const specImport = "github.com/SukramJ/go-fabric/cluster/spec"

// specServer is the generated server ([spec.Server]), the default for every
// cluster with a committed definition under cluster/spec that servers does
// not name (ADR 0018, decisions of wave 6, 2 and 3).
var specServer = serverType{Type: "*spec.Server", Import: specImport}

// servers is the hand-maintained table of the clusters whose default
// server is one of the module's self-held servers rather than the
// generated one: cluster name → Go type and import. Keep it in step with
// device/device.go defaultFactories, which builds them.
var servers = map[string]serverType{
	"OnOff": specServer, // TODO(brief Q): *onoff.Server from cluster/onoff once it lands.
}
