// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package quiescecheck guards that a cluster server type which runs
// timers can stop them (review II, R2-P1-clu-02, guard G5; per receiver
// type since R2-P6-test-1508).
//
// A server that leaves the topology — a reassembly that drops its
// endpoint, the bridge's Stop — is told so through
// contract.ClusterQuiescer.MatterQuiesce, which ends its timers so none
// outlives the bridge's use of the server (contract/matter.go). A type
// under cluster/ that runs a timer and declares no MatterQuiesce has no
// way to be told; its timers run on after the server is gone.
//
// The rule is per named type and syntactic. A type runs a timer when
//
//   - one of its methods (a function literal inside one included) calls a
//     method named AfterFunc, NewTimer or NewTicker, or time.After,
//     time.Tick, time.Sleep, context.WithTimeout or context.WithDeadline;
//   - or it embeds a type under cluster/ that runs a timer, or holds as
//     a field a value of one that runs a timer and has no MatterQuiesce:
//     a helper such as cluster.Quieter or transition.Engine makes its
//     holder a timer user, in whichever package the helper lives. A held
//     type with a MatterQuiesce of its own (a back reference to a server,
//     a sub-server) is not followed: whether the holder forwards to it is
//     not a syntactic question.
//
// A type has MatterQuiesce when it declares one or embeds a type that has
// one. A function without a receiver that runs a timer is held to the
// package: some type of the package must have MatterQuiesce. The check
// sees a timer and a MatterQuiesce, not whether the MatterQuiesce stops
// that timer, nor whether a timer outlives Stop at all — a timer every
// path stops by itself needs no quiesce. Such a type is an entry of
// [exceptions] with its reason. The list is exact: a new type with timers
// and no MatterQuiesce fails, and so does an entry that no longer needs
// one.
package quiescecheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the module root, two levels above this package.
const repoRoot = "../.."

// modulePath is the import path of the module root.
const modulePath = "github.com/SukramJ/go-fabric/"

// exceptions are the types under cluster/ that run timers and have no
// MatterQuiesce, keyed "dir.Type" ("dir.(func)" for a package's functions
// without a receiver), with the reason each may. The helper types arm the
// timer for a holder, which the rule holds instead; the others are what
// the per-type rule found on 2026-11-16 (R2-P6-test-1508) in the units
// that own them, each a finding to fix or to record there — the reason
// says what the timer is, not that it is safe.
var exceptions = map[string]string{
	// Helpers: each arms the timer of the type that holds it, which the
	// rule holds instead.
	"cluster.Quieter":                 "helper: the throttle of a quieter attribute; its holder quiesces it (Quieter.Stop)",
	"cluster/transition.Engine":       "helper: the transition step timer; its holder quiesces it",
	"cluster/spec.quietAttr":          "helper: holds a quieter attribute's Quieter; spec.Server.MatterQuiesce stops each (quiet.go)",
	"cluster/thermo.atomicWriteState": "helper: an open atomic write's timeout timer; ThermostatServer.MatterQuiesce closes every one (s.atomic.closeAll)",
	"cluster/core.otaIdleTimer":       "helper: the OTA requestor's BDX idle timer, held by otaHTTPSWriter",
	"cluster/core.otaHTTPSWriter":     "holds otaIdleTimer: the OTA download's idle timeout",
	// Found by the per-type rule (R2-P6-test-1508) and not read here for
	// whether the timer outlives the server: each is its owner unit's.
	"cluster/camera.PushTransport":            "pushtransport.go: the per-connection expiry timers (N-clumisc-4; unit appinfra)",
	"cluster/core.GeneralCommissioning":       "general_commissioning.go: the fail-safe watch timer (unit coresys)",
	"cluster/core.Identify":                   "identify.go: the IdentifyTime countdown ticker (unit coresys)",
	"cluster/core.OTASoftwareUpdateRequestor": "ota_requestor_query.go: the query timer and the update delay (unit coresys)",
	"cluster/core.WiFiNetworkCommissioning":   "network_commissioning_wifi.go: the rejoin timer, the disconnect and the connect / scan deadlines (unit coresec)",
	"cluster/levelcontrol.NativeSource":       "native.go: the ramp deadline of the host's native transition (unit applight)",
	"cluster/levelcontrol.nativeWire":         "holds NativeSource (unit applight)",
}

// timerMethods are the method names that arm a timer, on any receiver
// (time, a clock.Clock).
var timerMethods = map[string]bool{"AfterFunc": true, "NewTimer": true, "NewTicker": true}

// timerFuncs are the package functions that wait on the wall clock, by
// import path.
var timerFuncs = map[string]map[string]bool{
	"time":    {"After": true, "Tick": true, "Sleep": true},
	"context": {"WithTimeout": true, "WithDeadline": true},
}

// typeFacts is what the walk finds about one named type.
type typeFacts struct {
	timers  []string // file:line of each timer the type's methods arm
	quiesce bool     // declares MatterQuiesce
	holds   []string // "dir.Type" keys of its field types under the walked tree
	embeds  []string // "dir.Type" keys of its embedded types under the walked tree
}

// analyze walks the Go files under root (test files and testdata
// excepted) and returns the facts per "dir.Type", dir relative to base;
// an import path with the prefix modPrefix names the directory after it.
func analyze(base, root, modPrefix string) (map[string]*typeFacts, error) {
	types := map[string]*typeFacts{}
	get := func(key string) *typeFacts {
		f := types[key]
		if f == nil {
			f = &typeFacts{}
			types[key] = f
		}
		return f
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, filepath.Dir(p))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		imports := map[string]string{} // local name → import path
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			name := path[strings.LastIndex(path, "/")+1:]
			if im.Name != nil {
				name = im.Name.Name
			}
			imports[name] = path
		}
		// typeKey resolves a field or receiver type to its "dir.Type"; ""
		// for a type outside the walked tree.
		var typeKey func(e ast.Expr) string
		typeKey = func(e ast.Expr) string {
			switch x := e.(type) {
			case *ast.StarExpr:
				return typeKey(x.X)
			case *ast.IndexExpr:
				return typeKey(x.X)
			case *ast.IndexListExpr:
				return typeKey(x.X)
			case *ast.Ident:
				return dir + "." + x.Name
			case *ast.SelectorExpr:
				pkg, ok := x.X.(*ast.Ident)
				if !ok {
					return ""
				}
				path, ok := imports[pkg.Name]
				if !ok || !strings.HasPrefix(path, modPrefix) {
					return ""
				}
				return strings.TrimPrefix(path, modPrefix) + "." + x.Sel.Name
			}
			return ""
		}
		timersIn := func(n ast.Node) []string {
			var out []string
			ast.Inspect(n, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				hit := timerMethods[sel.Sel.Name]
				if pkg, ok := sel.X.(*ast.Ident); ok {
					switch path := imports[pkg.Name]; {
					case timerFuncs[path][sel.Sel.Name]:
						hit = true
					case path == "context":
						hit = false // context.AfterFunc runs on cancellation, not on a timer
					}
				}
				if hit {
					pos := fset.Position(call.Pos())
					out = append(out, filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line))
				}
				return true
			})
			return out
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				key := dir + ".(func)"
				if d.Recv != nil && len(d.Recv.List) == 1 {
					key = typeKey(d.Recv.List[0].Type)
				}
				facts := get(key)
				if d.Recv != nil && d.Name.Name == "MatterQuiesce" {
					facts.quiesce = true
				}
				// A clock adapter's AfterFunc that hands the call on is a
				// clock, not a timer user.
				if d.Body != nil && !(d.Recv != nil && timerMethods[d.Name.Name]) {
					facts.timers = append(facts.timers, timersIn(d.Body)...)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					facts := get(dir + "." + ts.Name.Name)
					for _, fld := range st.Fields.List {
						k := typeKey(fld.Type)
						if k == "" {
							continue
						}
						if len(fld.Names) == 0 {
							facts.embeds = append(facts.embeds, k)
						} else {
							facts.holds = append(facts.holds, k)
						}
					}
				}
			}
		}
		return nil
	})
	return types, err
}

// violations holds types to the rule of the package documentation,
// against the given exceptions.
func violations(types map[string]*typeFacts, exceptions map[string]string) []string {
	var runs, quiesces func(key string, seen map[string]bool) bool
	runs = func(key string, seen map[string]bool) bool {
		f := types[key]
		if f == nil || seen[key] {
			return false
		}
		seen[key] = true
		if len(f.timers) > 0 {
			return true
		}
		for _, k := range f.embeds {
			if runs(k, seen) {
				return true
			}
		}
		// A held value runs the holder's timer when it cannot be told
		// itself: a helper that runs one and has no MatterQuiesce. A held
		// server with its own MatterQuiesce — a back reference, or a
		// server the bridge quiesces itself — is not the holder's.
		for _, k := range f.holds {
			if runs(k, seen) && !quiesces(k, map[string]bool{}) {
				return true
			}
		}
		return false
	}
	quiesces = func(key string, seen map[string]bool) bool {
		f := types[key]
		if f == nil || seen[key] {
			return false
		}
		seen[key] = true
		if f.quiesce {
			return true
		}
		for _, k := range f.embeds {
			if quiesces(k, seen) {
				return true
			}
		}
		return false
	}
	pkgQuiesces := map[string]bool{}
	for key := range types {
		if quiesces(key, map[string]bool{}) {
			pkgQuiesces[key[:strings.LastIndex(key, ".")]] = true
		}
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range types {
			if !yield(k) {
				return
			}
		}
	})
	var out []string
	for _, key := range keys {
		f := types[key]
		var needs bool
		var via []string
		for _, k := range f.embeds {
			if runs(k, map[string]bool{}) {
				via = append(via, k)
			}
		}
		for _, k := range f.holds {
			if runs(k, map[string]bool{}) && !quiesces(k, map[string]bool{}) {
				via = append(via, k)
			}
		}
		if strings.HasSuffix(key, ".(func)") {
			needs = len(f.timers) > 0 && !pkgQuiesces[strings.TrimSuffix(key, ".(func)")]
		} else {
			needs = runs(key, map[string]bool{}) && !quiesces(key, map[string]bool{})
		}
		_, excepted := exceptions[key]
		switch {
		case needs && !excepted:
			what := strings.Join(f.timers, ", ")
			if what == "" {
				what = "through " + strings.Join(via, ", ")
			}
			out = append(out, fmt.Sprintf("%s runs timers (%s) and has no MatterQuiesce: implement contract.ClusterQuiescer so the timers end with the server, or add an exception with the reason none outlives it", key, what))
		case !needs && excepted:
			out = append(out, fmt.Sprintf("%s is an exception but no longer needs one: delete the entry", key))
		}
	}
	for key := range exceptions {
		if _, ok := types[key]; !ok {
			out = append(out, fmt.Sprintf("exception %s names no type under the walked tree: delete the entry", key))
		}
	}
	slices.Sort(out)
	return out
}

// TestTimerTypesQuiesce holds the rule of the package documentation over
// cluster/.
func TestTimerTypesQuiesce(t *testing.T) {
	t.Parallel()
	types, err := analyze(repoRoot, filepath.Join(repoRoot, "cluster"), modulePath)
	if err != nil {
		t.Fatal(err)
	}
	quiescers := 0
	for _, f := range types {
		if f.quiesce {
			quiescers++
		}
	}
	if len(types) < 100 || quiescers == 0 {
		t.Fatalf("%d types, %d with MatterQuiesce under cluster/: the walk broke", len(types), quiescers)
	}
	for _, v := range violations(types, exceptions) {
		t.Error(v)
	}
}

// TestTheGuardSeesEachGap holds the guard to the gaps the per-package
// rule had (R2-P6-test-1508), on the fixture packages under testdata/: a
// second type of a package whose other type quiesces, a type holding a
// helper of another package that arms the timer, a goroutine waiting on
// time.After; and it does not flag a type whose MatterQuiesce is promoted
// from an embedded one.
func TestTheGuardSeesEachGap(t *testing.T) {
	t.Parallel()
	types, err := analyze("testdata", filepath.Join("testdata", "gaps"), "fixture/")
	if err != nil {
		t.Fatal(err)
	}
	got := violations(types, map[string]string{"gaps/helper.Throttle": "helper"})
	want := []string{"gaps/holder.Server", "gaps/loop.Server", "gaps/twotypes.Second"}
	var flagged []string
	for _, v := range got {
		flagged = append(flagged, v[:strings.Index(v, " ")])
	}
	if !slices.Equal(flagged, want) {
		t.Errorf("flagged %v, want %v:\n%s", flagged, want, strings.Join(got, "\n"))
	}
}
