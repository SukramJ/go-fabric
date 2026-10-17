# Brief AZ — milestone 1, phase 11: Signer, window open, DCL snapshot mechanism, README status

Owner: Fable. Implementer: Opus. Roadmap `docs/roadmap.md` milestone 1
(`v0.7.0`); detail in `docs/concept-v3-commissioner-in-the-field.md` §A.2–A.5.
Runs in parallel with brief BA (phase 12: `transport/btp`, `controller/ble`,
`controller/commissioner/flow.go`, `cluster/core/network_commissioning.go`,
`examples/reference-bridge`, `examples/reference-controller/pair.go`) —
stay out of those files. Branch `m1/phase-11` from your worktree's HEAD
(`docs/roadmap` = main + the roadmap docs + this brief), one PR, report
`notes/briefs/reports/AZ.md`. `CHANGELOG.md` entries under `[Unreleased]`
are yours; `notes/parity/` is the owner's (list candidates in the PR).

Read `CLAUDE.md` (incl. Token use), concept v3 §A.2–A.5, and for every
item the matter.js lines below **at the pin**
(`git -C ../matter.js show 0ceb202d:<path>`; the checkout's HEAD has moved,
never read the working tree). Mirror, cite (`// Mirrors matter.js … at
0ceb202d`), test first. One commit per item.

## Items

1. **A.4 Window open / revoke in the reference controller.**
   `window open <node> [--timeout s] [--basic]`, `window revoke <node>`
   (new `examples/reference-controller/window.go`, one line in `main.go`'s
   command table, README §Commands). Mirror
   `packages/node/src/behavior/system/commissioning/CommissioningClient.ts:454-510`
   (revoke a stale window first, `#revokeStaleCommissioningWindow`;
   enhanced: `PaseClient.generateRandomDiscriminator` / `generateRandomPasscode`
   / `generatePakePasscodeVerifier` :490-504 — take salt length and
   iteration count from `PaseClient.ts` at the pin, not from memory) and
   `packages/matter.js/src/device/PairedNode.ts:1376-1420` (900 s default;
   `WindowNotOpen` on revoke is not an error). Go pieces that exist:
   `secure/spake2.PBKDF` / `NewVerifierContext` (`spake2.go:171,207`; the
   wire verifier is `w0 ‖ L`, 32 + 65 bytes — see `NewVerifierFromValue`
   :251 for the split), `secure/setup.QRCode` / `ManualCode` /
   `ManualCodeLong` (`setup.go:110,227,238`), `client.Node.Invoke`
   (`controller/client/interact.go:114`; AdministratorCommissioning
   commands are **timed** — `interaction.InvokeOptions{Timed: true}`),
   payloads `cluster/spec/administratorcommissioning/definition_gen.go:95,126,145`.
   Output: discriminator, passcode, manual code, QR string. Proof: a live
   test after `node/commissioner_control_live_test.go`: the own controller
   pairs the own node, opens an enhanced window, a second commissioner
   with a fresh fabric pairs with the printed passcode + discriminator;
   `revoke`; the basic variant. Add row C11 to
   `notes/manual-test-plan-0.4.0.md` Teil C ("window open, then add the
   device in Apple Home").
2. **A.3 Keys behind `crypto.Signer`.** `controller/ca` (`signer.key`,
   `CA.rootKey`, `encode.go:178 signCert`) and
   `controller/commissioner` (`FabricConfig`, `Fabric.Identity.PrivateKey`,
   `fabric.go:115,175,188-212`) accept a `crypto.Signer` (P-256) for the
   CA key(s) and the operational key: `ca.Config.RootSigner` /
   `ICACSigner`, `commissioner.FabricConfig.OperationalSigner`. When set,
   the stored record (`ca.Stored`, `ca.FabricRecord`, `persist.go:28-45`)
   holds certificates and a `KeyRef string`, no private key; when unset,
   the SQLite path is unchanged (back-compatible, same JSON). The CASE
   initiator's Sigma3 signature and the NOC CSR go through the signer
   (`signer.Sign(rand, digest, crypto.SHA256)` where `ecdsa.SignASN1` is
   today) — keep that diff minimal and name every changed signature site
   in the report; the owner reviews those lines. Ship one alternative
   signer: a passphrase-encrypted key file, **stdlib only**
   (`crypto/pbkdf2` + AES-GCM; `golang.org/x/crypto` would be a sixth
   direct dependency — not allowed), `controller/ca/keyfile` or similar,
   with a doc paragraph on wiring a PKCS#11 / KMS signer. Reference
   controller: `--ca-key file:<path>` (passphrase from `GOFABRIC_KEY_PASSPHRASE`).
   matter.js has no such seam (`packages/protocol/src/fabric/Fabric.ts:50-76`,
   `#keyPair: Key`) — list `BD-Controller-KeysBehindSigner` as a by_design
   candidate in the PR. Device side (`node`'s NOC key): only if it is no
   break; otherwise one paragraph in the report on what it would take.
3. **A.2 DCL snapshot — mechanism and data.** *(Amended 2026-10-17 after
   the owner's decision, see `notes/reference/dcl-data-terms-2026-10-17.md`:
   the data **is** committed.)* `controller/dcl/snapshot`: `Load(fs.FS) (*Snapshot, error)`
   reading `paa/*.der`, `cd-signers/*.der` and a `SOURCE` file (ledger
   URL, fetch time, count, SHA-256 per file — mirror the fields of
   `packages/protocol/src/dcl/SeedTypes.ts` `SeedSource` / `CertSeedEntry`),
   consumed as `DclCertificateService.ts:336-340` and `#consumeCertSeed`
   :1352-1410 do (parse, SKID check, malformed entry aborts). The
   snapshot serves as `attestation.PAAStore` and as the CD-signer set.
   `attestation.TrustMode`: `Development` (snapshot + the embedded test
   PAAs, the reference controller's default), `Production` (snapshot
   only, online `dcl.CertificateStore` on top when `--dcl` is set), `None`
   (no chain check, explicit flag only). `make update-dcl-snapshot` fills
   `controller/dcl/snapshot/data/` from the ledger with the existing
   `dcl.Client`; run it once and **commit the DER files and `SOURCE`**
   (a review step like the schema refresh, never automatic); the package
   embeds them. `THIRD-PARTY-NOTICES.md` gets a paragraph: public root
   certificates from the CSA Distributed Compliance Ledger, published by
   requirement of the Matter PKI Certificate Policy §2.2 (PAA
   certificates: Public), precedent connectedhomeip
   `credentials/production/`; the `SOURCE` file names ledger URL, fetch
   time, count and SHA-256 per file. The reference controller also takes
   `--trust-dir <path>` for a newer local set. CRL / revocation stays
   online (the relying-party duty, CP §1.4.5, §4.9.6).
4. **A.5 README status table.** Directly after the opening paragraph: a
   compact "Status" table — device side, controller side, BLE (controller:
   milestone 1 in progress; device: milestone 7), Thread (dataset
   provisioning only), ICD (milestone 5), certification (not pursued,
   certifiable; link), verified ecosystems (Apple Home, Home Assistant;
   Google Home and Alexa pending) — each row one line with a link to
   `docs/certifiability.md`, `docs/matterjs-comparison.md` or
   `docs/roadmap.md`. Numbers from `docs/roadmap.md` §Baseline.

## Guards

`make ci` green; `make race` on `controller/...`; `make reachability` in
the commit that moves it. The daemon's composition and FeatureMap do not
move — **no chip-tool run**; say so in the report. The live test of item
1 is the in-repo proof. `make fuzz` unchanged.

## Report (fixed shape)

Per item: what was built, the guard that proves it, differences from the
matter.js lines, decisions taken, left out, questions; the list of
signature sites changed (item 2); by_design candidates; HEAD of each
guard run.
