# Report AM — WiFiNetworkDiagnostics with a host port, SecondaryNetworkInterface, the DGWIFI family

Branch `wave19/wifi` from `planning/briefs-wave-19` (8b5b24f), pushed, no
PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`),
connectedhomeip at the harness pin `6170af84` (`git show 6170af84…:…`);
every citation added cites lines read there. Each commit carries its
regenerated reachability inventory (unreached stays **5**). chip-tool was
not run, and neither were `make chiptool-test` / `make chiptool-families`.

| Commit | Item |
| --- | --- |
| e71dd0a `feat(spec,device): generated WiFiNetworkDiagnostics definition and the SecondaryNetworkInterface device type` | §1 |
| ff0f580 `feat(core): WiFiNetworkDiagnostics server over a host WiFiReporter` | §2 |
| c6e23a2 `feat(node): Network.WiFi mounts WiFiNetworkDiagnostics on the root and states the WiFi condition` | §3 |
| 5ba14d2 `feat(reference-bridge): --wifi serves WiFiNetworkDiagnostics from a simulated link` | §4 |
| db3adae `test(chiptool): the DGWIFI family — table, PICS, golden, every daemon on --wifi` | §5 |
| (this commit) `notes: report AM` | — |

## Per section — what was built, what proves it

- **§1** `WiFiNetworkDiagnostics` in `script/clustergen/clusters.go` →
  `cluster/spec/wifinetworkdiagnostics/` (revision 1, PKTCNT bit 0, ERRCNT
  bit 1, 13 attributes, ResetCounts, 3 events). `SecondaryNetworkInterface`
  moved from devicegen's `excluded` to `committed` →
  `device/secondary_network_interface_gen.go` (0x0019 rev 1, utility,
  NetworkCommissioning M, Thread/WiFi/Ethernet diagnostics under their
  conditions); `device/root_node_gen.go` now names the WiFi definition for
  its `[WiFi]` requirement. What devicegen's tests required:
  `device/conformance_test.go` — `refusedDefaults` gets
  "SecondaryNetworkInterface: NetworkCommissioning needs a feature
  selection" (its interface features are O.a) and `fixtures` builds it with
  ET. Coverage floor `cluster/spec/wifinetworkdiagnostics` 100 (measured
  100 %). `go test ./script/clustergen/... ./script/devicegen/...
  ./schema/... ./device/... ./cluster/spec/...` green.
- **§2** `cluster/core/wifi_network_diagnostics.go`: `WiFiReporter` (every
  value method `(v, ok)`, `ResetCounts()`), `WiFiNetworkDiagnosticsConfig
  {Reporter, Features, Attributes, DataVersion}`,
  `NewWiFiNetworkDiagnostics` → `*WiFiNetworkDiagnostics` (embeds
  `*spec.Server`, `SetEndpoint`), the three event methods, ResetCounts.
  Tests (`wifi_network_diagnostics_parity_matterjs_test.go`):
  `TestParityMatterJS_WiFiNetworkDiagnosticsDefinition` (revision, feature
  bits, every attribute's id/name/type/conformance/quality, ResetCounts
  under ERRCNT, the events' ids/names/priority, against element file
  :20-83), `TestParityMatterJS_WiFiNetworkDiagnostics` (`spectest.CheckServer`
  bare and PKTCNT|ERRCNT; null on ok=false for every attribute; empty BSSID
  null; counters gated by feature; ResetCounts accepted only with ERRCNT,
  resets the reporter, UNSUPPORTED_COMMAND without; feature-gated
  attribute refused as opt-in), `TestParityMatterJS_WiFiNetworkDiagnosticsEvents`
  (each event's TLV bytes with the element file's field ids, info
  priority, endpoint).
- **§3** `node.Network.WiFi` (`node.WiFi{Reporter, Features, Attributes}`;
  nil Reporter = no cluster). With it, `RootClusters.WiFiNetworkDiagnostics`
  is built and mounted between SoftwareDiagnostics and
  EthernetNetworkDiagnostics, and the facade's assembler gets
  `RootDeviceConditions: ["WiFi"]`. `node/wifi_test.go`
  `TestWiFiDiagnostics`: over PASE, ServerList lists 0x0036 (and still
  0x0037) only with the port; Bssid reads the reporter's bytes;
  CurrentMaxRate not served unless opted in; the root states `WiFi` only
  with the port; `endpoint.ValidateDeviceTypes` reports nothing about
  either diagnostics cluster in both configurations. The existing node
  tests hold unchanged.
- **§4** `examples/reference-bridge/wifi.go`: `demoWiFi` (BSSID
  02:00:5E:10:20:01 locally administered, WPA2, AX, channel 36, RSSI −52,
  CurrentMaxRate 1.201 Gbit/s, seeded counters), `--wifi` sets
  `Network.WiFi` with PKTCNT|ERRCNT and CurrentMaxRate. App pipe
  `{"Name":"WiFiDiagnostics","Action":"Disconnection|AssociationFailure|ConnectionStatus|StepCounters"}`
  with optional `ReasonCode`, `AssociationFailureCause`, `Status`,
  `ConnectionStatus` (defaults: CHIP's stub values). README documents the
  flag as TESTING ONLY, the package comment of `wifi.go` says what a real
  Linux reporter needs (nl80211 or `iw`). `TestWiFiFlag`.
- **§5** `{name: "DGWIFI"}` in `families_table_test.go`;
  `startBridgeWith` passes `--wifi` whenever the binary offers it, and
  `TestChipCertificationFamilies` requires the flag; `ep0.txt`'s DGWIFI
  block; `reference-bridge.pics`: `DGWIFI.C=0`, `DGWIFI.S.E00/E01/E02=1`;
  golden: families `DGWIFI: [2.1, 2.2, 2.3, 3.2.Simulated]`, cases
  `2.1 {steps 7, skipped 7}`, `2.3 {steps 1, skipped 7}`, hostSkipped 2.2
  (manual), notApplicable 3.2.Simulated (`DGWIFI.C`);
  `docs/certifiability.md` regenerated (`DGWIFI | 4 | 2 | 0 | 0 | 1 | 1 |
  0`) plus one paragraph on the simulated link; `doc.go` one paragraph.

## Guards

`make ci` (build, vet, test, lint, cover-check), `make race` and
`make reachability-check` green at `db3adae` (HEAD logged before and after
each run; unchanged). Unreached stays 5. `cover-check`: 183 packages at or
above their floor. Not run: `make chiptool-test`, `make chiptool-families`
(the harness is the owner's), `make bench-smoke`, `make fuzz`.

## Brief lines that differ from the files

1. **§5 "the PICS in testdata/reference-bridge.pics (DGWIFI.S, the
   attribute … PICS, DGWIFI.S.F00/F01, DGWIFI.S.C00.Rsp)"** — those are
   device-generated codes: `devicePICS` fails when `reference-bridge.pics`
   sets a code matching `reGeneratedCode` (familyrun_test.go:118, :172-175).
   They live in the generated slice `testdata/pics/ep0.txt`, which only an
   `-update-chip-cases` run rewrites — and a narrowed run
   (`GOFABRIC_CHIP_FAMILIES=DGWIFI`) cannot (familyrun_test.go:41-46) while
   `devicePICS` fails on drift before any case runs. So I wrote the
   DGWIFI block of `ep0.txt` by hand as `gen_pics.py` derives it (cluster,
   A0000–A000c, C00.Rsp, F00, F01 = 1; E00–E02 = 0, none is mandatory);
   `reference-bridge.pics` carries only the non-generated `DGWIFI.C=0` and
   the optional events. Nothing else of `ep0.txt` changes: the base MCORE
   codes are not derived from Wi-Fi (`matter/testing/pics.py` :170-171,
   :236 at the pin).
2. **§5 "the harness starts the reference bridge with -wifi for the
   family"** — there is no per-family daemon configuration, and there
   cannot be one with the files as they are: the slices are generated once
   per run from one daemon and TC-IDM-10.4 holds every slice against the
   device (familyrun_test.go:126-250). Every daemon of the suite now
   carries `--wifi` (bridge_test.go `startBridgeWith`).
3. **§5 "the golden case list regenerated the way doc.go says"** — that
   is an `-update-chip-cases` family run, which needs the harness. The
   entries are predicted from the image's test descriptor (read with
   `docker run --network none --entrypoint cat <CHIP_TEST_IMAGE>
   /lib/test-descriptor.json`, no chip-tool started) and the PICS: 2.1 runs
   steps 1–6 and 8, skips 7 and 9–14 (`PICS_SKIP_SAMPLE_APP=0`); 2.3 runs
   step 1 and skips 2, 2a–2f (all `PICS_SKIP_SAMPLE_APP`); the descriptor
   lists 2.2 as `kind: manual`. The counting rule was checked against the
   DGETH entries (2.1: 6/9, 2.2: 11/11).
4. **§2 event methods `Disconnection(reasonCode)`,
   `AssociationFailure(cause, status)`, `ConnectionStatus(status)`** —
   named `OnDisconnectionDetected`, `OnAssociationFailureDetected`,
   `OnConnectionStatusChanged`, chip's delegate names
   (WiFiNetworkDiagnosticsCluster.cpp:84-112); `ConnectionStatus()` would
   read as a getter beside the reporter's. Same arguments, same events.
5. **§4 "look at how chip's all-clusters-app names its Wi-Fi diagnostics
   test commands"** — it has no app-pipe command for the cluster; it has
   TestEventTrigger codes (`WiFiDiagnosticsTestEventTriggerHandler.h`
   :44-56, handled in `all-clusters-common/src/wifi-diagnostics-stub.cpp`
   :78-107). The daemon takes both: the brief's `WiFiDiagnostics` pipe
   command and CHIP's three trigger codes (with `--enable-key`), raising
   the stub's values (reason 3; AuthenticationFailed/4; NotConnected).
6. **§1 "the three diagnostics under [Thread]/[WiFi]/[Ethernet]"** — the
   generated SecondaryNetworkInterface has no accessor for
   ThreadNetworkDiagnostics (no definition committed); the requirement is
   listed without one, as RootNode's already was.
7. **§2 the counters' types** — as the brief and the element file say:
   BeaconLostCount uint32, OverrunCount uint64 (:45, :69).

## Decisions

- **Both diagnostics on one root.** RootNode lists each under its own
  optional condition, `[WiFi]` and `[Ethernet]`
  (`schema/devicetype_requirements_gen.go` :177 and its Ethernet sibling);
  both can hold at once, and the validator reports nothing for either with
  WiFi stated and Ethernet not (`TestWiFiDiagnostics`). The node keeps
  `unknownEthernet` beside a Wi-Fi reporter, as the brief says. It does not
  state `Ethernet` itself today — that predates this brief.
- **All three events always listed.** chip's cluster can generate every
  one (:84-112); the config has no event selection.
- **CurrentMaxRate served by the simulated link** (`A000b=1`): optional,
  plausible, and only PICS_SKIP_SAMPLE_APP-gated steps read it.
- **BSSID/RSSI not range-checked on the way out**: chip encodes what the
  provider gives (non-empty BSSID as is, RSSI as is); the reporter's doc
  states the constraints.
- **`ResetCounts()` without an error** (chip ignores the provider's
  result, :243-247), answering SUCCESS.
- **`.github/workflows/chiptool.yml`** (outside the brief's paths, one
  token): `TestWorkflowRunsEveryFamily` fails `make ci` unless every
  declared family is in exactly one workflow group; DGWIFI joined `core`
  after DGETH.
- **Disk.** The disk ran full during §5 (the Go build cache held 75 GB);
  I deleted cache entries older than one day (`find ~/.cache/go-build
  -mtime +1 -delete`, ~10 GB). A further trim was refused by the
  permission classifier. The §5 commit was amended after the truncated
  reachability inventory it first picked up was regenerated.

## Left out

- A real Linux reporter (D5: the host's).
- NetworkCommissioning WI (D1).
- `MCORE.COM.WIFI` stays 0: the daemon does not commission or operate
  over Wi-Fi; the cluster is diagnostics only.
- The other golden counts that `--wifi` might move (see below) — only a
  full `-update-chip-cases` run can say.

## Questions

1. Is hand-writing the ep0 slice's DGWIFI block acceptable until the next
   full `-update-chip-cases` run rewrites it from the device? The
   alternative was leaving every family run red on drift.
2. `--wifi` on every harness daemon (rather than for DGWIFI alone) — any
   objection? It is the only shape consistent with one PICS slice per
   endpoint.
3. Should the node state `Ethernet` for its root (the
   EthernetNetworkDiagnostics `[Ethernet]` requirement) now that it states
   `WiFi`? Not done here.

## by_design candidates (not written)

- NetworkCommissioning Ethernet-only on a node that serves
  WiFiNetworkDiagnostics (D1; the owner's entry).
- The reference daemon's simulated Wi-Fi link in the harness
  (`--wifi`, TESTING ONLY), and `MCORE.COM.WIFI=0` beside `DGWIFI.S=1`.

## DGWIFI steps I expect to be sensitive

- **TC-DGWIFI-2.1 step 2** (BSSID, `type: octet_string`): non-null 6
  bytes here; a reporter answering null would hit the YAML runner's
  constraint check on a null value.
- **Steps 3–6** (SecurityType enum8, WiFiVersion 0–6, ChannelNumber,
  RSSI −120..0): non-null fixed values; fine unless the runner checks
  nullable typing differently.
- **2.3**: every step after 1 is behind `PICS_SKIP_SAMPLE_APP` (0), so the
  case executes only WaitForCommissionee — ResetCounts is not exercised
  by the family; it is by `TestParityMatterJS_WiFiNetworkDiagnostics`.
- **The golden counts**: 2.1 7/7 and 2.3 1/7 are predicted; a mismatch
  fails the case until regenerated.
- **Elsewhere**: `--wifi` changes ep0 for every family. TC-IDM-10.4@ep0
  (PICS checker) and any case walking the root's clusters (data-model
  sweep, IDM/ACE wildcard reads, DeviceConformance-style checks) now meet
  0x0036; a Python conformance check could also flag a Wi-Fi diagnostics
  cluster beside NetworkCommissioning without WI, or `MCORE.COM.WIFI=0`
  beside `DGWIFI.S=1`. The event PICS `DGWIFI.S.E00–E02=1` only matter to
  2.2, which is manual.

## Owner's decisions (Fable, 2026-10-14, tranche 19)

1. The hand-written DGWIFI block of `testdata/pics/ep0.txt` stands until
   the next full families run; the golden counts are verified by CI's
   certification job (a narrowed local run cannot update them).
2. `--wifi` on every harness daemon is accepted; `make chiptool-test` and
   the narrowed DGWIFI family ran on the tranche (handover §19).
3. The root states `Ethernet` as well as `WiFi` (owner commit): it always
   carries EthernetNetworkDiagnostics.
4. `.github/workflows/chiptool.yml` (outside the brief's paths) accepted.
5. `by_design`: `BD-Matter-WiFiHost-NetworkCommissioningEthernet` written.
