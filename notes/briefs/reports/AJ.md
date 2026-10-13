# Report AJ — `bridge/bridgetest.Commissioner` onto `controller/` (ADR 0020, stage 2 close-out)

Branch `wave16/bridgetest` from `planning/briefs-wave-16` (ac1497c), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`).
The `// Mirrors`/citation comments added cite `ClientSubscriptionHandler.ts:77,
:89, :108-112`, `InputChunk.ts:166-169` and `ClientInteraction.ts:991-994`,
all read at the pin. Each commit carries its regenerated reachability
inventory (unreached stays **5**). `secure/`, `transport/`, `mdns/`,
`bridge/` outside `bridgetest`, `node/` (non-test), `cluster/`, `im/`, the
rest of `controller/`, `bridgetest.go`, `notes/parity/by_design.md` and the
findings register are untouched.

| Commit | Item |
| --- | --- |
| 99856ea `feat(controller/interaction): Report.SubscriptionID and Report.Session` | §2 |
| b782f9a `test(controller/peer, node): the tests bridgetest's move onto controller/ needs` | §5 |
| 3bf15aa `feat(bridge/bridgetest): the Commissioner runs on controller/` | §1, §3, §4, and §5's coverage floor |
| (this commit) `notes: report AJ` | — |

§5 comes before §1/§3/§4. The import cycle (`controller/peer`'s in-package
tests import `bridgetest`) and the subscriptions over PASE had to be fixed
before `bridgetest` could import `controller/`. Both of those test changes
also pass against the old `bridgetest`. §1, §3 and §4 are one commit because
you can't build them apart: deleting `transport.go` removes the socket that
the CA, the handshakes and `Session` all ran on.

## Guards

- `make ci`: exit 0. Build, vet, `go test -count=1 ./...` (179 `ok`),
  gofumpt + golangci-lint (0 issues), `go mod tidy -diff`, `go mod verify`,
  and cover-check (181 packages at or above their floor). `bridge/bridgetest`
  measures 92.3 %, so its floor goes from 90 to 92 (the "whole percent just
  below" rule).
- `make race`: exit 0 on the first full run (179 `ok`, no race reported).
  Before that, a targeted `-race -count=3` run over
  `node`, `bridgetest` and four `controller/` packages failed once in `node`,
  and I lost the output. Seven later runs of the same set, 3–4 counts each,
  passed, so I can't name the test. Treat it as a possible flake to watch.
- `make reachability-check`: exit 0 (inventory committed first; unreached
  5).
- `make chiptool-test` (once): exit 0, `ok internal/chiptool 50.0s`, 313
  PASS, 0 FAIL, 1 SKIP (`TestMatterJSController`, its environment reason).
  The suite does not use `bridgetest`, so this is the regression check; it
  matches AI's 313/0/1.
- What the in-module tests prove:
  - **`bridgetest`'s own tests**: wrong passcode → `ErrHandshake`, CASE
    before AddNOC → `ErrHandshake`, advertiser lookups, the full session API
    over CASE (chunked wildcard read, statuses, invoke status, subscribe
    InvalidAction → `*StatusError`, a second controller denied by the ACL,
    closed sessions → `ErrSessionClosed`, RemoveFabric), and
    re-establishment through `Accept`/`NextReport`.
  - **New `bridgetest` tests**: `TestPASESession` (reads over PASE, Subscribe
    refused with `interaction.ErrUncommissioned`, Close idempotent) and
    `TestControllerIdentities` (the fabric's own identity, minting once,
    `ResolveSigma1Destination`).
  - **`node`**: `TestSubscriptionSurvivesRestart`, the brief's proof of
    §4b, passes: the restarted node's report under the old id reaches
    `Updated` and is routed to the accepted session.
  - **`controller/handshake`, `controller/ca`, `controller/interaction`** pass
    unchanged.

## Lines where the implementation differs from the brief, and why

1. **§1 / §4, the CASE server's fabrics.** The server resolves over every
   identity minted so far (`identityFabrics`: the same list
   `ResolveSigma1Destination` uses, `FabricByIndex` the fabric's own), not
   `FabricList{fabric.Fabric}`. `ResolveOperational` answers for every
   minted controller, and today's responder resolved over all of them. With
   only the fabric's own identity, a device re-establishing toward a
   `CASEAs` controller would get NoSharedTrustRoots.
2. **§1 / §3, feeding the names cache.** The fake `mdns.QueryConn`'s `Send`
   does not just ignore the query: it first runs `feedOperational`. I still
   also feed the cache before `Commission`, `CASE` and `CASEAs`, as the brief
   says. The reason: with an `Advertiser`, the operational record exists only
   after AddNOC, which happens inside `com.Commission`, so there is no point
   between AddNOC and the flow's CASE step where `bridgetest` gets control.
   The flow's DNS-SD query is that moment. Without an advertiser this makes
   no difference.
3. **§3 `Commission`, error class.** A failure that is a handshake failure
   (`handshake.ErrPasscodeMismatch`, `handshake.ErrUnexpectedData`,
   `*exchange.StatusReportError`) wraps `ErrHandshake`.
   `TestCommissionerRefusals` expects `ErrHandshake` for a wrong passcode,
   and that test is the specification. Other flow errors are returned as
   they are.
4. **§4b `route`, the unknown session.** The CASE server calls `OnSession`
   *after* its Success StatusReport goes out
   (`controller/handshake/server.go:389-418`). The node can send its first
   report before the session is registered. So `route` registers an unknown
   session (`wrap`), and `onSession` returns the same `Session` for
   `Accept`. Only a report with no session at all is dropped with
   `tb.Logf`.
5. **§4 `Invoke`.** A CommandStatusIB other than Success stays
   `InvokeResult.Status` (with `ClusterStatus`), not a `*StatusError`. That
   is what the type documents and what `TestCommissionerSessions` checks
   (an unknown command gives `err == nil` and a non-Success status). Only a
   StatusResponse in place of the InvokeResponse becomes `*StatusError`.
   Telling the two apart: `interaction.Invoke` returns the filled `Result`
   with its `*StatusError` for a command status, and a zero `Result` for a
   StatusResponse.
6. **§4, error mapping beyond Write/Invoke.** `Read` and `Subscribe` also map
   an interaction-level `*interaction.StatusError` (one with no `Path`) to
   `*StatusError`. The subscribe-to-nothing InvalidAction check needs this.
   An error on a session that closed during the call wraps
   `ErrSessionClosed`, except a context error.
7. **§4 keepalives.** An empty report goes to `KeepaliveReceived`, which the
   package does not set, so `NextReport` no longer returns keepalives as
   empty reports. The old read loop queued them. `KeepaliveReceived` carries
   no session, so I can't route it. This is announced under Changed.
8. **§0 / §1, determinism.** The brief says two `Dial`s with one seed mint the
   same bytes. That does not hold on this toolchain (go1.27.2):
   `crypto/ecdsa.GenerateKey` and `ecdsa.SignASN1` ignore the caller's
   reader since Go 1.26. I checked this here: a seeded `math/rand` reader
   gives a different key and signature on every call, and with
   `GODEBUG=cryptocustomrand=1` `MaybeReadByte` still makes the key
   nondeterministic. So `ca.New`'s root key, `commissioner.NewFabric`'s own
   key and every certificate signature are random whatever `Config.Rand` is.
   What the seed still determines: the IPK, the flow's nonces, the keys
   `CASEAs`/`ControllerIdentity` mint (drawn as raw scalars from the seeded
   stream), and the MRP jitter. `Options.Seed`'s doc and the CHANGELOG say
   exactly this. No test depends on reproducible bytes: I checked every
   `Seed` use in the `*_test.go` files (all are "a seed", none compares
   bytes).
9. **§5, `controller/peer`'s fabric helper** uses `ca.New(ca.Config{})` and a
   `crypto/rand` key, not a seeded one. Seeding cannot fix the key (point 8),
   and nothing compares it.
10. **§5, the peer node proof is not exported-API only.**
    `TestConnectToANodeThroughTheProcess` calls the unexported `probe`. I
    moved it to `package peer_test` anyway (`node_test.go` changes package)
    and reach `probe` through a new `controller/peer/export_test.go`
    (`Probe`, compiled only into tests). This is the standard idiom, and the
    test's intent is unchanged. The brief said "else it is a question"; it
    is open question 1. `node_live_test.go` (`!race`) uses `commissioned()`
    too, so it moved with it.
11. **§5 node tests.** `TestNodeServesPASEReadAndSubscription` keeps PASE and
    its reads, then `Close`s the PASE session, commissions with the same
    commissioner and subscribes over the returned CASE session.
    `TestSessionAndSubscriptionDiagnostics` does the PASE check first (the
    window has to be open), then commissions. It asserts on the one non-PASE
    session on `c.FabricIndex()` and counts the subscription by that
    session's id. "Commission first" in the brief can't apply to the PASE
    parts, because the window closes at CommissioningComplete.
12. **§3 `CASEAs`** passes `CASEOptions{}` as briefed, so the session's own
    CATs are nil (no resumption store). The NOC carries the CATs, which is
    what the device checks.
13. **Commits.** §1/§3/§4 are one commit (see above).

## `Options` fields left without effect

- `FailSafeSeconds`: no test in the module sets it (`grep -rn FailSafeSeconds
  --include=*_test.go`: none). It is documented as having no effect: the flow
  arms with `commissioner.DefaultFailSafeTime`. Its default (60) is still
  filled in.
- `AdminSubject`: no test sets it. It is documented as having no effect:
  CaseAdminSubject is `FabricConfig.RootNodeID` = `ControllerNodeID`. Its
  default is still filled in.
- `Seed`: it still has an effect, but less than before (point 8).

## What `BD-Matter-Commissioner-TestCA` should now say

The entry's three claims are all gone: the fixed NotBefore/NotAfter 0
validity, sending without retransmission, and a CA that follows the seed.
Suggested text:

> `bridge/bridgetest.Commissioner` is the module's own controller wired for
> a test: `controller/ca` with a fixed issuing instant (2026-01-01, so the
> validity is matter.js's −1 y / +10 y around it rather than around now),
> MRP retransmission through `controller/exchange`, the commissioning flow
> of `controller/commissioner`, and DNS-SD answers injected from the
> device's advertiser or the dialled address instead of multicast. `Seed`
> seeds the IPK, the flow's nonces, the keys of further controller
> identities and the MRP jitter; the root and fabric keys and the signatures
> are random because `crypto/ecdsa` ignores a caller's reader since Go 1.26.
> The unsecured ephemeral node id and message counter are the exchange
> manager's own (random). Test support only; nothing of it is on the device
> path.

If the only remaining divergence is the fixed instant, the owner may prefer
to retire the entry, since the rest now *is* the ported controller.

## Open questions

1. Is `controller/peer/export_test.go` (test-only `Probe`) acceptable for
   moving `TestConnectToANodeThroughTheProcess` into `peer_test`, or should
   that proof drop its two probe checks or stay in-package another way?
2. **Determinism.** Should the seed again fix every byte? That would need a
   change outside this brief. Either `controller/ca` and
   `controller/commissioner.NewFabric` accept a caller-supplied key (or
   `bridgetest` preloads a `ca.Store` with a seed-derived root), plus RFC 6979
   signing with a nil reader. Or accept the narrower promise as documented.
3. The CASE responder over every minted identity (point 1): keep, or limit to
   the fabric's own identity as the brief said?
4. Should `NextReport` report keepalives again? That would need a session on
   the keepalive callback in `controller/interaction`.
5. During one targeted `-race` run, before the green `make race`, `node`
   failed once and I could not reproduce it (see Guards).

## Dangling references (outside this brief's files; owner)

The deleted files are still cited in:
- `controller/exchange/manager.go:427` and `errors.go:141` (`bridgetest/transport.go:211/:217`)
- `controller/ca/encode.go:10` (`bridgetest/ca.go`)
- `controller/peer/probe.go:23` (`bridgetest/report.go`)
- the findings register (`matter_behaviour_findings.md:289`, the resolver at `bridgetest/handshake.go:212`)
- `notes/handover.md`

## `by_design` candidates (not written)

- The test commissioner's fixed certificate instant (the remaining part of
  `BD-Matter-Commissioner-TestCA`, see above).
- None for `controller/interaction`. The two `Report` fields are a
  parity-neutral addition: matter.js carries both on the chunk's exchange.

## Owner's decisions

Reviewed 2026-10-13: `bridge/bridgetest/commissioner.go`, `session.go`,
`seed.go`, `names.go`, `report.go` and the `controller/interaction` and
`controller/peer` diffs read in full; the Go 1.26 `crypto/ecdsa` behaviour
confirmed on this toolchain (`go doc crypto/ecdsa.GenerateKey`,
`SignASN1`: the reader is ignored unless `GODEBUG=cryptocustomrand=1`).

1. **`controller/peer/export_test.go`** is accepted: the test-only export
   is the standard idiom, and the node proof keeps its probe checks.
2. **The narrower seed promise stands.** `Options.Seed`'s doc, the
   CHANGELOG and `BD-Matter-Commissioner-TestCA` (rewritten) say what the
   seed still fixes. A fully reproducible certificate would need a
   caller-supplied key in `controller/ca` and nil-reader signing, and
   nothing in the module needs it. `ca.Config.Rand` and
   `commissioner.FabricConfig.Rand` now say that the key generation
   ignores them since Go 1.26 (a test that needs reproducible keys uses
   `testing/cryptotest.SetGlobalRandom`).
3. **The CASE responder answers for every minted identity** — kept; it is
   what `ResolveOperational` promises and what the old responder did.
4. **`NextReport` delivers no keepalives** — kept; a test that needs them
   uses `controller/interaction` directly (`KeepaliveReceived`).
5. **The unreproduced `-race` failure in `node`** is watched in CI; the
   tranche's own `make race` and the agent's full run passed.
6. **Dangling references** to the deleted files
   (`controller/exchange/manager.go`, `errors.go`, `controller/ca/encode.go`,
   `controller/peer/probe.go`, the findings register) are fixed on the
   tranche; `notes/handover.md` in the docs PR.
