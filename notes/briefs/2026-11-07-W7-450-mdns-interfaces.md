# Brief — issue 450: an interface allow-list for the mDNS advertiser (`node.MDNS.Interfaces`)

Date 2026-11-07. Hand-over from go-ha2fabric (#40); owner decisions 2026-11-07 (comment on the
issue): allow-list by interface name; it applies within the default container/virtualisation
filter (a named `docker0`/`hassio` stays filtered — the doc says so); it does not scope
`Network.Listen`. Branch `feat/450-mdns-interfaces`, one commit per step, one pull request
("Closes #450"); English; no attribution lines; commit with
`git -c user.name=SukramJ -c user.email=sukramj@icloud.com`. **Model:** Opus. Pins: matter.js
db021da0. Environment and guard rules as in CLAUDE.md. **After any mDNS change `make chiptool-test`
runs locally under the lock before the PR is opened** (the review fix of #437 broke commissioning
on this host and CI did not see it — CI runners have no Docker bridges).

## Facts verified (main f1e9677e, matter.js db021da0)

- `mdns/responder.go:61-67` `Responder.InterfaceFilter func(name string) bool` — true keeps the
  interface out of announcements and answers; `NewResponder` installs the container filter; nil
  falls back to it; set before the first Publish. `mdns/interface_filter.go:8-17`
  `virtualIfacePrefixes` (conservative prefix list, case-insensitive).
- `node/mdns.go:15-27` `MDNS{Off, HostName}`; `:50-57` `defaultAdvertiser` sets HostName and the
  interface-check interval only. `Config.Advertiser` (`node/config.go:61-65`) is not the route: a
  facade host does not import `mdns`.
- `mdns/diagnose.go` findings at :48, :75, :105, :119, :155, :173, :187 — the shape to extend.
- matter.js `packages/protocol/src/mdns/MdnsServer.ts:37-43` (one `InterfaceResponder` per
  interface, interfaces re-read on `Network.interfacesChanged`) and :209-214 (announce "on every
  multicast interface") — no allow-list; the *Ahead of matter.js* marker.

## Scope (one shape)

1. `node.MDNS.Interfaces []string`: empty = today's behaviour; non-empty = announce and answer on
   the named interfaces only, the default filter still applied. `defaultAdvertiser` composes
   `InterfaceFilter = func(name) bool { return defaultFilter(name) || (len(allow) > 0 && !slices.Contains(allow, name)) }`
   — expose the default filter from `mdns` as a named function if it is not exported yet (one
   shape, no second filter field). `Config.Validate` refuses an empty or duplicate name; an unknown
   name is allowed (interfaces come and go — the responder's interface check handles it).
2. `Node.DiagnoseMDNS`: a finding when a named interface does not exist on the host or has no
   address the advertise policy keeps (the operator's typo), in the existing findings' shape.
3. Tests: `mdns` — a responder with an allow-list opens sockets on the named interfaces only and
   answers no query on another (the socket tests' fake wire, see `mdns/addressless_interface_test.go`
   for the fakeWire pattern); `node` — `defaultAdvertiser` composes both filters (a named docker0
   stays filtered), `DiagnoseMDNS` names an unknown interface, empty list = byte-identical records.
4. Docs: the operations/embedding guide that describes `Network.MDNS` names the field and the
   reflector case (dual-homed host, gateway mDNS proxy relaying the other VLAN's answer; remedy:
   advertise on the controllers' VLAN only). CHANGELOG `### Added`. `apidiff` additive.
5. `notes/parity/by_design.md`: `BD-matterjs-mdns-interface-allowlist` in the *Ahead of matter.js*
   register with the MdnsServer.ts marker lines; one sentence that matter.js-based bridges offer
   the option at the host level (do not cite matter-hub as a gold standard).

## Guards

build, vet, gofumpt, `go test -count=1` + `-race` of `mdns`, `node`, `examples/reference-bridge`;
`golangci-lint run --timeout 40m ./...`; `make cover-check`; `make cite-check`,
`make reachability-check`, `make ahead-check`, `make apidiff-check`; **`make chiptool-test`** under
the lock (empty list — the default composition must commission as before). Report in the fixed
shape; push; open the PR; no auto-merge.
