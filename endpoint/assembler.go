// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
)

// Config tunes the assembler. The zero value is *not* valid — a non-zero
// VendorID and ProductID must be supplied: bridged endpoints serve them.
// The node's own label is not the assembler's: the root's BasicInformation
// server (cluster/core) serves it.
type Config struct {
	// VendorID is the bridge's IANA-assigned vendor identifier.
	VendorID uint16
	// ProductID is the bridge's vendor-assigned product identifier.
	ProductID uint16
	// VendorName is the node's own BasicInformation VendorName. A bridged
	// endpoint whose [Spec.VendorName] is empty serves it as its
	// BridgedDeviceBasicInformation VendorName, so the vendor fields
	// controllers rely on stay filled; empty as well, the attribute is not
	// served — matter.js's default for a bridged device, where vendorName
	// is optional and its bridge example sets none.
	VendorName string
	// Groups is the node's group state. When set, every bridged endpoint
	// whose device type mandates the Groups cluster (on-off-light,
	// on-off-plug-in-unit, … — schema.DeviceTypeRequiresServerCluster)
	// serves a real Groups server over it, and a Groups server a source
	// supplies itself is replaced by one: group membership is stack state,
	// as in matter.js (GroupsServer keeps it in the root's
	// GroupKeyManagementServer). nil keeps whatever the source supplies.
	Groups *groups.Manager
	// Fabrics is the node's fabric list (the node passes its store): the
	// assembler hands it to every Binding server an endpoint serves
	// ([mattercore.Binding.SetFabricSource]), which checks a request's
	// write against it. nil leaves the Binding servers without a source,
	// and they refuse every request write — fail closed (review II,
	// R2-P6-store-1010, decision 158).
	Fabrics mattercore.FabricSource
	// Persistence keeps a bridged endpoint's controller-written
	// NodeLabel and its scene table across restarts ([Persistence]); the
	// host hands a restored label back as [Spec.NodeLabel]. With Groups
	// set, every bridged endpoint whose device type mandates
	// ScenesManagement (the light device types) serves the stack's
	// ScenesManagement server, in place of any a source supplies — as
	// matter.js mounts ScenesManagementServer for those device types. nil
	// keeps both for the life of the process (or hands them to the
	// older OnNodeLabelWritten and Scenes hooks).
	Persistence Persistence
	// OnNodeLabelWritten fires after a controller wrote a bridged
	// endpoint's BridgedDeviceBasicInformation NodeLabel, with the
	// endpoint's stable key and the new label, so the host can persist it
	// and hand it back as [Spec.NodeLabel] after a restart — matter.js
	// keeps a written nodeLabel in the endpoint's persisted state. nil
	// keeps the label for the life of the process only. Ignored when
	// Persistence is set.
	//
	// Deprecated: use Persistence, whose SaveNodeLabel stores the label
	// before it is served and refuses the write when the store refuses.
	// Removable in the next minor release.
	OnNodeLabelWritten func(key SourceKey, label string)
	// Scenes persists the scene tables of the endpoints that serve
	// ScenesManagement (see [ScenesStore]). nil keeps the tables for the
	// life of the process. Ignored when Persistence is set.
	//
	// Deprecated: use Persistence, whose LoadScenes and SaveScenes report
	// the store's error. Removable in the next minor release.
	Scenes ScenesStore
	// RootDeviceConditions are the device-type conditions the host states
	// for the root endpoint ([Endpoint.DeviceConditions]) — the node's own
	// facts no topology shows, such as LanguageLocale, TimeLocale or
	// UnitLocale. nil states none.
	RootDeviceConditions []string
	// Clock is the time source of the stateful servers the assembler
	// builds per bridged endpoint — the Identify server's IdentifyTime
	// countdown (cluster/core.Identify.SetClock); nil is [clock.System].
	// A node passes its own (node.Config.Clock).
	Clock clock.Clock
}

// Persistence keeps a bridged endpoint's controller-written state across
// restarts, under the endpoint's stable key: its
// BridgedDeviceBasicInformation NodeLabel and its scene table — matter.js
// keeps both as nonvolatile endpoint state and stores a change in the
// commit's first phase (Datasource.ts:1224-1262 at db021da0). A change a
// controller causes is stored with the change, and a refused store answers
// FAILURE with nothing applied (the store-error rule of store/doc.go;
// notes/parity/by_design.md BD-Matter-NonVolatileWrite-StoredBeforeApplied).
type Persistence interface {
	// SaveNodeLabel stores label, written to the endpoint key names. It
	// runs before the label is served: an error fails a controller's
	// write with FAILURE and the endpoint keeps serving the label it had.
	SaveNodeLabel(ctx context.Context, key SourceKey, label string) error
	// LoadScenes returns the scene table stored for key, nil for none. An
	// error leaves the table unloaded: every scene command answers FAILURE
	// and nothing is saved until a later load succeeds, so a failed load
	// never lets a save write over the stored table.
	LoadScenes(ctx context.Context, key SourceKey) ([]byte, error)
	// SaveScenes stores key's scene table after a change, before the
	// change is served: an error fails a scene command with FAILURE and
	// the previous table is kept. A change that is the consequence of
	// another (a group's or a fabric's removal) is kept and saved again
	// with the next change.
	SaveScenes(ctx context.Context, key SourceKey, table []byte) error
}

// ScenesStore keeps an endpoint's scene table across restarts — matter.js
// keeps sceneTable as nonvolatile state. key is the endpoint's stable key.
//
// Deprecated: use [Persistence], whose LoadScenes and SaveScenes report the
// store's error. Removable in the next minor release.
type ScenesStore interface {
	LoadScenes(key SourceKey) []byte
	SaveScenes(key SourceKey, table []byte)
}

// persistence is the Persistence c's endpoints use: Persistence, else the
// older OnNodeLabelWritten and Scenes hooks adapted, else nil.
func (c Config) persistence() Persistence {
	switch {
	case c.Persistence != nil:
		return c.Persistence
	case c.OnNodeLabelWritten != nil || c.Scenes != nil:
		return hookPersistence{onLabel: c.OnNodeLabelWritten, scenes: c.Scenes}
	}
	return nil
}

// hookPersistence adapts the deprecated hooks to [Persistence]: neither can
// refuse, so a save never fails; a missing hook keeps its state for the
// life of the process.
type hookPersistence struct {
	onLabel func(SourceKey, string)
	scenes  ScenesStore
}

func (h hookPersistence) SaveNodeLabel(_ context.Context, key SourceKey, label string) error {
	if h.onLabel != nil {
		h.onLabel(key, label)
	}
	return nil
}

func (h hookPersistence) LoadScenes(_ context.Context, key SourceKey) ([]byte, error) {
	if h.scenes == nil {
		return nil, nil
	}
	return h.scenes.LoadScenes(key), nil
}

func (h hookPersistence) SaveScenes(_ context.Context, key SourceKey, table []byte) error {
	if h.scenes != nil {
		h.scenes.SaveScenes(key, table)
	}
	return nil
}

// Validate returns nil when the config is internally consistent.
func (c Config) Validate() error {
	if c.VendorID == 0 {
		return errors.New("endpoint: Config.VendorID must be non-zero")
	}
	if c.ProductID == 0 {
		return errors.New("endpoint: Config.ProductID must be non-zero")
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
	// rootState and aggregatorState are the root's and the Aggregator's
	// state — the DataVersion trackers of their clusters whose servers
	// keep none of their own — bound to every assembled root and
	// Aggregator, so a version survives reassembly as a bridged
	// endpoint's does (review II, decision 46).
	rootState, aggregatorState *endpointState
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
		states: newEndpointStateRegistry(cfg.Clock),
	}
	a.rootState = newEndpointState(cfg.Clock)
	a.aggregatorState = newEndpointState(cfg.Clock)
	return a, nil
}

// Assemble produces the topology from the given snapshots of
// [Spec] values. Endpoint IDs are looked up in the store; new
// sources receive a fresh ID allocated under a transaction. A stored
// source absent from the snapshots keeps its row and its number
// (see [Snapshot]); the owner removes one with [Store.RemoveEndpoint].
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
		ID:               0,
		DeviceType:       deviceTypeRootNode,
		Reachable:        true,
		DeviceConditions: append([]string(nil), a.cfg.RootDeviceConditions...),
		state:            a.rootState,
	}
	aggregator := &Endpoint{
		ID:         1,
		Aggregator: true,
		DeviceType: deviceTypeAggregator,
		Reachable:  true,
		state:      a.aggregatorState,
	}
	topology := &Topology{
		Endpoints:  []*Endpoint{root, aggregator},
		VendorID:   a.cfg.VendorID,
		ProductID:  a.cfg.ProductID,
		VendorName: a.cfg.VendorName,
	}

	seen := make(map[SourceKey]struct{})
	for _, snap := range snapshots {
		if snap.Scope == "" {
			return nil, errors.New("endpoint: snapshot Scope is required")
		}
		for i := range snap.Endpoints {
			eps, err := a.buildEndpointTree(ctx, snap.Scope, &snap.Endpoints[i], 1, false)
			if err != nil {
				return nil, err
			}
			for _, ep := range eps {
				seen[ep.SourceKey] = struct{}{}
			}
			topology.Endpoints = append(topology.Endpoints, eps...)
		}
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
		ep.BridgeVendorName = topology.VendorName
		ep.fabrics = a.cfg.Fabrics
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

// buildEndpointTree assembles spec under parent and its [Spec.Parts] under
// it, depth first: the spec's endpoint first, then its parts' endpoints.
// Mirrors matter.js Endpoint parts: a part is a child endpoint, numbered
// like any other, in its parent's PartsList.
//
// The PartsList follows the endpoint's composition. An endpoint whose
// device-type list holds a full-family type — a bridged node, BridgedNode
// being full-family since matter.js #4735 (Device Library § 2.5.6) — lists
// every descendant; any other endpoint its direct parts. Mirrors matter.js
// packages/node/src/behaviors/descriptor/DescriptorServer.ts:227-254
// (#currentPartsListNumbers: the IndexBehavior's partsByNumber, the
// endpoint itself removed, for a full-family composer; endpoint.parts
// otherwise; ascending either way).
func (a *Assembler) buildEndpointTree(ctx context.Context, scope string, spec *Spec, parent uint16, part bool) ([]*Endpoint, error) {
	if err := checkBridgedInfo(spec, part); err != nil {
		return nil, err
	}
	ep, err := a.buildEndpoint(ctx, scope, spec)
	if err != nil {
		return nil, err
	}
	ep.ParentEndpointID = parent
	ep.Part = part
	out := []*Endpoint{ep}
	for i := range spec.Parts {
		children, err := a.buildEndpointTree(ctx, scope, &spec.Parts[i], ep.ID, true)
		if err != nil {
			return nil, err
		}
		ep.PartIDs = append(ep.PartIDs, children[0].ID)
		out = append(out, children...)
	}
	if composesFullFamily(ep) {
		ep.PartIDs = ep.PartIDs[:0]
		for _, d := range out[1:] {
			ep.PartIDs = append(ep.PartIDs, d.ID)
		}
	}
	slices.Sort(ep.PartIDs)
	return out, nil
}

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
		FriendlyName:   truncateLabel(spec.FriendlyName),
		ChannelAddress: spec.ChannelAddress,
		Availability:   spec.Availability,
		Source:         spec.Source,
		Measurement:    spec.Measurement,
		PowerSource:    spec.PowerSource,
		SourceKey:      spec.StableKey,
		Scope:          scope,
		DeviceAddress:  spec.DeviceAddress,
		VendorName:     spec.VendorName,
		// What the host states of the bridged device's basic information
		// (R2-P5-apidev-02).
		BridgedBasicInformation: spec.BridgedBasicInformation,
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
		DeviceConditions:    append([]string(nil), spec.DeviceConditions...),
		DeviceTypes:         append([]uint16(nil), spec.DeviceTypes...),
		Tags:                append([]mattercore.SemanticTag(nil), spec.Tags...),
		ClientClusters:      append([]uint32(nil), spec.ClientClusters...),
		OptIn:               append([]uint32(nil), spec.OptIn...),
		groups:              a.cfg.Groups,
		persistence:         a.cfg.persistence(),
		log:                 a.logger,
	}, nil
}

// restoredState returns the state bound to spec's key, with the label the
// host restored for it (Spec.NodeLabel) installed unless a controller has
// written one during this process.
func (a *Assembler) restoredState(spec *Spec) *endpointState {
	st := a.states.stateFor(spec.StableKey)
	// The host's name is the Spec's again: a name set live
	// (Endpoint.SetHostName) is what the Spec carries by now, and a stale
	// one must not outlive the device that set it.
	st.clearHostName()
	if spec.NodeLabel != "" {
		st.restoreLabel(truncateLabel(spec.NodeLabel))
	}
	st.restoreConfigurationVersion(spec.ConfigurationVersion)
	return st
}

// assignOrReuseID looks up the existing endpoint_id for sourceKey;
// allocates a fresh one otherwise. Updates device_type either way so
// a profile change in the model side migrates cleanly. A number outside
// [Record]'s range [2, 65534] is an error naming the key.
func (a *Assembler) assignOrReuseID(ctx context.Context, scope string, sourceKey SourceKey, deviceType uint16) (uint16, error) {
	id, err := a.lookupOrAssignID(ctx, scope, sourceKey, deviceType)
	if err != nil {
		return 0, err
	}
	// A Store's number is held to [Record]'s range: 0 and 1 would put a
	// second root or Aggregator into the topology, 65535 is reserved
	// (R2-P6-store-0448).
	if id < recordIDMin || id > recordIDMax {
		return 0, fmt.Errorf("endpoint: store answered endpoint id %d for %q, outside [%d, %d]", id, sourceKey.String(), recordIDMin, recordIDMax)
	}
	return id, nil
}

// recordIDMin and recordIDMax bound a stored [Record.EndpointID].
const (
	recordIDMin uint16 = 2
	recordIDMax uint16 = 65534
)

// lookupOrAssignID is [Assembler.assignOrReuseID] before the range check.
func (a *Assembler) lookupOrAssignID(ctx context.Context, scope string, sourceKey SourceKey, deviceType uint16) (uint16, error) {
	rec, err := a.store.GetEndpoint(ctx, sourceKey)
	switch {
	case err == nil:
		// Already assigned — refresh device_type if it drifted, and the
		// scope if the source moved to another snapshot: a record that
		// kept its old scope would never be listed by the new scope, nor
		// erased with it (node.Aggregator.RetirePartition).
		if rec.DeviceType != deviceType || rec.Scope != scope {
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
