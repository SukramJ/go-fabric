# Brief BX1 — gate-1 fixes: the kit's defaults, one decoder, one shape over every server

Date 2026-10-20. Gate 1 report `notes/briefs/reports/G1.md` (findings
G1-1, G1-2, G1-5, G1-8, the wrong docs lines of G1-3); owner decisions of
2026-10-20 (BX1–BX3 in parallel, the three interfaces on the self-held
servers in this wave). go-fabric `main` at `ab4303fd` or later. matter.js
pin `862ad75f`, read only via `git -C $MATTERJS_DIR show 862ad75f:<path>`.
Briefs BT and BS and their reports are the template; CLAUDE.md's token
and model rules apply. Branch `m4/bx1`, commits `m4 BX1: <step>`, one
pull request, no auto-merge, label `needs-chiptool` (the daemon's
composition moves: the kit's defaults change which servers a DimmableLight
mounts). Parallel to BX2 (goroutines, locks, flakes: `secure/operational`,
`controller/exchange`, `transport/tcp`, `bridge/securechannel.go`,
`cluster/core/identify.go`, `cluster/lock`'s timers, `node/ota.go`,
`cluster/core/diagnostic_logs.go`, `bridge/bdx_responder.go`, `fabrictest`,
the lock sites of `cluster/core/messages.go`, `cluster/cover`,
`cluster/energy/devicemanagement.go`, `cluster/appliance/microwave.go`,
`cluster/measurement/soil.go`, `bdx/sink.go`) and BX3 (sweeps of error
prefixes, log names, `slog.Default()`, mutex comments, the facade aliases
in `node/facade.go`). Do not touch BX2's lock regions in the cluster files
you share with it (you add interfaces and invoke switches; it moves
calls out of locks); the owner merges.

**Model:** Opus. Security paths: `cluster/core/access_control.go` (G1-8:
typed status errors in place of text) is the one touch there, a
mechanical replacement of error values — no logic change; everything
else in that file is off-limits.

## 1 The kit mounts the self-held server by default (G1-1)

- Rule: for every cluster that has a self-held server in the module —
  a `cluster/<pkg>` package exporting a `Server` type (or the named type)
  for that cluster id with its own command logic — the kit's default is
  that server, not the generated `spec.Server` stub. Today only OnOff,
  Binding and EcosystemInformation are mapped (before BT:
  `script/devicegen/servers.go:26-30`; BT removed the table because every
  accessor became the hull). Build the table back as **the list of
  self-held defaults**, generated into `device/defaults_gen.go` (or kept
  as a hand-written `device/defaults.go` if a generator adds nothing —
  say which in the report): cluster id → constructor with its default
  config. Inventory the candidates with `grep -rn '^func New' cluster/`
  and `go doc` per package: `cluster/onoff`, `cluster/levelcontrol`
  (`native.go`: the self-contained form; the port form stays for
  `WithServer`), `cluster/light` (ColorControl), `cluster/cover`,
  `cluster/thermo`, `cluster/lock`, `cluster/fan`, `cluster/pump`,
  `cluster/switch`, `cluster/valve`, `cluster/opstate`, `cluster/modebase`
  derivations, `cluster/alarm`, `cluster/closure`, `cluster/boolcfg`,
  `cluster/filter`, `cluster/media`, `cluster/appliance`, `cluster/energy`,
  `cluster/servicearea`, `cluster/core`'s per-endpoint servers. A server
  that cannot be built without a host port (a mover, a lock actuator, a
  measurement source) is **not** a default: the stub stays and the
  report lists it with the port it needs (BU's declaration will carry
  the port). A server that runs on its own state (levelcontrol native,
  onoff, colorcontrol in its self-held form, fan, modebase …) is.
- `device.New…` builds the default with the features the device type
  mandates (`WithFeatures` still overrides) and the node's clock
  (`Options.Clock`); `WithServer` still replaces it.
- Tests: `device/defaults_test.go` — for every default: the device
  mounts it, `MatterAcceptedCommands` lists the cluster's mandatory
  commands (the stub listed none), and one command executes end to end
  over `fabrictest` for the three the consumer experiment used (OnOff
  Toggle, LevelControl MoveToLevel, BooleanState read) in
  `node/kit_defaults_parity_test.go` or the existing node parity files.
  The consumer's bridge (`notes/briefs/reports/G1-consumer/bridge.go.txt`)
  is the acceptance test in spirit: after this wave a controller dims a
  `NewDimmableLight`.

## 2 One request decoder (G1-2)

- `bridge/fields_reader.go:60-124`: the generated definition decodes
  first for every cluster that has one (`spec.DecodeRequest`); the
  hand-written decoders run only for a cluster without a registered
  definition (today: which? list them in the report — likely none of the
  application clusters) and for the core commands that have no
  definition. Read what each hand decoder returns (`cluster/wire` types)
  and who consumes it: the self-held servers' `MatterInvoke` switch on
  those types. Make every self-held server accept the generated request
  type of its cluster (`<cluster>def.<Command>Request`, value and
  pointer) **as the primary form** and keep the `wire` form only as a
  deprecated branch under the README window (one `Deprecated:` note per
  wire request type, removal release named). A server whose request
  struct differs in field types from the generated one (unit, nullable
  form) converts explicitly, never by reflection.
- Tests: the typed `Handle<Command>` of the hull over a self-held server
  receives the generated request (one test per converted server, in that
  package); `bridge/fields_reader_test.go` asserts the order (definition
  first). chip-tool: `GOFABRIC_CHIP_FAMILIES=LVL,OO,CC,WNCV,DRLK,FAN,IDM
  make chiptool-families` after the switch (the harness is yours in this
  wave; BX2/BX3 do not run it), plus `make chiptool-test`.

## 3 One shape over every server (G1-5; owner decision 4)

- Every self-held server implements `spec.AttributeSetter`
  (`SetAttributes(map[uint32]any) error`: the host's own change, stored
  and reported, no Sink, no veto — `onoff.Server.Set(on)` becomes the
  implementation of its OnOff attribute; a server with transitions
  (levelcontrol, colorcontrol) sets the value directly and stops a running
  transition of that attribute, as `Endpoint.set` writes state outside a
  command), `spec.CommandRegistrar` (`Handle(cmdID, fn)`: the host
  **replaces** the server's own handling of that command, as a matter.js
  host overrides the command method; the doc comment says so; an
  unknown or unaccepted command returns an error at registration) and
  `spec.EventEmitting` (`Emit(endpoint, eventID, data)` through the
  server's event path). Where a server has a host-confirm path (`Sink`),
  it also implements `spec.BeforeWriteSetter` so the hull's `OnWrite`
  works. The nine the BT report named first: `cluster/onoff`,
  `cluster/core.Binding`, `cluster/core.EcosystemInformation`,
  `cluster/levelcontrol`, `cluster/light`, `cluster/thermo`, `cluster/cover`,
  `cluster/fan`, `cluster/pump`; then every other self-held server you
  made a default in §1. The `Underlying().(*onoff.Server)` call sites in
  tests and examples go back to the typed form.
- Tests: the hand-written typed test of BT (`cluster/spec/levelcontrol/server_typed_test.go`)
  gets a sibling per converted server, in that server's package, over
  its real server: Set/Get/OnChanged/one Handle/one Emit.

## 4 The dispatcher's text fallback (G1-8)

`endpoint/dispatcher.go:831-890`: remove `containsAny` and the string
heuristics; an error without `im.StatusCodeError` (or the cluster-status
interface) answers FAILURE. Before removing, make every error that
reached the fallback by wording a typed one: `cluster/core/access_control.go:708-783`,
`:896-908` ("resource exhausted:", …) and whatever `grep -rn` for the
heuristic's words finds in non-test code (list them in the report). The
dispatcher test that covered the heuristic asserts the typed path.

## 5 The docs lines that are wrong today (G1-3, part)

`docs/getting-started/device.md` and `docs/guides/testing-in-process.md`:
`light.OnOff().Set(on)` → the typed form; the "Two directions of state"
section says `OnChanged` fires for both directions until BU gives it an
origin; `node.Endpoint`'s doc comment loses "Brief P's device.Device …
until that lands"; `fabrictest.Options.Logger`'s doc says what nil does
(read the code). The host's-view page, the typed packages' import-path
doc and the `go doc ./device` size are BU's.

## Guards and report

`make ci`, `make race` (mandatory), `make archcheck`, `make reachability`
then `-check` (regenerate, commit), `make cite-check`, `make testparity-check`,
the families above and `make chiptool-test`. HEAD before/after and one
exit line per guard; the families' results with any `-update-chip-cases`
diff explained. CHANGELOG `[Unreleased]`: `### Changed` for the kit's
defaults (a DimmableLight now mounts `cluster/levelcontrol`; what a host
with `WithServer` sees), `### Added` for the three interfaces on the
self-held servers, `### Deprecated` for the `wire` request forms,
`### Fixed` for the decoder order and the dispatcher fallback. Report
`notes/briefs/reports/BX1.md`: **Defaults** (the table: cluster, server,
or "stub, needs <port>"), **Decoder** (what moved, who still decodes by
hand), **One shape** (per server: what Set/Handle/Emit/OnWrite do),
**Dispatcher**, **Guards**, **Differences**, **Decisions**, **Left out**,
**Questions**, `by_design` candidates (one paragraph each with citations
— a self-held server whose `Handle` replaces logic matter.js keeps is
one). Copy this brief to `notes/briefs/2026-10-20-BX1-kit-defaults.md`
in the first commit. Never add attribution lines to commits or the PR
body.
