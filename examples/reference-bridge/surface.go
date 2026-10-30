// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// surfacePolicies are the daemon's surface policies, the node's and every
// device's (node.Config.Surface, device.Options.Surface): the
// "Ahead of matter.js" register rows --extensions names, and
// BD-chip-Thermostat-Schedules with --thermostat-schedules; the level
// --spec-level names; the controller profiles --profile names. Every ID
// must be one the module declares (device.AheadRows, node.AheadRows), the
// level one of schema.Levels and every profile one of surface.Profiles; a
// typo stops the daemon with the list of the known ones.
func surfacePolicies(f flags) (surface.Policies, error) {
	known := slices.Concat(device.AheadRows(), node.AheadRows())
	slices.Sort(known)
	known = slices.Compact(known)
	var ids []string
	for id := range strings.SplitSeq(f.extensions, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if !slices.Contains(known, id) {
			return surface.Policies{}, fmt.Errorf("--extensions: %q is no extension this module declares; known: %s", id, strings.Join(known, ", "))
		}
		ids = append(ids, id)
	}
	if f.thermostatSchedules {
		ids = append(ids, schedulesRow)
	}
	var p surface.Policies
	if len(ids) > 0 {
		p.Extensions = surface.NewExtensions(ids...)
	}
	// --spec-level: an operator's setting (concept v11 §C.5); "" is the
	// pin's level. An unknown level stops the daemon with the list.
	p.Level = surface.Level(strings.TrimSpace(f.specLevel))
	if err := (surface.Policies{Level: p.Level}).Validate(); err != nil {
		return surface.Policies{}, fmt.Errorf("--spec-level: %w", err)
	}
	// --profile: an operator's setting (concept v11 §C.6). An unknown
	// profile, or two whose levels differ without --spec-level, stops the
	// daemon.
	for name := range strings.SplitSeq(f.profiles, ",") {
		if name = strings.TrimSpace(name); name != "" {
			p.Profiles = append(p.Profiles, name)
		}
	}
	if err := p.Validate(); err != nil {
		return surface.Policies{}, fmt.Errorf("--profile: %w", err)
	}
	return p, nil
}

// profileList names the selectable controller profiles (surface.Profiles)
// for the flag's help.
func profileList() string {
	names := make([]string, 0, len(surface.Profiles()))
	for _, p := range surface.Profiles() {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// levelList names the selectable levels (schema.Levels) for the flag's
// help.
func levelList() string {
	names := make([]string, 0, len(schema.Levels()))
	for _, l := range schema.Levels() {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}

// withheldOr returns the device the specification level withheld with its
// error, so the node records it (node.Aggregator.Add), and nil with any
// other error.
func withheldOr[T any](d *T, err error) (*T, error) {
	if errors.Is(err, surface.ErrWithheld) && d != nil {
		return d, err
	}
	return nil, err
}

// addAll adds eps to the node's Aggregator and returns those it added. A
// device the specification level withheld is logged once, with its type
// and the level, and skipped: the daemon runs on (concept v11 §C.3).
func addAll(ctx context.Context, agg *node.Aggregator, eps []node.Endpoint, logger *slog.Logger) ([]node.Endpoint, error) {
	added := make([]node.Endpoint, 0, len(eps))
	for _, ep := range eps {
		err := agg.Add(ctx, ep)
		var w *surface.WithheldError
		switch {
		case errors.As(err, &w):
			name, _ := schema.DeviceTypeName(schema.DeviceTypeID(ep.Spec().DeviceType))
			logger.Warn("surface.withheld", slog.String("key", w.Endpoint), slog.String("device_type", name),
				slog.String("spec_level", string(w.Report.Level)))
		case err != nil:
			return nil, fmt.Errorf("aggregator: %w", err)
		default:
			added = append(added, ep)
		}
	}
	return added, nil
}

// surfaceDB is the in-memory SQLite store a --dump-surface composition
// runs on: shared by the pool's connections, gone when the last closes.
const surfaceDB = "file:reference-bridge-surface?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// dumpSurface composes the node exactly as run() does, on an in-memory
// store, a scratch OTA directory and without mDNS, starts it on a
// loopback port the system assigns so the topology is assembled, writes
// its surface report (node.Node.Surface, assembled) to w and stops the
// node. No record is published and --db is not opened.
func dumpSurface(ctx context.Context, f flags, logger *slog.Logger, passcode uint32, discriminator uint16, tcpConfig node.TCPConfig, w io.Writer) error {
	dir, err := os.MkdirTemp("", "reference-bridge-surface-")
	if err != nil {
		return fmt.Errorf("--dump-surface: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	db, err := sql.Open("sqlite", surfaceDB)
	if err != nil {
		return fmt.Errorf("--dump-surface: %w", err)
	}
	defer func() { _ = db.Close() }()
	n, _, err := compose(ctx, f, logger, passcode, discriminator, tcpConfig, &dumpEnv{db: db, otaDir: dir})
	if err != nil {
		return err
	}
	defer func() { _ = n.Stop(context.WithoutCancel(ctx)) }()
	if err := n.Start(ctx); err != nil {
		return fmt.Errorf("--dump-surface: %w", err)
	}
	compact, err := json.Marshal(n.Surface())
	if err != nil {
		return fmt.Errorf("--dump-surface: %w", err)
	}
	out, err := formatSurface(compact)
	if err != nil {
		return fmt.Errorf("--dump-surface: %w", err)
	}
	_, err = w.Write(out)
	return err
}

// formatSurface lays the canonical JSON out for a review diff: an object
// or array whose members hold no object or array is written on one line,
// compact; every other one member per line, indented by two spaces. The
// key order is the canonical one (sorted) and json.Compact of the result
// is the canonical encoding again.
func formatSurface(compact []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := formatValue(&buf, json.RawMessage(compact), ""); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func formatValue(buf *bytes.Buffer, raw json.RawMessage, indent string) error {
	raw = bytes.TrimSpace(raw)
	if flat(raw) {
		return json.Compact(buf, raw)
	}
	inner := indent + "  "
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		buf.WriteString("[\n")
		for i, it := range items {
			buf.WriteString(inner)
			if err := formatValue(buf, it, inner); err != nil {
				return err
			}
			if i < len(items)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "]")
		return nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return err
	}
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	buf.WriteString("{\n")
	for i, k := range keys {
		name, err := json.Marshal(k)
		if err != nil {
			return err
		}
		buf.WriteString(inner)
		buf.Write(name)
		buf.WriteString(": ")
		if err := formatValue(buf, members[k], inner); err != nil {
			return err
		}
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(indent + "}")
	return nil
}

// flat reports whether raw is a scalar, an array of scalars, or an object
// whose members are scalars and arrays of scalars: one line.
func flat(raw json.RawMessage) bool {
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return true
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return true
		}
		for _, it := range items {
			if t := bytes.TrimSpace(it); len(t) > 0 && (t[0] == '{' || t[0] == '[') {
				return false
			}
		}
		return true
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return true
	}
	for _, v := range members {
		t := bytes.TrimSpace(v)
		if len(t) > 0 && t[0] == '{' {
			return false
		}
		if len(t) > 0 && t[0] == '[' && !flat(t) {
			return false
		}
	}
	return true
}
