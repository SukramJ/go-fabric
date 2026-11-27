// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
)

// AccessControl implements the Matter Access Control Cluster
// (0x001F) per Matter Core Specification 1.5.1 §9.10. Mandatory on
// the Root endpoint; the cluster's `acl` attribute is the controller's
// authoritative source for what each subject may read / write / invoke
// on every cluster of every endpoint.
//
// AddNOC installs a fabric's first entry, the default Administer entry of
// §11.18.6.8.1, as soon as the fabric is persisted; an administrator
// writes the Acl and Extension lists of its own fabric afterwards
// ([AccessControl.MatterWrite]), and both are persisted. Apple Home reads
// Acl right after CASE; an empty / missing cluster surfaces as
// ACCESS_DENIED on every follow-up read and Apple's pairing UI tears the
// new fabric down via RemoveFabric.
type AccessControl struct {
	store ACLStoreFacade
	// log is the logger [AccessControl.SetLogger] stored; nil is slog.Default().
	log atomic.Pointer[slog.Logger]

	// writeMu serialises a controller's writes of Acl and Extension: one
	// write's read of the held list, its comparison and its store are one
	// step, so a write that answers unchanged has stored nothing and a
	// concurrent write's change is never undone under it (matter.js holds
	// the datasource across a write's change and the commit's comparison,
	// packages/node/src/behavior/state/managed/Datasource.ts:1345-1350 and
	// :1446-1457 at db021da0). It is taken before mu, never under it, and
	// the store is called under it. A write emits its events while it
	// holds writeMu, and an emitter's hook may run synchronously on the
	// writer's goroutine, so no read path takes it: a read of Acl orders
	// its mark against a drop by aclDropEpoch instead (decision 681).
	writeMu sync.Mutex
	// mu guards the fields below: the request's fabric and the cached
	// ACL state the handlers read, the held marks and the drop epochs;
	// the store is not called under it.
	mu            sync.RWMutex
	currentFabric uint8

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped after every successful ACL replace so subscribers
	// can detect changes. Satisfies [contract.ClusterDataVersion].
	dataVersion cluster.DataVersionTracker

	// extensions holds the per-fabric AccessControlExtensionStruct list
	// (attribute 0x0001, conformance EXTS). The list is keyed by fabric
	// index, loaded from the store at construction and written through to
	// it (loadExtensions, the Extension write); the entries are
	// vendor-opaque octstrings (max 128 bytes each) that controllers use
	// to attach metadata to an ACL fabric.
	extensions map[uint8][]AccessControlExtensionEntry
	// extLoadErr is the error of each fabric whose stored Extension could
	// not be read (loadExtension); that fabric's Extension write reads it
	// again and is refused while it still fails.
	extLoadErr map[uint8]error
	// extOwedClear names each fabric whose Extension memory dropped but
	// whose stored setting a failed clear left behind (MatterDropFabric):
	// the clear is retried by the fabric's next drop, a new fabric's
	// default admin entry at the index and that fabric's Extension write,
	// so a fabric on the reused index does not load it as its own at the
	// next start (review II, R2-C2-store-1004).
	extOwedClear map[uint8]bool
	// aclHeld names each fabric whose Acl rows the server last saw
	// non-empty in the store: at construction, at a read or a write of the
	// fabric's list, at a new fabric's default admin entry and at an entry
	// the node appended (NotifyLocalEntryInstalledOn). The store's
	// fabric cascade removes a fabric's rows before the drop runs, so the
	// drop asks the store only for a fabric named here, and an empty answer
	// is the Acl shrinking (review II, R4-par-4051).
	aclHeld map[uint8]bool
	// aclDropEpoch counts each fabric's drops and cleared marks; dropFabric
	// and markACLHeld(false) advance it in the hold of mu that reads or
	// clears the fabric's mark. A read of Acl notes it
	// before it lists the fabric's rows and marks the fabric held only if
	// it did not move: a read that listed rows the fabric's removal took
	// does not mark the fabric again after its drop cleared the mark
	// (review II, R5-conc-5004; decision 681). An auxiliary
	// synchronisation notes it the same way for the owed contexts it takes,
	// and a failed one puts back only those of fabrics whose count did not
	// move (#619). Entries are never deleted, so an index's count only
	// grows.
	aclDropEpoch map[uint8]uint64
	// changes reports what moves outside a write of this cluster: a
	// fabric drop's removal of Acl rows and of an Extension entry
	// ([AccessControl.OnAttributeChange]).
	changes cluster.AttributeChanges

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
	// auxDeferred is the context of each source change a batch deferred,
	// per fabric: the event of that fabric names the node behind its own
	// change, never the node of the command that closes the batch (review
	// II, R2-P6-conc-1007).
	auxDeferred map[uint8]context.Context
	// auxBatchEpochs is the drop epoch (aclDropEpoch) of each fabric whose
	// request opened a batch, noted at its begin and handed to the
	// synchronisation that closes the batches, so a fabric dropped while a
	// batch is open gets no context back from that synchronisation's
	// failure (#619).
	auxBatchEpochs map[uint8]uint64
	// auxOwed records a synchronisation a failed read of the source owes:
	// the next read of the cluster or the next change runs it
	// (settleOwedAuxiliary). auxOwedEmit is whether it fires events, and
	// auxOwedCtx the context of each failed change, per fabric, so the
	// event names the node behind that change (review II, R4-err-4055).
	auxOwed     bool
	auxOwedEmit bool
	auxOwedCtx  map[uint8]context.Context
	// auxSyncMu makes one synchronisation's read of the source and its
	// apply one step, so two never apply in the other order than they read.
	// It is taken before mu, never under it.
	auxSyncMu sync.Mutex

	// limits is what [AccessControlConfig.Limits] or
	// [AccessControl.SetLimits] set; a zero field is the constant.
	limits AccessControlLimits
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
// The server returns every entry whole; the dispatcher withholds another
// fabric's fabric-sensitive fields from a request.
type AccessControlAuxiliaryEntryStruct struct {
	Entry         AccessControlEntryStruct
	AuxiliaryType uint8
	// Redacted, when set, makes the bridge's writer send the entry as its
	// FabricIndex alone.
	//
	// Deprecated: the dispatcher scopes every fabric-scoped attribute of
	// the model from the schema, whatever the server returns; no server of
	// this module sets it any more. Return the entry whole. Removable in
	// v0.25.0.
	Redacted bool
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
	// Redacted, when set, makes the bridge's writer send the entry
	// without Data.
	//
	// Deprecated: the dispatcher scopes every fabric-scoped attribute of
	// the model from the schema, whatever the server returns; no server of
	// this module sets it any more. Return the entry whole. Removable in
	// v0.25.0.
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
// AdminNodeID and AdminPasscodeID are both nullable (quality X): the
// writer's CASE subject or, over PASE, passcode id 0 — the other one null
// (aclAdminFromContext). LatestValue is nullable (quality X) and
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
// AdminNodeID and AdminPasscodeID are both nullable (quality X) and set
// as in [AccessControlEntryChangedEvent].
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
	// Redacted, when set, makes the bridge's writer send the entry as its
	// FabricIndex alone.
	//
	// Deprecated: the dispatcher scopes every fabric-scoped attribute of
	// the model from the schema, whatever the server returns; no server of
	// this module sets it any more. Return the entry whole. Removable in
	// v0.25.0.
	Redacted bool
}

// ACLTargetStruct mirrors §9.10.4.5.
type ACLTargetStruct struct {
	Cluster    *uint32 // null ⇒ any cluster
	Endpoint   *uint16 // null ⇒ any endpoint
	DeviceType *uint32 // null ⇒ any device type
}

// NewAccessControl constructs the cluster.
//
// Deprecated: use [NewAccessControlWith], which takes the store and the
// capacity limits together and refuses a limit outside its range at
// construction; this constructor keeps its behaviour until it goes and is
// removable in v0.29.0 (review II, R4-api-4157, decision 669).
func NewAccessControl(s ACLStoreFacade) (*AccessControl, error) {
	if s == nil {
		return nil, errors.New("core: AccessControl store is required")
	}
	a := &AccessControl{store: s}
	a.loadExtensions(context.Background())
	a.loadACLHeld(context.Background())
	return a, nil
}

// AccessControlConfig drives [NewAccessControlWith].
type AccessControlConfig struct {
	// Store is the ACL store; required.
	Store ACLStoreFacade
	// Limits are the capacity attributes and the limits an ACL write is
	// held to; a zero field is its default. A field outside its range
	// ([AccessControlLimits]) is refused at construction: the attributes
	// are fixed, and a node does not advertise a capacity its own
	// constraint forbids.
	Limits AccessControlLimits
}

// NewAccessControlWith returns the AccessControl server on cfg.Store with
// cfg.Limits: the stored Extension entries are loaded and the fabrics
// holding Acl rows marked, under ctx. A config without a store, or with a
// limit outside its range, is refused with an error naming it.
func NewAccessControlWith(ctx context.Context, cfg AccessControlConfig) (*AccessControl, error) {
	if cfg.Store == nil {
		return nil, errors.New("core: AccessControl needs a store")
	}
	if err := cfg.Limits.check(); err != nil {
		return nil, err
	}
	a := &AccessControl{store: cfg.Store, limits: cfg.Limits}
	a.loadExtensions(ctx)
	a.loadACLHeld(ctx)
	return a, nil
}

// loadACLHeld marks every fabric the store lists whose Acl rows are not
// empty (aclHeld). A store that cannot list fabrics, or a list that fails,
// marks nothing: such a fabric is marked at its next read or write.
func (a *AccessControl) loadACLHeld(ctx context.Context) {
	lister, ok := a.store.(fabricLister)
	if !ok {
		return
	}
	recs, err := lister.ListFabrics(ctx)
	if err != nil {
		return
	}
	for i := range recs {
		f := recs[i].FabricIndex
		if rows, err := a.store.ListACL(ctx, f); err == nil {
			a.markACLHeld(f, len(rows) > 0)
		}
	}
}

// markACLHeld records whether the store holds Acl rows of fabric. Clearing
// the mark advances the fabric's drop epoch as a drop does: a read that
// listed the rows before they went does not mark the fabric again behind
// the clear (R5-conc-5004, decision 681).
func (a *AccessControl) markACLHeld(fabric uint8, held bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !held {
		delete(a.aclHeld, fabric)
		if a.aclDropEpoch == nil {
			a.aclDropEpoch = map[uint8]uint64{}
		}
		a.aclDropEpoch[fabric]++
		return
	}
	if a.aclHeld == nil {
		a.aclHeld = map[uint8]bool{}
	}
	a.aclHeld[fabric] = true
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
// at most (the attribute's per-fabric constraint). The failures are logged
// once, with their count and the first: a store failing at construction
// fails every index, and the per-fabric record is extLoadErr (review II,
// R2-P7-acl-05).
func (a *AccessControl) loadExtensions(ctx context.Context) {
	p, ok := a.store.(ACLExtensionPersistence)
	if !ok {
		return
	}
	var (
		failed     int
		firstIndex int
		firstErr   error
	)
	for f := 1; f <= 254; f++ {
		if err := a.loadExtension(ctx, p, uint8(f)); err != nil {
			if failed == 0 {
				firstIndex, firstErr = f, err
			}
			failed++
		}
	}
	if failed > 0 {
		a.logger().Warn("matter.access_control.extension_load_failed",
			slog.Int("count", failed), slog.Int("fabric_index", firstIndex), slog.Any("err", firstErr))
	}
}

// loadExtension reads fabric's stored Extension into memory. A setting the
// store cannot read is recorded in extLoadErr and returned: the fabric's
// Extension write reads it again first, and is refused while it still
// cannot be read, so the stored value is never overwritten unseen (review
// II, R2-P6-err-1016). A read that succeeds clears the record.
func (a *AccessControl) loadExtension(ctx context.Context, p ACLExtensionPersistence, fabric uint8) error {
	v, found, err := p.GetSetting(ctx, aclExtensionSettingKey(fabric))
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		if a.extLoadErr == nil {
			a.extLoadErr = make(map[uint8]error)
		}
		a.extLoadErr[fabric] = err
		return err
	}
	delete(a.extLoadErr, fabric)
	if !found || v == "" {
		return nil
	}
	data, err := hex.DecodeString(v)
	if err != nil {
		return nil //nolint:nilerr // a value that does not decode is no Extension, as before
	}
	if a.extensions == nil {
		a.extensions = make(map[uint8][]AccessControlExtensionEntry)
	}
	a.extensions[fabric] = []AccessControlExtensionEntry{{Data: data, FabricIndex: fabric}}
	return nil
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
	_ contract.FabricScopedState              = (*AccessControl)(nil)
	_ contract.AttributeChangeNotifier        = (*AccessControl)(nil)
)

// OnAttributeChange implements [contract.AttributeChangeNotifier]: a
// fabric's removal ([AccessControl.MatterDropFabric]) that takes the
// fabric's Acl rows or its Extension entry out of the attributes advances
// the DataVersion once and tells the listeners of the attributes that
// shrank, after its locks are released; no AccessControlEntryChanged or
// AccessControlExtensionChanged event is emitted for it. A controller's
// write is reported by the bridge itself (review II, R4-par-4051).
func (a *AccessControl) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return a.changes.OnAttributeChange(cb)
}

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

// SetCurrentFabric sets the fabric a local write of Acl or Extension —
// [AccessControl.MatterWrite] with no Interaction Model request behind it
// — lands on (see writeFabric). It scopes no read: a read with no request
// behind it sees every fabric's entries whole, and a request's read is
// scoped by the request's own fabric. Nothing in this module calls it.
//
// Deprecated: a fabric-scoped write takes its fabric from the request
// ([im.WithFabricFilter]); a local write without one lands on the fabric
// its entries carry. There is no replacement. Removable in
// v0.25.0.
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
// The node calls it through the removal contract
// ([AccessControl.MatterDropFabric]) when a fabric is removed, and for
// every stored entry of a fabric the table no longer holds before it
// serves a topology.
func (a *AccessControl) RemoveFabricExtension(fabricIndex uint8) {
	_ = a.MatterDropFabric(context.Background(), fabricIndex)
}

// MatterDropFabric implements [contract.FabricScopedState]: the fabric's
// Extension entry leaves memory and its setting
// ([AccessControl.RemoveFabricExtension]); the error is the setting's.
// The ACL entries are the store's rows, which its fabric cascade removes.
// It holds writeMu, as a write of the Extension does: the entry's removal
// from memory and from its setting is one step against such a write, so
// none in flight leaves the removed fabric's entry behind in either.
//
// A drop that takes Acl rows (a fabric the server saw rows of, whose rows
// the store no longer holds) or the Extension entry out of the attributes
// advances the DataVersion once and reports the attributes that shrank
// ([AccessControl.OnAttributeChange]); a drop that removes nothing — the
// second drop of a fabric, the drop at a new fabric's index whose rows
// the store holds — moves and reports nothing (review II, R4-par-4051).
func (a *AccessControl) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	moved, err := a.dropFabric(ctx, fabricIndex)
	if len(moved) > 0 {
		a.changes.Notify(moved...)
	}
	return err
}

// dropFabric is MatterDropFabric under writeMu; moved names the attributes
// that shrank, reported once the lock is released.
func (a *AccessControl) dropFabric(ctx context.Context, fabricIndex uint8) (moved []uint32, err error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.mu.Lock()
	aclHad := a.aclHeld[fabricIndex]
	if a.aclDropEpoch == nil {
		a.aclDropEpoch = map[uint8]uint64{}
	}
	a.aclDropEpoch[fabricIndex]++
	// The context a failed auxiliary change of the fabric left owed goes
	// with the fabric: an event of a later fabric at the index must not
	// name its administrator (R4-err-4055). So does the context an open
	// batch recorded for it (#619).
	delete(a.auxOwedCtx, fabricIndex)
	delete(a.auxDeferred, fabricIndex)
	a.mu.Unlock()
	if aclHad {
		// The store is asked under writeMu, as a write asks it; a list that
		// fails keeps the mark, so a later drop asks again.
		if rows, lerr := a.store.ListACL(ctx, fabricIndex); lerr == nil && len(rows) == 0 {
			a.markACLHeld(fabricIndex, false)
			moved = append(moved, accessControlAttrACL)
		}
	}
	a.mu.Lock()
	_, had := a.extensions[fabricIndex]
	if had {
		delete(a.extensions, fabricIndex)
		moved = append(moved, accessControlAttrExtension)
	}
	// A setting that could not be read at construction is not in memory
	// but may still be stored: it is cleared as well, or the next
	// construction loads the dropped fabric's Extension again (review II,
	// R2-P7-acl-03). Only an entry memory held moves the DataVersion.
	loadErr := a.extLoadErr[fabricIndex]
	unread := loadErr != nil
	delete(a.extLoadErr, fabricIndex)
	owed := a.extOwedClear[fabricIndex]
	a.mu.Unlock()
	if len(moved) > 0 {
		a.dataVersion.Bump()
	}
	if !had && !unread && !owed {
		return moved, nil
	}
	if err := a.persistExtension(ctx, fabricIndex, nil); err != nil {
		a.mu.Lock()
		if unread {
			// The setting is still stored and still unread: keep the
			// record, so a later drop clears it again.
			if _, ok := a.extLoadErr[fabricIndex]; !ok {
				a.extLoadErr[fabricIndex] = loadErr
			}
		} else {
			// Memory no longer names the fabric, the setting still does:
			// the clear is owed (R2-C2-store-1004).
			a.oweClearLocked(fabricIndex)
		}
		a.mu.Unlock()
		return moved, fmt.Errorf("core: AccessControl: clear the Extension of fabric %d: %w", fabricIndex, err)
	}
	a.mu.Lock()
	delete(a.extOwedClear, fabricIndex)
	a.mu.Unlock()
	return moved, nil
}

// oweClearLocked records that fabric's stored Extension is owed a clear.
// Caller holds mu.
func (a *AccessControl) oweClearLocked(fabric uint8) {
	if a.extOwedClear == nil {
		a.extOwedClear = make(map[uint8]bool)
	}
	a.extOwedClear[fabric] = true
}

// settleOwedClear retries the owed clear of fabric's stored Extension, if
// one is owed (R2-C2-store-1004); the error is the clear's. Caller holds
// writeMu.
func (a *AccessControl) settleOwedClear(ctx context.Context, fabric uint8) error {
	a.mu.RLock()
	owed := a.extOwedClear[fabric]
	a.mu.RUnlock()
	if !owed {
		return nil
	}
	if err := a.persistExtension(ctx, fabric, nil); err != nil {
		return fmt.Errorf("core: AccessControl: the owed clear of fabric %d's Extension: %w", fabric, err)
	}
	a.mu.Lock()
	delete(a.extOwedClear, fabric)
	a.mu.Unlock()
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// with an Extension entry, which [AccessControl] restores from every
// stored setting at construction.
func (a *AccessControl) MatterHeldFabrics() []uint8 {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]uint8, 0, len(a.extensions))
	for f := range a.extensions {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// MatterRead implements [contract.ClusterServer]. It has no request behind
// it: Acl and Extension carry every fabric's entries whole, as
// [AccessControl.MatterReadFiltered] serves a local read.
func (a *AccessControl) MatterRead(attrID uint32) (any, bool) {
	a.settleOwedAuxiliary(context.Background())
	return a.readSettled(attrID)
}

// readSettled is MatterRead after the owed synchronisation was settled:
// a read settles once, so MatterReadFiltered, which settled already,
// serves its non-list attributes here (#621).
func (a *AccessControl) readSettled(attrID uint32) (any, bool) {
	switch attrID {
	case accessControlAttrACL, accessControlAttrExtension:
		// A read without an IM request behind it is a local one and
		// sees every fabric's entries whole (localListRead).
		return a.localListRead(context.Background(), attrID)
	case accessControlAttrSubjectsPerAccessControl:
		return a.subjectsPerEntry(), true
	case accessControlAttrTargetsPerAccessControl:
		return a.targetsPerEntry(), true
	case accessControlAttrAccessControlEntriesPerFabric:
		return a.entriesPerFabric(), true
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
// Initial; a list without the session's own entries is "this subject
// has no Administer privilege" to it, and it tears the fabric down via
// RemoveFabric.
//
// A non-fabric-filtered read returns every fabric's entries whole (see
// unfilteredFabrics); the dispatcher withholds another fabric's
// fabric-sensitive fields from the request, as it scopes every
// fabric-scoped attribute of the model.
//
// A request from a session with no fabric yet — PASE before AddNOC,
// FabricIndex 0 — owns no entry: its fabric-filtered read is empty.
// A read with no request behind it ([im.LookupFabricFilter] reports no
// filter) is a local one: every fabric's entries, whole
// (localListRead). The two are not allowed to stand for one another,
// because the local read sees what a session without a fabric may not.
//
// Mirrors matter.js packages/node/src/behaviors/access-control/
// AccessControlServer.ts: every read of `acl` and `extension` consults
// the FabricFilter from the IM context. The non-fabric-scoped attributes
// (SubjectsPerAccessControlEntry, TargetsPerAccessControlEntry,
// AccessControlEntriesPerFabric, FeatureMap, ClusterRevision) fall
// through to MatterRead.
//
// The read settles an owed auxiliary synchronisation once, before it
// serves any attribute: the fall-through serves MatterRead's attributes
// without settling a second time.
func (a *AccessControl) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	a.settleOwedAuxiliary(ctx)
	if attrID == accessControlAttrAuxiliaryACL {
		filtered, fabricIndex := im.FabricFilterFromContext(ctx)
		return a.auxiliaryACLRead(ctx, filtered, fabricIndex, false)
	}
	if attrID != accessControlAttrACL && attrID != accessControlAttrExtension {
		return a.readSettled(attrID) //nolint:contextcheck // MatterRead's body: the unfiltered cluster-interface read takes no ctx by the Matter cluster-server contract
	}
	filtered, fabricIndex, request := im.LookupFabricFilter(ctx)
	if !request {
		// No Interaction Model request behind the read: a local caller.
		return a.localListRead(ctx, attrID)
	}
	var fabrics []uint8
	switch {
	case filtered && fabricIndex == 0:
		// A session without a fabric owns no entry.
	case filtered:
		fabrics = []uint8{fabricIndex}
	default:
		var err error
		if fabrics, err = a.unfilteredFabrics(ctx, fabricIndex); err != nil {
			return nil, false
		}
	}
	return a.listRead(ctx, attrID, fabrics)
}

// localListRead is the Acl or Extension read of a local actor — a read
// with no Interaction Model request behind it (the node facade's
// ReadAttribute, a host's MatterRead): every fabric's entries, each
// whole. The fabric last set with [AccessControl.SetCurrentFabric] scopes
// nothing here. Mirrors matter.js
// packages/protocol/src/action/server/AccessControl.ts:250-253 and
// :342-345 at db021da0 (mayRead grants a local actor every attribute and
// every fabric-sensitive field before any fabric check) and
// packages/node/src/behavior/state/managed/values/ListManager.ts:609-616
// (createProxy filters a fabric-scoped list only for a remote actor).
func (a *AccessControl) localListRead(ctx context.Context, attrID uint32) (any, bool) {
	fabrics, err := a.unfilteredFabrics(ctx, 0)
	if err != nil {
		return nil, false
	}
	return a.listRead(ctx, attrID, fabrics)
}

// listRead lists the Acl or Extension entries of fabrics in their order,
// each whole.
func (a *AccessControl) listRead(ctx context.Context, attrID uint32, fabrics []uint8) (any, bool) {
	if attrID == accessControlAttrExtension {
		out := []AccessControlExtensionEntry{}
		a.mu.RLock()
		for _, fabric := range fabrics {
			out = append(out, a.extensions[fabric]...)
		}
		a.mu.RUnlock()
		return out, true
	}
	out := []AccessControlEntryStruct{}
	for _, fabric := range fabrics {
		entries, err := a.listACLMarking(ctx, fabric)
		if err != nil {
			return nil, false
		}
		for _, e := range entries {
			out = append(out, aclEntryStruct(e))
		}
	}
	return out, true
}

// listACLMarking is a read's list of fabric's Acl rows, which marks the
// fabric held when they are not empty (aclHeld). It takes no writeMu — a
// write's event hook may read Acl on the goroutine that holds it
// (decision 681) — so the mark is ordered against a drop by the fabric's
// drop epoch: noted before the list, the mark is set only if no drop ran
// meanwhile. A read that saw the rows of a fabric removed meanwhile does
// not mark it again after its drop cleared the mark (review II,
// R5-conc-5004).
func (a *AccessControl) listACLMarking(ctx context.Context, fabric uint8) ([]store.ACLEntry, error) {
	a.mu.RLock()
	epoch := a.aclDropEpoch[fabric]
	a.mu.RUnlock()
	entries, err := a.store.ListACL(ctx, fabric)
	if err == nil && len(entries) > 0 {
		a.mu.Lock()
		if a.aclDropEpoch[fabric] == epoch {
			if a.aclHeld == nil {
				a.aclHeld = map[uint8]bool{}
			}
			a.aclHeld[fabric] = true
		}
		a.mu.Unlock()
	}
	return entries, err
}

// unfilteredFabrics lists the fabrics a non-fabric-filtered read of Acl or
// Extension covers: every fabric the store knows, in index order, the
// accessing one included. Acl and Extension are fabric-scoped ("F") but
// not fabric-sensitive lists, so matter.js ListManager createProxy filters
// them only for `session.fabricFiltered` — an unfiltered read sees every
// fabric's entries — whose fabric-sensitive fields ("S") the dispatcher
// withholds from a session whose fabric does not own the entry. A store that
// cannot enumerate fabrics leaves the accessing one, as before. A session
// without a fabric (accessing 0) adds none of its own.
func (a *AccessControl) unfilteredFabrics(ctx context.Context, accessing uint8) ([]uint8, error) {
	lister, ok := a.store.(fabricLister)
	if !ok {
		// The fabrics this server holds an Extension of are known without
		// the store; the Acl of another fabric is not (review II,
		// N4-w3core-1).
		var out []uint8
		a.mu.RLock()
		for fabric := range a.extensions {
			out = append(out, fabric)
		}
		a.mu.RUnlock()
		if accessing != 0 {
			out = append(out, accessing)
		}
		slices.Sort(out)
		return slices.Compact(out), nil
	}
	recs, err := lister.ListFabrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("core: AccessControl: list fabrics: %w", err)
	}
	out := make([]uint8, 0, len(recs)+1)
	for i := range recs {
		r := &recs[i]
		out = append(out, r.FabricIndex)
	}
	a.mu.RLock()
	for fabric := range a.extensions {
		if fabric != 0 {
			out = append(out, fabric)
		}
	}
	a.mu.RUnlock()
	if accessing != 0 {
		out = append(out, accessing)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// fabricGone refuses a write of attr for a fabric the store no longer
// holds — one removed while the write was on its way, whose removal's
// clear has already run: without the check the write would store under a
// removed index (review II, R2-P6-store-1010, N4-notesE-1). A pending row
// counts as held. A store that cannot list fabrics refuses nothing; a
// listing that fails refuses the write with FAILURE, as [fabricListed]
// does (fail closed): the Extension's setting has no tie to the fabric row
// that would refuse it instead (review II, R2-P7-acl-02). writeMu is held,
// as by MatterDropFabric.
func (a *AccessControl) fabricGone(ctx context.Context, fabric uint8, attr string) error {
	lister, ok := a.store.(fabricLister)
	if !ok {
		return nil
	}
	listed, err := fabricListed(ctx, lister, fabric)
	if err != nil {
		return storeFailureErr{err: fmt.Errorf("core: AccessControl: %s write for fabric %d: list fabrics: %w", attr, fabric, err)}
	}
	if listed {
		return nil
	}
	return aclStatusError{im.StatusUnsupportedAccess, fmt.Sprintf("%s write for fabric %d, which no longer exists", attr, fabric)}
}

// writeFabric resolves the accessing fabric of a write to the
// fabric-scoped ACL or Extension list. An IM write uses the request's
// fabric, stamped by bridge/receive.go from the inbound session; a write
// without an accessing fabric (a PASE session before AddNOC) is refused
// with UnsupportedAccess, and the entries' own FabricIndex is never
// consulted for it. A local write — MatterWrite without a request
// context, matter.js setStateOf — uses the [AccessControl.SetCurrentFabric]
// value, else the fabric its entries carry (entryFabric), and is refused
// without either. Mirrors chip src/app/WriteHandler.cpp:805-811 at 6170af84
// ("a fabric-scoped list and there is no accessing fabric →
// UNSUPPORTED_ACCESS") and matter.js
// packages/protocol/src/action/server/AccessControl.ts:358-365
// (authorizeWrite: "Permission denied: No accessing fabric",
// Status.UnsupportedAccess); Binding.MatterWrite does the same.
func (a *AccessControl) writeFabric(ctx context.Context, attr string, entryFabric uint8) (uint8, error) {
	_, fabric, request := im.LookupFabricFilter(ctx)
	if !request {
		a.mu.RLock()
		fabric = a.currentFabric
		a.mu.RUnlock()
		if fabric == 0 {
			fabric = entryFabric
		}
	}
	if fabric == 0 {
		return 0, aclStatusError{im.StatusUnsupportedAccess, attr + " is fabric-scoped; the write has no accessing fabric"}
	}
	return fabric, nil
}

// MatterWrite handles writes to the cluster's writable attributes: ACL
// (0x0000) and Extension (0x0001), each a fabric-scoped list the write
// replaces for the accessing fabric (writeFabric); any other attribute
// is refused. Apple Home rewrites the entire ACL ~10 ms after
// CommissioningComplete to install HomePod / AppleTV edge controllers
// as additional Administer subjects on the freshly-paired bridge.
// Without a working write path Apple times out after 10 s and tears the
// fabric down via RemoveFabric. An Extension write is held to matter.js's
// checks — at most one entry for the fabric, its Data a well-formed TLV
// list (AccessControlServer.ts:344-362, :416-434 at db021da0) — and to
// the model's 128 bytes of Data (access-control.element.ts:206).
func (a *AccessControl) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := a.MatterWriteChanged(ctx, attrID, value)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// write of [AccessControl.MatterWrite], changed when it stored a list —
// the accessing fabric's ACL or Extension list differed from the one it
// held. A list equal to the held one is not stored, fires no change event
// and leaves the server's own version (matter.js Datasource.ts:1456-1457,
// :1476-1492 at db021da0); the read, the comparison and the store hold
// writeMu, so a concurrent write is never undone under an unchanged verdict.
func (a *AccessControl) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) { //nolint:gocognit,gocyclo,funlen // wire/dispatch table over many attribute/opcode cases
	if attrID == accessControlAttrACL {
		entries, ok := value.([]AccessControlEntryStruct)
		if !ok {
			return false, fmt.Errorf("%w: ACL write: value type %T not []AccessControlEntryStruct", errAccessControlInvalidValue, value)
		}
		var entryFabric uint8
		if len(entries) > 0 {
			entryFabric = entries[0].FabricIndex
		}
		fabric, err := a.writeFabric(ctx, "ACL", entryFabric)
		if err != nil {
			return false, err
		}
		// An auxiliary entry is synthesised, never written. Mirrors
		// matter.js AccessControlServer #validateAccessControlListChanges:
		// "The spec forbids the field here without naming a status; CHIP
		// answers FAILURE".
		for i, e := range entries {
			if e.AuxiliaryType != nil {
				return false, aclStatusError{im.StatusFailure, fmt.Sprintf("ACL[%d] must not include AuxiliaryType", i)}
			}
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
		// ConstraintError (both typed, spec.Errorf).
		// Every entry in the list is persisted under the writer's fabric
		// (the stamp below), so every entry counts against the writer's
		// per-fabric limit — the FabricIndex a client puts on the wire is
		// raw input, not a scope. matter.js reaches the same count through
		// its fabric-scoped write machinery, which has already stamped the
		// accessing fabric before AccessControlServer.ts:186-189 filters on
		// it; counting the client's own value here instead let a list of
		// entries tagged with a foreign index pass the limit and then be
		// stored, all of them, on the writer's fabric.
		if fabricACLs := len(entries); fabricACLs > int(a.entriesPerFabric()) {
			return false, spec.Errorf(im.StatusResourceExhausted, "core: AccessControl.ACL write: resource exhausted: AccessControlEntriesPerFabric=%d > limit=%d", fabricACLs, a.entriesPerFabric())
		}
		for i, e := range entries {
			if len(e.Subjects) > int(a.subjectsPerEntry()) {
				return false, spec.Errorf(im.StatusResourceExhausted, "core: AccessControl.ACL[%d] write: resource exhausted: SubjectsPerAccessControlEntry=%d > limit=%d", i, len(e.Subjects), a.subjectsPerEntry())
			}
			if len(e.Targets) > int(a.targetsPerEntry()) {
				return false, spec.Errorf(im.StatusResourceExhausted, "core: AccessControl.ACL[%d] write: resource exhausted: TargetsPerAccessControlEntry=%d > limit=%d", i, len(e.Targets), a.targetsPerEntry())
			}
			// Enum validity: reject out-of-range Privilege / AuthMode values
			// before the semantic checks below. matter.js enforces these via
			// schema supervision (AccessControlEntryPrivilegeEnum 1..5,
			// AuthModeEnum 1..3); an unchecked write would persist e.g.
			// Privilege=7 / AuthMode=9. Mirrors chip AccessControl.cpp
			// Entry::IsValid() privilege/authMode guards.
			if e.Privilege < accessControlPrivilegeView || e.Privilege > accessControlPrivilegeAdminister {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d] write: constraint error: Privilege=%d not in 1..5 (View..Administer)", i, e.Privilege)
			}
			if e.AuthMode < accessControlAuthModePASE || e.AuthMode > accessControlAuthModeGroup {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d] write: constraint error: AuthMode=%d not in 1..3 (PASE/CASE/Group)", i, e.AuthMode)
			}
			if e.AuthMode == accessControlAuthModePASE {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d] write: constraint error: AuthMode=PASE is forbidden in ACL", i)
			}
			if e.AuthMode == accessControlAuthModeGroup && e.Privilege == accessControlPrivilegeAdminister {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d] write: constraint error: Group authmode + Administer privilege rejected", i)
			}
			// Subject range validation per AuthMode.
			// Mirrors chip src/access/AccessControl.cpp:735 IsValidCaseNodeId /
			// IsValidGroupNodeId per-subject checks inside Entry::IsValid().
			for j, subj := range e.Subjects {
				if subj == 0 {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Subjects[%d] write: constraint error: subject 0 is the undefined node ID", i, j)
				}
				if e.AuthMode == accessControlAuthModeCASE {
					// CASE subjects: operational node ID or CASE Auth Tag (CAT).
					if !aclIsValidCASESubject(subj) {
						return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Subjects[%d] write: constraint error: CASE subject 0x%016X is not a valid operational node ID or CASE auth tag", i, j, subj)
					}
				}
				if e.AuthMode == accessControlAuthModeGroup {
					// Group subjects: a Group ID, 0x0001 .. 0xFFFF.
					if !aclIsValidGroupSubject(subj) {
						return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Subjects[%d] write: constraint error: Group subject 0x%016X is not a group id", i, j, subj)
					}
				}
			}
			// Target validation: matter.js requires DeviceType and
			// Endpoint mutually exclusive, and at least one of
			// (Cluster, Endpoint, DeviceType) must be present.
			// Additionally validate cluster/endpoint/device-type ID ranges.
			for j, t := range e.Targets {
				if t.DeviceType != nil && t.Endpoint != nil {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Targets[%d] write: constraint error: DeviceType and Endpoint mutually exclusive", i, j)
				}
				if t.Cluster == nil && t.Endpoint == nil && t.DeviceType == nil {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Targets[%d] write: constraint error: at least one of Cluster/Endpoint/DeviceType must be set", i, j)
				}
				// Cluster / Endpoint / DeviceType ID validity, in the same
				// order matter.js checks them. Mirrors matter.js
				// packages/node/src/behaviors/access-control/AccessControlServer.ts:254-265 at db91886c
				// (ClusterId.isValid / EndpointNumber.isValid / DeviceTypeId.isValid,
				// each throwing ConstraintError). Cross-check chip
				// AccessControl.cpp:746-748 IsValidClusterId / IsValidEndpointId /
				// IsValidDeviceTypeId guards in Entry::IsValid().
				if t.Cluster != nil && !aclIsValidClusterID(*t.Cluster) {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Targets[%d] write: constraint error: Cluster 0x%08X is not a valid ClusterId", i, j, *t.Cluster)
				}
				// EndpointNumber.isValid rejects 0xFFFF (the reserved wildcard);
				// a uint16 endpoint cannot exceed that, so 0xFFFF is the only
				// invalid value.
				if t.Endpoint != nil && *t.Endpoint == 0xFFFF {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Targets[%d] write: constraint error: EndpointId 0xFFFF is reserved", i, j)
				}
				if t.DeviceType != nil && !aclIsValidDeviceTypeID(*t.DeviceType) {
					return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.ACL[%d].Targets[%d] write: constraint error: DeviceType 0x%08X is not a valid DeviceTypeId", i, j, *t.DeviceType)
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
		// The read of the held list, the comparison and the store are one
		// step under writeMu: a write that answers unchanged has stored
		// nothing, so it cannot undo a change another write made.
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.fabricGone(ctx, fabric, "Acl"); err != nil {
			return false, err
		}
		// Snapshot old ACL before the replace so we can classify the
		// change type for the spec-mandated AccessControlEntryChanged event
		// (§9.10.7.1). We read before write; a store error here is
		// non-fatal for the write itself — we fall back to ChangeType=Changed
		// if the snapshot fails.
		oldEntries, listErr := a.store.ListACL(ctx, fabric)
		// A write of the list the fabric holds changes nothing: it is not
		// stored, advances no version and fires no event, as matter.js
		// notes no change and queues no $Changed for an equal value
		// (Datasource.ts:1476-1492 at db021da0, #noteIfChanged). Without
		// the old list the write is stored and counts as a change.
		if listErr == nil && aclEntriesEqual(oldEntries, out) {
			return false, nil
		}

		if err := a.store.ReplaceACL(ctx, fabric, out); err != nil {
			return false, fmt.Errorf("core: AccessControl.ACL write: %w", err)
		}
		a.markACLHeld(fabric, len(out) > 0)
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
		// matter.js reports an ACL change on acl$Changed, which a write of
		// the list it already holds does not fire — the replace-with-empty
		// a chunked list write opens with included (TC-ACL-2.5); that write
		// returned above. A list the store could not read is compared as
		// empty.
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
			if im.IsListAppendWrite(ctx) && len(out) > len(oldEntries) {
				// A list append (ListIndex=null) adds the trailing entry and
				// touches none before it: only that one is reported, as
				// chip's AccessControl list append (CreateEntry → Added)
				// reports it — a chunked ACL write (REPLACE-ALL then
				// appends) yields Removed for the replaced entries and
				// Added per appended one (TC-ACL-2.6). matter.js receives
				// the whole list and reports every position
				// (BD-Matter-ACLAppendEvents).
				i = len(oldEntries)
			}
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
		return true, nil
	}
	if attrID == accessControlAttrExtension {
		// Extension (0x0001) write path: replace the per-fabric extension
		// list. The list carries vendor-opaque octstrings (max 128 bytes
		// each) that some controllers attach to ACL fabrics.
		// Mirrors matter.js AccessControlServer.ts in-memory extension state.
		entries, ok := value.([]AccessControlExtensionEntry)
		if !ok {
			return false, fmt.Errorf("%w: Extension write: value type %T not []AccessControlExtensionEntry", errAccessControlInvalidValue, value)
		}
		fabric, err := a.writeFabric(ctx, "Extension", 0)
		if err != nil {
			return false, err
		}
		// Every entry this write stores is re-stamped to the writer's own
		// fabric below (a write reaches no other fabric's list), so more
		// than one entry in a single write always means
		// more than one entry for that fabric. Mirrors matter.js
		// AccessControlServer.ts:344-362 at db91886c
		// (#validateAccessControlExtensionChanges): a fabric may hold at
		// most one AccessControlExtensionStruct.
		if len(entries) > 1 {
			return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.Extension write: constraint error: a fabric may hold at most one entry, got %d", len(entries))
		}
		for i, e := range entries {
			if len(e.Data) > 128 {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.Extension[%d] write: constraint error: Data length %d exceeds max 128", i, len(e.Data))
			}
			// Data must itself decode as a well-formed TLV List — mirrors
			// matter.js AccessControlServer.ts:416-434 at db91886c
			// (extensionEntryValidator's default implementation). A blob
			// that fails this is rejected rather than stored, so a
			// garbage write cannot wedge a later fabric-scoped read.
			if err := validateAccessControlExtensionData(e.Data); err != nil {
				return false, spec.Errorf(im.StatusConstraintError, "core: AccessControl.Extension[%d] write: constraint error: %w", i, err)
			}
		}
		stamped := make([]AccessControlExtensionEntry, len(entries))
		for i, e := range entries {
			stamped[i] = AccessControlExtensionEntry{
				Data:        append([]byte(nil), e.Data...),
				FabricIndex: fabric,
			}
		}
		// The read of the held entries, the comparison and the store are
		// one step under writeMu, as for Acl above: entries equal to the
		// held ones are neither persisted nor stored, advance no version
		// and fire no event (Datasource.ts:1476-1492 at db021da0).
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		if err := a.fabricGone(ctx, fabric, "Extension"); err != nil {
			return false, err
		}
		a.mu.RLock()
		loadErr := a.extLoadErr[fabric]
		a.mu.RUnlock()
		if loadErr != nil {
			// The stored Extension of the fabric could not be read: it is
			// read again now, and a write that would replace a value nobody
			// saw is refused (review II, R2-P6-err-1016).
			if p, ok := a.store.(ACLExtensionPersistence); ok {
				loadErr = a.loadExtension(ctx, p, fabric)
			}
			if loadErr != nil {
				return false, spec.Errorf(im.StatusFailure, "core: AccessControl.Extension write: the stored Extension of fabric %d could not be read: %v", fabric, loadErr)
			}
		}
		// A removed fabric's setting still stored at this index is cleared
		// before the write compares with memory: an equal (empty) write
		// stores nothing, and would leave it to the next start (review II,
		// R2-C2-store-1004).
		if err := a.settleOwedClear(ctx, fabric); err != nil {
			return false, storeFailureErr{err: fmt.Errorf("core: AccessControl.Extension write: %w", err)}
		}
		a.mu.RLock()
		oldExtensions := a.extensions[fabric]
		a.mu.RUnlock()
		if extensionsEqual(oldExtensions, stamped) {
			return false, nil
		}
		if err := a.persistExtension(ctx, fabric, stamped); err != nil {
			return false, fmt.Errorf("core: AccessControl.Extension write: persist: %w", err)
		}
		a.mu.Lock()
		if a.extensions == nil {
			a.extensions = make(map[uint8][]AccessControlExtensionEntry)
		}
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
		return true, nil
	}
	return false, spec.Errorf(im.StatusUnsupportedWrite, "core: AccessControl attribute 0x%04X is read-only", attrID)
}

// errAccessControlInvalidValue is returned for an Acl or Extension write's
// value of the wrong Go type: INVALID_DATA_TYPE ([invalidValueErr]; owner
// decision 234), as the other security-path servers answer it (review II,
// R2-P7-acl-04).
var errAccessControlInvalidValue error = invalidValueErr{msg: "core: AccessControl invalid value"}

// MatterInvoke — no commands; AccessControl in 1.5.1 is attribute-only.
func (a *AccessControl) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("core: AccessControl has no command 0x%X", cmdID)
}

// MatterReportable returns the attributes that emit reports on
// change. The ACL list and Extension list are reportable per
// §9.10.4 so subscribers see entries appearing immediately after
// AddNOC.
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
// and chip's AccessControl cluster server (spec §9.10.7). The Extension
// write emits it ([AccessControl.MatterWrite]).
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
	// packages/node/src/behaviors/access-control/AccessControlServer.ts:205-212 at db91886c
	// (CaseAuthenticatedTag.getVersion(cat) === 0 → ConstraintError) and
	// packages/types/src/datatype/CaseAuthenticatedTag.ts:76
	// (getVersion = tag & 0xffff). Matter §6.6.2.1.2.
	return (id>>32) == 0xFFFF_FFFD && (id&0xFFFF) != 0
}

// aclIsValidClusterID reports whether id is a well-formed Matter ClusterId.
// Mirrors matter.js packages/types/src/datatype/ClusterId.ts:22-41 (the
// ClusterId constructor validation via Mei.fromMei): a standard cluster has a
// zero vendor prefix (upper 16 bits) and a type suffix (low 16 bits) in
// 0x0000..0x7FFF; a manufacturer-specific cluster has a non-zero vendor prefix
// (≤ 0xFFF4) and a type suffix in 0xFC00..0xFFFE. Everything else is invalid.
// Matter §7.10.
func aclIsValidClusterID(id uint32) bool {
	vendorPrefix := id >> 16
	typeSuffix := id & 0xFFFF
	// Mei.fromMei rejects a vendor prefix above 0xFFF4 or a type suffix of
	// 0xFFFF outright (ManufacturerExtensibleIdentifier.ts:28-51).
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
// packages/node/src/behaviors/access-control/AccessControlServer.ts:416-434 at db91886c
// (extensionEntryValidator, the default implementation): a controller
// may attach arbitrary vendor metadata to a fabric's Extension entry,
// but the octet-string must itself be decodable TLV, or a later
// fabric-scoped read of it can wedge a strict controller's decoder.
func validateAccessControlExtensionData(data []byte) error {
	if len(data) < 2 || data[0] != byte(tlv.TypeList) || data[len(data)-1] != byte(tlv.TypeEndContainer) {
		return errors.New("core: extension must be a valid TLV")
	}
	if err := tlv.Validate(data); err != nil {
		return fmt.Errorf("extension must be a valid TLV: %w", err)
	}
	// The list decodes as a tagged list (TlvTaggedList(…, true)): each of
	// its members carries a context or profile tag, never an anonymous
	// one ("Structure element tags should have an id", TlvObject.ts). The
	// test plan's D_BAD_ELEM holds an anonymous octet string (TC-ACL-2.3).
	// The element stream was validated above; the walk ends at its end.
	dec := tlv.NewDecoder(data)
	depth := 0
	for el, err := dec.Next(); err == nil; el, err = dec.Next() {
		if el.Type == tlv.TypeEndContainer {
			depth--
			continue
		}
		if depth == 1 && el.Tag.Kind == tlv.TagKindAnonymous {
			return errors.New("core: extension must be a valid TLV: list member without a tag")
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
func aclAdminFromContext(ctx context.Context) (adminNode *uint64, adminPasscodeID *uint16) {
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
//
// A clear of the index's stored Extension that a removed fabric's drop
// owes is retried first: the new fabric must not load it as its own at
// the next start (review II, R2-C2-store-1004). A setting the store could
// not read at construction (extLoadErr) is owed the same clear: a new
// fabric at the index owns nothing stored there (review II,
// R3-store-3001). A clear that fails again stays owed and is logged.
func (a *AccessControl) NotifyAdminEntryInstalled(entry store.ACLEntry) {
	a.writeMu.Lock()
	a.mu.Lock()
	if a.extLoadErr[entry.FabricIndex] != nil {
		delete(a.extLoadErr, entry.FabricIndex)
		a.oweClearLocked(entry.FabricIndex)
	}
	a.mu.Unlock()
	err := a.settleOwedClear(context.Background(), entry.FabricIndex)
	a.writeMu.Unlock()
	if err != nil {
		a.logger().Warn("matter.access_control.extension_owed_clear_failed",
			slog.Int("fabric_index", int(entry.FabricIndex)), slog.Any("err", err))
	}
	a.markACLHeld(entry.FabricIndex, true)
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

// NotifyLocalEntryInstalled reports an entry the node itself appended to
// a fabric's ACL — the WebRtcTransportRequestor's and the OTA provider's
// Operate entries (node/webrtc_requestor_acl.go, node/ota_provider.go):
// the ACL changed, so the DataVersion
// moves, and no AccessControlEntryChanged event is emitted, because
// matter.js emits none for a change without a remote actor
// (packages/node/src/behaviors/access-control/AccessControlServer.ts:
// 286-302, #handleAccessControlListChange returns when the context has no
// session's fabric).
//
// It names no fabric, so the fabric is not marked as holding Acl rows and
// its removal may go unreported; [AccessControl.NotifyLocalEntryInstalledOn]
// names it.
//
// Deprecated: use [AccessControl.NotifyLocalEntryInstalledOn], which names
// the fabric the entry was appended to and marks it, so the fabric's
// removal is reported as the Acl shrinking; NotifyLocalEntryInstalled keeps
// its behaviour until it goes and is removable in v0.29.0 (review II,
// P8-PR1-opus-L4, #622, decision 690).
func (a *AccessControl) NotifyLocalEntryInstalled() { a.dataVersion.Bump() }

// NotifyLocalEntryInstalledOn is [AccessControl.NotifyLocalEntryInstalled]
// for an entry the node appended to fabricIndex's ACL: the DataVersion
// moves, no AccessControlEntryChanged event is emitted, and the fabric is
// marked as holding Acl rows when the store holds them, so the fabric's
// removal is reported as the Acl shrinking ([AccessControl.MatterDropFabric];
// review II, R5-par-5003). The store is asked under the server's write
// lock, as a drop asks it; a list that fails marks the fabric, so its
// removal is asked about rather than missed. Call it after the entry is
// stored and the write lock the entry was written under is released.
func (a *AccessControl) NotifyLocalEntryInstalledOn(ctx context.Context, fabricIndex uint8) {
	a.writeMu.Lock()
	rows, err := a.store.ListACL(ctx, fabricIndex)
	a.markACLHeld(fabricIndex, err != nil || len(rows) > 0)
	a.writeMu.Unlock()
	a.dataVersion.Bump()
}

// aclEntriesEqual reports whether two stored ACL lists hold the same
// entries in the same order (positions and fabric stamps ignored).
func aclEntriesEqual(a, b []store.ACLEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(aclEntryStruct(a[i]), aclEntryStruct(b[i])) {
			return false
		}
	}
	return true
}

// extensionsEqual reports whether two Extension lists hold the same data.
func extensionsEqual(a, b []AccessControlExtensionEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i].Data, b[i].Data) || a[i].FabricIndex != b[i].FabricIndex {
			return false
		}
	}
	return true
}

// SetLogger routes the server's diagnostics to l; nil restores
// slog.Default(). Safe at any time (ADR 0026 P9).
func (a *AccessControl) SetLogger(l *slog.Logger) { a.log.Store(l) }

// logger is the server's logger; a value without SetLogger logs through
// slog.Default().
func (a *AccessControl) logger() *slog.Logger {
	if l := a.log.Load(); l != nil {
		return l
	}
	return slog.Default()
}
