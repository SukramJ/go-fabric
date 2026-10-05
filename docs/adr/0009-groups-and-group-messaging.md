# ADR 0009 — A real Groups server and group message delivery, as matter.js builds them

- **Status**: Accepted
- **Date**: 2026-10-04
- **Supersedes**: [ADR 0004 — Groups stays a stub](./0004-groups-cluster-stays-stub.md)
- **Followed by**: [ADR 0010 — Groupcast and the Auxiliary ACL](./0010-groupcast-and-auxiliary-acl.md),
  which builds what the section "What is not built" below deferred, and
  [ADR 0012 — ScenesManagement server](./0012-scenesmanagement-server.md),
  which replaces the ScenesManagement stub this ADR kept
- **Related**:
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §2 / §5,
  [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md)
  (the retired `BD-Matter-P2-D19` Groups-stub entry),
  [`../../notes/parity/matter_behaviour_findings.md`](../../notes/parity/matter_behaviour_findings.md)
  ("Matter 1.6.1 pin — open items": what remains of Groupcast)

## Context

ADR 0004 kept Groups (0x0004) a read-only stub for three reasons: it matched
matter.js's surface "when no membership provider is wired", the original host
had no group concept to project, and a real implementation needs the
GroupKeyManagement coupling and the multicast session derivation — which
could not be mirrored without a matter.js checkout.

None of the three holds any more:

1. matter.js's default `GroupsServer` is not a stub. It enables GroupNames,
   keeps membership in the root's `GroupKeyManagementServer`
   (`addEndpointForGroup` / `removeEndpoint`), and the protocol package
   receives group messages for it (`FabricGroups`, `GroupSession.decode`,
   `SessionManager.groupSessionFromPacket`, `ServerGroupNetworking`). A stub
   that rejects AddGroup is the surface of *no* matter.js node.
2. Group membership is not a device concept for the host to project. In
   matter.js it is stack state on the node: the controller provisions it and
   the device obeys. A host's device model has no say in it — so "the host has
   no group primitive" is not a reason against it.
3. The matter.js checkout is available, and the key schedule, the group
   session id, the privacy key, the multicast address and the sealed message
   are pinned against bytes matter.js itself produced.

Matter 1.6.1 sharpened it: the light and plug device types this module
advertises require Groupcast on the root, and Groupcast presupposes groups
that work.

## Decision

Build Groups and group-message reception the way matter.js does, as stack
state. ScenesManagement stays a stub.

| matter.js | go-fabric |
| --- | --- |
| `GroupKeyManagementServer` keySetWrite / keySetRead / keySetRemove / keySetReadAllIndices, `#validateGroupKeyMap`, `452d6f5c` (GroupKeyMulticastPolicy ignored, PerGroupID reported) | `core.GroupKeyManagement`, plus the wire codec it never had (`bridge/fields_reader_groupkeys.go`) |
| `FabricGroups` / `KeySets` / `Groups` / `MessagingState` — operational keys, session ids, privacy keys, GroupKeyMap, endpoint map, reception state | `groups.Manager` |
| `GroupKeyManagementServer` state `groupTable` (persisted) | `store.matter_group_table`, owned by `groups.Manager` |
| `GroupsServer` | `core.Groups`, mounted by the assembler on every endpoint whose device type mandates Groups (`schema.DeviceTypeRequiresServerCluster`) and in place of any Groups server a source supplies |
| `GroupSession.decode`, `SessionManager.groupSessionFromPacket`, `MessageReceptionStateEncryptedWithRollover` | `groups.Manager.Decode`, `mrp.NewWindowEncryptedRollover` |
| `ExchangeManager` group branch, `InteractionMessenger.handleRequest` (no status on a group session), `InteractionServer` handleInvokeRequest / handleWriteRequest on a group session, `CommandInvokeResponse` / `AttributeWriteResponse` wildcard expansion over `subject.endpoints` | `Bridge.dispatchGroupMessage`, `im.HandleGroupInvokeRequest`, `im.HandleGroupWriteRequest`, `TopologyDispatcher.InvokeAuthorized` / `WriteAuthorized` |
| `FabricAccessControl` Group subject (`hasValidMapping`, group id as subject, no Administer) | `TopologyDispatcher.CheckACL` under `im.WithGroupSubject` |
| `ServerGroupNetworking` (join per group with endpoints, leave when gone, 30 s retry, leave on close) | `Bridge.reconcileGroupMemberships` over `udp.Listener.JoinGroup` / `LeaveGroup` |

The host hands one `*groups.Manager` to three places — `GroupKeyMgmtConfig.Groups`,
`endpoint.Config.Groups` and `Bridge.AttachGroupMessaging` — and implements
nothing. `contract/` gains nothing: there is no host-side primitive to
describe.

## What is not built

- **Groupcast (0x0065) and the AccessControl Auxiliary ACL** — built since
  [ADR 0010](./0010-groupcast-and-auxiliary-acl.md). At the time: matter.js's
  default `ServerNode.RootEndpoint` installs both; this module's root still
  corresponds to `RootEndpointWithoutGroupcast`. Groupcast is a cluster of its
  own weight — derived membership with sender-only groups, key creation
  through GroupKeyManagement, the IANA `FF05::FA` address policy, the
  auxiliary-ACL provider feeding AccessControl's `AuxiliaryAcl` /
  `AuxiliaryAccessUpdated`, and GroupcastTesting with the group-message
  outcome events. Mounting part of it would advertise a cluster that does not
  work; it stays an open item with its parts named in the findings.
- **Sending group messages.** There is no controller role (a scope decision,
  `docs/matterjs-comparison.md`), so no node-global group data counter and no
  outbound group session.
- **ScenesManagement** stays the stub (`BD-Matter-P2-D18`). matter.js couples
  scenes to groups only through RemoveGroup / RemoveAllGroups, which drop the
  group's scenes; the stub holds none.

## Consequences

- A controller can provision groups and drive the bridged lights and plugs
  with group commands. Nothing of it has been exercised against a real
  controller yet; the in-process end-to-end test and the matter.js-produced
  fixtures are the evidence.
- `wire.Groups` is deprecated; a host that still mounts it gets the real
  server in its place once `endpoint.Config.Groups` is set.
- An AccessControl Group entry's subject is a Group ID (0x0001..0xFFFF); the
  validator previously demanded a Group Node ID and refused every Group entry.
