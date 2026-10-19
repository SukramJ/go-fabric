# Report BM — milestone 3, phase 17d: the DoorLock server after `DoorLockServer.ts`

Branch `m3/phase-17d-doorlock` (from `origin/m3/phase-17cd-prep`, `origin/main`
merged at `v0.8.1` and `v0.8.2`). matter.js read at `862ad75f` only.

## What was built (matter.js anchors at 862ad75f)

`cluster/lock` grows from a KeyMatic without users to `DoorLockBaseServer`'s
scope, each part behind its feature bit (`DoorLockConfig.Features`; zero keeps
the former selection, UBOLT alone without UnlockWithTimeout):

| Part | Go | matter.js |
| --- | --- | --- |
| Construction, SupportedOperatingModes rules, credential key, re-armed expiry deadlines | `lock.New` (`doorlock_server.go`) | `DoorLockServer.ts:114-180` |
| LockDoor / UnlockDoor / UnlockWithTimeout / UnboltDoor | `lockCommand`, `actuate` | `:193-216`, `:1015-1042` |
| Hardware hook, failure reason | `LockOperationHandler`, `LockOperationFailedError`, `hardwareFailureReason` | `:218-236`, `:76-84`, `:1605-1624` |
| Users | `users.go` | `:261-415` |
| Credentials, LockAuth.Store | `credentials.go` | `:419-649`, `:1375-1483`, `LockAuth.ts:104-278` |
| Schedules, `isAccessGranted`, `localInstant` | `schedules.go` | `:653-931`, `:1365-1373`, `LockSchedule.ts:86-167` |
| Authorization, lockout | `authorizeLocked`, `authorizeUserLocked`, `identifyPinLocked`, `refuseWrongCodeLocked`, `isLockedOutLocked` | `:1049-1195` |
| Auto-relock | `scheduleAutoRelockLocked`, `runAutoRelock` | `:951-1011` |
| ExpiringUser | `armExpiringUserOnFirstUseLocked`, `armExpiryTimer`, `runExpireUser`, `disableUserLocked` | `:1203-1285` |
| Events | `emitLockOperationLocked`, `emitLockOperationErrorLocked`, `emitLockUserChangeLocked` | `:1289-1341` |
| DPS | `SetDoorState` | `:1345-1361` |
| Request validation (Supervision) | handlers' enum checks, `bridge/fields_doorlock.go` | `:1491-1521` |
| Persistence | `AuthStore`, `Cipher` (`auth.go`) | `State :1538-1574`, `cipher`/`auth :245-257` |

Locking: `op` is matter.js's behaviour lock (every command and timer action
under it, so operations never overlap); timer actions run in their own
goroutine, as matter.js's `callback(…, { lock: true })` waits for the lock.
`MatterQuiesce` stops the relock and expiry timers (`[Symbol.asyncDispose]`
`:182-189`). A server with a `StateSource` reads LockState from the host and
moves it through `LockInvoke` (or the source's `HandleLockOperation`); one
without keeps LockState itself and moves `DoorLockConfig.Hardware`.

Time zone: schedules are read in `DoorLockConfig.Location`, `time.Local` when
nil — the zone of the process, as matter.js's `localInstant` reads a JS
`Date` in the runtime's local zone (`LockSchedule.ts:97-112`). The Matter
TimeSynchronization zone is not consulted, nor is it in matter.js.

The daemon's lock (`examples/reference-bridge/fleet_safety.go`) selects USR,
PIN, COTA, WDSCH, YDSCH, HDSCH and keeps UBOLT, with the limits of matter.js's
CHIP test lock (`support/chip-testing/src/AllClustersTestInstance.ts:704-724`:
10 users, 10 PINs, 5 credentials per user, PIN 4–8, 10/10/10 schedules,
WrongCodeEntryLimit 3, UserCodeTemporaryDisableTime 10, AutoRelockTime 60),
CredentialRulesSupport Single, and its records in the node's store.

## Describe groups

| describe | Go file (`cluster/lock/`) | Test | ported | missing |
| --- | --- | --- | ---: | ---: |
| DoorLockServer (direct `it`) | `doorlockserver_server_parity_matterjs_test.go` | `TestDoorLockServerParity` | 17 | 0 |
| schedule-restricted access | `doorlockserver_schedulerestricted_parity_matterjs_test.go` | `TestDoorLockServerScheduleRestrictedAccessParity` | 4 | 0 |
| week day window bounds | `doorlockserver_weekdaybounds_parity_matterjs_test.go` | `TestDoorLockServerWeekDayWindowBoundsParity` | 2 | 0 |
| auto relock | `doorlockserver_autorelock_parity_matterjs_test.go` | `TestDoorLockServerAutoRelockParity` | 1 | 0 |
| auto relock with the largest AutoRelockTime | same file | `TestDoorLockServerAutoRelockLargestTimeParity` | 1 | 0 |
| ExpiringUser timeout | `doorlockserver_expiringuser_parity_matterjs_test.go` | `TestDoorLockServerExpiringUserTimeoutParity` | 12 | 0 |
| authorization of lock operations | `doorlockserver_authorization_parity_matterjs_test.go` | `TestDoorLockServerAuthorizationParity` | 6 | 0 |
| wrong-code lockout | `doorlockserver_lockout_parity_matterjs_test.go` | `TestDoorLockServerWrongCodeLockoutParity` | 5 | 1 |
| OperatingMode | `doorlockserver_operatingmode_parity_matterjs_test.go` | `TestDoorLockServerOperatingModeParity` | 4 | 0 |
| lock hardware | `doorlockserver_hardware_parity_matterjs_test.go` | `TestDoorLockServerLockHardwareParity` | 10 | 0 |
| lock operation events | `doorlockserver_operationevents_parity_matterjs_test.go` | `TestDoorLockServerLockOperationEventsParity` | 1 | 0 |
| LockUserChange | `doorlockserver_userchange_parity_matterjs_test.go` | `TestDoorLockServerLockUserChangeParity` | 4 | 0 |
| **total** | | | **67** | **1** |

Register: `planned` → `partial` (67 of 68); `testparityPlannedRatchet` −1
(63 after merging main's −5). Missing: "measures the lockout on the monotonic
clock" — it steps MockTime's wall clock apart from its timers; `clocktest.Fake`
has one reading, so the step moves the lockout deadline's reading too. The
server compares `time.Time` values, monotonic under `clock.System`, so the
behaviour holds in production.

Harness (`doorlockserver_harness_test.go`): a commissioned `fabrictest` pair
per case, the lock a bridged endpoint (`fabrictest.Endpoint`); events read
from the device's event log over the wire since a mark; MockTime.advance =
`Fake.Advance`, stepWallClock = `Fake.Set`, settled = `Site.Settle`. Two
ExpiringUser wall-clock cases pass through the fake's late timer fire where
matter.js passes through an early fire and re-arm; their asserted outcomes are
matter.js's (comment in the file). matter.js's `online({})` actor has fabric
NO_FABRIC; the port's requests come from the pair's fabric, which therefore
creates the seeded users.

Coverage `cluster/lock`: 92.2 % (floor 86 → 90).

## Fixes

- `bridge/fields_doorlock.go`: DoorLock requests whose fields the schema
  refuses answer INVALID_COMMAND for the ten commands matter.js remaps, and
  the Get…Schedule index fields are not constraint-checked
  (`DoorLockServer.ts:1491-1521`); the bridge answered CONSTRAINT_ERROR.
  Proven by `TestRequestValidationOverTheWire` and TC-DRLK-2.7 / 2.8.

## fabrictest additions

- `fabrictest.Endpoint` — the file brief BJ added (#170), carried identically
  until `v0.8.1` merged.

## Store changes

- Tables `matter_doorlock_users`, `matter_doorlock_credentials`,
  `matter_doorlock_schedules` (endpoint_key-scoped; no FK to fabrics, as
  matter.js keeps users across fabric removal); migration `v6`
  (`migrationVersions` v1–v4, v6; v5 is BL's), `schema.sql` extended.
- `store.LoadDoorLockAuth` / `SaveDoorLockAuth` (replace-all per endpoint in
  one transaction; a `door_lock.saved:<key>` setting tells "never saved" from
  "all cleared"); `store.SettingDoorLockCredentialKey` holds the 16-byte key,
  hex.
- Pins: `len(Migrations())` 5, last `v6`; 17 matter_* tables; fingerprint
  `b50fe661e4bdb70e9bdaf5f9841e744c48eab9b434b2471e6940da06942d506f`.
  When BL's v5 lands, the pin becomes 6 and the fingerprint moves again.

## chip-tool

- `make chiptool-test`: pass (commission, Go controller, suite).
- `GOFABRIC_CHIP_FAMILIES=DRLK make chiptool-families-update`: first run
  2.5/2.7/2.8/2.9 failed — 2.7/2.8 on CONSTRAINT_ERROR (the fix above),
  2.5/2.9 because `TC_DRLK_2_5.py:184` and `TC_DRLK_2_9.py:363` address the
  lock as endpoint 1 (case edits in the DRLK row, `{ep:0x000A}`). Second run:
  2.1–2.9 and 2.12 pass; 2.10, 3.2 manual; 2.11 needs RID+FGP; 2.13 ALIRO.
- DRLK 2.6: no longer a gap (exclusion removed); runs 11 steps.
- Goldens: DRLK 2.1–2.4, 2.8, 2.9 counts up, 2.5/2.6/2.7 new; PICS
  `pics/ep17.txt` follows the FeatureMap (42 codes); the hand PICS declares
  AutoRelockTime, WrongCodeEntryLimit, UserCodedTemporaryDisableTime and
  RequirePINForRemoteOperation writable. `docs/certifiability.md`: DRLK 10
  pass, 2 not supported, 2 harness (was 7/4/3).
- IDM/10.4 over every slice (narrowed run): pass.

## OVENOPSTATE/2.5

Run from this branch after merging `origin/main` (v0.8.1), without update:
`ran {Steps:26 StepsSkipped:36}, golden {StepsSkipped:42}` reproduced.
The log (each line printed twice by two loggers, hence 36 = 2 × 18) runs
steps 1–8 (commission, DeviceTypeList, istestmandated, PICS check, step 5,
**6 subscribe to OperationCompletion, 7 manual/pipe state to Stopped, 8
Start**) and skips 9–26. The golden's 42 = 2 × 21 skips 6–26.

- Step 5 (`TC_OpstateCommon.py` TEST_TC_OPSTATE_BASE_2_5) skips everything
  after it when `OVENOPSTATE.S.E01` is false. `pics/ep32.txt` changed
  `OVENOPSTATE.S.E01=0` → `=1` in commit 5b37e38 (#137, the findings wave,
  2026-10-17: "gen_pics.py marks device-type mandatory events", AY item 11)
  without the OVENOPSTATE/2.5 golden being regenerated — so steps 6–8 now
  run. The PICS did change on 2026-10-17; neither the clock refactor nor the
  oven countdown is involved.
- Step 9 reads no CountdownTime: `attribute_guard` finds CountdownTime
  (0x0002) not in the oven cavity's AttributeList (`OVENOPSTATE.S.A0002=0`),
  marks step 9 skipped, `initial_countdown_time` stays NullValue and steps
  10–26 are skipped. No CountdownTime or OperationalState value was read; the
  Start command (step 8) answered OperationalCommandResponse.
- Reading: the new count is the correct one for the committed PICS; the
  golden needs regenerating (owner's decision; not done here). If the oven
  should exercise 10–26 it would have to serve CountdownTime.

## Register candidates

- A monotonic reading in `clock.Clock` / `clocktest.Fake` (wall clock
  stepping apart from timers), which would port the missing lockout case and
  exercise the ExpiringUser early-fire re-arm.
- Request-schema remapping as a definition-level option of `cluster/spec`
  (today `bridge/fields_doorlock.go` holds the DoorLock rule by hand).

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` | 678bb96 | exit 0 |
| `make race` | 678bb96 | exit 0 |
| `make testparity-check` | 678bb96 | exit 0 |
| `make cite-check` | 678bb96 | exit 0 (drift 264, ratchet 265 → 264) |
| `make reachability-check` | 678bb96 | exit 0 (unreached 5, unchanged) |
| `make cover-check` | 678bb96 | exit 0 (`cluster/lock` floor 90) |
| `make chiptool-test` | 60e7989 | exit 0 |
| DRLK family (`-update`) | f94aac2 | exit 0 (goldens committed in 6f5bbe5) |
| IDM/10.4, every slice | 8642893 | exit 0 |
| OVENOPSTATE (no update) | 8642893 | exit 2 — 2.5 only, see below |

`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` throughout. An
earlier `make race` at 5e94aa2 failed once in `controller/client`
(`TestClientSubscribeCacheFabricFilterParity`: "restart controller 1: listen
[fe80::1]:5540 is in use", a fabrictest restart under load, untouched here);
the run at 678bb96 passed. After the chip-tool runs the branch merged
`v0.8.2` (the Switch server, daemon button) and took lint-only changes in
`cluster/lock`; the coordinator held the harness for PR #175, so
`make chiptool-test` and the DRLK family were not rerun at 678bb96 — CI's
`needs-chiptool` jobs run them.

## Differences to the brief

- The daemon keeps UBOLT besides the six features the brief names (dropping
  it would remove UnboltDoor from the daemon and DRLK 2.12).
- `DoorLockConfig.Features` zero keeps the former UBOLT-only selection;
  any explicit selection accepts UnlockWithTimeout (matter.js implements it).
- `lock.AuthStore` is defined over `store` types (`store.DoorLockAuth` …):
  `store` stays a leaf, `cluster/lock` imports it as `cluster/core` does.
- The record key is `DoorLockConfig.AuthKey` (the daemon uses the endpoint's
  StableKey); `Initial` credentials are always clear text and encrypted at
  start (matter.js encrypts state only when no key exists yet).
- The daemon's RequirePINforRemoteOperation is false (matter.js's CHIP lock:
  true), so controllers without PIN keep operating the demo lock.
- Several describes share a file (the two auto-relock describes).

## Decisions for the owner

- Whether `NewDoorLockServer` (kept, panics on a configuration `New` refuses)
  should be deprecated in favour of `lock.New`.
- OVENOPSTATE/2.5 golden (above).

## Left out

- ALIRO, ALBU, FACE; the legacy non-user PIN/RFID commands.
- DoorOpenEvents / DoorClosedEvents are counted when served, but the config
  offers no way to serve them yet.
- Removing a fabric does not clear the users it created (matter.js does not
  either).

## Questions

- Should a host get a way to serve the optional settings attributes
  (Language, LEDSettings, SoundVolume, …)? CHIP's ci-pics-values declares
  them writable for its sample lock; matter.js's CHIP lock serves them.

## by_design candidates

- **BD-Matter-DoorLock-CredentialCipher.** matter.js encrypts stored
  credential data with AES-128-CCM under a 16-byte random key kept in the
  server's nonvolatile state (`DoorLockServer.ts:153-164`, `:245-250`,
  `:1569-1573`; `LockAuth.ts:129-278`). The Go server uses AES-128-GCM
  (`crypto/cipher`, random 12-byte nonce ahead of the sealed data) under the
  same kind of key, kept once per node in `matter_settings`
  (`store.SettingDoorLockCredentialKey`); the standard library has no CCM and
  the module takes no dependency for one. The format is at rest only, never
  on the wire.
- **BD-Matter-DoorLock-HostLockState.** matter.js's DoorLockServer owns
  `lockState` and moves the hardware through `handleLockOperation`
  (`:218-236`, `:1040`); the Go server keeps that mode for a server without
  `StateSource`, and with one projects the host's lock and moves it through
  `LockInvoke`, leaving LockState to the host — the bridge's rich-model rule.
- **BD-Matter-DoorLock-ScheduleZone.** `localInstant` reads the runtime's
  local zone (`LockSchedule.ts:97-112`); the Go server reads
  `DoorLockConfig.Location`, `time.Local` when nil — the same default, with a
  host override.
