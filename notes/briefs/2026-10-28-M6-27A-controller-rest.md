# Brief 27-A — the controller remainder after matter.js: the invoke batcher, the discovery shape, the DCL vendor-info service, NOC rotation, PQC phase 1 (milestone 6, concept v10)

Date 2026-10-28. Owner decisions 2026-10-28: one wave, one PR; PQC phase 1
now, with `github.com/cloudflare/circl` (BSD-3) for ML-DSA — the seventh
direct dependency, a decision recorded in the report and
`THIRD-PARTY-NOTICES.md` (+ `licenses/`), to be replaced by the standard
library when Go ships `crypto/mldsa`. Read map:
`/home/markus/.cache/go-fabric-scratch/read-26d-27a.md` §5-8 (a map;
verify every line before citing). **Model:** Opus. Branch
`m6/27a-controller-rest` from origin/main (fetch first). One pull
request, commits per part, one report `notes/briefs/reports/27A.md`.
go-fabric `CLAUDE.md` rules (matter.js via `git -C
/home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`; citations;
no attribution lines; English; token rules; `secure/` and
`commissioning/` are Fable paths — PQC certificate parsing lands in
`controller/attestation` and `secure/` only where the certificate codec
lives; list every `secure/` file touched with the reason). Copy this
brief to `notes/briefs/` in the first commit. Disjoint from 26-D
(`cluster/jointfabric`, `controller/commissioner/jcm*.go`) and 26-A3
(`cluster/camera`, `cluster/tls`, the node TLS seam).

## 1 The invoke batcher — `client/interaction`

matter.js batches invokes per network lane on a timer tick
(`packages/protocol/src/action/client/ClientInteraction.ts` at the pin:
`BatchLane` :145 with the comment :142-144, `isBatchable` :185 — Large
Message invokes are not batched :195/:204 —, `#invokeWithBatching`
:744-849, `invoke()` :715 reading `maxPathsPerInvoke` :721, the lanes
drained on `close()`). Go has the split by `MaxPathsPerInvoke`
(`client/interaction/invoke_batch.go:50`, `controller/client/interact.go:243`)
and states the tick batcher is not ported (`interaction.go:13-15`). Port
it: `Interaction.Invoke` with `batchDuration` (option; matter.js's
default — cite it) collects invokes of one session into a lane and sends
them as one InvokeRequest at the tick, honouring `maxPathsPerInvoke` by
splitting (`#invokeWithSplitting` :656-713, InternalError below 1 :662),
with `commandRef` only on multi-command messages (:510); results are
demultiplexed to the callers. The six matter.js test files (read map §5:
`ClientInteractionInvokeTest.ts` 25 cases, `InvokeLargeMessageTest.ts`
18, `request/InvokeTest.ts` 15, `response/InvokeResultTest.ts` 13, and
the two others) ported by meaning into `*_parity_matterjs_test.go`; the
existing split tests stay. `controller.Node.Invoke` and the node facade
gain nothing new — the batching is below them (say in the report where a
facade option appears, if any).

## 2 The discovery shape — `client/discovery`, `controller`

matter.js's controller discovery is a family of cancelable classes
(`packages/node/src/behavior/system/controller/discovery/`: `Discovery.ts`
:37 abstract, `CommissioningDiscovery`, `InstanceDiscovery`,
`ContinuousDiscovery`, `PaseDiscovery`, `ParallelPaseDiscovery` and the
two others — list all eight with lines). Go's `client/discovery` has the
browser and scanner (13 files) and the commissioner runs its own search
(`controller/code.go:102-116`, `controller.go`). Port the shape by
meaning: a `discovery.Session` (or the name you justify) that is
cancelable, reports progress, and comes in the matter.js kinds —
commissioning (by discriminator / pairing code), instance (one
operational instance), continuous (keeps reporting until cancelled), and
the parallel PASE race over BLE and IP (`ParallelPaseDiscovery`: first
transport to answer wins, the other cancelled) which the commissioner uses
when both are configured (today: BLE xor IP). The 58 cases of the six
test files (`DiscoveryCancelTest.ts` 23, `DiscoveryErrorTest.ts` 13,
`DiscoveryTransportsTest.ts` 9, `ParallelPaseRaceTest.ts` 5,
`ContinuousDiscoveryTest.ts`, and the sixth) ported by meaning on the
in-memory network and `bletest`. The facade: `controller.PairOptions`
gets what a UI needs (a progress step for the race — reuse CK-1's
Discovery 2.1 with a sub-step per transport, say which) — no second
shape.

## 3 The DCL vendor-info service — `controller/dcl`

`packages/protocol/src/dcl/DclVendorInfoService.ts` (497 lines: `VendorInfo`
:41, `ProductInfo` :54, the static seed `STATIC_VENDORS` ~:136-158, class
:166, construction :209, offline :214, `infoFor` :221, the vendors getter,
`productInfoFor`, the update cycle) over `DclClient.ts` vendor fetch
(~:262-265). Go has `snapshot.Vendors` (static seed, `snapshot/vendors.go`)
and `Client.Vendors` but no service. Port `dcl.VendorInfoService`: seeded
from the snapshot, refreshed from the DCL on a schedule, offline mode,
`InfoFor(vendorID)` / `ProductInfoFor(vendorID, productID)` and the
observer/update shape matter.js has (an `Observe` as CK-1's manager has).
Tests: `DclVendorInfoServiceTest.ts` 29 and `DclVendorInfoServiceSeedTest.ts`
10 by meaning, on the existing DCL fixtures. Wire it where matter.js's
`DclBehavior.ts` (206 lines) wires it — the controller's DCL facade
(`controller.DCL()` or the existing accessor; say which).

## 4 NOC rotation — `controller/commissioner`

`packages/protocol/src/fabric/FabricAuthority.ts` (216 lines:
`#rotatedFabricIndices` :57, `defaultFabric(config, rotateNoc = true)`
:88 with the comment :86, `#rotateFabricNocKey` :181, once per runtime
:182-183). Go's `FabricAuthority` has the keypair rotation case only
(`fabricauthority_parity_matterjs_test.go:135-152`) and `rotateNoc=false`.
Port the rotation: on the first use of a stored fabric per runtime the
authority rotates the controller's NOC key (new keypair, new NOC signed
by the fabric's CA), updates the fabric's stored credentials and the
operational identity the peers see, once per runtime; the `fabricAdded`
event and `FabricBuilder` shape where the Go facade needs them (say what
stays out and why). Tests: the 19 cases of `FabricAuthorityTest.ts` by
meaning (the ones already ported stay).

## 5 PQC phase 1 — `controller/attestation`, the certificate codec

matter.js's "pqc-phase-1" (`packages/model/src/common/Specification.ts:92`):
`packages/general/src/crypto/MlDsa.ts` (172 lines: ML-DSA-44/65
parameters :30, seed length :38, key kinds :52-…) and the certificate
kinds it adds (read `packages/protocol/src/certificate/` for the PQC
certificate types and the Annex-H fixtures `PqcAnnexHCertificates.ts`).
Go: `controller/attestation/cert.go:75` names ML-DSA certificates as
disabled, `pqcattestationcertificates_parity_matterjs_test.go:10` says
phase 1 is not ported (the register row). Port phase 1: ML-DSA key
parsing and signature verification through `circl/sign/mldsa/mldsa44`
and `mldsa65`, the PQC attestation certificate kinds in the attestation
chain validation (DAC/PAI/PAA with ML-DSA signatures), the 27 `MlDsaTest.ts`
cases and the Annex-H certificate fixtures (`PqcAnnexHCertificates.ts`,
`CertificatesTest.ts`'s PQC cases) by meaning. Device-side PQC (NOC/ICAC
with ML-DSA, CASE) stays out — say so, with matter.js's own scope at the
pin as the reason (read what phase 1 covers there). The dependency:
`go get github.com/cloudflare/circl@<latest tagged>` — record version and
licence in `THIRD-PARTY-NOTICES.md` and `licenses/`; `make lint`'s tidy
diff must be clean; CHANGELOG names the dependency; the register row
moves from "not ported" to a ported row with the circl note and the
stdlib replacement intent.

## 6 Docs, register, release

- Docs: controller guide sections (batching, discovery kinds and the PASE
  race, vendor info, NOC rotation, PQC attestation), comparison rows for
  every item (✅ or a `by_design` entry), findings register rows closed.
- CHANGELOG `[Unreleased]` Added (and Changed for the dependency); a break
  only if a facade type moves (apidiff + migration guide). Coverage floors.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make ahead-check`, `make reachability` + `cite-report`
  (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
  committed, `mkdocs build --strict`; one bundle at the end under `flock
  /home/markus/.cache/go-fabric-scratch/guards.lock`. No chip-tool run
  (device composition unchanged) unless PQC touches the device's
  attestation path — then say so and run `make chiptool-test`. If the
  memory reaper kills `make`, report and do not restart. Hand back once:
  PR number, HEAD, guards, per part the ported/left-out test counts, the
  `secure/` files touched, differences, Questions verbatim, the
  by_design proposals.
