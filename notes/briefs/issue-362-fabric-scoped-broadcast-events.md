# Brief: fabric-scoped mDNS broadcast events (issue #362 follow-up, concept v12)

Owner decision 2026-11-02: the bridge's "reconnect" and "retransmit"
broadcast events reach only the operational service of the session's
fabric, and a terminated subscription starts the "reconnect" event for its
fabric — as matter.js's `DeviceAdvertiser` drives them. One Opus run, one
PR, no release (the tag waits for the owner's ecosystem test of #375).
Model: Opus implements; `secure/operational` is a security path — Fable
re-reads every line of it and every citation before the merge.

Read map (verified lines marked): `read-362-p2-fabric-scope.md` in the
reviewer's scratchpad; the facts below are the verified ones.

## What matter.js does (862ad75f, re-read by the reviewer)

- `packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150` —
  `sessions.deleted`: when the session's fabric has no session left for the
  peer (`fabric.hasSessionForPeer(session.peerNodeId)`, `Fabric.ts:414-420`),
  `advertisementForFabric(fabric).serviceDisconnected()` — that fabric's
  advertisement only (`Advertiser.ts:69-75`).
- `DeviceAdvertiser.ts:164-172` — `sessions.retry`: a retry on a secure
  session with a fabric → `#advertiseFabric(session.associatedFabric,
  "retransmit")`.
- `DeviceAdvertiser.ts:174-179` — `sessions.subscriptionsChanged`: when
  operational, the subscription `isTerminated` and the session has a
  fabric → `#advertiseFabric(session.fabric, "reconnect")`.
- `DeviceAdvertiser.ts:356-380` — `#advertiseFabric`: nothing while
  closing; a "startup" becomes "retransmit" when any session of the fabric
  holds a subscription. **Out of scope here** (it needs an event on
  `Advertiser.Publish`); the agent adds it to
  `notes/parity/matter_behaviour_findings.md` under "matter.js pin
  862ad75f — open items", citing these lines.
- `MdnsAdvertisement.ts:132-134` `serviceDisconnected` → `start("reconnect")`;
  `Advertisement.ts:78-91` a running schedule is not reset.
- `packages/protocol/test/protocol/DeviceAdvertiserTest.ts` (391 lines) has
  no case for these three events; the parity cases are this module's own.

## Deliverables (one commit each, `(#362)` in the subject)

### 1. The secure/operational trigger names the fabric

- `secure/operational/manager.go:102-111` `SetReannounceTrigger(fn func())`
  → `SetReannounceTrigger(fn func(fabricIndex uint8))`.
- `:164-179` `fireReannounceIfPeerGone`: fire once per **(fabric, peer)
  pair** that has no session left in this sweep, not once per sweep —
  matter.js runs the check per deleted session. Collect the pairs, call
  `hook(fabricIndex)` for each distinct one after the loop; a victim with
  fabric index 0 (PASE) fires nothing.
- `bridge/securechannel.go:1009-1015` `reannounceTriggerSetter` and
  `:1043-1044` registration follow the new signature.
- Tests: `secure/operational/manager_close_parity_test.go:59`,
  `reannounce_parity_matterjs_test.go:26,41` take the fabric; add the case
  "two fabrics lose their peer in one sweep → two triggers, one each", and
  "a same-peer replacement session suppresses the trigger" stays.

### 2. The bridge knows each fabric's operational instance

- `bridge/bridge.go:2039` `AnnounceFabric(ctx, compressedFabricID, nodeID)`
  → `AnnounceFabric(ctx, fabricIndex uint8, compressedFabricID, nodeID)`;
  `:2099` `WithdrawFabric(ctx, compressedFabricID, nodeID)` →
  `WithdrawFabric(ctx, fabricIndex uint8, compressedFabricID, nodeID)`. The
  bridge keeps `operationalByFabric map[uint8]string` (fabric index →
  `svc.InstanceName`) under `b.mu`; Announce sets, Withdraw deletes; the
  shutdown path (`:1801-1813`) clears it.
- Callers: `node/node.go:511`, `:524`, `:611`, `:613` pass the index they
  already hold (`fabricInstalled`, `fabricRemoved`, `fabricUpdated`).
- `operationalInstanceFor(fabricIndex) (string, bool)` is the one lookup
  the three events use; index 0 or unknown → `false`, and the event does
  nothing (matter.js returns when the session has no fabric).

### 3. Reconnect, scoped

- `bridge/securechannel.go:1324` `triggerSessionReannounce()` →
  `triggerSessionReannounce(fabricIndex uint8)`. The `serviceDisconnecter`
  branch (`:1351-1356`) calls `ServiceDisconnected` for that fabric's
  instance only. The `TriggerReannounce` and per-record fallbacks (`:1358`,
  `:1360`) stay as they are (an advertiser without the events gets what it
  got). Doc comment `:1305-1322` rewritten: it no longer names a gap.
- Test: `bridge/securechannel_close_parity_test.go:604`
  `TestTriggerSessionReannounce_ReconnectEvent` becomes fabric-scoped —
  two operational services (two fabrics), one commissionable; the event
  for fabric 2 reaches `op2` only; fabric 0 and an unknown index reach
  nothing. Other callers (`:480`, `:495`, `:561`,
  `bridge/reannounce_owner_test.go:48,56`,
  `bridge/bridge_diagnostic_events_test.go:33,60`) pass an index.

### 4. Retransmit, scoped

- `bridge/ackpump.go:373-380`: for each re-sent result (`r.Err == nil`,
  `r.SessionID != 0`) resolve the fabric with `resolveSessionFabric`
  (`bridge/subscribe.go:1542`); collect the distinct resolvable indices
  (not 0, not `FabricIndexUnresolvable`); `:410-424`
  `retransmissionBroadcast(fabrics []uint8)` calls
  `ServiceRetransmission` once per fabric's instance.
- Test: the retransmit half of `TestTriggerSessionReannounce_ReconnectEvent`
  moves to its own test with a wired fabric resolver (see
  `bridge/peer_loss_test.go:107,233,396` for a tracker-driven tick): two
  sessions on fabric 1, one on fabric 2 retransmitted → one event each.

### 5. A terminated subscription starts the reconnect event

- `im/subscription/manager.go:197-222` `SetOnSubscriptionTerminated(fn
  func(subID uint32))` → `fn func(subID uint32, fabricIndex uint8)`;
  `:242-262` `finishClose` passes `sub.FabricIndex`
  (`im/subscription/subscription.go:36`). The hook stays single-slot.
- `bridge/subscribe.go:116`: the bridge's hook forgets the persisted
  subscription (`bridge/subscription_persistence.go:227`) **and** calls
  `triggerSessionReannounce(fabricIndex)` — the same goroutine-bounded
  path as the session teardown, so matter.js's "reconnect" semantics hold
  (`Advertisement.start` does not reset a running schedule). Not while
  the bridge is stopping (the existing guard in `triggerSessionReannounce`).
- Terminated means `Close`, `ClosePeer`, `CloseEndpoint`, `CloseFabric`
  (`manager.go:197-222`); `Release` and `CloseSession` fire nothing, as
  matter.js fires only on `isTerminated`. For `CloseFabric` the fabric is
  being removed and its instance withdrawn — the lookup in §2 then finds
  nothing, which is the intended no-op; add the test.
- Tests: `bridge/subscribe_*_test.go` — a subscription closed by the peer's
  error status (`closeSubscriptionByExchange`, `bridge/subscribe.go:634-655`)
  and one abandoned by the ack pump (`closeSubscriptionByCounter`,
  `:577-597`) each produce one `ServiceDisconnected` for the session's
  fabric; `Release` (`bridge/subscription_reestablish.go:269,286`) produces
  none. `im/subscription` tests take the new hook signature.

### 6. Registers, docs, CHANGELOG

- `notes/parity/by_design.md`: row `BD-mDNS-BroadcastEvent-AllOperational`
  → the Retired table ("Closed: …", cite the new code); the L7-D07 retired
  row's last sentence ("The remaining difference …") is dropped.
- `notes/parity/matter_behaviour_findings.md:386-394` ("No reconnect
  broadcast when a subscription ends") removed; the startup→retransmit
  conversion entry added (see above).
- `CHANGELOG.md` `[Unreleased]` `### Changed`: one line for the scoped
  events and the subscription-end reconnect; `### Removed`/migration rows
  in `docs/guides/migrating-to-the-facade.md` for the three signature
  changes (`Bridge.AnnounceFabric`/`WithdrawFabric` gain `fabricIndex`,
  `operational.Manager.SetReannounceTrigger` takes `func(uint8)`,
  `subscription.Manager.SetOnSubscriptionTerminated` takes
  `func(uint32, uint8)`); `apidiff-check` names them.
- `docs/guides/ports.md` / `advanced-seams.md` where they describe the
  reannounce trigger or `AnnounceFabric`: grep and update.
- Go comments cite the matter.js lines above (`// Mirrors matter.js …`).

## Guards (background, summary log with HEAD before/after, one exit line per guard, `flock ~/.cache/go-fabric-scratch/guards.lock`, `export PATH=$HOME/go/bin:$PATH`)

`make ci`; `CGO_ENABLED=1 go test -race -count=1 ./bridge/... ./node/... ./secure/... ./im/... ./mdns/...`; `make cite-check`; `make apidiff-check`; `make testparity-check`; `make ahead-check`; `make reachability-check`; `mkdocs build --strict`; `make chiptool-test` (commissioning only; the families are not needed — the record content does not move). Logs under `~/.cache/go-fabric-scratch/i362b/`.

## Report shape

Per §: commit, tests. Guards with HEAD. Differences from the brief.
Decisions taken (matter.js line or recorded). Left out. Open questions.
by_design candidates. The list of every matter.js/chip citation written.
PR without auto-merge; rebase onto origin/main before the PR. No
attribution lines in commits or the PR. Author must be
`SukramJ <sukramj@icloud.com>` (`git var GIT_AUTHOR_IDENT`).
