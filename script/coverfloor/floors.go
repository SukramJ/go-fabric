// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// packageFloor is one package's minimum statement coverage.
type packageFloor struct {
	// pkg is the full import path, as `go test -cover` prints it.
	pkg string
	// min is the minimum statement coverage, in percent. Zero means the
	// package carries no measurable coverage at all — see why.
	min float64
	// why explains a floor that needs explaining: a zero floor, or a value
	// low enough that a reader would otherwise assume it was a typo. Left
	// empty for the ordinary case, where the number speaks for itself.
	why string
}

// floors is the per-package ratchet. Each entry was set at, or at the whole
// percent just below, what the package measured when the floor was
// introduced — so the gate is green on the day it lands and only ever
// reacts to a package getting worse.
//
// Adding a package to the module means adding it here; the check fails on a
// package it has no entry for, because a silently unguarded package is worse
// than no gate at all. Raising a floor is ordinary maintenance. LOWERING one
// is the interesting edit: it says a package's coverage was allowed to drop,
// and it belongs in the change that caused it, with the reason in the commit
// message.
//
// The floors are NOT a statement about what these packages deserve. One of
// them — contract, at 52 % — is conspicuously low for a module this heavily
// tested, and raising it means deciding what to test, which is a change of
// its own and not something to fold into a ratchet.
var floors = []packageFloor{
	{pkg: "github.com/SukramJ/go-fabric", min: 0, why: "module doc only, no statements to cover"},
	{pkg: "github.com/SukramJ/go-fabric/bootid", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/bridge", min: 84},
	{pkg: "github.com/SukramJ/go-fabric/bridge/bridgetest", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/cluster", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/cluster/closure", min: 81},
	{pkg: "github.com/SukramJ/go-fabric/cluster/core", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/cluster/cover", min: 87},
	{pkg: "github.com/SukramJ/go-fabric/cluster/levelcontrol", min: 74},
	{pkg: "github.com/SukramJ/go-fabric/cluster/light", min: 81},
	{pkg: "github.com/SukramJ/go-fabric/cluster/lock", min: 86},
	{pkg: "github.com/SukramJ/go-fabric/cluster/measurement", min: 87},
	{pkg: "github.com/SukramJ/go-fabric/cluster/modeselect", min: 86},
	// Pinned at full coverage. A package that reaches 100 % has no
	// uncovered branch to lose, so anything less is a new untested
	// statement — which is exactly the moment to notice it, while the
	// change that added it is still on screen.
	{pkg: "github.com/SukramJ/go-fabric/cluster/onoff", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/thermo", min: 78},
	{pkg: "github.com/SukramJ/go-fabric/cluster/valve", min: 79},
	{pkg: "github.com/SukramJ/go-fabric/cluster/wire", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/commissioning", min: 88},
	{pkg: "github.com/SukramJ/go-fabric/conformance", min: 93},
	// The module's lowest measured package by a wide margin. contract is
	// the host-facing interface vocabulary, and much of what is uncovered
	// are the default implementations and kind-registry helpers a host
	// exercises rather than this module's own tests. The floor records
	// where it stands; whether it should be raised is a separate decision
	// about what deserves a test, not a ratchet setting.
	{pkg: "github.com/SukramJ/go-fabric/contract", min: 52, why: "lowest in the module; see the note above this entry"},
	{pkg: "github.com/SukramJ/go-fabric/diagevent", min: 96},
	{pkg: "github.com/SukramJ/go-fabric/eligibility", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/endpoint", min: 86},
	{
		pkg: "github.com/SukramJ/go-fabric/endpoint/endpointtest",
		min: 0,
		why: "test scaffolding with no tests of its own; it is exercised through the packages that import it, and go credits that coverage to them",
	},
	{pkg: "github.com/SukramJ/go-fabric/endpoint/sqlitestore", min: 72},
	{
		pkg: "github.com/SukramJ/go-fabric/examples/reference-bridge",
		min: 0,
		why: "example daemon, exercised end to end by the chip-tool suite, which runs the built binary rather than its statements. It does have statement-level tests now — fleet_test.go drives a subscription through a mounted endpoint — but the floor stays at 0 because the daemon's value is the end-to-end run, and a number here would ratchet on the fake devices rather than on anything a consumer depends on",
	},
	{pkg: "github.com/SukramJ/go-fabric/im", min: 82},
	// Measured 91.7-92.7 across six runs at varying GOMAXPROCS: this
	// package has concurrent paths whose coverage depends on scheduling, so
	// a floor at the median makes the gate flaky rather than strict. Floors
	// go BELOW the observed minimum, not at the typical measurement -- a
	// gate that fails on a good day teaches people to re-run it.
	{pkg: "github.com/SukramJ/go-fabric/im/subscription", min: 91},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/bridgeseam",
		min: 0,
		why: "declaration-only seam: function variables another package installs from its init, so the package holds no statement a test could execute",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/channelseam",
		min: 0,
		why: "declaration-only seam: function variables another package installs from its init, so the package holds no statement a test could execute",
	},
	{pkg: "github.com/SukramJ/go-fabric/mdns", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/parity", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/schema", min: 76},
	// This tool. Its parser and comparison are covered; main() — flag
	// parsing, file opening, printing, the exit codes — is not, because
	// covering it would mean a test that runs the gate over the module the
	// gate is part of.
	{pkg: "github.com/SukramJ/go-fabric/script/coverfloor", min: 54},
	{
		pkg: "github.com/SukramJ/go-fabric/script/reachability",
		min: 0,
		why: "the analyzer's own tests read the committed inventory rather than running the analysis, so none of its statements execute under go test",
	},
	{pkg: "github.com/SukramJ/go-fabric/secure", min: 0, why: "package doc only, no statements to cover"},
	{pkg: "github.com/SukramJ/go-fabric/secure/aesccm", min: 96},
	{pkg: "github.com/SukramJ/go-fabric/secure/attestation", min: 80},
	{pkg: "github.com/SukramJ/go-fabric/secure/channel", min: 85},
	// These three floors are deliberately set from the module WITHOUT its
	// fuzz targets: secure/setup measured 84.0 without and 91.4 with,
	// secure/mattercert 90.5 and 91.1, tlv 91.9 and 95.1. `go test` replays
	// each fuzz target's seed corpus, so adding one raises its package's
	// coverage without a single new test function — and removing or
	// retargeting one lowers it again, on a tree nobody meant to make worse.
	// Anchoring below the fuzz contribution keeps the floor measuring the
	// package's own tests. Re-measure and raise them once the fuzz corpus
	// has settled; `make cover-check` says by how much.
	{pkg: "github.com/SukramJ/go-fabric/secure/mattercert", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/secure/operational", min: 92},
	{pkg: "github.com/SukramJ/go-fabric/secure/setup", min: 84},
	{pkg: "github.com/SukramJ/go-fabric/secure/sigma", min: 88},
	{pkg: "github.com/SukramJ/go-fabric/secure/spake2", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/store", min: 83},
	{pkg: "github.com/SukramJ/go-fabric/tlv", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/transport/message", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/transport/mrp", min: 99},
	{pkg: "github.com/SukramJ/go-fabric/transport/udp", min: 93},
}
