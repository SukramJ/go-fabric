# Brief phase 34 — the second composition profiles of the reference daemon (M8)

Date 2026-11-04. Roadmap `docs/roadmap.md` phase 34 (`| 34 |`): the
certification families without a run get a daemon composition that serves
them — WEBRTCP, AVSM, PAVST (+PAVSTI), AVSUM, TLSCERT, TLSCLIENT, ZONEMGMT,
CHIME, JFADMIN, JFDS, TBRM, DGTHREAD, THNETDIR — and the rest of the "31
prefixes without a family" is classified; proof "31 → ≤ 8". **Model:**
one Opus agent. Branch `m8/phase-34` from main 19588e9b, worktree
`../go-fabric-p34`, logs and `summary.log` in
`~/.cache/go-fabric-scratch/p34/`. The chip-tool harness is yours alone
while this wave runs (phase 33 runs beside you without it). Map:
`~/.cache/go-fabric-scratch/read-phase34.md` (Sonnet; §2 harness, §3 Go
servers, §4 chip at the pin, §5 matter.js, §Summary, §Questions) — a map,
never a citation: verify every line you build on.

Pins. matter.js 862ad75f (`git -C ../matter.js show 862ad75f:<path>`).
connectedhomeip **6170af84 — the working tree `../connectedhomeip` is at
f589ae56, so read chip only with `git -C ../connectedhomeip show
6170af84:<path>` / `ls-tree 6170af84`** (the tree over-counts: ZONEMGMT has
4 cases at the pin, not 5). Harness image `CHIP_TEST_IMAGE` (Makefile:303,
a build of CHIP 6170af84): check with Docker what it carries
(`chip-camera-app`, `matter-network-manager-app`, `/lib/test-descriptor.json`
entries for these families) before relying on it.

## 0 Working rules

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; `/tmp` is a
small tmpfs. Commits as `SukramJ <sukramj@icloud.com>` (`git var
GIT_AUTHOR_IDENT` first), `<area>: <what> (phase 34)`, no attribution
lines; stage named paths (never `git add -A` after `go build` of a main
package); never `git reset --hard`, never force-push; push regularly.
Guards under `flock ~/.cache/go-fabric-scratch/guards.lock`: `make ci`,
`make race`, `make cover-check`, `make reachability-check`,
`make cite-check`, `make apidiff-check`, `make ahead-check`,
`make surface-check`, `make chiptool-test`, and the new families through
`make chiptool-families GOFABRIC_CHIP_FAMILIES=<list>` with
`-update-chip-cases -update-certifiability-doc` where the Makefile's
`chiptool-families-update` target does it. A ratchet moves in the same
commit as its cause; every new test citation regenerates `make cite-report
&& make reachability` in the same commit. Security paths (`secure/`,
`commissioning/`, ACL, the four hand-written `cluster/core` servers) are
not touched; if a deliverable seems to need it, stop that deliverable and
report. Facade changes (`node/`, `device/`, `contract/`) are judged as API
design: one shape, the `node.Config.TLS` precedent (node/config.go:84-88,
node/tls.go:27-47). Official text in English; Docker is available.

## 1 Owner decisions 2026-11-04 (wizard)

1. **Full scope, own harness image.** Camera, TLS and Joint Fabric
   compositions in the reference daemon; THNETDIR and TBRM on the Thread
   composition; and an own harness image that adds chip's `jf-admin-app`,
   `jf-control-app` and `src/tools/push_av_server` (at 6170af84) on top of
   the pinned matter.js image, so JFADMIN, JFDS and PAVST run. AVANALY,
   ESALM, EPALM, TSTATM have no cluster at the matter.js pin (not in
   `parity/schema.json`): classified with that reason, no server.
2. **Variants like CNETWIFI** (`family.variant`, `daemonVariants`
   familyrun_test.go:109-111, `variantPICS` :123, `testdata/pics-<variant>/`,
   CNETWIFI row families_table_test.go:368): one variant per composition —
   `--camera-sim`, `--tls-sim`, `--jointfabric-sim`, and the Thread
   variant `--otbr <url>` against the virtual OTBR. `--profile` is taken
   (main.go:148, controller profiles); the existing families run unchanged.
3. **A root ThreadNetworkDiagnostics server** (endpoint 0, as chip's
   camera-app.matter:3255 at the pin, FeatureMap per ci-pics F00–F03:
   PacketCounts, ErrorCounts, MLECounts, MACCounts) fed by the border
   router, mounted through the facade (a `node.Config` knob after the TLS
   precedent; ADR 0027 decision 7 amended in a short addendum). THNETDIR
   declared on the 0x0091 border-router endpoint (`otbr.go:82-84` gains
   `threaddir` — `netinfra.NewThreadDirectory`, memory store).
   Revised 2026-11-04: repointed, no root server — DGTHREAD runs on the 0x0091 border-router endpoint, whose ThreadNetworkDiagnostics gains PacketCounts and ErrorCounts; no `node.Config` knob, no root-TND addendum.
4. **TBRM group by dispatch + weekly**, never on pull requests, armed only
   after one green `otbr-virtual.yml` run (`script/otbr-virtual-test.sh`:
   ENVIRONMENT exit 2 vs DEFECT exit 1 — the group's job reuses that
   split so an agent build failure is no defect).

## 2 Deliverables

1. **The counter and its ratchet.** A test in `internal/chiptool` (no
   build tag) that lists the chip case prefixes at the pin (`TC_<P>_*.py`,
   `Test_TC_<P>_*.yaml`, base/util files excluded) with ≥ 3 cases that are
   neither a `chipFamilies` row nor a code in the first column of
   `docs/certifiability.md`'s "Families this module does not run yet"
   table; `BR` and `MCORE` are PICS/controller-role prefixes, excluded by
   name with the reason. Source of the list: the chip checkout at the pin
   (`git ls-tree`), cached as `testdata/chip-prefixes.json` with the
   commit, regenerated by a `-update` flag. Ratchet constant ≤ 8 after
   this phase, documented like the testparity ratchets.
2. **Compositions in `examples/reference-bridge`** (pattern `media.go`,
   fleet wiring fleet.go:998-1012, :1223-1242; flags main.go:116-149):
   - `camera.go` (`--camera-sim`): a Camera endpoint (0x0142,
     `device/camera_gen.go`) with CameraAvStreamManagement (VDO, ADO, SNP;
     simulated per-feature stream ports and SnapshotCapturePort),
     WebRtcTransportProvider + Requestor (simulated `ProviderPort`,
     `PeerConnector`, `StreamSource`), ZoneManagement (TWODCART, `ZonePort`),
     Chime (`ChimePort`), PushAvStreamTransport (`PushTransportPort`,
     `ZoneSource`), CameraAvSettingsUserLevelManagement (AVSUM). The state
     the module does not persist (cluster/camera/doc.go:56-66) is kept by
     the example in `store.Settings` JSON (phase 30's Wi-Fi precedent), so
     a daemon restarted from the commissioned snapshot keeps it.
     App-pipe commands (examples/reference-bridge control.go:143-292):
     `SetHardPrivacyModeOn`, `ZoneTriggered`, `AmbientContextTriggered`
     (if the ACS server is composed), `AvAnalysisSessionEnd` (no-op with a
     log line: no AvAnalysis). Daemon flags mirroring chip camera-app's
     behaviour switches the cases pass (`--camera-deferred-offer`,
     `--camera-test-videosrc`, `--camera-test-audiosrc`): implement each
     as a behaviour of the simulated ports where the case only needs the
     switch's observable effect (read the case and camera-app's handling
     at the pin); a case that needs a real media pipeline is a skip with
     class (c) and the line that shows it.
   - `tls.go` (`--tls-sim`): `node.Config.TLS` with a deterministic
     `CertificatePort` (keys generated once, kept in Settings),
     `CertificateStore`, `EndpointStore`; TLSCERT + TLSCLIENT; the Push
     transport's TLS endpoint reaches it through the node.
   - `jointfabric.go` (`--jointfabric-sim`): a JointFabricAdministrator
     endpoint (0x0130) with `AdministratorPort`, `CommissionerReach`
     (node/commissioner_reach.go:21-37), `DatastoreSync`, `DatastoreStore`
     (memory + Settings). The cases drive chip's `jfa-app`/`jfc-app` as the
     other parties (`--string-arg jfa_server_app:… jfc_server_app:…`): the
     harness starts them from the image (deliverable 4).
   - Thread (`--otbr`): THNETDIR on the border router; the root TND of
     §1.3 when `--otbr` is set.
     Revised 2026-11-04: repointed, no root server (DGTHREAD on the border router, see §1.3).
3. **Harness rows and variants** (`internal/chiptool`): `variantCamera`,
   `variantTLS`, `variantJointFabric`, `variantThread` with
   `daemonVariants`, `testdata/pics-<variant>/` (generated by `devicePICS`
   familyrun_test.go:196 + hand declarations), `endpointDeviceTypes`
   consts (`dtCamera`, `dtChime`, `dtJointFabricAdministrator`,
   `dtThreadBorderRouter`, …), family rows for every family of the title
   with `variant:`, `deviceType`/`endpoints`, `args` (the chip
   `--string-arg`s), `exclude` with class + reason per skipped case,
   `knownProblems`; `clusterFamilies` untouched unless a cluster enters the
   default surface. Workflow `.github/workflows/chiptool.yml:158-171`: new
   groups `app-camera` (WEBRTCP, AVSM, PAVST, PAVSTI, AVSUM, ZONEMGMT,
   CHIME), `app-tls` (TLSCERT, TLSCLIENT), `app-jointfabric` (JFADMIN,
   JFDS) with the app-groups' policy, and `thread` (TBRM, DGTHREAD,
   THNETDIR) on `workflow_dispatch` + weekly only, building the virtual
   OTBR agent first (otbr-virtual.yml's steps; environment failure is no
   defect); `TestWorkflowRunsEveryFamily` learns the groups and the
   dispatch-only rule. The JF and camera groups use the own image
   (deliverable 4) by digest.
4. **The own harness image.** `internal/chiptool/image/Dockerfile`: FROM
   the pinned matter.js image; adds chip's `jf-admin-app`, `jf-control-app`
   built at 6170af84 (read `examples/jf-admin-app`, `examples/jf-control-app`
   and chip's own CI build at the pin for the gn args; the pigweed RPC
   server the JF cases use) and `src/tools/push_av_server` with its
   `requirements.txt`; the descriptor `/lib/test-descriptor.json` extended
   for the JF and PAVST runs the harness needs. Workflow
   `.github/workflows/chip-image.yml` (dispatch) builds and pushes
   `ghcr.io/sukramj/go-fabric-chip:<chip-commit>`; Makefile
   `CHIP_TEST_IMAGE` and the harness pin move to its digest **after the
   owner's first dispatch** (you build and verify the image locally with
   Docker and run the JF/PAVST families against it; the hand-back names
   the owner's step: dispatch the workflow, paste the digest). Disk: chip
   builds are large — build in a Docker volume, prune build containers,
   keep `/tmp` out of it.
5. **Runs and the status page.** Run every new family locally
   (`make chiptool-families GOFABRIC_CHIP_FAMILIES=…`, hours; TBRM against
   `make otbr-virtual-test`'s agent if it builds on this machine, else
   classify the environment honestly), update `testdata/chip-cases.golden.json`
   and `docs/certifiability.md` through the update flags; classify
   AVANALY, ESALM, EPALM, TSTATM ("cluster not in the matter.js snapshot at
   862ad75f"), WEBRTCR/BR as before; correct `docs/certifiability.md:663`
   (chip has `TC_MESS_1_1/3_1/3_2/3_3` at the pin): add a MESS family row
   (Messages is served by `cluster/core`) in `core-rest` and run it. Every
   case neither passed nor classified fails `TestCertifiabilityDocument`.
6. **Docs and records.** CHANGELOG `### Added` (the flags/compositions,
   the root TND, the image, the MESS family) and `### Changed`; the
   reference daemon's docs page and `docs/certifiability.md`'s not-run
   table; ADR 0027 addendum (root TND, THNETDIR on the border router, the
   thread group's policy); `docs/roadmap.md` phase-34 status line with the
   counter's value before/after; by_design rows for anything the
   simulation deliberately does differently from chip's camera-app.

## 3 Report shape

Per deliverable: what landed (files, flags, rows, cases passed / skipped
with class); the counter before/after; guards table (guard, HEAD, exit)
incl. each family run's summary; the image: how built, size, what the
owner must do; differences from this brief; decisions; left out; open
questions; every chip/matter.js line cited, re-read at the pins. Pull
request against main (English, report as body, no labels, auto-merge off),
hand back with SubagentHandback.
