# Brief BA — milestone 1, phase 12: BTP, the BLE central seam, the network steps

Owner: Fable. Implementer: Opus. Roadmap `docs/roadmap.md` milestone 1
(`v0.7.0`); detail in `docs/concept-v3-commissioner-in-the-field.md` §A.1
and [ADR 0024](../../docs/adr/0024-ble-commissioning-in-the-controller.md)
(read both first). Runs in parallel with brief AZ (phase 11:
`controller/ca`, `controller/attestation`, `controller/dcl`,
`controller/commissioner/fabric.go`, `examples/reference-controller/{window.go,main.go,README.md}`,
`README.md`) — stay out of those files; in `examples/reference-controller`
you own `pair.go` only (new flags), and you rebase onto the latest
`docs/roadmap` before the PR. Branch `m1/phase-12` from your worktree's
HEAD, one PR, report `notes/briefs/reports/BA.md`. `CHANGELOG.md` under
`[Unreleased]` is yours; `notes/parity/` is the owner's.

Read `CLAUDE.md` (incl. Token use). Every matter.js line **at the pin**
(`git -C ../matter.js show 0ceb202d:<path>`; the checkout's HEAD has moved).
chip at the harness pin where named
(`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`).
Mirror, cite, test first. Phase 13 (`ble/bluez`, `pair --ble`, `btvirt`)
is **not** in this brief: nothing here imports D-Bus or opens a radio.

## Items

1. **`transport/btp` — the Bluetooth Transport Protocol, pure Go.** Port
   `packages/protocol/src/ble/BtpSessionHandler.ts` (both roles: the
   central side the commissioner uses now, the peripheral side the tests
   need and milestone 7 will use), `BleConsts.ts` (service UUID, C1/C2/C3
   characteristic UUIDs, MTU and window defaults, timers), and the codec
   `packages/protocol/src/codec/BtpCodec.ts` (handshake request/response,
   segment header flags, sequence and ack numbers). The result is a
   `transport.Conn`-compatible reliable channel; **MRP is not used over
   BTP** — confirm against `BtpSessionHandler.ts` / the channel's
   reliability flag at the pin and cite the line. Tests: port
   `packages/protocol/test/codec/BtpCodecTest.ts` (358 lines) and
   `packages/protocol/test/ble/BtpSessionHandlerTest.ts` (1 526) as Go
   tests over an in-memory GATT pair; a fuzz target for the segment
   decoder (`make fuzz` picks it up).
2. **`controller/ble` — the central seam, no dependency.** Interfaces for
   what phase 13 implements over BlueZ: `Central` (scan, connect, the
   three characteristics, MTU), the commissionable advertisement parser
   (`packages/protocol/src/advertisement/ble/` and
   `packages/protocol/src/common/BleScanner.ts`: discriminator, vendor /
   product id, additional-data flag; test:
   `packages/protocol/test/common/BleScannerTest.ts`, 1 342 lines — port
   the parser cases), discovery by discriminator / setup code, and PASE
   over the BTP channel into the existing commissioning flow
   (`controller/commissioner`, `handshake`). An in-memory `Central` +
   peripheral pair lives in the package's test support.
3. **The network steps in the commissioning flow.** Port
   `packages/protocol/src/peer/ControllerCommissioningFlow.ts:1527-1900`
   into `controller/commissioner/flow.go`'s step list: read the device's
   NetworkCommissioning feature map and `Networks`, `ScanNetworks` where
   the flow does, `AddOrUpdateWiFiNetwork` (:1677) with SSID / credentials,
   `AddOrUpdateThreadNetwork` (:1874) with an operational dataset
   (bytes; the Thread TLV codec of `packages/protocol/src/thread/` only
   as far as the flow needs it — the dataset **source** is the operator in
   this phase: `--thread-dataset <hex>`), `ConnectNetwork`, the
   NetworkConfigResponse / ConnectNetworkResponse status handling and
   the breadcrumb, then operational discovery over DNS-SD and CASE as
   today. The steps must also run over IP when the device advertises
   Wi-Fi / Thread (as matter.js does), so they are testable without a
   radio. `pair` gets `--wifi-ssid`, `--wifi-credentials`,
   `--thread-dataset` (`examples/reference-controller/pair.go`).
   `commissioner.Options` carries the same.
4. **A simulated Wi-Fi interface on the device side, TESTING ONLY.**
   `cluster/core/network_commissioning.go` is Ethernet-only; add the
   Wi-Fi feature behind a host port (`contract` type, stdlib only; wired
   through `node.Config.Network`), with the command semantics the flow
   needs: `ScanNetworks`, `AddOrUpdateWiFiNetwork`, `RemoveNetwork`,
   `ConnectNetwork`, `ReorderNetwork`, `Networks`, `LastNetworkingStatus`,
   `LastNetworkID`, `LastConnectErrorValue`, `MaxNetworks`. matter.js's
   server is a generated stub (`NetworkCommissioningServer.ts`, 18
   lines), so the reference for the server's status codes and list rules
   is chip's `src/app/clusters/network-commissioning/network-commissioning.cpp`
   at the harness pin — cite it; matter.js's expectations of the device
   are in the flow lines above and the test subclasses in
   `packages/node/test/node/ScanNetworksRejectionCommissioningTest.ts`.
   The reference daemon gets `--wifi-sim` (a driver that "connects" to any
   SSID after a short delay and stays on the Ethernet address). The
   Ethernet-only default is unchanged: FeatureMap and the daemon's
   composition move only with the flag.

## Proof

- BTP vectors and session tests ported (item 1), scanner cases ported
  (item 2); the register of ported tests is milestone 2's — list the
  ported files in the report.
- End to end in-process: the commissioner, over the in-memory BTP pair,
  commissions the module's node with `--wifi-sim`, including the network
  steps, and the node is then reachable over IP (CASE) — a live test
  after `node/commissioner_control_live_test.go`.
- Over IP against the reference daemon `--wifi-sim`: `pair --passcode …
  --discriminator … --wifi-ssid lab --wifi-credentials secret`.
- `make ci`, `make race` on `transport/... controller/... cluster/core/...`,
  `make fuzz` with the new target, `make reachability`.
- chip-tool: the CNET family's Wi-Fi cases against the daemon with
  `--wifi-sim` are **phase 13's** harness work (single user; phase 11's
  live test does not need the harness). Do not run `make chiptool-families`
  in this phase; run `make chiptool-test` once if `node/root.go`'s
  composition moved without the flag (it should not).

## Report (fixed shape)

Per item: built, guard, differences from the cited lines (BTP timers and
window sizes verbatim), decisions, left out (Thread TLV beyond the flow's
needs, BlueZ, `pair --ble`), questions; by_design candidates (the
device-side Wi-Fi server from chip rather than matter.js is one); HEAD of
each guard run.
