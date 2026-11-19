// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/spec"
	admincomm "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/im"
)

// The commissioning cases of review II phase 4 (owner decision 68): the
// administrator commissioning cases TC-CADMIN-1.1, 1.2, 1.7, 1.8 and 1.18
// and the TC-DGWIFI-3.2 and TC-CNET-4.20 client cases, read mirrored (the
// daemon is the DUT, the Go controllers play the TS file's controllers),
// and TC-DD-1.9, 2.2 and 3.5 in matter.js's own direction (the Go
// controller is the DUT commissioner, the daemon the TH). Every TS line
// cited here was read at [goCertReadAt], the snapshot's pin b25ed960: the
// CADMIN cases' steps are defined in tc-cadmin-support.ts and
// tc-cadmin-window-support.ts there, and each step names that file.
//
// matter.js proves a step on the TH by the TH's log lines; the daemon's
// own lines stand in for them here (bridge/, node/, cluster/core/ log
// keys), and where the daemon logs no line for a stage the wire answer
// the Go controller received is the evidence, named at the step.

func init() {
	for _, c := range []func() goCertCase{
		caseCADMIN11, caseCADMIN12, caseCADMIN17, caseCADMIN18, caseCADMIN118,
		caseDGWIFI32, caseCNET420, caseDD19, caseDD22, caseDD35,
	} {
		registerGoCert(c)
	}
}

// The support files the CADMIN cases' steps are defined in at b25ed960;
// cadminSupport serves TC-CADMIN-1.17 in gocert_app_test.go too.
const (
	cadminSupport       = "tc-cadmin-support.ts"
	cadminWindowSupport = "tc-cadmin-window-support.ts"
)

// The budgets and constants of the CADMIN support files at b25ed960.
const (
	cwDuration          = 180 * time.Second           // CW_DURATION_SECONDS, tc-cadmin-support.ts:82
	cwClockAllowance    = 1800 * time.Millisecond     // CW_CLOCK_ALLOWANCE :88, 1 % of the window
	revokeTimedTimeout  = 10 * time.Second            // REVOKE_TIMED_INTERACTION_MS :94
	postRemovalTimeout  = 60 * time.Second            // POST_REMOVAL_TIMEOUT :654
	turnedOffWindow     = 10 * time.Second            // TURNED_OFF_WINDOW :1005
	lateStart           = cwDuration + 10*time.Second // LATE_START, tc-cadmin-window-support.ts:78
	noActionWindow      = 5 * time.Second             // NO_ACTION_WINDOW :81
	revokeAnswerTimeout = 30 * time.Second            // REVOKE_ANSWER_TIMEOUT :84
	advertisementWait   = 10 * time.Second            // ADVERTISEMENT_TIMEOUT :87
	certLogTimeout      = 15 * time.Second            // LOG_TIMEOUT, tc-support.ts:65
	noRecordProbe       = 10 * time.Second            // noRecordOrNoPase's probe, tc-cadmin-support.ts:294
	cadminVendorID      = 0xfff1                      // checkCommissioned :424
	expectedCR2Index    = 2                           // EXPECTED_CR2_FABRIC_INDEX :645
)

// The daemon's log lines that stand in for the TH log patterns of
// tc-cadmin-support.ts:97-123 and tc-cadmin-window-support.ts:93-105.
const (
	// COMMISSIONING_COMPLETE: the daemon answered CommissioningComplete
	// (cluster/core/general_commissioning.go matter.gencomm.cmd_ok).
	logCommissioned = "msg=matter.gencomm.cmd_ok command_name=CommissioningComplete"
	// WINDOW_OPEN: node/commissioning.go logs the enhanced and the basic
	// window apart.
	logEnhancedOpen = "msg=commissioning.window.open "
	logBasicOpen    = "msg=commissioning.basic_window.open "
	// WINDOW_CLOSED: node/commissioning.go, the window status Closed.
	logWindowClosed = "msg=commissioning.window.closed"
)

// PASE_ESTABLISHED: the Pake3 the daemon took (bridge/securechannel.go
// matter.rx.sc.pase stage=pake3), with no pase_err for it after.
var (
	logPake3    = []string{"msg=matter.rx.sc.pase ", " stage=pake3 "}
	logPake3Err = []string{"msg=matter.rx.sc.pase_err ", " stage=pake3 "}
)

// cadminIdentity are the CADMIN cases' daemon flags: a setup code of its
// own (the --discriminator and --passcode goTH.deviceOn passes), so a
// window — a basic one advertises and accepts the device's own code — is
// neither answered by another reference daemon on the LAN with the
// default identity (3840, 20202021; issue 481) nor joined by that
// daemon's commissioners. The TS takes the TH's identity as it finds it
// (th.commissioning); the cases read it from the daemon's banner.
var cadminIdentity = []string{"--discriminator", "1218", "--passcode", "20201218"}

// --- the TH log -----------------------------------------------------------------

// thLines are the TH log's lines after a mark that hold every part.
func thLines(th *goTH, from int, parts ...string) []string {
	var out []string
	for _, l := range strings.Split(thLogSince(th, from), "\n") {
		if l != "" && containsAll(l, parts) {
			out = append(out, l)
		}
	}
	return out
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// awaitTHLine is expectDeviceLog (tc-support.ts): the first TH log line
// after a mark that holds every part, within d.
func awaitTHLine(th *goTH, from int, d time.Duration, parts ...string) (string, error) {
	var line string
	if !waitFor(th.ctx, d, func() bool {
		if ls := thLines(th, from, parts...); len(ls) > 0 {
			line = ls[0]
			return true
		}
		return false
	}) {
		return "", fmt.Errorf("the TH logged no line with %q within %s", strings.Join(parts, " … "), d)
	}
	return line, nil
}

var reLogTime = regexp.MustCompile(`^time=(\S+) `)

// logTime is the time the daemon stamped a log line with (slog's
// TextHandler, examples/reference-bridge/main.go): matter.js times a TH
// line by when its follower took it in (LogLine.receivedAt), the daemon's
// own stamp is the closer measure.
func logTime(line string) (time.Time, error) {
	m := reLogTime.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}, fmt.Errorf("the TH log line carries no time: %s", line)
	}
	return time.Parse(time.RFC3339Nano, m[1])
}

// paseEstablished is PASE_ESTABLISHED since a mark.
func paseEstablished(th *goTH, from int) error {
	if _, err := awaitTHLine(th, from, certLogTimeout, logPake3...); err != nil {
		return fmt.Errorf("PASE: %w", err)
	}
	if ls := thLines(th, from, logPake3Err...); len(ls) > 0 {
		return fmt.Errorf("the TH refused the Pake3: %s", ls[0])
	}
	return nil
}

// --- the CADMIN controllers -----------------------------------------------------

// cadminRole is one TS controller role and the node it commissioned the
// daemon as (CommissionedRefs).
type cadminRole struct {
	ctrl *gocontroller.Controller
	node *client.Node
}

// instance is operationalMdnsInstanceName: the operational instance the
// controller's fabric and node id name.
func (r *cadminRole) instance() string {
	addr, _ := r.node.PeerAddress()
	return operationalInstance(r.ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
}

// ensure builds the role's controller once.
func (r *cadminRole) ensure(th *goTH, label string) error {
	if r.ctrl != nil {
		return nil
	}
	var err error
	r.ctrl, err = th.controller(label)
	return err
}

// decommissionAll is CommissionedRefs.decommissionAll: every role that
// still holds the daemon removes its fabric.
func decommissionAll(th *goTH, roles ...*cadminRole) {
	for _, r := range roles {
		if r.node != nil && r.ctrl != nil {
			ctx, cancel := context.WithTimeout(th.ctx, 30*time.Second)
			_ = r.ctrl.Registry().Decommission(ctx, r.node)
			cancel()
			r.node = nil
		}
	}
}

// cadminWindow is OpenedWindow (tc-cadmin-support.ts:320-336): the code
// the next commissioner joins with, the discriminator the window
// advertises and the daemon's window-open line.
type cadminWindow struct {
	code          string
	discriminator uint16
	openLine      string
	openedAt      time.Time
}

// openWindowChecked is openWindowChecked (tc-cadmin-support.ts:370-410):
// the opener opens a window of CW_DURATION_SECONDS by the method — an
// enhanced one carries a fresh passcode its manual code holds, a basic one
// takes the device's own setup code, so the next commissioner joins with
// the device's own onboarding payload (thQrPayload) — and the daemon
// logs the window open.
func openWindowChecked(th *goTH, opener *client.Node, basic bool, who string) (cadminWindow, error) {
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	w, err := opener.OpenCommissioningWindow(ctx, cwDuration, client.WindowOptions{Basic: basic})
	if err != nil {
		return cadminWindow{}, fmt.Errorf("%s: open a commissioning window: %w", who, err)
	}
	var out cadminWindow
	pattern := logEnhancedOpen
	if basic {
		out.code, out.discriminator, pattern = th.dut.info.qrCode, th.dut.info.discriminator, logBasicOpen
		th.note("%s opened a BCM window", who)
	} else {
		out.code, out.discriminator = w.ManualCode, w.Discriminator
		th.note("%s opened an ECM window; manualPairingCode length=%d, discriminator %d", who, len(w.ManualCode), w.Discriminator)
	}
	if out.openLine, err = awaitTHLine(th, from, certLogTimeout, pattern); err != nil {
		return cadminWindow{}, fmt.Errorf("commissioning-window-open log: %w", err)
	}
	if out.openedAt, err = logTime(out.openLine); err != nil {
		return cadminWindow{}, err
	}
	return out, nil
}

// checkCommissioned is checkCommissioned (tc-cadmin-support.ts:413-438):
// the fresh CASE session reads VendorID 0xfff1, and the daemon logged the
// completed commissioning since the mark.
func checkCommissioned(th *goTH, n *client.Node, who string, from int) error {
	v, err := th.readValue(n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorId))
	if err != nil {
		return fmt.Errorf("%s: read VendorID: %w", who, err)
	}
	if v.El.Uint != cadminVendorID {
		return fmt.Errorf("%s: expected VendorID 0xfff1 after commissioning, got 0x%x", who, v.El.Uint)
	}
	if _, err := awaitTHLine(th, from, certLogTimeout, logCommissioned); err != nil {
		return fmt.Errorf("commissioning-complete log for %s: %w", who, err)
	}
	th.note("%s read VendorID = 0x%x; the TH logged its CommissioningComplete answered", who, v.El.Uint)
	return nil
}

// commissionThrough is a role's controller.commission(target) and
// checkCommissioned (tc-cadmin-support.ts:700-707, :1064-1071). A
// controller commissions with its subscription, as matter.js's adapters
// do unless a step passes withoutSubscription (:1063); subscribe false is
// that option.
func commissionThrough(th *goTH, r *cadminRole, label, who, code string, subscribe bool) (int, error) {
	if err := r.ensure(th, label); err != nil {
		return 0, err
	}
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	opts := gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()}
	if !subscribe {
		off := false
		opts.AutoSubscribe = &off
	}
	n, err := r.ctrl.Pair(ctx, code, opts)
	if err != nil {
		return from, fmt.Errorf("%s: commission the daemon: %w", who, err)
	}
	r.node = n
	return from, checkCommissioned(th, n, who, from)
}

// fabricEntries reads OperationalCredentials.Fabrics unfiltered
// (readFabrics, tc-cadmin-support.ts:471-482).
func fabricEntries(th *goTH, n *client.Node) ([]operationalcredentials.FabricDescriptorStruct, error) {
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	p := attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrFabrics)
	r, err := n.Read(ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Attributes: []im.ConcreteAttributePath{p}, NoFabricFilter: true}, IncludeKnownVersions: true})
	if err != nil {
		return nil, err
	}
	v, ok := r.Value(0, operationalcredentials.ClusterID, operationalcredentials.AttrFabrics)
	if !ok {
		return nil, errors.New("the Fabrics read reported no value")
	}
	out := make([]operationalcredentials.FabricDescriptorStruct, 0, len(v.Children))
	for _, c := range v.Children {
		var f operationalcredentials.FabricDescriptorStruct
		if err := f.DecodeTLV(c); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func describeFabricEntries(fs []operationalcredentials.FabricDescriptorStruct) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, fmt.Sprintf("%s#%d fabricId=0x%x nodeId=0x%x vendorId=0x%x", f.Label, f.FabricIndex, f.FabricId, f.NodeId, f.VendorId))
	}
	return strings.Join(parts, ", ")
}

// controllerFabric is ControllerFabric (tc-cadmin-support.ts:508-520): the
// fabric index the controller reads as CurrentFabricIndex and the
// operational instance name it reports.
type controllerFabric struct {
	index    uint8
	instance string
}

func controllerFabricOf(th *goTH, r *cadminRole) (controllerFabric, error) {
	v, err := th.readValue(r.node, attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrCurrentFabricIndex))
	if err != nil {
		return controllerFabric{}, err
	}
	return controllerFabric{index: uint8(v.El.Uint), instance: r.instance()}, nil //nolint:gosec // a fabric index
}

// checkFabricList is checkFabricList (tc-cadmin-support.ts:529-569): the
// Fabrics list holds exactly the expected fabrics, and each entry's root
// public key, fabric id and node id produce the operational instance name
// its controller reports.
func checkFabricList(th *goTH, reader *client.Node, who string, expected map[string]controllerFabric) error {
	fabrics, err := fabricEntries(th, reader)
	if err != nil {
		return err
	}
	var mismatches []string
	if len(fabrics) != len(expected) {
		mismatches = append(mismatches, fmt.Sprintf("expected %d entries", len(expected)))
	}
	roles := make([]string, 0, len(expected))
	for role := range expected {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	var want []string
	for _, role := range roles {
		e := expected[role]
		want = append(want, fmt.Sprintf("%s#%d as %s", role, e.index, e.instance))
		i := slices.IndexFunc(fabrics, func(f operationalcredentials.FabricDescriptorStruct) bool { return f.FabricIndex == e.index })
		if i < 0 {
			mismatches = append(mismatches, fmt.Sprintf("no entry for %s's fabric index %d", role, e.index))
			continue
		}
		cfid, err := ca.CompressedFabricID(fabrics[i].RootPublicKey, fabrics[i].FabricId)
		if err != nil {
			return err
		}
		if name := operationalInstance(cfid, fabrics[i].NodeId); !strings.EqualFold(name, e.instance) {
			mismatches = append(mismatches, fmt.Sprintf("%s's entry names %s, %s reports %s", role, name, role, e.instance))
		}
	}
	detail := fmt.Sprintf("%s read %d fabric(s): %s; expected %s", who, len(fabrics), describeFabricEntries(fabrics), strings.Join(want, ", "))
	if len(mismatches) > 0 {
		return fmt.Errorf("%s; %s", detail, strings.Join(mismatches, "; "))
	}
	th.note("%s", detail)
	return nil
}

// nodeLabelPath is NODE_LABEL_PATH (tc-cadmin-support.ts:78).
var nodeLabelPath = attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)

// The daemon's lines for a NodeLabel write and read: expectMessageWithPath
// and expectAttributePathIB on the TH log (bridge/receive_dispatch.go
// matter.rx.im.write_path, matter.rx.im.read_path).
var (
	nodeLabelWriteLine = []string{"msg=matter.rx.im.write_path ", " endpoint=0 ", " cluster=40 ", " attribute=5 "}
	nodeLabelReadLine  = []string{"msg=matter.rx.im.read_path ", " endpoint=0 ", " cluster=40 ", " attribute=5 "}
)

var reLogSrc = regexp.MustCompile(` src=(\S+)`)

// writeAndReadNodeLabel is writeAndReadNodeLabel (tc-cadmin-support.ts:
// 571-601): the label written and read back, the daemon having received
// both. It returns the source the daemon logged the write from — the
// writer's address, which a later step looks for.
func writeAndReadNodeLabel(th *goTH, n *client.Node, who, label string) (string, error) {
	from := mark(th)
	if err := th.write(n, nodeLabelPath, spec.String(label), client.WriteOptions{}); err != nil {
		return "", fmt.Errorf("%s NodeLabel write: %w", who, err)
	}
	line, err := awaitTHLine(th, from, certLogTimeout, nodeLabelWriteLine...)
	if err != nil {
		return "", fmt.Errorf("TH received the NodeLabel write: %w", err)
	}
	m := reLogSrc.FindStringSubmatch(line)
	if m == nil {
		return "", fmt.Errorf("the TH's write line names no source: %s", line)
	}
	th.note("%s wrote NodeLabel %q", who, label)
	return m[1], readNodeLabelChecked(th, n, who, label)
}

// readNodeLabelChecked is readNodeLabelChecked (tc-cadmin-support.ts:
// 603-626).
func readNodeLabelChecked(th *goTH, n *client.Node, who, expected string) error {
	from := mark(th)
	v, err := th.readValue(n, nodeLabelPath)
	if err != nil {
		return fmt.Errorf("%s NodeLabel read: %w", who, err)
	}
	if _, err := awaitTHLine(th, from, certLogTimeout, nodeLabelReadLine...); err != nil {
		return fmt.Errorf("TH received the NodeLabel read: %w", err)
	}
	if v.El.String != expected {
		return fmt.Errorf("%s read NodeLabel %q, expected %q", who, v.El.String, expected)
	}
	th.note("%s read NodeLabel %q", who, v.El.String)
	return nil
}

// checkWindowNotOpen is checkWindowNotOpen (tc-cadmin-support.ts:628-640):
// WindowStatus reads WindowNotOpen (0).
func checkWindowNotOpen(th *goTH, n *client.Node, who string) error {
	v, err := th.readValue(n, attrPath(0, admincomm.ClusterID, admincomm.AttrWindowStatus))
	if err != nil {
		return fmt.Errorf("%s WindowStatus read: %w", who, err)
	}
	if v.El.Uint != 0 {
		return fmt.Errorf("%s read WindowStatus %d, expected WindowNotOpen (0)", who, v.El.Uint)
	}
	th.note("%s read WindowStatus 0 (WindowNotOpen)", who)
	return nil
}

// revoke is RevokeCommissioning, timed with REVOKE_TIMED_INTERACTION_MS
// (tc-cadmin-support.ts:1281-1285, tc-cadmin-window-support.ts:255-260).
func revoke(th *goTH, n *client.Node, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(th.ctx, timeout)
	defer cancel()
	_, err := n.Invoke(ctx, cmdPath(0, admincomm.ClusterID, admincomm.CmdRevokeCommissioning), nil,
		client.InvokeOptions{Timed: true, TimedTimeout: revokeTimedTimeout})
	return err
}

// revokeReceived is revokeReceived (tc-cadmin-window-support.ts:262-272):
// the daemon's invoke line for RevokeCommissioning on the root
// (bridge/receive_dispatch.go matter.rx.im.invoke).
func revokeReceived(th *goTH, from int) (string, error) {
	return awaitTHLine(th, from, certLogTimeout, "msg=matter.rx.im.invoke ", `req_paths="ep=0 cl=0x003C cmd=0x2"`)
}

// windowLifetime is windowTimeoutChecks and windowLifetimeCheck
// (tc-cadmin-window-support.ts:130-147, :174-191; tc-cadmin-support.ts
// :1376-1411): the daemon closes the window by itself, CW_DURATION_SECONDS
// within the clock allowance after it logged the window open.
func windowLifetime(th *goTH, w cadminWindow) error {
	from := strings.Index(th.dut.snapshotStderr(), w.openLine)
	if from < 0 {
		return errors.New("the window-open line is gone from the TH log")
	}
	from += len(w.openLine)
	latest := w.openedAt.Add(cwDuration + cwClockAllowance)
	remaining := max(time.Until(latest), 0)
	line, err := awaitTHLine(th, from, remaining+certLogTimeout, logWindowClosed)
	if err != nil {
		return fmt.Errorf("commissioning-window-closed log: %w", err)
	}
	closedAt, err := logTime(line)
	if err != nil {
		return err
	}
	lifetime := closedAt.Sub(w.openedAt)
	earliest, longest := cwDuration-cwClockAllowance, cwDuration+cwClockAllowance
	if lifetime < earliest || lifetime > longest {
		return fmt.Errorf("the TH's window-closed line came %s after its window-open line; the plan allows %s to %s", lifetime, earliest, longest)
	}
	th.note("the TH's window-closed line came %s after its window-open line; the plan allows %s to %s", lifetime, earliest, longest)
	return nil
}

// requireRefused is requireRefused (tc-cadmin-window-support.ts:261-298)
// and TC-CADMIN-1.1/1.2 step 11 (tc-cadmin-support.ts:1298-1336): the
// commissioner's attempt through the window ends in a give-up within
// ABSENT_DEVICE_GIVE_UP (isGiveUpOrRefusedPase :285-288 over
// isCommissioningGiveUp, tc-dd-support.ts:1485-1495: a DiscoveryError —
// the Go controller's gocontroller.ErrNoDevice, no candidate found — or a
// DiscoveryAggregateError, a candidate tried and failed — the Go
// controller's PASE step reported failed, controller/pair_race.go
// pairDiscovered), and either no
// commissionable record for the window's discriminator exists or the TH
// accepted no PASE (noRecordOrNoPase :290-302).
func requireRefused(th *goTH, r *cadminRole, label, who string, w cadminWindow) error {
	if err := r.ensure(th, label); err != nil {
		return err
	}
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, ddAbsentGiveUp)
	defer cancel()
	start := time.Now()
	var paseFailed bool
	n, err := th.pairWith(ctx, r.ctrl, w.code, func(st gocontroller.PairingStep) {
		if st.Name == commissioner.StepPASE && st.Phase == gocontroller.PairingFailed {
			paseFailed = true
		}
	})
	took := time.Since(start).Round(time.Millisecond)
	switch {
	case err == nil:
		r.node = n
		return fmt.Errorf("%s commissioned the TH through the window", who)
	case !errors.Is(err, gocontroller.ErrNoDevice) && !paseFailed:
		return fmt.Errorf("%s's commissioning failed after %s for a reason that is no give-up nor a refused PASE (isGiveUpOrRefusedPase, tc-cadmin-support.ts:285): %w", who, took, err)
	case took > ddAbsentWait:
		return fmt.Errorf("%s gave up after %s, past ABSENT_DEVICE_WAIT %s", who, took, ddAbsentWait)
	}
	th.note("%s could not commission: gave up after %s (%v; a candidate's PASE failed: %v)", who, took, err, paseFailed)
	return noRecordOrNoPase(th, w.discriminator, from)
}

// noRecordOrNoPase is noRecordOrNoPase with eitherNoRecordOrNoPase
// (tc-cadmin-support.ts:266-302): passes when no commissionable record for
// the discriminator answers within the probe, or the TH logged no PASE
// since the mark; names both.
func noRecordOrNoPase(th *goTH, discriminator uint16, from int) error {
	names, err := th.names()
	if err != nil {
		return err
	}
	_, probeErr := th.commissionable(names, discriminator, th.dut.info.port, noRecordProbe)
	noRecord := probeErr != nil && strings.HasPrefix(probeErr.Error(), "no commissionable record")
	noPase := len(thLines(th, from, logPake3...)) == 0
	detail := fmt.Sprintf("mDNS: no record for discriminator %d=%v; PASE: none accepted=%v", discriminator, noRecord, noPase)
	if !noRecord && !noPase {
		return fmt.Errorf("the TH advertised the window's discriminator and accepted a PASE: %s", detail)
	}
	th.note("%s", detail)
	return nil
}

// pairWith is goTH.pair under a caller's context — the give-up bound —
// with the pairing's progress reported to progress (nil: none).
func (th *goTH) pairWith(ctx context.Context, c *gocontroller.Controller, code string, progress func(gocontroller.PairingStep)) (*client.Node, error) {
	off := false
	return c.Pair(ctx, code, gocontroller.PairOptions{AutoSubscribe: &off, OnAttestationFailure: attestation.Accept(), Progress: progress})
}

// --- TC-CADMIN-1.1 / 1.2 ---------------------------------------------------------

// withDashesAndSpaces is withDashesAndSpaces (tc-cadmin-support.ts:
// 1010-1015): XXXX-XXX XXXX.
func withDashesAndSpaces(code string) (string, error) {
	if len(code) != 11 || strings.Trim(code, "0123456789") != "" {
		return "", fmt.Errorf("expected an 11-digit manual pairing code, got %q", code)
	}
	return code[:4] + "-" + code[4:7] + " " + code[7:], nil
}

func caseCADMIN11() goCertCase { return administratorBehaviorCase("TC-CADMIN-1.1", false) }
func caseCADMIN12() goCertCase { return administratorBehaviorCase("TC-CADMIN-1.2", true) }

// administratorBehaviorCase is administratorBehaviorCase
// (tc-cadmin-support.ts:1027-1461), read mirrored: TH_CR1 commissions the
// daemon and opens a window for DUT_CR2, which joins as a second
// administrator, works on after TH_CR1 goes quiet, and opens, revokes and
// lets expire windows of its own; TH_CR3 is refused through the revoked
// window and TH_CR1 through the last, as a fabric the daemon holds.
func administratorBehaviorCase(tc string, basic bool) goCertCase {
	windowPics, abbreviation := "CADMIN.C.C00.Tx", "ECM"
	if basic {
		windowPics, abbreviation = "CADMIN.C.C01.Tx", "BCM"
	}
	var (
		cr1, dut, cr3 cadminRole
		names         *discovery.Names
		pending       *cadminWindow
		expiring      *cadminWindow
		cr1Fabric     *controllerFabric
		enteredCode   string
		cr1Src        string
	)
	take := func() (cadminWindow, error) {
		if pending == nil {
			return cadminWindow{}, errors.New("no pending commissioning window; a commissioning-window step must run first")
		}
		w := *pending
		pending = nil
		return w, nil
	}
	dutOpens := func(th *goTH) (cadminWindow, error) {
		return openWindowChecked(th, dut.node, basic, "DUT_CR2")
	}
	s := func(n string, line int, title, pics string, run func(th *goTH) error) goStep {
		return goStep{n: n, line: line, file: cadminSupport, title: title, pics: pics, run: run}
	}
	step3 := "3.a"
	if basic {
		step3 = "3"
	}
	steps := []goStep{
		s("1", 1056, "TH_CR1 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
			var err error
			if names, err = th.names(); err != nil {
				return err
			}
			// withoutSubscription (:1063).
			_, err = commissionThrough(th, &cr1, "th_cr1", "TH_CR1", th.dut.info.manualCode, false)
			return err
		}),
		s("2", 1074, "TH_CR1 opens a commissioning window using "+abbreviation, windowPics, func(th *goTH) error {
			// The root endpoint read first (:1079-1080).
			if _, err := th.read(cr1.node, im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true}); err != nil {
				return fmt.Errorf("TH_CR1 reads the root endpoint: %w", err)
			}
			w, err := openWindowChecked(th, cr1.node, basic, "TH_CR1")
			if err != nil {
				return err
			}
			pending = &w
			return nil
		}),
		s(step3, 1086, "TH_CR1 sends command to TH_CE to read the list of Fabrics", "OPCREDS.C.A0001", func(th *goTH) error {
			f, err := controllerFabricOf(th, &cr1)
			if err != nil {
				return err
			}
			cr1Fabric = &f
			return checkFabricList(th, cr1.node, "TH_CR1", map[string]controllerFabric{"th_cr1": f})
		}),
	}
	if !basic {
		steps = append(steps, s("3.b", 1102, "Introduce dashes and spaces into the 11 digit Manual Code and enter it in DUT_CR2", "CADMIN.M.UserInterfaceDisplay", func(th *goTH) error {
			if pending == nil {
				return errors.New("step 2's window carries no manual pairing code")
			}
			entered, err := withDashesAndSpaces(pending.code)
			if err != nil {
				return err
			}
			plain, err := gocontroller.ParsePairingCode(pending.code)
			if err != nil {
				return fmt.Errorf("DUT_CR2 could not read %s: %w", pending.code, err)
			}
			formatted, err := gocontroller.ParsePairingCode(entered)
			if err != nil {
				return fmt.Errorf("DUT_CR2 could not read %q: %w", entered, err)
			}
			if formatted.Passcode != plain.Passcode || formatted.Discriminator != plain.Discriminator {
				return fmt.Errorf("DUT_CR2 read %q as passcode %d short discriminator %d, the code without dashes and spaces as %d, %d",
					entered, formatted.Passcode, formatted.Discriminator, plain.Passcode, plain.Discriminator)
			}
			enteredCode = entered
			th.note("DUT_CR2 read %q as the code without dashes and spaces", entered)
			return nil
		}))
	}
	steps = append(steps,
		s("4.a", 1138, "DUT_CR2 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
			w, err := take()
			if err != nil {
				return err
			}
			code := w.code
			if enteredCode != "" {
				code = enteredCode
			}
			from, err := commissionThrough(th, &dut, "dut_cr2", "DUT_CR2", code, true)
			if err != nil {
				return err
			}
			return paseEstablished(th, from)
		}),
		s("4.b", 1159, "Verify TH_CE is now discoverable over DNS-SD with two SRV Records", "", func(th *goTH) error {
			for _, r := range []*cadminRole{&cr1, &dut} {
				if err := th.awaitOperational(names, r.instance(), true, 20*time.Second); err != nil {
					return err
				}
			}
			th.note("%s and %s advertised", cr1.instance(), dut.instance())
			return nil
		}),
		s("4.c", 1174, "DUT_CR2 writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
			_, err := writeAndReadNodeLabel(th, dut.node, "DUT_CR2", "dut-cr2-4c")
			return err
		}),
		s("5", 1186, "DUT_CR2 sends command to TH_CE to read the list of Fabrics", "OPCREDS.C.A0001", func(th *goTH) error {
			if cr1Fabric == nil {
				return errors.New("TH_CR1's fabric was not read; step 3 must run first")
			}
			f, err := controllerFabricOf(th, &dut)
			if err != nil {
				return err
			}
			return checkFabricList(th, dut.node, "DUT_CR2", map[string]controllerFabric{"th_cr1": *cr1Fabric, "dut": f})
		}),
		s("6", 1202, "TH_CR1 writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
			src, err := writeAndReadNodeLabel(th, cr1.node, "TH_CR1", "th-cr1-6")
			if err != nil {
				return err
			}
			// messageReceivedOnFabric (:140-149): the daemon names no fabric
			// on a message line, it names the sender; TH_CR1's address is
			// the pattern step 8 looks for.
			cr1Src = src
			th.note("the TH logs TH_CR1's messages from %s, which step 8 looks for", src)
			return nil
		}),
		s("7", 1230, "DUT_CR2 reads, writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
			if err := readNodeLabelChecked(th, dut.node, "DUT_CR2", "th-cr1-6"); err != nil {
				return err
			}
			src, err := writeAndReadNodeLabel(th, dut.node, "DUT_CR2", "dut-cr2-7")
			if err != nil {
				return err
			}
			if src == cr1Src {
				return fmt.Errorf("DUT_CR2's messages come from %s too, so step 8 cannot attribute a message to TH_CR1", src)
			}
			return nil
		}),
		s("8", 1243, "Turn off TH_CR1", "", func(th *goTH) error {
			if cr1Src == "" {
				return errors.New("TH_CR1's messages were not seen; step 6 must run first")
			}
			from := mark(th)
			sleepCtx(th.ctx, turnedOffWindow)
			if ls := thLines(th, from, " src="+cr1Src+" "); len(ls) > 0 {
				return fmt.Errorf("the TH received a message from TH_CR1: %s", ls[0])
			}
			th.note("no message from TH_CR1 (%s) within %s", cr1Src, turnedOffWindow)
			return nil
		}),
		s("9", 1263, "DUT_CR2 opens a commissioning window using "+abbreviation, windowPics, func(th *goTH) error {
			w, err := dutOpens(th)
			if err != nil {
				return err
			}
			pending = &w
			return nil
		}),
		s("10", 1272, "DUT_CR2 revokes the commissioning window", "CADMIN.C.C02.Tx", func(th *goTH) error {
			from := mark(th)
			if err := revoke(th, dut.node, time.Minute); err != nil {
				return fmt.Errorf("DUT_CR2's RevokeCommissioning: %w", err)
			}
			if _, err := awaitTHLine(th, from, certLogTimeout, logWindowClosed); err != nil {
				return fmt.Errorf("commissioning-window-closed log: %w", err)
			}
			th.note("DUT_CR2's RevokeCommissioning answered with success; the TH closed its window")
			return checkWindowNotOpen(th, dut.node, "DUT_CR2")
		}),
		s("11", 1298, "TH_CR3 attempts a PASE through the revoked window", "CADMIN.C", func(th *goTH) error {
			w, err := take()
			if err != nil {
				return err
			}
			return requireRefused(th, &cr3, "th_cr3", "TH_CR3", w)
		}),
		s("12", 1338, "DUT_CR2 writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
			_, err := writeAndReadNodeLabel(th, dut.node, "DUT_CR2", "dut-cr2-12")
			return err
		}),
		s("13", 1350, "DUT_CR2 opens a commissioning window using "+abbreviation, windowPics, func(th *goTH) error {
			w, err := dutOpens(th)
			if err != nil {
				return err
			}
			expiring = &w
			return nil
		}),
	)
	step14 := s("14", 1358, "Wait for the commissioning window opened in step 13 to time out", "", func(th *goTH) error {
		if expiring == nil {
			return errors.New("step 13 opened no window")
		}
		if err := windowLifetime(th, *expiring); err != nil {
			return err
		}
		return checkWindowNotOpen(th, dut.node, "DUT_CR2")
	})
	if basic {
		step14.pics = windowPics // :1412
	}
	steps = append(steps, step14,
		s("15", 1416, "DUT_CR2 opens a commissioning window using "+abbreviation, windowPics, func(th *goTH) error {
			w, err := dutOpens(th)
			if err != nil {
				return err
			}
			pending = &w
			return nil
		}),
		s("16", 1422, "TH_CR1 commissions TH_CE again before the timeout from step 15", "CADMIN.C", func(th *goTH) error {
			w, err := take()
			if err != nil {
				return err
			}
			// The Go registry refuses a node it holds as commissioned before
			// any message goes out (client.ErrAlreadyCommissioned): TH_CR1
			// forgets its node, keeping its fabric, so the commissioning
			// reaches the TH's AddNOC as the step's check needs.
			if err := cr1.node.Erase(th.ctx); err != nil {
				return fmt.Errorf("TH_CR1 forgets its node: %w", err)
			}
			cr1.node = nil
			logFrom := len(th.logs.String())
			ctx, cancel := context.WithTimeout(th.ctx, ddAbsentWait)
			defer cancel()
			n, err := th.pairWith(ctx, cr1.ctrl, w.code, nil)
			if err == nil {
				cr1.node = n
				return errors.New("TH_CR1 commissioned the TH a second time")
			}
			// isFabricConflictRefusal (:188-190): the controller names the
			// TH's AddNOC answer, NOCResponse StatusCode FabricConflict (9,
			// ADD_NOC_FABRIC_CONFLICT :128-131).
			if !errors.Is(err, commissioner.ErrDeviceAlreadyCommissionedToThisFabric) {
				return fmt.Errorf("TH_CR1's commissioning failed for a reason that is no FabricConflict: %w", err)
			}
			if !strings.Contains(th.logs.String()[logFrom:], "context=addNoc status=FabricConflict") {
				return fmt.Errorf("TH_CR1's controller logged no AddNOC answer FabricConflict: %w", err)
			}
			th.note("TH_CR1 could not commission again: the TH answered AddNOC with FabricConflict (9): %v", err)
			return nil
		}),
	)
	return goCertCase{
		tc: tc, line: 9, pics: []string{"CADMIN.C", windowPics}, steps: steps, daemonArgs: cadminIdentity,
		finalize: func(th *goTH) { decommissionAll(th, &cr1, &dut, &cr3) },
	}
}

// --- TC-CADMIN-1.7 / 1.8 -----------------------------------------------------

func caseCADMIN17() goCertCase { return windowHandlingCase("TC-CADMIN-1.7", false) }
func caseCADMIN18() goCertCase { return windowHandlingCase("TC-CADMIN-1.8", true) }

// windowHandlingCase is windowHandlingCase (tc-cadmin-window-support.ts:
// 206-530), read mirrored: DUT_CR1 opens windows on the daemon and lets
// one time out, revokes one and revokes again with none open; TH_CR2 may
// join only through the last window, and only once.
func windowHandlingCase(tc string, basic bool) goCertCase {
	windowPics, abbreviation := "CADMIN.C.C00.Tx", "ECM"
	if basic {
		windowPics, abbreviation = "CADMIN.C.C01.Tx", "BCM"
	}
	var (
		dut, cr2, cr3 cadminRole
		pending       *cadminWindow
		joined        *cadminWindow
	)
	take := func() (cadminWindow, error) {
		if pending == nil {
			return cadminWindow{}, errors.New("no pending commissioning window; a commissioning-window step must run first")
		}
		w := *pending
		pending = nil
		return w, nil
	}
	// openWindow (:240-261): the window, and its commissionable record for
	// the window's discriminator within ADVERTISEMENT_TIMEOUT, which the
	// refused steps look for.
	openWindow := func(th *goTH) error {
		w, err := openWindowChecked(th, dut.node, basic, "DUT_CR1")
		if err != nil {
			return err
		}
		pending = &w
		names, err := th.names()
		if err != nil {
			return err
		}
		if _, err := th.commissionable(names, w.discriminator, th.dut.info.port, advertisementWait); err != nil {
			return fmt.Errorf("commissionable record for the window's discriminator: %w", err)
		}
		th.note("the TH advertises the window's discriminator %d", w.discriminator)
		return nil
	}
	operatorOnly := func(what string) func(th *goTH) error {
		return func(*goTH) error {
			return goSkip("%s needs a person in front of the DUT; no adapter can observe it (tc-cadmin-window-support.ts:299-305)", what)
		}
	}
	s := func(n string, line int, title, pics string, run func(th *goTH) error) goStep {
		return goStep{n: n, line: line, file: cadminWindowSupport, title: title, pics: pics, run: run}
	}
	return goCertCase{
		tc: tc, line: 9, pics: []string{"CADMIN.C", windowPics}, daemonArgs: cadminIdentity,
		finalize: func(th *goTH) { decommissionAll(th, &dut, &cr2, &cr3) },
		steps: []goStep{
			s("1", 315, "DUT_CR1 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				from, err := commissionThrough(th, &dut, "dut_cr1", "DUT_CR1", th.dut.info.manualCode, true)
				if err != nil {
					return err
				}
				return paseEstablished(th, from)
			}),
			s("2", 337, "DUT_CR1 opens a commissioning window using "+abbreviation, windowPics, openWindow),
			s("3.a", 341, "A User Consent screen pops up on the DUT", "CADMIN.M.UserInterfaceDisplay", operatorOnly("A consent screen on the DUT")),
			s("3.b", 351, "The 11 digit Manual Code is displayed on the DUT", "CADMIN.M.UserInterfaceDisplay", operatorOnly("A code shown on the DUT's screen")),
			s("3.c", 361, "A voice prompt of the Manual Code", "CADMIN.M.AudioInterface", operatorOnly("A voice prompt from the DUT")),
			s("4", 371, "TH_CR2 starts a commissioning process after the window's timeout + 10 seconds", "CADMIN.C", func(th *goTH) error {
				w, err := take()
				if err != nil {
					return err
				}
				if err := windowLifetime(th, w); err != nil {
					return err
				}
				// sleepUntil the window's open line + LATE_START (:384).
				if d := time.Until(w.openedAt.Add(lateStart)); d > 0 {
					sleepCtx(th.ctx, d)
				}
				delay := time.Since(w.openedAt)
				if delay < lateStart {
					return fmt.Errorf("TH_CR2 started %s after the TH's window-open line; the plan asks for %s", delay, lateStart)
				}
				th.note("TH_CR2 started %s after the TH's window-open line", delay.Round(time.Millisecond))
				return requireRefused(th, &cr2, "th_cr2", "TH_CR2", w)
			}),
			s("5", 398, "DUT_CR1 opens a new commissioning window using "+abbreviation, windowPics, openWindow),
			s("6", 402, "DUT_CR1 revokes the commissioning window", "CADMIN.C.C02.Tx", func(th *goTH) error {
				from := mark(th)
				if err := revoke(th, dut.node, time.Minute); err != nil {
					return fmt.Errorf("DUT_CR1's RevokeCommissioning: %w", err)
				}
				if _, err := revokeReceived(th, from); err != nil {
					return fmt.Errorf("TH received RevokeCommissioning: %w", err)
				}
				if _, err := awaitTHLine(th, from, certLogTimeout, logWindowClosed); err != nil {
					return fmt.Errorf("commissioning-window-closed log: %w", err)
				}
				th.note("DUT_CR1's RevokeCommissioning answered with success; the TH received it and closed its window")
				return checkWindowNotOpen(th, dut.node, "DUT_CR1")
			}),
			s("7", 423, "TH_CR2 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				w, err := take()
				if err != nil {
					return err
				}
				return requireRefused(th, &cr2, "th_cr2", "TH_CR2", w)
			}),
			s("8", 429, "DUT_CR1 revokes the commissioning window with none open", "CADMIN.C.C02.Tx", func(th *goTH) error {
				from := mark(th)
				err := revoke(th, dut.node, revokeAnswerTimeout)
				// isWindowNotOpenRefusal (:112-118): Failure carrying the
				// cluster status WindowNotOpen.
				var se *client.StatusError
				if !errors.As(err, &se) || se.Status != im.StatusFailure || se.ClusterStatus == nil ||
					admincomm.StatusCodeEnum(*se.ClusterStatus) != admincomm.StatusCodeWindowNotOpen {
					return fmt.Errorf("DUT_CR1's RevokeCommissioning with no window open: want Failure with cluster status WindowNotOpen (%d), got %w",
						admincomm.StatusCodeWindowNotOpen, err)
				}
				line, err := revokeReceived(th, from)
				if err != nil {
					return fmt.Errorf("TH received RevokeCommissioning: %w", err)
				}
				// REVOKE_WINDOW_NOT_OPEN (:93-100): the TH's invoke line
				// carries the Failure (1) it answered; the cluster status is
				// held on the response; the daemon's line carries the status
				// only.
				if !strings.Contains(line, `resp="status=1 (path=ep=0 cl=0x003C cmd=0x2)"`) {
					return fmt.Errorf("the TH's invoke line shows no Failure answer: %s", line)
				}
				sleepCtx(th.ctx, noActionWindow)
				for _, p := range []string{logEnhancedOpen, logBasicOpen, logWindowClosed, "msg=commissioning.own_window.open"} {
					if ls := thLines(th, from, p); len(ls) > 0 {
						return fmt.Errorf("the TH opened or closed a window after the refused revoke: %s", ls[0])
					}
				}
				th.note("the TH answered Failure with cluster status WindowNotOpen (%v) and opened or closed no window within %s", err, noActionWindow)
				return checkWindowNotOpen(th, dut.node, "DUT_CR1")
			}),
			s("9", 469, "DUT_CR1 writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
				_, err := writeAndReadNodeLabel(th, dut.node, "DUT_CR1", "dut-cr1-9")
				return err
			}),
			s("10", 476, "DUT_CR1 opens a commissioning window using "+abbreviation, windowPics, openWindow),
			s("11", 485, "TH_CR2 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				w, err := take()
				if err != nil {
					return err
				}
				joined = &w
				from, err := commissionThrough(th, &cr2, "th_cr2", "TH_CR2", w.code, true)
				if err != nil {
					return err
				}
				return paseEstablished(th, from)
			}),
			s("12", 506, "TH_CR3 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				if joined == nil {
					return errors.New("TH_CR2 joined through no window; step 11 must run first")
				}
				if err := checkWindowNotOpen(th, dut.node, "DUT_CR1"); err != nil {
					return fmt.Errorf("TH_CE closed the window TH_CR2 joined through: %w", err)
				}
				return requireRefused(th, &cr3, "th_cr3", "TH_CR3", *joined)
			}),
		},
	}
}

// --- TC-CADMIN-1.18 -------------------------------------------------------------

// caseCADMIN118 is fabricRemovalCase(…, "basic") (tc-cadmin-support.ts:
// 685-1002), read mirrored as the TC-CADMIN-1.17 port reads its enhanced
// twin: three controllers commission the daemon through basic windows, one
// fabric is removed and refused, the others' kept, and the removed one
// admitted again.
func caseCADMIN118() goCertCase {
	const windowPics = "CADMIN.C.C01.Tx"
	var (
		dut, cr2, cr3 cadminRole
		names         *discovery.Names
		pending       *cadminWindow
		cr2Index      uint8
		cr2Instance   string
		removed       *client.Node
	)
	open := func(th *goTH) error {
		w, err := openWindowChecked(th, dut.node, true, "DUT_CR1")
		if err != nil {
			return err
		}
		pending = &w
		return nil
	}
	through := func(r *cadminRole, label, who string) func(th *goTH) error {
		return func(th *goTH) error {
			if pending == nil {
				return errors.New("no pending commissioning window; a commissioning-window step must run first")
			}
			w := *pending
			pending = nil
			_, err := commissionThrough(th, r, label, who, w.code, true)
			return err
		}
	}
	s := func(n string, line int, title, pics string, run func(th *goTH) error) goStep {
		return goStep{n: n, line: line, file: cadminSupport, title: title, pics: pics, run: run}
	}
	return goCertCase{
		tc: "TC-CADMIN-1.18", line: 9, pics: []string{"CADMIN.C", windowPics}, daemonArgs: cadminIdentity,
		finalize: func(th *goTH) { decommissionAll(th, &dut, &cr2, &cr3) },
		steps: []goStep{
			s("1", 724, "DUT_CR1 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				var err error
				if names, err = th.names(); err != nil {
					return err
				}
				_, err = commissionThrough(th, &dut, "dut_cr1", "DUT_CR1", th.dut.info.manualCode, true)
				return err
			}),
			s("2", 741, "DUT_CR1 opens a commissioning window using BCM", windowPics, open),
			s("3", 745, "TH_CR2 starts a commissioning process with TH_CE", "CADMIN.C", through(&cr2, "th_cr2", "TH_CR2")),
			s("4", 749, "DUT_CR1 opens a commissioning window using BCM", windowPics, open),
			s("5", 753, "TH_CR3 starts a commissioning process with TH_CE", "CADMIN.C", through(&cr3, "th_cr3", "TH_CR3")),
			s("6", 757, "DUT_CR1 sends command to TH_CE to read the list of Fabrics on TH_CE", "OPCREDS.C.A0001", func(th *goTH) error {
				fabrics, err := fabricEntries(th, dut.node)
				if err != nil {
					return err
				}
				f, err := controllerFabricOf(th, &cr2)
				if err != nil {
					return err
				}
				cr2Index = f.index
				if !slices.ContainsFunc(fabrics, func(e operationalcredentials.FabricDescriptorStruct) bool { return e.FabricIndex == cr2Index }) {
					return fmt.Errorf("TH_CR2 reports fabric index %d, absent from DUT_CR1's own read: %s", cr2Index, describeFabricEntries(fabrics))
				}
				if len(fabrics) != 3 {
					return fmt.Errorf("expected 3 fabrics (dut, th_cr2, th_cr3), got %d", len(fabrics))
				}
				th.note("%d fabrics; th_cr2 fabricIndex=%d", len(fabrics), cr2Index)
				return nil
			}),
			s("7", 785, "DUT_CR1 sends RemoveFabric with FabricIndex = 2 to TH_CE", "OPCREDS.C.C0a.Tx", func(th *goTH) error {
				if cr2Index != expectedCR2Index {
					return fmt.Errorf("th_cr2's fabricIndex was %d, not the plan's assumed %d", cr2Index, expectedCR2Index)
				}
				cr2Instance = cr2.instance()
				from := mark(th)
				r, err := th.invoke(dut.node, cmdPath(0, operationalcredentials.ClusterID, operationalcredentials.CmdRemoveFabric),
					operationalcredentials.RemoveFabricRequest{FabricIndex: cr2Index}, false, client.InvokeOptions{})
				if err != nil {
					return err
				}
				// removeFabricResponseFailure (:197-210): StatusCode Ok, and a
				// fabricIndex it carries names the removed fabric.
				if st, ok := structField(r.Fields, 0); !ok || st.El.Uint != 0 {
					return fmt.Errorf("RemoveFabric did not answer with success: NOCResponse statusCode is %s, not Ok", describeNode(st))
				}
				if idx, ok := structField(r.Fields, 1); ok && idx.El.Uint != uint64(cr2Index) {
					return fmt.Errorf("NOCResponse names fabricIndex %s, not the removed %d", describeNode(idx), cr2Index)
				}
				// removeFabricSucceeded and fabricSessionsEnded on the TH log
				// (:822-843): the daemon's RemoveFabric answer and the
				// withdrawal of the fabric's operational record.
				if _, err := awaitTHLine(th, from, certLogTimeout, "msg=matter.opcreds.cmd_ok command_name=RemoveFabric"); err != nil {
					return fmt.Errorf("RemoveFabric-successful log: %w", err)
				}
				removed = cr2.node
				cr2.node = nil
				th.note("RemoveFabric %d: NOCResponse statusCode Ok; the TH logged RemoveFabric successful", cr2Index)
				return nil
			}),
			s("8", 861, "TH_CR2 writes and reads NodeLabel", "BINFO.C.A0005", func(th *goTH) error {
				if removed == nil {
					return errors.New("step ran before TH_CR2's fabric was removed")
				}
				ctx, cancel := context.WithTimeout(th.ctx, postRemovalTimeout)
				defer cancel()
				st, werr := removed.Write(ctx, []client.WriteRequest{{Path: nodeLabelPath, Value: spec.String("post-removal")}}, client.WriteOptions{})
				if werr == nil {
					werr = st.Err()
				}
				if err := postRemovalRefusal(werr); err != nil {
					cr2.node = removed
					return fmt.Errorf("writeAttribute(NodeLabel): %w", err)
				}
				ctx2, cancel2 := context.WithTimeout(th.ctx, postRemovalTimeout)
				defer cancel2()
				_, rerr := removed.Read(ctx2, client.ReadRequest{ReadRequest: readOf(nodeLabelPath), IncludeKnownVersions: true})
				if err := postRemovalRefusal(rerr); err != nil {
					cr2.node = removed
					return fmt.Errorf("readAttribute(NodeLabel): %w", err)
				}
				th.note("TH_CR2's write (%v) and read (%v) both refused", firstLine(werr, nil), firstLine(rerr, nil))
				return nil
			}),
			s("9", 901, "DUT_CR1 sends command to TH_CE to read the list of Fabrics", "OPCREDS.C.A0001", func(th *goTH) error {
				fabrics, err := fabricEntries(th, dut.node)
				if err != nil {
					return err
				}
				if len(fabrics) != 2 || slices.ContainsFunc(fabrics, func(e operationalcredentials.FabricDescriptorStruct) bool { return e.FabricIndex == cr2Index }) {
					return fmt.Errorf("expected 2 fabrics with index %d (TH_CR2) removed, got %s", cr2Index, describeFabricEntries(fabrics))
				}
				th.note("%d fabrics after removing index %d: %s", len(fabrics), cr2Index, describeFabricEntries(fabrics))
				return nil
			}),
			s("10", 931, "Verify TH_CE is discoverable over DNS-SD with 2 operational records", "", func(th *goTH) error {
				for _, r := range []*cadminRole{&dut, &cr3} {
					if err := th.awaitOperational(names, r.instance(), true, 20*time.Second); err != nil {
						return err
					}
				}
				if err := th.awaitOperational(names, cr2Instance, false, 10*time.Second); err != nil {
					return err
				}
				th.note("%s and %s advertised, %s withdrawn", dut.instance(), cr3.instance(), cr2Instance)
				return nil
			}),
			s("11", 974, "DUT_CR1 opens a commissioning window using BCM", windowPics, open),
			s("12", 975, "TH_CR2 starts a commissioning process with TH_CE", "CADMIN.C", func(th *goTH) error {
				// The Leave event deleted TH_CR2's node; one that missed it
				// still holds the node and forgets it before commissioning
				// again, as the TC-CADMIN-1.17 port does
				// (client.ErrAlreadyCommissioned otherwise).
				if removed != nil && removed.IsCommissioned() {
					if err := removed.Erase(th.ctx); err != nil {
						return fmt.Errorf("forget the lost node: %w", err)
					}
					removed = nil
				}
				return through(&cr2, "th_cr2", "TH_CR2")(th)
			}),
			s("13", 979, "TH_CR2 sends command to TH_CE to read the list of Fabrics", "OPCREDS.C.A0001", func(th *goTH) error {
				fabrics, err := fabricEntries(th, cr2.node)
				if err != nil {
					return err
				}
				if len(fabrics) != 3 {
					return fmt.Errorf("expected 3 fabrics after th_cr2 re-commissioned, got %d", len(fabrics))
				}
				th.note("%d fabrics: %s", len(fabrics), describeFabricEntries(fabrics))
				return nil
			}),
		},
	}
}

// postRemovalRefusal is isPostRemovalRefusal (tc-cadmin-support.ts:
// 168-182): a status the TH answered must be UnsupportedAccess, the one
// the removal explains; any other status is the TH honouring the fabric
// and refusing for a reason of its own. A failure to reach the TH at all
// is the plan's own symptom ("TH_CR2 is no longer on the network"), and a
// controller that took the device's Leave event refuses locally:
// matter.js's NoCommissionedPeerError (client.ErrNotCommissioned) and
// PeerCommunicationError, the Go client's
// peer.UnreachableError and the exchange's transient error
// (client/exchange/errors.go ErrTransient). Anything else —
// the call still pending when POST_REMOVAL_TIMEOUT ran out included, which
// matter.js's expectRejection fails as neither resolved nor rejected —
// says nothing about the removal.
func postRemovalRefusal(err error) error {
	if err == nil {
		return errors.New("succeeded after the fabric was removed")
	}
	var se *client.StatusError
	if errors.As(err, &se) {
		if se.Status != im.StatusUnsupportedAccess {
			return fmt.Errorf("the TH answered status %s, which the removal does not explain (want UnsupportedAccess): %w", se.Status, err)
		}
		return nil
	}
	// NoCommissionedPeerError: the controller deleted the peer on the
	// device's Leave event, which its subscription delivered, and finds no
	// commissioned node.
	var unreachable *peer.UnreachableError
	if errors.Is(err, client.ErrNotCommissioned) || errors.As(err, &unreachable) || errors.Is(err, exchange.ErrTransient) {
		return nil
	}
	// Wider than isPostRemovalRefusal by peer.ErrClosed: peer.ErrClosed
	// (matter.js ClosedError, general/src/MatterError.ts:386) is no
	// PeerCommunicationError; the Go client surfaces it where matter.js
	// surfaces a PeerCommunicationError.
	if errors.Is(err, peer.ErrClosed) {
		return nil
	}
	return fmt.Errorf("neither a status the removal explains nor a failure to reach the TH within %s: %w", postRemovalTimeout, err)
}

// --- TC-DGWIFI-3.2 ------------------------------------------------------------

// caseDGWIFI32 mirrors TC-DGWIFI-3.2 (TC-DGWIFI-3.2.test.ts:19-43): the
// Go TH commissions the daemon and sends ResetCounts to the root's
// WiFiNetworkDiagnostics, which every harness daemon serves with --wifi.
func caseDGWIFI32() goCertCase {
	var node *client.Node
	var ctrl *gocontroller.Controller
	return goCertCase{
		tc: "TC-DGWIFI-3.2", line: 19, pics: []string{"DGWIFI.C"},
		finalize: func(th *goTH) {
			if node != nil {
				_ = ctrl.Registry().Decommission(th.ctx, node)
			}
		},
		steps: []goStep{
			{n: "1", line: 27, title: "Commission TH to DUT", run: func(th *goTH) error {
				from := mark(th)
				var err error
				if ctrl, node, err = th.commission("dut"); err != nil {
					return err
				}
				// commissionThStep through commissionChecked (tc-dd-support.ts
				// :169-181): the TH's completion line.
				if _, err := awaitTHLine(th, from, ddCommissioningLogTimeout, logCommissioned); err != nil {
					return err
				}
				th.note("commissioned; the TH logged its CommissioningComplete answered")
				return nil
			}},
			{n: "2", line: 30, title: "DUT sends ResetCounts command to TH", pics: "DGWIFI.C.C00.Tx", run: func(th *goTH) error {
				from := mark(th)
				if _, err := th.invoke(node, cmdPath(0, wifinetworkdiagnostics.ClusterID, wifinetworkdiagnostics.CmdResetCounts), nil, false, client.InvokeOptions{}); err != nil {
					return err
				}
				// sendCommand's expectCommandPayload (tc-support.ts:1825-1839):
				// the TH received the command, with no fields.
				line, err := awaitTHLine(th, from, certLogTimeout, "msg=matter.rx.im.invoke ", `req_paths="ep=0 cl=0x0036 cmd=0x0"`)
				if err != nil {
					return fmt.Errorf("TH received ResetCounts: %w", err)
				}
				if !strings.Contains(line, `resp="status=0 (path=ep=0 cl=0x0036 cmd=0x0)"`) {
					return fmt.Errorf("the TH's invoke line shows no Success answer: %s", line)
				}
				th.note("ResetCounts answered Success; the TH received it")
				return nil
			}},
		},
	}
}

// --- TC-CNET-4.20 -------------------------------------------------------------

// The Wi-Fi network TC-CNET-4.20 adds (tc-cnet-support.ts:25-26).
var (
	cnetWiFiSSID        = []byte("matter-th-ap")
	cnetWiFiCredentials = []byte("matter-th-passphrase")
)

// cnetDeclare are the NetworkCommissioning codes of the daemon with
// --wifi-sim, the CNETWIFI row's composition, as its generated slice states
// them (testdata/pics-wifi-sim/ep0.txt:510-536); the committed default
// slices describe the Ethernet composition.
var cnetDeclare = map[string]string{
	"CNET.S":         "1",
	"CNET.S.F00":     "1",
	"CNET.S.F02":     "0",
	"CNET.S.C00.Rsp": "1",
	"CNET.S.C02.Rsp": "1",
	"CNET.S.C04.Rsp": "1",
	"CNET.S.C06.Rsp": "1",
	"CNET.S.C08.Rsp": "1",
}

// caseCNET420 mirrors TC-CNET-4.20 (cnetClientCase, tc-cnet-support.ts:
// 133-225): the Go TH sends the Network Commissioning commands to the
// daemon with --wifi-sim, whose simulated driver knows the case's network
// (--wifi-sim-ssid, examples/reference-bridge/wifisim.go); each answers
// NetworkingStatus Success, ArmFailSafe ErrorCode OK.
func caseCNET420() goCertCase {
	var node *client.Node
	var ctrl *gocontroller.Controller
	bc := func(step uint64) *uint64 { return &step } // each step's number as Breadcrumb (:68-69)
	send := func(cmd uint32, name string, fields spec.Encodable) func(th *goTH) error {
		return func(th *goTH) error {
			from := mark(th)
			// sendNetworkCommand (:71-91): the answer's NetworkingStatus (tag 0)
			// is Success.
			if _, err := th.invoke(node, cmdPath(0, networkcommissioning.ClusterID, cmd), fields, true, client.InvokeOptions{}); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if _, err := awaitTHLine(th, from, certLogTimeout, "msg=matter.rx.im.invoke ", fmt.Sprintf(`req_paths="ep=0 cl=0x0031 cmd=0x%X"`, cmd)); err != nil {
				return fmt.Errorf("TH received %s: %w", name, err)
			}
			th.note("%s NetworkingStatus=0; the TH received it", name)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-CNET-4.20", line: 9, pics: []string{"CNET.C.F00"},
		daemonArgs: []string{"--wifi-sim", "--wifi-sim-ssid", string(cnetWiFiSSID) + ":" + string(cnetWiFiCredentials)},
		declare:    cnetDeclare,
		finalize: func(th *goTH) {
			// The cleanup's CommissioningComplete, then decommissionAll
			// (:212-224).
			if node != nil {
				_, _ = th.invoke(node, cmdPath(0, generalcommissioning.ClusterID, generalcommissioning.CmdCommissioningComplete), nil, false, client.InvokeOptions{})
				_ = ctrl.Registry().Decommission(th.ctx, node)
			}
		},
		steps: []goStep{
			{n: "0", line: 144, file: "tc-cnet-support.ts", title: "Preconditions: the DUT commissions the TH", run: func(th *goTH) error {
				from := mark(th)
				var err error
				if ctrl, node, err = th.commission("dut"); err != nil {
					return err
				}
				if _, err := awaitTHLine(th, from, ddCommissioningLogTimeout, logCommissioned); err != nil {
					return err
				}
				th.note("commissioned; the TH logged its CommissioningComplete answered")
				return nil
			}},
			{
				n: "1", line: 147, file: "tc-cnet-support.ts", title: "DUT sends the ScanNetwork command to the TH", pics: "CNET.C.C00.Tx",
				run: send(networkcommissioning.CmdScanNetworks, "scanNetworks", networkcommissioning.ScanNetworksRequest{Breadcrumb: bc(1)}),
			},
			{n: "2", line: 153, file: "tc-cnet-support.ts", title: "DUT sends the ArmFailSafe command to the TH", run: func(th *goTH) error {
				// FAIL_SAFE_SECONDS 60 (:22), Breadcrumb 2; ErrorCode (tag 0) OK.
				if _, err := th.invoke(node, cmdPath(0, generalcommissioning.ClusterID, generalcommissioning.CmdArmFailSafe),
					generalcommissioning.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 2}, true, client.InvokeOptions{}); err != nil {
					return fmt.Errorf("armFailSafe: %w", err)
				}
				th.note("armFailSafe ErrorCode=0")
				return nil
			}},
			{
				n: "3", line: 186, file: "tc-cnet-support.ts", title: "DUT sends the AddOrUpdateWiFiNetwork command to the TH", pics: "CNET.C.C02.Tx",
				run: send(networkcommissioning.CmdAddOrUpdateWiFiNetwork, "addOrUpdateWiFiNetwork",
					networkcommissioning.AddOrUpdateWiFiNetworkRequest{Ssid: cnetWiFiSSID, Credentials: cnetWiFiCredentials, Breadcrumb: bc(3)}),
			},
			{
				n: "4", line: 192, file: "tc-cnet-support.ts", title: "DUT sends the ConnectNetwork command to the TH", pics: "CNET.C.C06.Tx",
				run: send(networkcommissioning.CmdConnectNetwork, "connectNetwork", networkcommissioning.ConnectNetworkRequest{NetworkId: cnetWiFiSSID, Breadcrumb: bc(4)}),
			},
			{
				n: "5", line: 198, file: "tc-cnet-support.ts", title: "DUT sends the ReorderNetwork command to the TH", pics: "CNET.C.C08.Tx",
				run: send(networkcommissioning.CmdReorderNetwork, "reorderNetwork", networkcommissioning.ReorderNetworkRequest{NetworkId: cnetWiFiSSID, NetworkIndex: 0, Breadcrumb: bc(5)}),
			},
			{
				n: "6", line: 206, file: "tc-cnet-support.ts", title: "DUT sends the RemoveNetwork command to the TH", pics: "CNET.C.C04.Tx",
				run: send(networkcommissioning.CmdRemoveNetwork, "removeNetwork", networkcommissioning.RemoveNetworkRequest{NetworkId: cnetWiFiSSID, Breadcrumb: bc(6)}),
			},
		},
	}
}

// --- TC-DD-1.9 ----------------------------------------------------------------

// caseDD19 is TC-DD-1.9 (TC-DD-1.9.test.ts:23-66), the Go controller as
// the DUT: a manual code naming a short discriminator no device advertises
// commissions nothing, the TH's printed manual code commissions it.
func caseDD19() goCertCase {
	d := &dutRun{}
	return goCertCase{
		tc: "TC-DD-1.9", line: 23, role: roleDUT, pics: ddDUTPICS("MCORE.DD.MANUAL_PC_COMMISSIONING"),
		finalize: d.finalize,
		steps: []goStep{
			{n: "0", line: 28, title: "Precondition: the DUT uses the discriminator a Manual Pairing Code names", run: func(th *goTH) error {
				p := thElevenDigitParts(th)
				p.discriminator ^= ddDiscriminatorMSB // DISCRIMINATOR_MSB (tc-dd-support.ts:1152)
				return d.giveUp(th, mustManual(th, p), "No device commissioned from a manual code naming a discriminator no device advertises")
			}},
			{n: "1", line: 47, title: "Provide the TH's Manual Pairing Code to the DUT", run: func(th *goTH) error {
				// thPrintedManualCode (tc-dd-support.ts:1046-1061): the code the
				// TH printed, the daemon's banner.
				return d.commission(th, th.dut.info.manualCode)
			}},
		},
	}
}

// --- TC-DD-2.2 ----------------------------------------------------------------

// ddNoRadio is TC-DD-2.2's noRadio (TC-DD-2.2.test.ts:18-23), with the
// leg's own fact.
func ddNoRadio(transport string) string {
	return noRadio(transport, "TC-DD-2.2.test.ts:18-23")
}

// caseDD22 is TC-DD-2.2 (TC-DD-2.2.test.ts:25-100), the Go controller as
// the DUT: its DNS-SD discovery reports the TH with its onboarding values.
func caseDD22() goCertCase {
	return goCertCase{
		tc: "TC-DD-2.2", line: 25, role: roleDUT, pics: []string{"MCORE.ROLE.COMMISSIONER"},
		steps: []goStep{
			{n: "1.a", line: 30, title: "TH starts matter announcement procedure using BLE transport", na: ddNoRadio("BLE")},
			{n: "1.b", line: 34, title: "DUT starts BLE scan", na: ddNoRadio("BLE")},
			{n: "2.a", line: 46, title: "DUT starts scan in background using BLE transport", na: ddNoRadio("BLE")},
			{n: "2.b", line: 49, title: "TH starts matter announcement using BLE transport", na: ddNoRadio("BLE")},
			{n: "2.c", line: 53, title: "(The plan states only the expected outcome for this step.)", na: ddNoRadio("BLE")},
			{n: "3.a", line: 57, title: "TH publishes Wi-Fi PAF", na: ddNoRadio("Wi-Fi PAF")},
			{n: "3.b", line: 70, title: "DUT starts Wi-Fi PAF scan", na: ddNoRadio("Wi-Fi PAF")},
			{n: "6", line: 76, title: "DUT performs service discovery using DNS-SD", run: func(th *goTH) error {
				// recordDiscoveredTh (tc-dd-support.ts:1288-1321): the DUT
				// looks for the TH's long discriminator and must report a node
				// with the TH's instance, discriminator, vendor and product.
				qr, err := gocontroller.ParsePairingCode(thQR(th))
				if err != nil {
					return err
				}
				instances := commissionableInstances(th, th.dut)
				ctrl, err := th.controller("dut")
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(th.ctx, ddMDNSTimeout)
				defer cancel()
				nodes, err := ctrl.DiscoverWith(ctx, discovery.ByLongDiscriminator(qr.Discriminator), 10*time.Second, gocontroller.DiscoverOptions{})
				if err != nil {
					return fmt.Errorf("DUT commissionable discovery failed: %w", err)
				}
				var reported []string
				found := false
				for i := range nodes {
					n := &nodes[i]
					vid, pid := vendorProduct(n)
					reported = append(reported, fmt.Sprintf("%s (D=%d vendorId=%d productId=%d CM=%d)", n.InstanceID, n.D, vid, pid, n.CM))
					if (len(instances) == 0 || slices.ContainsFunc(instances, func(s string) bool { return strings.EqualFold(s, n.InstanceID) })) &&
						n.D == qr.Discriminator && vid == int(qr.VendorID) && pid == int(qr.ProductID) {
						found = true
					}
				}
				detail := fmt.Sprintf("DUT reported %v; the TH advertises as %v, its onboarding payload names D=%d vendorId=%d productId=%d",
					reported, instances, qr.Discriminator, qr.VendorID, qr.ProductID)
				if !found {
					return errors.New(detail)
				}
				th.note("%s", detail)
				// recordCommissionable (:1227-1234).
				names, err := th.names()
				if err != nil {
					return err
				}
				if _, err := th.commissionable(names, th.dut.info.discriminator, th.dut.info.port, ddMDNSTimeout); err != nil {
					return fmt.Errorf("TH advertising as commissionable: %w", err)
				}
				return nil
			}},
		},
	}
}

// vendorProduct reads a commissionable record's VP key ("vendor+product").
func vendorProduct(d *client.Device) (vid, pid int) {
	if d.VP == nil {
		return 0, 0
	}
	v, p, _ := strings.Cut(*d.VP, "+")
	vid, _ = strconv.Atoi(v)
	pid, _ = strconv.Atoi(p)
	return vid, pid
}

// --- TC-DD-3.5 ----------------------------------------------------------------

// ddStage is one STAGES entry of TC-DD-3.5 (TC-DD-3.5.test.ts:37-77): the
// daemon's line for a commissioning stage, in the order § 5.5 runs them.
var ddStages = map[string][]string{
	// "Received pairing request": the PBKDFParamRequest the daemon took.
	"discovered": {"msg=matter.rx.sc.pase ", " stage=pbkdf_param_req "},
	"pase":       logPake3,
	// ArmFailSafe answered; its ErrorCode is the commissioner's to judge,
	// and the DUT's commissioning fails on any but OK.
	"failSafe":    {"msg=matter.gencomm.cmd_ok command_name=ArmFailSafe "},
	"regulatory":  {"msg=matter.gencomm.cmd_ok command_name=SetRegulatoryConfig "},
	"csr":         {"msg=matter.opcreds.cmd_ok command_name=CSRRequest "},
	"trustedRoot": {"msg=matter.opcreds.cmd_ok command_name=AddTrustedRootCertificate "},
	"adminAcl":    {"msg=matter.opcreds.cmd_ok command_name=AddNOC "},
	// The CASE session the Sigma3 established (node/security.go
	// case.session_established), logged as the session is installed.
	"case":                  {"msg=case.session_established ", " fabric_index="},
	"commissioningComplete": {logCommissioned},
	// The daemon clears its PASE sessions after an OK CommissioningComplete
	// and logs their ids as it does (bridge/pase_close.go
	// closePASESessionsOnCommissioningComplete, matter.session.pase_close):
	// what matter.js's "Session ended" and chip's "Released - Type:1 LSID"
	// say (TC-DD-3.5.test.ts:72-75).
	"paseClosed": {"msg=matter.session.pase_close ", " reason=commissioning_complete "},
}

// caseDD35 is TC-DD-3.5 (TC-DD-3.5.test.ts:153-280), the Go controller as
// the DUT commissioner: each commissioning stage is a daemon log line
// after the stage the previous step found.
func caseDD35() goCertCase {
	d := &dutRun{}
	var commissionedFrom, cursor int
	started := false
	stage := func(name, what string) func(th *goTH) error {
		return func(th *goTH) error {
			if !started {
				return errors.New("step 2's commissioning did not start, so this stage cannot be checked")
			}
			parts := ddStages[name]
			if len(parts) == 0 {
				return fmt.Errorf("%s: the TH logs no line for this stage, so it cannot be checked", what)
			}
			line, err := awaitTHLine(th, cursor, certLogTimeout, parts...)
			if err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
			cursor = strings.Index(th.dut.snapshotStderr()[cursor:], line) + cursor + len(line)
			th.note("%s: %s", what, strings.TrimSpace(line))
			return nil
		}
	}
	onEthernet := "The TH is already on the operational network over Ethernet, so the Commissioner neither needs to configure network credentials nor to tell it to connect (TC-DD-3.5.test.ts:147-150)"
	return goCertCase{
		tc: "TC-DD-3.5", line: 153, role: roleDUT, pics: []string{"MCORE.ROLE.COMMISSIONER"},
		finalize: d.finalize,
		steps: []goStep{
			{n: "1", line: 154, title: "Commissioner has regulatory and fabric information and accurate date, time and timezone", run: func(*goTH) error {
				return goSkip("the date, time and timezone on the DUT need a person in front of the DUT; no adapter reads them (TC-DD-3.5.test.ts:158-163)")
			}},
			{n: "2", line: 167, title: "Commissioner and Commissionee discover each other and connect", run: func(th *goTH) error {
				if err := d.setup(th); err != nil {
					return err
				}
				if err := d.advertising(th); err != nil {
					return err
				}
				commissionedFrom, cursor, started = mark(th), mark(th), true
				if err := d.commission(th, th.dut.info.manualCode); err != nil {
					return err
				}
				return stage("discovered", "the DUT connected to the TH")(th)
			}},
			{n: "3", line: 179, title: "Establish encryption keys with PASE", run: stage("pase", "the TH established PASE")},
			{n: "4", line: 185, title: "Commissioner re-arms the Fail-safe timer", run: stage("failSafe", "the TH armed the fail-safe for the DUT")},
			{n: "5", line: 192, title: "Commissioner configures regulatory information", run: func(th *goTH) error {
				// notApplicableAtRun regulatoryNotRequired (:112-125): no
				// NetworkCommissioning instance with Wi-Fi or Thread.
				r, err := th.read(d.node, im.ConcreteAttributePath{Cluster: networkcommissioning.ClusterID, Attribute: spec.AttrFeatureMap, HasCluster: true, HasAttribute: true})
				if err != nil {
					return err
				}
				var maps []string
				radio := false
				for i := range r.Attributes {
					a := &r.Attributes[i]
					maps = append(maps, fmt.Sprintf("endpoint %d: %d", a.Path.Endpoint, a.Value.El.Uint))
					if a.Value.El.Uint&0b11 != 0 {
						radio = true
					}
				}
				if !radio {
					return goSkip("No Network Commissioning instance of the TH has Wi-Fi or Thread (%s), so the Commissioner need not configure regulatory information (Core § 5.5 step 8)", strings.Join(maps, "; "))
				}
				return stage("regulatory", "the TH set the regulatory configuration")(th)
			}},
			{n: "6", line: 201, title: "Commissioner requests the operational CSR", run: stage("csr", "the TH answered the CSRRequest")},
			{n: "7", line: 207, title: "Commissioner configures operational credentials", run: stage("trustedRoot", "the TH installed the trusted root certificate")},
			{n: "8", line: 213, title: "Commissioner configures itself as administrator in ACL", run: stage("adminAcl", "the TH created the administrator ACL entry")},
			{n: "9", line: 219, title: "Commissioner configures operational network on TH", na: onEthernet},
			{n: "10", line: 223, title: "Commissioner instructs Commissionee to connect to operational network", na: onEthernet},
			{n: "12", line: 229, title: "Commissioner starts discovery of TH using Operational Discovery", run: func(th *goTH) error {
				if d.node == nil {
					return errors.New("the DUT holds no node; step 2 must run first")
				}
				addr, _ := d.node.PeerAddress()
				instance := operationalInstance(d.ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
				if err := th.awaitOperational(d.names, instance, true, ddMDNSTimeout); err != nil {
					return err
				}
				th.note("the TH advertises %s, the operational instance the DUT's discovery looks for", instance)
				return nil
			}},
			// matter.js matches /SecureSession +Created secure CASE session/ on its TH
			// (TC-DD-3.5.test.ts:63-67), logged when the session is created
			// (NodeSession.ts:157-158); case.session_established is the same event.
			{n: "13", line: 245, title: "Commissioner opens a CASE session with TH", run: stage("case", "the TH established CASE")},
			{n: "14", line: 251, title: "Commissioner sends CommissioningComplete", run: func(th *goTH) error {
				if err := stage("commissioningComplete", "the TH received CommissioningComplete")(th); err != nil {
					return err
				}
				if _, err := awaitTHLine(th, commissionedFrom, ddCommissioningLogTimeout, logCommissioned); err != nil {
					return fmt.Errorf("the TH completed the commissioning with success: %w", err)
				}
				return nil
			}},
			{n: "15", line: 272, title: "Commissioning channel between the Commissioner and Commissionee is terminated", run: stage("paseClosed", "the TH closed the PASE session")},
		},
	}
}
