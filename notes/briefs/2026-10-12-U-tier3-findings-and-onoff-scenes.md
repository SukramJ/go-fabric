# Brief U — Tier-3 findings (TCP, mDNS `T`, BDX block size, peer loss) and the OnOff scene paths

Owner: Fable. Implementer: Opus. Branch `wave7/transport` from
`planning/briefs-wave-7`, pushed, **no PR**. Report in
`notes/briefs/reports/U.md`. Read first: the four Tier-3 bullets in
`notes/parity/matter_behaviour_findings.md` ("## Tier 3 — LOW", first
four) — each names the module lines, the matter.js lines and a fix
package; this brief binds them — and `cluster/onoff/server.go:112-124`.
matter.js at `4633bfd7`, chip at the harness pin (see
`notes/briefs/README.md`). One commit per item, reachability in each.

## 1. Close a TCP connection when its last session goes
- Module: `bridge/conn.go:160` `bindSessionRoute`, `:235` `evictConn`.
- matter.js: `ExchangeManager.ts:665-672` `#closeTcpChannelIfLastSession`
  ("Mimics CHIP SDK behavior: close TCP connection when last session is
  removed"), scheduled from `:700-706` on session removal.
- Must: per-connection session count; on every `SessionRegistry` close
  decrement and at zero close the `tcp.Conn`; test
  `TestTCPConnClosedWhenLastSessionGoes` with `bridge/tcp_test.go`'s
  harness; the 10 s first-message timer unchanged.

## 2. Omit the `T` key when no TCP mode is advertised
- Module: `mdns/service.go:204-248` (`T` always written; locks in
  `mdns/service_test.go:30,173,300,316`).
- matter.js: `MdnsAdvertisement.ts:203-206` (`T` only when
  `description.tcp !== undefined` and operational),
  `ServerNetworkRuntime.ts:334-338` (`supportedTransports` set only when
  TCP is configured). chip: `src/lib/dnssd/Advertiser_ImplMinimalMdns.cpp:279-285`.
- Must: no key without a mode; `6` with both; the commissionable record
  never carries it. Parity test rows moved accordingly; the daemon with
  `--tcp` still advertises `T=6` (`TC-SC-4.3` reads it: say in the report
  that the SC family is the owner's fixture run).

## 3. BDX block size 8192 over TCP
- Module: `cluster/core/ota_requestor_query.go:551`
  (`otaBdxMaxBlockSizeNonTCP`); the exchange's route
  (`bridge/ota_provider_link.go`), `transport.Conn.Reliable`.
- matter.js: `OtaSoftwareUpdateRequestorServer.ts:54-55` (constants 8192 /
  1024), `:960-963` (chosen by `messenger.channel.type === "tcp"`).
- Must: the initiated exchange reports whether its route is reliable; the
  requestor picks the TCP constant then; `bridge/ota_provider_e2e_test.go`
  checks the negotiated block size over a TCP-bound session.

## 4. Report-retransmission exhaustion is peer loss
- Module: `bridge/ackpump.go:349-369` (`closeSubscriptionByCounter`, then
  `handleInitiatedPeerLoss` for initiated exchanges only).
- matter.js: `MessageExchange.ts:616-621` (`peerLost` on exhaustion),
  `ExchangeManager.ts` → `SessionManager.ts:677-695` `#handlePeerLoss`
  (every session with that peer created before `asOf`).
- Must: the report-counter case takes the same peer-loss path, closing
  the peer's sessions created before the exchange; the sessions' other
  subscriptions fall with them (`SetOnSessionClose`). Test with two
  sessions of one peer and one of another in `bridge/`.

## 5. OnOff scene paths
- Module: `cluster/onoff/server.go:112-124` (what is left out),
  `cluster/core/scenes_attributes.go` `applyValues` (how
  ScenesManagement recalls OnOff today).
- matter.js `OnOffServer.ts`: `:98-100`, `:114-117`, `:120-122` (a change
  invalidates the current scene), `:159-165` (OffWithEffect stores the
  global scene when GlobalSceneControl is true, then clears it), `:175-184`
  (OnWithRecallGlobalScene recalls it), `:268-310` (scene apply with
  transition time). Read the ScenesManagement side it calls
  (`packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts`,
  grep the functions those lines call; cite).
- Must: a `Scenes` port on `onoff.Server` (`Config.Scenes`, optional)
  with the three operations those paths need (invalidate current scene,
  store global scene, recall global scene — name them after matter.js),
  implemented by `cluster/core`'s ScenesManagement for the endpoint;
  the transition-delayed apply through the existing transition seam or
  reported as left out with the reason. Parity table rows for each path;
  `cluster/onoff` and `cluster/core` floors.

Not in scope: `secure/`, `commissioning/`, the device layer (brief T),
the chip-tool tables, by_design / findings (list for the owner).

## Guards
`make ci`, `make race` (bridge, mdns, cluster/core, cluster/onoff),
`make chiptool-test` locally, `make chiptool-families GOFABRIC_CHIP_FAMILIES=OO,S,SC,SU`
locally (`SC-4.3` for the `T` key, `SU` for the block size). Report as
brief S.
