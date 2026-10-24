# Report CC — M4 phase 21c: the limits page, `node.Config.Limits`, fuzz targets for the controller's decoders

Brief: `notes/briefs/2026-10-24-CC-m4-phase-21c-hardening.md`. Branch
`m4/phase-21c` from `origin/main` `58a13266` (CB, #250). matter.js pin
`862ad75f`.

## Limits

The page is `docs/guides/limits.md`, in the nav after Observability.
The verdict column applies the rule of concept v7 A.4: where matter.js
lets the application configure a value, the host can set it here; where
matter.js keeps a constant, the value stays a constant.

| Limit | Value here | matter.js at `862ad75f` | Verdict |
| --- | --- | --- | --- |
| Sessions per peer | `MaxSessionsPerPeer = 5` | constant `MAX_SESSIONS_PER_PEER`, `SessionManager.ts:185-189` | constant |
| PASE failures | `paseMaxErrors = 20` | constant `PASE_COMMISSIONING_MAX_ERRORS`, `PaseServer.ts:34`, `:106-110` | constant |
| PASE lockout | `paseLockoutCooldown = 15 * time.Minute`, `paseLockoutMaxCooldown = 4 * time.Hour` | none; the refusal ends with the window | module's own, constant |
| PASE pairing timeout | `pasePairingTimeout = 60 * time.Second` | constant `PASE_PAIRING_TIMEOUT`, `PaseServer.ts:33` | constant |
| Unsecured duplicate windows | `maxUnsecuredWindows = 256` | none | module's own, constant |
| Session-miss burst table | threshold 8, window 15 s, cooldown 5 min, 4096 entries | none | module's own, constant |
| Fabrics | `defaultSupportedFabrics = 5`, `maxSupportedFabrics = 254` | state `supportedFabrics`, default 254, `OperationalCredentialsServer.ts:86-89` | configurable: `node.Config.SupportedFabrics` (already) |
| ACL entries per fabric | 4 | state `accessControlEntriesPerFabric`, `AccessControlServer.ts:64-66` | configurable: `node.Limits.AccessControlEntriesPerFabric` |
| Subjects and targets per entry | 4, 4 | state, `AccessControlServer.ts:58-63` | configurable: `node.Limits.SubjectsPerAccessControlEntry`, `TargetsPerAccessControlEntry` |
| Groups per fabric | 22 | state `maxGroupsPerFabric = 22`, `GroupKeyManagementServer.ts:610` | configurable: `node.Limits.MaxGroupsPerFabric` |
| Group key sets per fabric | 20 | state `maxGroupKeysPerFabric = 20`, `GroupKeyManagementServer.ts:609` | configurable: `node.Limits.MaxGroupKeysPerFabric` |
| Commands per InvokeRequest | `DefaultMaxPathsPerInvoke = 10` | state `basicInformation.maxPathsPerInvoke`, `interaction.ts:13`, `BasicInformationServer.ts:73` | configurable: `node.Config.MaxPathsPerInvoke` (already) |
| Subscriptions per fabric | `DefaultMaxSubscriptionsPerFabric = 16` | none (by_design "Subscribe per-fabric quota eviction") | module's own, constant |
| A subscription's MaxInterval | `DefaultMaxIntervalCeilingSeconds = 3600` | `subscriptionOptions.maxInterval`, `NetworkServer.ts:210`, applied in `ServerSubscription.ts:314-344`; ceiling `MAX_INTERVAL_PUBLISHER_LIMIT`, `:71` | configurable: `node.Limits.SubscriptionMaxInterval` |
| TCP connections per peer IP | `MaxConnectionsPerPeerIP = 3` | constant, `TcpTransport.ts:28` | constant |
| TCP in flight per connection | `maxInFlightPerConn = 64` | none; async-iteration backpressure, `TcpChannel.ts:81-82` | module's own, constant |
| MRP retransmissions, backoff | `MaxRetransmissions = 4`, 1.6 / 0.25 / 1.1 | constants, `MRP.ts:33-42` | constant |
| MRP intervals | 500 ms / 300 ms / 4 s | `SessionIntervals.ts:45-49`; the node's own intervals are settable (`SessionsBehavior.ts:116-120`) | constant in this wave (Left out) |
| Duplicate-detection window | `windowSize = 32` | constant `MSG_COUNTER_WINDOW_SIZE`, `MessageReceptionState.ts:14` | constant |
| Controller exchanges per session | `MaxConcurrentExchangesPerSession = 30` | constant, `ExchangeManager.ts:54` | constant |
| Controller subscription ceiling | `SubscriptionMaxIntervalPublisherLimit = 60 * time.Minute` | constant, `ClientInteraction.ts:173` | constant; per request on `SubscribeRequest` |

`TestLimitsPage` (`script/docscheck/limits_test.go`) reads every
`` `Name = value` `` cell in the page's Value column (33 cells). For each
one it finds `Name` in the Go file the same cell links, using go/parser,
and compares the declared value with the cell as `types.ExprString`
prints it. It fails when a value changes, when a constant moves to
another file or is renamed, and when a row has no checked value. A
mutation check (a wrong value, a wrong file) failed as expected. A
docscheck test was chosen over imports in the constant's package because
most of the constants are unexported, and because an import check would
not notice the page naming the wrong file.

## Config.Limits

`node/limits.go`. A zero field takes the default the node had before.
`Config.Validate` (run by `node.New`) refuses a value outside its bound
with `ErrInvalidConfig`, and the error names `Limits.<field>` and the
bound.

| Field | Default | Bound | Plumbed to |
| --- | --- | --- | --- |
| `SubscriptionMaxInterval` | 1 h (`subscription.DefaultMaxIntervalCeilingSeconds`) | whole seconds, 1 s to 1 h | `subscription.Config.MaxIntervalCeilingSeconds`, `node/security.go` `wireSecurity` |
| `AccessControlEntriesPerFabric` | 4 | 4 to 65534 | `AccessControl.SetLimits`, `node/root.go` |
| `SubjectsPerAccessControlEntry` | 4 | 4 to 65534 | same |
| `TargetsPerAccessControlEntry` | 4 | 3 to 65534 | same |
| `MaxGroupsPerFabric` | 22 | uint16 | `GroupKeyMgmtConfig.MaxGroupsPerFabric`, `node/root.go` (the existing field, which also sets `groups.Manager`) |
| `MaxGroupKeysPerFabric` | 20 | 1 to 65535 (non-zero) | `GroupKeyMgmtConfig.MaxGroupKeysPerFabric` |

The bounds are the snapshot's constraints (`access-control.element.ts:38-49`,
`group-key-management.element.ts:43-47`) and `MAX_INTERVAL_PUBLISHER_LIMIT`.

New outside `node`:
- `cluster/core/limits.go`: `AccessControlLimits`, `AccessControl.SetLimits`, and the `Default…` constants as aliases of the unexported ones.
- `im/subscription`: `DefaultMaxSubscriptionsPerFabric`, `DefaultMaxIntervalCeilingSeconds` and `DefaultTickInterval`, used by `NewManager`.

**Owner-path lines.** All are in `cluster/core/access_control.go`, and each
replaces a constant's read with the configured value:
- `:84-86`, the struct field `limits AccessControlLimits` and its comment;
- `:471`, `:473`, `:475`, the three attribute reads (`a.subjectsPerEntry()`, `a.targetsPerEntry()`, `a.entriesPerFabric()`);
- `:716-717`, `:720-721`, `:723-724`, the three write checks and their error texts.

`group_key_management.go` is unchanged: its existing `GroupKeyMgmtConfig`
fields carry the values.

Tests:
- `node/limits_test.go`:
  - `TestLimitsAttributes` is one table over the five capacity fields. It reads the root attributes over PASE for a zero and a configured `Limits`: zero gives the default constants, configured gives the set values.
  - `TestLimitsSubscriptionMaxInterval` asks for a 2 h MaxInterval over a fabrictest commissioned pair: zero grants 1 h, 30 s grants 30 s.
  - `TestLimitsValidate` covers each field outside its bound.
- `cluster/core/limits_test.go`: an ACL write is refused with `ResourceExhausted` at N+1 under a set limit, and the defaults equal matter.js's.

## Fuzz

Each target has the invariant stated in its comment, skips inputs above
64 KiB, and has no findings on owner paths. The new targets in the second
`make fuzz` run, at `FUZZTIME=1000000x`, one target at a time, wall time
including the build, HEAD `64670ce4`:

| Target | Package | Invariant beyond "no panic" | 1000000x |
| --- | --- | --- | --- |
| `FuzzDecodeReport` (+ `im.UnmarshalSubscribeResponseTLV`) | client/interaction | reports + events ≤ input length; seeds: the ReportData/SubscribeResponse fixtures of `im/testdata/im-wire-fixtures.json` | 291 s |
| `FuzzParseTXTData` | client/discovery | SII, SAI ≤ 1 h; SAT 1..65535 ms; ICD ≤ 1; PH ≠ 0 | 48 s |
| `FuzzParseTxtDecimal` | client/discovery | ok ⇒ v ≤ limit and the value is v's decimal form | 66 s |
| `FuzzDecodeSupportedTransports` | client/discovery | only bits 1 and 2 count | 62 s |
| `FuzzParseDNSMessage` | mdns | records + questions ≤ input length | 55 s |
| `FuzzDecodeStatusReport` | client/exchange | ok ⇔ length ≥ 8; `Encode` round-trips byte-exact | 53 s |
| `FuzzParseRevocationSet` | controller/dcl | entries + serials ≤ input length | 384 s |
| `FuzzParseCRL` | controller/dcl | with and without an authority name; seed from `x509.CreateRevocationList` | 180 s |
| `FuzzDecodeCDContent` | controller/attestation (harness only) | the schema bounds (1..100 PIDs, certificate id 19 units, ≤ 10 PAAs) | 45 s |
| `FuzzDecodeAttestationElements` | controller/attestation (harness only) | — | 44 s |
| `FuzzParseAttCert` | controller/attestation (harness only) | both `authorityKeyID` values | 38 s |
| `FuzzDecodeCertificateChainResponse` | controller/commissioner (harness only) | certificate ≤ 600 bytes | 550 s |
| `FuzzDecodeNOCResponse` | controller/commissioner (harness only) | FabricIndex 1..254, DebugText ≤ 128 | 301 s |
| `FuzzDecodeCertSigningRequest` | controller/commissioner (harness only) | — | 346 s |
| `FuzzDecodeNetworks` | controller/commissioner (harness only) | entries ≤ input length | 362 s |
| `FuzzReadHeader` (+ `DecodeHeader`) | bdx/otaimage | an accepted header re-encodes to itself; header bytes ≤ input | 35 s |

`make fuzz` at the default `FUZZTIME` (100000x), all 40 targets: 603 s, exit 0.

**Finding (fixed, Opus path `bdx`).** `bdx/otaimage.ReadHeader`
allocated `make([]byte, p.HeaderSize)` from the untrusted 16-byte prefix
before reading. A 16-byte input claiming `HeaderSize = 0xFFFFFFFF`
allocated 4 GiB (measured through `runtime.MemStats`) and then failed
with "OTA file ended unexpectedly".
- Reproducer: LE uint32 `0x1BEEF11E`, LE uint64 `1<<40`, LE uint32 `0xFFFFFFFF`, nothing after (`hugeHeaderPrefix()` in `bdx/otaimage/fuzz_header_test.go`, also a seed).
- Fix (`otaimage.go:273-280`): `io.ReadAll(io.LimitReader(r, HeaderSize))` plus a length check, so the buffer grows only with the bytes that arrive. This mirrors matter.js `#processHeader`, which appends chunks until enough have arrived (`packages/protocol/src/ota/OtaImageReader.ts:157-191`).
- Regression test: `TestReadHeaderClaimedSizeIsNotAllocated`.
- CHANGELOG: one `### Fixed` line.

No targets were added in `client/handshake`, `secure/sigma` or
`secure/spake2`; those are the owner's.

## Guards

Summary log: one exit line per guard, HEAD per guard.

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` (build, build-32bit, vet, archcheck, apidiff-check, test, lint, cover-check, bluez-check) | `64670ce4` | exit 0 (1284 s) |
| `make race` | `64670ce4` | exit 0 on the re-run; the first run exited 2 because the link and SQLite steps hit "disk quota exceeded" in the build temp directory (no test failure, no DATA RACE) |
| `make fuzz` (default `FUZZTIME`) | `64670ce4` | exit 0 (603 s) |
| `make fuzz`, new targets at `FUZZTIME=1000000x` | `64670ce4` | 16 × exit 0 (table above) |
| `make archcheck` | `64670ce4` | exit 0 |
| `make reachability`, then `make reachability-check` | `64670ce4` (snapshot committed in it) | exit 0, exit 0 |
| `make cite-check` (`MATTERJS_DIR` set) | `64670ce4` (artefacts committed in it; drift 262, unchanged) | exit 0 |
| `make testparity-check` | `64670ce4` | exit 0 |
| `go test ./script/...` | `64670ce4` | exit 0 |
| `mkdocs build --strict` | `64670ce4` | exit 0 |

`make race` re-run at `64670ce4`, after the build temp directory was cleared: exit 0 (397 s), no DATA RACE.

`make race` ran once because §2 adds no lock. `SetLimits` is called once,
before the node serves.

## Differences

- Subscriptions per fabric is not a `Limits` field. matter.js has no quota, so by the A.4 rule it stays a constant. The brief's example test (the quota refused at N+1) is replaced by the MaxInterval and ACL tests.
- The attribute tests read over a bridgetest PASE session, the pattern of `node/knobs_test.go`, where the brief named fabrictest. The MaxInterval test uses fabrictest.
- `decodeReport` is at `client/interaction/report.go:450`, where the read map said `:427`.
- Four new comments first counted as citation drift: the sentence named the PascalCase attribute, while matter.js spells it in camelCase. They were rephrased, and drift is back at 262.

## Decisions

- `SupportedFabrics` and `MaxPathsPerInvoke` stay at the top level of `node.Config`, so a patch release breaks nothing. The page and the `Limits` doc say so.
- `SubscriptionMaxInterval` is a `time.Duration` in whole seconds, as the facade's other durations are. The engine keeps its `uint16` seconds.
- The defaults are exported constants below the facade (`mattercore.Default…`, `subscription.Default…`), so the zero-value test and the page compare with a name rather than a literal.
- The page check lives in `script/docscheck`, beside the other documentation guards.
- go-fabric counts the IPK's implicit key set 0 once against `MaxGroupKeysPerFabric` (`enforceKeySetBudget`, `group_key_management.go:815-840`: written non-zero key sets + 1 ≥ cap). matter.js `keySetWrite` does the same (`GroupKeyManagementServer.ts:379-380`). This was read only; the field doc and the page say it.

## Left out

- The node's own MRP session intervals (idle, active, threshold) stay constants. matter.js lets them be set (`SessionsBehavior.ts:116-120`, applied in `ServerNetworkRuntime.ts:357-363`). They reach the mDNS records, the PASE parameters (`bridge/handlers.go` `DefaultSessionParameters`) and CASE (`secure/operational/manager.go:403-407`, owner path). The owner decided: constants in this wave.
- matter.js `subscriptionOptions.minInterval` and `randomizationWindow`. go-fabric's `MinIntervalFloorSeconds` lifts the subscriber's floor, which matter.js's `minInterval` does not. Randomization is absent by design (`by_design.md` L10-D09).
- Groupcast's `MaxMembershipCount = 44` (`cluster/core/groupcast.go:52`) is not on the page and not configurable. matter.js derives it from GroupKeyManagement's maxGroupsPerFabric × 2 (`GroupcastServer.ts:928`).
- Fuzz targets in `client/handshake`, `secure/sigma` and `secure/spake2` (owner).
- No chip-tool run: neither the composition nor the FeatureMap moved, and the defaults equal today's constants.

## Questions

- The stale citation in `cluster/core/group_key_management.go:116-117` (`GroupKeyManagementServer.ts:615-616`; the lines are `:609-610` at the pin) is the owner's to fix, as announced.
- Should Groupcast's `MaxMembershipCount` follow `Limits.MaxGroupsPerFabric` (2 ×, as matter.js aligns them)? Today a `MaxGroupsPerFabric` above 22 is held to Groupcast's per-fabric quota of 22 for a Groupcast membership.

## by_design candidates

- **The publisher's default MaxInterval.** go-fabric grants up to one hour by default (`subscription.DefaultMaxIntervalCeilingSeconds`, chip's `kSubscriptionMaxIntervalPublisherLimit`). matter.js defaults `subscriptionOptions.maxInterval` to three minutes (`INTERNAL_INTERVAL_PUBLISHER_LIMIT`, `packages/node/src/node/server/ServerSubscription.ts:72`, reasoned at `:61-70`) under the same one-hour cap (`:71`). The hour is what the bridge has always advertised to Apple, Google and chip-tool, and `Limits.SubscriptionMaxInterval` gives a host the matter.js value. L10-D10 covers the clamp but not the default.
- **MRP constant names.** `transport/mrp.MRPBackoffThreshold = 1.6` is matter.js's `MRP.BACKOFF_BASE` (`packages/protocol/src/protocol/MRP.ts:36`). matter.js's `BACKOFF_THRESHOLD = 1` (`:45`) has no named counterpart here, and `MRPBackoffBase = 300 ms` is the active-interval fallback. The behaviour is the same and only the names differ. A rename is a public-API change, so it waits for a removal wave.
