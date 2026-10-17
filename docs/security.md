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
   live in that database **unencrypted**. File location, permissions and
   encryption at rest are the host's.

## Device side

| Threat | Control | Source |
| --- | --- | --- |
| Guessing the setup passcode | 20 failed PASE attempts close the window; the lockout lasts 15 minutes and doubles up to 4 hours; opening a new window resets the budget. ~10^8 passcodes against 80 guesses an hour. | `bridge/securechannel.go` (`paseMaxErrors`, `paseLockoutCooldown`) |
| Two commissioners racing | one PASE handshake at a time; a second is ignored until the first ends or 60 s pass | `bridge/securechannel.go` (`claimPaseInFlight`) |
| An abandoned commissioning | the fail-safe expires and rolls back what it covered: the pending key, trust root and CSR state and, if AddNOC completed, the new fabric with its ACL entries and group keys | `cluster/core/general_commissioning.go` |
| One fabric reading another's data | fabric-scoped attributes filter by the accessing fabric; fabric-sensitive events (AccessControl, Groupcast, CommissionerControl) are filtered and need Administer or Manage | `im/read.go`, `endpoint/dispatcher.go` |
| An operation beyond a subject's rights | every read, write, invoke and subscribe is checked against the ACL for the subject, the endpoint and the cluster's privilege | `endpoint/dispatcher.go`, `cluster/core/access_control.go` |
| Group traffic from outside the group | group messages are authenticated with the group's operational key and checked against the Group ACL entries; per-key, per-sender counters reject replays | `groups/`, [ADR 0009](adr/0009-groups-and-group-messaging.md) |
| A new controller while the host cannot attest | `Node.SetCommissioningBlocked` refuses every new commissioning, including a paired controller's multi-admin window; paired controllers keep working | `node/node.go`, `bridge/commissioning_window.go` |

**Attestation of the node itself.** With no chain configured, the node
serves the **CSA test chain** (vendor 0xFFF1): pairable with every
ecosystem's development mode, **not shippable**. A product loads its
vendor chain with `attestation.LoadChainFiles` and serves it through
`node.Config.Attestation`; the reference daemon's `--attestation-dir`
shows the wiring. A chain that does not load should block commissioning
rather than fall back silently.

## Controller side

| Concern | Behaviour | What a host should do |
| --- | --- | --- |
| Device attestation | DAC → PAI → PAA chain, the Certification Declaration and, with a revocation store, the CRLs (Matter Core §6.2.6.1). Findings are resolved by `commissioner.Options.OnAttestationFailure`. | Set `attestation.Reject()` (or a deciding function) in production. **The zero policy proceeds with a warning**, as matter.js's undefined policy does. |
| Trust anchors | `attestation.Trust`: the production PAAs and CD signers of the embedded DCL snapshot (`controller/dcl/snapshot`, refreshed by a reviewed `make update-dcl-snapshot`), plus the CSA test PAAs in `attestation.Development`; the online Distributed Compliance Ledger through `controller/dcl` on top | Use `attestation.Production` for real deployments, with a `dcl.CertificateStore` for the CRL revocation check — the snapshot holds no revocation data. Nothing contacts the ledger unless the host builds a client. |
| The fabric's root key | held in the fabric record in the host's store unless `ca.Config.RootSigner` / `ICACSigner` (and `commissioner.FabricConfig.OperationalSigner` for the operational key) hand it to a `crypto.Signer`: then the record holds the certificates and a key reference only; a three-tier PKI (root offline, an ICAC online) keeps the root out too | Protect the database, or keep the keys in an HSM, KMS or TPM behind a signer; `controller/ca/keyfile` is a passphrase-encrypted file signer and documents how to wire PKCS#11 or a KMS |
| Session resumption state | in memory only; a restart runs full CASE | — |
| Node identities | one fabric per controller database; node ids from the registry | Back up the database with the fabric |

## Not provided

- **Certification.** Nothing here is CSA-certified, and a product built on
  it is not certified by virtue of it ([ADR 0011](adr/0011-certifiability-is-a-goal.md)).
- **Encryption at rest, OS hardening, network segmentation** — the host's.
- **Thread and BLE on the device side** — out of scope.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting on the repository
rather than a public issue.
