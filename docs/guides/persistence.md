# Persistence

A node keeps its state behind a port, `store.Port`. The shipped backend is
SQLite: the host opens the database, picks the driver, and owns the
handle; the module embeds the DDL its queries are written against, applies
it, and never opens a database itself. The second backend, `store/memstore`,
keeps the same state in memory. This page describes the port and what a
backend of a host's own must guarantee, then lists what SQLite stores
where, what survives a restart, and how schema changes reach a host.

## The persistence port

`store.Port` is the sum of one interface per area of the node's state, each
named after the Matter concept it holds and carrying exactly the methods the
module's consumers call:

| Area | Holds | Consumers |
| --- | --- | --- |
| `store.Fabrics` | the fabric table; `AddFabric` allocates the index | OperationalCredentials, the node's CASE identities and commissioning window, the group manager |
| `store.Identity` | per fabric: NOC, ICAC, operational key, IPK | OperationalCredentials, the node's CASE identities |
| `store.ACL` | the AccessControl list per fabric | AccessControl, the bridge's access checks |
| `store.GroupKeys` | key sets and the GroupKeyMap | GroupKeyManagement, OperationalCredentials (the IPK set), the group manager |
| `store.Groups` | the group table and the Groupcast groups (`groups.Store` embeds it) | the group manager |
| `store.Resumption` | CASE resumption records | the operational session manager, the node's CASE initiator |
| `store.ServerSubscriptions` | the subscriptions of active CASE sessions | the bridge's subscription persistence |
| `store.Settings` | string settings and counters | the facade, AccessControl's Extension, VID verification, the group counter, the event-number ceiling |
| `store.OTA` | the OTA requestor's DefaultOTAProviders and state | the OTA requestor |
| `store.ThermostatPresets` | a Thermostat endpoint's presets | the Thermostat server (`store.NewThermostatPresetStore`) |
| `store.DoorLocks` | a lock endpoint's users, credentials and schedules | the DoorLock server |
| `store.ICD` | the ICD Management server's registered Check-In clients with their keys, and the ICDCounter | ICD Management (`node.Config.ICD`) |

A consumer names the areas it uses, never the whole port; the node, which
wires every consumer, holds a `store.Port`.

**Two implementations.**

- `store.New(db)`, SQLite. `Config.Store.Path` and `Config.Store.DB` build
  it; `Config.Store.Credentials` takes one you built.
- `memstore.New()` (`store/memstore`), in memory, for tests and for a small
  daemon that accepts losing its state on restart: a process that exits
  forgets every fabric, and each controller has to commission the node
  again. Pair it with the in-memory endpoint store:

```go
cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()} // epmem: endpoint/memstore
```

`fabrictest` devices run on these two by default; `fabrictest.Options`'s
`StoreOnDisk` puts them on a SQLite file.

**What a backend of the host's own must guarantee.**

- **Atomic per method.** A multi-row write (`ReplaceACL`,
  `ReplaceDefaultOTAProviders`, `SaveDoorLockAuth`, `RemoveGroupKeySet`,
  `RemoveFabric`, …) applies whole or not at all, and a refused write
  changes nothing. The port has no transaction across methods; the
  consumers do not need one.
- **The fabric cascade.** `RemoveFabric` removes everything the fabric
  owns: its identity, key sets, key map, group table, Groupcast groups,
  ACL, resumption records and default OTA providers. Server
  subscriptions, presets, lock records, settings and counters stay; the
  bridge deletes a removed fabric's subscriptions itself. A row naming a
  fabric the store does not hold is refused.
- **Monotonic counters.** The fabric index comes from a counter that only
  moves forward, wraps after 254 and skips occupied indexes, so a removed
  index is not reissued at once (matter.js FabricManager
  `#nextFabricIndex`); SQLite keeps it as the metadata counter
  `next_fabric_index`. A metadata counter holds at most 2^62.
- **The settings' keys are the callers'.** The store keeps any key as it
  is given (the settings keys below); `ota_requestor.state` and
  `door_lock.saved:<key>` are written by the store's own OTA and DoorLock
  methods.
- **Lists in their order** (fabrics by index, key sets and the key map
  and group tables by id, ACL in list order, subscriptions least recently
  saved first), **the not-found sentinels** (`ErrFabricNotFound`,
  `ErrIdentityNotFound`, `ErrGroupKeySetNotFound`, `ErrResumptionNotFound`),
  **copies** (a slice a caller passes or receives is the caller's), and
  **safety for concurrent use**.

`store/storetest` proves it: `storetest.Run(t, open)` runs one table of
cases against any `store.Port`, each on a fresh store from `open`. `store`
runs it against SQLite and `store/memstore` against itself; a host runs it
against its own backend:

```go
func TestMyBackend(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Port { return mybackend.Open(t.TempDir()) })
}
```

matter.js has the same seam one level down: a `StorageService`
(`packages/general/src/storage/StorageService.ts:37`) resolves a driver
(file, SQLite or `MemoryStorageDriver`) behind the abstract
`StorageDriver` (`StorageDriver.ts:26-42`), the server node receives it
through its environment (`packages/node/src/node/server/ServerEnvironment.ts:158`,
`:171-174`), and each fabric's state lives in a `fabric-<index>` context
(`packages/protocol/src/fabric/FabricManager.ts:251`) cleared when the
fabric is deleted (`FabricManager.ts:262-271`). go-fabric types the port by
area instead of by key-value context, so a backend stores records, not
opaque values.

*Source: `store/port.go:8-41`, `store/port.go:49-198`, `store/memstore/memstore.go:4-28`, `endpoint/memstore/memstore.go:4-22`, `store/storetest/storetest.go:4-21`, `store/contract_test.go`, `groups/manager.go:36-44`, `fabrictest/site.go:69-75`, `node/config.go:367-381`.*

## Opening the database

```go
db, err := sql.Open("sqlite", "file:node.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
```

- `foreign_keys(1)` is required. Removing a fabric deletes its rows
  through `ON DELETE CASCADE`, and SQLite ignores foreign keys silently
  with the pragma off.
- `busy_timeout` makes a contended endpoint-number allocation wait
  instead of failing.
- Both pragmas go in the DSN. A pragma set with a statement applies to
  one pooled connection only.

`Config.Store.Path` has the node open a file with exactly this DSN
(modernc.org/sqlite) and close it on `Stop`; `Config.Store.DB` takes a
handle you opened.

`node.New` applies the device schemas (`store.Apply` and
`sqlitestore.Apply`) to the database on every boot. Both scripts are
idempotent.

*Source: `node/store_path.go`, `store/doc.go:8-36`, `node/node.go` (`openStores`).*

## The device database

### Tables of `store` (the `matter_*` schema)

| Table | Since | Holds |
| --- | --- | --- |
| `matter_fabrics` | v1 (v0.1.0) | each fabric: index, fabric id, node id, root certificate and key, vendor, label, compressed fabric id |
| `matter_node_identities` | v1 | per fabric: NOC, ICAC, the private key, the IPK as the commissioner sent it |
| `matter_group_keys` | v1 | group key sets per fabric, up to three epoch keys |
| `matter_group_key_map` | v1, rebuilt in v3 | GroupKeyMap: group id to key set; since v3 without a foreign key, so an entry may name a key set not yet written |
| `matter_acl_entries` | v1 | the AccessControl list per fabric, in order |
| `matter_resumption` | v1 | CASE resumption records with the peer's CATs |
| `matter_diagnostics` | v1 | a single-row counter table, created and seeded but read by nothing (its typed API is removed in v0.10.0); the facade keeps its counters in `matter_settings` |
| `matter_metadata` | v1 | integer counters: `next_fabric_index`, `event_number_ceiling`, and since v0.12.0 an ICD node's `icd_management.counter` |
| `matter_settings` | v1 | string key/value pairs (next section) |
| `matter_persistent_subscriptions` | v1 | unused since v0.3.0; kept so old and new databases have the same schema |
| `matter_group_table` | v2 (v0.2.0) | GroupTable: the endpoints of each group |
| `matter_groupcast_groups` | v2 | the Groupcast cluster's per-group properties |
| `matter_server_subscriptions` | v2 | the subscriptions of active CASE sessions, to re-establish after a restart |
| `matter_ota_default_providers` | v4 (v0.3.0) | the OTA requestor's DefaultOTAProviders |
| `matter_thermostat_presets` | v5 (v0.8.3) | a Thermostat endpoint's presets and active preset handle |
| `matter_doorlock_users`, `matter_doorlock_credentials`, `matter_doorlock_schedules` | v6 (v0.8.4) | a lock endpoint's users, credentials (encrypted) and schedules |
| `matter_icd_registrations` | v7 (v0.12.0) | an ICD node's registered Check-In clients per fabric: check-in node id, monitored subject, key, client type; removed with the fabric. Since v0.12.1 the key is sealed at rest (AES-128-GCM under `icd_management.key`, bound to its row); `store.Upgrade` seals the keys an earlier release stored in clear |

### Tables of `endpoint/sqlitestore`

| Table | Holds |
| --- | --- |
| `matter_endpoints` | one row per source: its stable key's string form, its scope, its endpoint number and device type |
| `matter_endpoint_allocation` | the high-water mark of endpoint numbers. A number is never reused, because a controller caches accessories by number |

The stable key's string form is the primary key. If a host changes how its
key renders, the row is orphaned and the source gets a new endpoint number.

*Source: `store/schema.sql:18-349`, `store/migrations.go:43-54`, `store/migrations/v1.up.sql:4-6`, `store/metadata.go:15`, `store/settings.go:37-42`, `endpoint/sqlitestore/schema.sql:13-60`.*

### Settings keys

`matter_settings` holds the controller-written attribute values and the
facade's own state.

| Key | Written by | Holds |
| --- | --- | --- |
| `basic_information.node_label`, `basic_information.location` | the facade | the root's NodeLabel and Location as a controller wrote them |
| `basic_information.configuration_version` | the facade | the root's ConfigurationVersion |
| `basic_information.unique_id` | the facade | UniqueID, derived on the first boot and pinned |
| `bridged_node_label.<key>` | the facade | a bridged endpoint's NodeLabel |
| `bridged_configuration_version.<key>` | the facade | a bridged endpoint's ConfigurationVersion |
| `scenes.<key>` | the facade | a bridged endpoint's scene table |
| `binding.<key>`, `binding.@<endpoint>` | the facade | an endpoint's Binding list; `@` and the number for an endpoint without a stable key |
| `user_label.root`, `user_label.aggregator` | the facade | the UserLabel lists of the root and the Aggregator |
| `bridged_user_label.<key>` | the facade | a bridged endpoint's UserLabel list, when its device declares the cluster |
| `gendiag.reboot_count` | the facade | RebootCount: 0 on a database's first boot, then one more per boot |
| `gendiag.operational_ms` | the facade | accumulated operational time (`gendiag.operational_hours` is read once as its legacy form) |
| `ota_requestor.state` | `store` | the OTA requestor's state |
| `door_lock.credential_key` | `cluster/lock` | the key the DoorLock server encrypts credential data with |
| `icd_management.key` (`store.SettingICDKey`) | `store` | the key `store.Store` seals the ICD registrations' keys with; created with the first registration |
| `door_lock.saved:<key>` | `store` | a lock endpoint's records were saved at least once |
| `access_control.extension.<fabric>` | `cluster/core` | AccessControl's Extension list per fabric |
| `matter.fabric.<fabric>.vid.statement`, `.vvsc` | `cluster/core` | the VID verification statement and VVSC per fabric |

`<key>` is the endpoint's stable key in `String()` form, the same identity
endpoint numbers are kept under. A host that keeps scenes and labels
itself sets `Config.EndpointPersistence`, and the `scenes.` and
`bridged_node_label.` keys are then not written.

A host may use the settings table for its own state, as the reference
daemon does with `blind.position`. Pick a prefix that none of the keys
above use.

*Source: `store/settings.go:13-35`, `node/persist.go:24-38`, `node/persist.go:57-77`, `node/persist.go:244-299`, `node/persist.go:317-326`, `node/root.go:42-43`, `store/ota.go:25`, `store/doorlock.go:13-24`, `store/icd.go`, `cluster/core/access_control.go:295-297`, `cluster/core/vid_verification.go:65-67`, `examples/reference-bridge/persist.go:21-44`.*

## What survives a restart

| Survives | Does not survive |
| --- | --- |
| fabrics, NOCs, keys, ACL | live PASE and CASE sessions (controllers reconnect, with resumption) |
| group keys, group table, Groupcast properties | the event log's events (their numbers continue past the stored ceiling) |
| endpoint numbers, labels, ConfigurationVersions, scene tables, bindings | an open commissioning window (an uncommissioned node opens its own again at `Start`) |
| CASE resumption records | the diagnostic event ring behind DiagnosticLogs |
| the subscriptions of active CASE sessions, re-established under their old ids (not on an ICD node, whose controllers re-subscribe on a Check-In) | |
| UniqueID, RebootCount, operational time, OTA default providers | |
| an ICD node's Check-In registrations and ICDCounter | |

A restart does not re-identify the node: UniqueID is pinned, and fabrics
and endpoint numbers are the same.

*Source: `node/node.go:326-335`, `node/node.go:665-712`, `store/schema.sql:250-256`, `node/security.go:372`, `node/security.go:483`, `bridge/bridge.go:85-94`, `node/config.go:347-360`.*

## The controller database

A controller keeps its state in the same kind of file. The reference
controller and the quickstart apply `store.Apply` (for the settings table)
and `sqlstore.Apply`:

| Where | Holds |
| --- | --- |
| `matter_settings` `controller.ca` | the CA: root certificate, root key, certificate counter (JSON, matter.js field names) |
| `matter_settings` `controller.ca.fabric` | our fabric: fabric id, root node id, IPK, index, label, our key and NOC |
| `matter_settings` `controller.dcl.trust` | the DCL trust store (reference controller, with `--dcl`) |
| `matter_client_values` | each paired node's attribute values, as TLV |
| `matter_client_versions` | each stored cluster's DataVersion |
| `matter_client_values`, cluster `0xFFFFFFFD` | each node's ICD Check-In registration: key, pending key, counter state, monitored subject, client type |

The two `controller.ca` keys are `ca.SettingStore`'s default; another
`Key` moves both. Controller-side CASE resumption records are kept in
memory only, so the first command of a run makes a full CASE handshake.

*Source: `controller/ca/persist.go:90-111`, `controller/client/sqlstore/sqlstore.go:4-44`, `controller/client/sqlstore/schema.sql:11-31`, `examples/reference-controller/cli/stack.go:35-41`, `examples/reference-controller/README.md` (The database), `node/client.go:115-120`.*

## Schema changes

Each schema ships three ways:

- **`Apply`** runs the whole DDL. It is idempotent and safe on every boot.
  `store.Apply` then runs `store.Upgrade`, which rebuilds what an
  idempotent `CREATE` cannot change (the v3 rebuild of
  `matter_group_key_map`).
- **`Migrations()`** returns the same schema as versioned steps (`v1`,
  `v2`, …), each with `Up` and `Down`, for a host that runs its own
  migration tool. A released step never changes. Pin
  `len(Migrations())` and the last version, so the next release's new step
  fails the pin at the dependency bump instead of at a live write. `v3`'s
  `Down` is lossy.
- **`SchemaFingerprint(db)`** hashes the module's tables and indexes as
  SQLite records them. Compare it with the value the CHANGELOG records for
  the release to know your tables match. A host's own tables and indexes
  do not change it.

| Package | `Schema`/`Apply` | `Migrations` | `SchemaFingerprint` |
| --- | --- | --- | --- |
| `store` | yes | yes | yes |
| `controller/client/sqlstore` | yes | yes (its own list) | yes |
| `endpoint/sqlitestore` | yes | no | no |

The schemas are independent and can be applied in either order.

*Source: `store/schema.go:23-90`, `store/migrations.go:22-123`, `controller/client/sqlstore/sqlstore.go:11-16`, `controller/client/sqlstore/schema.go:31-104`, `endpoint/sqlitestore/sqlitestore.go:68-72`, `endpoint/sqlitestore/schema.sql:8-11`.*

## Backup

The module ships no backup tool. What a backup has to respect:

- **The file holds private keys.** The node's operational keys and the
  controller's CA key are stored unencrypted. The store's documentation
  leaves file-system protection to the operator. Protect the database and
  every copy of it like the keys.
- **Copy a consistent state.** Stop the node first, or use SQLite's own
  online mechanisms (`VACUUM INTO`, the backup API). Copying the file of a
  running node can catch a write half-done.
- **Run one copy at a time.** A restored database is the same node: the
  same fabrics, the same operational identity. Two running copies answer
  as one node under one identity.

*Source: `store/doc.go:38-39`, `store/schema.sql:48-57`, `examples/reference-controller/README.md` (The database).*

