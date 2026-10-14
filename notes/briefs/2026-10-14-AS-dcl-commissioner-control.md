# Brief AS — the DCL client and the CommissionerControl server (ADR 0020 stage 6, parts a and b; Joint Fabric deferred)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AR
(media: `cluster/spec` media definitions, `cluster/media`,
`cluster/core/messages.go`, `script/clustergen`, `script/devicegen`,
`device/`, `examples/reference-bridge`, `internal/chiptool`,
`docs/certifiability.md`) at the same time: stay out of those paths;
the shared files are `CHANGELOG.md` and `script/clustergen/clusters.go`
(add your one line, `CommissionerControl`, at the list's end and commit
the generated definition; the owner merges). **Never run `make
chiptool-test`, `make chiptool-families` or any chip-tool binary** (the
harness is the other agent's). Branch `wave25/dcl` from
`planning/briefs-wave-25`, pushed, **no PR**. Report
`notes/briefs/reports/AS.md`. Read first: `notes/briefs/README.md`,
[ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(stage-6 row and the sentence that stage 6 is separable),
`controller/attestation` (`PAAStore`, `Revocations`, `StaticStore`,
`TestStore`, how the verifier consumes them — `store.go:34-53`,
`verifier.go`), `controller/commissioner` (`Options.Attestation`,
`Context.Attestation`), `cluster/core/ota_provider.go` (the
`OTAImageSource` port; where a DCL version check would sit),
`bdx/otaimage` (`ExpectedChecksum`), `bridge/commissioning_window.go`
and `node.Node.CommissioningWindow()` (the window the CommissionerControl
server reverses), `cluster/core/administrator_commissioning.go` (the
window's cluster), `eligibility/vendor_name.go:5-25` (the privacy
decision: vendor names embedded, no network lookups by default),
`examples/reference-bridge/main.go` (flags), report AQ.

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`),
paths under `packages/protocol/src/dcl/`: `DclClient.ts` (429 lines:
the REST GETs — `/dcl/pki/root-certificates` :124, `/dcl/pki/all-certificates?subjectKeyId=` :144,
`/dcl/pki/certificates/{subject}/{skid}` :155, `/dcl/model/models/{vid}/{pid}` :172,
`/dcl/model/versions/{vid}/{pid}[/{sv}]` :187, :207,
`/dcl/pki/revocation-points` :275; `DEFAULT_DCL_TIMEOUT = Seconds(5)` :39,
:95-101; the production and test-net base URLs — read `DclConfig.ts`),
`DclCertificateService.ts` (2160 lines: PAA and CD-signer sets persisted
and refreshed daily, the GitHub development-certificate fallback, CRL
fetch with a 1-hour cache and Matter Core §6.2.6.1 validation),
`DclOtaUpdateService.ts` (1319 lines: `/dcl/model/versions`, the
http(s)-only download :615-712, `#verifyUpdate` :388-424 vid/pid/version
and the whole-file checksum), `DclVendorInfoService.ts`,
`DclRestApiTypes.ts`, `SeedTypes.ts`; the consumers
`packages/protocol/src/.../DeviceAttestationValidator.ts:220-399` (PAA
lookup, chain, test-certificate policy, DAC/PAI revocation, CD signer;
the service optional with warnings when absent) and
`ControllerCommissioner.ts:665` (where it is passed in). **CommissionerControl
has no logic in matter.js** (`CommissionerControlServer.ts:14`, an empty
class); the model is `packages/model/src/standard/elements/commissioner-control.element.ts`
(0x0751, revision 1: `SupportedDeviceCategories`, the
`CommissioningRequestResult` event, `RequestCommissioningApproval`,
`CommissionNode`, `ReverseOpenCommissioningWindow`), the behaviour
reference is chip at the harness pin:
`src/app/clusters/commissioner-control-server/` (every file; `git -C
../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`
for what the sparse checkout lacks) and chip's `fabric-sync-app`'s use
of it (`examples/fabric-sync/`), cited line by line. Guards: `make ci`,
`make race`, `make reachability-check`, `make chipdm-check` (a new
generated definition), no chip-tool (export `PATH=$HOME/go/bin:$PATH`,
`CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip`; a `make ci`
failing only on golangci-lint's timeout after "0 issues" is rerun as
`make lint`; a `node/` live test failing once with "Could not connect to
device" is rerun alone; new live tests in `node/` do not call
`t.Parallel()`). One commit per §, reachability in each. Allowed paths:
`controller/dcl` (new), `controller/attestation` **only** if the
`PAAStore`/`Revocations` seams lack something the DCL store needs (a
question in the report first), `cluster/core/commissioner_control.go`
(+ tests), `cluster/spec/commissionercontrol` (generated; the one list
line in `script/clustergen/clusters.go`), `node/` (§3 wiring),
`examples/reference-bridge` (§3: a `-dcl` flag only, off by default —
no CommissionerControl on the daemon), `script/coverfloor/floors.go`,
`CHANGELOG.md`. Everything else is read, not changed. `notes/parity/`
and ADRs are the owner's.

Out: Joint Fabric (JointFabricDatastore, JointFabricAdministrator, JCM,
the Administrator CAT, ICAC cross-signing — deferred by the owner past
`v1.0.0`: no matter.js behaviour, no proof without chip's JF apps), the
DCL's vendor-info service beyond what §1 needs, DCL writes, the
compliance endpoint, chip's CCTRL Python cases (they need
`fabric-sync-app`; the proof is in-repo), a CommissionerControl client.

## Owner's decisions

- **D1 — `controller/dcl` is a port of `DclClient.ts` and the used parts
  of `DclCertificateService.ts` and `DclOtaUpdateService.ts`**, on
  `net/http` only: `dcl.Client{BaseURL, HTTPClient, Timeout (5 s)}` with
  the GETs above, pagination as `DclClient.ts` does; `dcl.CertificateStore`
  implementing `attestation.PAAStore` and `attestation.Revocations`
  (PAAs and CD signers fetched, persisted through a small `Store`
  interface — a `store` setting key or a file the host names — and
  refreshed daily; the GitHub development-certificate fallback behind an
  option, off by default; CRLs lazily with the 1-hour cache and the
  §6.2.6.1 checks); `dcl.OTAVersions` answering the provider's
  `OTAImageSource` question "is this software version in the ledger for
  vid/pid, with which checksum" (the provider's host port stays the
  host's: a helper the host calls, not a change to `ota_provider.go`).
  The test-net and production base URLs as constants; a fake HTTP
  server in tests with `DclRestApiTypes.ts`'s shapes.
- **D2 — opt-in, privacy first.** Nothing contacts the ledger unless a
  host constructs a `dcl.Client`; the reference daemon's `-dcl
  <prod|testnet|off>` defaults to off and its doc names what leaves
  the host (vendor/product ids of attested devices, subject key ids).
  The embedded vendor names stay (`eligibility/vendor_name.go`).
- **D3 — the CommissionerControl server follows chip** (`cluster/core/commissioner_control.go`
  on the generated definition): `RequestCommissioningApproval` only over
  CASE (UNSUPPORTED_ACCESS otherwise), answered SUCCESS, the host port
  `CommissioningApprover.Approve(ctx, request) (approved bool, err)`
  asked asynchronously, the `CommissioningRequestResult` event with the
  request id, the client's node id, the fabric index and the status
  code; `CommissionNode` only for an approved, unexpired request (the
  statuses chip answers — cite), answering `ReverseOpenCommissioningWindow`
  with a PAKE verifier/discriminator/iterations/salt the server minted
  (the cluster's window parameters — read how chip builds them and what
  it does next: the server commissions the *client's* node through the
  window the client opens, which needs the module's own commissioner;
  wire `controller/commissioner` through a port the node fills) and the
  vid/pid check against the request. `SupportedDeviceCategories` from
  the config. Mounted on the root or an aggregator endpoint by the
  host (`node.Config.CommissionerControl{Approver, …}`); the reference
  daemon does not mount it.
- **D4 — no new dependency; no cgo.**

## §1 — `controller/dcl` (client, certificate store, OTA versions)

D1, D2. Tests: every GET against a fake server with recorded shapes,
pagination, the 5 s timeout, the PAA/CD-signer set round trip through
the `Store`, the daily refresh on a fake clock, the dev-certificate
fallback off/on, CRL cache and the §6.2.6.1 checks (a revoked DAC
serial found; an expired CRL ignored with a warning — cite the
service's lines), `OTAVersions` answering present/absent with the
checksum. A `commissioner` test commissioning the AK/AL harness node
with `Options.Attestation` = a `dcl.CertificateStore` fed by the fake
server holding the test PAA.

## §2 — the CommissionerControl server (`cluster/core`)

D3. Parity test against the snapshot; behaviour tests per command and
status (CASE-only, approval granted/denied/timed out, the event, an
unknown or expired request id, the vid/pid mismatch, the reverse
window's parameters).

## §3 — node wiring and the proof

`node.Config.CommissionerControl` (nil: not mounted); `-dcl` on the
daemon. `TestLiveCommissionerControl` (in `node/`, not parallel): two
in-process nodes on one fabric through the module's commissioner; the
controller invokes `RequestCommissioningApproval` on node A (the
approver port approves), receives `CommissioningRequestResult` over a
subscription, invokes `CommissionNode`, gets `ReverseOpenCommissioningWindow`,
opens node B's window with those parameters, and node A's commissioner
port commissions B (the module's commissioner) — assert B has a second
fabric and A's request state. If the last step needs more of
`controller/` than a port the node fills, prove up to the reverse
window and say so.

## Report

As AK–AR: per §, guards with HEAD, brief lines that differ from the
files (DclClient, DclCertificateService, chip), decisions, left out,
questions, `by_design` candidates (not written). Floors: `controller/dcl`
85.
