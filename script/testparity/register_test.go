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
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
const testparityPlannedRatchet = 2

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
const (
	testparityPortedRatchet  = 162
	testparityMissingRatchet = 963
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
const testparityLeftRatchet = 630

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
const (
	serverPlannedRatchet = 0
	serverPortedRatchet  = 19
	serverMissingRatchet = 1
	serverLeftRatchet    = 97
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
	funcs := map[string]map[string]bool{} // package dir -> Test function names
	for _, src := range sources {
		_, reg := mustLoadSource(t, src)
		for _, e := range reg.Entries {
			for _, ref := range e.Go {
				dir, name, _ := strings.Cut(ref, ":")
				if _, ok := funcs[dir]; !ok {
					funcs[dir] = testFuncs(t, filepath.Join(moduleRoot, filepath.FromSlash(dir)))
				}
				if !funcs[dir][name] {
					t.Errorf("%s: %s — no func %s in %s/*_test.go", e.Path, ref, name, dir)
				}
			}
		}
	}
}

// testFuncs parses a package directory's _test.go files and returns its
// top-level Test functions.
func testFuncs(t *testing.T, dir string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Errorf("%s: %v", dir, err)
		return names
	}
	fset := token.NewFileSet()
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, ent.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s: %v", ent.Name(), err)
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names[fn.Name.Name] = true
			}
		}
	}
	return names
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
	files, err := parseLsTree([]byte(ls))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "packages/node/test/a/ATest.ts" || files[1].Path != "packages/node/test/b/ZTest.ts" {
		t.Fatalf("parseLsTree = %+v", files)
	}
	if _, err := parseLsTree([]byte("garbage\n")); err == nil {
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
