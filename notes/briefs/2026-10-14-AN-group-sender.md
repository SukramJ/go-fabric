# Brief AN — the group message sender: outbound group sessions, the node-global group data counter, the Groupcast Sender feature (ADR 0020 stage 5, part 1)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AO
(binding manager, `binding/`, `cluster/core/binding.go`, `device/`,
`node/` binding wiring) at the same time: do not touch those; the only
shared file is `CHANGELOG.md` (append your own entries, the owner
merges). You run `make chiptool-test` once at the end; the other agent
does not. Branch `wave20/groupsend` from `planning/briefs-wave-20`,
pushed, **no PR**. Report `notes/briefs/reports/AN.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(stage 5 row), `notes/parity/by_design.md` `BD-Matter-GroupReceiveOnly`
and `BD-Matter-GroupcastNoSender` (this brief retires both — the owner
removes them, you flag every test they pin), `groups/` (the receiver
half: `Manager.Decode`, `MulticastAddressFor`, keys), `secure/channel`
(`privacy.go`, `session.go`'s nonce), `transport/message` (the packet
header codec: does it encode a group header?), `transport/udp`
(`Listener.Send`, `JoinGroup`), `cluster/core/groupcast.go`, `store/`
(`GetMetadataCounter`/`SetMetadataCounter`), `im/` client encoders
(brief AI), `controller/interaction` (`Invoke`, `InvokeOptions`).

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`),
paths under `packages/protocol/src/` unless noted: GS =
`session/GroupSession.ts`, SM = `session/SessionManager.ts`, ME =
`protocol/MessageExchange.ts`, MC = `codec/MessageCodec.ts`, G =
`groups/Groups.ts`, MS = `groups/MessagingState.ts`, ERR =
`groups/errors.ts`, CGI = `packages/node/src/node/client/ClientGroupInteraction.ts`,
GC = `packages/node/src/behaviors/groupcast/GroupcastServer.ts`, PEP =
`peer/PeerExchangeProvider.ts`. Security-relevant paths (`secure/`,
`groups/` keys): mirror line by line, cite each; no shortcuts.
Guards: `make ci`, `make race`, `make reachability-check`, `make fuzz`
(the codec gains an encoder; keep the fuzz targets building) and
`make chiptool-test` once (export `PATH=$HOME/go/bin:$PATH`; a `make ci`
that fails only on golangci-lint's timeout after "0 issues" is rerun as
`make lint`). One commit per §, reachability in each. Allowed paths:
`groups/`, `secure/channel`, `transport/message`, `transport/udp`,
`cluster/core/groupcast.go` (+ tests), `node/` (§5 only: the `Groups()`
surface), `script/coverfloor/floors.go`, `CHANGELOG.md`. `bridge/`,
`controller/`, `store/` schema, `endpoint/`, `device/`, generated files:
read, not changed — a change you need is a question in the report.
`notes/parity/` and ADRs are the owner's.

Out: the binding manager and `ClientGroup` as a registry node (AO and
later), groups on the controller side (`controller/client` groups),
MCSP beyond what GS/SM do, epoch-key rotation, the `GroupcastTesting`
event's sender side beyond EnableSenderTesting's state (GC:405-431).

## What matter.js does (verified by the owner at the pin)

- **Session:** `SessionManager.groupSessionForAddress` (SM:712-744): group
  id from the group node id (`GroupId.fromNodeId`, low 16 bits of
  `0xFFFF_FFFF_FFFF_xxxx`; 0 → error), the fabric's current key for the
  group (`fabric.groups.currentKeyForGroup`: GroupKeyMap → key set →
  the current epoch key; misses are `GroupKeySetMissingError` /
  `GroupKeyNotStartedError`, both `NoUsableGroupKeyError`, ERR:16-26),
  a cached session matched by (fabricIndex, sessionId, operational key)
  — never the id alone (:723-729), else `GroupSession.create`.
- **`GroupSession.create`** (GS:129-163): multicast address
  `fabric.groups.multicastAddressFor(groupId)` (policy: `ff05::fa` or
  `ff35:0040:fd<fabricId>:00<groupId>`, G:98-116 — Go has it), the UDP
  interface for it (none → error, :143-148), channel to port 5540
  (:150-153), privacy key = `MessagePrivacy.deriveKey(operationalGroupKey)`
  (:160); `usePrivacy` always true (:171-179: "the Security Flags SHALL
  have the P Flag set"); `supportsMRP = false`, long-lived (:74-75).
- **Header** (ME:706-732): sessionType Group, `sessionId` = the group
  session id, `messageId` from the session's counter, `destGroupId`,
  `sourceNodeId` = **our** node id on the fabric, P flag set, no
  destNodeId (MC:483's constraint); no MRP: no ack requested, no
  retransmission (:752).
- **Encode** (GS:251-287): payload AEAD with the operational group key,
  nonce = `Session.generateNonce(securityFlags, messageId, fabric.nodeId)`,
  AAD = the packet header bytes; then privacy: `mic` = last 16 bytes of
  the ciphertext, privacy nonce = `MessagePrivacy.buildNonce(sessionId,
  mic)`, header bytes from offset 4 obfuscated with the privacy key, the
  first 4 bytes kept.
- **Counter** (SM:192, :199, :981-1000): one node-global persisted group
  data counter, key `groupDataCounter`, reserve 1000, seeded above any
  legacy per-key maximum on first run (MS:15, :47, :71), rolls over to 0
  like CHIP with a Warn near rollover (spec §4.6.4), never throws.
- **Peer side** (PEP:75, :98-102; `Peer.ts:418-419`): a group address
  takes no CASE; the exchange is created on the group session. CGI:
  read, subscribe and timed requests are refused; `suppressResponse` is
  forced on invoke and write.
- **Groupcast Sender feature** (GC:83, :184-292, :328-368, :405-431):
  JoinGroup with an empty endpoint list is legal and leaves no group-table
  entry — the membership exists only in `groupProperties`
  (`retainedSenderOnly`, :269, :290-292, :850-868); LeaveGroup that
  removes the last endpoint keeps the group sender-only (:346-357);
  EnableSenderTesting sets `fabricUnderTest` with the 60 s timer
  (:79, :405-431).

## Owner's decisions

- **D1 — the sender lives in `groups/`** (`sender.go`): `Manager.Sender(ctx,
  fabricIndex, groupID) (*Sender, error)` resolves the key as SM:716-722
  (`ErrNoKey` extended with `ErrKeyNotStarted`), caches by (fabricIndex,
  sessionID, key) and builds the session; `Sender.Send(ctx, payload
  []byte, protocol uint16, opcode uint8) error` frames, encrypts,
  obfuscates and sends one datagram to the multicast address on port
  5540. The wire pieces go where their receive halves are: the group
  packet-header encoder in `transport/message` (if the codec cannot
  encode a group header yet), `channel.GroupEncrypt`/the privacy
  obfuscation in `secure/channel` beside the decrypt/deobfuscate it
  already has (read `privacy.go`, `session.go:350` and cite).
- **D2 — the counter is persisted through `store.SetMetadataCounter`**
  under key `groupDataCounter`, reserve 1000 (persist the reserved high
  mark before use, as the resumption/other counters in this module do —
  find the pattern and cite it), seeded above the per-key receive
  windows when the key is absent (MS:47, :71), rollover to 0 with a
  Warn `matter.groups.counter_near_rollover` (SM:988-997).
- **D3 — multicast egress:** `udp.Listener.SendMulticast(ip net.IP,
  port int, payload []byte) error` sends on every interface the
  listener joined multicast on (the egress interface is the socket's
  concern on Linux; `golang.org/x/net/ipv6` is a dependency already —
  use `ipv6.PacketConn.SetMulticastInterface` or per-interface sends,
  whichever `JoinGroup`'s implementation makes natural; cite GS:143-153
  for the "interface for the address" step).
- **D4 — the IM face:** `Sender.Invoke(ctx, path im.ConcreteCommandPath
  (endpoint ignored: a group invoke is group-addressed; read CGI for the
  path shape), fields spec.Encodable) error` encodes an InvokeRequest
  with `suppressResponse = true` through the `im/` client encoder
  (brief AI) and sends it; `Write` likewise; `Read`/`Subscribe`/timed
  are not offered (CGI). Timed invokes over a group are refused.
- **D5 — the Sender feature on `core.Groupcast`:** FeatureMap becomes
  `0x07` (Listener|Sender|PerGroup); the empty-endpoint JoinGroup, the
  sender-only retention on LeaveGroup and EnableSenderTesting behave as
  GC does; `groups.Manager` keeps sender-only memberships (no group-table
  entry, properties only) and `GroupcastMemberships` reports them so
  the bridge joins their multicast addresses too. The two `by_design`
  entries and the tests pinning them (`TestGroupcastAndAuxiliarySurfaceMatchSchema`,
  the "no endpoints" case of `TestGroupcastJoinGroupParityMatterJS`)
  are updated to the new behaviour — list every assertion you flipped.
- **D6 — no new dependency; no cgo.**

## §1 — codec and crypto (`transport/message`, `secure/channel`)

The group packet header encoder (MC:483's constraint enforced: destGroupId
and sourceNodeId present, destNodeId absent, session type Group, P flag);
`channel.EncryptGroup(key, nonceSource nodeID, header, payload)` mirroring
GS:251-266 and the privacy obfuscation of GS:267-286 (`DerivePrivacyKey`
exists; add the obfuscate counterpart of the deobfuscate the receive
path uses, with `buildNonce`'s sibling for the privacy nonce
`MessagePrivacy.buildNonce(sessionId, mic)` — cite
`packages/protocol/src/codec/MessagePrivacy.ts` lines). Round-trip tests
through the existing receive path (`groups.Manager.Decode`): a frame the
sender encodes is the frame the receiver decodes, privacy on; a fuzz
target for the encoder if `make fuzz` lists one per codec.

## §2 — the counter (`groups/counter.go`)

D2. Tests: first run seeds above the receive windows; restart resumes
past the reserved mark; rollover to 0 warns once.

## §3 — the session and the sender (`groups/sender.go`)

D1, D3, D4. `Sender` holds fabricIndex, group id, session id, key set
id, operational key, privacy key, multicast address, our node id on the
fabric. Errors: `ErrNoKey` (map or key set missing), `ErrKeyNotStarted`,
`ErrNoInterface`. Tests: key selection (mapped/unmapped/not started),
the cache keyed by key not id (two key sets hashing to one session id
— construct it), the header and datagram shape, `Invoke` with
`suppressResponse`, timed refused.

## §4 — the Groupcast Sender feature (`cluster/core/groupcast.go`, `groups/`)

D5. Tests: the parity surface (FeatureMap 0x07), empty-endpoint JoinGroup
retains a sender-only membership that `GroupcastMemberships` lists and
LeaveGroup of the last endpoint keeps, EnableSenderTesting sets the
fabric under test and the 60 s timer clears it; the receiver-side tests
untouched.

## §5 — `node.Node.Groups()` and the live proof

`groups.Manager` is reachable through `node.Node.Groups()` already; add
what §3 needs there (the listener for `SendMulticast`, our node id per
fabric from the store) without changing `node.Config`. Live proof
`TestLiveGroupSend` in `groups/` or `node/`: two in-process `node.Node`s
commissioned into one fabric by the module's commissioner (the AK/AL
live-test harness, `controller/client/live_test.go`'s `newLive`, is the
pattern; the registry can commission both), the controller writes the
same key set and GroupKeyMap to both and JoinGroup (receiver: endpoint
list with its OnOff endpoint; sender: empty list), the sender's
`Groups().Sender(fabric, group).Invoke(OnOff Toggle)` flips the
receiver's OnOff. If loopback multicast does not deliver on the test
host, say so and prove the frame instead by sending it to the
receiver's unicast address (the receive path accepts a group header on
any datagram — verify in `bridge/group_messaging.go` and cite) and the
multicast path by a `transport/udp` test on a real interface skipped
without one.

## Report

As AK–AM: per §, guards with HEAD, brief lines that differ from the
files, decisions, left out, questions, `by_design` candidates (not
written) and the two entries to retire with the tests you changed.
