| line | section | item | kind | stated blocker or condition to close |
| --- | --- | --- | --- | --- |
| 274 | Tier 2 | reply.go still copies Privacy/Control from request header (dead code) | docs/hygiene | remove on next touch of reply.go |
| 284 | Tier 2 | SetRegulatoryConfig ignores CountryCode; Location stays "XX" | correctness/interop | verify against Apple Home in concept v2 phase 5 |
| 330 | 1.6.1 pin | Groupcast not verified against a real controller | deferred-needs-real-device | internal/chiptool leg (join-group, multicast toggle, leave-group) |
| 349 | App clusters | Default Binding server resolves no peer; entries never acted on | conformance/certifiability | out of scope while module has no client role |
| 351 | App clusters | Constraint.In checked on controller writes only, not Initial/host Set | correctness/interop | validate after all initials applied; add localization start-up cases |
| 361 | App clusters | FanControl percent/speed formulas and On/Smart mapping recalled, unverified | conformance/certifiability | read fan-control-server.cpp and spec 4.4.6.x; correct functions |
| 368 | App clusters | SmokeCoAlarm default ExpressedState priority recalled, unverified | conformance/certifiability | compare with examples/smoke-co-alarm-app; adjust slice and test |
| 374 | App clusters | PowerSource (0x0011) never added to DeviceTypeList for SmokeCoAlarm | conformance/certifiability | append PowerSource when ep.PowerSource set; assert in test |
| 383 | App clusters | ModeBase DIRECTMODECH bit (20 vs 16) and status range 0x80 unverified | conformance/certifiability | read mode-base-server and RVC table; raise upstream if differs |
| 394 | App clusters | DIRECTMODECH advertised but InvalidInMode not enforced | conformance/certifiability | needs cross-cluster visibility; decide with a host |
| 401 | App clusters | CountdownTime reportability follows matter.js, not the spec's list | conformance/certifiability | — |
| 408 | App clusters | Not built: HEPA/carbon filter monitoring, pump extras, appliance optional clusters, etc. | conformance/certifiability | — |
| 423 | Tier 3 | UpdateNOC keeps invoking session open; matter.js force-closes it | correctness/interop | defer close of invoking session after response in fabricUpdated |
| 425 | Tier 3 | Unused UDP session closes without CloseSession (no peer address) | correctness/interop | bind UDP session to peer at establishment; needs fixture run |
| 427 | Tier 3 | SetpointRaiseLower on AUTO thermostat does not keep the deadband | correctness/interop | compute companion setpoint; add two refusals to parity table |
| 429 | Tier 3 | ArmFailSafe runs armed hook with cluster lock held | correctness/interop | release g.mu before hook; add test |
| 431 | Tier 3 | Node reopens commissioning window at once after last fabric removed | correctness/interop | reset policy on node facade: wait, clear state, reopen |
| 433 | Tier 3 | Mandatory command without handler advertised, answers UNSUPPORTED_COMMAND | conformance/certifiability | CheckServer verifies handler per command; spec.Server refuses at construction |
| 435 | Tier 3 | OTA requestor queryRunning cleared without defer | correctness/interop | use defer |
| 437 | Tier 3 | Fixed-width integer encodes exceed sanctioned workaround; provenance comments misstate matter.js | correctness/interop | — |
| 437 | Tier 3 | Invalid UTF-8 preserved on TLV decode | correctness/interop | — |
| 437 | Tier 3 | maxPathsPerInvoke unenforced; Invoke SuppressResponse ignored | conformance/certifiability | — |
| 437 | Tier 3 | CSRRequest lacks post-NOC ConstraintError guard | conformance/certifiability | — |
| 437 | Tier 3 | CaseAdminSubject CAT version 0 accepted | security | — |
| 437 | Tier 3 | Unfiltered NOCs read returns other fabrics' cert bytes | security | — |
| 437 | Tier 3 | Advertised port ignores the effective bind port | correctness/interop | — |
| 437 | Tier 3 | Ephemeral PASE provider leaks a reaper goroutine per close | correctness/interop | — |
| 437 | Tier 3 | Encrypted receive window permits counter rollover | security | — |
| 437 | Tier 3 | Negative-write parity guards pin unmounted implementations | docs/hygiene | — |
| 441 | Tier 3 | controller/client: 38 snapshot clusters lack generated definition; Extra() approximate | tooling/codegen | generator covers them or schema gains attribute table |
| 443 | Tier 3 | Registry record follows descriptor only on SessionAdded/commission, not every save | correctness/interop | peer.Observer.Updated |
| 443 | Tier 3 | Registry ForDevice stores no TTL; commissionable node expires after DefaultTTL | correctness/interop | scanner exposes the record TTL |
| 443 | Tier 3 | FabricRemovedError has no Go counterpart in decommission confirmation classes | correctness/interop | — |
| 445 | Tier 3 | Group sender mirrors matter.js epochStartTime Matter-epoch vs Unix-ms comparison | correctness/interop | — |
| 445 | Tier 3 | Group writes are not list-chunked | correctness/interop | — |
| 445 | Tier 3 | Group messages leave from ephemeral socket, not listener on 5540 | correctness/interop | bridge exposes its listener |
| 447 | Tier 3 | Client-role CASE resumption records are in memory only | correctness/interop | — |
| 447 | Tier 3 | core.TargetStruct sentinel encoding: endpoint 0 cannot be a binding target | correctness/interop | — |
| 447 | Tier 3 | make chipdm-check from a git worktree needs absolute CHIP_ROOT | tooling/codegen | — |
| 449 | Tier 3 | OTA provider resets directConsentObtained on every state change (mirrored) | correctness/interop | upstream changes or owner decides divergence |
| 449 | Tier 3 | OTA provider still sends userConsentNeeded after consent obtained (mirrored) | correctness/interop | upstream changes or owner decides divergence |
| 449 | Tier 3 | OTA provider ACL entry on first fabric only; second-fabric requestor cannot query | correctness/interop | upstream changes or owner decides divergence |
| 450 | Tier 3 | clustergen emits cross-cluster datatype (Channel ExternalIdList entry) as empty struct | tooling/codegen | clustergen resolves Cluster.Type references across definitions |
| 451 | Tier 3 | setup.QRCode accepts versions 1-7, trivial passcodes, invalid vendor/product pairs | conformance/certifiability | tighten the encoder |
| 451 | Tier 3 | No 21-digit manual-code encoder; no decode mode skipping validation | conformance/certifiability | — |
| 452 | Tier 3 | Flake under -race: controller/client TestReactionLeave reads store without waiting | test-flake | wait on NodeIDs in the test |
| 453 | Tier 3 | Timing flake: TestStartReaper_ActiveSessionSurvivesSweeps window too tight under load | test-flake | fake clock in the test |
| 454 | Tier 3 | UDP dispatch queues by session id outside 1024-goroutine cap; forged ids unbounded (DoS) | security | fix or by_design entry |
| 454 | Tier 3 | Concurrent Aggregator.Add/Remove reassemble last-writer-wins; device or endpoint number may be missed | correctness/interop | fix or by_design entry |
| 454 | Tier 3 | Node.Stop does not wait for running cluster handlers | correctness/interop | fix or by_design entry |
| 454 | Tier 3 | Start opens commissioning window when no fabric loads; paired node advertises as commissionable | correctness/interop | fix or by_design entry |
| 454 | Tier 3 | matter_diagnostics table and store API have no caller | docs/hygiene | fix or by_design entry |
| 454 | Tier 3 | endpoint/sqlitestore has no Migrations/SchemaFingerprint | tooling/codegen | fix or by_design entry |
| 454 | Tier 3 | Host-specific wording left in public docs and logs | docs/hygiene | fix or by_design entry |
| 455 | Tier 3 | DCL: expired CRL still revokes (nextUpdate never read; mirrored, pinned by test) | correctness/interop | owner ports or decides divergence |
| 455 | Tier 3 | DCL: out-of-band revocation, on-demand PAA fetch, compliance endpoint, vendor-info, OTA storage, ML-DSA not ported | conformance/certifiability | owner ports or decides divergence |
| 455 | Tier 3 | controller/dcl/cert.go copies cert-parsing helpers from controller/attestation | docs/hygiene | fold when a third consumer appears |
| 455 | Tier 3 | PAAStore/Revocations seams take no context; CRL fetches bounded by client timeout only | correctness/interop | owner ports or decides divergence |
| 494 | Harness | Maintenance mode not implemented; WNCV.S.M.Maintenance=0 | conformance/certifiability | port Mode/ConfigStatus sync and refusal; declare PICS 1 |
| 501 | Harness | TC-EEVSE-2.3 step 19: eleven ChargingTargets answer CONSTRAINT_ERROR, case expects RESOURCE_EXHAUSTED | conformance/certifiability | decide whether list bound belongs to decoder or server |
| 518 | Harness | TC-SU-2.7 would fail two asserts (ChangeReason, DownloadError fields) vs matter.js behaviour | conformance/certifiability | follow matter.js or the case; case is manual at pin |
| 551 | chipdm (ii) | GroupKeyMulticastPolicy is O in snapshot, D in Matter 1.6.1 | conformance/certifiability | snapshot corrected (matter.js gate until 1.6.1) |
| 560 | chipdm (ii) | matter.js switchColorMode calls hsvToXy where xyToHsv is meant | correctness/interop | upstream matter.js calls xyToHsv |
| 566 | chipdm (ii) | semtag.Label is O, spec says "MfgCode != null, O" | conformance/certifiability | matters once TagList served (TC-DESC-2.2) |
| 577 | Device type validation | PICS slices ep12/ep13 mark OperationCompletion (E01) absent though device types require it | tooling/codegen | gen_pics.py; for chip-tool harness owner |

Counts per kind (65 rows):
- security: 4
- correctness/interop: 29
- conformance/certifiability: 19
- test-flake: 2
- tooling/codegen: 5
- docs/hygiene: 5
- deferred-needs-real-device: 1
