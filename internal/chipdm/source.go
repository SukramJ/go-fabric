// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

// ChipModel is CHIP's data model as read from a connectedhomeip checkout,
// and where it came from. It lives only in memory: the XML carries a
// Connectivity Standards Alliance notice that forbids publishing it or
// deriving works from it, so nothing read from it is committed (ADR 0015).
type ChipModel struct {
	Provenance Provenance
	Model      *Model
}

// Provenance identifies the CHIP data model a comparison read.
type Provenance struct {
	Repository string

	// Commit is the connectedhomeip commit read (the Makefile's
	// CHIP_TEST_IMAGE_COMMIT, the commit the harness image was built from).
	Commit string

	// DataModel is the data_model/<version> directory read.
	DataModel string

	// Tree is git's object id of data_model/<version> at Commit — a content
	// hash of every file read.
	Tree string

	// SpecTag, SpecSHA and Scraper are the directory's own spec_tag,
	// spec_sha and scraper_version: the specification build and the
	// alchemy release CHIP generated the XML with.
	SpecTag string
	SpecSHA string
	Scraper string
}

// Repository is the upstream the data model is read from.
const Repository = "https://github.com/project-chip/connectedhomeip"

// DataModelVersion is CHIP's directory name for a Matter revision: a zero
// patch level is dropped (matter.js chip-data-model.ts versionFor).
func DataModelVersion(revision string) string {
	parts := strings.Split(revision, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	if len(parts) == 3 && parts[2] == "0" {
		parts = parts[:2]
	}
	return strings.Join(parts, ".")
}

// LoadFiles builds the CHIP model from one data model directory's files,
// keyed by their path below it ("clusters/OnOff.xml"). Namespaces are not
// read: the snapshot carries no semantic namespaces to compare them with.
func LoadFiles(files map[string][]byte) (*Model, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	l := &xmlLoader{}
	found := 0
	for _, name := range names {
		dir, file := path.Split(name)
		dir = strings.TrimSuffix(dir, "/")
		if !strings.HasSuffix(file, ".xml") {
			continue
		}
		switch dir {
		case "clusters", "device_types", "globals":
		case "namespaces":
			l.uncompared("semantic namespaces", 1)
			continue
		default:
			continue
		}
		if err := l.addFile(dir, name, files[name]); err != nil {
			return nil, err
		}
		found++
	}
	if found == 0 {
		return nil, errors.New("chipdm: no data model XML found")
	}
	return l.finish(), nil
}

// gitTimeout bounds every git call: a partial clone fetches missing blobs
// lazily, and a hung fetch would otherwise hang the test.
const gitTimeout = 5 * time.Minute

// runGit runs git in a checkout; a variable so tests can stand in for it.
var runGit = func(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...) //nolint:gosec // fixed program, this package's own arguments
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// TreeID is git's object id of data_model/<version> at commit, or an error
// when the checkout does not have the commit or the directory.
func TreeID(root, commit, version string) (string, error) {
	out, err := runGit(root, "rev-parse", "--verify", "--quiet", commit+":data_model/"+version)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ReadChipModel reads data_model/<version> at commit out of a
// connectedhomeip checkout — whatever its HEAD and sparse set: git archive
// reads the commit's tree, and a blobless partial clone fetches the blobs
// it lacks — and reduces it to the compared model.
func ReadChipModel(root, commit, version string) (*ChipModel, error) {
	tree, err := TreeID(root, commit, version)
	if err != nil {
		return nil, err
	}
	archive, err := runGit(root, "archive", "--format=tar", commit, "data_model/"+version)
	if err != nil {
		return nil, err
	}
	files, err := untar(archive, "data_model/"+version+"/")
	if err != nil {
		return nil, err
	}
	model, err := LoadFiles(files)
	if err != nil {
		return nil, err
	}
	return &ChipModel{
		Provenance: Provenance{
			Repository: Repository,
			Commit:     commit,
			DataModel:  version,
			Tree:       tree,
			SpecTag:    strings.TrimSpace(string(files["spec_tag"])),
			SpecSHA:    strings.TrimSpace(string(files["spec_sha"])),
			Scraper:    strings.TrimSpace(string(files["scraper_version"])),
		},
		Model: model,
	}, nil
}

// untar reads the regular files below prefix, keyed by their path below it.
func untar(data []byte, prefix string) (map[string][]byte, error) {
	files := map[string][]byte{}
	r := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("chipdm: reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, prefix) {
			continue
		}
		body, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("chipdm: reading %s: %w", h.Name, err)
		}
		files[strings.TrimPrefix(h.Name, prefix)] = body
	}
	return files, nil
}
