// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
)

// The settings keys the node keeps its state under. They are the
// reference daemon's (examples/reference-bridge/persist.go:72-78,
// wiring.go:944-976), so a database the daemon wrote keeps its labels,
// configuration versions, scene tables and counters under the facade.
const (
	rootNodeLabelKey              = "basic_information.node_label"
	rootLocationKey               = "basic_information.location"
	rootConfigVersionKey          = "basic_information.configuration_version"
	endpointLabelKeyRoot          = "bridged_node_label."
	endpointConfigVersionKey      = "bridged_configuration_version."
	scenesKeyRoot                 = "scenes."
	rebootCountSetting            = "gendiag.reboot_count"
	operationalTimeSetting        = "gendiag.operational_ms"
	legacyOperationalHoursSetting = "gendiag.operational_hours"
)

// persisted keeps the controller-written state of the node's endpoints in
// the store's settings table: a bridged endpoint's NodeLabel and
// ConfigurationVersion under its stable key, its scene table, and the
// root's NodeLabel, Location and ConfigurationVersion — the daemon's
// persistedLabels (persist.go:62-188). Only an [endpoint.StringKey]
// stable key is persisted, as there.
type persisted struct {
	st     *store.Store
	logger *slog.Logger
}

func (p persisted) loadUint32(ctx context.Context, key string) uint32 {
	v, ok, err := p.st.GetSetting(ctx, key)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

// configVersion returns the ConfigurationVersion persisted for a bridged
// endpoint, 0 for none.
func (p persisted) configVersion(ctx context.Context, key endpoint.SourceKey) uint32 {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return 0
	}
	return p.loadUint32(ctx, endpointConfigVersionKey+string(k))
}

// storeConfigVersions persists what bridge.IncreaseConfigurationVersion
// raised: a version never decreases, so it outlives a restart.
func (p persisted) storeConfigVersions(topo *endpoint.Topology, v bridge.ConfigurationVersions) {
	ctx := context.Background()
	set := func(k string, n uint32) {
		if err := p.st.SetSetting(ctx, k, strconv.FormatUint(uint64(n), 10)); err != nil {
			p.logger.Warn("configuration_version.persist", slog.String("err", err.Error()))
		}
	}
	if v.Node != 0 {
		set(rootConfigVersionKey, v.Node)
	}
	for id, n := range v.Bridged {
		if ep := topo.FindByID(id); ep != nil {
			if k, ok := ep.SourceKey.(endpoint.StringKey); ok {
				set(endpointConfigVersionKey+string(k), n)
			}
		}
	}
}

// storeLabel implements endpoint.Config.OnNodeLabelWritten.
func (p persisted) storeLabel(key endpoint.SourceKey, label string) {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return
	}
	if err := p.st.SetSetting(context.Background(), endpointLabelKeyRoot+string(k), label); err != nil {
		p.logger.Warn("label.persist", slog.String("err", err.Error()))
	}
}

// label returns the label persisted for a bridged endpoint, "" for none.
func (p persisted) label(ctx context.Context, key endpoint.SourceKey) string {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return ""
	}
	label, found, err := p.st.GetSetting(ctx, endpointLabelKeyRoot+string(k))
	if err != nil || !found {
		return ""
	}
	return label
}

// restoreRoot applies the persisted root NodeLabel, Location and
// ConfigurationVersion and wires their persistence.
func (p persisted) restoreRoot(ctx context.Context, b *mattercore.BasicInformation) {
	if v, ok, err := p.st.GetSetting(ctx, rootNodeLabelKey); err == nil && ok {
		_ = b.SetNodeLabel(v)
	}
	if v, ok, err := p.st.GetSetting(ctx, rootLocationKey); err == nil && ok {
		_ = b.SetLocation(v)
	}
	b.RestoreConfigurationVersion(p.loadUint32(ctx, rootConfigVersionKey))
	b.SetOnPersistentWrite(func(nodeLabel, location string) {
		for k, v := range map[string]string{rootNodeLabelKey: nodeLabel, rootLocationKey: location} {
			if err := p.st.SetSetting(context.WithoutCancel(ctx), k, v); err != nil {
				p.logger.Warn("label.persist", slog.String("err", err.Error()))
			}
		}
	})
}

// LoadScenes implements endpoint.ScenesStore.
func (p persisted) LoadScenes(key endpoint.SourceKey) []byte {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return nil
	}
	v, found, err := p.st.GetSetting(context.Background(), scenesKeyRoot+string(k))
	if err != nil || !found {
		return nil
	}
	return []byte(v)
}

// SaveScenes implements endpoint.ScenesStore.
func (p persisted) SaveScenes(key endpoint.SourceKey, table []byte) {
	k, ok := key.(endpoint.StringKey)
	if !ok {
		return
	}
	if err := p.st.SetSetting(context.Background(), scenesKeyRoot+string(k), string(table)); err != nil {
		p.logger.Warn("scenes.persist", slog.String("err", err.Error()))
	}
}

// countBoot returns this boot's RebootCount and persists it: 0 on the
// first boot of a database, one more on every boot after — matter.js
// GeneralDiagnosticsServer.ts initialize(), as the daemon's countBoot
// (wiring.go:951-971).
func countBoot(ctx context.Context, st *store.Store) (uint16, error) {
	raw, ok, err := st.GetSetting(ctx, rebootCountSetting)
	if err != nil {
		return 0, err
	}
	var count uint16
	if ok {
		prev, perr := strconv.ParseUint(raw, 10, 16)
		if perr != nil {
			return 0, fmt.Errorf("%s %q: %w", rebootCountSetting, raw, perr)
		}
		count = uint16(min(prev+1, math.MaxUint16))
	}
	if err := st.SetSetting(ctx, rebootCountSetting, strconv.FormatUint(uint64(count), 10)); err != nil {
		return 0, err
	}
	return count, nil
}

// loadOperationalTime is the operational time earlier boots accumulated,
// in milliseconds, or in whole hours under the legacy key
// (wiring.go:984-1010).
func loadOperationalTime(ctx context.Context, st *store.Store) (time.Duration, error) {
	raw, ok, err := st.GetSetting(ctx, operationalTimeSetting)
	if err != nil {
		return 0, err
	}
	if ok {
		ms, err := strconv.ParseUint(raw, 10, 63)
		if err != nil || ms > uint64(math.MaxInt64/int64(time.Millisecond)) {
			return 0, fmt.Errorf("%s %q: not a millisecond count", operationalTimeSetting, raw)
		}
		return time.Duration(ms) * time.Millisecond, nil //nolint:gosec // bounded above
	}
	raw, ok, err = st.GetSetting(ctx, legacyOperationalHoursSetting)
	if err != nil || !ok {
		return 0, err
	}
	h, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", legacyOperationalHoursSetting, raw, err)
	}
	return time.Duration(min(h, uint64(math.MaxInt64/int64(time.Hour)))) * time.Hour, nil //nolint:gosec // bounded above
}

// storeOperationalTime persists the operational time in milliseconds.
func storeOperationalTime(ctx context.Context, st *store.Store, g *mattercore.GeneralDiagnostics) error {
	ms := g.TotalOperationalTime().Milliseconds()
	return st.SetSetting(ctx, operationalTimeSetting, strconv.FormatInt(ms, 10))
}

// keepOperationalTime persists the operational time every interval until
// ctx ends (wiring.go:1019-1036); Stop stores it once more.
func keepOperationalTime(ctx context.Context, st *store.Store, g *mattercore.GeneralDiagnostics, interval time.Duration, logger *slog.Logger) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := storeOperationalTime(ctx, st, g); err != nil {
				logger.Warn("gendiag.operational_hours.persist", slog.String("err", err.Error()))
			}
		}
	}
}
