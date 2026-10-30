// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package leveltable reads the specification level files
// (parity/levels/<level>.json, extracted from matter.js's release tags by
// script/extract-levels-from-matter-js.ts) and renders what is generated
// from them: schema/levels_gen.go and the generated block of
// docs/reference/spec-levels.md (concept v11 §C.5, brief S3 part 2). It is
// a library so the drift tests (schema/levels_test.go, the reference
// daemon's level test) regenerate in memory what script/levelgen writes.
package leveltable
