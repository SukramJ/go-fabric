// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The snapshot side: parity/schema.json, read through its resolved layer
// ("effective", the clusters' "datatypes", "globalDatatypes"), which is
// matter.js's operational model — derived clusters (ModeBase, AlarmBase,
// ConcentrationMeasurement, …) already carry what they inherit. Device-type
// requirements carry only their conformance text, which is parsed with
// matter.js's grammar (ParseConformance).

type snapFile struct {
	Matter struct {
		Revision     string `json:"revision"`
		SourceCommit string `json:"sourceCommit"`
	} `json:"matter"`
	Clusters        []snapCluster    `json:"clusters"`
	DeviceTypes     []snapDeviceType `json:"deviceTypes"`
	GlobalDatatypes []*snapValue     `json:"globalDatatypes"`
	BaseDeviceTypes []snapDeviceType `json:"baseDeviceTypes"`
}

type snapCluster struct {
	ID             uint32 `json:"id"`
	Name           string `json:"name"`
	Revision       int    `json:"revision"`
	Base           string `json:"base"`
	Classification string `json:"classification"`
	Features       []struct {
		Name      string `json:"name"`
		Bit       *int   `json:"bit"`
		Effective struct {
			Conformance *snapConf `json:"conformance"`
		} `json:"effective"`
	} `json:"features"`
	Attributes []snapMember `json:"attributes"`
	Commands   []snapMember `json:"commands"`
	Events     []snapMember `json:"events"`
	Datatypes  []*snapValue `json:"datatypes"`
}

type snapMember struct {
	ID        uint32     `json:"id"`
	Name      string     `json:"name"`
	Direction string     `json:"direction"`
	Effective *snapValue `json:"effective"`
}

type snapConf struct {
	Text string          `json:"text"`
	AST  json.RawMessage `json:"ast"`
}

type snapValue struct {
	ID          *uint32         `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Metatype    string          `json:"metatype"`
	Primitive   string          `json:"primitive"`
	Entry       *snapValue      `json:"entry"`
	Fields      []*snapValue    `json:"fields"`
	Conformance *snapConf       `json:"conformance"`
	Access      *Access         `json:"access"`
	Quality     map[string]bool `json:"quality"`
	Constraint  *struct {
		Text string `json:"text"`
	} `json:"constraint"`
	Default   json.RawMessage `json:"default"`
	Direction string          `json:"direction"`
	Response  string          `json:"response"`
	Priority  string          `json:"priority"`
}

type snapDeviceType struct {
	ID             uint32 `json:"id"`
	Name           string `json:"name"`
	Classification string `json:"classification"`
	Revision       int    `json:"revision"`
	Requirements   []struct {
		ID          uint32 `json:"id"`
		Name        string `json:"name"`
		Element     string `json:"element"`
		Conformance string `json:"conformance"`
	} `json:"requirements"`
	// Effective is the device-type layer: the requirement tree matter.js's
	// device type validation reads, and the declared conditions.
	Effective *struct {
		Conditions   []string             `json:"conditions"`
		Requirements []*snapDTRequirement `json:"requirements"`
	} `json:"effective"`
}

// snapDTRequirement is one requirement of a device type's effective layer.
type snapDTRequirement struct {
	Element     string    `json:"element"`
	Name        string    `json:"name"`
	ID          *uint32   `json:"id"`
	Conformance *snapConf `json:"conformance"`
	Constraint  *struct {
		Text string `json:"text"`
	} `json:"constraint"`
	Quality  map[string]bool `json:"quality"`
	Referent *struct {
		ID       *uint32 `json:"id"`
		Name     string  `json:"name"`
		Declarer string  `json:"declarer"`
	} `json:"referent"`
	Requirements []*snapDTRequirement `json:"requirements"`
}

// SnapshotInfo is the snapshot's provenance.
type SnapshotInfo struct {
	Revision     string
	SourceCommit string
}

// LoadSnapshot reduces parity/schema.json to the compared model.
func LoadSnapshot(data []byte) (*Model, SnapshotInfo, error) {
	var f snapFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, SnapshotInfo{}, fmt.Errorf("snapshot: %w", err)
	}
	info := SnapshotInfo{Revision: f.Matter.Revision, SourceCommit: f.Matter.SourceCommit}
	m := &Model{}
	for i := range f.Clusters {
		c, err := snapshotCluster(&f.Clusters[i])
		if err != nil {
			return nil, info, err
		}
		m.Clusters = append(m.Clusters, c)
	}
	// The Base device type has no id; it sorts first, as CHIP's side does.
	for i := range f.BaseDeviceTypes {
		dt, err := snapshotBaseDeviceType(&f.BaseDeviceTypes[i])
		if err != nil {
			return nil, info, err
		}
		m.DeviceTypes = append(m.DeviceTypes, dt)
	}
	for i := range f.DeviceTypes {
		dt, err := snapshotDeviceType(&f.DeviceTypes[i])
		if err != nil {
			return nil, info, err
		}
		m.DeviceTypes = append(m.DeviceTypes, dt)
	}
	for _, d := range f.GlobalDatatypes {
		e, err := snapshotDatatype(d)
		if err != nil {
			return nil, info, fmt.Errorf("global %s: %w", d.Name, err)
		}
		m.Globals = append(m.Globals, e)
	}
	return m, info, nil
}

func snapshotCluster(sc *snapCluster) (*Cluster, error) {
	c := &Cluster{ID: u32(sc.ID), Name: sc.Name, Revision: sc.Revision, Base: sc.Base, Classification: sc.Classification}
	wrap := func(err error) error { return fmt.Errorf("cluster %s: %w", sc.Name, err) }
	for _, f := range sc.Features {
		e := &Element{Name: f.Name}
		if f.Bit != nil {
			e.Constraint = strconv.Itoa(*f.Bit)
		}
		conf, err := snapshotConf(f.Effective.Conformance)
		if err != nil {
			return nil, wrap(err)
		}
		e.Conformance = conf
		c.Features = append(c.Features, e)
	}
	for _, a := range sc.Attributes {
		e, err := snapshotMember(a)
		if err != nil {
			return nil, wrap(err)
		}
		c.Attributes = append(c.Attributes, e)
	}
	for _, cmd := range sc.Commands {
		e, err := snapshotMember(cmd)
		if err != nil {
			return nil, wrap(err)
		}
		if e.Direction == "" {
			e.Direction = cmd.Direction
		}
		if e.Direction == "" {
			// matter.js CommandModel: a command without a direction is a
			// response by its name.
			e.Direction = "request"
			if strings.HasSuffix(cmd.Name, "Response") {
				e.Direction = "response"
			}
		}
		c.Commands = append(c.Commands, e)
	}
	for _, ev := range sc.Events {
		e, err := snapshotMember(ev)
		if err != nil {
			return nil, wrap(err)
		}
		c.Events = append(c.Events, e)
	}
	for _, d := range sc.Datatypes {
		e, err := snapshotDatatype(d)
		if err != nil {
			return nil, wrap(err)
		}
		c.Datatypes = append(c.Datatypes, e)
	}
	return c, nil
}

func snapshotMember(m snapMember) (*Element, error) {
	if m.Effective == nil {
		return nil, fmt.Errorf("%s has no effective description", m.Name)
	}
	e, err := snapshotValue(m.Effective)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.Name, err)
	}
	e.ID, e.Name = u32(m.ID), m.Name
	return e, nil
}

func snapshotDatatype(d *snapValue) (*Element, error) {
	e, err := snapshotValue(d)
	if err != nil {
		return nil, err
	}
	switch d.Metatype {
	case "enum", "bitmap":
		e.Kind = d.Metatype
	case "object":
		e.Kind = "struct"
	}
	return e, nil
}

func snapshotValue(v *snapValue) (*Element, error) {
	e := &Element{
		ID:        v.ID,
		Name:      v.Name,
		Type:      v.Type,
		Metatype:  v.Metatype,
		Primitive: v.Primitive,
		Direction: v.Direction,
		Response:  v.Response,
		Priority:  v.Priority,
		Access:    v.Access,
	}
	var err error
	if e.Conformance, err = snapshotConf(v.Conformance); err != nil {
		return nil, err
	}
	if v.Constraint != nil {
		e.Constraint = v.Constraint.Text
	}
	if v.Quality != nil {
		e.Quality = []string{}
		for k, set := range v.Quality {
			if set {
				e.Quality = append(e.Quality, k)
			}
		}
		slices.Sort(e.Quality)
	}
	if len(v.Default) > 0 {
		if e.Default, err = snapshotDefault(v.Default); err != nil {
			return nil, err
		}
	}
	if v.Entry != nil {
		entry, err := snapshotValue(v.Entry)
		if err != nil {
			return nil, err
		}
		e.Entry = entry
		e.EntryType = entry.Type
	}
	for _, f := range v.Fields {
		field, err := snapshotValue(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		e.Fields = append(e.Fields, field)
	}
	return e, nil
}

func snapshotConf(c *snapConf) (*Conf, error) {
	if c == nil || len(c.AST) == 0 {
		return nil, nil
	}
	return confFromAST(c.AST)
}

// snapshotDefault reads a default as the extractor emits it: a JSON
// literal, a list, or one of matter.js's FieldValue objects
// ({"type":"reference"|"celsius"|"percent"|"bytes", …}).
func snapshotDefault(raw json.RawMessage) (*Value, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("default: %w", err)
	}
	switch x := v.(type) {
	case nil:
		return &Value{Kind: "null", Text: "null"}, nil
	case bool:
		return &Value{Kind: "bool", Text: strconv.FormatBool(x)}, nil
	case float64:
		return &Value{Kind: "number", Text: strconv.FormatFloat(x, 'f', -1, 64)}, nil
	case string:
		// A bigint survives the extract as a decimal string.
		if n, ok := parseNumber(x); ok {
			return &Value{Kind: "number", Text: n}, nil
		}
		return &Value{Kind: "string", Text: x}, nil
	case []any:
		return &Value{Kind: "list", Text: string(raw)}, nil
	case map[string]any:
		typ, _ := x["type"].(string)
		switch typ {
		case "reference":
			name, _ := x["name"].(string)
			return &Value{Kind: "reference", Text: name}, nil
		case "celsius", "percent":
			n, ok := x["value"].(float64)
			if !ok {
				return nil, fmt.Errorf("default: %s without a value", typ)
			}
			return &Value{Kind: typ, Text: strconv.FormatFloat(n, 'f', -1, 64)}, nil
		case "bytes":
			s, _ := x["value"].(string)
			return &Value{Kind: "bytes", Text: s}, nil
		}
		return &Value{Kind: "object", Text: string(raw)}, nil
	}
	return nil, fmt.Errorf("default: unsupported value %s", raw)
}

func snapshotDeviceType(sd *snapDeviceType) (*DeviceType, error) {
	dt := &DeviceType{ID: u32(sd.ID), Name: sd.Name, Revision: sd.Revision, Classification: sd.Classification}
	for _, r := range sd.Requirements {
		var side string
		switch r.Element {
		case "serverCluster":
			side = "server"
		case "clientCluster":
			side = "client"
		case "deviceType":
			side = "deviceType"
		default:
			return nil, fmt.Errorf("device type %s: requirement %s of unknown element %q", sd.Name, r.Name, r.Element)
		}
		conf, err := ParseConformance(r.Conformance)
		if err != nil {
			return nil, fmt.Errorf("device type %s: requirement %s: %w", sd.Name, r.Name, err)
		}
		req := &Requirement{ID: r.ID, Name: r.Name, Side: side, Conformance: conf}
		if eff := effectiveClusterRequirement(sd, r.Element, r.ID); eff != nil {
			if err := withElements(req, eff); err != nil {
				return nil, fmt.Errorf("device type %s: requirement %s: %w", sd.Name, r.Name, err)
			}
		}
		dt.Requirements = append(dt.Requirements, req)
	}
	if err := withConditions(dt, sd); err != nil {
		return nil, err
	}
	return dt, nil
}

// snapshotBaseDeviceType reads the Base device type, which has no id and so
// no raw requirement list: its requirements come from the effective layer.
func snapshotBaseDeviceType(sd *snapDeviceType) (*DeviceType, error) {
	dt := &DeviceType{Name: sd.Name, Revision: sd.Revision, Classification: sd.Classification}
	if sd.Effective == nil {
		return nil, fmt.Errorf("base device type %s has no effective layer", sd.Name)
	}
	for _, r := range sd.Effective.Requirements {
		var side string
		switch r.Element {
		case "serverCluster":
			side = "server"
		case "clientCluster":
			side = "client"
		default:
			continue
		}
		if r.ID == nil {
			return nil, fmt.Errorf("base device type %s: cluster requirement %s has no id", sd.Name, r.Name)
		}
		conf, err := snapshotConf(r.Conformance)
		if err != nil {
			return nil, fmt.Errorf("base device type %s: requirement %s: %w", sd.Name, r.Name, err)
		}
		req := &Requirement{ID: *r.ID, Name: r.Name, Side: side, Conformance: conf}
		if err := withElements(req, r); err != nil {
			return nil, fmt.Errorf("base device type %s: requirement %s: %w", sd.Name, r.Name, err)
		}
		dt.Requirements = append(dt.Requirements, req)
	}
	if err := withConditions(dt, sd); err != nil {
		return nil, err
	}
	return dt, nil
}

// effectiveClusterRequirement finds the effective-layer counterpart of a raw
// cluster requirement: the device type's own requirement of the cluster on
// that side. A requirement the raw layer inherits from a base device type
// without restating it has none — matter.js's validation reads only a
// device type's own requirements (DeviceTypeModel.requirements).
func effectiveClusterRequirement(sd *snapDeviceType, element string, id uint32) *snapDTRequirement {
	if sd.Effective == nil {
		return nil
	}
	for _, r := range sd.Effective.Requirements {
		if r.Element == element && r.ID != nil && *r.ID == id {
			return r
		}
	}
	return nil
}

// withElements carries a cluster requirement's quality and its feature,
// attribute, command and event requirements over from the effective layer.
func withElements(req *Requirement, eff *snapDTRequirement) error {
	if eff.Quality != nil {
		req.Quality = []string{}
		for flag, set := range eff.Quality {
			if set {
				req.Quality = append(req.Quality, flag)
			}
		}
		slices.Sort(req.Quality)
	}
	for _, n := range eff.Requirements {
		switch n.Element {
		case "feature", "attribute", "command", "event":
		default:
			continue
		}
		conf, err := snapshotConf(n.Conformance)
		if err != nil {
			return fmt.Errorf("%s requirement %s: %w", n.Element, n.Name, err)
		}
		er := &ElementRequirement{Element: n.Element, Name: n.Name, Conformance: conf}
		if n.Constraint != nil {
			er.Constraint = n.Constraint.Text
		}
		if n.Element != "feature" && n.Referent != nil {
			er.ID = n.Referent.ID
		}
		req.Elements = append(req.Elements, er)
	}
	return nil
}

// withConditions carries the declared conditions and the condition
// requirements over from the effective layer.
func withConditions(dt *DeviceType, sd *snapDeviceType) error {
	if sd.Effective == nil {
		return nil
	}
	dt.Conditions = sd.Effective.Conditions
	for _, r := range sd.Effective.Requirements {
		if r.Element != "condition" {
			continue
		}
		conf, err := snapshotConf(r.Conformance)
		if err != nil {
			return fmt.Errorf("device type %s: condition requirement %s: %w", sd.Name, r.Name, err)
		}
		cr := &ConditionRequirement{Name: r.Name, Conformance: conf}
		if r.Referent != nil {
			cr.DeviceType, cr.Name = r.Referent.Declarer, r.Referent.Name
		}
		if r.Constraint != nil {
			cr.Constraint = r.Constraint.Text
		}
		dt.ConditionRequirements = append(dt.ConditionRequirements, cr)
	}
	return nil
}
