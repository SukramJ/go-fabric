
# Brief: issue 465, review fixes — the macOS host-name verdict moves to the Responder (2026-11-08)

Follow-up to `2026-11-08-465-darwin-hostname.md` after the Linux split
review of f9b2dc83 (PR #468, comment of 2026-11-08 16:07Z: M1, M2, L1, L2,
L3, N1, N2). Same worktree (`../go-fabric-465`), same branch
(`465/darwin-hostname`), on top of d041e686. One commit, same author rule,
no push. **One shape for all findings** (decided by the session):

> The builders never take the verdict; `defaultHostName()` is the
> MAC-derived name on every platform, cheap, no lookup, no logger. A Service
> a builder named carries an unexported marker. The macOS verdict is the
> Responder's: taken at Publish, only without an override, only for a
> defaulted name, before `r.mu`; re-taken on the interface-change path;
> "served" means served **for this host**; "unanswered" is cached only
> when an addressed interface exists.

## Findings → changes

**M1 — served for this host.** `osHostNameServed` (`mdns/service.go:151`)
counts the lookup as "served" only when at least one returned address is
assigned to a local interface. Seam: a `localAddrs func() ([]net.Addr, error)`
field on `hostNamePolicy`, default `net.InterfaceAddrs`; compare with
`IP.Equal` on the `*net.IPNet`/`*net.IPAddr` IPs, zones ignored. A lookup
that resolves only to foreign addresses is the verdict "foreign": the
MAC-derived name, logged once at Warn `mdns.hostname.os_foreign name=
addrs=<resolved> fallback=<mac>`. Test: resolves to a foreign address →
MAC name; resolves to a mix with one local → OS name.

**L2 + L3 — the verdict is the Responder's, outside the lock.**
- `Build*Service` (`service.go:442-444`, `:708-710`): `host = defaultHostName()`
  (MAC-derived; the chooser; no logger parameter any more) and the returned
  `Service` gets `hostNameDefaulted: true` — a new **unexported** field on
  `Service` (`service.go:245`), documented: "set by the builders when
  HostName was left empty; a Service built by hand never has it".
  `mdns/commissioner.go:79` follows.
- `Responder.Publish` (`responder.go:262`): **before** `r.mu.Lock()`, when
  `r.HostName == "" && svc.hostNameDefaulted`, `host = r.policy().hostName(r.logger())`
  (OS name when served-for-this-host, else `svc.HostName`, the MAC name
  the builder put in). With an override (`r.HostName != ""`) nothing is
  looked up and nothing is logged. `r.policy()` is `defaultHostPolicy`
  unless a test injected one (`Responder.policy *hostNamePolicy` unexported
  field, nil = default). Keep `Validate` as it is (an empty HostName stays
  rejected; the builder's MAC name satisfies it).
- Remove the lookup from everything the builders call. `defaultHostName`
  keeps its name and the "" → MAC-derived semantics callers rely on
  (`bridge/bridge.go:2131-2133` comment names it); only the darwin branch
  leaves it.

**M2 — re-take on interface change.** matter.js re-reads the host name on
every interface change and publishes on **all** interfaces when it changed
(`MdnsServer.ts:40-43` doc, `#publishChange` `:321-339`: `previousHostname`,
`#updateHost()`, `changed === undefined` → `#repeatOn = "all"` at db021da0).
Mirror: `interfacesChanged` (`mdns/follow.go:95`) first takes the verdict
again **outside `r.mu`** (only when some published entry is defaulted and
no override is set — read that under a short lock, or keep a flag),
then under the lock: for every entry whose host moved, rebuild it the way
Publish replaces an entry (`stopLocked(prev)`, new `entry` with
`namesOf(svc, host)`, `ownsHost`, `fingerprint`; same `order` position),
and publish on **every** interface with the goodbyes of the old SRV
target's records — read `#updateHost` (`MdnsServer.ts` from `:454
#chooseHostname` on, at db021da0) and `publishEverywhereLocked`
(`mdns/publication.go:335`) / `removeLocked` (`responder.go:321`) for how
goodbyes are carried today, and reuse that path; cite the lines you read.
When the host did not move, `interfacesChanged` behaves exactly as today.
The policy's cache therefore becomes "until the next interface change",
not "once per process": replace `sync.Once` with a mutex-guarded verdict
plus an explicit `retake()`; the first `hostName()` after a `retake()` asks
again.

**L1 — no caching without an addressed interface.** The verdict "unanswered"
(and "foreign") is cached only when the chooser found an up interface with
addresses and a non-zero MAC (`hostNameChooser.choose` returned a MAC name).
Otherwise the verdict is not stored; the next `hostName()` asks again, and a
Debug line `mdns.hostname.verdict_deferred` says why. The "served" verdict
is always cached.

**N1 — docs.** `docs/getting-started/device.md:193`: add the half-sentence —
on macOS the OS host name instead, when mDNSResponder serves it for this
host, otherwise the MAC-derived name as everywhere; link `by_design.md`
L7-D03 with an absolute GitHub URL (the strict build rejects relative
ones). `mkdocs build --strict` is not available on this Mac natively; say
so and keep the change to one sentence.

**N2.** Falls away: the builders no longer look up, so
`TestBuildOperationalService_EmptyHostName_UsesDefault` (`service_test.go:~737-763`)
goes back to expecting the 16-hex shape on every platform. Revert the
either-or.

**Register and changelog.**
- `notes/parity/by_design.md` L7-D03 (line 921): the condition becomes
  "answered **for this host** (an address of a local interface) within 1 s";
  the verdict is taken at Publish without an override, re-taken on every
  interface change (matter.js `MdnsServer.ts:40-43`, `:321-339`), not cached
  while no interface has addresses; the remaining window: between a rename
  by mDNSResponder and the next interface change the module still defers
  to a name nobody answers. One paragraph, no more.
- `CHANGELOG.md:37` (the `### Fixed` line of issue 465): say "serves the
  module's host name for this host", "decided at publish time and again on
  every interface change", drop "once per process".
- Comments at `service.go:23-48` and `responder.go:55-63`: update to the
  new shape; keep the two verified citations (`MdnsServer.ts:80`,
  `:608-610`), add `:40-43` and `:321-339` where the re-take is described.
  No other matter.js facts.

## Tests

- `mdns/hostname_policy_test.go`: rewrite for the new policy — served-local
  → OS name; served-foreign → MAC name; unanswered → MAC name; non-darwin →
  no lookup call; verdict cached after an addressed interface, **not**
  cached without one (lookup count 2 after two calls), re-asked after
  `retake()`; the deadline test stays.
- Responder (`responder_advertiser_parity_matterjs_test.go`
  `TestMdnsAdvertisementOSHostNameVerdict`): move to the injected policy on
  the Responder; add — override set → zero lookups, no warning; defaulted
  service + served → OS name, no address records; defaulted + unanswered →
  MAC name with records; **interface change flips the verdict** (served →
  unanswered): the SRV target moves to the MAC name, goodbyes for the old
  SRV/host records go out, address records are announced; hand-built
  Service with an explicit HostName → never looked up.
- Existing responder and server parity tests unchanged and green.

## Guards (worktree; summary log in the scratchpad; grep logs only on failure)

`make ci` (the `TestDefaultSubscription` flake #467 is known — on that
failure alone, run `make lint cover-check surface-check` individually and
say so); `go test -race -count=1 ./mdns/... ./bridge/... ./node/...`;
`make cite-check`; `make apidiff-check` (an unexported field is no API
change — report if apidiff disagrees); **`make reachability` then
`make reachability-check`** (the first commit missed it; commit the
regenerated inventory/summary with the change). Not run here: `chiptool-*`
(the session runs the native acceptance; Linux reruns `make chiptool-test`).

## Report (fixed shape)

Per finding what changed (`file:line`); guards with HEAD before/after and
one `exit` line each; differences from this brief; decisions; left out;
questions; by_design candidates. One commit, `git -c user.name=SukramJ -c user.email=sukramj@icloud.com commit`, English, no attribution lines, no push.
