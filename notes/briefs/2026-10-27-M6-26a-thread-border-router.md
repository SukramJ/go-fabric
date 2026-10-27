# Brief 26a — the Thread Border Router over OTBR REST (ADR 0027; milestone 6 phase 26a)

Date 2026-10-27. **The specification is ADR 0027**
(`docs/adr/0027-thread-border-router-over-otbr-rest.md`, accepted
2026-10-25: decisions 1–7 and "Decisions taken (owner, 2026-10-23)" 1–4) —
read it whole, it is 230 lines and names every source line. Concept v10
§C wave 26a adds nothing beyond it except the reconciliation of the
dataset view with `cluster/netinfra` (wave 26-B, report
`notes/briefs/reports/26B.md` question 5: the codec gets chip's view —
first occurrence of a sub-TLV, chip's sizes, `ThreadOperationalDataset.cpp:172-301`
at `6170af84` — and `cluster/netinfra/threaddir.go` switches to it).
**Model:** Opus. Branch `m6/26a-thread-border-router` from main. One pull
request, one report `notes/briefs/reports/26a.md`. go-fabric `CLAUDE.md`
rules: matter.js only via `git -C /home/markus/Dokumente/GitHub/matter.js
show 862ad75f:<path>` (`packages/thread-br-client/src/…` is the behavioural
source for the OTBR REST client: `OtbrRestClient.ts`, `OtbrRestCapability.ts`,
`tlv/diag/*.ts`, `diagnostic/*.ts` — verify the paths with `git ls-tree`),
connectedhomeip via `git -C /home/markus/Dokumente/GitHub/connectedhomeip
show 6170af84:<path>` (`thread-border-router-management-server/
ThreadBorderRouterManagementCluster.cpp` 330 lines, `…Delegate.h:27-107`,
tests `tests/TestThreadBorderRouterManagementCluster.cpp` 524 lines);
every rule cites its line; no attribution lines; English; token rules.
Copy this brief to `notes/briefs/` in the first commit.

## Deliverables (ADR 0027)

1. `thread/` (layer `foundation`): the dataset codec moves from
   `controller/thread` (which forwards until the ADR's date);
   `archcheck` allows servers to import it. Chip's dataset view added as
   decided for 26-B; `cluster/netinfra` switches to it (and
   `BD-chip-ThreadNetworkDirectory-RulesInServer`'s sentence (3) is
   updated in the report for the owner).
2. `thread/otbr/` (layer `support`): the REST adapter after matter.js's
   `OtbrRestClient.ts` / `OtbrRestCapability.ts` — stdlib `net/http`, the
   endpoints the ADR names, the key-casing probe, `PUT /node/dataset/
   active|pending`, activation completion by polling `/node/state`
   bounded by the fail-safe; mutating operations only through the
   fail-safe-guarded path; `factoryReset` / `setCommissionerState` not
   wrapped. A fake OTBR (`thread/otbr/otbrtest`, in-process HTTP) for the
   tests, after matter.js's test fixtures where it has them.
3. `cluster/threadbr/`: `ThreadBorderRouterManagement` after chip's
   rules (cited per line) against the `BorderRouter` interface
   (`Delegate.h:63-106` in Go), `ThreadNetworkDiagnostics` read-only
   from the same interface (the 17 mandatory attributes, the provisional
   ExtAddress / Rloc16, the `MLECNT` and `MACCNT` features; `PKTCNT` and
   `ERRCNT` off — owner decision 2), the Thread variant of
   NetworkCommissioning (`cluster/core/network_commissioning_thread.go`;
   `MaxNetworks` 1; `ScanNetworks` answers the border router's own network
   from the active dataset — owner decision 3). chip's TBRM unit cases
   ported; the matter.js `thread-br-client` tests ported where they test
   the client (list the file, the count and the reason for every case left
   out).
4. The ThreadBorderRouter device type (`device/thread_border_router_gen.go`)
   binds the three servers through the declaration (`Declare.BorderRouter`
   generated like `SchedulePort`); the reference daemon gains the optional
   flag of ADR 0027 decision 6 (off by default; no chip-tool run unless
   the default composition changes).
5. Proof (ADR 0027 decision 7): unit tests of every rule, the fake OTBR
   end to end on `fabrictest`, and the virtual OTBR in CI *if* the ADR
   names a container the runner can start (say what it names; if it needs
   a dispatch-only job like the BLE one, add it dispatch-only).
6. Register: one Ahead-of-matter.js row for ThreadBorderRouterManagement
   (`BD-Thread-TBRM-ChipLogic` as the ADR calls it; marker the
   `ThreadBorderRouterManagementServer.ts:14` shell); `make ahead-check`
   green; the `by_design` text proposed. Docs: `docs/guides/thread-border-router.md`,
   `docs/matterjs-comparison.md:55` (○ → ✅) and `:119`, the ADR's status
   line "implemented in wave 26a", CHANGELOG `[Unreleased]` Added and the
   `controller/thread` forwarding note in the migration guide.
7. Guards: `make ci`, `make race` twice, `go test ./script/...`,
   `make reachability` + `cite-report` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`)
   with snapshots committed, `make ahead-check`, `make testparity-check`,
   `make archcheck`, `mkdocs build --strict`. `export GOTMPDIR=$HOME/.cache/gotmp`.
   One guard bundle at a time; another agent (wave 26-A2, `cluster/camera`)
   works in parallel — touch nothing there. Hand back once: PR number,
   HEAD, guards with results, differences from the ADR, Questions
   verbatim.
