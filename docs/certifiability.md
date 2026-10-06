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
- **Families** run in full, the way matter.js declares its own
  (`support/chip-testing/test/core`, `test/app-*`). A case that is not run is
  a gap of exactly one class:

  | Class | Meaning | What follows |
  | --- | --- | --- |
  | (a) defect | go-fabric misbehaves or lacks mandatory behaviour | fixed per matter.js, or an open finding carrying the TC id |
  | (b) not supported | optional per spec for the device types exposed, and declared so in PICS | the PICS line is the record |
  | (c) harness | the test case or the environment is at fault | the evidence is cited |
  | (d) out of scope | Bluetooth, Thread, Wi-Fi commissioning, controller role | the scope decision is named |

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
difference.

## PICS

The PICS a case runs with is generated from the device, not written for the
cases:

1. `internal/chiptool/testdata/gen_pics.py` reads the commissioned daemon with
   one wildcard read and writes a slice per endpoint
   (`internal/chiptool/testdata/pics/ep<N>.txt`) with CHIP's own derivation
   helpers (`matter/testing/pics.py`): every server-side cluster, attribute,
   command, feature and event code of every spec cluster, 0 or 1, plus the
   derivable MCORE codes (commissionee, bridge, OTA roles, multi-endpoint
   groups). Mandatory events follow from spec conformance.
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
| SC | 34 | 9 | 0 | 0 | 10 | 15 | 0 |
| SM | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| TIMESYNC | 14 | 3 | 0 | 0 | 11 | 0 | 0 |
| DLOG | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| ICDM | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| SU | 13 | 0 | 0 | 0 | 3 | 10 | 0 |
| BIND | 3 | 0 | 0 | 0 | 0 | 3 | 0 |
| ACFREMON | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| BOOL | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| CLCTRL | 12 | 5 | 0 | 0 | 7 | 0 | 0 |
| CC | 31 | 21 | 0 | 0 | 6 | 4 | 0 |
| DRLK | 14 | 7 | 0 | 0 | 4 | 3 | 0 |
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
| RVCRUNM | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| S | 7 | 5 | 0 | 0 | 1 | 1 | 0 |
| SMOKECO | 7 | 7 | 0 | 0 | 0 | 0 | 0 |
| SWTCH | 5 | 1 | 0 | 0 | 3 | 1 | 0 |
| TMP | 2 | 1 | 0 | 0 | 0 | 1 | 0 |
| TSTAT | 6 | 3 | 0 | 0 | 2 | 1 | 0 |
| VALCC | 10 | 8 | 0 | 0 | 2 | 0 | 0 |
| WNCV | 17 | 15 | 0 | 0 | 2 | 0 | 0 |
| **all** | **464** | **237** | **0** | **0** | **107** | **115** | **5** |

### Cases not run

| Case | Class | Reason |
| --- | --- | --- |
| ACL/2.11 | (b) not supported | not applicable: the case's PICS `ACL.S.F01` is false for the reference DUT |
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
| CLCTRL/3.1 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F06` is false for the reference DUT |
| CLCTRL/4.2 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F01` is false for the reference DUT |
| CLCTRL/4.4 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.A0000` is false for the reference DUT |
| CLCTRL/7.1 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F00 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.2 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.F01 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.3 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.C03.Rsp` is false for the reference DUT |
| CLCTRL/7.4 | (b) not supported | not applicable: the case's PICS `CLCTRL.S & CLCTRL.S.A0000 & CLCTRL.S.C03.Rsp` is false for the reference DUT |
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
| DESC/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DGGEN/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DLOG/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DRLK/2.10 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| DRLK/2.11 | (b) not supported | not applicable: the case's PICS `DRLK.S & DRLK.S.F00 & DRLK.S.F01 & DRLK.S.F02` is false for the reference DUT |
| DRLK/2.13 | (b) not supported | not applicable: the case's PICS `DRLK.S.F0d` is false for the reference DUT |
| DRLK/2.5 | (b) not supported | not applicable: the case's PICS `DRLK.S & DRLK.S.F04` is false for the reference DUT |
| DRLK/2.6 | (c) harness | TC-DRLK-2.6 gates every step on the Year Day Schedule feature (PICS DRLK.S.F0a=0 here) except its final "Cleanup the created user" ClearUser, which has no PICS gate and fails on a lock without the User feature (DRLK.S.F08=0) |
| DRLK/2.7 | (b) not supported | not applicable: the case's PICS `DRLK.S & DRLK.S.F08` is false for the reference DUT |
| DRLK/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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
| LVL/7.1 | (b) not supported | not applicable: the case's PICS `LVL.S & LVL.S.F02` is false for the reference DUT |
| LVL/8.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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
| PS/2.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| PS/2.3 | (b) not supported | not applicable: the case's PICS `PWRTL.S` is false for the reference DUT |
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
| SC/8.1 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.2 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.3 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.4 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.5 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.6 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SC/8.7 | (b) not supported | not applicable: the case's PICS `MCORE.SC.S.TCP` is false for the reference DUT |
| SU/1.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.2 | (b) not supported | not applicable: the case's PICS `MCORE.OTA.Requestor` is false for the reference DUT |
| SU/2.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.4 | (b) not supported | not applicable: the case's PICS `MCORE.OTA.Requestor` is false for the reference DUT |
| SU/2.5 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.6 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/2.7 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.1 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.3 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/3.4 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
| SU/4.1 | (b) not supported | not applicable: the case's PICS `MCORE.OTA.Requestor` is false for the reference DUT |
| SWTCH/2.2 | (b) not supported | TC-SWTCH-2.2 runs only on a latching switch (PICS SWTCH.S.F00=0: the daemon's switch is momentary) (runs, and skips itself) |
| SWTCH/2.5 | (b) not supported | TC-SWTCH-2.5 runs only with MomentarySwitchMultiPress (PICS SWTCH.S.F04=0) (runs, and skips itself) |
| SWTCH/2.6 | (b) not supported | TC-SWTCH-2.6 runs only with MomentarySwitchMultiPress and ActionSwitch (PICS SWTCH.S.F04=0, SWTCH.S.F05=0) (runs, and skips itself) |
| SWTCH/3.2 | (c) harness | a manual case: the CHIP test plan has a test-lab operator perform it, there is nothing to automate |
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

| Family | Why not | Class |
| --- | --- | --- |
| PWRTL, LTIME, LUNIT, LCFG, ULABEL, FLABEL, and the measurement, energy, media and appliance clusters matter.js runs | no server in this module | (b) not supported |
| ICDB | the module is not an ICD | (b) not supported |
| SC_TC, the BLE and Thread cases of DD and SC | Bluetooth, Thread | (d) out of scope |
| CNET Wi-Fi and Thread cases | NetworkCommissioning is Ethernet-only | (d) out of scope |

Some families run but have nothing the daemon can be tested on. The root's
IcdManagement and OTA Software Update Requestor are not mounted
(`examples/reference-bridge/wiring.go` buildRootClusters says why), so the
ICDM cases and the SU requestor cases are not applicable through the
device's own PICS (`ICDM.S=0`, `MCORE.OTA.Requestor=0`). The image lists
the DLOG case, the BIND cases and the SU YAML cases as manual, so the
status block records them as class (c); on substance BIND tests the binding
client (`BIND.C`, a controller-side role, (d)), the SU provider cases an OTA
provider (b), and DLOG and the SU requestor cases need BDX, which the module
does not implement (`BD-chip-DiagLogs-NoBDX`).

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
