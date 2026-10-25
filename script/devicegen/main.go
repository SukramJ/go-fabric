// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command devicegen generates the device layer's device types from the
// resolved layer of parity/schema.json (the matter.js model extract): one
// file per device type in scope under device/, holding the type's id and
// revision, its [device.Type] with the cluster requirements matter.js's
// generator derives, a constructor and the typed accessors of its default
// servers — plus device/types_gen.go, the registry of every generated
// type, device/types_gen_test.go, which calls every constructor and
// accessor, device/doc_gen.go, the package doc with the lists of typed
// cluster packages and device types, docs/reference/device-types.md, the
// requirement tables of every type (docs.go), and
// contract/devicetype_names_gen.go, the label table of
// contract.DeviceTypeName over every snapshot device type (labels.go).
//
// It is matter.js's support/codegen/src/endpoints translated: matter.js
// generates packages/node/src/devices and packages/node/src/endpoints from
// the model (EndpointFile.ts, RequirementGenerator.ts,
// ClusterRequirements.ts); so does this module. See
// docs/adr/0018-device-layer-and-node-facade.md.
//
// Usage, from the module root (make generate-matter-schema runs it after
// clustergen, whose committed definitions it references):
//
//	go run ./script/devicegen            # regenerate device/*_gen.go
//	go run ./script/devicegen -out DIR   # into DIR instead (the reference
//	                                     # page and the contract label
//	                                     # table too, unless -contract-out
//	                                     # names another for the latter)
//
// The committed set is the device types in scope (committed in
// devicetypes.go); every other snapshot device type is listed in
// excluded with its reason.
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
	outDir       = "device"
	specDir      = "cluster/spec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "devicegen:", err)
		os.Exit(1)
	}
}

// run is main without the process: it parses args, generates and writes.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("devicegen", flag.ContinueOnError)
	fs.SetOutput(stdout)
	out := fs.String("out", "", "output directory (default: device of this module)")
	contractOut := fs.String("contract-out", "", "output directory of the contract label table (default: -out if given, else contract of this module)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	g, err := newGenerator(root)
	if err != nil {
		return err
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join(root, outDir)
	}
	cdir := *contractOut
	switch {
	case cdir != "":
	case *out != "":
		cdir = *out
	default:
		cdir = filepath.Join(root, contractDir)
	}
	files, err := g.all()
	if err != nil {
		return err
	}
	names, err := g.generateContractNames()
	if err != nil {
		return err
	}
	ref, err := g.generateReference(committed)
	if err != nil {
		return err
	}
	rdir := filepath.Join(root, referenceDir)
	if *out != "" {
		rdir = *out
	}
	if err := writeAll(stdout, dir, files...); err != nil {
		return err
	}
	if err := writeAll(stdout, rdir, ref); err != nil {
		return err
	}
	return writeAll(stdout, cdir, names)
}

// writeAll creates dir and writes every file into it, naming each one it
// changed on stdout.
func writeAll(stdout io.Writer, dir string, files ...*output) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, o := range files {
		changed, err := write(dir, o)
		if err != nil {
			return err
		}
		if changed {
			_, _ = fmt.Fprintf(stdout, "wrote %s\n", filepath.Join(dir, o.File))
		}
	}
	return nil
}

// newGenerator loads the snapshot and notes which cluster definitions are
// committed under cluster/spec.
func newGenerator(root string) (*generator, error) {
	snap, err := loadSnapshot(filepath.Join(root, snapshotPath))
	if err != nil {
		return nil, err
	}
	defs := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, specDir))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A package with a generated server (server_gen.go): its Declare,
		// State, Patch and Server. A definition-only package (state_gen.go,
		// a cluster a hand-written core server serves, ADR 0013 amendment
		// of 2026-10-25) is no typed package of a device.
		if _, err := os.Stat(filepath.Join(root, specDir, e.Name(), "server_gen.go")); err == nil {
			defs[e.Name()] = true
		}
	}
	return &generator{snap: snap, defs: defs}, nil
}

// all generates every committed file.
func (g *generator) all() ([]*output, error) {
	var files []*output
	for _, name := range committed {
		o, err := g.generate(name)
		if err != nil {
			return nil, err
		}
		files = append(files, o)
	}
	reg, err := g.generateRegistry(committed)
	if err != nil {
		return nil, err
	}
	test, err := g.generateTest(committed)
	if err != nil {
		return nil, err
	}
	doc, err := g.generatePackageDoc(committed)
	if err != nil {
		return nil, err
	}
	hosts, err := g.generateHostServers()
	if err != nil {
		return nil, err
	}
	return append(files, reg, test, doc, hosts), nil
}

// write puts one file in place, touching it only when its content changed.
func write(dir string, o *output) (changed bool, err error) {
	path := filepath.Join(dir, o.File)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, o.Source) { //nolint:gosec // a path below the output directory this tool writes
		return false, nil
	}
	if err := os.WriteFile(path, o.Source, 0o644); err != nil { //nolint:gosec // generated source, world-readable like the rest of the tree
		return false, err
	}
	return true, nil
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
