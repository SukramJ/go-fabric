# Brief AR — the media and content clusters, the three media device types, the media families (ADR 0022; concept v2 phase 9)

Owner: Fable. Implementer: Opus (the only agent on the repo while you
work; you run `make chiptool-test` once and the narrowed media families
with the fixture update — the harness is single-user, one run at a
time). Branch `wave24/media` from `planning/briefs-wave-24`, pushed,
**no PR**. Report `notes/briefs/reports/AR.md`. Read first:
`notes/briefs/README.md`, [ADR 0022](../../docs/adr/0022-media-and-content-clusters.md)
(the decision; its Context has the measured facts), [ADR 0017](../../docs/adr/0017-generated-default-cluster-server.md),
[ADR 0013](../../docs/adr/0013-generated-cluster-definitions.md),
`cluster/spec/server.go` (`NewServer`, `Source`, `Sink`,
`Server.Handle`, `CommandHandler`, `ErrFabricScoped`, `defaultValue`),
`cluster/core/wifi_network_diagnostics.go` (a host port wrapped around
a generated server — the pattern), `cluster/core/binding.go` (a
core-held server for a fabric-scoped list — Messages' pattern),
`script/clustergen/clusters.go`, `script/devicegen/devicetypes.go`
(`excluded`: the three media types at :113-116; `CastingVideoClient`
stays excluded), `device/device.go` (`defaultFactories`,
`ErrNoDefaultServer`), `examples/reference-bridge/{main,control,wiring}.go`
and `wifi.go`/`ota_provider.go` (the TESTING-ONLY flag pattern and the
app-pipe), `internal/chiptool/doc.go`, `families_table_test.go`
(`knownProblems`), `testdata/reference-bridge.pics`, `gen_pics.py`,
report AM (the DGWIFI family's PICS work) and report AQ.

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
the fifteen element files under `packages/model/src/standard/elements/`
(kebab-case: `media-playback.element.ts`, `keypad-input.element.ts`,
`content-launcher.element.ts`, `application-launcher.element.ts`,
`application-basic.element.ts`, `account-login.element.ts`,
`audio-output.element.ts`, `channel.element.ts`,
`target-navigator.element.ts`, `media-input.element.ts`,
`low-power.element.ts`, `wake-on-lan.element.ts`,
`content-control.element.ts`, `content-app-observer.element.ts`,
`messages.element.ts`), the device files `basic-video-player.element.ts`,
`casting-video-player.element.ts`, `content-app.element.ts`, the
generated behaviours under `packages/node/src/behaviors/<kebab>/`
(fourteen stubs; `low-power/LowPowerServer.ts:13-25` is the one with
logic: `sleep()` emits `enterLowPowerMode`), and the devices under
`packages/node/src/devices/`. chip's media YAML cases under
`../connectedhomeip/src/app/tests/suites/certification/Test_TC_{ALOGIN,APBSC,APPLAUNCHER,APPOBSERVER,AUDIOOUTPUT,CHANNEL,CONTENTLAUNCHER,KEYPADINPUT,LOWPOWER,MEDIAINPUT,MEDIAPLAYBACK,TGTNAV,WAKEONLAN}_*.yaml`
(server-side `.S` cases; the `.C` ones are disabled) and
`src/python_testing/TC_{MEDIAPLAYBACK_6_11,MEDIAPLAYBACK_6_12,CONTENTLAUNCHER_10_8,10_9,10_10,ALOGIN_12_3}.py`;
chip's `examples/tv-app` and `tv-casting-app` at the harness pin
(`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>` —
the sparse checkout may lack them) are the behaviour reference for what
the model does not say (what a command answers, which status).
Guards: `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, `make chiptool-test` once, then the media families
with the fixture update (§5). Export `PATH=$HOME/go/bin:$PATH` and
`CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip` first; a
`make ci` failing only on golangci-lint's timeout after "0 issues" is
rerun as `make lint`; a `node/` live test failing once with "Could not
connect to device" is rerun alone. One commit per §, reachability in
each. Allowed paths: `script/clustergen/clusters.go` and
`cluster/spec/<the fifteen>` (generated), `cluster/media` (new),
`cluster/core/messages.go` (+ tests) **or** `cluster/media/messages.go`
if `spec.Server` can be extended — prefer extending `spec.Server` for
fabric-scoped *lists* in `cluster/spec/server.go` only if the change is
small and the owner's `ErrFabricScoped` tests still hold for structs;
say which you chose and why, `script/devicegen/devicetypes.go` and
`device/` (generated), `examples/reference-bridge` (§4),
`internal/chiptool` (§5: table, PICS, golden, `knownProblems`, the
daemon flags the families job passes), `docs/certifiability.md` (§5:
the family rows and one paragraph), `script/coverfloor/floors.go`,
`CHANGELOG.md`. `bridge/`, `node/` (except mounting what §4's daemon
needs through the existing topology seams — say so), `endpoint/`,
`contract/`, `controller/`: read, not changed. `notes/parity/` and the
ADRs are the owner's.

Out: CastingVideoClient and every client-side (`.C`) case, the Python
cases that need PICS past the pin (record them as not applicable with
the reason), ContentControl's and Messages' families (chip has none at
the pin), an ecosystem proof (phase 5), HTTPS/DRM/streaming of any kind
(the ports say what the host reports, nothing plays here).

## Owner's decisions

- **D1 — one package, `cluster/media`, one host port per cluster.** Each
  of the fourteen stub clusters gets `media.NewX(cfg XConfig) (*XServer,
  error)` wrapping `spec.NewServer` on the generated definition: the
  config carries the host port (an interface the host implements:
  `Playback` with `Play/Pause/Stop/StartOver/Previous/Next/Rewind/FastForward/SkipForward/SkipBackward/Seek`
  and the state the attributes report; `Channels` with the channel list,
  lineup, current channel and `ChangeChannel(ByNumber)/SkipChannel`;
  `Keypad` with `SendKey`; `Targets` with the target list and
  `NavigateTarget`; `Inputs`/`Outputs` with their lists and
  `Select/Rename`; `AppLauncher` with the catalog list and
  `Launch/Stop/HideApp`; `ContentLauncher` with `LaunchContent/LaunchURL`
  and the accept header / supported streaming protocols; `AccountLogin`
  with `GetSetupPIN/Login/Logout`; `ContentControl` and
  `ContentAppObserver` with their commands; `WakeOnLan` and
  `ApplicationBasic` attribute-only; `LowPower` with `Sleep`), the
  features to advertise, and the initial attribute state (list- and
  struct-typed mandatory attributes have no rendered default: the config
  requires them where the definition mandates them — `ErrMissingState`
  naming the attribute). A command's status comes from the port's error
  (`media.StatusError{Status}`; a plain error is FAILURE) and the
  response struct from the port's return; cite the element file's
  command and response for each.
- **D2 — Messages is a core-held server** for its fabric-scoped list
  (`cluster/core/messages.go` after `binding.go`'s shape: per-fabric
  read filtering, `PresentMessagesRequest`/`CancelMessagesRequest`
  through a host port), unless extending `spec.Server` for fabric-scoped
  lists is a small change that keeps `ErrFabricScoped` for structs — your
  call, reported.
- **D3 — the three device types are generated** (`devicegen`: move them
  from `excluded` into `committed`; the conditions `PhysicalInputs`,
  `ContentAppPlatform`, `ObserverClient` stated by the host as the
  root's are — read how the generated constructors take conditions and
  say whether they can express these three).
- **D4 — the reference daemon** gets `-media` (TESTING ONLY): endpoint
  "player" (BasicVideoPlayer: OnOff, MediaPlayback with a simulated
  transport whose state the app-pipe steps, KeypadInput, MediaInput with
  two inputs, AudioOutput, Channel with three channels and a lineup,
  TargetNavigator, LowPower, WakeOnLan, ContentLauncher — the clusters
  chip's `tv-app` exposes on its player endpoint; read its endpoint
  composition and mirror what the families need) and endpoint "app"
  (ContentApp: ApplicationBasic, ApplicationLauncher with a catalog,
  AccountLogin, KeypadInput); the player's ApplicationLauncher
  `StopApp` moves the app's ApplicationBasic.Status (APPLAUNCHER-3.8).
  The families job starts the daemon with `-media`; the PICS slices
  change for the new endpoints (regenerated by the update run).
- **D5 — the families:** every server-side media YAML case joins the
  table; PICS per the cases' keys (`gen_pics.py` derives the device's
  own; `reference-bridge.pics` the hand declarations — read the DGWIFI
  work in report AM); a case whose failure is a recorded gap of chip's
  checker or the model goes into `knownProblems` with its reason, as
  the existing entries do; the golden counts come from your update run.
  Say in the report which cases pass, which are known problems and why,
  which are not applicable.
- **D6 — no new dependency.**

## §1 — the definitions (`script/clustergen`, `cluster/spec`)

Add the fifteen to the clustergen list, generate, commit;
`go test ./script/clustergen/... ./cluster/spec/... ./schema/...` and
`make chipdm-check`.

## §2 — `cluster/media` (fourteen servers) and Messages (`cluster/core` or `media`)

D1, D2. Per cluster: a parity test against the snapshot (revision,
features, attributes, commands — the generator's `spectest` helpers)
and behaviour tests per command (the port called with the decoded
fields, the response encoded, a `StatusError` answered with its status,
no handler for an optional command the port does not implement →
UNSUPPORTED_COMMAND as the standard server does, the attribute state
from the port, `ErrMissingState`). LowPower's `Sleep` → the port, as
`LowPowerServer.ts:13-25`. Messages: per-fabric filtering tests after
Binding's.

## §3 — the device types (`script/devicegen`, `device/`)

D3. `go test ./script/devicegen/... ./device/...`, `make chipdm-check`;
a device-level test that each of the three mounts its mandatory servers
through the media package's constructors (via `defaultFactories` or
`WithServer` — read which the generator expects and say so).

## §4 — the reference daemon (`examples/reference-bridge`)

D4. The app-pipe commands (CHIP-style JSON) a family needs (e.g. a
playback state change, an input selection) — read the YAMLs first and
list the commands you added. Document the flag as TESTING ONLY.

## §5 — the media families (`internal/chiptool`, `docs/certifiability.md`)

D5. Run `GOFABRIC_CHIP_FAMILIES=<the media families, comma-separated> go test
-tags=chiptool -count=1 -timeout=2h -run '^TestChipCertificationFamilies$'
./internal/chiptool/... -args -update-chip-cases` (never with
`GOFABRIC_CHIP_CASES`), read the diff of `testdata/pics/` and
`chip-cases.golden.json`, commit them; then the same run without the
flag must pass. `make chiptool-test` once (the whole suite with the
daemon's new flag). `docs/certifiability.md` §Families gets the media
rows and one paragraph (what runs, what is a known problem, what is not
applicable); the table test computes the counts.

## Report

As AK–AQ: per §, guards with HEAD, brief lines that differ from the
files (element files, chip), decisions, left out, questions, `by_design`
candidates (not written), the family results. Floors: `cluster/media` 85.
