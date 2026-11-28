// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package testquality guards two habits that let a test pass without
// testing anything (review II, phase 5a, unit tassert, guards G1 and G2).
//
// # G1: a Test that cannot fail
//
// A top-level Test function cannot fail when nothing it runs can fail it:
// no Error, Errorf, Fatal, Fatalf, Fail or FailNow on a testing parameter, no
// panic call, no require. or assert. call, and every call it passes a
// testing parameter to resolves to a function of this repository that
// cannot fail either. A callee that does not resolve — another module, a
// function value, a method whose name several receivers share — is taken
// as one that may fail, and so is a function that stores the testing
// parameter (a field, a literal, an assignment, a return): the rule errs
// towards "may fail", so what it counts is certain. Not counted are a test
// that skips (G2 and the FixMe register judge those), a test whose name
// ends in DoesNotPanic (it says what it checks), and the tests of
// [cannotFailAllowed], which fail only under -race or at compile time.
// What is left is a test whose name claims something it never checks: a
// panic-only body, a body that only logs or prints, an empty generated
// round trip.
//
// # G2: a skip that hides a failure
//
// Every t.Skip, Skipf and SkipNow call in a _test.go file is classified.
// A skip that declares an unported case — the first statement of its
// function, after t.Parallel or t.Helper — is not judged here (the FixMe
// register and the parity register hold those). A skip whose reason — its
// arguments and the conditions around it, never its file or function name
// — names the environment (-short, the OS, a socket or multicast, docker,
// chip-tool, an environment variable, the race detector, a checkout of
// matter.js or connectedhomeip), or that stands in a live test file, is
// not judged either, but it is listed ([environmentSkips]). Of the rest, a skip lexically inside a range statement is G2a: a loop over
// loaded vectors or fixtures that skips an entry it does not know drops
// that entry unseen (R2-P5-tassert-01, -11). Any other is G2b: a skip on a
// fixture precondition, which turns a broken fixture into a pass
// (R2-P5-tassert-03, -10).
//
// # The numbers
//
// Each class is exact, by site, in both directions: [cannotFailLeft],
// [skipInRangeLeft], [preconditionSkipsLeft] and [environmentSkips] list
// what is left today with the reason it is left, and a row of
// [cannotFailAllowed] that excuses nothing fails. A new site fails, and a fixed one fails until
// its row goes in the same commit. The goal of each list is empty.
//
// The walk reads every .go file under the repository root, nested modules
// included, except dot directories, testdata, node_modules and vendor.
// TESTQUALITY_LIST=1 logs every counted site.
package testquality

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the repository root, two levels above this package.
const repoRoot = "../.."

// cannotFailLeft are the Test functions G1 still counts, as
// "<file>:<TestName>", with the reason each is left.
var cannotFailLeft = map[string]string{}

// cannotFailAllowed are Test functions that fail only under -race (make
// race runs them) or only at compile time, as "<file>:<TestName>". A row
// that excuses no Test G1 counts fails, as a stale row of the other lists
// does.
var cannotFailAllowed = map[string]string{
	"cluster/core/binding_test.go:TestBinding_ConcurrentReadWrite":                                                "race-only",
	"cluster/core/network_commissioning_test.go:TestNetcomm_Concurrent_Race":                                      "race-only",
	"controller/client/cache_internal_test.go:TestMemoryStoreConcurrent":                                          "race-only",
	"endpoint/dispatcher_acl_test.go:TestTopologyDispatcher_ImplementsACLChecker":                                 "compile-time interface assertion",
	"cluster/measurement/measurement_test.go:TestTemperatureServerDataVersion_ImplementsMatterClusterDataVersion": "compile-time interface assertion",
}

// skipInRangeLeft are the G2a sites left, as "<file>:<func>" with the
// number of skips in it.
var skipInRangeLeft = map[string]int{
	// The row's constraint is on the invoke path, where
	// TestNegativeInvokeParity holds it.
	"cluster/matter_negative_write_parity_test.go:TestNegativeWriteParity": 1,
	// noFixture names, per device type, why no fixture builds it; the
	// corpus test also skips the types script/devicegen's excluded list
	// names, each with its reason (devicegen's test fails on a type in
	// neither list).
	"device/conformance_test.go:TestEveryTypeConstructsAndConforms":                                         1,
	"device/standard_device_type_corpus_parity_matterjs_test.go:TestStandardDeviceTypeCorpusParityMatterJS": 2,
	// A parity declaration (skipCrash) in a loop over two variants.
	"endpoint/runtime_validation_parity_matterjs_test.go:testNodeScopePasses": 1,
	// No connectedhomeip checkout: the environment, with its reason in a
	// variable the scan does not read.
	"internal/chipdm/levels_test.go:TestChipDataModelLevels": 1,
	// TestTestparityRegisterCoversSnapshot reports the mismatch.
	"script/testparity/register_test.go:TestTestparityPageIsCurrent": 1,
}

// preconditionSkipsLeft are the G2b sites left, as "<file>:<func>" with the
// number of skips in it.
var preconditionSkipsLeft = map[string]int{}

// environmentSkips are the G2 sites whose reason names the environment
// ([envSkip], [envVar], [livePath]), as "<file>:<func>" with the number
// of skips in it, grouped by the environment each skips on. They are not
// judged; they are listed so that a new one is read, and so that a skip
// on a fixture precondition does not pass as the environment by its
// wording (R2-P5 DR5a-ci-06).
var environmentSkips = map[string]int{
	// The -short flag, the race detector, a tool not on PATH: a slow or
	// tool-bound test outside its run.
	"conformance/load_test.go:TestLoadSubscriptionFanout":                    1,
	"endpoint/sqlitestore/allocation_test.go:TestFullTableIsExhausted":       1,
	"script/clustergen/compile_test.go:TestEveryClusterGeneratesAndCompiles": 2,
	"script/docscheck/notices_test.go:TestNoticesListEveryLinkedModule":      1,

	// The host's network: multicast, IPv6, the mDNS sockets, its interfaces
	// and its OS host name.
	"bridge/group_messaging_e2e_test.go:TestGroupMessageArrivesOverRealMulticast":                                                           4,
	"client/discovery/names_test.go:TestNames_Constructors":                                                                                 1,
	"client/discovery/scanner_live_test.go:TestScanner_LiveFindsAnAdvertisedNode":                                                           4,
	"client/peer/node_live_test.go:TestConnectLiveThroughMulticast":                                                                         3,
	"client/peer/retention_test.go:TestIssue507_ZoneSpelledTwoWays":                                                                         1,
	"cluster/core/general_diagnostics_network_interfaces_parity_matterjs_test.go:TestGeneralDiagnosticsNetworkInterfacesTypeAndOperational": 1,
	"controller/commissioner/node_live_test.go:TestCommissionLiveThroughMulticast":                                                          3,
	"controller/operational_port_parity_matterjs_test.go:TestOperationalPortParityMatterJS":                                                 1,
	"mdns/hostname_policy_test.go:TestHostNamePolicy":                                                                                       1,
	"mdns/operational_resolver_live_test.go:TestOperationalResolver_LiveMulticastRoundTrip":                                                 2,
	"mdns/operational_resolver_live_test.go:TestResolution_ZonesLinkLocalByInterface":                                                       1,
	"mdns/query_conn_test.go:TestOpenQueryConn":                                                                                             1,
	"mdns/responder_advertiser_parity_matterjs_test.go:TestMdnsAdvertisementOSHostNameVerdict":                                              1,
	"mdns/socket_live_test.go:TestJoinMcast4_Success":                                                                                       1,
	"mdns/socket_live_test.go:TestJoinMcast6_ProbeAndClose":                                                                                 1,
	"mdns/socket_live_test.go:TestOpenSocketsWithoutInterface":                                                                              2,
	"mdns/socket_live_test.go:TestResponder_LiveHostAnswerV4":                                                                               3,
	"mdns/socket_live_test.go:TestResponder_LiveHostAnswerV6":                                                                               4,
	"mdns/socket_live_test.go:TestResponder_LiveOperationalRoundTrip":                                                                       2,
	"mdns/socket_live_test.go:TestSockets_JoinNewInterfaces":                                                                                2,
	"mdns/socket_live_test.go:TestSockets_Lifecycle":                                                                                        1,
	"mdns/socket_live_test.go:TestSockets_ReceiveAfterJoinNew":                                                                              6,
	"mdns/socket_live_test.go:livePublish":                                                                                                  1,
	"mdns/socket_live_test.go:skipUnlessMulticastV4":                                                                                        2,
	"transport/udp/multicast_test.go:TestJoinAndLeaveGroupOnLiveInterfaces":                                                                 2,
	"transport/udp/multicast_test.go:TestJoinGroupRejectsNonMulticastAndClosedListeners":                                                    1,
	"transport/udp/multicast_test.go:TestJoinGroupWithoutInterfaces":                                                                        1,
	"transport/udp/multicast_test.go:TestSendMulticastReachesAJoinedListener":                                                               5,
	"transport/udp/multicast_test.go:TestSendMulticastRefusals":                                                                             1,

	// A checkout or tool that is not there: matter.js, connectedhomeip,
	// chip-tool, git, node, and MATTERJS_DIR for the ahead-of register.
	"conformance/chiptool_smoke_test.go:TestChipToolPairingOnNetworkLong":      1,
	"internal/chipdm/crosscheck_test.go:chipModel":                             1,
	"internal/chipdm/levels_test.go:TestChipDataModelLevels":                   1,
	"internal/chiptool/chipcoverage_test.go:TestChipPythonCasesCovered":        1,
	"internal/chiptool/controller_test.go:resolveChipTool":                     1,
	"internal/chiptool/matterjs_test.go:startMatterJSController":               2,
	"schema/staleness_test.go:TestMatterJSSchemaStalenessAgainstLiveCheckout":  2,
	"schema/timed_test.go:TestTimedInvokeParity":                               1,
	"script/aheadcheck/aheadcheck_test.go:TestAheadcheckRegisterHoldsAtThePin": 2,
	"script/aheadcheck/aheadcheck_test.go:newFixtureRepo":                      1,

	// The CHIP harness: a leg that runs only on request (docker, its image,
	// the radio), a case the harness classes itself (manual, excluded,
	// self-skipped, failing as recorded, PICS false, IPv6 multicast
	// missing), and the Go-TH and
	// Go-controller legs' multicast.
	"internal/chiptool/blecases_test.go:TestChipToolBLECases":                           3,
	"internal/chiptool/blewifi_test.go:TestChipToolBLEWiFi":                             2,
	"internal/chiptool/familyrun_test.go:TestChipCertificationFamilies":                 1,
	"internal/chiptool/familyrun_test.go:requireIPv6":                                   1,
	"internal/chiptool/familyrun_test.go:runFamily":                                     3,
	"internal/chiptool/familyrun_test.go:runPythonCase":                                 2,
	"internal/chiptool/familyrun_test.go:skipNotApplicable":                             1,
	"internal/chiptool/gocert_test.go:TestGoCertificationCases":                         1,
	"internal/chiptool/gocert_test.go:runGoCertCase":                                    1,
	"internal/chiptool/gocontroller_test.go:TestGoControllerCommissionsReferenceBridge": 2,
	"internal/chiptool/gocontroller_test.go:TestGoControllerSubscribesReferenceBridge":  3,
	"internal/chiptool/groups_proof_test.go:testGroupMessaging":                         1,

	// The effective user and the file system: root, symlinks.
	"examples/reference-bridge/fifo_unix_test.go:TestAppPipeExistingIsMadePrivate": 1,
	"script/devicegen/coverage_test.go:TestNewGeneratorRefusesAnIncompleteRoot":    1,

	// A live device an environment variable names: a border router, a
	// wpa_supplicant.
	"thread/otbr/live_test.go:TestLiveOTBR":                       1,
	"wifi/wpasupplicant/live_linux_test.go:TestLiveWPASupplicant": 2,

	// The operating system: a test whose subject is the file of the
	// other GOOS, on a system that has none (P10-test-6903).
	"script/mutate/notbuilt_p10_test.go:TestMutateOtherGOOSFileNotBuiltP10": 1,
}

// minTests is the floor on Test functions seen: fewer means the walk broke.
const minTests = 5000

// envSkip matches a skip reason that names the environment: the -short
// flag, the host's network (multicast, sockets, its interfaces, its
// name), a tool or checkout that is not there (docker, chip-tool, git,
// node, a matter.js or connectedhomeip checkout), the race detector, the
// effective user, symlinks, and the case classes of the CHIP harness. The
// reason is the skip's arguments and the conditions of the if statements
// around it — what the skip tests — never its file or function name.
// envVar matches an environment variable: a lookup, a variable this
// repository names (GOFABRIC_…, MATTERJS_…, CHIP…), or a Go name for one
// (envX, xEnv); the two are kept apart because only envSkip ignores case.
// livePath matches the path of a live test file (*_live_test.go,
// live_*_test.go), whose skips are the host's by construction; it reads
// the path only (R2-P5 DR5a-ci-06).
//
// Every site the three match is in [environmentSkips]: the class is exact,
// like the other two, so a skip that only looks like the environment is
// read once by a person.
var (
	envSkip = regexp.MustCompile(`(?i)testing short|-short|skipped in|skipped under|load test|goos|advertiser|` +
		`multicast|ipv6|ipv4|socket|loopback|udp 5353|mdns|dns-sd|network interface|no interface|os host name|cannot run on this host|` +
		`docker|chip-?tool|chipToolErr|chipOnce|connectedhomeip|harness|matter\.js checkout|checkout not present|behind the checkout|built matter\.js|` +
		`on path|not installed|git |node is not|race detector|geteuid|symlink|` +
		`not applicable|selfSkip|failsAs|classHarness|classNotSupported|manualClass|goCertEnv|border router`)
	envVar   = regexp.MustCompile(`\bGetenv\b|\bLookupEnv\b|\b(GOFABRIC|MATTERJS|CHIP[A-Z]*)_[A-Z0-9_]+\b|\benv[A-Z]\w*|[a-z]Env\b`)
	livePath = regexp.MustCompile(`_live|live_`)
)

type fn struct {
	file    string
	name    string
	decl    *ast.FuncDecl
	dir     string
	imports map[string]string
	tnames  map[string]bool
	direct  bool
	unres   bool
	stores  bool
	skips   bool
	callees []string
}

var failMethods = map[string]bool{"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true, "Fail": true, "FailNow": true}

var skipMethods = map[string]bool{"Skip": true, "Skipf": true, "SkipNow": true}

func isTestingType(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.StarExpr:
		if sel, ok := x.X.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "testing" {
				return sel.Sel.Name == "T" || sel.Sel.Name == "B" || sel.Sel.Name == "F"
			}
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Name == "testing" && x.Sel.Name == "TB" {
			return true
		}
	}
	return false
}

func testingNames(ft *ast.FuncType, into map[string]bool) bool {
	found := false
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		if isTestingType(p.Type) {
			found = true
			for _, n := range p.Names {
				into[n.Name] = true
			}
		}
	}
	return found
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor"
}

// modules maps every module path under root to its directory.
func modules(t *testing.T, root string) map[string]string {
	t.Helper()
	mods := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if line := s.Text(); strings.HasPrefix(line, "module ") {
				mods[strings.TrimSpace(strings.TrimPrefix(line, "module "))] = filepath.Dir(p)
			}
		}
		return s.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return mods
}

// site is one counted skip.
type site struct {
	key  string // <file>:<func>
	line int
	text string
}

type scan struct {
	fns          map[string]*fn
	order        []string
	inRange      []site
	precondition []site
	environment  []site
	tests        int
}

func (s *scan) walk(t *testing.T, root string) {
	t.Helper()
	fset := token.NewFileSet()
	s.fns = map[string]*fn{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		imps := map[string]string{}
		for _, im := range f.Imports {
			path := strings.Trim(im.Path.Value, `"`)
			alias := filepath.Base(path)
			if im.Name != nil {
				alias = im.Name.Name
			}
			imps[alias] = path
		}
		dir := filepath.Dir(p)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if strings.HasSuffix(p, "_test.go") {
				s.classifySkips(fset, rel, fd)
			}
			tn := map[string]bool{}
			if !testingNames(fd.Type, tn) {
				continue
			}
			key := dir + "|" + fd.Name.Name
			if fd.Recv != nil {
				key = dir + "|M." + fd.Name.Name
			}
			if _, dup := s.fns[key]; dup {
				key += "#" + strconv.Itoa(fset.Position(fd.Pos()).Line)
			}
			s.fns[key] = &fn{file: rel, name: fd.Name.Name, decl: fd, dir: dir, imports: imps, tnames: tn}
			s.order = append(s.order, key)
			if fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") && strings.HasSuffix(p, "_test.go") {
				s.tests++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// classifySkips records the G2 sites of one function declaration.
func (s *scan) classifySkips(fset *token.FileSet, rel string, fd *ast.FuncDecl) {
	key := rel + ":" + fd.Name.Name
	var visit func(n ast.Node, stack []ast.Node)
	visit = func(n ast.Node, stack []ast.Node) {
		ast.Inspect(n, func(c ast.Node) bool {
			if c == nil {
				return false
			}
			if c != n {
				switch c.(type) {
				case *ast.FuncLit, *ast.RangeStmt, *ast.IfStmt:
					visit(c, append(append([]ast.Node(nil), stack...), c))
					return false
				}
			}
			call, ok := c.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !skipMethods[sel.Sel.Name] {
				return true
			}
			if _, ok := sel.X.(*ast.Ident); !ok {
				return true
			}
			s.classifySkip(fset, rel, key, call, stack, fd)
			return true
		})
	}
	visit(fd.Body, nil)
}

func (s *scan) classifySkip(fset *token.FileSet, rel, key string, call *ast.CallExpr, stack []ast.Node, fd *ast.FuncDecl) {
	if declaresCase(call, stack, fd) {
		return
	}
	text := exprText(call)
	inRange := false
	for _, n := range stack {
		switch x := n.(type) {
		case *ast.IfStmt:
			text += " " + exprText(x.Cond)
		case *ast.RangeStmt:
			inRange = true
		}
	}
	st := site{key: key, line: fset.Position(call.Pos()).Line, text: text}
	if livePath.MatchString(rel) || envSkip.MatchString(text) || envVar.MatchString(text) {
		s.environment = append(s.environment, st)
		return
	}
	if inRange {
		s.inRange = append(s.inRange, st)
		return
	}
	s.precondition = append(s.precondition, st)
}

// declaresCase reports whether the skip is the first statement of its
// function — after t.Parallel or t.Helper — and so declares a case that is
// not ported rather than skipping one that runs.
func declaresCase(call *ast.CallExpr, stack []ast.Node, fd *ast.FuncDecl) bool {
	body := fd.Body
	for i := len(stack) - 1; i >= 0; i-- {
		if fl, ok := stack[i].(*ast.FuncLit); ok {
			body = fl.Body
			break
		}
		if _, ok := stack[i].(*ast.IfStmt); ok {
			return false
		}
		if _, ok := stack[i].(*ast.RangeStmt); ok {
			return false
		}
	}
	for _, st := range body.List {
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			return false
		}
		c, ok := es.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if c == call {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Parallel" && sel.Sel.Name != "Helper") {
			return false
		}
	}
	return false
}

// exprText is the words of n: its literals and identifiers, so a skip
// reason held in a named constant is read by the constant's name.
func exprText(n ast.Node) string {
	var b strings.Builder
	ast.Inspect(n, func(c ast.Node) bool {
		switch x := c.(type) {
		case *ast.BasicLit:
			b.WriteString(x.Value + " ")
		case *ast.Ident:
			b.WriteString(x.Name + " ")
		}
		return true
	})
	return b.String()
}

// cannotFail returns the Test functions G1 counts, as "<file>:<TestName>",
// and the rows of [cannotFailAllowed] that excused one.
func (s *scan) cannotFail(mods map[string]string) (out, allowed map[string]bool) {
	importDir := func(path string) string {
		best := ""
		for m := range mods {
			if (path == m || strings.HasPrefix(path, m+"/")) && len(m) > len(best) {
				best = m
			}
		}
		if best == "" {
			return ""
		}
		return filepath.Join(mods[best], strings.TrimPrefix(path, best))
	}
	ambiguous := map[string]bool{}
	for k := range s.fns {
		if i := strings.Index(k, "#"); i >= 0 {
			ambiguous[k[:i]] = true
		}
	}
	lookup := func(f *fn, call *ast.CallExpr) (string, bool) {
		switch c := call.Fun.(type) {
		case *ast.Ident:
			k := f.dir + "|" + c.Name
			_, ok := s.fns[k]
			return k, ok && !ambiguous[k]
		case *ast.SelectorExpr:
			if id, ok := c.X.(*ast.Ident); ok {
				if ip, ok := f.imports[id.Name]; ok {
					d := importDir(ip)
					if d == "" {
						return "", false
					}
					k := d + "|" + c.Sel.Name
					_, ok := s.fns[k]
					return k, ok && !ambiguous[k]
				}
			}
			k := f.dir + "|M." + c.Sel.Name
			_, ok := s.fns[k]
			return k, ok && !ambiguous[k]
		}
		return "", false
	}
	for _, k := range s.order {
		f := s.fns[k]
		names := map[string]bool{}
		for n := range f.tnames {
			names[n] = true
		}
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok {
				testingNames(fl.Type, names)
			}
			return true
		})
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "panic" {
					f.direct = true
				}
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						if id.Name == "require" || id.Name == "assert" {
							f.direct = true
						}
						if names[id.Name] && failMethods[sel.Sel.Name] {
							f.direct = true
						}
						if names[id.Name] && skipMethods[sel.Sel.Name] {
							f.skips = true
						}
						if names[id.Name] && sel.Sel.Name == "Run" && len(x.Args) == 2 {
							if _, lit := x.Args[1].(*ast.FuncLit); !lit {
								f.unres = true
							}
						}
					}
				}
				passes := false
				for _, a := range x.Args {
					if id, ok := a.(*ast.Ident); ok && names[id.Name] {
						passes = true
					}
				}
				if !passes {
					break
				}
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && names[id.Name] {
						break
					}
				}
				if ck, ok := lookup(f, x); ok {
					f.callees = append(f.callees, ck)
				} else {
					f.unres = true
				}
			case *ast.CompositeLit:
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						e = kv.Value
					}
					if id, ok := e.(*ast.Ident); ok && names[id.Name] {
						f.stores = true
					}
				}
			case *ast.AssignStmt:
				for _, e := range x.Rhs {
					if id, ok := e.(*ast.Ident); ok && names[id.Name] {
						f.stores = true
					}
				}
			case *ast.ReturnStmt:
				for _, e := range x.Results {
					if id, ok := e.(*ast.Ident); ok && names[id.Name] {
						f.stores = true
					}
				}
			}
			return true
		})
	}
	can := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, k := range s.order {
			if can[k] {
				continue
			}
			f := s.fns[k]
			c := f.direct || f.unres || f.stores
			for _, ck := range f.callees {
				c = c || can[ck]
			}
			if c {
				can[k] = true
				changed = true
			}
		}
	}
	out, allowed = map[string]bool{}, map[string]bool{}
	for _, k := range s.order {
		f := s.fns[k]
		if can[k] || f.decl.Recv != nil || !strings.HasPrefix(f.name, "Test") || !strings.HasSuffix(f.file, "_test.go") {
			continue
		}
		if f.skips || strings.HasSuffix(f.name, "DoesNotPanic") {
			continue
		}
		ref := f.file + ":" + f.name
		if _, ok := cannotFailAllowed[ref]; ok {
			allowed[ref] = true
			continue
		}
		out[ref] = true
	}
	return out, allowed
}

func countBy(sites []site) map[string]int {
	out := map[string]int{}
	for _, s := range sites {
		out[s.key]++
	}
	return out
}

func compare(t *testing.T, class string, got, want map[string]int) {
	t.Helper()
	keys := map[string]bool{}
	for k := range got {
		keys[k] = true
	}
	for k := range want {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		if got[k] != want[k] {
			t.Errorf("%s: %s has %d, the list says %d — fix the new site, or lower the list in the same commit as the fix", class, k, got[k], want[k])
		}
	}
}

// TestTestsCanFail holds G1 and G2 at their lists.
func TestTestsCanFail(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	var s scan
	s.walk(t, root)
	if s.tests < minTests {
		t.Fatalf("%d Test functions seen, want at least %d: the walk broke", s.tests, minTests)
	}
	list := os.Getenv("TESTQUALITY_LIST") != ""

	g1, allowed := s.cannotFail(modules(t, root))
	got := map[string]int{}
	for ref := range g1 {
		got[ref] = 1
	}
	want := map[string]int{}
	for ref := range cannotFailLeft {
		want[ref] = 1
	}
	compare(t, "G1 cannot fail", got, want)
	// A row that excused nothing is stale: its test was renamed or removed,
	// or it can fail now on its own (R2-P5 DR5a-ci-15).
	for _, ref := range sortedKeys(boolKeys(cannotFailAllowed)) {
		if !strings.Contains(ref, ":Test") {
			t.Errorf("cannotFailAllowed %q is not <file>:<TestName>", ref)
		} else if !allowed[ref] {
			t.Errorf("cannotFailAllowed %q excuses no Test G1 counts: renamed, removed or able to fail now — remove the row", ref)
		}
	}

	compare(t, "G2a skip inside a range", countBy(s.inRange), skipInRangeLeft)
	compare(t, "G2b skip on a fixture precondition", countBy(s.precondition), preconditionSkipsLeft)
	compare(t, "G2 skip on the environment", countBy(s.environment), environmentSkips)

	if list {
		for _, ref := range sortedKeys(g1) {
			t.Logf("G1 %s", ref)
		}
		for _, st := range s.inRange {
			t.Logf("G2a %s (line %d): %s", st.key, st.line, st.text)
		}
		for _, st := range s.precondition {
			t.Logf("G2b %s (line %d): %s", st.key, st.line, st.text)
		}
		for _, st := range s.environment {
			t.Logf("G2env %s (line %d): %s", st.key, st.line, st.text)
		}
	}
}

func boolKeys(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
