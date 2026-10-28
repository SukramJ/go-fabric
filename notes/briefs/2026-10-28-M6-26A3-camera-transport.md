# Brief 26-A3 — camera transport: the two TLS clusters, WebRtcTransportProvider, PushAvStreamTransport (milestone 6, concept v10)

Date 2026-10-28. Concept v10 wave 26-A3 (`docs/concept-v10-completeness.md`,
§D "Form der chip-Ports" applies: chip's logic in the server, the host's
part behind a generated port, no media or TLS stack in the module). Read
map: `/home/markus/.cache/go-fabric-scratch/read-26a3-26c.md` §1-4, §7-8
(a map; verify every line against the source before citing it).
**Model:** Opus. Branch `m6/26a3-camera-transport` from origin/main (fetch
first). One pull request, commits per cluster, one report
`notes/briefs/reports/26A3.md`. go-fabric `CLAUDE.md` rules (chip via
`git -C /home/markus/Dokumente/GitHub/connectedhomeip show 6170af84:<path>`,
matter.js via `git -C /home/markus/Dokumente/GitHub/matter.js show
862ad75f:<path>`; citations per rule; no attribution lines; English; token
rules). Copy this brief to `notes/briefs/` in the first commit. Disjoint
from wave 26-C (`cluster/spec/ambientcontextsensing`, `cluster/sensing`):
touch nothing there. matter.js has no logic for any of the four
(`<Name>Server.ts:14` is the empty subclass in each — the register
marker); connectedhomeip is the authority (rule 5, Ahead-of-matter.js
rows, `make ahead-check` must hold).

## 1 TlsCertificateManagement (0x0801) and TlsClientManagement (0x0802) — `cluster/tls`

- New package `cluster/tls` (kit servers `tls.CertificateManagement`,
  `tls.ClientManagement`), after chip
  `src/app/clusters/tls-certificate-management-server/TLSCertificateManagementCluster.cpp`
  and `tls-client-management-server/TLSClientManagementCluster.cpp`.
- Configuration checks as chip's constructors: MaxRootCertificates ≥ 5 and
  MaxClientCertificates ≥ 5 (`TLSCertificateManagementCluster.cpp:70-71`),
  MaxProvisioned in 5…254 (`TLSClientManagementCluster.cpp:59-60`);
  the spec constants of `TLSCertificateManagementCluster.cpp:43-47`
  (cert ≤ 3000 bytes, fingerprint ≤ 64, ≤ 10 intermediates, nonce 32,
  ids ≤ 65534).
- The nine certificate commands and the three endpoint commands in chip's
  order with chip's status codes (handler ranges in the read map; cite
  each status line): ConstraintError / AlreadyExists / ResourceExhausted /
  NotFound / InvalidInState via the dependency checker (a root or client
  certificate referenced by a provisioned endpoint cannot be removed —
  `TLSClientManagementDelegate : Tls::CertificateDependencyChecker`),
  DynamicConstraintError, and the cluster-specific
  `kRootCertificateNotFound` / `kClientCertificateNotFound`
  (`TLSClientManagementCluster.cpp:195, :199`). Fabric scoping: every
  table entry is per fabric; `OnFabricRemoved` drops the fabric's entries
  (`TLSCertificateManagementCluster.cpp:571-575`, client :272-275).
- The tables (root certs, client certs with their CSR state, endpoints)
  live in the server in memory, with a `Persist` hook through the module's
  existing `store` pattern (as `store.DoorLockUser` carries the lock's
  users) — read how the lock persists and mirror the shape; if that costs
  a type move, say so in the report instead of moving it. The CSR
  generation (`GenerateClientCsr`) is host work: a `CertificatePort` with
  `GenerateCSR(nonce) (csr, nonceSignature, err)` and the certificate
  validity check of `ProvisionClientCertificate` (:425, :438); no TLS
  library enters the module (stdlib `crypto/x509` is allowed for parsing
  a DER certificate's fingerprint — say which calls).
- Ports generated in a new `script/clustergen/ports_tls.go` (map
  `tlsPorts`); `hosts` entries for the servers' host methods (CK-2 rule,
  e.g. the lookups the PushAV server needs, §3).
- Tests: port chip's 17 + 16 `TEST_F`
  (`tests/TestTLSCertificateManagementCluster.cpp`,
  `tests/TestTLSClientManagementCluster.cpp`) as `*_chip_test.go`; the
  three `FUZZ_TEST` of `FuzzTLSCertificateManagementPW.cpp` become one Go
  fuzz target over the table upsert paths (registered in `make fuzz`);
  `*_rules_test.go` for the paths no chip case reaches.

## 2 WebRtcTransportProvider (0x0553) — `cluster/camera`

- `camera.Provider` next to the requestor (26-A1), after chip
  `webrtc-transport-provider-server/WebRTCTransportProviderCluster.cpp`:
  the five commands (`SolicitOffer` :595-842, `ProvideOffer` :843-1133,
  `ProvideAnswer` :1135-1151, `ProvideICECandidates` :1153-1193,
  `EndSession` :1194-1213) with chip's validation helpers
  (`ICEServersFollowsSpecConstraints` :133-209, `CheckPrivacyModes`
  :400-445) and status codes; the session table (`FindSession` :295,
  `UpsertSession` :308, `RemoveSession` :332, `GenerateSessionId` :371,
  ids ≤ 65534); `CurrentSessions` served from it. The stream-id and
  stream-usage validation goes through the AvStream server when the
  device has one (`ValidateVideoStreamID`/`ValidateAudioStreamID`/
  `ValidateStreamUsage` in the delegate, `.h:208-240`) — reuse
  `camera.AvStream`'s host helpers, do not duplicate the rules.
- Port `ProviderPort` (handle solicit/provide offer, answer, ICE
  candidates, end session — the WebRTC stack is the host's, as the
  requestor's is) in `ports_camera.go`; deferred offer path kept
  (`HandleSolicitOffer` returning a deferred flag).
- Session key: reuse `webrtctransportrequestordef.SessionKey` only if the
  provider's key is the same shape; otherwise the def package gets its
  own type via the `hosts` `types` field (CK-2) — say which.
- Tests: chip's 7 `TEST_F` (`tests/TestWebRTCTransportProviderCluster.cpp`,
  attribute and session-table level) plus `provider_rules_test.go` for
  every handler path (chip has no handler tests — the rules tests are the
  parity proof here, each citing the chip line it checks).

## 3 PushAvStreamTransport (0x0555) — `cluster/camera`

- `camera.PushTransport`, after chip
  `push-av-stream-transport-server/PushAVStreamTransportLogic.cpp`
  (the glue `PushAVStreamTransportCluster.cpp` adds nothing but the
  attribute map): `ValidateUrl` :293-356, `ValidateIncomingTransportOptions`
  :387-664, `ValidateStreamParameters` :665-879, the seven handlers
  (`AllocatePushTransport` :880-1228 … `UpdateMotionZoneOptions`
  :1604-1846) with chip's status codes and the cluster-specific ones
  (`kInvalidTLSEndpoint`, `kInvalidURL`, `kInvalidZone`, …); the constants
  of `constants.h:37-45`; `SupportedFormats` seeded CMAF/CMAFIngest
  (Logic.cpp:91-92); the deallocation timer (`ScheduleTransportDeallocate`
  :357) on the module's clock.
- Dependencies, as chip wires them (`CodegenIntegration.cpp:124, :133`):
  the server takes the TLS client and certificate servers' lookup
  interfaces (§1's `hosts` methods) to validate `TLSEndpointID`, and the
  Zones server (26-A2) to validate zone ids (`kInvalidZone`); when the
  device has none of them the corresponding checks answer as chip does
  without a delegate — read it and say which status.
- Port `PushTransportPort` (allocate / deallocate / modify / set status /
  manual trigger / motion-zone update, the delegate of
  `push-av-stream-transport-delegate.h:42-348`); the storage structs of
  `push-av-stream-transport-storage.h` become def types where the port
  needs them (generated via `hosts` `types` or `ports` — say which).
- Tests: chip's 9 logic `TEST_F` (`tests/TestPushAVStreamTransportCluster.cpp`)
  ported in full; of the 20 storage and 33 remaining cases
  (`TestPushAVStreamTransportStorage.cpp` and the third file — name it)
  port those that check a rule the Go server has and list the rest with
  reasons (storage layout cases are chip's persistence, not ours).

## 4 Device kit, docs, release

- `device/defaults_camera.go`: the provider is the camera's default when
  the declaration carries `ProviderPort`; PushAV and the two TLS servers
  when their ports are present (as 26-A2 did for AvStream/Zones);
  `camera_gen.go` already lists the provider "M" (:42) and PushAV "O"
  (:51); TLS is "O" on CameraController only (`camera_controller_gen.go:61,
  :64`) — mount the TLS servers on any endpoint whose declaration asks for
  them, do not invent a device type. New `TestSelfHeldDefaults` cases and
  declaration tests.
- Docs: `docs/guides/camera.md` (transport model: offer/answer flow,
  push transports, TLS provisioning, the ports), ports-guide rows, Host
  table rows, comparison rows (the four clusters ✅ with the by_design
  entry), `docs/certifiability.md` if a CSA family covers them (check
  `internal/chiptool` families; probably none — say so).
- Register: four Ahead-of-matter.js rows (marker `<Name>Server.ts:14`
  each, source chip paths above, tests) — insert the rows; the by_design
  entries themselves are written by Fable after hand-back (propose each in
  one paragraph in the report).
- CHANGELOG `[Unreleased]` Added; no break expected (apidiff names one if
  a camera type moves — then Changed + migration guide). Coverage floors
  for the new packages.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make ahead-check`, `make reachability` + `cite-report`
  (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
  committed, `make fuzz` (the new target), `mkdocs build --strict`; one
  bundle at the end under `flock /home/markus/.cache/go-fabric-scratch/guards.lock`
  (one bundle at a time on the machine; wait for the lock). No chip-tool
  run unless the reference bridge's composition changes (it should not).
  If the memory reaper kills `make`, report it and do not restart on your
  own. Hand back once: PR number, HEAD, guards with results, per cluster
  the ported/left-out test counts, differences from the brief, Questions
  verbatim, the four by_design proposals.
