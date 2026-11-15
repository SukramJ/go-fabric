# ADR 0032 — The device side's peer path: established on `client/`, reused where the session lives, composed in `node`

- **Status**: Accepted (owner, 2026-11-15; Review II decision 115, wave B of phase 5b)
- **Date**: 2026-11-15
- **Supersedes in part**: [ADR 0019](./0019-tcp-bdx-and-the-ota-requestor.md),
  Consequences bullet 1 — the OTA requestor no longer "initiates CASE to a
  provider through the ADR 0008 initiator generalised to one Invoke and one
  BDX transfer"; the rest of ADR 0019 stands
- **Stands**: [ADR 0008 — subscription resumption (the narrow CASE initiator)](./0008-subscription-resumption.md)
- **Related**: [ADR 0020 — commissioner and controller role](./0020-commissioner-and-controller-role.md),
  [ADR 0026 — architecture principles](./0026-architecture-principles.md) (principle 12: `client/` below the roles),
  `notes/parity/by_design.md` (the "two stacks, one path" row)

## Context

A device-side server sometimes reaches a peer a controller named: the OTA
requestor its provider, the camera's WebRtcTransportProvider the requestor
of a WebRTC session. Until this decision both went through
`bridge.Bridge.ConnectPeer`: the ADR 0008 initiator, generalised by ADR 0019
to one Invoke and one BDX transfer — CASE over the bridge's UDP socket only,
a reused session picked by the highest session id of any transport, no
single-flight, and a sender that never asked a command's Large Message
quality.

The receiving side makes that path wrong. A command with the Large Message
quality on a session that carries no large messages is answered
InvalidTransportType:

- matter.js `packages/protocol/src/action/server/CommandInvokeResponse.ts:281-285`
  at db021da0, after the existence checks (`:263-271`) and the command's
  privilege (`:273-279`), before the fabric scope (`:287-290`) and the timed
  quality (`:292`); a wildcard path leaves such a command out after the
  authority check (`:378-386`). The session's `largeMessage` is its channel's
  (`packages/node/src/behavior/context/server/RemoteActorContext.ts:208`,
  `packages/protocol/src/session/Session.ts:372-374`): true on TCP
  (`transport/tcp/TcpChannel.ts:45`), false on BLE (`ble/Ble.ts:39`).
- connectedhomeip `src/app/InteractionModelEngine.cpp:1875-1884` at 6170af84
  (`CheckCommandFlags`); a session allows a large payload exactly when its
  peer address is TCP (`src/transport/SecureSession.h:163`).

Every WebRTC signal is such a command. With the gate in place — this module
now has it too (`im.WithLargeMessageSession`) — a camera of this module
signalling over UDP is refused by every conformant requestor, including one
of this module.

The client side already has the rule, in `client/`: matter.js infers
`largeMessage` from the command's schema when the caller left it open and
then requires TCP (`packages/protocol/src/action/client/ClientInteraction.ts:111-125`,
`:716-719`, `:1235`, `:1255`); a required transport is the only one tried
and `Peer.connect` first reuses the newest live session of it
(`packages/protocol/src/peer/Peer.ts:369-376`, `:440-448`, `:449-460`), newest
meaning the highest `activeTimestamp` among the sessions neither closing nor
peer-lost (`:582-602`), a stamp that moves only on a received message
(`session/Session.ts:142-147`). The peer's session set holds every fabric
session whoever opened it (`peer/PeerSet.ts:104`, `:108-110`, `:116-125`).
connectedhomeip's camera sends its signals with
`TransportPayloadCapability::kLargePayload`
(`examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:860-861`,
`:887-888`, `:914-915`, `:941-942`), attaching first to an existing CASE
session with that capability (`src/app/OperationalSessionSetup.cpp:75-94`).
matter.js's OTA requestor reaches its provider through the node's peers
(`OtaSoftwareUpdateRequestorServer.ts:553-565`), and keeps one resumption
record per peer for both roles (`session/SessionManager.ts:261`, `:896-908`;
read by the client `case/CaseClient.ts:128` and the responder
`case/CaseServer.ts:115`, saved by both `CaseClient.ts:379`,
`CaseServer.ts:219`, `:344`).

This module holds a peer's sessions in two stacks: the bridge's (the server
socket, `secure/operational`, its own exchange routing) and `client/` (its
own socket, `exchange.Manager`). A session lives in exactly one; its keys
and counters cannot be shared.

## Decision

**Shape 1 — two owners, one path.** `node` composes one unexported peer path
that serves as `core.OtaProviderConnector` and `camera.PeerConnector`:

1. **The requirement comes from the command.** A command with the Large
   Message quality (`schema.CommandLargeMessage`) requires a session that
   carries large messages — a TCP one; any other takes any transport.
2. **Reuse where the session lives.** Of the live sessions with the peer
   that meet the requirement — the bridge's
   (`bridge.Bridge.LivePeerSession`, of any opener, never a handshake) and
   the client stack's — the one the peer sent on last is used. The
   bridge's order is the peer's last activity
   (`operational.Manager.NewestSessionForPeer`), not the highest id.
3. **Establish on `client/`.** Without one, the node's client stack
   establishes a session with the requirement as its required transport
   (`peer.ConnectOptions.RequiredTransport`); one connection process per
   peer serves concurrent callers. The stack dials TCP when the node takes
   the TCP client role (`Network.TCP.Outgoing`), states the node's own
   session parameters (those its CASE responder states), and keeps its
   resumption records in the store's one row per peer, which the
   responder reads and writes too (`handshake.StoreResumption`).
4. **The command goes through the stack that holds the session**: on the
   client stack through `client/interaction`, on the bridge's through
   `bridge.PeerSession`; a BDX transfer through `exchange.BDXExchange` or
   the bridge's exchange.
5. **No session that carries the command, none to establish**: the camera
   signal is refused before anything is sent
   (`camera.ErrNoLargeMessageTransport`).
6. **The client stack on demand.** With `Config.Client` nil the node builds
   the stack at its first peer call; `Node.Peers` stays nil and the
   binding manager behaves as before.

`bridge.Bridge.ConnectPeer` and `ConnectOtaProvider` are deprecated and
keep their behaviour until the next minor release. The ADR 0008 initiator
stays in `bridge` for the re-establishment of former subscriptions, its one
caller again; that re-establishment stays on UDP — matter.js prefers TCP
there when the peer's last known address was TCP
(`packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts:298-309`),
a gap recorded in the findings register, not a deliberate divergence.

**The direction.** The end state is one session layer and one exchange
layer for both roles, as matter.js has: "the peer's sessions" one set, the
re-establishment on the one path, `bridge/case_initiator.go` gone. It is
decided after Review II under its own ADR; the next step towards it is the
client manager on the bridge's socket with one session-id space (design
note `peer`, Shape 2).

## Better than the others, than before, the right cut

- **Than matter.js**: equal in behaviour on the path — reuse across openers,
  TCP required for a Large Message command, newest by the peer's activity,
  one record per peer. Behind it in structure: two stacks composed, where
  matter.js has one. No claim of better.
- **Than connectedhomeip's camera app**: equal on the camera path (attach
  to a large-payload session, else establish with the capability); the
  quality is inferred from the schema in one place rather than passed by
  each of the four call sites.
- **Than before**: a camera of this module reaches a conformant requestor at
  all once the gate exists; establishment gains what `client/peer` has and
  the bridge initiator lacks — TCP, single-flight, the address schedule,
  throttling by network profile, resumption across restarts — and the
  duplicated Sigma driver shrinks back to its one ADR 0008 caller.
- **The cut**: the module owns the transport rule, the composition and the
  gate; the host gains nothing to wire — a camera host sees one more error
  value, `contract/` is untouched. `node` is where the two stacks meet,
  which is the facade's job; `bridge` stays free of `client/` imports (it
  is the server side's composition root, and `client/` sits below the
  roles, ADR 0026 principle 12).

## Consequences

- `im`: the Large Message gate in the invoke path and its session fact
  (`WithLargeMessageSession`), stated by both servers — the bridge from the
  route, the client stack's server from the session's transport.
- `secure/operational`: `Entry.LastPeerActivity`,
  `Manager.NewestSessionForPeer`. `bridge`: `PeerSessionRequirement`,
  `LivePeerSession`, `PeerSession.LastActivityMonotonic`.
  `client/exchange`: `BDXExchange`. `client/handshake`: `StoreResumption`.
  `cluster/camera`: `ErrNoLargeMessageTransport`.
- A node and a controller restarted resume with a peer from the store's
  record instead of a full CASE.
- The camera needs TCP: a node whose camera signals a requestor takes the
  TCP client role, and the requestor accepts TCP (or opened a TCP session
  itself).
- A node's client stack serves no Interaction Model request on the sessions
  it opens: a peer that sends a command back on such a session — a peer
  reuses its newest live session with us (`Peer.ts:440-460`;
  `OperationalSessionSetup.cpp:75-94`) — is answered InvalidAction there.
  An open point of this shape; the single session layer closes it.
