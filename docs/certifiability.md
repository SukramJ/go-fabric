# Certifiability status

Certification is not pursued by this project, and nothing built on it may be
described as certified. Certifiability is a goal: a product built on this
module should be able to pass certification
([ADR 0011](./adr/0011-certifiability-is-a-goal.md)). This page is where that
goal is measured. It is partly generated: the status block below is held to
the family table (`internal/chiptool/families_table_test.go`) and to the last
family run (`internal/chiptool/testdata/chip-cases.golden.json`) by
`TestCertifiabilityDocument`, which runs in every `go test ./...` and fails
when the two disagree or when a case neither passes nor is a classified gap
or not ported yet.

Passing borrowed certification cases here is evidence of certifiability, not a
certification result. Only an Authorized Test Laboratory run against a concrete
product produces one.

## How the status is produced

```sh
make chiptool-families                                  # every family, hours
GOFABRIC_CHIP_FAMILIES=IDM,ACL make chiptool-families   # a selection
```

In CI (`.github/workflows/chiptool.yml`) the nightly run covers every family.
A release pull request (head branch `release/*`) runs that same full set —
every family group, the chip-tool suite, the Go-TH groups (`gocert-cadmin`,
`gocert-dd`, `gocert-rest`), the control leg, the BLE-Wi-Fi leg and the BLE
cases leg — and, beyond it, the nested modules' release shape (`make
nested-release-check`: each nested module built against the go-fabric version
its go.mod requires, its replace dropped; a module out of shape fails a nested
release, `release/nested-*`, and is a warning on a root release; a sibling's
require of the version the nested release pull request itself tags is built
against that sibling's checked copy). Its required
check `chip certification cases against the reference daemon` stays red until
all of them have run and passed, so no release is tagged over a family only
the nightly run would have shown; the virtual
Bluetooth and OTBR legs (`ble-virtual.yml`, `otbr-virtual.yml`) need runner
kernel modules and stay weekly, outside that gate.

- **The device under test** is the reference daemon in
  `examples/reference-bridge`: a root node, an aggregator and one bridged
  endpoint per device surface the module serves (lights, sensors, closures,
  climate, appliances, an air purifier with its filters, a garage door, a
  lock, a smoke/CO alarm, a switch).
- **The harness** is the module's own image (`ghcr.io/thingfabric/go-fabric-chip`,
  `internal/chiptool/image`, built by `chip-image.yml`): matter.js's CHIP image
  (`ghcr.io/matter-js/chip`, the Makefile's `CHIP_BASE_IMAGE`) plus chip's
  jf-admin-app, jf-control-app and Push AV server, pinned by digest in the
  Makefile (`CHIP_TEST_IMAGE`) together with the CHIP commit it was built from
  (`CHIP_TEST_IMAGE_COMMIT`). It carries chip-tool,
  the YAML runner and the Python `TC_*` cases at that commit; the run uses the
  same image matter.js tests itself with.
- **The CSA Test Harness is not used.** The cases are driven straight from
  the `connectedhomeip` tree in that image; the harness
  (`project-chip/certification-tool`) would orchestrate the same YAML and
  Python cases with its own PICS handling, manual prompt steps, a web UI and
  the report format of a submission. It is deliberately no part of the
  guards (owner, 2026-10-21); one spot-check run of its backend against the
  reference daemon, with the report kept under `notes/`, is planned for
  milestone 6 as evidence that the results hold as the harness sees them.
- **Families** run in full, the way matter.js declares its own
  (`support/chip-testing/test/core`, `test/app-*`). A case that is not run is
  a gap of exactly one class, or not ported yet (below):

  | Class | Meaning | What follows |
  | --- | --- | --- |
  | (a) defect | go-fabric misbehaves or lacks mandatory behaviour | fixed per matter.js, or an open finding carrying the TC id |
  | (b) not supported | optional per spec for the device types exposed, and declared so in PICS | the PICS line is the record |
  | (c) harness | the test case or the environment is at fault | the evidence is cited |
  | (d) out of scope | Thread on the device side (no Thread radio) | the scope decision is named (the controller role entered with ADR 0020; BLE commissioning in the controller with ADR 0024; BLE and Wi-Fi commissioning of the node with ADR 0031) |

  A case matter.js passes and this module fails is class (a) by definition.

  A case can also be **not ported yet**, a fifth status beside passed and the
  four classes (owner decision 82): a certification case matter.js automates
  at the snapshot's pin that this module could run and has no port for, or a
  case whose fix is in the tree and whose certification run is pending. The
  module is unmeasured against it; the row says what the port or the run
  needs. `TestCertifiabilityDocument` holds both: the first kind must name a
  case of matter.js's list at the pin, by its TS file, that no Go-TH port
  runs; the second must name the unit test that holds the case's answer —
  the test must exist and name the case — and the family run runs the case.
  A row leaves the status when its port or its run passes; a run that fails
  makes it class (a).
- **Out-of-band controls** are the ones CHIP's CI drives its sample apps
  with: the daemon's app pipe (`--app-pipe`, CHIP's JSON commands), General
  Diagnostics TestEventTrigger (`--enable-key`), the Python cases'
  restart-flag protocol, and the YAML runner's accessory protocol
  (SystemCommands Reboot / FactoryReset), which the suite serves on a
  loopback port the way matter.js does (`internal/chiptool/accessory_test.go`).
  An operator step (a UserPrompt under `PICS_USER_PROMPT=0`) has no
  stand-in; such a case is a class (c) gap that names the step.
- **A second controller**: the matter.js controller leg
  (`TestMatterJSController`) commissions the daemon with matter.js's
  CommissioningController and checks read, write, invoke, reporting, a
  second fabric and subscription resumption across a restart. CI builds
  matter.js at the commit the schema snapshot pins
  (`parity/schema.json` `matter.sourceCommit`) and fails if the leg skips.
- **The Go controller as TH** (`make chiptool-gocert`, part of
  `make chiptool-test`, `GOFABRIC_GOCERT=IDM,SC` narrows, `-CADMIN` leaves a
  family out, an entry that names no registered case fails; CI runs it in
  three groups by family, the `gocert` job): matter.js automates
  its certification cases in `support/chip-testing/test/cert/TC-*.test.ts`, and
  in most of them its own controller is the DUT and a device app the TH. Those
  are the controller halves of the CHIP test plans, which CHIP's harness runs
  as manual cases. Every case of that directory at the snapshot's pin is
  accounted for in the status block — run by the leg, or classified with what
  a port needs ("matter.js certification cases not run"), never as a generic
  manual row — and `TestCertifiabilityDocument` fails on one that is neither,
  so a pin lift that adds cases fails until each is run or classified. Built
  with the `chiptool` tag, the same test reads the leg's registry of ports:
  a case counts as run only while a registered port runs it, and the golden
  file's Go-TH entries are exactly the registered ports, so a port deleted
  or renamed fails although its golden entry stays. The
  case list is `internal/chiptool/testdata/matterjs-cert-cases.json`, generated
  from the pinned commit. The Go-TH leg reads the TS files
  mirrored: the reference daemon is the DUT, the module's own controller
  performs the TS file's controller actions, and the TS file's TH-side
  expectations are what the daemon has to satisfy. Each case cites its TS
  file and the line of every step, as they stand at the snapshot's pin; a step's PICS is the TS's
  mirrored onto the server side (`ACT.C.C00.Tx` → `ACT.S.C00.Rsp`) and evaluated against the
  daemon's own PICS, and a step whose PICS is false is recorded skipped with
  the expression. One port holds a precondition looser than its TS file:
  TC-WNCV-7.1's step 0 requires position-aware Lift *and* position-aware
  Tilt (`TC-WNCV-7.1.test.ts:43-47`, stricter than the plan's "and/or" by its
  own comment, `:41-42`), and the daemon's window covering serves Lift only;
  the port reads the plan's "and/or" as: it requires each axis whose GoTo…Percentage command
  the daemon declares and one axis at least, and records the tilt step
  skipped by its PICS (`WNCV.S.C08.Rsp`), so its row below measures the lift
  axis alone. The leg needs the daemon and the host's multicast, not the
  harness image; its cases appear below as passed, with their step counts and
  the TS file they follow. The BDX receiver, SU requestor
  and BIND cases run the device as DUT in matter.js too: their PICS are
  evaluated as stated, the Go TH serves the OTA image from its own identity and
  scripts its provider's answers as matter.js's cert harness does, and the
  BIND cases run the daemon with `--light-switch`. A step matter.js marks
  long-running (minutes of real time each) runs with
  `GOFABRIC_GOCERT_LONG=1`; the counts below are the default run's.
- **The Go controller as DUT** (the same leg, roadmap phase 29b): the TS files
  in matter.js's own direction. The module's controller is the DUT and the
  reference daemon the TH device the TS file names, with the flags and app-pipe
  commands matter.js's TH apps have. Such a case's PICS are the controller's
  own declarations (matter.js's for its controller where it states them, the
  module's own where matter.js leaves the code to CHIP's PICS file), evaluated
  as stated. matter.js proves its DUT controller by the TH device's log; here
  each step names its evidence instead: the DUT controller's return value or
  error, a state read on the daemon, or a line of the daemon's log.
  TC-DD-3.12 and TC-DD-3.13 are ports of the pin's version: both start the
  daemon with `--custom-flow 1`, hold it to not having advertised in
  commissioning mode on every leg that runs, and on the IP leg open its window by the
  app pipe's `UserIntentCommissioningStart` before the discriminator check
  and the commissioning. A case
  passed in this role counts as passed in its family — the page states the
  module in both roles since ADR 0020 — and is listed under "Controller role"
  below; a case no family of the harness image lists is listed there and
  counted in none. The bridge client case TC-BR-4 runs the daemon with
  `--bridge-sim`, the bridge matter.js's bridge test instance simulates after
  chip's bridge-app, whose lights, sensors and composed device the case finds
  by their names (the daemon numbers its endpoints itself); each "the DUT
  took the change in" check reads the controller's own image of the bridge,
  kept by its subscription alone, before any read.
- **What a passing case executed** is pinned per case (steps run and steps
  skipped), so a PICS change that quietly turns a case into a no-op fails it.
- **A case that passes with a recorded gap** is one whose CHIP spec checker
  reports exactly the recorded problems and nothing else; it fails as soon as
  another problem appears or the recorded one disappears.
- **A case that fails as recorded** ("runs, and fails as recorded") is a
  classified gap whose case still runs: its own expectation departs from
  behaviour the module keeps on purpose, recorded in
  [`by_design.md`](../notes/parity/by_design.md). The run requires the
  recorded error after the recorded number of steps; it fails when the case
  passes or fails anywhere else. TC-PAVST-2.8 is the one case: its step 4
  sends a Large Message command over UDP and expects NOT_FOUND, where the
  module answers INVALID_TRANSPORT_TYPE as matter.js does
  (`BD-chip-LargeMessageGateEnforced`).

A static complement runs in CI on every pull request (and in `go test ./...`
wherever a connectedhomeip checkout is present): the
[CHIP data model cross-check](./chip-datamodel-crosscheck.md) compares the
whole schema snapshot — every cluster and device type, not only those the
reference daemon mounts — with connectedhomeip's `data_model/<version>` XML,
the model the Python cases judge a device by, and classifies every
difference; device types are compared down to their feature, attribute,
command and event requirements and their conditions, the overrides
TC_DeviceConformance enforces. What the snapshot states, the module holds
every assembled topology to: the bridge runs matter.js's device type
validation on each assembly ([ADR 0016](./adr/0016-device-type-validation.md)),
and a host's tests can assert it with
`endpointtest.AssertDeviceTypeConformance`.

## PICS

The PICS a case runs with is generated from the device, not written for the
cases:

1. `internal/chiptool/testdata/gen_pics.py` reads the commissioned daemon with
   one wildcard read and writes a slice per endpoint
   (`internal/chiptool/testdata/pics/ep<N>.txt`) with CHIP's own derivation
   helpers (`matter/testing/pics.py`): every server-side cluster, attribute,
   command, feature and event code of every spec cluster, 0 or 1, plus the
   derivable MCORE codes (commissionee, bridge, OTA roles, multi-endpoint
   groups). Mandatory events follow from spec conformance and from the
   device types the endpoint lists (OperationCompletion on a LaundryWasher).
2. `internal/chiptool/testdata/reference-bridge.pics` holds the declarations a
   device cannot report — transport (Ethernet only), onboarding (QR and
   11-digit manual code, no NFC, no UI), DNS-SD keys, the attestation material
   of the test build — each with the code that makes it true. It may not set
   a code the slices decide; the suite fails if it does.
3. CHIP's `ci-pics-values` supplies the rest (client-side codes the DUT never
   acts on, runner controls).

The family run regenerates the slices and fails when the device no longer
matches the committed ones. TC-IDM-10.4 (CHIP's PICS checker) runs once per
endpoint against that endpoint's slice with `PICS_SDK_CI_ONLY=0`, the form a
certification PICS has. A product built on this module produces its own PICS
the same way: run `gen_pics.py` against the product and add its own
non-derivable declarations.

## Status

<!-- BEGIN GENERATED by internal/chiptool TestCertifiabilityDocument; do not edit -->

Harness image: `ghcr.io/thingfabric/go-fabric-chip@sha256:a22ed731e3fa28eb380ca104289edfec7ec16d29fc61251b4dbd60bd48035392`.

| Family | Cases | Passed | of which with a recorded gap | not ported yet | (a) defect | (b) not supported | (c) harness | (d) out of scope |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ACE | 10 | 10 | 0 | 0 | 0 | 0 | 0 | 0 |
| ACL | 11 | 10 | 0 | 0 | 0 | 1 | 0 | 0 |
| BINFO | 4 | 2 | 0 | 0 | 0 | 2 | 0 | 0 |
| BRBINFO | 5 | 1 | 0 | 0 | 0 | 3 | 1 | 0 |
| CADMIN | 21 | 18 | 0 | 0 | 0 | 0 | 3 | 0 |
| CGEN | 10 | 3 | 0 | 0 | 0 | 7 | 0 | 0 |
| CGENTC | 10 | 10 | 0 | 0 | 0 | 0 | 0 | 0 |
| CNET | 23 | 3 | 0 | 0 | 0 | 17 | 3 | 0 |
| CNETWIFI | 23 | 6 | 0 | 0 | 0 | 11 | 6 | 0 |
| DA | 13 | 4 | 0 | 0 | 0 | 5 | 4 | 0 |
| DD | 30 | 16 | 0 | 0 | 0 | 0 | 14 | 0 |
| DESC | 3 | 2 | 0 | 0 | 0 | 0 | 1 | 0 |
| DGGEN | 7 | 6 | 0 | 0 | 0 | 0 | 1 | 0 |
| DT | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| G | 5 | 3 | 0 | 0 | 0 | 0 | 2 | 0 |
| GC | 8 | 8 | 0 | 0 | 0 | 0 | 0 | 0 |
| GRPKEY | 3 | 2 | 0 | 0 | 0 | 0 | 1 | 0 |
| IDM | 33 | 28 | 0 | 0 | 0 | 0 | 5 | 0 |
| OPCREDS | 9 | 6 | 0 | 0 | 0 | 1 | 2 | 0 |
| RR | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| SC | 34 | 20 | 0 | 0 | 0 | 3 | 11 | 0 |
| SM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| TIMESYNC | 14 | 3 | 0 | 0 | 0 | 11 | 0 | 0 |
| DLOG | 1 | 0 | 0 | 0 | 0 | 0 | 1 | 0 |
| ICDM | 7 | 0 | 0 | 0 | 0 | 7 | 0 | 0 |
| SU | 13 | 13 | 0 | 0 | 0 | 0 | 0 | 0 |
| BDX | 11 | 4 | 0 | 0 | 0 | 0 | 7 | 0 |
| BIND | 3 | 2 | 0 | 0 | 0 | 0 | 1 | 0 |
| FLABEL | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| ULABEL | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| LCFG | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| LTIME | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| LUNIT | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| DGSW | 4 | 1 | 0 | 0 | 0 | 2 | 1 | 0 |
| DGETH | 3 | 2 | 0 | 0 | 0 | 1 | 0 | 0 |
| DGWIFI | 4 | 2 | 0 | 0 | 0 | 1 | 1 | 0 |
| ACT | 3 | 1 | 0 | 0 | 0 | 0 | 2 | 0 |
| PSCFG | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| ECOINFO | 2 | 0 | 0 | 0 | 0 | 0 | 2 | 0 |
| ACFREMON | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| BOOL | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| BOOLCFG | 9 | 9 | 0 | 0 | 0 | 0 | 0 | 0 |
| CDOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| CMOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| AIRQUAL | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| PMICONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| PMKCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| FLDCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| NDOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| OZCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| PMHCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| RNCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| TVOCCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| SOIL | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| CLCTRL | 12 | 7 | 0 | 0 | 0 | 5 | 0 | 0 |
| CLDIM | 11 | 6 | 0 | 0 | 0 | 5 | 0 | 0 |
| CC | 31 | 21 | 0 | 0 | 0 | 6 | 4 | 0 |
| DRLK | 14 | 11 | 0 | 0 | 0 | 2 | 1 | 0 |
| EPREF | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| EWATERHTR | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| MTRID | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| SETRF | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| SEPR | 3 | 2 | 0 | 0 | 0 | 1 | 0 | 0 |
| COMMTR | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| EEM | 5 | 2 | 0 | 0 | 0 | 3 | 0 | 0 |
| EPM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| PWRTL | 2 | 1 | 0 | 0 | 0 | 1 | 0 | 0 |
| EGC | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| DEM | 11 | 9 | 0 | 0 | 0 | 2 | 0 | 0 |
| DEMM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| EEVSE | 10 | 10 | 0 | 0 | 0 | 0 | 0 | 0 |
| EEVSEM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| FAN | 12 | 7 | 0 | 0 | 0 | 5 | 0 | 0 |
| HEPAFREMON | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| FLW | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| I | 5 | 4 | 0 | 0 | 0 | 0 | 1 | 0 |
| LVL | 10 | 9 | 0 | 0 | 0 | 1 | 0 | 0 |
| LWM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| WASHERCTRL | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| DRYERCTRL | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| MOD | 8 | 3 | 0 | 0 | 0 | 5 | 0 | 0 |
| OCC | 5 | 5 | 0 | 0 | 0 | 0 | 0 | 0 |
| OO | 8 | 4 | 0 | 0 | 0 | 2 | 2 | 0 |
| OPSTATE | 6 | 6 | 0 | 0 | 0 | 0 | 0 | 0 |
| PCC | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| PS | 4 | 2 | 0 | 0 | 0 | 1 | 1 | 0 |
| RH | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| RVCCLEANM | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| RVCOPSTATE | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| TCCM | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| TCTL | 5 | 3 | 0 | 0 | 0 | 2 | 0 | 0 |
| DISHALM | 7 | 1 | 0 | 0 | 0 | 0 | 6 | 0 |
| REFALM | 3 | 2 | 0 | 0 | 0 | 1 | 0 | 0 |
| OVENOPSTATE | 5 | 4 | 0 | 0 | 0 | 1 | 0 | 0 |
| OTCCM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| MWOCTRL | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| MWOM | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| RVCRUNM | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| S | 7 | 6 | 0 | 0 | 0 | 1 | 0 | 0 |
| SEAR | 5 | 5 | 0 | 0 | 0 | 0 | 0 | 0 |
| SMOKECO | 7 | 7 | 0 | 0 | 0 | 0 | 0 | 0 |
| SWTCH | 7 | 5 | 0 | 0 | 0 | 2 | 0 | 0 |
| TMP | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| TSTAT | 6 | 5 | 0 | 0 | 0 | 1 | 0 | 0 |
| TSUIC | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| VALCC | 10 | 10 | 0 | 0 | 0 | 0 | 0 | 0 |
| WHM | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| WNCV | 17 | 15 | 0 | 0 | 0 | 2 | 0 | 0 |
| ALOGIN | 3 | 2 | 0 | 0 | 0 | 1 | 0 | 0 |
| APBSC | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| APPLAUNCHER | 5 | 5 | 0 | 0 | 0 | 0 | 0 | 0 |
| APPOBSERVER | 2 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| AUDIOOUTPUT | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| CHANNEL | 6 | 6 | 0 | 0 | 0 | 0 | 0 | 0 |
| CONTENTLAUNCHER | 9 | 6 | 0 | 0 | 0 | 3 | 0 | 0 |
| KEYPADINPUT | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| LOWPOWER | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| MEDIAINPUT | 8 | 8 | 0 | 0 | 0 | 0 | 0 | 0 |
| MEDIAPLAYBACK | 11 | 8 | 0 | 0 | 0 | 2 | 1 | 0 |
| TGTNAV | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| WAKEONLAN | 1 | 1 | 0 | 0 | 0 | 0 | 0 | 0 |
| MESS | 4 | 0 | 0 | 0 | 0 | 4 | 0 | 0 |
| TLSCERT | 13 | 11 | 0 | 0 | 2 | 0 | 0 | 0 |
| TLSCLIENT | 6 | 6 | 0 | 0 | 0 | 0 | 0 | 0 |
| WEBRTCP | 19 | 8 | 0 | 0 | 0 | 8 | 3 | 0 |
| WebRTCP | 13 | 12 | 0 | 0 | 0 | 1 | 0 | 0 |
| AVSM | 21 | 19 | 0 | 0 | 0 | 0 | 2 | 0 |
| AVSUM | 9 | 9 | 0 | 0 | 0 | 0 | 0 | 0 |
| ZONEMGMT | 4 | 4 | 0 | 0 | 0 | 0 | 0 | 0 |
| PAVST | 15 | 13 | 0 | 0 | 0 | 1 | 1 | 0 |
| PAVSTI | 2 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| CHIME | 6 | 6 | 0 | 0 | 0 | 0 | 0 | 0 |
| JFADMIN | 5 | 1 | 0 | 0 | 0 | 0 | 4 | 0 |
| JFDS | 5 | 0 | 0 | 0 | 0 | 0 | 5 | 0 |
| TBRM | 5 | 3 | 0 | 0 | 0 | 1 | 1 | 0 |
| DGTHREAD | 6 | 4 | 0 | 0 | 0 | 1 | 1 | 0 |
| THNETDIR | 3 | 3 | 0 | 0 | 0 | 0 | 0 | 0 |
| **all** | **855** | **602** | **0** | **0** | **2** | **143** | **108** | **0** |

### Cases not run

| Case | Class | Reason |
| --- | --- | --- |
| ACL/2.11 | (b) not supported | not applicable: the case's PICS `ACL.S.F01` is false for the reference DUT |
| ACT/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ACT/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ALOGIN/12.3 | (b) not supported | not applicable: the case's PICS `ALOGIN.S & ALOGIN.S.F00` is false for the reference DUT |
| APPOBSERVER/13.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| AVSM/2.16 | (c) harness | TC-AVSM-2.16 step 4 waits for the DUT's Answer to its ProvideOffer (TC_AVSM_2_16.py:249 at 6170af84) under the PICS AVSM.S alone (:61-62); the --camera-sim camera has no media stack to answer from (examples/reference-bridge/camera.go ProvideOffer; chip's camera-app: webrtc-provider-manager.cpp:531-532, :1124-1150). The body times out after 90 s |
| AVSM/2.17 | (c) harness | TC-AVSM-2.17 step 7 waits for the DUT's Answer to its ProvideOffer and step 12 for its End (TC_AVSM_2_17.py:289, :323-325 at 6170af84) under the PICS AVSM.S alone (:60-61); the --camera-sim camera has no media stack to answer from (see 2.16) |
| BDX/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BIND/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BINFO/3.1 | (b) not supported | not applicable: the case's PICS `BINFO.S.A0014` is false for the reference DUT |
| BINFO/3.2 | (b) not supported | not applicable: the case's PICS `BINFO.S & BINFO.S.M.DeviceConfigurationChange` is false for the reference DUT |
| BRBINFO/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BRBINFO/3.1 | (b) not supported | not applicable: the case's PICS `BRBINFO.S.A0014` is false for the reference DUT |
| BRBINFO/3.2 | (b) not supported | not applicable: the case's PICS `BRBINFO.S & BRBINFO.S.A.A0018 & BRBINFO.S.M.DeviceConfigurationChange` is false for the reference DUT |
| BRBINFO/4.1 | (b) not supported | TC-BRBINFO-4.1 exercises KeepActive of a bridged ICD (BridgedICDSupport, PICS BRBINFO.S.F00=0) against a LIT ICD test app the case starts itself (${LIT_ICD_APP}); the case carries no PICS gate, so it runs regardless (matter.js test/core/BRBINFO.test.ts excludes it for the same reason) |
| CADMIN/1.12 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CC/3.4.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CC/4.5.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CC/5.4.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CC/6.4.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CC/6.5 | (c) harness | the case reboots the DUT and expects the light's StartUpOnOff / StartUpColorTemperatureMireds applied; matter.js applies the start-up attributes only on an endpoint no Aggregator owns (OnOffServer.ts and ColorControlServer.ts initialize: !endpoint.ownerOfType(AggregatorEndpoint)) — a bridge restart is not the bridged device's power cycle — and every light here is bridged |
| CC/7.5.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CC/9.1 | (c) harness | TC-CC-9.1 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts) |
| CC/9.2 | (c) harness | TC-CC-9.2 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts) |
| CC/9.3 | (c) harness | TC-CC-9.3 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts) |
| CC/9.4.Simulated | (b) not supported | not applicable: the case's PICS `CC.C` is false for the reference DUT |
| CGEN/2.10 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.11 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.5 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.6 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.7 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.8 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CGEN/2.9 | (b) not supported | not applicable: the case's PICS `CGEN.S & CGEN.S.F00` is false for the reference DUT |
| CLCTRL/4.4 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.A0000` is false for the reference DUT |
| CLCTRL/7.1 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F00 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.2 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F01 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.3 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.4 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.A0000 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLDIM/5.1 | (b) not supported | not applicable: the case's PICS `CLDIM.S & CLDIM.S.F00 & CLDIM.S.C02.Rsp` is false for the reference DUT |
| CLDIM/5.2 | (b) not supported | not applicable: the case's PICS `CLDIM.S & CLDIM.S.F01 & CLDIM.S.C02.Rsp` is false for the reference DUT |
| CLDIM/5.3 | (b) not supported | not applicable: the case's PICS `CLDIM.S & CLDIM.S.C02.Rsp` is false for the reference DUT |
| CLDIM/6.1 | (b) not supported | not applicable: the case's PICS `CLDIM.S & CLDIM.S.F00 & CLDIM.S.C03.Rsp` is false for the reference DUT |
| CLDIM/6.2 | (b) not supported | not applicable: the case's PICS `CLDIM.S & CLDIM.S.F00 & CLDIM.S.C03.Rsp` is false for the reference DUT |
| CNET/4.1 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.10 | (b) not supported | not applicable: the case's PICS `CNET.S & CNET.S.F01` is false for the reference DUT |
| CNET/4.11 | (b) not supported | TC-CNET-4.11 verifies Wi-Fi ConnectNetwork; the Ethernet composition has no Wi-Fi feature (CNET.S.F00=0) and the case has no PICS gate; the Wi-Fi composition's row is CNETWIFI/4.11, (c) harness (two real access points) |
| CNET/4.12 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.15 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.16 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.2 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.21 | (c) harness | matter.js automates it at the snapshot's pin (TC-CNET-4.21.test.ts:9); the Go-TH leg does not run it, so the module is unmeasured here — needs a daemon composition with Thread NetworkCommissioning (`core.ThreadNetworkCommissioning`, ADR 0027), which the reference daemon does not mount |
| CNET/4.22 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.23 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.25 | (b) not supported | TC-CNET-4.25 verifies Wi-Fi per-device credentials; the Ethernet composition has no Wi-Fi feature (CNET.S.F00=0) and the case skips itself; the Wi-Fi composition runs it as CNETWIFI/4.25, where it skips itself (no per-device credentials) (runs, and skips itself) |
| CNET/4.26 | (b) not supported | TC-CNET-4.26 verifies Wi-Fi QueryIdentity (per-device credentials); the Ethernet composition has no Wi-Fi feature (CNET.S.F00=0) and the case skips itself; the Wi-Fi composition runs it as CNETWIFI/4.26, where it skips itself (no per-device credentials) (runs, and skips itself) |
| CNET/4.27 | (b) not supported | TC-CNET-4.27 verifies Wi-Fi network client identities; the Ethernet composition has no Wi-Fi feature (CNET.S.F00=0) and the case skips itself; the Wi-Fi composition runs it as CNETWIFI/4.27, where it skips itself (no per-device credentials) (runs, and skips itself) |
| CNET/4.29 | (b) not supported | TC-CNET-4.29 verifies Wi-Fi ConnectNetwork with per-device credentials; the Ethernet composition has no Wi-Fi feature (CNET.S.F00=0) and the case skips itself once given the endpoint its matcher needs; the Wi-Fi composition runs it as CNETWIFI/4.29, where it skips itself (no per-device credentials) (runs, and skips itself) |
| CNET/4.4 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.5 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.6 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.9 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNETWIFI/4.10 | (b) not supported | not applicable: the case's PICS `CNET.S & CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.11 | (c) harness | TC-CNET-4.11 moves the DUT between two real access points and switches the TH host's own Wi-Fi with them (PIXIT.CNET.WIFI_2ND_ACCESSPOINT_SSID/_CREDENTIALS, TC_CNET_4_11.py:1107-1112, and its host connect steps); the simulated driver has no second network and the harness host no Wi-Fi to switch |
| CNETWIFI/4.12 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNETWIFI/4.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNETWIFI/4.16 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.2 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.20 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNETWIFI/4.21 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNETWIFI/4.22 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.23 | (c) harness | TC-CNET-4.23 steps 0-15 pass on the simulated driver (a wrong SSID is NetworkNotFound, a wrong passphrase AuthFailure, examples/reference-bridge/wifisim.go); step 16 calls the TH's CloseBLEConnection (TC_CNET_4_23.py:498) before its CASE CommissioningComplete, which the harness image's controller, built without BLE, refuses (CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE without CONFIG_NETWORK_LAYER_BLE, src/controller/python/ChipDeviceController-ScriptBinding.cpp:1520-1528): the case assumes a BLE-commissioned DUT. Commissioned over BLE onto Wi-Fi (make chiptool-ble-cases, the image's BLE controller, examples/ble-wifi-light) it stops at step 3 instead: chip's wpa_supplicant mock associates a wrong SSID (matter/testing/linux/wifi.py:415-440), and all-clusters-app-ble fails there too |
| CNETWIFI/4.25 | (b) not supported | TC-CNET-4.25 verifies Wi-Fi per-device credentials (NetworkCommissioning PDC); the module serves no PDC, and the case skips itself on an endpoint without it (runs, and skips itself) |
| CNETWIFI/4.26 | (b) not supported | TC-CNET-4.26 verifies QueryIdentity (per-device credentials); no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.27 | (b) not supported | TC-CNET-4.27 verifies Wi-Fi network client identities (per-device credentials); no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.29 | (b) not supported | TC-CNET-4.29 verifies ConnectNetwork with per-device credentials; no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.3 | (b) not supported | not applicable: the case's PICS `CNET.S.F02` is false for the reference DUT |
| CNETWIFI/4.6 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CONTENTLAUNCHER/10.10 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.A0003` is false for the reference DUT |
| CONTENTLAUNCHER/10.8 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.F05` is false for the reference DUT |
| CONTENTLAUNCHER/10.9 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.F06 & CONTENTLAUNCHER.S.C01.Rsp` is false for the reference DUT |
| DA/1.10 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| DA/1.11 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| DA/1.12 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C00.Rsp & OPCREDS.S.C01.Tx & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| DA/1.13 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| DA/1.3 | (c) harness | matter.js automates it at the snapshot's pin (TC-DA-1.3.test.ts:101); the Go-TH leg does not run it, so the module is unmeasured here — needs, with the Go controller as DUT commissioning the daemon twice, each AttestationNonce observable: matter.js reads it from its TH's log; the daemon logs none, and the controller's nonce reaches only controller/attestation's validation input (validate.go:40-41) |
| DA/1.4 | (c) harness | matter.js automates it at the snapshot's pin (TC-DA-1.4.test.ts:458); the Go-TH leg does not run it, so the module is unmeasured here — needs, with the Go controller as DUT, a daemon started from each of chip's credentials/development/commissioner_dut vectors with the vector's product id (`--dac_provider`, `--product-id`, :279): the daemon reads one attestation chain from files (`--attestation-dir` in `examples/reference-bridge/main.go`) and serves a fixed product id (`testProductID`) |
| DA/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DA/1.8 | (c) harness | matter.js automates it at the snapshot's pin (TC-DA-1.8.test.ts:200); the Go-TH leg does not run it, so the module is unmeasured here — needs, with the Go controller as DUT, a daemon started from each of chip's credentials/development/commissioner_dut vectors with the vector's product id (`--dac_provider`, `--product-id`, :105): the daemon reads one attestation chain from files (`--attestation-dir` in `examples/reference-bridge/main.go`) and serves a fixed product id (`testProductID`) |
| DA/1.9 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT; controller role: matter.js drives chip's python script (prompt-driven-python-test.ts:41), the DUT answering its prompts — not ported (TC-DA-1.9.test.ts:32 TC_DA_1_9.py) |
| DD/1.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.11 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.19 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.22 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.8 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.9 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DEM/2.11 | (b) not supported | not applicable: the case's PICS `DEM.S.F07` is false for the reference DUT |
| DEM/2.9 | (b) not supported | not applicable: the case's PICS `DEM.S.F01 & DEM.S.F02` is false for the reference DUT |
| DESC/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGETH/3.2.Simulated | (b) not supported | not applicable: the case's PICS `DGETH.C` is false for the reference DUT |
| DGGEN/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGSW/2.2 | (b) not supported | not applicable: the case's PICS `DGSW.S.E00` is false for the reference DUT |
| DGSW/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGSW/3.2.Simulated | (b) not supported | not applicable: the case's PICS `DGSW.C` is false for the reference DUT |
| DGTHREAD/2.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGTHREAD/3.4.Simulated | (b) not supported | not applicable: the case's PICS `DGTHREAD.C` is false for the reference DUT |
| DGWIFI/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGWIFI/3.2.Simulated | (b) not supported | not applicable: the case's PICS `DGWIFI.C` is false for the reference DUT |
| DISHALM/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DISHALM/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DISHALM/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DISHALM/3.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DISHALM/3.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DISHALM/3.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DLOG/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DRLK/2.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DRLK/2.11 | (b) not supported | not applicable: the case's PICS `DRLK.S & DRLK.S.F00 & DRLK.S.F01 & DRLK.S.F02` is false for the reference DUT |
| DRLK/2.13 | (b) not supported | not applicable: the case's PICS `DRLK.S.F0d` is false for the reference DUT |
| ECOINFO/2.1 | (c) harness | TC_ECOINFO_2_1.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT |
| ECOINFO/2.2 | (c) harness | TC_ECOINFO_2_2.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT |
| EEM/2.3 | (b) not supported | not applicable: the case's PICS `EEM.S & EEM.S.F02 & EEM.S.F01` is false for the reference DUT |
| EEM/2.4 | (b) not supported | not applicable: the case's PICS `EEM.S & EEM.S.F03 & EEM.S.F00` is false for the reference DUT |
| EEM/2.5 | (b) not supported | not applicable: the case's PICS `EEM.S & EEM.S.F03 & EEM.S.F01` is false for the reference DUT |
| FAN/2.3 | (b) not supported | not applicable: the case's PICS `FAN.S.F02` is false for the reference DUT |
| FAN/2.4 | (b) not supported | not applicable: the case's PICS `FAN.S.F03` is false for the reference DUT |
| FAN/2.5 | (b) not supported | not applicable: the case's PICS `FAN.S & FAN.S.F05` is false for the reference DUT |
| FAN/3.6 | (b) not supported | not applicable: the case's PICS `FAN.S & FAN.S.F05` is false for the reference DUT |
| FAN/4.1 | (b) not supported | not applicable: the case's PICS `FAN.S & OO.S` is false for the reference DUT |
| FLW/2.2 | (c) harness | TC-FLW-2.2 has an operator change the measured value between two reads (a UserPrompt under FLW.M.FlowChange); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| G/2.2 | (c) harness | TC-G-2.2 step 7a (the Groups revision 4 path) writes MaxGroupsPerFabric+1 GroupKeyMap entries and expects Success; chip's own GroupDataProviderImpl::SetGroupKeyAt refuses the entry beyond MaxGroupsPerFabric at the image commit (src/credentials/GroupDataProviderImpl.cpp:1492, CHIP_ERROR_INVALID_LIST_LENGTH), as does matter.js GroupKeyManagementServer #validateGroupKeyMap (ResourceExhausted) and this module |
| G/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| GRPKEY/5.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| I/2.4 | (c) harness | TC-I-2.4 checks the Q-quality reporting of IdentifyTime added in Matter 1.4.2 against an expectation chip has not merged yet (connectedhomeip#42128); matter.js excludes it until then (test/app-slow/I.test.ts) |
| ICDM/2.1 | (b) not supported | not applicable: the case's PICS `ICDM.S` is false for the reference DUT |
| ICDM/3.1 | (b) not supported | not applicable: the case's PICS `ICDM.S` is false for the reference DUT |
| ICDM/3.2 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/3.3 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/3.4 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/4.1 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F02` is false for the reference DUT |
| ICDM/5.1 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F02` is false for the reference DUT |
| IDM/4.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/7.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/8.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| JFADMIN/1.1 | (c) harness | TC-JFADMIN-1.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_1_1.py:131-162; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFADMIN/1.4 | (c) harness | TC-JFADMIN-1.4 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_1_4.py:194-212; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFADMIN/2.1 | (c) harness | TC-JFADMIN-2.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_2_1.py:155-170; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFADMIN/2.2 | (c) harness | TC-JFADMIN-2.2 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_2_2.py:259-270; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFDS/2.1 | (c) harness | TC-JFDS-2.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_1.py:95-130; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFDS/2.2 | (c) harness | TC-JFDS-2.2 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_2.py:99-120; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFDS/2.3 | (c) harness | TC-JFDS-2.3 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_3.py:99-120; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFDS/2.4 | (c) harness | TC-JFDS-2.4 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_4.py:108-130; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| JFDS/2.5 | (c) harness | TC-JFDS-2.5 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_5.py:124-162; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter |
| LVL/7.1 | (b) not supported | not applicable: the case's PICS `LVL.S & LVL.S.F02` is false for the reference DUT |
| MEDIAPLAYBACK/6.11 | (b) not supported | not applicable: the case's PICS `MEDIAPLAYBACK.S & MEDIAPLAYBACK.S.A000b` is false for the reference DUT |
| MEDIAPLAYBACK/6.12 | (b) not supported | not applicable: the case's PICS `MEDIAPLAYBACK.S & MEDIAPLAYBACK.S.A000c` is false for the reference DUT |
| MEDIAPLAYBACK/6.8 | (c) harness | TC-MEDIAPLAYBACK-6.8 step 1 (and the reads after DeactivateTextTrack) expects ActiveTextTrack to read a TrackStruct with an empty ID and a null TrackAttributes (Test_TC_MEDIAPLAYBACK_6_8.yaml:26-31), the tv-app's empty track under chip's revision 3; the model at the pin makes TrackAttributes mandatory and not nullable (matter.js media-playback.element.ts:206-208) and ActiveTextTrack itself nullable (:71-74), so the daemon reads null while no text track is active |
| MESS/1.1 | (b) not supported | TC-MESS-1.1 runs only on an endpoint with SpokenMessages or AudioMessages (TC_MESS_1_1.py:76, TC_MESSTestBase.py:42-50); the player's Messages, like the tv-app's (featureMap 0, tv-app.matter:4419), has neither, and the case skips itself (runs, and skips itself) |
| MESS/3.1 | (b) not supported | not applicable: the case's PICS `MESS.S & MESS.S.F04` is false for the reference DUT |
| MESS/3.2 | (b) not supported | not applicable: the case's PICS `MESS.S & MESS.S.F05` is false for the reference DUT |
| MESS/3.3 | (b) not supported | not applicable: the case's PICS `MESS.S & MESS.S.F05 & MESS.S.E03` is false for the reference DUT |
| MOD/2.3 | (b) not supported | not applicable: the case's PICS `MOD.S & S.S` is false for the reference DUT |
| MOD/3.1 | (b) not supported | not applicable: the case's PICS `MOD.S.A0005` is false for the reference DUT |
| MOD/3.2 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004` is false for the reference DUT |
| MOD/3.3 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004` is false for the reference DUT |
| MOD/3.4 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004 & MOD.S.A0005 & OO.S.A4003` is false for the reference DUT |
| OO/2.4 | (c) harness | the case reboots the DUT and expects the light's StartUpOnOff / StartUpColorTemperatureMireds applied; matter.js applies the start-up attributes only on an endpoint no Aggregator owns (OnOffServer.ts and ColorControlServer.ts initialize: !endpoint.ownerOfType(AggregatorEndpoint)) — a bridge restart is not the bridged device's power cycle — and every light here is bridged |
| OO/2.6 | (b) not supported | not applicable: the case's PICS `OO.S & OO.S.F02` is false for the reference DUT |
| OO/2.8 | (c) harness | TC-OO-2.8 expects OnTime and OffWaitTime reported only on a change larger than 10 or to 0 — chip's OnOffLightingCluster.cpp SetOnTime/SetOffWaitTime (kValueDeltaReportTrigger), an SDK choice: Matter 1.6.1 gives neither attribute the Q quality (matter.js on-off.element.ts), and matter.js OnOffServer reports every countdown tick, as the daemon now does |
| OO/3.2.Simulated | (b) not supported | not applicable: the case's PICS `OO.C` is false for the reference DUT |
| OPCREDS/3.3 | (c) harness | matter.js automates it at the snapshot's pin (TC-OPCREDS-3.3.test.ts:455); the Go-TH leg does not run it, so the module is unmeasured here — needs, with the Go controller as DUT, the CSRResponse and AddNOC fields observable on the TH (steps 5b and 7b, :510, :543): matter.js reads them from its TH's log; the daemon logs the command names only (`matter.opcreds.cmd` in `cluster/core/operational_credentials.go`). The controller's UpdateNOC for a node it commissioned (step 11, :703-758) exists: `client.Registry.UpdateOperationalCertificate` over `controller/commissioner/noc_update.go` |
| OPCREDS/3.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| OPCREDS/3.9 | (b) not supported | not applicable: the case's PICS `OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| OVENOPSTATE/2.6 | (b) not supported | not applicable: the case's PICS `OVENOPSTATE.S & OVENOPSTATE.S.A0002` is false for the reference DUT |
| PAVST/2.15 | (b) not supported | TC-PAVST-2.15 drives UpdateMotionZoneOptions (command 0x08), which the matter.js snapshot does not carry (push-av-stream-transport.element.ts at b25ed960 lists commands 0x00-0x07); the module's server answers UNSUPPORTED_COMMAND |
| PAVST/2.8 | (c) harness | TC-PAVST-2.8 step 4 has TH2, a second controller on a fresh fabric commissioned on-network, send FindTransport — a Large Message ("L") command, push-av-stream-transport.element.ts:130 at db021da0 — with MRP_PAYLOAD and expects NOT_FOUND (TC_PAVST_2_8.py:179-189, TC_PAVSTTestBase.py:352-368, matter_testing.py:3105 at 6170af84); the module answers INVALID_TRANSPORT_TYPE (0xD1) on a UDP session, as matter.js does (CommandInvokeResponse.ts:281-285). chip's engine carries the same gate (InteractionModelEngine.cpp:1875-1884), inert for its code-driven clusters because its generator drops kLargeMessage (sdk_generator.py:62; PushAvStreamTransport Metadata.h:79-81): chip's camera-app answers NOT_FOUND there (PushAVStreamTransportLogic.cpp:1576-1585) only because that gate is inert. Kept by owner decision 183, notes/parity/by_design.md BD-chip-LargeMessageGateEnforced (runs, and fails as recorded) |
| PS/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| PS/2.3 | (b) not supported | not applicable: the case's PICS `PWRTL.S` is false for the reference DUT |
| PSCFG/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| PWRTL/2.2 | (b) not supported | TC-PWRTL-2.2 needs PowerTopology's ElectricalCircuit feature (has_feature(kElectricalCircuit), TC_PWRTL_2_2.py:76 at 6170af84), which the model at the matter.js pin does not define (parity/schema.json PowerTopology: NODE, TREE, SET, DYPF); the case skips itself (matter.js test/core/PWRTL.test.ts excludes it for the same reason) (runs, and skips itself) |
| REFALM/2.3 | (b) not supported | not applicable: the case's PICS `REFALM.S & REFALM.S.M.LocalSuppress` is false for the reference DUT |
| RH/2.2 | (c) harness | TC-RH-2.2 has an operator change the measured value between two reads (a UserPrompt under RH.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| S/2.3 | (b) not supported | not applicable: the case's PICS `S` is false for the reference DUT |
| SC/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/3.5 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT; controller role: matter.js drives chip's python script (prompt-driven-python-test.ts:41), the DUT answering its prompts — not ported (TC-SC-3.5.test.ts:43 TC_SC_3_5.py) |
| SC/4.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/4.6 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER & MCORE.DD.COMM_DISCOVERY` is false for the reference DUT |
| SC/4.7 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & MCORE.DD.COMM_DISCOVERY` is false for the reference DUT |
| SC/4.9 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SEPR/2.3 | (b) not supported | not applicable: the case's PICS `SEPR.S & SEPR.F00` is false for the reference DUT |
| SWTCH/2.2 | (b) not supported | TC-SWTCH-2.2 runs only on a latching switch (PICS SWTCH.S.F00=0: the daemon's switch is momentary) (runs, and skips itself) |
| SWTCH/2.6 | (b) not supported | TC-SWTCH-2.6 runs only with ActionSwitch (PICS SWTCH.S.F05=0): the daemon's button serves MomentarySwitchRelease, which ActionSwitch excludes (MSR conformance "[MS & !AS]", switch.element.ts) (runs, and skips itself) |
| TBRM/2.2 | (c) harness | TC-TBRM-2.2 step 13 compares the GetActiveDatasetRequest answer byte for byte with the dataset the case set (Test_TC_TBRM_2_2.yaml:158-166, PIXIT.TBRM.THREAD_ACTIVE_DATASET); an OpenThread border router serializes the dataset's TLVs in its own order (the virtual agent answers the Network Name TLV after the Mesh-Local Prefix), and the module answers the border router's bytes, as chip's TBRM server answers its Thread stack's; chip runs the case against network-manager-app's simulated stack, which keeps the bytes as set |
| TBRM/3.1.Simulated | (b) not supported | not applicable: the case's PICS `TBRM.C` is false for the reference DUT |
| TCCM/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| TCTL/2.3 | (b) not supported | not applicable: the case's PICS `TCTL.S & TCTL.S.F01` is false for the reference DUT |
| TCTL/3.3 | (b) not supported | not applicable: the case's PICS `TCTL.S & TCTL.S.F01` is false for the reference DUT |
| TIMESYNC/2.10 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.11 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.12 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.13 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F03` is false for the reference DUT |
| TIMESYNC/2.3 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S & TIMESYNC.S.F03 & TIMESYNC.S.C01.Rsp & TIMESYNC.S.C00.Rsp & TIMESYNC.S.A0001 & TIMESYNC.S.A0000` is false for the reference DUT |
| TIMESYNC/2.4 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.5 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.6 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F01` is false for the reference DUT |
| TIMESYNC/2.7 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.8 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TIMESYNC/2.9 | (b) not supported | not applicable: the case's PICS `TIMESYNC.S.F00` is false for the reference DUT |
| TLSCERT/2.2 | (a) defect | TC-TLSCERT-2.2 step 5: the module's reads of ProvisionedRootCertificates and ProvisionedClientCertificates serve the full certificates on every transport (BD-chip-TlsCertificateManagement-RulesInServer, owner decision 2026-10-28, after matter.js's generated behaviour); chip encodes the ids alone outside a large-payload transport (TLSCertificateManagementCluster.cpp:136-166 at the harness pin), and the case reads over MRP expecting no certificate |
| TLSCERT/2.9 | (a) defect | TC-TLSCERT-2.9 step 13: the module's reads of ProvisionedRootCertificates and ProvisionedClientCertificates serve the full certificates on every transport (BD-chip-TlsCertificateManagement-RulesInServer, owner decision 2026-10-28, after matter.js's generated behaviour); chip encodes the ids alone outside a large-payload transport (TLSCertificateManagementCluster.cpp:136-166 at the harness pin), and the case reads over MRP expecting no certificate; two such client certificates exceed one MRP message, so the read answers RESOURCE_EXHAUSTED |
| TMP/2.2 | (c) harness | TC-TMP-2.2 has an operator change the measured value between two reads (a UserPrompt under TMP.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| TSTAT/4.4 | (b) not supported | not applicable: the case's PICS `TSTAT.S & TSTAT.S.F0b` is false for the reference DUT |
| WEBRTCP/2.11 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C00.Rsp & WEBRTCP.S.C01.Tx & AVSM.S & AVSM.S.F00 & AVSM.S.F01 & PS.S.F01` is false for the reference DUT |
| WEBRTCP/2.17 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C02.Rsp & WEBRTCP.S.C03.Tx & WEBRTCR.C.C01.Tx & WEBRTCP.S.C06.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.18 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C00.Rsp & WEBRTCP.S.C01.Tx & WEBRTCR.S.C04.Rsp & WEBRTCP.S.C06.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.19 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C00.Rsp & WEBRTCP.S.C01.Tx & WEBRTCR.S.C04.Rsp & WEBRTCP.S.C06.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.20 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C02.Rsp & WEBRTCP.S.C03.Tx & WEBRTCR.S.C05.Rsp & WEBRTCP.S.C06.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.21 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.C02.Rsp & WEBRTCP.S.C03.Tx & WEBRTCR.C.C01.Rsp & WEBRTCR.S.C05.Rsp & WEBRTCP.S.C06.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.22 | (c) harness | TC-WEBRTCP-2.22 step 2 waits for the DUT's Answer to its ProvideOffer, then its ICE candidates and a connected peer (TC_WEBRTCP_2_22.py:143, :161, :167 at 6170af84); the --camera-sim camera answers ProvideOfferResponse and has no media stack to make an answer from (examples/reference-bridge/camera.go ProvideOffer), where chip's camera-app takes it from libdatachannel (webrtc-provider-manager.cpp:531-532, :1124-1150). The case's PICS (:76-84) omit the Answer code its sibling 2.17 gates on (WEBRTCR.C.C01.Tx, TC_WEBRTCP_2_17.py:82), which this DUT declares false; the body times out after 90 s |
| WEBRTCP/2.23 | (c) harness | TC-WEBRTCP-2.23 step 3 waits for the DUT's Answer, its ICE candidates and a connected peer (TC_WEBRTCP_2_23.py:190, :208, :214 at 6170af84); the --camera-sim camera has no media stack to answer from (see 2.22), and the case's PICS (:78-86) omit the Answer code 2.17 gates on |
| WEBRTCP/2.24 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.F02 & WEBRTCP.S.C00.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.25 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.F02 & WEBRTCP.S.C00.Rsp & AVSM.S & AVSM.S.F00 & AVSM.S.F01` is false for the reference DUT |
| WEBRTCP/2.33 | (c) harness | TC-WEBRTCP-2.33 step 2 waits for the DUT's Answer, its ICE candidates, a connected peer and RTP video frames and audio packets from the DUT (TC_WEBRTCP_2_33.py:110, :127, :132, :160, :165 at 6170af84); the --camera-sim camera captures, encodes and sends nothing (see 2.22), and the case's PICS (:172-180) omit the Answer code 2.17 gates on |
| WNCV/6.1.Simulated | (b) not supported | not applicable: the case's PICS `WNCV.C` is false for the reference DUT |
| WNCV/7.1.Simulated | (b) not supported | not applicable: the case's PICS `WNCV.C` is false for the reference DUT |
| WebRTCP/2.1 | (b) not supported | not applicable: the case's PICS `WEBRTCP.S & WEBRTCP.S.A0000 & WEBRTCP.S.C00.Rsp & WEBRTCP.S.C01.Tx & AVSM.S & AVSM.S.F00 & AVSM.S.F01 & PS.S.F01` is false for the reference DUT |

### Cases run with the Go controller as TH

matter.js's certification cases for the controller side, read mirrored: the reference daemon is the DUT,
the module's own controller performs the TS file's controller actions, and the TS file's TH-side
expectations are what the daemon has to satisfy (`internal/chiptool` TestGoCertificationCases).

| Case | Steps run | Steps skipped | Source |
| --- | ---: | ---: | --- |
| ACT/3.2 | 12 | 0 | Go controller as TH, after matter.js `TC-ACT-3.2.test.ts` |
| ALOGIN/12.2 | 4 | 0 | Go controller as TH, after matter.js `TC-ALOGIN-12.2.test.ts` |
| APPLAUNCHER/3.7.1 | 2 | 0 | Go controller as TH, after matter.js `TC-APPLAUNCHER-3.7.1.test.ts` |
| APPLAUNCHER/3.8.1 | 2 | 0 | Go controller as TH, after matter.js `TC-APPLAUNCHER-3.8.1.test.ts` |
| APPLAUNCHER/3.9.1 | 2 | 0 | Go controller as TH, after matter.js `TC-APPLAUNCHER-3.9.1.test.ts` |
| AUDIOOUTPUT/7.3 | 2 | 0 | Go controller as TH, after matter.js `TC-AUDIOOUTPUT-7.3.test.ts` |
| AUDIOOUTPUT/7.4 | 2 | 0 | Go controller as TH, after matter.js `TC-AUDIOOUTPUT-7.4.test.ts` |
| BDX/1.2 | 2 | 0 | Go controller as TH, after matter.js `TC-BDX-1.2.test.ts` |
| BDX/1.4 | 2 | 0 | Go controller as TH, after matter.js `TC-BDX-1.4.test.ts` |
| BDX/2.1 | 4 | 0 | Go controller as TH, after matter.js `TC-BDX-2.1.test.ts` |
| BDX/2.2 | 5 | 1 | Go controller as TH, after matter.js `TC-BDX-2.2.test.ts` |
| BIND/2.1 | 12 | 0 | Go controller as TH, after matter.js `TC-BIND-2.1.test.ts` |
| BIND/2.3 | 16 | 5 | Go controller as TH, after matter.js `TC-BIND-2.3.test.ts` |
| CADMIN/1.1 | 18 | 1 | Go controller as TH, after matter.js `TC-CADMIN-1.1.test.ts` |
| CADMIN/1.17 | 13 | 0 | Go controller as TH, after matter.js `TC-CADMIN-1.17.test.ts` |
| CADMIN/1.18 | 13 | 0 | Go controller as TH, after matter.js `TC-CADMIN-1.18.test.ts` |
| CADMIN/1.2 | 18 | 0 | Go controller as TH, after matter.js `TC-CADMIN-1.2.test.ts` |
| CADMIN/1.7 | 11 | 3 | Go controller as TH, after matter.js `TC-CADMIN-1.7.test.ts` |
| CADMIN/1.8 | 11 | 3 | Go controller as TH, after matter.js `TC-CADMIN-1.8.test.ts` |
| CC/3.4 | 9 | 0 | Go controller as TH, after matter.js `TC-CC-3.4.test.ts` |
| CC/4.5 | 11 | 0 | Go controller as TH, after matter.js `TC-CC-4.5.test.ts` |
| CC/5.4 | 5 | 0 | Go controller as TH, after matter.js `TC-CC-5.4.test.ts` |
| CC/6.4 | 10 | 0 | Go controller as TH, after matter.js `TC-CC-6.4.test.ts` |
| CC/7.5 | 11 | 0 | Go controller as TH, after matter.js `TC-CC-7.5.test.ts` |
| CC/9.4 | 7 | 0 | Go controller as TH, after matter.js `TC-CC-9.4.test.ts` |
| CHANNEL/5.4 | 2 | 0 | Go controller as TH, after matter.js `TC-CHANNEL-5.4.test.ts` |
| CHANNEL/5.5 | 2 | 0 | Go controller as TH, after matter.js `TC-CHANNEL-5.5.test.ts` |
| CHANNEL/5.6 | 2 | 0 | Go controller as TH, after matter.js `TC-CHANNEL-5.6.test.ts` |
| CNET/4.20 | 7 | 0 | Go controller as TH, after matter.js `TC-CNET-4.20.test.ts` |
| CONTENTLAUNCHER/10.4 | 2 | 0 | Go controller as TH, after matter.js `TC-CONTENTLAUNCHER-10.4.test.ts` |
| CONTENTLAUNCHER/10.6 | 2 | 0 | Go controller as TH, after matter.js `TC-CONTENTLAUNCHER-10.6.test.ts` |
| DD/3.18 | 9 | 0 | Go controller as TH, after matter.js `TC-DD-3.18.test.ts` |
| DD/3.20 | 8 | 0 | Go controller as TH, after matter.js `TC-DD-3.20.test.ts` |
| DD/3.21 | 5 | 0 | Go controller as TH, after matter.js `TC-DD-3.21.test.ts` |
| DGWIFI/3.2 | 2 | 0 | Go controller as TH, after matter.js `TC-DGWIFI-3.2.test.ts` |
| DRLK/3.2 | 20 | 0 | Go controller as TH, after matter.js `TC-DRLK-3.2.test.ts` |
| G/3.2 | 5 | 0 | Go controller as TH, after matter.js `TC-G-3.2.test.ts` |
| I/3.2 | 4 | 0 | Go controller as TH, after matter.js `TC-I-3.2.test.ts` |
| IDM/1.1 | 2 | 1 | Go controller as TH, after matter.js `TC-IDM-1.1.test.ts` |
| IDM/1.3 | 3 | 4 | Go controller as TH, after matter.js `TC-IDM-1.3.test.ts` |
| IDM/2.1 | 21 | 0 | Go controller as TH, after matter.js `TC-IDM-2.1.test.ts` |
| IDM/3.1 | 14 | 1 | Go controller as TH, after matter.js `TC-IDM-3.1.test.ts` |
| IDM/4.1 | 8 | 2 | Go controller as TH, after matter.js `TC-IDM-4.1.test.ts` |
| IDM/5.1 | 2 | 1 | Go controller as TH, after matter.js `TC-IDM-5.1.test.ts` |
| IDM/6.3 | 1 | 0 | Go controller as TH, after matter.js `TC-IDM-6.3.test.ts` |
| IDM/6.4 | 2 | 4 | Go controller as TH, after matter.js `TC-IDM-6.4.test.ts` |
| KEYPADINPUT/3.1 | 2 | 0 | Go controller as TH, after matter.js `TC-KEYPADINPUT-3.1.test.ts` |
| LOWPOWER/2.2 | 2 | 0 | Go controller as TH, after matter.js `TC-LOWPOWER-2.2.test.ts` |
| LVL/8.1 | 2 | 0 | Go controller as TH, after matter.js `TC-LVL-8.1.test.ts` |
| MEDIAINPUT/3.14 | 2 | 0 | Go controller as TH, after matter.js `TC-MEDIAINPUT-3.14.test.ts` |
| MEDIAINPUT/3.15 | 2 | 0 | Go controller as TH, after matter.js `TC-MEDIAINPUT-3.15.test.ts` |
| MEDIAINPUT/3.16 | 2 | 0 | Go controller as TH, after matter.js `TC-MEDIAINPUT-3.16.test.ts` |
| MEDIAINPUT/3.17 | 2 | 0 | Go controller as TH, after matter.js `TC-MEDIAINPUT-3.17.test.ts` |
| MEDIAPLAYBACK/6.10 | 4 | 0 | Go controller as TH, after matter.js `TC-MEDIAPLAYBACK-6.10.test.ts` |
| MEDIAPLAYBACK/6.5 | 4 | 0 | Go controller as TH, after matter.js `TC-MEDIAPLAYBACK-6.5.test.ts` |
| MEDIAPLAYBACK/6.7 | 7 | 0 | Go controller as TH, after matter.js `TC-MEDIAPLAYBACK-6.7.test.ts` |
| MOD/2.2 | 3 | 0 | Go controller as TH, after matter.js `TC-MOD-2.2.test.ts` |
| OO/3.2 | 7 | 0 | Go controller as TH, after matter.js `TC-OO-3.2.test.ts` |
| S/3.1 | 9 | 0 | Go controller as TH, after matter.js `TC-S-3.1.test.ts` |
| SC/4.2 | 3 | 0 | Go controller as TH, after matter.js `TC-SC-4.2.test.ts` |
| SC/4.8 | 4 | 0 | Go controller as TH, after matter.js `TC-SC-4.8.test.ts` |
| SC/5.3 | 5 | 3 | Go controller as TH, after matter.js `TC-SC-5.3.test.ts` |
| SC/6.1 | 8 | 3 | Go controller as TH, after matter.js `TC-SC-6.1.test.ts` |
| SC/8.1 | 1 | 0 | Go controller as TH, after matter.js `TC-SC-8.1.test.ts`; passes with chip-tool as TH too |
| SC/8.2 | 1 | 0 | Go controller as TH, after matter.js `TC-SC-8.2.test.ts`; passes with chip-tool as TH too |
| SC/8.3 | 2 | 0 | Go controller as TH, after matter.js `TC-SC-8.3.test.ts`; passes with chip-tool as TH too |
| SC/8.4 | 3 | 0 | Go controller as TH, after matter.js `TC-SC-8.4.test.ts`; passes with chip-tool as TH too |
| SC/8.5 | 2 | 0 | Go controller as TH, after matter.js `TC-SC-8.5.test.ts`; passes with chip-tool as TH too |
| SC/8.6 | 2 | 0 | Go controller as TH, after matter.js `TC-SC-8.6.test.ts`; passes with chip-tool as TH too |
| SC/8.7 | 2 | 0 | Go controller as TH, after matter.js `TC-SC-8.7.test.ts`; passes with chip-tool as TH too |
| SU/1.1 | 2 | 0 | Go controller as TH, after matter.js `TC-SU-1.1.test.ts` |
| SU/2.1 | 3 | 1 | Go controller as TH, after matter.js `TC-SU-2.1.test.ts` |
| SU/2.2 | 4 | 4 | Go controller as TH, after matter.js `TC-SU-2.2.test.ts`; passes with chip-tool as TH too |
| SU/2.3 | 3 | 3 | Go controller as TH, after matter.js `TC-SU-2.3.test.ts` |
| SU/2.4 | 2 | 0 | Go controller as TH, after matter.js `TC-SU-2.4.test.ts`; passes with chip-tool as TH too |
| SU/2.5 | 3 | 3 | Go controller as TH, after matter.js `TC-SU-2.5.test.ts` |
| SU/2.6 | 2 | 1 | Go controller as TH, after matter.js `TC-SU-2.6.test.ts` |
| SU/2.7 | 6 | 2 | Go controller as TH, after matter.js `TC-SU-2.7.test.ts` |
| SU/3.1 | 2 | 0 | Go controller as TH, after matter.js `TC-SU-3.1.test.ts` |
| SU/3.2 | 5 | 2 | Go controller as TH, after matter.js `TC-SU-3.2.test.ts` |
| SU/3.3 | 4 | 2 | Go controller as TH, after matter.js `TC-SU-3.3.test.ts` |
| SU/3.4 | 3 | 1 | Go controller as TH, after matter.js `TC-SU-3.4.test.ts` |
| SU/4.1 | 9 | 0 | Go controller as TH, after matter.js `TC-SU-4.1.test.ts`; passes with chip-tool as TH too |
| SWTCH/3.2 | 15 | 8 | Go controller as TH, after matter.js `TC-SWTCH-3.2.test.ts` |
| TGTNAV/8.2 | 2 | 0 | Go controller as TH, after matter.js `TC-TGTNAV-8.2.test.ts` |
| TSTAT/3.2 | 3 | 0 | Go controller as TH, after matter.js `TC-TSTAT-3.2.test.ts` |
| WNCV/6.1 | 4 | 0 | Go controller as TH, after matter.js `TC-WNCV-6.1.test.ts` |
| WNCV/7.1 | 2 | 1 | Go controller as TH, after matter.js `TC-WNCV-7.1.test.ts` |

#### Go-TH cases the families list under no case of that id

matter.js automates these cases under the test plan's id; chip's families carry the plan's steps under
other ids (`X.Simulated`, `3.7`), and no correspondence between the two is asserted here. Each case passed
with the Go controller as TH and is counted in no family.

| Case | Steps run | Steps skipped | Source |
| --- | ---: | ---: | --- |
| APPLAUNCHER/3.7.1 | 2 | 0 | after matter.js `TC-APPLAUNCHER-3.7.1.test.ts` |
| APPLAUNCHER/3.8.1 | 2 | 0 | after matter.js `TC-APPLAUNCHER-3.8.1.test.ts` |
| APPLAUNCHER/3.9.1 | 2 | 0 | after matter.js `TC-APPLAUNCHER-3.9.1.test.ts` |
| CC/3.4 | 9 | 0 | after matter.js `TC-CC-3.4.test.ts` |
| CC/4.5 | 11 | 0 | after matter.js `TC-CC-4.5.test.ts` |
| CC/5.4 | 5 | 0 | after matter.js `TC-CC-5.4.test.ts` |
| CC/6.4 | 10 | 0 | after matter.js `TC-CC-6.4.test.ts` |
| CC/7.5 | 11 | 0 | after matter.js `TC-CC-7.5.test.ts` |
| CC/9.4 | 7 | 0 | after matter.js `TC-CC-9.4.test.ts` |
| DGWIFI/3.2 | 2 | 0 | after matter.js `TC-DGWIFI-3.2.test.ts` |
| OO/3.2 | 7 | 0 | after matter.js `TC-OO-3.2.test.ts` |
| WNCV/6.1 | 4 | 0 | after matter.js `TC-WNCV-6.1.test.ts` |
| WNCV/7.1 | 2 | 1 | after matter.js `TC-WNCV-7.1.test.ts` |

### Controller role

matter.js's certification cases in their own direction: the module's controller is the DUT and the
reference daemon the TH device the TS file names (`internal/chiptool` TestGoCertificationCases). Such a
case counts as passed in its family; one no family of the harness image lists is counted in none.

#### Cases run with the Go controller as DUT

| Case | Steps run | Steps skipped | Source |
| --- | ---: | ---: | --- |
| BR/4 | 15 | 0 | Go controller as DUT, after matter.js `TC-BR-4.test.ts`; in no family of the harness image |
| CAMERA-GO/1.1 | 7 | 0 | Go controller as DUT, the camera acceptance of issue 410: one WebRTC session against chip-camera-app at 6170af84, the SDP over TCP (gocert_camera_test.go); in no family of the harness image |
| CGENTC-GO/1.1 | 3 | 0 | Go controller as DUT, the terms-and-conditions acceptance of issue 449: the commissioner reads the daemon's --tc-required requirement and fails early without the acknowledgement (gocert_tc_test.go); in no family of the harness image |
| DD/1.8 | 7 | 0 | Go controller as DUT, after matter.js `TC-DD-1.8.test.ts` |
| DD/1.9 | 2 | 0 | Go controller as DUT, after matter.js `TC-DD-1.9.test.ts` |
| DD/2.2 | 1 | 7 | Go controller as DUT, after matter.js `TC-DD-2.2.test.ts` |
| DD/3.11 | 6 | 4 | Go controller as DUT, after matter.js `TC-DD-3.11.test.ts` |
| DD/3.12 | 7 | 5 | Go controller as DUT, after matter.js `TC-DD-3.12.test.ts` |
| DD/3.13 | 7 | 5 | Go controller as DUT, after matter.js `TC-DD-3.13.test.ts` |
| DD/3.14 | 10 | 2 | Go controller as DUT, after matter.js `TC-DD-3.14.test.ts` |
| DD/3.15 | 4 | 0 | Go controller as DUT, after matter.js `TC-DD-3.15.test.ts` |
| DD/3.16 | 11 | 0 | Go controller as DUT, after matter.js `TC-DD-3.16.test.ts` |
| DD/3.17 | 15 | 0 | Go controller as DUT, after matter.js `TC-DD-3.17.test.ts` |
| DD/3.5 | 10 | 4 | Go controller as DUT, after matter.js `TC-DD-3.5.test.ts` |
| ICDB/1.3 | 5 | 0 | Go controller as DUT, after matter.js `TC-ICDB-1.3.test.ts`; in no family of the harness image |
| ICDM/6.1 | 4 | 0 | Go controller as DUT, after matter.js `TC-ICDM-6.1.test.ts`; in no family of the harness image |

#### Controller-role cases not run

matter.js's cases with its controller as DUT that the Go controller does not run as the DUT and no family of
the harness image lists; those a family lists carry the same reason on their row above.

| Case | Class | Reason |
| --- | --- | --- |
| TBRM/3.1 | (b) not supported | controller role: the TH is a Thread border router (`--otbr` needs a live OTBR REST API; matter.js runs it against chip's network-manager app only, TC-TBRM-3.1.test.ts:133-140) |
| WEBRTCR/2.1 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.1.test.ts:18 TC_WEBRTCR_2_1.py) |
| WEBRTCR/2.2 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.2.test.ts:18 TC_WEBRTCR_2_2.py) |
| WEBRTCR/2.3 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.3.test.ts:11 TC_WEBRTCR_2_3.py) |
| WEBRTCR/2.4 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.4.test.ts:11 TC_WEBRTCR_2_4.py) |
| WEBRTCR/2.5 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.5.test.ts:30 TC_WEBRTCR_2_5.py) |
| WEBRTCR/2.6 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.6.test.ts:19 TC_WEBRTCR_2_6.py) |
| WEBRTCR/2.7 | (c) harness | controller role, served since phase 37 (controller.Config.Camera): matter.js drives chip's python script with chip-camera-app as TH_SERVER and the DUT as requestor (tc-webrtcr-support.ts:139-145, prompt-driven-python-test.ts:41) — the prompt-driven runner is a follow-up (TC-WEBRTCR-2.7.test.ts:18 TC_WEBRTCR_2_7.py) |

### matter.js certification cases not run

matter.js automates 121 certification cases at `b25ed9604f2825857b8de28a3c29cae8124a7e49` (`support/chip-testing/test/cert/TC-*.test.ts`,
the snapshot's pin, listed in `internal/chiptool/testdata/matterjs-cert-cases.json`). The Go controller
runs 102 of them, as TH or as DUT (above); 10 controller-role cases carry their reason under "Controller role",
and 9 are not run yet: the module is unmeasured against them, and each names what a port needs. 0 of them
are not ported yet: the module could run them now. The others wait for what their row names. Those a family
lists carry the same row under "Cases not run"; this table lists the others.

| Case | Class | Reason |
| --- | --- | --- |
| CHANNEL/5.8 | (b) not supported | matter.js automates it at the snapshot's pin (TC-CHANNEL-5.8.test.ts:24); the Go-TH leg does not run it, so the module is unmeasured here — needs Channel's ElectronicGuide and RecordProgram features (GetProgramGuide, RecordProgram, CancelRecordProgram): the module serves them to a host port that implements ChannelGuide and ChannelRecorder (`cluster/media/channel.go`: `ChannelGuide`, `ChannelRecorder`), the daemon's video player implements neither (CHANNEL.S.F02=0, CHANNEL.S.F03=0) |
| DGETH/3.2 | (b) not supported | matter.js automates it at the snapshot's pin (TC-DGETH-3.2.test.ts:19); the Go-TH leg does not run it, so the module is unmeasured here — needs ResetCounts, which the module's EthernetNetworkDiagnostics does not offer, with neither PacketCounts nor ErrorCounts (`EthernetNetworkDiagnosticsConfig.Attributes` in `cluster/core/ethernet_network_diagnostics.go`; DGETH.S.C00.Rsp=0): a port would record its only step skipped |
| DGSW/3.2 | (b) not supported | matter.js automates it at the snapshot's pin (TC-DGSW-3.2.test.ts:19); the Go-TH leg does not run it, so the module is unmeasured here — needs ResetWatermarks: the module serves the Watermarks feature to a host that supplies a WatermarkReporter (`WatermarkReporter` in `cluster/core/software_diagnostics.go`), the reference daemon supplies none (DGSW.S.F00=0, DGSW.S.C00.Rsp=0) |
| DGTHREAD/3.4 | (c) harness | matter.js automates it at the snapshot's pin (TC-DGTHREAD-3.4.test.ts:27); the Go-TH leg does not run it, so the module is unmeasured here — needs the daemon's Thread variant (`--otbr` against a live OTBR REST API, the DGTHREAD family's composition, run by otbr-virtual.yml), which the Go-TH leg does not start |

### Cases the family run never reaches

Python cases of a family at the harness commit that the harness image's test descriptor does not
file, with the measured cause (the family table's absent rows, held to chip's tree by
TestChipPythonCasesCovered, `make chipcases-check`). They are counted in no column above.

| Case | Class | Reason |
| --- | --- | --- |
| DD/1.5 | (c) harness | not in the harness image's test descriptor, so the family run never reaches it: the case file imports matter.testing.nfc, which imports ndef, which the image's Python lacks (generate-test-descriptor re-run in the image: No module named 'ndef'), and generate-test-descriptor skips a file whose import raises (:290-296) |
| DD/3.23 | (c) harness | not in the harness image's test descriptor, so the family run never reaches it: the case file imports matter.testing.nfc, which imports ndef, which the image's Python lacks (generate-test-descriptor re-run in the image: No module named 'ndef'), and generate-test-descriptor skips a file whose import raises (:290-296) |
| DD/3.24 | (c) harness | not in the harness image's test descriptor, so the family run never reaches it: the case file imports matter.testing.nfc, which imports ndef, which the image's Python lacks (generate-test-descriptor re-run in the image: No module named 'ndef'), and generate-test-descriptor skips a file whose import raises (:290-296) |
| IDM/10.7 | (c) harness | not in the harness image's test descriptor, so the family run never reaches it: the case file imports matter.testing.nfc, which imports ndef, which the image's Python lacks (generate-test-descriptor re-run in the image: No module named 'ndef'), and generate-test-descriptor skips a file whose import raises (:290-296) |

### Cases that pass with a recorded gap

The CHIP checker reports exactly the recorded problems and nothing else; the case fails as soon as
another problem appears or a recorded one disappears.

| Case | Class | Gap |
| --- | --- | --- |

<!-- END GENERATED -->

## Families this module does not run yet

The reference set is matter.js's (`support/chip-testing/test/core`,
`test/app-*`) plus the family of every cluster server this module ships.
The families the harness runs are `chipFamilies` in
`internal/chiptool/families_table_test.go`; a cluster this module serves
whose family is not in that list is "not yet run", not "not supported".

| Family | Why not | Class |
| --- | --- | --- |
| ILL, PRS, DISHM | the module serves the cluster (`cluster/measurement` Illuminance and Pressure; `cluster/modebase` DishwasherMode), but the reference daemon exposes no endpoint with it and the harness does not run the family | (c) not yet run |
| CONCON | the module serves ContentControl (`cluster/media`), but chip has no case of the family at the harness pin | (c) no case |
| ICDM, ICDB | not applicable to the reference daemon: it is mains-powered and sets no `node.Config.ICD`, so it serves no ICD Management (`ICDM.S` is false in its generated PICS). Applicable to a host that sets `Config.ICD`: the module then serves ICD Management and the Check-In protocol after matter.js ([ICD guide](./guides/icd.md)); the harness does not run the families against such a node. The daemon's `--icd-lit` (TESTING ONLY) is such a node for the ICD client cases the Go controller runs as the DUT (TC-ICDM-6.1, TC-ICDB-1.3, under "Controller role" above) | (b) not supported |
| SC_TC (PASE over Thread MeshCoP, `TC_SC_TC_*.py`), the Thread cases of DD and SC | the device side has no Thread radio | (d) out of scope |
| commissioning the node over BLE in the certification cases: the BLE discovery cases of DD and SC | the node is commissioned over BLE onto Wi-Fi since milestone 7 (ADR 0031): `make chiptool-ble-wifi` has chip-tool commission `examples/ble-wifi-light` over BLE onto Wi-Fi under chip's BlueZ and wpa_supplicant mocks, with chip's all-clusters app as the control, and `make chiptool-ble-cases` runs the Python cases whose TH commissions over BLE in the test (below, "Cases that commission over BLE"). The DD and SC cases that discover the node over BLE are manual YAML cases at the harness pin (the rows above); no automated case of either family commissions over BLE | (c) harness |
| CNET Thread cases | the device side has no Thread radio; the Thread NetworkCommissioning variant over a border router (`core.ThreadNetworkCommissioning`, ADR 0027) is not on the reference daemon | (c) not yet run |
| TSTAT 4.5 (MatterScheduleConfiguration) | the module serves MSCH behind `surface.Policies.Extensions("BD-chip-Thermostat-Schedules")` and the reference daemon enables it with `--thermostat-schedules`, but `TC_TSTAT_4_5.py` is absent at the harness commit `6170af84` (present in connectedhomeip from 2026-10-23); lifting `CHIP_TEST_IMAGE` is a separate owner decision | (c) not yet run |
| ACS, WIFINM | the module serves AmbientContextSensing (`cluster/sensing`, wave 26-C, a host server only: no device type at the pin lists it, `BD-chip-AmbientContextSensing-RulesInServer`) and WiFiNetworkManagement (`cluster/netinfra`, wave 26-B), but the reference daemon composes neither and the harness does not run their families | (c) not yet run |
| CCTRL | the module serves CommissionerControl (`node.Config.CommissionerControl`, ADR 0020 stage 6), but the reference daemon mounts none, and chip runs the cases against its fabric-sync bridge (`TC_CCTRL_2_1.py` CI arguments: `app: examples/fabric-admin/scripts/fabric-sync-app.py`) | (c) not yet run |
| AVANALY, ESALM, EPALM, TSTATM, ASU, AUDIOCONTROL, MEDIAFILEMANAGEMENT, NETIM, PROXR, HSTAT, COMPRO | the cluster is not in the matter.js snapshot at `db021da0` (`parity/schema.json`; no `packages/model/src/standard/elements/<name>.element.ts` at the pin): AvAnalysis, ElectricalAlarm, ElectricalProtectionAlarm, ThermostatMode, AmbientSensingUnion, AudioControl, MediaFileManagement, NetworkIdentityManagement, ProximityRanging, Humidistat, CommissioningProxy. chip has them at the harness pin; a server waits for a snapshot that carries the cluster (schema values come from matter.js) | (b) not supported |
| WEBRTC | `TC_WEBRTC_1_1` to `1_8` establish a WebRTC media session with the camera (`TC_WEBRTC_1_1.py`:77, "Verify the WebRTC session has been successfully established"); the module carries no media stack (`cluster/camera`: the host runs the peer connection), and the harness image's descriptor lists none of the cases | (c) harness |
| WEBRTCR | controller role: the cases drive a camera controller (the image's descriptor: app `camera-controller`) through chip's prompt-driven Python runner with chip-camera-app as TH_SERVER; since phase 37 the controller serves the requestor (`controller.Config.Camera`) and `controller/camera` streams from a camera — the `CAMERA-GO/1.1` leg carries one session end to end against chip-camera-app (above) — so the cases wait only for the prompt-driven runner in the harness | (c) harness |
| the TZ, NTPC, NTPS and TSC cases of TIMESYNC | the module serves the four features after connectedhomeip since wave 27-B (`node.Config.TimeSynchronization`); the reference daemon turns none on, so the cases stay not applicable in the status block above | (c) not yet run |

CNET runs twice. The `CNET` row is the default composition, whose
NetworkCommissioning is Ethernet-only; its Wi-Fi cases are not applicable or
excluded there. The `CNETWIFI` row runs the same cases against the reference
daemon with `--wifi-sim`: NetworkCommissioning with the Wi-Fi feature
(`CNET.S.F00=1`) over a simulated driver (TESTING ONLY), with PICS generated
from that daemon (`internal/chiptool/testdata/pics-wifi-sim/`). Its
commissioned snapshots carry the Wi-Fi network a test harness commissioning
over BLE-Wi-Fi would have configured; since phase 30 the daemon keeps that
committed network in its database and rejoins it when it starts, as chip's
Linux driver does, so TC-CNET-4.1, 4.4 and 4.9 run on the restarted daemon,
and the simulated driver knows only the `--wifi-sim-ssid` networks. Cases
that need a real driver, a second access point, a BLE-commissioning TH or
per-device credentials stay classified in the table above. TC-EEVSE-2.3
runs too: its eleventh ChargingTarget is the server's RESOURCE_EXHAUSTED,
as chip answers (`BD-chip-EVSE-TargetsServerHeld`). Two cases are class
(a) in the table above: TC-TLSCERT-2.2 and 2.9. TC-PAVST-2.3, 2.4, 2.6,
2.11 and TC-WASHERCTRL-2.2 pass since the family run of 2026-11-15: the
servers answer as connectedhomeip does since owner decision 69 —
PushAvStreamTransport and LaundryWasherControls judge the fields concerned
themselves (`script/clustergen/relaxed.go`) — and the five cases, which
stood as not ported yet with their run pending until that run, are cases
of the table above with their step counts.

CGEN runs twice as well (phase 38). The `CGEN` row is the default
composition, which serves no terms and conditions, so TC-CGEN-2.5 to 2.11
are not applicable there. The `CGENTC` row runs the same cases against the
daemon with `--tc-required`: GeneralCommissioning's TermsAndConditions
feature (`CGEN.S.F00=1`, register row
`BD-chip-GeneralCommissioning-TermsAndConditions`), version 1 with
acknowledgement bit 1 required, the values connectedhomeip's CI starts its
terms-and-conditions app with, with PICS generated from that daemon
(`internal/chiptool/testdata/pics-tc/`). Its commissioned snapshots
acknowledge the terms during commissioning, as a TC-aware commissioner
does; TC-CGEN-2.5 to 2.11 commission the factory-fresh daemon themselves
with the PIXITs from their CI arguments. TC-CGEN-2.8 and 2.9 stop at their
factory-reset prompt, as in chip's CI.

The second compositions (phase 34) run the same way, each family against
the daemon variant that serves its clusters, with PICS generated from that
daemon (`internal/chiptool/testdata/pics-<variant>/`): `--tls-sim` for
TLSCERT and TLSCLIENT; `--camera-sim` with `--tls-sim` (a Camera and a Chime
after chip's camera-app) for WEBRTCP, AVSM, AVSUM, ZONEMGMT, PAVST, PAVSTI
and CHIME, a case whose CI starts the camera-app with
`--camera-deferred-offer` or `--camera-test-videosrc` starting the daemon
with the same switch; `--jointfabric-sim` for JFADMIN and JFDS; and `--otbr`
against a virtual OTBR agent for TBRM, DGTHREAD and THNETDIR, which run on
dispatch and weekly in `.github/workflows/otbr-virtual.yml`, never on a pull
request. The PAVST cases start chip's Push AV server and the JF cases chip's
JF apps; matter.js's image carries neither, so they run in go-fabric's own
image (`internal/chiptool/image`, built by `.github/workflows/chip-image.yml`).
`TestUnfamiliedPrefixes` holds the rest of chip's case prefixes to this
page: every prefix with three cases or more at the harness commit is a
family row or a row of the table above.

Some families run but have nothing the daemon can be tested on. The
default daemon is no ICD, so its root mounts no IcdManagement (`node/root.go`
buildRoot; the `--icd-lit` daemon above is the ICD variant), and the ICDM
cases are not applicable through its own PICS (`ICDM.S=0`).
The OTA Software Update Requestor is mounted since #49 (ADR 0019) and its
automated SU cases run, against a daemon in harness mode: the suite starts
every daemon with `--ota-harness` (TESTING ONLY, `internal/chiptool/bridge_test.go`
startBridgeWith), under which the example applier consents to an update and
reports a downloaded image's version as running at once, without a restart,
running the requestor's start-up path in place (VersionApplied,
NotifyUpdateApplied; `node/ota.go`). That is what the SU cases that read
SoftwareVersion and the VersionApplied event pass on. Without the flag the
applier keeps the old version running and the requestor reports a failed
update after the next restart; a product's applier and its restart into the
new image are the product's, and these cases do not test them. The image lists
the DLOG case, the BIND cases and the SU YAML cases as manual. The status
block records BIND/2.2 and DLOG/2.1 as class (c); BIND/2.1, BIND/2.3 and the
SU cases run in the Go-TH leg with the module's controller as TH (the table
above). On substance BIND tests the binding client (`BIND.C`, a
controller-side role, proven in-repo since wave 21), and the SU provider
cases an OTA provider (built in wave 23, ADR 0021; the in-repo proof is the
module's requestor downloading from the module's provider); the DLOG case
stays manual at the pinned image although the server now transfers over
BDX. BDX shipped with the OTA requestor in `v0.3.0` (ADR 0019) as the
receiving side; the sending side came with ADR 0021 in `v0.4.0`, and a
RetrieveLogsRequest that asks for BDX with a `TransferFileDesignator` is
now served over BDX as chip does (`BD-chip-DiagLogs-BDX`).

DGWIFI runs against a Wi-Fi link the daemon simulates: the harness starts
every daemon with `--wifi` (TESTING ONLY), which serves WiFiNetworkDiagnostics
on the root from fixed values and counters the app pipe steps
(`examples/reference-bridge/wifi.go`), while NetworkCommissioning stays
Ethernet-only and `MCORE.COM.WIFI` stays 0 — a product on Wi-Fi supplies its
own reporter (`node.Network.WiFi`).

The media families (ADR 0022) run against the daemon's `--media` pair
(TESTING ONLY, `examples/reference-bridge/media.go`): a CastingVideoPlayer
and a ContentApp that simulate connectedhomeip's tv-app — its transport,
channels, inputs, outputs, targets and the test suite's content app —
behind the `cluster/media` servers, and which every daemon of the suite
carries. Messages joins the player only with `--media-messages` (the MESS
family's variant): connectedhomeip's TC-IDM-10.5 at the harness pin
evaluates a device type's cluster conformance with
`EmptyClusterGlobalAttributes`, whose revision is 1
(`matter/testing/conformance.py:139-147`, `:277-294`;
`device_conformance_tests.py:404-414` at 6170af84), so the Casting Video
Player's `[Rev >= v2]` term for Messages is false and the cluster is
reported disallowed on a revision-2 player — the tv-app's own composition
(`tv-app.matter:4385`, `:4407`, revision 3) would be judged the same. The image lists the media cases whose every step is disabled as
manual, recorded as class (c) — those matter.js automates are not ported
yet, with a row that names what a port needs; TC-MEDIAPLAYBACK-6.8 expects an inactive
text track to read as a track with an empty ID, which the model at the pin
cannot carry (null is its inactive track), and is excluded as class (c);
the Python media cases gate on PICS codes the model at the pin
does not define (MediaPlayback's AvailableCommands and ContentInfo,
ContentLauncher's ContentReplication and Presets features, AccountLogin's
OAuth feature) and are not applicable. Nothing here plays media: a
product's host implements the ports over its own player.

## Cases that commission over BLE

`make chiptool-ble-cases` (`internal/chiptool/blecases_test.go`) runs
connectedhomeip's Python cases whose TH commissions the DUT over BLE onto
Wi-Fi inside the case (`--commissioning-method ble-wifi`) against
`examples/ble-wifi-light`, one fresh environment per case: chip's network
namespaces, BlueZ mock (bluezoo) and wpa_supplicant mock, as for
`make chiptool-ble-wifi`. The TH is the harness image's second Python
controller, `/opt/matter-ble`, built with BLE: the image's own controller is
built without it (matter.js `support/chip/support/install-chip-python` sets
`chip_config_network_layer_ble=false` and `chip_enable_ble=false`; its
`_ChipDeviceCtrl.so` has no BlueZ code and no `pychip_ble_adapter_list_new`).
The PICS are the light's own, generated over BLE first (`gen_pics.py`). A
case that does not pass runs once more against chip's `all-clusters-app-ble`
under the same mocks, the control. At the harness pin the Wi-Fi CNET cases
are the only Python cases that commission over BLE:

| Case | Over BLE onto Wi-Fi | Evidence |
| --- | --- | --- |
| TC-CNET-1.4 | passes | |
| TC-CNET-4.1 | passes | |
| TC-CNET-4.4 | (c) harness | step 4 expects ScanNetworks to return a network; the light answers at once with the mock's scan results, and chip's mock serves none (`BSSs` is an empty list, `wifi.py`:693-695). The control stops earlier, at step 3 (Networks not connected under the mock's unquoted SSID) |
| TC-CNET-4.9 | passes | |
| TC-CNET-4.11 | (c) harness | a second access point (`PIXIT.CNET.WIFI_2ND_ACCESSPOINT_SSID`) the mock does not serve (one network, `wifi.py`:735-736) and a TH host Wi-Fi to switch; the control fails the same assertion |
| TC-CNET-4.15 | passes | |
| TC-CNET-4.23 | (c) harness | step 3 expects a join to a wrong SSID to fail; the mock ignores the network's arguments and associates whatever is selected (`wifi.py`:415-440); the control fails at the same step |
| TC-CNET-4.25, 4.26, 4.27, 4.29 | (b) not supported | the cases run and skip themselves: no per-device credentials |

The leg is not a required check; the release gate includes it. Until the
pinned image carries `/opt/matter-ble` it skips, naming why.

## What a product owner must supply

A library cannot be certified; a product can. Everything below belongs to the
product and is out of this module's reach:

- **Vendor and product identity**: a CSA-assigned Vendor ID and the product's
  Product ID. The reference daemon uses the test vendor ID 0xFFF1.
- **Device attestation**: a production DAC and PAI chained to a PAA on the
  DCL, provisioned securely per device. The reference daemon uses CHIP's test
  attestation chain and a locally built test Certification Declaration
  (`secure/attestation`), which no ecosystem accepts outside development.
- **Certification Declaration** issued by the CSA for the product,
  and the **DCL entry** (model, software versions, OTA URLs, the CD).
- **The product's PICS and PIXIT**: generated from the product with
  `gen_pics.py` and completed with the product's own declarations
  (`reference-bridge.pics` shows which ones).
- **Platform obligations**: persistent storage that survives power loss,
  factory reset, a monotonic time source, an IPv6-capable Ethernet or Wi-Fi
  network interface with multicast (DNS-SD, group messaging), secure key
  storage for the operational keys.
- **Software update**: an OTA requestor path if the product ships one (the
  module has a requestor server, not an update agent).
- **The test laboratory run** itself, against the product, at the
  specification and test-harness version the CSA requires at the time.

## Open findings

Class (a) gaps that are not fixed yet are listed by TC id in
[`notes/parity/matter_behaviour_findings.md`](https://github.com/thingfabric/go-fabric/blob/main/notes/parity/matter_behaviour_findings.md)
under *Certification harness — open certifiability findings*; each excluded
case in the status block names its finding.
