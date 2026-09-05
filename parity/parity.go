// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package parity provides the matter.js HEAD schema snapshot to all
// matter-side parity tests in one embed location. Mirrors matter.js
// HEAD `packages/model/src/standard/elements/*.element.ts`.
//
// schema.json is the single copy of the extract in this module: the parity
// tests read it through SchemaJSON, and script/generate_matter_schema.go
// reads the same file to emit package schema's typed maps. Refresh it with
// script/extract-from-matter-js.ts (usage block at the end of that file),
// then run `go generate ./schema/...` — a snapshot refreshed without that
// second step leaves the generated revision maps describing the old extract,
// which schema.SchemaSnapshotSHA256 is there to catch.
//
// A host application may pin its own expected copy of these bytes to keep an
// unintended schema change from arriving silently in a dependency bump; the
// pin belongs to that repository, not here.
package parity

import _ "embed"

//go:embed schema.json
var schemaJSON []byte

// SchemaJSON returns the matter.js HEAD schema snapshot as raw JSON
// bytes. Callers (parity_matterjs_test.go in cluster + model packages)
// unmarshal into their local cluster/devicetype struct shapes.
func SchemaJSON() []byte {
	return append([]byte(nil), schemaJSON...) // defensive copy
}
