# ADR 0029 — ICD state is split by role, and the ICD client has no transaction model

- **Status**: Accepted (2026-10-25; brief CH, pull request 264)
- **Date**: 2026-10-25
- **Related**: [ADR 0020 — commissioner and controller role](./0020-commissioner-and-controller-role.md),
  [ADR 0026 — architecture principles](./0026-architecture-principles.md),
  `docs/guides/icd.md`, `notes/parity/by_design.md` entries
  `BD-Matter-ICD-RegistrationsInStoreArea`, `BD-Matter-ICDClient-OneOperationSlot`,
  `BD-Matter-ICD-NoNetworkProfiles`.

## Context

matter.js holds the device's ICD registrations and the controller's ICD peers
in one shape, `FabricIcd`, as nonvolatile behaviour state
(`packages/node/src/behaviors/icd-management/IcdManagementServer.ts:777-791`
at `862ad75f`), and runs the client's register, refresh, unregister and
forget as operations of its transaction model
(`packages/node/src/behavior/system/icd/IcdClient.ts:980-991`). Go has
neither behaviour state nor a transaction model, and this module keeps the
device role (`node`, `cluster/core`) and the controller role (`controller/client`)
on separate paths since ADR 0020.

## Decision

1. **Two tables, one codec.** The device's registrations, ICDCounter and keys
   are the `store.ICD` area (`icd.Registrations`, `store/icd.go`); the
   controller's registrations are `icd.PeerTable` in the client store under a
   pseudo-cluster. The Check-In codec (`icd/checkin`, on `secure/aesccm`) is
   shared, and is the only thing shared.
2. **One operation slot per peer.** The ICD client runs one operation at a
   time; a later Unregister or Forget abandons a running Register or refresh
   and the abandoned caller gets `ErrICDAbandoned`. Nothing joins an
   operation.
3. **The mode machine never cycles on its own.** As in matter.js `IcdMode.ts`,
   the host drives sleep and wake through `Node.ICD()`.

## Consequences

- A host's own `store.Port` implements the `ICD` area (a break announced in
  v0.12.0's migration guide); matter.js hosts have no such step because the
  behaviour state travels with the behaviour.
- The transaction and interleaving groups of `IcdClientTest` are not
  portable and are listed in the findings register with this ADR as the
  reason; phase 25 ports what the network profiles make observable.
- The ICD keys are sealed at rest since v0.12.1 (brief CJ 1.25; owner
  decision 2026-10-25), as the DoorLock credentials are (`store/icd_cipher.go`,
  AES-128-GCM under the store-held key `icd_management.key`; `store.Upgrade`
  re-seals clear keys). matter.js keeps them in clear. No protocol change;
  `by_design` `BD-Matter-ICD-RegistrationsInStoreArea`.

## Alternatives considered

- **One `FabricIcd` table for both roles**, as matter.js: rejected because a
  node that is also a controller would mix its own registrations with its
  peers' in one table keyed only by fabric, and the two roles persist in
  different stores here.
- **A transaction model for the ICD client alone**: rejected; it would be the
  only one in the module and would exist for 52 test cases, not for a
  behaviour a controller observes.
