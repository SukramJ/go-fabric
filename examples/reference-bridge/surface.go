// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/SukramJ/go-fabric/node"
)

// surfaceDB is the in-memory SQLite store a --dump-surface composition
// runs on: shared by the pool's connections, gone when the last closes.
const surfaceDB = "file:reference-bridge-surface?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// dumpSurface composes the node exactly as run() does, on an in-memory
// store, a scratch OTA directory and without mDNS, writes its surface
// report (node.Node.Surface) to w and stops the node. Nothing is started:
// no socket is bound, no record published, --db is not opened.
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
