// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// serverType is the Go type of a cluster's default server, which the
// generated accessor returns, and the package that declares it.
type serverType struct {
	// Type is the Go type as the generated file spells it.
	Type string
	// Import is the declaring package's import path.
	Import string
	// Alias is the import's name in the generated file; empty for the
	// path's last element.
	Alias string
}

// typedServer is the typed server script/clustergen generates beside the
// definition of the cluster pkg (cluster/spec/<pkg>/server_gen.go: the
// <pkg>def.Server that embeds spec.Server), the default for every cluster
// with a committed definition that servers does not name (ADR 0018,
// decisions of wave 6, 2 and 3; brief BT). device.New mounts the generated
// server in it, so the accessor finds it.
func typedServer(pkg string) serverType {
	return serverType{Type: "*" + pkg + "def.Server", Import: "github.com/SukramJ/go-fabric/cluster/spec/" + pkg, Alias: pkg + "def"}
}

// servers is the hand-maintained table of the clusters whose default
// server is one of the module's self-held servers rather than the
// generated one: cluster name → Go type and import. Keep it in step with
// device/device.go defaultFactories, which builds them.
var servers = map[string]serverType{
	"OnOff":                {Type: "*onoff.Server", Import: "github.com/SukramJ/go-fabric/cluster/onoff"},
	"Binding":              {Type: "*core.Binding", Import: coreImport},
	"EcosystemInformation": {Type: "*core.EcosystemInformation", Import: coreImport},
}

const coreImport = "github.com/SukramJ/go-fabric/cluster/core"
