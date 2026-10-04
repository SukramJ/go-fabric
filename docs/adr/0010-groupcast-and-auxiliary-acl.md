# ADR 0010 — Groupcast and the AccessControl Auxiliary ACL on the root, as matter.js installs them

- **Status**: Accepted
- **Date**: 2026-10-04
- **Builds on**: [ADR 0009 — A real Groups server and group message delivery](./0009-groups-and-group-messaging.md)
  (which deferred Groupcast)
- **Related**:
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §2,
  [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md)
  (`BD-Matter-GroupcastNoSender`, `BD-Matter-GroupcastListOrder`,
  `BD-Matter-AuxiliaryAccessAdminNode`)

## Context

Matter 1.6.1 makes the RootNode condition `GroupcastListenerCond` mandatory
for the light and plug device types this module advertises (OnOffLight rev 4,
OnOffPlugInUnit rev 5, the dimmable and colour light revisions). Under it,
RootNode rev 5 requires the Groupcast server (0x0065) with the Listener
feature and AccessControl with the Auxiliary (AUX) feature. matter.js's
default `ServerNode.RootEndpoint` installs exactly that —
`GroupcastServer.with("Listener", "Sender", "PerGroup")` and
`AccessControlServer.with("Extension", "Auxiliary")`
(`packages/node/src/node/ServerNode.ts`); a root without them corresponds to
`RootEndpointWithoutGroupcast`, which matter.js documents as non-conformant
for a node with a Groups server.

ADR 0009 built Groups and group-message reception and left Groupcast out
because mounting part of it would advertise a cluster that does not work.

## Decision

Build Groupcast and the Auxiliary ACL the way matter.js does, on the group
state ADR 0009 introduced — no second group state.

| matter.js | go-fabric |
| --- | --- |
| `GroupcastServer.joinGroup` / `leaveGroup` (incl. `0d30528a`) / `updateGroupKey` / `configureAuxiliaryAcl` / `groupcastTesting` (`452d6f5c`), `#requireAdmin`, `#applyKeySet` | `core.Groupcast` (`cluster/core/groupcast.go`); the Admin check through `im.AuthorityAt`, which `HandleInvokeRequest` stamps (matter.js `session.authorityAt`) |
| `GroupcastServer` state `groupProperties` (persisted) | `groups.Manager` Groupcast properties, `store.matter_groupcast_groups` |
| `#deriveMembership` from groupProperties + GKM groupTable + groupKeyMap; `#handleGroupTableChanged` prune (kDeleteGroupIfEmpty) | `groups.Manager.GroupcastMemberships` (derived on read); `RemoveEndpoint` drops the properties of a group that leaves the table |
| `GroupKeyManagementServer.createKeySetForGroupcast` / `validateKeySetId`, `#setGroupKeyMapping` through `#validateGroupKeyMap` | unexported `GroupKeyManagement` methods the Groupcast server calls |
| `FabricGroups.setGroupMulticastPolicy`, `Groups.multicastAddress` (`FF05::FA` for IanaAddr), `ServerGroupNetworking` rebind | `groups.Manager.MulticastAddressFor`; `Memberships` names the policy's address, so the bridge's reconciler joins FF05::FA once for every IanaAddr group |
| `AccessControlServer.registerAuxAclProvider`, `#syncAuxAcl`, `#auxiliaryAclFor` (chunking), `AuxiliaryAcl`, `AuxiliaryAccessUpdated`, AuxiliaryType refused on ACL writes | `core.AccessControl` gains AUX when `NewGroupcast` registers the group state as its provider |
| `FabricAccessControl` with auxiliary entries; Group wildcard targets not on endpoint 0 with AUX | `TopologyDispatcher.SetAuxiliaryACL`, fed by the new port `Bridge.AttachAuxiliaryACL` |
| `SessionManager.onGroupMessage` / `emitGroupMessage` from `groupSessionFromPacket`, `ExchangeManager` (replay) and `InteractionServer.handleInvokeRequest` (Success / FailedAuth); `GroupSessionNoKeyError` unmapped authentication | `groups.Manager.OnGroupMessage` / `ReportGroupMessage`, `groups.NoKeyError`, `im.HandleGroupInvoke`; the bridge reports each outcome through the `GroupMessaging` port |

The advertised feature set is **Listener and PerGroup** (FeatureMap `0x05`),
not matter.js's Listener, Sender and PerGroup. Sender obliges a node to hold
memberships for groups it *sends* to — JoinGroup with no endpoints, LeaveGroup
keeping an emptied group as sender-only, EnableSenderTesting — and to
originate group messages for them (matter.js `4ad47150`, group bindings that
send). This module has no sending role (a scope decision, ADR 0009 and
`docs/matterjs-comparison.md`), so the obligation behind the bit cannot be
met. RootNode needs the Sender condition only for switch and controller
device types (`GroupcastSenderCond`, conformance `O`), none of which a bridge
of this module advertises. Without Sender every path is the one matter.js
takes for a Listener-only server (`ListenerOnlyGroupcastServer` in its own
tests): an empty endpoint list is a ConstraintError, a group losing its last
endpoint is removed, EnableSenderTesting is not a value of the enum.

## Consequences

- The root a host assembles from `NewAccessControl`, `NewGroupKeyManagement`
  and `NewGroupcast` over one `*groups.Manager` is what matter.js's default
  root is, minus Sender. A host also hands that manager to
  `Bridge.AttachAuxiliaryACL`; without it the auxiliary grants are listed but
  not enforced (fails closed).
- Group commands now work with nothing but a Groupcast JoinGroup carrying
  `UseAuxiliaryAcl`: the key, the binding, the membership, the multicast
  address and the access grant come from one command, as a Matter 1.6.1
  controller expects.
- Nothing of it has met a real controller. The in-process end-to-end test
  (`bridge/groupcast_e2e_test.go`) and the wire fixtures matter.js produced
  (`bridge/testdata/groupcast-wire-fixtures.json`) are the evidence.
- The GroupKeyManagement Groupcast feature (GCAST, `GroupcastAdoption`) stays
  off, as in matter.js, whose server refuses it as provisional in 1.6.1.
