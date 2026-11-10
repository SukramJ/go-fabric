# Brief — issue 481: the Go-controller legs select the reference daemon by discriminator and port

Branch `fix/481-daemon-by-port` in `../go-fabric-481`, base `main` 5c993e69 (v0.20.0). One PR,
"Closes #481". Test-only change under `internal/chiptool/` (build tag `chiptool`); no public API,
no CHANGELOG entry (not user-visible), no matter.js/chip citation needed (harness logic).
Copy this brief to `notes/briefs/2026-11-10-481-daemon-by-port.md` as the first commit.

## Finding

`TestGoControllerCommissionsReferenceBridge` (internal/chiptool/gocontroller_test.go:101) and
`TestGoControllerSubscribesReferenceBridge` (:248) take the **first** `discovery.Device` that
`FindCommissionable(ctx, discovery.ByLongDiscriminator(br.info.discriminator), …)` delivers, and the
Go test harness's `goTH.commissionable` (internal/chiptool/gocert_sc_test.go:60-77) does the same.
On a LAN with another commissionable device advertising the default discriminator 3840 (W12's
local run: `172.18.4.29:5540`, "passcode wrong") the wrong device is commissioned and the leg fails;
CI's isolated network never sees this. The daemon's bound UDP port is known (`br.info.port`,
bridge_test.go:66; ephemeral for the Go legs, bridge_test.go:223-229), and every discovered address
carries its port (`discovery.Address.Port`, `Device.Addresses`).

## Scope

1. A helper in the test package, e.g. `func isDaemon(d discovery.Device, port int) bool` — true when
   at least one of `d.Addresses` has `Port == port`. Use it in the three `found` callbacks: a device
   with the discriminator but no address at the daemon's port is **logged** (`t.Logf("ignoring a
   foreign commissionable device with discriminator %d: instance %s, addresses %v")` — for `goTH`
   use its logger) and skipped; the search continues until the daemon's record or the timeout. The
   skip message of the "not seen" path stays. `goTH.commissionable` gets the port as a parameter
   (its callers pass the daemon's; look at every caller — `grep -n 'commissionable(' internal/chiptool/*.go`).
2. A unit test for the helper that does not need the harness (same build tag is fine, or a
   `_test.go` without the tag if the helper is in a file without the tag — keep it simple: put the
   helper and its test under the `chiptool` tag, run with `-tags=chiptool -run TestIsDaemon`).
3. Doc: `internal/chiptool/doc.go` — if it describes how the legs find the daemon, add one sentence
   (discriminator **and** port, because a foreign device on the default discriminator is a known LAN
   hazard). Otherwise nothing.

Not in scope: the chip-tool legs (chipcases_test.go, familyrun_test.go) drive the chip-tool binary
with `--discriminator`; chip-tool's own selection is not ours to change (issue text).

## Guards

`make ci` (`PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`), `go vet -tags=chiptool ./internal/chiptool/`,
then locally under `flock ~/.cache/go-fabric-scratch/guards.lock` from a script file:
`go test -tags=chiptool -count=1 -run '^TestGoController' ./internal/chiptool/ -v` (the two Go-controller
legs; needs Docker + avahi as `make chiptool-test` does — read the Makefile target for the env it
sets and mirror it) and `GOFABRIC_CHIP_FAMILIES=SC make chiptool-families` **only if** `goTH.commissionable`
changed its signature (it exercises the SC family's Go-TH path); say what ran. Logs under
`~/.cache/go-fabric-scratch/481/` with a summary log (HEAD + one exit line per guard). Author ident
`SukramJ <sukramj@icloud.com>`; commit with `git -c user.name=SukramJ -c user.email=sukramj@icloud.com`;
no attribution lines; never `git add -A`; no `time.Sleep` in production code (test helpers may wait
on contexts as the existing code does). PR title "chiptool: the Go-controller legs select the
reference daemon by discriminator and port (#481)", auto-merge off, report in the fixed shape (per §,
guards with HEAD, differences, decisions, left out, questions).
