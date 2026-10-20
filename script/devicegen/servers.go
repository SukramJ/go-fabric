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
	// Alias is the import's name in the generated file.
	Alias string
}

// typedServer is the typed server script/clustergen generates beside the
// definition of the cluster pkg (cluster/spec/<pkg>/server_gen.go), the
// type of every accessor (ADR 0018, decisions of wave 6, 2 and 3; brief BT
// and its addendum): <pkg>def.Wrap wraps whichever server the device
// mounts, the generated spec.Server or a self-held one (onoff.Server,
// core.Binding, core.EcosystemInformation).
func typedServer(pkg string) serverType {
	return serverType{Type: "*" + pkg + "def.Server", Import: "github.com/SukramJ/go-fabric/cluster/spec/" + pkg, Alias: pkg + "def"}
}
