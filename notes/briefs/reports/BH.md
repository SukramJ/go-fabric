# Report BH — milestone 2, phase 16: the port of `packages/protocol/test`

Branch `m2/phase-16-protocol` from `origin/main` at d401168. matter.js read
only at the pin (`git -C ../matter.js archive|show 862ad75f`). The
implementing agent split each tranche over parallel porting agents on
disjoint production packages (each fix only in a package its agent owned) and
committed per matter.js file, with the register entry, the regenerated page
and the ratchet in the same commit. "ported" in the tables counts every `it`
of the file the Go tests now cover (the cases ported before this wave
included); "missing" is what the register still lists.

## BH-1 — wire core

27 files, 345 open `it` cases before the tranche: 215 ported, 130 left as
`missing` with a reason in the register. 4 files `ported`, 23 `partial`;
`testparityPlannedRatchet` 134 → 125.

| matter.js file | status | Go test files | ported | missing |
| --- | --- | --- | ---: | ---: |
| `codec/MessageCodecTest.ts` | partial | `transport/message/messagecodec_parity_matterjs_test.go`, `groups/messagecodec_parity_matterjs_test.go` | 17 | 1 |
| `codec/MessagePrivacyTest.ts` | ported | `secure/channel/privacy_parity_matterjs_test.go` | 5 | 0 |
| `events/NonvolatileEventStoreTest.ts` | partial | `im/nonvolatile_event_store_parity_matterjs_test.go` | 3 | 8 |
| `events/VolatileEventStoreTest.ts` | partial | `im/volatile_event_store_parity_matterjs_test.go` | 11 | 9 |
| `interaction/InteractionClientMessengerTest.ts` | partial | `controller/interaction/interaction_client_messenger_parity_matterjs_test.go` | 10 | 21 |
| `protocol/DeviceAdvertiserTest.ts` | partial | `secure/operational/reannounce_parity_matterjs_test.go`, `bridge/advertiser_parity_matterjs_test.go` | 7 | 5 |
| `protocol/ExchangeManagerTest.ts` | partial | `controller/exchange/exchangemanager_parity_matterjs_test.go` | 6 | 4 |
| `protocol/MRPTest.ts` | partial | `controller/exchange/mrp_parity_matterjs_test.go` | 16 | 9 |
| `protocol/MessageChannelTest.ts` | partial | `controller/exchange/messagechannel_parity_matterjs_test.go` | 2 | 2 |
| `protocol/MessageCounterTest.ts` | partial | `groups/messagecounter_parity_matterjs_test.go` | 14 | 5 |
| `protocol/MessageExchangeTest.ts` | partial | `controller/exchange/messageexchange_parity_matterjs_test.go` | 30 | 14 |
| `protocol/MessageReceptionStateTest.ts` | partial | `transport/mrp/messagereceptionstate_parity_matterjs_test.go` | 32 | 1 |
| `protocol/ProtocolStatusCodeTest.ts` | ported | `controller/exchange/protocolstatuscode_parity_matterjs_test.go` | 3 | 0 |
| `protocol/TcpSessionBindingTest.ts` | partial | `bridge/conn_tcp_parity_matterjs_test.go` | 14 | 7 |
| `protocol/interaction/AttributeDataDecoderTest.ts` | partial | `controller/interaction/attribute_data_decoder_parity_matterjs_test.go` | 16 | 1 |
| `protocol/interaction/AttributeDataEncoderTest.ts` | partial | `im/attribute_data_encoder_parity_matterjs_test.go`, `bridge/attribute_data_encoder_parity_matterjs_test.go` | 6 | 1 |
| `protocol/interaction/EventDataDecoderTest.ts` | partial | `controller/interaction/event_data_decoder_parity_matterjs_test.go` | 3 | 2 |
| `securechannel/SecureChannelMessengerTest.ts` | partial | `controller/exchange/securechannelmessenger_parity_matterjs_test.go` | 1 | 4 |
| `session/SecureSessionTest.ts` | partial | `secure/channel/session_parity_matterjs_test.go`, `groups/session_parity_matterjs_test.go` | 9 | 2 |
| `session/SessionIntervalsTest.ts` | partial | `controller/exchange/intervals_parity_matterjs_test.go` | 9 | 4 |
| `session/SessionManagerTest.ts` | partial | `secure/operational/sessionmanager_parity_matterjs_test.go`, `groups/sessionmanager_parity_matterjs_test.go`, `controller/exchange/sessionmanager_parity_matterjs_test.go` | 17 | 19 |
| `session/SessionParametersTest.ts` | partial | `controller/exchange/params_parity_matterjs_test.go` | 13 | 2 |
| `session/secure/CasePairingTest.ts` | ported | `secure/sigma/parity_matterjs_test.go` | 3 | 0 |
| `session/secure/PasePairingTest.ts` | partial | `secure/spake2/parity_matterjs_test.go`, `controller/handshake/pase_parity_matterjs_test.go` | 8 | 2 |
| `transport/tcp/TcpChannelTest.ts` | partial | `transport/tcp/conn_parity_matterjs_test.go` | 24 | 1 |
| `transport/tcp/TcpTransportTest.ts` | partial | `transport/tcp/listener_parity_matterjs_test.go` | 5 | 6 |
| **total** | | | **305** | **130** |

The reason of every remaining `missing` is in the register entry's `reason`.
The large groups: the network-profile MRP margins (20 cases over MRPTest and
MessageExchangeTest), `abandonSubscription` and write chunking (19 cases of
InteractionClientMessengerTest), the event store's persisted events and
delete/close (17), the wall-clock-step cases (9 over SessionManagerTest and
ExchangeManagerTest), and values a Go type cannot hold (out-of-range
counters, a uint8 status 0x7777, a uint16 SAT above 65535, JS value shapes).

### Fixes

| Production file | matter.js (pin lines) | What |
| --- | --- | --- |
| `transport/message/packet.go` | `codec/MessageCodec.ts:429-437` | New `ProtocolHeader.MarshalPayload` (+ `ErrEncodeSecuredExtensions`), the checked encoder that refuses Secured Extensions as `encodePayload` does. |
| `transport/tcp/conn.go` | `transport/tcp/TcpChannel.ts:85-98,183-185,281-296` | A connection the peer closes hands every message completed before the close to the handler before it reports closed (it could drop them); proven by "delivers queued messages then terminates on close". |
| `bridge/conn.go` | `protocol/ExchangeManager.ts:655` | A dropped TCP connection logs one debug line per evicted session. |
| `secure/channel/group.go` | `session/GroupSession.ts:260-269` | `EncryptGroup` builds the nonce from the header's Source Node ID and refuses a disagreeing sender id (`ErrSourceNodeMismatch`). No key material changes. |
| `secure/operational/manager.go` | `session/SessionManager.ts:272-277` | Only a session the peer established runs the per-peer cap check; a session we initiate no longer evicts one of the peer's. |
| `im/eventlog.go` | `events/BaseEventStore.ts:62-67,141-147` | `DropBuffered` persists the current numbering as the ceiling first, so a restart continues at last+1. Same store key, a smaller value. |
| `controller/exchange/exchange.go` | `protocol/MessageExchange.ts:63-77,1234-1251`; `:125-130,794-797,812-825,987-997`; `:485-495,564-617` | BDX retransmission cap 7.4 s; new `SendOptions.AckWaitAbort` (ends only the ack wait); a duplicate of the message a pending BDX reply answers retransmits the reply at once instead of a standalone ack. |
| `controller/interaction/report.go` | `types/src/tlv/TlvArray.ts:146-150`, `interaction/AttributeDataDecoder.ts:310-344`; `interaction/InteractionMessenger.ts:985-1000` | A list chunk at the list's length index grows the list, a null chunk past the end is a no-op, repeated non-list values resolve to the highest data version; a StatusResponse Success in place of a report is a `*StatusError`. |
| `controller/interaction/write.go` | `interaction/InteractionMessenger.ts:103-106,1335-1354,1388-1392` | A write that does not fit one message, or a timed write whose WriteResponse might not, is refused with `ErrRequestTooLarge` before anything is sent. |

All in `CHANGELOG.md` `[Unreleased]` (### Added for the three new exported
names, ### Fixed). None lands in `node/`, `controller/client` or `fabrictest`.

### Register candidates

1. **Group sender node id after UpdateNOC** — `groups/sender.go:208,225`
   captures the node id at creation; `Reload` keeps it and nothing reloads on
   UpdateNOC. matter.js reads it per message (`session/GroupSession.ts:180-205`,
   `ExchangeManagerTest.ts:345-368`). Found by two agents independently;
   outside the owned packages of both.
2. **Empty ICAC in the CASE signed data** — `secure/sigma` `signedDataBytes`
   (`sigma.go:811-822`) drops an ICAC field that is present but empty; the
   CasePairingTest vector carries `300200` and its rebuild does not match.
   matter.js re-encodes the field as present (`session/case/CaseServer.ts:294-299`).
   Signature path of `secure/`: not touched.
3. **PASE failure path** — `bridge/securechannel.go:756-770` always answers
   InvalidParam and has no "still unacked" guard (`PaseServer.cancelPairing`).
4. **Network-profile MRP margins** — `MRP.ts:16-27,79-120,162-190`,
   `MessageExchange.ts:1214-1222` (`marginFor`, `additionalDelay`,
   `localFixedBackoff`): a feature across `transport/mrp` and
   `controller/exchange`, 20 missing cases.
5. **Write chunking across messages** — `InteractionMessenger.ts:1269-1460`
   (about 200 lines); Go now refuses where matter.js chunks.
6. **abandonSubscription / reportAwaitsStatus** —
   `InteractionMessenger.ts:1056-1137`, used at `ClientInteraction.ts:1096`;
   `AckWaitAbort` now exists in `controller/exchange` to build it on.
7. **Exchange closing/closed observables** — `MessageExchange.ts:1095-1115,1160-1185`.
8. **Device advertiser** — the operational record carries the mdns default
   SII/SAI/SAT, not the node's session parameters
   (`DeviceAdvertiser.ts:363-364`, needs a bridge API); no ICD advertisement
   provider (`DeviceAdvertiser.ts:195-197,356-380`); no operational-mode gate
   on refresh (`DeviceAdvertiser.ts:206-209`).
9. **TCP dialer pool** — no per-address connection reuse (`TcpTransport.ts:149-169`).
10. **Event store** — epoch 0 selects chip's 0x10000 where matter.js counts
    block size 0 as 1 (`BaseEventStore.ts:43-51`; `node/node.go` passes 0,
    brief BI's path); no delete-by-number and no plain clear.
11. **Session parameter checks** — local `MaxPathsPerInvoke` 0 not rejected
    (`SessionManager.ts:368-374`); `sessionparams.DecodeFields` masks an
    oversized TLV value (0x10009 → 9) instead of rejecting it.

### Guards (HEAD 5ca3de5, clean tree)

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 0 |
| `make testparity-check` | 0 |
| `make cite-check` | 0 (drift 266 → 265, ratchet lowered) |
| `make reachability-check` | 0 (regenerated; unreached 5, unchanged) |
| `make cover-check` | 0 on the rerun; the first run hit the 10-minute timeout in `controller/interaction` `TestLiveSustainedSurvivesRestart` (see Questions) |
| `make chiptool-test` | skipped: the harness is brief BI's (owner decision 0.12); the device-side fixes (session cap, group nonce source, event-number ceiling, TCP close order) are covered by the chip-tool jobs of the PR |

### Differences to the brief

- `SecureSessionTest`, `SessionManagerTest`, `MessageCodecTest`,
  `DeviceAdvertiserTest`, `AttributeDataEncoderTest`, `PasePairingTest` are
  split over two or three Go files, one per package whose subject they test;
  each header names the matter.js file, the register's `go` list names every
  function.
- The `controller/exchange` fix is three behaviours in about 75 logic lines
  (diff +122/−11 with a funlen split) — above "a few dozen"; kept because
  each is one unambiguous matter.js passage proven by its ported case. The
  owner may prefer to split it out.

### Decisions for the owner

- `controller/interaction.Write` refuses every oversized write, also the
  non-timed ones matter.js would chunk (before: an oversized message went out).
- AttributeDataEncoder "chunk array": the bridge splitter fuses
  `chunkAttributePayload` with the messenger's first-element packing
  (`InteractionMessenger.ts:657-664`), so the expectation is REPLACE [29] +
  ADD 40 where matter.js shows [] + ADD 29 + ADD 40.
- §8.9 "rejects" cases are asserted as "exactly one of data/status goes on
  the wire": Go cannot construct both or neither.
- MessageCounter runs on the group data counter's fixed configuration
  (reserve 1000, difference 100 000), the rollover vectors taken at the same
  distance.
- SLEEPY's activeThreshold 0 means "not stated" in Go (4 s fallback); the
  port advances 5 s first.
- "supportedTransports setter updates context" is ported as `Config.TCP`
  at `New` (no setter after construction).
- New exported names: `message.ProtocolHeader.MarshalPayload`,
  `message.ErrEncodeSecuredExtensions`, `exchange.SendOptions.AckWaitAbort`,
  `channel.ErrSourceNodeMismatch`.

### Left out

The `until` entries; nothing else of the tranche's scope.

### Questions

1. `TestLiveSustainedSurvivesRestart` (brief BG's migration onto
   `fabrictest`) hung once under the full `-cover ./...` load:
   `Device.Restart` → `ReestablishFormerSubscriptions` waits for the
   priming report's StatusResponse on the site's Fake, which nobody advances
   while `Restart` blocks, so a lost or late answer never times out. 120
   race runs of the test in isolation (8 in parallel) passed. Pre-existing
   by its shape; whether a BH-1 change made it likelier is not proven
   either way.
2. Should the network-profile MRP margins become a planned feature (20 cases)?
3. The wall-clock-step cases need a clock whose wall reading can step apart
   from the monotonic one — a `clocktest` addition, or a by_design entry?

### by_design candidates

- **Zero means unset in session parameters.** A zero field of
  `sessionparams.Parameters` means "not stated" (`secure/sessionparams/sessionparams.go:44-48`),
  so zero, fractional or untyped maxPathsPerInvoke, an SAT above 65535 and
  the object form of supportedTransports (`session/SessionParameters.ts:65-80`,
  `session/SessionManager.ts:368-379`) are unrepresentable rather than
  checked at run time.
- **Monotonic time instead of wall-clock tests.** Go's `time.Time`
  comparisons use the monotonic reading, so a wall-clock step does not reach
  session activity, eviction or peer-loss comparisons that matter.js guards
  separately (`SessionManagerTest.ts:794-924`, `ExchangeManagerTest.ts:178-186`;
  `Session.ts:58-59`, `SessionManager.ts:627-628,696`).
- **Flat client report.** `controller/interaction/report.go` mirrors the
  streaming `action/client/InputChunk.ts:174-245,336-354`; the deprecated
  per-path grouping of `interaction/AttributeDataDecoder.ts:162-181` and
  `EventDataDecoder` (3 missing cases) is not ported.
- **Event number epoch.** `EventLog` epoch 0 is
  `CHIP_DEVICE_CONFIG_EVENT_ID_COUNTER_EPOCH` (0x10000) with an inclusive
  ceiling; matter.js `events/BaseEventStore.ts:43-51,153-162` counts block
  size 0 as 1, defaults to 1000 and stores the exclusive next number. Neither
  reuses a number.
