// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package valveconfigurationandcontrol is the generated definition of the
// Matter ValveConfigurationAndControl cluster (definition_gen.go,
// server_gen.go) and the hand-written capability ports of its StateSource
// (ports.go).
package valveconfigurationandcontrol

import "context"

// The optional interfaces of a [StateSource] that unlock the Level (LVL)
// feature and its optional DefaultOpenLevel attribute. The kit's server
// (cluster/valve) refuses a declared selection whose interface the
// StateSource lacks, naming it; it resolves an Open's level — the
// command's TargetLevel, else DefaultOpenLevel, else 100 — and hands it to
// [StateSource.Open] in the request's TargetLevel (connectedhomeip
// ValveConfigurationAndControlCluster.cpp HandleOpenCommand :312-341 at
// 6170af84; the delegate's HandleOpenValve(level),
// valve-configuration-and-control-delegate.h:40).
//
// Hand-written beside the generated server_gen.go: script/clustergen owns
// that file, these are the kit server's capability ports.

// LevelSource is an optional interface of a [StateSource] for Level
// (LVL): the readers report the valve's level in percent, known false
// for null (quality X). A valve reports CurrentLevel as it moves and
// TargetLevel while a level is outstanding — the level an Open resolved,
// 0 after a Close (chip :253-256) — and null once it is reached.
type LevelSource interface {
	// CurrentLevel reports the valve's observed level.
	CurrentLevel() (percent uint8, known bool)
	// TargetLevel reports the level the valve is moving towards.
	TargetLevel() (percent uint8, known bool)
}

// DefaultOpenLevelSource is an optional interface of a [StateSource] for
// the DefaultOpenLevel attribute (conformance "[LVL]", access "RW VO",
// quality N): the level an Open without TargetLevel opens to, 1 to 100,
// persisted by the host. The server checks a write against the range
// and LevelStep before SetDefaultOpenLevel (chip :175-192).
type DefaultOpenLevelSource interface {
	// DefaultOpenLevel reports the configured default open level.
	DefaultOpenLevel() (percent uint8)
	// SetDefaultOpenLevel persists a new default open level.
	SetDefaultOpenLevel(ctx context.Context, percent uint8) error
}
