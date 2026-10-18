// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// durationFlag is a flag.Value of a duration: "500ms", "2s", or a bare
// number of seconds.
type durationFlag struct {
	d   time.Duration
	set bool
}

func (f *durationFlag) String() string {
	if f == nil || !f.set {
		return ""
	}
	return f.d.String()
}

func (f *durationFlag) Set(s string) error {
	d, err := parseDuration(s, time.Second)
	if err != nil {
		return err
	}
	f.d, f.set = d, true
	return nil
}

// or is the duration, or def when the flag was not given.
func (f *durationFlag) or(def time.Duration) time.Duration {
	if f.set {
		return f.d
	}
	return def
}

// parseDuration reads a Go duration, or a bare number in unit.
func parseDuration(s string, unit time.Duration) (time.Duration, error) {
	if n, err := strconv.ParseUint(s, 10, 32); err == nil {
		return time.Duration(n) * unit, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("duration %q", s)
	}
	return d, nil
}

// parseUint reads a decimal or 0x-hex number of at most bits bits.
func parseUint(s string, bits int) (uint64, error) {
	return strconv.ParseUint(s, 0, bits)
}

// parseU16 and parseU32 are parseUint at a width.
func parseU16(s string) (uint16, error) {
	v, err := parseUint(s, 16)
	return uint16(v), err //nolint:gosec // G115: parsed at 16 bits
}

func parseU32(s string) (uint32, error) {
	v, err := parseUint(s, 32)
	return uint32(v), err //nolint:gosec // G115: parsed at 32 bits
}

// lookupCluster finds a cluster by its matter.js name (case ignored) or
// its id, decimal or 0x hex. def is nil when no generated definition is
// linked.
func lookupCluster(s string) (id uint32, def *spec.Cluster, err error) {
	if n, perr := parseU32(s); perr == nil {
		return n, spec.Lookup(n), nil
	}
	for cid, name := range schema.ClusterNames {
		if strings.EqualFold(name, s) || strings.EqualFold(strings.ReplaceAll(name, " ", ""), s) {
			return cid, spec.Lookup(cid), nil
		}
	}
	return 0, nil, fmt.Errorf("unknown cluster %q", s)
}

// clusterName is the name of a cluster id for output.
func clusterName(id uint32) string {
	if n, ok := schema.ClusterNames[id]; ok {
		return n
	}
	return fmt.Sprintf("0x%04X", id)
}

// path is a parsed "<ep>/<Cluster>/<element>"; a "*" leaves a part a
// wildcard.
type path struct {
	endpoint   uint16
	hasEP      bool
	cluster    uint32
	hasCluster bool
	def        *spec.Cluster
	element    string // the third part as given; "" or "*" is a wildcard
}

func parsePath(s string) (path, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return path{}, fmt.Errorf("path %q: want <endpoint>/<Cluster>/<element>", s)
	}
	var p path
	if parts[0] != "*" {
		ep, err := parseU16(parts[0])
		if err != nil {
			return path{}, fmt.Errorf("path %q: endpoint %q", s, parts[0])
		}
		p.endpoint, p.hasEP = ep, true
	}
	if parts[1] != "*" {
		id, def, err := lookupCluster(parts[1])
		if err != nil {
			return path{}, fmt.Errorf("path %q: %w", s, err)
		}
		p.cluster, p.hasCluster, p.def = id, true, def
	}
	if parts[2] != "*" {
		p.element = parts[2]
	}
	return p, nil
}

// attributePath resolves p's element as an attribute.
func (p path) attributePath() (im.ConcreteAttributePath, *spec.Attribute, error) {
	ap := im.ConcreteAttributePath{Endpoint: p.endpoint, HasEndpoint: p.hasEP, Cluster: p.cluster, HasCluster: p.hasCluster}
	if p.element == "" {
		return ap, nil, nil
	}
	if n, err := parseU32(p.element); err == nil {
		ap.Attribute, ap.HasAttribute = n, true
		if p.def != nil {
			return ap, p.def.Attribute(n), nil
		}
		return ap, nil, nil
	}
	if p.def == nil {
		return ap, nil, fmt.Errorf("attribute %q: no definition of the cluster; give its id", p.element)
	}
	a := p.def.AttributeByName(p.element)
	if a == nil {
		return ap, nil, fmt.Errorf("%s has no attribute %q", p.def.Name, p.element)
	}
	ap.Attribute, ap.HasAttribute = a.ID, true
	return ap, a, nil
}

// commandPath resolves p's element as a request command; endpoint and
// cluster must be concrete unless group is set (a group command has no
// endpoint).
func (p path) commandPath(group bool) (im.ConcreteCommandPath, *spec.Command, error) {
	if !p.hasCluster || p.element == "" || (!group && !p.hasEP) {
		return im.ConcreteCommandPath{}, nil, errors.New("a command path is concrete: <endpoint>/<Cluster>/<Command>")
	}
	cp := im.ConcreteCommandPath{Endpoint: p.endpoint, HasEndpoint: p.hasEP, Cluster: p.cluster, HasCluster: true, HasCommand: true}
	if n, err := parseU32(p.element); err == nil {
		cp.Command = n
		if p.def != nil {
			return cp, p.def.Command(n, spec.Request), nil
		}
		return cp, nil, nil
	}
	if p.def == nil {
		return cp, nil, fmt.Errorf("command %q: no definition of the cluster; give its id", p.element)
	}
	for i := range p.def.Commands {
		if c := &p.def.Commands[i]; c.Direction == spec.Request && strings.EqualFold(c.Name, p.element) {
			cp.Command = c.ID
			return cp, c, nil
		}
	}
	return cp, nil, fmt.Errorf("%s has no command %q", p.def.Name, p.element)
}

// eventPath resolves p's element as an event.
func (p path) eventPath() (im.ConcreteEventPath, error) {
	ep := im.ConcreteEventPath{Endpoint: p.endpoint, HasEndpoint: p.hasEP, Cluster: p.cluster, HasCluster: p.hasCluster}
	if p.element == "" {
		return ep, nil
	}
	if n, err := parseU32(p.element); err == nil {
		ep.Event, ep.HasEvent = n, true
		return ep, nil
	}
	if p.def == nil {
		return ep, fmt.Errorf("event %q: no definition of the cluster; give its id", p.element)
	}
	for i := range p.def.Events {
		if strings.EqualFold(p.def.Events[i].Name, p.element) {
			ep.Event, ep.HasEvent = p.def.Events[i].ID, true
			return ep, nil
		}
	}
	return ep, fmt.Errorf("%s has no event %q", p.def.Name, p.element)
}

// encodeArg encodes a JSON argument as field's value; a nil field is
// chip-tool's untyped form.
func encodeArg(field *spec.Field, raw string) (spec.Node, error) {
	return spec.EncodeJSON(field, json.RawMessage(raw))
}

// attributeLine is one attribute of a report as a JSON line.
type attributeLine struct {
	Endpoint  uint16          `json:"endpoint"`
	Cluster   string          `json:"cluster"`
	Attribute string          `json:"attribute"`
	Version   *uint32         `json:"dataVersion,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Status    string          `json:"status,omitempty"`
}

// eventLine is one event of a report as a JSON line.
type eventLine struct {
	Endpoint uint16          `json:"endpoint"`
	Cluster  string          `json:"cluster"`
	Event    string          `json:"event"`
	Number   uint64          `json:"number"`
	Priority uint8           `json:"priority"`
	Value    json.RawMessage `json:"value,omitempty"`
	Status   string          `json:"status,omitempty"`
}

func attributeName(def *spec.Cluster, id uint32) (string, *spec.Attribute) {
	if def != nil {
		if a := def.Attribute(id); a != nil {
			return a.Name, a
		}
	}
	return fmt.Sprintf("0x%04X", id), nil
}

func eventName(def *spec.Cluster, id uint32) (string, *spec.Event) {
	if def != nil {
		if ev := def.Event(id); ev != nil {
			return ev.Name, ev
		}
	}
	return fmt.Sprintf("0x%02X", id), nil
}

// valueJSON renders a value of an attribute; an undecodable value falls
// back to the untyped form.
func valueJSON(field *spec.Field, n spec.Node) json.RawMessage {
	if field != nil {
		if v, err := spec.DecodeJSON(field, n); err == nil {
			return v
		}
	}
	v, err := spec.DecodeJSON(nil, n)
	if err != nil {
		return json.RawMessage(strconv.Quote(err.Error()))
	}
	return v
}

// reportLines renders a report as JSON lines.
func reportLines(r *interaction.Report) []any {
	out := make([]any, 0, len(r.Attributes)+len(r.Statuses)+len(r.Events)+len(r.EventStatuses))
	for i := range r.Attributes {
		a := &r.Attributes[i]
		def := spec.Lookup(a.Path.Cluster)
		name, attr := attributeName(def, a.Path.Attribute)
		var field *spec.Field
		if attr != nil {
			field = spec.AttributeField(attr)
		}
		v := a.DataVersion
		out = append(out, attributeLine{Endpoint: a.Path.Endpoint, Cluster: clusterName(a.Path.Cluster), Attribute: name, Version: &v, Value: valueJSON(field, a.Value)})
	}
	for _, s := range r.Statuses {
		name, _ := attributeName(spec.Lookup(s.Path.Cluster), s.Path.Attribute)
		out = append(out, attributeLine{Endpoint: s.Path.Endpoint, Cluster: clusterName(s.Path.Cluster), Attribute: name, Status: s.Status.String()})
	}
	for i := range r.Events {
		ev := &r.Events[i]
		name, def := eventName(spec.Lookup(ev.Path.Cluster), ev.Path.Event)
		line := eventLine{Endpoint: ev.Path.Endpoint, Cluster: clusterName(ev.Path.Cluster), Event: name, Number: ev.Number, Priority: uint8(ev.Priority)}
		if ev.HasValue {
			var field *spec.Field
			if def != nil {
				field = spec.EventField(def)
			}
			line.Value = valueJSON(field, ev.Value)
		}
		out = append(out, line)
	}
	for _, s := range r.EventStatuses {
		name, _ := eventName(spec.Lookup(s.Path.Cluster), s.Path.Event)
		out = append(out, eventLine{Endpoint: s.Path.Endpoint, Cluster: clusterName(s.Path.Cluster), Event: name, Status: s.Status.String()})
	}
	return out
}

// printJSONLines writes each value as one JSON line.
func printJSONLines(e *env, lines []any) error {
	for _, l := range lines {
		if err := e.writeJSON(l); err != nil {
			return err
		}
	}
	return nil
}
