# Network infrastructure

A Network Infrastructure Manager (device type `0x0090`) and a Thread Border
Router (`0x0091`) carry two clusters whose rules live in
[`cluster/netinfra`](https://github.com/SukramJ/go-fabric/tree/main/cluster/netinfra):
WiFiNetworkManagement (`0x0451`) and ThreadNetworkDirectory (`0x0453`).
matter.js serves both as the generated behaviour stub, with no logic, so
this module follows connectedhomeip's servers at the harness commit
`6170af84`. Both are entries of the *Ahead of matter.js* register. The kit
mounts these servers by default, with or without a declaration. Neither
server holds a radio: the host supplies the network's state.

## WiFiNetworkManagement: the credentials

The server starts without credentials. In that state `Ssid` and
`PassphraseSurrogate` read null. The host sets the network's credentials
through the typed server's `Host`, the kit server's host methods
(`wifinetworkmanagementdef.Host`):

```go
err := d.WiFiNetworkManagement().Host().SetNetworkCredentials([]byte("home"), passphrase)
```

`Host` also has `ClearNetworkCredentials` and `Passphrase`; `Ssid` is the
typed server's `Get`. `Host` is nil when the host mounted its own server.

| Rule | Behaviour |
| --- | --- |
| SSID | 1 to 32 bytes |
| Passphrase | 8 to 63 bytes, or 64 hexadecimal digits (a raw PSK); anything else is `netinfra.ErrInvalidCredentials` |
| Same pair again | no change is reported |
| New SSID | `Ssid` is reported |
| New passphrase | `PassphraseSurrogate` advances (one past the last value, or the real time in milliseconds when that is later) and is reported |
| `ClearNetworkCredentials` | both attributes read null again |
| `NetworkPassphraseRequest` | returns the passphrase over a CASE session; PASE or a group message is UNSUPPORTED_ACCESS, no credentials is INVALID_IN_STATE |

The passphrase is never an attribute, and it never appears in a log line
or an error. The typed server's `Set` cannot move `Ssid` or
`PassphraseSurrogate` (`spec.ErrNotHeld`). The credentials live in memory,
as chip's do, so the host sets them again at every start.

## ThreadNetworkDirectory: the datasets

The directory stores Thread operational datasets, keyed by their Extended
PAN ID, and the preferred network. The host persists both through a
`ThreadNetworkStore`
(`threadnetworkdirectorydef.ThreadNetworkStore`, declared as
`Declare.ThreadNetworkStore`):

| Method | Called |
| --- | --- |
| `Capacity() uint8` | once, for `ThreadNetworkTableSize` |
| `LoadNetworks(ctx)` | at construction and on `ReloadNetworks` |
| `SaveNetwork(ctx, extPanID, dataset)` | after `AddNetwork` passed its checks |
| `RemoveNetwork(ctx, extPanID)` | after `RemoveNetwork` passed its checks |
| `LoadPreferred(ctx)` | at construction; `ok` false when nothing was saved |
| `SavePreferred(ctx, extPanID)` | on a change of `PreferredExtendedPanID` (a controller's write, the host's `Set`, a clearing); nil is null |

Without a store, the server keeps both in memory, in a
`netinfra.MemoryThreadNetworkStore` of 32 networks (chip's default
capacity), or of the `Initial`'s `ThreadNetworkTableSize`. The server
checks the capacity and the presence of a network itself. A store's error
answers the command or the write with FAILURE and changes nothing.

| Command or write | Refused with |
| --- | --- |
| `AddNetwork` (timed) | CONSTRAINT_ERROR for a dataset that lacks one of the ten sub-TLVs chip requires, has one in the wrong size, or has an empty Network Name; INVALID_IN_STATE when the Active Timestamp is not newer than the stored network's; RESOURCE_EXHAUSTED when the table is full |
| `RemoveNetwork` (timed) | CONSTRAINT_ERROR for an Extended PAN ID that is not 8 bytes, or for the preferred network; NOT_FOUND for a network not stored |
| `GetOperationalDataset` | UNSUPPORTED_ACCESS without a CASE session; CONSTRAINT_ERROR for a wrong-sized ID; NOT_FOUND |
| write of `PreferredExtendedPanID` | CONSTRAINT_ERROR unless it names a stored network; an empty value is null |

The preferred network is the store's, as chip keeps it in attribute
storage. At start the server serves the saved one. A saved network the
store no longer holds is cleared with a warning, and the clearing is saved.
When the store has saved none, the declaration's `Initial`
(`PreferredExtendedPanId`) is the fallback, and it must name a stored
network. A host that changes the store outside the cluster calls
`ReloadNetworks` (`d.ThreadNetworkDirectory().Host().ReloadNetworks(ctx)`), which re-reads the datasets and reports `ThreadNetworks`
when it moved. A preferred network the store dropped is cleared.

The server checks a dataset the way chip's `OperationalDatasetView` reads
one, taking the first sub-TLV of each type and holding it to chip's sizes:
`thread.DatasetView`, which the Thread Border Router Management server
uses too ([Thread Border Router](thread-border-router.md)). The
commissioner's codec in the same package (`thread.DecodeDataset`, after
matter.js) reads the last occurrence and accepts the one-byte Channel; a
directory that judges what a controller hands it reads as chip does.

## ThreadNetworkDiagnostics

ThreadNetworkDiagnostics stays the generated server, as matter.js has it.
`ExtAddress` and `Rloc16` are provisional ("P, M") in Matter 1.6. They are
absent until the declaration's `Initial` gives them a value, which may be
null, as matter.js's `ThreadNetworkDiagnosticsProvisionalTest` has it.
