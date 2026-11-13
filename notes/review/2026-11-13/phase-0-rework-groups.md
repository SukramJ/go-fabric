# Review II, phase 0 — rework: groups, key material, Identify.Close, two tests

Worktree `.claude/worktrees/rework-groups`, branch `review2/p0-rework-groups`, base `4efe1761`, HEAD `20948593`.
References read at the pins (scratchpad `pin/`); every cited file was compared with `git show <pin>:<file>` of the local
checkouts (matter.js `db021da0`: `MessagingState.ts`, `GroupSession.ts`; connectedhomeip `6170af84`: `CHIPConfig.h`,
`GroupPeerMessageCounter.h/.cpp`, `SessionManager.cpp`, `PeerMessageCounter.h`, `CHIPCryptoPALOpenSSL.cpp`) — identical.
`FabricGroups.ts`, `NodeSession.ts` and `ExchangeManager.ts` were read in the pin copy only (not diffed).

Format: `commit | files | red→green | CHANGELOG | register text`.

## R2-P0-rv-B01 — the group reception bound is gone (owner decision 22)

- **commit** `28fd6aad`
- **files** `groups/decode.go`, `groups/manager.go`, `groups/messaging_state_parity_matterjs_test.go`,
  `groups/sessionmanager_parity_matterjs_test.go` (all four back to `c87940e2^`, then the two comments rewritten),
  `groups/reception_bound_test.go` (deleted), `groups/reception_replay_test.go` (new), `docs/guides/limits.md` (row removed),
  `docs/security.md` (sentence).
  No commit touched `groups/` between `c87940e2` and `4efe1761` (`git log c87940e2..4efe1761 -- groups/` is empty), so the
  code is exactly the state before the bound; `git diff c87940e2^ HEAD -- groups/` shows the two comments and the new test only.
- **red** (bounded code, `TestRecordedGroupMessageStaysADuplicate`): `16 senders: 48 of 48 byte-identical replays were
  accepted as new messages`, `100 senders: 300 of 300`, `1000 senders: 3000 of 3000`, each with `15 reception windows after N
  senders, want one each`. **green** after the revert; `-race` green.
- **what the tests pin** — `TestRecordedGroupMessageStaysADuplicate` (16 / 100 / 1000 senders, three round-robin replay
  rounds oldest first, then a real next counter is new and the recorded one stays refused);
  `TestOnlyAnAuthenticatedGroupMessageCreatesReceptionState` (1000 forged source node ids on a recorded datagram, a changed
  counter / payload / tag, a foreign key under a held session id with and without privacy, a key set no group is mapped to:
  zero entries each; the genuine datagram then creates exactly one); `TestReceptionStateOfOneKeyIsNotAnothers`.
- **matter.js, unbounded** — `packages/protocol/src/groups/MessagingState.ts:19` (`Map<string, Map<NodeId, …>>`), `:80-90`
  (`receptionStateFor` creates an entry for every source node id asked for), `:97-99` (`forgetReceptionState`, the only
  removal, drops a whole key); reached through `packages/protocol/src/session/GroupSession.ts:289-295` from
  `packages/protocol/src/protocol/ExchangeManager.ts:369-378`, after `groupSessionFromPacket` returned a decoded message.
- **memory per entry (measured)** — 48 to 62 bytes of retained heap per (key, source) entry: `runtime.MemStats.HeapAlloc`
  delta after two `runtime.GC()`, divided by n, for n = 1e3 / 1e4 / 1e5 / 1e6 entries under one key through
  `Manager.receptionWindow(key, source).Accept(7)` → 61.2 / 53.6 / 48.0 / 61.8 B (go1.27.4, darwin/arm64; a throwaway test,
  not committed). `unsafe.Sizeof(mrp.Window{})` = 24; the rest is the map slot. 1e6 claimed source node ids = 61.8 MB measured.
  Each entry costs its maker one datagram that authenticates.
- **who can grow the table** — only a holder of the group key (test above: nothing that fails to authenticate under a
  mapped key reaches `completeMessage`). The source node id is the sender's claim, so one key holder can add as many entries
  as it sends ids, until the key set is removed or rewritten (`forgetUnreferenced`).
- **kept from `c87940e2`** — no code. From its test file two properties, restated without a bound: one key's senders do not
  touch another key's replay protection; the first message of each of 1000 senders is not taken for a replay. From its docs
  edit: `docs/security.md` cites `groups/decode.go` without the line number it had before (`:281`), which would drift.
  `acceptCounter` was not kept: `mrp.Window` locks itself (`transport/mrp/window.go:46-48`), and
  `script/testparity/register.json:7929` / `notes/parity/matterjs_tests.md:1236` name `Manager.receptionWindow`, true again.
- **CHANGELOG** — remove the `[Unreleased]` bullet that starts "- `groups`: one operational group key keeps reception windows
  for at most 15 senders" (line 24). No replacement is needed: the behaviour is that of 0.22.2. If a line is wanted:
  "`groups`: the reception state of group messages stays one replay window per sender and key without a bound, as matter.js;
  a bound of 15 senders with eviction, never released, let a party without the key replay recorded messages (review II,
  R2-P0-rv-B01, owner decision 22)."
- **register text** (replaces the Ahead row `BD-chip-GroupReceptionPeersBounded` and its section at `by_design.md:6910`;
  it is no longer "ahead" — the behaviour is matter.js's, the departure is from connectedhomeip):

  > **BD-chip-GroupReceptionUnbounded — one replay window per sender and operational key, without a bound.**
  > `groups.Manager.receptionWindow` (`groups/decode.go`) keeps a window for every source node id seen under a key until the
  > last key set deriving the key goes, as matter.js: `MessagingState.ts:19`, `:80-90` at `db021da0` create one state per
  > (operational key, source node id) and only `forgetReceptionState` (`:97-99`) removes any. connectedhomeip bounds the table
  > at 15 senders per fabric (`src/lib/core/CHIPConfig.h:833`, `src/transport/GroupPeerMessageCounter.h:53-57` at `6170af84`),
  > evicts the least recently used sender when a sender it does not hold appears (`GroupPeerMessageCounter.cpp:79-95`) and
  > trusts that sender's first counter (`SessionManager.cpp:1287-1294`, `PeerMessageCounter.h:102-108`): a recorded datagram
  > of an evicted sender authenticates (`SessionManager.cpp:1213-1260`) and is accepted again, so with 16 or more senders
  > under a fabric a party without the key replays what it recorded. This module had that bound for one commit (`c87940e2`)
  > and reproduced the replay (16 senders: 48 of 48; R2-P0-rv-B01); owner decision 22 took it out. What it costs: memory under
  > a holder of the group key — 48 to 62 bytes of heap per claimed source node id (measured, `HeapAlloc` delta over 1e3 to
  > 1e6 entries, go1.27.4 darwin/arm64), one authenticated datagram each; a party without the key adds none
  > (`TestOnlyAnAuthenticatedGroupMessageCreatesReceptionState`). Pinned by `TestRecordedGroupMessageStaysADuplicate`.
  > A bound that does not reopen the replay (R2-P0-bound-A07, R2-P0-bound-E02 are open again) is a phase 3 design question.

  `phase-0-register.md:122` (A07 = E02 "fixed `c87940e2`") is no longer true: reverted by `28fd6aad`, open for phase 3.

## R2-P0-rv-B03 — what is overwritten after a PASE handshake, and what the docs claim

- **commit** `460bf429`
- **files** `secure/spake2/spake2.go`, `secure/spake2/zeroise_test.go`, `secure/sigma/zero.go` (doc),
  `secure/operational/manager.go`, `secure/operational/pase_zeroise_test.go` (new), `commissioning/pase.go`,
  `commissioning/pase_zeroise_internal_test.go` (new), `client/handshake/pase.go`, `client/handshake/export_test.go` (new),
  `client/handshake/pase_zeroise_test.go` (new).
- **per site**
  1. `operational.Manager.OpenFromPase` / `OpenFromPaseWithID` (`manager.go:826`, `:883` at the base): the 48 derived bytes
     have no later use — `channel.New` copies both keys (`secure/channel/session.go:142-143`), the entry copied the
     challenge. Both now go through `paseSession`, which overwrites the block on every way out (seam `secretHook`).
     red: `the overwrite saw 0 buffers` (old code with the seam stubbed); green: the block reads zero, the session still
     seals and opens against a peer built from the block as it was, the caller's Ke is untouched.
  2. `spake2.Verifier.ke` / `Prover.ke` after a success: cannot be overwritten by the package on its own — `SharedSecret()`
     may be asked again (`commissioning.PASEResponder` did, on every `Session()` / `AttestationChallenge()`). New
     `Verifier.Close()` / `Prover.Close()` overwrite Ke and whatever an abandoned handshake still holds (KcA and Ke before
     Pake3; w0, w1, x before Pake2) and end the object. red (no-op stubs): `Ke still readable after Close`, `w0 or w1 of a
     prover closed before Pake1 is still readable`, `ProcessPake2 after Close: <nil>`; green.
  3. `commissioning.PASEResponder`: derives the key block once, in `HandlePake3`, overwrites its copy of Ke and closes the
     verifier on every way out; `Session` / `AttestationChallenge` serve from the block (the challenge as a copy);
     new `Close()` overwrites the block. red (both overwrites mutated away): `the verifier still hands out Ke after
     HandlePake3`, `the key block is still readable after Close`; green.
  4. `client/handshake` `pairPASE`: `defer prover.Close()` (seam `newProver`). red (Close removed): `the prover still hands
     out Ke`, and for a handshake cancelled before Pake1 `the prover was left open`; green. The wrong-passcode case does not
     discriminate (a failed prover answers `ErrSessionState` already).
- **what still lives on, and why** (now stated in the `zero` docs of spake2 and sigma, on `Verifier`, `Prover`,
  `PASEResponder`, `paseSession`):
  - the session's two keys in `channel.Session` and the attestation challenge in the entry — the session;
  - the stored verifier (`VerifierContext`: w0, L) — serves every later handshake;
  - `PASEResponder`'s key block until its `Close` — `Session` and `AttestationChallenge` hand it out on every call;
  - Ke in a `Verifier` / `Prover` until its `Close`. **`bridge/handlers.go:490-492` does not call it** (nor `:357`, `:368`,
    `:478`, `:487`, where a verifier is dropped): on the device side the verifier's own Ke — and KcA/Ke of an abandoned
    handshake — still goes to the collector. One line each; `bridge/` was not mine;
  - sigma: the session keys and a finished handshake's shared secret (resumption) for as long as the Responder / Initiator
    is held, and the `Identity` (IPK, signing key). The sentence "every buffer this package holds a reference to" is gone
    from both docs;
  - the standard library's own state (math/big, hmac, hkdf, pbkdf2, nistec, aes) — unreachable.
- **CHANGELOG** — reword the bullet that starts "- `secure/spake2`, `secure/sigma`, `client/handshake`, `node`: handshake key
  material" (line 23): add `secure/operational` and `commissioning` to the packages; after "…the bytes the session keys are
  cut from" add "on both sides (`operational.Manager.OpenFromPase`, `OpenFromPaseWithID` too)"; add "Ke stays in a
  `spake2.Verifier` or `Prover` until its `Close`". New lines:
  `### Added` — "`secure/spake2`: `Verifier.Close` and `Prover.Close` end a handshake object and overwrite the key material it
  still holds; `commissioning.PASEResponder.Close` does the same for the responder (review II, R2-P0-rv-B03)."
  `### Changed` — "`commissioning.PASEResponder` derives its key block once, in `HandlePake3`, and overwrites the shared
  secret there; after a failed `HandlePake3` or `Close` every method returns `ErrPASEStateMismatch`, and
  `AttestationChallenge` returns a copy."
- **register text** — `BD-chip-HandshakeSecretsOverwritten` (`by_design.md:144`) is unchanged in its marker and source; its
  section should say what lives on, in the words of the list above, and that the bridge's PASE adapter does not close its
  verifier yet. The new comment in `paseSession` cites `CHIPCryptoPALOpenSSL.cpp:1150-1151` (`ClearSecretData(Kcab)`,
  `ClearSecretData(Kae)`), read at the pin; `commissioning/pase.go` now cites `NodeSession.ts:80-90` where it said `:78-88`
  (the `createHkdfKey` call and the three slices are at 80-90 in the pin copy).

## R2-P0-rv-B08 — the extra copies

- **commit** `44b231f6` | **files** `secure/spake2/spake2.go`, `secure/spake2/zeroise_test.go`
- `scalarTo32Bytes` writes with `big.Int.FillBytes` into the buffer its callers overwrite; no `n.Bytes()` copy.
- `PBKDF`: `crypto/pbkdf2.Key` takes the password as a string only (go1.27.4 `crypto/pbkdf2/pbkdf2.go:40`) and converts it
  again for its HMAC key (`crypto/internal/fips140/pbkdf2/pbkdf2.go:30`), so the copy stays; the comment says so, and the
  `zero` doc names it.
- **red→green: none possible.** `testing.AllocsPerRun(scalarTo32Bytes)` was 1 before and after — the compiler kept the
  `Bytes()` temporary on the stack — so no test can see the copy go. `TestScalarTo32Bytes` pins the value and the padding.
- **CHANGELOG** none. **register** none.

## R2-P0-rv-B04 — Identify.Close and the host's handler

- **commit** `c1ce783a` | **files** `cluster/core/identify.go`, `cluster/core/identify_close_test.go` (new),
  `contract/identify.go` (doc), `docs/guides/ports.md` (IdentifyHandler row)
- **verdict: confirmed, two ways.** `Close` waited for the countdown goroutine (`identify.go:194` at the base), and that
  goroutine (a) blocked in `op.Lock()` (`:403`) while a request held `op` across the host's `MatterIdentify` (`:286-293`),
  and (b) called the host's `MatterIdentify(…, 0)` itself at zero (`:419-421`). Either way `Close` took as long as the
  handler, for ever when the handler waits for a lock the closing goroutine holds. (b) predates `f780f271`.
- **fix** — `op` is a one-slot queue (a channel, created on first use, so the zero value still works) instead of a mutex:
  the tick takes it with a `select` on `done` and leaves uncounted when the server closes; the stop at zero is called from
  a goroutine of its own that keeps the slot until the handler returns, so a request still waits behind the stop. The
  countdown goroutine, the one `Close` waits for, is never in host code. The zero is stored and reported without the host.
- **red** (base code): `Close while a request's handler is in flight: not within the test's bound` (30 s), and the same for
  the stop case. **green**; `-race -count=50` of every `Identify` test green (34 s).
  Ordering: `TestIdentify_ARequestWaitsForTheCountdownsStop`, red when the stop gives the slot up before its call
  (`the host handled [1 7] while the stop was in flight, want [1]`), green; `TestIdentifyCountdownStopDoesNotOvertakeAWrite`
  (f780f271's, untouched) green.
- **what remains** — the stop's goroutine is the one goroutine `Close` does not wait for (ADR 0026 P5); it ends when the
  host's handler returns. `op` is still held across host code (P6), which is `f780f271`'s decision.
- **CHANGELOG** — reword the bullet that starts "- `cluster/core`, `contract`: Identify's countdown tick and a request that
  sets IdentifyTime run one after the other" (line 81): append "`Identify.Close` does not wait for a handler in flight — the
  countdown gives up its place when the server closes, and the stop it reports is called from a goroutine of its own
  (review II, R2-P0-rv-B04)."
- **register** none.

## R2-P0-rv-B05 — the rolled-back hook test

- **commit** `d67b4c95` | **file** `cluster/core/general_commissioning_rolled_back_test.go`
- Every case runs in a `synctest` bubble (the package's neighbours do: `general_commissioning_test.go`,
  `icd_management_test.go`, with `clocktest.Fake` inside); `synctest.Wait()` after the expiry returns when the watcher's
  goroutine has finished. The `time.Sleep(time.Millisecond)` is gone (**sleep ratchet: −1 `time.Sleep`;
  `clock/sleep_ratchet_test.go` not touched** — at this HEAD the count is 71 / 134 against 66 / 126). Both `thelper`
  findings (`:65`, `:75`) are gone (`golangci-lint run ./cluster/core/`).
- **the old test was wrong** — product correct, 2 000 000 `runtime.Gosched()` between the two hooks: old test
  `hooks ran [host], want [host owner]`; new test green.
- **red when the behaviour is broken** (owner hook skipped after the host's): `hooks ran [host], want [host owner]` in all
  three "after the host's hook" cases. **stable**: `-race -count=50` green.
- **CHANGELOG** none. **register** none.

## R2-P0-rv-B06 — the expiry watcher test

- **commit** `20948593` | **file** `cluster/core/general_commissioning_watcher_test.go`
- One `synctest` bubble; `synctest.Wait()` replaces the 50 yields at each of the three points, and "fired once" is now also
  asserted right at the deadline.
- **the old test was wrong** — product correct, the watcher delayed by 2 000 000 `runtime.Gosched()` after its timer: old
  test `the expiry fired 0 time(s), want once`; new test green.
- **red when the behaviour is broken**: a replaced watcher not stopped → `59 expiry timers armed after 100 re-arms, want 1`
  (and the bubble reports the leaked goroutines); an expiry that does not fire → `at the last deadline: fired 0`.
  **stable**: `-race -count=50` green (3.5 s for both tests).
- **CHANGELOG** none. **register** none.

## Guards (HEAD `20948593`)

`gofumpt -l` (groups secure commissioning cluster/core client/handshake contract): nothing · `go vet`: 0 ·
`go test -count=1 -p 4 ./groups/... ./secure/... ./commissioning/... ./cluster/core/ ./bridge/ ./client/handshake/ ./node/
./script/errprefix/ ./script/docscheck/ ./contract/... ./script/citecheck/ ./script/aheadcheck/`: 0 ·
`go test -run '^Fuzz' ./groups/... ./secure/...`: 0 · `CGO_ENABLED=1 go test -race` on the touched packages: 0 ·
`golangci-lint run` on the touched packages: 2 findings, both `prealloc` in
`cluster/core/general_diagnostics_network_interfaces_parity_matterjs_test.go:189`, `:200` — a file this work did not touch.
Summary log: scratchpad `rework-groups-final.log`.

Not run: `make` targets (reachability, apidiff, cover-check, surface), `./...`. `go test ./clock/` is red at the base and
here for the sleep ratchet alone (the other agent's work).

## Not changed, with reasons

- `bridge/handlers.go`: the PASE adapter does not call `Verifier.Close` — not my area; see B03.
- `node/security.go:645`: already overwrites the copy it is handed; nothing to add.
- `clock/sleep_ratchet_test.go`: left to the agent that removes the other sleeps.
- `notes/parity/*`, `CHANGELOG.md`, `script/reachability/*`: the coordinator's. Three exported methods are new
  (`spake2.Verifier.Close`, `spake2.Prover.Close`, `commissioning.PASEResponder.Close`), all reached by tests; the
  reachability inventory may move when regenerated.
- The stop call of Identify's countdown still gets `context.Background()`, as before.
- `cluster/core/identify_handler_parity_matterjs_test.go` (a `time.After` and a `time.Sleep` of the campaign): the other
  agent's.

## Outside the listed area, touched because the finding names it

- `client/handshake/pase.go` (B03: "`pairPASE` just drops the Prover"; the campaign's `fd3d535b` edited the same lines),
  with `export_test.go` and a test.
- `contract/identify.go`, `docs/guides/ports.md`: the handler's contract text (B04 cites `contract/identify.go:22-35`).

## Open questions

1. Should the bridge's PASE adapter close its verifier (`bridge/handlers.go:357`, `:368`, `:478`, `:487`, `:490-492`)?
   Without it the B03 claim holds on the device side for the derived bytes and the handed-out copy, not for the verifier's Ke.
2. Should `Identify.Close` cancel the context of a stop call in flight? Today the stop is delivered with
   `context.Background()`; a cancelled context could make a host skip the stop.
3. Noticed, not examined: `Identify.Close` during a running identify ends the countdown without telling the host to stop.
   What matter.js's `[Symbol.asyncDispose]` does there was not read.
