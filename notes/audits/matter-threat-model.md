# Matter threat model — go-fabric

> **Why this file is not beside `README.md`.** Everything at the module root is
> part of the published surface a consumer reads before depending on go-fabric.
> This document is the opposite: a working record of where the security
> properties are enforced, what they assume, and where they stop — including
> entries that are unresolved. It belongs in a working-notes tree
> (`notes/audits/`) so the root stays a description of what the module *is*,
> and so a later audit has somewhere to land next to this one rather than
> competing with the README for the reader's trust.

---

## Scope and method

The module is a Matter **node/bridge** implementation: it accepts PASE and CASE
handshakes, serves the interaction model, and persists fabrics, identities,
ACLs and group keys. It is not a commissioner.

Every claim below is anchored to a `path:line` that was read. Claims that could
not be settled from the tree are marked **NOT VERIFIED** in those words rather
than closed with a plausible answer.

Two structural facts govern the whole document:

1. **go-fabric is a library.** Several enforcement points are *optional
   capabilities* the host wires. Where a capability is absent, this document
   states what the code then does — because "the host will wire it" is an
   assumption, not a control.
2. **The store is handed in.** `store.New(db *sql.DB)` (`store/store.go:21`)
   takes an already-open database. File location, permissions and at-rest
   encryption are entirely outside this module.

---

## 1. PASE passcode brute-force budget

### What limits an attacker's attempts

| Control | Value | Where |
| --- | --- | --- |
| Failures before lockout | 20 | `bridge/securechannel.go:480` (`paseMaxErrors`) |
| First lockout duration | 15 min | `bridge/securechannel.go:519` |
| Backoff | doubles per consecutive lockout, capped at 4 h | `bridge/securechannel.go:617-633` |
| Concurrent handshakes | exactly one | `bridge/securechannel.go:536-547` (`claimPaseInFlight`) |
| Abandoned-handshake timeout | 60 s | `bridge/securechannel.go:496` |
| Per-source dedup windows | 256 | `bridge/securechannel.go:489` |

The counter is incremented only on a *genuine* pairing failure — a decode
failure or a SPAKE2+ confirmation mismatch. Missing-handler and state-replay
conditions are excluded (`bridge/securechannel.go:706-747`); the increment sits
at `:747`.

### What happens when the budget is exhausted

`recordPaseFailure` (`:567`) fires exactly once, on the transition to 20:

1. `engagePaseLockout` (`:617`) sets `paseLockoutUntil`, bumps the backoff
   streak, and **resets the failure counter to 0** — from that point the
   cooldown, not the counter, is what keeps PASE closed.
2. The open commissioning window is revoked.
3. Every subsequent `PBKDFParamRequest` / `Pake1` / `Pake3` is dropped before
   dispatch by the `paseLockedOut()` gate at `bridge/securechannel.go:321`.

The lockout expires on its own (`paseLockedOut`, `:645`) and is cleared
immediately when a fresh acceptor is installed (`resetPaseFailures`, `:664`).
The one exception — the cap's own `RevokeWindow` re-attaching the acceptor and
thereby unlocking itself microseconds later — is guarded by
`preserveLockoutOnReset` (`:594-599`, honoured at `:672`).

### Passcode space

`commissioning/pase.go:70` rejects passcodes outside `1..99999998`;
`:73` pins PBKDF2 iterations to `1000..100000` and `:76` the salt to 16–32
bytes. `secure/setup/setup.go:275` (`IsValidSetupPIN`) additionally rejects the
trivial PINs listed at `:257`. Confirmation is compared with
`subtle.ConstantTimeCompare` (`secure/spake2/spake2.go:412`, `:543`).

**Effective budget: 20 guesses per 15 minutes (80/h) against a ~10^8 space, and
the rate halves with every further lockout.**

### The assumption this rests on

That the attacker must *guess*. Every control here is a rate limit; none of
them is a secret. If the passcode leaks — printed label photographed, QR code
shared, a host that ships a fixed passcode across a product line — the whole
section is irrelevant, because the attacker needs one attempt, not 20.

### What the attacker must already have

Only IP reachability to the bridge's Matter UDP port. Nothing else: PASE is
answered on the unsecured session type before any authentication exists.

### What is NOT defended against

- **Pairing denial of service.** The counter is a single bridge-wide atomic
  (`b.paseFailures`, incremented at `:568`), not per source address. Twenty
  malformed `Pake1` datagrams from any LAN host disable pairing for 15 minutes,
  then 30, then 60. The design chose this knowingly (`:498-517`), and the
  operator's way out is to open a new pairing window.
  **Corrected after verification: an earlier version of this section claimed
  the only recovery is a daemon restart. That is wrong, and this document
  contradicted itself — the cooldown expires unattended
  (`paseLockedOut`, `bridge/securechannel.go:645-649`), which the section above
  already says and `TestBridge_PaseLockoutExpiresOnItsOwn` already pins.**
  The counter being device-wide rather than per-peer is also not a divergence:
  matter.js keys it per `PaseServer` with no source component
  (`packages/protocol/src/session/pase/PaseServer.ts:42,:96,:107`) and chip
  keeps one `mFailedCommissioningAttempts`
  (`src/app/server/CommissioningWindowManager.cpp:44,:161`). Both end *worse*
  than this implementation: at the cap they stop PASE for good rather than for
  a cooldown. The denial-of-service surface is real and shared with both gold
  standards; a per-source budget would diverge from them and buy nothing,
  because the source address is unauthenticated and spoofable at this stage.
- **A passcode acceptor that outlives the window.** `dispatchPase` is reached
  whenever a handler is attached; the switch at `bridge/securechannel.go:320`
  checks only the lockout, never whether a commissioning window is open. The
  reference host attaches the provider once at boot
  (`examples/reference-bridge/wiring.go:412`). So an uncommissioned bridge
  answers PASE from its configured passcode indefinitely. This is a deliberate
  divergence from matter.js (whose `PaseServer` dies with the window) and it is
  precisely why the lockout had to be invented.
- **Offline attack on a captured handshake.** Not applicable to SPAKE2+ by
  construction, but note the iteration count is as low as 1000
  (`commissioning/pase.go:73`) if the host chooses it — that is the spec floor,
  not a hardened value.
- **Timing side channels outside the confirmation compare.** The tag compare is
  constant-time; the surrounding decode paths were not analysed for timing.
  **NOT VERIFIED.**

---

## 2. Fail-safe abuse

### The window

`ArmFailSafe` is handled at `cluster/core/general_commissioning.go:491`.
Defaults: single-arm max 900 s (`:207`), cumulative max 900 s (`:213`). A PASE
session that completes Pake3 gets a 60-second window armed automatically
without asking (`AutoArmOnPaseEstablished`, `:866`).

### What an attacker can do inside an armed window

Holding a session that armed the fail-safe, the caller may invoke the
fabric-mutating OpCreds commands — `CSRRequest`, `AddTrustedRootCertificate`,
`AddNOC`, `UpdateNOC` — each of which is otherwise rejected with
`FailsafeRequired` (`cluster/core/operational_credentials.go:504-510`; the
armed-checks at `:1208`, `:1336`, `:1701`, `:1949`). In practice: install a
fabric on the bridge, with its ACLs and group keys.

Three ownership rules bound this:

- A CASE session may not arm while a commissioning window is open for another
  admin and nothing is yet armed (`:520-525`) → `BusyWithOtherAdmin`.
- Once armed by a fabric, only that fabric may re-arm **or disarm** (`:535`).
  This is what stops fabric B from disarming fabric A's window mid-flow and
  rolling back A's pending NOC.
- `CommissioningComplete` is refused over PASE, refused with no armed
  fail-safe, and refused when the requesting fabric differs from the arming
  fabric (`:718-734`).

### What the expiry path rolls back

`watchFailSafeExpiry` (`:641`) disarms, zeroes the Breadcrumb (`:659`), resets
the cumulative cap, and fires `onFailSafeExpired`. Wired to
`OperationalCredentials.OnFailSafeExpiry` (`:486`) that means:

- all pending commissioning state cleared — pending private key, trust root,
  CSR nonce/session, `nocWasInvoked` (`clearPendingState`, `:440`);
- if `AddNOC` had already completed, `revertAddNOC` (`:2077`) deletes the
  fabric's ACL entries (`:2082`), its group keys (`:2083`), and the fabric row
  itself (`:2084`).

A disarm (`ExpiryLengthSeconds == 0`, `:542`) runs the **same** revert path, so
an early disarm cannot leak a half-installed NOC.

### The assumption this rests on

That the fail-safe timer actually fires. It is a per-arm goroutine
(`go g.watchFailSafeExpiry`, `:617`) whose `select` also returns on
`ctx.Done()` (`:646-650`) — the context is the *invoke* context of the
`ArmFailSafe` command. If a host cancels that context when the command
completes rather than at shutdown, the watcher returns without reverting and
the armed state is never cleaned up by the timer. Whether any host does this
is **NOT VERIFIED**; the reference bridge was not traced through to its invoke
context lifetime.

### What the attacker must already have

Either a completed PASE handshake (which needs the passcode, and which
auto-arms a 60 s window at `:866` before the commissioner asks for anything),
or an established CASE session on a fabric.

### What is NOT defended against

- **`SetRegulatoryConfig` is not reverted.** `g.regulatoryConfig` is written at
  `cluster/core/general_commissioning.go:690` and appears nowhere else as an
  assignment except construction (`:219`) — verified by grepping the identifier
  across the tree. The fail-safe expiry path does not restore it. Regulatory
  config is low-impact for an Ethernet-only bridge.
  **Corrected after verification: this is not a divergence.** The specification
  puts a non-fabric-scoped rollback at RECOMMENDED, not mandatory, and neither
  gold standard does it. matter.js enumerates the fail-safe rollback steps 1–8
  and leaves step 9 — *"Optionally … it is RECOMMENDED that the Node rollback
  the state of all non fabric-scoped data"* — as a TODO
  (`packages/protocol/src/common/FailsafeContext.ts:284-330`), and its server
  restores only the NetworkCommissioning networks and the breadcrumb
  (`ServerNodeFailsafeContext.ts`). chip's `src/app/FailSafeContext.cpp` carries
  no regulatory handling at all. Recorded as a known gap at RECOMMENDED tier
  rather than as a defect.
- **~~Fail-safe armed over PASE has no owner.~~ FIXED — this was the one
  finding of the four that survived verification, and it is closed.** AddNOC
  now re-stamps the window onto the fabric it installed
  (`OpcredsConfig.RearmFailSafeForFabric`, Matter §11.18.6.16), which is what
  both references do and what lets the ownership check below be plain
  equality. The description that follows is the state before that change,
  kept because it explains why the check reads the way it does now.
- **~~(historical)~~ Fail-safe armed over PASE has no owner.** A PASE arm records
  `failSafeFabricIndex = 0`. `CommissioningComplete`'s ownership check is
  `g.failSafeFabricIndex != 0 && sessFabric != ...` (`:730`) — so when the
  window was armed over PASE, **any** CASE fabric can complete it.
- **The 60-second auto-arm is not attacker-gated beyond PASE itself.** Anyone
  who completes PASE gets an armed window without sending a command.
- **Window-open denial.** A CASE session on fabric A can hold an armed
  fail-safe for up to the cumulative cap; while armed, `OpenWindow` is refused
  by the `FailSafeChecker` guard (`bridge/commissioning_window.go:319`). A
  hostile-but-authorized admin can therefore block other admins from opening a
  commissioning window for the cap duration.

---

## 3. Fabric isolation

### Where the fabric index comes from

This is the load-bearing point of the whole model, and it is worth stating
plainly: **the fabric index of a fabric-scoped element is never read from the
wire.**

That qualifier is load-bearing, and an earlier version of this section omitted
it — which produced a false finding against `RemoveFabric`. Matter distinguishes
the two cases by access class on the same cluster: `UpdateFabricLabel` (0x9) is
`access: "F A"`, fabric-scoped, and takes its index from the session;
`RemoveFabric` (0xa) is `access: "A"` with `FabricIndex` a **mandatory request
field**, constraint `"1 to 254"`
(matter.js `packages/model/src/standard/elements/operational-credentials.element.ts:120-124`
and `:127-130`). Both references read it off the wire deliberately — matter.js
`OperationalCredentialsServer.ts:395-398` against
`this.context.session.associatedFabric` two methods above, and chip
`OperationalCredentialsCluster.cpp:817-862`, whose post-step treats removing
*another* fabric as the normal case. The gate there is privilege, not scope:
Administer (5), enforced at `endpoint/dispatcher.go:754-767`. An
admin-privileged fabric removing another is the spec-sanctioned multi-admin
model.

For everything that IS fabric-scoped, the invariant holds: The receive path
resolves a session by `Header.SessionID` (`bridge/receive.go:288`), decrypts
under that session's keys (`:310`), and only then asks the session table which
fabric that session belongs to (`resolveSessionFabric`,
`bridge/subscribe.go:1058`). The result is stamped into the request context
(`bridge/receive_dispatch.go:139/141` for reads, `:293/295` writes, `:419/421`
invokes) and every fabric-scoped cluster reads it back via
`im.FabricFilterFromContext`.

The binding to that index is made at CASE establishment:

- Sigma1's `DestinationID` is an HMAC over the fabric IPK, root public key,
  fabric id and node id; the responder resolves *which* identity to answer with
  by matching it (`secure/sigma/protocol.go:953`).
- Sigma3 checks the peer NOC's fabric-id against the responder's
  (`secure/sigma/protocol.go:1129-1137`), and — the ordering comment at
  `:1139-1146` is the important part — nothing lifted out of the peer
  certificate reaches responder state until `verifyTranscript` (`:1147`) proves
  the sender owns it.

### Where isolation is enforced

- **Reads/writes/invokes:** `TopologyDispatcher.CheckACL`
  (`endpoint/dispatcher.go:790`). It fails closed on every non-grant path,
  including "no ACL source wired at all" (`:794-802`).
- **Fabric-scoped attribute projection:** `AccessControl.MatterReadFiltered`
  serves only the requesting fabric's entries (`cluster/core/access_control.go:382-398`).
- **Subject matching:** an ACE's `Subjects` list is matched by exact
  operational node id or by CAT (`aclSubjectMatches`, `endpoint/dispatcher.go:851`).
- **Group sessions cannot read:** Read/Subscribe/Timed over a group session are
  rejected before dispatch (`bridge/im_gate.go:45-50`), so a multicast Read
  cannot enumerate the attribute tree.

### The assumption this rests on

**That the host wired the fabric resolver.** `resolveSessionFabric` returns `0`
when the session lookup does not implement `SessionFabricResolver`
(`bridge/subscribe.go:1065-1068`), and `OperationalSessionLookup.FabricFor`
returns `(0, false)` when built without the closure
(`bridge/handlers.go:821-826`). Fabric index `0` means PASE — and
`CheckACL` returns **`StatusSuccess` unconditionally** for fabric 0
(`endpoint/dispatcher.go:791-792`).

So a host that wires `NewOperationalSessionLookup(...)` and forgets
`.WithFabricResolver(...)` gets a bridge where **every authenticated CASE
session bypasses the ACL entirely**, silently, with no error and no log. The
reference wiring gets it right and even says so in a comment
(`examples/reference-bridge/wiring.go:352-353`); nothing enforces it. This is
the single highest-value finding in this document.

### What the attacker must already have

To reach the ACL gate at all: a completed CASE handshake, which requires a NOC
signed by a root the bridge trusts, on a fabric whose id matches, plus the
fabric IPK. That is a high bar. The realistic attacker here is therefore
**another legitimate fabric** — a second admin on a multi-admin bridge — not an
outsider.

### What is NOT defended against

- **A PASE session bypasses all ACLs for its lifetime.** By design
  (commissioning must work before any ACL exists), but note the consequence in
  §4 below.
- **`AdoptFabricIndex` has no production caller.** Grepping the identifier
  across the tree outside `secure/operational/` and tests returns nothing. So
  after `AddNOC`, the PASE session that installed the fabric keeps fabric index
  0 — i.e. keeps its ACL bypass — until it is closed. Whether hosts are
  expected to call it is a documented capability
  (`secure/operational/manager.go:752-770`); that no in-tree caller does is a
  measurement, not a verdict.
- **A missing subject resolver degrades, silently, to wildcard-only matching.**
  `resolveSessionSubject` returns `(0, nil)` when unwired
  (`bridge/subscribe.go:1081-1090`); `aclSubjectMatches` then matches only ACEs
  with an empty `Subjects` list (`endpoint/dispatcher.go:852`, and the
  `subjectNodeID != 0` guard at `:865`). That direction fails closed, which is
  the right way round — but it is a behaviour change no one is told about.
- **The Sigma3 fabric-id check is conditional on an optional interface.**
  `if extractor, ok := r.verifier.(PeerFabricIDExtractor); ok`
  (`secure/sigma/protocol.go:1129`). A host supplying its own verifier that does
  not implement it loses the check with no diagnostic. The module's own
  `mattercert.Verifier` implements all three extractors
  (`secure/mattercert/verify.go:189`, `:214`, `:237`).
- **Cross-fabric resource exhaustion.** Session-table and subscription quotas
  were not analysed here. **NOT VERIFIED.**

---

## 4. Group-key handling

### Where the keys live

Epoch keys arrive by `KeySetWrite` and are persisted verbatim into
`matter_group_keys` — three `(EpochKey, EpochStartTime)` pairs per key set,
scoped by `(fabric_index, group_key_set_id)` (`store/groupkeys.go:28-38`,
upsert at `:42`). Each key is exactly 16 bytes, enforced at
`cluster/core/group_key_management.go:549-552`.

They are stored **in the clear**. The sibling comment on the operational
private key states the position outright: *"Persisted as-is; at-rest encryption
is the operator's responsibility"* (`store/identity.go:25-27`). The same
applies to the fabric IPK stored in the same row (`store/identity.go:29-31`).

### How they are derived

Two distinct derivations:

- **Operational IPK** — `DeriveOperationalIPK`
  (`secure/sigma/ipk.go:64`): `HKDF-SHA256(ikm = raw AddNOC IPKValue,
  salt = compressedFabricID, info = "GroupKey v1.0", L = 16)`. Every input is
  cited to matter.js HEAD in the doc comment rather than to spec prose. This
  value is the leading prefix of every CASE Sigma HKDF salt
  (`secure/sigma/protocol.go:250`, `:298`, `:316`).
- **Group session keys from epoch keys** — **not implemented in this module.**
  Grepping `GroupSecurityInfo`, `DeriveOperationalIPK` and `SessionGroup`
  across the tree finds no production consumer that turns a stored epoch key
  into an encryption key. `SessionGroup` appears in exactly three
  non-test places: the constant (`transport/message/message.go:57`), the
  header-validation allow-list (`:263`), and the IM gate that rejects group
  reads (`bridge/im_gate.go:45`). The only decrypt path is
  `lookup.Lookup(hdr.SessionID)` (`bridge/receive.go:288`), which resolves
  unicast sessions.

### What their compromise costs

Split the answer, because the two halves differ sharply:

- **Inside go-fabric: little.** No code path uses a stored epoch key to
  encrypt, decrypt or authenticate anything. Reading `matter_group_keys` gives
  an attacker no access to this bridge that reading the same file's
  `matter_node_identities` table has not already given them far more of.
- **Outside go-fabric: the fabric's whole group plane.** Epoch keys are
  *fabric-wide* secrets shared with every node in the group. Their compromise
  lets an attacker forge and decrypt group (multicast) traffic against **other**
  vendors' nodes on that fabric — nodes that do implement group messaging.
  go-fabric is a custodian of a secret whose blast radius is entirely off-box.

That asymmetry is the point: the cost of losing these keys is not measured on
the machine that lost them.

### The assumption this rests on

That the database file is protected by the host — filesystem permissions,
full-disk encryption, or a host-supplied encrypted `*sql.DB`. `store.New`
(`store/store.go:21`) accepts whatever it is handed and sets no policy.

### What the attacker must already have

To *write* group keys: `Administer` privilege on the fabric
(`cluster/core/group_key_management.go:172-176`). To *read* them: filesystem
access to the database, because the wire never returns them — `KeySetRead`
deliberately omits the `EpochKey*` fields per §11.2.10.6.3
(`cluster/core/group_key_management.go:651-653`).

### What is NOT defended against

- **Anything with read access to the database file.** Group epoch keys,
  per-fabric IPKs and operational private keys are all plaintext in the same
  SQLite file. No key wrapping, no OS keychain, no separation between the
  key material and the rest of the model.
- **A PASE session writing group keys, if the host wires
  `SetCurrentFabric`.** `MatterInvoke` derives the fabric from the IM context
  and falls back to `g.currentFabric` when that is 0
  (`cluster/core/group_key_management.go:414-418`). Today that fallback is inert
  — `SetCurrentFabric` has no production caller anywhere in the tree (verified
  by grep; the code says so itself at `:331` and `:408`). If a host ever wires
  it, a PASE session would resolve to that fabric, and because PASE bypasses
  `CheckACL` (`endpoint/dispatcher.go:791`) the `Administer` requirement above
  would not apply. The safety here is the absence of a caller, not a check.
- **Key rotation and epoch-start enforcement at use time.** The write path
  validates epoch ordering (`:520-530`); nothing consumes `EpochStartTime` to
  retire a key, because nothing consumes the keys at all.
- **Deletion is not shredding.** `RemoveGroupKeySet` / `RemoveGroupKeysByFabric`
  (`store/groupkeys.go:120`, `:134`) issue SQL `DELETE`s. Whether the bytes
  survive in the SQLite file or WAL afterwards is **NOT VERIFIED** — and no
  `VACUUM` or secure-delete pragma appears in `store/schema.sql`.

---

## 5. `//nolint` inventory under `secure/`

**Rewritten after the inventory was acted on.** It found 67 directives — 15 in
production files, 52 in tests — and named six production ones as vague or
unsupported plus a `see #20` reference appearing in seven comments. All of that
has been resolved; the counts below are the state after, and the sections that
listed the old ones are gone rather than kept as a record of work already done.

**21 directives remain: 15 in production, 6 in tests.** 46 were removed.

The lint config matters for reading these. `.golangci.yaml:92-101` excludes
`contextcheck`, `errcheck`, `funlen`, `gocognit`, `gocyclo`, `gosec`, `noctx`
and `unparam` on `path: _test\.go`. `staticcheck` is **not** in that list.

### 5.1 What the cleanup found

Two thirds of the directives suppressed nothing at all:

- **Seven `gosec` directives in test files.** The config already excludes
  `gosec` there, so each one was decoration. That is worse than harmless: a
  reader who learns that `//nolint` often means nothing stops reading them, and
  the one that hides something real goes with the rest.
- **39 `SA1019` directives on `elliptic.Marshal`.** The deprecation was real;
  the suppression was the wrong answer. The call sites moved to
  `ecdsa.PublicKey.Bytes`, which is the exact inverse of the
  `ecdsa.ParseUncompressedPublicKey` the decoder already uses, and the
  directives went with them.

**`see #20` resolved to a tracking issue in the repository this code was
extracted from.** Dead here. All seven pointers were replaced with the
substance they were pointing at — a reference a reader cannot follow is worse
than no reference, because it looks like due diligence.

### 5.2 What the vague ones were hiding

Four of the six named production directives turned out to suppress something
worth stating precisely, and one of them was hiding a defect.

`verify.go`'s G115 suppression covered a `uint64` conversion of `Unix()`. Writing
down what the wrap actually does surfaced a **second, worse case that needed no
exotic clock**: `NotBefore + matterEpochUTCSeconds` overflows for a field close
to 2^64, wrapping to a small number that every real clock is past. With
`NotAfter == 0` — the long-lived RCAC convention — `decode.go`'s ordering check
does not fire either, so **a certificate with a nonsense NotBefore was accepted
on an ordinary clock**. Measured: `NotBefore = 2^64-1001` becomes 946683799.

Both conversions are checked now (`matterToUnixSeconds`), and three tests pin
it, including the negative control that an ordinary never-expiring certificate
still verifies. The pre-1970 clock case remains annotated rather than checked,
because there the comparisons part company in the safe direction: a certificate
carrying a NotAfter is still rejected as expired.

The other three — `aesccm.go`'s two length conversions, `tbs_der.go`'s
timestamp conversion, `builder.go`'s legacy hash — are warranted, and their
comments now name what bounds the value and what happens if that bound is ever
wrong, rather than asserting safety.

### 5.3 What is not guarded

`nolintlint` is not enabled, so nothing stops a future directive from being as
vague as the ones removed here. That is a deliberate gap rather than an
oversight: the linter's own check for unexplained directives would fire on
every one of the 15 that remain, all of which carry explanations it cannot
read. Re-examining them is a review task, and this section is where the last
one was written down.
