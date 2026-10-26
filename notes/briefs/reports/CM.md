# Report CM — milestone 5 phase 25 (v0.13.0)

Branch `m5/phase-25` from `dbf93f30` (`v0.12.4`). Brief
`notes/briefs/2026-10-25-CM-phase-25.md`, read map
`notes/reads/read-cm-phase-25.md` (commit `3dcda126`). matter.js pin
`862ad75f`; every ported rule cites its line.

## Per section

### §1 OTA push from the controller — `bc199cc0`, `9059c17f`, `cf845714`

- `controller/softwareupdate`: `Manager` after `SoftwareUpdateManager.ts`
  (consents, the update queue, `AddUpdateConsent`, `ForceUpdate`,
  `RemoveConsent`, `QueryUpdates`, `CheckForUpdates`, `QueuedUpdates`,
  `TransferOptionsFor`, `SuppressNextStartUp`, `OnStatusChange`, the stalled
  detection by BDX progress and apply delay, the failed-apply detection on
  startUp and after the resubscription, the test-image cleanup), the
  announcements after `OtaAnnouncements.ts` (DefaultOTAProviders write,
  AnnounceOTAProvider), the `Images` store after `DclOtaUpdateService`
  (modes prod/test/local, the ledgers).
- The provider is node/'s one (`node.OTAProviderConfig`, now with
  `RebootQuery` and `TransferBlockSize`; `node.Node.OTAProviderEndpoint`,
  `BDX`). `controller.Config.OTA` runs the manager; the controller starts
  its own node through `OTAConfig.StartNode` (required) from an
  `OwnNodeSpec`, commissions it into its fabric on first use and keeps it in
  its database (`Controller.OwnNode`). `node/controllernode` (facade layer)
  is the starter (`Start`, `Config`, `Wrap`, `Node`).
- The reference controller's `ota serve` runs on the manager; the own
  node's record moves from `controller.self` to `controller.ownnode`.
- Proof (§1.4): `examples/reference-bridge:TestOTAPushToTheReferenceDaemon`
  (the reference daemon's composition updated over the air on a fabrictest
  site) and `controller:TestOTAPushFromTheController`.
- Tests: OtaTest's manager cases (`controller/softwareupdate:TestManager*`,
  `controller:TestOTA*Parity`), DclOtaUpdateServiceTest's store cases
  (`TestImages*`).

### §2 The TCP initiator — `dc965a24`

Transport resolution (`client/peer.ResolveTransports`), TCP sessions bound
to their connection (`client/exchange.AttachChannel`), large messages over
TCP, `controller.Config.TCP` / `TransportPreference`; `ClientNodeTcpTest`
(22, loopback), `LargeMessageTransportTest`, `InvokeLargeMessageTest`,
`PeerConnectionSocketOwnershipTest` ported.

### §3 Network profiles — `4db2ee8d`

`client/peer.NetworkProfiles` (templates, per-medium selection, connection,
exchange and probe throttling), the MRP margins (`client/exchange`), the
`icdLit` routing of an await-mode LIT ICD, `controller.Config.Networks` /
`OwnNetworkProfile`; `NetworkProfileTest`, `MRPTest`,
`ClientNodeInteractionIcdTest` ported. **Break:** `Priority bool` →
`Network string`.

### §4 The commissioner advertisement — `863fa76e`

`mdns.BuildCommissionerService` (`_matterd._udp`), `controller.Config.Advertise`
/ `Advertiser`; MdnsTest's commissioner cases ported.

### §5 The parked register items — `5790d12f`, `8187cc7e`, `6f14b723`

| Item | Outcome |
| --- | --- |
| `ClientInteraction.close`, in-flight, abandoned subscriptions | `Interaction.Close`, `CloseSubscriptions`, `ErrClosed`; the in-flight set; the abandoned establishment answers the priming report `INVALID_SUBSCRIPTION` (ClientInteractionSubscribeTest 2 of 7 and one case of its own, ClientSubscribeTest 2 of 5) |
| multi-command invoke | `InvokeBatch` cases of ClientInteractionInvokeTest (4); the batcher of single commands across a tick is not ported (question 1) |
| write chunking | `Write` chunks across messages after `sendWriteCommand` (InteractionClientMessengerTest 8 cases, ClientChunkedWriteTest 6 of 6 on fabrictest) |
| device MaxPathsPerInvoke | already `node.Config.MaxPathsPerInvoke`; the register's "fixed 10" was stale |
| `ForgetCommissioned` | `commissioner.Context.Scanners` (`CommissionedScanner`); the controller's BLE scanner is per pairing, so the controller passes none |
| `threadNetworkNameOf` | the dataset decoded whole (`controller/thread.DecodeDataset`) |
| `Node.Disable` | closes every subscription of the node |
| `includeKnownVersions` | `client.SubscribeRequest` struct (**Break**), two ClientSubscribeTest cases |
| priming report after cancel | see the first row |
| `Cluster.Supports`, `Cluster.Revision` | conformance under the peer's features and revision (`spec.Conformance.ApplicabilityDeprecatedOptional`); the model's revision; PeerBehaviorTest 5 of 12 |
| Registry `AutoStart` | decision: unchanged (below) |
| a node that fails to load | kept, unavailable (`Node.LoadError`, `ErrLoadFailed`, `peer.ErrNoFabric`); PeersLoadFailureTest ported |
| fabrictest group messages | the multicast model of 18c already carries them; PeersCommissionedTest ported (`Commissioned` leaves out group node ids) |
| test ports | CommissioningIdentityTest ported (`discovery.MismatchOf`, the payload identity check in `Pair`); SplitCommissioningTest 3 of 6 (`Registry.CompleteCommissioning`); the four discovery files and ReportedSessionParametersTest → `M6 phase 27` |

Planned ratchet 17 → 12.

### §6 `MarkComplete` goes — `a9f919ae`

**Break:** `Aggregator/Partition/Batch.MarkComplete`,
`endpoint.Snapshot.ModelComplete` and `Retain` removed; stored endpoint
numbers are kept until `Forget` or `RetirePartition`.

### §7 Docs, guards, report — `73f71e02`

Roadmap status, comparison rows (OTA push, TCP initiator, network profiles,
commissioner advertisement) ✅, the controller guide sections, ports,
advanced seams, the manual test plan rows C7a and C13, the reference
controller README, CHANGELOG (Breaks first), migration guide.

## Guards

GUARDS

## Differences from the brief

- The layering guard (`script/archcheck`, ADR 0026 principle 1) forbids
  `controller → node`: the own node is composed in the new facade package
  `node/controllernode` and reaches the controller through the
  `controller.OwnNode` port; `OTAConfig.StartNode` is therefore required.
- `ota serve`: `--version`, `--vendor`, `--product` must match the image
  header, which the manager offers the image by
  (`DclOtaUpdateService.ts:534-536`).
- The tests the brief names for §6 (`TestAssemble_GCSkipsCentral…`,
  `…GCOfVanished…`, `…EndpointNumbersReservedUntilExplicitRemoval`) exist
  only in `by_design.md`; the replacements are
  `TestPartitionsKeepTheirStoredNumbers`, `TestBatchCollectsNothing`,
  `TestStoredNumbersSurviveAbsence`.
- §2: the TCP session-parameter check runs after CASE
  (`PeerConnection.ts:596-621` validates before), `client/handshake` being
  the owner's.
- §5: fabrictest needed no group-message work; the finding was stale.
- §2/§3 lint findings were fixed in the §1/§5 commits.
- A peer of unknown medium now takes the conservative limits (4 exchanges,
  100 ms, 1.5 s margin) — matter.js's default, a behaviour change.
- `chiptool-test` skipped: the reference bridge's composition and
  FeatureMap did not move.
- `TestIcdClientParity` timed out twice in full package runs and passed
  alone each time (known load flake).

## Decisions

- **§1.3 provider composition.** A controller-owned node (`node.Node`,
  node/'s provider) in the controller's database with a node id of its
  own, commissioned by the controller into its fabric; composed in the
  facade layer (`node/controllernode`) because the controller role may not
  import the facade. matter.js's provider node id is the controller's root
  node id (the controller is the ServerNode).
- **Registry `AutoStart`.** Unchanged: `client.RegistryConfig.AutoStart`
  stays opt-in at the seam, and `controller.New` sets it unless
  `Config.ManualStart`, so the controller's default is matter.js's
  `autoStartCommissionedPeers = true` (`NetworkServer.ts:236`). Changing the
  seam's zero value would break every registry composer for no facade
  gain; the doc comment says so.

## Left out

| What | `until` / state |
| --- | --- |
| ContinuousDiscoveryTest, DiscoveryCancelTest, DiscoveryErrorTest, DiscoveryTransportsTest | `M6 phase 27`: matter.js's controller Discovery (ScannerSet, parallel PASE, cancellation, error kinds, transport selection) has no Go shape |
| ReportedSessionParametersTest | `M6 phase 27`: no device-side session-interval option nor accessor |
| OtaTest: the armer through a real device restart (5), the BDX liveness cases (6) | `missing` with reasons |
| ClientInvokeTest: the batcher of single commands (39) | `missing`, question 1 |
| ClientInteractionSubscribeTest: the BDX cases (3), request reuse, failing close | `missing` |
| SplitCommissioningTest: non-Ok errorCode, status cause, persistence failure | `missing` |
| DclOtaUpdateServiceTest: the store remainder (36) | `missing` |
| `bdxAdditionalMrpDelay` on the device-side BDX | not applied (question 4) |
| The Thread border-router client | milestone 6 phase 26a (ADR 0027) |

## Questions

1. **The invoke batcher.** matter.js joins the single invokes of one
   timer tick into one InvokeRequest (`ClientInteraction.ts:715-1000`);
   a goroutine has no tick to join on. Port it — with which window and
   default (off, or a short `InvokeOptions.BatchDuration`) — or keep
   `Node.InvokeBatch` as the one multi-command form and classify the 39
   ClientInvokeTest cases? Symbol: `client/interaction.Interaction.Invoke`.
2. **largeMessage inference.** `inferLargeMessage` reads the schema's
   "L" quality, which the snapshot extractor does not emit. Extend
   `script/extract-from-matter-js.ts` and `schema/` with it (a snapshot
   refresh, the owner's review)? Symbol: `schema` command/attribute
   qualities, `client/interaction.ClientRequest.LargeMessage`.
3. **The own node in the facade layer.** The layering forbids
   `controller → node`; the port (`controller.OwnNode`, `OTAConfig.StartNode`
   required) and `node/controllernode` (added to the facade layer in
   `script/archcheck/layers.json`) keep it. Accept, or prefer an archcheck
   exception with `StartNode` defaulting to `controllernode.Start`?
   Symbol: `controller.OTAConfig.StartNode`.
4. **BDX MRP delay on the device side.** `TransferOptions.AdditionalMrpDelay`
   (matter.js `bdxAdditionalMrpDelay`) is computed but node/'s BDX
   responder takes no per-transfer MRP delay. Add a port? Symbol:
   `node.OTAProviderConfig` (a `TransferMrpDelay func(OTAPeer)`), `bdx.Responder`.
5. **Controller discovery.** The four discovery test files need matter.js's
   `Discovery` / `ParallelPaseDiscovery` shape (a scanner set, parallel PASE
   attempts, cancellation, error kinds). Build it in milestone 6 phase 27,
   or classify the files? Symbol: `controller.Controller.Pair`, `Discover`.
6. **Reported session parameters on the device.** ReportedSessionParametersTest
   needs `node.Config` session intervals and an accessor for what the
   controller sent — `bridge` / `secure` ground. Symbol: `node.Config`
   (session intervals), the device session's reported parameters.
7. **Findings register.** These entries are closed by this bundle (I did
   not edit the register): Network profiles; `ClientInteraction.close` …
   write chunking (the batcher left, question 1); `_matterd._udp`;
   `ForgetCommissioned`; `threadNetworkNameOf`; `Node.Disable`;
   `includeKnownVersions`; the priming report after a cancel;
   `Cluster.Supports` / `Revision`; Registry `AutoStart` (decided,
   unchanged); a node that fails to load; fabrictest group messages
   (stale).

## by_design candidates

- **`MarkComplete` gone** (§6). `BD-Matter-EndpointGC-ModelCompleteGate`
  and `BD-Matter-EndpointNumber-Monotonic` describe the removed gate and
  need the owner's rewrite: a stored endpoint number is kept until `Forget`
  or `RetirePartition`.
- **The provider node's identity** (§1.3). matter.js serves the provider
  from the controller's ServerNode, so the provider's node id is the
  controller's root node id (`docs/OTA.md` "Enable OTA Provider on
  Controller Creation"; `OtaAnnouncements.ts:47-58`, `:272-278`); here the
  provider is an own node with a node id of its own, commissioned into the
  fabric by the controller, because the controller role runs no server node.
- **Privacy masking of the commissioner record** (§4). `PrivacyMasked`
  leaves out the vendor and product (`omitPrivateDetails`,
  `CommissionerMdnsAdvertisement.ts:20-71`), not the extended-announcement
  masking; SII/SAI/SAT are left out as the defaults.
- **SupportedTransports absent versus zero** (§2). A Go bitmap cannot tell
  an absent tag 8 from 0; a peer at 1.5+ with no bits is read as absent,
  so its mDNS `T` key decides (`Peer.ts:352-410`).
- **The TCP session-parameter check after CASE** (§2). matter.js checks
  before the session is used (`PeerConnection.ts:596-621`); here after the
  handshake, which is `client/handshake` (owner path).
- **`OnClosing` on destroy** (§2). The session's closing hook also fires on
  destroy, where matter.js's fires on close only.
- **DefaultOTAProviders comparison** (§1). The announcement compares the
  existing entry without the FabricIndex, the read being fabric-filtered
  (`OtaAnnouncements.ts:186-197`).
- **A Go `Updated` returns no error** (§5). The two ClientSubscribeTest
  cases whose `updated` throws (`ClientSubscribeTest.ts:1275-1330`) have no
  Go shape.
- **A stray CommandStatusIB** (§5). `InvokeBatch` returns one `Result` per
  command and drops a status for no sent command, where matter.js passes
  it up (`ClientInteraction.ts:616-646`, ClientInteractionInvokeTest
  "passes a stray CommandStatusIB up").
- **No commissionable discoveries in the registry** (§5). `client/discovery`
  reports them without registering a node (`Peers.ts:410-413` filters them
  out of `commissioned`).
- **The forgetting scanners** (§5). The controller's BLE scanner lives for
  one pairing, so `controller.New` passes no `Scanners`; matter.js's
  `ScannerSet` is long-lived (`ControllerCommissioner.ts:321-341`).
