// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// DatastoreClusterID is the JointFabricDatastore cluster id.
const DatastoreClusterID = dsdef.ClusterID

// Generated types the datastore speaks.
type (
	// DatastoreSync carries the entries to the member nodes and fetches
	// their lists.
	DatastoreSync = dsdef.DatastoreSync
	// DatastoreStore persists the tables.
	DatastoreStore = dsdef.DatastoreStore
	// GroupKeySet is a GroupKeySetList entry.
	GroupKeySet = dsdef.DatastoreGroupKeySetStruct
	// AdminEntry is an AdminList entry.
	AdminEntry = dsdef.DatastoreAdministratorInformationEntryStruct
	// NodeEntry is a NodeList entry.
	NodeEntry = dsdef.DatastoreNodeInformationEntryStruct
	// GroupEntry is a GroupList entry.
	GroupEntry = dsdef.DatastoreGroupInformationEntryStruct
	// EndpointGroupIDEntry is an EndpointGroupIDList entry.
	EndpointGroupIDEntry = dsdef.DatastoreEndpointGroupIDEntryStruct
	// EndpointBindingEntry is an EndpointBindingList entry.
	EndpointBindingEntry = dsdef.DatastoreEndpointBindingEntryStruct
	// NodeKeySetEntry is a NodeKeySetList entry.
	NodeKeySetEntry = dsdef.DatastoreNodeKeySetEntryStruct
	// ACLEntry is a NodeACLList entry.
	ACLEntry = dsdef.DatastoreACLEntryStruct
	// AccessControlEntry is the access control entry of an ACLEntry.
	AccessControlEntry = dsdef.DatastoreAccessControlEntryStruct
	// AccessControlTarget is a target of an AccessControlEntry.
	AccessControlTarget = dsdef.DatastoreAccessControlTargetStruct
	// EndpointEntry is a NodeEndpointList entry.
	EndpointEntry = dsdef.DatastoreEndpointEntryStruct
	// BindingTarget is the binding of an EndpointBindingEntry.
	BindingTarget = dsdef.DatastoreBindingTargetStruct
	// StatusEntry is an entry's state, the time it changed and its failure.
	StatusEntry = dsdef.DatastoreStatusEntryStruct
	// State is a StatusEntry's state.
	State = dsdef.DatastoreStateEnum
	// AddGroupRequest is an AddGroup command's payload.
	AddGroupRequest = dsdef.AddGroupRequest
	// UpdateGroupRequest is an UpdateGroup command's payload.
	UpdateGroupRequest = dsdef.UpdateGroupRequest
)

// The datastore's capacities, connectedhomeip's
// (JointFabricDatastore.h:512-517 at 6170af84).
const (
	MaxNodes            = 256
	MaxAdminNodes       = 32
	MaxGroups           = MaxNodes / 16
	MaxGroupKeySet      = MaxGroups * 16
	MaxFriendlyNameSize = 32
	MaxACLs             = 64
	// maxDERCertLength bounds AnchorRootCA (Credentials::kMaxDERCertLength,
	// CHIPCert.h:57).
	maxDERCertLength = 600
	// epochKeySize bounds an epoch key (CHIP_CRYPTO_SYMMETRIC_KEY_LENGTH_BYTES,
	// JointFabricDatastore.cpp:31-35).
	epochKeySize = 16
)

// The CATs a Joint Fabric reserves for its administrators and its anchor
// (CASEAuthTag.h:35-36); a group of either CAT is not the datastore's to
// change (JointFabricDatastore.cpp:1367-1371, :1418-1423, :1596-1600).
const (
	AdminCATIdentifier  uint16 = 0xFFFF
	AnchorCATIdentifier uint16 = 0xFFFE
)

// Errors of the datastore that carry no IM status — connectedhomeip's plain
// CHIP_ERRORs, which a command answers FAILURE
// (ClusterStatusCode(CHIP_ERROR), StatusCode.cpp:41-62).
var (
	// ErrNoSync is an operation that pushes to a node without a
	// [DatastoreSync] (CHIP_ERROR_INCORRECT_STATE).
	ErrNoSync = errors.New("jointfabric: the datastore has no DatastoreSync")
	// ErrRefreshBusy is a RefreshNode while one runs
	// (CHIP_ERROR_INCORRECT_STATE, JointFabricDatastore.cpp:243-245).
	ErrRefreshBusy = errors.New("jointfabric: a refresh is running")
	// ErrNoMemory is a table at its capacity (CHIP_ERROR_NO_MEMORY).
	ErrNoMemory = errors.New("jointfabric: the datastore table is full")
	// ErrNotFound is an entry the datastore does not hold
	// (CHIP_ERROR_NOT_FOUND).
	ErrNotFound = errors.New("jointfabric: no such datastore entry")
	// ErrInvalidArgument is a value over its bound
	// (CHIP_ERROR_INVALID_ARGUMENT).
	ErrInvalidArgument = errors.New("jointfabric: value out of bounds")
	// ErrSync wraps a DatastoreSync's error.
	ErrSync = errors.New("jointfabric: sync to the node failed")
	// ErrStore reports tables the store could not load or save.
	ErrStore = errors.New("jointfabric: store")
)

func constraintError(format string, args ...any) error {
	return spec.Errorf(im.StatusConstraintError, "JointFabricDatastore: "+format, args...)
}

func notFoundStatus(format string, args ...any) error {
	return spec.Errorf(im.StatusNotFound, "JointFabricDatastore: "+format, args...)
}

// DatastoreConfig carries the construction parameters of a [Datastore].
type DatastoreConfig struct {
	// Sync carries the entries to the member nodes; without one the
	// operations that push answer FAILURE, as connectedhomeip without a
	// delegate does.
	Sync DatastoreSync
	// Store persists the tables under Key; nil keeps them in memory.
	Store DatastoreStore
	// Key names the persisted record (the device's StableKey).
	Key string
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// aclEntry is a NodeACLList entry as the datastore holds it: the subjects
// and targets as lists (datastore::ACLEntryStruct,
// JointFabricDatastore.h:46-62).
type aclEntry struct {
	NodeID      uint64
	ListID      uint16
	Privilege   dsdef.DatastoreAccessControlEntryPrivilegeEnum
	AuthMode    dsdef.DatastoreAccessControlEntryAuthModeEnum
	Subjects    []uint64
	Targets     []AccessControlTarget
	StatusEntry StatusEntry
}

// encode is the entry as the attribute and a sync carry it
// (joint-fabric-datastore-server.cpp:250-271).
func (a aclEntry) encode() ACLEntry {
	return ACLEntry{NodeId: a.NodeID, ListId: a.ListID, StatusEntry: a.StatusEntry, AclEntry: AccessControlEntry{
		Privilege: a.Privilege, AuthMode: a.AuthMode,
		Subjects: spec.Nullable[[]uint64]{Value: slices.Clone(a.Subjects)},
		Targets:  spec.Nullable[[]AccessControlTarget]{Value: slices.Clone(a.Targets)},
	}}
}

// tables are the datastore's records (JointFabricDatastore.h:519-526,
// :562-577): the anchor, the status and the lists. They are the persisted
// record.
type tables struct {
	AnchorRootCA      []byte                 `json:"anchorRootCA,omitempty"`
	AnchorNodeID      uint64                 `json:"anchorNodeID,omitempty"`
	AnchorVendorID    uint16                 `json:"anchorVendorID,omitempty"`
	AnchorFabricIndex uint8                  `json:"anchorFabricIndex,omitempty"`
	FriendlyName      string                 `json:"friendlyName,omitempty"`
	Status            StatusEntry            `json:"status"`
	Nodes             []NodeEntry            `json:"nodes,omitempty"`
	GroupKeySets      []GroupKeySet          `json:"groupKeySets,omitempty"`
	Admins            []AdminEntry           `json:"admins,omitempty"`
	Groups            []GroupEntry           `json:"groups,omitempty"`
	EndpointGroupIDs  []EndpointGroupIDEntry `json:"endpointGroupIDs,omitempty"`
	Bindings          []EndpointBindingEntry `json:"bindings,omitempty"`
	NodeKeySets       []NodeKeySetEntry      `json:"nodeKeySets,omitempty"`
	ACLs              []aclEntry             `json:"acls,omitempty"`
	Endpoints         []EndpointEntry        `json:"endpoints,omitempty"`
}

// settingPrefix keys the persisted record, followed by the device's
// StableKey — the TLS servers' shape (cluster/tls/persist.go).
const settingPrefix = "jointfabric.datastore:"

// Datastore implements [contract.ClusterServer] for JointFabricDatastore:
// the Joint Fabric's registry of its nodes, administrators, groups, key
// sets, endpoints, bindings and ACLs, with each entry's pending/committed
// state; the commands change it and push the change to the node through
// the [DatastoreSync]; RefreshNode reconciles a node's lists with what the
// node holds. The rules are connectedhomeip's JointFabricDatastore.cpp and
// joint-fabric-datastore-server.cpp at 6170af84; matter.js's
// JointFabricDatastoreServer is the generated behaviour
// (JointFabricDatastoreServer.ts:14 at 862ad75f).
//
// A command answers when its change is in the tables; the push to the
// node runs after the answer, as connectedhomeip's delegate completes
// asynchronously, and commits the entry when it returns. The host methods
// of the same name push before they return.
type Datastore struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	sync  DatastoreSync
	store DatastoreStore
	key   string

	// mu guards t, dirty and the refresh state.
	mu         sync.Mutex
	t          tables
	dirty      map[uint32]struct{}
	refreshing uint64 // 0: no refresh (kUndefinedNodeId)
	inRefresh  bool

	// pushes are the pushes the commands started.
	pushes sync.WaitGroup
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*Datastore)(nil)
	_ contract.ClusterDataVersion            = (*Datastore)(nil)
	_ contract.ClusterAttributeLister        = (*Datastore)(nil)
	_ contract.ClusterCommandLister          = (*Datastore)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*Datastore)(nil)
	_ contract.AttributeChangeNotifier       = (*Datastore)(nil)
	_ spec.CommandRegistrar                  = (*Datastore)(nil)
)

// NewDatastore builds a JointFabricDatastore server; with a Store the
// persisted tables are loaded.
func NewDatastore(ctx context.Context, cfg DatastoreConfig) (*Datastore, error) {
	d := &Datastore{sync: cfg.Sync, store: cfg.Store, key: settingPrefix + cfg.Key, dirty: map[uint32]struct{}{}}
	if d.store != nil {
		v, ok, err := d.store.GetSetting(ctx, d.key)
		if err != nil {
			return nil, fmt.Errorf("%w: load %s: %w", ErrStore, d.key, err)
		}
		if ok && v != "" {
			if err := json.Unmarshal([]byte(v), &d.t); err != nil {
				return nil, fmt.Errorf("%w: decode %s: %w", ErrStore, d.key, err)
			}
		}
	}
	handlers := map[uint32]spec.CommandHandler{}
	for id, h := range d.commands() {
		handlers[id] = h
	}
	srv, err := spec.NewServer(dsdef.Definition, spec.Options{}, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, Source: datastoreAttrs{d},
	})
	if err != nil {
		return nil, fmt.Errorf("jointfabric: %w", err)
	}
	d.srv, d.Instance, d.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	return d, nil
}

// markLocked records changed attributes; the caller holds d.mu.
func (d *Datastore) markLocked(attrIDs ...uint32) {
	for _, id := range attrIDs {
		d.dirty[id] = struct{}{}
	}
}

// takeDirtyLocked returns and clears the changed attributes.
func (d *Datastore) takeDirtyLocked() []uint32 {
	if len(d.dirty) == 0 {
		return nil
	}
	ids := make([]uint32, 0, len(d.dirty))
	for id := range d.dirty {
		ids = append(ids, id)
	}
	clear(d.dirty)
	slices.Sort(ids)
	return ids
}

// update runs fn under the lock, saves the tables when it changed them
// and reports what it changed. A save that fails restores the tables as
// they were and is the change's error (ErrStore), as the TLS servers
// answer a failed save (cluster/tls/certificates.go). connectedhomeip
// reports NodeList only (MarkNodeListChanged,
// joint-fabric-datastore-server.cpp:287-291); here every attribute a
// change touches advances the data version and is reported.
func (d *Datastore) update(ctx context.Context, fn func() error) error {
	d.mu.Lock()
	var prev []byte
	if d.store != nil {
		prev, _ = json.Marshal(d.t)
	}
	err := fn()
	ids := d.takeDirtyLocked()
	if len(ids) > 0 && d.store != nil {
		if serr := d.saveLocked(ctx); serr != nil {
			d.t = tables{}
			_ = json.Unmarshal(prev, &d.t)
			ids = nil
			if err == nil {
				err = serr
			}
		}
	}
	d.mu.Unlock()
	if len(ids) > 0 && d.srv != nil {
		_ = d.srv.MarkChanged(ids...)
	}
	return err
}

// saveLocked writes the tables to the store; the caller holds d.mu.
func (d *Datastore) saveLocked(ctx context.Context) error {
	b, err := json.Marshal(d.t)
	if err != nil {
		return fmt.Errorf("%w: encode %s: %w", ErrStore, d.key, err)
	}
	if err := d.store.SetSetting(context.WithoutCancel(ctx), d.key, string(b)); err != nil {
		return fmt.Errorf("%w: save %s: %w", ErrStore, d.key, err)
	}
	return nil
}

// pushFn is a push to a node: the port call and, once it returned, its
// commit to the tables.
type pushFn func(ctx context.Context) error

// mutate runs fn under the lock, reports what it changed and returns the
// pushes it collected.
func (d *Datastore) mutate(ctx context.Context, fn func(pushes *[]pushFn) error) ([]pushFn, error) {
	var pushes []pushFn
	err := d.update(ctx, func() error { return fn(&pushes) })
	if err != nil {
		return nil, err
	}
	return pushes, nil
}

// runPushes runs the pushes in order; the errors are joined under ErrSync.
func runPushes(ctx context.Context, pushes []pushFn) error {
	var errs []error
	for _, p := range pushes {
		if err := p(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrSync, errors.Join(errs...))
}

// do is a host method: the change, then its pushes.
func (d *Datastore) do(ctx context.Context, fn func(pushes *[]pushFn) error) error {
	pushes, err := d.mutate(ctx, fn)
	if err != nil {
		return err
	}
	return runPushes(ctx, pushes)
}

// doAsync is a command: the change answers, its pushes run after.
func (d *Datastore) doAsync(ctx context.Context, fn func(pushes *[]pushFn) error) error {
	pushes, err := d.mutate(ctx, fn)
	if err != nil {
		return err
	}
	if len(pushes) > 0 {
		bg := context.WithoutCancel(ctx)
		d.pushes.Add(1)
		go func() {
			defer d.pushes.Done()
			_ = runPushes(bg, pushes)
		}()
	}
	return nil
}

// commit applies fn to the tables once a push returned.
func (d *Datastore) commit(ctx context.Context, fn func()) {
	_ = d.update(ctx, func() error { fn(); return nil })
}

// Wait waits for the pushes and the refresh the commands started.
func (d *Datastore) Wait() { d.pushes.Wait() }

// markCommitted marks the first entry pred matches Committed. Mirrors
// connectedhomeip detail::MarkEntryCommittedIfFound
// (JointFabricDatastore.h:75-85): the entry is found again by its stable
// key once the push returned, not by a position taken before.
func markCommitted[T any](entries []T, status func(*T) *StatusEntry, pred func(T) bool) bool {
	for i := range entries {
		if pred(entries[i]) {
			status(&entries[i]).State = dsdef.DatastoreStateCommitted
			return true
		}
	}
	return false
}

// friendlyName bounds a node's friendly name to 32 bytes
// (GenericDatastoreNodeInformationEntry::Set, JointFabricDatastore.h:129-150).
func friendlyName(name string) string {
	if len(name) > MaxFriendlyNameSize {
		return name[:MaxFriendlyNameSize]
	}
	return name
}

// datastoreAttrs serves the 14 attributes from the tables
// (joint-fabric-datastore-server.cpp:74-285).
type datastoreAttrs struct{ d *Datastore }

// MatterAttribute implements [spec.Source].
func (a datastoreAttrs) MatterAttribute(attrID uint32) (any, bool) {
	d := a.d
	d.mu.Lock()
	defer d.mu.Unlock()
	t := &d.t
	switch attrID {
	case dsdef.AttrAnchorRootCa:
		return slices.Clone(t.AnchorRootCA), true
	case dsdef.AttrAnchorNodeId:
		return t.AnchorNodeID, true
	case dsdef.AttrAnchorVendorId:
		return t.AnchorVendorID, true
	case dsdef.AttrFriendlyName:
		return t.FriendlyName, true
	case dsdef.AttrGroupKeySetList:
		return spec.List[GroupKeySet](cloneKeySets(t.GroupKeySets)), true
	case dsdef.AttrGroupList:
		return spec.List[GroupEntry](slices.Clone(t.Groups)), true
	case dsdef.AttrNodeList:
		return spec.List[NodeEntry](slices.Clone(t.Nodes)), true
	case dsdef.AttrAdminList:
		return spec.List[AdminEntry](cloneAdmins(t.Admins)), true
	case dsdef.AttrStatus:
		return t.Status, true
	case dsdef.AttrEndpointGroupIdList:
		return spec.List[EndpointGroupIDEntry](slices.Clone(t.EndpointGroupIDs)), true
	case dsdef.AttrEndpointBindingList:
		return spec.List[EndpointBindingEntry](slices.Clone(t.Bindings)), true
	case dsdef.AttrNodeKeySetList:
		return spec.List[NodeKeySetEntry](slices.Clone(t.NodeKeySets)), true
	case dsdef.AttrNodeAclList:
		return spec.List[ACLEntry](encodeACLs(t.ACLs)), true
	case dsdef.AttrNodeEndpointList:
		return spec.List[EndpointEntry](slices.Clone(t.Endpoints)), true
	}
	return nil, false
}

func encodeACLs(acls []aclEntry) []ACLEntry {
	out := make([]ACLEntry, len(acls))
	for i, a := range acls {
		out[i] = a.encode()
	}
	return out
}

func cloneKeySet(k GroupKeySet) GroupKeySet {
	k.EpochKey0.Value = slices.Clone(k.EpochKey0.Value)
	k.EpochKey1.Value = slices.Clone(k.EpochKey1.Value)
	k.EpochKey2.Value = slices.Clone(k.EpochKey2.Value)
	return k
}

func cloneKeySets(in []GroupKeySet) []GroupKeySet {
	out := make([]GroupKeySet, len(in))
	for i, k := range in {
		out[i] = cloneKeySet(k)
	}
	return out
}

func cloneAdmins(in []AdminEntry) []AdminEntry {
	out := slices.Clone(in)
	for i := range out {
		out[i].Icac = slices.Clone(out[i].Icac)
	}
	return out
}

// The host's reads of the tables, chip's getters
// (JointFabricDatastore.h:287-441); each returns a copy.

// AnchorRootCA returns the anchor's root certificate.
func (d *Datastore) AnchorRootCA() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.AnchorRootCA)
}

// AnchorNodeID returns the anchor's node id, 0 when unset.
func (d *Datastore) AnchorNodeID() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t.AnchorNodeID
}

// AnchorVendorID returns the anchor's vendor id, 0 when unset.
func (d *Datastore) AnchorVendorID() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t.AnchorVendorID
}

// AnchorFabricIndex returns the Joint Fabric's index on this node, 0 when
// unset.
func (d *Datastore) AnchorFabricIndex() uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t.AnchorFabricIndex
}

// FriendlyName returns the datastore's friendly name.
func (d *Datastore) FriendlyName() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t.FriendlyName
}

// Status returns the datastore's status.
func (d *Datastore) Status() StatusEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.t.Status
}

// NodeList returns the nodes.
func (d *Datastore) NodeList() []NodeEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.Nodes)
}

// GroupKeySetList returns the group key sets.
func (d *Datastore) GroupKeySetList() []GroupKeySet {
	d.mu.Lock()
	defer d.mu.Unlock()
	return cloneKeySets(d.t.GroupKeySets)
}

// AdminList returns the administrators.
func (d *Datastore) AdminList() []AdminEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return cloneAdmins(d.t.Admins)
}

// GroupList returns the groups.
func (d *Datastore) GroupList() []GroupEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.Groups)
}

// EndpointGroupIDList returns the endpoints' group memberships.
func (d *Datastore) EndpointGroupIDList() []EndpointGroupIDEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.EndpointGroupIDs)
}

// EndpointBindingList returns the endpoints' bindings.
func (d *Datastore) EndpointBindingList() []EndpointBindingEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.Bindings)
}

// NodeKeySetList returns the nodes' key sets.
func (d *Datastore) NodeKeySetList() []NodeKeySetEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.NodeKeySets)
}

// NodeACLList returns the nodes' ACL entries.
func (d *Datastore) NodeACLList() []ACLEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return encodeACLs(d.t.ACLs)
}

// NodeEndpointList returns the nodes' endpoints.
func (d *Datastore) NodeEndpointList() []EndpointEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.t.Endpoints)
}

// The host's setters (JointFabricDatastore.h:287-361).

// SetAnchorRootCA sets the anchor's root certificate; one of 600 bytes or
// more is refused (:287-296).
func (d *Datastore) SetAnchorRootCA(ctx context.Context, rcac []byte) error {
	if len(rcac) >= maxDERCertLength {
		return ErrInvalidArgument
	}
	return d.update(ctx, func() error {
		d.t.AnchorRootCA = slices.Clone(rcac)
		d.markLocked(dsdef.AttrAnchorRootCa)
		return nil
	})
}

// SetAnchorNodeID sets the anchor's node id (:299-303).
func (d *Datastore) SetAnchorNodeID(ctx context.Context, nodeID uint64) error {
	return d.update(ctx, func() error {
		d.t.AnchorNodeID = nodeID
		d.markLocked(dsdef.AttrAnchorNodeId)
		return nil
	})
}

// SetAnchorVendorID sets the anchor's vendor id (:306-310).
func (d *Datastore) SetAnchorVendorID(ctx context.Context, vendorID uint16) error {
	return d.update(ctx, func() error {
		d.t.AnchorVendorID = vendorID
		d.markLocked(dsdef.AttrAnchorVendorId)
		return nil
	})
}

// SetAnchorFabricIndex records the Joint Fabric's index on this node, so
// that its removal wipes the datastore (:316-317).
func (d *Datastore) SetAnchorFabricIndex(ctx context.Context, fabricIndex uint8) error {
	return d.update(ctx, func() error {
		d.t.AnchorFabricIndex = fabricIndex
		return nil
	})
}

// SetFriendlyName sets the datastore's friendly name; one of 32 bytes or
// more is refused (:340-351).
func (d *Datastore) SetFriendlyName(ctx context.Context, name string) error {
	if len(name) >= MaxFriendlyNameSize {
		return ErrInvalidArgument
	}
	return d.update(ctx, func() error {
		d.t.FriendlyName = name
		d.markLocked(dsdef.AttrFriendlyName)
		return nil
	})
}

// SetStatus sets the datastore's status (:358-363).
func (d *Datastore) SetStatus(ctx context.Context, state State, updateTimestamp uint32, failureCode uint8) error {
	return d.update(ctx, func() error {
		d.t.Status = StatusEntry{State: state, UpdateTimestamp: updateTimestamp, FailureCode: dsdef.Status(failureCode)}
		d.markLocked(dsdef.AttrStatus)
		return nil
	})
}

// OnFabricRemoved wipes the datastore and the anchor when the removed
// fabric is the Joint Fabric; any other fabric's removal leaves it. Mirrors
// JointFabricDatastore.h:323-338 (OnFabricRemoved, ClearAllRecords
// :532-559).
func (d *Datastore) OnFabricRemoved(ctx context.Context, fabricIndex uint8) {
	_ = d.update(ctx, func() error {
		if d.t.AnchorFabricIndex == 0 || fabricIndex != d.t.AnchorFabricIndex {
			return nil
		}
		d.t = tables{}
		d.markLocked(dsdef.AttrAnchorRootCa, dsdef.AttrAnchorNodeId, dsdef.AttrAnchorVendorId, dsdef.AttrFriendlyName,
			dsdef.AttrGroupKeySetList, dsdef.AttrGroupList, dsdef.AttrNodeList, dsdef.AttrAdminList, dsdef.AttrStatus,
			dsdef.AttrEndpointGroupIdList, dsdef.AttrEndpointBindingList, dsdef.AttrNodeKeySetList, dsdef.AttrNodeAclList,
			dsdef.AttrNodeEndpointList)
		return nil
	})
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (d *Datastore) MatterDataVersion() uint32 { return d.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (d *Datastore) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return d.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (d *Datastore) MatterRead(attrID uint32) (any, bool) { return d.srv.MatterRead(attrID) }

// MatterWrite refuses a write: every attribute is read-only.
func (d *Datastore) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return d.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches the commands; a host's handler runs in its
// place ([spec.SelfHeld.Handle]).
func (d *Datastore) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &d.SelfHeld, cmdID, fields)
}
