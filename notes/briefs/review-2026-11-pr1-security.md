# Brief: review 2026-11 — PR 1, the security bundle

Branch `review/security` from `main` (769ea3c2). No release with this PR (owner 2026-11-01: one tag after all fix PRs). Owner decisions in
`notes/review/2026-11/SUMMARY.md`; findings and verdicts in that directory
(every item below is verified there with `file:line` on both sides — read
the verdict row and the theme-file section before touching the code).
Pins: matter.js `../matter.js` at 862ad75f, chip via
`git -C ../connectedhomeip show 6170af84:<path>`.

## Items (each a commit; the finding ID in the commit title)

1. **W2-wire-01** — IM on the unsecured session 0: reject in
   `bridge/im_gate.go classifyIMOpcode` before decode, mirroring matter.js
   (`ExchangeManager.ts:391-412` drops; `InteractionServer.ts:161`
   `requiresSecureSession`); make the ACL terms in `im/read.go:490`,
   `im/write.go:254`, `im/invoke.go:690-697` fail closed (no fabric and not
   PASE ⇒ deny). Flip `bridge/receive_test.go:311` TestDispatch_IMReadRoutes
   and the sibling session-0 routing tests; add a parity test.
2. **W3-clusec-01** — `cluster/core/access_control.go:676-681` and
   `:924-929`: refuse `request && fabric == 0` with UnsupportedAccess (as
   `binding.go:343-345`), drop both fallbacks, flip
   `access_control_test.go:979-1010`.
3. **W2-sec-02 / W6-reg-01** — call `operational.Manager.ClosePASESessions`
   from the node's CommissioningComplete hook (`node/root.go:233-242`, after
   the 100 ms grace the old register entry describes — Apple sends the
   command over the fabric-adopted PASE session) and from the fail-safe
   expiry hook (`:215-220`); correct
   `notes/parity/matter_behaviour_findings.md:306`.
4. **W3-clusec-02** — UpdateNOC (`cluster/core/operational_credentials.go:1967-2000`):
   session fabric must equal the fail-safe's fabric (ConstraintError,
   `OperationalCredentialsServer.ts:355-360`); MissingCsr after
   AddTrustedRootCertificate (`:337-345`).
5. **Fabric-aware fail-safe port** (W3-clusec-03, W3-club1-02, W3-clua2-01):
   `node/failsafe.go:54` passes `gc.FailSafeArmed` (any fabric); change the
   port to take the accessing fabric (chip `FailSafeContext.h:76-79`
   `IsFailSafeArmed(fabricIndex)`), feed OpCreds CSRRequest/AddNOC/
   AddTrustedRootCertificate (`operational_credentials.go:1463, :1606, :2278`),
   Thread BR (`cluster/threadbr/management.go:291-296, :349`), Joint Fabric
   (`cluster/jointfabric/administrator.go:469, :505-513`). Status
   FailsafeRequired. Add the Ahead-of-matter.js row (marker:
   `OperationalCredentialsServer.ts:602-604` checks armed only) in
   `notes/parity/by_design.md`.
6. **W6-reg-02** — call `store.RemoveResumptionsByFabric` from
   `node.fabricUpdated` (`node/node.go:589-601`; matter.js
   `FailsafeContext.ts:167-170`); correct register H1 (`:243`).
7. **W4-ctlint-01 (+02)** — `controller/dcl/crl.go:87` depth cap (32) in
   `derDecodeRec`; `controller/dcl/revocation.go:352` `io.LimitReader`
   (DataFileSize from the ledger, else 1 MiB); OTA download
   (`controller/softwareupdate/images.go:454-461`, `ota.go:273`)
   LimitReader to OTAFileSize+1. Add a fuzz/regression test with a deeply
   nested DER.
8. **W5-ex-01** — one opener in `node` (`node/store_path.go:30`) building the
   URI with `url.URL{Scheme:"file", Path: path, RawQuery: pragmas}`; use it
   from `controller/open.go:36`, `fabrictest/device.go:159`,
   `examples/quickstart-device/main.go:72`. Test with a path containing `#`
   and `?`.
9. **W5-ex-05** — create the store file (and WAL/SHM) with 0600; document in
   `docs/guides/operations.md:37`.
10. **W1-sec-01/02/03** — Sigma3/Sigma2 fabric/node binding fails closed
    when the responder has an identity and the verifier offers no extractor
    (`secure/sigma/protocol.go:966-985`, `initiator.go:309-331`; test rigs
    get an adapter, no API break); Sigma1 tags 1–3 mandatory
    (`secure/sigma/sigma.go:304-384`, InvalidParam on absence,
    `CaseMessages.ts:30-32`); AKID must equal root or ICAC SKID
    (`secure/mattercert/verify.go:296-318`, `Noc.ts:163-184`).
11. **W6-test-02** — the race in `client/discovery/session.go:222`
    (context.AfterFunc hook logging after Run returned; released at `:300`):
    release the hook before Run returns.
12. **W6-doc-01** — `cluster/onoff/server.go:378-379` and
    `cluster/closure/closuredimension_server.go:556-557, 615-619`: pass a
    `contract.StatusError` through (as v0.15.6 did for the other servers).
13. **Register + notes**: commit `notes/review/2026-11/` and this brief;
    `CHANGELOG.md` `[Unreleased]` gets a **Security**
    section naming items 1–10 in one line each (no exploit detail beyond
    the finding sentence).

## Rules

- Read CLAUDE.md "matter.js is the gold standard" and "Token use". Cite the
  mirrored matter.js/chip line in a `// Mirrors …` comment on every changed
  behaviour. Every item adds or flips a parity test.
- No new dependency. `make fmt` before each commit. No attribution lines.
- Guards in the background with a summary log under
  `~/.cache/go-fabric-scratch/` (`flock ~/.cache/go-fabric-scratch/guards.lock`):
  `make ci` (build vet test lint cover-check), `make race`, then
  `make chiptool-test` once at the end (items 1, 2, 3, 5 change what
  chip-tool sees). Read logs by grep (`FAIL`, `DATA RACE`, `_test.go:`),
  never cat.
- Report in the fixed shape: per item — commit, files, the parity test,
  guard status with HEAD; differences from the brief; decisions taken;
  left out and why; open questions. Push the branch, open the PR with the
  report as body, do NOT enable auto-merge (the owner's reviewer verifies
  first).
