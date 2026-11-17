// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/bootid"
	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/store"
)

// The settings keys the node keeps its state under. They are the
// reference daemon's (examples/reference-bridge/persist.go:72-78 at b4ea1f96^,
// wiring.go:944-976 at b4ea1f96^), so a database the daemon wrote keeps its labels,
// configuration versions, scene tables and counters under the facade.
const (
	rootNodeLabelKey              = "basic_information.node_label"
	rootLocationKey               = "basic_information.location"
	rootConfigVersionKey          = "basic_information.configuration_version"
	endpointLabelKeyRoot          = "bridged_node_label."
	endpointConfigVersionKey      = "bridged_configuration_version."
	scenesKeyRoot                 = "scenes."
	bindingKeyRoot                = "binding."
	rebootCountSetting            = "gendiag.reboot_count"
	operationalTimeSetting        = "gendiag.operational_ms"
	legacyOperationalHoursSetting = "gendiag.operational_hours"
)

// keyedSettingRoots are the settings roots the node keeps a bridged
// endpoint's state under by its stable key, one key per endpoint: the root
// and the key's String(). [Aggregator.Forget] erases each of them for a
// forgotten key (nonVolatile.forget); the nonvolatile attribute values,
// one key per attribute, are erased beside them (nonVolatileCandidates).
// A root added here is erased with the rest; one kept elsewhere fails
// TestKeyedSettingRootsAreForgotten.
var keyedSettingRoots = []string{
	endpointLabelKeyRoot,
	endpointConfigVersionKey,
	scenesKeyRoot,
	bindingKeyRoot,
	bridgedUserLabelKeyRoot,
	endpointSurfaceKey,
}

// keyedSettingKeys are the keys of every keyed setting root at
// endpointKeys.
func keyedSettingKeys(endpointKeys []string) []string {
	keys := make([]string, 0, len(keyedSettingRoots)*len(endpointKeys))
	for _, root := range keyedSettingRoots {
		for _, key := range endpointKeys {
			keys = append(keys, root+key)
		}
	}
	return keys
}

// persisted keeps the controller-written state of the node's endpoints in
// the store's settings table: a bridged endpoint's NodeLabel and
// ConfigurationVersion under its stable key, its scene table, and the
// root's NodeLabel, Location and ConfigurationVersion — the daemon's
// persistedLabels (persist.go:64-188 at b4ea1f96^). A stable key is persisted under
// its String(), the identity the assembler keys endpoint numbers by —
// an [endpoint.StringKey] under the string itself, so a database the
// daemon wrote reads back; a host's own key type under what it prints.
type persisted struct {
	st     store.Settings
	logger *slog.Logger
	// salt is the UniqueID rotation salt uniqueID consults; nil is
	// bootid.Salt (a test pins its own).
	salt func() [16]byte
}

// EndpointState is the endpoint-level stack state the facade keeps for its
// own assembler, exported for a host that keeps it itself or assembles its
// endpoints itself ([Config].Snapshotter, issue #117): a bridged endpoint's
// scene table (under "scenes.<key>") and the NodeLabel a controller wrote
// (under "bridged_node_label.<key>"), each keyed by the endpoint's stable
// key's String(). It is the assembler's [endpoint.Persistence] — a change a
// controller causes is stored with the change, and a refused store answers
// FAILURE (the store-error rule of store/doc.go) — and the label's load: a
// host restores a label by setting its endpoint.Spec's NodeLabel from
// LoadNodeLabel before assembling, as the facade's [Aggregator] does.
type EndpointState interface {
	endpoint.Persistence
	// LoadNodeLabel returns the label stored for key, "" for none.
	LoadNodeLabel(ctx context.Context, key device.SourceKey) (string, error)
}

// NewEndpointState returns the facade's [EndpointState] over st's settings
// table — the keys the node uses, so a host may build it before [New] (for
// the assembler its [Config].Snapshotter runs) over a store the node later opens. st
// must hold the store schema (store.Apply); a nil logger is
// slog.Default(). A refused save is logged and returned.
func NewEndpointState(st store.Settings, logger *slog.Logger) EndpointState {
	if logger == nil {
		logger = slog.Default()
	}
	return persisted{st: st, logger: logger}
}

// EndpointPersistence is the endpoint-level stack state the facade keeps
// for its own assembler: a bridged endpoint's scene table (under
// "scenes.<key>") and the per-endpoint NodeLabel a commissioner wrote
// (under "bridged_node_label.<key>"), each keyed by the endpoint's stable
// key's String(). A host restores a label by setting its endpoint.Spec's
// NodeLabel from [EndpointPersistence.NodeLabel] before assembling.
//
// Deprecated: use [EndpointState], whose saves and loads report the
// store's error. Removable in the next minor release.
type EndpointPersistence interface {
	endpoint.ScenesStore //nolint:staticcheck // the deprecated port keeps its deprecated shape
	// OnNodeLabelWritten persists a commissioner-written NodeLabel of the
	// bridged endpoint under key.
	OnNodeLabelWritten(key device.SourceKey, label string)
	// NodeLabel is the label persisted for key, "" for none.
	NodeLabel(ctx context.Context, key device.SourceKey) string
}

// NewEndpointPersistence returns the facade's [EndpointPersistence] over
// st's settings table — the keys the node uses. st must hold the store
// schema (store.Apply); a nil logger is slog.Default().
//
// Deprecated: use [NewEndpointState]. Removable in the next minor release.
func NewEndpointPersistence(st store.Settings, logger *slog.Logger) EndpointPersistence {
	return legacyEndpoints{NewEndpointState(st, logger)}
}

// legacyEndpoints is an [EndpointState] seen through the deprecated
// [EndpointPersistence]: its hooks cannot report an error, so a refused
// save is the state's to log, and a refused load reads as none.
type legacyEndpoints struct{ s EndpointState }

func (l legacyEndpoints) OnNodeLabelWritten(key device.SourceKey, label string) {
	_ = l.s.SaveNodeLabel(context.Background(), key, label)
}

func (l legacyEndpoints) NodeLabel(ctx context.Context, key device.SourceKey) string {
	label, _ := l.s.LoadNodeLabel(ctx, key)
	return label
}

func (l legacyEndpoints) LoadScenes(key device.SourceKey) []byte {
	table, _ := l.s.LoadScenes(context.Background(), key)
	return table
}

func (l legacyEndpoints) SaveScenes(key device.SourceKey, table []byte) {
	_ = l.s.SaveScenes(context.Background(), key, table)
}

// endpointState is the endpoint-level persistence the node's assembler
// and Aggregator use: EndpointState, else the deprecated
// EndpointPersistence adapted, else own.
func (c *Config) endpointState(own persisted) EndpointState {
	switch {
	case c.EndpointState != nil:
		return c.EndpointState
	case c.EndpointPersistence != nil:
		return hostEndpoints{c.EndpointPersistence}
	}
	return own
}

// hostEndpoints is a host's deprecated [EndpointPersistence] seen as an
// [EndpointState]: its hooks cannot refuse, so a save never fails.
type hostEndpoints struct{ p EndpointPersistence }

func (h hostEndpoints) SaveNodeLabel(_ context.Context, key device.SourceKey, label string) error {
	h.p.OnNodeLabelWritten(key, label)
	return nil
}

func (h hostEndpoints) LoadNodeLabel(ctx context.Context, key device.SourceKey) (string, error) {
	return h.p.NodeLabel(ctx, key), nil
}

func (h hostEndpoints) LoadScenes(_ context.Context, key device.SourceKey) ([]byte, error) {
	return h.p.LoadScenes(key), nil
}

func (h hostEndpoints) SaveScenes(_ context.Context, key device.SourceKey, table []byte) error {
	h.p.SaveScenes(key, table)
	return nil
}

// keyString is the settings-key suffix of a stable key, false for nil.
func keyString(key device.SourceKey) (string, bool) {
	if key == nil {
		return "", false
	}
	return key.String(), true
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
func (p persisted) configVersion(ctx context.Context, key device.SourceKey) uint32 {
	k, ok := keyString(key)
	if !ok {
		return 0
	}
	return p.loadUint32(ctx, endpointConfigVersionKey+k)
}

// configVersionValues are the settings values of what
// bridge.IncreaseConfigurationVersion raised, by key.
func (p persisted) configVersionValues(topo *endpoint.Topology, v bridge.ConfigurationVersions) map[string]string {
	values := map[string]string{}
	if v.Node != 0 {
		values[rootConfigVersionKey] = strconv.FormatUint(uint64(v.Node), 10)
	}
	for id, n := range v.Bridged {
		if ep := topo.FindByID(id); ep != nil {
			if k, ok := keyString(ep.SourceKey); ok {
				values[endpointConfigVersionKey+k] = strconv.FormatUint(uint64(n), 10)
			}
		}
	}
	return values
}

// storeConfigVersions persists what bridge.IncreaseConfigurationVersion
// raised: a version never decreases, so it outlives a restart. Every
// version is written in one write, all or none; a refused write is
// returned.
func (p persisted) storeConfigVersions(ctx context.Context, topo *endpoint.Topology, v bridge.ConfigurationVersions) error {
	if err := p.st.SetSettings(ctx, p.configVersionValues(topo, v)); err != nil {
		return fmt.Errorf("node: persist configuration versions: %w", err)
	}
	return nil
}

// SaveNodeLabel implements [EndpointState]: the label under
// "bridged_node_label.<key>"; a refused write is logged and returned, so a
// controller's write answers FAILURE.
func (p persisted) SaveNodeLabel(ctx context.Context, key device.SourceKey, label string) error {
	k, ok := keyString(key)
	if !ok {
		return nil
	}
	if err := p.st.SetSetting(context.WithoutCancel(ctx), endpointLabelKeyRoot+k, label); err != nil {
		p.logger.Warn("label.persist", slog.Any("err", err))
		return fmt.Errorf("node: persist the NodeLabel of %s: %w", k, err)
	}
	return nil
}

// LoadNodeLabel implements [EndpointState]: the label persisted for a
// bridged endpoint, "" for none.
func (p persisted) LoadNodeLabel(ctx context.Context, key device.SourceKey) (string, error) {
	k, ok := keyString(key)
	if !ok {
		return "", nil
	}
	label, _, err := p.st.GetSetting(ctx, endpointLabelKeyRoot+k)
	if err != nil {
		return "", fmt.Errorf("node: load the NodeLabel of %s: %w", k, err)
	}
	return label, nil
}

// restoreRoot applies the persisted root NodeLabel, Location and
// ConfigurationVersion and wires their persistence.
func (p persisted) restoreRoot(ctx context.Context, b *mattercore.BasicInformation) {
	if v, ok, err := p.st.GetSetting(ctx, rootNodeLabelKey); err == nil && ok {
		_ = b.RestoreNodeLabel(v)
	}
	if v, ok, err := p.st.GetSetting(ctx, rootLocationKey); err == nil && ok {
		_ = b.RestoreLocation(v)
	}
	b.RestoreConfigurationVersion(p.loadUint32(ctx, rootConfigVersionKey))
	// A store failure fails the write (mattercore.BasicInformation
	// SetOnPersistentWrite): the host's SetNodeLabel returns it, a
	// controller's write answers FAILURE. The write uses New's context
	// without its cancellation, so a caller's cancelled context does not
	// abort a store write already begun. Both keys go in one write
	// (store.Settings.SetSettings): a refused write leaves neither changed,
	// so a label that answered FAILURE is not served after a restart.
	b.SetOnPersistentWrite(func(nodeLabel, location string) error {
		err := p.st.SetSettings(context.WithoutCancel(ctx), map[string]string{rootNodeLabelKey: nodeLabel, rootLocationKey: location})
		if err != nil {
			p.logger.Warn("label.persist", slog.Any("err", err))
		}
		return err
	})
}

// LoadScenes implements [EndpointState] (endpoint.Persistence): the table
// under "scenes.<key>", nil for none; a refused read is logged and
// returned, and the endpoint's table stays unloaded until a load succeeds.
func (p persisted) LoadScenes(ctx context.Context, key device.SourceKey) ([]byte, error) {
	k, ok := keyString(key)
	if !ok {
		return nil, nil
	}
	v, found, err := p.st.GetSetting(ctx, scenesKeyRoot+k)
	if err != nil {
		p.logger.Warn("scenes.load", slog.Any("err", err))
		return nil, fmt.Errorf("node: load the scene table of %s: %w", k, err)
	}
	if !found {
		return nil, nil
	}
	return []byte(v), nil
}

// SaveScenes implements [EndpointState] (endpoint.Persistence): the table
// under "scenes.<key>"; a refused write is logged and returned, so a scene
// command answers FAILURE.
func (p persisted) SaveScenes(ctx context.Context, key device.SourceKey, table []byte) error {
	k, ok := keyString(key)
	if !ok {
		return nil
	}
	if err := p.st.SetSetting(context.WithoutCancel(ctx), scenesKeyRoot+k, string(table)); err != nil {
		p.logger.Warn("scenes.persist", slog.Any("err", err))
		return fmt.Errorf("node: persist the scene table of %s: %w", k, err)
	}
	return nil
}

// uniqueID returns the BasicInformation UniqueID the store pinned on an
// earlier boot, or derives one, pins it and returns it — matter.js
// BasicInformationServer initialize (BasicInformationServer.ts:89-91):
// uniqueId is created once when the state holds none and persisted
// (quality N), so the attribute keeps its F quality across a config edit
// to the label the derivation reads. The key is the daemon's
// (store.SettingUniqueID). A boot with bootid rotation enabled derives a
// per-boot value and neither reads nor writes the store, so the value a
// boot without rotation pinned before is served again once rotation is
// off.
func (p persisted) uniqueID(ctx context.Context, derive func() string) (string, error) {
	salt := p.salt
	if salt == nil {
		salt = bootid.Salt
	}
	if salt() != [16]byte{} {
		return derive(), nil
	}
	stored, found, err := p.st.GetSetting(ctx, store.SettingUniqueID)
	if err != nil {
		return "", err
	}
	if found && stored != "" {
		return stored, nil
	}
	id := derive()
	if err := p.st.SetSetting(ctx, store.SettingUniqueID, id); err != nil {
		return "", err
	}
	return id, nil
}

// countBoot returns this boot's RebootCount and persists it: 0 on the
// first boot of a database, one more on every boot after — matter.js
// GeneralDiagnosticsServer.ts initialize(), as the daemon's countBoot
// (wiring.go:951-971 at b4ea1f96^).
func countBoot(ctx context.Context, st store.Settings) (uint16, error) {
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
// (wiring.go:984-1010 at b4ea1f96^).
func loadOperationalTime(ctx context.Context, st store.Settings) (time.Duration, error) {
	raw, ok, err := st.GetSetting(ctx, operationalTimeSetting)
	if err != nil {
		return 0, err
	}
	if ok {
		ms, err := strconv.ParseUint(raw, 10, 63)
		if err != nil || ms > uint64(math.MaxInt64/int64(time.Millisecond)) {
			return 0, fmt.Errorf("node: %s %q: not a millisecond count", operationalTimeSetting, raw)
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
func storeOperationalTime(ctx context.Context, st store.Settings, g *mattercore.GeneralDiagnostics) error {
	ms := g.TotalOperationalTime().Milliseconds()
	return st.SetSetting(ctx, operationalTimeSetting, strconv.FormatInt(ms, 10))
}

// keepOperationalTime persists the operational time every interval until
// ctx ends (wiring.go:1019-1036 at b4ea1f96^); Stop stores it once more.
func keepOperationalTime(ctx context.Context, clk clock.Clock, st store.Settings, g *mattercore.GeneralDiagnostics, interval time.Duration, logger *slog.Logger) {
	tick := clock.Or(clk).NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
			if err := storeOperationalTime(ctx, st, g); err != nil {
				logger.Warn("gendiag.operational_hours.persist", slog.Any("err", err))
			}
		}
	}
}

// bindingKey is the settings key an endpoint's Binding list is kept
// under: "binding." and the endpoint's stable key, as its NodeLabel and
// scene table are (keyString); an endpoint without one (the root, the
// Aggregator, a host topology's unkeyed endpoint) under "@" and its
// number, which is fixed for those.
func bindingKey(ep *endpoint.Endpoint) string {
	if k, ok := keyString(ep.SourceKey); ok {
		return bindingKeyRoot + k
	}
	return bindingKeyRoot + "@" + strconv.FormatUint(uint64(ep.ID), 10)
}

// storedBinding is one Binding entry as the store keeps it; an absent
// field is its zero (core.TargetStruct's sentinels).
type storedBinding struct {
	Node        uint64 `json:"node,omitempty"`
	Group       uint16 `json:"group,omitempty"`
	Endpoint    uint16 `json:"endpoint,omitempty"`
	Cluster     uint32 `json:"cluster,omitempty"`
	FabricIndex uint8  `json:"fabricIndex"`
	// EndpointPresent / ClusterPresent keep a target naming endpoint 0
	// or cluster 0 (core.TargetStruct's presence flags); absent in a list
	// stored before they existed, which then reads as before.
	EndpointPresent bool `json:"endpointPresent,omitempty"`
	ClusterPresent  bool `json:"clusterPresent,omitempty"`
}

// bindingConfig keeps the Binding list of the endpoint under key in the
// settings table — the Binding attribute is nonvolatile (quality N,
// matter.js packages/model/src/standard/elements/binding.element.ts:
// 21-25), persisted as UserLabel's list is (buildLabels).
func (p persisted) bindingConfig(ctx context.Context, key string) mattercore.BindingConfig {
	// The node's fabric list, the source AccessControl checks a write's
	// fabric against: the node's store.Port lists its fabrics. A settings
	// store without the list leaves Fabrics nil, the persisted Binding
	// refuses every request write (fail closed), and the log says why
	// (review II, R2-P6-store-1010, decision 158).
	fabrics, ok := p.st.(mattercore.FabricSource)
	if !ok {
		p.logger.Warn("binding.no_fabric_source", slog.String("key", key),
			slog.String("reason", "the store lists no fabrics; request writes to this Binding are refused"))
	}
	return mattercore.BindingConfig{
		Fabrics: fabrics,
		Load: func() ([]mattercore.TargetStruct, error) {
			v, ok, err := p.st.GetSetting(ctx, key)
			if err != nil || !ok {
				return nil, err
			}
			var stored []storedBinding
			if err := json.Unmarshal([]byte(v), &stored); err != nil {
				// A list that does not decode restores nothing; the next
				// change overwrites it.
				p.logger.Warn("binding.restore", slog.String("key", key), slog.Any("err", err))
				return nil, nil
			}
			out := make([]mattercore.TargetStruct, 0, len(stored))
			for _, e := range stored {
				out = append(out, mattercore.TargetStruct{Node: e.Node, Group: e.Group, Endpoint: e.Endpoint, Cluster: e.Cluster, FabricIndex: e.FabricIndex, EndpointPresent: e.EndpointPresent, ClusterPresent: e.ClusterPresent})
			}
			return out, nil
		},
		Save: func(list []mattercore.TargetStruct) error {
			stored := make([]storedBinding, 0, len(list))
			for _, e := range list {
				stored = append(stored, storedBinding{Node: e.Node, Group: e.Group, Endpoint: e.Endpoint, Cluster: e.Cluster, FabricIndex: e.FabricIndex, EndpointPresent: e.HasEndpoint() && e.Endpoint == 0, ClusterPresent: e.HasCluster() && e.Cluster == 0})
			}
			b, err := json.Marshal(stored)
			if err == nil {
				err = p.st.SetSetting(context.WithoutCancel(ctx), key, string(b))
			}
			if err != nil {
				p.logger.Warn("binding.persist", slog.String("key", key), slog.Any("err", err))
			}
			return err
		},
	}
}
