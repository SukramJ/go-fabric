# Brief BB — milestone 1, phase 13: `ble/bluez`, `pair --ble`, the btvirt job, the CNET Wi-Fi cases

Owner: Fable. Implementer: Opus. Roadmap `docs/roadmap.md` milestone 1
(`v0.7.0`, the last phase); concept v3 §A.1 and
[ADR 0024](../../docs/adr/0024-ble-commissioning-in-the-controller.md)
(decisions 3, 5, 6 and the Consequences are this phase). Phase 12 (PR #144,
branch `m1/phase-12`) is the base: branch `m1/phase-13` from
`origin/m1/phase-12` if #144 is still open when you start, else from
`origin/main`; rebase onto `origin/main` before the PR. One PR, report
`notes/briefs/reports/BB.md`. `CHANGELOG.md` under `[Unreleased]` is yours;
`notes/parity/` is the owner's. **You own the chip-tool harness** in this
phase (single user).

Read `CLAUDE.md` (incl. Token use), the ADR, concept v3 §A.1, report BA
(`notes/briefs/reports/BA.md`) for what phase 12 built, and the seam you
implement: `controller/ble/central.go` (`Central`, `Connection`,
`Peripheral`, `Reachability`, `ListeningClock`), `controller/ble/channel.go`
(`Dial`), `controller/ble/scanner.go`, `controller/ble/bletest`. matter.js at
the pin (`git -C ../matter.js show 0ceb202d:<path>`) is the reference for the
**sequence**: `packages/nodejs-ble/src/NobleBleClient.ts` (scan filter,
connect, service / characteristic discovery, MTU),
`packages/nodejs-ble/src/NobleBleChannel.ts` (subscribe C2, write C1,
handshake, disconnect handling), `packages/nodejs-ble/src/BleScanner.ts`.
matter.js uses noble, not BlueZ, so the D-Bus mapping has no matter.js line
to cite: cite the BlueZ API documents instead
(https://github.com/bluez/bluez/tree/master/doc — `org.bluez.Adapter.rst`,
`org.bluez.Device.rst`, `org.bluez.GattService.rst`,
`org.bluez.GattCharacteristic.rst`), by file and section. UUIDs come from
`transport/btp` (`ServiceUUID`, the C1/C2/C3 constants), nothing hard-coded
twice.

## Items

1. **The sub-module `ble/bluez`** (`github.com/SukramJ/go-fabric/ble/bluez`,
   own `go.mod`: `require github.com/SukramJ/go-fabric v0.6.0` plus
   `replace github.com/SukramJ/go-fabric => ../..` for in-repo builds — the
   release bumps the require to the tag it ships with; say so in the
   sub-module's README). Dependency `github.com/godbus/dbus/v5`
   (BSD-2-Clause): the sub-module's own `THIRD-PARTY-NOTICES.md` and
   `licenses/`; the main module's `go.mod` does not change (five direct
   dependencies, ADR 0024 decision 3 — the main module never imports the
   sub-module). `bluez.NewCentral(adapter string) (ble.Central, error)`
   implementing `ble.Central` and `ble.Connection` over the system bus:
   `org.bluez.Adapter1` `SetDiscoveryFilter` (`UUIDs: [ServiceUUID]`,
   `Transport: "le"`, `DuplicateData: true`) and `StartDiscovery`;
   `org.freedesktop.DBus.ObjectManager` `InterfacesAdded` /
   `PropertiesChanged` on `org.bluez.Device1` for `ServiceData[ServiceUUID]`
   → the discovery callback's `serviceData`; `Device1.Connect`, wait for
   `ServicesResolved`; the `GattService1` with the Matter UUID and its
   `GattCharacteristic1` C1 (`WriteValue`, type `request`), C2
   (`StartNotify`, values via `PropertiesChanged` `Value`), C3 (`ReadValue`,
   optional); ATT MTU from the characteristic's `MTU` property where BlueZ
   offers it, else the BTP minimum; `Disconnect`, and `Disconnected()`
   closed on `Connected=false`. Linux only (`//go:build linux`), a stub
   that returns a named error elsewhere. Every D-Bus failure wraps the
   D-Bus error name (`org.bluez.Error.NotReady`,
   `org.freedesktop.DBus.Error.AccessDenied` …) so a rights problem is
   readable. Unit tests over a fake bus where feasible (an in-process
   `dbus` server or an interface seam around the five calls); the live
   path is item 3's.
2. **`pair --ble` without a module cycle.** Move the reference controller's
   CLI into `examples/reference-controller/cli` (`package cli`,
   `func Run(args []string, stdin io.Reader, stdout io.Writer, opts Options) error`,
   `Options.Central func(adapter string) (ble.Central, error)`); `main.go`
   becomes the plain build (`Central == nil`: `pair --ble` answers "this
   build has no BLE; build ble/bluez/cmd/reference-controller"). The BLE
   build is `ble/bluez/cmd/reference-controller/main.go` (ten lines,
   `cli.Run` with `bluez.NewCentral`). `pair --ble [--ble-adapter hci0]`:
   discovery by discriminator through `ble.NewScanner` (long from a QR
   code, short from a manual code, as the DNS-SD path does), `ble.Dial`,
   `commissioner.Options.Channel`; it requires `--wifi-ssid` /
   `--wifi-credentials` or `--thread-dataset` (a device on BLE has no
   network yet — refuse without, with that sentence), then the phase-12
   flow and operational discovery over DNS-SD. README of the reference
   controller: the BLE build, rights (the `bluetooth` group or a polkit
   rule for `org.bluez`), the minimum BlueZ version you verified, the
   adapter flag. `docs/getting-started/controller.md`: one paragraph.
   Manual test plan: row C12 (a real BLE device, hand-over to Apple Home
   after `window open`).
3. **The virtual end-to-end job.** `.github/workflows/ble-virtual.yml`,
   `workflow_dispatch` only (owner decision 2026-10-18: GitHub-hosted,
   manual), `ubuntu-24.04`: `sudo apt-get install bluez bluez-tests`,
   `sudo btvirt -l2` (two virtual HCI controllers), chip's
   `chip-all-clusters-app` from the pinned CHIP image (`CHIP_TEST_IMAGE`
   in the Makefile; the container needs the host's system bus —
   `-v /run/dbus:/run/dbus` as the harness already does for avahi — and
   `--privileged` or the capabilities BlueZ needs; `--ble-device 1`;
   **verify first that the image's app is built with BLE** —
   `chip-all-clusters-app --help | grep -i ble` — and if it is not, the
   job and the report say so and the item ends as an environment limit,
   not a workaround), our BLE build `pair --ble --code <the app's setup
   code> …` with the network flags the app's NetworkCommissioning feature
   needs, then `read`. The same steps as `make ble-virtual-test` (needs
   sudo for btvirt; documented). The job separates "environment"
   (btvirt / BlueZ / image) from "defect" in its summary, like the
   harness's control leg. Run it once from your branch with
   `gh workflow run` if the workflow file can be dispatched from a branch;
   otherwise the first run is the owner's after the merge — say which.
4. **The CNET Wi-Fi cases in the harness.** With `--wifi-sim` the daemon
   serves the Wi-Fi feature (phase 12). From chip's CNET case list at the
   harness pin determine every case gated on `CNET.S.F00` (Wi-Fi),
   `internal/chiptool/families_table_test.go:263-272` classifies five of
   them out of scope today. Run the family against the daemon with
   `--wifi-sim` and `CNET.S.F00=1` as a **second CNET run** (the certified
   default composition does not move: the default run stays as it is),
   classify each case (pass / a daemon gap / PDC and other unbuilt
   features / manual), and record the results in the harness table and
   `docs/certifiability.md`'s CNET rows. `make chiptool-test` once if any
   file under `node/` or `cluster/core/` changed; `make chiptool-families`
   narrowed to CNET.

## Guards

`make ci`, `make race` on `controller/... examples/...`; the sub-module:
`go build ./... && go vet ./... && go test ./...` and `go mod tidy -diff`
inside `ble/bluez`, and a CI step that does the same (add it to the ci
workflow); `make reachability` where it moves; the harness runs of item 4.

## Report (fixed shape)

Per item: built, guard, differences from the cited sequence
(`NobleBleClient.ts` / `NobleBleChannel.ts`), decisions, left out,
questions; the BlueZ version and rights you verified; whether the image's
app has BLE; the CNET classification table; by_design candidates; HEAD of
each guard run.
