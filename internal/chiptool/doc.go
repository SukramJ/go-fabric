// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

// Package chiptool is this module's real-controller guard: it drives the
// CSA reference commissioner (chip-tool) and the CSA certification test
// harness (the YAML runner and the Python TC_* cases) against the reference
// daemon in examples/reference-bridge.
//
// Everything else in this module is tested against itself. A Matter stack
// that only ever talks to its own test client can be wrong in ways no
// in-tree test can see, because both sides share the misreading. This suite
// is the one place where the other side of the wire is somebody else's code.
//
// Certification is not pursued by this project and nothing built on it may
// be described as certified; certifiability is a goal
// (docs/adr/0011-certifiability-is-a-goal.md). The certification families
// here are how it is held, and docs/certifiability.md — checked against the
// family table by TestCertifiabilityDocument — is the status.
//
// The `chiptool` build tag keeps the harness-driven tests out of
// `make test`, `make race` and the default `go test ./...`. Three entry
// points:
//
//   - TestChipToolCommissionsReferenceBridge — commission, read, invoke,
//     tear down (`make chiptool-test`);
//   - TestChipToolSuite — the data-model sweep and the further legs over one
//     interactive chip-tool session (`make chiptool-test`);
//   - TestChipCertificationFamilies — the CSA certification families, run
//     by family the way matter.js runs them (`make chiptool-families`,
//     narrowed with GOFABRIC_CHIP_FAMILIES=IDM,ACL,…).
//
// # The harness
//
// By default the suite runs chip-tool and the Python harness inside
// matter.js's CHIP image (ghcr.io/matter-js/chip, multi-arch), pinned by
// digest in the Makefile's CHIP_TEST_IMAGE; the CHIP commit it was built
// from is logged with every run. The container shares the host network and
// the host's avahi over /run/dbus, so the only host prerequisites are Linux,
// Docker and a running avahi-daemon. `make chiptool-image` pulls the image
// ahead of time; the suite pulls it on first use otherwise.
//
// A host chip-tool is the fallback, for the commissioning test and the
// chip-tool suite only: $GOFABRIC_CHIPTOOL_BIN, then ./bin/chip-tool (`make
// chiptool-extract`, arm64), then chip-tool on PATH (the snap). Setting
// $GOFABRIC_CHIPTOOL_BIN or $GOFABRIC_CHIP_HARNESS=host selects it; the
// families then skip, naming why. A snap-confined chip-tool can only write
// under $HOME/snap/chip-tool/common and sees a private /tmp, so the suite
// puts its key-value stores there by itself; $GOFABRIC_CHIPTOOL_KVS_BASE
// overrides that.
//
// `make chiptool-setup` makes a sparse connectedhomeip checkout at
// ../connectedhomeip — the source CLAUDE.md sends you to when chip-tool or a
// CHIP test case behaves unexpectedly.
//
// # The families and their PICS
//
// familyrun_test.go runs each family of families_table_test.go in full; a
// case left out is a gap with its ADR 0011 class and reason, reported as a
// skip naming both. What a passing case executed is pinned in
// testdata/chip-cases.golden.json (steps run and skipped), so a PICS edit
// that quietly turns a case into a no-op fails it.
//
// The PICS is generated from the device: testdata/gen_pics.py reads the
// commissioned daemon with one wildcard read and writes a slice per endpoint
// (testdata/pics/ep<N>.txt) with CHIP's own derivation helpers. Each case
// runs with CHIP's ci-pics-values, the hand declarations a device cannot
// report (testdata/reference-bridge.pics, which may not touch a generated
// code), and the slice of the endpoint under test. TC-IDM-10.4 holds every
// slice against the device.
//
// The reference daemon offers the out-of-band controls the CHIP cases
// expect, off by default: a named pipe for CHIP-style JSON commands
// (--app-pipe) and General Diagnostics TestEventTrigger (--enable-key), plus
// the harness's restart-flag protocol, served by monitorRestartFlag, and
// the YAML runner's accessory protocol (SystemCommands Reboot and
// FactoryReset), served on a loopback port by accessory_test.go the way
// matter.js serves it.
//
// Nothing is skipped silently on a machine that has the prerequisites: a
// missing harness skips with the command that provides it, a missing
// reference-daemon binary fails loudly with the command to run, and a
// chip-tool that lacks a cluster or command a subtest needs skips that
// subtest naming the capability.
package chiptool
