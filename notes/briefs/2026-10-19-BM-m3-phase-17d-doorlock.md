# Brief BM — milestone 3, phase 17d: the DoorLock server after `DoorLockServer.ts`

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
(owner decision 0.3, §A.2, §A.3 "DoorLock", §A.5, Teil B 17d, Teil C).
Base: `origin/m3/phase-17cd-prep` (main at `v0.8.0` plus this brief; merge
`origin/main` before the final guards — PRs #170 and #171 land `v0.8.1`
and `v0.8.2` meanwhile, and brief BL's PR may land before yours). Branch
`m3/phase-17d-doorlock`, one PR against `main` at the end carrying the
label `needs-chiptool`, report `notes/briefs/reports/BM.md`.
`CHANGELOG.md` under `[Unreleased]` is yours; `notes/parity/` (except the
generated `matterjs_tests.md`), `docs/adr/` and `CLAUDE.md` are the
owner's. Brief BL runs in parallel on `m3/phase-17c-thermostat` and owns
`cluster/thermo`, `store/thermostat_presets.go`, `store/migrations/v5.*.sql`
and `examples/reference-bridge/fleet_climate.go`: do not touch those. You
own the chip-tool harness in this pair (`internal/chiptool/`,
`docs/certifiability.md`).

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept, the Sonnet map
`notes/briefs/reads/read-m3-phase17.md` §1.3, §2.3, §3.3 (a map, never a
citation), `docs/guides/testing-in-process.md`, `fabrictest/doc.go`,
`node/attribute_read_response_parity_matterjs_test.go` (a BI port as the
pattern on `fabrictest`), `cluster/lock/doorlock_server.go` (whole, 421
lines: `StateSource`, `DoorLockConfig`, the lock operation and its
events), `cluster/lock/doorlock_server_events_test.go`, the generated
`cluster/spec/doorlock/definition_gen.go` (features :126-138, the
user/credential/schedule structs and command payloads, the events),
`cluster/switch/server.go` (PR #171: a hand-written server with timers on
`clock.Clock` over the generated `spec.Server`, the pattern to follow),
`cluster/core/group_key_management.go` (a server that persists through a
`store` interface and learns of fabric removal), `store/doc.go`,
`store/settings.go`, `store/identity.go`, `store/migrations.go`
(`migrationVersions` :54), `store/migrations/v4.up.sql`, `store/schema.sql`,
`examples/reference-bridge/fleet_safety.go:165-260`,
`internal/chiptool/doc.go`, `internal/chiptool/families_table_test.go:590-600`
(DRLK 2.6 today), `internal/chiptool/testdata/pics/` (the lock's slice is
the file with `DRLK.S=1`). matter.js at the pin only, via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`
(your worktree has no `../matter.js`; `MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`
for `make`): `packages/node/src/behaviors/door-lock/DoorLockServer.ts`
whole (1628 lines: `LockOperationFailedError` :76, `initialize` :114-192,
the four lock commands :193-235, `handleLockOperation` :236, `cipher`/`auth`
:248-259, users :261-418, credentials :419-652, week-day :653-758,
year-day :759-855 and holiday schedules :856-934, `#fabricIndex`/`#sourceNode`
:935-950, auto-relock :951-1013, `#actuate` :1015, `#authorize` :1049,
`#authorizeUser` :1082, `#completeOperation` :1121, `#identifyPin` :1145,
`#refuseWrongCode` :1175, `#isLockedOut` :1192, operating modes :1197,
expiring users :1208-1288, the events :1289-1344, door state :1345,
the helpers :1365-1500, the Supervision overrides :1510-1520, `State`
:1538 (users, credentials, the three schedule lists and `credentialKey`,
all nonvolatile), `Internal` :1576, the functions :1595-1628),
`LockAuth.ts` (whole, 279 lines: `CredentialRef`, `User`, `Credential`,
`Store` :129 with the cipher), `LockSchedule.ts` (whole, 168 lines:
`WeekDay`, `YearDay`, `Holiday`, `localInstant` :97, `isAccessGranted`
:135), the test `packages/node/test/behaviors/door-lock/DoorLockServerTest.ts`
(1 946 lines, 68 `it` in 12 `describe`, on `MockServerNode`, `MockSite`,
`interaction` and `settled`) and `packages/node/src/testing/mock-server-node.ts`,
`node-helpers.ts` as far as the test uses them.

## Scope

`DoorLockServerTest.ts`, 68 cases, register `planned` today → `ported`
(or `partial` with every missing case and its reason). The server grows
from a KeyMatic without users to matter.js's full scope, each part behind
its feature bit, a bridge that selects none served as today.

## What to build (owner decision 0.3: full port, selectable features, credentials in `store/`)

1. **Features in `DoorLockConfig`**: USR, PIN, RID, FGP, COTA, WDSCH,
   YDSCH, HDSCH, DPS, UBOLT (the generated definition's conformance is
   checked by `spec.NewServer`; ALIRO, ALBU and FACE stay out). The
   attributes each feature adds (NumberOfTotalUsersSupported,
   NumberOf…CredentialsSupportedPerUser, Max/MinPINCodeLength,
   WrongCodeEntryLimit, UserCodeTemporaryDisableTime, AutoRelockTime,
   RequirePINforRemoteOperation, the schedule counts, …) come from the
   config with matter.js's defaults where the model has none.
2. **Users and credentials** (`lock/users.go`, `lock/credentials.go`):
   SetUser/GetUser/ClearUser, SetCredential/GetCredentialStatus/ClearCredential
   with every status matter.js answers (DUPLICATE, OCCUPIED, INVALID_FIELD,
   RESOURCE_EXHAUSTED, the programming PIN rule, the per-user credential
   limit, index 0xFFFE clears all), the LockUserChange events, the fabric
   index and source node recorded from the session
   (`im.FabricFilterFromContext`, `im.SubjectFromContext`).
3. **Schedules** (`lock/schedules.go`): the three kinds and
   `isAccessGranted` on the node's clock (`localInstant` as matter.js
   derives it; say in the report what time zone the Go port uses and why).
4. **Authorization and lockout**: `#authorize`/`#authorizeUser`/`#identifyPin`
   for LockDoor, UnlockDoor, UnlockWithTimeout and UnboltDoor with
   RequirePINforRemoteOperation (COTA), the wrong-code counter,
   WrongCodeEntryLimit and UserCodeTemporaryDisableTime (`#refuseWrongCode`,
   `#isLockedOut`), schedule-restricted and disabled users, the
   LockOperationError events with their reasons.
5. **Auto-relock and expiring users** on `clock.Clock` (`DoorLockConfig.Clock`,
   nil = `clock.System`): AutoRelockTime and UnlockWithTimeout
   (:951-1013), ExpiringUser armed on first use and expired by timer
   (:1208-1288); every timer stopped on quiesce as `[Symbol.asyncDispose]`
   :184-191 stops them.
6. **Persistence**: `lock.AuthStore` (an interface in `cluster/lock`: load
   and save users, credentials and the three schedule lists for one
   endpoint), implemented in `store/doorlock.go` on new tables
   `matter_doorlock_users`, `matter_doorlock_credentials`,
   `matter_doorlock_schedules` (migration `v6` — `store/migrations/v6.up.sql`
   and `v6.down.sql`, `migrationVersions` += "v6" after BL's "v5",
   `schema.sql` extended; `TestMigrationsMatchApply`, `TestMigrationsPin`
   and the fingerprint tests tell you what else moves). Credential data is
   stored encrypted: matter.js generates a random 16-byte `credentialKey`
   once and keeps it in nonvolatile state, encrypting with AES-CCM
   (:165-176, :248-253, `LockAuth.Store` :129); the Go port generates the
   key once, keeps it under a named key in `matter_settings`
   (`store.SettingDoorLockCredentialKey`, `GetSetting`/`SetSetting`) and
   encrypts with AES-128-GCM from the standard library (no CCM in
   stdlib, no sixth dependency) — a `by_design` candidate you describe in
   the report. `DoorLockConfig.Auth` nil keeps everything in memory.
7. **The daemon** (`examples/reference-bridge/fleet_safety.go`): the lock
   selects USR, PIN, COTA, WDSCH, YDSCH, HDSCH with an `AuthStore` on the
   daemon's store and matter.js-like limits; `make chiptool-test`, then
   `GOFABRIC_CHIP_FAMILIES=DRLK make chiptool-families` with
   `-update-chip-cases` as `internal/chiptool/doc.go` describes; the PICS
   slice of the lock's endpoint follows the FeatureMap (`gen_pics.py`);
   DRLK 2.6's row in `families_table_test.go` is re-classified from what
   the run shows (it needs YDSCH, which the daemon now has) and
   `docs/certifiability.md` regenerated. Every run in the background with
   a log, read by grep.
8. **Fixes** elsewhere only when small and cited (concept §A.2 rule 4);
   anything that changes `contract/`, a wire shape or another package's
   API is a register candidate.

## How a port is built

As brief BJ's section "How a port is built" (binding): one Go file per
matter.js `describe` is allowed here because the file is large — name
them `cluster/lock/doorlockserver_<describe>_parity_matterjs_test.go`,
every test function in the register's `go` list, the `it` names as
subtests, vectors and expectations from the pin, fixes cited and proven,
the register at the end of each describe group, `make testparity` and
the ratchet with the file's last commit. Harness: `fabrictest`
(`NewSite`, `AddDevice` with `fabrictest.Endpoint("lock",
deviceTypeDoorLock, server)`, `CommissionedPair`, the wire helpers,
`Site.Fake()` for the timers, `Device.Restart` for persistence, a second
controller for the per-fabric cases via `Site.AddController` and
`Device.OpenWindow`; `settled` is `Site.Settle`). No `time.Sleep`.

Commits per step and per describe group, message
`m3/phase-17d BM: <what was built>` or
`m3/phase-17d BM: DoorLockServerTest.ts "<describe>" → cluster/lock (<n> cases, <m> missing)`.

Guards at the end and after every change that moved device behaviour:
`make ci`, `make race`, `make testparity-check`, `make cite-check`,
`make reachability-check`, `make cover-check` (`cluster/lock` keeps or
raises its floor in `script/coverfloor/floors.go`), `make chiptool-test`,
the DRLK family; background runs with a summary log; regenerate and
commit the citation and reachability artefacts when a check exits 2 for
them.

## Report shape (`notes/briefs/reports/BM.md`)

What was built with its matter.js anchors; the describe groups with
their Go test file and the case count ported and missing; **Fixes**;
**fabrictest additions**; **store changes** (tables, migration, key,
fingerprint); **chip-tool** (the runs, goldens and PICS that moved, the
2.6 classification); **Register candidates**; **Guards** with HEAD and
one exit line each; **Differences** to this brief; **Decisions** for the
owner; **Left out**; **Questions**; `by_design` candidates (one paragraph
each with citations — the owner writes the entry).

## Out of scope

ALIRO (and ALBU), FACE, the legacy non-user PIN/RFID commands matter.js
does not implement, Thermostat (brief BL), every other
`packages/node/test/behaviors` directory.
