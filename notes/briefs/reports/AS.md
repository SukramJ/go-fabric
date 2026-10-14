# Report AS — the DCL client and the CommissionerControl server (ADR 0020 stage 6, parts a and b)

Branch `wave25/dcl` from `planning/briefs-wave-25` (01aaa1c), pushed, no PR.
matter.js read at `0ceb202d` (`git show 0ceb202d:<path>`), connectedhomeip at
the harness pin `6170af84` (`git show <pin>:<path>`); every `// Mirrors` and
citation comment cites lines read there. DCS below is
`packages/protocol/src/dcl/DclCertificateService.ts`, CCC chip's
`src/app/clusters/commissioner-control-server/CommissionerControlCluster.cpp`,
CCD `examples/fabric-sync/bridge/CommissionerControlDelegate.cpp`. Each
commit carries its regenerated reachability inventory (unreached stays **5**,
the ratchet constant untouched). No chip-tool binary was run.

| Commit | Item |
| --- | --- |
| 5c9fe36 `feat(controller/dcl): the DCL client, the certificate store and the OTA version lookup` | §1 |
| 3f4c59f `feat(cluster/core): the CommissionerControl server` | §2 |
| 0e3ddfe `feat(node): CommissionerControl on the Aggregator, the daemon's --dcl` | §3 |
| 3492631 `test(coverfloor): the floor of cluster/spec/commissionercontrol` | §2 fix-up (`make ci` found the missing floor) |
| (this commit) `notes: report AS` | — |

## Per section — what was built, what proves it

- **§1 `controller/dcl`** (new, `net/http` only, floor 85, measured 88.2).
  `client.go`/`types.go`: `dcl.Client{BaseURL, HTTPClient, Timeout, Logger}`
  = DclClient.ts — `ProductionURL`/`TestNetURL` (DclConfig.ts:17-25),
  `DefaultTimeout` 5 s (:39), `#fetchJson` (:91-121: Content-Type header,
  the ledger's error body as `ResponseError`, every other failure code 500
  with the cause), `#fetchPaginatedJson` (:62-89, `pagination.key`
  encodeURIComponent-encoded), the GETs `RootCertificates` (:123-131),
  `CertificatesBySubjectKeyID` (:140-152), `RootCertificateBySubject`
  (:154-169), `Model` (:171-184), `ModelVersions` (:186-199), `ModelVersion`
  (:201-222), `RevocationPoints` (:271-280), `RevocationPointsByIssuer`
  (:285-298), `supportedRecords` (:305-313) and the raw→schema mappings
  (:323-422). `certstore.go`/`github.go`/`store.go`: `CertificateStore`
  (`OpenCertificateStore`) implements `attestation.PAAStore` and
  `attestation.Revocations`: DCS construction/update/fetch (:332-352,
  :952-1148: production roots, test-net with `FetchTestCertificates`, the
  production upgrade, never-downgrade, FetchedAt kept, `#parseAndCheckSkid`),
  `PAA` = `getCertificate`+`getCertificateAsDer` with considerTest (DAV:235,
  :269; self-heal re-fetch :442-593), `CDSigner` = `getOrFetchCdSigner`
  (:851-891), `AddCertificate` (:810-840), the GitHub fallback
  (:1159-1323 over a port of `Github.ts`), persistence through `dcl.Store`
  (`MemoryStore`, `FileStore`, `SettingStore` over `*store.Store`'s
  settings), the daily refresh on an `exchange.Clock`. `revocation.go`/
  `crl.go`: `isRevoked` (:691-726), the hour-long lookup cache, 
  `#fetchAndValidateRevocation` (:1434-1504), `#processRevocationPoint`
  (:1510-1565), `#authenticateCrl` (:1568-1624), `#anchorCrlSigner`
  (:1632-1713), `#assertIssuedBy` (:1734-1747), `parseCrl` (:1785-1998) on a
  port of DerCodec's decode/encode, `sameName` (:188-212). `ota.go`:
  `OTAVersions.CheckForUpdate` (checkForUpdate :292-386 without stored
  images, #queryDclForUpdate :182-281), `CheckVersion`/`IsApplicable`
  (:722-819), `Lookup` (is the version in the ledger, with which checksum),
  `Download` (:667-709, http/https only, 5 min), `VerifyImage`
  (#verifyUpdate :388-424 with OtaImageReader.file :65-110).
  Proofs: `client_test.go` (every GET against a fake server in
  DclRestApiTypes.ts's shapes, path encoding, schema versions, pagination,
  the timeout, error bodies), `certstore_test.go` (round trip through the
  Store and an offline reopen, the daily refresh on a manual clock, test-net
  and upgrade, bad roots, CD signer fetch, self-heal, GitHub off/on and
  rate limit, the Stores), `revocation_test.go` (PAI-, PAA-, PAA-delegated
  and PAI-delegated CRLs, the cache's hour, NOT_FOUND / transient / 4xx /
  5xx, thirteen rejection reasons each asserted by its message, partitions
  with and without IDP, indirect CRLs, the expired CRL, sameName),
  `ota_test.go`, and `commission_test.go`: the module's commissioner
  attests the harness `node.Node` against a `CertificateStore` fed by a
  fake ledger holding the CSA NoVID test PAA and serving the test CD signer
  by key identifier (no PaaNotTrusted/CdSignerVerificationSkipped finding;
  without the PAA → PaaNotTrusted; a node whose DAC its PAI's CRL lists →
  CertificateRevoked).
- **§2 `cluster/core/commissioner_control.go`** on the generated
  `cluster/spec/commissionercontrol` (the one line in
  `script/clustergen/clusters.go`). `CommissionerControl` = CCC with CCD:
  RequestCommissioningApproval CASE-only (CCC:141-148), one request open at
  a time (CCD:77), SUCCESS (CCC:175), the `CommissioningApprover` asked
  asynchronously, `CommissioningRequestResult` with request id, client node
  id, fabric index and SUCCESS/FAILURE/TIMEOUT (CCD:79-83, :117);
  CommissionNode: 30–120 (CONSTRAINT_ERROR, CCC:189-194, before the session
  check), CASE (CCC:196-201), approved request of the same client and id
  (FAILURE, CCD:139-145, TC_CCTRL_2_2.py steps 5/14), the minted window
  (CCD:154-175), the `ReverseCommissioner` run after the response
  (CCC:217-226), the reset (CCD:200-201). `CommissioningApprovalRequest.Matches`
  = SetUpCodePairer.cpp:690-707. Proofs:
  `commissioner_control_parity_matterjs_test.go` (spectest.CheckServer
  against the snapshot definition), `commissioner_control_test.go` (CASE
  only for PASE/group/no subject, constraint order, granted with the
  request/client/fabric/label and the event, unknown id / other client /
  other fabric FAILURE, the window's verifier re-derived from the passcode
  the commissioner gets, reset, denied, approver error, TIMEOUT, expiry,
  Close, randomness failure, Matches).
- **§3 `node`** — `Config.CommissionerControl`
  (`CommissionerControlConfig{Approver, Commissioner,
  SupportedDeviceCategories, ApprovalTimeout, ApprovalValidity}`), mounted
  on the Aggregator (where `device/aggregator_gen.go` lists it under
  FabricSynchronization and fabric-sync mounts it, kAggregatorEndpointId),
  `Node.CommissionerControl()`, closed in `Stop`.
  `TestLiveCommissionerControl` (not parallel) runs the whole flow: the
  controller (fabric 1) commissions A and B, subscribes to A's event,
  invokes RequestCommissioningApproval (SUCCESS; approver asked with
  112233/fabric/label), receives the SUCCESS event over the subscription,
  invokes CommissionNode, opens B's window with the answer
  (AdministratorCommissioning.OpenCommissioningWindow, timed), and A's
  commissioner port — the module's `controller/commissioner` on a fabric 2,
  filling the node's port — commissions B: **B holds both fabrics**, A's
  request is closed and A takes a new one. The last step needed nothing of
  `controller/` beyond the port. Daemon: `--dcl prod|testnet|off`
  (default off, `dcl.go`, README): looks the `--ota-provider` image up in
  the ledger and logs the verdict; tests `TestDCLFlag`,
  `TestLedgerCheckedImages`.

## Guards

Sequential, all at HEAD `3492631` (logged before and after each run):

- `make ci` — first run at `0e3ddfe` failed only on the coverage floor
  check ("cluster/spec/commissionercontrol has no floor entry"); floor
  added (100) in `3492631`; rerun **green** (lint 0 issues; `controller/dcl`
  88.2 % vs floor 85, `cluster/core` 90.5, `node` 82.7).
- `make race` — **green**.
- `make reachability-check` — **green** (unreached 5, ratchet untouched).
- `make chipdm-check` (CHIP_ROOT set) — **green**, `TestChipDataModelCrosscheck`
  ran (not skipped): the new generated definition agrees with chip's data
  model.

## Brief lines that differ from the files

1. "an expired CRL ignored with a warning — cite the service's lines": DCS
   parses `nextUpdate` (:1849-1874) and returns it, but no line of the
   service reads it — an expired CRL counts. The port does the same;
   `TestRevocationExpiredCRL` pins it.
2. "CRLs lazily with the 1-hour cache": `AsyncCache` (general/src/util/
   Cache.ts:72-78, :114-137) expires an entry an hour after its *last use*,
   swept by an hourly timer (so up to ~2 h idle). The port checks lazily on
   use (sliding hour, never more).
3. "the GitHub development-certificate fallback behind an option, off by
   default": matter.js turns it on with `fetchTestCertificates`
   (DCS:1000-1001). Followed the brief (D1/D2); commented in the option.
4. D3 "CommissionNode only for an approved, unexpired request" and
   "approval … timed out": chip has neither — its delegate decides at once
   and keeps an approval until used (CCD:121). Both are config
   (`ApprovalTimeout`, `ApprovalValidity`), 0 = chip's behaviour. The
   TIMEOUT (0x94) and FAILURE result codes for timeout/denial are my
   reading of the status enum, not cited from chip.
5. D3 "the vid/pid check against the request": chip does not check it in
   the server; the fabric-sync admin puts the request's vid/pid into the
   setup payload it pairs with (FabricAdmin.cpp:88-113) and the pairer
   skips a discovered node that does not match (SetUpCodePairer.cpp:
   690-707) — TC_CCTRL_2_2 steps 11-20 expect an approved, answered
   request whose commissioning then fails. Ported as `Matches` for the
   host's commissioner (the live test's port applies it).
6. D3 "a PAKE verifier/discriminator/iterations/salt the server minted":
   chip's example mints only the salt and uses fixed passcode 20202021 and
   discriminator 3840 (CCD:33-36). The port mints passcode and
   discriminator at random too.
7. D2 says the daemon's doc names "vendor/product ids of attested devices,
   subject key ids": the daemon attests no device (no commissioner). Its
   `--dcl` sends the requestor's vid/pid and the offered version; the doc
   says so and names what a commissioner's `CertificateStore` would send.
8. ValidateCommissionNodeCommand compares the node id only, with a TODO to
   move to ScopedNodeId (CCD.h:40); the port compares node id **and**
   fabric index.

## Decisions

- `dcl.Client == nil` in `CertificateOptions` is matter.js's `offline`
  (nothing fetched, CRLs not checked); a nil `TestClient` with
  `FetchTestCertificates` is the client's settings against `TestNetURL`.
- The trust store persists as one JSON blob (metadata + DER) through
  `dcl.Store`; entries without DER are dropped on load (#loadIndex's
  missing-entry branch).
- The attestation seams take no context: the store's on-demand fetches
  (CD signer, CRLs, self-heal) run on `context.Background()` bounded by the
  client timeouts (5 s; 3 s for revocation, DCS:1436).
- The trust-store certificate parsing is duplicated in `controller/dcl/
  cert.go` (same readings as `controller/attestation/cert.go`, which keeps
  them unexported) rather than exporting from `controller/attestation`.
- `OTAVersions` stores no images; `VerifyImage` computes the whole-file
  checksum of any NI type `otaimage.NewDigest` knows (matter.js's
  HASH_ALGORITHM_BY_ID) and keeps OtaImageReader's "size mismatch is a
  warning when the checksum matched".
- CommissionerControl lives on the Aggregator, not the root; the
  commissioning context is bounded by the 300 s window.

## Left out

Revocation sets installed out of band (`installRevocations`,
`parseRevocationSet`), seeds, `getOrFetchCertificate`/`deleteCertificate`,
the compliance endpoint (so `#checkSpecificVersion`'s specification version
from compliance info), the vendor-info service and `fetchAllVendors`,
DclOtaUpdateService's image storage, ML-DSA certificates, Joint Fabric, a
CommissionerControl client, chip's CCTRL Python cases (per brief).

## Questions

1. **Out of the allowed paths, needed for the guards**: adding the
   generated definition makes `script/devicegen` regenerate
   `device/aggregator_gen.go` and `device/types_gen_test.go` (+10 lines:
   the Aggregator's CommissionerControl definition and accessor);
   `TestCommittedFilesAreCurrent` fails otherwise. Committed in §2; AR owns
   `device/` — regenerate at integration.
2. **im/ (not changed, security-relevant)**: the CommissionerControl event
   is access "S M" (fabric-sensitive, Manage). `im/read.go`
   `isFabricSensitiveEventCluster` / `eventReadPrivilege` hard-code
   AccessControl and Groupcast, so today a mounted server's
   CommissioningRequestResult is readable at View and by other fabrics.
   Proposed: add 0x0751 to both (privilege 4). The server is opt-in and the
   daemon does not mount it.
3. Should `controller/attestation` export its certificate reading so
   `controller/dcl` drops its copy, and should `PAAStore`/`Revocations` take
   a `context.Context`?
4. Default for `ApprovalTimeout` (0 = wait for the approver, as chip)?

## by_design candidates (not written)

GitHub fallback off by default; random passcode/discriminator for the
reverse window; ScopedNodeId check in CommissionNode; sliding CRL cache
without the hourly sweep; trust store as one blob; `nil Client` = offline;
`ApprovalTimeout`/`ApprovalValidity`.
