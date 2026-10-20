# Brief BP — milestone 3, phase 18c: Groupcast

Date 2026-10-19. Roadmap milestone 3 (`docs/roadmap.md`), phase 18 after
[concept v5](../../docs/concept-v5-behaviour-depth.md) Teil E (owner
decisions 18.1–18.3, 2026-10-19) and the phase-18 read
(`notes/briefs/reads/read-m3-phase-18-system-set.md`, §1.7 Groupcast).
matter.js pin `862ad75f`, read with
`git -C $MATTERJS_DIR show 862ad75f:<path>`. Third of four sequential
waves (`v0.8.7`); brief BO (18b, `v0.8.6`) is the predecessor and the
template: its rules apply unchanged where this brief says nothing.

**Model (decision 18.1):** Opus implements; Groupcast is one of the four
servers ADR 0013 skips, so the owner reads every cited fix line against
the pin before the pull request merges. The security rule of brief BO
holds: a fix on `cluster/core/groupcast.go`, `groups/` or `bridge/`
group paths is its own commit with the matter.js line in the message and
a `// Mirrors` comment, `make race` is mandatory, nothing is loosened.

## Scope

One matter.js test file, 49 cases open at the pin, and the one leg of
18b left to this wave:

| File | Harness | Cases open | Register today | Go subject |
| --- | --- | ---: | --- | --- |
| `groupcast/GroupcastServerTest.ts` (2 243 lines, 65 cases in 20 `describe`) | `MockServerNode` + `MockNetwork` + `MockSite` | 49 | partial, 6 `go` in `cluster/core/groupcast_parity_matterjs_test.go` | `cluster/core/groupcast.go`, `groups/groupcast.go`, `groups/manager.go`, `bridge/group_messaging.go`, `store/` (group properties) |
| `operational-credentials/OperationalCredentialsServerTest.ts` "keeps group keys, endpoints, multicast policy and group ACL of the fabric" | `node/operational_credentials_server_parity_matterjs_test.go` `TestOperationalCredentialsUpdateNOC` | 1 (its last leg) | `missing` since BO | the socket's multicast memberships unchanged across UpdateNOC |

The 49 titles are the register entry's `missing` list (verbatim `it`
titles); map each to its `describe` from the file. The `describe` groups
at the pin, with their line: joinGroup :69, leaveGroup :298,
updateGroupKey :504, configureAuxiliaryAcl :550, GKM mirror :627,
endpoint mirror :885, legacy removal prune :938, per-fabric quota :1196,
multicast policy ordering :1366, order-independent multicast rebind
:1423, shutdown cleanup :1471, multicast membership across fabrics :1501,
MaxMcastAddrCount :1542, multicast join failures :1591, groupcastTesting
duration :1712, groupcastTesting events :1752, derived membership :1917,
persistence :2170, AuxiliaryAccessUpdated admin (CASE session) :2204. The
read counted one loop-generated `it(name, …)` in this file (the register
lists it as `name`): resolve its real titles from the loop and list them
in `missing`/`go` by title.

Around a third of the cases run on `MockNetwork`: multicast joins, the
IANA address shared across fabrics, join failures with retry, the
shutdown cleanup, the rebind after reload. They need the model below;
without it they stay `missing`, never `excluded` (concept E.6).

## The fabrictest multicast model (build it first)

What exists:

- The bridge joins and leaves groups through `multicastMember`
  (`bridge/group_messaging.go:123-124`: `JoinGroup(net.IP)`,
  `LeaveGroup(net.IP)`), handed in by `startGroupNetworking`
  (`:194-199`, called with the bridge's own listener at
  `bridge/bridge.go:871`); `reconcileGroupMemberships` joins and leaves
  on every membership change, `Stop` leaves everything (`:213-221`).
- The real member is `transport/udp.Listener` (`transport/udp/multicast.go:42`
  JoinGroup → `membership`), which refuses a socket that is not a kernel
  socket with `ErrNoMulticastInterface` (`:62`). `fabrictest` builds its
  listeners through the udp seam on its in-memory `socket`
  (`fabrictest/net.go:127-148`, `udpseam.NewListener(s)`), so today every
  join on a site fails there.
- `fabrictest.Network` (`net.go:62-75`) delivers unicast datagrams by
  `netip.AddrPort`; `NetOptions` (`:32-46`) carries loss, delay,
  duplicate and seed; `Network.Filter` sees every datagram. The site
  replaced mDNS multicast by answering queries itself
  (`fabrictest/mdns.go:15-23`); group multicast has no model yet.

What this wave adds, smallest API first:

1. **The seam.** `udp.Listener.membership` delegates to the underlying
   connection when it implements an optional interface
   (`JoinGroup(net.IP) error` / `LeaveGroup(net.IP) error`) before it
   demands a kernel socket; the fabrictest `socket` implements it. One
   production change in `transport/udp/multicast.go`, a few lines, with a
   test in `transport/udp`.
2. **Memberships on the network.** `Network` records, per socket, the
   IPv6 multicast groups it joined; a datagram to a multicast address is
   delivered to every socket that joined it (the site's own address
   scope, nothing routes), still through `Filter`, loss, delay and
   duplicate.
3. **The assertion surface.** `Device.MulticastGroups() []net.IP` (the
   groups the device's socket holds, sorted) and
   `Network.Members(group net.IP) int` or an equivalent pair; the ported
   cases assert joins, leaves, the shared IANA address, the rebind and
   the shutdown cleanup on them.
4. **Failure injection.** `NetOptions.JoinError func(group net.IP) error`
   (nil never fails): a non-nil error fails the join of that group once
   per call, so "retries a failed multicast join without another group
   change" and the failure-streak cases run; the log assertions capture
   the node's `slog` records through the site's logger (`Site.Logger`,
   `fabrictest/site.go:115`) with a handler the test installs — look for
   an existing capture seam in `fabrictest` before adding one.
5. **Docs.** `fabrictest/doc.go` names the model in one paragraph; the
   godoc of every new symbol says what the matter.js `MockNetwork`
   counterpart is.

A membership the real stack would join on an interface the site does not
have is not modelled; say so in the godoc. No change to `bridge/`,
`groups/` or `node/` for the model itself — if the node needs a seam to
hand a different member than its listener, that is a question, not a
change.

## How a port is built

Brief BO §"How a port is built" items 1–9 apply, with these deltas:

- Files: the six existing tests in
  `cluster/core/groupcast_parity_matterjs_test.go` stay; the new cases go
  into that file when they run on the server alone and into
  `node/groupcast_server_parity_matterjs_test.go` when they need a
  commissioned pair, the network model or a restart on one store. The
  register `go` list names every function of both files.
- Fix candidates from the `missing` list (rule 4 of BO): the GKM mirror
  rules (Membership ↔ GroupKeyMap and group table, the per-fabric cap
  aligned with `maxGroupsPerFabric`, unmapped groups kept out of the
  operational map), the legacy-removal prune rules (RemoveAllGroups and a
  listener group, a sender-only group untouched, the retained mark), the
  MaxMcastAddrCount limit and the IANA fallback, the join retry without
  another change, the testing duration default of 60 s and the latest
  request winning, the derived Membership after reload. Anything in the
  group message crypto path (`groups/sender.go`, `groups/receiver.go`,
  key derivation), a store schema, a wire shape, `contract/` or another
  package's API stays `missing` with its reason and goes to **Register
  candidates**.
- Commit messages `m3/phase-18c BP: GroupcastServerTest.ts <describe> →
  <package> (<n> cases, <m> missing)`, one commit per `describe` group or
  small set of them; a fix `m3/phase-18c BP fix: <one sentence>
  (<matter.js file>:<lines>)`; the model `m3/phase-18c BP: fabrictest
  multicast model`.
- The OpCreds leg: when the model holds memberships, extend
  `TestOperationalCredentialsUpdateNOC` with the membership assertion and
  move the case from `missing` to the register's `go` reason.
- Guards as BO item 8; the families to run locally after a fix that
  moved device behaviour: `GOFABRIC_CHIP_FAMILIES=GRPKEY,G,GC,ACL,IDM
  make chiptool-families` (GC carries the multicast case 2.8 in the
  families table; IDM/10.4 whenever a PICS moved).

## Report shape (`notes/briefs/reports/BP.md`)

As brief BO: the file with its Go test files and the case count ported,
missing and excluded per `describe`; **Fixes**; **fabrictest additions**
(every new symbol with its matter.js `MockNetwork` counterpart);
**Register candidates**; **Guards** with HEAD and one exit line each, the
families run and their result; **Differences** to this brief;
**Decisions** the owner should check; **Left out**; **Questions**;
`by_design` candidates (one paragraph each with citations — the owner
writes the entry).

## Out of scope

Key set 0 and the IPK: the owner's own commits on this branch precede
yours (`node/security.go`, `groups/manager.go` key set 0, the two
`KeySetsTest.ts` cases, the findings entry "a rotated IPK does not reach
CASE"); do not touch `secure/`, `node/security.go` or the key-set-0 paths
of `groups/manager.go`. Binding (brief BQ, 18d); every `packages/node/test/behaviors`
directory of phase 19; `AccessControlCustomExtensionTest.ts` (milestone
4); the families table rows and `docs/certifiability.md` beyond what the
harness regenerates for a fix you made.
