// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
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
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// the next minor release.
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
	AnchorRootCA      []byte `json:"anchorRootCA,omitempty"`
	AnchorNodeID      uint64 `json:"anchorNodeID,omitempty"`
	AnchorVendorID    uint16 `json:"anchorVendorID,omitempty"`
	AnchorFabricIndex uint8  `json:"anchorFabricIndex,omitempty"`
	// AnchorRootPublicKey and AnchorFabricID are the anchor fabric's
	// identity as the node's fabric table holds it: an index alone names
	// whichever fabric holds it now (R2-P6 RECORD-13).
	AnchorRootPublicKey []byte                 `json:"anchorRootPublicKey,omitempty"`
	AnchorFabricID      uint64                 `json:"anchorFabricID,omitempty"`
	FriendlyName        string                 `json:"friendlyName,omitempty"`
	Status              StatusEntry            `json:"status"`
	Nodes               []NodeEntry            `json:"nodes,omitempty"`
	GroupKeySets        []GroupKeySet          `json:"groupKeySets,omitempty"`
	Admins              []AdminEntry           `json:"admins,omitempty"`
	Groups              []GroupEntry           `json:"groups,omitempty"`
	EndpointGroupIDs    []EndpointGroupIDEntry `json:"endpointGroupIDs,omitempty"`
	Bindings            []EndpointBindingEntry `json:"bindings,omitempty"`
	NodeKeySets         []NodeKeySetEntry      `json:"nodeKeySets,omitempty"`
	ACLs                []aclEntry             `json:"acls,omitempty"`
	Endpoints           []EndpointEntry        `json:"endpoints,omitempty"`
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
// (JointFabricDatastoreServer.ts:14 at db021da0).
//
// The datastore is the anchor fabric's alone (owner decision 129, ahead
// of connectedhomeip, whose joint-fabric-datastore-server.cpp asks no
// accessing fabric): the registry holds the Joint Fabric's epoch keys and
// its pushes act with the anchor's authority. A command of a request of
// any other fabric answers UNSUPPORTED_ACCESS; a read or a subscription
// of one sees every list empty and every other attribute at its zero
// value ([Datastore.MatterReadFiltered] — the contract has no way yet for
// a server to refuse a read with a status). While no anchor is recorded,
// every peer is refused so: a Datastore without an [Administrator], or
// one whose anchor fabric's NOC lacks the administrator and anchor CATs,
// serves a peer only after the host's [Datastore.SetAnchorFabricIndex]. The
// anchor is its fabric index and that fabric's identity (root public key,
// fabric id) in the node's fabric table, which the node attaches
// ([Datastore.AttachNode]); without a table no peer is admitted, and a
// reused index with another identity is not the anchor. A call of the
// host carries no request and is not fenced.
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

	// mu guards t, dirty, saved, gen and the refresh state.
	mu    sync.Mutex
	t     tables
	dirty map[uint32]struct{}
	// saved is the record the store holds: an update saves only when the
	// tables differ from it.
	saved []byte
	// restore is what a refused save restores the tables to: the last
	// saved record, or the empty record once a wipe ran — whether or not
	// the wipe's own save went through — so a wipe stays a wipe across
	// every later refused save until the store takes the empty record
	// (R2-P6-conc-1107, Fable REWORK-6).
	restore []byte
	// gen counts the wipes of the tables; a refresh or a push started
	// before one calls no port and commits nothing after it
	// (R2-P6-conc-1107).
	gen uint64
	// fabrics is the node's fabric table ([Datastore.AttachNode]); the
	// anchor's identity is read from it. checked/admitted cache the last
	// comparison; every update and every AttachNode clears it.
	fabrics  FabricTable
	checked  bool
	admitted bool
	// version counts the updates and the AttachNode calls: a comparison
	// made outside mu is cached only when nothing moved meanwhile (the
	// table is an interface a host may implement with an uncomparable
	// type).
	version uint64
	// wiping marks the update that wipes: its attributes are reported even
	// when its save is refused (the tables are wiped either way).
	wiping     bool
	refreshing uint64 // 0: no refresh (kUndefinedNodeId)
	inRefresh  bool

	// pushes are the pushes the commands started.
	pushes sync.WaitGroup

	// bindingMu guards bindingLocks: one lock per node, so the
	// read-merge-write of one of its endpoints' binding lists
	// ([Datastore.pushBinding]) and the refresh's write of its whole list
	// ([Datastore.syncBindingsOfNode]) run one at a time.
	bindingMu    sync.Mutex
	bindingLocks map[bindingEndpoint]*endpointLock
}

// bindingEndpoint keys a binding lock: the node, whose endpoints' binding
// lists the lock guards together ([Datastore.lockNodeBindings]).
type bindingEndpoint struct {
	node uint64
}

// endpointLock is a lock a waiter can leave when its context ends; refs
// counts its holders and waiters, so the map drops it with the last.
type endpointLock struct {
	held chan struct{}
	refs int
}

// lockEndpoint takes the binding lock k, or fails with the context's
// error; the returned func releases it.
func (d *Datastore) lockEndpoint(ctx context.Context, k bindingEndpoint) (func(), error) {
	d.bindingMu.Lock()
	if d.bindingLocks == nil {
		d.bindingLocks = map[bindingEndpoint]*endpointLock{}
	}
	l := d.bindingLocks[k]
	if l == nil {
		l = &endpointLock{held: make(chan struct{}, 1)}
		d.bindingLocks[k] = l
	}
	l.refs++
	d.bindingMu.Unlock()
	drop := func() {
		d.bindingMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(d.bindingLocks, k)
		}
		d.bindingMu.Unlock()
	}
	select {
	case l.held <- struct{}{}:
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
	return func() {
		<-l.held
		drop()
	}, nil
}

// lockNodeBindings takes the binding lock of the node ([Datastore.lockEndpoint]
// keyed by the node alone): a push to any of its endpoints and the
// refresh's write of its whole list hold it, so neither writes a list read
// before the other wrote (review II, R2-P1-clu-05).
func (d *Datastore) lockNodeBindings(ctx context.Context, nodeID uint64) (func(), error) {
	return d.lockEndpoint(ctx, bindingEndpoint{node: nodeID})
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*Datastore)(nil)
	_ contract.ClusterDataVersion            = (*Datastore)(nil)
	_ contract.ClusterAttributeLister        = (*Datastore)(nil)
	_ contract.ClusterCommandLister          = (*Datastore)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*Datastore)(nil)
	_ contract.AttributeChangeNotifier       = (*Datastore)(nil)
	_ contract.FabricScopedState             = (*Datastore)(nil)
	_ contract.FabricScopedReader            = (*Datastore)(nil)
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
		if d.saved, err = json.Marshal(d.t); err != nil {
			return nil, fmt.Errorf("%w: encode %s: %w", ErrStore, d.key, err)
		}
		d.restore = d.saved
	}
	handlers := map[uint32]spec.CommandHandler{}
	for id, h := range d.commands() {
		handlers[id] = h
	}
	srv, err := spec.NewServer(dsdef.Definition, spec.Options{}, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, Source: datastoreAttrs{d}, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
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

// update runs fn under the lock, saves the tables when they differ from
// the record the store holds and reports the attributes fn marked. A
// save that fails restores the tables the store holds and is the
// change's error (ErrStore), as the TLS servers answer a failed save
// (cluster/tls/certificates.go). The record is encoded once per change
// and written only when it moved (R2-P6-bound-1113 c); the write runs
// under the lock, so a store's latency is every reader's. connectedhomeip
// reports NodeList only (MarkNodeListChanged,
// joint-fabric-datastore-server.cpp:287-291); here every attribute a
// change touches advances the data version and is reported.
func (d *Datastore) update(ctx context.Context, fn func() error) error {
	d.mu.Lock()
	err := fn()
	ids := d.takeDirtyLocked()
	if d.store != nil {
		if serr := d.persistLocked(ctx); serr != nil {
			if !d.wiping {
				ids = nil
			}
			if err == nil {
				err = serr
			}
		}
	}
	d.wiping = false
	d.checked = false
	d.version++
	d.mu.Unlock()
	if len(ids) > 0 && d.srv != nil {
		_ = d.srv.MarkChanged(ids...)
	}
	return err
}

// persistLocked writes the tables to the store when they differ from the
// record it holds; a failed write restores the tables to restore — the
// last saved record, or the empty one after a wipe
// ([Datastore.MatterDropFabric]). A wipe therefore stays a wipe in memory
// across refused saves; the store keeps the stale record until a save
// goes through, and after a restart the reconcile wipes the stale record,
// whose anchor the fabric table no longer holds. The caller holds d.mu.
func (d *Datastore) persistLocked(ctx context.Context) error {
	b, err := json.Marshal(d.t)
	if err == nil && bytes.Equal(b, d.saved) {
		return nil
	}
	if err == nil {
		err = d.store.SetSetting(context.WithoutCancel(ctx), d.key, string(b))
		if err == nil {
			d.saved, d.restore = b, b
			return nil
		}
		err = fmt.Errorf("%w: save %s: %w", ErrStore, d.key, err)
	} else {
		err = fmt.Errorf("%w: encode %s: %w", ErrStore, d.key, err)
	}
	d.t = tables{}
	_ = json.Unmarshal(d.restore, &d.t)
	return err
}

// pushFn is a push to a node: the port call and, once it returned, its
// commit to the tables.
type pushFn func(ctx context.Context) error

// mutate runs fn under the lock, reports what it changed and returns the
// pushes it collected.
func (d *Datastore) mutate(ctx context.Context, fn func(pushes *[]pushFn) error) ([]pushFn, context.Context, error) {
	var pushes []pushFn
	var gen uint64
	err := d.update(ctx, func() error {
		gen = d.gen
		return fn(&pushes)
	})
	if err != nil {
		return nil, ctx, err
	}
	return pushes, context.WithValue(ctx, genKey{}, gen), nil
}

// genKey carries, in a push's or a refresh's context, the generation of
// the tables it started on ([Datastore.commit]).
type genKey struct{}

// errWiped is a commit whose tables were wiped since its operation began.
var errWiped = fmt.Errorf("%w: the datastore was wiped", ErrNotFound)

// live refuses a push's port call or a refresh's step whose tables were
// wiped since its operation began ([genKey] in ctx): the push carries the
// removed Joint Fabric's entries, its key sets among them, to no node.
func (d *Datastore) live(ctx context.Context) error {
	gen, scoped := ctx.Value(genKey{}).(uint64)
	if !scoped {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.gen {
		return errWiped
	}
	return nil
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
	pushes, pctx, err := d.mutate(ctx, fn)
	if err != nil {
		return err
	}
	return runPushes(pctx, pushes)
}

// doAsync is a command: the change answers, its pushes run after.
func (d *Datastore) doAsync(ctx context.Context, fn func(pushes *[]pushFn) error) error {
	pushes, pctx, err := d.mutate(ctx, fn)
	if err != nil {
		return err
	}
	if len(pushes) > 0 {
		bg := context.WithoutCancel(pctx)
		d.pushes.Add(1)
		go func() {
			defer d.pushes.Done()
			_ = runPushes(bg, pushes)
		}()
	}
	return nil
}

// commit applies fn to the tables once a push or a refresh step
// returned; its error is the store's (R2-P6-err-1120). An operation whose
// tables were wiped since it began ([genKey] in ctx) commits nothing and
// ends with errWiped (R2-P6-conc-1107).
func (d *Datastore) commit(ctx context.Context, fn func()) error {
	gen, scoped := ctx.Value(genKey{}).(uint64)
	return d.update(ctx, func() error {
		if scoped && gen != d.gen {
			return errWiped
		}
		fn()
		return nil
	})
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
	for i := range in {
		out[i] = cloneKeySet(in[i])
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

// recordAnchor records an anchor in one change — its fabric index, root
// certificate (one of 600 bytes or more is left out, as
// SetAnchorRootCA refuses it), node id and vendor id — unless the
// datastore records one already: an anchored datastore is never
// re-pointed (it is wiped first). The error is the store's, or ErrInvalidArgument
// for an anchor recorded already.
func (d *Datastore) recordAnchor(ctx context.Context, f store.FabricRecord) error {
	return d.update(ctx, func() error {
		if d.t.AnchorFabricIndex != 0 {
			return fmt.Errorf("%w: the datastore's anchor is fabric %d", ErrInvalidArgument, d.t.AnchorFabricIndex)
		}
		d.t.AnchorFabricIndex = f.FabricIndex
		d.t.AnchorRootPublicKey, d.t.AnchorFabricID = slices.Clone(f.RootPublicKey), f.FabricID
		if len(f.RootCert) < maxDERCertLength {
			d.t.AnchorRootCA = slices.Clone(f.RootCert)
			d.markLocked(dsdef.AttrAnchorRootCa)
		}
		if f.NodeID != 0 {
			d.t.AnchorNodeID = f.NodeID
			d.markLocked(dsdef.AttrAnchorNodeId)
		}
		if f.VendorID != 0 {
			d.t.AnchorVendorID = f.VendorID
			d.markLocked(dsdef.AttrAnchorVendorId)
		}
		return nil
	})
}

// SetAnchorFabricIndex records the Joint Fabric's index on this node, so
// that its removal wipes the datastore (:316-317), and opens the
// datastore to that fabric's peers. The fabric's identity (root public
// key, fabric id) is taken from the fabric table — now, when one is
// attached, else at [Datastore.AttachNode]; until then no peer is
// admitted.
func (d *Datastore) SetAnchorFabricIndex(ctx context.Context, fabricIndex uint8) error {
	err := d.update(ctx, func() error {
		if d.t.AnchorFabricIndex != fabricIndex {
			// Who may read every attribute changed: each is reported, so
			// a subscriber of the admitted fabric gets the tables (Fable
			// RECORD-12).
			d.markLocked(allAttrs...)
			d.t.AnchorRootPublicKey, d.t.AnchorFabricID = nil, 0
		}
		d.t.AnchorFabricIndex = fabricIndex
		return nil
	})
	if err != nil {
		return err
	}
	d.mu.Lock()
	table := d.fabrics
	d.mu.Unlock()
	if table != nil {
		d.checkAnchor(ctx, table)
	}
	return nil
}

// AttachNode hands the datastore the node's fabric table, which the node
// calls as the endpoint joins the topology (a host that wires the
// datastore outside the node calls it itself; without a table no peer is
// admitted). It then checks the recorded anchor ([Datastore.checkAnchor]).
// The admission compares the accessing fabric with the table once and
// caches the verdict until the next change of the datastore: the node
// forwards every fabric removal and every install of an index to it
// ([Datastore.MatterDropFabric]), and a host that wires the datastore
// outside the node must forward them too, or a fabric that took the
// anchor's index after a removal it did not forward is held against the
// cached verdict (Fable RECORD-16).
func (d *Datastore) AttachNode(fabrics FabricTable, _ wire.WindowController) {
	d.mu.Lock()
	d.fabrics, d.checked = fabrics, false
	d.version++
	d.mu.Unlock()
	if fabrics != nil {
		d.checkAnchor(context.Background(), fabrics)
	}
}

// checkAnchor holds the recorded anchor against the fabric table and
// returns the anchor's index while the table holds that fabric with the
// recorded identity, else 0:
//   - no anchor, or the table does not hold its index: 0 (a removed
//     anchor fabric's record is the reconcile's to wipe);
//   - an anchor recorded without its identity — a record of an earlier
//     build, or the host's SetAnchorFabricIndex before a table was
//     attached: the identity of the fabric the table holds at that index
//     is adopted. Re-deriving rather than treating the record as
//     unanchored keeps an earlier build's registry with its anchor; the
//     node's reconcile, which runs before the topology's AttachNode,
//     already wiped a record whose index the table no longer holds, so
//     what is adopted is the fabric that held the index when the record
//     was written, unless the index was handed out again while the
//     datastore's endpoint was absent — a window that exists only for a
//     record written before this build;
//   - the table holds the index with another identity: the anchor
//     fabric is gone and its index handed out again while the datastore
//     did not hear of it (an endpoint absent at the removal and at the
//     reuse) — the datastore is wiped, as the removal would have
//     (R2-P6 RECORD-13).
func (d *Datastore) checkAnchor(ctx context.Context, table FabricTable) uint8 {
	d.mu.Lock()
	anchor, rpk, fid := d.t.AnchorFabricIndex, slices.Clone(d.t.AnchorRootPublicKey), d.t.AnchorFabricID
	d.mu.Unlock()
	if anchor == 0 {
		return 0
	}
	f, err := table.GetFabric(ctx, anchor)
	if err != nil {
		return 0
	}
	switch {
	case len(rpk) == 0:
		adopted := false
		err := d.update(ctx, func() error {
			if d.t.AnchorFabricIndex == anchor && len(d.t.AnchorRootPublicKey) == 0 {
				d.t.AnchorRootPublicKey, d.t.AnchorFabricID = slices.Clone(f.RootPublicKey), f.FabricID
				// Who may read every attribute changed: each is reported
				// (Fable REWORK-8).
				d.markLocked(allAttrs...)
				adopted = true
			}
			return nil
		})
		if adopted && err == nil { // a refused save rolled the adoption back
			return anchor
		}
		return 0
	case bytes.Equal(rpk, f.RootPublicKey) && fid == f.FabricID:
		return anchor
	}
	_ = d.MatterDropFabric(ctx, anchor)
	return 0
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

// OnFabricRemoved is [Datastore.MatterDropFabric] without its error. The
// node reaches the datastore through [contract.FabricScopedState] alone.
func (d *Datastore) OnFabricRemoved(ctx context.Context, fabricIndex uint8) {
	_ = d.MatterDropFabric(ctx, fabricIndex)
}

// MatterDropFabric implements [contract.FabricScopedState]: the anchor
// fabric's removal wipes the datastore and the anchor; any other fabric's
// leaves it. A refresh or a push in flight then calls no port and commits
// nothing more. A second call, from the administrator's forward or the
// reconcile, changes nothing. The error is the store's: the tables stay
// wiped in memory and the next change saves them ([Datastore.persistLocked]). Mirrors
// JointFabricDatastore.h:323-338 (OnFabricRemoved, ClearAllRecords
// :532-559); as a [contract.FabricScopedState] the node also wipes at its
// reconcile the record of an anchor fabric removed while the datastore
// did not hear of it (R2-P6-store-1114).
func (d *Datastore) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	return d.update(ctx, func() error {
		if d.t.AnchorFabricIndex == 0 || fabricIndex != d.t.AnchorFabricIndex {
			return nil
		}
		d.t = tables{}
		d.gen++
		d.wiping = true
		d.restore, _ = json.Marshal(tables{})
		d.markLocked(allAttrs...)
		return nil
	})
}

// allAttrs are the cluster's attributes, from the generated definition.
var allAttrs = func() []uint32 {
	out := make([]uint32, 0, len(dsdef.Definition.Attributes))
	for _, a := range dsdef.Definition.Attributes {
		out = append(out, a.ID)
	}
	return out
}()

// admits reports whether a request of fabric reaches the datastore: the
// recorded anchor fabric's alone — its index and its identity (root
// public key, fabric id) as the attached fabric table holds that index —
// none while no anchor is recorded or no table is attached. The
// comparison is cached until the next update or AttachNode, the only
// ways the anchor or the table's view can change under the datastore
// (a fabric's removal or install of the index reaches it as
// MatterDropFabric, an update).
func (d *Datastore) admits(ctx context.Context, fabric uint8) bool {
	d.mu.Lock()
	anchor, rpk, fid, table, version := d.t.AnchorFabricIndex, slices.Clone(d.t.AnchorRootPublicKey), d.t.AnchorFabricID, d.fabrics, d.version
	if anchor == 0 || fabric != anchor || table == nil || len(rpk) == 0 {
		d.mu.Unlock()
		return false
	}
	if d.checked {
		ok := d.admitted
		d.mu.Unlock()
		return ok
	}
	d.mu.Unlock()
	f, err := table.GetFabric(ctx, anchor)
	ok := err == nil && bytes.Equal(f.RootPublicKey, rpk) && f.FabricID == fid
	d.mu.Lock()
	// A table that answered an error decides nothing: the verdict is
	// cached only for an answer, so a busy store or a cancelled request
	// does not lock the anchor out until the next update (Fable REWORK-7).
	if d.version == version && err == nil {
		d.checked, d.admitted = true, ok
	}
	d.mu.Unlock()
	return ok
}

// MatterReadAllowed implements [contract.ReadAccessDeclarer]: a request
// reads the cluster's attributes only from the fabric the datastore
// admits ([Datastore.admits]); the universal globals (0xFFF8-0xFFFD) are
// every request's. The endpoint dispatcher asks it before the read and
// every subscription report, and answers a refused concrete path
// UNSUPPORTED_ACCESS and leaves the attribute out of a wildcard (decision
// 152, R2-P6-sec-1103). A read without a request — the host's — is
// admitted.
func (d *Datastore) MatterReadAllowed(ctx context.Context, attrID uint32) bool {
	_, fabric, request := im.LookupFabricFilter(ctx)
	if !request || (attrID >= 0xFFF8 && attrID <= 0xFFFD) {
		return true
	}
	return d.admits(ctx, fabric)
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a request
// of a fabric the datastore does not admit ([Datastore.admits]) reads
// every list empty and every other attribute at its zero value. Behind the
// endpoint dispatcher such a request never gets here
// ([Datastore.MatterReadAllowed] refuses it first); the empty values are
// the fallback for a dispatcher that does not ask a
// [contract.ReadAccessDeclarer] — no attribute content leaves either way.
// No attribute of the cluster is nullable. Owner decision 129,
// R2-P6-sec-1103.
func (d *Datastore) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	if _, fabric, request := im.LookupFabricFilter(ctx); request && !d.admits(ctx, fabric) {
		if v, ok := withheld(attrID); ok {
			return v, true
		}
	}
	return d.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
}

// withheld is what a request the datastore does not admit reads of attrID:
// every attribute but the universal globals (0xFFF8-0xFFFD, the rule of
// im/read.go isGlobalAttributeID), so an attribute a later schema adds is
// withheld too — deny by default. A list reads empty, another attribute
// of today's definition its zero value; an attribute this switch does not
// know yet reads null (R2-P6-sec-1103, Fable RECORD-11).
func withheld(attrID uint32) (any, bool) {
	if attrID >= 0xFFF8 && attrID <= 0xFFFD {
		return nil, false
	}
	switch attrID {
	case dsdef.AttrAnchorRootCa:
		return []byte{}, true
	case dsdef.AttrAnchorNodeId:
		return uint64(0), true
	case dsdef.AttrAnchorVendorId:
		return uint16(0), true
	case dsdef.AttrFriendlyName:
		return "", true
	case dsdef.AttrStatus:
		return StatusEntry{}, true
	case dsdef.AttrGroupKeySetList:
		return spec.List[GroupKeySet]{}, true
	case dsdef.AttrGroupList:
		return spec.List[GroupEntry]{}, true
	case dsdef.AttrNodeList:
		return spec.List[NodeEntry]{}, true
	case dsdef.AttrAdminList:
		return spec.List[AdminEntry]{}, true
	case dsdef.AttrEndpointGroupIdList:
		return spec.List[EndpointGroupIDEntry]{}, true
	case dsdef.AttrEndpointBindingList:
		return spec.List[EndpointBindingEntry]{}, true
	case dsdef.AttrNodeKeySetList:
		return spec.List[NodeKeySetEntry]{}, true
	case dsdef.AttrNodeAclList:
		return spec.List[ACLEntry]{}, true
	case dsdef.AttrNodeEndpointList:
		return spec.List[EndpointEntry]{}, true
	}
	return nil, true
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the anchor
// fabric, when one is recorded — everything the datastore holds is the
// Joint Fabric's.
func (d *Datastore) MatterHeldFabrics() []uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.t.AnchorFabricIndex == 0 {
		return nil
	}
	return []uint8{d.t.AnchorFabricIndex}
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

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (d *Datastore) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return d.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches the commands; a host's handler runs in its
// place ([spec.SelfHeld.Handle]).
func (d *Datastore) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &d.SelfHeld, cmdID, fields)
}
