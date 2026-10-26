# Intermittently connected devices

An intermittently connected device (ICD) is a node that sleeps between
short active windows, such as a battery sensor or a lock. This module
covers both sides of that relationship:

- **The device side.** A node built with `node.Config.ICD` serves ICD
  Management, counts and sends Check-In messages to its registered clients,
  and advertises itself as an ICD.
- **The controller side.** The module's controller registers on a LIT
  peer, receives its Check-Ins, and holds interactions while the peer
  sleeps. Its sustained subscription parks and re-subscribes when the peer
  wakes.

Both sides mirror matter.js: `IcdManagementServer.ts`, `IcdMode.ts` and
`protocol/src/icd` on the device side, and `IcdClient.ts`,
`SustainedSubscription.ts` and `RebootResubscribeArmer.ts` on the
controller side.

A mains-powered bridge is not an ICD. The reference daemon sets no
`Config.ICD`, so its root endpoint does not serve ICD Management, and the
ICDM and ICDB certification families do not apply to it
([Certifiability](../certifiability.md)).

## The device: `node.Config.ICD`

`Config.ICD` is a `*node.ICDConfig`. When it is nil the node is not an
ICD. When it is set, the root endpoint mounts ICD Management with the
configured timings:

| Field | Meaning |
| --- | --- |
| `Mode` | `ICDModeSIT`: the Check-In protocol only. `ICDModeLIT`: adds LongIdleTimeSupport and the UserActiveModeTrigger, which LIT makes mandatory |
| `IdleModeDuration` | the longest idle period, in whole seconds, from 1 s to 18 h |
| `ActiveModeDuration` | the shortest active window after a wake, at most `IdleModeDuration` |
| `ActiveModeThreshold` | how long network activity keeps the device active; at least 5 s for LIT |
| `MaximumCheckInBackoff` | the longest gap between Check-Ins to a client that does not answer; zero means `IdleModeDuration` |
| `ClientsSupportedPerFabric` | the Check-In clients a fabric may register; zero means 1 |
| `UserActiveModeTrigger` | the hint, and its instruction, for how a user wakes the device; required for LIT |
| `DynamicSITLIT` | lets the host switch a LIT device between SIT and LIT at run time |

A zero timing takes the schema default. `node.New` validates the
configuration (`Config.Validate`) and refuses one that ICD Management
would refuse.

An ICD node also changes behaviour on its own, beyond serving the cluster:

- **Check-Ins.** It sends a Check-In to each registered client whose
  subject holds no active subscription, at every idle-to-active wake. The
  counter is persisted, and the back-off grows up to
  `MaximumCheckInBackoff` for a client that does not answer.
- **Advertisement.** Its operational DNS-SD record carries the ICD key
  (`ICD=0`/`1`) and `SAT`. A LIT device omits the session idle interval.
  The record is published again when the operating mode flips.
- **MRP.** It pads its MRP backoff by the fast-poll interval (200 ms).
- **Subscriptions.** They are not persisted across a restart, because a
  controller re-subscribes on the next Check-In. A LIT-capable device
  grants its idle cycle as every subscription's MaxInterval.
- **Operating mode.** A LIT device operates as LIT while a client is
  registered, or while `DynamicSITLIT` forces it, and as SIT otherwise.

The registrations and their keys are stored in the `store.ICD` area
(table `matter_icd_registrations`, schema version 7). The counter is the
metadata counter `icd_management.counter`. See
[Persistence](persistence.md).

### The mode machine: `Node.ICD()`

A node of this module never actually sleeps, and stays reachable. The
idle/active mode is a logical state that drives the specification's
behaviour and the host's power hooks. It never cycles on its own. The host
drives it through the handle `Node.ICD()` returns, which is nil for a node
that is not an ICD:

| Method | Effect |
| --- | --- |
| `RequestActiveMode()` | wake for a full active window; Check-Ins go out at the wake |
| `EnterIdleMode()` | enter idle mode now |
| `TriggerUserActiveMode()` | the physical user trigger: reset the Check-In back-off and wake |
| `Active()`, `Mode()` | the idle/active mode, and the operating mode (SIT or LIT) |
| `SetOperatingMode(m)`, `WithdrawForcedOperatingMode()` | force SIT or LIT (`DynamicSITLIT`), and return to the registration-driven mode |
| `OnModeChange(fn)`, `OnModeChangeAsync(fn)` | the active-entered, idle-entered and may-enter-idle events, inline or queued on the node's callback goroutine |

Network activity extends the active window by `ActiveModeThreshold`. When
the window elapses with no further activity, the node signals that it may
now enter idle. The host decides when it actually does.

## The controller: `client.Node.ICD()`

Every `client.Node` of the controller has an `*client.ICDClient`:

| Call | Effect |
| --- | --- |
| `Register(ctx, opts)` | `RegisterClient` with a fresh key. The registration is stored with the node, and the Check-In receive path is armed. Options: the monitored subject (nil is the controller's own node ID), the client type, and the single-admin guard (`AllowMultiAdmin`, `IgnoredVendors`) |
| `Unregister(ctx)` | waits for a sleeping LIT peer to wake, then sends `UnregisterClient` and clears the registration |
| `Forget(ctx)` | clears the registration locally, without `UnregisterClient` |
| `StayActive(ctx, d)` | asks the peer to stay active and returns the duration it promised |
| `Registered()`, `Registration()`, `Available()`, `Awake()`, `NextExpectedCheckIn()`, `PeerSupportsLIT()` | the client's state |
| `OnCheckIn`, `OnRegistered`, `OnUnregistered`, `OnKeyRefreshed`, `OnCheckInMissed`, `OnAvailableChanged` | hooks; each returns its unsubscribe function |

The controller also acts on its own:

- **LIT auto-registration.** For a peer that operates as LIT, it
  registers once the default subscription is active and a fresh read
  confirms the mode. `Node.LITSupported` requires LongIdleTimeSupport and
  a specification version of 1.4.0 or later.
- **Check-Ins.** A Check-In arrives unsecured and goes to the fabric's ICD
  peer table. There it is decrypted under the current or the pending key,
  and a replay is dropped. A Check-In that requests a key refresh starts
  one.
- **Holding interactions.** A Read, Write or Invoke to a registered LIT
  peer that sleeps waits for its next Check-In.
  `client.WithICDAwaitTimeout(ctx, d)` bounds that wait. A peer that does
  not wake in time returns `*client.ICDPeerAsleepError`.
- **Parking the subscription.** The sustained subscription to a
  registered LIT peer parks while the peer sleeps, and re-subscribes on
  its wake signal. When the operating mode flips, it is recreated on the
  priority connection.
- **Timings.** A LIT peer's session parameters take a 5 s idle-interval
  floor when no source gives one. The address monitor adopts a new mDNS
  address for a sleeping LIT peer without probing it.
- **Decommission.** Removing the node clears its registration and its
  receive path.

The registration is stored in the client store with the node's
attribute values (under a cluster ID no cluster has), so it survives a
controller restart.

`client/peer.RebootResubscribeArmer` re-subscribes a peer that restarts
after an OTA apply. The controller's OTA push (`controller.Config.OTA`) arms it.

*Source: `node/icd.go`, `cluster/core/icd_management.go`, `icd/`,
`icd/checkin/`, `store/icd.go`, `controller/client/icd.go`,
`controller/client/icd_errors.go`, `client/interaction/sustained.go`,
`client/peer/reboot_armer.go`.*
