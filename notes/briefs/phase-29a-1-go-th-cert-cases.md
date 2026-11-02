# Brief: phase 29a, wave 1 — the Go controller as test harness: certification cases matter.js automated, device side

Milestone 8 (Proof), phase 29 (`docs/roadmap.md` §"Milestone 8: Proof";
concept v13 §C.1). Branch `m8/phase-29a-1` from `main`. One PR, title
"chiptool: the Go controller as test harness — the IDM, SC, CADMIN, ACT, G,
LVL, S and SWTCH cases matter.js automated run against the reference daemon
(phase 29a-1)". CHANGELOG under `[Unreleased]` → Added. No release (patch
versions inside M8 are the reviewer's after each wave).

## Why, and the one fact that frames it

matter.js automated 68 certification cases in
`support/chip-testing/test/cert/TC-*.test.ts` (862ad75f). **In most of
them matter.js's controller is the DUT and a device app is the TH**
(`support/chip-testing/README.md:112-118`); only SC-8.x, BDX-1.2/2.2,
SU-2.x/4.1 and BIND run with the device as DUT. Our harness runs the
other way: chip-tool / CHIP Python as controller, the reference daemon as
DUT (`internal/chiptool/doc.go:1-9`). Owner decisions 2026-11-02: phase 29a
ports the **device-DUT** reading of these cases with **our Go controller
in the TH role** (the step list and the TH-side expectations of the TS file
are what our daemon must satisfy; the DUT-controller actions of the TS
file are what our Go TH performs); phase 29b (later, own brief) runs them
in matter.js's native direction with our controller as DUT. 44 of the 68
ids are "manual" rows of `docs/certifiability.md` today ("a manual case:
the CHIP test plan has a test-lab operator perform it, there is nothing to
automate", `familyrun_test.go:422-426`); this wave makes the first 19 run.

The read map `~/.cache/go-fabric-scratch/read-phase29-cert.md` has
`file:line` for everything below (sections 1 framework, 2 the cases by
family, 3 our harness and controller entry points, 4 status rows). It is
a map, not a citation: re-read every line you cite.

## Deliverables

1. **The Go-TH leg** in `internal/chiptool/` (build tag `chiptool`, like
   the rest): a case registry `goCertCases` keyed by TC id (`IDM/1.1`
   shape as the families use, `familyrun_test.go:411`), each case a Go
   function over a `goTH` context: a started reference daemon
   (`startBridge`, `bridge_test.go:219`; flags `main.go:112-139`), a Go
   controller built as `gocontroller_test.go:46-152` builds it
   (`controller.New` is the facade, `controller/controller.go:230`; use
   it rather than the hand-wired stack if it covers the case), the
   daemon's app-pipe (`control.go:26-84`) for simulated events, the
   DNS-SD browser (`client/discovery.Scanner`, `scanner.go:333`;
   `mdns.OpenQueryConn` for raw records), and a step recorder: every step
   records `pass | fail | skipped(reason)` with the TS source line, as
   `cert-context.ts:103` and `tc-support.ts:104 record` do. PICS: the
   daemon's generated PICS (`testdata/gen_pics.py`, `doc.go:59-73`) decide
   per-step `pics` expressions the TS file states; a step whose PICS is
   false is `skipped` with the expression, never silently passed.
2. **Status integration**: the golden `testdata/chip-cases.golden.json`
   gets the Go-TH cases with their step counts (`caseCounts`,
   `chipcases_test.go:166-176`), `TestCertifiabilityDocument`
   (`certifiability_doc_test.go:50`) renders them as passed with the
   source "Go controller as TH, after matter.js `TC-X.test.ts`" and drops
   their "manual" rows; the `(c) harness` count on the status page falls
   accordingly; `docs/certifiability.md` §"How the status is produced"
   gets a paragraph on the leg. Correct `docs/roadmap.md` phase 29: the
   golden has 44 manual rows among the 68, not 45.
3. **The cases of this wave** (TS file = the step list; the map's §2 has
   lines per step):
   - IDM: 1.1, 1.3, 2.1, 3.1, 4.1, 5.1, 6.3, 6.4 (the TH sends the
     Invoke/Read/Subscribe/Write shapes the TS steps describe; the DUT's
     answers are the TS "expected" text; `all-clusters` app in matter.js →
     the daemon's reference composition here).
   - SC: 4.2 (the daemon's commissionable advertisement with an unknown
     TXT key present on the network: the TH advertises the alias as
     `mdns-alias.ts:33` does, the DUT must keep its own records sound;
     read the TS for what the device side has to show), 4.8, 5.3, 6.1.
   - CADMIN-1.17, ACT-3.2 (Actions on the bridge composition), G-3.2
     (group keys and ACL via generic `Node.Invoke`/`Write`, map §3.6),
     LVL-8.1, S-3.1 (ScenesManagement), SWTCH-3.2 (`SimulateMultiPress`
     etc. through the pipe, `control.go:140-148`).
   Each Go case cites its TS file and the step lines; a step the daemon
   cannot do is a `(b)` or `(a)` finding in the report, not a hidden
   skip — a real defect of the daemon or a server is fixed in this PR only
   if it is small and outside the security paths; otherwise it is a
   findings-register bullet (`notes/parity/matter_behaviour_findings.md`,
   "Certification harness" section) and the case records `fail` with the
   finding id.
4. **Make and CI**: `make chiptool-gocert` (the leg alone;
   `GOFABRIC_GOCERT=IDM,SC` narrows) and part of `make chiptool-test`'s
   scope; `.github/workflows/chiptool.yml`: the leg runs in the `chiptool`
   job (same triggers, `:190-276`); the required check names stay.
5. **Docs**: `internal/chiptool/doc.go` (the leg, the direction, why the
   TS files are read mirrored), `docs/certifiability.md` prose,
   CHANGELOG Added line, `docs/matter-parity-contract.md` guard table row.

## Rules

- Author identity before every commit: `git var GIT_AUTHOR_IDENT` →
  `SukramJ <sukramj@icloud.com>`; no trailers.
- `export PATH=$HOME/go/bin:$PATH`; guard bundles under
  `flock ~/.cache/go-fabric-scratch/guards.lock` in the background, logs
  `~/.cache/go-fabric-scratch/p29a/` with a summary log; read logs by grep.
  The chip-tool harness is single-user: the leg, `make chiptool-test` and
  any family run alone under the flock.
- Read slices, not files. Cite matter.js as `support/chip-testing/test/cert/TC-X.test.ts:<line>`
  at 862ad75f; chip via `git -C ../connectedhomeip show 6170af84:<path>`.
- Do not touch `secure/`, `commissioning/`, `cluster/core`, ACL. Do not
  edit `notes/manual-test-plan-*.md`.
- Guards: `make ci`; `CGO_ENABLED=1 go test -race -count=1 -tags=chiptool
  ./internal/chiptool/...` (compile and the untagged tests);
  `make chiptool-gocert`; `make chiptool-test`; `go test ./internal/chiptool/
  -run TestCertifiabilityDocument` (regenerate with
  `-args -update-certifiability-doc`); `make cite-check`,
  `make reachability-check`, `make apidiff-check`.
- Commit in small steps; rebase onto `origin/main` before the PR (another
  PR may land meanwhile; keep both CHANGELOG line sets); push with
  `--force-with-lease` after a rebase; open the PR without auto-merge;
  report in the fixed shape (per deliverable: commit, test; per case: step
  verdicts and the TS lines; guards with HEAD and exit; differences from
  the brief; decisions; left out; open questions; findings filed) and list
  every matter.js/chip citation you wrote.
