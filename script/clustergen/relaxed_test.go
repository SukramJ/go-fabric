// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestRelaxationTableResolves holds the relaxation table to the snapshot:
// every entry names a cluster the generator writes, and its reference line
// lands in that cluster's definition.
func TestRelaxationTableResolves(t *testing.T) {
	t.Parallel()
	snap := testSnapshot(t)
	generated := slices.Concat(committed, definitionOnly)
	for _, s := range serverRelaxations {
		for _, e := range s.entries {
			cluster, _, _ := strings.Cut(e.key, ".")
			if !slices.Contains(generated, cluster) {
				t.Errorf("%s: %s names cluster %s, which the generator does not write", s.server, e.key, cluster)
				continue
			}
			if e.ref == "" {
				t.Errorf("%s: %s has no reference line", s.server, e.key)
			}
			o, err := generate(snap, cluster)
			if err != nil {
				t.Fatalf("%s: %v", cluster, err)
			}
			if !bytes.Contains(o.Source, []byte(strconv.Quote(e.ref))) {
				t.Errorf("%s: %s's reference line is not in the definition", s.server, e.key)
			}
		}
	}
}

// TestRelaxationNamingNothingFailsTheGeneration: an entry whose field,
// attribute or command the cluster lacks stops the generation instead of
// relaxing nothing.
func TestRelaxationNamingNothingFailsTheGeneration(t *testing.T) {
	snap := testSnapshot(t)
	saved := serverRelaxations
	t.Cleanup(func() { serverRelaxations = saved })
	for _, e := range []relaxation{
		{kind: relaxRequestField, key: "OnOff.OnWithTimedOff.NoSuchField", ref: "x"},
		{kind: relaxStructField, key: "OnOff.NoSuchStruct.Field", ref: "x"},
		{kind: relaxAttribute, key: "OnOff.NoSuchAttribute", ref: "x"},
		{kind: relaxRefusal, key: "OnOff.NoSuchCommand", refusal: statusInvalidCommand, ref: "x"},
		{kind: relaxRequestListEntry, key: "OnOff.OnWithTimedOff.OnOffControl", ref: "x"},
	} {
		serverRelaxations = []struct {
			server  string
			entries []relaxation
		}{{"test", []relaxation{e}}}
		if _, err := generate(snap, "OnOff"); err == nil || !strings.Contains(err.Error(), e.key) {
			t.Errorf("%s: generation error %v, want one naming the entry", e.key, err)
		}
	}
}
