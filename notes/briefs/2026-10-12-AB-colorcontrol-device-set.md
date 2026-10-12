# Brief AB — issue #66: `ColorControlServer.Set`, the device's own colour reaches the server

Owner: Fable. Implementer: Opus. Branch `wave10/color` from
`planning/briefs-wave-10`, pushed, **no PR**. Report in
`notes/briefs/reports/AB.md`. Read first: `notes/briefs/README.md`,
`gh issue view 66`, brief AA (`2026-10-12-AA-host-ports-outside-the-lock.md`,
"The rule this brief establishes" — it binds this brief too), and the
twin to copy: `cluster/onoff/server.go:688-699` `Server.Set` (stores and
reports a device-originated value, the Sink is not called, no timer
moves). matter.js at `4633bfd7`; read it with
`git -C ../matter.js show 4633bfd7:<path>`. One commit, reachability in
it, CHANGELOG under `[Unreleased]` → Added.

## 1. `light.ColorControlServer.Set(c Color)` — option 1 of the issue

- Module: `cluster/light/colorcontrol_server.go:184-212` (`Color`,
  `ColorWriter`), `:213-250` (`colorState`, `color()`), `:427-446`
  (`SetWriter`, `SetColorWriter`, `Color()` — the host surface today),
  `:361`, `:380` (reporting through the quieters and `s.changes.Notify`),
  `cluster/light/colorcontrol_modes.go:58` `switchColorMode(st, oldMode,
  newMode)` (the Go port of matter.js's conversion),
  `cluster/light/colorcontrol_transitions.go:137-210` `applyChanges` (how
  a transition step stores and reports), `:150-175` `update` (holds
  `commit` across the writers — `Set` must **not** take `commit`, brief
  AA's rule), `:279-291` `stopHueAndSaturationMovement` /
  `stopAllColorMovement`.
- matter.js `packages/node/src/behaviors/color-control/ColorControlServer.ts`:
  there is **no** device-originated set path — a matter.js host writes the
  state attributes directly and the behavior's `$Changed` observers
  react; the only conversion between modes is `switchColorMode`
  (`:1445-1535`), which stops all colour movement first (`:1449`) and
  converts on a best-effort basis; `stopAllColorMovement` `:843-848`.
  The issue's sentence "matter.js stops its transition on an external
  $Changed" is **not** something to cite — verify whether any `$Changed`
  observer in the file stops a transition; report what you find, and in
  either case the design below stops the movement because a device-side
  change ends what the controller asked for (as `switchColorMode` does
  before it converts).
- Must:
  - `func (s *ColorControlServer) Set(c Color)`: the device's confirmed
    colour becomes the current state. `c.Mode` is the EnhancedColorMode
    the device reports (`ColorMode` is derived from it as the server
    derives it elsewhere — find and reuse that mapping). Steps, under the
    state lock only (`mu`; never `cmd`, never `commit`): stop all colour
    movement (`stopAllColorMovement`, and `endTransitions` so
    RemainingTime reports 0 — say in the report whether a running colour
    loop is stopped too; choose: it is not, the loop is a command the
    device did not end — and list it as an open question); if the mode
    changes, `switchColorMode(st, old, new)` first so the other modes'
    fields are converted as a mode switch converts them; then the new
    mode's own fields from `c` overwrite the converted ones (HS mode: hue
    + saturation, and enhancedHue = hue << 8 as the server keeps them
    coupled — check how `colorcontrol_modes.go` couples them; EHS mode:
    enhancedHue + saturation, hue derived; XY mode: x, y; CT mode:
    mireds, clamped to the physical min/max as a write would clamp — say
    what the write path does and do the same); every attribute whose
    value changed, plus ColorMode / EnhancedColorMode when they changed,
    is reported through the server's change notifier (the quieters where
    the server already uses them, so the report follows the same quiet
    rules as a transition's). The writers are **not** called.
  - A `Color` with a mode the server does not serve (no feature for it)
    is logged and ignored, as thermo's `SetSystemMode` treats an invalid
    mode (`cluster/thermo/thermostat_server.go:333-338`).
  - `Color()` returns the new value afterwards; a controller's Read
    returns it; a subscription receives the report.
  - Doc comment on `Set` states the contract: the twin of
    `onoff.Server.Set`; no writer call; movement stopped; safe to call
    from inside a writer (brief AA's rule — add a test that calls `Set`
    from inside `ColorWriter.SetColor` synchronously; it must not block,
    which `update`'s lock structure should already allow — confirm, and
    if it blocks, fix it the way brief AA prescribes and say so).
- Tests (`cluster/light`, a parity file `colorcontrol_set_parity_matterjs_test.go`):
  Set in the same mode (fields stored, reported, writer not called);
  Set that switches the mode (converted fields match what a mode switch
  by command yields for the same input — compute the expectation through
  the command path in the test, not by hand); Set during a running
  transition (movement stops, RemainingTime 0 reported, the final colour
  is the device's); Set with an unserved mode (ignored, logged); Set from
  inside the writer (no deadlock, 1 s deadline); every existing test green.
  `CGO_ENABLED=1 go test -race -count=1 ./cluster/light/...`.
- Not in scope: option 2 (`ColorSource` read-through) — state in the
  report why `Set` suffices for the issue's two losses (live read,
  proactive report) and what a read-through would add.

## 2. Report

The usual; open questions; by_design candidates with citations (expected:
"a device-side colour change stops the colour movement but not the colour
loop", "Set converts the other modes as switchColorMode would", with the
matter.js lines), not written.

## Guards

`make ci`, race on `cluster/light`, `make reachability` in the commit,
`make reachability-check`. The chip-tool harness is **in use by brief AA's
agent**; do not run `make chiptool-test` or the families — the owner runs
them on the integrated tranche. No chip-tool test, PICS slice or golden
changes. `secure/`, `commissioning/`, `by_design.md`, the findings
register untouched.
