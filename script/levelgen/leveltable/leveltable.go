// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package leveltable

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// File is one level file.
type File struct {
	// Name is the file's base name ("1.4.2.json").
	Name   string `json:"-"`
	Matter struct {
		Revision     string `json:"revision"`
		Tag          string `json:"tag"`
		SourceCommit string `json:"sourceCommit"`
		Method       string `json:"method"`
	} `json:"matter"`
	DeviceTypes []DeviceType `json:"deviceTypes"`
	Clusters    []Cluster    `json:"clusters"`

	raw []byte
}

// DeviceType is a device type of a level.
type DeviceType struct {
	ID       uint32 `json:"id"`
	Revision uint16 `json:"revision"`
}

// Cluster is a cluster of a level: its revision, its features by bit and
// the ids of its attributes, request commands, response commands and
// events, and those its own conformance makes mandatory ("M"). A command
// id is keyed by its direction: one id can name a request at one level
// and a response at another (Thermostat 2), so the two lists are never
// merged (review II R2-P6-par-2713, owner decision 227).
type Cluster struct {
	ID               uint32    `json:"id"`
	Revision         uint16    `json:"revision"`
	Features         []Feature `json:"features"`
	Attributes       []uint32  `json:"attributes"`
	RequestCommands  []uint32  `json:"requestCommands"`
	ResponseCommands []uint32  `json:"responseCommands"`
	Events           []uint32  `json:"events"`
	Mandatory        struct {
		Attributes       []uint32 `json:"attributes"`
		RequestCommands  []uint32 `json:"requestCommands"`
		ResponseCommands []uint32 `json:"responseCommands"`
		Events           []uint32 `json:"events"`
	} `json:"mandatory"`
}

// Feature is a FeatureMap bit and its code.
type Feature struct {
	Bit  uint8  `json:"bit"`
	Code string `json:"code"`
}

// MaxLevels is how many levels a LevelSet (uint8) holds.
const MaxLevels = 8

// Load reads every *.json of dir, ordered by level ascending.
func Load(dir string) ([]File, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []File
	for _, n := range names {
		b, err := os.ReadFile(n) //nolint:gosec // G304: the level files of the module's own tree
		if err != nil {
			return nil, err
		}
		var f File
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if err := checkDirected(b); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		f.Name, f.raw = filepath.Base(n), b
		if want := f.Matter.Revision + ".json"; f.Name != want {
			return nil, fmt.Errorf("%s: carries revision %q, so its name must be %s", n, f.Matter.Revision, want)
		}
		if _, err := ParseLevel(f.Matter.Revision); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no level file in %s", dir)
	}
	if len(out) > MaxLevels {
		return nil, fmt.Errorf("%d level files; a LevelSet holds %d", len(out), MaxLevels)
	}
	slices.SortFunc(out, func(a, b File) int { return CompareLevels(a.Matter.Revision, b.Matter.Revision) })
	return out, nil
}

// checkDirected refuses a level file in the format before owner decision
// 227: a cluster's "commands" list, request and response ids merged, which
// the reader would otherwise read as no commands at all.
func checkDirected(b []byte) error {
	var probe struct {
		Clusters []struct {
			ID        uint32          `json:"id"`
			Commands  json.RawMessage `json:"commands"`
			Mandatory struct {
				Commands json.RawMessage `json:"commands"`
			} `json:"mandatory"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	for _, c := range probe.Clusters {
		if c.Commands != nil || c.Mandatory.Commands != nil {
			return fmt.Errorf("cluster 0x%04X lists \"commands\" without their direction: re-extract the level (make generate-level)", c.ID)
		}
	}
	return nil
}

// ParseLevel splits a level id ("1.4.2") into its numbers.
func ParseLevel(s string) ([]int, error) {
	parts := strings.Split(s, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return nil, fmt.Errorf("level %q is no dotted revision", s)
		}
		out[i] = n
	}
	return out, nil
}

// CompareLevels orders two level ids numerically ("1.4" < "1.4.1" <
// "1.4.2" < "1.5.1"); an id that does not parse sorts as a string.
func CompareLevels(a, b string) int {
	x, errA := ParseLevel(a)
	y, errB := ParseLevel(b)
	if errA != nil || errB != nil {
		return strings.Compare(a, b)
	}
	return slices.Compare(x, y)
}

// Hash is the SHA-256 over the level files in level order: per file its
// name, a newline, its bytes.
func Hash(files []File) string {
	h := sha256.New()
	for fi := range files {
		f := &files[fi]
		h.Write([]byte(f.Name + "\n"))
		h.Write(f.raw)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Pin is what the pin snapshot (parity/schema.json) carries that the
// generator checks the last level against.
type Pin struct {
	Revision    string
	DeviceTypes []uint32
	Clusters    []PinCluster
	// DeviceTypeNames and ClusterNames are the pin's names, for the doc.
	DeviceTypeNames, ClusterNames map[uint32]string
}

// PinCluster is a cluster of the pin: its command ids by their effective
// direction.
type PinCluster struct {
	ID                                uint32
	Features                          []uint8
	Attributes                        []uint32
	RequestCommands, ResponseCommands []uint32
	Events                            []uint32
}

// LoadPin reads parity/schema.json's ids.
func LoadPin(path string) (Pin, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the module's own snapshot
	if err != nil {
		return Pin{}, err
	}
	var raw struct {
		Matter struct {
			Revision string `json:"revision"`
		} `json:"matter"`
		DeviceTypes []struct {
			ID   uint32 `json:"id"`
			Name string `json:"name"`
		} `json:"deviceTypes"`
		Clusters []struct {
			ID       uint32 `json:"id"`
			Name     string `json:"name"`
			Features []struct {
				Name string `json:"name"`
				Bit  *uint8 `json:"bit"`
			} `json:"features"`
			Attributes []struct {
				ID uint32 `json:"id"`
			} `json:"attributes"`
			Commands []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Direction string `json:"direction"`
				Effective *struct {
					Direction string `json:"direction"`
				} `json:"effective"`
			} `json:"commands"`
			Events []struct {
				ID uint32 `json:"id"`
			} `json:"events"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Pin{}, fmt.Errorf("%s: %w", path, err)
	}
	p := Pin{Revision: raw.Matter.Revision, DeviceTypeNames: map[uint32]string{}, ClusterNames: map[uint32]string{}}
	for _, d := range raw.DeviceTypes {
		p.DeviceTypes = append(p.DeviceTypes, d.ID)
		p.DeviceTypeNames[d.ID] = d.Name
	}
	for _, c := range raw.Clusters {
		p.ClusterNames[c.ID] = c.Name
		pc := PinCluster{ID: c.ID}
		for _, f := range c.Features {
			// A feature without a usable bit is refused, not skipped: the
			// check against the last level would miss it (R2-P6-par-2714).
			if f.Bit == nil {
				return Pin{}, fmt.Errorf("%s: %s.%s: feature without a bit", path, c.Name, f.Name)
			}
			if *f.Bit > 31 {
				return Pin{}, fmt.Errorf("%s: %s.%s: feature bit %d outside the 32-bit FeatureMap", path, c.Name, f.Name, *f.Bit)
			}
			pc.Features = append(pc.Features, *f.Bit)
		}
		for _, a := range c.Attributes {
			pc.Attributes = append(pc.Attributes, a.ID)
		}
		for _, x := range c.Commands {
			// The effective direction, the raw one when the effective
			// layer states none (generate_matter_schema's commandDirection).
			dir := x.Direction
			if x.Effective != nil && x.Effective.Direction != "" {
				dir = x.Effective.Direction
			}
			switch dir {
			case "request":
				pc.RequestCommands = append(pc.RequestCommands, x.ID)
			case "response":
				pc.ResponseCommands = append(pc.ResponseCommands, x.ID)
			default:
				return Pin{}, fmt.Errorf("%s: %s.%s: command direction %q", path, c.Name, x.Name, dir)
			}
		}
		for _, e := range c.Events {
			pc.Events = append(pc.Events, e.ID)
		}
		p.Clusters = append(p.Clusters, pc)
	}
	return p, nil
}

// CheckPin fails when the last level is not the pin's revision, or when
// an element of the pin is unknown to it, naming the element.
func CheckPin(files []File, pin Pin) error {
	last := files[len(files)-1]
	if last.Matter.Revision != pin.Revision {
		return fmt.Errorf("the last level is %s, the pin's revision %s", last.Matter.Revision, pin.Revision)
	}
	dts := map[uint32]bool{}
	for _, d := range last.DeviceTypes {
		dts[d.ID] = true
	}
	var missing []string
	for _, id := range pin.DeviceTypes {
		if !dts[id] {
			missing = append(missing, fmt.Sprintf("device type 0x%04X", id))
		}
	}
	cls := map[uint32]*Cluster{}
	for i := range last.Clusters {
		cls[last.Clusters[i].ID] = &last.Clusters[i]
	}
	for pi := range pin.Clusters {
		pc := &pin.Clusters[pi]
		c := cls[pc.ID]
		if c == nil {
			missing = append(missing, fmt.Sprintf("cluster 0x%04X", pc.ID))
			continue
		}
		for _, b := range pc.Features {
			if !slices.ContainsFunc(c.Features, func(f Feature) bool { return f.Bit == b }) {
				missing = append(missing, fmt.Sprintf("feature bit %d of cluster 0x%04X", b, pc.ID))
			}
		}
		for _, k := range []struct {
			kind      string
			pin, have []uint32
		}{
			{"attribute", pc.Attributes, c.Attributes},
			{"request command", pc.RequestCommands, c.RequestCommands},
			{"response command", pc.ResponseCommands, c.ResponseCommands},
			{"event", pc.Events, c.Events},
		} {
			for _, id := range k.pin {
				if !slices.Contains(k.have, id) {
					missing = append(missing, fmt.Sprintf("%s 0x%04X of cluster 0x%04X", k.kind, id, pc.ID))
				}
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("level %s (the pin's) does not know: %s", last.Matter.Revision, strings.Join(missing, ", "))
	}
	return nil
}

// set is a LevelSet under construction.
type set = uint8

// GoSource renders schema/levels_gen.go, gofmt-formatted.
func GoSource(files []File) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Code generated by script/levelgen from parity/levels/*.json — DO NOT EDIT.
// Run ` + "`go run ./script/levelgen`" + ` to regenerate.

package schema

`)
	b.WriteString("// levels are the specification levels, ascending: the Matter revisions\n")
	b.WriteString("// matter.js's release tags model (parity/levels/<level>.json).\n")
	b.WriteString("var levels = []Level{\n")
	for fi := range files {
		f := &files[fi]
		fmt.Fprintf(&b, "\t%q, // matter.js %s (%s), method %s\n", f.Matter.Revision, f.Matter.Tag, short(f.Matter.SourceCommit), f.Matter.Method)
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "// LevelsSnapshotSHA256 is the SHA-256 over the level files in level\n// order (per file: its name, a newline, its bytes) at generation time;\n// TestLevelsSnapshotHash holds it to the files.\nconst LevelsSnapshotSHA256 = %q\n\n", Hash(files))

	dts := map[uint32]set{}
	type cl struct {
		levels                                  set
		revisions                               []uint16
		features                                map[uint8]set
		attributes, requests, responses, events map[uint32]set
	}
	cls := map[uint32]*cl{}
	for i := range files {
		f := &files[i]
		bit := set(1) << i
		for _, d := range f.DeviceTypes {
			dts[d.ID] |= bit
		}
		for ci := range f.Clusters {
			c := &f.Clusters[ci]
			x := cls[c.ID]
			if x == nil {
				x = &cl{
					revisions: make([]uint16, len(files)), features: map[uint8]set{},
					attributes: map[uint32]set{}, requests: map[uint32]set{}, responses: map[uint32]set{}, events: map[uint32]set{},
				}
				cls[c.ID] = x
			}
			x.levels |= bit
			x.revisions[i] = c.Revision
			for _, ft := range c.Features {
				x.features[ft.Bit] |= bit
			}
			mark(x.attributes, c.Attributes, bit)
			mark(x.requests, c.RequestCommands, bit)
			mark(x.responses, c.ResponseCommands, bit)
			mark(x.events, c.Events, bit)
		}
	}
	b.WriteString("// deviceTypeLevels are the levels each device type exists at.\n")
	b.WriteString("var deviceTypeLevels = map[DeviceTypeID]LevelSet{\n")
	for _, id := range sortedKeys(dts) {
		fmt.Fprintf(&b, "\t0x%04X: 0x%02X,\n", id, dts[id])
	}
	b.WriteString("}\n\n")
	b.WriteString("// clusterLevels are each cluster's levels, its ClusterRevision per level\n")
	b.WriteString("// (0 where absent) and the levels of its features (by bit), attributes,\n")
	b.WriteString("// request commands, response commands and events (by id).\n")
	b.WriteString("var clusterLevels = map[uint32]*levelCluster{\n")
	for _, id := range sortedKeys(cls) {
		x := cls[id]
		fmt.Fprintf(&b, "\t0x%04X: {\n\t\tlevels: 0x%02X,\n\t\trevisions: [MaxLevels]uint16{", id, x.levels)
		for i, r := range x.revisions {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%d", r)
		}
		b.WriteString("},\n")
		writeSetMap(&b, "features", x.features, "%d")
		writeSetMap(&b, "attributes", x.attributes, "0x%04X")
		writeSetMap(&b, "requestCommands", x.requests, "0x%02X")
		writeSetMap(&b, "responseCommands", x.responses, "0x%02X")
		writeSetMap(&b, "events", x.events, "0x%02X")
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return formatSource(b.Bytes())
}

// mark adds bit to the set of every id.
func mark(m map[uint32]set, ids []uint32, bit set) {
	for _, id := range ids {
		m[id] |= bit
	}
}

func writeSetMap[K ~uint8 | ~uint32](b *bytes.Buffer, field string, m map[K]set, idFmt string) {
	if len(m) == 0 {
		return
	}
	fmt.Fprintf(b, "\t\t%s: map[%s]LevelSet{", field, keyType[K]())
	for i, k := range sortedKeys(m) {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, idFmt+": 0x%02X", k, m[k])
	}
	b.WriteString("},\n")
}

func keyType[K ~uint8 | ~uint32]() string {
	var k K
	if any(k) == any(uint8(0)) {
		return "uint8"
	}
	return "uint32"
}

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func short(commit string) string {
	if len(commit) > 9 {
		return commit[:9]
	}
	return commit
}
