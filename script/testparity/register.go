// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Status is how far a matter.js test file is ported.
type Status string

// The four statuses, in the order the page counts them.
const (
	StatusPorted   Status = "ported"
	StatusPartial  Status = "partial"
	StatusPlanned  Status = "planned"
	StatusExcluded Status = "excluded"
)

var statuses = []Status{StatusPorted, StatusPartial, StatusPlanned, StatusExcluded}

// The exclusion classes. Nothing else is a reason to leave a file out.
const (
	ClassControllerStage    = "controller-stage"
	ClassJSRuntimeOnly      = "js-runtime-only"
	ClassJointFabric        = "joint-fabric"
	ClassDeviceBLEThread    = "device-ble-thread"
	ClassTestInfrastructure = "test-infrastructure"
)

var classes = map[string]bool{
	ClassControllerStage:    true,
	ClassJSRuntimeOnly:      true,
	ClassJointFabric:        true,
	ClassDeviceBLEThread:    true,
	ClassTestInfrastructure: true,
}

// Register is register.json.
type Register struct {
	Entries []Entry `json:"entries"`
}

// Entry is the register's verdict on one snapshot path. The package doc
// gives the field rules; validate enforces them.
type Entry struct {
	Path        string   `json:"path"`
	Status      Status   `json:"status"`
	Go          []string `json:"go,omitempty"`
	Missing     []string `json:"missing,omitempty"`
	Class       string   `json:"class,omitempty"`
	Until       string   `json:"until,omitempty"`
	Reason      string   `json:"reason"`
	SnapshotSHA string   `json:"snapshotSha"`
}

var (
	shaPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	goRefPattern = regexp.MustCompile(`^[a-z0-9_-]+(?:/[a-z0-9_-]+)*:Test[A-Za-z0-9_]*$`)
	// untilPattern is the shape of an until: a roadmap milestone, with or
	// without its phase. A planned entry carries one when its subject is
	// work of a later phase (the device ICD role of milestone 5), so the
	// trees phase 16 brings to 100 % are checked as "ported, or until a
	// named phase"; an excluded controller-stage or device-ble-thread entry
	// always carries one.
	untilPattern = regexp.MustCompile(`^M[1-7]( phase \d{1,2})?$`)
)

func loadRegister(p string) (*Register, error) {
	raw, err := os.ReadFile(p) //nolint:gosec // G304: the committed register under the module root
	if err != nil {
		return nil, err
	}
	var reg Register
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if err := reg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &reg, nil
}

// validate checks every entry's fields on their own and the register's order;
// it does not consult the snapshot (check does).
func (r *Register) validate() error {
	var errs []error
	for i := range r.Entries {
		e := &r.Entries[i]
		if err := e.validate(); err != nil {
			errs = append(errs, fmt.Errorf("entry %d (%s): %w", i, e.Path, err))
		}
		if i > 0 && r.Entries[i-1].Path >= e.Path {
			errs = append(errs, fmt.Errorf("entry %d (%s): not sorted after %s, or a duplicate", i, e.Path, r.Entries[i-1].Path))
		}
	}
	return errors.Join(errs...)
}

func (e *Entry) validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if e.Path == "" {
		fail("no path")
	}
	if strings.TrimSpace(e.Reason) == "" {
		fail("no reason")
	}
	if !shaPattern.MatchString(e.SnapshotSHA) {
		fail("snapshotSha %q is not a blob id", e.SnapshotSHA)
	}
	switch e.Status {
	case StatusPorted, StatusPartial:
		if len(e.Go) == 0 {
			fail("%s without go references", e.Status)
		}
		for _, ref := range e.Go {
			if !goRefPattern.MatchString(ref) {
				fail("go reference %q is not <package dir>:<TestFunc>", ref)
			}
		}
		if e.Status == StatusPorted && len(e.Missing) > 0 {
			fail("ported with missing it cases; that is partial")
		}
		if e.Status == StatusPartial && len(e.Missing) == 0 {
			fail("partial without missing it cases; that is ported")
		}
		if e.Class != "" || e.Until != "" {
			fail("class/until on a %s entry", e.Status)
		}
	case StatusPlanned:
		if len(e.Go) > 0 || len(e.Missing) > 0 || e.Class != "" {
			fail("planned carries only path, status, reason, snapshotSha and until")
		}
	case StatusExcluded:
		if len(e.Go) > 0 || len(e.Missing) > 0 {
			fail("go/missing on an excluded entry")
		}
		if !classes[e.Class] {
			fail("unknown class %q", e.Class)
		}
		switch {
		case e.Class == ClassControllerStage && e.Until == "":
			fail("controller-stage without until")
		case e.Until != "" && e.Class != ClassControllerStage && e.Class != ClassDeviceBLEThread:
			fail("until on class %s", e.Class)
		}
	default:
		fail("unknown status %q", e.Status)
	}
	if e.Until != "" && !untilPattern.MatchString(e.Until) {
		fail("until %q is not a roadmap milestone or phase (M5, M5 phase 23)", e.Until)
	}
	return errors.Join(errs...)
}

// check holds the register against the snapshot: the same paths, both ways,
// and a ported, partial or excluded verdict checked against the blob now
// pinned. A planned entry's snapshotSha is a record only: nothing is claimed
// about the file that a change could falsify.
func (r *Register) check(snap *Snapshot) error {
	var errs []error
	byPath := make(map[string]SnapshotFile, len(snap.Files))
	for _, f := range snap.Files {
		byPath[f.Path] = f
	}
	seen := make(map[string]bool, len(r.Entries))
	for i := range r.Entries {
		e := &r.Entries[i]
		seen[e.Path] = true
		f, ok := byPath[e.Path]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: in the register but not in the snapshot — the file is gone upstream; remove the entry", e.Path))
			continue
		}
		if e.SnapshotSHA != f.SHA {
			switch e.Status {
			case StatusPorted, StatusPartial:
				errs = append(errs, fmt.Errorf("%s: %s against blob %s, the snapshot pins %s — re-check the mapping", e.Path, e.Status, e.SnapshotSHA, f.SHA))
			case StatusExcluded:
				errs = append(errs, fmt.Errorf("%s: excluded against blob %s, the snapshot pins %s — re-check the exclusion", e.Path, e.SnapshotSHA, f.SHA))
			case StatusPlanned:
				// A record only: a planned entry claims nothing a change could falsify.
			}
		}
	}
	var missing []string
	for p := range byPath {
		if !seen[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		errs = append(errs, fmt.Errorf("%s: in the snapshot but not in the register — add an entry", p))
	}
	return errors.Join(errs...)
}

// count returns the number of entries per status.
func (r *Register) count() map[Status]int {
	n := make(map[Status]int, len(statuses))
	for i := range r.Entries {
		n[r.Entries[i].Status]++
	}
	return n
}
