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
