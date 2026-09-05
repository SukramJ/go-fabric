# reference-bridge

A runnable Matter bridge built **only from `go-fabric`'s public API** — no
`internal/`, no `_test.go` helper, no fork. It bridges two hard-coded fake
devices, advertises itself over mDNS, accepts a commissioner over PASE and
serves the operational CASE session that follows.

It exists as the module's *second* consumer. The first is the daemon
go-fabric was extracted from, which remembers how the code used to be
arranged. This one does not, so what it costs to write is a measurement of
the seam rather than a demonstration of it.

```sh
go run ./examples/reference-bridge
go run ./examples/reference-bridge --help
```

Startup prints the pairing information on stdout:

```
  go-fabric reference bridge
  --------------------------------------------------------------
  listening on     [::]:5540
  bridged devices  2
  vendor/product   0xFFF1 / 0x8001  (CSA TEST identity — not shippable)

  discriminator    3840
  setup passcode   20202021
  manual code      34970112332
  QR payload       MT:-24J0AFN00KA0648G00

  pair with, e.g.:
    chip-tool pairing onnetwork-long 1 20202021 3840
  --------------------------------------------------------------
```

## The fleet

| Endpoint | Device type | Backed by |
| --- | --- | --- |
| 2 | OnOffLight `0x0100` | `demoLight`, a `contract.EndpointSource` with a hand-written OnOff (`0x0006`) cluster server |
| 3 | TemperatureSensor `0x0302` | `demoThermometer`, a `contract.FloatMeasurementSource` — no cluster code at all; the assembler picks the cluster from the declared measurement class |

The two are deliberately different shapes. A device with commands has to
implement `contract.ClusterServer` itself; a read-only measurement does not,
and the difference in what the host has to write is large.

## This is a TEST identity

The bridge advertises vendor `0xFFF1`, product `0x8001`, and presents the
attestation chain from `attestation.BuildTestChain` — the CSA **test** PAA
from the Matter specification's test vectors.

That is what makes this example pairable at all: chip-tool, Apple Home and
Google Home all ship the matching test PAA in their trust stores, so no
vendor-supplied DAC is needed and no `--bypass-attestation-verifier`.

It is also why nothing built on it may ship. `0xFFF1` is reserved for test
and development; a device advertising it is not a product identity, cannot be
certified, and must not be sold or described as Matter-compliant. A real
product replaces the vendor/product pair *and* all four attestation inputs
(`DAC`, `DACPrivateKey`, `PAI`, `CertificationDeclaration`) with material
issued under its own CSA membership. See the repository README's
"Not certified" section.

## Persistence: what it costs, and what breaks without it

Endpoint numbers are the one piece of bridge state a controller caches and
cannot be told to re-read: it keys its accessory list on the number. An
in-memory store makes the example *run* and quietly makes it *wrong* across
a restart — every commissioned controller loses every accessory, or worse,
silently rebinds an accessory to a different device that inherited its
number.

So this example persists, and after the first version of it was written by
hand, both halves it had to hand-write moved into the module:

- **`endpoint/sqlitestore` is the production `endpoint.Store`.**
  `endpointtest.NewFakeStore` remains in-memory test scaffolding;
  `sqlitestore.New(db)` is what a bridge runs on. Neither package imports a
  driver — they take an already-open `*sql.DB` — so the host picks the
  driver and the DSN. This one uses `modernc.org/sqlite`, already a direct
  requirement of `go-fabric`, so persisting adds no new dependency.
- **The module owns the DDL.** `store.Schema()` and `sqlitestore.Schema()`
  return the embedded text each package's own queries are written against;
  `Apply` runs it. `persist.go` calls both and writes no SQL, so a schema
  change arrives with the dependency bump. A host with a migration tool
  feeds the two scripts through that instead.
- **Numbers only ever advance.** The high-water mark lives in
  `matter_endpoint_allocation`, and `RemoveEndpoint` never returns a number
  to the pool.
- **Keys are plain strings here.** The fleet's `StableKey`s are
  `endpoint.StringKey`, so the store's default decoding is correct. A host
  with a composite key type must pass `sqlitestore.WithKeyDecoder`: without
  it the assembler's garbage collection cannot match a listed key against a
  live one and deletes every endpoint number on the first model-complete
  assembly.

Everything else is deliberately volatile: CASE sessions live in RAM (Matter
treats them as such), and subscriptions are not re-armed at boot even though
the `matter_persistent_subscriptions` table exists — a controller re-subscribes
after a restart.

Delete `reference-bridge.db` to factory-reset.

## What this example does NOT do

Each of these is real production wiring that the loom daemon carries and this
one omits, listed so the omission is not mistaken for "not needed":

- **IPK rotation.** The CASE identity is derived from `IdentityRecord.IPK`
  only. A `KeySetWrite` that rotates the IPK carries up to three epoch keys in
  `GroupKeySetID=0`, and a correct bridge tries every one when matching an
  inbound `Sigma1.DestinationID`.
- **CASE resumption.** `sigma.Responder.SetResumptionStore` is not wired, so
  every reconnect runs a full Sigma1–3 handshake instead of the one-round-trip
  resume.
- **Fabric teardown.** `RemoveFabric` persists, but no session, subscription
  or resumption record is evicted, and the operational mDNS record is not
  withdrawn.
- **AdministratorCommissioning (`0x003C`)** and a runtime commissioning
  window. The window here is open for the process lifetime with a fixed
  passcode; there is no `OpenCommissioningWindow` path and no
  multi-admin/enhanced window.
- **Live updates.** Neither device pushes: nothing implements
  `contract.ChangeNotifier`, so subscribers get heartbeats rather than change
  reports, and `Bridge.Reassemble` is never called.
- **BasicInformation events** (`StartUp`, `ShutDown`, `Leave`) and
  persisted `GeneralDiagnostics` counters.
