# Changelog

All notable changes to go-fabric are recorded in this file.
The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The module carries its own version lane, independent of any host that embeds
it. Nothing is tagged yet: the API has had one real caller, so consumers
track a pseudo-version until the reference daemon has exercised the surface
long enough for a `v0.1.0` to mean something.

## [Unreleased]

### Added

- **`endpoint/sqlitestore`, a production `endpoint.Store`.** The port shipped
  with only `endpointtest.NewFakeStore` behind it, so every consumer had to
  write the real one — and endpoint identity is the piece of bridge state
  that must survive a restart, because a controller keys its accessory list
  on the endpoint number and is never told to re-read it. The package takes
  an already-open `*sql.DB` and imports no driver, and it is a separate
  package on purpose: a host that brings its own store links none of it and
  creates none of its tables. Its `WithKeyDecoder` option exists for a
  failure the port made possible and nothing announced — the assembler
  garbage-collects by comparing the keys a store *lists* against the keys the
  live snapshot carried, as interface values, so a store that hands back
  `endpoint.StringKey` to a host with a composite key type makes every live
  row look vanished and deletes every endpoint number on the first
  model-complete assembly. Both directions are pinned by a test through the
  real assembler.
- **`store.Schema()` / `store.Apply`, and the same pair on
  `endpoint/sqlitestore`.** The DDL the store's queries are written against
  lived under `store/testdata/`, where a host can read it and never import
  it; the module's own example had transcribed 140 lines of it, and two
  copies of one schema drift with nothing to catch it. The text is now
  embedded in the package it belongs to and handed out, so a column added
  here arrives with the dependency bump. Both scripts are idempotent, and
  the store package's own tests apply them through the exported entry point
  rather than through a fixture beside it.
- **`sigma.DeriveOperationalIPK`.** Turning the raw `AddNOC.IPKValue` into
  the operational IPK the CASE handshake keys on was documented in a doc
  comment and implemented nowhere in the module, so each host reconstructed
  a security-relevant HKDF derivation from prose. It is now exported next to
  `ComputeDestinationID`, which consumes it, with every input cited to
  matter.js and pinned by matter.js's own group-key test vector. The doc
  comments that had described the raw value as `Identity.IPK` were wrong and
  say the opposite now.

- **Test-support packages, so a consumer's tests stop reimplementing the
  module's fakes and stop sending traffic to set up state.** The module's
  first external consumer — a scenario harness driving the bridge from
  another repository — hit four places where the public API was too narrow,
  and one of its workarounds cost that repository a flaky CI job: with no way
  to tell the bridge where a subscription's reports go, the harness issued a
  real `SubscribeRequest` over the wire and read the id back, and on a loaded
  machine that setup traffic landed inside the window the scenario then
  measured. `bridge/bridgetest` closes it — `EstablishSubscriptionTarget`
  registers the reply route directly, and `AckPumpTick` runs one whole pump
  iteration (the outbound retransmits included, which `RunAckPumpOnce` does
  not cover) rather than waiting out the pump goroutine's ticker.
  `endpoint/endpointtest` carries the in-memory `endpoint.Store` and the
  empty-fleet snapshotter that were stranded in `_test.go` files and so were
  invisible outside the package. None of this widened the production API:
  the affordances reach the bridge through a module-internal seam, so an
  external module sees `bridgetest` and cannot reach what it stands on.
  `EstablishSubscriptionTarget` leaves the *whole* post-handshake state: the
  `SubscribeRequest` it stands in for also anchored the peer's inbound MRP
  duplicate-detection window, and a secure session's window anchors with an
  all-ones bitmap — so without the anchor the peer's next two messages had
  to arrive in order or lose the earlier one to the duplicate path, which is
  how the same consumer's StandaloneAck raced its own StatusResponse.
  `SubscriptionTarget.PeerCounter` carries the counter that request would
  have used, and a secure target must name it: nothing on the bridge side
  can derive a counter from a handshake that never happened.
- **`message.Header.SecurityFlags`.** The Security Flags byte is an input
  every caller of `channel.Session.Encrypt` / `Decrypt` has to supply
  alongside the header, and it was derivable only inside this module. It now
  sits next to the encoder that writes it, so the byte fed to the AEAD nonce
  and the byte written to the wire come from one derivation — the bridge's
  private copy had already drifted in its session-type mask, harmlessly only
  because decode rejects the values where the two disagreed.
- **The Matter bridge stack is a standalone module.** The subtree and its
  port contracts left the host daemon they grew in and became
  `github.com/SukramJ/go-fabric`, with its own dependency set: TLV codec,
  Interaction Model, PASE/CASE session establishment, MRP over IPv6 UDP,
  DNS-SD advertisement, the cluster servers a bridge needs, and the endpoint
  assembler. The `contract` package is the seam: a host implements those
  interfaces to expose its own devices as bridged endpoints and keeps
  ownership of its device model, while this module owns the wire format.
  Tests that reached into the host's device model, or read fixtures outside
  the module, did not travel; the ones whose subject is the wire format did.
- **Four commissioning security guards are back.** The NOC length cap, the
  root-certificate subject check, the ACL cleanup on revert, and the
  commissioning window's rejection of a PASE caller were lost in the
  extraction — their subject moved and they did not, so they existed in
  neither repository. They return as behavioural tests rather than the source
  scans they were: the subject now sits in the same module as its test, so
  the real handler is driven and the effect observed, where a scan could not
  tell a check that runs from one that was moved after the action it
  protects. Each carries a control (the 400-byte NOC, the untouched fabric
  indices, the CASE leg), and each was observed failing before it was kept.
- **A licence-header guard over the whole module.** Every `.go` file must
  carry the SPDX identifier and the copyright line ahead of its package
  clause. It reads the comment block before `package` rather than lines 1
  and 2, because a build-constrained file opens with its build tag.
- **CI, a pinned lint gate and dependency tracking.** Build, vet, race-enabled
  tests and lint run on every push and pull request, with the Go, `gofumpt`
  and `golangci-lint` versions pinned to the same values the reference daemon
  uses. Dependabot tracks the module's Go dependencies and the SHA-pinned
  actions.
- **The Matter schema pipeline arrived with the code it feeds.** The matter.js
  extractor (`script/extract-from-matter-js.ts`) and the schema generator
  (`script/generate_matter_schema.go`) followed `parity/schema.json` and
  `schema/` out of the reference daemon, where the two halves had been sitting
  in different repositories with a `cp` between them. The generator now reads
  the embedded snapshot itself rather than a second copy of the extract, so
  the constants it emits cannot describe bytes no parity test validates;
  `go generate ./schema/...` drives it, and it gofmts its own output instead
  of leaning on a formatting step in a `Makefile` this module does not have.
  Regenerating from the unchanged snapshot reproduces `clusters.go`,
  `devicetypes.go` and the `SchemaSnapshotSHA256` constant byte for byte.

### Fixed

- **A first pairing of the reference daemon timed out in operational
  discovery.** The post-AddNOC hook rebuilt the CASE identity and published
  nothing. Boot advertised an operational record for every fabric already in
  the store, so restart-then-reconnect worked and hid the gap -- but a first
  pairing never goes through that path. The commissioner finished PASE,
  installed the fabric, then resolved
  `<compressed>-<node>._matter._tcp` against a record that had never been
  published, and spent its retry budget on a lookup that could not succeed.
  The hook now advertises the fabric, keyed on the identity it just loaded so
  the record and the CASE responder cannot name different nodes. Found by the
  chip-tool guard on its first run, with its control leg green in the same
  run.
- **The reachability snapshot stamped itself with a revision it could not
  know.** `inventory.json` carried `head` and `generated`, both the git
  revision at generation time -- necessarily the parent of the commit that
  carries the file. Against a check that compares the regenerated snapshot
  byte-for-byte, that field goes stale on the next run no matter what the
  code does, and it tells a reader the snapshot was measured one commit
  earlier than it was. Both fields are gone; the commit holding the file is
  its provenance.
