# Wave 5, theme `ex`: examples and fabrictest

Scope: `examples/reference-bridge/`, `examples/reference-controller/`,
`examples/quickstart-device/`, `examples/quickstart-controller/`,
`examples/host-view/`, `fabrictest/`. Consumer review against `README.md`,
`docs/guides/{embed-in-a-daemon,host-view,operations,testing-in-process}.md`,
`docs/getting-started/{device,controller}.md`, the package docs and
`notes/manual-test-plan-0.4.0.md`.
HEAD `769ea3c2`. Reviewer: Opus. `go build ./examples/...` and
`go vet ./examples/... ./fabrictest/...`: exit 0. A goroutine and
store probe ran as a scratch module outside the tree (replace directive onto
this checkout); nothing in the repository was changed.

## W5-ex-01 — high — the SQLite `file:` URI is built from an unescaped path

`fabrictest/device.go:159`, `examples/quickstart-device/main.go:72`; the same
construction in `node/store_path.go:30` (`node.Store.Path`, which the reference
bridge and host-view use) and `controller/open.go:36` (`controller.OpenDB`,
which the quickstart and the reference controller use).

Each call opens `"file:"+path+"?_pragma=…"`, with `path` not escaped. A `#` or
`?` in the path ends the file name. So the database opens at a truncated path,
and the pragmas (`foreign_keys(1)`, `busy_timeout`, and WAL for the controller)
are dropped without any error. The probe confirmed this. With
`Options{StoreOnDisk: true}`, `CommissionedPair` was called in sequential
`t.Run("iter", …)` subtests, which Go names `iter`, `iter#01`, `iter#02`, and so
on. Iterations 0 and 1 pass. Every later iteration fails with
`commission device 2: … expected message missing after 11.229s`. The cause is
that each `#`-named site's store resolves to the same file,
`/tmp/TestDiskPairOnlyiter`. That file sits outside `t.TempDir()`, is never
cleaned up, is mode 0644, and already holds the previous run's fabric. The
same test passes six times out of six when it runs as a top-level test, whose
name has no `#`. In production the same bug means a `--db` path containing `#`
or `?` silently writes to another file with foreign keys off, which breaks the
cascade delete on fabric removal (`examples/reference-bridge/persist.go:12-16`).

Contradicts: `fabrictest/site.go:90-96` ("a SQLite file in the test's temporary
directory"), `docs/guides/operations.md:14-17` ("opens the file itself … with
foreign keys on and a 5 s busy timeout").

Proposed change: build the DSN with `url.URL{Scheme: "file", Path: path,
RawQuery: …}`, or `url.PathEscape` the path, in all four places, and add a
fabrictest case under a `#`-named subtest.

Confidence: confirmed (probe; the stray files were found in `/tmp` and removed).

## W5-ex-02 — medium — quickstart-device teaches three packages below the facade that the node already covers

`examples/quickstart-device/main.go:20-27, 47-54, 71-73, 94-99`; mirrored
line for line in `docs/getting-started/device.md:57-61, 84-90, 111, 124, 129`.

The "smallest bridge" does four things it should not need to:

- it builds the DNS-SD advertiser by hand (`mdns.NewZeroconf`,
  `mdns.NewSubtypeResponder`, `AttachSubtypeResponder`). This is what a nil
  `node.Config.Advertiser` already does (`node/mdns.go:43-60`), except that
  the quickstart ignores the responder error silently and never closes `zc`;
- it opens SQLite itself (`sql.Open` plus the driver import) instead of using
  `node.Store.Path`;
- it uses `endpoint.StringKey` instead of `device.StringKey`
  (`device/options.go:59`);
- it follows OnOff through the untyped `light.OnChanged(onoff.ClusterID, …)`
  from the server package `cluster/onoff`. `examples/host-view/main.go:52`
  uses the typed `light.OnOff().OnChanged` for the same thing.

This is the first file a new host copies, and it contradicts the facade's
own guide.

Contradicts: `docs/guides/embed-in-a-daemon.md:238-250` ("no `bridge`,
`endpoint` or `im` … no `mdns`: the node advertises itself … no `store`:
`Store.Path` is one SQLite file that the node opens and closes"), and the
table in `docs/getting-started/device.md:193`, which describes the nil
`Advertiser` as exactly what the code builds by hand.

Proposed change: reduce the quickstart (and the page) to `node`, `device`,
`contract` and `cluster/spec/onoff`: `Store.Path`, a nil `Advertiser`,
`device.StringKey`, typed `OnOff().OnChanged`. Consider an import-allowlist
test for the three small examples, like the one the embed guide credits to
its daemon.

Confidence: confirmed.

## W5-ex-03 — medium — quickstart-controller acts on the first registered node, not the node it just paired

`examples/quickstart-controller/main.go:59-69`; the same in
`docs/getting-started/controller.md:139-143`.

`run` discards the `*client.Node` returned by `c.Pair` and then uses
`c.Nodes()[0]`. `Controller.Nodes` is `Registry.Commissioned`, which keeps
nodes in join order (`controller/client/registry.go:555-573`), and the nodes
loaded from the database come first. On a second run with `-code` against the
same `-db`, the program pairs the new device but reads, toggles and
subscribes to the old one.

Contradicts: `main.go:4-7` ("pairs a node by its QR or manual code, reads its
NodeLabel, toggles its endpoint 1 OnOff").

Proposed change: use the node `Pair` returns when `-code` is given, and fall
back to `Nodes()` (and say which node is used) only without it.

Confidence: confirmed (code path; not run against a device).

## W5-ex-04 — medium — the reference-bridge README describes wiring the code no longer has, and omissions the node has since closed

`examples/reference-bridge/README.md:20-23, 26-41, 236-260, 274-280`.

- **Subtype responder (:22-23).** It is listed as host work. The node builds
  it (`main.go:24-28`).
- **Default servers (:26-34).** The lights are said to take
  `cluster/onoff.Server` and switch it with `onoff.Server.Set`, and host
  servers to arrive "through `device.WithServer`" from `cluster/valve`,
  `cluster/levelcontrol`, `cluster/fan`, `cluster/lock`, `cluster/thermo` and
  `cluster/opstate`. No non-test file calls `WithServer` or imports any of
  those packages. They use typed `Declare`/`HostServer` (`fleet_evse.go:207`,
  `fleet_sensors.go:465`).
- **Sensors (:39).** It cites `device.WithMeasurement`, which no longer
  exists. `docs/guides/migrating-to-the-facade.md:246` lists it as replaced.
- **Persistence (:236-260).** It presents `sqlitestore.New(db)` and
  `Bridge.ReestablishFormerSubscriptions` as what this example wires, but the
  example only sets `node.Store.Path`.
- **"Does NOT do" (:274-280).** It lists IPK rotation and CASE resumption as
  responder as missing. The node it runs does both: candidate IPKs over key
  set 0 (`node/security.go:51-55`) and `responder.SetResumptionStore`
  (`node/security.go:670-675`).

Contradicts: the code cited above, and `docs/guides/operations.md:21-24` (the
file holds "the CASE resumption records").

Proposed change: rewrite those README sections against `main.go`/`fleet*.go`
as they stand, and drop the two closed omissions.

Confidence: confirmed.

## W5-ex-05 — medium — neither the node nor the examples protect the database file that holds private keys

`node/store_path.go:30`, `controller/open.go:36`, `examples/reference-bridge/main.go:418`
(`Store.Path`), `examples/reference-controller/cli/stack.go:84`; no `Chmod`,
`OpenFile` mode or umask anywhere in `node/`, `controller/` or the examples
(grep).

SQLite creates the file under the process umask. The probe's file was
`-rw-r--r--`. The device file holds the operational private keys. The
reference controller's file holds the CA root key unless `--ca-key` is given
(`examples/reference-controller/README.md:40`). On a multi-user host both
files are world-readable by default. The guides tell the operator to protect
the file, but neither the reference programs nor the facade do it.

Contradicts: `docs/guides/operations.md:35-36` ("protect it like a key: it
holds the node's private operational keys unencrypted").

Proposed change: create the file 0600 before opening it (for example
`os.OpenFile(path, O_CREATE, 0o600)`), in `openStorePath` and
`controller.OpenDB`, so every host inherits it. Otherwise the reference
programs should do it and the guide should say the host must.

Confidence: confirmed (code; mode observed on the probe's file).

## W5-ex-06 — low — every commissioned fabrictest site leaks the fail-safe watcher goroutine

`node/security.go:617` → `cluster/core/general_commissioning.go:907, 685-692`.

The PASE auto-arm passes `context.Background()`. Its watcher waits on that
context and a timer on the node's clock, and `Node.Stop` does not cancel it.
On a fabrictest site the fake clock never moves again after `Close`, so one
goroutine and the node graph it references outlive every commissioned site.
The probe showed +1 goroutine per site after `Close` (in-memory and on-disk
stores alike), each one parked in `watchFailSafeExpiry`. In production the
watcher outlives `Stop` by at most the auto-arm period.

Contradicts: `fabrictest/doc.go:10-11` ("devices and controllers on one
simulated network, closed with the site").

Proposed change: pass the node's lifetime context (`n.life`) to
`AutoArmOnPaseEstablished`, so `Stop` ends the watcher.

Confidence: confirmed (probe stack dump).

## W5-ex-07 — low — the reference controller hand-encodes TLV for group and binding provisioning that has typed definitions

`examples/reference-controller/cli/group.go:26-35, 64-104`,
`examples/reference-controller/cli/bind.go:21-36, 125-146, 167-174`.

The comment at `group.go:26-29` says GroupKeyManagement, AccessControl and
Groupcast "have no generated definition". `cluster/spec/groupkeymanagement`
(`KeySetWriteRequest`, `definition_gen.go:261`), `cluster/spec/groupcast`
(`JoinGroupRequest`, `:178`), `cluster/spec/binding` (`TargetStruct`) and
`cluster/spec/accesscontrol` (`AccessControlEntryStruct`, `:132`) all exist.
The code builds raw `tlv.Element` trees with magic field numbers copied from
`node/groupsend_live_test.go` and `node/binding_live_test.go`. This is also a
facade gap: the controller role has no helper for "provision a group on these
nodes" or "bind A to B with the ACL entry", so every host would copy these 150
lines.

Contradicts: `README.md` §API stability and the CLAUDE.md rule that the typed
`cluster/spec` definitions are the one data model.

Proposed change: switch to the generated request and struct types, and name
a `controller/client` group-provisioning and binding helper as a facade
candidate.

Confidence: confirmed.

## W5-ex-08 — low — a failed `Start` is not followed by `Stop`, against the host-view guide

`examples/host-view/main.go:106-108`; the same pattern in `node/node.go:924-928`
(`Node.Run`, used by quickstart-device), `node/controllernode/controllernode.go:60-62`
and `examples/reference-controller/cli/self.go:79-81`.

`node.Start`'s error returns do not release what `New` and a partial start
began (`node/node.go:720-750`: bare `return err`). The callers do not call
`Stop` either. So the engine goroutines `New` started remain, and with
`Store.Path` the SQLite file stays open. host-view also drops a
`PairingCodes` error silently (`main.go:117`).

Contradicts: `docs/guides/host-view.md:221-222` ("On an error path the node is
stopped: `New` already started its engine.").

Proposed change: either `Start` releases on failure or `Run`, `controllernode.Start`
and the examples call `Stop`. Document which in `Node.Start`'s doc.

Confidence: confirmed that no `Stop` runs; the leaked resources are plausible
(not measured).

## W5-ex-09 — low — the reference controller copies `controllernode.Start` to add one option

`examples/reference-controller/cli/self.go:72-102` against
`node/controllernode/controllernode.go:53-72`.

`startSelf` repeats `controllernode.Start` line for line. It adds only
`Network.PreferIPv4` for an IPv4 `--node-listen`, an IPv4 loopback, and the
test hooks. A host with an IPv4 lab network has to make the same copy.

Contradicts: `controller/ota.go:75-78` ("node/controllernode.Start" is the
starter).

Proposed change: give `controllernode.Start` (or `OwnNodeSpec`) the IPv4
preference, and keep only the test hooks in the example.

Confidence: confirmed.

## W5-ex-10 — low — the reference bridge's package doc overstates its facade purity; three facade gaps

`examples/reference-bridge/main.go:24-28` against `main.go:55, 368, 454, 487`,
`fleet_evse.go:140-165`.

The doc says the daemon "names none of bridge, mdns, secure/* or store". It
imports `store` (`store.NewThermostatPresetStore`) and reaches `n.Bridge()`
twice. Each use names a gap:

- the node has no listing of every endpoint with its device types
  (`printTopology`). `Node.Topology` gives only the bridged devices;
- there is no facade for persisting thermostat presets;
- `device`'s default EnergyEvse has no host port, so the charger drops to
  `cluster/energy.NewEnergyEvse` with an `EvseHost` and passes it as
  `HostServer`.

Contradicts: `README.md:3-4` of the example ("built only from go-fabric's
public API" holds; "assembled only from the facade" does not) and the
package doc above.

Proposed change: correct the doc sentence, and record the three gaps for the
W5 `api` register, or close them in `node`/`device`.

Confidence: confirmed.

## W5-ex-11 — low — `fabrictest/doc.go` says devices run on SQLite by default

`fabrictest/doc.go:32-33` ("its own SQLite stores in the test's temporary
directory") against `fabrictest/site.go:90-96` and `fabrictest/device.go:164`
(in-memory `store/memstore` and `endpoint/memstore` unless `StoreOnDisk`).

Contradicts: `docs/guides/testing-in-process.md:67`, which states the
in-memory default correctly.

Proposed change: fix the package doc sentence.

Confidence: confirmed.

## W5-ex-12 — note — fabrictest's API is two shapes, the facade and the seams

`go doc ./fabrictest`: `Device.Bridge() *bridge.Bridge`,
`Device.Endpoints() []*endpoint.Endpoint`, `Device.Peers`, `Controller.Exchanges`,
`Commissioner`, `Peers`, `Resumption`, `Subscriptions`, and
`fabrictest.Endpoint(key, deviceType, servers...)`. These sit beside the
facade-shaped `Topology{Bridged: []node.Endpoint}`, `Pair.Node()` and the wire
helpers. There are also two device constructors (`AddDevice(Topology)`,
`AddNode(node.Config)`). The doc example (`doc.go:27`) builds an
`im.ConcreteCommandPath` while the guide uses `client.CommandAt`. Separately,
`README.md:122-124` says `internal/` holds "the two one-way seams (`bridgeseam`,
`channelseam`)", but fabrictest also imports `internal/nodeseam`,
`internal/udpseam` and `internal/inflight`. No exported signature names an
`internal/` type (checked).

Proposed change: decide in W5 `api` whether the seam accessors stay in the
public harness, and align the doc example and the README's `internal/` list.

Confidence: confirmed.

## W5-ex-13 — note — quickstart-controller carries a test seam

`examples/quickstart-controller/main.go:24, 32-33, 87`. `var openNames =
discovery.Open` exists only so that `main_test.go:30` can replace it. It
pulls `client/discovery` into the minimal controller, although
`controller.Config.Names` nil already means `discovery.Open`
(`controller/controller.go:117-119`).

Proposed change: pass `Names` only from the test, or wrap `open` so the test
injects it, leaving the program with the facade's default.

Confidence: confirmed.

## W5-ex-14 — note — the test app pipe is world-writable

`examples/reference-bridge/fifo_unix.go:31` (`os.Chmod(path, 0o666)`). While
`--app-pipe` is on, any local user can inject test commands (sensor values,
configuration-version changes, consent). The flag is labelled TESTING ONLY and
is off by default (`main.go:115`).

Proposed change: 0600, or 0660 with a comment on why the harness container
needs group access.

Confidence: confirmed.

## W5-ex-15 — note — Wi-Fi passphrase and Thread dataset only on argv

`examples/reference-controller/cli/pair.go:123-125`. `--wifi-credentials` and
`--thread-dataset` are accepted only as flags, so they show up in `ps` and in
shell history. The CA key passphrase, by contrast, comes from
`GOFABRIC_KEY_PASSPHRASE` (README:40). They are not logged (grep).

Proposed change: accept an environment variable or `@file` form for both.

Confidence: confirmed.

## Checked and sound

- `go build ./examples/...` and `go vet ./examples/... ./fabrictest/...`
  are clean.
- Test identity is warned about everywhere a host would copy it:
  quickstart-device `main.go:7, 79-83`, host-view `main.go:7, 81, 88`,
  reference-bridge README §"This is a TEST identity" and the banner
  (`main.go:466` "CSA TEST identity — not shippable"),
  `docs/getting-started/device.md` (test chain must not ship), and
  `controller.Config.Attestation`, which has no default
  (`controller/controller.go:86-92`).
- Reference-bridge lifecycle:
  - `signal.NotifyContext` on SIGINT/SIGTERM;
  - exactly one `Stop` on every path (`started` flag, `main.go:187-193`;
    `assemble`'s `ok` guard, `:352-357`);
  - `Run`'s bounded stop timeout;
  - error exit through `slog.Error` and status 1;
  - `--dump-surface` touches neither `--db` nor the network
    (`main.go:297-307`).
- What a bridge must persist is delegated to `node.Store.Path` (fabrics,
  ACL, group keys, resumption records, subscriptions, endpoint numbers,
  UniqueID, per `docs/guides/operations.md:21-25`). The daemon's own state
  (blind position, lock credentials, thermostat presets) goes through
  `n.Store()`.
- Every reference-bridge flag is read (grep per field). The flags the
  chip-tool harness passes (`--app-pipe`, `--wifi`, `--wifi-sim`, `--media`,
  `--ota-harness`, `--mdns-os-hostname`, `--thermostat-schedules`,
  `--log-level`) exist.
- Reference-controller commands and flags match
  `notes/manual-test-plan-0.4.0.md` C1–C13. These include `subscribe --max`,
  `group create --key/--nodes/--endpoints`, `group send`, `bind … --to`,
  `ota serve --image/--announce/--once/--yes/--version/--vendor/--product`
  and `window open/revoke`. The clash between the window's `--timeout` and
  the global `--timeout` is handled (`window.go:78-108`) and documented
  (README:195-197).
- `appendToList` reads with `IncludeKnownVersions` and fabric filtering
  before writing the list back, so a Binding/ACL append does not drop the
  fabric's existing entries (`bind.go:49-75`,
  `controller/client/interact.go:22-24`).
- No secret is logged at info level in either reference program. Passcodes
  go to stdout only, in the banner and in `window open`, by design. The CA
  key is logged only as a reference.
- fabrictest offers what a host test needs:
  - in-process commissioning (`CommissionedPair`, `Commission`,
    `CommissionNode`);
  - read/write/invoke/subscribe/events, timed variants included;
  - device and controller restart;
  - window open (`Device.OpenWindow`), with close and revoke and fabric
    removal reachable through `Device.Node()` / `Pair.Node()`
    (`node.Node.RemoveFabric`, `client.Node.RevokeCommissioningWindow`,
    `Registry.Decommission`);
  - time control (`Step`, `Settle`, `Run`, `Eventually`).
- `go test -count=6 -run 'TestDeviceRestart/StoreOnDisk=true' ./fabrictest/`
  passes. The in-memory probe passes 6 of 6 sequential sites, apart from the
  leak in W5-ex-06.

## Not read

- `examples/reference-bridge`: `fleet*.go` bodies (about 7k lines; only
  grepped), `control*.go`, `media.go`, `otbr.go`, `wifi*.go`, `attestation.go`,
  `surface.go`, `metrics.go`, `tcp.go`, and every `_test.go`.
- `examples/reference-controller/cli`: `interact.go`, `paths.go`, `ota.go`,
  `discover.go`, `cakey.go`, the body of `pair.go`, and `group.go` after
  line 130.
- `fabrictest`: the bodies of `controller.go`, `net.go`, `multicast.go`,
  `ble.go`, `raw.go`, `access.go` and `endpoint.go`, and the tests other than
  `guide_test.go` and slices of `site_test.go`.
- `docs/guides/migrating-to-the-facade.md` beyond line 246, and the
  `ble/*/cmd/reference-controller` mains.
- matter.js `packages/node/src/node/ServerNode.ts` and `server/*` at
  `862ad75f` were not opened. Persistence was judged against
  `docs/guides/operations.md` and `persistence.md`'s table list instead, so
  W5-ex-05 and the persistence line above carry no matter.js citation.
