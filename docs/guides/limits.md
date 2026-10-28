# Limits

A node holds every table a peer can fill to a fixed size, and gives up on a
peer that stops answering after a fixed number of retries. This page lists
those limits. Each row gives the value and the constant that holds it, the
reference it comes from, what a peer sees when it reaches the limit, and
whether a host can change it.

The rule for the last column comes from matter.js: where matter.js lets the
application configure a value, the host can set it here, either as a field
of `node.Config.Limits` (`node.Limits`) or as a field of `node.Config`.
Where matter.js keeps a constant, the value is a constant here too.
`SupportedFabrics` and `MaxPathsPerInvoke` were configurable before
`node.Limits` existed and stay where they were, on `node.Config`, so no
host has to change its configuration in a patch release.
"Module's own" marks a limit matter.js does not have, with the reason the
module keeps it. The matter.js references are at the pin
`862ad75f`, the one the parity tests and `make cite-check` read.

## Sessions and pairing

| Limit | Value | Reference | At the limit | Host sets |
| --- | --- | --- | --- | --- |
| Sessions per peer | `MaxSessionsPerPeer = 5` in [`secure/operational/manager.go`](https://github.com/SukramJ/go-fabric/blob/main/secure/operational/manager.go) | matter.js `MAX_SESSIONS_PER_PEER`, `packages/protocol/src/session/SessionManager.ts:185-189` | A CASE session the peer establishes beyond the cap closes that peer's least recently active session. The cap counts per peer, so one noisy peer cannot evict another peer's sessions. | constant |
| PASE failures per commissioning window | `paseMaxErrors = 20` in [`bridge/securechannel.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/securechannel.go) | matter.js `PASE_COMMISSIONING_MAX_ERRORS`, `packages/protocol/src/session/pase/PaseServer.ts:34`, `:106-110` | The 20th failed PASE handshake (a malformed Pake1 counts) revokes the open commissioning window and starts the lockout. Log event `matter.rx.sc.pase_bruteforce`, a `pairing` diagnostic event, the `pase.lockouts` counter. | constant |
| PASE lockout | `paseLockoutCooldown = 15 * time.Minute`, doubling per lockout up to `paseLockoutMaxCooldown = 4 * time.Hour`, in [`bridge/securechannel.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/securechannel.go) | Module's own. matter.js ends the refusal with the window (`PaseServer.ts:106-110`). This node also answers PASE from its configured passcode when no window is open (an uncommissioned node), so the refusal needs an expiry of its own. Without one, 20 malformed Pake1s from any host on the LAN would disable pairing until a restart. | PASE messages are dropped until the lockout ends or a new commissioning window opens, which clears the lockout. Log event `matter.rx.sc.pase_locked_out`. | constant |
| One PASE handshake at a time; pairing timeout | `pasePairingTimeout = 60 * time.Second` in [`bridge/securechannel.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/securechannel.go) | matter.js `PASE_PAIRING_TIMEOUT`, `PaseServer.ts:33` | A second commissioner is ignored while a handshake runs. A handshake that has not finished within 60 s releases its slot. | constant |
| Unsecured duplicate-detection windows | `maxUnsecuredWindows = 256` in [`bridge/securechannel.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/securechannel.go) | Module's own. The source node ID of unsecured PASE traffic is unauthenticated, so a flood of spoofed sources would otherwise grow the table without bound. | Past the cap, a new source is treated as fresh. The PASE handler's own replay guard still rejects a genuine replay. | constant |
| Session-miss burst table | `sessionMissBurstThreshold = 8` misses within `sessionMissBurstWindow = 15 * time.Second`, one log line per `sessionMissBurstCooldown = 5 * time.Minute`, at most `sessionMissBurstMaxEntries = 4096` session IDs, in [`bridge/session_miss_burst.go`](https://github.com/SukramJ/go-fabric/blob/main/bridge/session_miss_burst.go) | Module's own. It throttles a log line and does not change the protocol. The cap exists because any host that can reach the UDP port can spray session-ID misses. | The eighth miss for one session ID within the window logs `matter.rx.session_miss.burst` at INFO. Once the table is full, new session IDs are not tracked. | constant |

The [security page](../security.md) describes the PASE limits from the attacker's side.

## Fabrics, access control, groups

| Limit | Value | Reference | At the limit | Host sets |
| --- | --- | --- | --- | --- |
| Fabrics | `defaultSupportedFabrics = 5`, at most `maxSupportedFabrics = 254`, in [`node/config.go`](https://github.com/SukramJ/go-fabric/blob/main/node/config.go) | OperationalCredentials `SupportedFabrics`, Matter 1 to 254. matter.js defaults it to 254 (`packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:86-89`). The facade keeps 5, its value from before the field was configurable. | AddNOC answers NOCResponse `TableFull` (matter.js `OperationalCredentialsServer.ts:255-258`). | `node.Config.SupportedFabrics` |
| ACL entries per fabric | `accessControlEntriesPerFabricLimit = 4` in [`cluster/core/access_control.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/access_control.go) | matter.js state `accessControlEntriesPerFabric`, default 4 (`packages/node/src/behaviors/access-control/AccessControlServer.ts:64-66`); Matter 4 to 65534 | An ACL write with more entries for the fabric fails with `ResourceExhausted` (`AccessControlServer.ts:179-181`). | `node.Limits.AccessControlEntriesPerFabric` |
| Subjects and targets per ACL entry | `accessControlSubjectsPerEntry = 4`, `accessControlTargetsPerEntry = 4` in [`cluster/core/access_control.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/access_control.go) | matter.js state `subjectsPerAccessControlEntry`, `targetsPerAccessControlEntry`, default 4 (`AccessControlServer.ts:58-63`); Matter 4 to 65534 and 3 to 65534 | An entry with more subjects or targets fails the write with `ResourceExhausted` (`AccessControlServer.ts:192-198`). | `node.Limits.SubjectsPerAccessControlEntry`, `node.Limits.TargetsPerAccessControlEntry` |
| Groups per fabric | `defaultMaxGroupsPerFabric = 22` in [`cluster/core/group_key_management.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/group_key_management.go) | matter.js state `maxGroupsPerFabric = 22` (`packages/node/src/behaviors/group-key-management/GroupKeyManagementServer.ts:610`) | A group beyond the limit is refused with `ResourceExhausted` (`GroupKeyManagementServer.ts:219-223`, `:480-486`). | `node.Limits.MaxGroupsPerFabric` |
| Group key sets per fabric | `defaultMaxGroupKeysPerFabric = 20` in [`cluster/core/group_key_management.go`](https://github.com/SukramJ/go-fabric/blob/main/cluster/core/group_key_management.go) | matter.js state `maxGroupKeysPerFabric = 20` (`GroupKeyManagementServer.ts:609`); Matter 1 to 65535 | The IPK's key set 0 counts as one. KeySetWrite beyond the limit fails with `ResourceExhausted` (`GroupKeyManagementServer.ts:379-384`). | `node.Limits.MaxGroupKeysPerFabric` |

## Interaction Model

| Limit | Value | Reference | At the limit | Host sets |
| --- | --- | --- | --- | --- |
| Commands per InvokeRequest | `DefaultMaxPathsPerInvoke = 10` in [`im/invoke.go`](https://github.com/SukramJ/go-fabric/blob/main/im/invoke.go) | matter.js `DEFAULT_MAX_PATHS_PER_INVOKE` (`packages/types/src/protocol/definitions/interaction.ts:13`), the `basicInformation.maxPathsPerInvoke` state | A larger batch is refused with `InvalidAction` (matter.js `packages/node/src/node/server/InteractionServer.ts:1017-1021`). BasicInformation and the PASE and CASE session parameters advertise the same value. | `node.Config.MaxPathsPerInvoke` |
| Subscriptions per fabric | `DefaultMaxSubscriptionsPerFabric = 16` in [`im/subscription/manager.go`](https://github.com/SukramJ/go-fabric/blob/main/im/subscription/manager.go) | Module's own. matter.js has no per-fabric quota. The cap bounds the report engine's work per fabric; 16 is above the Matter floor of 3 that BasicInformation's CapabilityMinima advertises. | The SubscribeRequest is answered `ResourceExhausted`. | constant |
| A subscription's MaxInterval | `DefaultMaxIntervalCeilingSeconds = 3600` in [`im/subscription/manager.go`](https://github.com/SukramJ/go-fabric/blob/main/im/subscription/manager.go) | matter.js `MAX_INTERVAL_PUBLISHER_LIMIT` (one hour, `packages/node/src/node/server/ServerSubscription.ts:71`) and the configurable `network.subscriptionOptions.maxInterval` (`packages/node/src/behavior/system/network/NetworkServer.ts:210`, applied in `ServerSubscription.ts:314-344`). matter.js defaults the configurable value to three minutes (`:72`); this module defaults it to the hour, as chip does. | A subscriber asking for a longer MaxInterval is granted the limit, never less than its own MinIntervalFloor. | `node.Limits.SubscriptionMaxInterval` (1 s to 1 h) |

## Transport and reliability

| Limit | Value | Reference | At the limit | Host sets |
| --- | --- | --- | --- | --- |
| TCP connections per peer address | `MaxConnectionsPerPeerIP = 3` in [`transport/tcp/listener.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/tcp/listener.go) | matter.js `MAX_CONNECTIONS_PER_PEER_IP`, `packages/protocol/src/transport/tcp/TcpTransport.ts:28` | A fourth inbound connection from the same IP address is closed. Log event `matter.tcp.reject`. | constant |
| Messages in flight per TCP connection | `maxInFlightPerConn = 64` in [`transport/tcp/conn.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/tcp/conn.go) | Module's own. matter.js gets the same backpressure from async iteration over the socket (`TcpChannel.ts:81-82`). | The reader stops reading, and the peer sees TCP backpressure. A stream never drops a message. | constant |
| MRP retransmissions | `MaxRetransmissions = 4` in [`transport/mrp/retransmit.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/mrp/retransmit.go) | matter.js `MRP.MAX_TRANSMISSIONS = 5`, the first send included (`packages/protocol/src/protocol/MRP.ts:33`) | A reliable message that is still unacknowledged after the fourth retransmission fails its exchange. | constant |
| MRP backoff | `MRPBackoffThreshold = 1.6`, `MRPBackoffJitterFactor = 0.25`, `MRPBackoffMargin = 1.1` in [`transport/mrp/retransmit.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/mrp/retransmit.go) | matter.js `MRP.BACKOFF_BASE = 1.6`, `BACKOFF_JITTER = 0.25`, `BACKOFF_MARGIN = 1.1` (`MRP.ts:36-42`), Matter §4.12.2.1 | Each retransmission waits longer by the growth factor, with jitter. | constant |
| MRP intervals when the peer announced none | `SessionIdleIntervalDefault = 500 * time.Millisecond`, `SessionActiveIntervalDefault = 300 * time.Millisecond`, `SessionActiveThresholdDefault = 4 * time.Second`, `MRPBackoffBase = 300 * time.Millisecond`, in [`transport/mrp/retransmit.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/mrp/retransmit.go) | matter.js `SessionIntervals.defaults` (`packages/protocol/src/session/SessionIntervals.ts:45-49`). | The retransmission timing toward a peer that sent no session parameters. | constant |
| The node's own MRP intervals | 500 ms idle, 300 ms active, 4000 ms active threshold, in [`node/sessions.go`](https://github.com/SukramJ/go-fabric/blob/main/node/sessions.go) | matter.js `sessions.intervals` (`packages/node/src/behavior/system/sessions/SessionsBehavior.ts:115-120`), handed to the session manager (`ServerNetworkRuntime.ts:357-363`) and the operational advertisement (`DeviceAdvertiser.ts:361-364`). A threshold above 65535 ms is refused (`SessionManager.ts:58-67`). | The PASE and CASE session parameters and the operational record's SII, SAI and SAT carry them; an ICD's record carries its own threshold and, while LIT, no SII. `Node.SessionParameters` returns what the node reports. | `node.Config.Sessions` (whole milliseconds; the threshold at most 65535 ms) |
| BDX retransmission margin | none by default | matter.js `LimitsConfig.bdxAdditionalMrpDelay` (`packages/node/src/behavior/system/network/NetworkServer.ts:152-153`), applied as the exchange's `peerAdditionalMrpDelay` (`packages/protocol/src/bdx/BdxProtocol.ts:139-142`) | The margin joins the peer's base interval on the exchange of every inbound BDX transfer (an OTA requestor's download), and the backoff grows it. | `node.Limits.BDXAdditionalMrpDelay` (not negative) |
| Duplicate-detection window | `windowSize = 32` in [`transport/mrp/window.go`](https://github.com/SukramJ/go-fabric/blob/main/transport/mrp/window.go) | matter.js `MSG_COUNTER_WINDOW_SIZE`, `packages/protocol/src/protocol/MessageReceptionState.ts:14` | A counter that falls behind the window is treated as a duplicate. | constant |

## Controller side

| Limit | Value | Reference | At the limit | Host sets |
| --- | --- | --- | --- | --- |
| Exchanges per session | `MaxConcurrentExchangesPerSession = 30` in [`client/exchange/manager.go`](https://github.com/SukramJ/go-fabric/blob/main/client/exchange/manager.go) | matter.js `MAXIMUM_CONCURRENT_OUTGOING_EXCHANGES_PER_SESSION`, `packages/protocol/src/protocol/ExchangeManager.ts:54` | Opening one more exchange closes the session's least recently active one. The unsecured session is exempt. | constant |
| A subscription's requested MaxInterval | `SubscriptionMaxIntervalPublisherLimit = 60 * time.Minute` in [`client/interaction/subscribe.go`](https://github.com/SukramJ/go-fabric/blob/main/client/interaction/subscribe.go) | matter.js `SUBSCRIPTION_MAX_INTERVAL_PUBLISHER_LIMIT`, `packages/protocol/src/action/client/ClientInteraction.ts:173` | The MaxIntervalCeiling a subscription asks for when the caller gives none. | per request (`SubscribeRequest.MaxIntervalCeiling`) |

## How the values stay true

`TestLimitsPage` in
[`script/docscheck`](https://github.com/SukramJ/go-fabric/tree/main/script/docscheck)
reads every `` `Name = value` `` cell of this page. It finds `Name` in the
Go file the row links and compares the declared value with the cell. The
test fails when a constant changes its value, moves to another file or is
renamed, until the page follows.
