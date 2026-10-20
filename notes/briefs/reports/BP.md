# Report BP — milestone 3, phase 18c: Groupcast, the fabrictest multicast model

Branch `m3/phase-18c-groupcast` from the brief (f76b2e4) and the owner's
key-set-0 commit 5e2d745. Implementer: Opus, one agent. The model, the
whole `GroupcastServerTest.ts` and the OpCreds leg are done.

## Files

| matter.js file (at 862ad75f) | Go test | ported | missing | excluded |
| --- | --- | ---: | ---: | ---: |
| `groupcast/GroupcastServerTest.ts` | `cluster/core/groupcast_parity_matterjs_test.go` (the six earlier tests + 12 new), `node/groupcast_server_parity_matterjs_test.go` (9 new) | 49 | 1 | 0 |
| `operational-credentials/OperationalCredentialsServerTest.ts` "keeps group keys, endpoints, multicast policy and group ACL of the fabric" | `node/operational_credentials_server_parity_matterjs_test.go` `TestOperationalCredentialsUpdateNOC` | 1 | 0 | 0 |

The register listed 49 open cases, one of them the loop-generated
`name` (:358-405), which resolves to two titles: 50 open, 49 ported.

Per `describe` (open cases at the pin; core = `cluster/core`, node = `node/`):

| describe (line) | ported | missing | where |
| --- | ---: | ---: | --- |
| joinGroup :69 | 2 | 0 | core |
| leaveGroup :298 | 3 | 1 | core |
| updateGroupKey :504 | 1 | 0 | core |
| configureAuxiliaryAcl :550 | 3 | 0 | core |
| GKM mirror :627 | 8 | 0 | core |
| endpoint mirror :885 | 2 | 0 | core |
| legacy removal prune :938 | 6 | 0 | 4 core, 2 node (fabric removal) |
| per-fabric quota :1196 | 1 | 0 | core |
| multicast policy ordering :1366 | 2 | 0 | node |
| order-independent multicast rebind :1423 | 1 | 0 | node |
| shutdown cleanup :1471 | 1 | 0 | node |
| multicast membership across fabrics :1501 | 1 | 0 | node |
| MaxMcastAddrCount :1542 | 2 | 0 | core |
| multicast join failures :1591 | 3 | 0 | node |
| groupcastTesting duration :1712 | 2 | 0 | core (fake clock) |
| groupcastTesting events :1752 | 3 | 0 | core |
| derived membership :1917 | 6 | 0 | 3 core, 3 node (reload) |
| persistence :2170 | 1 | 0 | node |
| AuxiliaryAccessUpdated admin (CASE session) :2204 | 1 | 0 | node (over CASE) |
| **total** | **49** | **1** | |

Missing: "removes a group a wildcard request empties without the Sender
feature" (the loop's `ListenerOnlyRootEndpoint` title) — the module builds
Groupcast with Listener, Sender and PerGroup only
(`GroupcastServer.ts:81-83` as RootEndpoint installs it) and has no
Listener-only configuration to run it on. The cases matter.js runs on its
`IanaOnlyRootEndpoint` join with an explicit IanaAddr policy, so the
PerGroup feature does not change what they assert; the node file says so.

Register: `GroupcastServerTest.ts` stays `partial` (1 missing, `go` names
all 29 test functions of both files); `OperationalCredentialsServerTest.ts`
loses the multicast case from `missing`. `planned` unchanged (52), the
ratchet did not move. Harness: `fabrictest` commissioned pairs, a second
fabric by a second controller (`secondFabric`), `Device.Act` with an
administrator's context, `Device.Restart`/`Stop`, `RemoveFabric`, the wire
helpers for the CASE case; a fake clock in the core duration cases
(`GroupcastConfig.Clock`). No `time.Sleep`.

## Fixes

| production file | matter.js lines | what | commit |
| --- | --- | --- | --- |
| `cluster/core/groupcast.go` (`joinGroup`) | `GroupcastServer.ts:253-258` (`setGroupMulticastPolicy` before the group table writes), `:280-283` | JoinGroup sets the group's policy before it writes the endpoints; a new IanaAddr group used to join its per-group FF35 address first, leave it and join FF05::FA (seen on the model: `[join ff35:… leave ff35:… join ff05::fa]`). | a6cf0b7 (message cites :252-257; corrected to :253-258 in 72b09f2) |
| `bridge/group_messaging.go`, `bridge/bridge.go` | `ServerGroupNetworking.ts:39-40`, `:136-140`, `:159-176` | A failing multicast join logs once at info per failure streak (`matter.groups.join_failed`), at debug on its retries; the streak ends on success or when the address is no longer wanted. It used to warn on every attempt. | 2123c4c |

Both proven by reverting them: the policy-ordering and the three
join-failure cases fail without them. CHANGELOG `### Fixed` names both.

Also added (not a fix, `### Added`): `GroupcastConfig.MaxMcastAddrCount`
(0 keeps 44), the counterpart of matter.js's State override
(`GroupcastServer.ts:933`; the test sets 4, `GroupcastServerTest.ts:1543-1547`).
With 44/44 and the per-fabric quota of 22 the limit cannot be reached, so
the two MaxMcastAddrCount cases need it.

## fabrictest additions (commit 1c46627)

| symbol | matter.js `MockNetwork` counterpart |
| --- | --- |
| `transport/udp`: `Listener.JoinGroup`/`LeaveGroup` delegate to a socket with its own `JoinGroup`/`LeaveGroup` before demanding a kernel socket (unexported `multicastMember`, test `TestMembershipDelegatesToTheSocketModel`) | `MockUdpSocket.addMembership` / `dropMembership` |
| the in-memory socket's `JoinGroup`/`LeaveGroup`; `Network` keeps the groups per host address and delivers a datagram to a group to the socket on its port of every host holding it | `MockNetwork.addMembership` / `dropMembership`, `shouldReceive` |
| `Device.MulticastGroups() []net.IP` | `MockNetwork.isMemberOf` on the node's network |
| `Network.Members(group) int` | `isMemberOf` across the site's hosts |
| `Network.WatchMemberships(fn) (stop)`, `MembershipChange` (`String()` = "join ff05::fa") | `recordMembershipChanges` (`multicast-helpers.ts`) |
| `Network.FailJoins(fn)` | a test's throwing `addMembership` |

`fabrictest/doc.go` names the model; a membership the real stack joins per
interface is one membership here, and an interface the site does not have
is not modelled. Log capture uses the existing seam, `Options.Logger`.
Tests: `fabrictest/multicast_internal_test.go` (delivery, leave, refused
join, watch), `fabrictest/multicast_test.go` (a device joins and its Stop
leaves).

## Register candidates

- `udp.Listener.SendMulticast` still demands a kernel socket: a group
  message sent by a fabrictest host does not reach the model. A second
  delegation (the socket's own `SendMulticast`) would let a group Invoke
  run end to end on the site. Not needed by this file; a `transport/udp`
  API change, so left.

## Guards

HEAD before and after 72b09f2 (background run, summary log):

```
ci exit 0
race exit 0
testparity-check exit 0
cite-check exit 0
reachability-check exit 0
cover-check exit 0
```

## Families

`GOFABRIC_CHIP_FAMILIES=GRPKEY,G,GC,ACL,IDM make chiptool-families`
at 72b09f2 (both fixes moved device behaviour): exit 0, 2218 s, no
fixture or doc diff.

| family | pass | skip |
| --- | ---: | ---: |
| GC | 8 (2.1-2.8, multicast case 2.8 included) | 0 |
| GRPKEY | 2 | 1 |
| G | 2 | 3 |
| ACL | 10 | 1 |
| IDM | 67 (10.4 on every endpoint) | 13 |

The 18 skips are the harness's standing ones: 17 manual cases (a
test-lab operator performs them) and the excluded TC-G-2.2 (step 7a,
chip's own GroupDataProviderImpl refuses the entry too). No PICS moved.

## Differences to the brief

- Memberships are kept per host address, not per socket, and outlive the
  socket's close — MockNetwork keeps them on the host, and only that makes
  "drops joined multicast memberships on shutdown" non-vacuous (a closed
  socket's record would vanish whether or not the node left).
- Join failure injection is `Network.FailJoins(fn)`, not
  `NetOptions.JoinError`: a func field makes the public `NetOptions`
  incomparable (`fabrictest/accessors_test.go` compares it to the zero
  value, and so may a consumer).
- 50 open cases, not 49 (the loop `name` is two titles); PR title says so.
- The CHANGELOG `### Added` carries the fabrictest model and
  `MaxMcastAddrCount`, landed in the core port commit rather than the model
  commit.

## Decisions the owner should check

1. `GroupcastConfig.MaxMcastAddrCount` — an additive field of
   `cluster/core`, the package under test, added to run the two
   MaxMcastAddrCount cases at matter.js's value 4.
2. The join-failure log message changed key and level
   (`matter.groups.join` warn → `matter.groups.join_failed` info/debug);
   an operator filtering on the old key loses it.
3. Fix 1 writes the properties twice in a JoinGroup (policy first, the
   auxiliary flag at the end); a refused endpoint mirror after the first
   write leaves the policy behind, as matter.js's FabricGroups policy write
   is not rolled back either. The capacity checks still run before both.
4. "removes a fabric with joined groups without a transaction conflict":
   the conflict is matter.js's reactor runtime; the port asserts no
   error-level log while the fabric goes and the observable result.
5. "rejects the Listener feature when AccessControl lacks the Auxiliary
   feature" asserts the Go constructor error with AccessControl nil — the
   Go AccessControl has no feature without Auxiliary to pass.
6. The register `go` list also names `TestNewGroupcastRequiresItsCollaborators`
   and `TestGroupcastAndAuxiliarySurfaceMatchSchema` ("every function of
   both files").

## Left out

Binding, `AccessControlCustomExtensionTest.ts`, `secure/`,
`node/security.go`, `groups/ipk.go` and the key-set-0 code untouched.
`SendMulticast` on the model (above).

## Questions

- Should the model also carry group sends (`SendMulticast` delegation),
  so `node/groupsend_live_test.go`-style cases run on fabrictest?

## by_design candidates

- **Groupcast feature set fixed at Listener + Sender + PerGroup.** matter.js
  builds the server with any subset (`GroupcastServerTest.ts:38-51`
  `ListenerOnlyRootEndpoint`, `IanaOnlyRootEndpoint`); its RootEndpoint
  installs all three (`GroupcastServer.ts:81-83`, the Matter 1.6.1 Root Node
  conditions). The module builds only that root, so the Listener-only case
  (`:358-405`) has nothing to run on — a configuration matter.js offers
  that a bridge does not need.

## Decided in review (2026-10-20)

- Every cited fix line read against the pin: fix 1 `GroupcastServer.ts:253-258`
  (the policy before the group table writes) and `:280-283`; fix 2
  `ServerGroupNetworking.ts:39-40`, `:136-140`, `:159-176`; the seam's
  `MockUdpSocket` counterpart; `MaxMcastAddrCount` 44 (`GroupcastServer.ts:933`).
  All hold.
- The six decisions stand as made: the two property writes of JoinGroup
  are matter.js's order; the log key `matter.groups.join_failed` at
  info/debug is matter.js's level (the CHANGELOG names the old key);
  "without a transaction conflict" as no error-level log plus the result;
  "fabric is replaced" as UpdateNOC on the same fabric id; the IanaOnly
  cases on the full server; `MaxMcastAddrCount` as a config field.
- by_design: `BD-Matter-Groupcast-RootFeatureSet` (the Listener-only case
  stays `missing`). The testing source address's zone is no divergence:
  the event carries the 16 bytes either way, no entry.
- `SendMulticast` on the model: a harness follow-up, not a parity gap —
  brief BQ or phase 19 takes it when a ported case needs a group send on
  fabrictest; no register entry.
