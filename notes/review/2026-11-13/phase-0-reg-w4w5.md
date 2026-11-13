# Phase 0 — register reconciliation, waves W4 and W5

- **Scope:** every finding of `../2026-11/wave-4-ctl-{net,sec,im,int}.md`,
  `wave-5-{ex,api}.md`, their verdict files and the W4/W5 parts of
  `SUMMARY.md` — 75 findings.
- **Baseline:** `95e03096` (branch `review/2026-11-13`).
- **Pins:** matter.js `db021da0`, connectedhomeip `6170af84`, read from the
  `git archive` exports under the session scratchpad (`pin/matter.js`,
  `pin/connectedhomeip`). Old citations are at `862ad75f`; where a line is
  re-cited here it was re-located by content at `db021da0`.
- **Method:** each fix located with `grep -n`, the surrounding path read;
  the guard tests run targeted (`go test -count=1 -run … ./pkg/`, all `ok`
  unless §5 says otherwise). "Measured" = read in code or run; anything
  else is marked *inferred* or *unverified*.

Status words: **holds** (fix present, defect gone), **no longer true**
(the claim was overtaken by later work), **still true**, **partly** (fix
incomplete). No regression was found.

| | holds | no longer true | still true | partly | regressed |
| --- | --- | --- | --- | --- | --- |
| W4 (40) | 33 | 3 | 3 | 1 | 0 |
| W5 (35) | 31 | 1 | 2 | 1 | 0 |

## 2.1 `wave-4-ctl-net.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W4-ctlnet-01 | medium | fixed 8959f20b | holds | `client/exchange/margins.go:60` (`satAdd(d, pad.Fixed)`), `:71-79`; `client/exchange/exchange.go:705-719` (`backoffForLocked`: `satAdd(d, initial)` before the cap). No other addition after the saturation: callers `exchange.go:681,688`, `:762` subtracts, `:1063` and `margins.go:90` sum over `MaxTransmissions` only. Tests `margins_parity_matterjs_test.go:191` (subtest "caps a saturated backoff …" of `TestMessageExchangeMRPBackoffMargin`), `address_test.go:139` `TestAddressHelpers` | none |
| W4-ctlnet-02 | medium | fixed 2befacbe | holds | `client/exchange/manager.go:845,856` (`adoptSource` only for a non-duplicate message that opens an exchange on a secure session), `session.go:145-152`, `:109-122`; `client/peer/peer.go:400`. Tests `address_test.go:91` `TestNewExchangeMovesSessionToSource`, `client/peer/monitor_test.go` `TestSessionMoveCarriesOperationalAddress` | none |
| W4-ctlnet-03 | low | fixed a10d6b76 | holds | `exchange.go:609-618` (`endAckWaitLocked` on ctx), `:625-634`, re-arm guarded at `:796`. Test `retransmission_test.go` `TestCancelledSendEndsRetransmission` | none |
| W4-ctlnet-04 | low | fixed a10d6b76 | holds | `client/peer/connection.go:241-249` (`liveSessionLocked` under `p.mu`). Test `set_test.go` `TestInitiateConnectionWithLiveSession` | none |
| W4-ctlnet-05 | low | fixed a10d6b76 | holds | `client/peer/peer.go:696-709` (second sweep after `workers.Wait`). Test `set_test.go` `TestCloseSweepsSessionsAfterWorkers` | none |
| W4-ctlnet-06 | note | fixed a10d6b76 | holds | `exchange.go:754-759` (`x.closed \|\| (x.initiator && x.closing)`). Test `retransmission_test.go` `TestClosingResponderKeepsRetransmitting` | none |
| W4-ctlnet-07 | note | open (parity) | still true | `client/peer/connection.go:1368-1370` returns `ErrNotConnectable`; classified "general" → `DelayAfterUnhandledError` (`:1731`). No `by_design` row (grep `NotConnectable`, `W4-ctlnet-07`: none) | BY_DESIGN (§3.1) |

## 2.2 `wave-4-ctl-sec.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W4-ctlsec-01 | low | fixed 0a368832 | holds | `controller/commissioner/wire.go:148-159` returns the nonce; `flow.go:977-1010` compares before `publicKeyFromCSR`. Test `csr_nonce_chip_test.go:69` `TestCertificatesCsrNonceEchoed`. Ahead row `by_design.md:130`, `:5542`. Marker drift: the row and the code comment cite `ControllerCommissioningFlow.ts:1395`; at `db021da0` the line is `:1412` (still the file's only `csrNonce`) — the ratchet's business | none |
| W4-ctlsec-02 | low | fixed 0a368832 | holds | `controller/ca/ca.go:64-80` (`validateCATs`), called at `:643` before the only `dnTagCAT` append (`:663`). Test `cats_parity_matterjs_test.go:19` `TestGenerateNOCValidatesTheCATList`. matter.js `CaseAuthenticatedTag.ts:96-105` unchanged at the pin | none |
| W4-ctlsec-03 | note | open ("stay with matter.js", no row) | still true | `controller/attestation/validate.go:229-241`: validity at the DAC's notBefore only. No row (grep `notAfter`, `DacExpired`: none) | BY_DESIGN (§3.2) |
| W4-ctlsec-04 | note | left as note | no longer true | `controller/attestation/profile.go:352-400` (`assertCommonProfile`, `assertPaiProfile`, `assertDacProfile`: v3, cA, pathLen, key-usage bits, criticality), enforced by default (`cert.go:85,126,130`; `validate.go:361` passes `false`). Mirrors matter.js `kinds/AttestationCertificates.ts:153,182,210-230` at `db021da0`. Tests `TestAttestationProfileRefusesDAC`, `…RefusesPAI` (`attestationcertificateprofile_parity_matterjs_test.go:106`, `:148`) | STRIKE — overtaken by the pin lift |
| W4-ctlsec-05 | note | left as note | no longer true | `controller/attestation/validate.go:141`: an unknown CD signer is the error finding `CdSignerUnknown`; `CdSignerVerificationSkipped` is deprecated upstream (`DeviceAttestationValidator.ts:76,81-82,466` at `db021da0`) | STRIKE — overtaken (#431) |
| W4-ctlsec-06 | note | fixed 0a368832 (doc) | holds | `controller/commissioner/commissioner.go:168-175`; rows `by_design.md:3774`, `:4658`. matter.js still carries the lenient default (`ControllerCommissioningFlow.ts:150`, `:1373` TODO at `db021da0`), so the row stays | none |
| W4-ctlsec-07 | note | no change (already documented) | holds | `docs/security.md:47-48` | none |
| W4-ctlsec-08 | note | open (parity) | still true | `controller/commissioner/flow.go:274-281`: the disarm runs only for a `CommissioningError`/`attestation.Error`/`StatusError` (`errors.go:229-239`); a bare context error is returned as is. No row | FIX (§3.3) |

## 2.3 `wave-4-ctl-im.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W4-ctlim-01 | medium | fixed 8aac8e76 | holds | `controller/client/interact.go:52-54` (`Read` → `emitEvents=false`), `:146-148,186` (host `Subscribe` → nil), `structure.go:338-344` (no emit without the flag); cursor raised only in `deliverEvent` (`network.go:624-630`). Tests `client_emit_events_parity_matterjs_test.go` `TestHostReadDoesNotEmitEvents`, `reactions_test.go:68` | none |
| W4-ctlim-02 | medium | fixed b05adad8, 94140f5e | holds | `client/interaction/invoke.go:195-197,247`, `invoke_batcher.go:65-71`, `write.go:238-242` → TimedRequest sent at `write.go:149-156`; `schema/timed.go:29,45`. Tests `TestInvokeBatcherParity` (`invoke_batcher_parity_matterjs_test.go:263`), `TestWriteRequestTimedFromModel` | none |
| W4-ctlim-03 | medium | fixed 0466b6b2 | holds | `controller/client/registry.go:468-479` (`ia.CloseSubscriptions()`), `client/interaction/interaction.go:213-226` (closes the active set of the peer). Test subtest "closes a host's sustained subscription when the node is deleted" (`client_connectivity_parity_matterjs_test.go:228`) | none |
| W4-ctlim-04 | medium | fixed 0dc4ad84 | holds | `controller/client/node.go:220-250` (`applyDeferred` → `jobs.enqueue`), `jobs.go:12,37-52` (bound 100, FIFO), drained in `Stop` (`node.go:501`). Test `client_structure_jobs_parity_matterjs_test.go:21` `TestBlockingHookDoesNotDelayStatusResponse`. See new finding C03 | none |
| W4-ctlim-05 | low | fixed f0d69498 | holds | `interact.go:191-213` (refresh leaves the event filters). Test `network_test.go:441` `TestSustainedHostSubscriptionKeepsEventFilters` | none |
| W4-ctlim-06 | low | fixed f0d69498 | holds | `controller/client/cache.go:168-172,289-303` (`flushMu` across snapshot and `Save`; the armed timer stopped). Test `cache_internal_test.go:384` `TestFlushesAreSerialised` | none |
| W4-ctlim-07 | low | fixed f0d69498 | holds | `client/interaction/provider.go:161-170`, `:268-275`. Test `provider_test.go:157` `TestMaximumPeerResponseTimeCarriesLocalPad` | none |
| W4-ctlim-08 | low | fixed f0d69498 | holds | `write.go:135-148` (`chunkWrite` after `begin`, the exchange's session). Test `write_test.go:271` `TestWriteChunksForTheExchangeSession` | none |
| W4-ctlim-09 | low | fixed f0d69498 | holds | `controller/client/network.go:319-343`. Test `network_test.go:540` `TestStopDuringInitialReadEstablishesNothing` | none |
| W4-ctlim-10 | note | fixed f0d69498 | holds | `client/interaction/sustained.go:249-255`. Test `report_internal_test.go:213` `TestSetActiveAnnouncesGoingLiveAgain` | none |
| W4-ctlim-11 | note | fixed f0d69498 | holds | `read.go:66-72`, `subscribe.go:216-218`. Test `TestEncodeFitting` (`report_internal_test.go:200`) | none |

## 2.4 `wave-4-ctl-int.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W4-ctlint-01 | **high** | fixed #359 (item 7) | holds | Depth: `controller/dcl/crl.go:53-59` (`derMaxDepth = 32`), `:95-98`, `:136` (`depth+1`); `derDecodeRec` is the only recursion and `derDecode` its only entry (10 call sites, all `crl.go`/`revocation.go:443`). Body: `revocation.go:335` (`maxCRLSize = 1 MiB`), `:365-367` (a point announcing more is rejected before the read), `:372` (`LimitReader(limit+1)`), `:375-377`. Tests `fuzz_revocation_test.go:125` `TestDERDepthIsBounded`, fuzz seed `:89`, `revocation_test.go:463` `TestDownloadCRLBounded` | none |
| W4-ctlint-02 | medium | fixed #359 (item 7) | **partly** | Bounded only when the record states a size: `controller/dcl/ota.go:286-296`. Unbounded `io.Copy` when `OTAFileSize == 0` (`ota.go:280-285`), into a `bytes.Buffer` (`controller/softwareupdate/images.go:661-669`, `:741-742`). The proposed `MaxImageSize` cap does not exist (grep: none). Test `ota_test.go:227` covers the stated-size case only | FIX (§3.4, with C01) |
| W4-ctlint-03 | medium | fixed af994d4e | holds | `images.go:214-218` (`otaimage.ValidateFile` before `Put`), `:246-262`; row `BD-Matter-OtaImageDigestAtStore`. Test `images_parity_matterjs_test.go:183` `TestImagesStoreValidatesFileParity` | none |
| W4-ctlint-04 | low | fixed c9ca2bcd | holds (read; test not run, §5) | `ble/bluez/central_linux.go:675-690` (`writeTimeout` 30 s → `btp.ErrDisconnected`). Test `central_linux_test.go:473` `TestUnansweredWriteIsADisconnect` (Linux only) | none |
| W4-ctlint-05 | low | fixed c9ca2bcd | holds | `ble/esphome/gatt.go:179-189` (`markGone` on ctx). Test `central_test.go:211` `TestCancelledWriteEndsTheConnection` | none |
| W4-ctlint-06 | low | fixed c9ca2bcd | holds | `controller/dcl/types.go:156-168`. Test `ota_test.go:250` `TestOTAVersionWithoutMaximum` | none |
| W4-ctlint-07 | low | fixed c9ca2bcd | holds | `bdx/otaimage/otaimage.go:251-256,283-285`; chip `src/lib/core/OTAImageHeader.cpp:53`, `:127-128` read at the pin. Test `fuzz_header_test.go:117` `TestReadHeaderRefusesOversizeHeader` | none |
| W4-ctlint-08 | low | fixed c9ca2bcd | holds | `images.go:162` (`(?i)`), `:339`, `:467`. Test `TestImagesFilenameCaseParity` | none |
| W4-ctlint-09 | note | by_design paragraph (trust as fetched) | no longer true | A fetched signer is trusted only when the CSA CD root issued it: `controller/dcl/certstore.go:520-560` (`verifyCDSigner` on store and ledger answers), `by_design.md:3972-3982` (rewritten at #431). matter.js `DclCertificateService.ts:832-845,894-991` at `db021da0`. Tests `certstore_test.go:205` `TestCertificateStoreCDSigner`, `certstore_parity_matterjs_test.go:1050` | STRIKE — overtaken; the register paragraph already says so |
| W4-ctlint-10 | note | fixed 49f05b93 | holds | `controller/dcl/client.go:165,190-193,202-204`, `github.go:201`, `thread/otbr/client.go:140`. Tests `bounds_test.go:21` `TestResponseBounds`, `borderrouter_test.go:253` `TestResponseBodyBounded` | none |
| W4-ctlint-11 | note | fixed 49f05b93 (doc) | holds | `docs/adr/0027-…md:174-176`, `thread/otbr/doc.go:20`, `docs/guides/thread-border-router.md:8-9` | none |
| W4-ctlint-12 | note | fixed 49f05b93 | holds | `controller/dcl/certstore.go:130-133,207,213-229`. Test `certstore_test.go:398` `TestCertificateStoreCloseCancelsPeriodicUpdate` | none |
| W4-ctlint-13 | note | fixed 49f05b93 (doc) | holds | `ble/esphome/doc.go:45-48`, `docs/guides/ble-proxies.md:41-51` | none |
| W4-ctlint-14 | note | fixed c9ca2bcd, 809e03ad | holds | `thread/otbr/client.go:43-45` (`Config.Clock`), `borderrouter.go:38-42,83-86`, `ble/esphome/discover.go:187-202`, `otaimage.go:277,293` (read error wrapped). Tests `clock_logger_test.go:23`, `discover_test.go:110`. The "three spellings of the timeout" sub-item was not changed: `dcl.Client.Timeout` (`client.go:85`) and `otbr.Config.Timeout` (`client.go:39`) carry one name; `esphome.Options.ConnectTimeout` names a different thing (the BLE connect) | STRIKE the residual sub-item |

## 2.5 `wave-5-ex.md`

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W5-ex-01 | **high** | fixed #359 (item 8) | holds | `store/sqlite.go:46-62` (`url.URL{Scheme:"file", OmitHost:true, Path, RawQuery}`); the four sites use it: `node/store_path.go:18`, `controller/open.go:36`, `fabrictest/device.go:165`, the quickstart through `Store.Path` (`examples/quickstart-device/main.go:69`). No other non-test `"file:"+` DSN (grep; `examples/reference-bridge/surface.go:131` is a constant in-memory name). Probe (a scratch `go run` of the same `url.URL` literal, not kept): `/tmp/a#b?c%d.db` → `file:/tmp/a%23b%3Fc%25d.db?…`, `//tmp/x.db` → `file:%2F/tmp/x.db?…`. Tests `store/sqlite_test.go:18` `TestOpenSQLiteEscapesThePath`, `:48` `…RelativePath` (ran: ok). The class survives in test helpers — new finding C02 | none |
| W5-ex-02 | medium | fixed e44b6d50 | holds | `examples/quickstart-device/main.go:21-24,69` (imports `cluster/spec/onoff`, `contract`, `device`, `node` only). Guard `examples/imports_test.go:42` `TestSmallExamplesImportTheFacadeOnly` | none |
| W5-ex-03 | medium | fixed e44b6d50 | holds | `examples/quickstart-controller/main.go:61` (uses the node `Pair` returns; no `Nodes()[0]`, grep) | none (no test names the node choice) |
| W5-ex-04 | medium | fixed fa402bff | holds | README grep for `WithServer`, `WithMeasurement`, `sqlitestore.New`, `ReestablishFormerSubscriptions`, "IPK rotation": none; `:354` now states resumption records persist | none |
| W5-ex-05 | medium | fixed #359 | holds | `store/sqlite.go:47-51` (`O_CREATE\|O_EXCL`, `0o600`, before `sql.Open`); every facade and example path goes through it (see ex-01; `examples/reference-controller/cli/stack.go:84` → `controller.OpenDB`). An existing file keeps its mode — stated in the doc comment (`sqlite.go:41-45`) and `docs/guides/operations.md:47-52`. Other secret-bearing writer: `controller/ca/keyfile/keyfile.go:209` (`0o600`). Test `sqlite_test.go:66` `TestOpenSQLiteCreatesTheFileOwnerOnly` (ran: ok) | none |
| W5-ex-06 | low | fixed 107f897e | holds | `node/security.go:616-620` (the node's `life`), `node/commissioning.go:429`. Test `failsafe_watcher_test.go:29` `TestClosedSiteLeavesNoFailSafeWatcher` | none |
| W5-ex-07 | low | fixed 792a699c (typed) + open note (helper) | still true (helper) | Typed part holds: `cli/group.go:19-22,78-79`, `cli/bind.go:17-19,143`. No provisioning helper in `controller/` or `controller/client` (grep `ProvisionGroup\|func (.*) Bind`: none); `bind.go:74` still builds the list container by hand | FIX or STRIKE — owner (§3.5) |
| W5-ex-08 | low | fixed 378a9975, 0b66456d | holds | `node/node.go:825-838` (`started` set only after `startLocked`), `:1100-1101` (`Run` stops), `examples/host-view/main.go:106-107`. Test `node_test.go:665` `TestStartFailureLeavesTheNodeStartable` | none |
| W5-ex-09 | low | fixed a53362e1 | no longer true | `node/controllernode` was retired in #416 (7e895c1d); `cli/self.go:17-22`. Its stale mentions are new finding C04 | STRIKE — the copied starter is gone |
| W5-ex-10 | low | open note (three facade gaps) | still true, changed | Gap 1 is closed in the facade — `node/endpoints.go:66` `Node.Endpoints()` (#401) — but the daemon still reads `n.Bridge().Topology()` (`examples/reference-bridge/main.go:570-571,603`) and its doc still calls it a gap (`main.go:29-31`). Gap 2: `main.go:456` `store.NewThermostatPresetStore`. Gap 3: `fleet_evse.go:159` `energy.NewEnergyEvse`; the kit's default is built "without a host" (`device/defaults_appliance.go:238-248`) | FIX 1 and 3, STRIKE 2 (§3.6) |
| W5-ex-11 | low | fixed fa402bff | holds | `fabrictest/doc.go:33-34` | none |
| W5-ex-12 | note | fixed 390a75d2 (W6) | holds | `fabrictest/seam.go:20-40`, `internal/fabrictestseam`; README `:125-127`. `Controller.Commissioner()` and `Fabric()` stay public (`fabrictest/controller.go:430,437`) — they return the controller role's own public types, not a layer below it | none |
| W5-ex-13 | note | fixed e44b6d50 | holds | no `openNames`/`discovery` in `examples/quickstart-controller/main.go` (grep) | none |
| W5-ex-14 | note | fixed 9d5c0e14 | holds | `examples/reference-bridge/fifo_unix.go:23,39` (`0o660`). Test `fifo_unix_test.go:16` `TestAppPipeIsNotWorldWritable` | none |
| W5-ex-15 | note | fixed bc168c4b | holds | `cli/pair.go:89-100,163-164` (`@file`, `GOFABRIC_WIFI_CREDENTIALS`, `GOFABRIC_THREAD_DATASET`). Test `pair_test.go:38` | none |

## 2.6 `wave-5-api.md`

The three API breaks and the five duplicate-shape pairs (WithServer/HostServer,
Option/Declaration, TypeByID/Lookup, PairingCodes/SetupPayload,
IncreaseConfigurationVersion(s); plus the two AheadRows) are all in place.

| ID | sev | 2026-11 | at 95e03096 | evidence | disposition |
| --- | --- | --- | --- | --- | --- |
| W5-api-01 | medium | fixed 40c1c674 | holds | `node/commissioning.go:559,584,602` (`OpenBasicWindow`, `OpenOwnWindow`, `CloseWindow`); no `Node.CommissioningWindow()` and no alias (grep). Test `window_facade_test.go:19` `TestNodeOpenCloseReopenOwnWindow` | none |
| W5-api-02 | medium | fixed b5f9c2d3 | holds | `device/lifecycle.go:114-120` the one input; no `Reachable` in `device/options.go`, no `AvailabilityProbe` (grep). Test `node/lifecycle_test.go:187` `TestDeviceReachableReachesSubscription` | none |
| W5-api-03 | medium | fixed 36811c34 | holds | `node/node.go:193` `New(ctx, cfg)`; goroutine starts in `startLocked` after the last fallible step (`:869-876`, `node/security.go:515-517`). The window provider's reaper under `context.Background()` (`node/commissioning.go:437`) starts with a window, not in `New`, and ends in `commissioningHost.stop` (`:461-468`). Test `new_goroutines_test.go:22` `TestNewStartsNoGoroutine` (ran: ok) | none |
| W5-api-04 | medium | fixed 4cf5845d | holds | `node/observers.go:316,342`, `health.go:93,100`, `ota_provider.go:115,125`, `diagnostics.go:31,44`; no error-returning hook left in `observers.go` (grep). Test `observers_test.go:251` `TestObserveAsyncQueuesInOrder` | none |
| W5-api-05 | medium | fixed e966c90f | holds | `device/options.go:44-47` (`Options.Logger`); no `var Logger` (grep). Test `device/defaults_test.go:171` | none |
| W5-api-06 | low | fixed 378a9975, 0b66456d | holds | `node/aggregator.go:222,373-379`; `node/node.go:825-838`. Tests `node_test.go:665`, `:714` `TestAggregatorAfterStop` | none |
| W5-api-07 | low | fixed c7a78abb | holds | `node/facade.go:210-222` | none |
| W5-api-08 | low | fixed be73b65c | holds | no `WithServer`, `Option`, `TypeByID` in `device/` (grep); `type.go:158` `Lookup`, `options.go:372` `HostServer` | none |
| W5-api-09 | low | fixed 7fad2075 | holds | no `Node.PairingCodes`; `aggregator.go:575-578` (`ctx`, `ErrNotStarted`); `config.go:414`, `commissioning.go:497` (`PBKDFIterations uint32` twice) | none |
| W5-api-10 | low | fixed dedd23bf | holds | no `Enabled`/`Disable…`/`Disabled` bool in `node/config.go`, `client.go`, `mdns.go`, `health.go` (grep); `config.go:163-166,312-317` | none |
| W5-api-11 | low | fixed b4ac157d | holds | `node/config.go:146-150`, `:560-563` (unexported, set through `internal/nodeseam`); `bootid/bootid.go:79` `EnableRotation` | none |
| W5-api-12 | low | fixed 2de3de16 | holds | `contract/dataversion.go:49-53` (unexported); no `SetMeasurementMaterializer` in `contract/` (grep); `matter.go:410` | none |
| W5-api-13 | low | fixed 6801c78b | holds | none of "every DP", `go-fabricadapter`, `MatterEndpointSource`, `0x0042` in `eligibility/*.go`; `notes/parity/controller_profiles.md:42-44`. Test `eligibility/compat_test.go:13` | none |
| W5-api-14 | low | fixed 6f7c4597 | holds | `node/surface.go:60` the only `AheadRows`. Test `surface_ahead_test.go:62` `TestNodeAheadRows` | none |
| W5-api-15 | low | fixed d61a8884 | holds | only `schema/lookup.go:53` `DeviceTypeName`. Guard `cluster/measurement/materializers_test.go:123` | none |
| W5-api-16 | low | fixed 7903302f | holds | `node/doc.go:49-56`, `device/type.go:150` | none |
| W5-api-17 | low | fixed 5eff492c | holds | `node/config.go:79,129`, `surface/element.go:42` (`Provenance`) | none |
| W5-api-18 | note | fixed e0e7f7e6 | **partly** | `device/on_off_light_gen.go:27`: `func OnOffLightType() *Type { return &typeOnOffLight }` — the variable can no longer be reassigned, but the accessor hands out the registry's own pointer and `Type`'s fields are exported (`device/type.go:22-45`), so `device.OnOffLightType().Revision = 5` still rewrites the registry process-wide; `type.go:20-21` still says "callers must not modify it". Read, not compiled | FIX or STRIKE — owner (§3.7) |
| W5-api-19 | note | fixed 7c5d3066 | holds | `node/observers.go:87-97`. Test `session_events_parity_matterjs_test.go:62` `TestSessionEventsParity` | none |
| W5-api-20 | note | fixed 33650a65 | holds | `device/options.go:69`, `node/config.go:156` (both values) | none |

`SUMMARY.md`: "PR 1 verification" items 7 and 8 are the rows W4-ctlint-01/02
and W5-ex-01/05 above (item 7 holds for the CRL, is incomplete for OTA).
"What stayed open" for W4 and W5 is exactly §3.1, §3.2, §3.3, §3.5, §3.6.
No W4 or W5 finding was struck in 2026-11.

## 3. Dispositions

### 3.1 W4-ctlnet-07 — BY_DESIGN

Measured: after `Set.Disconnect` an attempt returns `ErrNotConnectable`
(`client/peer/connection.go:1368-1370`), which the classifier treats as
"general" and retries after `DelayAfterUnhandledError` on the peer's
context until the peer closes. matter.js at `db021da0` does the same:
`packages/protocol/src/peer/PeerSet.ts:83-85` throws
`ImplementationError("Client networking is not initialized")`, and
`PeerConnection.ts:715-717` gives every unclassified error
`delay = timing.delayAfterUnhandledError; category = "general"` (logged
at `:762-764`).

Row text: *BD-Matter-Peer-ConnectAfterDisconnectRetries — a `Connect`
after `Set.Disconnect` starts a connection process that fails each attempt
with `ErrNotConnectable` and retries every `DelayAfterUnhandledError`
until the peer closes, as matter.js retries its `ImplementationError`
(`PeerSet.ts:83-85`, `PeerConnection.ts:715-717`). Mirrored; a refusal in
`initiateConnection` would be an Ahead row.* Proving test: a set
disconnected, `Connect` with a short timeout, the fake clock stepped twice
by the delay — two attempts, both `ErrNotConnectable`.

Alternative (owner): refuse in `initiateConnection` when
`p.set.isDisconnected()` — one check under `p.mu`, an Ahead row with the
marker `PeerSet.ts:85`. It removes a warn loop nobody can act on.

### 3.2 W4-ctlsec-03 — BY_DESIGN

Measured: `controller/attestation/validate.go:229-241` judges the chain at
the DAC's notBefore and never reads the clock; an expired DAC or PAI
passes. matter.js at `db021da0` is identical
(`certificate/DeviceAttestationValidator.ts:393-420`, "Step 3b:
Certificate validity at DAC's notBefore timestamp"). connectedhomeip
refuses an expired DAC:
`src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp:638-641`
(`IsCertificateValidAtCurrentTime(info.dacDerBuffer)` → `kDacExpired`,
compiled out under `CURRENT_TIME_NOT_IMPLEMENTED`).

The owner decided on 2026-11-02 to stay with matter.js; what is missing is
the row. Row text: *BD-Matter-Attestation-ValidityAtDacNotBefore — the
attestation chain is valid when the DAC's notBefore lies inside the PAI's
and the PAA's validity; the current time is not consulted, so a DAC past
its notAfter is accepted (matter.js `DeviceAttestationValidator.ts:393-420`).
connectedhomeip rejects it (`DefaultDeviceAttestationVerifier.cpp:638-641`).
Mirrored (owner 2026-11-02): devices in the field outlive their DAC.*
Proving test: a parity case with a DAC whose notAfter is in the past —
accepted, no `CertificateExpired` finding.

### 3.3 W4-ctlsec-08 — FIX (recommended), or BY_DESIGN

Measured: `flow.execute` disarms only for a commissioning or status error
(`controller/commissioner/flow.go:274-281`, `errors.go:229-239`); a step
that returns a bare `context.Canceled`/`DeadlineExceeded` leaves through
`return err` at `:290` with the device's fail-safe armed. The comment at
`:276-280` already argues that the reset must outlive ctx. matter.js at
`db021da0` has the same shape — `ControllerCommissioningFlow.ts:437-467`
resets for `causedBy(error, CommissioningError, StatusResponseError)` and
rethrows anything else — but it has no abort signal in the flow at all, so
a cancelled context is this module's own surface, not mirrored behaviour.

Fix: in `execute`, treat an error that `errors.Is` a context error with a
fail-safe armed like the disarm branch: `resetFailsafeTimer(context.
WithoutCancel(ctx))`, bounded by the exchange's response timeout as the
existing branch is, then return the context error unchanged. A
`by_design` paragraph records it as the Go-surface divergence it is.
Proving test: a commissioner test device that records ArmFailSafe calls;
cancel ctx after step ArmFailSafe; assert one `ArmFailSafe(0)` arrives and
`Commission` returns `context.Canceled`.

BY_DESIGN alternative: *BD-Matter-Commissioner-ContextAbortKeepsFailSafe*
citing `ControllerCommissioningFlow.ts:463-465` — weaker, because the host
that cancels gets a device that refuses re-commissioning until expiry.

### 3.4 W4-ctlint-02 — FIX

See C01: the bound follows the vendor's statement and is absent without
one.

### 3.5 W5-ex-07 — owner decision

Measured: the CLI is on the generated types; a host still writes the
sequence itself (key set write, group join, ACL entry; binding append with
the ACL entry) — about 150 lines in `cli/group.go` and `cli/bind.go`.
Whether matter.js offers such a helper on its controller was not checked
(unverified).

FIX sketch: `client.Node.Bind(ctx, source endpoint, target, clusters)` and
`Registry.ProvisionGroup(ctx, GroupSpec{ID, KeySet, Nodes, Endpoints})` in
`controller/client`, moving the CLI's code under the facade; tests on
fabrictest: two devices, a group provisioned, a group invoke reaching
both; a binding with its ACL entry read back fabric-filtered. STRIKE
alternative: an example's length is not a defect; file it as a feature
issue. Recommendation: FIX — every host that provisions groups needs the
same sequence and getting the ACL entry wrong is a security-relevant
mistake the facade can prevent.

### 3.6 W5-ex-10 — FIX gaps 1 and 3, STRIKE gap 2

1. FIX: `printTopology` and the banner read `Node.Endpoints()` instead of
   `Node.Bridge().Topology()`; the package doc drops the sentence. Test:
   extend `TestSmallExamplesImportTheFacadeOnly`'s idea to the reference
   bridge as "no `.Bridge()` call" (a grep-shaped guard in
   `examples/reference-bridge/wiring_test.go`).
2. STRIKE: `store` is the documented persistence seam behind
   `Node.Store()`, and the kit already takes the store as a port
   (`thermostatdef.Declare.PresetStore`, `device/defaults.go:481-483`,
   `cluster/spec/thermostat/server_gen.go:724-728`); a host persisting its
   own state through that seam is the intended cut. The doc should stop
   calling it a gap.
3. FIX: a host port on the kit's default EnergyEvse — `evsedef.Declare`
   gains the `EvseHost` port as the thermostat's Declare carries its
   ports, wired in `newEnergyEvseServer` (`defaults_appliance.go:241`).
   Test: a device declared with the port sees `EnergyTransferStarted`
   read its meter.

### 3.7 W5-api-18 — owner decision

FIX sketch: the accessors and `Lookup` return a copy with cloned slices
(`Servers`, `Clients`, `Conditions`), the registry keeps its own values;
test: mutate the returned value, call the accessor again, compare with
the generated constant. Cost: an allocation per call and no pointer
identity. STRIKE alternative: the documented read-only pointer is ordinary
Go (`type.go:20-21`), the reassignment the finding led with is closed.
Recommendation: FIX — a registry one field write can corrupt for every
node in the process is what ADR 0025 principle 10 names, and the copy is
cheap at construction time.

## 4. New findings

### R2-P0-bound-C01 — medium — the OTA download is bounded by the vendor's own statement, and unbounded without one

- `controller/dcl/ota.go:280-285` (`OTAFileSize == 0` → `io.Copy(w,
  resp.Body)`), `:286-296` (limit = the record's `OTAFileSize`, any value
  up to `MaxInt64-1`); buffers `controller/softwareupdate/images.go:661-669`
  and `:741-742`.
- Claim (measured): a ledger record whose `otaFileSize` is absent or not a
  decimal maps to 0 (`controller/dcl/types.go:161-164`; no check refuses
  such a record, grep `OTAFileSize` in `client.go`: none), and
  `UpdateInfoFromURL` always passes 0 (`images.go:742`). In both cases the
  whole answer is held in RAM for up to `DefaultDownloadTimeout` (5 min,
  `ota.go:26`). A record that states a size raises the bound to whatever
  it states. This is the defect class PR 1 closed for CRLs — "DataFileSize
  is the vendor's statement and must not raise the bound"
  (`revocation.go:331-335`) — left open for images. Inferred, not
  reproduced: the memory exhaustion itself.
- Citation: matter.js streams to storage without a bound
  (`DclOtaUpdateService.ts` as cited at `ota.go:241-245`; not re-read at
  the pin — unverified here); the Go consequence differs because the
  image is buffered.
- Fix: `ImagesConfig.MaxImageSize` (default an owner value; not invented
  here) applied in `Download` as the limit when the record states none
  and as the ceiling when it states more — an `OTAError` before or during
  the read. Test: a fake host serving past the cap with `OTAFileSize: 0`,
  and a record announcing more than the cap.

### R2-P0-test-C02 — low — 26 test call sites still build the `file:` URI by concatenation

- e.g. `node/node_test.go:35`, `controller/controller_test.go:37`,
  `store/testhelper_test.go:39`, `endpoint/sqlitestore/sqlitestore_test.go:36`,
  `bridge/secure_harness_test.go:48`, `controller/client/registry_live_test.go:140,471`
  (full list: `grep -rn '"file:"+' --include='*_test.go' .`, less the three `--ca-key` strings of `cli/cakey_test.go` and `keyfile_test.go:40`).
- Claim (measured): `t.TempDir()` keeps `#` in the directory name —
  `testing.go:1612` `const allowed = "!#$%&()+,-.=@^_{}~ "` (go1.27.4) —
  so a helper called from a subtest Go renamed `name#01` opens a truncated
  path outside the temp directory with the pragmas dropped: W5-ex-01's
  mechanism. Inferred: no test in the tree hits it today (not searched).
- Fix: the helpers call `store.OpenSQLite`; standing guard: a test that
  greps the module for `"file:"+` outside `store/sqlite.go` (the defect
  class has none yet).

### R2-P0-conc-C03 — low — `client.Node.Stop(ctx)` drains the job queue without ctx

- `controller/client/node.go:475-505`: the subscription wait honours ctx,
  then `n.jobs.wait()` (`:501`, `jobs.go:73-79`) and `n.persist.stop()`
  (`:502`) do not.
- Claim (measured in code, not reproduced): a host hook that blocks — the
  case W4-ctlim-04 moved off the report path — now blocks `Stop`, `Close`
  and `Registry.deleteNode` (`registry.go:468`) past their context. The
  doc says a hook must not call `Stop`; it does not say a slow hook holds
  it. matter.js drains its queue on every exit path
  (`ClientStructure.ts:482-486` as cited at `node.go:498-500` and `jobs.go:71-72`; not re-read
  at the pin — unverified) and has no deadline to break.
- Fix: `jobQueue.waitContext(ctx)`; on expiry `Stop` returns `ctx.Err()`
  and leaves the queue to finish. Test: a hook blocked on a channel, `Stop`
  with a 50 ms context returns `DeadlineExceeded`.

### R2-P0-doc-C04 — note — doc comments still describe the retired own node

- `controller/ota.go:27-35` (`[OTAConfig].StartNode`, a dead doc link —
  no such field, grep; "node/controllernode.Start is that starter"),
  `controller/softwareupdate/script.go:92` ("node/controllernode installs
  it"). `node/controllernode` went with #416 (`docs/adr/0026-…md:19`).
- Fix: rewrite both passages to the phase-37 shape (`controller/ota_provider.go:28-34`
  already tells it).

## 5. Not checked / not performable

- **W4-ctlint-04's test** (`TestUnansweredWriteIsADisconnect`): not
  performable on this host — `ble/bluez` is `_linux.go` only; the fix was
  read, not run.
- **Pin re-read of every citation inside the fixes**: only the lines named
  in this file were read at `db021da0` / `6170af84`. The other citations
  the fix commits added (the W4 verdict's "Reviewer verification" list)
  were verified at `862ad75f` on 2026-11-02 and not re-located here; their
  drift is the citation ratchet's.
- **W5-ex-03, -04, -11, -13, W5-api-07, -16, -17, W4-ctlint-11, -13,
  W4-ctlsec-06, -07**: doc or example fixes with no test of their own;
  status rests on the grep and the read named in the row.
- **W5-api-18**: the mutation through the accessor was read, not compiled.
- **W5-ex-07**: whether matter.js has a controller-side provisioning
  helper — not looked up.
- **C01's memory exhaustion and C03's hang**: derived from the code path,
  no reproduction written (phase 3 writes exploits as failing tests).
- **Full packages**: no `make` target, no full test run; the guard tests
  were run by name only (`client/exchange`, `client/peer`,
  `client/interaction`, `controller/{dcl,ca,commissioner,softwareupdate,client}`,
  `bdx/otaimage`, `store`, `node`, `examples`, `examples/reference-bridge`,
  `thread/otbr`, `ble/esphome` — all `ok`).
