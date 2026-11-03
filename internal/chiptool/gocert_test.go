// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The Go-TH leg: the certification cases matter.js automated in
// support/chip-testing/test/cert/TC-*.test.ts (862ad75f), read mirrored and
// run with the module's own controller as the test harness against the
// reference daemon as the DUT.
//
// In matter.js's cert suite the DUT is, in most cases, a controller — its
// own CommissioningController — and the TH a device app
// (support/chip-testing/README.md:112-118). The CHIP test plans these
// files implement are the controller halves CHIP's harness runs as manual
// cases, which is why the families leave them as "manual" rows. Read
// mirrored, the same files describe the device half: the step list and
// the TH-side expectations are what the reference daemon has to satisfy,
// and the DUT-controller actions are what the Go TH performs. Each case
// below cites its TS file and the line of every step it mirrors; a step's
// PICS is the TS's, mirrored onto the server side (mirrorPICS) and
// evaluated against the daemon's own generated slices, and a step whose
// PICS is false is recorded skipped with the expression, never passed.
//
// The leg needs the daemon binary and the host's multicast, nothing of the
// CHIP harness image; it skips, naming why, where multicast is
// unavailable. What a passing case executed is pinned in
// testdata/chip-cases.golden.json under goCert (steps run and skipped),
// regenerated with -update-chip-cases like the families' counts.
//
// A case of role roleDUT runs in matter.js's own direction instead: the
// module's controller is the DUT, the reference daemon the TH device
// (phase 29b). Its PICS are the controller's own declarations
// (goTHPICS), evaluated as stated; a step's evidence is named at the
// step — the DUT controller's return value or error, a state read on the
// daemon, or a daemon log line — because matter.js proves its DUT by the
// TH device's log, which a Go TH does not print.

// goCertEnv narrows the leg to families or case ids ("IDM,SC/4.2").
const goCertEnv = "GOFABRIC_GOCERT"

// goCertLongEnv runs the steps matter.js marks longRunning, which cost
// minutes of real time each: matter.js skips them unless
// MATTER_CERT_LONG_RUNNING is set (packages/testing/src/chip/cert/
// cert-test.ts:318-322, longRunningEnabled :728-731). A run with it set
// checks no golden counts and writes none: the golden records the default
// run.
const goCertLongEnv = "GOFABRIC_GOCERT_LONG"

// goCertLong reports whether the run includes the long-running steps.
func goCertLong() bool {
	v := strings.ToLower(os.Getenv(goCertLongEnv))
	return v != "" && v != "0" && v != "false"
}

// goRole is who the Go controller is in a case: the TH (29a, the TS file
// read mirrored) or the DUT (29b, the TS file in matter.js's own
// direction).
type goRole uint8

const (
	// roleTH: the daemon is the DUT, the Go controller performs the TS
	// file's controller actions as TH.
	roleTH goRole = iota
	// roleDUT: the Go controller is the DUT, the daemon is the TH device
	// the TS file names (support/chip-testing/README.md:112-118).
	roleDUT
)

// goCertCase is one matter.js case file, read mirrored (roleTH) or in its
// own direction (roleDUT).
type goCertCase struct {
	// tc is the matter.js case id ("TC-IDM-1.1"); its file is
	// support/chip-testing/test/cert/<tc>.test.ts.
	tc string
	// line is the line of the file's certTest( registration.
	line int
	// pics is the case's own PICS list (certTest pics), as the TS states it.
	pics []string
	// at is the server PICS expression whose first endpoint the case's
	// steps run against ("SWTCH.S"); "" is the root endpoint.
	at    string
	steps []goStep
	// multicast marks a case that sends a Matter group message: it needs
	// an IPv6-capable LAN interface, as the families' multicast cases do
	// (familyrun_test.go requireIPv6), and skips whole without one.
	multicast bool
	// finalize runs after the last step whatever happened, as the cert
	// engine runs a case's finalizer in its finally block
	// (packages/testing/src/chip/cert/cert-test.ts:498-502).
	finalize func(th *goTH)
	// native marks a case whose DUT is a device in matter.js too (the
	// BDX receiver, SU requestor, BIND switch cases): its PICS are the
	// device's as stated and are not mirrored onto the server side.
	native bool
	// daemonArgs are the DUT daemon's further flags (bridgeOptions.extraArgs:
	// --light-switch for BIND); its endpoints beyond the committed slices'
	// are the flagged composition's.
	daemonArgs []string
	// listen is the DUT daemon's --listen; "" is the Matter port. A case
	// whose TH device receives group messages leaves the port to it.
	listen string
	// declare are PICS codes of the composition daemonArgs adds, which the
	// committed slices (generated from the default composition) do not
	// describe; each case states where its values come from.
	declare map[string]string
	// role is the Go controller's role; the zero value is roleTH.
	role goRole
}

// goStep is one TS step.
type goStep struct {
	n    string // the TS step number ("1", "0a")
	line int    // the line of the TS file's .step( call
	// file is the TS file of the .step( call where it is not the case's
	// own — a support file that defines the steps of several cases
	// (tc-dd-flow-support.ts); "" is the case's file.
	file  string
	title string
	// pics is the TS step's PICS expression, client side; the runner
	// mirrors it onto the server side.
	pics string
	// need is a further server PICS expression the mirrored step needs of
	// the daemon — the attribute or event the step reads or writes.
	need string
	// at overrides the case's endpoint code for this step.
	at string
	// na is the TS's notApplicable reason: the step is recorded skipped.
	na string
	// long is the TS's longRunning reason: the step is skipped unless
	// GOFABRIC_GOCERT_LONG is set ([goCertLongEnv]).
	long string
	// finding is the id of a findings-register bullet
	// (notes/parity/matter_behaviour_findings.md): the step fails with that
	// daemon defect, and the case passes with it recorded. A step that
	// then passes fails the run: the finding is closed and its id goes.
	finding string
	run     func(th *goTH) error
}

// goVerdict is a step's verdict: matter.js StepVerdict's pass | fail |
// skipped (packages/testing/src/chip/cert/cert-context.ts:103).
type goVerdict string

const (
	goPass    goVerdict = "pass"
	goFail    goVerdict = "fail"
	goSkipped goVerdict = "skipped"
)

// goStepRecord is one recorded step, as tc-support.ts:104 record keeps it.
type goStepRecord struct {
	step    goStep
	verdict goVerdict
	detail  string
}

// errGoSkip is a step that skips itself at run time (matter.js
// notApplicableAtRun, cert-dsl.ts:156-206), with its reason.
type errGoSkip struct{ reason string }

func (e errGoSkip) Error() string { return "skipped: " + e.reason }

func goSkip(format string, args ...any) error { return errGoSkip{reason: fmt.Sprintf(format, args...)} }

// goCertCounts is a passing Go-TH case's counts in the golden file, with
// the matter.js file it mirrors.
type goCertCounts struct {
	Steps        int    `json:"steps"`
	StepsSkipped int    `json:"stepsSkipped"`
	Source       string `json:"source"`
	// Findings are the steps that fail with a recorded daemon defect,
	// "<step>: <finding id>".
	Findings []string `json:"findings,omitempty"`
	// Role is "dut" for a case the Go controller runs as the DUT
	// (roleDUT); empty is the TH.
	Role string `json:"role,omitempty"`
}

// goCertCases is the leg's case registry, keyed by the family runner's
// case id ("IDM/1.1", familyrun_test.go:411). Each entry builds a fresh
// case, so a case's closure state starts empty on every run.
var goCertCases = map[string]func() goCertCase{}

// registerGoCert adds a case under its family id.
func registerGoCert(build func() goCertCase) {
	c := build()
	id := goCertID(c.tc)
	if _, dup := goCertCases[id]; dup {
		panic("duplicate Go-TH case " + id)
	}
	goCertCases[id] = build
}

// goCertID is "TC-IDM-1.1" → "IDM/1.1".
func goCertID(tc string) string {
	s := strings.TrimPrefix(tc, "TC-")
	i := strings.LastIndex(s, "-")
	return s[:i] + "/" + s[i+1:]
}

func (c goCertCase) source() string { return c.tc + ".test.ts" }

// TestGoCertificationCases runs the Go-TH leg: every registered case,
// against a fresh reference daemon each, with a fresh Go controller as TH.
func TestGoCertificationCases(t *testing.T) {
	bin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bin, "db", "listen", "wifi", "media", "app-pipe")
	if os.Getenv(goCertEnv) != "" && *updateChipCases {
		t.Logf("%s narrows the leg: only the cases run are rewritten in the golden file", goCertEnv)
	}
	names, err := discovery.Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	_ = names.Close()
	pics := loadCommittedPICS(t)
	var only []string
	if v := os.Getenv(goCertEnv); v != "" {
		only = strings.Split(v, ",")
	}
	if goCertLong() {
		t.Logf("%s is set: the long-running steps run, and the golden counts are neither checked nor written", goCertLongEnv)
	}
	ids := make([]string, 0, len(goCertCases))
	for id := range goCertCases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ran := map[string]goCertCounts{}
	for _, id := range ids {
		fam, _, _ := strings.Cut(id, "/")
		if only != nil && !slices.Contains(only, fam) && !slices.Contains(only, id) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			c := goCertCases[id]()
			counts, ok := runGoCertCase(t, bin, flags, pics, c)
			if !ok {
				return
			}
			if goCertLong() {
				t.Logf("%s ran %+v (long-running steps included)", id, counts)
				return
			}
			ran[id] = counts
			checkGoCertCounts(t, id, counts)
		})
	}
	writeGoCertGolden(t, ran)
}

// runGoCertCase runs one case and reports its counts, ok when every step
// passed or skipped.
func runGoCertCase(t *testing.T, bin string, flags map[string]bool, pics *committedPICS, c goCertCase) (goCertCounts, bool) {
	t.Helper()
	if has, candidates := lanIPv6(); c.multicast && !has {
		t.Skipf("skipped, %s: the case sends a Matter group message (IPv6 multicast), and no up multicast interface of this host has IPv6 (%v)", classHarness, candidates)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	th := newGoTH(ctx, t, bin, flags, pics, c)
	defer th.close()
	if c.role == roleDUT {
		t.Logf("%s: support/chip-testing/test/cert/%s:%d at matter.js 862ad75f, in its own direction (the Go controller is the DUT, the daemon the TH)", c.tc, c.source(), c.line)
	} else {
		t.Logf("%s: support/chip-testing/test/cert/%s:%d at matter.js 862ad75f, read mirrored (the daemon is the DUT)", c.tc, c.source(), c.line)
	}
	if expr := strings.Join(c.pics, " & "); expr != "" {
		if missing := th.undeclared(expr); len(missing) > 0 {
			t.Fatalf("%s: the case's PICS %q names %v, which the Go controller as DUT declares nothing about (goTHPICS)", c.tc, expr, missing)
		}
		m := th.casePICS(expr)
		if ok, err := th.picsHolds(m, th.caseEP()); err != nil || !ok {
			t.Fatalf("%s: the case's PICS %q (mirrored %q) is false for the reference daemon (%v): the case does not apply", c.tc, expr, m, err)
		}
	}
	counts := goCertCounts{Source: c.source()}
	if c.role == roleDUT {
		counts.Role = "dut"
	}
	failed := false
	for i := range c.steps {
		s := &c.steps[i]
		r := th.runStep(*s)
		file := c.source()
		if s.file != "" {
			file = s.file
		}
		where := fmt.Sprintf("%s step %s (%s:%d)", c.tc, r.step.n, file, r.step.line)
		switch {
		case r.verdict == goPass && s.finding != "":
			failed = true
			t.Errorf("%s: passed, but it is recorded as failing with finding %s — the defect is fixed: drop the "+
				"finding from the step and from notes/parity/matter_behaviour_findings.md (%s)", where, s.finding, r.detail)
		case r.verdict == goPass:
			counts.Steps++
			t.Logf("%s: pass — %s", where, r.detail)
		case r.verdict == goSkipped:
			counts.StepsSkipped++
			t.Logf("%s: skipped — %s", where, r.detail)
		case s.finding != "":
			counts.Steps++
			counts.Findings = append(counts.Findings, s.n+": "+s.finding)
			t.Logf("%s: fail, recorded as finding %s — %s", where, s.finding, r.detail)
		default:
			failed = true
			t.Errorf("%s: fail — %s", where, r.detail)
		}
	}
	if c.finalize != nil {
		c.finalize(th)
	}
	if failed {
		th.dut.dump(t)
		t.Logf("the Go controllers' last log lines:\n%s", th.logs.tail(200))
		return counts, false
	}
	return counts, true
}

// --- the TH ------------------------------------------------------------------

// goTH is a case's test harness: the started reference daemon, the Go
// controllers that act as the TS file's DUT controllers, and the step's
// notes.
type goTH struct {
	t     *testing.T
	ctx   context.Context
	c     goCertCase
	bin   string
	flags map[string]bool
	pics  *committedPICS
	dut   *bridgeProcess
	log   *slog.Logger
	// ep is the endpoint the running step's PICS were evaluated on.
	ep    uint16
	notes []string
	ctrls []*gocontroller.Controller
	extra []*bridgeProcess
	logs  syncBuffer
	// extraEPs are the DUT's endpoints its daemonArgs add.
	extraEPs []uint16
}

// syncBuffer is a log sink the controllers write to concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String is everything written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// tail is the last n lines.
func (b *syncBuffer) tail(n int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := strings.Split(strings.TrimRight(b.buf.String(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// runStep runs one step through the gates matter.js's engine applies in
// order (cert-test.ts): notApplicable, then the step's PICS (:272), then
// the step itself.
func (th *goTH) runStep(s goStep) goStepRecord {
	th.t.Helper()
	rec := goStepRecord{step: s}
	if s.na != "" {
		rec.verdict, rec.detail = goSkipped, "not applicable: "+s.na
		return rec
	}
	at := s.at
	if at == "" {
		at = th.c.at
	}
	ep, ok := th.endpointOf(at)
	if !ok {
		rec.verdict, rec.detail = goSkipped, fmt.Sprintf("PICS %q holds on no endpoint of the reference daemon", at)
		return rec
	}
	var gate []string
	if s.pics != "" {
		if missing := th.undeclared(s.pics); len(missing) > 0 {
			rec.verdict, rec.detail = goFail, fmt.Sprintf("PICS %q names %v, which the Go controller as DUT declares nothing about (goTHPICS)", s.pics, missing)
			return rec
		}
		gate = append(gate, "("+th.casePICS(s.pics)+")")
	}
	if s.need != "" {
		gate = append(gate, "("+s.need+")")
	}
	if len(gate) > 0 {
		expr := strings.Join(gate, " & ")
		holds, err := th.picsHolds(expr, ep)
		if err != nil {
			rec.verdict, rec.detail = goFail, fmt.Sprintf("PICS %q: %v", expr, err)
			return rec
		}
		if !holds {
			rec.verdict = goSkipped
			rec.detail = fmt.Sprintf("PICS %q not met on endpoint %d", expr, ep)
			if s.pics != "" {
				rec.detail += fmt.Sprintf(" (the TS step's %q)", s.pics)
			}
			return rec
		}
	}
	// matter.js's next gate: a longRunning step is skipped unless the run
	// asked for them (cert-test.ts:318-322).
	if s.long != "" && !goCertLong() {
		rec.verdict, rec.detail = goSkipped, fmt.Sprintf("long-running: %s; set %s=1 to run it", s.long, goCertLongEnv)
		return rec
	}
	th.ep = ep
	th.notes = th.notes[:0]
	err := th.call(s.run)
	var skip errGoSkip
	switch {
	case errors.As(err, &skip):
		rec.verdict, rec.detail = goSkipped, skip.reason
	case err != nil:
		rec.verdict, rec.detail = goFail, err.Error()
	default:
		rec.verdict, rec.detail = goPass, strings.Join(th.notes, "; ")
	}
	return rec
}

// call runs a step, a panic recorded as its failure: a step that runs
// after an earlier one failed finds its state missing.
func (th *goTH) call(run func(th *goTH) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the step panicked: %v", r)
		}
	}()
	return run(th)
}

func newGoTH(ctx context.Context, t *testing.T, bin string, flags map[string]bool, pics *committedPICS, c goCertCase) *goTH {
	t.Helper()
	pipe := filepath.Join(t.TempDir(), "app-pipe")
	// The Matter port: a group message goes to it (groups/sender.go
	// groupPort, GroupSession.ts:150-153), and the cases run one at a time.
	listen := c.listen
	if listen == "" {
		listen = fmt.Sprintf(":%d", matterPort)
	}
	dut := startBridgeWith(t, bin, flags, bridgeOptions{appPipe: pipe, listen: listen, extraArgs: c.daemonArgs})
	th := &goTH{t: t, ctx: ctx, c: c, bin: bin, flags: flags, pics: pics, dut: dut}
	// The controllers' log, kept for a failing case's dump.
	th.log = slog.New(slog.NewTextHandler(&th.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	th.checkTopology()
	return th
}

// checkTopology holds the daemon's endpoints to the committed slices: the
// PICS the steps are gated by describe this composition only.
func (th *goTH) checkTopology() {
	th.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out := th.dut.snapshotStdout()
		if strings.Contains(out, "topology end") {
			eps := make([]uint16, 0, len(th.pics.sorted))
			for _, m := range reTopologyLine.FindAllStringSubmatch(out, -1) {
				n, _ := strconv.Atoi(m[1])
				eps = append(eps, uint16(n)) //nolint:gosec // an endpoint number
			}
			slices.Sort(eps)
			if len(th.c.daemonArgs) > 0 && len(eps) > len(th.pics.sorted) && slices.Equal(eps[:len(th.pics.sorted)], th.pics.sorted) {
				// The flagged composition's endpoints come after every
				// committed one (light_switch.go: the switch comes last).
				th.extraEPs = eps[len(th.pics.sorted):]
				return
			}
			if !slices.Equal(eps, th.pics.sorted) {
				th.t.Fatalf("the daemon serves endpoints %v, the committed PICS slices describe %v: regenerate them "+
					"(make chiptool-families-update) before the Go-TH leg can gate on them", eps, th.pics.sorted)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	th.t.Fatal("the reference daemon printed no topology block")
}

func (th *goTH) close() {
	for _, c := range slices.Backward(th.ctrls) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = c.Close(ctx)
		cancel()
	}
	for _, b := range th.extra {
		b.stop()
	}
}

// note adds to the running step's evidence.
func (th *goTH) note(format string, args ...any) {
	th.notes = append(th.notes, fmt.Sprintf(format, args...))
}

// device starts the run's index-th further device as matter.js's
// identityFor gives it its identity (packages/testing/src/chip/cert/
// cert-dsl.ts:297-312): the primary's discriminator and passcode plus the
// index, with the daemon's further flags (bridgeOptions.extraArgs), on a
// port of its own.
func (th *goTH) device(index int, args ...string) *bridgeProcess {
	return th.deviceOn(index, ":0", args...)
}

// deviceOn is device on a given --listen: the Matter port for a device
// that receives group messages, which go to it (groups/sender.go
// groupPort).
func (th *goTH) deviceOn(index int, listen string, args ...string) *bridgeProcess {
	extra := append([]string{
		"--discriminator", strconv.Itoa(int(th.dut.info.discriminator) + index),
		"--passcode", strconv.Itoa(20202021 + index),
	}, args...)
	b := startBridgeWith(th.t, th.bin, th.flags, bridgeOptions{listen: listen, extraArgs: extra})
	th.extra = append(th.extra, b)
	return b
}

// caseEP is the case's endpoint (0 when its code names none).
func (th *goTH) caseEP() uint16 {
	ep, _ := th.endpointOf(th.c.at)
	return ep
}

// endpointOf is the first endpoint on which the server PICS expression
// holds ("" is the root).
func (th *goTH) endpointOf(expr string) (uint16, bool) {
	if expr == "" {
		return 0, true
	}
	for _, ep := range th.pics.sorted {
		if ok, _ := th.picsHolds(expr, ep); ok {
			return ep, true
		}
	}
	return 0, false
}

// mustEP is endpointOf for a step that cannot run without the endpoint.
func (th *goTH) mustEP(code string) (uint16, error) {
	ep, ok := th.endpointOf(code)
	if !ok {
		return 0, goSkip("no endpoint of the reference daemon serves %s", code)
	}
	return ep, nil
}

// picsHolds evaluates a server-side expression against the daemon's hand
// declarations, its root slice and the slice of ep.
func (th *goTH) picsHolds(expr string, ep uint16) (bool, error) {
	ast, err := parsePICSExpr(expr)
	if err != nil {
		return false, err
	}
	// Each slice describes one endpoint in full, so a root-node code reads 0
	// on every other endpoint's slice: a code holds when the step's
	// endpoint, the root or the hand declarations declare it.
	values := map[string]string{}
	for _, src := range []map[string]string{th.pics.base, th.pics.byEP[0], th.pics.byEP[ep]} {
		for k, v := range src {
			if values[k] != "1" {
				values[k] = v
			}
		}
	}
	// The Go TH's own declarations and the flagged composition's win over
	// the daemon's files, as a controller's PICS overlay the device file in
	// matter.js's runs (packages/testing/src/chip/cert/cert-app-pics.ts:
	// 70-80, picsWithOverrides).
	for _, src := range []map[string]string{goTHPICS, th.c.declare} {
		maps.Copy(values, src)
	}
	return ast.evaluate(values), nil
}

// casePICS is a TS PICS expression as the leg evaluates it: as stated for
// a native case and for a case the Go controller runs as the DUT,
// mirrored onto the server side otherwise.
func (th *goTH) casePICS(expr string) string {
	if th.c.native || th.c.role == roleDUT {
		return expr
	}
	return mirrorPICS(expr)
}

// undeclared are the codes of a DUT case's PICS expression the Go
// controller declares nothing about: such a code would otherwise read the
// daemon's value, a statement about the TH device, where the TS states
// one about the DUT controller. Empty for a TH case.
func (th *goTH) undeclared(expr string) []string {
	if th.c.role != roleDUT {
		return nil
	}
	var out []string
	for _, code := range rePICSCode.FindAllString(expr, -1) {
		if _, ok := goTHPICS[code]; !ok && !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	return out
}

// goTHPICS is what the Go controller declares about itself, as TH and as
// DUT alike: the codes of the roles it takes in these cases — OTA provider
// and BDX sender and responder, the administrator that announces a
// provider, the commissioner that scans a QR code or reads a manual code,
// the ICD client — after matter.js's declarations for its own controller
// (support/chip-testing/src/cert/InProcessControllerAdapter.ts:177-308,
// MATTERJS_CONTROLLER_PICS), each with the line matter.js declares it at.
// A code matter.js leaves to the CHIP PICS file its runs load
// (src/app/tests/suites/certification/ci-pics-values at connectedhomeip
// 6170af84, packages/testing/src/chip/config.ts:22) is marked "own" with
// that file's line and the reason the value holds for this controller.
// The reference daemon is none of these in any case of the leg, so the
// codes name the Go controller in either reading and are not mirrored —
// except where the mirror table names a code (picsSpecial), which a TH case
// keeps mirroring. A DUT case may only name codes declared here (undeclared).
var goTHPICS = map[string]string{
	"MCORE.ROLE.COMMISSIONER":          "1", // :179
	"MCORE.DD.QR_COMMISSIONING":        "1", // :180
	"MCORE.DD.MANUAL_PC_COMMISSIONING": "1", // :181
	"MCORE.DD.SCAN_QR_CODE":            "1", // :184
	// Own: ParsePairingCode reads both manual code lengths
	// (secure/setup.DecodeManualCode); ci-pics-values:1117-1118.
	"MCORE.DD.11_MANUAL_PC": "1",
	"MCORE.DD.21_MANUAL_PC": "1",
	// Own: Pair commissions a commissionable device whatever flow its QR
	// code names — the flow is parsed (PairingCode.Flow), and the device's
	// commissioning mode is the TH's to enter; ci-pics-values:1114-1116.
	"MCORE.DD.STANDARD_COMM_FLOW":    "1",
	"MCORE.DD.USER_INTENT_COMM_FLOW": "1",
	"MCORE.DD.CUSTOM_COMM_FLOW":      "1",
	// Own: the controller discovers over BLE (PairOptions.BLE, ADR 0024)
	// and has no Wi-Fi PAF; ci-pics-values:1128 and :1095 state the same.
	"MCORE.DD.DISCOVERY_BLE": "1",
	"MCORE.DD.DISCOVERY_PAF": "0",
	// Own: client.Node.ICD() is the IcdManagement client; ci-pics-values:2761.
	"ICDM.C":                       "1",
	"ICDM.C.C00.Tx":                "1", // :214
	"ICDM.C.C02.Tx":                "1", // :215
	"ICDM.C.C03.Tx":                "1", // :216
	"ICDB.C":                       "1", // :240
	"MCORE.BDX.Sender":             "1", // :262
	"MCORE.BDX.Responder":          "1", // :263
	"MCORE.BDX.SynchronousSender":  "1", // :264
	"MCORE.OTA.Provider":           "1", // :276
	"MCORE.OTA.HTTPS":              "0", // :281
	"MCORE.ACL.Administrator":      "1", // :286
	"OTAR.C.M.AnnounceOTAProvider": "1", // :287
	"OTAP.S.M.DelayedActionTime":   "1", // :296
	"OTAP.S.M.UserConsentNeeded":   "1", // :297
	// The bridge client: client.Node holds a bridge's endpoints, their
	// names, state and battery in its image, kept by its sustained
	// subscription (controller/client doc.go).
	"MCORE.BRIDGECLIENT":           "1", // :303
	"MCORE.DEVLIST.UseDevices":     "1", // :304
	"MCORE.DEVLIST.UseDeviceName":  "1", // :305
	"MCORE.DEVLIST.UseDeviceState": "1", // :306
	"MCORE.DEVLIST.UseBatInfo":     "1", // :307
}

// declares is picsHolds for one code on the step's endpoint.
func (th *goTH) declares(code string) bool {
	ok, _ := th.picsHolds(code, th.ep)
	return ok
}

// controller builds a Go controller with its own fabric — one per TS
// controller role (dut, th_cr2, …).
func (th *goTH) controller(label string) (*gocontroller.Controller, error) {
	return th.controllerWith(label, nil)
}

// controllerWith is controller with the configuration amended by edit
// (nil: none) — the TCP roles of the TCP cases.
func (th *goTH) controllerWith(label string, edit func(*gocontroller.Config)) (*gocontroller.Controller, error) {
	db, err := gocontroller.OpenDB(filepath.Join(th.t.TempDir(), label+".db"))
	if err != nil {
		return nil, err
	}
	th.t.Cleanup(func() { _ = db.Close() })
	cfg := gocontroller.Config{DB: db, Attestation: attestation.TestStore(), Label: label, Logger: th.log}
	if edit != nil {
		edit(&cfg)
	}
	c, err := gocontroller.New(th.ctx, cfg)
	if err != nil {
		return nil, err
	}
	th.ctrls = append(th.ctrls, c)
	return c, nil
}

// pair commissions the device a code names with c, without the default
// subscription: the steps subscribe themselves, as the TS's do.
func (th *goTH) pair(c *gocontroller.Controller, code string) (*client.Node, error) {
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	off := false
	return c.Pair(ctx, code, gocontroller.PairOptions{AutoSubscribe: &off, OnAttestationFailure: attestation.Accept()})
}

// commission is the TS's `dut.commission({passcode, discriminator})`: a
// fresh controller commissions the daemon with its own pairing code.
func (th *goTH) commission(label string) (*gocontroller.Controller, *client.Node, error) {
	c, err := th.controller(label)
	if err != nil {
		return nil, nil, err
	}
	n, err := th.pair(c, th.dut.info.manualCode)
	if err != nil {
		return nil, nil, fmt.Errorf("commission the daemon: %w", err)
	}
	return c, n, nil
}

// pipeAsync writes one app-pipe command and returns at once; wait blocks
// until the daemon logged it applied. The daemon applies a switch sequence
// before it logs it (examples/reference-bridge fleet_sensors.go execute),
// so a step that reads the switch while it is pressed cannot use
// bridgeProcess.pipe, which waits.
func (th *goTH) pipeAsync(command map[string]any) (wait func() error, err error) {
	line, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	name, _ := command["Name"].(string)
	offset := len(th.dut.snapshotStderr())
	w, err := os.OpenFile(th.dut.opts.appPipe, os.O_WRONLY, 0) //nolint:gosec // the daemon's own FIFO
	if err != nil {
		return nil, err
	}
	_, err = w.Write(append(line, '\n'))
	_ = w.Close()
	if err != nil {
		return nil, err
	}
	return func() error {
		ok := waitFor(th.ctx, 30*time.Second, func() bool {
			return strings.Contains(th.dut.snapshotStderr()[offset:], "msg=apppipe.applied name="+name)
		})
		if !ok {
			return fmt.Errorf("app-pipe command %s: no apppipe.applied within 30s", line)
		}
		return nil
	}, nil
}

// --- interactions --------------------------------------------------------------

func attrPath(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func cmdPath(ep uint16, cluster, cmd uint32) client.CommandPath {
	return client.CommandPath{Endpoint: ep, Cluster: cluster, Command: cmd}
}

// read reads attribute paths through n.
func (th *goTH) read(n *client.Node, paths ...im.ConcreteAttributePath) (*client.Report, error) {
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	// IncludeKnownVersions: the node's cache would otherwise send its data
	// versions along and the DUT rightly omit what has not changed
	// (client.Node.Read), which is not the read the TS step sends.
	return n.Read(ctx, client.ReadRequest{ReadRequest: readOf(paths...), IncludeKnownVersions: true})
}

// readValue reads one attribute and returns its value.
func (th *goTH) readValue(n *client.Node, p im.ConcreteAttributePath) (spec.Node, error) {
	r, err := th.read(n, p)
	if err != nil {
		return spec.Node{}, err
	}
	for _, s := range r.Statuses {
		if s.Path.Endpoint == p.Endpoint && s.Path.Cluster == p.Cluster && s.Path.Attribute == p.Attribute {
			return spec.Node{}, fmt.Errorf("read %s: status %s", describePath(p), s.Status)
		}
	}
	v, ok := r.Value(p.Endpoint, p.Cluster, p.Attribute)
	if !ok {
		return spec.Node{}, fmt.Errorf("read %s: no value in the report", describePath(p))
	}
	return v, nil
}

// write writes one value and fails on any status but Success.
func (th *goTH) write(n *client.Node, p im.ConcreteAttributePath, v spec.Encodable, opts client.WriteOptions) error {
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	st, err := n.Write(ctx, []client.WriteRequest{{Path: p, Value: v}}, opts)
	if err != nil {
		return fmt.Errorf("write %s: %w", describePath(p), err)
	}
	if err := st.Err(); err != nil {
		return fmt.Errorf("write %s: %w", describePath(p), err)
	}
	return nil
}

// invoke invokes a command and fails on any status but Success; with
// statusField it also holds the response's status field (tag 0) to 0, as
// tc-support.ts invokeCommand does for a response that carries one
// (:1609-1622).
func (th *goTH) invoke(n *client.Node, p client.CommandPath, fields spec.Encodable, statusField bool, opts client.InvokeOptions) (client.Result, error) {
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	r, err := n.Invoke(ctx, p, fields, opts)
	if err != nil {
		return r, fmt.Errorf("invoke 0x%04X/0x%02X on endpoint %d: %w", p.Cluster, p.Command, p.Endpoint, err)
	}
	if statusField {
		st, ok := structField(r.Fields, 0)
		if !ok || st.El.Uint != 0 {
			return r, fmt.Errorf("invoke 0x%04X/0x%02X on endpoint %d: response status field %s", p.Cluster, p.Command, p.Endpoint, describeNode(st))
		}
	}
	return r, nil
}

func readOf(paths ...im.ConcreteAttributePath) client.ReadPaths {
	return client.ReadPaths{Attributes: paths}
}

// structField returns a structure's member with context tag tag.
func structField(n spec.Node, tag uint8) (spec.Node, bool) {
	for _, c := range n.Children {
		if c.El.Tag == tlv.ContextTag(tag) {
			return c, true
		}
	}
	return spec.Node{}, false
}

func describePath(p im.ConcreteAttributePath) string {
	ep := "*"
	if p.HasEndpoint {
		ep = strconv.Itoa(int(p.Endpoint))
	}
	return fmt.Sprintf("%s/0x%04X/0x%04X", ep, p.Cluster, p.Attribute)
}

// describeNode renders a value for a step's evidence.
func describeNode(n spec.Node) string {
	switch {
	case n.El.IsNull:
		return "null"
	case n.El.IsContainer:
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			parts = append(parts, describeNode(c))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case n.El.Octets != nil:
		return hex.EncodeToString(n.El.Octets)
	}
	switch n.El.Type {
	case tlv.TypeBoolFalse, tlv.TypeBoolTrue:
		return strconv.FormatBool(n.El.Bool)
	case tlv.TypeUTF8Str1, tlv.TypeUTF8Str2, tlv.TypeUTF8Str4, tlv.TypeUTF8Str8:
		return strconv.Quote(n.El.String)
	case tlv.TypeSignedInt1, tlv.TypeSignedInt2, tlv.TypeSignedInt4, tlv.TypeSignedInt8:
		return strconv.FormatInt(n.El.Int, 10)
	case tlv.TypeFloat4, tlv.TypeFloat8:
		return strconv.FormatFloat(n.El.Float, 'g', -1, 64)
	default:
	}
	return strconv.FormatUint(n.El.Uint, 10)
}

// encodable adapts an encoding function to spec.Encodable.
type encodable func(enc *tlv.Encoder, tag tlv.Tag)

func (f encodable) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { f(enc, tag) }

// listOf encodes a list of structures.
func listOf[T spec.Encodable](items ...T) spec.Encodable {
	return encodable(func(enc *tlv.Encoder, tag tlv.Tag) { spec.PutList(spec.PutStruct[T])(enc, tag, items) })
}

// emptyFields is a command without fields.
var emptyFields = encodable(func(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	_ = enc.EndContainer()
})

// waitFor polls cond until it holds or d passes.
func waitFor(ctx context.Context, d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		select {
		case <-ctx.Done():
			return cond()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return cond()
}

// --- PICS ----------------------------------------------------------------------

// committedPICS is the daemon's PICS as committed: the hand declarations
// (testdata/reference-bridge.pics) and the slices generated from the
// device (testdata/pics/ep<N>.txt). The leg gates on these without the
// harness image; TC-IDM-10.4 holds the slices against the device in the
// families, and checkTopology holds this daemon to their endpoints.
type committedPICS struct {
	base   map[string]string
	byEP   map[uint16]map[string]string
	sorted []uint16
}

func loadCommittedPICS(t *testing.T) *committedPICS {
	t.Helper()
	base, err := readPICS(filepath.Join("testdata", "reference-bridge.pics"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("testdata", "pics", "ep*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no committed PICS slices under testdata/pics (%v)", err)
	}
	p := &committedPICS{base: base, byEP: map[uint16]map[string]string{}}
	for _, f := range files {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "ep"), ".txt"))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		v, err := readPICS(f)
		if err != nil {
			t.Fatal(err)
		}
		p.byEP[uint16(n)] = v //nolint:gosec // an endpoint number
		p.sorted = append(p.sorted, uint16(n))
	}
	slices.Sort(p.sorted)
	return p
}

// picsMirror maps the client-side codes a TS case states onto the server
// codes that describe the same capability of the device:
//
//   - X.C → X.S, X.C.Fnn → X.S.Fnn, X.C.Annnn → X.S.Annnn, X.C.Enn → X.S.Enn;
//   - X.C.Cnn.Tx (the client sends) → X.S.Cnn.Rsp (the server accepts);
//   - MCORE.IDM.C… (the controller's interactions) → MCORE.IDM.S, the
//     device's Interaction Model server, which has no per-interaction codes;
//   - MCORE.ROLE.COMMISSIONER → MCORE.ROLE.COMMISSIONEE;
//   - the Switch client's manual polling and eventing codes → the server's
//     CurrentPosition attribute and InitialPress event, which are what a
//     client polls and receives.
var (
	rePICSCode   = regexp.MustCompile(`[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+`)
	rePICSClient = regexp.MustCompile(`^([A-Z0-9]+)\.C(\..*)?$`)
	rePICSTx     = regexp.MustCompile(`^(C[0-9a-fA-F]{2})\.Tx$`)
	picsSpecial  = map[string]string{
		"MCORE.ROLE.COMMISSIONER":         "MCORE.ROLE.COMMISSIONEE",
		"SWTCH.C.M.SwitchStatePolling":    "SWTCH.S.A0001",
		"SWTCH.C.M.SwitchStateEventing":   "SWTCH.S.E01",
		"MCORE.IDM.C":                     "MCORE.IDM.S",
		"MCORE.IDM.C.SubscribeEvent":      "MCORE.IDM.S",
		"MCORE.IDM.C.ReadEvent":           "MCORE.IDM.S",
		"MCORE.IDM.C.InvokeRequest":       "MCORE.IDM.S",
		"MCORE.IDM.C.ReadRequest":         "MCORE.IDM.S",
		"MCORE.IDM.C.WriteRequest":        "MCORE.IDM.S",
		"MCORE.IDM.C.SubscribeRequest":    "MCORE.IDM.S",
		"MCORE.IDM.C.TimedRequest":        "MCORE.IDM.S",
		"MCORE.IDM.C.InvokeRequest.Batch": "MCORE.IDM.S",
	}
)

func mirrorPICS(expr string) string {
	return rePICSCode.ReplaceAllStringFunc(expr, mirrorPICSCode)
}

func mirrorPICSCode(code string) string {
	if m, ok := picsSpecial[code]; ok {
		return m
	}
	if _, ok := goTHPICS[code]; ok {
		return code
	}
	if strings.HasPrefix(code, "MCORE.IDM.C.") {
		return "MCORE.IDM.S"
	}
	m := rePICSClient.FindStringSubmatch(code)
	if m == nil {
		return code
	}
	rest := m[2]
	if parts := strings.SplitN(strings.TrimPrefix(rest, "."), ".", 2); len(parts) == 2 {
		if tx := rePICSTx.FindStringSubmatch(parts[0] + "." + parts[1]); tx != nil {
			rest = "." + tx[1] + ".Rsp"
		}
	}
	return m[1] + ".S" + rest
}

// --- the golden counts ---------------------------------------------------------

// checkGoCertCounts holds a passing case's counts against the golden file.
func checkGoCertCounts(t *testing.T, id string, got goCertCounts) {
	t.Helper()
	if *updateChipCases {
		return
	}
	g := loadGolden(t)
	want, ok := g.GoCert[id]
	if !ok {
		t.Fatalf("%s passed with %+v but has no goCert entry in testdata/%s; run with -update-chip-cases, read the diff, commit it",
			id, got, chipCasesGoldenFile)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s ran %+v, golden file says %+v: the case or the PICS moved; read the step lines and regenerate with "+
			"-update-chip-cases only once you know which steps changed and why", id, got, want)
	}
}

// writeGoCertGolden records the passing cases' counts under
// -update-chip-cases, and drops their manual rows: a case the leg runs is
// no longer one nobody automates.
func writeGoCertGolden(t *testing.T, ran map[string]goCertCounts) {
	t.Helper()
	if !*updateChipCases || len(ran) == 0 {
		return
	}
	g := loadGolden(t)
	goldenMu.Lock()
	defer goldenMu.Unlock()
	if g.GoCert == nil {
		g.GoCert = map[string]goCertCounts{}
	}
	for id, c := range ran {
		g.GoCert[id] = c
		delete(g.HostSkipped, id)
	}
	saveGolden(t, g)
}
