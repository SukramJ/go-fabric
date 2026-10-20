# Report BQ — milestone 3, phase 18d: Binding, group sends on the fabrictest model

Branch `m3/phase-18d-binding` from main b34c245 (which carries the brief).
Implementer: Opus, one agent. 41 of the 47 open cases are ported; 6 stay
`missing`, each with its reason.

## Files

| matter.js file (at 862ad75f) | Go test | ported | missing | excluded |
| --- | --- | ---: | ---: | ---: |
| `binding/BindingManagerTest.ts` | `binding/manager_parity_matterjs_test.go` (26), `node/binding_manager_parity_matterjs_test.go` (5) | 31 | 3 | 0 |
| `binding/BindingIntegrationTest.ts` | `node/binding_integration_parity_matterjs_test.go` | 9 | 3 | 0 |
| `binding/BindingServerTest.ts` | `cluster/core/binding_parity_matterjs_test.go` | 1 | 0 | 0 |

Per `describe` (open cases at the pin):

| describe (line) | ported | missing | where |
| --- | ---: | ---: | --- |
| BindingManager :109 | 15 | 3 | 10 unit (`binding`), 5 node |
| client selection :886 | 9 | 0 | unit |
| without an observer of established :1107 | 7 | 0 | unit |
| Binding integration :57 (kind=group) | 1 | 0 | node |
| kind=client with a default subscription :352 | 0 | 3 | — (Register candidate) |
| Binding integration, group sends :602 | 8 | 0 | node |
| BindingServer :16 | 1 | 0 | `cluster/core` (external test, manager + server) |

Missing:

- "lazy-installs into node.env on first get": js-runtime-only. `Environment.get` installs the manager.
- "close clears queue, pending subscriptions, and maps without emitting": `binding.Manager.Close` is the node's shutdown. It disposes every server first, as `BindingServer[Symbol.asyncDispose]` does (`BindingServer.ts:160-163`), so it emits removed. matter.js's `manager.close()` alone does not emit (`BindingManager.ts:663-686`). This is a by_design candidate.
- "stops watching a fabric's group keys once the fabric is deleted": js-runtime-only. This is matter.js's per-fabric observer on the group key maps (`BindingManager.ts:477-495`, `#fabricDeleted` :90-93). The Go manager holds no such watch: the node pushes `GroupKeysChanged` from `groups.Manager.OnGroupcastChanged` (`node/client.go:184`).
- The three default-subscription cases: see **The default subscription**.

## Fixes

| production file | matter.js | what changed | commit |
| --- | --- | --- | --- |
| `binding/manager.go`, `binding/resolve.go` | `BindingManager.ts:39-40` (ServerRecord.groups), `:119-141` (register/unregister), `:191-195` (disposeServer), `:528-532` (#isRegisteredGroup) | The manager now records a group entry as registered when `Register` is called, and forgets it when `Unregister` or `Detach` is called. Before, this happened only when the worker reached the entry. A resolution under way, inside the group sender's lookup, therefore sees an unregister that arrived meanwhile and stops. Before the fix, `isRegisteredGroup` could never catch an unregister, because both ran in order on the worker, and the host saw established then removed. Proven by "kind=group does not resolve an entry unregistered while its group is being registered" (it failed before with `[48 49]`). `recheckGroups` iterates the call-time registrations. | 8c9d145 |
| `groups/request.go` | `ClientInteraction.ts:86-95` (peerAddressDiagnostic), `:495-508` (the info "Invoke" line) | A group invoke now logs `matter.groups.invoke` at info as it leaves. The record names the group, the destination, the fabric, the cluster, the command and `suppress_response`. Proven by "names the group on the log line of a group command it sends". | abe0a7a |

Both fixes have a CHANGELOG `### Fixed` entry. No Groupcast path was touched.

## fabrictest additions (commit 73e78a7, `Prepare` in 5fdb8cf)

- `transport/udp`: `multicastMember` grows `SendMulticast(group, port, payload)`. `Listener.SendMulticast` delegates to it after its own checks (address, size, Close), the same way JoinGroup and LeaveGroup do. Test: `TestSendMulticastDelegatesToTheSocketModel`.
- `fabrictest` socket `SendMulticast`: the datagram goes onto the network from the host's address, through `Network.Filter` and the delivery options, to every member of the group. Counterpart: `MockUdpSocket.send` (`MockUdpSocket.ts:87-97`) routed per `MockNetwork.shouldReceive` (`MockNetwork.ts:79-81`). Test: `TestMulticastModelGroupSend`.
- `Device.Start` hands the bridge's in-memory socket to the node's group state (`Node.Groups().SetTransport`) after start. A device's group messages therefore go out on the site, as matter.js sends them from the operational socket. The production node keeps its own send socket (`node/groupsend.go`, BD-Matter-GroupSendFromEphemeralPort). Test: `TestDeviceGroupSend`.
- `Topology.Prepare func(*node.Node)`: this runs after `node.New` and before `Start`, on every start. It plays the role of matter.js's `createOnline(…, { online: false })` followed by `node.start()`. It is used by "queues register calls before node online".

## The default subscription

matter.js does not keep this subscription itself. The test sets it on the bound `ClientNode` (`network.defaultSubscription` with `autoSubscribe`, `BindingIntegrationTest.ts:180-219`). `NetworkClient` then keeps a `SustainedSubscription`, and that subscription detects loss by timeout and resubscribes. The binding manager adds nothing beyond the `ClientNode` (`BindingManager.ts:569-611`).

The Go counterpart already exists: a host subscribes through `Resolution.Interaction.Subscribe` with `Sustain` (`controller/interaction/sustained.go`). No change to `binding/` or `node/client.go` is needed. What is missing is a site: fabrictest carries no node client stack. `node/client.go:62-110` opens a kernel socket (`udp.New`) and its own DNS-SD cache (`discovery.Open`), so two fabrictest nodes cannot reach each other over CASE. The isolation in the cases is `Network.Filter`, and the restart is `Device.Restart`.

What it would take:
1. A node seam for the client listener and the names. This is an internal seam like `bridgeseam.UseListener`, about 40 lines in `node` and `internal/`.
2. fabrictest wiring of both onto the site, the way `Controller` builds its `Names`. About 50 lines.
3. The three cases, about 150 lines.

"Reports without data of the peer's root endpoint" has no Go `ClientNode` behaviours to assert. Its counterpart would be "the light received one Subscribe on endpoint 1 only, no read of endpoint 0". This is beyond a few dozen lines and touches `node`'s seams, so all three stay `missing` and are listed as a Register candidate.

## Register candidates

1. fabrictest node client stack (above). This unblocks the three default-subscription cases and any client-kind binding test on a site.
2. `binding.Manager.Close` without emitting removed, if the owner wants matter.js's split between disposing the servers (emits) and closing the manager (does not emit). This would change a public API's semantics. Today `node.Stop` relies on `Close` to emit.
3. Optional: `binding.GroupTarget` with request options (Timed) and `Write`. Today "refuses a timed invoke on a group" runs on the node's `groups.Sender`, which the target wraps, because the target cannot express a timed invoke. That is an API change of `binding`.

## Guards

Final run, HEAD 697fb7f before and after (summary log `bq-guards.log`):

```
exit 0 ci
exit 0 race
exit 0 testparity-check
exit 0 cite-check
exit 0 reachability-check
exit 0 cover-check
```

The first run, at 6494316, found three problems, each fixed in its own commit:
- The linter flagged prealloc, thelper and rangeValCopy in the new tests (fixed in 4c6549a).
- One new citation drifted. The send citation shared a sentence with `dropMembership`; it was reworded and the drift is back to 264.
- The reachability snapshot had moved by the new test entry points only. Unreached stays at 5, so no ratchet moved (697fb7f).

`testparityPlannedRatchet` is unchanged (`planned` did not move).

## Families

`GOFABRIC_CHIP_FAMILIES=BIND,ACL,IDM make chiptool-families` ran at HEAD 697fb7f, before and after (summary log `bq-families.log`): exit 0, 2187 s.

| family | pass | skip | fail |
| --- | ---: | ---: | ---: |
| BIND | 0 | 3 | 0 |
| ACL | 10 | 1 | 0 |
| IDM | 20 | 13 | 0 |

BIND 2.1, 2.2 and 2.3 are skipped by the harness as manual cases ("the CHIP test plan has a test-lab operator perform it"). So the chip-tool families do not exercise the binding fix. TC-BIND-2.1 runs in process as `node:TestLiveBinding`, which passes in `make ci` and `make race`. No `-update-chip-cases`: nothing moved.

## Differences to the brief

- The brief describes the default subscription as one "the matter.js client keeps on the bound peer". The matter.js test sets it on the `ClientNode` (`NetworkClient`), and the Go mechanism exists (`Sustain`). The blocker is the fabrictest client stack, not the manager.
- The group-send delegation needed one more piece than the brief named. The node's group sender opens a kernel socket of its own, so fabrictest also hands the bridge's socket to `Node.Groups()` (`SetTransport`) after start.
- "queues register calls before node online" needed a fabrictest hook (`Topology.Prepare`). `Device.Act` before `Start` reaches the stopped node, not the new one.
- `BindingManagerTest.ts` file split: there are 26 unit cases. The brief's file names were kept (`binding/manager_parity_matterjs_test.go` plus `node/binding_manager_parity_matterjs_test.go`).
- The BindingServer case is an external test of `cluster/core` that imports `binding`, per the brief. It needs the manager.

## Decisions the owner should check

- "kind=group resolves on a key provisioned through the fabric object a replacement superseded" (:822-853). The Go node has one group state per fabric index, and UpdateNOC closes the fabric's sessions (`bridge.CloseFabricSessionsAfterUpdate`, mirroring `Fabric.replaced`). So no superseded fabric exists to provision through. The port provisions over CASE after the replacement (KeySetWrite plus GroupKeyMap on the wire). Its sibling (:792-820) provisions locally. The owner may prefer `missing` (js-runtime-only) instead.
- In both replacement cases, UpdateNOC is committed: `Peer.Disconnect` with a cause, then CommissioningComplete over a new CASE session. BP's case did not commit. Without the commit, a fake-clock advance could expire the fail-safe.
- "records no cache fabric filter for a group": the Go counterpart is "the group resolution has no peer and no interaction (no client node, hence no cache), and nothing logs at warn level from the provisioning through a send".
- "two registrations … share the same ClientGroup": this runs on two switches, two Binding servers. matter.js uses two fake servers on one endpoint, but Go allows one server per endpoint. The assertion is the equality of the two `Resolution.Group` values, which is the same cached `*groups.Sender`.
- The log record's shape (`matter.groups.invoke`, attributes `group`, `dest`, `fabric_index`, `cluster`, `command`, `suppress_response`) is my choice. matter.js's line is a formatted string.

## Left out

- `TcpSessionBindingTest.ts` (phase 19), as the brief says.
- The client stack on fabrictest (Register candidate 1).
- A Write log line for group writes. matter.js logs "Write" too (`ClientInteraction.ts:317`), but no case asks for it.

## Questions

1. Should the replacement case of the "superseded fabric object" stay ported as described, or become js-runtime-only?
2. Should Register candidate 1 (fabrictest client stack) be planned for phase 19, where `TcpSessionBindingTest.ts` will need two nodes too?

## by_design candidates

- **BD-Matter-BindingManager-CloseEmitsRemoved**: matter.js `BindingManager.close()` clears its maps without emitting (`BindingManager.ts:663-686`). `removed` fires for live bindings only when a server is disposed (`disposeServer` :191-228, called by `BindingServer[Symbol.asyncDispose]` `BindingServer.ts:160-163`). `binding.Manager.Close` does both, disposing every server and then stopping. It is the node's shutdown, and `node.Stop` has no separate per-server disposal. Reasoning: the host observes the same removals at shutdown, and the manager has no life apart from the node.
- **BD-Matter-BindingManager-Worker** (existing entry, an update): matter.js keeps `ServerRecord.groups` at call time (`BindingManager.ts:119-141`). With fix 8c9d145, the Go manager does the same in `Manager.registered`, while resolutions stay on the worker. The entry's claim that the worker serializes everything should name this one call-time state.
- **BD-Matter-GroupTarget-InvokeOnly**: matter.js `ClientGroupInteraction` (`ClientGroupInteraction.ts:33-82`) takes timed requests (and refuses them) and also writes. `binding.GroupTarget` offers `Invoke(ctx, path, fields)` only, without options or a response, and the refusals live on `groups.Sender` (`groups/request.go`). Reasoning: the target is the group sender narrowed to what a binding sends, and a group answers nothing.

## Decided in review (2026-10-20)

- Every cited fix line read against the pin: fix 1 `BindingManager.ts:34-44`,
  `:119-141`, `:191-195`, `:528-532` (the call-time `groups` map,
  `#isRegisteredGroup`); fix 2 `ClientInteraction.ts:86-95`, `:495-508`
  (`peerAddressDiagnostic`, the info line of every invoke); the model's
  `MockUdpSocket.ts:87-97`, `MockNetwork.ts:79-81`; the close case
  `BindingManager.ts:663-686`, `BindingServer.ts:160-163`;
  `ClientGroupInteraction.ts:33-82`. All hold.
- Question 1: the superseded-fabric case stays ported. What Go can show is
  the observable — a key that reaches the fabric index after the
  replacement resolves the binding, here over CASE where the sibling
  provisions locally; the identity of the Fabric object is matter.js
  runtime. The test's comment says so; no register change.
- Question 2: yes. The fabrictest node client stack is brief BR's first
  harness commit (as the multicast model was BP's) and the three
  default-subscription cases go with it; a findings entry records the gap.
- The other decisions stand as made: UpdateNOC committed in both
  replacement cases; "records no cache fabric filter" as no peer, no
  interaction, no warn line; the shared ClientGroup on two switches; the
  log record's attributes.
- by_design: `BD-Matter-BindingManager-CloseEmitsRemoved`,
  `BD-Matter-GroupTarget-InvokeOnly`; one sentence added to
  `BD-Matter-BindingManager-Worker`. Findings: the fabrictest client
  stack, the group write log line.
