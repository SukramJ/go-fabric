// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command levelgen generates the specification-level table (concept v11
// §C.5, brief S3 part 2) from parity/levels/*.json: schema/levels_gen.go,
// then the generated block of docs/reference/spec-levels.md. It fails when
// the last level is not the pin's revision or does not know an element of
// the pin (parity/schema.json), naming the element.
//
// The doc's server-internal section is measured on the reference daemon:
// levelgen builds examples/reference-bridge (after writing levels_gen.go,
// which the build needs) and runs it with --dump-surface --spec-level
// <level> for every level below the pin. It imports neither schema nor
// surface itself, so it runs while levels_gen.go is missing or stale.
//
//	go run ./script/levelgen          # from the module root
//	go run ./script/levelgen -doc=false
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/script/levelgen/leveltable"
)

const (
	levelsDir = "parity/levels"
	pinPath   = "parity/schema.json"
	goPath    = "schema/levels_gen.go"
	docPath   = "docs/reference/spec-levels.md"
	// InternalReason is the level policy's reason for an effective element
	// absent at the level that no candidate decided (surface package).
	internalReason = "kept: server-internal, not level-controlled"
)

func main() {
	doc := flag.Bool("doc", true, "also regenerate docs/reference/spec-levels.md (builds and runs the reference daemon)")
	flag.Parse()
	if err := run(*doc); err != nil {
		fmt.Fprintln(os.Stderr, "levelgen:", err)
		os.Exit(1)
	}
}

func run(doc bool) error {
	files, err := leveltable.Load(levelsDir)
	if err != nil {
		return err
	}
	pin, err := leveltable.LoadPin(pinPath)
	if err != nil {
		return err
	}
	if err := leveltable.CheckPin(files, pin); err != nil {
		return err
	}
	src, err := leveltable.GoSource(files)
	if err != nil {
		return err
	}
	if err := writeIfChanged(goPath, src); err != nil {
		return err
	}
	if !doc {
		return nil
	}
	internal, err := measureInternal(files)
	if err != nil {
		return err
	}
	block := leveltable.Doc(files, pin, internal)
	old, err := os.ReadFile(docPath)
	if err != nil {
		return err
	}
	out, err := leveltable.Splice(old, block)
	if err != nil {
		return fmt.Errorf("%s: %w", docPath, err)
	}
	return writeIfChanged(docPath, out)
}

// measureInternal runs the reference daemon's dump at every level below
// the pin and collects its server-internal keep decisions.
func measureInternal(files []leveltable.File) (map[string][]leveltable.Internal, error) {
	dir, err := os.MkdirTemp("", "levelgen-")
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("build the reference daemon: %w", err)
	}
	out := map[string][]leveltable.Internal{}
	for _, f := range files[:len(files)-1] {
		level := f.Matter.Revision
		dump := exec.CommandContext(context.Background(), bin, "--dump-surface", "--spec-level", level) //nolint:gosec // G204: the binary built above
		dump.Dir = dir
		var stderr bytes.Buffer
		dump.Stderr = &stderr
		b, err := dump.Output()
		if err != nil {
			return nil, fmt.Errorf("reference-bridge --dump-surface --spec-level %s: %w\n%s", level, err, stderr.String())
		}
		rows, err := ParseInternal(b)
		if err != nil {
			return nil, fmt.Errorf("level %s: %w", level, err)
		}
		out[level] = rows
	}
	return out, nil
}

// ParseInternal reads the server-internal keep decisions of a surface
// report's JSON (surface.Report.MarshalJSON's encoding).
func ParseInternal(b []byte) ([]leveltable.Internal, error) {
	var rep struct {
		Decisions []struct {
			Cluster  string `json:"cluster"`
			Endpoint string `json:"endpoint"`
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Policy   string `json:"policy"`
			Reason   string `json:"reason"`
			Verdict  string `json:"verdict"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		return nil, fmt.Errorf("the dump does not parse: %w", err)
	}
	var out []leveltable.Internal
	for _, d := range rep.Decisions {
		if d.Policy != "level" || d.Verdict != "keep" || d.Reason != internalReason {
			continue
		}
		c, err := hexID(d.Cluster)
		if err != nil {
			return nil, err
		}
		id, err := hexID(d.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, leveltable.Internal{Endpoint: d.Endpoint, Cluster: c, Kind: d.Kind, ID: id})
	}
	return out, nil
}

func hexID(s string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("id %q: %w", s, err)
	}
	return uint32(v), nil
}

// writeIfChanged writes content to path only when it differs from what is
// on disk, keeping mtimes stable on repeated runs.
func writeIfChanged(path string, content []byte) error {
	old, err := os.ReadFile(path) //nolint:gosec // G304: the module's own generated files
	if err == nil && bytes.Equal(old, content) {
		fmt.Printf("  (unchanged: %s)\n", path)
		return nil
	}
	return os.WriteFile(path, content, 0o600)
}
