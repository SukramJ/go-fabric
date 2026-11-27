// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The tests in this file are the guard on the COMMITTED register: the
// snapshot is the pin's, every snapshot path has exactly one entry, every Go
// test an entry names exists, a ported or partial mapping was checked against
// the blob now pinned (an exclusion too), the page is what rendering produces, and the number of
// planned files sits exactly on its ratchet. None of them needs a matter.js
// clone; regenerating the snapshot (make testparity-snapshot) is a pin lift's
// job, and its diff is what tells the next mapping check where to look.
package main

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testparityPlannedRatchet is the number of matter.js test files the
// committed register lists as planned: neither ported, nor in part, nor
// excluded with a class.
//
// It is an exact ratchet, checked in both directions on purpose, like
// reachabilityUnreachedRatchet. Growth has to be justified in the commit that
// raises it — a pin lift that brings new upstream test files does, and says
// so; a DROP has to be locked in by lowering it, so a file ported once cannot
// slide back to planned under a ceiling that was never tightened.
//
// Set to 134 by the initial fill (brief BE) at matter.js 862ad75f: 324 test
// files, every one decided from the seed read and then checked against its
// describe/it names and the Go tests' Test and t.Run names. Phase 16 brings
// the protocol/test and node/test/node share of it to zero, milestone 3 the
// node/test/behaviors share. Phase 33 widened the roots to
// packages/general/test, packages/model/test, packages/nodejs/test and
// packages/types/test (192 files) and decided every one of them in the
// same pull request, so the count stays 2. The db021da0 lift planned 18
// new files on its waves P1–P4 (20); wave P1 ported its four
// (AdministratorCommissioningServerTest, CaseSessionCatsTest,
// DistinguishedNameTest, OperationalCertificateVerifyTest) as partial: 16.
// // Pin lift wave P2 (#431) ports AttestationCertificateProfileTest.ts (in
// part), CdSignersTest.ts and DeviceAttestationValidatorChipCdTest.ts as well: 13.
//
// Pin lift wave P3 (#432) decided seven of the lift's planned files (the
// GeneralDiagnostics, mDNS address-change and transport, label and
// localisation tests); CommissionableMdnsScannerWindowChangeTest.ts stays: 6.
// Pin lift wave P4 (#433) ports CommissioningClosedSessionReArmTest.ts,
// InitialContactScheduleTest.ts and VendorIdVerificationTest.ts, merged
// with wave P3: 3.
// Issue 441 (wave W7) ports CommissionableMdnsScannerWindowChangeTest.ts: 2.
// Pin lift b25ed960 (P0) plans the 20 new upstream test files a unit of the
// lift owns as a whole (eleven outside packages/node/test/node, nine in it),
// each entry naming its unit; the other eight new files are excluded: 22.
// Pin lift b25ed960 wave P1 decides two of them in part: ThermostatSuggestionsTest.ts
// (61 of 91 cases, P1-E) and SessionViewsTest.ts (the controller's seven rows,
// P1-A): 20.
// Pin lift b25ed960 wave P2 ports both files of unit P2-D, NocsrValidatorTest.ts
// and CsrNonceCommissioningTest.ts: 18.
// Pin lift b25ed960 wave P2b ports StoredNodeLabelTest.ts (P2b-A),
// ClientSubscriptionHandlerTest.ts and PeerSubscriptionTest.ts (P2b-E), and
// decides ServerSubscriptionSessionTest.ts in part (P2b-B, the CASE-server
// interception cases missing): 14.
// Pin lift b25ed960 wave P3 decides its eight planned files: ported
// ZoneManagementServerTest.ts (P3-B), SchedulesAtomicWriteTest.ts (P3-A),
// CommissioningAttemptLimitTest.ts (P3-C), ClientRequireExistingSessionTest.ts,
// CommissioningFailsafeExpiryTest.ts and PaseBleChannelCloseTest.ts (P3-D);
// SchedulesConfigurationTest.ts and SchedulesLocalWriteTest.ts in part, their
// restarts on stored schedules missing (P3-A, coordinator decision C-7): 6.
// Pin lift b25ed960 wave P4 ports two: ThermostatEventsTest.ts (P4-A) and
// SecretRedactionTest.ts (P4-B): 4.
// Pin lift b25ed960 wave P5 ports OperationalCertificateUpdateTest.ts (P5-B,
// owner decision 293: 73 of 75 cases, two left): 3.
const testparityPlannedRatchet = 3

// testparityPortedRatchet is the number of ported entries, and
// testparityMissingRatchet the number of matter.js it cases the partial
// entries name as missing, summed (W6-test-07). The planned ratchet alone
// let a file leave planned for partial with one Go test, a ported entry
// drop to partial and a missing list grow without a guard firing; these two
// close that. Both are exact, in both directions, as the planned one is: a
// port that removes a missing case or completes a file lowers the missing
// count or raises the ported count in the same commit; a pin lift that adds
// upstream cases, or a re-check that finds a case not covered after all,
// raises the missing count and says why.
//
// Set at the W6 fix PR (review 2026-11) to 113 ported and 1140 missing
// (the review counted 1136; W6-test-08 moved four cases of FixMe-skipped
// anchors into the missing lists). Issue #362 lowered missing to 1130:
// the module's own mDNS responder made ten MdnsServerTest.ts and
// MdnsTest.ts cases portable. Phase 31 (the server tranche) took the 332
// cases of eleven entries out of missing — ported or left — and one
// ProtocolServiceTest.ts case; 16 stay missing with a named reason (the
// failing report sender, the no-MRP session and the mDNS scanner seams,
// the multi-message invoke response, gaps in the findings register):
// 121 ported, 813 missing. Phase 32 (the controller and protocol tranche)
// took 261 of the 312 cases of nine entries out of missing — ported or
// left — and completed four files; 51 stay missing with a named reason
// (the PR's report): 125 ported, 552 missing.
// The seam wave (issue #396) moves cases out of missing item by item: the
// five Presets observer cases (left, owner verdict), DiscoveryCancelTest.ts
// (three ported on a held PASE, one left), ClientInvokeTest.ts (one ported
// on a held connect, ten left), DecommissionTest.ts (five ported on a
// patched RemoveFabric, nine left), FailsafeCommissioningTest.ts's ACL
// failure (ported on a failing credential store) and IcdClientTest.ts (ten
// ported on the device's registration hook, restarts and a sleeping peer,
// five left, six stay missing) complete five files: 130 ported, 501 missing.
// Phase 33 (the general,
// model, nodejs and types roots) adds 192 files: 23 ported, 19 in part
// with 91 missing cases — each with a named reason on the page, the
// behaviour gaps among them findings-register rows (the float64 bounds,
// derived defaults, the FabricIndex a write keeps, an IS1 in a character
// string, copyHealth, the socket open) — 153 ported, 600 missing.
// T3-SW-1 (InvokeOptions' revision, DelayReportData and address override,
// interaction.Config.Network) ports eight ClientInvokeTest.ts cases and
// leaves beforeDispatch, completing the file: 154 ported, 591 missing.
// The follow-up wave's T3-SW-2 (the commissionable re-announcement on a
// closed PASE session) completes FailsafeCommissioningTest.ts: 155 ported,
// 589 missing. T3-SW-3 (the ICD wakefulness on the monotonic clock)
// completes IcdPeerWakefulnessTest.ts and ports IcdClientTest's wall-clock
// deadline, and PingAddress ports DecommissionTest's probe case out of left:
// 156 ported, 586 missing, 576 left.
// Phase 36: the administrator's basic window of
// CommissioningWindowLifetimeTest.ts ported on the Basic feature: 585 missing.
// Phase 36: OccupancySensingServerTest.ts ported on the OccupancyEvent
// feature: 157 ported, 584 missing.
// Phase 39: packages/nodejs-ble/test joins the roots for the BTP
// renegotiation; NobleBleChannelTest.ts ported (its 18 stall and ATT_MTU
// cases), ScanControlTest.ts excluded: 158 ported.
// Pin lift db021da0 wave P1: four planned files become partial with 53
// missing cases named (the DNS-SD advertisement reads of an
// administrator's window, the VVSC kind and issuer-DN cases of the
// certificate tests, the vendor-id DN attributes, the DER round trips):
// 1033 missing.
// Pin lift wave P2 (#431): CdSignersTest.ts and
// DeviceAttestationValidatorChipCdTest.ts ported, DeviceAttestationValidatorTest.ts
// completed (its five CD signer cases); the CD envelope cases of
// CertificationDeclarationTest.ts and fourteen lookupCdSigner cases of
// DclCertificateServiceTest.ts leave missing, the 23 distinguished-name
// cases of AttestationCertificateProfileTest.ts (#4880, wave P1) join it:
// merged with wave P1: 149 ported, 1024 missing.
// Pin lift wave P3 (#432): NetworkInterfaceChangesTest.ts, MdnsServerAddressChangeTest.ts,
// GeneralDiagnosticsNetworkInterfacesTest.ts, UserLabelServerTest.ts and FixedLabelServerTest.ts
// ported; 18 MdnsServerTest.ts/MdnsTest.ts goodbye cases out of missing, ten
// MdnsServerTransportTest.ts and five GeneralDiagnosticsServerTest.ts fault cases in,
// merged with waves P1 and P2: 154 ported, 1021 missing.
// Pin lift wave P4 (#433): CommissioningClosedSessionReArmTest.ts ported, ConnectionStateTest.ts's two no-address cases: 150 ported, 1022 missing.
// Pin lift wave P4 (#433): InputChunkTest.ts completed by the nine resolved-timestamp cases, ClientEventNotificationTest.ts's resolved delivery: 151 ported, 1012 missing.
// Pin lift wave P4 (#433): the TCP limit cases of matter.js #4793 in MessageChannelTest.ts, TcpChannelTest.ts, SessionParametersTest.ts and BdxTest.ts: 998 missing.
// Pin lift wave P4 (#433): InitialContactScheduleTest.ts ported, ClientConnectivityTest.ts's Sigma1 refresh: 152 ported, 997 missing.
// Pin lift wave P4 (#433): VendorIdVerificationTest.ts ported, the VVSC rows of OperationalCertificateVerifyTest.ts: 153 ported, 988 missing.
// Merged with wave P3: 158 ported, 985 missing.
// Issue 439 (wave W7): GeneralDiagnosticsServerTest.ts's five fault cases
// ported over contract.FaultSource: 159 ported, 980 missing.
// Issue 440 (wave W7): MdnsServerTransportTest.ts's ten remaining cases
// ported, six MdnsServerTest.ts cases of the one-second rule and the
// turn's announcement and MdnsTest.ts's announcement of devices that
// start together: 160 ported, 963 missing.
// Issue 441 (wave W7): CommissionableMdnsScannerWindowChangeTest.ts
// ported, no scanner change: 161 ported.
// Wave W8 (#461): BorderRouterRegistryTest.ts ported by thread/brdiscovery,
// merged with wave W7: 162 ported.
// Review II (core2): AttestationCertificateProfileTest.ts's "VendorID and
// ProductID in the distinguished name" ported but for its invalid-UTF-8
// case: 941 missing.
// Review II, phase 4: ServerSubscriptionTest.ts's "abandons the
// subscription after repeated MRP exhaustion, leaving the session alone"
// (bridge) and ListManagerTest.ts's masking of another fabric's sensitive
// fields (cluster/core, by meaning) ported: 939 missing.
// Review II, phase 4, wave 3 (R2-P4-nw-07b): GroupcastServerTest.ts's
// eight cases on the testing events of a rejected message (cluster/core,
// bridge) and SecureSessionTest.ts's eleven of "GroupKeyMap check" and the
// round trip without privacy (groups) ported: 920 missing.
// Review II, phase 5a (R2-P5-tassert-09): ConstructionValidationTest.ts's
// strict node over BLE without NetworkCommissioning (endpoint) and
// ProtocolServiceTest.ts's request constraint violation (node) ported:
// 918 missing.
// Pin lift b25ed960 (P0): 300 new matter.js it cases in moved files go to
// missing, each list's reason naming the lift unit that takes it (103 of
// them OtaTest.ts's rewritten update flow; ThermostatBehaviorTest.ts's six
// ControlSequenceOfOperation cases have no unit yet, findings F-b25ed960-22),
// and 16 ported files become partial by their new cases or by a case upstream
// renamed with the opposite outcome: 146 ported, 1218 missing.
// Pin lift b25ed960 wave P1 (units P1-A … P1-F) ports 61 missing cases net:
// six files become ported (ValueValidatorTest.ts of node, the two Presets
// files, ClientStartUpTest.ts, PeersLoadFailureTest.ts,
// PhysicalDevicePropertiesTest.ts), the two planned files it takes in part
// add 23 missing cases (ThermostatSuggestionsTest.ts's 13 that wait for
// decisions 316-318, SessionViewsTest.ts's 10 device-side rows), and the
// localization files keep five stored-value cases (a port on node) and three
// TimeFormat starts (decision 315) missing: 152 ported, 1157 missing.
// Pin lift b25ed960 wave P2 (units P2-C, P2-D, P2-G, P2-H, P2-J) ports 48
// missing cases net: five files become ported (DeviceAttestationValidatorTest.ts,
// NocsrValidatorTest.ts, CsrNonceCommissioningTest.ts, ThermostatSuggestionsTest.ts,
// ThermostatBehaviorTest.ts); ClientBridgeStructureTest.ts 14,
// NodeLifecycleSeededTest.ts 9, DeviceAttestationValidatorTest.ts 6,
// ClientAttestationTest.ts 3, ThermostatSuggestionsTest.ts 13,
// ThermostatBehaviorTest.ts 1 and SessionViewsTest.ts 3 leave missing, and
// DoorLockServerTest.ts's local user-type refusal returns to it (decision 339
// moved the request check to the wire path, findings F-b25ed960-36):
// 157 ported, 1109 missing.
// Pin lift b25ed960 wave P2b (units P2b-A, -B, -E, -I, -K) ports 70 missing
// cases and adds 13: eleven files become ported (constraintTest.ts,
// GroupsServerSchemaTest.ts, ScenesManagementServerTest.ts,
// UserLabelServerTest.ts, StoredNodeLabelTest.ts, ClientSubscriptionHandlerTest.ts,
// PeerSubscriptionTest.ts, RebootResubscribeArmerTest.ts and the three
// localization tests); the UTF-8 byte cases (#4995), the supersession cases
// (#4882), the compact priming and timer cases (#4943, #4947) and the stored
// localization values leave missing, five of them to left (below);
// ServerSubscriptionSessionTest.ts, decided in part, adds its 13
// CASE-server interception cases: 168 ported, 1052 missing.
// Pin lift b25ed960 wave P3 (units P3-A … P3-D) ports three missing cases and
// adds three: seven files become ported (the six planned files of the
// previous ratchet's comment that it ports whole, and ClientInvokeTest.ts,
// whose requireExistingSession bypass P3-D ports);
// AtomicWriteHandlerTest.ts's Schedules write of another peer (P3-A) and
// AdministratorCommissioningServerTest.ts's twentieth attempt through a
// revoke (P3-C) leave missing; SchedulesConfigurationTest.ts's two and
// SchedulesLocalWriteTest.ts's one restart on stored schedules join it
// (coordinator decision C-7): 175 ported, 1052 missing.
// Pin lift b25ed960 wave P4 ports ThermostatEventsTest.ts (P4-A, 44 of 46
// cases, two left) and SecretRedactionTest.ts (P4-B, 16 of 20, four left);
// P4-D1's bridge ports of MessageExchangeTest.ts and SessionManagerTest.ts
// cover cases already ported on the controller: 177 ported, 1052 missing.
// Wave P4b ports thirteen OtaTest.ts cases of the update attempts (unit C,
// the apply and admission tables, the consent while querying, the
// NotifyUpdateApplied branches, the delegation switch); the apply under an
// unknown token (OtaTest.ts:1897) goes back, its link not ported: 1039 missing.
// Pin lift b25ed960 wave P5 ports 22 OtaTest.ts cases (P5-A, decision 292:
// the queue's phases, the failed consent, #retarget, the announcement, the
// consent tables and the exact offer; P5-A2: the four stored-image selection
// rows), completes DclOtaUpdateServiceTest.ts (P5-A2, findApplicable's skip of
// a stored local image under isProduction) and ports
// OperationalCertificateUpdateTest.ts (P5-B): 179 ported, 1016 missing.
// Pin lift b25ed960 wave P6 takes seven cases out of missing: DoorLockServerTest.ts's
// local user-type refusal (P6-F, the store check), ServerSubscriptionTest.ts's
// refused shutdown flush (P6-C) and its flush on a given session (left, P6-B),
// and ServerSubscriptionSessionTest.ts's four record cases on the node's
// responder (P6-D): 179 ported, 1009 missing.
// Review II phase 8 (#604, R2-C2-doc-2156) takes PeerAddressMonitorTest.ts's
// four sleeping-LIT-ICD cases out of missing: 179 ported, 1005 missing.
// Review II phase 9 unit P1-C ports the new PeerConnectionBusyTest.ts (#5050), all nine cases: +1 ported.
const (
	testparityPortedRatchet  = 180
	testparityMissingRatchet = 1005
)

// testparityLeftRatchet is the number of left it cases summed over the
// ported and partial entries: the cases that leave missing without a port,
// each with its class and reason (phase 31). Exact in both directions like
// the others: a case moved from missing to left, or a left case ported
// after all, moves it in the same commit, so a missing list cannot shrink
// into left unseen. Set to 127 by phase 31: the left rows of the eleven
// server-tranche entries. Raised to 270 by phase 32: 143 left rows of the
// nine controller- and protocol-tranche entries — the write-through cache
// (BD-Matter-ClientNode-WriteNotThroughCache), the sequential node-id
// allocator (BD-Matter-ControllerNodeIDRandom), the ICD transaction model,
// the ML-DSA ledger stores (BD-Matter-DCL-MirroredScope) and JS runtime
// artefacts, each row with its class and reason.
// The seam wave (issue #396) adds the five Presets observer rows
// (js-runtime-only, owner verdict), the discovery node's start, ten
// ClientInvokeTest.ts rows (five of them a feature gap of InvokeOptions,
// named for the findings register), nine DecommissionTest.ts
// construction-lifecycle rows and five IcdClientTest.ts transaction-model
// rows: 300.
// Phase 33 adds the left
// rows of its new files, 285 — TlvObject's schema API and TlvArray's chunks
// (BD-Matter-TLVSchemaLayer), zero-length-as-null
// (BD-Matter-TLVNullIsTheNullElement), the setup-code codec shape
// (BD-Matter-SetupCodecShape), the mDNS relevance pre-filter
// (BD-Matter-MdnsRelevancePreFilter), AES-CCM-8 and CMAC
// (BD-Matter-AESCCMMatterOnly), and JavaScript type checks, text parsers,
// model resolution, hashing and key-format APIs (js-runtime-only).
// T3-SW-1 adds ClientInvokeTest.ts's beforeDispatch row (js-runtime-only:
// a server-only field of the shared Invoke type, Invoke.ts:52-55): 577.
// Phase 39 adds NobleBleChannelTest.ts's 17 left rows — noble's connect
// retries, interview and abort handling (js-runtime-only, the platform's
// behind the ble.Central seam) and the unsubscribe after a handshake
// timeout (by-design, BD-Matter-BTP-RenegotiationAfterStall): 593.
// Pin lift db021da0 wave P1 adds the twelve js-runtime-only rows of the
// certificate tests' object-view API (DistinguishedName.rdnsOf, frozen
// objects, spread copies, an undefined property): 604.
// Pin lift wave P2 (#431) leaves two DclCertificateServiceTest.ts lookups
// by design: the unreadable stored DER (the Go index holds it) and the
// test signer, which the validator knows built in
// (BD-Matter-Attestation-CDSignerTrust) — merged with wave P1: 606.
// Pin lift wave P3 (#432) leaves 13 cases, each with class and reason: five
// JS Network lifecycle cases, four address-change cases (two by design,
// BD-Matter-MdnsFollowsAdvertisedAddresses), three state-field cases of
// GeneralDiagnosticsServerTest.ts and the lone-ADD skip (findings T3-P3-1),
// merged with waves P1 and P2: 619.
// Issue 438 (wave W7) closes the lone-ADD skip: im/write.go answers it
// BUSY as matter.js does (InteractionServer.ts:511-531 at db021da0): 618.
// Wave W8 (#461) leaves twelve BorderRouterRegistryTest.ts cases — the
// addresses, ports and MeshCoP version fields thread/brdiscovery does not
// keep (device-ble-thread, ADR 0027 decision 5) and the JS start/stop,
// target-observer and log-flag mechanics (js-runtime-only), merged with
// wave W7: 630.
// Wave W12 (#478) leaves two PeerAddressMonitorTest.ts session-suspect
// cases by design (BD-Matter-SuspectProbeWalksAlternates): 632.
// Wave 28a (#498) ports the two DeviceTypeConformanceTest.ts cases of a
// node that commissions over BLE, left as device-ble-thread until the
// device side had Bluetooth (endpoint:TestTopologyCommissionsOverBLE): 630.
// Pin lift b25ed960 (P0) leaves 87 new matter.js cases, each with class and
// reason — JavaScript state-layer proxies and observers, log-line text,
// legacy record migration, outbound client-peer validation and the
// ClientNode write-through cache (BD-Matter-ClientNode-WriteNotThroughCache),
// the mDNS pre-filter and always-emitted keys (BD-Matter-MdnsRelevancePreFilter,
// BD-mDNS-SAT-AlwaysEmitted) — and removes the two ValueValidatorTest.ts and
// one ClientNodeTest.ts rows whose cases are gone upstream: 714.
// Pin lift b25ed960 wave P1 leaves 31 cases, each with class and reason:
// ThermostatSuggestionsTest.ts's eight transaction cases (js-runtime-only)
// and nine by design (BD-Matter-Thermostat-Suggestions-Seams, the
// whole-second timer by coordinator decision C-10), ThermostatBehaviorTest.ts's
// six ControlSequenceOfOperation cases (BD-Matter-Thermostat-ControlSequenceFromFeatures,
// C-3), three configured localization start values refused
// (BD-Matter-Localization-ConfiguredStartValueRefused), four JavaScript
// getter and log cases of the localization tests and PresetsConfigurationTest.ts's
// pre-accessor key (js-runtime-only), moved from missing: 745.
// Pin lift b25ed960 wave P2 leaves NocsrValidatorTest.ts's "passes a defect of
// its own crypto through unchanged" (js-runtime-only: no injectable crypto,
// no InternalError to tell from peer data): 746.
// Pin lift b25ed960 wave P2b leaves FabricManagerTest.ts's two stored-label
// cases (by design, BD-chip-FabricLabelBytes: the labels were bounded in
// bytes before), two ServerSubscriptionTest.ts observer and lifetime cases
// (js-runtime-only) and TimeFormatLocalizationServerTest.ts's stored and
// detected calendar fallback (by design, decision 360: no node calendar
// list): 751.
// Pin lift b25ed960 wave P3 leaves 14 cases, each with class and reason:
// SchedulesAtomicWriteTest.ts's observer case and five
// SchedulesLocalWriteTest.ts and three SchedulesConfigurationTest.ts
// state-layer cases (js-runtime-only), SchedulesConfigurationTest.ts's start
// without the forward feature (by design, the extension gate) and
// SchedulesLocalWriteTest.ts's three lifecycle rules the host's replace is
// not held to (by design, BD-Thermostat-HostSchedules, owner decision O-8),
// and CommissioningFailsafeExpiryTest.ts's expiry before the flow armed (by
// design, BD-chip-Commissioner-AbortDisarmsFailSafe): 765.
// Pin lift b25ed960 wave P3b ports TimeFormatLocalizationServerTest.ts's
// stored and detected calendar fallback on node over Config.TimeFormat's
// calendar list (decision 360, C-15): 764.
// Pin lift b25ed960 wave P4 leaves six: ThermostatEventsTest.ts's events not
// enabled and forward-gate start (by design, BD-chip-Thermostat-Events, owner
// decision W2) and SecretRedactionTest.ts's two cases with the switch off (by
// design, BD-Matter-SecretRedaction-NoOffSwitch, owner decision W1), its
// derived-cluster and elemental-form cases (js-runtime-only): 770.
// Pin lift b25ed960 wave P5 leaves OperationalCertificateUpdateTest.ts's two
// cases that wait for matter.js's commissioning-state transaction
// (js-runtime-only, P5-B): 772.
// Pin lift b25ed960 wave P6 leaves ServerSubscriptionTest.ts's flush on a
// given session (by design, BD-Matter-Subscription-FlushOnOwnSessionOnly,
// P6-B): 773.
const testparityLeftRatchet = 773

// The matterjs-server register's four ratchets, exact in both directions as
// the matter.js register's are (the comments above give the rules). Set by
// phase 37 wave B (issue 412) at matterjs-server 0bd06f2: seven files
// ported — the policies' StreamPolicyTest.ts, VideoWindowSearchTest.ts,
// SdpConstraintsTest.ts, SnapshotPolicyTest.ts, OverlayPolicyTest.ts,
// WebRtcProviderArgumentsTest.ts and SessionRegistryClientKeyTest.ts —
// eleven planned for wave C (the stream manager's ten files and
// CameraVideoAllocateTest.ts, which drives it), two excluded as
// host-server; no case missing or left. Phase 37 wave C (issue 410) took
// the eleven planned files: ten ported, MatterCameraDeviceIoTest.ts in part
// (one case missing: no device type of the module serves the WebRTC
// provider without AV stream management), 97 cases left (host-server:
// the WebSocket argument parsing and wire rendering, the connection's
// candidate queue, the server process's lazy service; js-runtime-only:
// JavaScript values the typed fields cannot hold, Observable and Logger
// mechanics). The two topology files issue 380 ported joined the snapshot
// as ported: 0 planned, 19 ported, 1 missing, 97 left.
// Wave W8 (#461): NetworkTopologyServiceTest.ts's five border-router cases
// ported on thread/brdiscovery's registry, the file ported: 19 ported, 1
// missing.
// Review II 6b (R2-P6-conc-2210, decision 207): CameraStreamManagerTest.ts's
// "lets an abandoned deallocate drop a lease that was only reused since"
// left as by-design — the module cuts the give-back chain at the cleanup
// budget instead of abandoning it (BD-matterjs-server-CameraStreamManagement):
// 98 left.
const (
	serverPlannedRatchet = 0
	serverPortedRatchet  = 19
	serverMissingRatchet = 1
	serverLeftRatchet    = 98
)

// moduleRoot is where the committed artefacts live, seen from this package.
const moduleRoot = "../.."

// mustLoad loads the matter.js register.
func mustLoad(t *testing.T) (*Snapshot, *Register) {
	t.Helper()
	return mustLoadSource(t, matterjsSource)
}

func mustLoadSource(t *testing.T, src *source) (*Snapshot, *Register) {
	t.Helper()
	snap, err := loadSnapshot(filepath.Join(moduleRoot, src.snapshotFile))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegister(filepath.Join(moduleRoot, src.registerFile))
	if err != nil {
		t.Fatal(err)
	}
	return snap, reg
}

func TestTestparitySnapshotIsThePin(t *testing.T) {
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			snap, _ := mustLoadSource(t, src)
			commit, err := src.pin(moduleRoot)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Commit != commit {
				t.Fatalf("snapshot is of %s %s, the pin is %s — regenerate the snapshot and re-check every changed file's mapping", src.name, snap.Commit, commit)
			}
			if strings.Join(snap.Roots, ",") != strings.Join(src.roots, ",") {
				t.Errorf("snapshot roots %v, want %v", snap.Roots, src.roots)
			}
			checkSnapshotFiles(t, snap, src.roots)
		})
	}
}

// checkSnapshotFiles holds every snapshot file to the roots: sorted, a blob
// id, a *Test.ts under a root (or a root itself, for a register of files),
// some lines.
func checkSnapshotFiles(t *testing.T, snap *Snapshot, roots []string) {
	t.Helper()
	for i, f := range snap.Files {
		if i > 0 && snap.Files[i-1].Path >= f.Path {
			t.Errorf("snapshot not sorted or duplicate at %s", f.Path)
		}
		if !shaPattern.MatchString(f.SHA) {
			t.Errorf("%s: sha %q", f.Path, f.SHA)
		}
		underRoot := slices.ContainsFunc(roots, func(r string) bool { return f.Path == r || strings.HasPrefix(f.Path, r+"/") })
		if !strings.HasSuffix(f.Path, "Test.ts") || !underRoot {
			t.Errorf("%s: not a *Test.ts under the snapshot roots", f.Path)
		}
		if f.Lines <= 0 {
			t.Errorf("%s: %d lines", f.Path, f.Lines)
		}
	}
}

func TestTestparityRegisterCoversSnapshot(t *testing.T) {
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			snap, reg := mustLoadSource(t, src)
			if err := reg.check(snap); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTestparityGoReferencesResolve(t *testing.T) {
	funcs := map[string]map[string]string{} // package dir -> Test function names -> exclusion
	for _, src := range sources {
		_, reg := mustLoadSource(t, src)
		for _, e := range reg.Entries {
			for _, ref := range e.Go {
				dir, name, _ := strings.Cut(ref, ":")
				if _, ok := funcs[dir]; !ok {
					funcs[dir] = testFuncs(t, filepath.Join(moduleRoot, filepath.FromSlash(dir)))
				}
				excluded, ok := funcs[dir][name]
				switch {
				case !ok:
					t.Errorf("%s: %s — no func %s in %s/*_test.go", e.Path, ref, name, dir)
				case excluded != "":
					t.Errorf("%s: %s — %s is in %s, which go test on linux/amd64 without build tags does not build", e.Path, ref, name, excluded)
				}
			}
		}
	}
}

// testFuncs parses a package directory's _test.go files and returns its
// top-level Test functions, each with "" when `go test` on CI builds its
// file (linux/amd64, no build tags), else the file and the constraint that
// leaves it out — a ported entry that names a test `make test` never runs
// is no port (R2-P6-test-2810).
func testFuncs(t *testing.T, dir string) map[string]string {
	t.Helper()
	names := map[string]string{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("%s: %v", dir, err)
		return names
	}
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.BuildTags = "linux", "amd64", nil
	fset := token.NewFileSet()
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, ent.Name()), nil, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			t.Errorf("%s: %v", ent.Name(), err)
			continue
		}
		excluded := ""
		if ok, err := ctx.MatchFile(dir, ent.Name()); err != nil {
			t.Errorf("%s: %v", ent.Name(), err)
		} else if !ok {
			excluded = ent.Name() + " (" + constraint(f) + ")"
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = excluded
			}
		}
	}
	return names
}

// constraint is a file's //go:build line, or its name's GOOS/GOARCH suffix
// when it has none.
func constraint(f *ast.File) string {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build ") {
				return c.Text
			}
		}
	}
	return "its file name's GOOS/GOARCH suffix"
}

func TestTestparityPageIsCurrent(t *testing.T) {
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			snap, reg := mustLoadSource(t, src)
			if err := reg.check(snap); err != nil {
				t.Skip("register does not match the snapshot; TestTestparityRegisterCoversSnapshot reports it")
			}
			want := renderPage(src, snap, reg)
			got, err := os.ReadFile(filepath.Join(moduleRoot, src.pageFile))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s is stale: run make testparity and commit the result", src.pageFile)
			}
		})
	}
}

func TestTestparityServerRatchets(t *testing.T) {
	_, reg := mustLoadSource(t, serverSource)
	missing, left := 0, 0
	for _, e := range reg.Entries {
		missing += len(e.Missing)
		left += len(e.Left)
	}
	counts := reg.count()
	for _, r := range []struct {
		name         string
		got, ratchet int
	}{
		{"planned files", counts[StatusPlanned], serverPlannedRatchet},
		{"ported files", counts[StatusPorted], serverPortedRatchet},
		{"missing it cases", missing, serverMissingRatchet},
		{"left it cases", left, serverLeftRatchet},
	} {
		if r.got != r.ratchet {
			t.Errorf("matterjs-server register: %d %s, ratchet %d — justify the move and set the ratchet in the same commit", r.got, r.name, r.ratchet)
		}
	}
	if len(reg.Skips) > 0 {
		t.Errorf("matterjs-server register lists %d skips; the module's FixMe skips belong to %s", len(reg.Skips), registerFile)
	}
}

func TestTestparityPlannedRatchet(t *testing.T) {
	_, reg := mustLoad(t)
	got := reg.count()[StatusPlanned]
	switch {
	case got > testparityPlannedRatchet:
		t.Fatalf("%d planned files, ratchet %d: a file went back to planned, or new upstream test files arrived — justify it and raise testparityPlannedRatchet in the same commit", got, testparityPlannedRatchet)
	case got < testparityPlannedRatchet:
		t.Fatalf("%d planned files, ratchet %d: lock the gain in by lowering testparityPlannedRatchet to %d", got, testparityPlannedRatchet, got)
	}
}

func TestTestparityPortedRatchet(t *testing.T) {
	_, reg := mustLoad(t)
	got := reg.count()[StatusPorted]
	switch {
	case got < testparityPortedRatchet:
		t.Fatalf("%d ported files, ratchet %d: a ported file went back to partial, planned or excluded — justify it and lower testparityPortedRatchet in the same commit", got, testparityPortedRatchet)
	case got > testparityPortedRatchet:
		t.Fatalf("%d ported files, ratchet %d: lock the gain in by raising testparityPortedRatchet to %d", got, testparityPortedRatchet, got)
	}
}

func TestTestparityMissingRatchet(t *testing.T) {
	_, reg := mustLoad(t)
	got := 0
	for _, e := range reg.Entries {
		got += len(e.Missing)
	}
	switch {
	case got > testparityMissingRatchet:
		t.Fatalf("%d missing it cases, ratchet %d: a missing list grew — justify it (a pin lift, a case found uncovered) and raise testparityMissingRatchet in the same commit", got, testparityMissingRatchet)
	case got < testparityMissingRatchet:
		t.Fatalf("%d missing it cases, ratchet %d: lock the gain in by lowering testparityMissingRatchet to %d", got, testparityMissingRatchet, got)
	}
}

func TestTestparityLeftRatchet(t *testing.T) {
	_, reg := mustLoad(t)
	got := 0
	for _, e := range reg.Entries {
		got += len(e.Left)
	}
	switch {
	case got > testparityLeftRatchet:
		t.Fatalf("%d left it cases, ratchet %d: cases left without a port — name each one's class and reason in the commit and raise testparityLeftRatchet to %d", got, testparityLeftRatchet, got)
	case got < testparityLeftRatchet:
		t.Fatalf("%d left it cases, ratchet %d: a left case was ported or removed — lower testparityLeftRatchet to %d", got, testparityLeftRatchet, got)
	}
}

func TestTestparityEntryValidation(t *testing.T) {
	sha := strings.Repeat("a", 40)
	ok := []Entry{
		{Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPartial, Go: []string{"secure/channel:TestY"}, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPlanned, Until: "M5 phase 23", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Until: "M4", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassDeviceBLEThread, Until: "M7", Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPartial, Go: []string{"im:TestX"}, Missing: []string{"m"}, Left: []Left{{It: "l", Class: ClassSkip, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		{Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{{It: "l", Class: ClassByDesign, Reason: "BD-X"}, {It: "k", Class: ClassDeviceBLEThread, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
	}
	for i, e := range ok {
		if err := e.validate(); err != nil {
			t.Errorf("ok[%d]: %v", i, err)
		}
	}
	bad := map[string]Entry{
		"unknown status":        {Path: "p", Status: "done", Reason: "r", SnapshotSHA: sha},
		"unknown class":         {Path: "p", Status: StatusExcluded, Class: "later", Reason: "r", SnapshotSHA: sha},
		"go on excluded":        {Path: "p", Status: StatusExcluded, Class: ClassJointFabric, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"no reason":             {Path: "p", Status: StatusPlanned, SnapshotSHA: sha},
		"no sha":                {Path: "p", Status: StatusPlanned, Reason: "r"},
		"no path":               {Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		"ported without go":     {Path: "p", Status: StatusPorted, Reason: "r", SnapshotSHA: sha},
		"ported with missing":   {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		"partial without miss":  {Path: "p", Status: StatusPartial, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"malformed go ref":      {Path: "p", Status: StatusPorted, Go: []string{"im.TestX"}, Reason: "r", SnapshotSHA: sha},
		"class on ported":       {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: sha},
		"planned with go":       {Path: "p", Status: StatusPlanned, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: sha},
		"stage without until":   {Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Reason: "r", SnapshotSHA: sha},
		"until on js-runtime":   {Path: "p", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Until: "M9", Reason: "r", SnapshotSHA: sha},
		"malformed until":       {Path: "p", Status: StatusPlanned, Until: "later", Reason: "r", SnapshotSHA: sha},
		"until past M19":        {Path: "p", Status: StatusExcluded, Class: ClassControllerStage, Until: "M20", Reason: "r", SnapshotSHA: sha},
		"missing on excluded":   {Path: "p", Status: StatusExcluded, Class: ClassJointFabric, Missing: []string{"m"}, Reason: "r", SnapshotSHA: sha},
		"blank reason (spaces)": {Path: "p", Status: StatusPlanned, Reason: "  ", SnapshotSHA: sha},
		"left also missing":     {Path: "p", Status: StatusPartial, Go: []string{"im:TestX"}, Missing: []string{"m"}, Left: []Left{{It: "m", Class: ClassSkip, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		"left without class":    {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{{It: "l", Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		"left unknown class":    {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{{It: "l", Class: "later", Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		"left without reason":   {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{{It: "l", Class: ClassSkip, Reason: " "}}, Reason: "r", SnapshotSHA: sha},
		"left without it":       {Path: "p", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{{Class: ClassSkip, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		"left on planned":       {Path: "p", Status: StatusPlanned, Left: []Left{{It: "l", Class: ClassSkip, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
		"left on excluded":      {Path: "p", Status: StatusExcluded, Class: ClassJointFabric, Left: []Left{{It: "l", Class: ClassSkip, Reason: "r"}}, Reason: "r", SnapshotSHA: sha},
	}
	for name, e := range bad {
		if err := e.validate(); err == nil {
			t.Errorf("%s: validate accepted %+v", name, e)
		}
	}
	unsorted := Register{Entries: []Entry{ok[2], ok[2]}}
	if err := unsorted.validate(); err == nil {
		t.Error("validate accepted a duplicate path")
	}
}

func TestTestparityCheckReportsDrift(t *testing.T) {
	sha, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	snap := &Snapshot{Files: []SnapshotFile{{Path: "a", SHA: sha}, {Path: "b", SHA: sha}, {Path: "d", SHA: sha}, {Path: "e", SHA: sha}}}
	reg := &Register{Entries: []Entry{
		{Path: "a", Status: StatusPorted, Go: []string{"im:TestX"}, Reason: "r", SnapshotSHA: other},
		{Path: "b", Status: StatusExcluded, Class: ClassJSRuntimeOnly, Reason: "r", SnapshotSHA: other},
		{Path: "c", Status: StatusPlanned, Reason: "r", SnapshotSHA: sha},
		{Path: "d", Status: StatusPlanned, Reason: "r", SnapshotSHA: other},
	}}
	err := reg.check(snap)
	if err == nil {
		t.Fatal("check accepted a register that does not match the snapshot")
	}
	for _, want := range []string{"a: ported against blob", "re-check the mapping", "b: excluded against blob", "re-check the exclusion", "c: in the register but not in the snapshot", "e: in the snapshot but not in the register"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("check error lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "d: ") {
		t.Errorf("check reported a planned entry's changed blob:\n%v", err)
	}

	// A left row that names a describe is held to the snapshot's describe
	// names; an it-level row is free text.
	snap = &Snapshot{Files: []SnapshotFile{{Path: "a", SHA: sha, Describes: []string{"Top"}}}}
	reg = &Register{Entries: []Entry{{Path: "a", Status: StatusPorted, Go: []string{"im:TestX"}, Left: []Left{
		{It: "free text", Class: ClassSkip, Reason: "r"},
		{It: "Top > it", Class: ClassSkip, Reason: "r"},
		{It: "Gone > it", Class: ClassSkip, Reason: "r"},
	}, Reason: "r", SnapshotSHA: sha}}}
	err = reg.check(snap)
	if err == nil || !strings.Contains(err.Error(), `names describe "Gone"`) {
		t.Errorf("check on left names = %v, want the unknown describe reported", err)
	}
	if err != nil && strings.Count(err.Error(), "names describe") != 1 {
		t.Errorf("check reported %d left names, want 1:\n%v", strings.Count(err.Error(), "names describe"), err)
	}
}

func TestTestparitySnapshotParsing(t *testing.T) {
	ls := "100644 blob " + strings.Repeat("1", 40) + "\tpackages/node/test/b/ZTest.ts\n" +
		"100644 blob " + strings.Repeat("2", 40) + "\tpackages/node/test/b/helper.ts\n" +
		"040000 tree " + strings.Repeat("3", 40) + "\tpackages/node/test/b\n" +
		"100644 blob " + strings.Repeat("4", 40) + "\tpackages/node/test/a/ATest.ts\n"
	files, others, err := parseLsTree([]byte(ls))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "packages/node/test/a/ATest.ts" || files[1].Path != "packages/node/test/b/ZTest.ts" {
		t.Fatalf("parseLsTree = %+v", files)
	}
	// The other .ts blob is handed to noUnnamedTests (R2-P6-test-2811).
	if len(others) != 1 || others[0].Path != "packages/node/test/b/helper.ts" {
		t.Fatalf("parseLsTree others = %+v", others)
	}
	if _, _, err := parseLsTree([]byte("garbage\n")); err == nil {
		t.Error("parseLsTree accepted a malformed line")
	}

	src := "import x from \"y\";\n" +
		"describe(\"Top one\", () => {\n" +
		"    describe(\"nested\", () => {\n" +
		"        it(\"does\", () => {});\n" +
		"    });\n" +
		"});\n" +
		"describe('single \\'quoted\\'', () => {});\n" +
		"describe.skip(`template`, () => {});\n" +
		"describe(Foo.name, () => {});\n" +
		"describe(\n    \"next line\", () => {});"
	got := topLevelDescribes([]byte(src))
	want := []string{"Top one", `single \'quoted\'`, "template", "Foo.name", "next line"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("topLevelDescribes = %q, want %q", got, want)
	}
	for in, n := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "a\nb\n": 2} {
		if got := countLines([]byte(in)); got != n {
			t.Errorf("countLines(%q) = %d, want %d", in, got, n)
		}
	}
}

// TestTestparityFixMeSkipsRegistered holds register.json's skips to the
// tree: every Go test whose body calls t.Skip with a "FixMe" message is
// listed, and every listed one still is such a test (W6-test-08). A skip
// that names a gap is read later as a fact; the register is where it is
// reviewed.
func TestTestparityFixMeSkipsRegistered(t *testing.T) {
	_, reg := mustLoad(t)
	listed := map[string]bool{}
	for _, s := range reg.Skips {
		listed[s.Test] = true
	}
	found := fixMeSkips(t, moduleRoot)
	for ref := range found {
		if !listed[ref] {
			t.Errorf("%s skips with a FixMe message but is not in %s skips: port it, delete it, or register it", ref, registerFile)
		}
	}
	for ref := range listed {
		if !found[ref] {
			t.Errorf("%s is in %s skips but is no FixMe-skipped test: remove the row", ref, registerFile)
		}
	}
}

// fixMeSkips walks the module's _test.go files and returns
// "<package dir>:<TestFunc>" for every top-level function that calls
// t.Skip (or Skipf) with a string literal starting "FixMe".
func fixMeSkips(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (n == "testdata" || n == "node_modules" || n == "bin" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf") {
					return true
				}
				arg := call.Args[0]
				for {
					bin, ok := arg.(*ast.BinaryExpr)
					if !ok {
						break
					}
					arg = bin.X
				}
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.HasPrefix(strings.Trim(lit.Value, "\"`"), "FixMe") {
					out[filepath.ToSlash(rel)+":"+fn.Name.Name] = true
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTestFuncsNameTheConstraint: a Test function in a file go test does not
// build on CI — a //go:build ignore file, a _darwin file — is reported with
// its constraint, so a register reference into it fails (R2-P6-test-2810).
func TestTestFuncsNameTheConstraint(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a_test.go":        "package p\n\nimport \"testing\"\n\nfunc TestBuilt(t *testing.T) {}\n",
		"b_test.go":        "//go:build ignore\n\npackage p\n\nimport \"testing\"\n\nfunc TestIgnored(t *testing.T) {}\n",
		"c_darwin_test.go": "package p\n\nimport \"testing\"\n\nfunc TestDarwin(t *testing.T) {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := testFuncs(t, dir)
	if got["TestBuilt"] != "" {
		t.Errorf("TestBuilt excluded: %q", got["TestBuilt"])
	}
	if !strings.Contains(got["TestIgnored"], "//go:build ignore") {
		t.Errorf("TestIgnored: %q, want the ignore constraint", got["TestIgnored"])
	}
	if !strings.Contains(got["TestDarwin"], "c_darwin_test.go") {
		t.Errorf("TestDarwin: %q, want its file", got["TestDarwin"])
	}
}

// TestNoUnnamedTestsReadsTheBlobs: a .ts file under the roots whose name
// does not end in Test.ts fails the snapshot when it holds a top-level
// describe, and passes when it holds only a helper (R2-P6-test-2811).
func TestNoUnnamedTestsReadsTheBlobs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("helper.ts", "export function suite() {\n    describe(\"x\", () => {});\n}\n")
	write("spec.test.ts", "describe(\"y\", () => {});\n")
	var ls []byte
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "a"}, {"ls-tree", "-r", "HEAD"}} {
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		ls = out
	}
	_, others, err := parseLsTree(ls)
	if err != nil {
		t.Fatal(err)
	}
	err = noUnnamedTests(dir, others)
	if err == nil || !strings.Contains(err.Error(), "spec.test.ts") || strings.Contains(err.Error(), "helper.ts") {
		t.Errorf("noUnnamedTests = %v, want spec.test.ts alone", err)
	}
}
