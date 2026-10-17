# go-fabric

Matter in pure Go, as a library a host application embeds: the **device
and bridge side** — the node a controller commissions, with the cluster
servers, OTA and media — and a **full commissioner/controller** that pairs
and drives other nodes. It is a semantic port of
[matter.js](https://github.com/matter-js/matter.js): defaults, constraints,
wire shapes and behaviours are mirrored from it, and standing tests hold
the port to it. It is **not certified** by the Connectivity Standards
Alliance, and nothing built on it may be described as certified.

## Where to start

| You want to | Read |
| --- | --- |
| Put your devices on Matter (a bridge, a gateway, a single device) | [Getting started: device](getting-started/device.md), then [Host ports](guides/ports.md) |
| Pair and control Matter devices from Go | [Getting started: controller](getting-started/controller.md) |
| Decide whether the module fits your product | [Architecture](architecture.md), [Security model](security.md), [matter.js comparison](matterjs-comparison.md), [Certifiability](certifiability.md), [Roadmap](roadmap.md) |
| Run it in production | [Concurrency](guides/concurrency.md), [Persistence](guides/persistence.md), the [API stability policy](https://github.com/SukramJ/go-fabric/blob/main/README.md#api-stability) |
| Change it | [CONTRIBUTING](https://github.com/SukramJ/go-fabric/blob/main/CONTRIBUTING.md), the [parity contract](matter-parity-contract.md), the [decision records](adr/README.md) |

## At a glance

- **One module, five runtime dependencies, no cgo.** (`golang.org/x/tools` in `go.mod` serves only the repository's own tooling.) It builds with
  `CGO_ENABLED=0`; SQLite is the pure-Go `modernc.org/sqlite`.
- **Matter 1.6.1** data model, extracted from matter.js at a pinned commit
  and embedded; 118 served clusters and 79 generated device types.
- **Transports:** IPv6 (and IPv4) UDP with MRP, TCP for large payloads,
  DNS-SD. No Thread radio, no Bluetooth on the device side; the
  commissioner's BLE support is decided and in progress
  ([ADR 0024](adr/0024-ble-commissioning-in-the-controller.md)).
- **Proof:** connectedhomeip's own certification cases run against the
  reference daemon in CI, and the module's controller commissions the
  module's node in-repo. The [certifiability report](certifiability.md)
  lists every family and its status.
- **Licence:** Apache-2.0 (since `v0.5.0`); see the
  [NOTICE](https://github.com/SukramJ/go-fabric/blob/main/NOTICE) for the matter.js lineage.

The Go API reference is on
[pkg.go.dev](https://pkg.go.dev/github.com/SukramJ/go-fabric).
