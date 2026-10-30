# Certifiability status

Certification is not pursued by this project, and nothing built on it may be
described as certified. Certifiability is a goal: a product built on this
module should be able to pass certification
([ADR 0011](./adr/0011-certifiability-is-a-goal.md)). This page is where that
goal is measured. It is partly generated: the status block below is held to
the family table (`internal/chiptool/families_table_test.go`) and to the last
family run (`internal/chiptool/testdata/chip-cases.golden.json`) by
`TestCertifiabilityDocument`, which runs in every `go test ./...` and fails
when the two disagree or when a case neither passes nor is a classified gap.

Passing borrowed certification cases here is evidence of certifiability, not a
certification result. Only an Authorized Test Laboratory run against a concrete
product produces one.

## How the status is produced

```sh
make chiptool-families                                  # every family, hours
GOFABRIC_CHIP_FAMILIES=IDM,ACL make chiptool-families   # a selection
```

- **The device under test** is the reference daemon in
  `examples/reference-bridge`: a root node, an aggregator and one bridged
  endpoint per device surface the module serves (lights, sensors, closures,
  climate, appliances, an air purifier with its filters, a garage door, a
  lock, a smoke/CO alarm, a switch).
- **The harness** is matter.js's CHIP image (`ghcr.io/matter-js/chip`),
  pinned by digest in the Makefile (`CHIP_TEST_IMAGE`) together with the CHIP
  commit it was built from (`CHIP_TEST_IMAGE_COMMIT`). It carries chip-tool,
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
  a gap of exactly one class:

  | Class | Meaning | What follows |
  | --- | --- | --- |
  | (a) defect | go-fabric misbehaves or lacks mandatory behaviour | fixed per matter.js, or an open finding carrying the TC id |
  | (b) not supported | optional per spec for the device types exposed, and declared so in PICS | the PICS line is the record |
  | (c) harness | the test case or the environment is at fault | the evidence is cited |
  | (d) out of scope | Bluetooth and Thread on the device side, Wi-Fi commissioning of the node itself | the scope decision is named (the controller role entered with ADR 0020; BLE commissioning in the controller with ADR 0024) |

  A case matter.js passes and this module fails is class (a) by definition.
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
- **What a passing case executed** is pinned per case (steps run and steps
  skipped), so a PICS change that quietly turns a case into a no-op fails it.
- **A case that passes with a recorded gap** is one whose CHIP spec checker
  reports exactly the recorded problems and nothing else; it fails as soon as
  another problem appears or the recorded one disappears.

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

Harness image: `ghcr.io/matter-js/chip@sha256:d6f1de89d714309beb621543d451a98996690eea82fa74d1563abd7d2b3cb326`.

| Family | Cases | Passed | of which with a recorded gap | (a) defect | (b) not supported | (c) harness | (d) out of scope |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ACE | 10 | 10 | 0 | 0 | 0 | 0 | 0 |
| ACL | 11 | 10 | 0 | 0 | 1 | 0 | 0 |
| BINFO | 4 | 2 | 0 | 0 | 2 | 0 | 0 |
| BRBINFO | 5 | 1 | 0 | 0 | 3 | 1 | 0 |
| CADMIN | 21 | 9 | 0 | 0 | 3 | 9 | 0 |
| CGEN | 10 | 3 | 0 | 0 | 7 | 0 | 0 |
| CNET | 23 | 2 | 0 | 0 | 12 | 4 | 5 |
| CNETWIFI | 23 | 3 | 0 | 3 | 11 | 6 | 0 |
| DA | 13 | 4 | 0 | 0 | 5 | 4 | 0 |
| DD | 30 | 2 | 0 | 0 | 1 | 27 | 0 |
| DESC | 3 | 2 | 0 | 0 | 0 | 1 | 0 |
| DGGEN | 7 | 6 | 0 | 0 | 0 | 1 | 0 |
| DT | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| G | 5 | 2 | 0 | 0 | 0 | 3 | 0 |
| GC | 8 | 8 | 0 | 0 | 0 | 0 | 0 |
| GRPKEY | 3 | 2 | 0 | 0 | 0 | 1 | 0 |
| IDM | 33 | 20 | 0 | 0 | 0 | 13 | 0 |
| OPCREDS | 9 | 6 | 0 | 0 | 1 | 2 | 0 |
| RR | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| SC | 34 | 16 | 0 | 0 | 3 | 15 | 0 |
| SM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| TIMESYNC | 14 | 3 | 0 | 0 | 11 | 0 | 0 |
| DLOG | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| ICDM | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| SU | 13 | 3 | 0 | 0 | 0 | 10 | 0 |
| BDX | 11 | 0 | 0 | 0 | 0 | 11 | 0 |
| BIND | 3 | 0 | 0 | 0 | 0 | 3 | 0 |
| FLABEL | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| ULABEL | 4 | 4 | 0 | 0 | 0 | 0 | 0 |
| LCFG | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| LTIME | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| LUNIT | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| DGSW | 4 | 1 | 0 | 0 | 2 | 1 | 0 |
| DGETH | 3 | 2 | 0 | 0 | 1 | 0 | 0 |
| DGWIFI | 4 | 2 | 0 | 0 | 1 | 1 | 0 |
| ACT | 3 | 0 | 0 | 0 | 0 | 3 | 0 |
| PSCFG | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| ECOINFO | 2 | 0 | 0 | 0 | 0 | 2 | 0 |
| ACFREMON | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| BOOL | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| BOOLCFG | 9 | 9 | 0 | 0 | 0 | 0 | 0 |
| CDOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| CMOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| AIRQUAL | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| PMICONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| PMKCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| FLDCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| NDOCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| OZCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| PMHCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| RNCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| TVOCCONC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| SOIL | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| CLCTRL | 12 | 5 | 0 | 0 | 7 | 0 | 0 |
| CLDIM | 11 | 6 | 0 | 0 | 5 | 0 | 0 |
| CC | 31 | 21 | 0 | 0 | 6 | 4 | 0 |
| DRLK | 14 | 10 | 0 | 0 | 2 | 2 | 0 |
| EPREF | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| EWATERHTR | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| MTRID | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| SETRF | 4 | 4 | 0 | 0 | 0 | 0 | 0 |
| COMMTR | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| EGC | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| DEM | 11 | 9 | 0 | 0 | 2 | 0 | 0 |
| DEMM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| EEVSE | 10 | 9 | 0 | 1 | 0 | 0 | 0 |
| EEVSEM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| FAN | 12 | 7 | 0 | 0 | 5 | 0 | 0 |
| HEPAFREMON | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| FLW | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| I | 5 | 3 | 0 | 0 | 0 | 2 | 0 |
| LVL | 10 | 8 | 0 | 0 | 1 | 1 | 0 |
| LWM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| MOD | 8 | 2 | 0 | 0 | 5 | 1 | 0 |
| OCC | 5 | 5 | 0 | 0 | 0 | 0 | 0 |
| OO | 8 | 4 | 0 | 0 | 2 | 2 | 0 |
| OPSTATE | 6 | 6 | 0 | 0 | 0 | 0 | 0 |
| PCC | 4 | 4 | 0 | 0 | 0 | 0 | 0 |
| PS | 4 | 2 | 0 | 0 | 1 | 1 | 0 |
| RH | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| RVCCLEANM | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| RVCOPSTATE | 4 | 4 | 0 | 0 | 0 | 0 | 0 |
| TCCM | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| TCTL | 5 | 3 | 0 | 0 | 2 | 0 | 0 |
| DISHALM | 7 | 1 | 0 | 0 | 0 | 6 | 0 |
| REFALM | 3 | 2 | 0 | 0 | 1 | 0 | 0 |
| OVENOPSTATE | 5 | 4 | 0 | 0 | 1 | 0 | 0 |
| OTCCM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| MWOCTRL | 4 | 4 | 0 | 0 | 0 | 0 | 0 |
| MWOM | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| RVCRUNM | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| S | 7 | 5 | 0 | 0 | 1 | 1 | 0 |
| SEAR | 5 | 5 | 0 | 0 | 0 | 0 | 0 |
| SMOKECO | 7 | 7 | 0 | 0 | 0 | 0 | 0 |
| SWTCH | 5 | 2 | 0 | 0 | 2 | 1 | 0 |
| TMP | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| TSTAT | 6 | 3 | 0 | 0 | 2 | 1 | 0 |
| TSUIC | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| VALCC | 10 | 8 | 0 | 0 | 2 | 0 | 0 |
| WHM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| WNCV | 17 | 15 | 0 | 0 | 2 | 0 | 0 |
| ALOGIN | 3 | 1 | 0 | 0 | 1 | 1 | 0 |
| APBSC | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| APPLAUNCHER | 5 | 5 | 0 | 0 | 0 | 0 | 0 |
| APPOBSERVER | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| AUDIOOUTPUT | 4 | 2 | 0 | 0 | 0 | 2 | 0 |
| CHANNEL | 6 | 3 | 0 | 0 | 0 | 3 | 0 |
| CONTENTLAUNCHER | 9 | 4 | 0 | 0 | 3 | 2 | 0 |
| KEYPADINPUT | 3 | 2 | 0 | 0 | 0 | 1 | 0 |
| LOWPOWER | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| MEDIAINPUT | 8 | 4 | 0 | 0 | 0 | 4 | 0 |
| MEDIAPLAYBACK | 11 | 5 | 0 | 0 | 2 | 4 | 0 |
| TGTNAV | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| WAKEONLAN | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| **all** | **692** | **391** | **0** | **4** | **126** | **166** | **5** |

### Cases not run

| Case | Class | Reason |
| --- | --- | --- |
| ACL/2.11 | (b) not supported | not applicable: the case's PICS `ACL.S.F01` is false for the reference DUT |
| ACT/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ACT/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ACT/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ALOGIN/12.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ALOGIN/12.3 | (b) not supported | not applicable: the case's PICS `ALOGIN.S & ALOGIN.S.F00` is false for the reference DUT |
| APPOBSERVER/13.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| AUDIOOUTPUT/7.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| AUDIOOUTPUT/7.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BDX/2.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BIND/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BIND/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BIND/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BINFO/3.1 | (b) not supported | not applicable: the case's PICS `BINFO.S.A0014` is false for the reference DUT |
| BINFO/3.2 | (b) not supported | not applicable: the case's PICS `BINFO.S & BINFO.S.M.DeviceConfigurationChange` is false for the reference DUT |
| BRBINFO/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| BRBINFO/3.1 | (b) not supported | not applicable: the case's PICS `BRBINFO.S.A0014` is false for the reference DUT |
| BRBINFO/3.2 | (b) not supported | not applicable: the case's PICS `BRBINFO.S & BRBINFO.S.A.A0018 & BRBINFO.S.M.DeviceConfigurationChange` is false for the reference DUT |
| BRBINFO/4.1 | (b) not supported | TC-BRBINFO-4.1 exercises KeepActive of a bridged ICD (BridgedICDSupport, PICS BRBINFO.S.F00=0) against a LIT ICD test app the case starts itself (${LIT_ICD_APP}); the case carries no PICS gate, so it runs regardless (matter.js test/core/BRBINFO.test.ts excludes it for the same reason) |
| CADMIN/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.12 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.16 | (b) not supported | not applicable: the case's PICS `CADMIN.S & CADMIN.S.F00` is false for the reference DUT |
| CADMIN/1.17 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.18 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.4 | (b) not supported | not applicable: the case's PICS `CADMIN.S.F00` is false for the reference DUT |
| CADMIN/1.6 | (b) not supported | not applicable: the case's PICS `CADMIN.S & CADMIN.S.F00` is false for the reference DUT |
| CADMIN/1.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CADMIN/1.8 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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
| CHANNEL/5.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CHANNEL/5.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CHANNEL/5.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CLCTRL/3.1 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F06` is false for the reference DUT |
| CLCTRL/4.2 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F01` is false for the reference DUT |
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
| CNET/4.11 | (d) out of scope | TC-CNET-4.11 verifies Wi-Fi ConnectNetwork; Wi-Fi commissioning is out of scope (ADR 0011 (d), docs/matterjs-comparison.md), and the case has no PICS gate (CNET.S.F00=0) |
| CNET/4.12 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.15 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.16 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.2 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.20 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.21 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CNET/4.22 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.23 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.25 | (d) out of scope | TC-CNET-4.25 verifies Wi-Fi per-device credentials; Wi-Fi commissioning is out of scope and the case skips itself on a node without the Wi-Fi feature (runs, and skips itself) |
| CNET/4.26 | (d) out of scope | TC-CNET-4.26 verifies Wi-Fi QueryIdentity (per-device credentials); out of scope with Wi-Fi commissioning, and the case skips itself (runs, and skips itself) |
| CNET/4.27 | (d) out of scope | TC-CNET-4.27 verifies Wi-Fi network client identities; out of scope with Wi-Fi commissioning, and the case skips itself (runs, and skips itself) |
| CNET/4.29 | (d) out of scope | TC-CNET-4.29 verifies Wi-Fi ConnectNetwork with per-device credentials; out of scope with Wi-Fi commissioning, and the case skips itself once given the endpoint its matcher needs (runs, and skips itself) |
| CNET/4.4 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.5 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNET/4.6 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNET/4.9 | (b) not supported | not applicable: the case's PICS `CNET.S.F00` is false for the reference DUT |
| CNETWIFI/4.1 | (a) defect | TC-CNET-4.1 reads Networks for the one connected network the DUT was commissioned onto; cluster/core's WiFiNetworkCommissioning does not persist its committed networks, so a restarted daemon reports none (chip persists it: NetworkCommissioningWiFiDriver.cpp:77-118, :135-138) |
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
| CNETWIFI/4.23 | (c) harness | TC-CNET-4.23 expects ConnectNetwork to an incorrect SSID or passphrase to fail; the TESTING-ONLY simulated driver connects to any SSID (examples/reference-bridge/wifisim.go ConnectNetwork), so only a real driver can answer it |
| CNETWIFI/4.25 | (b) not supported | TC-CNET-4.25 verifies Wi-Fi per-device credentials (NetworkCommissioning PDC); the module serves no PDC, and the case skips itself on an endpoint without it (runs, and skips itself) |
| CNETWIFI/4.26 | (b) not supported | TC-CNET-4.26 verifies QueryIdentity (per-device credentials); no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.27 | (b) not supported | TC-CNET-4.27 verifies Wi-Fi network client identities (per-device credentials); no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.29 | (b) not supported | TC-CNET-4.29 verifies ConnectNetwork with per-device credentials; no PDC, the case skips itself (runs, and skips itself) |
| CNETWIFI/4.3 | (b) not supported | not applicable: the case's PICS `CNET.S.F02` is false for the reference DUT |
| CNETWIFI/4.4 | (a) defect | TC-CNET-4.4 scans for the connected network's SSID; after a restart the daemon has no network (WiFiNetworkCommissioning does not persist its committed networks, as for 4.1) |
| CNETWIFI/4.6 | (b) not supported | not applicable: the case's PICS `CNET.S.F01` is false for the reference DUT |
| CNETWIFI/4.9 | (a) defect | TC-CNET-4.9 removes and re-adds the connected network; after a restart the daemon has none (WiFiNetworkCommissioning does not persist its committed networks, as for 4.1) |
| CONTENTLAUNCHER/10.10 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.A0003` is false for the reference DUT |
| CONTENTLAUNCHER/10.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CONTENTLAUNCHER/10.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| CONTENTLAUNCHER/10.8 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.F05` is false for the reference DUT |
| CONTENTLAUNCHER/10.9 | (b) not supported | not applicable: the case's PICS `CONTENTLAUNCHER.S & CONTENTLAUNCHER.S.F06 & CONTENTLAUNCHER.S.C01.Rsp` is false for the reference DUT |
| DA/1.10 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| DA/1.11 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| DA/1.12 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C00.Rsp & OPCREDS.S.C01.Tx & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| DA/1.13 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| DA/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DA/1.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DA/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DA/1.8 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DA/1.9 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| DD/1.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.11 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.8 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/1.9 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.11 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER & MCORE.DD.QR_COMMISSIONING & MCORE.DD.STANDARD_COMM_FLOW` is false for the reference DUT |
| DD/3.12 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.13 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.15 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.16 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.17 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.18 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.19 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.20 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.21 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.22 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DD/3.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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
| DRLK/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ECOINFO/2.1 | (c) harness | TC_ECOINFO_2_1.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT |
| ECOINFO/2.2 | (c) harness | TC_ECOINFO_2_2.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT |
| EEVSE/2.3 | (a) defect | TC-EEVSE-2.3 step 19 (eleven ChargingTargets in one day) expects RESOURCE_EXHAUSTED; the generated SetTargets decoder refuses the list at its `max 10` constraint with CONSTRAINT_ERROR before the server's own check (cluster/energy/evse.go, evseMaxTargetsPerDay) can answer, as matter.js does (CommandInvokeResponse.ts requestTlv.validate). notes/parity/matter_behaviour_findings.md, Certification harness, TC-EEVSE-2.3 |
| FAN/2.3 | (b) not supported | not applicable: the case's PICS `FAN.S.F02` is false for the reference DUT |
| FAN/2.4 | (b) not supported | not applicable: the case's PICS `FAN.S.F03` is false for the reference DUT |
| FAN/2.5 | (b) not supported | not applicable: the case's PICS `FAN.S & FAN.S.F05` is false for the reference DUT |
| FAN/3.6 | (b) not supported | not applicable: the case's PICS `FAN.S & FAN.S.F05` is false for the reference DUT |
| FAN/4.1 | (b) not supported | not applicable: the case's PICS `FAN.S & OO.S` is false for the reference DUT |
| FLW/2.2 | (c) harness | TC-FLW-2.2 has an operator change the measured value between two reads (a UserPrompt under FLW.M.FlowChange); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| G/2.2 | (c) harness | TC-G-2.2 step 7a (the Groups revision 4 path) writes MaxGroupsPerFabric+1 GroupKeyMap entries and expects Success; chip's own GroupDataProviderImpl::SetGroupKeyAt refuses the entry beyond MaxGroupsPerFabric at the image commit (src/credentials/GroupDataProviderImpl.cpp:1492, CHIP_ERROR_INVALID_LIST_LENGTH), as does matter.js GroupKeyManagementServer #validateGroupKeyMap (ResourceExhausted) and this module |
| G/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| G/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| GRPKEY/5.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| I/2.4 | (c) harness | TC-I-2.4 checks the Q-quality reporting of IdentifyTime added in Matter 1.4.2 against an expectation chip has not merged yet (connectedhomeip#42128); matter.js excludes it until then (test/app-slow/I.test.ts) |
| I/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| ICDM/2.1 | (b) not supported | not applicable: the case's PICS `ICDM.S` is false for the reference DUT |
| ICDM/3.1 | (b) not supported | not applicable: the case's PICS `ICDM.S` is false for the reference DUT |
| ICDM/3.2 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/3.3 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/3.4 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F00` is false for the reference DUT |
| ICDM/4.1 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F02` is false for the reference DUT |
| ICDM/5.1 | (b) not supported | not applicable: the case's PICS `ICDM.S & ICDM.S.F02` is false for the reference DUT |
| IDM/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/4.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/4.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/5.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/6.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/7.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| IDM/8.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| KEYPADINPUT/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| LOWPOWER/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| LVL/7.1 | (b) not supported | not applicable: the case's PICS `LVL.S & LVL.S.F02` is false for the reference DUT |
| LVL/8.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAINPUT/3.14 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAINPUT/3.15 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAINPUT/3.16 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAINPUT/3.17 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAPLAYBACK/6.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAPLAYBACK/6.11 | (b) not supported | not applicable: the case's PICS `MEDIAPLAYBACK.S & MEDIAPLAYBACK.S.A000b` is false for the reference DUT |
| MEDIAPLAYBACK/6.12 | (b) not supported | not applicable: the case's PICS `MEDIAPLAYBACK.S & MEDIAPLAYBACK.S.A000c` is false for the reference DUT |
| MEDIAPLAYBACK/6.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAPLAYBACK/6.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MEDIAPLAYBACK/6.8 | (c) harness | TC-MEDIAPLAYBACK-6.8 step 1 (and the reads after DeactivateTextTrack) expects ActiveTextTrack to read a TrackStruct with an empty ID and a null TrackAttributes (Test_TC_MEDIAPLAYBACK_6_8.yaml:26-31), the tv-app's empty track under chip's revision 3; the model at the pin makes TrackAttributes mandatory and not nullable (matter.js media-playback.element.ts:206-208) and ActiveTextTrack itself nullable (:71-74), so the daemon reads null while no text track is active |
| MOD/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| MOD/2.3 | (b) not supported | not applicable: the case's PICS `MOD.S & S.S` is false for the reference DUT |
| MOD/3.1 | (b) not supported | not applicable: the case's PICS `MOD.S.A0005` is false for the reference DUT |
| MOD/3.2 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004` is false for the reference DUT |
| MOD/3.3 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004` is false for the reference DUT |
| MOD/3.4 | (b) not supported | not applicable: the case's PICS `MOD.S.A0004 & MOD.S.A0005 & OO.S.A4003` is false for the reference DUT |
| OO/2.4 | (c) harness | the case reboots the DUT and expects the light's StartUpOnOff / StartUpColorTemperatureMireds applied; matter.js applies the start-up attributes only on an endpoint no Aggregator owns (OnOffServer.ts and ColorControlServer.ts initialize: !endpoint.ownerOfType(AggregatorEndpoint)) — a bridge restart is not the bridged device's power cycle — and every light here is bridged |
| OO/2.6 | (b) not supported | not applicable: the case's PICS `OO.S & OO.S.F02` is false for the reference DUT |
| OO/2.8 | (c) harness | TC-OO-2.8 expects OnTime and OffWaitTime reported only on a change larger than 10 or to 0 — chip's OnOffLightingCluster.cpp SetOnTime/SetOffWaitTime (kValueDeltaReportTrigger), an SDK choice: Matter 1.6.1 gives neither attribute the Q quality (matter.js on-off.element.ts), and matter.js OnOffServer reports every countdown tick, as the daemon now does |
| OO/3.2.Simulated | (b) not supported | not applicable: the case's PICS `OO.C` is false for the reference DUT |
| OPCREDS/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| OPCREDS/3.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| OPCREDS/3.9 | (b) not supported | not applicable: the case's PICS `OPCREDS.S & OPCREDS.S.F00 & OPCREDS.S.C02.Rsp & OPCREDS.S.C03.Tx` is false for the reference DUT |
| OVENOPSTATE/2.6 | (b) not supported | not applicable: the case's PICS `OVENOPSTATE.S & OVENOPSTATE.S.A0002` is false for the reference DUT |
| PS/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| PS/2.3 | (b) not supported | not applicable: the case's PICS `PWRTL.S` is false for the reference DUT |
| PSCFG/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| REFALM/2.3 | (b) not supported | not applicable: the case's PICS `REFALM.S & REFALM.S.M.LocalSuppress` is false for the reference DUT |
| RH/2.2 | (c) harness | TC-RH-2.2 has an operator change the measured value between two reads (a UserPrompt under RH.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| S/2.3 | (b) not supported | not applicable: the case's PICS `S` is false for the reference DUT |
| S/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/1.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/2.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/3.5 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER` is false for the reference DUT |
| SC/4.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/4.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/4.6 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONER & MCORE.DD.COMM_DISCOVERY` is false for the reference DUT |
| SC/4.7 | (b) not supported | not applicable: the case's PICS `MCORE.ROLE.COMMISSIONEE & MCORE.DD.COMM_DISCOVERY` is false for the reference DUT |
| SC/4.8 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/4.9 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/5.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SC/6.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SWTCH/2.2 | (b) not supported | TC-SWTCH-2.2 runs only on a latching switch (PICS SWTCH.S.F00=0: the daemon's switch is momentary) (runs, and skips itself) |
| SWTCH/2.6 | (b) not supported | TC-SWTCH-2.6 runs only with ActionSwitch (PICS SWTCH.S.F05=0): the daemon's button serves MomentarySwitchRelease, which ActionSwitch excludes (MSR conformance "[MS & !AS]", switch.element.ts) (runs, and skips itself) |
| SWTCH/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| TCCM/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| TCTL/2.3 | (b) not supported | not applicable: the case's PICS `TCTL.S & TCTL.S.F01` is false for the reference DUT |
| TCTL/3.3 | (b) not supported | not applicable: the case's PICS `TCTL.S & TCTL.S.F01` is false for the reference DUT |
| TGTNAV/8.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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
| TMP/2.2 | (c) harness | TC-TMP-2.2 has an operator change the measured value between two reads (a UserPrompt under TMP.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in |
| TSTAT/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| TSTAT/4.3 | (b) not supported | not applicable: the case's PICS `TSTAT.S & TSTAT.S.F08 & TSTAT.S.F0a` is false for the reference DUT |
| TSTAT/4.4 | (b) not supported | not applicable: the case's PICS `TSTAT.S & TSTAT.S.F0b` is false for the reference DUT |
| VALCC/3.2 | (b) not supported | TC-VALCC-3.2 runs only with the Level feature (PICS VALCC.S.F01=0: the daemon's valve is open/closed) (runs, and skips itself) |
| VALCC/3.3 | (b) not supported | TC-VALCC-3.3 runs only with DefaultOpenLevel (PICS VALCC.S.A0006=0) (runs, and skips itself) |
| WNCV/6.1.Simulated | (b) not supported | not applicable: the case's PICS `WNCV.C` is false for the reference DUT |
| WNCV/7.1.Simulated | (b) not supported | not applicable: the case's PICS `WNCV.C` is false for the reference DUT |

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
| PWRTL, ILL, PRS, DISHM | the module serves the cluster (`cluster/measurement` PowerTopology, Illuminance and Pressure; `cluster/modebase` DishwasherMode), but the reference daemon exposes no endpoint with it and the harness does not run the family | (c) not yet run |
| EEM, EPM | the module serves both clusters (`cluster/measurement`) and the reference daemon's ElectricalMeter carries them, but the harness does not run the families | (c) not yet run |
| CONCON, MESSAGES | the module serves ContentControl (`cluster/media`) and Messages (`cluster/core`), but chip has no case of either family at the harness pin | (c) no case |
| ICDM, ICDB | not applicable to the reference daemon: it is mains-powered and sets no `node.Config.ICD`, so it serves no ICD Management (`ICDM.S` is false in its generated PICS). Applicable to a host that sets `Config.ICD`: the module then serves ICD Management and the Check-In protocol after matter.js ([ICD guide](./guides/icd.md)); the harness does not run the families against such a node | (b) not supported |
| SC_TC, the BLE and Thread cases of DD and SC | Bluetooth, Thread | (d) out of scope |
| CNET Thread cases | the device side has no Thread radio; the Thread NetworkCommissioning variant over a border router (`core.ThreadNetworkCommissioning`, ADR 0027) is not on the reference daemon | (c) not yet run |
| TBRM | the module serves ThreadBorderRouterManagement over an OTBR (`cluster/threadbr`, ADR 0027) and the reference daemon's `--otbr` adds the endpoint, but the harness does not run the family against a daemon with a virtual OTBR yet (`.github/workflows/otbr-virtual.yml` builds one, dispatch-only) | (c) not yet run |
| JFADMIN, JFDS | the module serves JointFabricAdministrator and JointFabricDatastore after connectedhomeip (`cluster/jointfabric`, wave 26-D; report 26-D cites `TC_JFDS_2_2` and `TC_JFADMIN_2_2`), but the reference daemon composes neither and the harness does not run the families | (c) not yet run |
| TSTAT 4.5 (MatterScheduleConfiguration) | the module serves MSCH behind `surface.Policies.Extensions("BD-chip-Thermostat-Schedules")` and the reference daemon enables it with `--thermostat-schedules`, but `TC_TSTAT_4_5.py` is absent at the harness commit `6170af84` (present in connectedhomeip from 2026-10-23); lifting `CHIP_TEST_IMAGE` is a separate owner decision | (c) not yet run |
| the camera, TLS, network-infrastructure and AmbientContextSensing families | the module serves the seven camera clusters, the two TLS clusters (`cluster/camera`, `cluster/tls`, waves 26-A1 to 26-A3), WiFiNetworkManagement and ThreadNetworkDirectory (`cluster/netinfra`, wave 26-B) and AmbientContextSensing (`cluster/sensing`, wave 26-C), but the reference daemon composes none of them and the harness does not run their families | (c) not yet run |
| the TZ, NTPC, NTPS and TSC cases of TIMESYNC | the module serves the four features after connectedhomeip since wave 27-B (`node.Config.TimeSynchronization`); the reference daemon turns none on, so the cases stay not applicable in the status block above | (c) not yet run |

CNET runs twice. The `CNET` row is the certified default composition, whose
NetworkCommissioning is Ethernet-only; its Wi-Fi cases are not applicable or
excluded there. The `CNETWIFI` row runs the same cases against the reference
daemon with `--wifi-sim`: NetworkCommissioning with the Wi-Fi feature
(`CNET.S.F00=1`) over a simulated driver (TESTING ONLY), with PICS generated
from that daemon (`internal/chiptool/testdata/pics-wifi-sim/`). Its
commissioned snapshots carry the Wi-Fi network a test harness commissioning
over BLE-Wi-Fi would have configured. Cases that need a real driver, a second
access point or per-device credentials stay classified in the table above.

Some families run but have nothing the daemon can be tested on. The root's
IcdManagement is not mounted (`node/root.go` buildRoot says why), so the
ICDM cases are not applicable through the device's own PICS (`ICDM.S=0`).
The OTA Software Update Requestor is mounted since #49 (ADR 0019) and its
automated SU cases run. The image lists
the DLOG case, the BIND cases and the SU YAML cases as manual, so the
status block records them as class (c); on substance BIND tests the binding
client (`BIND.C`, a controller-side role, (d) — proven in-repo since wave 21), and the SU provider cases an
OTA provider (b — built in wave 23, ADR 0021; the cases stay manual YAML, the
in-repo proof is the module's requestor downloading from the module's
provider; the DLOG case stays manual at the pinned image although the
server now transfers over BDX). BDX shipped with the OTA requestor in `v0.3.0` (ADR 0019) as the
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
carries. The image lists the media cases whose every step is disabled as
manual, recorded as class (c); TC-MEDIAPLAYBACK-6.8 expects an inactive
text track to read as a track with an empty ID, which the model at the pin
cannot carry (null is its inactive track), and is excluded as class (c);
the Python media cases gate on PICS codes the model at the pin
does not define (MediaPlayback's AvailableCommands and ContentInfo,
ContentLauncher's ContentReplication and Presets features, AccountLogin's
OAuth feature) and are not applicable. Nothing here plays media: a
product's host implements the ports over its own player.

## What a product owner must supply

A library cannot be certified; a product can. Everything below belongs to the
product and is out of this module's reach:

- **Vendor and product identity**: a CSA-assigned Vendor ID and the product's
  Product ID. The reference daemon uses the test vendor ID 0xFFF1.
- **Device attestation**: a production DAC and PAI chained to a PAA on the
  DCL, provisioned securely per device. The reference daemon uses CHIP's test
  attestation chain and a locally built test Certification Declaration
  (`secure/attestation`), which no ecosystem accepts outside development.
- **Certification Declaration** issued by the CSA for the certified product,
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
[`notes/parity/matter_behaviour_findings.md`](../notes/parity/matter_behaviour_findings.md)
under *Certification harness — open certifiability findings*; each excluded
case in the status block names its finding.
