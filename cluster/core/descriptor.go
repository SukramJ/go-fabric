// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Descriptor implements the Matter Descriptor cluster (0x001D) per
// Matter Core Specification 1.5.1 §9.5. Mandatory on every endpoint:
// it advertises the device-type list, the server cluster list, the
// client cluster list, and the parts list (sub-endpoints).
//
// The bridge wires one Descriptor per endpoint — the Root endpoint's
// PartsList enumerates every bridged endpoint; bridged endpoints
// have an empty PartsList (no sub-endpoints).
type Descriptor struct {
	deviceTypes []DeviceTypeStruct
	clientList  []uint32

	// mu guards the fields the topology assembler installs after the
	// endpoint is already serving reads. The daemon attaches the
	// PartsList / ServerList providers well after bridge.Start, so a
	// commissioner re-establishing CASE can be inside MatterRead while
	// the provider is written.
	mu                 sync.RWMutex
	serverList         []uint32
	partsList          []uint16
	partsListProvider  func() []uint16
	serverListProvider func() []uint32
	// tagList is TagList, served only with the TagList feature selected
	// (tagListOn, [Descriptor.EnableTagList]).
	tagListOn bool
	tagList   []SemanticTag

	// version is the cluster's DataVersion, bumped whenever an observed
	// ServerList / PartsList differs from the last one; lastLists is that
	// last observation. See MatterDataVersion.
	versionMu sync.Mutex
	version   cluster.DataVersionTracker
	lastLists string
}

// MatterDataVersion implements [contract.ClusterDataVersion].
//
// Descriptor's content changes only through its providers (the mounted set
// and the topology), never through a write, so there is no write path to
// bump on. The version is therefore derived from the content: each call
// renders the two dynamic lists and bumps when they differ from the last
// rendering. A Descriptor without a version answered every read with the
// sentinel DataVersion, which the read path will not filter on — a
// controller's DataVersionFilter for it never matched, and the CHIP read
// cases (TC-IDM-2.2 step 13) fail exactly that. Mirrors matter.js, where
// every cluster's Datasource carries a version that advances when its
// state changes (Datasource.ts).
func (d *Descriptor) MatterDataVersion() uint32 {
	server, _ := d.MatterRead(descriptorAttrServerList)
	parts, _ := d.MatterRead(descriptorAttrPartsList)
	lists := fmt.Sprint(server, parts)
	d.versionMu.Lock()
	defer d.versionMu.Unlock()
	if d.lastLists != "" && d.lastLists != lists {
		d.version.Bump()
	}
	d.lastLists = lists
	return d.version.Current()
}

// DeviceTypeStruct is the Descriptor.DeviceTypeList entry, the generated
// struct of descriptor.element.ts: (DeviceType, Revision).
type DeviceTypeStruct = descdef.DeviceTypeStruct

// Cluster ID, revision and attribute ids: the generated definition's
// (cluster/spec/descriptor, ADR 0013). The attribute list and the write
// statuses come from it as well; the lists' content is the topology's.
const (
	descriptorClusterID       = descdef.ClusterID
	descriptorClusterRevision = descdef.Revision

	descriptorAttrDeviceTypeList = descdef.AttrDeviceTypeList
	descriptorAttrServerList     = descdef.AttrServerList
	descriptorAttrClientList     = descdef.AttrClientList
	descriptorAttrPartsList      = descdef.AttrPartsList
	descriptorAttrTagList        = descdef.AttrTagList
)

// descriptorInst is the definition bound to what the server serves: no
// feature, so TagList (conformance TAGLIST) is not served.
var descriptorInst = mustInstance(descdef.Definition, spec.Options{})

// descriptorInstTagList is the definition with the TagList feature, for a
// Descriptor [Descriptor.EnableTagList] opted in.
var descriptorInstTagList = mustInstance(descdef.Definition, spec.Options{Features: uint32(descdef.FeatureTagList)})

// SemanticTag is a TagList entry, the generated semtag struct.
type SemanticTag = descdef.Semtag

// ErrTagListNotEnabled is [Descriptor.AddTags] on a Descriptor without the
// TagList feature. Mirrors matter.js DescriptorServer.ts:124-126.
var ErrTagListNotEnabled = errors.New(`matter: you must enable the descriptor "TagList" feature to set tags`)

// errDescriptorReadOnly is returned for any write attempt — the
// Descriptor cluster has no writable attributes.
var errDescriptorReadOnly = spec.Errorf(im.StatusUnsupportedWrite, "core: Descriptor is read-only")

// NewDescriptor returns a Descriptor with the supplied static lists.
// deviceTypes must contain at least one entry per Matter §9.5.5.1. A
// device type listed twice is kept once, at the revision of its first
// entry (see [dedupeDeviceTypes]).
func NewDescriptor(deviceTypes []DeviceTypeStruct, serverList, clientList []uint32, partsList []uint16) (*Descriptor, error) {
	if len(deviceTypes) == 0 {
		return nil, errors.New("core: Descriptor requires at least one DeviceType entry")
	}
	return &Descriptor{
		deviceTypes: dedupeDeviceTypes(deviceTypes),
		serverList:  append([]uint32(nil), serverList...),
		clientList:  append([]uint32(nil), clientList...),
		partsList:   append([]uint16(nil), partsList...),
	}, nil
}

// dedupeDeviceTypes returns a copy of deviceTypes in which every device
// type ID appears once, in the order of its first entry and at that
// entry's revision. Mirrors matter.js 0ceb202d packages/node/src/behaviors/
// descriptor/DescriptorServer.ts:91-111 (addDeviceTypes: `if
// (existingDeviceType.deviceType === newDeviceType.deviceType) continue
// nextInput` at :104-108 — "A device type that is already listed keeps its
// listed revision", :86-87). It catches a bridged endpoint whose host
// device type is BridgedNode itself, which endpoint.Materialize lists
// next to the BridgedNode entry it adds.
func dedupeDeviceTypes(deviceTypes []DeviceTypeStruct) []DeviceTypeStruct {
	out := make([]DeviceTypeStruct, 0, len(deviceTypes))
next:
	for _, dt := range deviceTypes {
		for _, listed := range out {
			if listed.DeviceType == dt.DeviceType {
				continue next
			}
		}
		out = append(out, dt)
	}
	return out
}

// Compile-time assertion: Descriptor satisfies MatterClusterServer.
var _ contract.ClusterServer = (*Descriptor)(nil)

// MatterClusterID implements [contract.ClusterServer].
func (d *Descriptor) MatterClusterID() uint32 { return descriptorClusterID }

// MatterRead returns the static lists. PartsList is mutable when the
// topology changes — callers update it via [Descriptor.SetPartsList].
func (d *Descriptor) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case descriptorAttrDeviceTypeList:
		return append([]DeviceTypeStruct(nil), d.deviceTypes...), true
	case descriptorAttrServerList:
		// Apple Home reads ServerList post-CASE and rejects an endpoint
		// as schematically inconsistent when a Subscribe AttributeReport
		// arrives for a cluster that is NOT in ServerList — or when a
		// cluster IS in ServerList but never produces an AttributeReport.
		// Mirrors matter.js DescriptorServer.#serverList
		// (DescriptorServer.ts:241-250): always derived from the set of
		// mounted behaviors, never hardcoded. The provider closure is
		// the go-fabric equivalent — set after endpoint composition
		// is complete so the list cannot drift from the actual mounted
		// cluster set (Bug K).
		d.mu.RLock()
		provider := d.serverListProvider
		var static []uint32
		if provider == nil {
			// Copy only on the fallback path: every assembled endpoint
			// installs a provider, so materialising the static list first
			// would allocate on every read Apple Home drives post-CASE and
			// then throw the copy away.
			static = append(static, d.serverList...)
		}
		d.mu.RUnlock()
		if provider != nil {
			return provider(), true
		}
		return static, true
	case descriptorAttrClientList:
		// Always return a non-nil slice — Apple Home's IM-decoder
		// rejects `null` for a list-typed attribute (matter.js spec
		// says default `[]`, not null) and drops the entire ClientList
		// from the topology dictionary cache, which then propagates
		// into HAPErrorDomain Code=14 "No Endpoints In Use".
		out := make([]uint32, 0, len(d.clientList))
		out = append(out, d.clientList...)
		return out, true
	case descriptorAttrPartsList:
		d.mu.RLock()
		provider := d.partsListProvider
		var out []uint16
		if provider == nil {
			// Same as ServerList above: the copy is only needed when no
			// provider answers the read.
			out = make([]uint16, 0, len(d.partsList))
			out = append(out, d.partsList...)
		}
		d.mu.RUnlock()
		if provider != nil {
			return provider(), true
		}
		return out, true
	case descriptorAttrTagList:
		d.mu.RLock()
		defer d.mu.RUnlock()
		if d.tagListOn {
			return cloneTags(d.tagList), true
		}
		// TagList (Matter 1.4 §9.5.6.5) is conformance "TAGLIST" —
		// only present when the TAGLIST feature bit is set in the
		// FeatureMap. We advertise FeatureMap=0 (no TAGLIST), so the
		// attribute MUST be reported as unsupported. Returning a
		// generic `[]uint32{}` here makes Apple's iOS Matter SDK
		// reject the whole Descriptor cluster (Apple does not yet
		// ship a `semtag` struct schema) and HAP-Service-Build
		// aborts with HAPErrorDomain Code=14. matter.js / chip-tool
		// tolerate the absence; re-enable when we surface tags.
		return nil, false
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return d.def().ReadGlobal(attrID)
	}
	return nil, false
}

// def is the definition bound to the server's feature selection.
func (d *Descriptor) def() *spec.Instance {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.tagListOn {
		return descriptorInstTagList
	}
	return descriptorInst
}

// EnableTagList selects the TagList feature and sets the initial TagList —
// matter.js DescriptorServer.with("TagList") with a tagList state. FeatureMap
// then carries TAGLIST and TagList is served. Without it neither is (the
// default: Apple's Matter stack rejects a Descriptor serving TagList, see
// MatterAttributes).
func (d *Descriptor) EnableTagList(tags ...SemanticTag) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tagListOn = true
	d.tagList = cloneTags(tags)
}

// AddTags adds semantic tags to TagList, deduplicated: a tag with the
// manufacturer code, namespace and tag of a listed one is not added again;
// a non-null label given with it replaces the listed label, a null or
// missing label keeps it. It fails with [ErrTagListNotEnabled] without the
// TagList feature. Mirrors matter.js DescriptorServer.ts addTags (:122-147).
func (d *Descriptor) AddTags(tags ...SemanticTag) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.tagListOn {
		return ErrTagListNotEnabled
	}
next:
	for _, t := range tags {
		for i := range d.tagList {
			listed := &d.tagList[i]
			if listed.MfgCode == t.MfgCode && listed.NamespaceId == t.NamespaceId && listed.Tag == t.Tag {
				if t.Label != nil && !t.Label.Null && (listed.Label == nil || listed.Label.Null || listed.Label.Value != t.Label.Value) {
					listed.Label = &spec.Nullable[string]{Value: t.Label.Value}
				}
				continue next
			}
		}
		d.tagList = append(d.tagList, cloneTags([]SemanticTag{t})...)
	}
	return nil
}

// cloneTags copies tags, labels included.
func cloneTags(tags []SemanticTag) []SemanticTag {
	out := make([]SemanticTag, len(tags))
	for i, t := range tags {
		if t.Label != nil {
			l := *t.Label
			t.Label = &l
		}
		out[i] = t
	}
	return out
}

// MatterWrite always rejects — Descriptor has no writable attributes: a
// served one answers UNSUPPORTED_WRITE and any other
// UNSUPPORTED_ATTRIBUTE, as the definition judges them; the error still
// matches errDescriptorReadOnly.
func (d *Descriptor) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := d.def().ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("%w: 0x%04X", errDescriptorReadOnly, attrID), status: err}
}

// MatterInvoke always rejects — Descriptor has no commands.
func (d *Descriptor) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("core: Descriptor has no commands (got 0x%02X)", cmdID)
}

// MatterReportable returns the attributes that subscribe-able. PartsList
// changes when the bridge adds or removes a bridged endpoint.
func (d *Descriptor) MatterReportable() []uint32 {
	return []uint32{descriptorAttrPartsList}
}

// MatterAttributes implements [contract.ClusterAttributeLister]
// so wildcard subscribe / read enumerates the full Descriptor surface.
// Apple Home reads DeviceTypeList + ServerList + PartsList on every
// endpoint to build the HAP topology — missing entries trigger
// "no enumeration/topology dictionary found" and pairing fails.
//
// FeatureMap (0xFFFC) and ClusterRevision (0xFFFD) are included explicitly
// here in addition to being prepended by the dispatcher's global-attribute
// expansion (dispatcher.go:265). The explicit listing ensures that any
// Subscribe wildcard on a Descriptor cluster sees these attributes even if
// the dispatcher's global prepend behaviour changes, and closes the Apple
// cache-drop for EP14/EP28 Descriptor.FeatureMap / ClusterRevision.
// Mirrors chip src/app/clusters/descriptor/descriptor.cpp which serves
// FeatureMap=0 and ClusterRevision via the standard
// AttributeAccessInterface global-attribute mechanism on every
// endpoint, including bridged ones.
func (d *Descriptor) MatterAttributes() []uint32 {
	// TagList (0x0004) intentionally omitted: it is gated behind the
	// TAGLIST FeatureMap bit (Matter 1.4 §9.5.4.1) which we do not
	// advertise. Apple's iOS Matter SDK does not yet ship the
	// `semtag` struct schema and rejects the whole Descriptor cluster
	// when TagList shows up in the wildcard expansion → HAP build
	// fails with Code=14.
	return append(d.def().MatterAttributes(), cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision)
}

// SetPartsList replaces the parts list under a copy. Used by the
// endpoint assembler when the topology changes — typically only
// relevant on the Root endpoint's Descriptor.
func (d *Descriptor) SetPartsList(partsList []uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.partsList = append([]uint16(nil), partsList...)
}

// SetPartsListProvider installs a closure consulted on every
// PartsList read; it overrides the static list set by [NewDescriptor]
// and [SetPartsList]. The Root endpoint's Descriptor uses this so
// the bridge's dynamic endpoint registry (populated post-Reassemble)
// surfaces directly through `0:0x001D:0x0003` reads. Apple Home reads
// PartsList right after the initial subscribe; an empty list makes
// the bridge look "empty" and Apple's iCloud-Heim sync rejects the
// commissioning with a generic add-failed error.
//
// Pass nil to revert to the static list.
func (d *Descriptor) SetPartsListProvider(p func() []uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.partsListProvider = p
}

// SetServerListProvider installs a closure consulted on every
// ServerList read; it overrides the static list set by [NewDescriptor].
// The Root endpoint's Descriptor uses this so the actual mounted
// MatterClusterServer set (assembled in daemon.go after the full
// endpoint composition completes) is the single source of truth and
// the advertised ServerList cannot drift from it.
//
// Mirrors matter.js DescriptorServer.#serverList
// (packages/node/src/behaviors/descriptor/DescriptorServer.ts:241-250):
// always derived from `endpoint.behaviors.supported`, never hardcoded.
//
// Pass nil to revert to the static list (only used by unit tests that
// pre-populate a synthetic ServerList).
func (d *Descriptor) SetServerListProvider(p func() []uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.serverListProvider = p
}
