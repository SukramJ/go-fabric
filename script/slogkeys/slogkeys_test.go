// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package slogkeys guards the attribute-key convention of ADR 0026
// principle 9 (docs/guides/observability.md): a log attribute built with
// slog.<Type>("<key>", …) uses a key of the table below, `err` is always
// slog.Any (the error value, not its text), and one call states a key
// once. A host that greps or indexes the module's logs reads the same key
// for the same thing in every package.
//
// The test parses every non-test Go file of the module with go/parser (no
// type checking) outside examples/, script/, notes/ and .claude/, as
// script/errprefix does; package main is exempt (a program's lines are
// its own). There is no ratchet: the tree is clean, and a new key is a
// row added to [coreKeys] or [areaKeys] in the same commit — the reader
// of the guide sees it.
//
// The guide's key section is generated from the table: `go test
// ./script/slogkeys -run TestGuideTable -update` rewrites it, and the test
// fails while the committed page differs.
package slogkeys

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the key section of docs/guides/observability.md")

// key is one row of the convention: the key, the value it carries and the
// constructor that carries it.
type key struct {
	name, value, meaning string
}

// coreKeys are the keys every package shares: the same thing under the
// same key, whatever logs it.
var coreKeys = []key{
	{"fabric_index", "slog.Int", "the fabric's index on this node (1–254); not the fabric id, which is `fabric_id`"},
	{"node_id", "slog.Uint64", "an operational node id: the peer's, a subject's, a binding target's"},
	{"endpoint", "slog.Int (slog.Any for a path's optional endpoint)", "an endpoint number"},
	{"cluster", "slog.Uint64 (slog.Any for a path's optional cluster; slog.String for a name)", "a cluster id"},
	{"attribute", "slog.Uint64 (slog.Any for a path's optional attribute; slog.String for a name)", "an attribute id"},
	{"session_id", "slog.Int", "a secure session's local id"},
	{"exchange_id", "slog.Int", "an exchange id"},
	{"subscription_id", "slog.Uint64 (slog.Int where the id is held as an int)", "a subscription id"},
	{"group_id", "slog.Int", "a group id"},
	{"peer", "slog.String", "the network address of the node at the other end — of a session, an exchange, a handshake, a commissioning attempt"},
	{"src", "slog.String", "the source address of one received datagram — what the socket saw, which a peer's later datagrams may not share"},
	{"err", "slog.Any", "the error value; never its text (`slog.String(\"err\", err.Error())` is refused)"},
	{"device", "slog.String", "the host's device key — the reference daemon's, in examples/ only; the module itself has no device key"},
	{"reason", "slog.String", "why the event happened: a refusal's cause, a close's trigger"},
	{"status", "slog.String (a status name), slog.Int (a code); slog.Bool for whether a report entry is a status", "a Matter status (Interaction Model, secure channel)"},
}

// areaKeys are the keys one area uses for what only it logs. They are
// accepted as they stand; a second area that logs the same thing uses
// the same key, and a thing the core table names never takes one of
// these.
var areaKeys = []string{
	"accept_test", "accepted_findings", "ack_counter", "action",
	"active_preset_handle", "address", "address_index", "addresses",
	"adjusted", "admin_vendor_id", "advertised_ms", "after", "age", "akid",
	"also_named_by", "attempt", "attribute_reports", "attribute_set",
	"attribute_statuses", "attributes", "authorities", "auxiliary_acl",
	"backoff", "boot_reason", "bridged_endpoints", "budget", "by", "bytes",
	"candidate", "case_admin_subject", "category", "ceiling", "changed",
	"channel", "check", "chunk", "chunk_count", "chunks", "cleared",
	"client_node_id", "closed", "cluster_set", "code", "color_mode",
	"command", "command_name", "concurrent", "configured_ms", "conn",
	"consecutive_errors", "context", "count", "counter", "country",
	"ctx_fabric_index", "current", "current_fabric_index", "datagram_bytes",
	"debug", "detail", "device_type", "discriminator", "dropped", "duplicate",
	"endpoint_set", "endpoints", "entries", "event", "event_paths",
	"event_reports", "events", "examined", "expected", "fabric_id",
	"fallback", "family", "features", "field", "file", "final", "floor",
	"force", "former", "formerly", "fragment_size", "from", "general_code",
	"github", "grace", "hint", "holds", "hook", "host", "icac_bytes", "ideal",
	"ids", "iface", "ifindex", "ignored", "in", "incoming", "initial_events",
	"initial_reports", "instance", "interval", "invoke_fabric_index",
	"invokes", "ipk_len", "ipv4", "ipv6", "iterations", "key", "key_set",
	"keys", "kind", "label", "left", "listen", "location", "lockout", "max",
	"max_attempts", "max_bytes", "max_errors", "max_interval",
	"measurement_seen", "message", "message_bytes", "min", "min_interval",
	"missing", "more", "name", "new", "next_query", "noc_bytes",
	"noc_subject_fabric_id", "noc_subject_node_id", "node", "notifier_ok",
	"of", "offered", "offline", "op", "opcode", "origin", "original_bytes",
	"outgoing", "panic", "parent", "part", "path", "path_index", "paths",
	"payload_bytes", "policy", "port", "preset_handle", "previous",
	"product_id", "production", "proto", "protocol_code", "protocol_id",
	"provider", "qname", "qtype", "reachable", "rearm_for", "records",
	"recovered_total", "reestablished", "registered", "reports", "req_paths",
	"request_id", "requested", "requirement", "resp", "response_type",
	"responses", "result", "resumed", "retransmission", "retry", "retry_in",
	"retry_limit", "role", "running", "scope", "send_failures",
	"send_interval", "service", "sessions", "since_last_restart", "skid",
	"source", "source_endpoint", "source_node_id", "ssid", "stack", "stage",
	"state", "status_code", "status_name", "statuses", "step", "stored",
	"stored_fabrics", "subject", "subscriptions_reaped", "subsystem",
	"supported", "suppress_response", "sw_version", "system_mode", "target",
	"test", "timed", "timeout", "timeout_ms", "to", "token", "token_version",
	"total", "total_endpoints", "trimmed_bytes", "type", "unicast",
	"uri_node", "url", "valid", "valid_mapping", "value", "value_type",
	"vendor_id", "version", "violation", "what", "with_paths",
}

// skippedTop are the top-level directories the guard does not judge.
var skippedTop = map[string]bool{
	"examples": true,
	"script":   true,
	"notes":    true,
	".claude":  true,
	".git":     true,
	"docs":     true,
}

// attrConstructors are the slog functions whose first argument is a key.
var attrConstructors = map[string]bool{
	"String": true, "Int": true, "Int64": true, "Uint64": true, "Float64": true,
	"Bool": true, "Time": true, "Duration": true, "Any": true, "Group": true,
}

// minChecked is the floor on judged attributes: fewer means the walk or
// the call matching broke and the guard is silently checking nothing.
const minChecked = 1000

type finding struct{ pos, text string }

func allowed() map[string]bool {
	out := map[string]bool{}
	for _, k := range coreKeys {
		out[k.name] = true
	}
	for _, k := range areaKeys {
		out[k] = true
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestAttributeKeys walks the module and reports every key outside the
// table, every `err` that is not slog.Any and every key stated twice in
// one call.
func TestAttributeKeys(t *testing.T) {
	root := moduleRoot(t)
	ok := allowed()
	var checked int
	var findings []finding
	unknown := map[string]int{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (skippedTop[strings.SplitN(rel, string(filepath.Separator), 2)[0]] || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n, fs, perr := checkFile(path, filepath.ToSlash(rel), ok, unknown)
		if perr != nil {
			return perr
		}
		checked += n
		findings = append(findings, fs...)
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if checked < minChecked {
		t.Fatalf("only %d attributes checked, want at least %d: the walk or the call matching is broken", checked, minChecked)
	}
	for _, f := range findings {
		t.Errorf("%s: %s", f.pos, f.text)
	}
	if len(unknown) > 0 && testing.Verbose() {
		keys := make([]string, 0, len(unknown))
		for k := range unknown {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		t.Logf("keys outside the table: %q", keys)
	}
	// A row nothing uses any more is dropped from the table.
	used := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (skippedTop[strings.SplitN(rel, string(filepath.Separator), 2)[0]] || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			collectKeys(path, used)
		}
		return nil
	})
	for _, k := range areaKeys {
		if !used[k] {
			t.Errorf("area key %q is used nowhere: drop it from areaKeys", k)
		}
	}
	t.Logf("checked %d attributes", checked)
}

// keyOf returns the literal key of a slog attribute constructor call.
func keyOf(e ast.Expr) (fn, key string, pos token.Pos, ok bool) {
	call, isCall := e.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return "", "", 0, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", "", 0, false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent || pkg.Name != "slog" || !attrConstructors[sel.Sel.Name] {
		return "", "", 0, false
	}
	lit, isLit := call.Args[0].(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", "", 0, false
	}
	k, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", "", 0, false
	}
	return sel.Sel.Name, k, lit.Pos(), true
}

func parse(path string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	return fset, f, err
}

// checkFile judges one file and returns how many attributes it checked.
func checkFile(path, rel string, ok map[string]bool, unknown map[string]int) (int, []finding, error) {
	fset, f, err := parse(path)
	if err != nil {
		return 0, nil, fmt.Errorf("slogkeys: parse %s: %w", rel, err)
	}
	if f.Name.Name == "main" {
		return 0, nil, nil
	}
	var n int
	var out []finding
	at := func(p token.Pos) string { return fmt.Sprintf("%s:%d", rel, fset.Position(p).Line) }
	ast.Inspect(f, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if fn, k, pos, isAttr := keyOf(call); isAttr {
			n++
			switch {
			case !ok[k]:
				unknown[k]++
				out = append(out, finding{at(pos), fmt.Sprintf("slog.%s key %q is not in the table (docs/guides/observability.md; script/slogkeys)", fn, k)})
			case k == "err" && fn != "Any":
				out = append(out, finding{at(pos), fmt.Sprintf("slog.%s(\"err\", …): the error is slog.Any(\"err\", err)", fn)})
			}
		}
		// One call states a key once.
		seen := map[string]bool{}
		for _, a := range call.Args {
			if _, k, pos, isAttr := keyOf(a); isAttr {
				if seen[k] {
					out = append(out, finding{at(pos), fmt.Sprintf("key %q stated twice in one call", k)})
				}
				seen[k] = true
			}
		}
		return true
	})
	return n, out, nil
}

// collectKeys adds every literal key of the file's attribute calls to used.
func collectKeys(path string, used map[string]bool) {
	_, f, err := parse(path)
	if err != nil || f.Name.Name == "main" {
		return
	}
	ast.Inspect(f, func(node ast.Node) bool {
		if e, isExpr := node.(ast.Expr); isExpr {
			if _, k, _, isAttr := keyOf(e); isAttr {
				used[k] = true
			}
		}
		return true
	})
}

const (
	guidePath  = "docs/guides/observability.md"
	beginGuide = "<!-- BEGIN slogkeys: generated by `go test ./script/slogkeys -run TestGuideTable -update`; do not edit -->"
	endGuide   = "<!-- END slogkeys -->"
)

// guideSection renders the key section of the guide from the table.
func guideSection() string {
	var b strings.Builder
	b.WriteString(beginGuide + "\n\n")
	b.WriteString("| Key | Value | Meaning |\n| --- | --- | --- |\n")
	for _, k := range coreKeys {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", k.name, k.value, k.meaning)
	}
	b.WriteString("\nThe keys one area uses for what only it logs, accepted as they stand:\n\n")
	area := slices.Clone(areaKeys)
	slices.Sort(area)
	quoted := make([]string, len(area))
	for i, k := range area {
		quoted[i] = "`" + k + "`"
	}
	b.WriteString(strings.Join(quoted, ", ") + ".\n\n")
	b.WriteString(endGuide)
	return b.String()
}

// TestGuideTable holds the guide's key section to the table.
func TestGuideTable(t *testing.T) {
	path := filepath.Join(moduleRoot(t), guidePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	i, j := strings.Index(page, beginGuide), strings.Index(page, endGuide)
	if i < 0 || j < i {
		t.Fatalf("%s has no generated key section (%q … %q)", guidePath, beginGuide, endGuide)
	}
	want := page[:i] + guideSection() + page[j+len(endGuide):]
	if want == page {
		return
	}
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Errorf("%s's key section differs from the table; run go test ./script/slogkeys -run TestGuideTable -update", guidePath)
}

// TestAreaKeysAvoidTheCore: no area key is a core key, and the lists hold
// no duplicate.
func TestAreaKeysAvoidTheCore(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range coreKeys {
		if seen[k.name] {
			t.Errorf("core key %q twice", k.name)
		}
		seen[k.name] = true
	}
	for _, k := range areaKeys {
		if seen[k] {
			t.Errorf("area key %q is a core key or listed twice", k)
		}
		seen[k] = true
	}
}

// TestCheckFileReports: the checker reports an unknown key, an `err`
// that is not slog.Any and a key stated twice, and accepts the rest.
func TestCheckFileReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.go")
	src := `package x

import "log/slog"

func f(l *slog.Logger, err error) {
	l.Info("x.a", slog.Int("fabric", 1))
	l.Info("x.b", slog.String("err", err.Error()))
	l.Info("x.c", slog.Int("session_id", 1), slog.Int("session_id", 2))
	l.Info("x.d", slog.Any("err", err), slog.Int("fabric_index", 1))
}
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	n, got, err := checkFile(path, "x.go", allowed(), map[string]int{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Errorf("checked %d attributes, want 6", n)
	}
	want := []string{"x.go:6", "x.go:7", "x.go:8"}
	at := make([]string, 0, len(got))
	for _, f := range got {
		at = append(at, f.pos)
	}
	if !slices.Equal(at, want) {
		t.Errorf("findings at %v, want %v: %v", at, want, got)
	}
}
