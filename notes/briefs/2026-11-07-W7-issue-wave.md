# Brief W7 — the issue wave after v0.18.0: #449, #450, #438, #439, #440, #441, #445 in one pull request

Date 2026-11-07. Owner decision 2026-11-07 (wizard, "eine große Welle"): the seven open issues
land in one pull request, one implementing agent, one commit per issue step, issues in the order
below (consumers first). Branch `w7/issue-wave`, worktree `../go-fabric-w7`, base `origin/main`
(after #414 merged — merge `origin/main` before the PR if it moved, never rebase; regenerated files
by regenerating `make cite-report reachability surface`; CHANGELOG conflicts keep both sides).
English; no attribution lines; commit with `git -c user.name=SukramJ -c user.email=sukramj@icloud.com`
(`git var GIT_AUTHOR_IDENT` = `SukramJ <sukramj@icloud.com>`). **Model:** Opus; Fable line-reviews
the controller/commissioner citations (#449) and the `cluster/core` GeneralDiagnostics lines (#439)
before the merge. Pins: matter.js db021da0 (`git -C ../matter.js show db021da0:<path>`),
connectedhomeip 6170af84 (`git -C ../connectedhomeip show 6170af84:<path>`). Environment:
`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; guard bundles and chip-tool runs under
`flock ~/.cache/go-fabric-scratch/guards.lock`, in the background, summary logs in
`~/.cache/go-fabric-scratch/w7/` with a HEAD line; logs by grep, never cat. No `time.Sleep`/`time.After`
in production code. New citations → `make cite-report && make reachability` in the same commit;
ratchets (`script/coverfloor/floors.go`, reachability, testparity register) move in the same commit
with the justification. `golangci-lint run --timeout 40m ./...` (not `make lint`). Do not touch
`secure/*`, `commissioning/`, `client/handshake`, ACL, or the four hand-written `cluster/core` servers
(GeneralCommissioning, OperationalCredentials, AdministratorCommissioning, AccessControl) — the
device-side GeneralCommissioning TC server is read, not changed, by #449.

Read each issue once (`gh issue view N`) for the host's wording and the owner's decision comments;
the verified lines are in the per-issue sections below — cite those, not the issues' unpinned ones.

## 1 — #449 Commissioner: the device's TC requirement as a typed result

Owner decisions: read in the info step, typed error right after it (before ArmFailSafe/AddNOC),
`Deadline` as data; `PairOptions.TermsAndConditions` first-class (the `Edit` seam stays).
Full brief with verified lines: `notes/briefs/2026-11-07-W7-449-tc-requirement.md` —
read it; it is the section's content. Harness: `GOFABRIC_CHIP_FAMILIES=CGENTC make chiptool-families`.

## 2 — #450 node: an interface allow-list for the mDNS advertiser

Owner decisions: by interface name, within the default container filter, `Network.Listen`
untouched. Full brief: `notes/briefs/2026-11-07-W7-450-mdns-interfaces.md` — read it.
Do this section together with §5 (#440), both in `mdns/`; one `make chiptool-test` after both.

## 3 — #438 im: a lone list ADD answers BUSY as matter.js does

matter.js at db021da0 `packages/node/src/node/server/InteractionServer.ts:511-531` (a chunked
write element with a list index not preceded by a replace of the same attribute in the same
interaction → BUSY, the "previous processed path" marker unchanged), marker scoped to the whole
interaction across chunks `:456-458`, `:529` — re-read these lines first and re-pin if they moved.
Module: `im/write.go` and `endpoint/dispatcher.go` (the list-append path). matter.js's order: this
BUSY precedes the writability check — a lone ADD to a read-only list (FixedLabel) is BUSY, not
UNSUPPORTED_WRITE: check the FixedLabel entry in CHANGELOG 0.18.0 `### Fixed` ("list ADD to
FixedLabel's LabelList is refused with UNSUPPORTED_WRITE") and the test behind it; it changes to
BUSY with this issue — CHANGELOG `### Changed` says so. Tests: un-skip the T3-P3-1 subtest in
`node/label_parity_matterjs_test.go`; an `im/*parity_matterjs_test.go` case for the marker across
two chunks; close findings-register row T3-P3-1 (`notes/parity/matter_behaviour_findings.md`).
Harness: `GOFABRIC_CHIP_FAMILIES=ULABEL,IDM,ACE make chiptool-families` (ACE because the write
path moved — CI-only guard lesson).

## 4 — #439 cluster/core: GeneralDiagnostics fault attributes and events

matter.js `packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts` at
db021da0: ActiveHardwareFaults, ActiveRadioFaults, ActiveNetworkFaults and the three
`*FaultChange` events (current and previous lists) — read the server and the five missing
`GeneralDiagnosticsServerTest.ts` cases first, cite the lines. Module: `cluster/core/general_diagnostics.go`
serves neither; add a `contract/` port (stdlib only) through which a host supplies the fault lists;
the root server mirrors them and emits the change events; attributes stay absent when no port is
attached (as today — the FeatureMap/attribute list and the harness PICS move only with the port).
Tests: the five cases by meaning (`script/testparity/register.json` row), DGGEN family once
(`GOFABRIC_CHIP_FAMILIES=DGGEN`). CHANGELOG `### Added`; docs page of the root clusters.

## 5 — #440 mdns: the rest of matter.js #4844

`MdnsServer.ts`, `InterfaceResponder.ts` at db021da0: the RFC 6762 §6 one-second rule with the
deferred repeated answer, same-turn batching of announcements into one packet per interface
(`MdnsServer.ts:209-214`), once-per-outage notices per interface and address family; prune the
link-state cache on every interface listing (`MdnsServer.ts:405-411`); pass the interface list down
instead of a `net.Interfaces` call per interface under the responder's mutex. The goodbye scope on
an addressless interface is already fixed by #446 (`mdns/answer.go`, `BD-Matter-MdnsFollowsAdvertisedAddresses`)
— verify, do not redo. Timing on the responder's clock. Tests: the ten `MdnsServerTransportTest.ts`
cases by meaning (register row). CHANGELOG `### Changed`; docs mDNS page. Then, with §2 done,
**`make chiptool-test` under the lock** (commissioning over real mDNS on this host) — mandatory
before the PR; a red run is fixed, not explained away.

## 6 — #441 client/discovery: port CommissionableMdnsScannerWindowChangeTest.ts

`packages/protocol/test/mdns/CommissionableMdnsScannerWindowChangeTest.ts` and the scanner it
exercises (`packages/protocol/src/mdns/` at db021da0): the commissioning window opening, changing
or closing under a running scan (discriminator and CM TXT values changing). Port by meaning against
`client/discovery`'s commissionable browser; fix what differs (citing the lines). Register row
planned → ported/partial with justification; CHANGELOG line only if behaviour changes.

## 7 — #445 the remaining device-side Matter-time readers

Follow #444/#447's participant interface (`node/matter_time.go`: `AttachMatterTime`,
`MatterTimeChanged`, `RequiresMatterTime`; `clock.MatterTime`, `clock.MatterTimeOf`). Readers and
chip authorities are in the issue's table (6170af84 lines — re-read them): `im/eventlog.go:202`,
`cluster/filter/filter.go:401`, `cluster/core/diagnostic_logs.go:305`,
`cluster/energy/devicemanagement.go:314`, `commoditytariff.go:280`, `evse.go:321`. One hook, no
second clock port; whether a reader re-evaluates on `MatterTimeChanged` is decided per reader after
chip (DEM pause/forecast timers: read `DeviceEnergyManagementCluster.cpp:449, :698`);
`RequiresMatterTime` only where chip's Startup refuses. The eight duplicated 946684800 offsets
(#445 comment) collapse onto `clock.MatterTimeOf`/the participant. Tests: a fabrictest per reader
group (event log; ResourceMonitoring; DiagnosticLogs; energy) where SetUTCTime moves the root's
UTCTime and the stamp follows. `BD-chip-TimeSynchronization-Features` lists every reader.
CHANGELOG `### Changed`.

## Guards (final, under the lock, HEAD in the log)

build, vet, gofumpt, `go test -count=1 ./...`, `go test -race` of the touched packages,
`golangci-lint run --timeout 40m ./...`, `make cover-check`, `make cite-check`,
`make reachability-check`, `make ahead-check`, `make apidiff-check` (additive only — a Break needs
the owner), `mkdocs build --strict`; the chip-tool runs named per section (CGENTC; ULABEL,IDM,ACE;
DGGEN; `make chiptool-test`). Then push, `gh pr create` ("Closes #449, closes #450, closes #438,
closes #439, closes #440, closes #441, closes #445"; English; no attribution), no auto-merge.

## Report (fixed shape)

Per issue: what landed (file:line), the matter.js/chip lines followed, tests and harness evidence;
guards with HEAD and exit codes; differences from this brief; decisions; left out; questions;
by_design candidates (expected: `BD-Matter-Commissioner-TermsRequirementRead`,
`BD-matterjs-mdns-interface-allowlist`, both Ahead rows; #445 extends `BD-chip-TimeSynchronization-Features`).
