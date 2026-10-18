# Brief BC — milestone 1, phase 13b: the virtual BLE job with a matter.js peripheral

Owner: Fable. Implementer: Opus. Closes roadmap milestone 1 (`v0.7.0`
waits for this job, owner decision 2026-10-18). Base: `origin/main` after
#147 (phase 13: `ble/bluez`, `pair --ble`, `script/ble-virtual-test.sh`,
`.github/workflows/ble-virtual.yml`, report `notes/briefs/reports/BB.md`).
Branch `m1/phase-13b`, one PR, report `notes/briefs/reports/BC.md`.
`CHANGELOG.md` under `[Unreleased]` is yours; `notes/parity/`, `docs/adr/`
and `CLAUDE.md` are the owner's (ADR 0024 §5 is already amended for this).

Read `CLAUDE.md` (incl. Token use), `notes/briefs/README.md`, report BB
§3, `script/ble-virtual-test.sh` and the workflow. matter.js at the pin
(`git -C ../matter.js show 0ceb202d:<path>`): `packages/nodejs-ble/README.md`
(the whole file: prerequisites, `ble.enable`, `ble.hci.id`, the Linux
notes on bleno/noble and `NOBLE_MULTI_ROLE`), `packages/nodejs-ble/src/NodeJsBle.ts`
(:16-36 `hciId` from `ble.hci.id`), `BlenoBleServer.ts:26-29`
(`BLENO_HCI_DEVICE_ID`), `packages/nodejs-ble/package.json:40-41`
(`@stoprocent/bleno ^0.13.1`, `@stoprocent/noble ^2.8.0`), and the example
node that runs with BLE and a Wi-Fi NetworkCommissioning behaviour (grep
`packages/examples/src` for `ble.enable` / `WiFiNetworkInterface` /
`--ble`; `packages/examples/README.md`). Our side: the BLE build of the
reference controller (`ble/bluez/cmd/reference-controller`) and
`pair --ble` with the network flags.

## Items

1. **The peripheral.** A small matter.js device script under
   `script/ble-virtual/` (`package.json` pinning `@matter/main` and
   `@matter/nodejs-ble` to the published version nearest the pin — the
   nearest tag below `0ceb202d` is `v0.18.0-alpha.0-20261008-6066b7499`;
   find the npm alpha that corresponds and record it in the report; a
   lockfile is committed), modelled on matter.js's own BLE-capable
   example: an on/off device with `NetworkCommissioningServer` in the
   Wi-Fi variant (accepting the credentials the flow sends and then
   "connecting", as that example does), `ble.enable = true`,
   `ble.hci.id` = the second btvirt controller, a fixed passcode and
   discriminator printed as the manual code, logging to a file. No
   change to matter.js; no copy of its source into this repo beyond the
   script that uses the packages (Apache-2.0 either way; say so in the
   script header).
2. **The job and the script.** `script/ble-virtual-test.sh` and
   `.github/workflows/ble-virtual.yml` drive: `btvirt -l2`; BlueZ keeps
   `hci0` for our central; `hci1` is taken down in BlueZ and handed to
   bleno (raw HCI needs `cap_net_raw` on the node binary or root — do what
   matter.js's README prescribes for Linux); `node` ≥ the version
   `@matter/main` requires, `make g++` for node-gyp; start the peripheral;
   `reference-controller pair --ble --code <manual code> --wifi-ssid lab
   --wifi-credentials secret12`; then `read` over the operational session
   (the peripheral stays reachable over IP — matter.js advertises its
   operational service on the runner's loopback/interfaces); `window
   open` as a bonus if time allows. The script keeps its three-way exit
   (pass / defect / environment) and its step summary; "environment" now
   also covers a bleno build or HCI-claim failure, named as such. Run
   locally what you can without sudo (the Node install and the peripheral
   starting on no adapter will fail at the HCI step — that is expected);
   push the branch and dispatch the workflow **after the PR is merged**
   is impossible for you, so: open the PR, say so, and the owner
   dispatches; if the owner has merged a previous version of the workflow
   by the time you push, dispatch it yourself from the branch and iterate
   on the result (`gh run view --log-failed`, grep).
3. **The control leg.** A second job step that runs the same peripheral
   against **matter.js's own controller** over BLE (`@matter/main`'s
   controller example with `ble.enable`, noble on `hci0` while our central
   is not running), so that a failure separates "our central" from "the
   virtual stack": if matter.js ↔ matter.js fails, the summary says
   environment; if only ours fails, defect. Same pattern as the harness's
   `chiptool-control` leg.
4. **Docs.** `docs/matterjs-comparison.md`'s BLE row and the reference
   controller README name the virtual counterpart; the manual test plan
   row C12 is unchanged (a real device remains the field proof).

## Guards

`make ci` (the script and workflow are shell/YAML: `shellcheck` if the
repo runs it, the workflow's `actionlint` if present — check the Makefile);
`go test ./script/...` if the script directory carries Go. No chip-tool
run: nothing under `node/`, `cluster/` or the daemon moves. The decisive
guard is the dispatched workflow run; its URL and summary go into the
report, with the npm versions, node version, BlueZ version and btvirt
version it ran with.

## Report (fixed shape)

Per item: built, guard, decisions, left out, questions; the environment
versions; the workflow run result (or why it could not run) and what each
of the three legs (ours, control, environment) showed; by_design
candidates.
