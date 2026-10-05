// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// AccessControl implements the Matter Access Control Cluster
// (0x001F) per Matter Core Specification 1.5.1 §9.10. Mandatory on
// the Root endpoint; the cluster's `acl` attribute is the controller's
// authoritative source for what each subject may read / write / invoke
// on every cluster of every endpoint.
//
// go-fabric exposes the ACL list READ-ONLY in v1.1 — every entry is
// inserted by the bridge itself (the AddNOC handler installs the
// default Administer entry per §11.18.6.8.1 immediately after the
// fabric is persisted). Apple Home reads ACL right after CASE; an
// empty / missing cluster surfaces as ACCESS_DENIED on every
// follow-up read and Apple's pairing UI tears the new fabric down via
// RemoveFabric. Implementing ACL READ is the minimum surface that
// keeps Apple happy through the post-CASE handshake.
type AccessControl struct {
	store ACLStoreFacade

	mu            sync.RWMutex
	currentFabric uint8

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped after every successful ACL replace so subscribers
	// can detect changes. Satisfies [contract.ClusterDataVersion].
	dataVersion cluster.DataVersionTracker

	// extensions holds the per-fabric AccessControlExtensionStruct list
	// (attribute 0x0001, conformance EXTS). The list is keyed by fabric
	// index and stored in-memory; the entries are vendor-opaque octstrings
	// (max 128 bytes each) that controllers use to attach metadata to an
	// ACL fabric. Mirroring matter.js AccessControlServer.ts in-memory
	// extension state.
	extensions map[uint8][]AccessControlExtensionEntry

	// Event surface — wired by the bridge during topology assembly via
	// [SetMatterEventEmitter] + [SetEndpoint] so [MatterWrite] can fire
	// the spec-mandated AccessControlEntryChanged event (Matter §9.10.7.1,
	// event id 0x0, priority Info) on every ACL mutation. Mirrors matter.js
	// packages/node/src/behaviors/access-control/AccessControlServer.ts where
	// acl attribute writes trigger the entryChanged event.
	endpoint uint16
	emitter  contract.EventEmitter

	// auxSource supplies the auxiliary entries of the Auxiliary (AUX)
	// feature; nil keeps the feature off. auxApplied is the set the
	// AuxiliaryAcl attribute last served, per fabric, so a change can be
	// told from a recomputation. Mirrors matter.js AccessControlServer
	// internal.auxiliaryAclProviders / state.auxiliaryAcl.
	auxSource  AuxiliaryACLSource
	auxApplied map[uint8][]store.ACLEntry
	// auxBatch defers synchronisation while a command changes several
	// sources in a row, so it is evaluated once, as matter.js derives once
	// per command transaction.
	auxBatch int
}

// AuxiliaryACLSource supplies the auxiliary access control entries of the
// AccessControl Auxiliary feature and signals their changes. *groups.Manager
// satisfies it; the Groupcast server registers it ([NewGroupcast]), as
// matter.js GroupcastServer calls AccessControlServer.registerAuxAclProvider.
type AuxiliaryACLSource interface {
	// AuxiliaryACL returns the entries of one fabric, or of every fabric
	// for fabricIndex 0.
	AuxiliaryACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error)
	// Fabrics lists the fabrics that exist.
	Fabrics(ctx context.Context) ([]uint8, error)
	// OnGroupcastChanged registers fn for a change that may move the
	// entries; ctx is the context of the request behind the change.
	OnGroupcastChanged(fn func(ctx context.Context, fabricIndex uint8))
}

// AccessControlEntryStruct auxiliary types (AccessControlAuxiliaryTypeEnum,
// access-control.element.ts).
const (
	// AccessControlAuxiliaryTypeSystem is AuxiliaryType System (0).
	AccessControlAuxiliaryTypeSystem uint8 = 0
	// AccessControlAuxiliaryTypeGroupcast is AuxiliaryType Groupcast (1).
	AccessControlAuxiliaryTypeGroupcast uint8 = 1
)

// AccessControlAuxiliaryEntryStruct is one entry of the AuxiliaryAcl
// attribute: an AccessControlEntryStruct with its AuxiliaryType (field 5).
// Every field but FabricIndex is fabric-sensitive (access "S"), so an
// unfiltered read carries another fabric's entry Redacted: FabricIndex
// alone. Mirrors matter.js StructManager, whose mayRead withholds a
// fabric-sensitive field from a session of another fabric.
type AccessControlAuxiliaryEntryStruct struct {
	Entry         AccessControlEntryStruct
	AuxiliaryType uint8
	Redacted      bool
}

// AuxiliaryAccessUpdatedEvent is the payload of AuxiliaryAccessUpdated
// (event 0x0003, conformance AUX, access "S A"): [0] AdminNodeID (nullable),
// [0xFE] FabricIndex. Mirrors access-control.element.ts.
type AuxiliaryAccessUpdatedEvent struct {
	AdminNodeID *uint64
	FabricIndex uint8
}

// AccessControlExtensionEntry mirrors Matter §9.10.4.6
// AccessControlExtensionStruct. The Data field is a vendor-opaque
// octet-string (max 128 bytes); FabricIndex is stamped by the cluster
// server from the IM session context on every write.
type AccessControlExtensionEntry struct {
	Data        []byte
	FabricIndex uint8
	// Redacted marks another fabric's entry on a non-fabric-filtered
	// read: Data is fabric-sensitive ("S", access-control.element.ts
	// AccessControlExtensionStruct) and is left out.
	Redacted bool
}

// ACLStoreFacade is the subset of [store.Store] this cluster reads
// and writes. ReplaceACL is the post-CASE write path Apple Home uses
// to install HomePod / AppleTV edge controllers as additional
// Administer subjects after CommissioningComplete (see Matter §9.10
// + the iCloud-Heim post-pairing step Apple's homed runs).
type ACLStoreFacade interface {
	ListACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error)
	ReplaceACL(ctx context.Context, fabricIndex uint8, entries []store.ACLEntry) error
}

// Cluster ID + revision per Matter §9.10.
const (
	accessControlClusterID       uint32 = 0x001F
	accessControlClusterRevision uint16 = 3 // matter.js HEAD access-control.element.ts:21 default=3

	accessControlAttrACL                           uint32 = 0x0000
	accessControlAttrExtension                     uint32 = 0x0001
	accessControlAttrSubjectsPerAccessControl      uint32 = 0x0002
	accessControlAttrTargetsPerAccessControl       uint32 = 0x0003
	accessControlAttrAccessControlEntriesPerFabric uint32 = 0x0004

	// Capacity limits we report. The Matter Core spec floor is 4 per
	// dimension; matter.js uses the same constants. Higher numbers
	// would advertise capacity we do not actually enforce.
	accessControlSubjectsPerEntry      uint16 = 4
	accessControlTargetsPerEntry       uint16 = 4
	accessControlEntriesPerFabricLimit uint16 = 4

	// AuthMode + Privilege values mirror Matter §9.10.4.4 enums and
	// matter.js packages/types/src/clusters/access-control.ts.
	accessControlAuthModePASE        uint8 = 1
	accessControlAuthModeCASE        uint8 = 2 //nolint:unused // listed for symmetry with matter.js enum
	accessControlAuthModeGroup       uint8 = 3
	accessControlPrivilegeView       uint8 = 1 // lowest valid privilege enum (View); Administer=5 is the highest
	accessControlPrivilegeAdminister uint8 = 5

	// accessControlEventEntryChanged is the Matter §9.10.7.1 event
	// (id 0x0, priority Info) emitted after every ACL mutation. Mirrors
	// matter.js packages/model/src/standard/elements/access-control.element.ts:62.
	accessControlEventEntryChanged uint32 = 0x0000
	// accessControlEventExtensionChanged is the Matter §9.10.7.2 event
	// (id 0x1, conformance EXTS). chip emits this event per spec §9.10.7
	// and matter.js element.ts:77-88 lists it as conformance "EXTS".
	// Emitted from the Extension write path alongside the DataVersion
	// bump, mirroring how accessControlEventEntryChanged rides the ACL
	// write.
	accessControlEventExtensionChanged uint32 = 0x0001

	// accessControlAttrAuxiliaryACL is AuxiliaryAcl (0x0007, conformance
	// AUX, access "R F A", quality C) and accessControlEventAuxiliary
	// AccessUpdated its change event (0x0003). The Auxiliary feature is
	// FeatureMap bit 2. Mirrors access-control.element.ts.
	accessControlAttrAuxiliaryACL        uint32 = 0x0007
	accessControlEventAuxiliaryAccessUpd uint32 = 0x0003
	accessControlFeatureExtension        uint32 = 0x1
	accessControlFeatureAuxiliary        uint32 = 0x4
	accessControlAuxiliaryACLMaxEntries         = 2000
)

// ChangeType constants for [AccessControlEntryChangedEvent], mirroring
// Matter §9.10.4.2 ChangeTypeEnum and matter.js
// packages/model/src/standard/elements/access-control.element.ts:35-39.
const (
	// AccessControlChangeTypeChanged signals an existing entry was modified.
	AccessControlChangeTypeChanged uint8 = 0
	// AccessControlChangeTypeAdded signals a new entry was inserted.
	AccessControlChangeTypeAdded uint8 = 1
	// AccessControlChangeTypeRemoved signals an entry was deleted.
	AccessControlChangeTypeRemoved uint8 = 2
)

// AccessControlEntryChangedEvent is the payload for event 0x0000 on
// cluster 0x001F. Mirrors Matter §9.10.7.1 and matter.js
// packages/model/src/standard/elements/access-control.element.ts:62-74.
// Priority: Info (MatterEventPriorityInfo).
//
// AdminNodeID and AdminPasscodeID are both nullable (quality X); both
// are nil in v1.1 because the IM layer does not yet track which
// commissioner sent the write. LatestValue is nullable (quality X) and
// set to nil for bulk-replace operations where per-entry diffing is
// ambiguous.
type AccessControlEntryChangedEvent struct {
	AdminNodeID     *uint64                   // nullable; nil = "not tracked"
	AdminPasscodeID *uint16                   // nullable; nil = "not tracked"
	ChangeType      uint8                     // AccessControlChangeType{Changed,Added,Removed}
	LatestValue     *AccessControlEntryStruct // nullable; nil for bulk-replace
	FabricIndex     uint8
}

// AccessControlExtensionChangedEvent is the payload for event 0x0001
// on cluster 0x001F. Mirrors Matter §9.10.7.2 and matter.js
// packages/model/src/standard/elements/access-control.element.ts:84-98
// (same field set as AccessControlEntryChanged, but LatestValue is an
// AccessControlExtensionStruct). Priority: Info
// (MatterEventPriorityInfo).
//
// AdminNodeID and AdminPasscodeID are both nullable (quality X); both
// are nil in v1.1 for the same reason as [AccessControlEntryChangedEvent].
type AccessControlExtensionChangedEvent struct {
	AdminNodeID     *uint64                      // nullable; nil = "not tracked"
	AdminPasscodeID *uint16                      // nullable; nil = "not tracked"
	ChangeType      uint8                        // AccessControlChangeType{Changed,Added,Removed}
	LatestValue     *AccessControlExtensionEntry // nullable; nil for bulk-replace
	FabricIndex     uint8
}

// AccessControlEntryStruct mirrors Matter §9.10.4.4 AccessControlEntry.
// Field order matches the wire-encoded TLV tags so the default
// attribute writer can emit it via reflection.
type AccessControlEntryStruct struct {
	Privilege uint8             // 1=View, 2=ProxyView, 3=Operate, 4=Manage, 5=Administer
	AuthMode  uint8             // 1=PASE, 2=CASE, 3=Group
	Subjects  []uint64          // nullable; nil ⇒ matches every subject
	Targets   []ACLTargetStruct // nullable; nil ⇒ matches every cluster/endpoint/device-type
	// AuxiliaryType is field 5 as a write carried it. A controller may
	// not set it — the ACL write refuses an entry that does — and a stored
	// entry never has one.
	AuxiliaryType *uint8
	FabricIndex   uint8
	// Redacted marks another fabric's entry on a non-fabric-filtered
	// read: it goes out with its FabricIndex alone, every other field
	// being fabric-sensitive (access-control.element.ts
	// AccessControlEntryStruct, access "S").
	Redacted bool
}

// ACLTargetStruct mirrors §9.10.4.5.
type ACLTargetStruct struct {
	Cluster    *uint32 // null ⇒ any cluster
	Endpoint   *uint16 // null ⇒ any endpoint
	DeviceType *uint32 // null ⇒ any device type
}

// NewAccessControl constructs the cluster.
func NewAccessControl(s ACLStoreFacade) (*AccessControl, error) {
	if s == nil {
		return nil, errors.New("matter: AccessControl store is required")
	}
	a := &AccessControl{store: s}
	a.loadExtensions(context.Background())
	return a, nil
}

// ACLExtensionPersistence is the optional key-value side of the store an
// AccessControl keeps its Extension entries in ([store.Store] has it).
// matter.js persists the extension attribute like any other fabric-scoped
// state; without persistence a reboot lost it (TC-ACL-2.10 step 9 reboots
// the DUT and reads the extension back).
type ACLExtensionPersistence interface {
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// aclExtensionSettingKey is the settings key of one fabric's Extension.
func aclExtensionSettingKey(fabric uint8) string {
	return "access_control.extension." + strconv.Itoa(int(fabric))
}

// loadExtensions restores the persisted Extension entries, one per fabric
// at most (the attribute's per-fabric constraint).
func (a *AccessControl) loadExtensions(ctx context.Context) {
	p, ok := a.store.(ACLExtensionPersistence)
	if !ok {
		return
	}
	for f := 1; f <= 254; f++ {
		v, found, err := p.GetSetting(ctx, aclExtensionSettingKey(uint8(f)))
		if err != nil || !found || v == "" {
			continue
		}
		data, err := hex.DecodeString(v)
		if err != nil {
			continue
		}
		if a.extensions == nil {
			a.extensions = make(map[uint8][]AccessControlExtensionEntry)
		}
		a.extensions[uint8(f)] = []AccessControlExtensionEntry{{Data: data, FabricIndex: uint8(f)}}
	}
}

// persistExtension writes one fabric's Extension entries ("" clears them).
func (a *AccessControl) persistExtension(ctx context.Context, fabric uint8, entries []AccessControlExtensionEntry) error {
	p, ok := a.store.(ACLExtensionPersistence)
	if !ok {
		return nil
	}
	v := ""
	if len(entries) > 0 {
		v = hex.EncodeToString(entries[0].Data)
	}
	return p.SetSetting(ctx, aclExtensionSettingKey(fabric), v)
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*AccessControl)(nil)
	_ contract.FabricScopedReader             = (*AccessControl)(nil)
	_ contract.EventReceiver                  = (*AccessControl)(nil)
	_ contract.ClusterDataVersion             = (*AccessControl)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*AccessControl)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*AccessControl)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (a *AccessControl) MatterClusterID() uint32 { return accessControlClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
// Returns the current per-cluster monotonic counter bumped on every
// successful ACL replace. Mirrors matter.js AccessControlServer.ts
// DataVersion tracking on ACL attribute mutations.
func (a *AccessControl) MatterDataVersion() uint32 { return a.dataVersion.Current() }

// MinReadPrivilege implements [contract.ClusterAttributeReadPrivilege].
// ACL (0x0000) and Extension (0x0001) require Administer (5) per Matter
// §9.10.5.3. Mirrors chip
// src/app/clusters/access-control-server/access-control-server.cpp
// AttributeReadAclRequired guard, and matter.js
// packages/model/src/standard/elements/access-control.element.ts
// access: "administer" on acl + extension attributes.
func (*AccessControl) MinReadPrivilege(attrID uint32) uint8 {
	switch attrID {
	case accessControlAttrACL, accessControlAttrExtension, accessControlAttrAuxiliaryACL:
		return accessControlPrivilegeAdminister // 5
	default:
		return 1 // View — standard default
	}
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege].
// ACL (0x0000) and Extension (0x0001) require Administer (5) per Matter
// §9.10.5.3 (access "RW … A"). Mirrors matter.js
// packages/model/src/standard/elements/access-control.element.ts:28,32.
func (*AccessControl) MinWritePrivilege(attrID uint32) uint8 {
	switch attrID {
	case accessControlAttrACL, accessControlAttrExtension:
		return accessControlPrivilegeAdminister // 5
	default:
		return 3 // Operate — standard default
	}
}

// SetCurrentFabric is called by the IM dispatcher before fabric-scoped
// reads so the cluster filters the ACL list to the requesting fabric.
func (a *AccessControl) SetCurrentFabric(idx uint8) {
	a.mu.Lock()
	a.currentFabric = idx
	a.mu.Unlock()
}

// RemoveFabricExtension purges fabricIndex's entry from the in-memory
// Extension map (attribute 0x0001) and bumps DataVersion when an entry
// was actually present. Every fabric-scoped attribute must be purged
// when its fabric is removed (Matter §9.10.5, mirroring how
// OperationalCredentials.handleRemoveFabric clears its own fabric-scoped
// in-memory state — see that method's FabricManager.ts:241-248
// #handleFabricDeleted citation) — the ACL list is store-backed and
// already cleared by the store's FK CASCADE, but Extension is an
// in-memory map keyed by fabric index this cluster owns exclusively, so
// nothing else purges it. Without this, a fabric index reused by a later
// commissioning inherits the removed controller's Extension entry: fabric
// indices are reused (AddNOC allocates from the store's free indices),
// so the next controller assigned index N would read back index N's
// stale metadata as if it had written it itself.
//
// Called from the daemon's matterFabricTeardown, the same composition-
// root fan-out that closes operational sessions and subscriptions for
// the removed fabric.
func (a *AccessControl) RemoveFabricExtension(fabricIndex uint8) {
	a.mu.Lock()
	_, had := a.extensions[fabricIndex]
	if had {
		delete(a.extensions, fabricIndex)
	}
	a.mu.Unlock()
	if had {
		_ = a.persistExtension(context.Background(), fabricIndex, nil)
		a.dataVersion.Bump()
	}
}

// MatterRead implements [contract.ClusterServer].
func (a *AccessControl) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case accessControlAttrACL:
		ctx := context.Background()
		a.mu.RLock()
		fabric := a.currentFabric
		a.mu.RUnlock()
		entries, err := a.store.ListACL(ctx, fabric)
		if err != nil {
			return nil, false
		}
		out := make([]AccessControlEntryStruct, 0, len(entries))
		for _, e := range entries {
			ace := AccessControlEntryStruct{
				Privilege:   uint8(e.Privilege),
				AuthMode:    uint8(e.AuthMode),
				Subjects:    append([]uint64(nil), e.Subjects...),
				FabricIndex: e.FabricIndex,
			}
			if len(e.Targets) > 0 {
				ace.Targets = make([]ACLTargetStruct, 0, len(e.Targets))
				for _, t := range e.Targets {
					ace.Targets = append(ace.Targets, ACLTargetStruct{
						Cluster:    t.Cluster,
						Endpoint:   t.Endpoint,
						DeviceType: t.DeviceType,
					})
				}
			}
			out = append(out, ace)
		}
		return out, true
	case accessControlAttrExtension:
		a.mu.RLock()
		exts := a.extensions[a.currentFabric]
		a.mu.RUnlock()
		if len(exts) == 0 {
			return []AccessControlExtensionEntry{}, true
		}
		out := make([]AccessControlExtensionEntry, len(exts))
		copy(out, exts)
		return out, true
	case accessControlAttrSubjectsPerAccessControl:
		return accessControlSubjectsPerEntry, true
	case accessControlAttrTargetsPerAccessControl:
		return accessControlTargetsPerEntry, true
	case accessControlAttrAccessControlEntriesPerFabric:
		return accessControlEntriesPerFabricLimit, true
	case cluster.AttrGlobalFeatureMap:
		// FeatureMap = EXTS (Extension, bit 0). chip's MTRBaseClusters.h
		// declares MTRAccessControlFeatureExtension = 0x1 (iOS 18.4)
		// and matter.js's `AccessControlServer.with("Extension")` sets
		// the feature flag whenever the Extension list attribute is
		// served. We serve Extension (`accessControlAttrExtension`
		// returns an empty list above) → advertise the feature.
		// FeatureMap = 0 made Apple's HAP-mapper classify the cluster
		// as schematically inconsistent (Extension list present but
		// not feature-flagged) and drop the entire AccessControl
		// schema validation, so the cluster advertises EXTS.
		return a.featureMap(), true
	case cluster.AttrGlobalClusterRevision:
		return accessControlClusterRevision, true
	case accessControlAttrAuxiliaryACL:
		// A read without an IM request behind it is a local one and
		// sees every entry whole.
		return a.auxiliaryACLRead(context.Background(), false, 0, true)
	}
	return nil, false
}

// MatterReadFiltered implements [contract.FabricScopedReader].
// AccessControl.ACL is a fabric-scoped attribute per Matter §9.10.5.3
// — every entry carries a FabricIndex and the wire MUST return only
// entries for the requesting fabric when FabricFiltered=true. Apple
// Home reads ACL on the CASE session immediately after Subscribe-
// Initial; without this filter the read falls back to MatterRead's
// `a.currentFabric` (only ever set by ACL writes — zero on a fresh
// CASE session), ListACL returns `[]`, and Apple interprets the empty
// list as "this subject has no Administer privilege" and tears the
// fabric down via RemoveFabric.
//
// A non-fabric-filtered read returns every fabric's entries, another
// fabric's redacted to its FabricIndex (see unfilteredFabrics).
//
// Mirrors matter.js packages/node/src/behaviors/access-control/
// AccessControlServer.ts: every read of `acl` and `extension` consults
// the FabricFilter from the IM context. The non-fabric-scoped attributes
// (SubjectsPerAccessControlEntry, TargetsPerAccessControlEntry,
// AccessControlEntriesPerFabric, FeatureMap, ClusterRevision) fall
// through to MatterRead.
func (a *AccessControl) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	if attrID == accessControlAttrAuxiliaryACL {
		filtered, fabricIndex := im.FabricFilterFromContext(ctx)
		return a.auxiliaryACLRead(ctx, filtered, fabricIndex, false)
	}
	if attrID != accessControlAttrACL && attrID != accessControlAttrExtension {
		return a.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read; it takes no ctx by the Matter cluster-server contract
	}
	filtered, fabricIndex := im.FabricFilterFromContext(ctx)
	if fabricIndex == 0 {
		// PASE (pre-AddNOC) or no FabricFilter set: fall through to
		// MatterRead which uses a.currentFabric (the last write target).
		return a.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read; it takes no ctx by the Matter cluster-server contract
	}
	fabrics := []uint8{fabricIndex}
	if !filtered {
		var err error
		if fabrics, err = a.unfilteredFabrics(ctx, fabricIndex); err != nil {
			return nil, false
		}
	}
	if attrID == accessControlAttrExtension {
		out := []AccessControlExtensionEntry{}
		a.mu.RLock()
		for _, fabric := range fabrics {
			for _, e := range a.extensions[fabric] {
				e.Redacted = fabric != fabricIndex
				if e.Redacted {
					e.Data = nil
				}
				out = append(out, e)
			}
		}
		a.mu.RUnlock()
		return out, true
	}
	out := []AccessControlEntryStruct{}
	for _, fabric := range fabrics {
		entries, err := a.store.ListACL(ctx, fabric)
		if err != nil {
			return nil, false
		}
		for _, e := range entries {
			if fabric != fabricIndex {
				out = append(out, AccessControlEntryStruct{FabricIndex: e.FabricIndex, Redacted: true})
				continue
			}
			out = append(out, aclEntryStruct(e))
		}
	}
	return out, true
}

// unfilteredFabrics lists the fabrics a non-fabric-filtered read of Acl or
// Extension covers: every fabric the store knows, in index order, the
// accessing one included. Acl and Extension are fabric-scoped ("F") but
// not fabric-sensitive lists, so matter.js ListManager createProxy filters
// them only for `session.fabricFiltered` — an unfiltered read sees every
// fabric's entries — while StructManager hides each entry's
// fabric-sensitive fields ("S": Privilege, AuthMode, Subjects, Targets,
// AuxiliaryType; Data) from a session whose fabric does not own the entry
// (protocol/src/action/server/AccessControl.ts mayRead), and
// InteractionMessenger encodes the entry without them. A store that
// cannot enumerate fabrics leaves the accessing one, as before.
func (a *AccessControl) unfilteredFabrics(ctx context.Context, accessing uint8) ([]uint8, error) {
	lister, ok := a.store.(fabricLister)
	if !ok {
		return []uint8{accessing}, nil
	}
	recs, err := lister.ListFabrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("matter: AccessControl: list fabrics: %w", err)
	}
	out := make([]uint8, 0, len(recs)+1)
	for _, r := range recs {
		out = append(out, r.FabricIndex)
	}
	a.mu.RLock()
	for fabric := range a.extensions {
		out = append(out, fabric)
	}
	a.mu.RUnlock()
	out = append(out, accessing)
	slices.Sort(out)
	return slices.Compact(out), nil
}

// MatterWrite handles writes to the cluster's writable attributes.
// The only writable attribute today is ACL (0x0000) — Apple Home
// rewrites the entire list ~10 ms after CommissioningComplete to
// install HomePod / AppleTV edge controllers as additional Administer
// subjects on the freshly-paired bridge. Without a working write path
// Apple times out after 10 s and tears the fabric down via
// RemoveFabric. Extension (0x0001) is not implemented — matter.js does
// the same and Apple does not write it.
func (a *AccessControl) MatterWrite(ctx context.Context, attrID uint32, value any) error { //nolint:gocognit,gocyclo,funlen // wire/dispatch table over many attribute/opcode cases
	if attrID == accessControlAttrACL {
		entries, ok := value.([]AccessControlEntryStruct)
		if !ok {
			return fmt.Errorf("matter: AccessControl.ACL write: value type %T not []AccessControlEntryStruct", value)
		}
		// An auxiliary entry is synthesised, never written. Mirrors
		// matter.js AccessControlServer #validateAccessControlListChanges:
		// "The spec forbids the field here without naming a status; CHIP
		// answers FAILURE".
		for i, e := range entries {
			if e.AuxiliaryType != nil {
				return aclStatusError{im.StatusFailure, fmt.Sprintf("ACL[%d] must not include AuxiliaryType", i)}
			}
		}
		// Fabric resolution priority (matches MatterReadFiltered):
		//   1. ctx-fabric stamped by bridge/receive.go from the inbound
		//      CASE session — the spec-correct source for every
		//      fabric-scoped write per Matter §9.10.5.3.
		//   2. a.currentFabric set via SetCurrentFabric — legacy hook
		//      retained for tests that pre-date the ctx plumbing.
		//   3. entries[0].FabricIndex when the caller stamps the entry
		//      themselves (rare; mostly clients-as-server in tests).
		//   4. Hard-coded fabric=1 last resort.
		// Without (1) Apple Home's post-CommissioningComplete ACL
		// rewrite lands in the wrong fabric, Apple reads its own fabric
		// on the next Subscribe-Initial, sees the unchanged
		// case_admin_subject ACL, and tears the pair down with the iOS
		// "accessory could not be added" dialog.
		_, ctxFabric := im.FabricFilterFromContext(ctx)
		a.mu.RLock()
		fabric := a.currentFabric
		a.mu.RUnlock()
		if ctxFabric != 0 {
			fabric = ctxFabric
		}
		if fabric == 0 && len(entries) > 0 && entries[0].FabricIndex != 0 {
			fabric = entries[0].FabricIndex
		}
		if fabric == 0 {
			fabric = 1 // last-resort: only ever 1 fabric in v1.1.
		}
		// Validation — Mirrors chip src/access/AccessControl.cpp:680-764
		// Entry::IsValid() and matter.js packages/node/src/behaviors/
		// access-control/AccessControlServer.ts:165-265. Rules:
		//   1. AccessControlEntriesPerFabric (≤ 4 entries on this fabric).
		//   2. SubjectsPerAccessControlEntry (≤ 4 subjects per entry).
		//   3. TargetsPerAccessControlEntry (≤ 4 targets per entry).
		//   4. AuthMode != PASE on every entry — PASE auth is only valid
		//      during commissioning, never in the persisted ACL.
		//   5. Group-AuthMode entries must NOT carry Administer privilege.
		//   6. CASE-AuthMode: each non-zero subject must be a valid CASE
		//      NodeID (operational range) or CASE Auth Tag.
		//      Mirrors chip AccessControl.cpp:735 IsValidCaseNodeId check.
		//   7. Group-AuthMode: each subject must be a valid Group NodeID
		//      (0xFFFF_FFFF_FFFF_FF00 .. 0xFFFF_FFFF_FFFF_FFFF range).
		//      Mirrors chip AccessControl.cpp:735 IsValidGroupNodeId check.
		//   8. Target ClusterId must be ≤ 0xFFFF_FFFF (Matter §7.18.2.4).
		//      Mirrors chip AccessControl.cpp:746 IsValidClusterId.
		//   9. Target EndpointId must be ≤ 0xFFFE (0xFFFF is wildcard/invalid).
		//      Mirrors chip AccessControl.cpp:747 IsValidEndpointId.
		//  10. Target DeviceTypeId must be ≤ 0xFFFF_FFFF.
		//      Mirrors chip AccessControl.cpp:748 IsValidDeviceTypeId.
		//  11. DeviceType and Endpoint on the same Target are mutually exclusive.
		//  12. At least one of (Cluster, Endpoint, DeviceType) must be set on each Target.
		// Limit failures → ResourceExhausted; semantic failures →
		// ConstraintError (both via the dispatcher's `writeErrorStatus`
		// substring matching).
		// Every entry in the list is persisted under the writer's fabric
		// (the stamp below), so every entry counts against the writer's
		// per-fabric limit — the FabricIndex a client puts on the wire is
		// raw input, not a scope. matter.js reaches the same count through
		// its fabric-scoped write machinery, which has already stamped the
		// accessing fabric before AccessControlServer.ts:186-189 filters on
		// it; counting the client's own value here instead let a list of
		// entries tagged with a foreign index pass the limit and then be
		// stored, all of them, on the writer's fabric.
		if fabricACLs := len(entries); fabricACLs > int(accessControlEntriesPerFabricLimit) {
			return fmt.Errorf("matter: AccessControl.ACL write: resource exhausted: AccessControlEntriesPerFabric=%d > limit=%d", fabricACLs, accessControlEntriesPerFabricLimit)
		}
		for i, e := range entries {
			if len(e.Subjects) > int(accessControlSubjectsPerEntry) {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: resource exhausted: SubjectsPerAccessControlEntry=%d > limit=%d", i, len(e.Subjects), accessControlSubjectsPerEntry)
			}
			if len(e.Targets) > int(accessControlTargetsPerEntry) {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: resource exhausted: TargetsPerAccessControlEntry=%d > limit=%d", i, len(e.Targets), accessControlTargetsPerEntry)
			}
			// Enum validity: reject out-of-range Privilege / AuthMode values
			// before the semantic checks below. matter.js enforces these via
			// schema supervision (AccessControlEntryPrivilegeEnum 1..5,
			// AuthModeEnum 1..3); an unchecked write would persist e.g.
			// Privilege=7 / AuthMode=9. Mirrors chip AccessControl.cpp
			// Entry::IsValid() privilege/authMode guards.
			if e.Privilege < accessControlPrivilegeView || e.Privilege > accessControlPrivilegeAdminister {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: constraint error: Privilege=%d not in 1..5 (View..Administer)", i, e.Privilege)
			}
			if e.AuthMode < accessControlAuthModePASE || e.AuthMode > accessControlAuthModeGroup {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: constraint error: AuthMode=%d not in 1..3 (PASE/CASE/Group)", i, e.AuthMode)
			}
			if e.AuthMode == accessControlAuthModePASE {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: constraint error: AuthMode=PASE is forbidden in ACL", i)
			}
			if e.AuthMode == accessControlAuthModeGroup && e.Privilege == accessControlPrivilegeAdminister {
				return fmt.Errorf("matter: AccessControl.ACL[%d] write: constraint error: Group authmode + Administer privilege rejected", i)
			}
			// Subject range validation per AuthMode.
			// Mirrors chip src/access/AccessControl.cpp:735 IsValidCaseNodeId /
			// IsValidGroupNodeId per-subject checks inside Entry::IsValid().
			for j, subj := range e.Subjects {
				if subj == 0 {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Subjects[%d] write: constraint error: subject 0 is the undefined node ID", i, j)
				}
				if e.AuthMode == accessControlAuthModeCASE {
					// CASE subjects: operational node ID or CASE Auth Tag (CAT).
					if !aclIsValidCASESubject(subj) {
						return fmt.Errorf("matter: AccessControl.ACL[%d].Subjects[%d] write: constraint error: CASE subject 0x%016X is not a valid operational node ID or CASE auth tag", i, j, subj)
					}
				}
				if e.AuthMode == accessControlAuthModeGroup {
					// Group subjects: a Group ID, 0x0001 .. 0xFFFF.
					if !aclIsValidGroupSubject(subj) {
						return fmt.Errorf("matter: AccessControl.ACL[%d].Subjects[%d] write: constraint error: Group subject 0x%016X is not a group id", i, j, subj)
					}
				}
			}
			// Target validation: matter.js requires DeviceType and
			// Endpoint mutually exclusive, and at least one of
			// (Cluster, Endpoint, DeviceType) must be present.
			// Additionally validate cluster/endpoint/device-type ID ranges.
			for j, t := range e.Targets {
				if t.DeviceType != nil && t.Endpoint != nil {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Targets[%d] write: constraint error: DeviceType and Endpoint mutually exclusive", i, j)
				}
				if t.Cluster == nil && t.Endpoint == nil && t.DeviceType == nil {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Targets[%d] write: constraint error: at least one of Cluster/Endpoint/DeviceType must be set", i, j)
				}
				// Cluster / Endpoint / DeviceType ID validity, in the same
				// order matter.js checks them. Mirrors matter.js
				// packages/node/src/behaviors/access-control/AccessControlServer.ts:266-278
				// (ClusterId.isValid / EndpointNumber.isValid / DeviceTypeId.isValid,
				// each throwing ConstraintError). Cross-check chip
				// AccessControl.cpp:746-748 IsValidClusterId / IsValidEndpointId /
				// IsValidDeviceTypeId guards in Entry::IsValid().
				if t.Cluster != nil && !aclIsValidClusterID(*t.Cluster) {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Targets[%d] write: constraint error: Cluster 0x%08X is not a valid ClusterId", i, j, *t.Cluster)
				}
				// EndpointNumber.isValid rejects 0xFFFF (the reserved wildcard);
				// a uint16 endpoint cannot exceed that, so 0xFFFF is the only
				// invalid value.
				if t.Endpoint != nil && *t.Endpoint == 0xFFFF {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Targets[%d] write: constraint error: EndpointId 0xFFFF is reserved", i, j)
				}
				if t.DeviceType != nil && !aclIsValidDeviceTypeID(*t.DeviceType) {
					return fmt.Errorf("matter: AccessControl.ACL[%d].Targets[%d] write: constraint error: DeviceType 0x%08X is not a valid DeviceTypeId", i, j, *t.DeviceType)
				}
			}
		}

		// Per spec §9.10.5 every ACL entry is fabric-scoped — we MUST
		// stamp the caller's fabric on every entry before persisting.
		out := make([]store.ACLEntry, 0, len(entries))
		for i, e := range entries {
			rec := store.ACLEntry{
				FabricIndex: fabric,
				Privilege:   store.Privilege(e.Privilege),
				AuthMode:    store.AuthMode(e.AuthMode),
				Subjects:    append([]uint64(nil), e.Subjects...),
				Position:    uint16(i), //nolint:gosec // i bounded by accessControlEntriesPerFabricLimit; see #20
			}
			if len(e.Targets) > 0 {
				rec.Targets = make([]store.ACLTarget, 0, len(e.Targets))
				for _, t := range e.Targets {
					rec.Targets = append(rec.Targets, store.ACLTarget{
						Cluster:    t.Cluster,
						Endpoint:   t.Endpoint,
						DeviceType: t.DeviceType,
					})
				}
			}
			out = append(out, rec)
		}
		// Snapshot old ACL before the replace so we can classify the
		// change type for the spec-mandated AccessControlEntryChanged event
		// (§9.10.7.1). We read before write; a store error here is
		// non-fatal for the write itself — we fall back to ChangeType=Changed
		// if the snapshot fails.
		oldEntries, _ := a.store.ListACL(ctx, fabric)

		if err := a.store.ReplaceACL(ctx, fabric, out); err != nil {
			return fmt.Errorf("matter: AccessControl.ACL write: %w", err)
		}
		// Bump DataVersion after a successful mutation so DataVersionFilter
		// evaluation correctly detects the cluster changed. Must happen
		// AFTER the store write succeeds per DataVersionTracker contract.
		a.dataVersion.Bump()

		// Emit AccessControlEntryChanged per Matter §9.10.7.1, one event per
		// entry of the writing fabric, the way matter.js
		// AccessControlServer.ts #handleAccessControlListChange does: each
		// position of the new list is Added (no old entry there) or Changed,
		// carrying the new entry; old entries past the new list's end are
		// Removed, last first, carrying the old entry. AdminNodeID /
		// AdminPasscodeID name the actor (#adminDataFromSession).
		// TC-ACL-2.5 / 2.6 / 2.9 read the events back.
		a.mu.RLock()
		emitter := a.emitter
		endpoint := a.endpoint
		a.mu.RUnlock()
		if emitter != nil {
			nodeID, passcodeID := aclAdminFromContext(ctx)
			emit := func(changeType uint8, latest store.ACLEntry) {
				v := aclEntryStruct(latest)
				emitter.MatterEmitEvent(endpoint, accessControlClusterID, accessControlEventEntryChanged,
					AccessControlEntryChangedEvent{
						AdminNodeID:     nodeID,
						AdminPasscodeID: passcodeID,
						ChangeType:      changeType,
						LatestValue:     &v,
						FabricIndex:     fabric,
					}, contract.EventPriorityInfo)
			}
			i := 0
			for ; i < len(out); i++ {
				changeType := AccessControlChangeTypeChanged
				if i >= len(oldEntries) {
					changeType = AccessControlChangeTypeAdded
				}
				emit(changeType, out[i])
			}
			for j := len(oldEntries) - 1; j >= i; j-- {
				emit(AccessControlChangeTypeRemoved, oldEntries[j])
			}
		}
		return nil
	}
	if attrID == accessControlAttrExtension {
		// Extension (0x0001) write path: replace the per-fabric extension
		// list. The list carries vendor-opaque octstrings (max 128 bytes
		// each) that some controllers attach to ACL fabrics.
		// Mirrors matter.js AccessControlServer.ts in-memory extension state.
		entries, ok := value.([]AccessControlExtensionEntry)
		if !ok {
			return fmt.Errorf("matter: AccessControl.Extension write: value type %T not []AccessControlExtensionEntry", value)
		}
		// Every entry this write stores is re-stamped to the writer's own
		// fabric below (v1.1 has no cross-fabric write path to begin
		// with), so more than one entry in a single write always means
		// more than one entry for that fabric. Mirrors matter.js
		// AccessControlServer.ts:352-370
		// (#validateAccessControlExtensionChanges): a fabric may hold at
		// most one AccessControlExtensionStruct.
		if len(entries) > 1 {
			return fmt.Errorf("matter: AccessControl.Extension write: constraint error: a fabric may hold at most one entry, got %d", len(entries))
		}
		for i, e := range entries {
			if len(e.Data) > 128 {
				return fmt.Errorf("matter: AccessControl.Extension[%d] write: constraint error: Data length %d exceeds max 128", i, len(e.Data))
			}
			// Data must itself decode as a well-formed TLV List — mirrors
			// matter.js AccessControlServer.ts:424-441
			// (extensionEntryValidator's default implementation). A blob
			// that fails this is rejected rather than stored, so a
			// garbage write cannot wedge a later fabric-scoped read.
			if err := validateAccessControlExtensionData(e.Data); err != nil {
				return fmt.Errorf("matter: AccessControl.Extension[%d] write: constraint error: %w", i, err)
			}
		}
		_, ctxFabric := im.FabricFilterFromContext(ctx)
		a.mu.RLock()
		fabric := a.currentFabric
		a.mu.RUnlock()
		if ctxFabric != 0 {
			fabric = ctxFabric
		}
		if fabric == 0 {
			fabric = 1
		}
		stamped := make([]AccessControlExtensionEntry, len(entries))
		for i, e := range entries {
			stamped[i] = AccessControlExtensionEntry{
				Data:        append([]byte(nil), e.Data...),
				FabricIndex: fabric,
			}
		}
		if err := a.persistExtension(ctx, fabric, stamped); err != nil {
			return fmt.Errorf("matter: AccessControl.Extension write: persist: %w", err)
		}
		a.mu.Lock()
		if a.extensions == nil {
			a.extensions = make(map[uint8][]AccessControlExtensionEntry)
		}
		oldExtensions := a.extensions[fabric]
		a.extensions[fabric] = stamped
		emitter := a.emitter
		endpoint := a.endpoint
		a.mu.Unlock()
		a.dataVersion.Bump()

		// Emit AccessControlExtensionChanged per Matter §9.10.7.2, the
		// same way the ACL write emits AccessControlEntryChanged above:
		// one event per write, ChangeType derived from the per-fabric
		// list-length delta, LatestValue=nil for a bulk-replace (spec
		// quality X — permitted to omit) unless exactly one entry is
		// involved on either side of the change.
		if emitter != nil {
			changeType := AccessControlChangeTypeChanged
			switch {
			case len(stamped) > len(oldExtensions):
				changeType = AccessControlChangeTypeAdded
			case len(stamped) < len(oldExtensions):
				changeType = AccessControlChangeTypeRemoved
			}
			extNodeID, extPasscodeID := aclAdminFromContext(ctx)
			var latest *AccessControlExtensionEntry
			switch {
			case changeType == AccessControlChangeTypeRemoved && len(oldExtensions) > 0:
				v := oldExtensions[0]
				latest = &v
			case changeType != AccessControlChangeTypeRemoved && len(stamped) > 0:
				v := stamped[0]
				latest = &v
			}
			emitter.MatterEmitEvent(
				endpoint,
				accessControlClusterID,
				accessControlEventExtensionChanged,
				AccessControlExtensionChangedEvent{
					AdminNodeID:     extNodeID,
					AdminPasscodeID: extPasscodeID,
					ChangeType:      changeType,
					LatestValue:     latest,
					FabricIndex:     fabric,
				},
				contract.EventPriorityInfo,
			)
		}
		return nil
	}
	return fmt.Errorf("matter: AccessControl attribute 0x%04X not writable", attrID)
}

// MatterInvoke — no commands; AccessControl in 1.5.1 is attribute-only.
func (a *AccessControl) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("matter: AccessControl has no command 0x%X", cmdID)
}

// MatterReportable returns the attributes that emit reports on
// change. The ACL list and Extension list are reportable per
// §9.10.4 so subscribers see entries appearing immediately after
// AddNOC; v1.1 ships the static set.
func (a *AccessControl) MatterReportable() []uint32 {
	return []uint32{accessControlAttrACL, accessControlAttrExtension}
}

// MatterAttributes implements [contract.ClusterAttributeLister]
// so wildcard reads expand correctly. Returns the full attribute set
// EXCLUDING the universal globals (FeatureMap, ClusterRevision) —
// the dispatcher merges those automatically.
func (a *AccessControl) MatterAttributes() []uint32 {
	attrs := []uint32{
		accessControlAttrACL,
		accessControlAttrExtension,
		accessControlAttrSubjectsPerAccessControl,
		accessControlAttrTargetsPerAccessControl,
		accessControlAttrAccessControlEntriesPerFabric,
	}
	if a.auxiliaryEnabled() {
		attrs = append(attrs, accessControlAttrAuxiliaryACL)
	}
	return attrs
}

// MatterEvents implements [contract.ClusterEventLister] so the
// dispatcher synthesises the global EventList (0xFFFA) attribute
// correctly for this cluster. Includes AccessControlExtensionChanged
// (0x0001) per matter.js
// packages/model/src/standard/elements/access-control.element.ts:77-88
// and chip's AccessControl cluster server (spec §9.10.7). The event is
// listed here so EventList synthesis is complete if EventList suppression
// is lifted; no emission path is wired because Extensions are not
// implemented in v1.1.
func (a *AccessControl) MatterEvents() []uint32 {
	events := []uint32{accessControlEventEntryChanged, accessControlEventExtensionChanged}
	if a.auxiliaryEnabled() {
		events = append(events, accessControlEventAuxiliaryAccessUpd)
	}
	return events
}

// SetMatterEventEmitter implements [contract.EventReceiver].
// Called by the bridge during topology assembly so [MatterWrite] can
// fire the §9.10.7.1 AccessControlEntryChanged event without the
// cluster holding a direct reference to the bridge. Idempotent.
func (a *AccessControl) SetMatterEventEmitter(emitter contract.EventEmitter) {
	a.mu.Lock()
	a.emitter = emitter
	a.mu.Unlock()
}

// SetEndpoint stamps the endpoint id this AccessControl server is
// mounted on. Matter events carry the (endpoint, cluster, event)
// triple so the commissioner can fan them out to the right
// subscription path. The root endpoint is always 0 in standard
// topologies, but the bridge injects the real value here so the
// cluster does not hard-code it.
func (a *AccessControl) SetEndpoint(endpoint uint16) {
	a.mu.Lock()
	a.endpoint = endpoint
	a.mu.Unlock()
}

// aclIsValidCASESubject reports whether id is valid as a CASE-AuthMode ACL subject.
// Valid: operational node ID (0x0001..0xFFFF_FFEF_FFFF_FFFF) or CASE Auth Tag
// (upper 32 bits == 0xFFFF_FFFD). The upper operational bound must be
// 0xFFFF_FFEF_FFFF_FFFF: anything above it is a reserved Node ID subrange
// (CAT 0xFFFF_FFFD.., Temporary-Local 0xFFFF_FFFE.., Group
// 0xFFFF_FFFF_FFFF_FF00..) and is not a plain operational node. A
// byte-transposed bound of 0xFFFF_FFFF_FFFF_FFEF would accept the whole
// reserved range as a CASE subject. Mirrors matter.js
// packages/types/src/datatype/NodeId.ts:27-28 (OPERATIONAL_NODE_MIN/MAX) and
// :57-59 (isOperationalNodeId). Cross-check chip
// src/lib/core/NodeId.h:59 kMaxOperationalNodeId = 0xFFFF'FFEF'FFFF'FFFF.
func aclIsValidCASESubject(id uint64) bool {
	// Operational node ID range.
	if id >= 0x0000_0000_0000_0001 && id <= 0xFFFF_FFEF_FFFF_FFFF {
		return true
	}
	// CASE Auth Tag: upper 32 bits == 0xFFFF_FFFD. The low 16 bits carry
	// the CAT version, which MUST NOT be 0 — a version-0 CAT is rejected
	// with ConstraintError. Mirrors matter.js
	// packages/node/src/behaviors/access-control/AccessControlServer.ts:211-220
	// (CaseAuthenticatedTag.getVersion(cat) === 0 → ConstraintError) and
	// packages/types/src/datatype/CaseAuthenticatedTag.ts:31-33
	// (getVersion = tag & 0xffff). Matter §6.6.2.1.2.
	return (id>>32) == 0xFFFF_FFFD && (id&0xFFFF) != 0
}

// aclIsValidClusterID reports whether id is a well-formed Matter ClusterId.
// Mirrors matter.js packages/types/src/datatype/ClusterId.ts:22-32 (the
// ClusterId constructor validation via Mei.fromMei): a standard cluster has a
// zero vendor prefix (upper 16 bits) and a type suffix (low 16 bits) in
// 0x0000..0x7FFF; a manufacturer-specific cluster has a non-zero vendor prefix
// (≤ 0xFFF4) and a type suffix in 0xFC00..0xFFFE. Everything else is invalid.
// Matter §7.10.
func aclIsValidClusterID(id uint32) bool {
	vendorPrefix := id >> 16
	typeSuffix := id & 0xFFFF
	// Mei.fromMei rejects a vendor prefix above 0xFFF4 or a type suffix of
	// 0xFFFF outright (ManufacturerExtensibleIdentifier.ts:23-38).
	if vendorPrefix > 0xFFF4 || typeSuffix > 0xFFFE {
		return false
	}
	if vendorPrefix == 0 && typeSuffix <= 0x7FFF {
		return true // standard cluster
	}
	return vendorPrefix != 0 && typeSuffix >= 0xFC00 && typeSuffix <= 0xFFFE
}

// aclIsValidDeviceTypeID reports whether id is a well-formed Matter
// DeviceTypeId. Mirrors matter.js packages/types/src/datatype/DeviceTypeId.ts:20-28
// (the DeviceTypeId constructor validation via Mei.fromMei): the vendor prefix
// (upper 16 bits) must be ≤ 0xFFF4 and the type suffix (low 16 bits) must be in
// 0x0000..0xBFFF. Matter §7.19.2.29.
func aclIsValidDeviceTypeID(id uint32) bool {
	vendorPrefix := id >> 16
	typeSuffix := id & 0xFFFF
	return vendorPrefix <= 0xFFF4 && typeSuffix <= 0xBFFF
}

// aclIsValidGroupSubject reports whether id is valid as a Group-AuthMode ACL
// subject: a Group ID, 0x0001..0xFFFF. The subject of a Group entry is the
// group id the message is addressed to (Matter §9.10.5.6), which is also
// what a group message's Incoming Subject Descriptor carries. Mirrors
// matter.js AccessControlServer.ts (#validateAccessControlListChanges:
// `GroupId(Number(subject)) === GroupId.NO_GROUP_ID` → ConstraintError,
// and GroupId() refuses a value beyond 0xFFFF) and
// FabricAccessControl #getIsdFromMessage (`isd.subjects.push(subject.id)`
// for a group subject).
func aclIsValidGroupSubject(id uint64) bool {
	return id >= 0x0001 && id <= 0xFFFF
}

// validateAccessControlExtensionData reports whether data decodes as a
// well-formed TLV List — a single top-level, untagged element whose
// type byte is TlvType.List (0x17) and whose final byte is the
// EndOfContainer marker (0x18), with everything in between parsing as
// balanced TLV. Mirrors matter.js
// packages/node/src/behaviors/access-control/AccessControlServer.ts:424-441
// (extensionEntryValidator, the default implementation): a controller
// may attach arbitrary vendor metadata to a fabric's Extension entry,
// but the octet-string must itself be decodable TLV, or a later
// fabric-scoped read of it can wedge a strict controller's decoder.
func validateAccessControlExtensionData(data []byte) error {
	if len(data) < 2 || data[0] != byte(tlv.TypeList) || data[len(data)-1] != byte(tlv.TypeEndContainer) {
		return errors.New("extension must be a valid TLV")
	}
	if err := tlv.Validate(data); err != nil {
		return fmt.Errorf("extension must be a valid TLV: %w", err)
	}
	// The list decodes as a tagged list (TlvTaggedList(…, true)): each of
	// its members carries a context or profile tag, never an anonymous
	// one ("Structure element tags should have an id", TlvObject.ts). The
	// test plan's D_BAD_ELEM holds an anonymous octet string (TC-ACL-2.3).
	dec := tlv.NewDecoder(data)
	depth := 0
	for {
		el, err := dec.Next()
		if err != nil {
			break
		}
		if el.Type == tlv.TypeEndContainer {
			depth--
			continue
		}
		if depth == 1 && el.Tag.Kind == tlv.TagKindAnonymous {
			return errors.New("extension must be a valid TLV: list member without a tag")
		}
		if el.Type == tlv.TypeStructure || el.Type == tlv.TypeArray || el.Type == tlv.TypeList {
			depth++
		}
	}
	return nil
}

// aclAdminFromContext names the actor of an ACL or Extension change for the
// change events: a CASE session's subject node id, or passcode id 0 for a
// PASE session (or a change without a session). Mirrors matter.js
// AccessControlServer.ts #adminDataFromSession.
func aclAdminFromContext(ctx context.Context) (*uint64, *uint16) {
	node, _ := im.SubjectFromContext(ctx)
	if im.IsPASEFromContext(ctx) || node == 0 {
		zero := uint16(0)
		return nil, &zero
	}
	return &node, nil
}

// NotifyAdminEntryInstalled reports the default Administer entry AddNOC
// installed for a new fabric: the ACL changed, so the DataVersion moves and
// an AccessControlEntryChanged event (Added, AdminPasscodeID 0 — AddNOC
// always runs over PASE) is emitted. Mirrors matter.js
// AccessControlServer.ts, which emits the event itself for the entry it
// adds on fabric creation. TC-ACL-2.5/2.6/2.9 read it back.
func (a *AccessControl) NotifyAdminEntryInstalled(entry store.ACLEntry) {
	a.dataVersion.Bump()
	a.mu.RLock()
	emitter := a.emitter
	endpoint := a.endpoint
	a.mu.RUnlock()
	if emitter == nil {
		return
	}
	v := aclEntryStruct(entry)
	zero := uint16(0)
	emitter.MatterEmitEvent(endpoint, accessControlClusterID, accessControlEventEntryChanged,
		AccessControlEntryChangedEvent{
			AdminPasscodeID: &zero,
			ChangeType:      AccessControlChangeTypeAdded,
			LatestValue:     &v,
			FabricIndex:     entry.FabricIndex,
		}, contract.EventPriorityInfo)
}
