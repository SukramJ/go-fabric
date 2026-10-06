// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Command clustergen generates cluster definitions from the resolved layer
// of parity/schema.json (the matter.js model extract): one package per
// cluster under cluster/spec/, holding the cluster's ids, typed enums,
// bitmaps and structs with their TLV codecs, the command and event
// payloads, the spec.Cluster definition the cluster/spec runtime works from,
// and a generated test that holds the definition against the snapshot and
// round-trips every codec.
//
// It is matter.js's support/codegen translated: matter.js generates its
// cluster types (packages/types/src/clusters) from the model and writes its
// behaviors by hand; so does this module. See
// docs/adr/0013-generated-cluster-definitions.md.
//
// Usage, from the module root (make generate-matter-schema runs it):
//
//	go run ./script/clustergen            # regenerate the committed clusters
//	go run ./script/clustergen -all -out DIR
//	                                      # every cluster into DIR
//
// The committed set is the clusters a server in this module is built on
// (committed in clusters.go); adding one is adding its name there.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const (
	snapshotPath = "parity/schema.json"
	outDir       = "cluster/spec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "clustergen:", err)
		os.Exit(1)
	}
}

// run is main without the process: it parses args, generates and writes.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("clustergen", flag.ContinueOnError)
	fs.SetOutput(stdout)
	all := fs.Bool("all", false, "generate every cluster in the snapshot")
	out := fs.String("out", "", "output directory (default: cluster/spec of this module)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	snap, err := loadSnapshot(filepath.Join(root, snapshotPath))
	if err != nil {
		return err
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join(root, outDir)
	}
	names := committed
	if *all {
		names = sortedClusterNames(snap)
	}
	for _, name := range names {
		o, err := generate(snap, name)
		if err != nil {
			return err
		}
		changed, err := write(dir, o)
		if err != nil {
			return err
		}
		if changed {
			_, _ = fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(dir, o.Package))
		}
	}
	return nil
}

// write puts one package's files in place, touching only files whose
// content changed.
func write(dir string, o *output) (changed bool, err error) {
	pkgDir := filepath.Join(dir, o.Package)
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		return false, err
	}
	for name, content := range map[string][]byte{"definition_gen.go": o.Source, "definition_gen_test.go": o.Test} {
		path := filepath.Join(pkgDir, name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) { //nolint:gosec // a path below the output directory this tool writes

			continue
		}
		if err := os.WriteFile(path, content, 0o644); err != nil { //nolint:gosec // generated source, world-readable like the rest of the tree
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// moduleRoot returns the directory holding this module's go.mod, found from
// this source file so `go generate` and a run from the root agree.
func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed: cannot locate the module root")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", file)
		}
		dir = parent
	}
}
