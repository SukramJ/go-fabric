# Security model

What the module enforces, what it assumes of the host, and where it
stops, for both roles. The detailed responder-side record — every
enforcement point with its source line, including open questions — is
the [threat model](https://github.com/SukramJ/go-fabric/blob/main/notes/audits/matter-threat-model.md); this page is
the summary an architect needs.

Two facts govern everything below:

1. **It is a library.** Several controls are ports the host wires. Where a
   port is left unwired the module does something defined — usually a
   no-op — and the [host ports guide](guides/ports.md) says which. "The
   host will wire it" is an assumption, not a control.
2. **The store is handed in.** `store.New` takes an open `*sql.DB`.
   Fabric keys, operational keys, group keys and the controller's CA key
   live in that database **unencrypted**, and so do the CASE resumption
   secrets — a device's and the controller's — and a Wi-Fi node's network
   credentials ([what the store holds in clear](#what-the-store-holds-in-clear)). A
   device's store and the controller's seal them when the host hands it a
   sealer whose key lives outside the database (`store.NewSealed`,
   `node.Store.Sealer`, `controller.Config.Sealer`; [sealing at
   rest](#sealing-at-rest)); without one the database is protected by
   nothing beyond the file. File location, permissions and the sealing key
   are the host's.

## Device side

| Threat | Control | Source |
| --- | --- | --- |
| Guessing the setup passcode | 20 failed PASE attempts close the window; the lockout lasts 15 minutes and doubles up to 4 hours; opening a new window resets the budget. ~10^8 passcodes against 80 guesses an hour. The budget and the lockout are the bridge's, not a transport's: attempts over UDP, TCP and BLE count against the same 20, and the lockout refuses PASE on all three — a party in radio range can spend the budget of IP commissioning | `bridge/securechannel.go` (`paseMaxErrors`, `paseLockoutCooldown`; one counter in `recordPaseFailure`, one refusal in `dispatchSecureChannel` ahead of every PASE opcode), `bridge/conn.go` (`Bridge.receive`), `bridge/ble.go` (`connReceiver.deliver` hands a BLE message to the same `receive`) |
| Two commissioners racing | one PASE handshake at a time; a second is ignored until the first ends or 60 s pass, and a PASE message of any other handshake meanwhile is ignored without being counted; a handshake that ran out its 60 s counts as one of the 20 failures when the slot is taken again, so abandoned handshakes start the lockout as failed ones do; while an established PASE session exists that is not closing, no new PASE exchange is accepted (matter.js `PaseServer.ts:80-82`) | `bridge/securechannel.go` (`Bridge.claimPase`, `paseSessionExists`) |
| Anyone in Bluetooth range of a BLE node (`node.Network.BLE`) | the node is on the radio only while it has no fabric, and advertises only while its own window is open; what the radio reaches is PASE under the same attempt limit — the link is neither paired nor encrypted by the platform, and nothing relies on it. One central at a time, and a BTP handshake that must complete within 15 s of the connection or of its latest request. A central that connects and never starts the handshake is disconnected after 15 s; matter.js starts the timer only at the handshake request. A central that has not completed PASE when the window closes is disconnected, as connectedhomeip closes its BLE connections then; matter.js keeps the transport | `node/commissioning.go` (`followOnBLE`), `bridge/ble.go`, `transport/btp/accept.go`; [ADR 0031](adr/0031-ble-and-wifi-provisioning-on-the-device-side.md) |
| An abandoned commissioning | the fail-safe expires and rolls back what it covered: the pending key, trust root and CSR state and, if AddNOC or UpdateNOC completed, the fabric row it wrote — AddNOC inserts the row pending, UpdateNOC marks it pending with the values it replaced, in one write, and only CommissioningComplete commits it, as its last store write; `Node.Stop` expires an armed fail-safe and runs this rollback, `OnFabricRemoved` included, before it returns; a node that goes down inside the window otherwise rolls every pending row back when it starts again, and runs a rolled-back fabric's removal consequences, and for a rolled-back UpdateNOC the removal of the fabric's resumption records — already when the node is constructed, as the rollback itself is — and the host's `OnFabricUpdated`, before it serves; a rollback the store did not let land leaves the row pending, and the next fresh arm, fail-safe end, CommissioningComplete or start rolls it back — a fresh arm before it is recorded, so an arm from the fabric that rollback removes, or whose row it could not roll back, answers FAILURE and arms nothing, and a CommissioningComplete commits the row it finds pending for its fabric or answers FAILURE; an arm made while an expired fail-safe rolls back waits for the rollback; an arm — or a CommissioningComplete — that finds the fail-safe past its deadline runs that rollback itself, and when the rollback ends the arming session — a PASE session, or a CASE session of the fabric the expired window's AddNOC added — the arm answers FAILURE and arms nothing | `cluster/core/general_commissioning.go`, `cluster/core/operational_credentials.go` |
| A commissioning channel left open | the PASE sessions — the one AddNOC moved onto the new fabric included — are closed once the CommissioningComplete response is out (plus 100 ms), when the fail-safe expires and when the commissioning window is revoked; the bridge does all three itself, with or without the node facade, for a host that mounts `core.GeneralCommissioning` and attaches its window | `bridge/pase_close.go`, `bridge/commissioning_window.go` |
| One fabric reading another's data | the dispatcher scopes every fabric-scoped attribute of the model on a request, whatever the server returns: a fabric-filtered read and every read of a fabric-sensitive list serve the accessing fabric's entries alone, and a read that is not fabric-filtered serves an entry of another fabric — every entry, for a session without a fabric — without its fabric-sensitive fields; an entry that names no fabric fails the path (the scope table is generated from the schema snapshot, `schema.AttributeFabricScope`; held by `node`'s `TestFabricSensitiveFieldsWithheldFromOtherFabrics` for the root's servers, the device kit's and a stand-in for every other such list, and by `script/fabricscopecheck`). A fabric-sensitive event record (each event the snapshot marks so, from the same generated table) reaches only the session whose accessing fabric owns it, a commissioning session included, and needs the event's read privilege | `endpoint/dispatcher.go`, `im/fabric_scope.go`, `im/read.go` |
| Writing fabric-scoped state before a fabric exists | a write to a fabric-scoped attribute (AccessControl's ACL and Extension, Binding, GroupKeyMap, DefaultOTAProviders) or an invoke of a fabric-scoped command on a session with no accessing fabric — the commissioning channel before AddNOC included — is answered UnsupportedAccess before a cluster server sees it; the servers refuse it again on their own | `im/write.go`, `im/invoke.go`, `schema/fabric_scoped.go` (`IsFabricScopedInvoke`) |
| An operation beyond a subject's rights | every read, write, invoke and subscribe is checked against the ACL for the subject, the endpoint and the cluster's privilege | `endpoint/dispatcher.go`, `cluster/core/access_control.go` |
| Group traffic from outside the group | group messages are authenticated with the group's operational key and checked against the Group ACL entries; per-key, per-sender counters reject replays for as long as the key is held, however many senders it has had. The key proves membership of the group, not the sender: the source node id is what a holder of the key states, and each id a holder states is one more counter kept — the number of senders per key has no bound, and only a holder of the key can raise it | `groups/` (`groups/decode.go`), [ADR 0009](adr/0009-groups-and-group-messaging.md) |
| A new controller while the host cannot attest | `Node.SetCommissioningBlocked` refuses every new commissioning, including a paired controller's multi-admin window; paired controllers keep working | `node/node.go`, `bridge/commissioning_window.go` |

**Attestation of the node itself.** With no chain configured, the node
serves the **CSA test chain** (vendor 0xFFF1): pairable with every
ecosystem's development mode, **not shippable**. A product loads its
vendor chain with `attestation.LoadChainFiles` and serves it through
`node.Config.Attestation`; the reference daemon's `--attestation-dir`
shows the wiring. A chain that does not load should block commissioning
rather than fall back silently.

### What the store holds in clear

A device's store from `store.New` keeps these secrets as they are, with
no encryption of its own; one from `store.NewSealed` seals every one of them
([sealing at rest](#sealing-at-rest)):

| Secret | Where |
| --- | --- |
| each fabric's operational private key and its IPK | `matter_node_identities` |
| the group key sets, up to three epoch keys each | `matter_group_keys` |
| on a JointFabricDatastore server given a store: the joint fabric's group key sets | `matter_settings`, keys `jointfabric.datastore:<key>`, as JSON |
| the CASE resumption records: one shared secret per fabric and peer, which lets a returning peer skip the full CASE handshake | `matter_resumption`, column `shared_secret` |
| on a Wi-Fi node (`node.Network.WiFi`): the committed networks, each SSID with its credentials — a secret of the home network, not only of this node | `matter_settings`, key `netcomm.wifi.networks`, as JSON |
| while an UpdateNOC waits for CommissioningComplete: the operational private key it replaced, so a restart inside the fail-safe window can restore it | `matter_node_identities`, column `prior_private_key` (since v8); cleared when CommissioningComplete commits the update or the fail-safe's end or a start rolls it back. A database a release before v8 left in the middle of an UpdateNOC holds it, with the IPK, in `matter_settings`, key `operational_credentials.pending_noc_update`, until the first start clears it |

Whoever can read the database file, or a copy of it — a backup, a support
bundle — reads all of them, as does the host process. `store.OpenSQLite`
creates the file readable by its owner only (0600) and leaves the mode of
an existing file alone; a database the host opens itself has the
permissions the host gave it.

Two kinds of secret are sealed before they are stored: the keys of an ICD
node's Check-In registrations and DoorLock credential data (AES-128-GCM).
The node keeps both sealing keys as settings of the same database
(`icd_management.key`, `door_lock.credential_key`), so the sealing keeps
those secrets out of a read of one table, not from a reader of the whole
file.

The factory reset (`node.Node.FactoryReset`, `controller.Controller.Erase`)
empties every `matter_*` table — the node's endpoint records among them,
when the node keeps them in the same file — and then rewrites the SQLite file
(`VACUUM`) and empties its write-ahead log (`wal_checkpoint(TRUNCATE)`):
afterwards none of the erased bytes — nor those of rows deleted before the
reset — are in the database file, its `-wal` or its `-shm`, measured in both
the rollback-journal and the WAL mode. When that scrub cannot finish —
another connection's reader holds the `-wal`, a full disk fails the
`VACUUM` — the reset still completes (the rows are gone; a node pins its
fresh UniqueID) and its error wraps `store.ErrScrubIncomplete`: the erased
bytes may remain in the file until `store.Scrub` of it succeeds, which the
host can run again once the reader is gone. Copies the module does not see — the
flash controller's wear-levelled blocks, file-system snapshots, the free
blocks of a deleted journal, backups — are outside what it can promise.
An endpoint store the host supplies (`node.Config.Store.Endpoints`) on a
database file of its own is erased row by row and not scrubbed: scrubbing
that file is the host's (`store.Scrub`).

### Sealing at rest

`store.NewSealed(ctx, db, sealer)` returns a store that seals every secret
of the table above, the ICD keys, DoorLock credential data (over the lock's
own encryption), the two sealing keys, the pending-NOC settings of a
release before v8 and the JointFabricDatastore record (settings under
`jointfabric.datastore:`, which carry the joint fabric's group key sets),
under a `store.Sealer` the host supplies; `store.NewAESGCMSealer(key)` is
the standard-library one (AES-256-GCM, a random nonce per value, a 32-byte
key). The key must live outside the database — a file of its own, the
operating system's keychain, a TPM or a KMS: only then are the secrets in a
copy of the file, its free pages and its `-wal` included, unreadable without
it; the rest of the file — certificates, fabric and node ids, ACLs — stays
readable. Each value is bound to its table, column and row, so a value moved
to another row or column does not open.

Sealing protects the confidentiality of the secrets in a copy of the
database. It gives no integrity or rollback protection: the non-secret
columns and the seal marks are not authenticated, and an older sealed value
of the same row, written back into the file, opens.

The first `NewSealed` marks the database with a seal canary: a reserved
row of `matter_settings` holding a known value sealed under the sealer. On
the way in it seals every clear row and writes the canary in one
transaction, then scrubs the file as the factory reset does, so the clear
values of an existing database do not stay behind — measured in WAL mode,
in the rollback journal's `DELETE` (SQLite's default), `TRUNCATE` and
`PERSIST` modes and under `locking_mode=EXCLUSIVE` (the scrub truncates a
kept `-journal`). Copies taken before the upgrade — backups, snapshots, the
blocks of a truncated `-wal` or `-journal` — keep the clear values, as for
the factory reset above. When the scrub cannot finish — a full disk, a
reader holding the `-wal` — `NewSealed` still returns the sealed store: the
scrub stays owed (`Store.ScrubOwed`), the node logs one warning, and every
later `NewSealed` runs it again. A host that wants to fail closed checks
`ScrubOwed` and stops.

After that, while the store is open, it is strict: it writes no secret in
clear, and a secret row it reads without the seal mark — a mark edited in
the file, a clear row planted — is refused with `store.ErrUnseal`, never
read as the secret; so is a value under another key or moved to another
row. A database that carries the canary is held to the same rule at every
open: `NewSealed` opens the canary first and refuses a clear secret row
instead of sealing it, and a store without the sealer (`store.New`)
refuses to write a secret into it (`store.ErrNoSealer`) and reads a sealed
one as `store.ErrNoSealer`. The canary is not tamper protection: whoever
can write the file can delete its row, and the database then counts as
never sealed. Every store of the database needs the sealer; a concurrent
writer without it on the same database is not supported. A secret of the
wrong length is refused on write and on read, in both stores, with or without
a sealer (the resumption secret 32 bytes, the IPK 16, the epoch keys 16, a
non-empty private key; an empty optional epoch key is stored as absent); a
key set row of the wrong length left by an earlier release is left out of
`ListGroupKeySets`, so the fabric's other key sets stay usable. Migration v10's `Down` refuses a database
that holds a sealed row.

Wiring: set `node.Store.Sealer` beside `Path` or `DB` — the node applies
the schema, opens its credential store with `store.NewSealed` and `New`
returns its error. A host that builds the store itself applies the schema
(`store.Apply`), calls `store.NewSealed` and hands the store over as
`node.Store{DB: db, Credentials: sealed}` (then without `Sealer`). A wrong
key fails `store.NewSealed`, and with it `New`, with `store.ErrUnseal`
before anything is written: `NewSealed` opens the canary first — on a
database the first `NewSealed` has not marked yet, one sealed value the
database already holds. Provisioning the key is the host's; to rotate it,
the host calls `store.Reseal(ctx, db, old, new)` while no store is open on
the file: one transaction opens every sealed value and the canary under
the old sealer and seals them under the new one, the canary last, then the
file is scrubbed of the values under the old key (a scrub that does not
finish is `store.ErrScrubIncomplete` after the commit and is owed to the
next `NewSealed`). The transaction is atomic only with a journal:
`journal_mode=OFF` is not supported, as a Reseal that fails part-way
under it can leave the file half-moved. There is no unseal: the way back to a clear database is
a factory reset, which drops the canary with every other row, or a restore.
A node whose sealed store does not open — `New` failed with
`store.ErrUnseal`, the key lost or a value tampered with — has no
`FactoryReset` to call: the host erases the database itself with
`store.New(db).Erase(ctx)` on the node's `*sql.DB`, which needs no sealer,
empties every `matter_*` table (the endpoint records among them) and
scrubs the file, or it removes the file; the next `New` starts
uncommissioned.
`Reseal` moves the device store's rows only, not the controller's node
registry on the same file: a controller's database rotates with
`controller.Reseal(ctx, db, old, new)`, which reseals both stores in one
transaction and scrubs once, and a registry kept alone with
`sqlstore.Reseal`.

The controller takes the same sealer as `controller.Config.Sealer` (opt-in,
off by default): `New` opens its store with `store.NewSealed` and its node
registry with `sqlstore.NewSealed`, under the same rules — a database
written without a sealer is sealed on the way in and its file scrubbed
(each store records the scrub it owes with its sealing; a scrub that did
not finish leaves both stores open, owed — `ScrubOwed` on each — and is
run by the next `New`), another key fails `New` with `store.ErrUnseal`,
and a store without the sealer reads a sealed secret as
`store.ErrNoSealer` (so `New` without it fails reading the sealed CA). The
registry carries its own seal canary (schema v3, the reserved
`matter_client_metadata` row `sqlstore.seal_canary`): `sqlstore.NewSealed`
opens it first, a marked registry refuses a clear secret value, and
`sqlstore.New` refuses to write a secret into it. The controller's factory
reset (`Controller.Erase`) drops it with the registry's last row. What a controller's database holds, sealed or
not:

| Sealed under `Config.Sealer` | Where |
| --- | --- |
| the CA: root (and ICAC) private key with its certificates | `matter_settings`, key `controller.ca` |
| the controller's fabric: IPK and operational private key with its NOC | `matter_settings`, key `controller.ca.fabric` |
| the CASE resumption records: one shared secret per node | `matter_resumption`, column `shared_secret` |
| the group key sets the controller writes to its nodes | `matter_group_keys` |
| a node's kept onboarding code (its setup passcode) | `matter_client_values`, cluster `0xFFFFFFFC`, attribute 0 |
| the controller's ICD Check-In key and pending key on a node | `matter_client_values`, cluster `0xFFFFFFFD`, attributes 0 and 1 |

| Not sealed | Where |
| --- | --- |
| the nodes' attribute values and versions, their commissioning records, the onboarding code's source, time and checks, an ICD registration's counters, subject and client type | `matter_client_values`, `matter_client_versions` |
| the DCL trust store a host keeps there (`controller.dcl.trust`) and every other setting | `matter_settings` |
| a CA kept under another `ca.SettingStore.Key` than the default | `matter_settings` |

With `Config.CAKey` (`ca.Config.RootSigner`) the root key is not in the
database at all.

*Source: `store/seal.go` (`Sealer`, `AESGCMSealer`, `NewSealed`;
`TestSealedStoreLeavesNoClearSecret`, `TestNewSealedSealsAClearDatabase`,
`TestSealedStoreWrongKeyOrNoSealer`, `TestSealedValueMovedDoesNotOpen`,
`TestSealedStoreRefusesClearRows`, `TestNewSealedLeavesNoClearSecretPerJournalMode`,
`TestReadSideLengthChecks`, `TestMigrationV10DownRefusesSealedRows`), `store/migrations/v10.up.sql`,
`store/schema.sql` (`matter_resumption`), `store/resumption.go` (`SharedSecret`), `store/erase.go` (`Erase`, `Scrub`, `ErrScrubIncomplete`; `TestStoreEraseLeavesNoResidue`, `TestStoreEraseWhoseCheckpointAReaderBlocks`), `node/factory_reset.go` (`eraseStores`; `TestFactoryResetLeavesNoEndpointRecordResidue`, `TestFactoryResetWhoseScrubAReaderBlocksStillPins`), `cluster/core/network_commissioning_wifi.go` (`wifiNetworksKey`, `storedWiFiNetwork`, `WiFiNetworkCommissioning.saveCommittedLocked`), `store/icd_cipher.go` (`SettingICDKey`), `store/doorlock.go` (`SettingDoorLockCredentialKey`), `store/sqlite.go` (`OpenSQLite`).*

## Controller side

| Concern | Behaviour | What a host should do |
| --- | --- | --- |
| Device attestation | DAC → PAI → PAA chain, the Certification Declaration and, with a revocation store, the CRLs (Matter Core §6.2.6.1). Findings are resolved by `commissioner.Options.OnAttestationFailure`. | Set `attestation.Reject()` (or a deciding function) in production. **The zero policy proceeds with a warning**, as matter.js's undefined policy does — and the steps that follow attestation include network provisioning, so the Wi-Fi credentials or the Thread dataset of `commissioner.Options` (`WiFiNetwork`, `ThreadNetwork`) then go to a device whose attestation failed (`controller/commissioner/flow.go`, `flow.initializeSteps`, the step order). |
| Trust anchors | `attestation.Trust`: the CSA's five built-in CD signing keys and the Matter test CD signer (`attestation.KnownCDSigner`), the production PAAs and CD signers of the embedded DCL snapshot (`controller/dcl/snapshot`, refreshed by a reviewed `make update-dcl-snapshot`), plus the CSA test PAAs in `attestation.Development`; the online Distributed Compliance Ledger through `controller/dcl` on top | Use `attestation.Production` for real deployments, with a `dcl.CertificateStore` for the CRL revocation check — the snapshot holds no revocation data. Nothing contacts the ledger unless the host builds a client. With one, the controller also fetches the URLs the ledger's records name: CRL distribution points over `http:` or `https:` — the schema text matter.js carries allows both (`packages/types/src/dcl/device-attestation-revocation.ts:98-101` at db021da0) and matter.js fetches either (`DclCertificateService.ts:1639-1641`); the CRL is authenticated by its signature (`controller/dcl/revocation.go`, `CertificateStore.downloadCRL`) — and OTA images over `https:` only (`controller/dcl/ota.go`, `OTAVersions.Download`). The URLs the GitHub API's answers name, when the store mirrors the development certificates (`controller/dcl/github.go`), are treated the same way; the configured `GitHubConfig.APIURL` is the host's. Without a host `http.Client`, such a download connects to public addresses only: a loopback, private, link-local, unspecified or multicast address (IPv4, IPv6, IPv4-mapped) is refused when it is dialled — only those five classes; other special-use ranges such as 100.64.0.0/10, NAT64 64:ff9b::/96 or 6to4 2002::/16 are not refused; the check runs for the URL itself and every redirect hop, after name resolution (`newGuardedClient` in `controller/dcl/client.go`); `dcl.Client.AllowPrivateDestinations` lifts that for a lab (`softwareupdate.Images` takes the test-net client's switch for a test-net record). A host's `dcl.Client.HTTPClient` is the host's policy and is not guarded. The environment's proxy stays in use: a URL or redirect hop whose host is `localhost` or an IP address of those classes is refused before it is handed to the proxy — the host judged as net/http sends it (mapped to ASCII for a CONNECT), lower-cased, without leading or trailing dots, an IPv4 address in any inet_aton-style spelling (decimal, octal, hexadecimal, short forms) included; no proxy is handed a host outside ASCII — and a host name sent through the proxy is the proxy's to resolve and connect to. An image the host names by URL itself (`softwareupdate.Images.UpdateInfoFromURL`, `OTAVersions.DownloadHostURL`) may use `http:` and any address. A download that starts at an `https:` URL follows no redirect away from `https:`, whatever the host's `http.Client` (`downloadClient` in `controller/dcl/client.go`). |
| The fabric's root key | held in the fabric record in the host's store unless `ca.Config.RootSigner` / `ICACSigner` (and `commissioner.FabricConfig.OperationalSigner` for the operational key) hand it to a `crypto.Signer`: then the record holds the certificates and a key reference only; a three-tier PKI (root offline, an ICAC online) keeps the root out too | Protect the database, or keep the keys in an HSM, KMS or TPM behind a signer; `controller/ca/keyfile` is a passphrase-encrypted file signer and documents how to wire PKCS#11 or a KMS |
| Session resumption state | kept in the controller's database: one record per node it has had a CASE session with, holding that session's shared secret, so a restarted controller resumes instead of running full CASE (`controller/controller.go`, the `handshake.NewStoreResumption` over the store it opens in `New`; table `matter_resumption`). Without `Config.Sealer` the record is in clear, and whoever reads the database, or a copy of it, can resume a session as the controller's node with that device until the record is replaced; with it the record is sealed ([sealing at rest](#sealing-at-rest)). A device persists its records the same way, [in clear](#what-the-store-holds-in-clear) unless its store is sealed | Set `Config.Sealer` with a key outside the database, or protect the database as the fabric's keys: file permissions, an encrypted volume, backups treated as key material |
| Node identities | one fabric per controller database; node ids from the registry | Back up the database with the fabric |
| Kept onboarding codes | off by default. With `Config.KeepOnboardingCodes`, or after `Node.SetOnboardingCode`, the controller's store holds a node's QR or manual pairing code — the device's setup passcode — in plain text, as it holds the fabric's keys. Whoever reads the store or calls `Controller.Inventory()` can read it. The passcode is worth something whenever the device accepts it again: after a factory reset, or in a basic commissioning window. `client.SecretCode` renders as `redacted` under `fmt`, `slog` and `encoding/json`; `Reveal()` returns the text | Protect the database as the keys. An API that serves the inventory hands out setup passcodes: give it the authorisation an export of the fabric's keys would need, and call `Reveal()` only where the code is shown or printed |

## Not provided

- **Certification.** Nothing here is CSA-certified, and a product built on
  it is not certified by virtue of it ([ADR 0011](adr/0011-certifiability-is-a-goal.md)).
- **Encryption of the whole file at rest, OS hardening, network segmentation** — the host's.
- **A Thread radio on the device side** — out of scope: the module never
  joins the Thread mesh. What it serves of Thread on the device side — the
  Thread variant of NetworkCommissioning and ThreadBorderRouterManagement —
  runs against a host's border router over the OTBR REST API
  (`thread/otbr`), a plaintext HTTP link the operational dataset crosses
  in clear ([Thread border router](guides/thread-border-router.md)). BLE
  on the device side is the host's radio (`node.Network.BLE`): its pairing
  and link security are the platform's, and the node is on it only while
  it has no fabric.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting on the repository
rather than a public issue.
