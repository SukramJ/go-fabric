# W4 verdict (2026-11-01, HEAD 769ea3c2)

Theme files: `wave-4-ctl-net.md` (Opus, done), `wave-4-ctl-sec.md` (Fable), `wave-4-ctl-im.md` (Opus), `wave-4-ctl-int.md` (Opus).

## Verified against the pins (Fable re-read, both sides)

| ID | Severity | Verdict |
| --- | --- | --- |
| W4-ctlnet-01 | medium | confirmed arithmetic: `transport/mrp/retransmit.go:62-63` caps at MaxInt64; `client/exchange/margins.go:59` adds `pad.Fixed` and `client/exchange/exchange.go:607` adds `InitialRetransmissionTime` after the cap → negative duration, `min` at `:609` keeps it, `AfterFunc` fires at once. Trigger (unlimited fallback retransmissions, `client/peer/connection.go:785-838`) plausible. matter.js `MessageExchange.ts:1242-1250` is safe via Infinity. Fix: saturating add, or cap after the pads. |
| W4-ctlnet-02 | medium | confirmed: `client/exchange/manager.go:936` sends to `s.Addr()`; no address adoption on an inbound new exchange, while matter.js `ExchangeManager.ts:425-431` adopts the newest address and `bridge/receive.go:159-161` does so on the device side. Fix: bind the session route to the newest inbound address on the client side too. |
| W4-ctlim-02 | medium | confirmed: matter.js `action/request/Invoke.ts:136-138` infers `timedRequest` from the command model (`cmdData.timed`) unless `options.timed === false`; `client/interaction/invoke.go:171` derives it from the options alone (`write.go:225` likewise), and the batcher sends timed-required commands untimed. Fix: consult the generated definition's timed flag. |
| W4-ctlim-03 | medium | confirmed on the matter.js side (`ClientNode.ts:291-300` closes the whole interaction); ours `controller/client/registry.go:444-446` calls `CloseForPeer` — whether the host's sustained subscription survives and retries is reviewer-confirmed, to be traced at fix time. |
| W4-ctlim-01 | medium | confirmed: `controller/client/network.go:300` forwards every report's events to `Hooks.Event` unconditionally; matter.js gates on `emitEvents` (`ClientStructure.ts:412`, default false, set only for the default subscription / first read). Fix: emit only for the default subscription's reports, keep the cursor there. |
| W4-ctlim-04 | medium | code sequence confirmed by the reviewer (`client/interaction/report.go:489→510`); impact depends on host latency — fix: send the StatusResponse before cache apply/store/hooks, as `ClientStructure.ts:417-439` defers. |
| W4-ctlint-01 | **high** | confirmed: `controller/dcl/crl.go:87` `derDecodeRec` recurses per DER level without a depth bound; `controller/dcl/revocation.go:352` reads the CRL body with `io.ReadAll` before any size check. A vendor-hosted CRL (URL from the ledger) nested deep enough overflows the goroutine stack — fatal, not recoverable. matter.js's recursive DerCodec throws a catchable RangeError. Fix: depth cap (e.g. 32) in the decoder, `io.LimitReader` on the body (DataFileSize from the ledger, else 1 MiB). |
| W4-ctlint-02 | medium | reviewer-confirmed: OTA image download buffered fully in RAM without bound (`images.go:454-461`, `ota.go:273`); matter.js streams (`DclOtaUpdateService.ts:573`). Fix: LimitReader to OTAFileSize+1 and a config cap. |
| W4-ctlint-03 | medium | reviewer-confirmed: `Images.Store` (`images.go:212-235`) validates the header only; matter.js re-verifies the file (`:388-424`). Fix: `otaimage.ValidateFile` before `Put`. |

## Counts (from the reviewers' reports)

| Theme | high | medium | low | note |
| --- | --- | --- | --- | --- |
| ctl-sec (Fable) | 0 | 0 | 2 | 6 |
| ctl-im (Opus) | 0 | 4 | 5 | 2 |
| ctl-net (Opus) | 0 | 2 | 3 | 2 |
| ctl-int (Opus) | 1 | 2 | 5 | 6 |
| **W4** | **1** | **8** | **15** | **16** |

## Fixed in PR (branch `review/w4`, 2026-11-02)

The register lines are at `769ea3c2`; each fix relocated them on `main`
2278b129 with `grep -n` and re-read the cited lines on both sides at the
pins (matter.js `862ad75f`, connectedhomeip `6170af84`).

| ID | Commit | Note |
| --- | --- | --- |
| W4-ctlnet-01 | 8959f20b | `satAdd` in `client/exchange/margins.go`, used for the fixed pad and `InitialRetransmissionTime` |
| W4-ctlnet-02 | 2befacbe | `route` adopts the source of a message that opens a new exchange; `Session.OnAddressChanged` carries the peer's operational address |
| W4-ctlnet-03 | a10d6b76 | ctx cancel ends the ack wait (`endAckWaitLocked`) |
| W4-ctlnet-04 | a10d6b76 | `initiateConnection` completes at once next to a live session |
| W4-ctlnet-05 | a10d6b76 | `Peer.Close` sweeps the sessions again after `workers.Wait()` |
| W4-ctlnet-06 | a10d6b76 | `considerClosed` semantics in `retransmit` |
| W4-ctlnet-07 | — | open note (parity), unchanged |
| W4-ctlim-01 | 8aac8e76 | `emitEvents` through `apply`/`mutate`; the default subscription's owner token |
| W4-ctlim-02 | b05adad8, 94140f5e | `schema.IsTimedInvoke` / new generated `schema.IsTimedWrite` (empty at the pin); `InvokeOptions.TimedOff` + `fabrictest.InvokeUntimed` for the two NEEDS_TIMED wire tests |
| W4-ctlim-03 | 0466b6b2 | `deleteNode` closes the node interaction's subscriptions |
| W4-ctlim-04 | 0dc4ad84 | per-node ordered job queue (bound 100) for a subscription report's store writes and hooks; the caller's `Updated` stays inline |
| W4-ctlim-05 | f0d69498 | refresh keeps the request's event filters |
| W4-ctlim-06 | f0d69498 | flush mutex; a direct flush stops the armed timer |
| W4-ctlim-07 | f0d69498 | both providers pad with `Manager.LocalPad()` |
| W4-ctlim-08 | f0d69498 | `chunkWrite` after `begin`, the exchange's session |
| W4-ctlim-09 | f0d69498 | re-check `started && !closing` before subscribing; adopt only while the owner is current |
| W4-ctlim-10 | f0d69498 | `setActive(true)` always announces |
| W4-ctlim-11 | f0d69498 | Read/Subscribe fitted after `begin` |
| W4-ctlint-03 | af994d4e | `Images.Store` runs `otaimage.ValidateFile` |
| W4-ctlint-04 | c9ca2bcd | BlueZ `WriteC1` bounded by 30 s → `btp.ErrDisconnected` |
| W4-ctlint-05 | c9ca2bcd | a cancelled ESPHome GATT op ends the connection |
| W4-ctlint-06 | c9ca2bcd | an absent maximum maps to `math.MaxUint32` (no API change) |
| W4-ctlint-07 | c9ca2bcd | `otaimage.MaxHeaderSize` = 1024; Ahead-of-matter.js row `BD-Matter-OtaImageHeaderSizeCap` |
| W4-ctlint-08 | c9ca2bcd | `(?i)` pattern, lower-case `Mode` |
| W4-ctlint-10 | 49f05b93 | 16 MiB `http.MaxBytesReader` on the three fetchers; 1000-page cap |
| W4-ctlint-11 | 49f05b93 | ADR 0027, `thread/otbr` doc, Thread Border Router guide |
| W4-ctlint-12 | 49f05b93 | cancellable periodic-update context, cancelled in `Close` |
| W4-ctlint-13 | 49f05b93 | package doc, BLE proxies guide, `BD-Controller-BleOverEsphomeProxies` |
| W4-ctlint-14 | c9ca2bcd (part) | `ReadHeader` wraps the read error; the clock/logger threading of `Probe`/`Translate`/`Discover` is left open — it needs an API addition (owner question) |
| W4-ctlsec-*, W4-ctlint-09 | — | the owner's reviewer, on this branch after the handback |
