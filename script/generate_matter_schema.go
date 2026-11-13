// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build ignore

// generate_matter_schema reads the matter.js HEAD schema snapshot embedded at
// parity/schema.json and emits typed Go constant maps under schema/:
//
//   - clusters.go  — ClusterRevisions, ClusterNames (map[uint32]uint16/string)
//   - devicetypes.go — DeviceTypeRevisions, DeviceTypeNames, DeviceTypeServerClusters
//   - attribute_access_gen.go — attributeWritePrivileges (writable attributes
//     whose effective write privilege is above Operate), changesOmittedAttributes,
//     fabricScopedWritePaths (writable attributes whose effective access
//     is fabric-scoped or fabric-sensitive) and readOnlyAttributes (every
//     attribute below the global range that matter.js does not let a remote
//     party write)
//   - devicetype_requirements_gen.go — deviceTypeDefinitions (each device
//     type's requirement tree, conditions and composition: the device-type
//     layer of the snapshot), baseDeviceTypeDefinitions, clusterFeatures,
//     clusterClassifications, unbindableClusters
//   - timed_gen.go — timedInvokePaths (every request command whose
//     effective access carries "T", the timed-interaction quality) and
//     timedWritePaths (every attribute whose effective access carries it)
//   - large_message_gen.go — largeMessageCommands (every request command
//     whose effective quality carries "L", the Large Message quality)
//   - schema_provenance_gen.go — SchemaSnapshotSHA256
//
// It reads the same bytes package parity embeds rather than a second copy of
// the extract, so the generated constants cannot describe a snapshot no test
// validates against.
//
// The generated files carry a DO-NOT-EDIT banner and the module SPDX header.
// Run it through the schema package's go:generate directive:
//
//	go generate ./schema/...
//
// Or directly:
//
//	go run ./script/generate_matter_schema.go
//
// Refreshing the snapshot itself is a separate, manual step — see the usage
// block at the end of script/extract-from-matter-js.ts.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Paths are module-relative; main resolves them against the module root so the
// generator behaves the same run from the root and run by `go generate` with
// the schema package directory as its working directory.
const (
	snapshotPath    = "parity/schema.json"
	schemaDir       = "schema"
	clustersFile    = "schema/clusters.go"
	devicetypesFile = "schema/devicetypes.go"
	provenanceFile  = "schema/schema_provenance_gen.go"
	accessFile      = "schema/attribute_access_gen.go"
	requirementFile = "schema/devicetype_requirements_gen.go"
	timedFile       = "schema/timed_gen.go"
	largeMsgFile    = "schema/large_message_gen.go"
	namesFile       = "schema/element_names_gen.go"
)

// moduleRoot returns the directory holding this module's go.mod, found by
// walking up from this source file. Anchoring on the source rather than on the
// working directory keeps `go generate ./schema/...` (which runs with the
// package directory as cwd) reading and writing the same tree as a run from
// the module root.
func moduleRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed: cannot locate the module root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// snapshotCluster mirrors the cluster shape in parity/schema.json.
type snapshotCluster struct {
	ID             uint32              `json:"id"`
	Name           string              `json:"name"`
	Revision       uint16              `json:"revision"`
	FeatureMap     uint32              `json:"featureMap"`
	Attributes     []snapshotAttribute `json:"attributes"`
	Commands       []snapshotCommand   `json:"commands"`
	Events         []snapshotEvent     `json:"events"`
	Features       []snapshotFeature   `json:"features"`
	Classification string              `json:"classification"`
	Bindable       *bool               `json:"bindable"`
}

// snapshotFeature mirrors one feature of a cluster in parity/schema.json.
type snapshotFeature struct {
	Name string `json:"name"`
	Bit  *int   `json:"bit"`
	// Effective carries the feature's title (matter.js FeatureModel.title).
	Effective *struct {
		Title string `json:"title"`
	} `json:"effective"`
}

// snapshotEvent mirrors one event of a cluster in parity/schema.json.
type snapshotEvent struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// snapshotAttribute mirrors one attribute of a cluster in parity/schema.json.
type snapshotAttribute struct {
	ID      uint32 `json:"id"`
	Name    string `json:"name"`
	Access  string `json:"access"`
	Quality string `json:"quality"`
	// Effective carries the attribute's resolved access — what matter.js's
	// effectiveAccess answers, a derived cluster's inherited attribute
	// included — field by field (the extractor's accessOut), with the timed
	// quality ("T") as access.timed, as for a command — and its resolved
	// quality, of which the fixed flag ("F") is read here.
	Effective *struct {
		Access  snapshotAccess `json:"access"`
		Quality struct {
			Fixed bool `json:"fixed"`
		} `json:"quality"`
	} `json:"effective"`
}

// snapshotAccess mirrors an effective access in parity/schema.json: the
// fields of matter.js's Access the extractor writes (accessOut), each
// absent when matter.js leaves it undefined.
type snapshotAccess struct {
	RW        string `json:"rw"`
	ReadPriv  string `json:"readPriv"`
	WritePriv string `json:"writePriv"`
	Fabric    string `json:"fabric"`
	Timed     bool   `json:"timed"`
}

// String displays the access in the standard Matter syntax, as matter.js
// Access.toString does (packages/model/src/aspects/Access.ts:193-223 at
// db021da0): rw, the fabric flag, the privileges — once when read and write
// agree — and "T".
func (a snapshotAccess) String() string {
	var parts []string
	if a.RW != "" {
		parts = append(parts, a.RW)
	}
	if a.Fabric != "" {
		parts = append(parts, a.Fabric)
	}
	switch {
	case a.ReadPriv == a.WritePriv:
		if a.ReadPriv != "" {
			parts = append(parts, a.ReadPriv)
		}
	case a.ReadPriv != "":
		parts = append(parts, a.ReadPriv+a.WritePriv)
	default:
		parts = append(parts, a.WritePriv)
	}
	if a.Timed {
		parts = append(parts, "T")
	}
	return strings.Join(parts, " ")
}

// snapshotCommand mirrors one command of a cluster in parity/schema.json:
// its direction and the access and quality of its effective layer, where
// the extractor records the timed quality ("T") as access.timed and the
// Large Message quality ("L") as quality.largeMessage.
type snapshotCommand struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Effective *struct {
		// Direction is the effective direction: a derived cluster's
		// override of a base command (RvcOperationalState's Pause) states
		// none of its own and inherits the base's here.
		Direction string `json:"direction"`
		Access    struct {
			Timed bool `json:"timed"`
		} `json:"access"`
		Quality struct {
			LargeMessage bool `json:"largeMessage"`
		} `json:"quality"`
	} `json:"effective"`
}

// snapshotRequirement mirrors one cluster requirement of a device type in
// parity/schema.json: which cluster the Matter Device Library
// specifies for the type, on which side, and under which conformance.
type snapshotRequirement struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Element     string `json:"element"`
	Conformance string `json:"conformance"`
}

// snapshotDeviceType mirrors the deviceType shape in parity/schema.json.
type snapshotDeviceType struct {
	ID             uint32                `json:"id"`
	Name           string                `json:"name"`
	Classification string                `json:"classification"`
	Revision       uint16                `json:"revision"`
	Requirements   []snapshotRequirement `json:"requirements"`
	Effective      *snapshotDTEffective  `json:"effective"`
}

// snapshotDTEffective mirrors a device type's effective layer: what
// matter.js's device type validation reads of it.
type snapshotDTEffective struct {
	Composition  string                  `json:"composition"`
	Base         string                  `json:"base"`
	Conditions   []string                `json:"conditions"`
	Requirements []snapshotDTRequirement `json:"requirements"`
}

// snapshotDTRequirement mirrors one requirement of a device type's
// effective layer, nested requirements included.
type snapshotDTRequirement struct {
	Element     string  `json:"element"`
	Name        string  `json:"name"`
	ID          *uint32 `json:"id"`
	Type        string  `json:"type"`
	Instance    int     `json:"instance"`
	Location    string  `json:"location"`
	Conformance *struct {
		Text string          `json:"text"`
		AST  json.RawMessage `json:"ast"`
	} `json:"conformance"`
	Constraint *struct {
		Text  string          `json:"text"`
		Value json.RawMessage `json:"value"`
		Min   json.RawMessage `json:"min"`
		Max   json.RawMessage `json:"max"`
	} `json:"constraint"`
	Quality  map[string]bool `json:"quality"`
	Referent *struct {
		ID          *uint32 `json:"id"`
		Bit         *uint32 `json:"bit"`
		Name        string  `json:"name"`
		Command     string  `json:"command"`
		Declarer    string  `json:"declarer"`
		Provisional bool    `json:"provisional"`
	} `json:"referent"`
	Requirements []snapshotDTRequirement `json:"requirements"`
}

// snapshot is the top-level structure of parity/schema.json.
type snapshot struct {
	Clusters        []snapshotCluster    `json:"clusters"`
	DeviceTypes     []snapshotDeviceType `json:"deviceTypes"`
	BaseDeviceTypes []snapshotDeviceType `json:"baseDeviceTypes"`
}

func main() {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "chdir %s: %v\n", root, err)
		os.Exit(1)
	}

	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", snapshotPath, err)
		os.Exit(1)
	}

	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", snapshotPath, err)
		os.Exit(1)
	}

	if len(snap.Clusters) == 0 || len(snap.DeviceTypes) == 0 {
		fmt.Fprintf(os.Stderr, "snapshot appears empty: %d clusters, %d deviceTypes\n",
			len(snap.Clusters), len(snap.DeviceTypes))
		os.Exit(1)
	}

	// Sort for deterministic output.
	sort.Slice(snap.Clusters, func(i, j int) bool {
		return snap.Clusters[i].ID < snap.Clusters[j].ID
	})
	sort.Slice(snap.DeviceTypes, func(i, j int) bool {
		return snap.DeviceTypes[i].ID < snap.DeviceTypes[j].ID
	})

	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", schemaDir, err)
		os.Exit(1)
	}

	// Compute the SHA-256 of the raw snapshot bytes so the generated
	// provenance file lets tests detect when someone hand-edits a generated
	// constant without re-running the generator.
	sum := sha256.Sum256(raw)
	snapshotSHA256 := hex.EncodeToString(sum[:])

	if err := writeClustersFile(snap.Clusters); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", clustersFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d clusters)\n", clustersFile, len(snap.Clusters))

	if err := writeDeviceTypesFile(snap.DeviceTypes); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", devicetypesFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d device types)\n", devicetypesFile, len(snap.DeviceTypes))

	if err := writeAccessFile(snap.Clusters); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", accessFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", accessFile)

	if err := writeTimedFile(snap.Clusters); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", timedFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", timedFile)

	if err := writeLargeMessageFile(snap.Clusters); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", largeMsgFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", largeMsgFile)

	if err := writeNamesFile(snap.Clusters); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", namesFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", namesFile)

	if err := writeRequirementsFile(snap); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", requirementFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", requirementFile)

	if err := writeProvenanceFile(snapshotSHA256); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", provenanceFile, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (SHA-256 %s)\n", provenanceFile, snapshotSHA256)
}

const fileHeaderTpl = `// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Code generated by script/generate_matter_schema.go from
// parity/schema.json — DO NOT EDIT.
// Run ` + "`go generate ./schema/...`" + ` to regenerate.

package schema

`

func writeClustersFile(clusters []snapshotCluster) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)

	// ClusterRevisions
	buf.WriteString("// ClusterRevisions maps every cluster ID present in the matter.js HEAD\n")
	buf.WriteString("// schema snapshot to its revision number. Generated from\n")
	buf.WriteString("// parity/schema.json.\n")
	buf.WriteString("var ClusterRevisions = map[uint32]uint16{\n")
	for _, c := range clusters {
		fmt.Fprintf(&buf, "\t0x%04X: %d, // %s\n", c.ID, c.Revision, c.Name)
	}
	buf.WriteString("}\n\n")

	// ClusterNames
	buf.WriteString("// ClusterNames maps every cluster ID to its canonical matter.js name.\n")
	buf.WriteString("var ClusterNames = map[uint32]string{\n")
	for _, c := range clusters {
		fmt.Fprintf(&buf, "\t0x%04X: %q,\n", c.ID, c.Name)
	}
	buf.WriteString("}\n")

	return writeIfChanged(clustersFile, buf.Bytes())
}

func writeDeviceTypesFile(dts []snapshotDeviceType) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)

	// DeviceTypeID
	buf.WriteString("// DeviceTypeID is a Matter device type id (DimmableLight 0x0101): the\n")
	buf.WriteString("// key of the device-type tables below and the type of the generated\n")
	buf.WriteString("// device-type constants (device.DimmableLightID, ...). The\n")
	buf.WriteString("// Descriptor's DeviceTypeList carries it as a 32-bit devtype-id whose\n")
	buf.WriteString("// upper half is zero for every standard device type.\n")
	buf.WriteString("type DeviceTypeID uint16\n\n")

	// DeviceTypeRevisions
	buf.WriteString("// DeviceTypeRevisions maps every device-type ID present in the matter.js\n")
	buf.WriteString("// HEAD schema snapshot to its revision number. Generated from\n")
	buf.WriteString("// parity/schema.json.\n")
	buf.WriteString("var DeviceTypeRevisions = map[DeviceTypeID]uint16{\n")
	for _, dt := range dts {
		fmt.Fprintf(&buf, "\t0x%04X: %d, // %s\n", dt.ID, dt.Revision, dt.Name)
	}
	buf.WriteString("}\n\n")

	// DeviceTypeNames
	buf.WriteString("// DeviceTypeNames maps every device-type ID to its canonical matter.js name.\n")
	buf.WriteString("var DeviceTypeNames = map[DeviceTypeID]string{\n")
	for _, dt := range dts {
		fmt.Fprintf(&buf, "\t0x%04X: %q,\n", dt.ID, dt.Name)
	}
	buf.WriteString("}\n\n")

	// DeviceTypeServerClusters
	buf.WriteString("// DeviceTypeServerClusters maps every device-type ID to the cluster IDs the\n")
	buf.WriteString("// Matter Device Library specifies for it as a SERVER cluster, excluding\n")
	buf.WriteString("// those whose conformance is X (disallowed). A cluster absent from a type's\n")
	buf.WriteString("// set may not be mounted as a server on an endpoint of that type: the\n")
	buf.WriteString("// device would be non-conformant, and ecosystems reject it in ways that\n")
	buf.WriteString("// range from ignoring the extra cluster to mis-categorising the whole\n")
	buf.WriteString("// accessory. Clusters the type specifies only as a CLIENT are deliberately\n")
	buf.WriteString("// absent here — a client requirement means the type CONSUMES that cluster\n")
	buf.WriteString("// from another endpoint, not that it may serve it.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Generated from parity/schema.json.\n")
	buf.WriteString("var DeviceTypeServerClusters = map[DeviceTypeID][]uint32{\n")
	for _, dt := range dts {
		ids := serverClusterIDs(dt)
		if len(ids) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", dt.ID, dt.Name)
		for _, r := range ids {
			fmt.Fprintf(&buf, "\t\t0x%04X, // %s (%s)\n", r.ID, r.Name, conformanceOrNone(r.Conformance))
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n\n")

	// DeviceTypeMandatoryServerClusters
	buf.WriteString("// DeviceTypeMandatoryServerClusters maps every device-type ID to the server\n")
	buf.WriteString("// clusters the Matter Device Library makes unconditionally mandatory for it\n")
	buf.WriteString("// (conformance exactly M) — the set matter.js mounts by default for a device\n")
	buf.WriteString("// type (its `requirements.server.mandatory`). A cluster whose conformance\n")
	buf.WriteString("// carries a condition, a feature or a revision gate is absent: whether it\n")
	buf.WriteString("// applies is the projection's decision, not the schema's.\n")
	buf.WriteString("//\n")
	buf.WriteString("// Generated from parity/schema.json.\n")
	buf.WriteString("var DeviceTypeMandatoryServerClusters = map[DeviceTypeID][]uint32{\n")
	for _, dt := range dts {
		var ids []snapshotRequirement
		for _, r := range serverClusterIDs(dt) {
			if strings.TrimSpace(r.Conformance) == "M" {
				ids = append(ids, r)
			}
		}
		if len(ids) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", dt.ID, dt.Name)
		for _, r := range ids {
			fmt.Fprintf(&buf, "\t\t0x%04X, // %s\n", r.ID, r.Name)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")

	return writeIfChanged(devicetypesFile, buf.Bytes())
}

func writeProvenanceFile(sha256hex string) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)
	buf.WriteString("// SchemaSnapshotSHA256 is the SHA-256 hex digest of\n")
	buf.WriteString("// parity/schema.json at generation time.\n")
	buf.WriteString("// The TestMatterSchemaSnapshotHashMatchesEmbedded test recomputes the\n")
	buf.WriteString("// hash at test time and fails when they diverge — catching hand-edits\n")
	buf.WriteString("// to generated constants that did not go through the generator.\n")
	fmt.Fprintf(&buf, "const SchemaSnapshotSHA256 = %q\n", sha256hex)
	return writeIfChanged(provenanceFile, buf.Bytes())
}

// writeIfChanged gofmts content and writes it to path only when the result
// differs from what is on disk, keeping file mtimes stable on repeated runs.
//
// The formatting pass is part of the generator rather than a separate step
// after it: the emitted maps rely on gofmt's comment alignment, so a raw write
// would leave the tree failing the formatter gate on an otherwise unchanged
// regeneration.
func writeIfChanged(path string, content []byte) error {
	formatted, err := format.Source(content)
	if err != nil {
		return fmt.Errorf("gofmt %s: %w", path, err)
	}
	content = formatted

	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, content) {
		fmt.Printf("  (unchanged: %s)\n", path)
		return nil
	}
	return os.WriteFile(path, content, 0o644)
}

// serverClusterIDs returns dt's server-cluster requirements in ascending ID
// order, dropping those with conformance X — the Device Library's marker for
// "this cluster is disallowed on this device type".
func serverClusterIDs(dt snapshotDeviceType) []snapshotRequirement {
	out := make([]snapshotRequirement, 0, len(dt.Requirements))
	for _, r := range dt.Requirements {
		if r.Element != "serverCluster" || strings.TrimSpace(r.Conformance) == "X" {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// conformanceOrNone renders a requirement's conformance for the generated
// comment; the Descriptor requirement carries none.
func conformanceOrNone(c string) string {
	if strings.TrimSpace(c) == "" {
		return "-"
	}
	return c
}

// writePrivilege is an attribute's write privilege from its effective
// access in the snapshot: 0 for an attribute that is not writable (matter.js
// Access.writable, packages/model/src/aspects/Access.ts:44-46 at db021da0:
// an rw other than "R"), else the level of writePriv — 3 Operate, 4 Manage,
// 5 Administer. A writable attribute whose effective access names no write
// privilege, or one this table does not know, is an error: the generator
// states no privilege the snapshot does not carry.
func writePrivilege(a snapshotAccess) (uint8, error) {
	switch a.RW {
	case "", "R":
		return 0, nil
	case "RW", "R[W]", "W":
	default:
		return 0, fmt.Errorf("effective access rw %q is not one this generator knows", a.RW)
	}
	switch a.WritePriv {
	case "O":
		return 3, nil
	case "M":
		return 4, nil
	case "A":
		return 5, nil
	default:
		return 0, fmt.Errorf("writable (rw %q) with write privilege %q: want O, M or A", a.RW, a.WritePriv)
	}
}

func writeAccessFile(clusters []snapshotCluster) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)
	buf.WriteString("// attributeWritePrivileges maps every writable attribute whose matter.js\n")
	buf.WriteString("// write privilege is above Operate (4 Manage, 5 Administer) to that\n")
	buf.WriteString("// privilege. Generated from each attribute's effective access in\n")
	buf.WriteString("// parity/schema.json.\n")
	buf.WriteString("var attributeWritePrivileges = map[uint32]map[uint32]uint8{\n")
	// The attribute's own effective access, as writeTimedFile reads it: a
	// derived cluster (BridgedDeviceBasicInformation from BasicInformation,
	// the mode clusters from ModeBase) lists the attributes it inherits
	// without an access string, and matter.js's effectiveAccess — which the
	// extractor records per attribute — is where the base's is resolved.
	// Looking the string up by (id, name) in whichever cluster states one
	// first would take another cluster's privilege as soon as two disagree.
	for _, c := range clusters {
		var rows []string
		for _, a := range c.Attributes {
			if a.Effective == nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): the snapshot carries no effective layer", c.Name, c.ID, a.Name, a.ID)
			}
			p, err := writePrivilege(a.Effective.Access)
			if err != nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): %w", c.Name, c.ID, a.Name, a.ID, err)
			}
			if p > 3 {
				rows = append(rows, fmt.Sprintf("\t\t0x%04X: %d, // %s %q\n", a.ID, p, a.Name, a.Effective.Access))
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n\n")
	buf.WriteString("// changesOmittedAttributes lists every attribute whose matter.js quality\n")
	buf.WriteString("// carries \"C\" (changesOmitted): a change to it is never reported to a\n")
	buf.WriteString("// subscriber. Generated from the quality strings in parity/schema.json.\n")
	buf.WriteString("var changesOmittedAttributes = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		var rows []string
		for _, a := range c.Attributes {
			if hasQuality(a.Quality, "C") {
				rows = append(rows, fmt.Sprintf("\t\t0x%04X: {}, // %s %q\n", a.ID, a.Name, a.Quality))
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n\n")
	// fabricScopedWritePaths: matter.js's limits.fabricScoped
	// (packages/protocol/src/action/server/AccessControl.ts:524 at db021da0,
	// fabric "F" or "S") on an attribute that is writable — the attributes
	// AttributeWriteResponse.ts:248-251 refuses without an accessing fabric.
	buf.WriteString("// fabricScopedWritePaths lists every writable attribute whose effective\n")
	buf.WriteString("// access in parity/schema.json is fabric-scoped (\"F\") or\n")
	buf.WriteString("// fabric-sensitive (\"S\"): a write to it needs an accessing fabric.\n")
	buf.WriteString("var fabricScopedWritePaths = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		var rows []string
		for _, a := range c.Attributes {
			if a.Effective == nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): the snapshot carries no effective layer", c.Name, c.ID, a.Name, a.ID)
			}
			acc := a.Effective.Access
			switch acc.Fabric {
			case "":
				continue
			case "F", "S":
			default:
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): effective access fabric %q is not one this generator knows", c.Name, c.ID, a.Name, a.ID, acc.Fabric)
			}
			p, err := writePrivilege(acc)
			if err != nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): %w", c.Name, c.ID, a.Name, a.ID, err)
			}
			if p == 0 {
				continue
			}
			rows = append(rows, fmt.Sprintf("\t\t0x%04X: {}, // %s %q\n", a.ID, a.Name, acc))
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n\n")
	// readOnlyAttributes: the complement of matter.js's limits.writable
	// (packages/protocol/src/action/server/AccessControl.ts:523 at db021da0,
	// `access.writable && !fixed`), over every cluster of the snapshot. The
	// ids of the global range are left to the write gate: the snapshot
	// lists FeatureMap and ClusterRevision where a cluster's element file
	// states them and none of the other four, so a table built from it
	// would cover the range in part.
	buf.WriteString("// readOnlyAttributes lists every attribute below the global range\n")
	buf.WriteString("// (0xFFF8) that a remote party may not write: its effective access in\n")
	buf.WriteString("// parity/schema.json is not writable, or its effective quality is fixed\n")
	buf.WriteString("// (\"F\"). The global attributes are read-only on every cluster and are\n")
	buf.WriteString("// answered by AttributeWritable itself.\n")
	buf.WriteString("var readOnlyAttributes = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		var rows []string
		for _, a := range c.Attributes {
			if a.ID >= globalAttributeFirst {
				continue
			}
			if a.Effective == nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): the snapshot carries no effective layer", c.Name, c.ID, a.Name, a.ID)
			}
			p, err := writePrivilege(a.Effective.Access)
			if err != nil {
				return fmt.Errorf("%s (0x%04X) attribute %s (0x%04X): %w", c.Name, c.ID, a.Name, a.ID, err)
			}
			switch {
			case p == 0:
				rows = append(rows, fmt.Sprintf("\t\t0x%04X: {}, // %s %q\n", a.ID, a.Name, a.Effective.Access))
			case a.Effective.Quality.Fixed:
				rows = append(rows, fmt.Sprintf("\t\t0x%04X: {}, // %s %q, fixed\n", a.ID, a.Name, a.Effective.Access))
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	return writeIfChanged(accessFile, out)
}

// globalAttributeFirst is the first id of the global attribute range
// (GeneratedCommandList 0xFFF8 … ClusterRevision 0xFFFD; matter.js
// packages/model/src/standard/elements/generated-command-list.element.ts:14
// at db021da0).
const globalAttributeFirst = 0xFFF8

// writeTimedFile emits timedInvokePaths: every request command whose
// effective access carries the timed quality ("T", matter.js
// Access.Timed.Required), which matter.js answers NeedsTimedInteraction for
// outside a timed interaction
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295).
func writeTimedFile(clusters []snapshotCluster) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)
	buf.WriteString("// timedInvokePaths lists every request command whose effective access in\n")
	buf.WriteString("// parity/schema.json carries the timed quality (\"T\"): it may only be\n")
	buf.WriteString("// invoked inside a timed interaction.\n")
	buf.WriteString("var timedInvokePaths = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		cmds := slices.Clone(c.Commands)
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].ID < cmds[j].ID })
		var rows []string
		for _, m := range cmds {
			if m.Direction != "request" || m.Effective == nil || !m.Effective.Access.Timed {
				continue
			}
			rows = append(rows, fmt.Sprintf("\t\t0x%02X: {}, // %s\n", m.ID, m.Name))
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n\n")
	// timedWritePaths: matter.js Write.ts:188 makes a write timed when an
	// attribute's effective access carries the timed quality.
	buf.WriteString("// timedWritePaths lists every attribute whose effective access in\n")
	buf.WriteString("// parity/schema.json carries the timed quality (\"T\"): a client writes\n")
	buf.WriteString("// it inside a timed interaction.\n")
	buf.WriteString("var timedWritePaths = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		attrs := slices.Clone(c.Attributes)
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].ID < attrs[j].ID })
		var rows []string
		for _, a := range attrs {
			if a.Effective == nil || !a.Effective.Access.Timed {
				continue
			}
			rows = append(rows, fmt.Sprintf("\t\t0x%04X: {}, // %s\n", a.ID, a.Name))
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	return writeIfChanged(timedFile, out)
}

// writeLargeMessageFile emits largeMessageCommands: every request command
// whose effective quality carries the Large Message quality ("L", matter.js
// Quality.largeMessage, packages/model/src/aspects/Quality.ts:71), which
// matter.js's client reads to send the invoke over TCP
// (packages/protocol/src/action/client/ClientInteraction.ts:111-127
// inferLargeMessage, which probes the request command's schema).
func writeLargeMessageFile(clusters []snapshotCluster) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)
	buf.WriteString("// largeMessageCommands lists every request command whose effective quality\n")
	buf.WriteString("// in parity/schema.json carries the Large Message quality (\"L\"): its\n")
	buf.WriteString("// invoke needs a transport that carries large messages (TCP).\n")
	buf.WriteString("var largeMessageCommands = map[uint32]map[uint32]struct{}{\n")
	for _, c := range clusters {
		cmds := slices.Clone(c.Commands)
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].ID < cmds[j].ID })
		var rows []string
		for _, m := range cmds {
			if m.Direction != "request" || m.Effective == nil || !m.Effective.Quality.LargeMessage {
				continue
			}
			rows = append(rows, fmt.Sprintf("\t\t0x%02X: {}, // %s\n", m.ID, m.Name))
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
		for _, r := range rows {
			buf.WriteString(r)
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	return writeIfChanged(largeMsgFile, out)
}

// hasQuality reports whether a matter.js quality string carries flag as a
// token of its own ("N C" carries "C").
func hasQuality(quality, flag string) bool {
	return slices.Contains(strings.Fields(quality), flag)
}

// writeRequirementsFile emits the device-type layer of the snapshot: every
// device type's requirement tree (deviceTypeDefinitions), the Base device
// type (baseDeviceTypeDefinitions), and the cluster facts device type
// validation reads (clusterFeatures, clusterClassifications,
// unbindableClusters).
func writeRequirementsFile(snap snapshot) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)

	buf.WriteString("// deviceTypeDefinitions holds every device type of the snapshot as\n")
	buf.WriteString("// matter.js's device type validation reads it: its requirement tree in\n")
	buf.WriteString("// declaration order, the conditions it declares, its composition and the\n")
	buf.WriteString("// device type it derives from. Generated from the device types'\n")
	buf.WriteString("// \"effective\" layer in parity/schema.json.\n")
	buf.WriteString("var deviceTypeDefinitions = map[DeviceTypeID]*DeviceTypeDefinition{\n")
	for _, dt := range snap.DeviceTypes {
		if dt.Effective == nil {
			return fmt.Errorf("device type %s has no effective layer; re-extract the snapshot", dt.Name)
		}
		lit, err := deviceTypeLiteral(dt, true)
		if err != nil {
			return fmt.Errorf("device type %s: %w", dt.Name, err)
		}
		fmt.Fprintf(&buf, "\t0x%04X: %s,\n", dt.ID, lit)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// baseDeviceTypeDefinitions holds the device types classified \"base\" (the\n")
	buf.WriteString("// Base device type), whose requirements apply to every endpoint that lists\n")
	buf.WriteString("// a device type. Generated from baseDeviceTypes in parity/schema.json.\n")
	buf.WriteString("var baseDeviceTypeDefinitions = []*DeviceTypeDefinition{\n")
	for _, dt := range snap.BaseDeviceTypes {
		if dt.Effective == nil {
			return fmt.Errorf("base device type %s has no effective layer", dt.Name)
		}
		lit, err := deviceTypeLiteral(dt, false)
		if err != nil {
			return fmt.Errorf("base device type %s: %w", dt.Name, err)
		}
		fmt.Fprintf(&buf, "\t%s,\n", lit)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// clusterFeatures lists every cluster's features in bit order. Generated\n")
	buf.WriteString("// from the clusters' features in parity/schema.json.\n")
	buf.WriteString("var clusterFeatures = map[uint32][]ClusterFeature{\n")
	for _, c := range snap.Clusters {
		var feats []string
		for _, f := range c.Features {
			if f.Bit == nil || *f.Bit < 0 || *f.Bit > 31 {
				continue
			}
			title := ""
			if f.Effective != nil {
				title = f.Effective.Title
			}
			feats = append(feats, fmt.Sprintf("{Name: %q, Bit: %d, Title: %q}", f.Name, *f.Bit, title))
		}
		if len(feats) == 0 {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: {%s}, // %s\n", c.ID, strings.Join(feats, ", "), c.Name)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// clusterClassifications maps every cluster to its effective classification\n")
	buf.WriteString("// (matter.js ClusterModel.effectiveClassification). Generated from\n")
	buf.WriteString("// parity/schema.json.\n")
	buf.WriteString("var clusterClassifications = map[uint32]string{\n")
	for _, c := range snap.Clusters {
		if c.Classification == "" {
			continue
		}
		fmt.Fprintf(&buf, "\t0x%04X: %q, // %s\n", c.ID, c.Classification, c.Name)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// unbindableClusters lists the clusters a Binding entry never directs\n")
	buf.WriteString("// (matter.js ClusterModel.effectiveBindable false). Generated from\n")
	buf.WriteString("// parity/schema.json.\n")
	buf.WriteString("var unbindableClusters = map[uint32]struct{}{\n")
	for _, c := range snap.Clusters {
		if c.Bindable != nil && !*c.Bindable {
			fmt.Fprintf(&buf, "\t0x%04X: {}, // %s\n", c.ID, c.Name)
		}
	}
	buf.WriteString("}\n")

	return writeIfChanged(requirementFile, buf.Bytes())
}

// deviceTypeLiteral renders one device type as a *DeviceTypeDefinition
// literal.
func deviceTypeLiteral(dt snapshotDeviceType, withID bool) (string, error) {
	e := dt.Effective
	var b strings.Builder
	b.WriteString("{")
	if withID {
		fmt.Fprintf(&b, "ID: 0x%04X, ", dt.ID)
	}
	fmt.Fprintf(&b, "Name: %q, Classification: %q, Revision: %d, Composition: %q", dt.Name, dt.Classification, dt.Revision, e.Composition)
	if e.Base != "" {
		fmt.Fprintf(&b, ", Base: %q", e.Base)
	}
	if len(e.Conditions) > 0 {
		quoted := make([]string, 0, len(e.Conditions))
		for _, c := range e.Conditions {
			quoted = append(quoted, fmt.Sprintf("%q", c))
		}
		fmt.Fprintf(&b, ",\n\t\tConditions: []string{%s}", strings.Join(quoted, ", "))
	}
	if len(e.Requirements) > 0 {
		reqs, err := requirementsLiteral(e.Requirements, 2)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, ",\n\t\tRequirements: %s", reqs)
	}
	b.WriteString("}")
	return b.String(), nil
}

// requirementsLiteral renders a requirement list as a
// []DeviceTypeRequirement literal, one requirement per line.
func requirementsLiteral(reqs []snapshotDTRequirement, depth int) (string, error) {
	indent := strings.Repeat("\t", depth)
	var b strings.Builder
	b.WriteString("[]DeviceTypeRequirement{\n")
	for _, r := range reqs {
		lit, err := requirementLiteral(r, depth+1)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s\t%s,\n", indent, lit)
	}
	b.WriteString(indent + "}")
	return b.String(), nil
}

var requirementElementConst = map[string]string{
	"serverCluster": "RequirementServerCluster", "clientCluster": "RequirementClientCluster",
	"feature": "RequirementFeature", "attribute": "RequirementAttribute", "command": "RequirementCommand",
	"event": "RequirementEvent", "commandField": "RequirementCommandField",
	"deviceType": "RequirementDeviceType", "condition": "RequirementCondition",
}

var locationConst = map[string]string{"Root": "LocationRoot", "Self": "LocationSelf", "Descendant": "LocationDescendant"}

// requirementLiteral renders one requirement (and its nested ones).
func requirementLiteral(r snapshotDTRequirement, depth int) (string, error) {
	element, ok := requirementElementConst[r.Element]
	if !ok {
		return "", fmt.Errorf("requirement %s: unknown element %q", r.Name, r.Element)
	}
	parts := []string{"Element: " + element, fmt.Sprintf("Name: %q", r.Name)}
	if r.ID != nil {
		parts = append(parts, fmt.Sprintf("ID: 0x%04X, HasID: true", *r.ID))
	}
	if r.Type != "" {
		parts = append(parts, fmt.Sprintf("Type: %q", r.Type))
	}
	if r.Instance != 0 {
		parts = append(parts, fmt.Sprintf("Instance: %d", r.Instance))
	}
	if r.Location != "" {
		loc, ok := locationConst[r.Location]
		if !ok {
			return "", fmt.Errorf("requirement %s: unknown location %q", r.Name, r.Location)
		}
		parts = append(parts, "Location: "+loc)
	}
	if r.Conformance != nil {
		body, err := schemaConformance(r.Conformance.AST)
		if err != nil {
			return "", fmt.Errorf("requirement %s: %w", r.Name, err)
		}
		parts = append(parts, fmt.Sprintf("Conformance: Conformance{Text: %q, %s}", r.Conformance.Text, body))
	}
	if r.Constraint != nil {
		parts = append(parts, fmt.Sprintf("Constraint: %q", r.Constraint.Text))
		if count := countRange(r.Constraint.Value, r.Constraint.Min, r.Constraint.Max); count != "" {
			parts = append(parts, "Count: "+count)
		}
	}
	if r.Quality["singleton"] {
		parts = append(parts, "Singleton: true")
	}
	if ref := r.Referent; ref != nil {
		rp := []string{"Resolved: true"}
		switch {
		case ref.ID != nil:
			rp = append(rp, fmt.Sprintf("ID: 0x%04X", *ref.ID))
		case ref.Bit != nil:
			rp = append(rp, fmt.Sprintf("ID: %d", *ref.Bit))
		}
		rp = append(rp, fmt.Sprintf("Name: %q", ref.Name))
		if ref.Command != "" {
			rp = append(rp, fmt.Sprintf("Command: %q", ref.Command))
		}
		if ref.Declarer != "" {
			rp = append(rp, fmt.Sprintf("Declarer: %q", ref.Declarer))
		}
		if ref.Provisional {
			rp = append(rp, "Provisional: true")
		}
		parts = append(parts, "Referent: Referent{"+strings.Join(rp, ", ")+"}")
	}
	if len(r.Requirements) > 0 {
		nested, err := requirementsLiteral(r.Requirements, depth)
		if err != nil {
			return "", err
		}
		parts = append(parts, "\n"+strings.Repeat("\t", depth)+"Requirements: "+nested)
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}

// countRange renders matter.js's RequirementModel.componentCountRange for a
// constraint: an exact number is both bounds, a numeric min / max bounds
// one side; anything else (desc, a reference) bounds nothing.
func countRange(value, minRaw, maxRaw json.RawMessage) string {
	num := func(raw json.RawMessage) (int, bool) {
		if len(raw) == 0 {
			return 0, false
		}
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	}
	if v, ok := num(value); ok {
		return fmt.Sprintf("CountRange{Set: true, Min: %d, HasMin: true, Max: %d, HasMax: true}", v, v)
	}
	lo, hasLo := num(minRaw)
	hi, hasHi := num(maxRaw)
	if !hasLo && !hasHi {
		return ""
	}
	parts := []string{"Set: true"}
	if hasLo {
		parts = append(parts, fmt.Sprintf("Min: %d, HasMin: true", lo))
	}
	if hasHi {
		parts = append(parts, fmt.Sprintf("Max: %d, HasMax: true", hi))
	}
	return "CountRange{" + strings.Join(parts, ", ") + "}"
}

// schemaConformance renders the fields of one conformance AST node as a
// schema.Conformance literal body (without the braces). The node types are
// matter.js's Conformance.Ast types, kept as the Op string.
func schemaConformance(raw json.RawMessage) (string, error) {
	var node struct {
		Type  string          `json:"type"`
		Param json.RawMessage `json:"param"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return "", err
	}
	parts := []string{fmt.Sprintf("Op: %q", node.Type)}
	child := func(r json.RawMessage) (string, error) {
		body, err := schemaConformance(r)
		if err != nil {
			return "", err
		}
		return "{" + body + "}", nil
	}
	switch node.Type {
	case "M", "O", "P", "D", "X", "Z", "empty", "desc":
	case "name":
		var name string
		if err := json.Unmarshal(node.Param, &name); err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("Name: %q", name))
	case "value":
		parts = append(parts, fmt.Sprintf("Value: %q", string(node.Param)))
	case "revision":
		var rev int
		if err := json.Unmarshal(node.Param, &rev); err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("Rev: %d", rev))
	case "choice":
		var ch struct {
			Name   string          `json:"name"`
			Num    int             `json:"num"`
			OrMore bool            `json:"orMore"`
			OrLess bool            `json:"orLess"`
			Expr   json.RawMessage `json:"expr"`
		}
		if err := json.Unmarshal(node.Param, &ch); err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("Choice: &ConformanceChoice{Name: %q, Num: %d, OrMore: %t, OrLess: %t}", ch.Name, ch.Num, ch.OrMore, ch.OrLess))
		e, err := child(ch.Expr)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Args: []Conformance{"+e+"}")
	case "otherwise":
		var terms []json.RawMessage
		if err := json.Unmarshal(node.Param, &terms); err != nil {
			return "", err
		}
		args := make([]string, 0, len(terms))
		for _, t := range terms {
			e, err := child(t)
			if err != nil {
				return "", err
			}
			args = append(args, e)
		}
		parts = append(parts, "Args: []Conformance{"+strings.Join(args, ", ")+"}")
	case "optionalIf", "!":
		e, err := child(node.Param)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Args: []Conformance{"+e+"}")
	case "==", "!=", "|", "^", "&", ".", ">", "<", ">=", "<=":
		var bin struct {
			LHS json.RawMessage `json:"lhs"`
			RHS json.RawMessage `json:"rhs"`
		}
		if err := json.Unmarshal(node.Param, &bin); err != nil {
			return "", err
		}
		l, err := child(bin.LHS)
		if err != nil {
			return "", err
		}
		r, err := child(bin.RHS)
		if err != nil {
			return "", err
		}
		parts = append(parts, "Args: []Conformance{"+l+", "+r+"}")
	default:
		return "", fmt.Errorf("unknown conformance node %q", node.Type)
	}
	return strings.Join(parts, ", "), nil
}

// commandDirection is a command's effective direction, its own when the
// effective layer states none.
func commandDirection(m snapshotCommand) string {
	if m.Effective != nil && m.Effective.Direction != "" {
		return m.Effective.Direction
	}
	return m.Direction
}

// writeNamesFile writes the attribute, command and event names of every
// cluster of the snapshot (schema.AttributeName, CommandName,
// ResponseCommandName, EventName), whether or not cluster/spec holds a
// generated definition of the cluster.
func writeNamesFile(clusters []snapshotCluster) error {
	var buf bytes.Buffer
	buf.WriteString(fileHeaderTpl)
	type idName struct {
		id   uint32
		name string
	}
	table := func(doc, name string, elems func(c snapshotCluster) []idName) {
		buf.WriteString(doc)
		fmt.Fprintf(&buf, "var %s = map[uint32]map[uint32]string{\n", name)
		for _, c := range clusters {
			list := elems(c)
			if len(list) == 0 {
				continue
			}
			sort.Slice(list, func(i, j int) bool { return list[i].id < list[j].id })
			fmt.Fprintf(&buf, "\t0x%04X: { // %s\n", c.ID, c.Name)
			for _, e := range list {
				fmt.Fprintf(&buf, "\t\t0x%04X: %q,\n", e.id, e.name)
			}
			buf.WriteString("\t},\n")
		}
		buf.WriteString("}\n\n")
	}
	table("// attributeNames maps every cluster's attribute ids to their matter.js\n// names. Generated from parity/schema.json.\n", "attributeNames",
		func(c snapshotCluster) []idName {
			out := make([]idName, 0, len(c.Attributes))
			for _, a := range c.Attributes {
				out = append(out, idName{a.ID, a.Name})
			}
			return out
		})
	commands := func(direction string) func(c snapshotCluster) []idName {
		return func(c snapshotCluster) []idName {
			var out []idName
			for _, m := range c.Commands {
				if commandDirection(m) == direction {
					out = append(out, idName{m.ID, m.Name})
				}
			}
			return out
		}
	}
	table("// commandNames maps every cluster's request command ids to their\n// matter.js names. Generated from parity/schema.json.\n", "commandNames", commands("request"))
	table("// responseCommandNames maps every cluster's response command ids to\n// their matter.js names. Generated from parity/schema.json.\n", "responseCommandNames", commands("response"))
	table("// eventNames maps every cluster's event ids to their matter.js names.\n// Generated from parity/schema.json.\n", "eventNames",
		func(c snapshotCluster) []idName {
			out := make([]idName, 0, len(c.Events))
			for _, e := range c.Events {
				out = append(out, idName{e.ID, e.Name})
			}
			return out
		})
	return writeIfChanged(namesFile, buf.Bytes())
}
