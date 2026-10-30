// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/script/levelgen/leveltable"
	"github.com/SukramJ/go-fabric/surface"
)

// levelDump is the daemon's --dump-surface report at level ("" = no
// flag).
func levelDump(t *testing.T, level string) surface.Report {
	t.Helper()
	dir := t.TempDir()
	f := flags{
		dbPath: filepath.Join(dir, "daemon.db"), listen: "127.0.0.1:0", label: "level-test", salt: "go-fabric-ref-salt",
		iterations: 1000, passcode: 20202021, discriminator: 3840, advertise: true, specLevel: level,
	}
	var out bytes.Buffer
	if err := dumpSurface(context.Background(), f, slog.New(slog.DiscardHandler), 20202021, 3840, node.TCPConfig{}, &out); err != nil {
		t.Fatalf("level %q: %v", level, err)
	}
	var rep surface.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// levelKept are the reasons a level keeps an element it does not know.
func levelKept(reason string) bool {
	return strings.HasPrefix(reason, "kept: mandatory at pin (absent at level ") ||
		reason == surface.ReasonFeatureSetInvalid || reason == surface.ReasonSurfaceChangeSignal ||
		reason == surface.ReasonServerInternal
}

type elementKey struct {
	endpoint string
	cluster  uint32
	kind     surface.Kind
	id       uint32
}

// servedElements are the effective elements of ep, as level-table keys.
func servedElements(ep surface.Endpoint) []elementKey {
	var out []elementKey
	for _, c := range ep.Clusters {
		out = append(out, elementKey{ep.Key, c.ID, surface.KindCluster, c.ID})
		for x := c.FeatureMap; x != 0; x &= x - 1 {
			out = append(out, elementKey{ep.Key, c.ID, surface.KindFeature, uint32(1) << bits.TrailingZeros32(x)})
		}
		for _, id := range c.Attributes {
			out = append(out, elementKey{ep.Key, c.ID, surface.KindAttribute, id})
		}
		for _, id := range slices.Concat(c.Accepted, c.Generated) {
			out = append(out, elementKey{ep.Key, c.ID, surface.KindCommand, id})
		}
		for _, id := range c.Events {
			out = append(out, elementKey{ep.Key, c.ID, surface.KindEvent, id})
		}
	}
	return out
}

// TestDaemonAtEveryLevel composes the daemon's topology at each level
// (brief S3 part 5.1): no element the level does not know is served
// unless a level decision keeps it with one of the level's reasons, and
// every withheld device is out of the effective surface. Negative
// control: at the pin the surface is the one without the flag, and no
// level decision exists.
func TestDaemonAtEveryLevel(t *testing.T) {
	plain := levelDump(t, "")
	for _, l := range schema.Levels() {
		rep := levelDump(t, string(l))
		if rep.Level != l {
			t.Fatalf("level %s: report level %q", l, rep.Level)
		}
		kept := map[elementKey]bool{}
		for _, d := range rep.Decisions {
			if d.Policy == "level" && d.Verdict == surface.Keep && levelKept(d.Reason) {
				kept[elementKey{d.Endpoint, d.Cluster, d.Kind, d.ID}] = true
			}
		}
		eps := append([]surface.Endpoint{rep.Effective.Root}, rep.Effective.Endpoints...)
		for _, ep := range eps {
			for _, k := range servedElements(ep) {
				el := surface.Element{Endpoint: k.endpoint, Cluster: k.cluster, Kind: k.kind, ID: k.id}
				levels := surface.ElementLevels(el)
				if levels == 0 || levels.Has(l) || kept[k] {
					continue
				}
				t.Errorf("level %s serves %s, which it does not know, without a keep decision", l, el)
			}
		}
		withheld := map[string]bool{}
		for _, w := range rep.Withheld {
			withheld[w.Endpoint] = true
		}
		for _, ep := range rep.Effective.Endpoints {
			if withheld[ep.Key] {
				t.Errorf("level %s: withheld %s is served", l, ep.Key)
			}
		}
		if l != surface.PinLevel() {
			continue
		}
		if rep.Effective.Digest() != plain.Effective.Digest() || len(rep.Withheld) != 0 {
			t.Errorf("the pin's level differs from no flag")
		}
		for _, d := range rep.Decisions {
			if d.Policy == "level" {
				t.Errorf("a level decision at the pin: %+v", d)
			}
		}
	}
}

// TestSpecLevelsDocIsCurrent regenerates docs/reference/spec-levels.md's
// generated block — the level table, the computed exceptions and the
// server-internal elements of the daemon's composition at each level —
// and requires the committed block to equal it.
func TestSpecLevelsDocIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := leveltable.Load(filepath.Join(root, "parity", "levels"))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := leveltable.LoadPin(filepath.Join(root, "parity", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	internal := map[string][]leveltable.Internal{}
	for _, f := range files[:len(files)-1] {
		rep := levelDump(t, f.Matter.Revision)
		for _, d := range rep.Decisions {
			if d.Policy == "level" && d.Verdict == surface.Keep && d.Reason == surface.ReasonServerInternal {
				internal[f.Matter.Revision] = append(internal[f.Matter.Revision], leveltable.Internal{
					Endpoint: d.Endpoint, Cluster: d.Cluster, Kind: d.Kind.String(), ID: d.ID,
				})
			}
		}
	}
	want := leveltable.Doc(files, pin, internal)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "reference", "spec-levels.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := leveltable.Block(doc)
	if !ok || !bytes.Equal(got, want) {
		t.Error("docs/reference/spec-levels.md's generated block is stale: run `go run ./script/levelgen` and read the diff")
	}
}

// TestSpecLevelFlag: an unknown level stops the daemon with the list.
func TestSpecLevelFlag(t *testing.T) {
	_, err := surfacePolicies(flags{specLevel: "1.5"})
	if !errors.Is(err, surface.ErrUnknownLevel) || !strings.Contains(err.Error(), "--spec-level") || !strings.Contains(err.Error(), "1.5.1") {
		t.Fatalf("--spec-level 1.5: %v", err)
	}
	if p, err := surfacePolicies(flags{specLevel: " 1.4.2 "}); err != nil || p.Level != "1.4.2" {
		t.Fatalf("--spec-level 1.4.2: %+v, %v", p, err)
	}
	if !strings.Contains(levelList(), "1.6.1") {
		t.Error(levelList())
	}
}
