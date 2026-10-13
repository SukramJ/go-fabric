# Brief AF — `controller/exchange` and `controller/handshake`: the initiator's exchange layer, PASE client and CASE client (ADR 0020, stage 1, part 2)

Owner: Fable. Implementer: Opus. Branch `wave12/handshake` from
`planning/briefs-wave-12`, pushed, **no PR**. Report in
`notes/briefs/reports/AF.md`. Read first: `notes/briefs/README.md`,
[ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md),
concept v2 §B.1, and `bridge/bridgetest/{commissioner,handshake,transport,session}.go`
— the test commissioner that already runs PASE and CASE as the initiator
without MRP; it is the working reference for the message sequences on the
Go side and stays untouched in this brief (it moves onto `controller/` in
stage 2). matter.js at **`0ceb202d`**; paths below are relative to
`packages/protocol/src`. Read the lines a fact cites before you mirror
them. One commit per item, reachability in each, CHANGELOG under
`[Unreleased]` → Added. Guards: `make ci`, `make race`,
`make reachability-check`, and `make chiptool-test` once at the end (the
harness is yours; brief AE does not use it; it proves nothing changed on
the responder, which you do not touch). Brief AE (`wave12/discovery`)
builds `controller/discovery` and exports a query connection from
`mdns/`; you do not touch `mdns/`, `controller/discovery`, `bridge/`,
`node/`. `secure/` and `transport/` are read, not changed; a change you
need there is a question in the report.

## What exists (reuse, do not duplicate)

- `transport/udp.Listener` (`transport/udp/listener.go:59-135`: a socket
  with a `Handler(buf, src)` and `Conn(dst)` for sends),
  `transport/message` (the packet and protocol header codec),
  `transport/mrp`: `Counter` (`counter.go:29-74`), `Window`
  (`window.go:46-85`), `AckTracker` (`ack.go:127-247`, the 200 ms
  piggyback delay), `BackoffDuration` (`retransmit.go:44-60`, the
  §4.12.2.1 formula — `Retransmitter` is deprecated, do not build on it;
  `bridge/outbound_reliable.go` shows the production loop on
  `BackoffDuration`).
- `secure/channel.Session` (`secure/channel/session.go:41-173`, `Config`
  `:95-123`: keys, node ids, CATs, peer session id, initial counter) —
  encrypt/decrypt of a secure session; `secure/spake2`: `Prover`
  (`spake2.go:459-566`: `NewProver(passcode, salt, iterations, idA, idB,
  context)`, `GeneratePake1`, `ProcessPake2(y, cB) (cA, err)`,
  `SharedSecret`), the PBKDFParamRequest/Response and Pake codecs in
  `secure/spake2/wire.go` (the responder's `commissioning/pase.go` and
  `bridgetest/handshake.go:61-122` use them); `secure/sigma`:
  `NewPeerInitiator(InitiatorConfig)` (`initiator.go:96-124`: identity,
  verifier, session id, peer node id, root public key, resumption record,
  session params), `GenerateSigma1`, `ProcessSigma2Bytes`,
  `ProcessSigma2Resume`, the key schedule and `ResumptionRecord`
  (`protocol.go:114`); `secure/sessionparams.Parameters`
  (`sessionparams.go:49-191`); `secure/mattercert.NewVerifier(rootPubKey,
  clock)` (`verify.go:167`) which implements `sigma.PeerVerifier` with the
  node-id and fabric-id extractors.
- `bridgetest/handshake.go:33-59` `handshakeStep` (send, await the reply
  opcode, decode a StatusReport) and `:61-122` PASE, `:168-210` CASE: the
  sequences this brief turns into production code with MRP.

## 1. `controller/exchange` — sessions and exchanges of the initiator

The port of `protocol/ExchangeManager.ts` and `protocol/MessageExchange.ts`
for the client role, over one `transport/udp.Listener` the controller
owns. Types: `Manager` (one socket, the session table, the exchange
table, the receive loop), `Session` (unsecured session 0 for PASE/CASE,
or a secure session wrapping `channel.Session`, with the peer's session
parameters and a message-counter window), `Exchange` (one initiated
exchange).

Facts to mirror:

- An initiated exchange has a fresh exchange id from a counter and is
  indexed apart from received ones (`ExchangeManager.ts:145-156`,
  `exchangeId | 0x10000`); at most 30 outgoing exchanges per session
  (`:54`). The receive loop routes a message by session id, exchange id
  and the initiator flag to its exchange (`:220-330` `#receiveMessage`,
  read it: the duplicate check through the counter window, the standalone
  ack for a duplicate, a message for no exchange answered with a
  standalone ack when it requires one, unknown session dropped). The
  controller receives no unsolicited exchanges in stage 1 (subscription
  reports are stage 3), so a received exchange that is not ours is acked
  if it requires an ack and otherwise ignored, logged at debug.
- `Exchange.Send(ctx, messageType, payload, opts)` (`MessageExchange.ts:605-700`):
  refuses when closed; MRP ack required unless `SuppressAck` or the
  session does not use MRP; a StandaloneAck and a StatusReport always
  travel on protocol 0 whatever the exchange's protocol (`:656-659`); a
  second send while the previous message is unacked is a flow error
  (`:664-668`); the pending received message's id is piggybacked as the
  ack and its standalone-ack timer stopped (`:674-680`); on a transient
  error, timeout or network error with nothing received on the exchange,
  the manager's `peerLost` hook fires (`:613-621`, `#reportsPeerLoss`
  `:631-638`; the session-level suppression is the commissioning PASE
  case — a `Session` option).
- Retransmission (`:879-983` `#retransmitMessage`, `#initializeResubmission`;
  `MRP.ts:33-48`): up to `MAX_TRANSMISSIONS = 5` sends; interval
  `base · 1.1 · 1.6^max(0, n−1) · (1 + rand · 0.25) + fixedBackoff` with
  base = the peer's active or idle interval (active when the peer was
  heard within its active threshold) plus an additional delay
  (`MRP.ts:162-181`) — `mrp.BackoffDuration` is that formula; the
  standalone ack for a received message that needs one goes after 200 ms
  (`MRP.ts:48`, `AckTracker`'s delay).
- `Exchange.Next(ctx, opts)` (`:822-860`): waits for the next message on
  the exchange with a timeout = the maximum peer response time
  (`MRP.ts:79-110`: the peer's worst-case MRP wait for 5 transmissions
  plus ours plus the expected processing time (default 2 s, `:55`) plus a
  5 s buffer (`:56`)); on expiry `PeerMessageMissingError`.
- `Exchange.Close` (`:1053-1155`): sends the pending standalone ack,
  waits for our last message's ack up to the retransmission budget, then
  removes the exchange; the StatusReport decode of `bridgetest/transport.go:217`
  moves here as an exported helper.
- Session ids: the next free id round-robin; when all are taken the
  oldest inactive session is closed and its id reused
  (`session/SessionManager.ts:532-560`). The unsecured session has id 0,
  a fresh message counter, no encryption (`session/UnsecuredSession.ts`).
  A secure session carries `peerSessionParameters` (fallbacks overlaid by
  what the peer reported) and the reported ones separately
  (`SessionParameters.ts:54-90`); encryption is `channel.Session`.
- `Manager.CloseSession(sess)` sends CloseSession on a fresh exchange
  before dropping a secure session (`ExchangeManager.ts:716-726`), as the
  bridge does for the responder.

Everything above is for UDP; TCP is stage 3 material and not here. No
group sessions.

## 2. `controller/handshake` — PASE client (`session/pase/PaseClient.ts`)

`PASE(ctx, mgr, addr, passcode, PASEOptions) (*exchange.Session, error)`,
on a fresh exchange over the unsecured session to `addr`, protocol 0:

1. Random 32-byte initiator random, a free session id (`:136-137`);
   PBKDFParamRequest with passcode id 0 (`pase/PaseMessenger.ts:24`),
   `hasPbkdfParameters = false`, and our session parameters (`:140-150`).
2. PBKDFParamResponse: the PBKDF parameters are required (`:158-160`);
   the responder's session parameters, when present, become the
   unsecured session's timing at once (`:163-165`) and the peer's
   parameters are our fallbacks overlaid by them (`:168-171`).
3. SPAKE2+ context = SHA-256 of `"CHIP PAKE V1 Commissioning"` ‖ request
   bytes ‖ response bytes (`PaseMessenger.ts:25`, `:174-178`); Pake1,
   Pake2 (`spake2.Prover`), a verifier mismatch is `ErrPasscodeMismatch`
   (`:186-190`); Pake3 is sent without honouring ctx cancellation
   (`:192-195`: the device committed its state) and the StatusReport
   Success awaited (`:197`).
4. The secure session: fabric none, peer node id unspecified, the
   responder's session id, Ke as the shared secret, empty salt, initiator,
   not a resumption, the peer parameters and the reported ones
   (`:199-212`).
5. On any error after the request was acked and that is not a network,
   transient or retransmission-limit error, send StatusReport InvalidParam
   even when ctx was cancelled, failures ignored, then return the error
   (`:94-115`: the device would otherwise stay locked for 60 s).

## 3. `controller/handshake` — CASE client (`session/case/CaseClient.ts`)

`CASE(ctx, mgr, addr, fabric, peerNodeID, CASEOptions) (*exchange.Session, resumed bool, err error)`
where `fabric` carries the `sigma.Identity`, the root public key, the IPK
and a `mattercert.Verifier`, and `CASEOptions` the CATs, the resumption
store and the initial-send retransmission options (`:76-80`, applied to
Sigma1 only):

1. A resumption record is looked up by (fabric, peer node id) (`:127`);
   with one, Sigma1 carries the resumption id and MIC (`:128-147`),
   without, the plain Sigma1 (`:148-164`); the destination id is the
   fabric's for the peer and the random (`:138`, `:153`);
   `sigma.NewPeerInitiator` does both.
2. Sigma2Resume (`:168-233`): refused without a record; the session
   parameters are ours overlaid by the record's, overlaid by the
   responder's (`:184-188`); the MIC is checked, the session salt uses the
   old resumption id, the record gets the new id; Success is sent before
   the session is added (`:220-222`) and a send failure only logged.
3. Sigma2 (`:234-367`): the responder's session parameters update the
   unsecured session's timing (`:245-247`); the NOC's node id must be the
   expected peer, its fabric id the fabric's, an ICAC's fabric id (when
   set) the fabric's, and the chain must verify (`:288-309`); Sigma3 and
   the Success status; the session's CATs are the option's or the
   record's (`:334`); a new record with the responder's resumption id
   (`:357-364`) overwrites a stale one.
4. The record is saved after the session exists; a save failure is a
   warning (`:369-381`). On error, InvalidParam is sent unless ctx was
   cancelled, the last message is unacked, or the error is network,
   transient or retransmission-limit (`:84-92`).

`sigma.Initiator` already implements the cryptography and the Sigma2
checks of a verifier that extracts node id and fabric id; the handshake
package wires it to the exchange layer and adds what the initiator does
not do (session parameters, the InvalidParam rule, the record store).
Where `sigma.Initiator` lacks something the list above needs (e.g. the
CATs in Sigma3, the Sigma2Resume MIC check), say so in the report —
`secure/` is the owner's.

## Tests and proof

- `controller/exchange`: unit tests with a fake socket — exchange ids and
  the 0x10000 index, the 30-exchange cap, flow error on a second send,
  piggyback ack, duplicate detection and standalone ack, retransmission
  schedule (5 sends, the backoff of `MRP.ts:162-181` with a seeded
  random), `Next` timeout = `MRP.ts:79-110` for given parameters, Close
  behaviour, session-id reuse. Parity file
  `controller/exchange/parity_matterjs_test.go` with the constants and the
  formulas cited.
- `controller/handshake`: PASE and CASE against an in-process responder —
  a `node.New` node (see `node/` tests and `bridgetest` for how a device
  is started in a test) for PASE with its passcode; for CASE install a
  fabric with `bridgetest.Commissioner.Commission` (its test CA, `ca.go`)
  and then open CASE with `controller/handshake` using the same identity
  (`Commissioner` exposes what the identity needs — read `handshake.go:124-166`;
  add an exported accessor on `bridgetest` if one is missing and say so),
  including a resumption on the second CASE. Wrong passcode →
  `ErrPasscodeMismatch` and the InvalidParam on the wire; a cancelled ctx
  before Pake3 sends InvalidParam, after Pake3 does not.
- `make chiptool-test` at the end (unchanged responder).
- Coverage floors for both packages; reachability regenerated.

## Report

`notes/briefs/reports/AF.md`: the type map, the `secure/` gaps found, the
lines that differed from the brief, what stage 2 needs from here
(ArmFailSafe → … is not in this brief), open questions. The owner's
decisions follow in the report file.
