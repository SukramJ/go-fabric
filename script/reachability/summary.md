# Exported-API reachability summary

Root set: test-seeded: Test*/Benchmark*/Fuzz*/Example* functions of every test package and the package initializer of each such package (this module is a library; its example programs are one host's composition, not a production root set)
Entry points: 10134 across 290 test packages, and 368 package initializers.

## Overview

| Metric | Count |
|---|---|
| Total exported | 27836 |
| Reached by a test | 20874 |
| Whitelisted | 6939 |
| **Unreached** | **23** |

## Top-20 packages by unreached exported identifiers

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/jointfabric | 4 | 0 | 0 |
| icd | 3 | 0 | 0 |
| node | 3 | 0 | 0 |
| bdx | 2 | 0 | 0 |
| bridge | 2 | 0 | 0 |
| cluster/core | 2 | 0 | 0 |
| controller/client | 2 | 0 | 0 |
| client/exchange | 1 | 0 | 0 |
| cluster/measurement | 1 | 0 | 0 |
| secure/operational | 1 | 0 | 0 |
| secure/sigma | 1 | 0 | 0 |
| transport/tcp | 1 | 0 | 0 |

## First 50 unreached functions and methods

| Package | Identifier | File | Line |
|---|---|---|---|
| bdx | ServerSession.InitType | bdx/responder.go | 568 |
| bdx | ServerSession.Scope | bdx/responder.go | 562 |
| bridge | Bridge.ControllerRTT | bridge/bridge.go | 3274 |
| bridge | Bridge.SubscriptionFlushReporter | bridge/subscribe.go | 404 |
| client/exchange | Manager.LocalFixedMrpBackoff | client/exchange/manager.go | 249 |
| cluster/core | OperationalCredentials.RolledBackUpdates | cluster/core/operational_credentials.go | 736 |
| cluster/core | ScenesState.ForgetFabric | cluster/core/scenes_management.go | 538 |
| cluster/jointfabric | Datastore.ForceAddNodeKeySetEntry | cluster/jointfabric/datastore_ops.go | 1014 |
| cluster/jointfabric | Datastore.FriendlyName | cluster/jointfabric/datastore.go | 674 |
| cluster/jointfabric | Datastore.SetAnchorVendorID | cluster/jointfabric/datastore.go | 775 |
| cluster/jointfabric | Datastore.Status | cluster/jointfabric/datastore.go | 681 |
| cluster/measurement | HeldBooleanState.Set | cluster/measurement/booleanstate_held.go | 68 |
| controller/client | ICDClient.OnAvailableChanged | controller/client/icd.go | 405 |
| controller/client | ICDClient.OnRegistered | controller/client/icd.go | 391 |
| icd | ModeState.IsActive | icd/mode.go | 94 |
| icd | PeerTable.NotePeerActive | icd/peer_table.go | 255 |
| icd | Registrations.Clear | icd/registrations.go | 82 |
| node | ICD.OnModeChangeAsync | node/icd.go | 319 |
| node | Node.OTAProviderEndpoint | node/ota_provider.go | 165 |
| node | Node.OnOTAProviderStatusAsync | node/ota_provider.go | 148 |
| secure/operational | Entry.PeerMRPIntervals | secure/operational/manager.go | 479 |
| secure/sigma | Initiator.PeerNodeID | secure/sigma/initiator.go | 175 |
| transport/tcp | Dialer.OnPanicRecovered | transport/tcp/dialer.go | 36 |

## Full by-package breakdown

| Package | Funcs | Types | Other |
|---|---|---|---|
| cluster/jointfabric | 4 | 0 | 0 |
| icd | 3 | 0 | 0 |
| node | 3 | 0 | 0 |
| bdx | 2 | 0 | 0 |
| bridge | 2 | 0 | 0 |
| cluster/core | 2 | 0 | 0 |
| controller/client | 2 | 0 | 0 |
| client/exchange | 1 | 0 | 0 |
| cluster/measurement | 1 | 0 | 0 |
| secure/operational | 1 | 0 | 0 |
| secure/sigma | 1 | 0 | 0 |
| transport/tcp | 1 | 0 | 0 |
