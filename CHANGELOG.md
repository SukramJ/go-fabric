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
