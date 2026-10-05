// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	// The pure-Go SQLite driver. Neither store package imports a driver —
	// both take an already-open *sql.DB — so picking one, and picking the
	// DSN, is this host's job.
	_ "modernc.org/sqlite"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/store"
)

// connectionPragmas are the modernc.org/sqlite `_pragma` query parameters
// applied to every connection the pool opens, not just the first:
// connection-scoped pragmas must ride on the DSN, because setting one with
// a statement primes a single pooled connection and leaves the rest on
// their defaults.
//
// foreign_keys carries the module's cascade-delete on fabric removal, which
// is a schema-level FOREIGN KEY and silently no-ops with the pragma off.
// busy_timeout is what makes a contended endpoint-id allocation wait rather
// than fail — see sqlitestore.Store.UpsertEndpointAssigning.
const connectionPragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// openDB opens (creating on first run) this host's SQLite file and applies
// the two schemas the bridge needs.
//
// Neither DDL is written here. Each package embeds the text its own queries
// are written against and hands it over — store.Apply for the matter_*
// operational tables, sqlitestore.Apply for endpoint identity — so a schema
// change arrives with the dependency bump instead of drifting against a
// copy kept in this file. A host with a migration tool of its own feeds
// store.Schema() and sqlitestore.Schema() through that instead; both
// scripts are idempotent, so applying them on every boot is the intended
// use rather than a first-run special case.
func openDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?"+connectionPragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := store.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply store schema: %w", err)
	}
	if err := sqlitestore.Apply(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply endpoint schema: %w", err)
	}
	return db, nil
}

// persistedLabels keeps controller-written NodeLabels in the store's
// settings table: the root's under rootNodeLabelKey, each bridged
// endpoint's under its stable key.
type persistedLabels struct {
	st     *store.Store
	logger *slog.Logger
}

const (
	rootNodeLabelKey     = "basic_information.node_label"
	rootLocationKey      = "basic_information.location"
	endpointLabelKeyRoot = "bridged_node_label."
)

// store implements endpoint.Config.OnNodeLabelWritten.
func (p persistedLabels) store(key endpoint.SourceKey, label string) {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return
	}
	if err := p.st.SetSetting(context.Background(), endpointLabelKeyRoot+string(k), label); err != nil {
		p.logger.Warn("label.persist", slog.String("err", err.Error()))
	}
}

// load returns the label persisted for a bridged endpoint, "" for none.
func (p persistedLabels) load(ctx context.Context, key endpoint.StringKey) string {
	label, ok, err := p.st.GetSetting(ctx, endpointLabelKeyRoot+string(key))
	if err != nil || !ok {
		return ""
	}
	return label
}

// restoreRoot applies the persisted root NodeLabel / Location and wires
// their persistence — BasicInformation's writable attributes, which
// matter.js keeps in node storage.
func (p persistedLabels) restoreRoot(ctx context.Context, b *mattercore.BasicInformation) {
	if v, ok, err := p.st.GetSetting(ctx, rootNodeLabelKey); err == nil && ok {
		_ = b.SetNodeLabel(v)
	}
	if v, ok, err := p.st.GetSetting(ctx, rootLocationKey); err == nil && ok {
		_ = b.SetLocation(v)
	}
	b.SetOnPersistentWrite(func(nodeLabel, location string) {
		for k, v := range map[string]string{rootNodeLabelKey: nodeLabel, rootLocationKey: location} {
			if err := p.st.SetSetting(context.Background(), k, v); err != nil {
				p.logger.Warn("label.persist", slog.String("err", err.Error()))
			}
		}
	})
}
