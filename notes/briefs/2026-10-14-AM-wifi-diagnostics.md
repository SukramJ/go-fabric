# Brief AM — WiFiNetworkDiagnostics server with a host port, the SecondaryNetworkInterface device type, the DGWIFI family (concept v2 phase 10, §B.4)

Owner: Fable. Implementer: Opus. A second Opus agent works on
`controller/client` (brief AL) at the same time: **do not touch
`controller/`**, and **never run `make chiptool-test`, `make chiptool-families`
or any chip-tool binary** — the harness is single-user and the other
agent runs it once; the owner runs the DGWIFI family on the tranche.
Branch `wave19/wifi` from `planning/briefs-wave-19`, pushed, **no PR**.
Report in `notes/briefs/reports/AM.md`. Read first: `notes/briefs/README.md`,
[ADR 0013](../../docs/adr/0013-generated-cluster-definitions.md), concept
v2 §B.4, `cluster/core/ethernet_network_diagnostics.go` (the pattern:
generated definition, a reporter read on every read, chip's null
conventions cited), `cluster/core/general_diagnostics.go` (events,
TestEventTrigger), `node/root.go` (how the root's optional servers are
built; `unknownEthernet`), `endpoint/assembler.go` (`RootDeviceConditions`),
`script/clustergen/clusters.go`, `script/devicegen/devicetypes.go`
(`excluded`), `internal/chiptool/doc.go`, `families_table_test.go`,
`testdata/reference-bridge.pics` and `examples/reference-bridge/control.go`
(the app-pipe), `docs/certifiability.md` §Families.

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`).
`WiFiNetworkDiagnosticsServer.ts` (`packages/node/src/behaviors/wi-fi-network-diagnostics/`)
is the empty generated class (:14) — as the Ethernet one; matter.js has
no platform filling for it. The element file is
`packages/model/src/standard/elements/wi-fi-network-diagnostics.element.ts`
(kebab-case): cite its lines for every id, conformance, quality and
default. The behaviour authority for what matter.js does not model is
chip (CLAUDE.md): `../connectedhomeip/src/app/clusters/wifi-network-diagnostics-server/WiFiNetworkDiagnosticsCluster.cpp`
(256 lines, at the harness pin) — read it in full for the null/unknown
conventions, the counter reads, `ResetCounts` and the events, and cite
its lines as `ethernet_network_diagnostics.go` cites its sibling.
Guards: `make ci`, `make race`, `make reachability-check`, `make chipdm-check`
(the data-model cross-check must stay green: a new server changes
nothing in the snapshot, but run it) — export `PATH=$HOME/go/bin:$PATH`
first; a `make ci` that fails only on golangci-lint's own timeout after
"0 issues" is rerun as `make lint`. One commit per §, reachability in
each, CHANGELOG under `[Unreleased]` → Added. Allowed paths:
`cluster/core` (new files), `cluster/spec` (generated, through the
generator list), `device/` (generated, through the generator list),
`script/clustergen/clusters.go`, `script/devicegen/devicetypes.go`,
`node/` (§3), `examples/reference-bridge` (§4), `internal/chiptool`
(§5: table, PICS, golden), `docs/certifiability.md` (§5, the family
row and one line in the text), `script/coverfloor/floors.go`,
`CHANGELOG.md`. `notes/parity/by_design.md`, the findings register, ADRs
and `docs/chip-datamodel-crosscheck.md` are the owner's.

## Owner's decisions

- **D1 — `NetworkCommissioning` stays Ethernet-only** (concept v2 B.4,
  0.4). A Wi-Fi host advertises `FeatureMap = ET` there and states the
  device-type condition `WiFi` through `RootDeviceConditions`, so the
  root's `[WiFi]` conformance (RootNode requirements, snapshot) holds
  for `WiFiNetworkDiagnostics`. The owner writes the `by_design` entry.
- **D2 — the server reads a reporter on every read**, like Ethernet: no
  attribute change is reported to subscribers, as chip reads its
  provider; `Rssi` and the counters carry the "C" quality anyway.
- **D3 — events are the host's to raise**: the server exposes
  `Disconnection(reasonCode)`, `AssociationFailure(cause, status)`,
  `ConnectionStatus(status)` as methods that emit the event with the
  snapshot's field ids; the reference bridge raises them from the
  app-pipe for the harness.
- **D4 — `ResetCounts` resets the reporter** through a `ResetCounts()`
  method of the port (chip: the provider's `ResetCounts`); the server
  answers Success; with `ERRCNT` absent the command is not accepted
  (conformance `ERRCNT`, element file).
- **D5 — the reference bridge gets a simulated reporter** behind
  `-wifi` (TESTING ONLY, like `-app-pipe`): fixed plausible values
  (BSSID, SecurityType WPA2, WiFiVersion AX, channel, RSSI, counters
  that the app-pipe can step and reset), features `PKTCNT|ERRCNT`, the
  condition `WiFi` stated. A real Linux reporter is the host's (one
  sentence in the package doc: what it needs — nl80211 or `iw`); not
  here.

## §1 — the generated definition and the device type

Add `WiFiNetworkDiagnostics` to `script/clustergen/clusters.go`, run the
generator, commit `cluster/spec/wifinetworkdiagnostics/`. Remove
`SecondaryNetworkInterface` from `devicegen`'s `excluded`, run it, commit
`device/secondary_network_interface_gen.go` (and whatever the generator's
tests require — read `script/devicegen` first; the device type is
`0x0019` revision 1, utility, NetworkCommissioning M, the three
diagnostics under `[Thread]`/`[WiFi]`/`[Ethernet]`). `go test
./script/clustergen/... ./script/devicegen/... ./schema/... ./device/...`.

## §2 — `cluster/core/wifi_network_diagnostics.go`

`WiFiReporter` interface (every method `ok=false` → null, as chip
encodes a failed provider read — cite the lines): `BSSID() ([]byte, bool)`,
`SecurityType() (SecurityTypeEnum, bool)`, `WiFiVersion()`, `ChannelNumber()
(uint16, bool)`, `RSSI() (int8, bool)` (constraint −120..0), the PKTCNT
counters `BeaconRxCount`, `PacketMulticastRxCount`, `PacketMulticastTxCount`,
`PacketUnicastRxCount`, `PacketUnicastTxCount` (`(uint32, bool)`), the
ERRCNT counters `BeaconLostCount`, `OverrunCount` (`(uint32/uint64, bool)` —
the element file's types), `CurrentMaxRate() (uint64, bool)`, and
`ResetCounts()`. `WiFiNetworkDiagnosticsConfig{Reporter, Features
(PKTCNT, ERRCNT bits), Attributes []uint32 (the optional ones to expose;
the feature-gated ones only with their feature)}`;
`NewWiFiNetworkDiagnostics(cfg)`; the three event methods (D3);
`ResetCounts` (D4). Parity test (`*parity_matterjs_test.go`): revision,
feature bits, attribute ids/types/qualities against the snapshot, the
command's conformance; behaviour tests: null on `ok=false`, counters
gated by features, `ResetCounts` accepted only with ERRCNT and resetting
the reporter, each event's wire shape (ids from the element file).

## §3 — `node/`

`node.Config` gains the Wi-Fi port (read `node/config.go` for where
network/diagnostics options live — a `Network.WiFi WiFiReporter` or
`Diagnostics.WiFi`, nil = no cluster; with it, `Features` and the
condition): when set, the root endpoint gets the server (next to the
Ethernet one, which stays — a Wi-Fi host still has `unknownEthernet`
unless the host supplies an Ethernet reporter too; say in the report
whether both on one root is what the snapshot allows: RootNode lists
each under its own condition) and `RootDeviceConditions` gains `"WiFi"`.
Test: a node with the port exposes `0x0036` on endpoint 0 with the
condition; without it, nothing changes (the existing tests hold).

## §4 — the reference bridge

`-wifi` flag (D5): `simulatedWiFi` reporter in `examples/reference-bridge`;
app-pipe commands (follow `control.go`'s CHIP-style JSON: look at how
chip's `all-clusters-app` names its Wi-Fi diagnostics test commands, if
it has any, in `../connectedhomeip/examples/` — else name them
`WiFiDiagnostics` with `Action: Disconnection|AssociationFailure|ConnectionStatus|StepCounters`)
that raise the three events and step the counters. Document the flag in
the example's README/doc as TESTING ONLY.

## §5 — the DGWIFI family in the harness (table, PICS, golden; not run)

`Test_TC_DGWIFI_2_1`, `_2_2`, `_2_3` (and `_3_2_Simulated` if the PICS
makes it applicable — read it) join `families_table_test.go`; the PICS
in `testdata/reference-bridge.pics` (`DGWIFI.S`, the attribute and event
PICS the cases key on, `DGWIFI.S.F00/F01`, `DGWIFI.S.C00.Rsp`; read the
YAML in `../connectedhomeip/src/app/tests/suites/certification/` for the
exact PICS names); the golden case list regenerated the way `doc.go`
says; the harness starts the reference bridge with `-wifi` for the
family (read how the families job passes flags). `docs/certifiability.md`
§Families gets the `DGWIFI` row with the counts the table test
computes, and the text one sentence on the simulated reporter (as
DGETH/3.2.Simulated is explained). **You do not run chip-tool**; the
owner runs `make chiptool-test` and `GOFABRIC_CHIP_FAMILIES=DGWIFI make
chiptool-families` on the tranche and reports back; write the PICS so
the three cases are expected to pass, and say in the report which steps
you expect to be sensitive (null-valued attributes, event PICS).

## Report

As the AK/AL reports: per §, guards with HEAD, brief lines that differ
from the files (element file, chip source), decisions, left out,
questions, `by_design` candidates (not written). Floors: `cluster/core`
unchanged unless cover-check asks; new example code is excluded as the
example is today.
