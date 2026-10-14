# Brief AQ — the OTA Software Update Provider, the BDX sender roles, DiagnosticLogs over BDX (ADR 0021; concept v2 phase 8)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AP
(`device/`, `script/devicegen`, `endpoint/` validator, `controller/client`,
`cluster/core/binding.go`, `node/` binding wiring) at the same time: stay
out of those paths; `CHANGELOG.md` is shared (append your entries).
**Never run `make chiptool-test`, `make chiptool-families` or any
chip-tool binary** — the harness is single-user and the other agent
runs it; the owner runs it on the tranche. Branch `wave23/otaprovider`
from `planning/briefs-wave-23`, pushed, **no PR**. Report
`notes/briefs/reports/AQ.md`. Read first: `notes/briefs/README.md`,
[ADR 0021](../../docs/adr/0021-ota-provider-and-diagnosticlogs-over-bdx.md)
(the decision; its Context has the measured facts), [ADR 0019](../../docs/adr/0019-tcp-bdx-and-the-ota-requestor.md),
`bdx/` (every file: the messenger, the receiver flows, `FileDesignator`,
the requestor's use of it in `cluster/core/ota*.go`), `bridge/receive.go`
(protocol routing) and `bridge/bridge.go`'s package doc (how a port is
attached), `cluster/core/diagnostic_logs.go`, `node/root.go` and
`node/config.go` (how optional servers and host ports are wired),
`examples/reference-bridge/main.go` and `control.go`, the AK/AL live
tests (`controller/client/live_test.go`, the registry commissioning
several nodes), `notes/parity/by_design.md` `BD-Matter-OTAProvider-NotExposed`
and `BD-chip-DiagLogs-NoBDX` (both retire — the owner removes them; flag
every test that pins them).

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`):
OP = `packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts`
(668 lines — read it whole), the BDX protocol under
`packages/protocol/src/bdx/` (`BdxMessenger.ts`, `BdxSession.ts`,
`BdxSessionConfiguration.ts`, `bdx-session-initiator.ts`,
`flow/OutboundFlow.ts`, `flow/DrivenSendingFlow.ts`,
`flow/FollowingReceivingFlow.ts`, `FileDesignator.ts`), the OTA provider
endpoint/device file (`ls-tree | grep -i ota-provider`) and
`docs/OTA.md` if present. chip at the harness pin for DiagnosticLogs:
`../connectedhomeip/src/app/clusters/diagnostic-logs-server/`
(`DiagnosticLogsCluster.cpp`, `DiagnosticLogsCluster.h`,
`BDXDiagnosticLogsProvider.cpp/.h`, `DiagnosticLogsProviderDelegate.h`)
and the BDX parts they use (`src/protocols/bdx/BdxTransferSession.*`,
`BdxTransferProxy*`, `DiagnosticLogs.h` for `kMaxFileDesignatorLen`,
`kMaxLogContentSize`) — the checkout is sparse and at a later commit;
these files are identical to the pin (Sonnet's diff), cite the pin's
lines (`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`).
Guards: `make ci`, `make race`, `make reachability-check`,
`make chipdm-check` (if `device/` changes), `make fuzz` (BDX codec
targets), `make bench-smoke` if `bdx/` has benchmarks (export
`PATH=$HOME/go/bin:$PATH`, `CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip`;
a `make ci` failing only on golangci-lint's own timeout after "0 issues"
is rerun as `make lint`; a `node/` live test failing once with "Could
not connect to device" is rerun alone). One commit per §, reachability
in each. Allowed paths: `bdx/`, `cluster/core` (new `ota_provider*.go`,
`diagnostic_logs*.go`), `bridge/` (§1: BDX inbound routing and the
attach seam only), `node/` (§3–§4: config, the provider endpoint, the
DLOG wiring), `device/` + `script/devicegen` **only** to un-exclude
OtaProvider (0x0014) if it is excluded (coordinate: the other agent
changes `script/devicegen` for a registry — touch only the `excluded`
map and regenerate; say so in the report), `examples/reference-bridge`
(§5), `script/coverfloor/floors.go`, `CHANGELOG.md`. `controller/`,
`secure/`, `transport/`, `store/` schema, `endpoint/`, `contract/`:
read, not changed — except §6's test-side BDX receiver if it needs a
protocol handler on `controller/exchange` (a question in the report
first; a test-only seam is acceptable if `exchange.Manager.SetProtocolHandler`
suffices). `notes/parity/` and ADRs are the owner's.

Out: DCL lookups, the peer's BasicInformation validation beyond what the
QueryImage fields carry, image signing/verification (the host's image
source decides), OTA requestor changes (it exists; a gap it shows is a
question), moving the harness image pin, automated DLOG/2.1, the OTA
provider on the controller side.

## Owner's decisions

- **D1 — `bdx/` gains the sender roles** as matter.js's flows:
  `OutboundFlow` (the sender's side of a transfer), `DrivenSendingFlow`
  (sender-driven: the sender initiates with `SendInit`, waits for
  `SendAccept`, streams `Block`s, each acknowledged, `BlockEOF` ends — the
  DiagnosticLogs case) and `FollowingReceivingFlow` (receiver-driven: the
  receiver sent `ReceiveInit`, the sender answers `ReceiveAccept` and
  serves `BlockQuery`s — the OTA case). Names in Go: `bdx.Send` /
  `bdx.Serve` (or the flow names; keep matter.js's words in the doc
  comments), over the existing `Messenger`. Block size, timeouts and the
  negotiation (`BdxSessionConfiguration.ts`) as matter.js; the idle
  timeout a parameter (the provider passes 5 min, OP:296).
- **D2 — the provider is `core.OTAProvider`** (`ota_provider.go`), the
  port of OP with host ports:
  `ImageSource` (`Query(ctx, QueryImageRequest) (Image, bool, error)` —
  the image for vendor/product/version/protocols, as a `bdx.FileDesignator`
  plus software version and string; false = NotAvailable),
  `Consent` (`Request(ctx, …) (granted bool, delay time.Duration, error)`,
  OP:248: the default delay 120 s, Discontinue without consent, OP:434-435),
  `ApplyPolicy` (nil: Proceed). Defaults as OP: the update token 32
  random bytes, DelayedActionTime capped at 86 400 s, Busy with a
  2-minute retry while an image is in flight (OP:218, :235), an
  in-progress entry stale after 15 minutes, `BdxSynchronous` preferred
  (OP:289), the URI `bdx://<root node id 16 hex>/ota/<file>`
  (`bdx.FileDesignator.BdxURI`), `NotifyUpdateApplied` clears the entry.
  Statuses and every field of the three responses as OP; cite each.
- **D3 — the provider has its own endpoint**, device type OtaProvider
  (0x0014) with the generated `device/` type (un-exclude it, D1 of the
  path rules); `node.Config.OTAProvider{ImageSource, Consent, ApplyPolicy}`
  (nil ImageSource: no endpoint) adds the endpoint to the node's
  topology (the Aggregator's sibling, not under it — read how `node/`
  assembles the root's children and cite matter.js's
  `endpoints/ota-provider.ts`). `BD-Matter-OTAProvider-NotExposed`
  retires (owner).
- **D4 — the bridge routes BDX inbound** to a `bdx.Responder` the node
  attaches (`Bridge.AttachBDXResponder` in the package doc's port list,
  fail-closed when unattached: BDX messages are answered as today);
  transfers run on the session the `ReceiveInit` arrived on.
- **D5 — DiagnosticLogs follows chip:** `core.DiagnosticLogs` with
  `TransferProtocol = BDX` becomes a BDX initiator and sender on the
  requesting session (`BDXDiagnosticLogsProvider.cpp`: a new exchange,
  `SendInit` with the request's designator, the command answered Success
  only after `SendAccept`, then 1024-byte blocks, 5-minute idle timeout,
  `EndLogCollection` on completion or failure); the statuses as chip:
  Exhausted when the whole log fits the response (then no transfer),
  Denied when the provider fails, Busy while a transfer runs,
  InvalidCommand without a designator, ConstraintError above
  `kMaxFileDesignatorLen` (32), the oldest entry's timestamps in a
  Success response (`StartLogCollection`). The `LogProvider` host port
  grows what chip's delegate needs (`StartLogCollection`, `CollectLog`,
  `EndLogCollection`, `GetSizeForIntent`) — keep the existing port
  working for a host that only hands over a byte slice (an adapter).
  Cite chip line by line; `BD-chip-DiagLogs-NoBDX` retires (owner).
  Say in the report which of the differences Sonnet listed
  (`read-phase8.md` is not in the repo; the ADR's Context names them)
  you changed and which the generated request decode already covered.
- **D6 — no new dependency; no cgo.** The test image is bytes in the
  example, not a file in the repo over 64 KiB.

## §1 — BDX sender roles and bridge routing (`bdx/`, `bridge/`)

D1, D4. Tests: `bdx` round trips sender→receiver in-process through the
existing receiver for both drives (every message of
`BdxMessenger.ts` once), block size negotiation, the idle timeout, an
aborted transfer (`BlockQuery` after `BlockEOF`, a `StatusReport`), fuzz
targets for any new codec; `bridge` routes a `ReceiveInit` to the
responder and refuses BDX without one (the fail-closed answer, cited
from matter.js `BdxProtocol.ts`'s unexpected-message status if it has
one, else `ErrUnknownProtocol` as today).

## §2 — the provider (`cluster/core/ota_provider.go`)

D2. Parity test against the snapshot (revision, commands, response
fields) and behaviour tests per command and status (NotAvailable,
Busy with retry, the token, DelayedActionTime cap, Discontinue without
consent, Proceed, the stale entry, NotifyUpdateApplied).

## §3 — the provider endpoint (`node/`, `device/`)

D3. Test: a node with `Config.OTAProvider` exposes endpoint N of type
0x0014 with cluster 0x0029 and a valid Descriptor; without it nothing
changes (the existing node tests hold); `make chipdm-check`.

## §4 — DiagnosticLogs over BDX (`cluster/core/diagnostic_logs.go`, `node/`)

D5. Tests per status, the designator rules, the transfer (an in-process
receiver through `bdx`), Busy during a transfer, Exhausted without one,
the timestamps; the existing DiagnosticLogs tests updated where chip's
statuses differ (list them).

## §5 — the reference daemon (`examples/reference-bridge`)

`-ota-provider` (TESTING ONLY): a provider endpoint with one embedded
test image (software version +1 over the daemon's, a few KiB), consent
granted, Proceed; the app-pipe can withhold consent for a test. Document
in the example's doc as TESTING ONLY.

## §6 — the proofs

`TestLiveOTAProvider` (in `node/`, after the AK/AL pattern): two
in-process nodes commissioned into one fabric through `client.Registry`
— a provider node (`Config.OTAProvider` with a test image) and a
requestor node (the existing requestor); the controller writes the
provider's location into the requestor's `DefaultOTAProviders`
(`cluster/core`'s requestor reads it — check how it is told to query:
`AnnounceOTAProvider` or the attribute) and the requestor queries,
downloads over BDX (receiver-driven), applies (whatever the requestor's
apply port does in-process), and `NotifyUpdateApplied` reaches the
provider; assert the provider's log of the three commands and the
requestor's `UpdateState` sequence. `TestLiveDiagnosticLogsBDX`: the
controller invokes `RetrieveLogsRequest(BDX, "logs.txt")` on a node whose
`LogProvider` holds 5 KiB; a BDX receiver on the controller's exchange
(test-only, built on `bdx`'s receiver over `exchange.Manager.SetProtocolHandler`
— say in the report what it took) receives the file; the command answered
Success after `SendAccept`; a 100-byte log answers Exhausted with the
content in the response.

## Report

As AK–AP: per §, guards with HEAD, brief lines that differ from the
files (matter.js, chip), decisions, left out, questions, `by_design`
candidates (not written), the two entries to retire with the tests you
changed. Floors: `bdx/` unchanged unless cover-check asks; the new
`cluster/core` code under its floor.
