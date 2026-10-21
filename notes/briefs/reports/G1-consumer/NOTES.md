# G1 consumer test: notes

Branch `gate1-consumer` from `origin/m4/phase-20b` (a71223c2). Sources I used:
`go doc -all` of `node`, `device` and `contract`, one step further (`endpoint`,
`clock`, `cluster/spec/{onoff,levelcontrol,booleanstate}`, `cluster/onoff`,
`cluster/spec.Node`, `tlv.Element`, `im.ConcreteAttributePath`,
`controller/interaction.AttributeData`), `go doc ./fabrictest` and its symbols,
`docs/getting-started/device.md`, and `docs/guides/{testing-in-process,concurrency,ports}.md`.
I read no module source and no forbidden page.

Result: `go build`, `go vet`, `go test -run . -count=1` and `go test -race` all
pass. The binary prints its pairing codes, opens its window, and exits with 0
about 1 s after SIGINT. Two experiments in a throw-away test (since deleted)
showed things the docs do not say. They are marked **[probe]** below.

## 1. Where I had to guess

1. **The program on the getting-started page is not on the page.**
   `docs/getting-started/device.md` § "The program" consists of
   `--8<-- "examples/quickstart-device/main.go"`. In the repository, and to
   anyone not reading the rendered site, that is a snippet directive and not
   code, and `examples/` is off limits. The one complete program the docs
   offer was therefore unavailable, and I put the bring-up together from
   `go doc node`.
2. **Commissioning salt and iterations.** `node.Commissioning` requires
   `PBKDFIterations` and `Salt`, and no doc gives a value a host should
   choose. I guessed `node.PBKDFIterationsMin` and a 16-byte ASCII literal.
   `NewCommissioningCredentials` exists, but it draws random values, and the
   host would then have to persist them itself, which is wrong for a
   "config literal".
3. **Which fields are mandatory.** device.md lists the `Validate` checks
   (VendorID, ProductID, NodeLabel, a store). I guessed that
   `BasicInformation.VendorName/ProductName/SoftwareVersion` are optional and
   set them anyway.
4. **How to tell a controller's change from my own.** device.md § "Two
   directions of state" presents `Device.OnChanged` as the
   "Controller to device" direction. **[probe]** It also fires for the
   host's own `LevelControl().Set(...)`: one callback per host Set. A wall
   switch that moves the level every second would log a "controller set
   the level" line every second. I guessed a filter: remember the last level
   the host set and ignore a change to that value. This is racy in
   principle. OnOff has a clean controller-only path (`onoff.Config.Sink`
   through `WithServer`), but LevelControl has no documented equivalent.
5. **Whether a controller can dim the light at all.** **[probe]** A
   `MoveToLevel` invoked on `device.NewDimmableLight`'s default LevelControl
   server answers **UnsupportedCommand**. With
   `LevelControl().HandleMoveToLevel(fn)` registered, the same invoke answers
   **InvalidCommand** and `fn` is never called. I could not find out why
   without the source: maybe my `levelcontroldef.MoveToLevelRequest{Level: 100}`
   payload is encoded wrongly, maybe the handler is not consulted. The
   `levelcontrol.NewNativeSource` named in `docs/guides/ports.md` is probably
   the intended server. No allowed page says how to mount it on a
   `DimmableLight`. My bridge therefore logs level changes, but no
   controller can currently cause one. The task's test does not need it,
   because it only toggles.
6. **Is `Set` on the typed wrappers safe from my goroutine, and is it
   reported?** device.md promises this for `onoff.Server.Set(bool)`. It
   promises nothing for `levelcontroldef.Server.Set(Patch)` or
   `booleanstatedef.Server.Set(Patch)`. I assumed both. The test shows the
   value is read back over the wire. I did not test whether a subscription
   gets a report.
7. **The value type for `WithInitial`.** `map[uint32]any` gives no hint
   whether CurrentLevel takes `uint8`, `uint64` or a pointer. I avoided the
   option and set the initial level with a `Set` call before the node is
   built (`NewDevices`). I also guessed that a `Set` before `node.New` /
   `Start` is allowed. It is.
8. **Does the BooleanState StateChange event have to be emitted by
   the host?** `booleanstatedef.Server.EmitStateChange(endpoint uint16, …)`
   wants the endpoint number, which a `device.Device` does not expose
   (only `fabrictest.Device.EndpointOf` has it). I guessed that the event is
   optional (feature `ChangeEvent`) and do not emit it.
9. **How to test *my program* with fabrictest.** `fabrictest` builds its own
   `node.Node` from `Topology`, so my `node.Config` literal (store path,
   identity) cannot be under test. I guessed that a host splits device
   construction (`NewDevices`) from node bring-up (`run`) and hands only the
   endpoints to `Topology.Bridged`. Nothing in the guide says so.
10. **How the wall-switch goroutine runs under fabrictest's fake clock.**
    I made it take a `clock.Clock` (`clock.Or(nil)` is the system clock).
    In the test I do not start the goroutine. Instead I call the step
    functions inside `pair.Device.Act`, because a ticker on the fake clock
    advanced by `Run`/`Eventually` would move the level between reads. I
    guessed that `Act` is the right place for local changes. The guide's
    snippet does this, but device.md says `Set` is safe from any goroutine
    anyway.
11. **The reported value shape.** To read a uint I guessed
    `AttributeData.Value.El.Uint` (`spec.Node` → `tlv.Element`) from the
    guide's `.El.Bool`. That was two packages beyond `fabrictest`.

## 2. Names I could not find, or found under a surprising name

- **`light.OnOff().Set(on)`** (device.md, "Device to controller"): on a
  `*device.DimmableLight`, `OnOff()` returns `*onoffdef.Server`
  (`cluster/spec/onoff`), whose `Set` takes a `Patch`, not a bool.
  `Set(bool)` is `cluster/onoff.Server.Set`, a different package with the
  same package name `onoff`.
- **The package name differs from its import path.** `go doc` prints
  `onoffdef.Server`, `levelcontroldef.Server` and `booleanstatedef.Server`.
  The import paths are `cluster/spec/onoff` and so on, and the package
  clauses say `package onoff`. The `…def` names are aliases the device
  package chose, and I had to invent the same aliases. `go doc ./device`
  never states the import path; I found it with `go list -f '{{.Imports}}'`.
- **`endpoint.StringKey`**: `device.Options.StableKey` has type
  `device.SourceKey` (an alias), but the ready-made implementation lives
  only in `endpoint`. No `device.StringKey` exists, so a device-only host
  imports `endpoint` just for this.
- **`im.ConcreteAttributePath` and `im.ConcreteCommandPath`** with four
  `Has*` booleans to set by hand. fabrictest has no
  `fabrictest.AttributePath(ep, cluster, attr)` helper, so I wrote one.
- **The endpoint number of a device**: there is no
  `Device.Endpoint()`/`Node.EndpointOf(key)` on the production side. Only
  `fabrictest.Device.EndpointOf` has it.
- **The import path of `im` in the guide**: the guide snippet uses `im.` and
  `onoff.` without imports. `onoff` exists twice (`cluster/onoff` and
  `cluster/spec/onoff`), and both have `ClusterID` and `CmdToggle`.

## 3. Two ways to do one thing

- **Observing changes:** `Device.OnChanged(clusterID, fn(attrIDs))` versus
  the typed `light.OnOff().OnChanged(fn(state, changed))`. I took the first
  because device.md names it. The second would have saved the `Get()`.
- **Refusing or observing commands:** `onoff.Config.Sink` (through
  `WithServer(onoff.New(...))`), the typed `OnOff().HandleOn/HandleOff/HandleToggle`,
  and `OnOff().OnWrite`. It is unclear whether `HandleOn` replaces the
  default server's logic or adds to it.
- **Setting OnOff:** `cluster/onoff.Server.Set(bool)` versus
  `onoffdef.Server.Set(Patch)`.
- **A contact sensor's state:** `ContactSensor.BooleanState().Set(Patch)`
  versus `WithMeasurement(contract.BoolMeasurementSource)` plus a
  `ChangeNotifier`. I took the typed `Set`.
- **Initial state:** `WithInitial(map[uint32]any)` versus a `Set` before
  start.
- **Running:** `Node.Run(ctx)` versus `Start` + wait + `Stop`. I took `Run`.
- **Clock:** `device.Options.Clock` per device versus `node.Config.Clock`.
  The two must be kept equal by hand, and fabrictest wants `site.Clock()`
  in the device options.

## 4. Where the docs contradict `go doc`

- device.md "Device to controller: call `light.OnOff().Set(on)`" does not
  compile for a device-layer light (see §2). testing-in-process.md § Time
  repeats it: `plug.OnOff().Set(true)`.
- device.md places `Device.OnChanged` under "Controller to device". In
  fact it fires for host changes too ([probe]). `go doc` only says "the
  attributes … that changed".
- `fabrictest.Options.Logger`: "nil discards them". Yet my test run with
  `Options{}` prints about 20 `INFO matter.opcreds.cmd…` lines to stderr
  (`-v` output). Something in the device logs to `slog.Default` past the
  node's logger.
- `node.Endpoint`'s doc: "Brief P's device.Device satisfies it; until that
  lands, any type with a Spec method does". This is stale wording in a
  public doc (it has landed), and it names an internal brief.
- device.md's constructor paragraph says each mandatory cluster gets "a
  default server". For DimmableLight's LevelControl, that default answers
  UnsupportedCommand to MoveToLevel ([probe]). Nothing in `go doc device`
  warns that the default is inert.

## 5. Size and reading time

- `bridge.go`: 217 lines (177 without blank and comment lines).
  `bridge_test.go`: 111 lines (96).
- Reading before the first line of code: about 5 minutes of agent wall
  clock, roughly 2,500 lines of `go doc` and guide text over about
  25 tool calls. The whole run, probes included, took 9 minutes. A human
  reader would need far longer: `go doc -all ./device` alone is 5,388
  lines, and almost all of it is generated device-type tables.

## 6. The three changes that would have helped most

1. **Make the getting-started program readable where the page is read.**
   Inline the quickstart's code in `docs/getting-started/device.md` (or make
   the snippet render in the repo view as well). Add a second example with
   a dimmable light and a sensor that covers the wall-switch direction and
   the controller direction, plus its fabrictest test. That is this task,
   and it is the bridge most hosts write.
2. **One clear host-side API for each direction on the typed wrappers.**
   For example, `light.LevelControl().Set(...)` (host change, documented as
   reported and goroutine-safe), plus a controller-only hook,
   `OnCommand`/`Sink`, for *every* actuator cluster, not just OnOff. Fix
   device.md's `OnOff().Set(on)`. Either make `Device.OnChanged` carry an
   origin (host or controller), or document that it fires for both. Make
   `device.NewDimmableLight`'s default LevelControl actually execute
   MoveToLevel, or state in `go doc` which server to mount
   (`levelcontrol.NewNativeSource`?) and how.
3. **A consumer-shaped surface that needs fewer packages.** Today the
   program needs `node`, `device`, `endpoint` (StringKey), `clock`, three
   `cluster/spec/*` packages under invented aliases, and the test also
   needs `im`, `fabrictest`, `controller/interaction`, `cluster/spec` and
   `tlv` to read one bool. Concretely: alias `device.StringKey`, state the
   import paths of the typed `…def` packages in `go doc ./device`, and add
   fabrictest helpers such as `fabrictest.ReadBool/ReadUint(ctx, pair, key,
   cluster, attr)` that take the stable key instead of an `im` path with
   four `Has*` flags. Also document how a host tests *its own* bring-up:
   split devices from `node.Config` (§1.9).

(The two Go files are kept as `.go.txt` so they are evidence, not part of the module build; the test passed on `main` at `ab4303fd`.)
