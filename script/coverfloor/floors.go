// SPDX-License-Identifier: Apache-2.0
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
	{pkg: "github.com/SukramJ/go-fabric/bdx", min: 75},
	{pkg: "github.com/SukramJ/go-fabric/binding", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/bdx/otaimage", min: 76},
	{pkg: "github.com/SukramJ/go-fabric/bootid", min: 89},
	{
		pkg: "github.com/SukramJ/go-fabric/bridge",
		min: 83,
		why: "Go 1.27.2 counts statements per cover block differently: against 1.27.1 the profile has the same blocks and the same covered/uncovered set, only fewer statements per block, so the percentage fell without any test or code change (84.9 % to 83.6 %)",
	},
	{pkg: "github.com/SukramJ/go-fabric/bridge/bridgetest", min: 92},
	{pkg: "github.com/SukramJ/go-fabric/clock", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/clock/clocktest", min: 95},
	{pkg: "github.com/SukramJ/go-fabric/cluster", min: 96},
	{pkg: "github.com/SukramJ/go-fabric/cluster/alarm", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/alarmbase", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/cluster/appliance", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/closure", min: 96},
	{pkg: "github.com/SukramJ/go-fabric/cluster/core", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/cluster/cover", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/energy", min: 93},
	{pkg: "github.com/SukramJ/go-fabric/cluster/fan", min: 98},
	// 85, not 86: NativeSource's owed switch-off is paid by whichever of
	// the device's notification and the ramp's deadline timer gets there
	// first (native.go settle/pay), so a run covers one branch or the
	// other — CI measured 85.9 where a local run measured 86.1.
	{pkg: "github.com/SukramJ/go-fabric/cluster/levelcontrol", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/cluster/light", min: 94},
	{pkg: "github.com/SukramJ/go-fabric/cluster/lock", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/cluster/measurement", min: 86},
	{pkg: "github.com/SukramJ/go-fabric/cluster/media", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/cluster/modebase", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/modeselect", min: 86},
	// Pinned at full coverage. A package that reaches 100 % has no
	// uncovered branch to lose, so anything less is a new untested
	// statement — which is exactly the moment to notice it, while the
	// change that added it is still on screen.
	{pkg: "github.com/SukramJ/go-fabric/cluster/onoff", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/opstate", min: 99},
	{pkg: "github.com/SukramJ/go-fabric/cluster/pump", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/switch", min: 97},
	{pkg: "github.com/SukramJ/go-fabric/cluster/thermo", min: 95},
	{pkg: "github.com/SukramJ/go-fabric/cluster/camera", min: 90},
	{
		pkg: "github.com/SukramJ/go-fabric/cluster/transition",
		min: 93,
		why: "Go 1.27.2 counts statements per cover block differently: against 1.27.1 the profile has the same blocks and the same covered/uncovered set, only fewer statements per block, so the percentage fell without any test or code change (94.2 % to 93.8 %)",
	},
	{pkg: "github.com/SukramJ/go-fabric/cluster/valve", min: 98},
	{pkg: "github.com/SukramJ/go-fabric/cluster/wire", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/commissioning", min: 88},
	{pkg: "github.com/SukramJ/go-fabric/conformance", min: 93},
	{pkg: "github.com/SukramJ/go-fabric/controller", min: 72, why: "the composition root: New, Close and the refusals of Pair are tested here; the commissioning Pair hands to Registry.Commission is covered in controller/client"},
	{pkg: "github.com/SukramJ/go-fabric/controller/attestation", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/controller/ble", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/controller/ble/bletest", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/controller/ca", min: 88},
	{pkg: "github.com/SukramJ/go-fabric/controller/ca/keyfile", min: 73},
	{pkg: "github.com/SukramJ/go-fabric/controller/client", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/controller/client/sqlstore", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/controller/commissioner", min: 85},
	{pkg: "github.com/SukramJ/go-fabric/controller/dcl", min: 85},
	{
		pkg: "github.com/SukramJ/go-fabric/controller/softwareupdate",
		min: 29,
		why: "brief CM §1: measured 29.4. The manager runs against a controller and a device: its end-to-end and OtaTest cases are the controller package's (controller:TestOTA*), which a per-package profile does not count; the package's own tests set the queue's state as matter.js's do",
	},
	{pkg: "github.com/SukramJ/go-fabric/controller/dcl/snapshot", min: 83},
	{pkg: "github.com/SukramJ/go-fabric/client/discovery", min: 95},
	{pkg: "github.com/SukramJ/go-fabric/groups", min: 93},
	// The module's lowest measured package (76.7 when last raised).
	// contract is the host-facing interface vocabulary, and much of what is uncovered
	// are the default implementations and kind-registry helpers a host
	// exercises rather than this module's own tests. The floor records
	// where it stands; whether it should be raised is a separate decision
	// about what deserves a test, not a ratchet setting.
	{pkg: "github.com/SukramJ/go-fabric/contract", min: 76, why: "lowest in the module; see the note above this entry"},
	{pkg: "github.com/SukramJ/go-fabric/client/exchange", min: 93},
	{pkg: "github.com/SukramJ/go-fabric/client/interaction", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/client/handshake", min: 86},
	{
		pkg: "github.com/SukramJ/go-fabric/client/peer",
		min: 91,
		why: "brief CM §2/§3: measured 91.5 (was 93). The TCP initiator's and the network profiles' remaining paths — a TCP dial, a profile's run-time Configure, the cooling of a released slot — run through the controller's TCP and network tests (controller:TestClientNodeTcpParity, client/peer's profile tests on a harness without TCP)",
	},
	{pkg: "github.com/SukramJ/go-fabric/thread", min: 97},
	{pkg: "github.com/SukramJ/go-fabric/diagevent", min: 96},
	{pkg: "github.com/SukramJ/go-fabric/eligibility", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/device", min: 97},
	{pkg: "github.com/SukramJ/go-fabric/endpoint", min: 86},
	// Brief BG: measured 86.9; the uncovered part is the error paths of a
	// host that fails to build or listen, which fail the test through tb.
	{pkg: "github.com/SukramJ/go-fabric/fabrictest", min: 85},
	{
		pkg: "github.com/SukramJ/go-fabric/endpoint/endpointtest",
		min: 0,
		why: "test scaffolding, exercised through the packages that import it, and go credits that coverage to them; only the device type conformance helper has a test of its own",
	},
	{pkg: "github.com/SukramJ/go-fabric/endpoint/memstore", min: 99},
	{pkg: "github.com/SukramJ/go-fabric/endpoint/sqlitestore", min: 77},
	{
		pkg: "github.com/SukramJ/go-fabric/examples/host-view",
		min: 34,
		why: "brief CM §6: measured 34.8 once MarkComplete went (brief BU: 36.2). The tests drive the declared devices over the wire and on a node; run and main bind the network, which a unit test does not",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/examples/reference-bridge",
		min: 0,
		why: "example daemon, exercised end to end by the chip-tool suite, which runs the built binary rather than its statements. It does have statement-level tests now — fleet_test.go drives a subscription through a mounted endpoint — but the floor stays at 0 because the daemon's value is the end-to-end run, and a number here would ratchet on the fake devices rather than on anything a consumer depends on",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/examples/reference-controller",
		min: 0,
		why: "brief BB item 2: only main, which hands os.Args to cli.Run; the command line and its tests are examples/reference-controller/cli",
	},
	// Brief AT §4: the floor the brief set; measured 74.0 with
	// TestReferenceController and TestReferenceControllerOTA alone (then
	// package main, moved to cli by brief BB item 2).
	{pkg: "github.com/SukramJ/go-fabric/examples/reference-controller/cli", min: 70},
	// Brief AW: measured 59.3-60.2; the uncovered part is main and the
	// paired-node path, which needs a device on the network.
	{pkg: "github.com/SukramJ/go-fabric/examples/quickstart-controller", min: 35, why: "brief BV-a: the stack moved into controller.New (covered in controller); what remains is main and use, which need a paired node"},
	{
		pkg: "github.com/SukramJ/go-fabric/examples/quickstart-device",
		min: 20,
		why: "brief AW: measured 24.4. The test drives newNode, the program's substance; main and run bind :5540 and the mDNS groups, which a unit test does not",
	},
	{pkg: "github.com/SukramJ/go-fabric/icd", min: 90},
	{pkg: "github.com/SukramJ/go-fabric/icd/checkin", min: 94},
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
		pkg: "github.com/SukramJ/go-fabric/internal/chiptool",
		min: 0,
		why: "the CHIP certification harness: its untagged files are the family table and the certifiability-document check, test files only, so the package holds no statement; the harness itself runs behind the chiptool build tag",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/channelseam",
		min: 0,
		why: "declaration-only seam: function variables another package installs from its init, so the package holds no statement a test could execute",
	},
	{pkg: "github.com/SukramJ/go-fabric/internal/clockvar", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/internal/inflight", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/internal/serialqueue", min: 97},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/deviceseam",
		min: 0,
		why: "declaration-only seam: an interface and a function variable device installs from its init, so the package holds no statement a test could execute",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/nodeseam",
		min: 0,
		why: "declaration-only seam: a function variable node installs from its init, so the package holds no statement a test could execute",
	},
	{pkg: "github.com/SukramJ/go-fabric/internal/paritytest", min: 97},
	{
		pkg: "github.com/SukramJ/go-fabric/internal/udpseam",
		min: 0,
		why: "declaration-only seam: an interface and a function variable transport/udp installs from its init, so the package holds no statement a test could execute",
	},
	{pkg: "github.com/SukramJ/go-fabric/mdns", min: 91},
	{
		pkg: "github.com/SukramJ/go-fabric/node",
		min: 83,
		why: "the end-to-end tests commission the node over UDP with bridgetest.Commissioner (PASE, AddNOC, CASE, re-establishment after a restart, RemoveFabric); the enhanced commissioning window, UpdateNOC's fabric hook, CASE resumption and the OTA applier have no test in the package",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/node/controllernode",
		min: 67,
		why: "brief CM §1: measured 67.9; Start's failure paths (node.New refusing the spec, a socket address that does not resolve) have no test",
	},
	{pkg: "github.com/SukramJ/go-fabric/node/metrics/expvar", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/parity", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/schema", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/script/aheadcheck", min: 90, why: "the parser, the check and run against a fixture git repository built in the test; main (flags, the exit) is exercised by make ahead-check"},
	{pkg: "github.com/SukramJ/go-fabric/script/archcheck", min: 0, why: "doc and test only, no statements to cover"},
	{pkg: "github.com/SukramJ/go-fabric/script/apidiff", min: 35, why: "the report parsing and the CHANGELOG match are tested; run, which drives the version control and the apidiff tool, is exercised by make apidiff-check"},
	// BX4's forward generator; main() only writes the files its test compares.
	{
		pkg: "github.com/SukramJ/go-fabric/script/citecheck",
		min: 68,
		why: "the collector, resolver, checker, summary and the derived corrections run against testdata and an in-memory tree; the git plumbing (rev-parse, ls-tree, cat-file --batch) and main need the matter.js clone, which make cite-check and its CI job provide",
	},
	// This tool. Its parser and comparison are covered; main() — flag
	// parsing, file opening, printing, the exit codes — is not, because
	// covering it would mean a test that runs the gate over the module the
	// gate is part of.
	{pkg: "github.com/SukramJ/go-fabric/script/coverfloor", min: 52},
	{pkg: "github.com/SukramJ/go-fabric/script/docscheck", min: 0, why: "test only, no statements to cover: the guard is the test"},
	{pkg: "github.com/SukramJ/go-fabric/script/errprefix", min: 0, why: "test only, no statements to cover: the guard is the test"},
	{pkg: "github.com/SukramJ/go-fabric/script/slogkeys", min: 0, why: "test only, no statements to cover: the guard is the test"},
	{
		pkg: "github.com/SukramJ/go-fabric/script/dclsnapshot",
		min: 0,
		why: "main only, asking the production ledger; snapshot.Fetch, which it calls, is covered against a fake ledger in controller/dcl/snapshot",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/script/testparity",
		min: 67,
		why: "the register guard and the snapshot parsers are covered; writeSnapshot and main, which shell out to git against a matter.js clone, are not, because the guard must run without one",
	},
	{
		pkg: "github.com/SukramJ/go-fabric/script/reachability",
		min: 0,
		why: "the analyzer's own tests read the committed inventory rather than running the analysis, so none of its statements execute under go test",
	},
	{pkg: "github.com/SukramJ/go-fabric/secure", min: 0, why: "package doc only, no statements to cover"},
	{pkg: "github.com/SukramJ/go-fabric/secure/aesccm", min: 96},
	{
		pkg: "github.com/SukramJ/go-fabric/secure/attestation",
		min: 80,
		why: "Go 1.27.2 counts statements per cover block differently: against 1.27.1 the profile has the same blocks and the same covered/uncovered set, only fewer statements per block, so the percentage fell without any test or code change (85.7 % to 80.2 %)",
	},
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
	{
		pkg: "github.com/SukramJ/go-fabric/secure/sigma",
		min: 86,
		why: "Go 1.27.2 counts statements per cover block differently: against 1.27.1 the profile has the same blocks and the same covered/uncovered set, only fewer statements per block, so the percentage fell without any test or code change (89.1 % to 86.7 %)",
	},
	{pkg: "github.com/SukramJ/go-fabric/secure/spake2", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/secure/sessionparams", min: 100},
	// Brief CA: 85.2 with the contract table (store/storetest) on SQLite.
	{pkg: "github.com/SukramJ/go-fabric/store", min: 85},
	// Brief CA: measured 99.5; the rest is a next-fabric-index counter a host overwrote
	// out of range and nextFreeFrom's fall-through on a full table, which
	// AddFabric refuses first.
	{pkg: "github.com/SukramJ/go-fabric/store/memstore", min: 99},
	{
		pkg: "github.com/SukramJ/go-fabric/store/storetest",
		min: 0,
		why: "the contract table, exercised by store's and store/memstore's tests, and go credits that coverage to them (80.8 % of it run against memstore)",
	},
	{pkg: "github.com/SukramJ/go-fabric/tlv", min: 91},
	{pkg: "github.com/SukramJ/go-fabric/transport", min: 0, why: "the Conn interface only, no statements to cover"},
	{pkg: "github.com/SukramJ/go-fabric/transport/btp", min: 93},
	{pkg: "github.com/SukramJ/go-fabric/transport/message", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/transport/mrp", min: 99},
	{pkg: "github.com/SukramJ/go-fabric/transport/tcp", min: 89},
	{pkg: "github.com/SukramJ/go-fabric/transport/udp", min: 93},
	// ADR 0013: the cluster-definition runtime, its test support, the
	// generator, the first server built on it, and the committed generated
	// definitions. Generated code is measured like any other (schema/ is
	// too); each generated package's generated test round-trips every codec
	// and holds the definition against the snapshot, which covers it fully.
	{pkg: "github.com/SukramJ/go-fabric/cluster/filter", min: 100},
	// The two unreachable error returns of boolcfg.New (a definition
	// spec.NewServer refuses, an initial value SetAttributes refuses).
	{pkg: "github.com/SukramJ/go-fabric/cluster/boolcfg", min: 98},
	// The unreachable construction errors of the two netinfra servers (a
	// definition spec.NewServer refuses, a store-held value Set refuses).
	{pkg: "github.com/SukramJ/go-fabric/cluster/netinfra", min: 97},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec", min: 97},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/groupcast", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/watertanklevelmonitoring", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/chime", min: 100},
	// Brief CL §1: every snapshot cluster has a definition.
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/doorlock", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/fancontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/onoff", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/modeselect", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/operationalstate", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/windowcovering", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/thermostat", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pressuremeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/userlabel", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/softwarediagnostics", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/actions", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/booleanstate", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/airquality", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ozoneconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/pm1concentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/radonconcentrationmeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/powersource", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/powertopology", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/switchcluster", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/groups", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/identify", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/descriptor", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/basicinformation", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/binding", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ovenmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/energyevse", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/energypreference", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/meteridentification", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/servicearea", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/closuredimension", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/commodityprice", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/accountlogin", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/audiooutput", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/channel", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/contentappobserver", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/keypadinput", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/lowpower", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/mediainput", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/messages", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/servicearea", min: 100},
	{pkg: "github.com/SukramJ/go-fabric/cluster/spec/spectest", min: 95},
	{pkg: "github.com/SukramJ/go-fabric/script/clustergen", min: 95},
	{pkg: "github.com/SukramJ/go-fabric/script/devicegen", min: 95},
	// The CHIP data model cross-check (internal/chipdm): measured 97.4 without
	// a connectedhomeip checkout (as cover-check runs in CI: the cross-check
	// skips, the synthetic-fixture unit tests carry it), 98.6 with one.
	{pkg: "github.com/SukramJ/go-fabric/internal/chipdm", min: 96},
}
