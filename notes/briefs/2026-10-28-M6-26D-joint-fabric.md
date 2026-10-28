# Brief 26-D — Joint Fabric after connectedhomeip: the Datastore, the Administrator with its trust verification, the commissioner's JCM (milestone 6, concept v10; ADR 0020 stage 6c)

Date 2026-10-28. Owner decisions 2026-10-27/28: Joint Fabric now, as one
wave (not split), after chip — matter.js has no logic
(`JointFabricAdministratorServer.ts:14`, `JointFabricDatastoreServer.ts:14`
are the empty subclasses at `862ad75f`; the register markers). Read map:
`/home/markus/.cache/go-fabric-scratch/read-26d-27a.md` §1-4 (a map;
verify every line before citing). **Model:** Opus. Branch
`m6/26d-joint-fabric` from origin/main (fetch first). One pull request,
commits per part, one report `notes/briefs/reports/26D.md`. go-fabric
`CLAUDE.md` rules (chip via `git -C /home/markus/Dokumente/GitHub/connectedhomeip
show 6170af84:<path>`, matter.js via `git -C /home/markus/Dokumente/GitHub/matter.js
show 862ad75f:<path>`; citations; no attribution lines; English; token
rules; `secure/`, `commissioning/`, ACL are Fable paths — the commissioner's
trust verification touches `controller/commissioner`: implement it there
behind clear seams and list every file under `secure/` or `commissioning/`
you had to touch, with the reason, in the report). Copy this brief to
`notes/briefs/` in the first commit. Disjoint from 26-A3 (`cluster/camera`,
`cluster/tls`, `node/` TLS seam) and 27-A (`client/interaction` batching,
`client/discovery`, `controller/dcl`, `controller/commissioner` NOC
rotation — coordinate by file: your commissioner changes are the JCM files
only, new files where possible).

## 1 JointFabricDatastore (0x0752) — `cluster/jointfabric`

- `jointfabric.Datastore` after chip `src/app/server/JointFabricDatastore.cpp`
  (2376 lines; the server file `joint-fabric-datastore-server.cpp` only
  maps the 24 command callbacks :293-… and the 14 attribute reads onto it,
  and reports NodeList changes :287-291). Capacities of
  `JointFabricDatastore.h:512-517` (kMaxNodes 256, kMaxAdminNodes 32,
  groups, key sets, friendly name 32, ACLs 64). Setters :287-358
  (anchor CA/node/vendor/fabric index, friendly name, status), the
  node lifecycle (`AddPendingNode` :181, `UpdateNode` :201, `RemoveNode`
  :221, `RefreshNode` :241, `ContinueRefresh` :254-1130, `SetNode` :1132),
  key sets, admins, groups, endpoints, ACLs and bindings (:1140-…) with
  chip's status codes (ConstraintError, NotFound, ResourceExhausted — the
  line list is in the read map §1e; cite each). Every entry carries chip's
  pending/committed state (`MarkEntryCommittedIfFound`).
- Port `DatastoreSync` = chip's `Delegate` (:166-250: the eight `SyncNode`
  overloads and the six `Fetch*`): the host (an admin app) pushes the
  datastore's entries to the member nodes and fetches their lists; the
  module owns the tables and the refresh state machine. `Listener`
  collapses into the attribute change reporting.
- Persistence: the tables through the module's store pattern (the TLS
  servers of 26-A3 persist one settings record per server — mirror that
  shape once 26-A3 is on main, else the lock's credential record).
- Tests: chip's 26 `TEST` of `tests/TestJointFabricDatastore.cpp` ported
  as `datastore_chip_test.go`; `datastore_rules_test.go` for the rest;
  the Python cases `TC_JFDS_2_1…2_5`, `TC_JFDS_BindingSync` are read for
  the rules they check and listed in the report (ported where a rule has
  no chip unit test).

## 2 JointFabricAdministrator (0x0753) — `cluster/jointfabric`

- `jointfabric.Administrator` after chip
  `joint-fabric-administrator-server/joint-fabric-administrator-server.cpp`
  (the `#if CHIP_DEVICE_CONFIG_ENABLE_JOINT_FABRIC` body :172-449):
  `OpenJointCommissioningWindow` :173-237 (cluster-specific statuses
  :196-226; it opens the commissioning window through the node's
  AdministratorCommissioning with the joint flag — read how the window is
  opened in `cluster/core` and reuse it), `AnnounceJointFabricAdministrator`
  :239-290, `ICACCSRRequest` :305-354, `AddICAC` :356-391 (the
  VerifyAddICAC steps and DN rules); `TransferAnchorRequest` /
  `TransferAnchorComplete` are empty in chip (:439-449) — serve them as
  chip does (success without effect) and say so in the by_design proposal.
  `AdministratorFabricIndex` from the node's fabric table. Fail-safe
  participation as chip's `OnPlatformEventHandler` :109-117 (join the
  node's fail-safe wiring of 26a, `node/failsafe.go`).
- `JCMCommissionee` (`JCMCommissionee.cpp`, 510 lines): the commissionee's
  trust verification state machine (read the admin's fabric index,
  fabrics, certs and NOCs; vendor-id verification) in
  `jointfabric/commissionee.go`, driven from the administrator server;
  tests: chip's 13 `TEST_F` of `tests/TestJCMCommissionee.cpp`.
- Port `AdministratorPort`: `GetIcacCsr` (chip's delegate, `.h:29-36`)
  and the host's consent where chip asks the user (the
  `jf-admin-app`'s `JFAManager.cpp` shows the calls — read it for the
  sequence, cite it as the example app, not as the server).
- Device type: `device/joint_fabric_administrator_gen.go` exists (0x130,
  both clusters mandatory, `endpoints/joint-fabric-administrator.ts`);
  the kit mounts both servers as its defaults when the declaration carries
  the ports (`defaults_jointfabric.go`); `TestSelfHeldDefaults` cases.

## 3 The commissioner's JCM — `controller/commissioner/jcm*.go`

- After chip `src/controller/jcm/DeviceCommissioner.cpp` (555 lines:
  `StartJCMTrustVerification` :45, `ContinueAfterUserConsent` :63,
  `ContinueAfterVendorIDVerification` :75, parsers :87-…),
  `AutoCommissioner.cpp` (:39, :61), `src/credentials/jcm/TrustVerification.cpp`
  (the stage machine :34-45) and `VendorIdVerificationClient.cpp`
  (:28-127): when a commissionee advertises as a joint-fabric
  administrator, the commissioner reads its admin fabric index and
  endpoint, its fabrics and NOCs, verifies the vendor id, asks the host
  for consent (a `PairOptions`/commissioner option: `JointFabric
  *JointFabricOptions{Consent func(TrustInfo) bool}`), then commissions
  onto the joint fabric. The progress steps of CK-1 get the JCM stages as
  Go-named steps (say which numbers; by_design sentence).
- Tests: chip's 7 + 3 + 13 `TEST_F` (`TestJCMCommissioner.cpp`,
  `TestTrustVerification.cpp`, `TestVendorIdVerificationClient.cpp`)
  ported by meaning; a `fabrictest` end-to-end: a device with the
  administrator kit is commissioned by the Go commissioner with consent,
  the datastore sees the anchor, a refused consent aborts.

## 4 Docs, register, release

- `docs/guides/joint-fabric.md` (new, in the nav: what Joint Fabric is,
  the datastore sync port, the administrator and its CSR port, the
  commissioner side and consent), ports/Host rows, comparison rows (both
  clusters ✅), ADR 0020: stage 6c status "implemented in wave 26-D"
  (amend the row at `docs/adr/0020-*.md:115`), roadmap row.
- Register: two Ahead-of-matter.js rows (markers above; sources chip
  paths above; tests) plus a third for the commissioner's JCM if a
  matter.js marker exists for it (the commissioning flow has no JCM step —
  cite the step list `ControllerCommissioningFlow.ts:536-656` as the
  absence); `make ahead-check` holds. The by_design entries are Fable's
  after hand-back — propose each in one paragraph.
- CHANGELOG `[Unreleased]` Added; a break only if a type moves (then
  Changed + migration guide). Coverage floors for the new packages.
- Guards: `make ci`, `make race` twice, `go test ./script/...`,
  `make ahead-check`, `make reachability` + `cite-report`
  (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`) with snapshots
  committed, `mkdocs build --strict`; one bundle at the end under `flock
  /home/markus/.cache/go-fabric-scratch/guards.lock` (one bundle at a time;
  wait for the lock). chip-tool only if the reference bridge's composition
  changes (it should not). If the memory reaper kills `make`, report and
  do not restart. Hand back once: PR number, HEAD, guards, per part the
  ported/left-out test counts, the `secure/`/`commissioning/` files
  touched, differences, Questions verbatim, the by_design proposals.
