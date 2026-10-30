// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command surfacecheck regenerates the surface snapshot: what a controller
// sees of the reference daemon (examples/reference-bridge), composed with
// its default flags and the zero-value surface policies (concept v11 C.10
// item 1, ADR 0030). It builds the daemon, runs it with --dump-surface and
// writes
//
//   - script/surfacecheck/surface.json — the daemon's surface report as
//     --dump-surface prints it: the effective surface of the root, the
//     Aggregator and every device, the policies' digest and every
//     decision;
//   - script/surfacecheck/summary.md — the digests, the endpoint and
//     cluster counts, and the clusters per endpoint.
//
// `make surface` runs it from the module root; `make surface-check`
// regenerates and fails when either file moved. The snapshot is the
// ratchet: every change of what the daemon serves is a review diff in the
// pull request that causes it.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SukramJ/go-fabric/surface"
)

// The snapshot's files, relative to the module root.
const (
	surfacePath = "script/surfacecheck/surface.json"
	summaryPath = "script/surfacecheck/summary.md"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "surfacecheck:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "surfacecheck-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	goTool := os.Getenv("GO")
	if goTool == "" {
		goTool = "go"
	}
	bin := filepath.Join(dir, "reference-bridge")
	build := exec.CommandContext(context.Background(), goTool, "build", "-o", bin, "./examples/reference-bridge") //nolint:gosec // G204: the go tool the Makefile names
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build the reference daemon: %w", err)
	}
	// Run in the scratch directory: the dump opens no store, and nothing
	// it might write lands in the tree.
	dump := exec.CommandContext(context.Background(), bin, "--dump-surface") //nolint:gosec // G204: the binary built above
	dump.Dir = dir
	dump.Stderr = os.Stderr
	out, err := dump.Output()
	if err != nil {
		return fmt.Errorf("reference-bridge --dump-surface: %w", err)
	}
	var rep surface.Report
	if err := json.Unmarshal(out, &rep); err != nil {
		return fmt.Errorf("the dump does not parse: %w", err)
	}
	if err := os.WriteFile(surfacePath, out, 0o600); err != nil {
		return err
	}
	return os.WriteFile(summaryPath, summary(rep), 0o600)
}

// summary renders the human-readable side of the snapshot.
func summary(rep surface.Report) []byte {
	eff := rep.Effective
	all := append([]surface.Endpoint{eff.Root}, eff.Endpoints...)
	clusters := 0
	for _, ep := range all {
		clusters += len(ep.Clusters)
	}
	digest := eff.Digest()
	var b bytes.Buffer
	b.WriteString("# Surface snapshot summary\n\n")
	b.WriteString("What a controller sees of the reference daemon (`examples/reference-bridge --dump-surface`,\n")
	b.WriteString("default flags, zero-value surface policies). `make surface` regenerates it; `make surface-check`\n")
	b.WriteString("fails when it moved. The clusters the endpoint assembler mounts on a bridged endpoint\n")
	b.WriteString("(Descriptor, BridgedDeviceBasicInformation, Identify, Groups, ScenesManagement) are not part of it.\n")
	b.WriteString("The effective surface digest is the SHA-256 of the canonical JSON — every id a 0x hex string —\n")
	b.WriteString("without endpoint numbers, so a change of that encoding moves it as well.\n\n")
	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| %s | `%s` |\n", digestLabel, hex.EncodeToString(digest[:]))
	fmt.Fprintf(&b, "| Policies digest | `%s` |\n", hex.EncodeToString(rep.Policies[:]))
	fmt.Fprintf(&b, "| Endpoints (the root included) | %d |\n", len(all))
	fmt.Fprintf(&b, "| Clusters | %d |\n", clusters)
	fmt.Fprintf(&b, "| Decisions | %d |\n", len(rep.Decisions))
	fmt.Fprintf(&b, "| Withheld | %d |\n\n", len(rep.Withheld))
	b.WriteString("## Clusters per endpoint\n\n| Endpoint | Device types | Clusters |\n|---|---|---|\n")
	for _, ep := range all {
		key := "`" + ep.Key + "`"
		if ep.Key == "" {
			key = "(root)"
		}
		types := make([]string, 0, len(ep.DeviceTypes))
		for _, dt := range ep.DeviceTypes {
			types = append(types, fmt.Sprintf("0x%04X r%d", dt.ID, dt.Revision))
		}
		fmt.Fprintf(&b, "| %s | %s | %d |\n", key, strings.Join(types, ", "), len(ep.Clusters))
	}
	return b.Bytes()
}

// digestLabel names the summary row that states the effective surface's
// digest; surface_test.go reads it back.
const digestLabel = "Effective surface digest"
