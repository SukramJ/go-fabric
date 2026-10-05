// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

// Package chiptool is this module's real-commissioner guard: it drives the
// CSA reference commissioner (`chip-tool`) against the reference daemon in
// examples/reference-bridge, over a real PASE handshake and the operational
// CASE session that follows.
//
// Everything else in this module is tested against itself. A Matter stack
// that only ever talks to its own test client can be wrong in ways no
// in-tree test can see, because both sides share the misreading. This suite
// is the one place where the other side of the wire is somebody else's code.
//
// The `chiptool` build tag keeps it out of `make test`, `make race` and the
// default `go test ./...`: it needs a Linux host, a chip-tool binary and
// minutes of wall clock. Run it with `make chiptool-test`, which builds the
// reference daemon first. CI runs it nightly, on a `needs-chiptool`
// pull-request label, on manual dispatch and on any pull request that
// touches Go code — see .github/workflows/chiptool.yml, whose second job
// (`chiptool-control`) drives the same chip-tool against a CHIP reference app
// so a red run here can be attributed to this module rather than to the
// environment.
//
// # Running it locally
//
// On a Linux host, two commands, no exports:
//
//	make chiptool-setup   # once: ../connectedhomeip at the pin + bin/chipyaml-venv
//	make chiptool-test
//
// The suite finds its chip-tool in this order: $GOFABRIC_CHIPTOOL_BIN,
// ./bin/chip-tool (written by `make chiptool-extract`), chip-tool on PATH.
// Every test logs which binary it ran — path, real file, release or image
// pin, and a digest — because the CI pin and a local chip-tool are different
// releases and a result means nothing without the build behind it.
//
//   - arm64 Linux: `make chiptool-extract` copies the CI pin out of the
//     connectedhomeip/chip-cert-bins image, exactly as CI does.
//   - amd64 Linux: that image publishes arm64 manifests only, so extraction
//     stops with an explanation. Install the snap instead
//     (`sudo snap install chip-tool`; it brings its own libraries). A
//     snap-confined chip-tool can only write under
//     $HOME/snap/chip-tool/common and sees a private /tmp, so the suite puts
//     its key-value stores there by itself (removed afterwards);
//     $GOFABRIC_CHIPTOOL_KVS_BASE overrides that. The snap serves the YAML
//     runner's `interactive server` websocket like the pinned build does.
//
// Either way chip-tool needs a running avahi-daemon (operational discovery
// after AddNOC goes through it), which a desktop Linux has and CI starts.
//
// `make chiptool-setup` prepares the YAML leg's inputs the way the
// chiptool-conformance job does: a sparse, shallow connectedhomeip checkout
// at the Makefile's CHIP_CERT_BINS_IMAGE commit (at ../connectedhomeip, the
// path CLAUDE.md names; it also carries examples/chip-tool and src/ for
// reading controller behaviour) and a venv with matter-yamltests and
// matter-idl installed from that checkout. The conformance test uses both by
// default; $GOFABRIC_CHIP_ROOT and $GOFABRIC_CHIPYAML_PYTHON override them.
//
// Nothing in the suite is skipped silently on a machine that has chip-tool:
// a missing chip-tool skips (the canonical Go "missing prerequisite") with
// the command that provides it, a missing reference-daemon binary fails
// loudly with the command to run, and a chip-tool that lacks a cluster or
// command a subtest needs skips that subtest naming the capability.
package chiptool
