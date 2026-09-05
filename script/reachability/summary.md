# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package (this module is a library and has no production main to seed from)
Entry points: 3052 across 41 test packages.

## Overview

| Metric | Count |
|---|---|
| Total exported | 2652 |
| Reached by a test | 1268 |
| Whitelisted | 1373 |
| **Unreached** | **11** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| contract | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| mdns | 1 | 0 | 0 |
| schema | 1 | 0 | 1 |
| secure/setup | 1 | 0 | 0 |
| cluster/core | 0 | 1 | 0 |
| secure/attestation | 0 | 0 | 1 |
| secure/channel | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |

## First 50 unreached functions

| Package | Identifier | File | Line |
|---|---|---|---|
| cluster/measurement | CelsiusToInt16 | cluster/measurement/measurement.go | 1818 |
| contract | DeviceTypeName | contract/matter.go | 552 |
| endpoint | ComposeNodeLabel | endpoint/spec.go | 88 |
| mdns | DeriveUniqueIDFromIdentity | mdns/rotating_id.go | 117 |
| schema | DeviceTypeAllowsServerCluster | schema/lookup.go | 65 |
| secure/setup | IsValidSetupPIN | secure/setup/setup.go | 275 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/measurement | 1 | 0 | 0 |
| contract | 1 | 0 | 0 |
| endpoint | 1 | 0 | 0 |
| mdns | 1 | 0 | 0 |
| schema | 1 | 0 | 1 |
| secure/setup | 1 | 0 | 0 |
| cluster/core | 0 | 1 | 0 |
| secure/attestation | 0 | 0 | 1 |
| secure/channel | 0 | 0 | 1 |
| secure/sigma | 0 | 0 | 1 |
