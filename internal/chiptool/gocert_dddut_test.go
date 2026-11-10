// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The onboarding-payload DD cases in matter.js's own direction: the
// module's controller is the DUT commissioner, the reference daemon the
// all-clusters TH whose QR and manual codes the TS reads (here the
// daemon's banner, built by secure/setup from its --passcode and
// --discriminator). The helpers mirror tc-dd-support.ts at 862ad75f:
//
//   - a refusal counts only as a refusal of the payload itself
//     (CommissioningRefusals.requireRefusal :734, isPayloadRefusal :972):
//     Pair returns gocontroller.ErrPairingCode within REFUSAL_TIMEOUT
//     (:86), and the daemon logged no PASE attempt — ParsePairingCode runs
//     before any network step (controller/controller.go Pair);
//   - a well-formed code naming a device that is not there ends in a
//     give-up within ABSENT_DEVICE_GIVE_UP (:873; requireGiveUp :783,
//     isCommissioningGiveUp :1165): Pair returns gocontroller.ErrNoDevice
//     when its context, set to that bound, ends with no attempt
//     (controller/pair_race.go pairDiscovered), the TH having been seen
//     advertising first;
//   - a commissioning parses the code first, then commissions and holds
//     the TH's completion line (commissionByTarget :1474, commissionChecked
//     :130), returning the TH to commissioning mode when an earlier step
//     commissioned it (restoreCommissioningMode :1421).

func init() {
	for _, c := range []func() goCertCase{caseDD18, caseDD311, caseDD312, caseDD313, caseDD314, caseDD315, caseDD316, caseDD317} {
		registerGoCert(c)
	}
}

// The budgets and constants of tc-dd-support.ts.
const (
	ddRefusalTimeout        = 15 * time.Second // REFUSAL_TIMEOUT :86
	ddVendorOutcomeTimeout  = 20 * time.Second // TC-DD-3.17.test.ts VENDOR_OUTCOME_TIMEOUT :31
	ddDiscriminatorMSB      = 0x100            // DISCRIMINATOR_MSB :1097
	ddShortDiscriminatorBit = 8                // SHORT_DISCRIMINATOR_SHIFT :1091
	ddOnNetworkOnly         = 1 << 2           // ON_NETWORK_ONLY :313
	ddBLEOnly               = 1 << 1           // DiscoveryCapabilitiesSchema {ble}
	ddWiFiPAFOnly           = 1 << 3           // DiscoveryCapabilitiesSchema {wifiPublicActionFrame}
	ddStandardFlow          = 0                // STANDARD_FLOW :331
	ddUserIntentFlow        = 1                // USER_INTENT_FLOW :334
	ddCustomFlow            = 2                // CUSTOM_FLOW :337
)

// ddInvalidPasscodes are the trivial passcodes the plans list
// (INVALID_PASSCODES :100-102); ddTestVendorIDs the test vendor ids
// (TEST_VENDOR_IDS :105).
var (
	ddInvalidPasscodes = []int{0, 11111111, 22222222, 33333333, 44444444, 55555555, 66666666, 77777777, 88888888, 99999999, 12345678, 87654321}
	ddTestVendorIDs    = []int{0xfff1, 0xfff2, 0xfff3, 0xfff4}
)

// dutRun is a DD case's DUT controller, the DNS-SD cache the harness
// probes the TH's advertisement with, and the node the DUT last
// commissioned the TH as (CommissionedRefs "dut").
type dutRun struct {
	names *discovery.Names
	ctrl  *gocontroller.Controller
	node  *client.Node
}

func (d *dutRun) setup(th *goTH) error {
	if d.ctrl != nil {
		return nil
	}
	var err error
	if d.names, err = th.names(); err != nil {
		return err
	}
	d.ctrl, err = th.controller("dut")
	return err
}

// pair is the DUT's commission(target): with the registry's default
// subscription, as matter.js's adapter commissions with autoSubscribe
// (InProcessControllerAdapter.ts:2745-2756), and test devices' attestation
// accepted, as its judgeAttestation does without an attestation option
// (:2799-2801).
func (d *dutRun) pair(ctx context.Context, code string) (*client.Node, error) {
	return d.ctrl.Pair(ctx, code, gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()})
}

// mark is where the TH's log stands now.
func mark(th *goTH) int { return len(th.dut.snapshotStderr()) }

// thLogSince is the TH's log after a mark.
func thLogSince(th *goTH, from int) string { return th.dut.snapshotStderr()[from:] }

// noPASE is the TH-side evidence of a code that never reached the
// network: the daemon logged no PASE message since the mark (the
// PBKDFParamRequest is the first, bridge/securechannel.go
// matter.rx.sc.pase). A settle second lets a stray attempt surface.
func noPASE(th *goTH, from int) error {
	time.Sleep(time.Second)
	if strings.Contains(thLogSince(th, from), "msg=matter.rx.sc.pase ") {
		return errors.New("the TH logged a PASE attempt for it")
	}
	return nil
}

// refuse is CommissioningRefusals.requireRefusal (tc-dd-support.ts:734-748).
func (d *dutRun) refuse(th *goTH, code, what string) error {
	if err := d.setup(th); err != nil {
		return err
	}
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, ddRefusalTimeout)
	defer cancel()
	start := time.Now()
	n, err := d.pair(ctx, code)
	took := time.Since(start).Round(time.Millisecond)
	switch {
	case err == nil:
		_ = d.ctrl.Registry().Decommission(th.ctx, n)
		return fmt.Errorf("%s: the DUT commissioned the TH from %s", what, code)
	case !errors.Is(err, gocontroller.ErrPairingCode):
		return fmt.Errorf("%s: commissioning from %s failed after %s for a reason that is no refusal of the payload (isPayloadRefusal, tc-dd-support.ts:972): %w", what, code, took, err)
	}
	if err := noPASE(th, from); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	th.note("%s: %s refused in %s (%v), no PASE attempt on the TH", what, code, took, err)
	return nil
}

// refuseEach is requireEachRefused (:754-772): every code is tried and
// recorded before the step fails.
func (d *dutRun) refuseEach(th *goTH, codes, whats []string) error {
	var failures []string
	for i, code := range codes {
		if err := d.refuse(th, code, whats[i]); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d codes: %s", len(failures), len(codes), strings.Join(failures, "; "))
	}
	return nil
}

// advertising is recordCommissionable (:1108-1114): the TH's
// commissionable record, so a give-up is about the code, not about a TH
// that is not there.
func (d *dutRun) advertising(th *goTH) error {
	if _, err := th.commissionable(d.names, th.dut.info.discriminator, th.dut.info.port, ddMDNSTimeout); err != nil {
		return fmt.Errorf("the TH is not advertising before the attempt: %w", err)
	}
	return nil
}

// giveUp is requireGiveUp (:783-820): the TH is seen advertising, then a
// well-formed code naming a device that is not there ends in
// gocontroller.ErrNoDevice once ABSENT_DEVICE_GIVE_UP passes; a payload
// refusal is no give-up.
func (d *dutRun) giveUp(th *goTH, code, what string) error {
	if err := d.setup(th); err != nil {
		return err
	}
	if _, err := d.restore(th); err != nil {
		return err
	}
	if err := d.advertising(th); err != nil {
		return err
	}
	from, logFrom := mark(th), len(th.logs.String())
	ctx, cancel := context.WithTimeout(th.ctx, ddAbsentGiveUp)
	defer cancel()
	start := time.Now()
	n, err := d.pair(ctx, code)
	took := time.Since(start).Round(time.Millisecond)
	switch {
	case err == nil:
		_ = d.ctrl.Registry().Decommission(th.ctx, n)
		return fmt.Errorf("%s: the DUT commissioned a device from %s", what, code)
	case errors.Is(err, gocontroller.ErrPairingCode):
		return fmt.Errorf("%s: the DUT refused %s as a payload, which is no give-up — the code is well formed: %w", what, code, err)
	case !errors.Is(err, gocontroller.ErrNoDevice):
		return fmt.Errorf("%s: commissioning from %s failed after %s for a reason that is no give-up (isCommissioningGiveUp, tc-dd-support.ts:1165): %w", what, code, took, err)
	case took > ddAbsentWait:
		return fmt.Errorf("%s: the DUT gave up after %s, past ABSENT_DEVICE_WAIT %s", what, took, ddAbsentWait)
	}
	if err := noPASE(th, from); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	passedOver := strings.Count(th.logs.String()[logFrom:], "matter.controller.discovery.passing_over")
	th.note("%s: the DUT gave up on %s after %s (%v; %d advertisement(s) passed over), no PASE attempt on the TH", what, code, took, err, passedOver)
	return nil
}

// honored is recordDiscriminatorHonored (:931-958): the TH's own QR code
// with a discriminator no device of the run advertises
// (absentDiscriminator :895-905, the TH's XOR 0xFFF) ends in a give-up.
func (d *dutRun) honored(th *goTH) error {
	absent := int(th.dut.info.discriminator ^ 0xFFF)
	code, err := qrPayloadWith(th.dut.info.qrCode, map[string]int{"discriminator": absent})
	if err != nil {
		return err
	}
	return d.giveUp(th, code, fmt.Sprintf("DUT commissions the device its code names (discriminator %d replaced by %d)", th.dut.info.discriminator, absent))
}

// restore is restoreCommissioningMode (:1421-1439): a TH an earlier step
// commissioned is decommissioned and seen back in commissioning mode —
// its log opens its own window again, and its commissionable record is
// back (recordBackInCommissioningMode :1358-1412).
func (d *dutRun) restore(th *goTH) (bool, error) {
	if d.node == nil {
		return false, nil
	}
	from := mark(th)
	if err := d.ctrl.Registry().Decommission(th.ctx, d.node); err != nil {
		return false, fmt.Errorf("decommission the TH an earlier step commissioned: %w", err)
	}
	d.node = nil
	if !waitFor(th.ctx, ddCommissioningLogTimeout, func() bool {
		return strings.Contains(thLogSince(th, from), "msg=commissioning.own_window.open")
	}) {
		return false, errors.New("the TH logged no reopened commissioning window after its fabric was removed")
	}
	return true, d.advertising(th)
}

// offering is a per-transport leg's claim on a parse (PayloadOffering
// :248-252): the capability bit and the flow.
type offering struct {
	bit  int
	name string
	flow int
}

// parse is recordParse (:264-293): the DUT reads the payload — its own
// parse, not the harness's — as the TH's discriminator and passcode, and,
// for a leg, as offering the leg's capability over its flow
// (offeringCheck :369-398).
func (d *dutRun) parse(th *goTH, payload string, o *offering) error {
	p, err := gocontroller.ParsePairingCode(payload)
	if err != nil {
		return fmt.Errorf("DUT could not parse the payload: %w", err)
	}
	if p.Manual || p.Discriminator != th.dut.info.discriminator || p.Passcode != th.dut.info.passcode {
		return fmt.Errorf("DUT read %s as manual=%v discriminator=%d passcode=%d; the TH's own setup code is discriminator=%d passcode=%d",
			payload, p.Manual, p.Discriminator, p.Passcode, th.dut.info.discriminator, th.dut.info.passcode)
	}
	th.note("DUT read the %d-character payload as version=%d vendorId=%d productId=%d flowType=%d discoveryCapabilities=0b%08b discriminator=%d passcode=%d",
		len(payload), p.Version, p.VendorID, p.ProductID, p.Flow, p.Capabilities, p.Discriminator, p.Passcode)
	if o != nil {
		if p.Capabilities&(1<<o.bit) == 0 || int(p.Flow) != o.flow {
			return fmt.Errorf("DUT read %s as flowType=%d capabilities=0b%08b: the payload does not offer %s over flow %d", payload, p.Flow, p.Capabilities, o.name, o.flow)
		}
		th.note("it offers %s over flow %d", o.name, o.flow)
	}
	return nil
}

// manualParse is recordManualParse (:1055-1088): the DUT reads the code as
// the TH's short discriminator and passcode and the vendor and product
// the code carries.
func (d *dutRun) manualParse(th *goTH, code string) error {
	p, err := gocontroller.ParsePairingCode(code)
	if err != nil {
		return fmt.Errorf("DUT could not parse %s: %w", code, err)
	}
	digits, err := manualPairingCodeDigits(code)
	if err != nil {
		return err
	}
	short := th.dut.info.discriminator >> ddShortDiscriminatorBit
	wantVID, wantPID := digits["vendorId"], digits["productId"]
	if wantVID == "" {
		wantVID, wantPID = "0", "0"
	}
	if !p.Manual || p.Discriminator != short || p.Passcode != th.dut.info.passcode ||
		strconv.Itoa(int(p.VendorID)) != wantVID || strconv.Itoa(int(p.ProductID)) != wantPID {
		return fmt.Errorf("DUT read the %d-digit code as shortDiscriminator=%d passcode=%d vendorId=%d productId=%d; the TH's own setup code is shortDiscriminator=%d passcode=%d, and the code carries vendorId=%s productId=%s",
			len(code), p.Discriminator, p.Passcode, p.VendorID, p.ProductID, short, th.dut.info.passcode, wantVID, wantPID)
	}
	th.note("DUT read the %d-digit code as shortDiscriminator=%d passcode=%d vendorId=%d productId=%d", len(code), p.Discriminator, p.Passcode, p.VendorID, p.ProductID)
	return nil
}

// commission is commissionByTarget (:1474-1501): the TH back in
// commissioning mode if an earlier step commissioned it, the DUT's parse
// of the code, then commissionChecked (:130-155) — the DUT's result
// naming the node, and the TH's own line that a commissioning completed
// (its CommissioningComplete answered, bridge's matter.gencomm.cmd_ok).
func (d *dutRun) commission(th *goTH, code string) error {
	if err := d.setup(th); err != nil {
		return err
	}
	if _, err := d.restore(th); err != nil {
		return err
	}
	var err error
	if strings.HasPrefix(code, "MT:") {
		err = d.parse(th, code, nil)
	} else {
		err = d.manualParse(th, code)
	}
	if err != nil {
		return err
	}
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	n, err := d.pair(ctx, code)
	if err != nil {
		return fmt.Errorf("commissioning from %s failed: %w", code, err)
	}
	d.node = n
	addr, _ := n.PeerAddress()
	if !waitFor(th.ctx, ddCommissioningLogTimeout, func() bool {
		return strings.Contains(thLogSince(th, from), "msg=matter.gencomm.cmd_ok command_name=CommissioningComplete")
	}) {
		return errors.New("the TH logged no completed commissioning")
	}
	th.note("commissioned from %s as node %d; the TH logged its CommissioningComplete answered", code, addr.NodeID)
	return nil
}

// vendorOutcome is recordVendorOutcome (:1195-1280): either outcome
// passes — a give-up, or the TH onboarded with the user aware of the
// risk — but only a give-up counts as termination, and the TH is seen
// advertising first.
func (d *dutRun) vendorOutcome(th *goTH, code string, vendorID, thVendorID int) error {
	if err := d.setup(th); err != nil {
		return err
	}
	restored, err := d.restore(th)
	if err != nil {
		return err
	}
	if !restored {
		if err := d.advertising(th); err != nil {
			return err
		}
	}
	substitution := fmt.Sprintf("the code names vendor 0x%x where the TH's own payload names 0x%x", vendorID, thVendorID)
	if vendorID == thVendorID {
		substitution = fmt.Sprintf("the code names the TH's own vendor 0x%x, so it is step 1's code and substitutes nothing", thVendorID)
	}
	logFrom := len(th.logs.String())
	ctx, cancel := context.WithTimeout(th.ctx, ddVendorOutcomeTimeout)
	defer cancel()
	start := time.Now()
	n, err := d.pair(ctx, code)
	took := time.Since(start).Round(time.Millisecond)
	switch {
	case err == nil:
		d.node = n
		th.note("vendor 0x%x: DUT onboarded the TH after %s (%s), which the plan allows where the user accepts the risk", vendorID, took, substitution)
	case errors.Is(err, gocontroller.ErrNoDevice):
		passedOver := strings.Count(th.logs.String()[logFrom:], "matter.controller.discovery.passing_over")
		th.note("vendor 0x%x: DUT terminated commissioning after %s (%s; %d advertisement(s) passed over): %v", vendorID, took, substitution, passedOver, err)
	default:
		return fmt.Errorf("vendor 0x%x: commissioning from %s failed after %s for a reason that says nothing about the code (%s): %w", vendorID, code, took, substitution, err)
	}
	return nil
}

// finalize is the cases' finalize: refusals.settle has nothing to wait
// for (every attempt here ended within its step), commissioned.
// decommissionAll removes the fabric a step left.
func (d *dutRun) finalize(th *goTH) {
	if d.node != nil && d.ctrl != nil {
		_ = d.ctrl.Registry().Decommission(th.ctx, d.node)
		d.node = nil
	}
}

// generated records a harness-built QR payload (recordGeneratedPayload
// :675-682).
func generated(th *goTH, payload string, stated map[string]string, from string) error {
	detail, err := checkGeneratedPayload(payload, stated, from)
	if err != nil {
		return err
	}
	th.note("%s", detail)
	return nil
}

// generatedManual records a harness-built manual code
// (recordGeneratedManualCode :1714-1721).
func generatedManual(th *goTH, code string, stated map[string]string, unchangedFrom, differsFrom string) error {
	detail, err := checkGeneratedManualCode(code, stated, unchangedFrom, differsFrom)
	if err != nil {
		return err
	}
	th.note("%s", detail)
	return nil
}

func itoa(v int) string { return strconv.Itoa(v) }

// thQR is thQrPayload (:227-245): the TH's own QR code.
func thQR(th *goTH) string { return th.dut.info.qrCode }

// qrWithFields is qrPayloadWith over the TH's QR code; a failure is the
// harness's.
func qrWithFields(th *goTH, fields map[string]int) string {
	code, err := qrPayloadWith(thQR(th), fields)
	if err != nil {
		th.t.Fatalf("build a QR payload: %v", err)
	}
	return code
}

// thElevenDigitParts is thElevenDigitCodeParts (:1009-1016).
func thElevenDigitParts(th *goTH) manualParts {
	return manualParts{discriminator: int(th.dut.info.discriminator), passcode: int(th.dut.info.passcode)}
}

// thCodeParts is thCodeParts (:1026-1047): the 21-digit form's parts,
// with the vendor and product the TH's QR code names, which must be set.
func thCodeParts(th *goTH) (manualParts, error) {
	f, err := qrPayloadFields(thQR(th))
	if err != nil {
		return manualParts{}, err
	}
	vid, _ := strconv.Atoi(f["vendorId"])
	pid, _ := strconv.Atoi(f["productId"])
	if vid == 0 || pid == 0 {
		return manualParts{}, fmt.Errorf("the TH's onboarding payload names vendor %d and product %d; a 21-digit manual pairing code carries both", vid, pid)
	}
	return manualParts{vidPidPresent: true, discriminator: int(th.dut.info.discriminator), passcode: int(th.dut.info.passcode), vendorID: intp(vid), productID: intp(pid)}, nil
}

// with is the parts with overrides applied.
func (p manualParts) with(edit func(*manualParts)) manualParts {
	edit(&p)
	return p
}

// mustManual is manualPairingCode for parts the case builds; a failure is
// the harness's.
func mustManual(th *goTH, p manualParts) string {
	code, err := manualPairingCode(p)
	if err != nil {
		th.t.Fatalf("build a manual pairing code: %v", err)
	}
	return code
}

// ddDUTPICS is a DD case's PICS as the TS states it.
func ddDUTPICS(codes ...string) []string {
	return append([]string{"MCORE.ROLE.COMMISSIONER"}, codes...)
}

// noRadio is the reason the TS gives a per-transport commissioning step
// it cannot run (TC-DD-3.11.test.ts:33-39, tc-dd-flow-support.ts:32-38;
// cite names the one of the case), with the leg's own fact: the DUT runs
// without a BLE central or a Wi-Fi PAF radio here (PairOptions.BLE unset).
func noRadio(transport, cite string) string {
	return "the DUT commissioner has no " + transport + " radio in the leg (PairOptions.BLE unset, no Wi-Fi PAF) — a commissioning driven " +
		"from a payload naming " + transport + " would proceed over IP and prove nothing about the transport (" + cite + ")"
}

// --- the cases -------------------------------------------------------------------

// planTLVData is TC-DD-1.8's own example TLV data (:25, PLAN_TLV_DATA):
// an anonymous structure carrying serial number "1234567890" under
// context tag 0.
var planTLVData = []byte{0x15, 0x2c, 0x00, 0x0a, '1', '2', '3', '4', '5', '6', '7', '8', '9', '0', 0x18}

// largeTLVData is TC-DD-1.8's large TLV data (:54-60, largeTlvData): one
// UTF-8 string of 135 characters under manufacturer tag 0x82, which fills
// the payload to the plan's 255 characters (:28, :35).
func largeTLVData() []byte {
	b := []byte{0x15, 0x2c, 0x82, 135}
	b = append(b, strings.Repeat("1", 135)...)
	return append(b, 0x18)
}

// withTLVData is the TH's QR code with TLV data appended (:49-51,
// withTlvData through QrPairingCodeCodec.encode).
func withTLVData(th *goTH, tlv []byte) (string, error) {
	p, err := setup.DecodeQRCode(thQR(th))
	if err != nil {
		return "", err
	}
	p.Extra = &setup.Extra{Raw: tlv}
	return setup.QRCode(p)
}

// largeQR is :62-70 largeQrPayload: exactly 255 characters.
func largeQR(th *goTH) (string, error) {
	code, err := withTLVData(th, largeTLVData())
	if err == nil && len(code) != 255 {
		err = fmt.Errorf("large onboarding payload is %d characters, not the plan's 255", len(code))
	}
	return code, err
}

// caseDD18 is TC-DD-1.8: the TH's QR code, the same with TLV data, and a
// 255-character one, each parsed and commissioned.
func caseDD18() goCertCase {
	d := &dutRun{}
	parseOf := func(build func(th *goTH) (string, error)) func(th *goTH) error {
		return func(th *goTH) error {
			code, err := build(th)
			if err != nil {
				return err
			}
			return d.parse(th, code, nil)
		}
	}
	commissionOf := func(build func(th *goTH) (string, error)) func(th *goTH) error {
		return func(th *goTH) error {
			code, err := build(th)
			if err != nil {
				return err
			}
			return d.commission(th, code)
		}
	}
	own := func(th *goTH) (string, error) { return thQR(th), nil }
	withTLV := func(th *goTH) (string, error) { return withTLVData(th, planTLVData) }
	return goCertCase{
		tc: "TC-DD-1.8", line: 72, role: roleDUT, pics: ddDUTPICS("MCORE.DD.QR_COMMISSIONING"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "0", line: 77, title: "the DUT uses the discriminator its onboarding code names", run: d.honored},
			{n: "1", line: 87, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan the TH's QR code", run: parseOf(own)},
			{n: "2", line: 99, title: "parse the TH's QR code and onboard the TH", run: commissionOf(own)},
			{n: "3.a", line: 108, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan the QR code with TLV data", run: parseOf(withTLV)},
			{n: "3.b", line: 120, title: "onboard the TH from the QR code with TLV data", run: commissionOf(withTLV)},
			{n: "4.a", line: 133, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan the 255-character QR code", run: parseOf(largeQR)},
			{n: "4.b", line: 146, title: "onboard the TH from the 255-character QR code", run: commissionOf(largeQR)},
		},
	}
}

// caseDD311 is TC-DD-3.11: Standard-flow QR codes offering BLE, Wi-Fi PAF
// and the IP network; each parsed, only the IP network one commissioned
// (:190-202).
func caseDD311() goCertCase {
	d := &dutRun{}
	leg := func(bits int) map[string]int { return map[string]int{"discoveryCapabilities": bits} }
	gen := func(bits int) func(th *goTH) error {
		return func(th *goTH) error {
			return generated(th, qrWithFields(th, leg(bits)), map[string]string{
				"version": "0", "flowType": itoa(ddStandardFlow), "discoveryCapabilities": itoa(bits),
			}, thQR(th))
		}
	}
	scan := func(bits, bit int, name string) func(th *goTH) error {
		return func(th *goTH) error {
			return d.parse(th, qrWithFields(th, leg(bits)), &offering{bit: bit, name: name, flow: ddStandardFlow})
		}
	}
	return goCertCase{
		tc: "TC-DD-3.11", line: 47, role: roleDUT, pics: ddDUTPICS("MCORE.DD.QR_COMMISSIONING", "MCORE.DD.STANDARD_COMM_FLOW"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "0", line: 52, title: "the DUT uses the discriminator its onboarding code names", run: d.honored},
			{n: "1.a", line: 62, pics: "MCORE.DD.DISCOVERY_BLE", title: "a Standard-flow QR code offering BLE", run: gen(ddBLEOnly)},
			{n: "1.b", line: 83, pics: "MCORE.DD.SCAN_QR_CODE & MCORE.DD.DISCOVERY_BLE", title: "scan it", run: scan(ddBLEOnly, 1, "ble")},
			{n: "1.c", line: 98, na: noRadio("BLE", "TC-DD-3.11.test.ts:33-39"), title: "commission over BLE"},
			{n: "2.a", line: 108, pics: "MCORE.DD.DISCOVERY_PAF", title: "a Standard-flow QR code offering Wi-Fi PAF", run: gen(ddWiFiPAFOnly)},
			{n: "2.b", line: 129, pics: "MCORE.DD.SCAN_QR_CODE & MCORE.DD.DISCOVERY_PAF", title: "scan it", run: scan(ddWiFiPAFOnly, 3, "wifiPublicActionFrame")},
			{n: "2.c", line: 144, na: noRadio("Wi-Fi PAF", "TC-DD-3.11.test.ts:33-39"), title: "commission over Wi-Fi PAF"},
			{n: "3.a", line: 154, title: "a Standard-flow QR code offering the IP network", run: gen(ddOnNetworkOnly)},
			{n: "3.b", line: 175, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan it", run: scan(ddOnNetworkOnly, 2, "onIpNetwork")},
			{n: "3.c", line: 190, title: "parse it and commission over the IP network", run: func(th *goTH) error {
				if err := d.setup(th); err != nil {
					return err
				}
				if err := d.advertising(th); err != nil {
					return err
				}
				return d.commission(th, qrWithFields(th, leg(ddOnNetworkOnly)))
			}},
		},
	}
}

// ddFlowSupport is the support file the flow cases' steps are defined in.
const ddFlowSupport = "tc-dd-flow-support.ts"

// flowCase is defineFlowQrTest (tc-dd-flow-support.ts:72-189): per leg
// (BLE, Wi-Fi PAF, IP network, :77-99) a generated payload offering the
// leg's capability over the flow, two parses, and — on the IP leg only —
// the commissioning.
func flowCase(tc string, flow int, flowPICS string) goCertCase {
	d := &dutRun{}
	legs := []struct {
		n, transport, name string
		bit, bits          int
		pics               string
	}{
		{"1", "BLE", "ble", 1, ddBLEOnly, "MCORE.DD.DISCOVERY_BLE"},
		{"2", "Wi-Fi PAF", "wifiPublicActionFrame", 3, ddWiFiPAFOnly, "MCORE.DD.DISCOVERY_PAF"},
		{"3", "IP Network", "onIpNetwork", 2, ddOnNetworkOnly, ""},
	}
	steps := []goStep{{n: "0", line: 104, file: ddFlowSupport, title: "the DUT uses the discriminator its onboarding code names", run: d.honored}}
	for _, leg := range legs {
		fields := map[string]int{"discoveryCapabilities": leg.bits, "flowType": flow}
		scanGate := "MCORE.DD.SCAN_QR_CODE"
		if leg.pics != "" {
			scanGate += " & " + leg.pics
		}
		scan := func(th *goTH) error {
			return d.parse(th, qrWithFields(th, fields), &offering{bit: leg.bit, name: leg.name, flow: flow})
		}
		steps = append(steps,
			goStep{n: leg.n + ".a", line: 123, file: ddFlowSupport, pics: leg.pics, title: leg.transport + " payload over the flow", run: func(th *goTH) error {
				if err := generated(th, qrWithFields(th, fields), map[string]string{
					"version": "0", "flowType": itoa(flow), "discoveryCapabilities": itoa(leg.bits),
				}, thQR(th)); err != nil {
					return err
				}
				// The plan's "Commissionee is NOT in commissioning mode"
				// does not hold: the TH advertises from its start, as
				// matter.js records (:143-150, NOT_COMMISSIONABLE_UNAVAILABLE).
				th.note("unverified: the TH advertises as commissionable from its start, so the precondition that it is not in commissioning mode does not hold")
				return nil
			}},
			goStep{n: leg.n + ".b", line: 154, file: ddFlowSupport, pics: scanGate, title: "scan the QR code", run: scan},
			goStep{n: leg.n + ".c", line: 167, file: ddFlowSupport, pics: scanGate, title: "the DUT parses the QR code", run: scan},
		)
		if leg.pics == "" {
			steps = append(steps, goStep{n: leg.n + ".d", line: 182, file: ddFlowSupport, title: "commission over the IP network", run: func(th *goTH) error {
				if err := d.setup(th); err != nil {
					return err
				}
				if err := d.advertising(th); err != nil {
					return err
				}
				return d.commission(th, qrWithFields(th, fields))
			}})
		} else {
			steps = append(steps, goStep{n: leg.n + ".d", line: 182, file: ddFlowSupport, na: noRadio(leg.transport, "tc-dd-flow-support.ts:32-38"), title: "commission over " + leg.transport})
		}
	}
	return goCertCase{
		tc: tc, line: 12, role: roleDUT, pics: ddDUTPICS("MCORE.DD.QR_COMMISSIONING", flowPICS),
		finalize: d.finalize, steps: steps,
	}
}

// caseDD312 is TC-DD-3.12, the User-Intent flow (TC-DD-3.12.test.ts:11-18).
func caseDD312() goCertCase {
	return flowCase("TC-DD-3.12", ddUserIntentFlow, "MCORE.DD.USER_INTENT_COMM_FLOW")
}

// caseDD313 is TC-DD-3.13, the Custom flow (TC-DD-3.13.test.ts:11-18).
func caseDD313() goCertCase {
	return flowCase("TC-DD-3.13", ddCustomFlow, "MCORE.DD.CUSTOM_COMM_FLOW")
}

// caseDD314 is TC-DD-3.14: a reserved version, each trivial passcode and
// another prefix refused; OnNetwork-only payloads commissioned.
func caseDD314() goCertCase {
	d := &dutRun{}
	const invalidVersion = 0b010 // INVALID_VERSION (:26)
	const invalidPrefix = "AB:"  // INVALID_PREFIX (:29)
	onNetwork := func(th *goTH) string {
		return qrWithFields(th, map[string]int{"discoveryCapabilities": ddOnNetworkOnly})
	}
	absent := func(bit int, name string) func(th *goTH) error {
		// recordDiscoveryCapabilityAbsent (tc-dd-support.ts:410-453): derived
		// from step 1's payload, and the DUT reads no such capability.
		return func(th *goTH) error {
			src, err := qrPayloadFields(thQR(th))
			if err != nil {
				return err
			}
			delete(src, "discoveryCapabilities")
			if err := generated(th, onNetwork(th), src, ""); err != nil {
				return err
			}
			p, err := gocontroller.ParsePairingCode(onNetwork(th))
			if err != nil {
				return fmt.Errorf("DUT could not parse the payload: %w", err)
			}
			if p.Capabilities&(1<<bit) != 0 {
				return fmt.Errorf("DUT read the payload as offering %s (0b%08b)", name, p.Capabilities)
			}
			th.note("DUT read the payload as offering 0b%08b, so %s is not offered", p.Capabilities, name)
			return nil
		}
	}
	commissionOnNetwork := func(th *goTH) error { return d.commission(th, onNetwork(th)) }
	return goCertCase{
		tc: "TC-DD-3.14", line: 34, role: roleDUT, pics: ddDUTPICS("MCORE.DD.QR_COMMISSIONING"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "0", line: 39, title: "the DUT uses the discriminator its onboarding code names", run: d.honored},
			{n: "1", line: 49, title: "scan the commissionee's QR code", run: func(th *goTH) error { return d.parse(th, thQR(th), nil) }},
			{n: "2.a", line: 57, title: "a QR code with an invalid version", run: func(th *goTH) error {
				return generated(th, qrWithFields(th, map[string]int{"version": invalidVersion}), map[string]string{"version": itoa(invalidVersion)}, thQR(th))
			}},
			{n: "2.b", line: 72, title: "the DUT refuses it", run: func(th *goTH) error {
				return d.refuse(th, qrWithFields(th, map[string]int{"version": invalidVersion}), "invalid-version payload refused")
			}},
			{n: "3.a", line: 85, pics: "MCORE.DD.DISCOVERY_BLE", title: "the QR code without BLE", run: absent(1, "ble")},
			{n: "3.b", line: 103, pics: "MCORE.DD.DISCOVERY_BLE", title: "commissioned over a capability other than BLE", run: commissionOnNetwork},
			{n: "4.a", line: 117, pics: "MCORE.DD.DISCOVERY_PAF", title: "the QR code without Wi-Fi PAF", run: absent(3, "wifiPublicActionFrame")},
			{n: "4.b", line: 135, pics: "MCORE.DD.DISCOVERY_PAF", title: "commissioned over a capability other than Wi-Fi PAF", run: commissionOnNetwork},
			{n: "5.a", line: 150, title: "a QR code per invalid passcode", run: func(th *goTH) error {
				for _, pc := range ddInvalidPasscodes {
					if err := generated(th, qrWithFields(th, map[string]int{"passcode": pc}), map[string]string{"passcode": itoa(pc)}, thQR(th)); err != nil {
						return err
					}
				}
				return nil
			}},
			// 5.b refuses each in turn, the first acceptance failing the step
			// (:177-195, a for loop over requireRefusal).
			{n: "5.b", line: 177, title: "the DUT refuses each", run: func(th *goTH) error {
				for _, pc := range ddInvalidPasscodes {
					if err := d.refuse(th, qrWithFields(th, map[string]int{"passcode": pc}), fmt.Sprintf("payload carrying passcode %d refused", pc)); err != nil {
						return err
					}
				}
				return nil
			}},
			{n: "6.a", line: 196, title: "a QR code with the prefix AB:", run: func(th *goTH) error {
				return generated(th, invalidPrefix+strings.TrimPrefix(thQR(th), "MT:"), map[string]string{"prefix": invalidPrefix}, thQR(th))
			}},
			// Pair takes the code as one string and reads one without the MT:
			// prefix as a manual code, its other characters dropped
			// (controller.ParsePairingCode) — as matter.js's own commission
			// does with a pairingCode string (CommissioningClient.ts:1352-1353,
			// ManualPairingCodeCodec.decode); its cert adapter hands the
			// payload to the QR decoder instead, as the target names it a QR
			// code (InProcessControllerAdapter.ts:1056-1065). Refused before
			// the network either way, which is what the step asks; the
			// evidence names the reason.
			{n: "6.b", line: 211, title: "the DUT refuses it", run: func(th *goTH) error {
				return d.refuse(th, invalidPrefix+strings.TrimPrefix(thQR(th), "MT:"), "invalid-prefix payload refused")
			}},
		},
	}
}

// caseDD315 is TC-DD-3.15: the TH's 11-digit and 21-digit manual codes
// commission.
func caseDD315() goCertCase {
	d := &dutRun{}
	long := func(th *goTH) (string, manualParts, error) {
		parts, err := thCodeParts(th)
		if err != nil {
			return "", parts, err
		}
		// twentyOneDigitCode (:32-39): the codec's own 21-digit form.
		code, err := setup.ManualCodeLong(th.dut.info.discriminator, th.dut.info.passcode, uint16(*parts.vendorID), uint16(*parts.productID)) //nolint:gosec // 16-bit ids read off the QR code
		return code, parts, err
	}
	return goCertCase{
		tc: "TC-DD-3.15", line: 41, role: roleDUT, pics: ddDUTPICS("MCORE.DD.MANUAL_PC_COMMISSIONING"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "1.a", line: 46, pics: "MCORE.DD.11_MANUAL_PC", title: "the TH's 11-digit manual pairing code", run: func(th *goTH) error {
				return generatedManual(th, th.dut.info.manualCode, map[string]string{
					"length": "11", "futureFormat": "false", "vidPidPresent": "false",
					"shortDiscriminator": itoa(int(th.dut.info.discriminator >> ddShortDiscriminatorBit)), "passcode": itoa(int(th.dut.info.passcode)),
				}, "", "")
			}},
			{n: "1.b", line: 66, pics: "MCORE.DD.11_MANUAL_PC", title: "commission from it", run: func(th *goTH) error {
				return d.commission(th, th.dut.info.manualCode)
			}},
			{n: "2.a", line: 75, pics: "MCORE.DD.21_MANUAL_PC", title: "the TH's 21-digit manual pairing code", run: func(th *goTH) error {
				code, parts, err := long(th)
				if err != nil {
					return err
				}
				return generatedManual(th, code, map[string]string{
					"length": "21", "futureFormat": "false", "vidPidPresent": "true",
					"shortDiscriminator": itoa(int(th.dut.info.discriminator >> ddShortDiscriminatorBit)), "passcode": itoa(int(th.dut.info.passcode)),
					"vendorId": itoa(*parts.vendorID), "productId": itoa(*parts.productID),
				}, "", "")
			}},
			{n: "2.b", line: 100, pics: "MCORE.DD.21_MANUAL_PC", title: "commission from it", run: func(th *goTH) error {
				code, _, err := long(th)
				if err != nil {
					return err
				}
				return d.commission(th, code)
			}},
		},
	}
}

// wrongCheckDigit is TC-DD-3.16's wrongCheckDigit (:52-54).
func wrongCheckDigit(code string) int {
	n, _ := strconv.Atoi(code[len(code)-1:])
	return (n + 1) % 10
}

// caseDD316 is TC-DD-3.16: 11-digit manual code mutations.
func caseDD316() goCertCase {
	d := &dutRun{}
	src := func(th *goTH) string { return th.dut.info.manualCode }
	sub := func(th *goTH, edit func(*manualParts)) string {
		return mustManual(th, thElevenDigitParts(th).with(edit))
	}
	future := func(p *manualParts) { p.futureFormat = true }
	vp := func(p *manualParts) { p.vidPidPresent = true }
	wrongDisc := func(p *manualParts) { p.discriminator ^= ddDiscriminatorMSB }
	return goCertCase{
		tc: "TC-DD-3.16", line: 56, role: roleDUT, pics: ddDUTPICS("MCORE.DD.11_MANUAL_PC"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "1", line: 61, title: "the 11-digit manual pairing code from the commissionee", run: func(th *goTH) error { return d.manualParse(th, src(th)) }},
			{n: "2.a", line: 72, title: "a code with the reserved version", run: func(th *goTH) error {
				p := thElevenDigitParts(th)
				return generatedManual(th, sub(th, future), map[string]string{
					"futureFormat": "true", "vidPidPresent": "false", "shortDiscriminator": itoa(p.discriminator >> ddShortDiscriminatorBit & 0x03),
				}, src(th), "")
			}},
			{n: "2.b", line: 95, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, sub(th, future), "reserved-version code refused") }},
			{n: "3.a", line: 108, title: "a code with VID_PID_PRESENT set", run: func(th *goTH) error {
				return generatedManual(th, sub(th, vp), map[string]string{"vidPidPresent": "true"}, src(th), "")
			}},
			{n: "3.b", line: 123, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, sub(th, vp), "header/length mismatch refused") }},
			{n: "4.a", line: 136, title: "a code with another short discriminator", run: func(th *goTH) error {
				p := thElevenDigitParts(th)
				return generatedManual(th, sub(th, wrongDisc), map[string]string{
					"shortDiscriminator": itoa((p.discriminator ^ ddDiscriminatorMSB) >> ddShortDiscriminatorBit),
				}, src(th), src(th))
			}},
			{n: "4.b", line: 159, title: "the DUT commissions no device from it", run: func(th *goTH) error {
				return d.giveUp(th, sub(th, wrongDisc), "no device commissioned from the wrong discriminator")
			}},
			{n: "5.a", line: 176, title: "a code per invalid passcode", run: func(th *goTH) error {
				for _, pc := range ddInvalidPasscodes {
					if err := generatedManual(th, sub(th, func(p *manualParts) { p.passcode = pc }), map[string]string{"passcode": itoa(pc)}, src(th), ""); err != nil {
						return err
					}
				}
				return nil
			}},
			{n: "5.b", line: 200, title: "the DUT refuses each", run: func(th *goTH) error {
				codes, whats := make([]string, 0, len(ddInvalidPasscodes)), make([]string, 0, len(ddInvalidPasscodes))
				for _, pc := range ddInvalidPasscodes {
					codes = append(codes, sub(th, func(p *manualParts) { p.passcode = pc }))
					whats = append(whats, fmt.Sprintf("code carrying passcode %d refused", pc))
				}
				return d.refuseEach(th, codes, whats)
			}},
			{n: "6.a", line: 216, title: "a code with a wrong check digit", run: func(th *goTH) error {
				code := sub(th, func(p *manualParts) { p.checkDigit = intp(wrongCheckDigit(src(th))) })
				return generatedManual(th, code, map[string]string{"checkDigitCorrect": "false"}, src(th), src(th))
			}},
			{n: "6.b", line: 233, title: "the DUT refuses it", run: func(th *goTH) error {
				return d.refuse(th, sub(th, func(p *manualParts) { p.checkDigit = intp(wrongCheckDigit(src(th))) }), "wrong-check-digit code refused")
			}},
		},
	}
}

// caseDD317 is TC-DD-3.17: 21-digit manual code mutations.
func caseDD317() goCertCase {
	d := &dutRun{}
	parts := func(th *goTH) manualParts {
		p, err := thCodeParts(th)
		if err != nil {
			th.t.Fatalf("TC-DD-3.17 needs the TH's vendor and product: %v", err)
		}
		return p
	}
	// thManualPairingCode (tc-dd-support.ts:983-988): the TH's parts with
	// overrides, laid out.
	code := func(th *goTH, edit func(*manualParts)) string { return mustManual(th, parts(th).with(edit)) }
	same := func(*manualParts) {}
	src := func(th *goTH) string { return code(th, same) }
	future := func(p *manualParts) { p.futureFormat = true }
	noVP := func(p *manualParts) { p.vidPidPresent = false }
	wrongDisc := func(p *manualParts) { p.discriminator ^= ddDiscriminatorMSB }
	pid0 := func(p *manualParts) { p.productID = intp(0) }
	// checkDigitCodes (:341-347).
	wrong := func(th *goTH) string {
		return code(th, func(p *manualParts) { p.checkDigit = intp(wrongCheckDigit(src(th))) })
	}
	return goCertCase{
		tc: "TC-DD-3.17", line: 36, role: roleDUT, pics: ddDUTPICS("MCORE.DD.21_MANUAL_PC"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "1", line: 41, title: "the 21-digit manual pairing code from the commissionee", run: func(th *goTH) error { return d.manualParse(th, src(th)) }},
			{n: "2.a", line: 52, title: "a code with the reserved version", run: func(th *goTH) error {
				return generatedManual(th, code(th, future), map[string]string{
					"futureFormat": "true", "vidPidPresent": "false", "shortDiscriminator": itoa(parts(th).discriminator >> ddShortDiscriminatorBit & 0x03),
				}, src(th), "")
			}},
			{n: "2.b", line: 75, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, code(th, future), "reserved-version code refused") }},
			{n: "3.a", line: 88, title: "a code with VID_PID_PRESENT cleared", run: func(th *goTH) error {
				return generatedManual(th, code(th, noVP), map[string]string{"vidPidPresent": "false"}, src(th), "")
			}},
			{n: "3.b", line: 103, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, code(th, noVP), "header/length mismatch refused") }},
			{n: "4.a", line: 116, title: "a code with another short discriminator", run: func(th *goTH) error {
				return generatedManual(th, code(th, wrongDisc), map[string]string{
					"shortDiscriminator": itoa((parts(th).discriminator ^ ddDiscriminatorMSB) >> ddShortDiscriminatorBit),
				}, src(th), src(th))
			}},
			{n: "4.b", line: 139, title: "the DUT commissions no device from it", run: func(th *goTH) error {
				return d.giveUp(th, code(th, wrongDisc), "no device commissioned from the wrong discriminator")
			}},
			{n: "5.a", line: 159, title: "a code per invalid passcode", run: func(th *goTH) error {
				for _, pc := range ddInvalidPasscodes {
					if err := generatedManual(th, code(th, func(p *manualParts) { p.passcode = pc }), map[string]string{"passcode": itoa(pc)}, src(th), ""); err != nil {
						return err
					}
				}
				return nil
			}},
			{n: "5.b", line: 186, title: "the DUT refuses each", run: func(th *goTH) error {
				codes, whats := make([]string, 0, len(ddInvalidPasscodes)), make([]string, 0, len(ddInvalidPasscodes))
				for _, pc := range ddInvalidPasscodes {
					codes = append(codes, code(th, func(p *manualParts) { p.passcode = pc }))
					whats = append(whats, fmt.Sprintf("code carrying passcode %d refused", pc))
				}
				return d.refuseEach(th, codes, whats)
			}},
			{n: "6.a", line: 206, title: "a code per test vendor id", run: func(th *goTH) error {
				for _, vid := range ddTestVendorIDs {
					if err := generatedManual(th, code(th, func(p *manualParts) { p.vendorID = intp(vid) }), map[string]string{"vendorId": itoa(vid)}, src(th), ""); err != nil {
						return err
					}
				}
				return nil
			}},
			// The plan's "unless" branch (:237-241): each code's outcome is
			// recorded, not asserted, every code tried before the step fails.
			{n: "6.b", line: 232, title: "the DUT terminates or onboards with the user aware of the risk", run: func(th *goTH) error {
				var failures []string
				for _, vid := range ddTestVendorIDs {
					if err := d.vendorOutcome(th, code(th, func(p *manualParts) { p.vendorID = intp(vid) }), vid, *parts(th).vendorID); err != nil {
						failures = append(failures, err.Error())
					}
				}
				if len(failures) > 0 {
					return fmt.Errorf("%d of %d codes: %s", len(failures), len(ddTestVendorIDs), strings.Join(failures, "; "))
				}
				return nil
			}},
			{n: "7.a", line: 278, title: "a code with product id 0", run: func(th *goTH) error {
				return generatedManual(th, code(th, pid0), map[string]string{"productId": "0"}, src(th), "")
			}},
			{n: "7.b", line: 293, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, code(th, pid0), "product-id-0 code refused") }},
			{n: "8.a", line: 306, title: "a code with a wrong check digit", run: func(th *goTH) error {
				return generatedManual(th, wrong(th), map[string]string{"checkDigitCorrect": "false"}, src(th), src(th))
			}},
			{n: "8.b", line: 321, title: "the DUT refuses it", run: func(th *goTH) error { return d.refuse(th, wrong(th), "wrong-check-digit code refused") }},
		},
	}
}
