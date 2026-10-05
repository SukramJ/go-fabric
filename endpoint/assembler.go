// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/SukramJ/go-fabric/groups"
)

// Config tunes the assembler. The zero value is *not* valid — at
// minimum, a non-zero VendorID + ProductID + non-empty NodeLabel
// must be supplied (the root BasicInformation cluster mandates them).
type Config struct {
	// VendorID is the bridge's IANA-assigned vendor identifier.
	VendorID uint16
	// ProductID is the bridge's vendor-assigned product identifier.
	ProductID uint16
	// NodeLabel is the user-visible bridge label.
	NodeLabel string
	// Groups is the node's group state. When set, every bridged endpoint
	// whose device type mandates the Groups cluster (on-off-light,
	// on-off-plug-in-unit, … — schema.DeviceTypeRequiresServerCluster)
	// serves a real Groups server over it, and a Groups server a source
	// supplies itself is replaced by one: group membership is stack state,
	// as in matter.js (GroupsServer keeps it in the root's
	// GroupKeyManagementServer). nil keeps whatever the source supplies.
	Groups *groups.Manager
	// OnNodeLabelWritten fires after a controller wrote a bridged
	// endpoint's BridgedDeviceBasicInformation NodeLabel, with the
	// endpoint's stable key and the new label, so the host can persist it
	// and hand it back as [Spec.NodeLabel] after a restart — matter.js
	// keeps a written nodeLabel in the endpoint's persisted state. nil
	// keeps the label for the life of the process only.
	OnNodeLabelWritten func(key SourceKey, label string)
	// Scenes persists the scene tables of the endpoints that serve
	// ScenesManagement (see [ScenesStore]). With Groups set, every bridged
	// endpoint whose device type mandates ScenesManagement (the light
	// device types) serves the stack's ScenesManagement server, in place
	// of any a source supplies — as matter.js mounts ScenesManagementServer
	// for those device types. nil keeps the tables for the life of the
	// process.
	Scenes ScenesStore
}

// ScenesStore keeps an endpoint's scene table across restarts — matter.js
// keeps sceneTable as nonvolatile state. key is the endpoint's stable key.
type ScenesStore interface {
	LoadScenes(key SourceKey) []byte
	SaveScenes(key SourceKey, table []byte)
}

// Validate returns nil when the config is internally consistent.
func (c Config) Validate() error {
	if c.VendorID == 0 {
		return errors.New("endpoint: Config.VendorID must be non-zero")
	}
	if c.ProductID == 0 {
		return errors.New("endpoint: Config.ProductID must be non-zero")
	}
	if strings.TrimSpace(c.NodeLabel) == "" {
		return errors.New("endpoint: Config.NodeLabel must be non-empty")
	}
	return nil
}

// Assembler turns snapshots of [Spec] values into a [Topology].
// Multi-call-safe; concurrent calls serialise through the underlying
// store transactions.
type Assembler struct {
	store  Store
	cfg    Config
	logger *slog.Logger
	// states owns the per-endpoint state that must outlive a single
	// dispatch (DataVersion trackers, the Identify cluster server),
	// keyed by the stable [SourceKey]. It lives across every
	// [Assembler.Assemble] so a bridged endpoint's DataVersion and
	// running Identify survive reassembly — see
	// [endpointStateRegistry] and [Endpoint.state].
	states *endpointStateRegistry
}

// New returns an assembler. logger may be nil; the assembler then
// uses [slog.Default]. Deciding *which* sources deserve a [Spec] — the
// operator's allowlist above all — happens before this point, in
// whatever walks the owner's model.
func New(s Store, cfg Config, logger *slog.Logger) (*Assembler, error) {
	if s == nil {
		return nil, errors.New("endpoint: store is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	a := &Assembler{
		store:  s,
		cfg:    cfg,
		logger: logger,
		states: newEndpointStateRegistry(),
	}
	return a, nil
}

// Assemble produces the topology from the given snapshots of
// [Spec] values. Endpoint IDs are looked up in the store; new
// sources receive a fresh ID allocated under a transaction. Vanished
// sources (rows in the store with no matching snapshot entry) are
// removed.
//
// Snapshots must have unique Scope values; the assembler does not
// deduplicate. The caller is expected to pass exactly one snapshot per
// scope.
func (a *Assembler) Assemble(ctx context.Context, snapshots []Snapshot) (*Topology, error) {
	// Apple-compatible three-tier topology (mirrors matter.js's
	// `BridgedDevicesNode.ts`):
	//   EP 0 = RootNode  (DeviceType 0x0016) — system services
	//   EP 1 = Aggregator(DeviceType 0x000E) — Descriptor.PartsList enumerates bridged
	//   EP ≥ 2 = bridged devices
	root := &Endpoint{
		ID:         0,
		DeviceType: deviceTypeRootNode,
		Reachable:  true,
	}
	aggregator := &Endpoint{
		ID:         1,
		DeviceType: deviceTypeAggregator,
		Reachable:  true,
	}
	topology := &Topology{
		Endpoints: []*Endpoint{root, aggregator},
		VendorID:  a.cfg.VendorID,
		ProductID: a.cfg.ProductID,
		NodeLabel: a.cfg.NodeLabel,
	}

	seen := make(map[SourceKey]struct{})
	for _, snap := range snapshots {
		if snap.Scope == "" {
			return nil, errors.New("endpoint: snapshot Scope is required")
		}
		for i := range snap.Endpoints {
			ep, err := a.buildEndpoint(ctx, snap.Scope, &snap.Endpoints[i])
			if err != nil {
				return nil, err
			}
			seen[ep.SourceKey] = struct{}{}
			topology.Endpoints = append(topology.Endpoints, ep)
		}
	}

	if err := a.gcVanished(ctx, snapshots, seen); err != nil {
		return nil, err
	}

	// Release the state of sources that vanished / were de-exposed this
	// run so a later re-add gets a fresh version (matches matter.js
	// destroying the Datasource on endpoint removal), a running Identify
	// countdown stops, and the registry stays bounded to the live
	// topology. State of endpoints still present in `seen` is retained,
	// keeping their version stable.
	a.states.retain(seen)

	sort.SliceStable(topology.Endpoints, func(i, j int) bool {
		return topology.Endpoints[i].ID < topology.Endpoints[j].ID
	})
	// Stamp every bridged endpoint with the bridge-wide VID/PID so the
	// BridgedDeviceBasicInformation cluster server can read them from
	// the endpoint without a back-pointer to the topology. Skipped for
	// the root + aggregator endpoints (they only carry root-side
	// BasicInformation, not BridgedDeviceBasicInformation).
	for _, ep := range topology.Endpoints {
		if ep == nil || ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		ep.BridgeVendorID = topology.VendorID
		ep.BridgeProductID = topology.ProductID
	}
	return topology, nil
}

// deviceTypeRootNode is the Matter Device Type ID for the root
// endpoint of a Matter Node. Mirrors Matter Device Library §2.1
// ("Root Node" / 0x0016) and matter.js's `RootNodeDt.id`.
const deviceTypeRootNode = 0x0016

// deviceTypeAggregator is the Matter Device Type ID for the
// Aggregator endpoint (EP 1) that hosts bridged sub-endpoints in its
// Descriptor.PartsList. Mirrors Matter Device Library §13.2
// ("Aggregator" / 0x000E) and matter.js's `AggregatorDt.id`.
const deviceTypeAggregator = 0x000E

// buildEndpoint turns one [Spec] into the assembled endpoint:
// it resolves the persisted endpoint id for the spec's stable key and
// binds the per-identity state that has to survive a reassembly.
func (a *Assembler) buildEndpoint(ctx context.Context, scope string, spec *Spec) (*Endpoint, error) {
	id, err := a.assignOrReuseID(ctx, scope, spec.StableKey, spec.DeviceType)
	if err != nil {
		return nil, err
	}
	reachable := true
	if spec.Availability != nil {
		reachable = spec.Availability()
	}
	return &Endpoint{
		ID:         id,
		DeviceType: spec.DeviceType,
		Reachable:  reachable,
		// The 32-byte NodeLabel cap is Matter's constraint, so the
		// assembly enforces it however the label was produced.
		FriendlyName:   truncateUTF8(spec.FriendlyName, nodeLabelMaxBytes),
		ChannelAddress: spec.ChannelAddress,
		Availability:   spec.Availability,
		Source:         spec.Source,
		Measurement:    spec.Measurement,
		PowerSource:    spec.PowerSource,
		SourceKey:      spec.StableKey,
		Scope:          scope,
		DeviceAddress:  spec.DeviceAddress,
		// Reuse the state bound to this stable source key so the
		// endpoint's per-cluster version and Identify server survive
		// reassembly.
		state: a.restoredState(spec),
		// Bridged endpoints are children of the Aggregator (EP 1).
		// Mirrors chip examples/bridge-app/linux/main.cpp:261-276
		// AddDeviceEndpoint(..., parentEndpointId=1) and matter.js
		// aggregator.add(child) which establishes the same parent chain.
		ParentEndpointID:    1,
		HasParentEndpointID: true,
		groups:              a.cfg.Groups,
		onNodeLabelWritten:  a.cfg.OnNodeLabelWritten,
		scenesStore:         a.cfg.Scenes,
	}, nil
}

// restoredState returns the state bound to spec's key, with the label the
// host restored for it (Spec.NodeLabel) installed unless a controller has
// written one during this process.
func (a *Assembler) restoredState(spec *Spec) *endpointState {
	st := a.states.stateFor(spec.StableKey)
	if spec.NodeLabel != "" {
		st.restoreLabel(truncateUTF8(spec.NodeLabel, nodeLabelMaxBytes))
	}
	st.restoreConfigurationVersion(spec.ConfigurationVersion)
	return st
}

// assignOrReuseID looks up the existing endpoint_id for sourceKey;
// allocates a fresh one otherwise. Updates device_type either way so
// a profile change in the model side migrates cleanly.
func (a *Assembler) assignOrReuseID(ctx context.Context, scope string, sourceKey SourceKey, deviceType uint16) (uint16, error) {
	rec, err := a.store.GetEndpoint(ctx, sourceKey)
	switch {
	case err == nil:
		// Already assigned — refresh device_type if it drifted.
		if rec.DeviceType != deviceType {
			rec.DeviceType = deviceType
			rec.Scope = scope
			if _, err := a.store.UpsertEndpointAssigning(ctx, rec); err != nil {
				return 0, fmt.Errorf("endpoint: refresh device_type: %w", err)
			}
		}
		return rec.EndpointID, nil
	case isNotFound(err):
		// Allocate a fresh ID under a transaction.
		id, err := a.store.UpsertEndpointAssigning(ctx, Record{
			Key:        sourceKey,
			Scope:      scope,
			DeviceType: deviceType,
		})
		if err != nil {
			return 0, fmt.Errorf("endpoint: assign new id: %w", err)
		}
		return id, nil
	default:
		return 0, fmt.Errorf("endpoint: lookup: %w", err)
	}
}

// gcVanished removes store rows for sources that no longer appear
// in any snapshot. seen contains every key produced this run; we
// list the persisted keys per central and drop the difference.
//
// Snapshots that are not model-complete are exempt: at daemon boot the
// topology is assembled before the readiness-gated CCU device load has
// populated the model, so a registered central briefly contributes an
// empty (or partial) device list. Treating that as "every device
// vanished" would delete all persisted endpoint-ID rows on each boot
// and renumber the bridged fleet — controllers key their accessory
// cache on the endpoint number, so persisted numbers must survive a
// restart. Mirrors matter.js, which reserves persisted endpoint
// numbers at initialization (packages/node/src/storage/server/
// ServerEndpointStores.ts, assignNumber) and erases one only on
// explicit endpoint deletion (packages/node/src/node/server/
// ServerEndpointInitializer.ts, eraseDescendant).
func (a *Assembler) gcVanished(ctx context.Context, snapshots []Snapshot, seen map[SourceKey]struct{}) error {
	for _, snap := range snapshots {
		if !snap.ModelComplete {
			// The central has not finished its initial device load; an
			// absent source is "not loaded yet", not "vanished". Keep
			// every persisted row until a model-complete snapshot vouches
			// for the fleet.
			a.logger.Debug(
				"matter endpoint gc skipped: model incomplete",
				slog.String("scope", snap.Scope),
			)
			continue
		}
		records, err := a.store.ListEndpoints(ctx, snap.Scope)
		if err != nil {
			return fmt.Errorf("endpoint: gc list: %w", err)
		}
		for _, rec := range records {
			if _, kept := seen[rec.Key]; kept {
				continue
			}
			if err := a.store.RemoveEndpoint(ctx, rec.Key); err != nil {
				return fmt.Errorf("endpoint: gc remove: %w", err)
			}
			a.logger.Debug(
				"matter endpoint gc",
				slog.String("scope", snap.Scope),
				slog.String("source", rec.Key.String()),
				slog.Int("endpoint_id", int(rec.EndpointID)),
			)
		}
	}
	return nil
}
