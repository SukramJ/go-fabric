# ADR 0028 — The test site paces the stack by its counted in-flight work, and paced code holds no wall-clock deadline

- **Status**: Accepted (2026-10-25; brief CG, pull request 262)
- **Date**: 2026-10-25
- **Related**: [ADR 0026 — architecture principles](./0026-architecture-principles.md)
  (clock ownership, the logger rule), concept v4 (the test foundation,
  `docs/history/concept-v4-test-foundation.md`), `docs/guides/testing-in-process.md`
  (the pacing model as a host reads it), `notes/briefs/2026-10-24-CG-m4-flake-sweep.md`
  and its report, `notes/parity/by_design.md` entries
  `BD-Matter-SitePacingByCountedWork`, `BD-Matter-SiteBuildWithoutBudget`,
  `BD-Matter-MdnsSocketReadDeadline`, `BD-Matter-BridgetestWallClock`.

## Context

`fabrictest` runs a device and a controller of this module in one process
on a fake clock and advances that clock only when the stack is quiet, the
way matter.js's `MockTime.resolve` moves time only when no task is ready
(`packages/testing/src/mocks/time.ts` at `862ad75f`). Until brief CG the
site judged "quiet" by the process: ten milliseconds with no datagram sent,
nothing pending in the in-memory network and no runnable or running
goroutine beyond a standing count read from the Go runtime's scheduler
metrics. Two things were wrong with that on a loaded CI runner.

First, paced production code still held deadlines on the machine's clock:
fifteen `context.WithTimeout` sites in `bridge/` (the store, advertising,
the OTA link, group messaging, the subscription re-establishment, the
handshake) and the node's stop. While the site paced the fake clock slowly
under load, those real deadlines expired, and a re-establishment or a stop
failed that passed on a quiet machine.

Second, the process is the wrong unit. Another test's goroutines made a
site look busy forever (starvation), and a goroutine of the stack that was
blocked on something the fake clock did not own was invisible, so the clock
moved past the instant it needed. The failures of 2026-10-24 were of this
family: a read sent on a session the device had just closed, reports that
never arrived in thirty seconds, a transition's end not observed in ten.
Each passed locally and under `go test -count=50`; each failed under eight
parallel race-enabled processes.

JavaScript can observe "nothing is ready" because it has one thread. Go
cannot observe it per test. What the stack can state about itself is the
work it has accepted and not finished.

## Decision

1. **Paced code holds no deadline on the wall clock.** Every component with
   an injected `clock.Clock` builds its deadlines with
   `clock.WithTimeout` / `clock.WithDeadline` on that clock. A guard
   (`clock/nowalldeadline_test.go`, run with the `clock` package's tests)
   refuses `context.WithTimeout`, `context.WithDeadline`,
   `time.After`, `time.NewTimer`, `time.NewTicker`, `time.Sleep` and
   `time.AfterFunc` in the non-test code of the paced packages. Its allow
   list names the kept sites with their reason: the two mDNS socket read
   deadlines (the kernel's) and `bridge/bridgetest` (a wall-clock test
   helper over real sockets).
2. **The stack counts its work.** `internal/inflight` is a counter group
   a component enters and leaves at the points where work is accepted and
   finished: a datagram from read to handler return, an exchange delivered
   to its handler, a report queued and not yet sent, a device callback
   queued, a node start, stop and build. Production cost is a nil check;
   the group travels with the clock (`inflight.WithGroup`), so a component
   needs no new parameter.
3. **The site judges quiet by the counted work.** `fabrictest` declares the
   stack quiet when the counts and the in-memory network stood still for
   `Settle` (ten milliseconds). Counted work that stands still holds the
   clock for at most one second (`holdBudget`, the role of matter.js's
   `MAX_HOST_ASYNC_MS`), then the site logs what it abandons and moves on;
   `node.build` is exempt, because a construction waits for the CPU, not
   for a clock. The process-wide goroutine check survives only as a
   fifty-millisecond grace on an in-memory site and as the rule on a
   `Loopback` site, whose real sockets do work the counters cannot see.
4. **A test moves the clock through the site.** `Site.Step(d)` advances
   under the quiet protocol; `Site.Run` and `Site.Eventually` advance to
   the next timer. A test never calls `Fake().Advance` while the stack may
   be working.
5. **Flakes are reproduced, not retried.** `make stress` runs a test under
   `golang.org/x/tools/cmd/stress` with eight parallel race-enabled
   processes; `make stress-flakes` runs the named list, and the nightly
   `flakes` job keeps it green. A test that fails under stress has a root
   cause in the code or the test, never a retry.

## Consequences

- A host that passes its own `Clock` now also paces the deadlines that were
  wall-clock before: `subscription.ReestablishTimeout`, the store timeouts,
  the advertise timeouts, the stop timeout. On `clock.System{}` nothing
  changes.
- `fabrictest` tests that advanced the fake clock from inside a condition
  moved to `Site.Step` / `Eventually`. The guide names the two rules.
- The counted set is a contract between the stack and the site: a new
  path that accepts work and waits on a clock enters a counter, or the
  site may move the clock past it. The `inflight` package doc lists the
  counters and what each catches; a flake that survives stress is the
  first place to look for a missing one.
- The runtime-metrics check is no longer the arbiter, so parallel tests of
  one package no longer starve each other's sites; `holdSite` stays only
  because sites still share one process's CPU.
- Divergences from matter.js are recorded in `by_design.md`: the site
  counts work where MockTime observes a single thread; the build is
  exempt from the budget; the mDNS socket deadlines and bridgetest keep
  the wall clock.

## Alternatives considered

- **Keep the process heuristic and raise the quiet period.** Slower and
  still wrong: a blocked goroutine is invisible at any period, and a
  neighbour test's goroutines still starve a site.
- **Serialise every site test in the process.** Hides the starvation, not
  the invisibility; halves the throughput of the suites that matter most.
- **Run CI with fewer parallel packages.** Moves the failure rate, not the
  cause; the fix belongs in the code, not in the runner flags.
- **Per-test goroutine tracking through the runtime.** Go offers no
  per-goroutine ownership the site could read without instrumenting every
  `go` statement; counting the work at its seams is the smaller contract.
