# Review II, phase 0 — diff review: crypto, commissioning servers, groups, transport

Diff `95e03096..fd3d535b` over `secure/ transport/ groups/ commissioning/ cluster/core/ node/ contract/ client/handshake/`.
Sources read at `fd3d535b` (the working tree; `HEAD` `dbba76ac` differs from it in `CHANGELOG.md` only).
References read at the pins: matter.js `db021da0`, connectedhomeip `6170af84`.
Measured = traced in code or run; anything else is marked PLAUSIBLE with what would settle it.

## Findings

Format: `ID | severity | verdict | file:line | defect | failure scenario | proposed fix`

**R2-P0-rv-B01 | high | CONFIRMED (reproduced) | `groups/decode.go:343-357`, security argument at `:313-323`** |
A recorded datagram of a sender the table does not hold is accepted ("trust first") *and* evicts the least recently used sender — no key needed — so once 16 or more distinct senders have been recorded under one operational key, every recorded group message can be replayed without limit; the comment's "whoever lacks the key cannot cause it" (`:317-318`) is false. |
Scenario: a fabric whose group key set is used by 16 senders (switches, controllers; one key set usually serves all groups of a fabric, and the table is per key, not per group). An on-link party without the key records one multicast datagram of each. Replaying them round-robin, the sender whose turn it is is always the one just evicted: its fresh window accepts the old counter, it takes the front, the next sender in the cycle falls off. Throwaway test (`go test -overlay`, `m.Decode` on the byte-identical datagrams, 16 senders, 5 rounds): 80 of 80 replays came back `Duplicate == false`; `bridge/group_messaging.go:295` drops on `Duplicate` only, so each replay is dispatched (an old Toggle, an old scene recall). After one eviction the evicted sender's whole recorded history is replayable in ascending counter order, not "once more" (`mrp.Window.Accept`, `transport/mrp/window.go:142-178`: everything ahead of the trusted first counter is new). Before the diff the map was unbounded and a byte-identical replay was refused for as long as the key lived; matter.js at the pin has no bound (`MessagingState.ts:80-90`). connectedhomeip has the same weakness (`GroupPeerMessageCounter.cpp:79-95` evicts on a not-found lookup) — the citation holds, the argument built on it does not. The memory growth the bound closes needed a key holder; the replay it opens does not. |
Fix: keep the bound for the *windows* but not the trust decision — on eviction keep the evicted sender's last maximum counter in a second, much larger and still bounded table (source node id + uint32 per entry) and prime a re-entering sender's window from it instead of trusting its first counter; or raise the per-key bound far above any real sender count together with a global cap. Either way correct the comment: eviction is caused by any authenticating message of an untracked sender, replayed ones included. Add the regression case (16 recorded senders, round-robin replay).

**R2-P0-rv-B02 | low | CONFIRMED | `groups/decode.go:290`, `transport/btp/session.go:202-203`, `secure/spake2/spake2.go:652-669`, `secure/sigma/zero.go:6-18`** |
Three deliberate departures from matter.js name `by_design` as their record ("by_design, Ahead of matter.js"; "owner decision 2026-11-13, by_design"; "matter.js does not") and `notes/parity/by_design.md` at `fd3d535b` carries no row for any of them — no hit for `GroupPeerMessageCounter`, `MessagingState.ts:80`, `BLEEndPoint.cpp:1014`, `BtpSessionHandler.ts:123`, `ClearSecretData`, `bound-A07`, `W1-wire-07`, `W1-sec-05`; `docs/security.md` has none either. |
Scenario: the next pin lift re-reads the *Ahead of matter.js* register (CLAUDE.md, Workflow 5) and finds no marker for the reception bound or the window-0 refusal, so neither is reconciled; a reader following the comment finds nothing. |
Fix: add the rows (reception bound with marker `MessagingState.ts:80-90`; BTP window 0 with `BtpSessionHandler.ts:123`, `:180-191`; key-material overwrite with `PaseServer.ts:174-190`, `CaseServer.ts:243-285`), or drop the words "by_design" from the comments.

**R2-P0-rv-B03 | low | CONFIRMED | `secure/operational/manager.go:826`, `:883`; `secure/spake2/spake2.go:315` (Verifier.ke), `:498` (Prover.ke); `commissioning/pase.go:173-181`** |
The subject of `fd3d535b` ("the PASE shared secret and the bytes the session keys are cut from are overwritten once the session holds its keys") holds on the controller side only: on the device side `OpenFromPase`/`OpenFromPaseWithID` leave their 48 derived bytes (`derived`) to the collector, and on both sides the handshake object's own Ke is never overwritten after a success — `node/security.go:645` and `client/handshake/pase.go:185-187` clear the *copy* `SharedSecret()` returned (`spake2.go:471-472`, `:647-648`), while `bridge/handlers.go:490-492` and `pairPASE` just drop the Verifier/Prover. `commissioning.PASEResponder.sessionKeys` clears neither its Ke copy nor the 48 bytes, and re-derives them on every `Session()`/`AttestationChallenge()` call. |
Scenario: a heap image taken after a commissioning (core dump, swap) still holds Ke and I2R‖R2I‖AttestationChallenge of the PASE session on the device, which is what the change set out to remove; nothing fails functionally. |
Fix: `defer clear(derived)` in both `OpenFromPase*` (channel.New and the entry copy, `manager.go:887-901`); give Verifier and Prover a `Close()` that overwrites `ke` and call it where the adapter and `pairPASE` drop them; the same two clears in `commissioning/pase.go`.

**R2-P0-rv-B04 | low | PLAUSIBLE | `cluster/core/identify.go:285-293`, `:401-423`, with `Close` at `:184-195`; contract text `contract/identify.go:22-35`** |
`op` is now held across the host's `MatterIdentify` on the request path, and the countdown goroutine takes `op` on every tick; `Identify.Close` waits for that goroutine (`countdown.Wait`). A tick that falls into a running handler therefore parks `Close` until the handler returns — before the diff only the one tick that reached zero called into the host. |
Scenario: goroutine A (topology change) holds the host's device-model lock and removes the endpoint → `Identify.Close()`; goroutine B serves an Identify command and is inside the host's `MatterIdentify`, waiting for the same lock; the 1 s tick fires, the countdown goroutine blocks on `op` (held by B), `Close` blocks on the goroutine, A never releases the lock. The contract text warns about setting IdentifyTime from the handler and about returning promptly, not about locks held across endpoint removal. Settles it: whether any host removes endpoints (Reassemble) while holding a lock its `IdentifyHandler` takes — not checked in the hosts. |
Fix: have the countdown goroutine honour `done` while it waits for `op` (try-lock loop or a channel-based queue), so `Close` does not depend on a handler in flight; or state the rule in `contract/identify.go`.

**R2-P0-rv-B05 | low | PLAUSIBLE | `cluster/core/general_commissioning_rolled_back_test.go:65-73`, assertion at `:93-95`** |
The "the timer" case leaves its wait loop as soon as *one* hook is recorded and then asserts the full list; with a host hook the expected list is `[host owner]`, appended by the watcher goroutine in two separate critical sections. |
Scenario: the test's first `hooks(r)` reads between `note("host")` and `note("owner")` (it can even hold `r.mu` while the watcher waits for it and win it again for the second read) → `hooks ran [host], want [host owner]`. Not seen in 40 runs (`-count=40`), so the window is narrow; it is a failure that would be blamed on the product. |
Fix: wait for `len(hooks(r)) == len(want)`.

**R2-P0-rv-B06 | low | PLAUSIBLE | `cluster/core/general_commissioning_watcher_test.go:70-80`** |
After the deadline the test gives the watcher goroutine 50 `Yield`s (about 50 ms of wall time, `clocktest/fake.go:265-270`) and then asserts `fired == 1`; nothing synchronises with the goroutine. |
Scenario: a loaded runner (the race job) schedules the watcher later than that → "the expiry fired 0 time(s), want once". Settles it: a run under `-race` with the machine busy; not done here. |
Fix: wait on a channel the hook closes, bounded by the test deadline.

**R2-P0-rv-B07 | note | PLAUSIBLE | `cluster/core/general_diagnostics.go:1129-1150`, `:1076-1116` (removed: `syntheticEthernetIface`, `maxIfacesForApple = 4`)** |
The change follows matter.js faithfully (`GeneralDiagnosticsServer.ts:505-518`: filter, sort, `slice(0, 8)`, no synthetic entry) but deletes two guards whose removed comments recorded observed Apple Home failures: an empty list ("No enumeration/topology dictionary found", RemoveFabric about 5 s after the first subscription) and more than 4 entries ("No known schema for decoding attribute value", HAPErrorDomain 14). |
Scenario: a container whose only interfaces have no 6/8-byte MAC now serves `[]`; a Docker host with bridges and veth pairs serves 5-8 entries. If either removed observation was right, pairing with Apple Home regresses on those hosts. Not verifiable here; settles it: one Apple Home pairing against a node with 0 and one with 5-8 listed interfaces. The diff carries no test or note that the observations were re-examined. |
Fix: none before that check; record its outcome where the removed comments stood.

**R2-P0-rv-B08 | note | CONFIRMED | `secure/spake2/spake2.go:793-798` (`scalarTo32Bytes`), `:176` (`string(pwd)`)** |
`scalarTo32Bytes` copies every secret scalar through `n.Bytes()` into a second buffer that is never overwritten, and PBKDF hands the passcode bytes to pbkdf2 as a string copy; the doc of `zero` (`:667-668`) says every buffer the package holds a reference to is overwritten. |
Scenario: none functional; x, y, w0 and w1 each leave one extra heap copy per call, which the new zeroise tests cannot see (they hold the returned slice). |
Fix: `n.FillBytes(out)`; pass the passcode as `[]byte` where pbkdf2 allows, or say so in the doc.

Checked and found in order (no finding): the Verifier/Prover/Responder/Initiator state machines after the new terminal states (every caller allocates a fresh object per attempt — `bridge/handlers.go:343`, `client/handshake/pase.go:140`; no comparison against cleared material is reachable; `ProcessPake3`, `ProcessPake2` run at most once per object); aliasing of every overwritten buffer (`deriveKeys` copies KcA/KcB/Ke out of the wiped blocks, the salt helpers build their own buffers, `channel.New` and both session managers copy keys and challenge, `SharedSecret`/`ECDHSharedSecret` return copies, resumption records are not touched); the idempotent Sigma1 replay and the Sigma3 re-ack after the shared-secret drop; `ephPriv = nil` (regenerated on every Sigma1, `protocol.go:851-856`); the fail-safe watcher's stop/replace/fire interleavings; the nil fail-safe port on all four commands; the challenge check ahead of CSRRequest's pending state; GroupKeyMap's write fabric; BTP window 0 on both roles and both central call sites (`controller/ble/channel.go:226-231`, `:412-433`), window 1; `tcp.Conn.Wait` against a late `serve`; the passcode-free error texts.

## New citations checked

64 distinct cited locations in added lines of the reviewed paths were read at the pins (connectedhomeip: `CHIPCryptoPALOpenSSL.cpp`, `CHIPCryptoPAL.h`, `CASESession.cpp`, `SessionKeystore.h`, `RawKeySessionKeystore.cpp`, `BLEEndPoint.cpp`, `CHIPConfig.h`, `GroupPeerMessageCounter.cpp/.h`, `SessionManager.cpp`, `PeerMessageCounter.h`, `WriteHandler.cpp`, `OperationalCredentialsCluster.cpp`; matter.js: `PaseServer.ts`, `CaseServer.ts`, `BtpSessionHandler.ts`, `MessagingState.ts`, `FailsafeContext.ts`, `AccessControl.ts`, `OperationalCredentialsServer.ts`, `DeviceCertification.ts`, `IdentifyServer.ts`, `AccessControlServer.ts`, `access-control.element.ts`, `general-diagnostics.element.ts`, `GeneralDiagnosticsServer.ts`, `NodeJsNetwork.ts`, `SessionManager.ts`, `PairingCodeSchema.ts`, `OperationalBase.ts`).

Every one says what its comment claims. Two remarks, neither a miss:

- `secure/sigma/zero.go:13-15`: `SessionKeystore.h:164` is the destructor of `AutoReleaseSymmetricKey`; the `AutoReleaseSessionKey` the comment names is its alias, declared in `CASESession.h:467`.
- `cluster/core/general_diagnostics.go` states itself that `IsOperational` is the interface's up flag; matter.js derives it from the mDNS interface limit (`GeneralDiagnosticsServer.ts:497-502`, read at the pin), so "operational first" sorts by a different predicate. Disclosed in the comment, not in `by_design.md`.

What did not hold is not a citation but two statements beside citations: the security argument in `groups/decode.go:313-323` (B01) and the three references to `by_design` (B02).

Not read: the lineless reference `messages-server.cpp at 6170af84` in `cluster/core/messages_test.go`; the citations added under `bridge/` (`AdministratorCommissioningServer.ts:137-141` and others) apart from `FailsafeContext.ts:152-153`, `:187-192`, which hold.

## Not reviewed

- `bridge/` beyond `pase_close.go`, the `RevokeWindow` hunk and the PASE adapter: its tests, `AttachRootClusters` ordering, `Bridge.Stop` detaching the hooks. The claim in `contract/matter.go:784-790` (notifiers registered with none of the bridge's locks held) and the `EventEmitter` text were not checked against `bridge/` and `im/subscription`.
- `Node.Stop` and its context: the diff contains no change to it in these paths (`node/node.go` changes two comments), so there was nothing to review.
- "ACL writes without an accessing fabric": `cluster/core/access_control.go` changes comments only in this range; the code is elsewhere (`im/`).
- Read only in passing: `cluster/core/descriptor.go`, `messages.go`, the Identify and GroupKeyManagement parity tests, `node/served_revision_parity_matterjs_test.go`, `secure/mattercert/verify_validity_by_design_test.go`, `secure/channel/session_test.go`, `secure/operational/*_test.go`, `secure/setup/*_test.go`, the opcreds test-rig edits, the rewritten BTP second-session test (read, not traced against `BlenoBleServer`).
- Not run: the full `cluster/core` and `node` suites, anything under `-race`. Run and green: `secure/spake2`, `secure/sigma`, `transport/btp`, `transport/tcp`, `groups`, `commissioning`, and the two fail-safe hook tests 40 times.
- The hosts: whether any `IdentifyHandler` takes a lock held across endpoint removal (B04), and Apple Home behaviour (B07).
