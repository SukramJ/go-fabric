# Brief BQ — milestone 3, phase 18d: Binding

Date 2026-10-20. Roadmap milestone 3 (`docs/roadmap.md`), phase 18 after
[concept v5](../../docs/concept-v5-behaviour-depth.md) Teil E (owner
decisions 18.1–18.3, 2026-10-19; risk E.6 "Binding-Clients") and the
phase-18 read (`notes/briefs/reads/read-m3-phase-18-system-set.md`, §1.8
Binding: the three test files with every `it` title and line). matter.js
pin `862ad75f`, read with `git -C $MATTERJS_DIR show 862ad75f:<path>`.
Last of the four sequential waves (`v0.8.8`); briefs BO and BP are the
template: BO §"How a port is built" items 1–9 and BP's deltas apply where
this brief says nothing.

**Model (decision 18.1):** Opus implements; the owner reads every cited
fix line against the pin before the pull request merges. `binding/` and
`cluster/core/binding.go` are not security servers, so a fix needs no
special commit rule beyond BO item 4 (own commit, `// Mirrors`, CHANGELOG);
`make race` stays mandatory (the manager resolves peers in the
background).

## Scope

Three matter.js test files, 47 cases open at the pin:

| File | Harness | Cases open | Register today | Go subject |
| --- | --- | ---: | --- | --- |
| `binding/BindingManagerTest.ts` (1 375 lines, 47 cases in 3 `describe`: BindingManager :109, client selection :886, without an observer of established :1107) | `MockServerNode` + `MockNetwork` | 34 | partial, 7 `go` in `binding/manager_test.go` | `binding/manager.go`, `resolve.go`, `reason.go` |
| `binding/BindingIntegrationTest.ts` (857 lines, 14 cases in 3 `describe`: Binding integration :57, kind=client with a default subscription of one attribute :352, group sends :602) | `MockServerNode` + `MockNetwork`, two nodes | 12 | partial, 2 `go` (`node:TestLiveBinding`, `binding:TestDormantThenWoken`) | `binding/`, `node/client.go`, `node/groupsend.go`, `cluster/core/binding.go` |
| `binding/BindingServerTest.ts` (223 lines, 5 cases) | `MockServerNode` | 1 | partial, 3 `go` | `cluster/core/binding.go` |

The titles are the register entries' `missing` lists (verbatim `it`
titles); the map §1.8 has every title with its line. The existing Go
tests stay; `binding/manager_test.go` is the unit harness of the manager
(its fakes for `Topology`, `Peers`, `GroupSender`), `node/` on
`fabrictest` is the harness for everything that needs two nodes, a
commissioned fabric, group keys or a restart.

`packages/protocol/test/protocol/TcpSessionBindingTest.ts` is a transport
file, not this wave (phase 19).

## What is settled before you build (E.6, answered)

- **Client selection needs no `contract/` change.** matter.js installs the
  selected client behaviors on the materialized peer endpoint
  (`BindingManager.ts:547-567`, `#selectClientClusters` on
  `client.schema.effectiveBindable`, `#installClientBehaviors`); the Go
  manager selects the same clients (`binding/resolve.go`
  `selectClientClusters` on `schema.ClusterBindable`, `Topology.ClientClusters`)
  and hands them to the host as `Resolution.Clients` — there is no
  behavior to install, the interaction is bound to the peer. The nine
  "client selection" cases (:886-1106) assert on `Resolution.Clients` and
  on the drop reason (`binding/reason.go` `ignoredEntryReason`, matter.js
  `:707-720` verbatim). If a case needs more than that, it is a question,
  not a change.
- **Group sends run on fabrictest only with the model extended.** Brief BP
  built the multicast model (`fabrictest/net.go`, memberships, delivery,
  `Device.MulticastGroups`, `Network.FailJoins`) and the join/leave seam
  in `transport/udp/multicast.go`; `udp.Listener.SendMulticast`
  (`transport/udp/multicast.go:121`) still demands a kernel socket, so a
  node's group send (`groups/sender.go:304`, through `node/groupsend.go`)
  fails on a site. First commit of this wave: `SendMulticast` delegates to
  the socket model the same way (`multicastMember` grows a
  `SendMulticast(group net.IP, port int, payload []byte) error`, the
  fabrictest socket delivers to every member of the group), with a
  `transport/udp` test and a `fabrictest` test. Then the eight "group
  sends" cases (:602-857) and the group kinds of the manager file run.
- **The default subscription of a bound client is a candidate, not a
  fix.** The three cases of "kind=client with a default subscription of
  one attribute" (:352-474: reports without data of the peer's root
  endpoint, the lost peer by the subscription timeout and the
  resubscription, the restarting peer) describe a subscription the
  matter.js client keeps on the bound peer to notice its loss; the Go
  manager keeps none (`binding/` has no subscription). Read the matter.js
  mechanism (`BindingManager.ts` `#establishClientKind` :569-611,
  `resolution.node.lifecycle.online`, and the peer's `lifecycle` in
  `packages/node/src/node/client/`), state in the report what it would
  take in Go (which package, how many lines), and leave the three cases
  `missing` with that reason unless the whole of it is a few dozen lines
  inside `binding/` and `node/client.go` — then it is a fix with its own
  commit.
- **Runtime-only cases.** "lazy-installs into node.env on first get"
  (:110) is matter.js's environment wiring: `missing` with
  "js-runtime-only: Environment.get installs the manager" in the reason
  (BO rule 5). Everything a controller or a host observer can see is
  ported.
- **"after its fabric was replaced" / "the fabric object a replacement
  superseded"** (:792-853) run as UpdateNOC on the same fabric id over
  fabrictest, as BP did for Groupcast ("fabric is replaced").
- **"queues register calls before node online; flushes after online"**
  (:118-146): the Go manager's `Attach`/`Register` before `Node.Start` —
  port on fabrictest with `Device.Act` before `Start`, or on the unit
  harness if the manager exposes the queue; a difference is a fix
  candidate (BO rule 4).

## How a port is built

BO §"How a port is built" items 1–9, with these deltas:

- Files: `binding/manager_parity_matterjs_test.go` for the manager cases
  on the unit harness (the existing `manager_test.go` stays as it is; the
  register `go` list names both), `node/binding_integration_parity_matterjs_test.go`
  and `node/binding_manager_parity_matterjs_test.go` for the cases that
  need a site, `cluster/core/binding_parity_matterjs_test.go` for the
  BindingServer case (or the existing file of that package if one holds
  the three `go` tests — one Go file per matter.js file, BO item 1).
- Fix candidates from the `missing` lists: the server-kind checks (target
  endpoint lacks the cluster server, a cluster server added at runtime,
  the endpoint has the cluster only as a client — `BindingManager.ts:259-285`, the admission checks of `#resolveAndEmit`),
  the group-kind lifecycle rules (establishes once for several key
  changes, no resolution after the server was disposed, the key watch
  released when the fabric is deleted, the key withdrawn or the entry
  unregistered while the group is being registered — `:437-533`), the
  dormant-entry rules (:375-436), the group-send refusals (a timed invoke
  on a group, a group invoke naming an endpoint, no key held — the
  integration file :602-857 names the messages), "still warns about an
  entry that can never resolve". Anything that changes `contract/`, a
  store schema, a wire shape or the `node.Client` API is a Register
  candidate.
- Commit messages `m3/phase-18d BQ: <File>Test.ts <describe> → <package>
  (<n> cases, <m> missing)`; a fix `m3/phase-18d BQ fix: <one sentence>
  (<matter.js file>:<lines>)`; the model `m3/phase-18d BQ: fabrictest
  multicast model — group sends`.
- Guards as BO item 8 (`make race` mandatory). The families to run
  locally after a fix that moved device behaviour:
  `GOFABRIC_CHIP_FAMILIES=BIND,ACL,IDM make chiptool-families` (BIND is
  TC-BIND-2.1 against the reference daemon; IDM/10.4 whenever a PICS
  moved; `-update-chip-cases` only when the diff is read and explained
  in the report).

## Report shape (`notes/briefs/reports/BQ.md`)

As briefs BO and BP: the files with their Go test files and the case
count ported, missing and excluded per `describe`; **Fixes**;
**fabrictest additions** (the group-send delegation, with its matter.js
`MockNetwork`/`MockUdpSocket` counterpart); **The default subscription**
(what it would take in Go); **Register candidates**; **Guards** with HEAD
and one exit line each, the families run and their result;
**Differences** to this brief; **Decisions** the owner should check;
**Left out**; **Questions**; `by_design` candidates (one paragraph each
with citations — the owner writes the entry).

## Out of scope

`TcpSessionBindingTest.ts` (phase 19, transport); the phase-19
`packages/node/test/behaviors` directories; `AccessControlCustomExtensionTest.ts`
(milestone 4); `secure/`, `node/security.go`, the key-set-0 code of
`groups/` (the owner's); the Groupcast paths BP ported (touch
`cluster/core/groupcast.go` and `groups/groupcast.go` only where a
group-send case proves a defect there, as its own fix commit); the
families table rows and `docs/certifiability.md` beyond what the harness
regenerates for a fix you made.
