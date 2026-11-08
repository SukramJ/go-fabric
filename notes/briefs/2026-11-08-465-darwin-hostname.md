
# Brief: issue 465 — the darwin host name is verified before its address records are left to the OS (2026-11-08)

Owner decisions (2026-11-08, Mac session wizard): fix shape **1 — verify,
then defer**; implemented on the Mac (owner exception, as W8) by one Opus
agent in worktree `../go-fabric-465`, branch `465/darwin-hostname`, base =
main d23d2dce. One PR, auto-merge OFF (Linux review). Author
`SukramJ <sukramj@icloud.com>`, no attribution or co-author lines. The
acceptance (`GOFABRIC_GOCERT=BDX make chiptool-gocert`, native) is run by
the session, not by the implementer — see Guards.

## Why (measured 2026-11-08, issue 465 comments)

The Go-TH leg fails at step 0 on macOS: the controller's browse never
delivers the daemon. Cause, measured on the Mac:

- `mdns/service.go:33-45` `defaultHostName`: on darwin the OS host name
  (`os.Hostname()`, `Lisa`) is kept as the SRV target, and
  `mdns/responder.go:301` (`ownsHost` false ⇔ host equals `osHostName()`,
  `:449-456`) leaves its A/AAAA to the OS responder (`mdns/records.go:98-100`,
  `by_design.md` L7-D03 at line 921).
- mDNSResponder serves only the Bonjour LocalHostName (`scutil --get
  LocalHostName` → `Lisa-3`, a conflict rename that never touches the kernel
  name). Standard mDNS questions for `Lisa.local` A/AAAA go unanswered
  (8/8), `Lisa-3.local` is answered at once; `dns-sd -G v4v6 Lisa.local.`
  returns nothing.
- The scanner solicits the SRV target's A/AAAA (`client/discovery/scanner.go:762-770`)
  and delivers a device only with an address (`:786-790`, mirrors
  `CommissionableMdnsScanner.ts:443-452`), so discovery times out.
- The bind-address hypothesis of the issue (224.0.0.0 vs wildcard) was
  refuted by measurement: identical delivery on every bind. **Do not touch
  `mdns/socket.go`.**
- Go's resolver resolves the served name without cgo and without the
  macOS Local Network grant: `net.DefaultResolver.LookupIPAddr(ctx,
  "Lisa-3.local")` → 6 addresses in 5 ms (CGO_ENABLED=0); `Lisa.local`
  runs into the context deadline.

matter.js has no platform rule: `packages/protocol/src/mdns/MdnsServer.ts:80`
at db021da0 (`hostnameFor: details => this.#hostname ?? hostnameForMac(details.mac)`)
and `:608-610` (`hostnameForMac`: `<MAC hex, upper>0000.local`), own
A/AAAA on every platform (no `darwin`/`platform` in that file at the pin).
L7-D03 is the recorded divergence and keeps its reason (an Apple Home
teardown observed with a separately published name) — but only where the
OS actually serves the name we defer to.

## Scope

1. `mdns/service.go` `defaultHostName` (darwin branch): keep the OS host
   name **only if the system resolver answers `<name>.local`**; otherwise
   fall through to the MAC-derived chooser exactly as every other platform
   does (matter.js's behaviour). Shape:
   - a package-level seam `var lookupHostAddrs = net.DefaultResolver.LookupIPAddr`
     (or a small func type) so tests inject the verdict; deadline **1 s**
     (`context.WithTimeout`); "served" = `err == nil && len(addrs) > 0`.
   - the decision is a pure function, testable on every platform:
     e.g. `osHostNameServed(goos, name string, lookup …) bool` or the
     darwin branch factored as `darwinHostName(osName string, served func(string) bool) (string, bool)`.
     `runtime.GOOS` is read once at the call site, never inside the pure
     function.
   - the verdict is taken **once per process** and cached (the
     `defaultHostNames` chooser or a `sync.Once` beside it): a name that
     flips would re-announce every service; the chooser's stability rule
     (`hostNameChooser`, `service.go:78-96`) is the precedent.
   - one log line at the first decision, through the package's existing
     logger pattern (check how `mdns` logs today — `slog` via the
     Responder's logger if one exists, else the package's convention):
     `mdns.hostname.os_unanswered name=<os name> fallback=<mac name>` on
     the fallback, debug level on the served branch.
2. `mdns/responder.go:301`: no change expected — the MAC-derived fallback
   never equals `osHostName()`, so `ownsHost` becomes true and the address
   records are published. **Verify** that with a test (see Tests), do not
   assume it. The `Responder.HostName` override keeps its semantics (an
   operator-set name is always left to the OS, `responder.go:68-75`).
3. Comment at `mdns/service.go:33-39` and the Responder doc at
   `responder.go:55-59`: state the rule precisely (OS name kept when the
   OS responder serves it; otherwise matter.js's MAC name with own
   records) and cite `MdnsServer.ts:80`, `:608-610` at db021da0 for the
   behaviour fallen back to.
4. `notes/parity/by_design.md` row L7-D03 (line 921): the macOS exception
   is now conditional — add the condition, the measured reason (kernel
   host name ≠ Bonjour LocalHostName after a conflict rename; issue 465),
   and that the fallback is matter.js's behaviour. One paragraph, no prose
   beyond the citations and one sentence of reasoning.
5. `CHANGELOG.md` `[Unreleased]` `### Fixed`: one line — `mdns`: on macOS
   the responder leaves the host's address records to mDNSResponder only
   when mDNSResponder serves the module's host name; when the kernel host
   name differs from the Bonjour name (conflict rename), the MAC-derived
   name with the module's own A/AAAA is used, as matter.js does (issue 465).
   Before: a controller on the same LAN never obtained an address for the
   node.
6. Docs: `grep -rn 'macOS\|host name' docs/` — if a page describes the
   macOS host-name rule, update it in the same PR; if none does, report
   that and change nothing.

Out of scope: `mdns/socket.go` (hypothesis refuted), reading the
LocalHostName from SystemConfiguration (owner declined), dropping L7-D03
(owner declined), the chip-tool harness, anything in `client/discovery`.

## Tests

- `mdns`: a unit test of the pure decision for the three cases —
  darwin + served → OS name; darwin + unanswered → MAC-derived name of the
  chooser; non-darwin → MAC-derived name regardless of the lookup (the
  lookup must not even be called: assert with a counting fake).
- `mdns`: a Responder test with the seam forced to "unanswered" on a
  darwin-shaped input — the published record set contains A/AAAA for the
  fallback name (`ownsHost` true); with "served" — no address records
  (today's behaviour). Use the existing responder test helpers
  (`responder_server_parity_matterjs_test.go`, `parity_mdns_server_test.go`
  show how record sets are inspected); extend an existing pattern, do not
  invent a new harness.
- The lookup deadline: a fake that blocks until the context ends returns
  "unanswered" within ~1 s (use the package's clock seam if one exists;
  otherwise a real 1 s is acceptable once).
- Existing parity tests (`TestMdnsServerHostName`) stay green unchanged.

## Guards (run in the worktree, log summaries to the scratchpad, read the summary; open a log only on failure with grep)

`make ci`; `go test -race -count=1 ./mdns/... ./bridge/... ./node/...`;
`make cite-check`; `make apidiff-check` (no API change expected — report
if it says otherwise); `make cover-check` is part of `make ci`. Not run
by the implementer: anything `chiptool-*` (Linux / the session's native
acceptance). Say what was skipped and why.

## Report (fixed shape)

Per scope item what changed (files, `file:line`); guards with HEAD before
and after and one `exit` line each; differences from this brief;
decisions taken; left out; questions; `by_design` candidates. Commit(s)
on `465/darwin-hostname` with `git -c user.name=SukramJ -c user.email=sukramj@icloud.com commit`, message in English, no attribution lines, **no push**.
