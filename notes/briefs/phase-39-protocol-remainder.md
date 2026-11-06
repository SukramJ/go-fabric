# Brief phase 39 — the protocol remainder (M10)

Roadmap milestone 10, row 39 (`docs/roadmap.md`): "the protocol remainder,
consumer-driven: the OTA requestor over HTTPS, the BTP renegotiation after
a stall, the trusted-time-source client, the Fabric Sync remainder; ARL and
NFC stay non-goals". The read map
(`~/.cache/go-fabric-scratch/read-phase39.md`, Sonnet 2026-11-06; the
load-bearing lines re-read by Fable at the pins) found no consumer for any
of the four; the owner decided on 2026-11-06 (§1) to build all four, Fabric
Sync cut to the datastore's `SyncNode` overload. Two are matter.js parity,
two are ahead of matter.js with connectedhomeip as the authority:

- **OTA requestor over HTTPS — parity.** matter.js's requestor downloads
  over HTTPS (`OtaSoftwareUpdateRequestorServer.ts:1008-1062`
  `#handleHttpsDownload`: `fetch(uri)`, a non-ok status is an
  `OtaDownloadError`, the body streams into the download location with
  progress; `:1072-1115` `#handleDownload` routes `bdx://` and `https://`
  by the announced `transferProtocolsSupported`, refuses the rest) and
  announces BDX only by default (`:1362-1372`, "HTTPS can be added as
  protocol as well"). chip has no HTTPS (`DefaultOTARequestor.cpp:747`
  `kProtocolsSupported[] = { kBDXSynchronous }`). Here: BDX only
  (`cluster/core/ota_requestor_query.go:423`, `:514-519` refuses a non-bdx
  URI; `ota_software_update_requestor.go:48-50` states the gap;
  `by_design.md:2318-2321` records the absence as a non-divergence). The
  module's own provider already offers `HTTPSURI` (`cluster/core/ota_provider.go:427-440`).
- **BTP renegotiation after a stall — parity.** matter.js's central
  channel re-handshakes with the minimum segment size when a peripheral
  completes the handshake and then sends no BTP packet before the ack
  timeout (`packages/nodejs-ble/src/NobleBleChannel.ts:835` subscribes
  `stalledAfterHandshake`, `:864-889` `#startRenegotiation` at most once,
  every send awaits it, `:891-918` `#renegotiate`: unsubscribe C2,
  handshake with `MINIMUM_ATT_MTU`, adopt the session, replay the
  unacknowledged messages; `packages/protocol/src/ble/BtpSessionHandler.ts:50-84`
  the stall observable — "an interop workaround with no basis in the
  specification"). chip closes after the 15 s ack timeout
  (`src/ble/BLEEndPoint.cpp:1454-1464`). Here the session half exists
  (`transport/btp/session.go:282-291` `OnStall`, `:559-566` `Suspend`,
  `:644-680`); nothing calls `OnStall` — `controller/ble/channel.go`
  holds one session and has no re-handshake (`:53-62`, `:98-143` `Dial`,
  `:149-175` handshake).
- **Trusted-time-source client — ahead.** chip's node reads UTCTime and
  Granularity from the trusted node's root over CASE and sets its time
  (`src/app/clusters/time-synchronization-server/TimeSynchronizationCluster.cpp:525-539`
  `AttemptToGetTimeFromTrustedNode`, `:401-428` the read, `:436-460`
  the decode, `:462-492` `OnDone` with the conservative granularity map,
  `:379-399` the NTP fallback and `TimeFailure`, `:494-566` the triggers:
  server ready without granularity, a new trusted source); matter.js's
  server is the empty subclass (`TimeSynchronizationServer.ts:14`). Here
  the source is stored, reported and announced (`time_synchronization_features.go:412-462`),
  UTC stays the host clock (`time_synchronization.go:75-79`), `TimeSource`
  is not served and `TimeFailure` never emitted.
- **Fabric Sync remainder — ahead, cut to `SyncNode`.** The only remainder
  the repository names is the datastore's `SyncNode(nodeId, endpointId,
  bindings)` overload (`docs/guides/joint-fabric.md:142-146`); the
  fabric-sync admin role (chip `examples/fabric-sync/admin/`) stays out
  (no consumer, no matter.js model; MCORE_FS stays class (c)).

ARL (`BD-Matter-ARL-NotMounted`) and NFC (matter.js has none) stay
non-goals.

Pins: matter.js `862ad75f` (read only with
`git -C ../matter.js show 862ad75f:<path>`), connectedhomeip `6170af84`
(read only with `git -C ../connectedhomeip show 6170af84:<path>`; the
working tree is at another commit).

## 0 Working rules

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp` (Linux; on
macOS the Go tools from `make setup`). **One worktree per agent** on the
shared branch (`git worktree add ../go-fabric-p39-<wave> m10/phase-39`;
phase 38's lesson: several agents in one worktree share the index, and a
commit by path swept other agents' hunks). Commits as
`SukramJ <sukramj@icloud.com>` (`git var GIT_AUTHOR_IDENT` first),
`<area>: <what> (phase 39)`, no attribution lines; stage named paths
(`git add <new files>` then `git commit -- <paths>`); never
`git add -A`, never `git reset --hard`, never force-push, never rebase;
`git pull --rebase=false origin m10/phase-39` before every push, push
regularly. Shared files (`notes/parity/by_design.md`, `CHANGELOG.md`,
`docs/matterjs-comparison.md`) are touched only by adding one's own
entries next to the related existing entry, never at the end of the file.
Regenerated artefacts (`script/citecheck/*`, `script/reachability/*`,
`script/surfacecheck/*`, `script/testparity/*`) are regenerated, never
hand-edited, and committed with the change that moved them (a new
citation: `make cite-report && make reachability` in the same commit; a
surface change: `make surface`). The cite checker takes the last
`at <sha>` of a *sentence*: never mix matter.js and chip citations in one
sentence; bare basenames resolve only when unique in the pin.

Guards, no chip-tool harness needed for any wave (CI runs it on the pull
request): `make lint` (if golangci-lint times out under load, rerun with
`--timeout 20m` on the touched packages and say so), `go test -race` on
the touched packages, `make cover-check` read for the packages touched
(a flake in the cover run hides the floor table — rerun `go test -cover`
per package), `make reachability-check`, `make cite-check`,
`make apidiff-check` (additive API only; a break is announced in
`CHANGELOG.md` and the migration guide), `make ahead-check`,
`make surface-check`, `make testparity`. Security paths (`secure/`,
`commissioning/`, ACL, the four hand-written `cluster/core` servers) are
not in this phase; `cluster/core`'s OTA requestor and TimeSynchronization
are generated-definition servers with hand-written logic — Opus work
with Fable's line review of every cited fix line before the pull request
merges. Facade changes (`contract/`, `node/`, `controller/`) are judged
as API design: one shape, the existing port precedents
(`OtaProviderConnector`, `cluster/core/ota_software_update_requestor.go:153-157`;
`CommissionerReach`, `cluster/jointfabric/commissionee.go:36`). Production
code never calls `time.Sleep`/`time.After`; timers run on the component's
clock; tests wait with `context.WithTimeout(t.Context(), …)`. Official
text in English.

## 1 Owner decisions 2026-11-06 (wizard)

1. **All four, Fabric Sync cut to the `SyncNode` overload** (Fable
   recommended the parity pair plus `SyncNode`, the trusted-time client as
   a comparison row; the owner chose all four; recorded).
2. **The node's UTC becomes a served source: an offset over the host
   clock**, set by `SetUTCTime`, the trusted-time client and the NTP
   fallback alike (chip `:856-872` sets the system clock; here the host
   clock is never touched). This closes the owner decision of 2026-10-28
   ("SetUTCTime answers chip's statuses without setting the clock",
   `by_design.md:5838-5842`): rewrite that entry.
3. **Execution on the second machine (macOS, no chip-tool harness)**: one
   handover issue per wave references this brief; the agents run there in
   separate worktrees on `m10/phase-39`; the guards, the pull request and
   the review run on the Linux machine.

Owner decisions 2026-11-06, second wizard (after the four reports):

4. **OTA over HTTPS**: the size cap stays (256 MiB); the total timeout
   becomes an idle timeout between two received bytes (60 s) — a total
   timeout couples image size to link speed; a redirect to `http://` is
   refused by the module's default client (the brief refuses `http://`
   URIs, the image validation checks no signature), a host client keeps its
   own policy. Both recorded in `BD-Matter-OTARequestor-HTTPS`.
5. **`SyncNode` with an endpoint**: a required method of `DatastoreSync`
   (`SyncEndpointBindings`), an announced API break with the migration
   guide — chosen over the recommended optional interface
   (`EndpointBindingSync`) so every host writes the merged list; the
   reference bridge implements it, the JFDS families exercise it on the
   Linux machine.
6. **TimeSynchronization**: `PlatformTime` stays as the third config field
   (chip's delegate order); the device kit's MatterTime follows the root's
   UTC offset in a follow-up wave (one Matter time per node).
7. **Follow-ups as issues, not this phase**: `ble/bluez` implements
   `MTUExchange` (esphome checked); the live multicast test selects its
   node by discriminator; `node/commissioner_reach.go` sets the path flags.

Decisions the brief takes itself (on record for the review):

- **HTTPS is opt-in**, as matter.js's default list is BDX only:
  `OTARequestorConfig.TransferProtocols []DownloadProtocol` (zero = BDX
  only; the query announces the list, `ota_requestor_query.go:423`).
  Only `https://` and `bdx://` URIs are accepted (matter.js `:1094`
  refuses `http://`). An `HTTPClient` port (`Do(*http.Request)` —
  `*http.Client` satisfies it) so a host can restrict egress and tests use
  `httptest`; nil is `http.DefaultClient` with a request timeout. Two
  recorded deviations from matter.js's `fetch`: a total size cap (the
  provider's image bound, cf. `BD-Matter-OtaImageHeaderSizeCap`,
  `by_design.md:5442`) and the timeout; redirects are followed as `fetch`
  follows them.
- **The BTP renegotiation lives in `controller/ble/channel.go`** as
  matter.js's channel has it: at most once per channel, every send waits
  for it, a failure closes the channel; the session is swapped behind
  `btp.Conn`. The ATT_MTU settle wait of `NobleBleChannel.ts:40-41,
  :520-560` (up to 2 s for the MTU exchange) goes with it. The proof is
  the `bletest` fake peripheral that withholds packets after the
  handshake, and matter.js's stall tests ported by meaning (list them
  under `packages/protocol/test/ble` and `packages/nodejs-ble/test` at
  the pin first).
- **The trusted-time client reads through a read-capable peer port**
  (`TimePeerReader`: open a CASE session to a node of a fabric and read
  attributes — the node facade implements it over the client stack as
  `CommissionerReach` is implemented), runs chip's state machine on the
  server's clock, serves `TimeSource` and emits `TimeFailure`, maps the
  granularity as chip does, and takes the NTP fallback from a host port
  (`NTPClient`; nil behaves as chip's default delegate: not implemented →
  `TimeFailure`).
- **`SyncNode` with an endpoint and bindings** follows chip's datastore
  (`joint-fabric-datastore-server` at the pin) and extends the existing
  JFDS parity tests.

## 2 Deliverables

### 2.1 OTA requestor over HTTPS (wave A) — `cluster/core/ota_software_update_requestor.go`, `ota_requestor_query.go`, `node/ota.go`, docs

- `TransferProtocols` in the requestor config and the node's OTA config
  (`node/ota.go:221` `buildOtaRequestor`); the announced list; the
  `https://` branch of `handleDownload` (`:514`): GET through the
  `HTTPClient`, a non-2xx status is `otaDownloadError` with the status
  (matter.js `:1013-1020`), the body streams through the same
  `bdx.FileSink` temp file with `updateProgress` from `Content-Length`
  (`:1022-1040`), then the existing validation
  (`validateUpdateFile`, matter.js `:1124`); the size cap and the timeout
  end the download as a failed attempt.
- Tests: `httptest` server (success with and without `Content-Length`,
  404, a truncated body, the cap, the timeout, `http://` refused, HTTPS
  not announced → the provider's BDX offer still works); the parity case
  is matter.js's code (no test exists at the pin — say so in the register
  entry).
- Register: an ordinary `by_design` entry `BD-Matter-OTARequestor-HTTPS`
  (opt-in as matter.js, the cap and the timeout as deviations, no
  redirect policy beyond `fetch`'s); `BD-Matter-OTARequestor-OnSpecInstance`
  loses its "absence of HTTPS" sentence; comparison row
  `docs/matterjs-comparison.md:127`; CHANGELOG Added.

### 2.2 BTP renegotiation after a stall (wave B) — `controller/ble/channel.go`, `controller/ble/central.go`, `transport/btp` (only if the session half needs a hook), `ble/bletest`

- `Channel` keeps a replaceable session behind `btp.Conn`; `OnStall`
  starts the renegotiation once (`NobleBleChannel.ts:864-889`):
  detach the C2 handler, `UnsubscribeC2`, handshake with
  `btp.MinimumATTMTU` (`:891-918`), adopt the session, replay the
  messages in order, release the sends that waited; a failure or a lost
  connection closes the channel (`:920-926`). `Dial` waits up to 2 s for
  the ATT_MTU exchange before it sizes the segment (`:40-41`, `:520-560`).
- Tests: the `bletest` fake peripheral withholds every BTP packet after
  the handshake when the segment is above the minimum, answers at the
  minimum; the replay arrives in order; a second stall closes; matter.js's
  stall tests ported by meaning (`*_parity_matterjs_test.go` in
  `controller/ble`, register entries in `script/testparity`).
- Register: an ordinary `by_design` entry
  `BD-Matter-BTP-RenegotiationAfterStall` (an interop workaround matter.js
  documents as without spec basis; chip closes); comparison row
  `docs/matterjs-comparison.md:56` loses its "not ported" sentence;
  CHANGELOG Added. No chip-tool leg exists (no BTP family); say so.

### 2.3 Trusted-time-source client (wave C) — `cluster/core/time_synchronization*.go`, `node/`, `contract/` (ports), docs

- **The UTC source** (decision 2): the server holds an offset over its
  clock with the granularity and `TimeSource`; `UTC()` returns clock +
  offset (GeneralDiagnostics' TimeSnapshot follows); `SetUTCTime` sets it
  with chip's checks (`:856-872`; the statuses of 2026-10-28 stay), the
  client and the NTP fallback set it too; persisted? — chip does not
  persist the time (the clock does); here the offset is in memory, lost
  at restart as a real clock would not be: record the difference.
- **The client** (`:525-539`, `:401-492`): on start without a granularity
  and whenever a trusted source is set (`:494-508`, `:556-593`), open the
  read through `TimePeerReader` to the source's node and fabric, read
  root UTCTime + Granularity, apply chip's conservative granularity map
  (`:462-492`), `TimeSource` = NodeTimeCluster (`:479`); on failure the
  NTP fallback (`:379-399`) through `NTPClient` (nil → `TimeFailure`,
  `:385-398`), success → Granularity Milliseconds, `TimeSource`
  NonMatterSNTP (`:510-522`). Timers on the server's clock; no
  `time.Sleep`.
- **Ports**: `TimePeerReader` in `cluster/core` (interface; the node
  implements it over the client stack as it implements
  `CommissionerReach`, `node/jointfabric.go`), `NTPClient` in
  `node.Config.TimeSynchronization.Config` (host-supplied). The facade's
  `TimeSynchronizationConfig` doc says what the node's UTC is now.
- Tests: chip's `tests/TestTimeSynchronizationCluster.cpp` cases ported by
  meaning where they touch the client; a fabrictest pair where the
  trusted node serves a UTCTime and the client adopts it (and the
  fallback paths); `TimeFailure` emitted; TIMESYNC/2.13 stays the
  command-side case.
- Register: extend `BD-chip-TimeSynchronization-Features`
  (`by_design.md:117`, body `:5825-5848`): the client ported, the UTC
  source; rewrite the 2026-10-28 entry (`:5838-5842`) to decision 2;
  comparison row `:135`; CHANGELOG Added + Changed (the node's UTC moves
  with SetUTCTime — user-visible); `node/surface.go` row unchanged (TSC
  already in the AheadSet).

### 2.4 `SyncNode` with an endpoint and bindings (wave D) — `cluster/jointfabric`, `cluster/core` datastore, docs/guides/joint-fabric.md

- The datastore's `SyncNode(nodeId, endpointId, bindings)` overload after
  chip's `joint-fabric-datastore-server` at the pin (read the command's
  handling of an endpoint and its binding list; cite the lines); the
  JFDS parity tests extended; `docs/guides/joint-fabric.md:142-146`
  loses the "left" sentence; register: extend the existing JFDS row;
  CHANGELOG Added. JFDS/2.x goldens move only if a case now runs — the
  harness run is the Linux machine's (say which case).

### 2.5 Docs, roadmap, CHANGELOG (the closing agent, once)

`docs/roadmap.md` row 39 status paragraph; `docs/matterjs-comparison.md`
rows 56, 127, 135 and the ARL/NFC non-goal lines; `docs/certifiability.md`
if a family count moved; the CHANGELOG lines above.

## 3 Report shape

Per §: what landed (files), coverage before/after for the packages
touched, guards with HEAD and exit, differences to matter.js or chip
recorded (entry ids), decisions taken that the brief left open, left out
and why, questions for the owner. Short.
