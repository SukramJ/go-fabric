// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package schema exposes typed Go lookups for Matter cluster and device-type
// metadata extracted from matter.js HEAD. The primary data lives in the
// generated files clusters.go and devicetypes.go (maps keyed by uint32 ID);
// this file adds the hand-written lookup helpers that wrap the maps.
//
// Source of truth: parity/schema.json — the same embedded snapshot every
// parity test in this module reads, extracted from a matter.js HEAD checkout
// by script/extract-from-matter-js.ts (usage block at the end of that file).
// Refreshing the snapshot is a deliberate, manual step: a revision bump can
// carry attribute, constraint and command changes that need review.
//
// After updating the snapshot, regenerate the Go code:
//
//	go generate ./schema/...
//
// Mirrors matter.js HEAD `packages/model/src/standard/elements/*.element.ts`.
package schema

//go:generate go run ../script/generate_matter_schema.go
