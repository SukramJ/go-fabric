# Changelog

All notable changes to go-fabric are recorded in this file.
The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The module carries its own version lane, independent of any host that embeds
it. Nothing is tagged yet: the API has had one real caller, so consumers
track a pseudo-version until the reference daemon has exercised the surface
long enough for a `v0.1.0` to mean something.

## [Unreleased]

### Fixed

- **The per-fabric ACL cap counted the FabricIndex the client sent, then
  stored every entry on the writer's fabric.** `AccessControl.ACL` writes
  filtered the count to entries whose FabricIndex matched the writer's (or
  was zero) before checking `AccessControlEntriesPerFabric`, and then stamped
  the writer's fabric on every entry it persisted — so a list of twenty
  entries tagged with a foreign index passed the limit of four and all twenty
  landed on the writer's fabric, while the attribute kept reporting four. The
  count is now the list as stored. matter.js reaches the same number because
  its fabric-scoped write machinery stamps the accessing fabric before
  `AccessControlServer.ts` filters on it.
- **Privacy masking covered only the first AES block of the protected
  header; the region is 20 bytes when both node ids are present.** Counter,
  Source Node ID and a 64-bit Destination Node ID are 4 + 8 + 8 bytes, and
  matter.js runs the CTR keystream over all of them
  (`MessagePrivacy.ts:53-56`). Capping the mask at 16 bytes left the tail of
  the destination id in the clear on send and obfuscated on receive, and the
  AEAD tag then failed on whichever side had unmasked the wrong bytes.
  `channel.PrivacyKeystream` now yields as many blocks as the region needs.
  Alongside it, a unicast frame carrying the P bit is dropped rather than
  unmasked — privacy enhancements are defined for group messages, and
  matter.js (`ExchangeManager.ts:220-224`) and the chip SDK both drop the
  unicast case.
- **The AEAD nonce used a Security Flags byte rebuilt from typed fields.**
  Reserved bits 4-2 of a received frame were dropped on decode and read back
  as zero, so a peer that set one would fail the tag check on every frame
  while matter.js decoded it (it keeps the raw byte for exactly this use,
  `MessageCodec.ts:45`). `Header.NonceSecurityFlags` returns the byte as
  received for a decoded header.
- **The nonce node id came from the header's Source Node ID when present,
  not from the session.** matter.js and chip build the nonce from the peer id
  the session was established with (`NodeSession.ts:187`); reading it off the
  header made a mismatched label fail the tag and a matching one partly
  sender-chosen. `Session.Decrypt` uses the session's peer id unconditionally.
- **PASE session parameters decoded the two retransmit timeouts into 16
  bits.** `idleInterval` and `activeInterval` are `TlvUInt32` in matter.js
  `PaseMessages.ts` — and `uint32` on this module's own CASE path — so a
  commissioner advertising 100 000 ms was paced at 34 464 ms. Both fields of
  `spake2.MRPParameters` are `uint32` now; `ActiveThresholdTimeMs` stays
  `uint16` as the schema declares it.
- **`sigma.Responder.SetResumptionStore` and `SetSessionParameters` wrote
  without the responder lock** that every handshake path reads under, unlike
  the two sibling setters. Both take it now.
- **`subscription.Manager.Start` said "Idempotent" and was not** — every call
  launched another engine goroutine, doubling every tick and heartbeat. A
  `sync.Once` makes the doc true.

### Added

- **`MeasurementContext` — a host-registered measurement kind can now express
  every shape the library can.** The materialiser signature carried only the
  source, so the two built-ins that need the endpoint id — the generic switch,
  which takes it at construction because event delivery addresses a Matter
  path, and PowerSource, which must name the endpoint it feeds (§11.7.6.20) —
  had to stay hard-coded in `endpoint/materialize.go`. Both are ordinary
  registered materialisers now, and that file names no measurement class at
  all. The context is a struct rather than a bare id on purpose: adding a field
  later is source-compatible, where adding a parameter after `v0.1.0` would
  cost a deprecation cycle.

- **The reference daemon mounts the three cluster servers that had no host.**
  `cluster/valve`, `cluster/modeselect` and `cluster/levelcontrol` shipped with
  no endpoint anywhere mounting them, so their subscription paths were
  exercised only through collaborations a test constructed itself — a
  bracketing test by this project's own definition, recorded rather than
  hidden when they landed. A WaterValve, a ModeSelect device and a Speaker now
  sit in the example fleet behind fake devices that hold real state, and the
  guard that could not be written before exists: a device-side change reaching
  a subscriber through a **mounted** endpoint.

- **A stated API-stability and deprecation policy, with a guard behind it.**
  The module is on its own SemVer lane and nothing is tagged, so a consumer had
  no way to know what it may depend on or how much warning a rename carries.
  README.md now says which packages are public (measured against the real
  `internal/` tree, not asserted), how a deprecation is marked, how long it
  stays, and what pre-1.0 changes about that. Prose enforces nothing, so two
  tests hold the policy to its word: a marker that does not open its paragraph
  with `Deprecated: ` produces no SA1019 warning at any consumer and is
  refused, and a deprecated identifier that appears nowhere in `CHANGELOG.md`
  is refused too — the window only means something to someone told the clock
  is running. Both pass vacuously today, because the module has deprecated
  nothing, and say so out loud rather than looking asleep.
- **One chip `Test_TC_` conformance case runs against the reference daemon.**
  `Test_TC_OO_2_2` drives Off/On/Toggle with read-back, including both
  idempotency cases, gated in CI like the other chip-tool jobs. Certification
  stays a non-goal: the PICS file says in its own text that it is written only
  as far as this case needs and is not a certification artefact. Running it
  needs chip's Python runner, because chip-tool at the pinned commit registers
  no `tests` command at all — that is recorded rather than worked around.

- **Eleven new fuzz targets, over the five packages that parse bytes.** The
  module fuzzed only its four Interaction-Model decoders; everything else that
  turns wire bytes into structures was unfuzzed, including the two packages a
  commissioner reaches *before* anything is trusted — DER certificate parsing
  in `secure/mattercert` and the setup-payload surface in `secure/setup`. Seeds
  come from each package's own tests rather than being invented, so they are
  known-good, and each target carries deliberately malformed variants. None
  found a crash. Where an encoder exists the target asserts a round-trip; none
  asserts a specific error for malformed input, which is not the property.
- **Ten benchmarks and a per-package coverage floor.** Both were absent
  entirely. The benchmarks cover the paths a running bridge repeats — TLV
  encode/decode and validate, inbound datagram decode, initial-report
  construction, endpoint assembly — and each says in its doc comment why that
  path was chosen. `script/coverfloor` compares `go test -cover` against a
  table with a reason per row; floors sit below each package's measured
  coverage so the gate ratchets against regression rather than failing on day
  one.

- **`cluster/levelcontrol` — a host-agnostic LevelControl server (0x0008).** Speaker
  0x0022 mandates OnOff and LevelControl servers, and this module had neither
  a LevelControl server nor a way to build one: `cluster/wire` carried command
  decoders only. Serves exactly the three conformance-M attributes
  (CurrentLevel 0x0, Options 0xf, OnLevel 0x11) and all eight M commands. The
  four `WithOnOff` variants get their own port methods rather than a flag on a
  shared request — a bool a host forgets to read compiles fine and turns "turn
  it on and set the level" into "set the level while it stays off".
- **The measurement-kind set is open: a host can register one without editing
  this module.** It was a closed enum answered by switches, so every new
  measurement meant a library change. `RegisterMeasurementKind` now takes a
  descriptor carrying the device type, the cluster id and the materialiser
  that builds the servers, and refuses one without a materialiser — a registry
  able to advertise what it cannot build is the defect, so it is made
  impossible rather than documented. The sixteen built-ins keep their exact
  values and behaviour; their materialisers moved verbatim into
  `cluster/measurement` and install themselves through the same seam idiom the
  module already uses, so `contract` gains no dependency. What a registered
  kind cannot yet express is stated in `endpoint/materialize.go`: the two
  shapes that need the endpoint id at construction — the generic switch and
  the battery re-entry — stay library-only.

- **`cluster/valve` — ValveConfigurationAndControl (0x0081), the server behind
  WaterValve 0x0042.** Serves the five attributes matter.js marks conformance M
  (OpenDuration 0x0, DefaultOpenDuration 0x1, RemainingDuration 0x3,
  CurrentState 0x4, TargetState 0x5) and handles Open 0x0 / Close 0x1. Every id
  and conformance string is cited to
  `valve-configuration-and-control.element.ts`; AutoCloseTime 0x2 is `"TS"` and
  the level attributes are `"LVL"`, so neither is served while FeatureMap is 0
  — an `Open` carrying TargetLevel is refused with ConstraintError rather than
  silently ignored. It reaches a host through a narrow port instead of mutating
  internal state: a host refusal does not become Success.
- **`cluster/modeselect` — ModeSelect (0x0050), the server behind device type
  0x0027.** Description 0x0, StandardNamespace 0x1, SupportedModes 0x2 and
  CurrentMode 0x3 served from a host-supplied list, ChangeToMode 0x0 forwarded
  to the host, and an unsupported mode answered with InvalidCommand. StartUpMode
  and OnMode are absent and FeatureMap is 0, because DEPONOFF would make OnMode
  mandatory. The wire writer gained the one case it was missing — a list of
  structs — so SupportedModes can reach a controller at all, with the constraint
  bounds (255 modes, 64 tags) applied at encode time rather than trusted from
  the host.

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

### Removed

- **`bridge.New` no longer takes an `endpoint.Store`.** The signature is now
  `New(snap Snapshotter, advertiser mdns.Advertiser, cfg Config, logger
  *slog.Logger)`. The store was stored on the `Bridge` and read by nothing —
  the bridge consumes a fully assembled topology through the `Snapshotter` and
  never looks an endpoint record up itself, so the parameter asked every host
  for a collaborator the package had no use for and made the constructor read
  as though the bridge owned endpoint persistence. There is no replacement,
  because there was nothing to replace: a host still needs a store, and still
  passes it to the `endpoint` assembler behind its snapshotter, exactly as
  before.

  **Upgrading requires a source change**: delete the first argument at every
  `bridge.New` call site. `bridge.New(store, snap, adv, cfg, logger)` becomes
  `bridge.New(snap, adv, cfg, logger)`; the store variable stays where it is,
  feeding the assembler. Nothing else moves, and a missed call site is a
  compile error rather than a behaviour change. The error string
  `"bridge: store is required"` is gone with the nil check that produced it.

  This ships without a deprecation window. A parameter cannot carry a
  `Deprecated:` marker that `staticcheck` would ever warn on, so honouring the
  window would mean shipping a second constructor for two minor releases; the
  module is at `v0` with nothing tagged, where README.md's API-stability
  section says `main` is the only consumable version and may break in any
  commit. What that state does not excuse is an unannounced break, which is
  what this entry is.

### Removed

- **`bridge.New`'s `store` parameter.** Nothing read it: the field was
  assigned and never used, which a compiler probe confirmed rather than a
  grep. Removing it is a breaking change to a v0 API, so it is announced here
  with the call-site edit rather than left for a consumer to discover at build
  time — the module's stated policy allows the break before `v0.1.0` but not
  an unannounced one. A parameter cannot carry a `Deprecated:` marker, so the
  window the policy describes does not apply and this entry stands in for it.

### Fixed

- **A certificate whose `NotBefore` overflows the epoch conversion was
  accepted.** `NotBefore` and `NotAfter` are Matter-epoch seconds and reaching
  Unix time adds the epoch; a field close to 2^64 wraps that addition to a
  small number, and a small number is one every real clock is already past — so
  the validity window passed. With `NotAfter == 0`, the long-lived RCAC
  convention, `decode.go`'s ordering check does not fire either, leaving nothing
  else to catch it. Measured: `NotBefore = 2^64-1001` becomes 946683799, and
  the certificate verified on an ordinary clock — no exotic device state
  required. Both conversions are checked now. Found by writing down what a
  `//nolint` was actually suppressing.

- **A fail-safe armed over PASE could be completed by any already-commissioned
  fabric.** A PASE arm stamps fabric index 0 because PASE has no fabric, and
  the ownership check read `failSafeFabricIndex != 0 && …` — so that 0 meant
  "anyone may complete this". An already-authorised controller on another
  fabric could send `CommissioningComplete` into a window it had not opened,
  aborting the admin who did and committing half-installed state without the
  expiry rollback running. Both references keep plain equality here and stay
  correct because AddNOC re-stamps the context onto the fabric it installed
  (Matter §11.18.6.16); this module now does the same, so the check is
  equality and a first pairing still completes. Guarded in both directions:
  the hijack is refused, and the legitimate PASE-arm-then-CASE-complete
  sequence still works.

- **A bridge that attaches an ACL it cannot enforce now refuses to start.**
  `CheckACL` answers Success for fabric index 0, which means "PASE, no fabric
  yet" and is correct while commissioning. But `resolveSessionFabric` returns
  that same 0 when the session lookup does not implement
  `SessionFabricResolver` — so a host that wired an ACL and a lookup without
  that capability had *every* CASE session resolve to 0, and every operational
  request passed the access check as though the device were still being
  commissioned. Nothing failed and nothing logged; the AccessControl entries
  were simply never applied. The neighbouring branch in `CheckACL` already
  fails closed when there is no ACL source at all, on the same reasoning, so
  this is that answer one layer out — at start-up, where it is a wiring
  mistake rather than a silent runtime state. A host that attaches no ACL is
  unaffected. Found by writing the threat model, not by review.

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
