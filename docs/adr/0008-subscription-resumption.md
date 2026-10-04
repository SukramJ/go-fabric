# ADR 0008 — Server-side subscription persistence and re-establishment after restart

- **Status**: Accepted
- **Date**: 2026-10-04
- **Related**:
  [ADR 0003 — Sigma resumption extraction](./0003-sigma-resume-extraction.md),
  [ADR 0006 — Subscribe dispatch seam](./0006-subscribe-dispatch-seam.md),
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §1 / §4,
  [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md)
  (the retired `BD-Matter-SubscriptionResumption-Deferred` entry)

## Context

A restart drops every server subscription. The controller notices only when its
own liveness timer for the subscription expires — up to the negotiated
MaxInterval plus its slack — and the device looks unresponsive for that long.

`BD-Matter-SubscriptionResumption-Deferred` deferred the fix on the premise
that matter.js implements no subscription resumption. That premise is false at
matter.js HEAD. `SubscriptionsServer`
(`packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts`)
records every server subscription of a CASE session, persists it (enabled by
default, `persistenceEnabled`), and on the next start re-establishes it towards
the controller: it opens a CASE session to the peer **as the initiator**, then
sends the priming report of the former subscription under its **old
SubscriptionId**, so the controller's still-waiting `ReadClient` resumes as if
nothing happened. Recent fixes keep it on that path: #4519 (persist the
subscriptions of a node commissioned while running), #4594 (re-establish after
`stop()`/`start()` too, and drop the volatile events of the former run so the
priming report does not replay a `ShutDown`), #4586 (keep-alive timing on the
monotonic clock).

The deferral named the real obstacles, and they are still the work:

1. `im/subscription.Manager` allocates the SubscriptionId itself; there was no
   restore-with-id path.
2. Report delivery is session-bound (`bridge/subscribe.go`, the `subTargets`
   table). After a restart no session exists, and go-fabric was a CASE
   responder only, with no way to find a controller on the network
   (`mdns/` only advertises).

## Decision

Build it the way matter.js does.

### What is mirrored

| matter.js | go-fabric |
| --- | --- |
| `SubscriptionsServer.#addSubscription` — record a subscription when it becomes active (CASE sessions only; `SessionsBehavior` skips PASE) | `Bridge.persistSubscription`, called after the SubscribeResponse (and after a re-established priming report) |
| `#subscriptionCancelled` — drop the record only when the subscription is **terminated** (peer cancel through `keepSubscriptions=false`, `InvalidSubscription`/`Failure` from the peer, giving up after failed reports); a session close or shutdown keeps it | `subscription.Manager` tells *terminated* closes (`Close`, `ClosePeer`, `CloseEndpoint`, `CloseFabric`, replace-on-resubscribe) apart from non-terminating ones (`CloseSession`, `CloseFabricExcept`, `Release`); only the former fire `SetOnSubscriptionTerminated` |
| `beginRun` — the stored list becomes the *former* subscriptions and recording restarts from empty | `Bridge.Start` loads the store into the former set and clears the store |
| `reestablishFormerSubscriptions` — group by peer, connect with `REESTABLISH_SUBSCRIPTIONS_TIMEOUT` = 2 s, skip a peer that already began a normal subscribe (`subscriptionEstablishmentStarted` block-list), stop a peer's loop on a transport failure or `InvalidSubscription` | `Bridge.ReestablishFormerSubscriptions` — same grouping, timeout, block-list and break rules |
| `InteractionServer.establishFormerSubscription` — new `ServerSubscription` with the old id and the stored `maxInterval`/`sendInterval`, priming report on a device-initiated exchange with status reports suppressed and no SubscribeResponse, then `activate()` | `subscription.Manager.Restore` + `Bridge.establishFormerSubscription` |
| `Peer.connect` → `PeerConnection` → `IpService` discovery (SRV/TXT query for `<CFID>-<NodeID>._matter._tcp.local`, A/AAAA for an SRV target without addresses, `DnssdSolicitor.DefaultRetries`) → `CaseClient.pair` | `mdns.OperationalResolver` → `Bridge` CASE dial over its own UDP socket → `sigma.Initiator` |
| `CaseClient.pair` — Sigma1 with resumption when a record exists, Sigma2 verification incl. peer node-id / fabric-id checks, Sigma3, or Sigma2Resume | `sigma.NewPeerInitiator`, `Initiator.ProcessSigma2`, `Initiator.ProcessSigma2Resume` |
| `EventsBehavior` clears a volatile event store when the node goes offline (#4594) | `Bridge.Stop` drops the buffered events, keeping the numbering |

A former subscription that cannot be re-established is simply not recorded
again: the store was cleared at start, and only a subscription that becomes
active is written back. That is matter.js's drop-on-failure, exactly.

### What is persisted

One row per active CASE subscription, keyed by SubscriptionId, carrying
matter.js's `PeerSubscription` shape: SubscriptionId, peer (FabricIndex,
NodeId), attribute and event paths, `isFabricFiltered`, MinIntervalFloor,
MaxIntervalCeiling, the negotiated MaxInterval and the send interval. Data
version and event filters are not persisted (matter.js does not either), so the
priming report carries full data. The rows live in the new
`matter_server_subscriptions` table; the old, never-wired
`matter_persistent_subscriptions` API is deprecated.

Rows are also deleted when a fabric is removed. matter.js reaches the same end
state one restart later (the re-establishment of a removed fabric fails and the
row is not written back); deleting at removal time is the cheaper route to it.

### Scope boundary — the narrow initiator

The module's non-goal "no controller / commissioner role" stands. This ADR adds
exactly two capabilities, both used **only** by `ReestablishFormerSubscriptions`:

- a CASE **initiator** towards a peer that is already on one of this node's
  fabrics and already held a subscription here, and
- operational address resolution for that one peer: an mDNS query for its
  operational instance (no browsing, no commissionable discovery, no cache).

There is no API that opens a session to an arbitrary node, no PASE initiator,
no commissioning, no reading or invoking on other nodes. A change that wants
any of those is a different decision, not an extension of this one.

### Failure behaviour

Every failure is local to the peer it concerns and costs only the
re-establishment: the controller then recovers exactly as it did before this
ADR (its liveness timeout, then a fresh subscribe). Nothing fails `Start`.

- No resolver / initiator attached, or resolution or CASE does not finish
  within 2 s: the peer is skipped, its former subscriptions are dropped.
- Priming report rejected with `InvalidSubscription`: the controller no longer
  knows the subscription; the remaining subscriptions of that peer are skipped.
- Transport failure while priming: the remaining subscriptions of that peer are
  skipped (matter.js: "report sends do not declare peer loss").
- Any other error: that subscription is dropped, the next one is tried.

### How a host controls it

- `Bridge.AttachSubscriptionStore` — where rows go. `*store.Store` satisfies
  it. Unattached: a noop store, nothing is persisted (silent).
- `Bridge.SetSubscriptionPersistence(false)` — the `persistenceEnabled = false`
  switch: nothing is recorded and nothing is re-established. Default on, as in
  matter.js.
- `Bridge.AttachCaseInitiatorProvider` and `Bridge.AttachOperationalResolver`
  — the two collaborators re-establishment needs. Unattached: every former
  subscription is dropped (silent).
- `Bridge.ReestablishFormerSubscriptions(ctx)` — the host calls it once its
  CASE identities are loaded, before announcing its operational records
  (matter.js runs it before `enterOperationalMode`).

## Consequences

- A restart no longer costs each controller a liveness timeout.
- The module gains initiator-side code it previously did not have; the scope
  boundary above is what keeps that from becoming a controller role.
- Nothing here has been exercised against a real controller in CI. The
  controller-side assumptions — a chip `ReadClient` accepts a report for a
  subscription it still holds on a session the device opened, and answers an
  unknown one with `InvalidSubscription` — are matter.js's and chip's, not
  verified by this module's suite.
