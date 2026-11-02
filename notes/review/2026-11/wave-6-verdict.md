# W6 verdict (2026-11-01, HEAD 769ea3c2)

Theme files: `wave-6-test.md` (Opus, done), `wave-6-reg.md` (Opus, done; 926 register entries checked), `wave-6-doc.md` (Opus, done; ~3,230 references by script, ~250 claims by hand).

## Verified (Fable re-read)

| ID | Severity | Verdict |
| --- | --- | --- |
| W6-test-01 | **high** (process) | confirmed via `gh api …/branches/main/protection/required_status_checks`: `testparity`, `matter.js citations`, `chip data model cross-check`, `ble/bluez`, `ble/esphome` are not required; CI on main at 68e6201f (run 41228066743) failed only `matter.js citations` and merged. Fix: add the five to the required list (ci.yml job names). |
| W6-test-02 | medium | confirmed: run 41272352093 on main (1bafb2e9) failed `test (linux, race)`; the race is in `client/discovery/session.go:222` (context.AfterFunc hook logging after Run returned, released only at `:300`). Belongs with the W4 fixes. |
| W6-reg-01 | **high** (register) | confirmed (= W2-sec-02 root cause): `matter_behaviour_findings.md:306` FIXED claim is false; `ClosePASESessions` never had a caller in the module. |
| W6-reg-02 | medium | confirmed: `store.RemoveResumptionsByFabric` (`store/resumption.go:148`) has no caller; `node.fabricUpdated` (`node/node.go:589-601`) closes sessions but keeps resumption records; matter.js `FailsafeContext.ts:167-170` deletes them on replaceFabric. Register H1 (`:243`) claims it FIXED. Fix: call it from `fabricUpdated`; correct H1. |
| W6-reg-03/04 | medium | reviewer-confirmed: 22 by_design rows record divergences the module has since closed (7 on the security path) — register maintenance, no code. |
| W6-reg-05 | medium (decision) | reviewer-confirmed: BD-Matter-TimeSync-NotMounted is inverted — the node mounts TimeSynchronization by default, matter.js does not. Owner decision: keep (record the real divergence) or stop mounting by default. |
| W6-test-03/04/05/06/07, W6-reg-06/07/08 | medium | reviewer-confirmed (chip families not run for served clusters; label-gated certification groups; 14 coverage floors 5–13 points slack; no fuzz on the handshake decoders; 1136 unported matter.js cases; two Mirrors comments describing behaviour matter.js no longer has). |

| W6-doc-01 | medium | confirmed: `cluster/onoff/server.go:378-379` and `closuredimension_server.go:556-557, 615-619` map every port error to FAILURE, against `docs/guides/device.md:310-319` and `ports.md:15-19` (v0.15.6 fixed the other kit servers). Fix: the same `contract.StatusError` passthrough. |
| W6-doc-02 | medium | reviewer-confirmed: `matter-ecosystem-observations.md:104-124` names guards that exist only in OpenCCU-Loom. |

## Counts (from the reviewers' reports)

| Theme | high | medium | low | note |
| --- | --- | --- | --- | --- |
| test (Opus) | 1 | 6 | 6 | 5 |
| reg (Opus) | 1 | 7 | 7 | 2 |
| doc (Opus) | 0 | 2 | 23 | 2 |
| **W6** | **2** | **15** | **36** | **9** |

## Fixed in PR (W6, branch `review/w6`)

| ID | Commit | What |
| --- | --- | --- |
| W6-test-01 | — (PR 1) | the five guards are required checks on `main` since PR 1 (`matter.js citations`, `chip data model cross-check`, `testparity`, `ble/bluez`, `ble/esphome`); recorded |
| W6-test-02 | — (#361) | the discovery hook released before the outcome (W4) |
| W6-test-03 | a77a4fd7 | WASHERCTRL, DRYERCTRL, SEPR, PWRTL, EEM, EPM families with golden step counts (local run at CHIP 6170af84); `TestServedClustersHaveAFamily` holds `surface.json` to families ∪ the not-run table |
| W6-test-04 | d805ec48 | the `commissioning` group runs on every Go-touching pull request |
| W6-test-05, W6-test-13 | 0d8a7b04 | a floor more than 3 points below its measurement fails; 32 floors raised; reference-bridge 75, endpointtest 54; levelcontrol 86 (deterministic test) |
| W6-test-06 | 483451e1 | nine fuzz targets (Sigma1/3, PBKDFParamRequest, Pake1/3, Check-In, TimedRequest, StatusResponse, BTP session packets) |
| W6-test-07 | aec5feee | `testparity` ratchets ported (113) and missing (1140) |
| W6-test-08 | ff14c1b6 | the stale FixMe skips deleted (the six named plus the nine mdns anchors and three that the register named as ports); the 16 left are register `skips`, guarded |
| W6-test-09 | 5ee84619 | the three dead sentinels removed (CHANGELOG Changed); reachability ratchet 0 |
| W6-test-10 | d52eaf43 | `node:TestServedClusterRevisionAndFeatureMap` replaces the four tautological revision tests |
| W6-test-11, W6-test-12 | 12182368 | the nightly keeps a failing run's fuzz corpus; ble-virtual and otbr-virtual weekly |
| W6-test-16 | 67120c92 | `paritytest.Conformance` fails on unmodelled forms |
| W6-test-18 | a58f3d9b | the guard prose; reachability says why the examples seed no production mode; the loom set and its STRICT switch retired |
| W5-ex-12 | 957cdbf1 | the `fabrictest` seam accessors moved to `internal/fabrictestseam` (CHANGELOG Changed, migration guide row) |
| W6-reg-01, W6-reg-02 | — (PR 1) | |
| W6-reg-03, -04, -05, -06, -15, -16 | 20762247 | sixteen by_design entries retired (Retired entries table), two audit rows resolved, seven entries rewritten to their residue, the TimeSync row per the owner decision, the detached paragraph moved |
| W6-reg-07 | ba7725c6 | the unicast reply carries no P bit; the unreachable outbound masking removed |
| W6-reg-08 | — | the reviewer's (security server) |
| W6-reg-09, W6-reg-10 | a80195f3 | findings register: four fixed bullets removed, the wording and drifted lines |
| W6-reg-11 | a3840b23 | pin-lifts: the five verdicts, 39 Ahead rows, the TEVT row |
| W6-reg-12 | 1e93d385 | by_design refresh from the four maps, every matter.js line re-read at 862ad75f |
| W6-reg-13 | b03816fb | the five named Mirrors comments and the drifts of the touched files; drift ratchet 251 |
| W6-reg-14, W6-reg-17 | dbe283fe | the skipped subtest removed; the FailsafeCommissioning subtests renamed |
| W6-doc-01 | — (#361) | |
| W6-doc-02, -18 | c06eb1a9 | |
| W6-doc-03..05 | 8732c5d6 | |
| W6-doc-06..09, -19 | 6a801814 | |
| W6-doc-10..13 | c16f72f8 | |
| W6-doc-14..16, -27 | 7e2adac9 | |
| W6-doc-17 | a2b7ff94 | |
| W6-doc-20..23 | 1178b9c0 | |
| W6-doc-24, -25 | 3d03ce74 | |
| W6-doc-26 | 8343c3d4 | |

### Notes that stay open (W6)

- W6-test-14 (the class-(a) certification gaps), W6-test-15 (`time.Sleep` in tests), W6-test-17 (`err != nil`-only refusals): notes, unchanged.
- The cases item 1 excluded, none a module defect: TC-WASHERCTRL-2.2 (c; an undefined enum value answers CONSTRAINT_ERROR as matter.js does, chip expects INVALID_IN_STATE), TC-EEM-2.2 and TC-EPM-2.2 (c; the reference daemon has no EnergyReporting fake-readings test event trigger, 0x0091…), TC-PWRTL-2.2 (b; ElectricalCircuit is not in the model at the pin).
- `client/discovery` coverage stays at floor 94: its spread is goroutine scheduling in the cache loop, which no clock drives.
- pin-lifts #4777: the priming-report abort on an error status has no test (have it, untested).
- 251 drifted citations remain on the ratchet (`script/citecheck`); this PR fixed the touched files' only.
