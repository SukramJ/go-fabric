# Report AN — the group message sender (ADR 0020, stage 5, part 1)

Branch `wave20/groupsend` from `planning/briefs-wave-20` (bc1f744), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`);
every `// Mirrors`/citation comment cites lines read at the pin. Each
commit carries its regenerated reachability inventory (unreached stays
**5** throughout; the ratchet constant is untouched).

| Commit | Item |
| --- | --- |
| 3316665 `feat(groups): encode a group message …` | §1 |
| 3e55b8a `feat(groups): the node-global group data counter` | §2 |
| 5efe9c8 `feat(groups): the group sender …, udp SendMulticast` | §3 |
| 187508f `feat(core): the Groupcast Sender feature …` | §4 |
| c25cb23 `feat(node): Node.Groups() sends …` (+ CHANGELOG) | §5 |
| 97beeb6 `docs(groups): cite SessionManager.ts:742 …` | comment fix |
| (this commit) `notes: report AN` | — |

## Per section — what was built, what proves it

- **§1** `transport/message.Header.MarshalPacket` (MC:440-445 refusal of
  message extensions → `ErrEncodeExtensions`; MC:481-488 group constraint →
  `ErrGroupHeader`). The codec could already *write* a group header
  (`Marshal` handles DSIZ=group); what was missing was the constraint.
  `secure/channel/group.go`: `EncryptGroup` (GS:251-267), `ObfuscateGroupHeader`
  (GS:276-286), `PrivacyNonce` (MessagePrivacy.ts:38-47), `Obfuscate`
  (:53-56). Proof: `groups/TestGroupEncodeMatchesMatterJS` — from the header
  fields and plaintext of every fixture in `groups/testdata/group-crypto-fixtures.json`
  the encoder reproduces matter.js's datagram **byte for byte** (privacy on
  and off) and `Manager.Decode` opens it; `channel` unit tests show the
  obfuscation is the receive keystream; `FuzzMarshalPacket` added (the
  package has one fuzz target per codec function). `transport/message`
  stays at 100 % coverage.
- **§2** `groups/counter.go`: `PersistedMessageCounter` as
  SM:981-1007 configures it — key `groupDataCounter`
  (`groups.GroupDataCounterKey`) via `GetMetadataCounter`/`SetMetadataCounter`,
  reserve 1000 persisted before a value at/past the mark is handed out
  (pattern: `im/eventlog.go:173`), wrap to 0, one
  `matter.groups.counter_near_rollover` warning at MAX−100 000 (or at start
  when already that close). `groups.CounterStore`; a store without it →
  `ErrNoCounterStore`. Tests: first run (random 28-bit start, mark persisted
  first), restart resumes past the mark, rollover warns once and re-reserves
  1000, failure paths.
- **§3** `groups/sender.go`, `groups/request.go`: `Manager.Sender`
  (SM:712-744 + GS.create), `Sender.Send` (ME:640-776 on a group session),
  `Sender.Invoke` / `Sender.Write` with `groups.RequestOptions`
  (CGI:44-81), `Manager.SetTransport` / `groups.MulticastTransport`;
  `udp.Listener.SendMulticast` (per-interface `IPV6_PKTINFO` sends over the
  interface set `JoinGroup` uses). Errors: `ErrNoKey` (map/key set missing),
  `ErrKeyNotStarted`, `ErrNoInterface`, `ErrInvalidGroupID`,
  `ErrInvalidGroupOperation`. Tests: key selection (mapped/unmapped/absent
  key set/not started/slot order), the cache keyed by key with two key sets
  whose keys **collide on one session id** (found by search in the test),
  the Sender's datagram equal to the matter.js fixture at the fixture's
  counter/exchange id, header/payload-header shape after Decode
  (P flag, source node id, counters, initiator, no R/A flag, exchange ids
  counting), Invoke/Write with SuppressResponse and no endpoint, timed /
  endpoint / incomplete write path refused, fabric removal closes the
  sessions. `TestSendMulticastReachesAJoinedListener` sends real IPv6
  multicast to a joined socket (passes on this host; skips without an
  interface or loopback).
- **§4** `core.Groupcast` FeatureMap 0x07; empty-endpoint JoinGroup joins
  sender-only; ReplaceEndpoints with none and LeaveGroup removing the last
  listed endpoint keep the group sender-only (GC:262-292, :328-369) through
  the new `groups.Manager.RemoveEndpointRetainingSender` (the
  retainedSenderOnly mark, GC:631-646); EnableSenderTesting behaves as
  EnableListenerTesting (GC:405-431); the table-capacity check applies only
  when the join leaves the group an endpoint. Tests: see the flipped list
  below plus new subtests (sender-only join, replace-with-none,
  legacy group kept with its PerGroup policy, EnableSenderTesting 60 s
  timer) and `groups/TestGroupRetainedSenderOnly`.
- **§5** `node/groupsend.go`: `Start` opens a `[::]:0` UDP socket and
  `SetTransport`s it on the node's `groups.Manager`, `Stop` closes it
  (one field + two calls in `node.go`; `node.Config` unchanged). Our node id
  per fabric comes from the fabric record the manager already loads.
  `node/TestLiveGroupSend`: two `node.Node`s commissioned into one fabric by
  `controller/commissioner`; KeySetWrite on both, JoinGroup (receiver: plug
  endpoint + UseAuxiliaryAcl; sender: no endpoints); the sender's
  `Groups().Sender(fabric, 0x0101).Invoke(OnOff Toggle)` flips the
  receiver's plug on and off; the requested destination is
  `[ff05::fa]:5540` (IanaAddr default). Delivery is to the receiver's
  unicast address — the nodes bind 127.0.0.1:0, so neither port 5540 nor
  IPv6 multicast reaches them; the bridge accepts a group datagram on any
  address (bridge/receive.go:91-97, group_messaging.go:237-239). The
  multicast leg is the udp test above (loopback multicast **does** deliver
  on this host).

## Guards (sequential)

| Guard | HEAD before → after | Result |
| --- | --- | --- |
| `make ci` | c25cb23 → c25cb23 | green (0 issues; cover-check passes, no floor moved) |
| `make race` | 97beeb6 → 97beeb6 | green |
| `make reachability-check` | 97beeb6 → 97beeb6 | green (unreached 5) |
| `make fuzz` | 97beeb6 → 97beeb6 | green, 20 targets at 100000x |
| `make chiptool-test` (once) | 97beeb6 → 97beeb6 | green; `TestMatterJSController` skipped (no built matter.js at the worktree-relative `../matter.js`) |

`make ci` ran on c25cb23; 97beeb6 differs only by one comment line.

## Brief lines that differ from the files (file followed)

1. **D2 / §2 "seeded above the per-key receive windows (MS:47, :71)".**
   MS:41-75 read the legacy per-key *send* counters (`<hex>-data` storage
   keys) — not receive windows, which are in memory only. This module never
   sent a group message, so it has no legacy counters; the seed is absent
   and matter.js's random start (MessageCounter.ts:57-60) applies. The §2
   test "first run seeds above the receive windows" is therefore "first run
   starts at the random 28-bit start and persists the mark first".
2. **D5 "`GroupcastMemberships` reports them so the bridge joins their
   multicast addresses too".** `GroupcastMemberships` does list sender-only
   groups, but matter.js `ServerGroupNetworking` joins only groups in
   `fabric.groups.endpoints` (ServerGroupNetworking.ts:63-64, :87-89): a
   sender-only group is not joined. `Manager.Memberships` (the bridge's
   join list) is unchanged; the sender-only test asserts no multicast
   membership.
3. **D4 "endpoint ignored".** CGI:71-73 *refuses* a path naming an endpoint
   (`InvalidGroupOperationError`); `Sender.Invoke` refuses
   `HasEndpoint` with `ErrInvalidGroupOperation` and encodes no endpoint.
4. **D4 signature.** "Timed invokes over a group are refused" needs a way to
   ask for one; `Invoke`/`Write` take a `groups.RequestOptions{Timed bool}`.
5. **§1 "(if the codec cannot encode a group header yet)".** It could;
   only the MC:483 constraint was missing (`MarshalPacket`).
6. **D3 "on every interface the listener joined multicast on".** The
   listener tracks no joined set; `SendMulticast` uses the same interface
   list `JoinGroup` iterates. matter.js sends on *one* interface
   (NodeJsUdpSocket.ts:298-325) — see by_design candidate A.
7. **§5 "the listener for SendMulticast".** The bridge does not expose its
   operational `*udp.Listener` and `bridge/` is not mine; the node sends
   from its own ephemeral `[::]:0` socket — see question 1.
8. **§5 "writes the same key set and GroupKeyMap to both and JoinGroup".**
   JoinGroup binds the group in GroupKeyMap itself (GC:280,
   `#setGroupKeyMapping`); the live test does KeySetWrite + JoinGroup and
   no separate GroupKeyMap write.

## Decisions

- `CounterStore` is a separate interface found by type assertion on the
  manager's `Store` rather than new methods on `groups.Store`, so no
  external `Store` implementation breaks; `*store.Store` satisfies it.
- The counter is created lazily on the first `Sender` (matter.js builds it
  with the SessionManager); a failed creation is not cached.
- The epoch start comparison mirrors matter.js exactly:
  `EpochStartTime` µs / 1000 ≤ `Time.nowMs` (Unix ms) — see question 3.
- A cached `Sender` keeps the multicast address computed at creation, as
  the matter.js session does (the cache key has no policy) — see
  by_design candidate C.
- A fabric's removal (`ForgetFabric`) closes its cached Senders; a closed
  Sender answers `ErrNoFabric` (GroupSession registers with the fabric,
  GS:104-105, :432-435).
- Exchange ids: a manager-wide `ExchangeCounter` (random start, wrap).
- The retainedSenderOnly mark is the removal call itself
  (`RemoveEndpointRetainingSender`), since this module prunes inside the
  removal rather than in a deferred reactor.
- `Sender.Write` writes the value as one AttributeDataIB (no list
  chunking) — see left out.

## Left out

- List chunking of a group `Write` (matter.js `Write` chunks list values
  into replace-all + appends by default).
- The GroupcastTesting event's sender side (as the brief scopes).
- Controller-side groups, `ClientGroup`, binding manager (AO), epoch-key
  rotation, MCSP.
- A live multicast-to-5540 run between two nodes (the test nodes cannot
  bind 5540 or IPv6 multicast in process; see §5).

## Outside the allowed paths (needed for `make ci`)

Two tests pinned the retired `BD-Matter-GroupcastNoSender` and failed with
the Sender feature; I flipped their assertions (no production change):
`bridge/groupcast_wire_test.go` (`TestGroupcastMalformedPayloadsOverTheWire`,
row "GroupcastTesting EnableSenderTesting": ConstraintError → Success) and
`examples/reference-bridge/wiring_test.go`
(`TestRootMountsGroupcastWithTheAuxiliaryACL`: FeatureMap 0x5 → 0x7, comment).

## The two `by_design` entries to retire, and the tests changed

`BD-Matter-GroupReceiveOnly` and `BD-Matter-GroupcastNoSender` (the owner
removes them; `docs/matterjs-comparison.md` rows 52 and 99, ADR 0010 and
the concept doc still name `BD-Matter-GroupcastNoSender`). Assertions
flipped:

- `cluster/core TestGroupcastAndAuxiliarySurfaceMatchSchema` — features
  `LN, PGA` → `LN, SD, PGA` (FeatureMap 0x5 → 0x7).
- `TestGroupcastJoinGroupParityMatterJS` — the "no endpoints" row removed
  from the ConstraintError table; new subtests "an empty endpoint list joins
  sender-only", "replacing the endpoints with none keeps the group
  sender-only".
- `TestGroupcastLeaveGroupParityMatterJS` — "…and the group with the last"
  (removed) → "…and keeps the group sender-only with the last" (member,
  binding kept, no aux ACL; LeaveGroup without endpoints then removes it);
  new "a Groups-cluster group kept sender-only keeps its policy".
- `TestGroupcastTestingParityMatterJS` — "EnableSenderTesting without SD"
  removed from the ConstraintError table; EnableSenderTesting now asserted
  Success with the 60 s period.
- `TestGroupcastMembershipReadsAndSurfaceParityMatterJS` — FeatureMap
  0x5 → 0x7.
- `bridge TestGroupcastMalformedPayloadsOverTheWire`, `examples
  TestRootMountsGroupcastWithTheAuxiliaryACL` (above).
- `groups/TestGroupLeavingTheTableLosesItsProperties` unchanged (the
  kDeleteGroupIfEmpty prune still applies to external removals).

## by_design candidates (not written)

- **A** — multicast egress on every multicast interface (D3) vs. matter.js's
  one interface (NodeJsUdpSocket.ts:298-325); a receiver on two interfaces
  sees the second copy as a replay (MessageReplay in GroupcastTesting).
- **B** — group messages leave from an ephemeral `[::]:0` socket, not the
  operational 5540 socket (source port differs from matter.js).
- **C** (finding rather than design) — a cached outbound session keeps its
  creation-time multicast address after a policy change (matter.js caches
  by fabric/session id/key only; mirrored).
- **D** — no list chunking for group writes.

## Questions

1. Should `bridge` expose its UDP listener (or a `SendMulticast` port) so
   group messages leave from 5540 as in matter.js? Then `node/groupsend.go`
   would hand that over instead of opening its own socket.
2. Brief line 2 above: confirm sender-only groups are *not* to be joined
   (file) rather than joined (brief).
3. matter.js compares `EpochStartTime` (spec: µs since 2000-01-01) with Unix
   ms (`Timestamp.fromMicroseconds` vs `Time.nowMs`), i.e. it treats
   epoch-us as Unix time; mirrored. A findings-register entry?
4. `ErrNoKey` now serves both the receive path ("no key candidate") and the
   send path (GroupKeySetMissingError); a separate sentinel if hosts need to
   tell them apart?

## Owner's decisions (Fable, 2026-10-14, tranche 20)

Spot-checked at the pin: MS:41-75 (legacy *send* counters) and
`ServerGroupNetworking.ts:63, :87` (joins the groups with endpoints only)
read as reported.

1. The bridge keeps its listener private for now; the ephemeral send
   socket is `BD-Matter-GroupSendFromEphemeralPort`, closing in the
   findings register.
2. Sender-only groups are not joined — confirmed.
3. The epoch-start comparison is a findings-register note (matter.js
   compares Matter-epoch µs as Unix ms; mirrored).
4. `ErrNoKey` shared by both paths stands.
5. `by_design`: `BD-Matter-GroupReceiveOnly` and `BD-Matter-GroupcastNoSender`
   removed; `BD-Matter-GroupSendOnEveryMulticastInterface` and
   `BD-Matter-GroupSendFromEphemeralPort` written. Candidate C (stale
   multicast address in a cached sender) is matter.js's own behaviour, no
   entry; D (no chunking) is a findings note. The two test assertions
   outside the brief's paths are accepted.
