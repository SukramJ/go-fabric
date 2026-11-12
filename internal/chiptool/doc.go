// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

// Package chiptool is this module's real-controller guard: it drives the
// CSA reference commissioner (chip-tool) and the CSA certification test
// harness (the YAML runner and the Python TC_* cases) against the reference
// daemon in examples/reference-bridge.
//
// Everything else in this module is tested against itself. A Matter stack
// that only ever talks to its own test client can be wrong in ways no
// in-tree test can see, because both sides share the misreading. This suite
// is the one place where the other side of the wire is somebody else's code.
//
// Certification is not pursued by this project and nothing built on it may
// be described as certified; certifiability is a goal
// (docs/adr/0011-certifiability-is-a-goal.md). The certification families
// here are how it is held, and docs/certifiability.md — checked against the
// family table by TestCertifiabilityDocument — is the status.
//
// The `chiptool` build tag keeps the harness-driven tests out of
// `make test`, `make race` and the default `go test ./...`. Five entry
// points:
//
//   - TestChipToolCommissionsReferenceBridge — commission, read, invoke,
//     tear down (`make chiptool-test`);
//   - TestChipToolSuite — the data-model sweep and the further legs over one
//     interactive chip-tool session (`make chiptool-test`);
//   - TestChipCertificationFamilies — the CSA certification families, run
//     by family the way matter.js runs them (`make chiptool-families`,
//     narrowed with GOFABRIC_CHIP_FAMILIES=IDM,ACL,…, further to single
//     cases with GOFABRIC_CHIP_CASES=FAN/3.1,… and repeated with
//     GOFABRIC_CHIP_REPEAT=20 — a narrowed run cannot -update-chip-cases);
//   - TestGoCertificationCases — the Go-TH leg below (`make chiptool-gocert`,
//     part of `make chiptool-test`, narrowed with GOFABRIC_GOCERT=IDM,SC/4.2);
//   - TestChipToolBLEWiFi — chip-tool-ble commissions examples/ble-wifi-light
//     over BLE onto Wi-Fi under chip's BlueZ and wpa_supplicant mocks, with
//     all-clusters-app-ble as the control (`make chiptool-ble-wifi`,
//     blewifi_test.go; not part of `make chiptool-test`).
//
// # The harness
//
// By default the suite runs chip-tool and the Python harness inside the
// module's own harness image (ghcr.io/sukramj/go-fabric-chip, amd64: matter.js's
// CHIP image plus chip's jf-admin-app, jf-control-app and Push AV server,
// internal/chiptool/image), pinned by digest in the Makefile's
// CHIP_TEST_IMAGE; the CHIP commit it was built from is logged with every
// run. The container shares the host network and
// the host's avahi over /run/dbus, so the only host prerequisites are Linux,
// Docker and a running avahi-daemon. `make chiptool-image` pulls the image
// ahead of time; the suite pulls it on first use otherwise.
//
// A host chip-tool is the fallback, for the commissioning test and the
// chip-tool suite only: $GOFABRIC_CHIPTOOL_BIN, then ./bin/chip-tool (`make
// chiptool-extract`, arm64), then chip-tool on PATH (the snap). Setting
// $GOFABRIC_CHIPTOOL_BIN or $GOFABRIC_CHIP_HARNESS=host selects it; the
// families then skip, naming why. A snap-confined chip-tool can only write
// under $HOME/snap/chip-tool/common and sees a private /tmp, so the suite
// puts its key-value stores there by itself; $GOFABRIC_CHIPTOOL_KVS_BASE
// overrides that.
//
// `make chiptool-setup` makes a sparse connectedhomeip checkout at
// ../connectedhomeip — the source CLAUDE.md sends you to when chip-tool or a
// CHIP test case behaves unexpectedly.
//
// # The families and their PICS
//
// familyrun_test.go runs each family of families_table_test.go in full; a
// case left out is a gap with its ADR 0011 class and reason, reported as a
// skip naming both. What a passing case executed is pinned in
// testdata/chip-cases.golden.json (steps run and skipped), so a PICS edit
// that quietly turns a case into a no-op fails it.
//
// The PICS is generated from the device: testdata/gen_pics.py reads the
// commissioned daemon with one wildcard read and writes a slice per endpoint
// (testdata/pics/ep<N>.txt) with CHIP's own derivation helpers. Each case
// runs with CHIP's ci-pics-values, the hand declarations a device cannot
// report (testdata/reference-bridge.pics, which may not touch a generated
// code), and the slice of the endpoint under test. TC-IDM-10.4 holds every
// slice against the device.
//
// The reference daemon offers the out-of-band controls the CHIP cases
// expect, off by default: a named pipe for CHIP-style JSON commands
// (--app-pipe) and General Diagnostics TestEventTrigger (--enable-key), plus
// the harness's restart-flag protocol, served by monitorRestartFlag, and
// the YAML runner's accessory protocol (SystemCommands Reboot and
// FactoryReset), served on a loopback port by accessory_test.go the way
// matter.js serves it.
// Every daemon the suite starts also carries --wifi, a simulated Wi-Fi link
// behind WiFiNetworkDiagnostics on the root for the DGWIFI family, and
// --media, a video player and a content app after chip's tv-app for the
// media families: the PICS slices are generated from one daemon, so every
// case runs against the same device.
//
// # The Go-TH leg
//
// matter.js automated 68 certification cases in
// support/chip-testing/test/cert/TC-*.test.ts, and in most of them its own
// controller is the DUT and a device app the TH
// (support/chip-testing/README.md:112-118): the controller halves of the
// CHIP test plans, which CHIP's harness has as manual cases and the
// families therefore leave as "manual" rows. gocert_test.go reads those
// files mirrored: the reference daemon is the DUT, the module's own
// controller (controller.New, one per TS controller role) performs the
// TS file's controller actions as TH, and the TS file's TH-side
// expectations — what the device answers, reports, advertises and does —
// are what the daemon has to satisfy. Matter is symmetric enough that a
// plan's controller step ("the DUT sends a Read Request with these
// paths") is a device step when read from the other side ("the device
// answers exactly these paths"); where the TS checks the TH's log for what
// arrived, the mirrored step checks the daemon's answer and its effect (a
// read-back, a report, a DNS-SD record).
//
// Each case (goCertCase) cites its TS file and every step the line of its
// .step( call; every step records pass, fail or skipped with its reason,
// as matter.js's StepVerdict does. A step's PICS is the TS's, mirrored
// onto the server side (mirrorPICS: X.C.Cnn.Tx → X.S.Cnn.Rsp, X.C → X.S,
// MCORE.IDM.C… → MCORE.IDM.S, MCORE.ROLE.COMMISSIONER → COMMISSIONEE) and
// evaluated against the committed PICS of the daemon (the hand
// declarations and the generated slices, held to the daemon's endpoints by
// checkTopology); a step whose PICS is false is recorded skipped with the
// expression, never passed. The TS addresses its all-clusters app's
// endpoint 1; the mirrored step addresses the daemon's first endpoint whose
// PICS hold, and a substitution of an attribute the daemon does not serve
// is named at the step. The out-of-band controls of the TS (matter.js's
// backchannel) are the daemon's app pipe. The leg needs no harness image;
// its counts are pinned in testdata/chip-cases.golden.json under goCert,
// and TestCertifiabilityDocument renders those cases as passed, "Go
// controller as TH, after matter.js TC-X.test.ts".
//
// The same leg runs cases in matter.js's own direction (goCertCase role
// roleDUT, phase 29b): the module's controller is the DUT and the daemon
// the TH device the TS file names, with the flags and app-pipe commands
// matter.js's TH apps have (--icd-lit for its lit-icd app, --bridge-sim
// for its bridge test instance and that app's backchannel). A DUT case's
// PICS are the controller's own declarations (goTHPICS), evaluated as
// stated, and a code the controller declares nothing about fails the step
// rather than reading the daemon's value. matter.js proves its DUT by the
// TH device's log; a DUT step names its evidence instead — the DUT
// controller's return value or error, a state read on the daemon, or a
// daemon log line. The onboarding-payload DD cases (gocert_dddut_test.go,
// payloads built bit by bit in gocert_payload_test.go as matter.js's
// harness builds them), the ICD client cases (gocert_icd_test.go) and the
// bridge client case TC-BR-4 (gocert_br_test.go: the DUT's own image of the
// bridge, client.Node.Endpoints and Cluster.Value, taken before any read
// and followed on its default subscription alone) run this way; their counts carry role "dut" in the golden file, and
// TestCertifiabilityDocument renders them under "Controller role", "Go
// controller as DUT, after matter.js TC-X.test.ts", passed in their family.
// The cases 29b does not run carry their reason there (controllerRoleCases).
//
// Some of the cases run the device as DUT in matter.js too (goCertCase
// native): the BDX receiver and SU requestor cases (gocert_bdx_test.go,
// gocert_su_test.go), where the TS's TH is a controller serving an OTA
// image, the BIND cases (gocert_bind_test.go), whose DUT is a light
// switch — the daemon with --light-switch, triggered through the app pipe's
// SendOnOffToBindings —, and the TCP cases (gocert_tcp_test.go), where the
// Go TH holds a session over TCP with the daemon and severs it
// (exchange.Session.Channel). Their PICS are evaluated as stated. The Go TH serves
// OTA images from its own node (controller.OTAConfig) and scripts the
// provider's answers as matter.js's cert adapter does
// (softwareupdate.Config ScriptQueryImage, ScriptApplyUpdate,
// OnProviderExchange); it reads each BDX message off its end of the
// transfer (bdx.ServerSession.OnMessage). The codes of the roles the Go TH
// takes (provider, BDX sender, administrator, QR commissioner, ICD client) are its own
// declarations (goTHPICS), as matter.js declares its controller's, and a
// flagged composition's codes are the case's (goCertCase.declare). A step
// matter.js marks longRunning is skipped unless GOFABRIC_GOCERT_LONG=1; such
// a run checks no golden counts. A step that fails with a daemon defect
// recorded in notes/parity/matter_behaviour_findings.md carries its id
// (goStep.finding): the case passes with the finding listed, and fails once
// the step passes.
//
// Nothing is skipped silently on a machine that has the prerequisites: a
// missing harness skips with the command that provides it, and a missing
// reference-daemon binary fails loudly with the command to run. The
// chip-tool is the pinned image's, so every cluster and command the suite
// drives is there.
package chiptool
