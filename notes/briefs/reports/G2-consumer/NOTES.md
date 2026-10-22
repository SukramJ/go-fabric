# Gate 2: the context-free consumer

Base: `origin/main` at `c9e7c473` (#213 merged). Read: `go doc -all` of
`node`, `device`, `contract`, `fabrictest`, `controller/client`, `schema`,
`cluster/spec/{onoff,levelcontrol,booleanstate,thermostat,doorlock}`;
`docs/getting-started/*.md`, `docs/guides/*.md`, `examples/host-view/`,
`README.md`. Nothing else, apart from compiler errors and test runs.

Programs (kept as evidence, not built; they ran in `examples/g2consumer/`):

- `main.go.txt` — (a) the bridge: dimmable light dimmed every second,
  contact sensor, thermostat (setpoint from the host's `heating`, setpoint
  changes into its `Sink`), door lock (`bolt` as `StateSource`).
- `main_test.go.txt` — (a) the `fabrictest` test: commission, read all
  four, MoveToLevel, write the setpoint, LockDoor (timed), origins.
- `controller_test.go.txt` — (b) the controller over `controller/client`:
  `Controller.CommissionNode`, `Node.Subscribe` on OnOff and CurrentLevel,
  `Node.Read` + `Cluster.Decoded` for the setpoint.

Result: `go test -count=1 ./examples/g2consumer/...` ok (5.3 s), also
under `-race`. Both tests pass only because they assert what actually
happens where it differs from the docs (points 1, 2, 3).

## Defects (behaviour the docs promise and the module does not deliver)

1. **LevelControl's `OnChanged` never fires.** `main.go.txt:176` subscribes
   `b.light.LevelControl().OnChanged`; neither the host's own `Set`
   (`main.go.txt:235`) nor a controller's MoveToLevel calls it. The untyped
   `Device.OnChanged(levelcontroldef.ClusterID, …)` is silent too, and
   `OnChangedAsync` returns no error. Same device, `OnOff().OnChanged`
   fires. Docs: `docs/getting-started/device.md:163-170` ("Both fire for
   every change, whoever made it"), `go doc ./cluster/spec/levelcontrol
   Server.OnChanged` ("after each change the server reports"),
   `go doc ./device Device.OnChanged` ("a cluster … whose server notifies
   nothing, never calls fn" — the only hint, and it is not on the typed
   accessor). Workaround: `HandleMoveToLevel` in the declaration
   (`main.go.txt:144-149`), which replaces the kit's MoveToLevel logic
   (transitions, OnOff coupling) to see the command at all.

2. **The host's `Set` of CurrentLevel never reaches a subscriber.**
   `controller_test.go.txt:94-113`: after `b.stepLevel()` the
   subscription's report count stays at 2 (priming + the OnOff change),
   even after `site.Fake().Advance(2 * time.Second)` past the quieter
   reporting window; a `fabrictest.Read` of the same attribute returns the
   new value at once (`main_test.go.txt:70`). OnOff on the same
   subscription is reported (`controller_test.go.txt:88`). Docs:
   `docs/getting-started/device.md:178-182` ("The value is stored and
   reported to subscribers"), `go doc ./cluster/spec/levelcontrol
   Server.Set`. Likely the same root as 1 (CurrentLevel is a "Q"
   attribute, `contract.SelfReportedAttributeLister`). For a bridge this
   is the Apple-Home "level never updates" bug. Workaround in the test:
   `Node.Read` refreshes the image (`controller_test.go.txt:105`).

3. **A controller's write of a thermostat setpoint is stored but not
   reported to `OnChanged`.** The write reaches `heating.SetSetpoint`
   (`main.go.txt:66`) and `Thermostat().Get()` serves 2250 afterwards
   (`main_test.go.txt:118`), yet `Thermostat().OnChanged`
   (`main.go.txt:184`) sees only the host's own `Set` (`main_test.go.txt:
   136-142`, assertion pinned to the observed list). Docs: `contract`
   `OriginControllerWrite` ("a controller wrote the attribute"),
   `docs/guides/host-view.md:57-61`. Not checked whether a subscriber sees
   the write; observed with a `Sink` declared.

## Names I could not find, and guessed

4. **No scalar value for a write.** `fabrictest.Write` and
   `client.Node.Write` take a `spec.Encodable`; for an int16 attribute no
   facade package offers one. `docs/getting-started/controller.md:177-179`
   says "a generated type, or a TLV tree through `interaction.Raw`" — the
   generated packages have no scalar types, and `interaction.Raw`'s TLV
   tree is undocumented here. I wrote my own `EncodeTLV` with a guessed
   `enc.PutInt(tag, int64(v))` on `tlv.Encoder` (`main_test.go.txt:27-31`),
   which happened to compile. A developer without luck needs `go doc
   ./tlv`, a package the facade never names.

5. **Reading a value out of `fabrictest.Read`.** The result is
   `interaction.AttributeData`; only `examples/host-view/main_test.go:49`
   shows `got.Value.El.Bool`. `El.Uint` and `El.Int` (`main_test.go.txt:
   70, 78, 82`) are guesses that compiled. `client/interaction` and `im`
   are imported by every wire helper but documented nowhere I was allowed
   to look (`go doc ./fabrictest Read`).

6. **`interaction.SubscribeRequest` and `interaction.ReadRequest` fields.**
   `client.Node.Subscribe`/`Read` take them; the only field named anywhere
   is `Updated` (`docs/getting-started/controller.md:185-186`,
   `docs/guides/concurrency.md:125`). `Attributes []im.ConcreteAttributePath`
   (`controller_test.go.txt:47`, `:106`) is a guess from the
   `client.SubscribeOptions` field of the same name; `Updated
   func(*interaction.Report)` a guess from `fabrictest.ReadWith`'s result.
   Nothing says how the returned `*interaction.Subscription` is closed
   (`controller_test.go.txt:60`, `_ = sub`; the site closes it).

7. **`spec.Nullable` zero value.** `levelcontroldef.MoveToLevelRequest`'s
   `TransitionTime spec.Nullable[uint16]` (`go doc ./cluster/spec/levelcontrol
   MoveToLevelRequest`); I sent the zero value (`main_test.go.txt:96`)
   without knowing whether it is null or 0. `spec` is not documented on
   the facade.

8. **Which features the kit's Thermostat serves.**
   `docs/getting-started/device.md:107-110`: "Thermostat … for a feature
   selection their server serves (`device/defaults.go` lists them)" — a
   source file, not a doc. I set `Features: thermostatdef.FeatureHeating`
   (`main.go.txt:161`) to be safe; whether the zero value would have done
   is unknown.

9. **Timed commands.** That LockDoor needs `InvokeTimed`
    (`main_test.go.txt:124`) is visible only as `Timed: true` inside the
    `Definition` literal of `go doc ./cluster/spec/doorlock`; the
    `CmdLockDoor` comment (`request → status, M`) does not say so.
    `schema.IsTimedInvoke` exists but nothing on the facade points to it.

10. **`StateSource.IsLocked() (locked, observed bool)`** — what `observed
    false` does (unknown LockState? the server's own state?) is not said;
    nor whether the server stores LockState itself after `LockInvoke`
    returns or re-reads `IsLocked` (`main.go.txt:93`, `go doc
    ./cluster/spec/doorlock StateSource`). It worked with `observed` true.

11. **`Cluster.Decoded` is not typed.** The task asked for "the typed
    reads the API offers"; `controller/client` has `Decoded(attr) (any,
    error)` only. A uint8 attribute comes back as `uint64`, an int16 as
    `int64` (`controller_test.go.txt:71`, `:125`) — said nowhere
    (`go doc ./controller/client Cluster.Decoded`: "decoded by its type").
    My first `level() == uint8(20)` never matched and, with
    `context.Background()` as in `docs/guides/testing-in-process.md:161`,
    `Site.Eventually` waited forever (the test hung until killed; fixed
    with a timeout, `controller_test.go.txt:27`). The device side has
    typed `State`; the controller side has no typed counterpart.

## Two ways to do one thing

12. **Command handlers and the write veto**: `Declare.Handle<Cmd>` /
    `Declare.OnWrite` and `Server.Handle<Cmd>(fn) error` /
    `Server.OnWrite(fn) error` (`go doc ./cluster/spec/levelcontrol`).
    The docs present the declaration as "the one form"
    (`docs/getting-started/device.md:127-131`); the setters stay public
    with no word on when to use them.

13. **Subscribing from the controller**: `CommissionOptions.AutoSubscribe`
    / `DefaultSubscription` (`client.SubscribeOptions`) and
    `Node.Subscribe(interaction.SubscribeRequest)`. I turned the default
    off (`controller_test.go.txt:36`) and used `Node.Subscribe`; nothing
    says which one a host should prefer or whether both together are
    allowed (`SubscribeOptions.KeepSubscriptions` hints at a conflict).

14. **The bridged NodeLabel**: `device.Options.NodeLabel` ("a NodeLabel a
    controller wrote in an earlier run and the host persisted") and
    `Device.Rename` ("persisted by the node"). If the node persists it,
    when does a host fill `Options.NodeLabel`? (`go doc ./device Options`).

15. **`OnChangedAsync` error asymmetry**: the typed
    `Server.OnChangedAsync` returns `(unsubscribe, error)`
    (`spec.ErrNoQueue`), the typed `OnChanged` and `Device.OnChangedAsync`
    return no error. One hook, three signatures.

## Types from packages the docs never mention

16. `device.Options.StableKey` is `device.SourceKey = endpoint.SourceKey`;
    `go doc ./device SourceKey` does not say what it is. Only
    `docs/guides/host-view.md:37` says "any `fmt.Stringer`".
    `fabrictest.Device.EndpointOf(key string)` and `fabrictest.Endpoint(key
    string, …)` take a string instead.

17. `doorlockdef.AuthStore` names `store.DoorLockAuth`
    (`go doc ./cluster/spec/doorlock AuthStore`); `docs/guides/testing-in-
    process.md:23` uses `endpoint.StringKey`; the fabrictest test imports
    `im`; `client` returns `interaction.*`, `handshake.PeerAddress`,
    `fabricdevice.Type`; `node.Config` aliases `mattercore`, `bridge`,
    `endpoint`, `attestation`, `subscription` types (`go doc ./node`
    lines 248-1286). None of these packages is on the facade list a
    developer is pointed to.

18. `contract.Origin` says `OriginHostSet` covers "a command handler of
    the host's included", but `contract.OriginFrom(ctx)` inside my
    `HandleMoveToLevel` returns `OriginCommand` (`main.go.txt:146`,
    asserted `main_test.go.txt:104`). Which origin a `Set` made from inside
    the handler reports could not be observed (point 1).

## Docs that did not match what I could read

19. `docs/getting-started/device.md:34` and `controller.md:34` include the
    quickstart programs by mkdocs snippet; in the repository the page shows
    `--8<-- "examples/…"`, and the consumer rules (and anyone reading the
    Markdown on GitHub) never see the program the page walks through. Most
    sections end in `*Source: node/config.go:…*` lines that point into
    files a consumer does not read.

20. `go doc ./fabrictest` package example uses `onoff.ClusterID` /
    `onoff.CmdToggle`; every other page imports the package as `onoffdef`.

21. `docs/getting-started/controller.md:10-28` builds the controller from
    nine packages (`store`, `ca`, `commissioner`, `transport/udp`,
    `client/exchange`, …) none of which is on the facade; with
    `fabrictest` that stack is hidden, so (b) did not need it — but a
    production controller program would leave the facade at line one.

## What worked without friction

- Declaring the four devices: one `Declare` per cluster, ports by name
  (`Sink`, `StateSource`), typed `Patch`/`State`, typed accessors.
- The thermostat's `Sink` saw the controller's setpoint
  (`main_test.go.txt:115`); the lock's `StateSource.LockInvoke` saw
  exactly one LockDoor and LockState read back Locked (`:127-131`).
- `fabrictest.CommissionedPair` / `CommissionNode`: commissioning in one
  call, ~0.1 s, on the fake clock.
- `Device.EndpointNumber` after the pair exists.
