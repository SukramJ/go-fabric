# Phase 0 — fixes for the W4/W5 register (review II)

Worktree `/Users/markus/Documents/GitHub/go-fabric/.claude/worktrees/agent-a0ad069e5a99af1ef`,
branch `worktree-agent-a0ad069e5a99af1ef`, base `95e03096`. Brief:
`phase-0-reg-w4w5.md` (main checkout). Not pushed, no pull request.

This file was meant for the main checkout's `notes/review/2026-11-13/`;
the agent's worktree isolation refuses writes there, so it sits in the
worktree at the same relative path, untracked (not part of the four
commits).

```
2443c047 controller/commissioner: the CSR nonce marker is cited where matter.js draws it at the pin, ControllerCommissioningFlow.ts:1412 at db021da0 (review II, W4-ctlsec-01)
8ff69e50 controller: the OTA comments describe the provider on the controller's own endpoint tree, not the retired own node (review II, R2-P0-doc-C04)
2db1283f test: every SQLite test database is opened by store.OpenSQLite or a net/url DSN, and script/sqlitedsn refuses a concatenated file: URI (review II, R2-P0-test-C02)
2a0dec86 examples/reference-bridge: the banner and the topology block read Node.Topology and Node.Endpoints, the daemon calls no Bridge accessor (review II, W5-ex-10 gap 1)
```

## Implemented

### W5-ex-10 gap 1 | 2a0dec86

- **Files**: `examples/reference-bridge/main.go`, `examples/reference-bridge/wiring_test.go`.
- **What**: the banner counts `len(n.Topology().Devices)` (one entry per
  `topo.Bridged()` endpoint, `node/lifecycle.go:190-196`); `printTopology`
  prints `topologyBlock(n.Endpoints())`. The package doc no longer lists
  the endpoint listing among what reaches below the facade and no longer
  calls the remaining two "a gap the facade does not cover yet" (gaps 2
  and 3 are still named as reaching below the facade; their disposition is
  the coordinator's).
- **Output unchanged** (measured): the block prints the first entry of each
  endpoint's Descriptor DeviceTypeList; on the daemon's full fleet (45
  endpoints) it equals the tree's `Endpoint.DeviceType` for every endpoint
  (probe run, 0 differences). Source for bridged endpoints and parts:
  `endpoint/helpers.go:21-41` (`listedDeviceTypes` lists `DeviceType`
  first when `Part || DeviceType != 0`). For the root and the Aggregator it
  is measured by the test only, not read from their Descriptor
  construction. Edge not covered by the fleet: a bridged endpoint with
  `DeviceType == 0` printed `0x0000` before and prints its first listed
  type now.
- **Test, red → green**:
  - `TestTheDaemonCallsNoBridgeAccessor` (AST walk over the non-test
    files, refuses any `.Bridge()` call). Red before the fix:
    `main.go:570:8 calls .Bridge(), which is below the facade`,
    `main.go:603:14 …`. Green after.
  - `TestTopologyBlockIsTheAssembledTree` holds the block against the old
    rendering from `Bridge().Topology()` and the banner count against
    `len(topology.Bridged())`. It could not exist before the refactor
    (`topologyBlock` is new), so it is a characterisation test; mutation
    check: printing the last DeviceTypeList entry instead of the first
    makes it fail, the fix makes it pass.
- **CHANGELOG**: none (the daemon's output is unchanged).
- **Doc follow-up**: none found — `docs/` does not describe this gap
  (`grep -rn "Node.Bridge\|Bridge()\|reach below\|printTopology"` over
  `docs/guides`, `docs/getting-started`, `docs/*.md` and the example's
  README: no statement about the daemon's listing).

### R2-P0-test-C02 | 2db1283f

- **Files**: 24 test files (the 25 `sql.Open("sqlite", "file:"+…)` sites
  moved by script, asserted per site against its pragma class),
  `endpoint/sqlitestore/allocation_test.go`, new
  `script/sqlitedsn/sqlitedsn_test.go`, `script/coverfloor/floors.go`
  (floor entry for the new test-only package, as `errprefix`/`slogkeys`).
- **Per pragma class** (what each site carried → what it calls now):
  - `foreign_keys(1)&busy_timeout(5000)` (14 sites) → `store.OpenSQLite(path)`: same pragma set (`store/sqlite.go:29`).
  - `busy_timeout(5000)&journal_mode(WAL)&foreign_keys(1)`, literal or the
    `connectionPragmas` constant (7 sites) → `store.OpenSQLite(path, "journal_mode(WAL)")`:
    same set. The three `connectionPragmas` constants are gone; `store`'s
    tests keep the rationale comment on a `walMode` constant.
  - `busy_timeout(5000)&journal_mode(WAL)` without foreign keys (2 sites:
    `controller/commissioner/fabric_persist_test.go:98`,
    `controller/client/registry_live_test.go:140`) → `store.OpenSQLite(path, "journal_mode(WAL)")`.
    **Changed**: foreign keys are now on — what `controller.OpenDB` sets (`controller/open.go:36`).
  - no pragmas (2 sites: `cluster/lock/doorlock_auth_test.go:26`,
    `examples/reference-controller/cli/cakey_test.go:21`) → `store.OpenSQLite(path)`.
    **Changed**: foreign keys and the busy timeout are now on.
  - `endpoint/sqlitestore/allocation_test.go:137` (`TestBusyWriterIsWaitedOut`,
    table-driven pragmas, one case `busy_timeout(0)`): **not** moved onto
    the opener — the opener always adds `busy_timeout(5000)` and I did not
    look up which of two `busy_timeout` pragmas the driver applies. The
    DSN is built with `net/url` (`url.URL{Scheme: "file", OmitHost: true, Path, RawQuery}`,
    the shape of `store/sqlite.go:52-57`), pragmas byte-identical.
  - Every moved site now also pre-creates the file 0600 (`store/sqlite.go:47`).
  - The `_ "modernc.org/sqlite"` blank imports of the touched test files
    are left in place (harmless; `store` imports the driver).
- **Guard** `script/sqlitedsn` (shape of `script/errprefix`: package doc
  with the rules, AST walk from the module root, a floor on parsed files):
  refuses a `+` whose left operand is a string literal starting `file:`
  and a `fmt.Sprintf` whose format starts `file:` with `%s`/`%v`/`%q`
  (`script/aheadcheck/surface_test.go:207,349` use `%d` for an in-memory
  name and pass by that rule). Test and generated files are judged;
  `notes/`, `docs/`, `.claude/`, `.git/`, `testdata` are skipped. The one
  exception is counted per file (`keyReferences`): the keyfile signer's
  `file:<path>` key reference — `controller/ca/keyfile/keyfile.go` 1,
  `keyfile_test.go` 1, `examples/reference-controller/cli/cakey_test.go` 3
  (`--ca-key`). The opener needs no exemption (it does not concatenate).
- **Test, red → green**:
  - On the tree before the move: `TestNoFileURIIsConcatenated` FAIL with 26
    sites — 25 lines plus `cakey_test.go holds 4 "file:" sites, keyReferences counts 3`
    — the report's count.
  - After the move: `ok`.
  - Plant `store/zz_plant_test.go` with `sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "plant.db")+"?_pragma=foreign_keys(1)")`:
    FAIL `store/zz_plant_test.go:10: "file:" + …`; plant removed: `ok`.
  - `TestConcatenationsAreFound`: the matcher's own table (12 cases).
- **Claim re-read**: `testing.go:1612` (go1.27.4, `$(go env GOROOT)`) —
  `const allowed = "!#$%&()+,-.=@^_{}~ "` — holds. I did not reproduce a
  truncated path with a `#01` subtest.
- **CHANGELOG**: none (tests and a guard).
- **Doc follow-up**: none.
- **For the coordinator**: `script/reachability/inventory.json` lists the
  script packages (`:243-258`) and does not hold `script/sqlitedsn` — not
  edited (out of bounds); `make reachability` / `reachability-check` not run.

### R2-P0-doc-C04 | 8ff69e50

- **Files**: `controller/ota.go`, `controller/controller.go`,
  `controller/softwareupdate/script.go`, `provider.go`, `manager.go` —
  comments only.
- **What**: the two passages the report names (`ota.go:27-35`,
  `script.go:92`) and the same statement elsewhere in the two packages:
  `ota.go:121-123` (an orphaned `// OwnNode is …` doc comment with no
  declaration under it — removed), `controller.go:214-215`, `:290-292`,
  `:559-560`, `script.go:23`, `provider.go:42-43` (`controller.OwnNodeSpec.Provider`),
  `:62-63` (`controller.OwnNode.BDX`), `manager.go:494`. Each now names
  what the code does: `openOTA` builds the manager and
  `mattercore.NewOTAProvider`, `serveEndpoints` mounts it at
  `OTAProviderEndpoint` (`serve.go:147-153`), `mgr.SetProvider(RootNodeID, OTAProviderEndpoint)`
  (`ota.go:106`), `wrapProviderCommands` installs the scripted commands
  (`ota_provider.go:81,160`), `retireOwnNode` migrates (`ota_provider.go:253`).
  No new matter.js or chip citation; the existing ones (at `862ad75f`)
  were not re-read and not touched.
- **Left as they are** (historical, past tense, accurate):
  `controller/ota_provider.go:28-34,53`, `controller/groups.go:24,55`,
  `controller/client/registry.go:452`.
- **Test**: none (comments); `go vet` and the two packages' tests green.
- **CHANGELOG**: none.
- **Doc follow-up**: see "Noticed" 1 — the reference controller's README
  and CLI still tell the own node; not touched.

### W4-ctlsec-01 marker drift | 2443c047

- **Files**: `controller/commissioner/flow.go:1003`,
  `controller/commissioner/csr_nonce_chip_test.go:64-65`.
- **What**: `ControllerCommissioningFlow.ts:1395 at 862ad75f` →
  `ControllerCommissioningFlow.ts:1412 at db021da0`.
- **Read in the pinned tree** (`scratchpad/pin/matter.js/packages/protocol/src/peer/ControllerCommissioningFlow.ts`):
  `:1412` is `fields: { csrNonce: this.fabric.crypto.randomBytes(32) },`
  and the file's only `csrNonce` (grep: one hit); `:1406-1437` destructure
  only `nocsrElements` and `attestationSignature` and decode only
  `certSigningRequest` — "never reads it back" holds. The chip half of the
  sentence, unchanged: `DefaultDeviceAttestationVerifier.cpp:868-870` in
  the pinned tree is the size check, the comment and
  `csrNonceSpan.data_equal(csrNonce)`.
- **Unverified**: that the two scratchpad trees are at `db021da0` /
  `6170af84` — taken from the brief; git commands outside the worktree are
  refused for this agent, so I did not run `rev-parse` there.
  `parity/schema.json:7` names `db021da06b…` as the repository's pin. The
  old `:1395 at 862ad75f` was not checked (no tree at that commit).
- **Test**: `TestCertificatesCsrNonceEchoed` green (comment change).
- **CHANGELOG**: none.
- **For the coordinator**: the register row (`notes/parity/by_design.md:130`,
  `:5545`) still says `:1395` — yours. The old citation was commit-qualified
  and so not in `script/citecheck/drift.json`; two citations moved from
  `at 862ad75f` to `at db021da0` — `make cite-check` not run (needs the
  clones), whether `summary.md`'s "Checked at their own `at <sha>`" row
  moves is unverified.

## Not implemented

Nothing in scope was dropped. By instruction not touched: W5-ex-10 gap 2
(STRIKE) and gap 3 (owner), W4-ctlsec-08, W4-ctlint-02 / R2-P0-bound-C01,
W5-ex-07, W5-api-18, R2-P0-conc-C03.

One part of R2-P0-test-C02 deviates from the proposed fix ("the helpers
call `store.OpenSQLite`"): `allocation_test.go:137` gets a `net/url` DSN
instead, for the reason above.

## Guards (run in the worktree, each exit 0)

| Command | Result |
| --- | --- |
| `gofumpt -l examples script/sqlitedsn script/coverfloor store endpoint/sqlitestore node device bridge controller cluster/lock` (at HEAD 2443c047) | no output |
| `go vet ./store/... ./endpoint/sqlitestore/... ./node/ ./device/ ./bridge/ ./bridge/bridgetest/ ./controller/ ./controller/dcl/ ./controller/commissioner/ ./controller/client/ ./controller/client/sqlstore/ ./cluster/lock/ ./examples/... ./script/sqlitedsn/` | exit 0 |
| `go vet ./controller/ ./controller/softwareupdate/ ./controller/commissioner/ ./script/coverfloor/` | exit 0 |
| `go test -count=1 ./examples/...` (after W5-ex-10) | ok (reference-bridge 14.1 s) |
| `go test -count=1 ./script/sqlitedsn/ ./store/... ./endpoint/sqlitestore/... ./cluster/lock/ ./controller/client/sqlstore/ ./controller/dcl/ ./bridge/bridgetest/ ./device/` | ok |
| `go test -count=1 ./node/ ./bridge/ ./examples/reference-bridge/ ./examples/reference-controller/cli/` | ok (node 98.6 s) |
| `go test -count=1 ./controller/ ./controller/commissioner/ ./controller/client/` | ok (client 86.1 s) |
| `go test -count=1 ./controller/softwareupdate/`, `./controller/` (after doc-C04) | ok |
| `go test -count=1 ./controller/commissioner/` (after ctlsec-01) | ok |
| `go test -count=1 ./script/errprefix/ ./script/sqlitedsn/ ./script/coverfloor/` (at HEAD) | ok |
| `go test -count=1 -run TestLicenseHeaderOnEveryGoFile .` (at HEAD) | ok |

Not run: `make` targets, repo-wide tests, `-race`, `golangci-lint`,
`make reachability-check`, `make cite-check`, the chip-tool suite (Linux
only; it parses the topology block — `internal/chiptool/bridge_test.go:538-595`,
`gocert_test.go:551` — whose text is held unchanged by the test above, not
by a harness run).

## Noticed (new, not fixed)

1. **The reference controller still documents and offers the retired own
   node.** `examples/reference-controller/README.md:264-273` (section "The
   controller's own node": "`Controller.OwnNode`, started by
   `node/controllernode`"), `:37`, `:284-286`; comments in
   `cli/group.go:89,141,228`, `cli/stack.go:52,59,186`. And the flag
   `--node-listen` is dead: `nodeListen` is declared, defaulted and
   registered (`cli/cli.go:92,101,115`) and read nowhere else
   (`grep -rn "nodeListen\|node-listen"` over the worktree's Go files:
   those three lines, plus `cli/main_test.go:303`, which passes the flag).
   The code itself is on the phase-37 shape (`cli/self.go:17-22`,
   `cli/stack.go:162-167`). Same class as R2-P0-doc-C04, plus a
   user-visible flag without effect.
2. **The topology block names one device type per endpoint** although its
   line reads "device types" and the suite's parser reads a
   comma-separated list (`internal/chiptool/bridge_test.go:538-594`:
   `endpointFor`, `endpointAfter`, `reTopologyLine`).
   `Node.Endpoints` now has the whole DeviceTypeList at hand; printing it
   would change which endpoint `endpointFor` resolves for a type listed
   second somewhere, so I kept the output — a decision for whoever can run
   the harness.
3. `node/node_test.go:33` cited `examples/reference-bridge/persist.go:34`
   for the daemon's pragmas; that line is inside `restoreBlind` today.
   Corrected in 2db1283f to `node/store_path.go` (the comment sat on a
   moved site).
