# Brief W14 — reconnect residuals after #478 (issue 488)

Base: main at 1f1998f1. Branch `w14/488-reconnect-residuals`. One pull request. Issue 488 holds the observation and the causes with `file:line`; read it first.

## Decisions

1. **The proven host is learned from discovery, not only from the handshake.** Whenever the peer has a session and `Descriptor.OperationalHost` is empty or differs, and a discovered address equal to the descriptor's operational address (zone ignored, as `hostOwning`) carries a host, that host becomes `OperationalHost` (`changedLocked`, so the controller persists it). Sites: `followAddressLocked` as today, plus the peer's reaction to an address-set change while connected (the address monitor already observes the service — use that observation; no new goroutine, no timer), plus once before ranking (`shiftLocked`, `orderForPeer`). An address no discovered target owns leaves the host as it was (unchanged rule).
2. **While the proven host is known but no discovered address carries it, a reconnect tries the last operational address first.** It is queued as the fallback is (`maybeAttemptFallbackLocked`: today only when nothing is discovered; now also when the proven host is absent from the discovered set), ranks ahead of every other target's address, and its attempt sets the stagger like any other — the other target's address follows `DelayBeforeNextAddress` later (or at once on a failed attempt, BD-Matter-PeerAttemptFailAdvance). The existing rules stay: a non-fallback attempt starting retires the fallback; a fallback discovery then names becomes a real address; the proven host's address arriving later preempts (`preemptForProvenLocked`) unless the attempt at the same address is already running. A proven host that *is* advertised — with the same or a new address — is ranked as W12 ranks it; the old operational address is not queued then (no cost for a genuine address change). A peer without a proven host keeps the parity order.
   Best architecture: the session is the only proof of which target is the node; a cache that momentarily lost the node's SRV to another responder's cache-flush is not evidence against it. A hold-off (waiting for the host to return) was rejected in W12 and stays rejected.
3. **A link-local address leaves the service with its record, on every interface it was heard on.** `Service.deleteAddress` removes, for a link-local IP, every address of that SRV entry with the same IP whatever its zone. Accumulating one zoned address per receiving interface while the record lives stays (matter.js parity: both may route).
4. **`matter.controller.peer.probe` names the probed address.** The run's logger keeps `address` (the session's current address, unchanged for consumers); each probe adds `target=<probed address>`.

## Files

- `client/peer/peer.go` (`followAddressLocked`, a `learnHostLocked` helper), `client/peer/connection.go` (`provenHost`, `shiftLocked`, `maybeAttemptFallbackLocked`, `compareForHost`/ranking of the fallback, `preemptForProvenLocked`), `client/peer/monitor.go` (address-change observation, `orderForPeer`, probe logging).
- `client/discovery/service.go` (`deleteAddress`).
- Tests beside the W12 ones (`grep -ln 'provenHost\|OperationalHost' client/peer/*_test.go controller/client/*_test.go fabrictest/ -r`), `client/discovery/*_test.go`.
- matter.js at db021da0 for the comments you touch: `packages/general/src/net/dns-sd/IpService.ts:296-340` and `:392-408`, `DnssdName.ts:348`, `packages/protocol/src/peer/PeerConnection.ts:360-379`. Do not write new matter.js or chip claims beyond these and the ones already in the touched comments; if you need one, put it under Questions.

## Tests (each fails before the change)

1. Session established from the stored address before discovery has addresses; discovery then names the address under host H → `Descriptor.OperationalHost == H`, the changed hook ran, and a later reconnect with H and another target F discovered starts at H's address.
2. Proven host H, discovered set holds only F's addresses → first attempt at the last operational address; F's address starts after `DelayBeforeNextAddress` (fake clock), not before. H's address arriving meanwhile, equal to the operational address → no second attempt at it, the running one continues and connects.
3. Proven host H advertised with a new address only → first attempt at the new address; the old operational address is not attempted.
4. Discovery: an fe80 AAAA for target T arrives on interface a, then on b; T's SRV is deleted → `Addresses()` is empty. Same for the AAAA's own expiry.
5. The probe log carries `target` for the current address and for an alternate.

## Documents (text below is verbatim)

- `notes/parity/by_design.md`, entry `### BD-Matter-ProvenOperationalHost`: append to the paragraph, before "Register row above":
  `The host is also learned after the handshake, when discovery first names the target that owns a live session's operational address — a session made from the stored address at start precedes discovery. While the proven host is known but no discovered address carries it, a reconnect tries the last operational address first, as matter.js tries it when nothing is discovered (`PeerConnection.ts:360-379` at db021da0): another responder's cache-flush SRV removes the node's own SRV from the cache for as long as the node takes to answer again (RFC 6762 §10.2), and the session, not the momentary cache, says which target is the node (issue 488).`
- `notes/parity/by_design.md`, new entry after `BD-Matter-SuspectProbeWalksAlternates`:
  `### BD-Matter-LinkLocalZoneVariants — a link-local address leaves the service on every interface it was heard on`
  `matter.js keys a cached AAAA record by its address (`DnssdName.ts:348` at db021da0), keeps the interface of its last arrival, and zones a link-local address to it when the service reads the record (`IpService.ts:392-408`); an arrival on a second interface adds a second zoned address (`:315-328`), and the record's deletion removes only the variant of the last interface (`:330-340`). The other stays in the service's address set until the service closes. This module removes every zoned variant of the address with the record. Reason: issue 488 — a border router's link-local address, reflected onto a second host interface, stayed a connection candidate after its SRV target was gone. Retires when matter.js removes them.`
- `docs/guides/` page that describes the reconnect order (the one W12 changed; `grep -rn 'proven' docs/`): one or two sentences for decisions 1–2; the log field `target` where `peer.probe` is described, if it is.
- `CHANGELOG.md` `[Unreleased]`: `### Fixed` lines for 1–3, `### Changed` for the `target` log field.

## Guards (under `flock ~/.cache/go-fabric-scratch/guards.lock`, from a script file, summary to `~/.cache/go-fabric-scratch/w14/summary.log` with HEAD before/after and one exit line per guard)

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`. `make ci` (if golangci hits its 5-minute timeout under load, run `make lint`, `make cover-check` alone and say so); `go test -race -count=3 ./client/... ./controller/client/...`; `go test -race -count=1 ./controller/... ./fabrictest/... ./node/... ./binding/...`; `make cite-report && make reachability` in the commit that moves citations or positions, then `make cite-check reachability-check ahead-check apidiff-check surface-check`; `mkdocs build --strict` if a docs page changed. No chip-tool run: nothing device-side moves.

## Rules

Commit as `git -c user.name=SukramJ -c user.email=sukramj@icloud.com`; check `git var GIT_AUTHOR_IDENT` first; named paths only (never `git add -A`); no attribution or co-author lines; no `time.Sleep`/`time.After` in production code; logs by grep. Push the branch and open the pull request ("Wave W14: …", body in English, `Closes #488`), auto-merge off. Report in the fixed shape (per decision, guards with HEAD, differences, decisions, left out, questions, by_design candidates).
