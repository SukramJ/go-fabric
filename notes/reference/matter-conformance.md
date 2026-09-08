# Matter bridge — conformance plan

**Status:** the test list below is maintained; the version it targets is not
pinned here. The authoritative Matter revision is whatever
`parity/schema.json` records under `matter`
(`revision`, `specificationVersion`, `sourceCommit`) — it moves with every
snapshot refresh (`make generate-matter-schema`, see
[`CLAUDE.md`](../../CLAUDE.md)), so a number written into this page goes stale
within a release. At the time of writing the snapshot pins revision 1.6.0.
**Related:** [OpenCCU-Loom ADR 0012](https://github.com/SukramJ/openccu-loom/blob/main/docs/adr/0012-matter-pure-go-implementation.md),
[`conformance/`](../../conformance)

This document collects the tests that exercise this module:
automated Go tests plus manual smoke runs against real controller
hardware. Sections 1 and 2 describe what CI actually runs and when;
section 3 is a manual checklist that no release currently gates on.
Where a test is a gate, the section says which workflow enforces it.

---

## 1. Automated tests (CI)

This module *is* the Matter stack, so everything below runs here unless a
line says otherwise. The commands are written for this repository's root.

What is **not** here is the host side of a bridge — the model walk that turns a
host's devices into endpoints, and the per-device projections. In OpenCCU-Loom,
the reference consumer, those live in `internal/north/matteradapter/` and
`internal/model/`, with their own chip-tool suite under `tests/chiptool/`; they
run in that repository's CI, not this one.

### 1.1 Unit + round-trip (every PR)

`make race` — `go test -race -count=1 ./...` over the whole module. There is no Matter-only invocation because the module is Matter.

### 1.2 Golden-vector conformance (every PR)

`go test ./conformance/ -run TestTLVCoreVectors` — pinned wire
bytes for the TLV codec. It carries no build tag, so it also runs as part of
1.1; naming it separately is for the reader who wants to run just this one.
Drift in encoder ordering or decoder tolerance fails it.

### 1.3 Subscription fan-out under load (every PR)

`go test ./conformance/ -run TestLoadSubscriptionFanout`. It
carries no build tag either, so it runs on every pull request rather than
nightly — the "nightly" this section used to claim was never configured
anywhere. Failure modes: linearity drift in the attribute-change sweep, or
mutex hot-spotting.

---

## 2. chip-tool suite (build-tag-gated)

`make chiptool-test` — the Go suite behind the `chiptool` build tag, driving
the real CSA reference commissioner against the reference daemon in
`examples/reference-bridge`.

Prerequisites:

- `chip-tool` from the `connectedhomeip` repo (the workflow extracts it from
  the pinned `connectedhomeip/chip-cert-bins` image). `GOFABRIC_CHIPTOOL_BIN`
  pins the binary in CI so the job cannot pass by skipping; locally the
  `LookPath` check makes the suite optional.
- A Linux host. chip-tool has no macOS build and Docker Desktop does not
  bridge multicast, which is why this guard lives in CI and nowhere else.

`.github/workflows/chiptool.yml` runs three jobs on `ubuntu-24.04-arm`:

| Job | What it drives | Why |
| --- | --- | --- |
| `chiptool` | chip-tool → `examples/reference-bridge` | the suite proper: PASE, AddNOC, CASE, an attribute read, an OnOff command, teardown |
| `chiptool-control` | chip-tool → a CHIP reference app, **no go-fabric code in the path** | the control leg. Green control + red suite is a go-fabric defect; both red is the environment |
| `chiptool-conformance` | the CSA `Test_TC_*` YAML cases → the reference daemon | borrowed as regression cases. **Certification is a non-goal**; the PICS file is written only as far as those cases need it |

It is not a blanket per-PR gate — the runs are slow and the image pull is
~2.5 GiB. It starts nightly, on manual dispatch, on the `needs-chiptool`
label, and automatically on any pull request that touches Go code. That last
trigger is wider here than in a consuming daemon on purpose: this module *is*
the Matter stack, so almost any change to it can move commissioning. The
patterns behind it are read out of the workflow by `chiptool_trigger_test.go`
rather than restated, because a filter that stops matching fails silently.

**The suite is not a release gate.** `release.yml` is tag-triggered and does
not wait on it, so a tag can be pushed while the last chip-tool run is red.
Whether a chiptool job is a required status check in branch protection lives
in repository configuration, not in `.github/`, and is not settled here — it
would gate *merging a pull request*, never *publishing a tag*.

---

## 3. Manual smoke tests (host-level, not this module)

Pairing a real controller exercises a *bridge*, and a bridge is a host
application plus this module. The ecosystem checklist therefore belongs to the
host, not here: it needs a device model, a commissioning-window surface and a
UI to open it from. OpenCCU-Loom keeps that checklist, and what the ecosystems
were observed to do with the result is collected in
[Matter Ecosystem Observations](../../docs/matter-ecosystem-observations.md).

What this module can be smoke-tested with on its own is
`examples/reference-bridge`: start it, open a pairing window, and commission
it from Apple Home, Google Home, Alexa or the Home Assistant Matter server by
scanning the printed QR payload. That is the same path the `chiptool` job
automates, with a real ecosystem controller on the other end instead of
chip-tool. Nothing records the outcome of such a run today; if one is meant to
be durable it has to be written down somewhere.

---

## 4. Known limitations

- **No BLE pairing path.** Commissioning is on-network (DNS-SD) only, by
  design — see [`docs/matterjs-comparison.md`](../../docs/matterjs-comparison.md).
- **No controller/commissioner role.** This module is a responder; it does not
  commission other nodes.
- **Certification is a non-goal.** The CSA cases in job 3 are regression
  cases, not a certification run.
- **Subscription resumption across a host restart is not wired.**
  `store/subscriptions.go` carries the `matter_persistent_subscriptions`
  table, but no part of the subscription lifecycle writes or reads it, so a
  restart drops every subscription and controllers re-subscribe once CASE is
  re-established (`BD-Matter-SubscriptionResumption-Deferred` in
  [`by_design.md`](../parity/by_design.md)). CASE *session* resumption
  (Sigma2Resume) is a different mechanism and **is** implemented, in
  `secure/sigma`.
