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
// surface itself, so it runs while levels_gen.go is missing or stale. When
// the measurement or the doc fails, levels_gen.go is put back as it was:
// a failed run leaves both files as they were (R2-P6-err-2709).
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
	"time"

	"thingfabric.dev/go-fabric/script/levelgen/leveltable"
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

func run(doc bool) (err error) {
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
	prev, prevErr := os.ReadFile(goPath)
	if err := writeIfChanged(goPath, src); err != nil {
		return err
	}
	if !doc {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		// The table was written for the daemon build; the doc did not
		// follow, so the table goes back to what it was.
		var rerr error
		if prevErr == nil {
			rerr = writeIfChanged(goPath, prev)
		} else {
			rerr = os.Remove(goPath)
		}
		if rerr != nil {
			err = fmt.Errorf("%w; restoring %s: %w", err, goPath, rerr)
		}
	}()
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

// buildTimeout and dumpTimeout bound the reference daemon's build and each
// of its dumps.
var (
	buildTimeout = 10 * time.Minute
	dumpTimeout  = 2 * time.Minute
)

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
	// Each child runs under a deadline: a build or a dump that hangs fails
	// the run, naming the step, instead of hanging make
	// generate-level(s) without a message (R2-P6-err-2717).
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), buildTimeout)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, goTool, "build", "-o", bin, "./examples/reference-bridge") //nolint:gosec // G204: the go tool the Makefile names
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	build.WaitDelay = time.Second
	if err := build.Run(); err != nil {
		if buildCtx.Err() != nil {
			err = fmt.Errorf("%w (after the %v deadline)", buildCtx.Err(), buildTimeout)
		}
		return nil, fmt.Errorf("build the reference daemon: %w", err)
	}
	out := map[string][]leveltable.Internal{}
	for fi := range files[:len(files)-1] {
		f := &files[fi]
		level := f.Matter.Revision
		b, err := dumpLevel(dir, bin, level)
		if err != nil {
			return nil, err
		}
		rows, err := ParseInternal(b)
		if err != nil {
			return nil, fmt.Errorf("level %s: %w", level, err)
		}
		out[level] = rows
	}
	return out, nil
}

// dumpLevel runs the daemon's surface dump at one level under
// dumpTimeout.
func dumpLevel(dir, bin, level string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dumpTimeout)
	defer cancel()
	dump := exec.CommandContext(ctx, bin, "--dump-surface", "--spec-level", level) //nolint:gosec // G204: the binary built above
	dump.Dir = dir
	dump.WaitDelay = time.Second
	var stderr bytes.Buffer
	dump.Stderr = &stderr
	b, err := dump.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (after the %v deadline)", ctx.Err(), dumpTimeout)
		}
		return nil, fmt.Errorf("reference-bridge --dump-surface --spec-level %s: %w\n%s", level, err, stderr.String())
	}
	return b, nil
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
	return writeAtomic(path, content)
}

// writeAtomic replaces path with content through a temporary file beside
// it and a rename, so an interrupted write leaves the old file or the new
// one, never a truncated one (R2-P6-err-2709). The file keeps the mode of
// the one it replaces, 0o644 for a new one.
func writeAtomic(path string, content []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), mode)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}
