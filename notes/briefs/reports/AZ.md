# Report AZ — milestone 1, phase 11

Branch `m1/phase-11` from `docs/roadmap` (d74f5d6), rebased onto
`origin/main` (b4ffec3; the roadmap merged as #141, the DCL terms as
#142 — no Go file moved, the hashes below are the rebased ones, on which
the guards' trees are identical). matter.js read at the
pin `0ceb202d` (`git -C ../matter.js show 0ceb202d:<path>`), chip at the
harness pin `6170af84`. Every commit carries its reachability inventory
(unreached unchanged at 5) and a CHANGELOG entry under `[Unreleased]`.
Item 3 follows the owner's amendment of 2026-10-17: the DCL data is
committed. BA's files are untouched.

| Commit | Item |
| --- | --- |
| 5830d2d `feat(reference-controller): window open and window revoke` | 1 (A.4) |
| 5e65d52 `feat(controller): CA and operational keys behind crypto.Signer` | 2 (A.3) |
| a277d1a `feat(controller): embedded DCL snapshot and trust modes` | 3 (A.2) |
| 2666f2a `docs(README): status table after the opening paragraph` | 4 (A.5) |
| 761c57f `chore(coverfloor): floors for the three new packages` | guard |

## 1. Window open / revoke (A.4)

**Built.** `examples/reference-controller/window.go`: `window open <node>
[--timeout s] [--basic]`, `window revoke <node>`; README §Commands; row C11
in `notes/manual-test-plan-0.4.0.md` Teil C. Enhanced: VendorID/ProductID
read from BasicInformation, stale window revoked, random discriminator
(`randomUint16 % 4096`), passcode (27-bit candidates, `isValidPasscode`,
100 tries), 32-byte salt, 1000 iterations (`CRYPTO_PBKDF_ITERATIONS_MIN`,
`CryptoConstants.ts:24`), verifier `w0(32) ‖ L(65)`, timed
OpenCommissioningWindow; prints discriminator, passcode, manual code
(11 digits, Standard flow) and QR (version 0, Standard, on-IP).
Basic: FeatureMap BC checked first, then revoke, then the timed command.
Revoke: Failure + cluster status WindowNotOpen is "no window", not an error.
**Guard.** `TestReferenceControllerWindow` (pairs the harness; opens a
window; a second tool on a fresh database = second fabric pairs with the
printed passcode + discriminator; harness then holds 2 fabrics; QR and
manual decode back; revoke, revoke without window; `--basic` refused on a
node without BC), `TestPakePasscodeVerifier` (round-trip through
`spake2.NewVerifierFromValue`), `TestRandomWindowCredentials`,
`TestWindowTimeout`.
**Differences.** Mirrors `CommissioningClient.ts:454-549` (the newer API);
`PairedNode.ts:1376-1420` reads the same. matter.js takes vendor/product
from the seeded client state; we read them live (one Read).
**Decisions.** `--timeout` clashes with the global network `--timeout`:
after `window open` it is the window's (seconds), the network one goes
before the command (`windowTimeout`, README says so). The basic variant is
proved only as the refusal: our device side does not advertise BC
(`cluster/wire/admincommissioning.go:41`), and the FeatureMap must not move.
**Left out.** The QR's TLV timeout extension (matter.js TODO at :508 too).
**Questions.** None.

## 2. Keys behind `crypto.Signer` (A.3)

**Built.** `ca.Config.RootSigner` / `ICACSigner`,
`commissioner.FabricConfig.OperationalSigner`, `sigma.Identity.Signer`.
With a signer the record holds certificates, the public key and a KeyRef
(`ca.KeyReferencer`, else `"external"`), no private key; JSON fields
`rootKeyRef`, `icacKeyRef`, `keyRef` (fabric), `privateKey` omitted when
empty. A record that still holds the key is rewritten without it when a
signer over the same key is configured (the migration path); a signer
over another key is `ErrConfig` (CA) or a fresh NOC (operational key); a
KeyRef record without its signer is `ErrConfig` / `ErrFabricMismatch`.
Without a signer the SQLite path and its JSON are unchanged.
`controller/ca/keyfile`: PBKDF2-HMAC-SHA256 (600 000 iterations, floor
100 000) + AES-256-GCM, header as GCM additional data, mode 0600,
`O_EXCL`; package doc has the PKCS#11 / KMS paragraph.
Reference controller `--ca-key file:<path>` (root key; passphrase from
`GOFABRIC_KEY_PASSPHRASE`); a missing file is created from the database's
root key or, for a new database, a new key.
**Guard.** `TestRootSigner`, `TestRootSignerTakesOverStoredKey`,
`TestICACSigner` (incl. offline root), `TestOperationalSignerRecord`,
`TestCommissionWithOperationalSigner` (live: CASE Sigma3 via the signer),
`TestPeerInitiator_SignsThroughSigner`, keyfile tests,
`TestReferenceControllerCAKey` (moves the key, pairs, reads, refuses
without the flag and with a wrong passphrase).
**Changed signature sites (owner review).**
- `controller/ca/encode.go` `signCert`: `ecdsa.SignASN1(rand, key, digest)`
  → `key.Sign(rand, digest, crypto.SHA256)`; parameter `*ecdsa.PrivateKey`
  → `crypto.Signer`.
- `secure/sigma/sigma.go` `signTranscript`: `ecdsa.Sign(rand.Reader, priv,
  digest)` → `key.Sign(rand.Reader, digest, crypto.SHA256)` + ASN.1 →
  `r‖s` (range-checked); new `Identity.signingKey()` (Signer, else
  PrivateKey, else nil → error).
- `secure/sigma/initiator.go:125` key check and `:345` Sigma3 call;
  `secure/sigma/protocol.go:846` Sigma2 call (device responder: same
  algorithm, `PrivateKey.Sign` is `SignASN1`, which `ecdsa.Sign` wraps).
**Differences.** matter.js has no such seam (`Fabric.ts:50-76`, `#keyPair`).
**Decisions.** `--ca-key` covers the root key only; the operational key
has the library seam but no CLI flag (one key file for both would reuse
the root key as a NOC key).
**Device side (not done).** The node's NOC key is generated in-process in
`commissioning/csr.go` `BuildCSR` (:77), stored as a raw scalar in
`store/identity.go` (`PrivateKey []byte`) and parsed in
`node/security.go:163`. A seam needs a host key-generator port for
CSRRequest (the HSM must create the key), a store row holding a KeyRef
instead of the scalar (a migration `v5`), and `node.Config` wiring to set
`sigma.Identity.Signer` (which exists now). Additive, but a store
migration and a new port — its own item before the freeze.
**Questions.** Q1: `--op-key` for the operational key wanted?

## 3. DCL snapshot (A.2, amended)

**Built.** `controller/dcl/snapshot`: `//go:embed data`, `Embedded`,
`Load(fs.FS)`, `Snapshot` (an `attestation.PAAStore`, no `Revocations`),
`Fetch` (the generator over the existing `dcl.Client`), `SOURCE` JSON
(ledger, `builtAt`, per kind `expectedCount` + entries with file, sha256,
subjectKeyId, kind, notBefore/notAfter — `SeedSource` / `CertSeedEntry`).
Load mirrors `#consumeCertSeed` (:1352-1422): test and expired skipped,
parse/SKID mismatch skipped (`#parseAndCheckSkid` :492-515), wrong shape,
missing file or SHA-256 mismatch stops the kind (`ErrMalformed`, partial
snapshot returned). Data committed: 74 PAAs (all approved roots) and 5 CD
signers from `https://on.dcl.csa-iot.org`, fetched 2026-10-17T19:22:08Z
(356 KiB). `make update-dcl-snapshot` (`script/dclsnapshot`), comment
"review step, never automatic"; same in `controller/dcl/doc.go`.
`attestation.TrustMode` `Development` / `Production` / `None`,
`attestation.Trust`, `attestation.Layered`. New exports in `controller/dcl`:
`CheckTrustStoreCertificate`, `PEMToDER`, `ErrSKIDMismatch`.
Reference controller `--trust development|production|none` (default
development), `--trust-dir`, debug line `controller.dcl_snapshot` with
the fetch time. THIRD-PARTY-NOTICES paragraph (CP §2.2 Table 5, §4.4.2,
URL, chip precedent); `docs/security.md` rows updated.
**Guard.** `TestEmbedded`, `TestLoad`, `TestFetch` (fake ledger),
`TestSnapshotIsNoRevocationSource`, `TestTrustModes` (the snapshot holds
the root, the revocation question still reaches the online store),
`TestReferenceControllerTrust` (production: `PaaNotTrusted` for the
harness's test chain; development: only `CertificationTypeTest`; debug
line; `--trust-dir`).
**Differences.** matter.js consumes the seed into the one
`DclCertificateService`; here the snapshot is a layer under the online
store (`Layered`: snapshot, online, test PAAs; first hit wins).
**Decisions.** CD signer set: the ledger has no CD-signer listing (matter.js
fetches by SKID on demand, `getOrFetchCdSigner` :851-891), so
`snapshot.CDSignerKeyIDs` names the five official keys of chip's
`DefaultDeviceAttestationVerifier.cpp:146-256` (gCdSigningKey001Kid…005)
— a chip fact, please verify; the ledger's child listing of the CSA root
`97:E4:69…` returns the same five. `--dcl` now adds the online ledger on top
of the trust set instead of replacing the test PAAs (behaviour change,
CHANGELOG). `None` is `commissioner.Context.Attestation` nil (the existing
"no store" path).
**Left out.** Seeding the online `CertificateStore` from the snapshot (matter.js's
form) — needs `dcl` to import `snapshot` (cycle) or a seed option.
**Questions.** Q2: keep the five-key CD signer list, or a ledger child
query (not in matter.js's `DclClient`)?

## 4. README status table (A.5)

**Built.** "Status" table after the opening paragraph, seven rows, links to
`docs/roadmap.md`, `docs/matterjs-comparison.md`, `docs/certifiability.md`;
numbers from the roadmap §Baseline. Thread row says "dataset provisioning
only, by the commissioner (milestone 1, phase 12)" — the dataset step is
BA's, not in the tree yet.
**Guard.** none beyond `make ci` (README is not in mkdocs).

## by_design candidates

- `BD-Controller-KeysBehindSigner`: CA and operational keys behind
  `crypto.Signer`, record with KeyRef; matter.js `Fabric.ts:50-76` /
  `CertificateAuthority` keep the key pair in storage.
- `BD-Controller-DclSnapshotLayered`: the snapshot is a separate layer
  under the online store, not a seed consumed into it
  (`DclCertificateService.ts:336-340`); and its CD signer set is named
  (chip's five keys) rather than discovered.

## Guards

No chip-tool run: the daemon's composition and FeatureMap did not move.
`make fuzz` unchanged (no fuzz target touched).

- `make ci`: green at 761c57f (first run at 2666f2a failed only on the
  missing floor entries for the three new packages; fixed in 761c57f).
- `make reachability-check`: green at 761c57f (unreached 5).
- `make race` (whole suite, CGO_ENABLED=1): green at 2666f2a; 761c57f
  changes only `script/coverfloor/floors.go`, so no second race run.
