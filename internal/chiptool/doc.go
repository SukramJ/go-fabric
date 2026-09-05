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
// tens of seconds of wall clock. Run it with `make chiptool-test`, which
// builds the reference daemon first. CI runs it nightly, on a
// `needs-chiptool` pull-request label and on manual dispatch — see
// .github/workflows/chiptool.yml, whose second job (`chiptool-control`)
// drives the same chip-tool against a CHIP reference app so a red run here
// can be attributed to this module rather than to the environment.
//
// Nothing in the suite is skipped silently on a machine that has chip-tool:
// a missing chip-tool skips (the canonical Go "missing prerequisite"), a
// missing reference-daemon binary fails loudly with the command to run.
package chiptool
