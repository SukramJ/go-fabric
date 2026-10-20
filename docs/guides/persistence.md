# Persistence

A node keeps its state in SQLite. The host opens the database, picks the
driver, and owns the handle. The module embeds the DDL its queries are
written against, applies it, and never opens a database itself. This page
lists what is stored where, what survives a restart, and how schema
changes reach a host.

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
| `matter_diagnostics` | v1 | a single-row counter table with a typed API; the facade keeps its counters in `matter_settings` instead |
| `matter_metadata` | v1 | integer counters: `next_fabric_index`, `event_number_ceiling` |
| `matter_settings` | v1 | string key/value pairs (next section) |
| `matter_persistent_subscriptions` | v1 | unused since v0.3.0; kept so old and new databases have the same schema |
| `matter_group_table` | v2 (v0.2.0) | GroupTable: the endpoints of each group |
| `matter_groupcast_groups` | v2 | the Groupcast cluster's per-group properties |
| `matter_server_subscriptions` | v2 | the subscriptions of active CASE sessions, to re-establish after a restart |
| `matter_ota_default_providers` | v4 (v0.3.0) | the OTA requestor's DefaultOTAProviders |

### Tables of `endpoint/sqlitestore`

| Table | Holds |
| --- | --- |
| `matter_endpoints` | one row per source: its stable key's string form, its scope, its endpoint number and device type |
| `matter_endpoint_allocation` | the high-water mark of endpoint numbers. A number is never reused, because a controller caches accessories by number |

The stable key's string form is the primary key. If a host changes how its
key renders, the row is orphaned and the source gets a new endpoint number.

*Source: `store/schema.sql:18-280`, `store/migrations.go:43-54`, `store/migrations/v1.up.sql:4-6`, `store/metadata.go:15`, `store/settings.go:50-55`, `endpoint/sqlitestore/schema.sql:13-60`.*

### Settings keys

`matter_settings` holds the controller-written attribute values and the
facade's own state.

| Key | Written by | Holds |
| --- | --- | --- |
| `basic_information.node_label`, `basic_information.location` | the facade | the root's NodeLabel and Location as a controller wrote them |
| `basic_information.configuration_version` | the facade | the root's ConfigurationVersion |
| `basic_information.unique_id` | the facade | UniqueID, derived on the first boot and pinned |
| `basic_information.unique_id_rotated` | (deprecated) | read once for a migration and cleared |
| `bridged_node_label.<key>` | the facade | a bridged endpoint's NodeLabel |
| `bridged_configuration_version.<key>` | the facade | a bridged endpoint's ConfigurationVersion |
| `scenes.<key>` | the facade | a bridged endpoint's scene table |
| `binding.<key>`, `binding.@<endpoint>` | the facade | an endpoint's Binding list; `@` and the number for an endpoint without a stable key |
| `user_label.root`, `user_label.aggregator` | the facade | the UserLabel lists of the root and the Aggregator |
| `gendiag.reboot_count` | the facade | RebootCount: 0 on a database's first boot, then one more per boot |
| `gendiag.operational_ms` | the facade | accumulated operational time (`gendiag.operational_hours` is read once as its legacy form) |
| `ota_requestor.state` | `store` | the OTA requestor's state |
| `access_control.extension.<fabric>` | `cluster/core` | AccessControl's Extension list per fabric |
| `matter.fabric.<fabric>.vid.statement`, `.vvsc` | `cluster/core` | the VID verification statement and VVSC per fabric |

`<key>` is the endpoint's stable key in `String()` form, the same identity
endpoint numbers are kept under. A host that keeps scenes and labels
itself sets `Config.EndpointPersistence`, and the `scenes.` and
`bridged_node_label.` keys are then not written.

A host may use the settings table for its own state, as the reference
daemon does with `blind.position`. Pick a prefix that none of the keys
above use.

*Source: `store/settings.go:13-48`, `node/persist.go:22-36`, `node/persist.go:55-75`, `node/persist.go:250-305`, `node/persist.go:323-332`, `node/root.go:39-40`, `store/ota.go:25`, `cluster/core/access_control.go:290-292`, `cluster/core/vid_verification.go:63-65`, `examples/reference-bridge/persist.go:62-86`.*

## What survives a restart

| Survives | Does not survive |
| --- | --- |
| fabrics, NOCs, keys, ACL | live PASE and CASE sessions (controllers reconnect, with resumption) |
| group keys, group table, Groupcast properties | the event log's events (their numbers continue past the stored ceiling) |
| endpoint numbers, labels, ConfigurationVersions, scene tables, bindings | an open commissioning window (an uncommissioned node opens its own again at `Start`) |
| CASE resumption records | the diagnostic event ring behind DiagnosticLogs |
| the subscriptions of active CASE sessions, re-established under their old ids | |
| UniqueID, RebootCount, operational time, OTA default providers | |

A restart does not re-identify the node: UniqueID is pinned, and fabrics
and endpoint numbers are the same.

*Source: `node/node.go:272-281`, `node/node.go:500-535`, `store/schema.sql:250-256`, `node/security.go:256`, `node/security.go:364`, `bridge/bridge.go:85-94`, `node/config.go:259-274`.*

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

The two `controller.ca` keys are `ca.SettingStore`'s default; another
`Key` moves both. Controller-side CASE resumption records are kept in
memory only, so the first command of a run makes a full CASE handshake.

*Source: `controller/ca/persist.go:90-111`, `controller/client/sqlstore/sqlstore.go:4-44`, `controller/client/sqlstore/schema.sql:11-31`, `examples/reference-controller/cli/stack.go:35-41`, `examples/reference-controller/README.md` (The database), `node/client.go:96-101`.*

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

*Source: `store/schema.go:23-90`, `store/migrations.go:22-120`, `controller/client/sqlstore/sqlstore.go:11-16`, `controller/client/sqlstore/schema.go:31-104`, `endpoint/sqlitestore/sqlitestore.go:68-72`, `endpoint/sqlitestore/schema.sql:8-11`.*

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

*Source: `store/doc.go:38-40`, `store/schema.sql:48-57`, `examples/reference-controller/README.md` (The database).*

