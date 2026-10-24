// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import "context"

// The persistence port. Each interface below is one area of the node's
// nonvolatile state, named after the Matter concept it holds, with exactly
// the methods the module's consumers call; [Port] is their sum. A consumer
// names the area it uses, never the whole port (ADR 0026 principle 1); the
// node, which wires every consumer, holds a [Port].
//
// Two implementations ship: [Store] on SQLite and store/memstore in
// memory. A host's own backend implements the interfaces and proves it with
// the contract tests in store/storetest. What every implementation
// guarantees:
//
//   - Every method is atomic: a multi-row write (ReplaceACL,
//     ReplaceDefaultOTAProviders, SaveDoorLockAuth, RemoveGroupKeySet, …)
//     is applied whole or not at all, and a concurrent reader sees the state
//     before it or after it.
//   - Removing a fabric removes everything that fabric owns: its identity,
//     group key sets and key map, group table, groupcast groups, ACL,
//     resumption records and default OTA providers. Server subscriptions,
//     thermostat presets, door-lock records, settings and counters are not
//     fabric-owned rows and stay (the bridge deletes a fabric's
//     subscriptions itself).
//   - A row that names a fabric the store does not hold is refused.
//   - The fabric index is allocated from a counter that only moves forward
//     and wraps after 254, skipping occupied indexes (matter.js
//     FabricManager #nextFabricIndex).
//   - Returned slices are the caller's: mutating one never changes the store.
//
// matter.js has the same split one level down: a StorageService
// (packages/general/src/storage/StorageService.ts:37) resolves a driver —
// file, SQLite, memory (MemoryStorageDriver.ts:13) — behind the abstract
// StorageDriver get/set/values (StorageDriver.ts:26-42), and the server
// node receives it through its environment (packages/node/src/node/server/
// ServerEnvironment.ts:158, :171-174). Each fabric's state lives in a
// context of its own (createContext("fabric-<index>"), FabricManager.ts:251)
// whose clearAll runs when the fabric is deleted (FabricManager.ts:262-271);
// the cascade above is that clearAll. Go types the port by area instead of by key-value
// context, so a backend stores records, not opaque values.

// Fabrics is the fabric table (Matter §11.18.5, FabricDescriptorStruct).
// AddFabric allocates the index from the persisted next-fabric-index
// counter when the record carries none.
type Fabrics interface {
	AddFabric(ctx context.Context, rec FabricRecord) (uint8, error)
	// GetFabric returns [ErrFabricNotFound] for an index the table does
	// not hold.
	GetFabric(ctx context.Context, fabricIndex uint8) (FabricRecord, error)
	// ListFabrics returns every fabric by index ascending.
	ListFabrics(ctx context.Context) ([]FabricRecord, error)
	UpdateFabricLabel(ctx context.Context, fabricIndex uint8, label string) error
	UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error
	UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error
	// RemoveFabric removes the fabric and every row it owns; see the
	// cascade above.
	RemoveFabric(ctx context.Context, fabricIndex uint8) error
}

// Identity is the node's operational identity on each fabric: NOC, ICAC,
// operational key and IPK.
type Identity interface {
	UpsertIdentity(ctx context.Context, rec IdentityRecord) error
	// GetIdentity returns [ErrIdentityNotFound] for a fabric without one.
	GetIdentity(ctx context.Context, fabricIndex uint8) (IdentityRecord, error)
}

// ACL is the AccessControl cluster's per-fabric entry list (Matter §9.10).
type ACL interface {
	// ListACL returns the fabric's entries in list order.
	ListACL(ctx context.Context, fabricIndex uint8) ([]ACLEntry, error)
	// ReplaceACL swaps the fabric's entries for entries, renumbering
	// Position 0..n-1 in input order.
	ReplaceACL(ctx context.Context, fabricIndex uint8, entries []ACLEntry) error
}

// GroupKeys is GroupKeyManagement's key sets and key map (Matter §11.2).
type GroupKeys interface {
	UpsertGroupKeySet(ctx context.Context, rec GroupKeySet) error
	// GetGroupKeySet returns [ErrGroupKeySetNotFound] for a missing set.
	GetGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) (GroupKeySet, error)
	// ListGroupKeySets returns the fabric's sets by GroupKeySetID ascending.
	ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]GroupKeySet, error)
	// RemoveGroupKeySet removes the set and the key-map entries naming it.
	RemoveGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) error
	// RemoveGroupKeysByFabric removes every set and key-map entry of the
	// fabric.
	RemoveGroupKeysByFabric(ctx context.Context, fabricIndex uint8) error
	SetGroupKeyMapping(ctx context.Context, m GroupKeyMapping) error
	RemoveGroupKeyMapping(ctx context.Context, fabricIndex uint8, groupID uint16) error
	// ListGroupKeyMappings returns the fabric's key map by GroupID
	// ascending.
	ListGroupKeyMappings(ctx context.Context, fabricIndex uint8) ([]GroupKeyMapping, error)
}

// Groups is the group table (GroupKeyManagement.GroupTable, the Groups
// cluster's membership) and the Groupcast cluster's groups. groups.Store is
// this area plus the fabric and key-set reads the group manager needs.
type Groups interface {
	// UpsertGroupTableEntry refuses an entry without endpoints.
	UpsertGroupTableEntry(ctx context.Context, e GroupTableEntry) error
	RemoveGroupTableEntry(ctx context.Context, fabricIndex uint8, groupID uint16) error
	// ListGroupTable returns the fabric's entries by GroupID ascending.
	ListGroupTable(ctx context.Context, fabricIndex uint8) ([]GroupTableEntry, error)
	UpsertGroupcastGroup(ctx context.Context, g GroupcastGroup) error
	RemoveGroupcastGroup(ctx context.Context, fabricIndex uint8, groupID uint16) error
	// ListGroupcastGroups returns the fabric's groups by GroupID ascending.
	ListGroupcastGroups(ctx context.Context, fabricIndex uint8) ([]GroupcastGroup, error)
}

// Resumption is the CASE session-resumption records, one per (fabric,
// peer), found by resumption id or by peer.
type Resumption interface {
	// UpsertResumption refuses a ResumptionID that is not 16 bytes and a
	// SharedSecret that is not 32.
	UpsertResumption(ctx context.Context, rec ResumptionRecord) error
	// GetResumptionByID and GetResumptionByPeer return
	// [ErrResumptionNotFound] on a miss.
	GetResumptionByID(ctx context.Context, resumptionID []byte) (ResumptionRecord, error)
	GetResumptionByPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (ResumptionRecord, error)
	RemoveResumption(ctx context.Context, fabricIndex uint8, peerNodeID uint64) error
	RemoveResumptionsByFabric(ctx context.Context, fabricIndex uint8) error
}

// ServerSubscriptions is the server subscriptions of active CASE sessions,
// kept so they can be re-established after a restart (ADR 0008). The
// payload is opaque to the store.
type ServerSubscriptions interface {
	SaveServerSubscription(ctx context.Context, subscriptionID uint32, fabricIndex uint8, peerNodeID uint64, payload []byte) error
	DeleteServerSubscription(ctx context.Context, subscriptionID uint32) error
	DeleteServerSubscriptionsByFabric(ctx context.Context, fabricIndex uint8) error
	// LoadServerSubscriptions returns every payload, the least recently
	// saved first.
	LoadServerSubscriptions(ctx context.Context) ([][]byte, error)
	ClearServerSubscriptions(ctx context.Context) error
}

// Settings is the node's string settings and its counters. The keys are
// the callers' (the Setting… and MetadataKey… constants, each cluster's
// own); a counter is a uint64 at most 1<<62.
type Settings interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	SetSetting(ctx context.Context, key, value string) error
	GetMetadataCounter(ctx context.Context, key string) (value uint64, ok bool, err error)
	SetMetadataCounter(ctx context.Context, key string, value uint64) error
}

// OTA is the OTA Software Update Requestor's nonvolatile state: the
// fabric-scoped DefaultOTAProviders and the requestor's own encoding of the
// rest.
type OTA interface {
	// ListDefaultOTAProviders returns every fabric's entries, by fabric
	// and then in the order they were written.
	ListDefaultOTAProviders(ctx context.Context) ([]OTAProviderLocation, error)
	ReplaceDefaultOTAProviders(ctx context.Context, fabricIndex uint8, entries []OTAProviderLocation) error
	LoadOTARequestorState(ctx context.Context) (state []byte, ok bool, err error)
	SaveOTARequestorState(ctx context.Context, state []byte) error
}

// ThermostatPresets is the Thermostat server's persisted presets and active
// preset handle, per endpoint (the host's key for it).
type ThermostatPresets interface {
	LoadThermostatPresets(ctx context.Context, endpointKey string) (presets, activeHandle []byte, ok bool, err error)
	SaveThermostatPresets(ctx context.Context, endpointKey string, presets, activeHandle []byte) error
}

// DoorLocks is the DoorLock server's user database, credentials and
// schedules, per endpoint. (The lock's credential key is a setting.)
type DoorLocks interface {
	// LoadDoorLockAuth returns saved false until SaveDoorLockAuth ran for
	// the endpoint; users, credentials and schedules come back by index.
	LoadDoorLockAuth(ctx context.Context, endpointKey string) (auth DoorLockAuth, saved bool, err error)
	// SaveDoorLockAuth replaces the endpoint's records; a duplicate index
	// is refused.
	SaveDoorLockAuth(ctx context.Context, endpointKey string, auth DoorLockAuth) error
}

// ICD is the ICD Management server's nonvolatile state: the registered
// Check-In clients with their keys, per fabric, and the ICDCounter. A
// node that is no ICD never touches it.
type ICD interface {
	// ListICDRegistrations returns every fabric's registrations, by
	// fabric and then in the order they first registered.
	ListICDRegistrations(ctx context.Context) ([]ICDRegistration, error)
	// ReplaceICDRegistrations replaces one fabric's registrations; a
	// removed fabric takes its registrations along.
	ReplaceICDRegistrations(ctx context.Context, fabricIndex uint8, regs []ICDRegistration) error
	// LoadICDCounter returns ok false before the first SaveICDCounter.
	LoadICDCounter(ctx context.Context) (counter uint32, ok bool, err error)
	SaveICDCounter(ctx context.Context, counter uint32) error
}

// Port is the whole persistence port: every area. [Store] and
// store/memstore implement it; node.Config.Store.Credentials takes one.
type Port interface {
	Fabrics
	Identity
	ACL
	GroupKeys
	Groups
	Resumption
	ServerSubscriptions
	Settings
	OTA
	ThermostatPresets
	DoorLocks
	ICD
}

var _ Port = (*Store)(nil)
