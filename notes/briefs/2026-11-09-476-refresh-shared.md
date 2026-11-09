# Brief — issue 476: TestServiceRefreshShared joins the flight deterministically

Branch `fix/476-topology-refresh-shared` in `../go-fabric-476`, base `main` 35e49fa1 (v0.19.2). One PR,
"Closes #476". Test-only wave; no wire change, no chip-tool run, no CHANGELOG entry (not user-visible).
Analysis by Fable 2026-11-09, in the issue.

## Finding (from #476)

`controller/topology/service_internal_test.go` `TestServiceRefreshShared` (:329-342) starts two
`Refresh` goroutines and orders only "a second read started" (`recv(t, b.started)`, :336), which the
first flight's own second member satisfies (`started` is buffered 16, :237). Under load the second
goroutine enters `Service.Refresh` (`service.go:328-343`) only after the two releases let the first
flight finish and clear `s.inflight` (`runRefresh`'s defer, :345-352); it opens a second flight whose
reads block on `b.release` forever, and the flight's deadline is `DefaultRefreshTimeout` (30 s) on the
harness's fake clock, which nothing advances → 10-minute package timeout (run 42655147792).

## Scope

### §1 A join seam (`controller/topology/service.go`)

Split `Refresh` so the join is a call of its own: `func (s *Service) joinRefresh() *flight` — under
`s.mu`, returns the in-flight flight or creates one and starts `runRefresh` (the body of :328-335
today); `Refresh` becomes `f := s.joinRefresh()` + the existing select on `f.done`/`ctx.Done()`.
No behaviour change; unexported; the doc comment of `Refresh` stays. If `flight` needs the topology
readable after done, it already has `f.topo`.

### §2 The test (`service_internal_test.go`)

Rewrite `TestServiceRefreshShared` to call `joinRefresh()` twice **synchronously** before anything is
released: assert the two pointers are the same flight (the coalescing property, asserted before any
blocking step so a regression fails at once, not by timeout); then `close(b.release)` (not two sends),
wait `<-f.done`, and assert the shared topology (`len(f.topo.Nodes) == 2`). Keep the port comment
(`NetworkTopologyServiceTest.ts:721-746`, matterjs-server at 0bd06f22) — the property ported is the
same, the ordering is now the test's, not the scheduler's. Keep a public-path check too: after the
join assertion, one `Refresh(ctx)` call through the public path may be added only if it cannot block
(e.g. after `close(b.release)` and `<-f.done`, a second `Refresh` must start a NEW flight — optional).

### §3 The same assumption elsewhere in the package

Read every test in `controller/topology/*_test.go` that starts `Refresh` in a goroutine beside a
`blocking` fixture (grep `go func() {.*Refresh`, `newBlocking`) and check whether its ordering relies
on goroutine scheduling (the `:299` concurrency test uses `RefreshTimeout: 20ms` + `h.fake.Resolve`,
which looks sound — confirm). Fix only what has the same defect; report the rest as checked.

### §4 Fail fast instead of a 10-minute hang

Where a test waits on a flight that could stall, bound the wait by the test's own deadline on the
fake clock (`h.fake.Resolve(ctx, …)` with `context.WithTimeout` as `:307-311` does), so a regression
is a failure in seconds. Do not change `DefaultRefreshTimeout` or production timing.

## Guards

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; `go test -race -count=20 ./controller/topology/`
(must pass every time; also run once with `GOMAXPROCS=1` and once with `-cpu 1,2,8`), `make ci`
(lint incl. gofumpt, cover-check for `controller/topology` — the floor must hold or move with its
ratchet in the same commit), `make reachability-check` (an unexported method does not move it; confirm).
Logs under `~/.cache/go-fabric-scratch/476/` with a summary log (HEAD + one `exit` line per guard),
guard bundles under `flock ~/.cache/go-fabric-scratch/guards.lock` from a script file. No
`time.Sleep`/`time.After` in tests either (the harness clock is injected). Author ident
`SukramJ <sukramj@icloud.com>`, commit with `git -c user.name=SukramJ -c user.email=sukramj@icloud.com`,
no attribution lines, never `git add -A`, Apache header on any new file. PR title
"test: TestServiceRefreshShared joins the refresh flight deterministically (#476)", auto-merge off,
report in the fixed shape (per §, guards with HEAD, differences, decisions, left out, questions).
