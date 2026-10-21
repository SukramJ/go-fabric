// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build ignore

// bx3-mutex adds "// <mu> guards <fields>." above every sync.Mutex /
// sync.RWMutex struct field that has no comment (BX3 sweep 3, G1-16, ADR 0026
// P6). The guarded set is derived from the Lock sites in the same file: the
// fields of the receiver read or written between a Lock and its Unlock (or the
// end of the block when the Unlock is deferred). A mutex whose sites are
// irregular, that has no Lock site in its file, or whose set is empty or
// larger than maxFields is not touched; it is printed as "SKIP" with the
// reason, for the report.
//
// Usage, from the repository root (idempotent; a field that has a comment is
// never touched):
//
//	go run notes/briefs/reports/BX3-scripts/bx3-mutex.go [-apply] [-skip path,path...] dir...
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const maxFields = 7

type mu struct {
	typ, name string
	pos       token.Pos
	line      int
	indent    string
	fields    map[string]bool
	sites     int
	irregular string
}

func main() {
	apply := flag.Bool("apply", false, "write the comments")
	skip := flag.String("skip", "", "comma-separated path prefixes to leave alone")
	flag.Parse()
	var skips []string
	for _, s := range strings.Split(*skip, ",") {
		if s != "" {
			skips = append(skips, s)
		}
	}
	var files []string
	for _, root := range flag.Args() {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			for _, s := range skips {
				if strings.HasPrefix(p, s) {
					return nil
				}
			}
			files = append(files, p)
			return nil
		})
	}
	sort.Strings(files)
	changed := 0
	for _, f := range files {
		changed += doFile(f, *apply)
	}
	fmt.Printf("mutex comments: %d proposed/applied\n", changed)
}

func isMutex(e ast.Expr) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := s.X.(*ast.Ident)
	return ok && x.Name == "sync" && (s.Sel.Name == "Mutex" || s.Sel.Name == "RWMutex")
}

func doFile(path string, apply bool) int {
	fset := token.NewFileSet()
	src, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return 0
	}
	lines := strings.Split(string(src), "\n")
	muxes := map[string][]*mu{}              // type -> mutexes without a comment
	fieldsOf := map[string]map[string]bool{} // type -> field names
	fieldType = map[string]map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fieldsOf[ts.Name.Name] = map[string]bool{}
			fieldType[ts.Name.Name] = map[string]string{}
			for _, fl := range st.Fields.List {
				if !isMutex(fl.Type) {
					for _, n := range fl.Names {
						fieldsOf[ts.Name.Name][n.Name] = true
						fieldType[ts.Name.Name][n.Name] = types.ExprString(fl.Type)
					}
				}
				if !isMutex(fl.Type) {
					continue
				}
				if fl.Doc != nil || fl.Comment != nil {
					continue
				}
				p := fset.Position(fl.Pos())
				indent := lines[p.Line-1][:len(lines[p.Line-1])-len(strings.TrimLeft(lines[p.Line-1], "\t"))]
				for _, n := range fl.Names {
					muxes[ts.Name.Name] = append(muxes[ts.Name.Name], &mu{typ: ts.Name.Name, name: n.Name, pos: fl.Pos(), line: p.Line, indent: indent, fields: map[string]bool{}})
				}
				if len(fl.Names) > 1 {
					for _, m := range muxes[ts.Name.Name][len(muxes[ts.Name.Name])-len(fl.Names):] {
						m.irregular = "several mutexes in one declaration"
					}
				}
			}
		}
	}
	if len(muxes) == 0 {
		return 0
	}
	// mutex fields themselves never count as guarded data
	isMu := map[string]map[string]bool{}
	for t, ms := range muxes {
		isMu[t] = map[string]bool{}
		for _, m := range ms {
			isMu[t][m.name] = true
		}
	}
	summarize(file, fieldsOf)
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
			continue
		}
		typ := recvType(fd.Recv.List[0].Type)
		ms := muxes[typ]
		if len(ms) == 0 {
			continue
		}
		recv := fd.Recv.List[0].Names[0].Name
		walkBlock(fd.Body.List, recv, typ, ms, fieldsOf[typ])
	}
	type edit struct {
		line int
		text string
	}
	var edits []edit
	for _, ms := range muxesSorted(muxes) {
		if ms.irregular == "" && ms.sites == 0 {
			ms.irregular = "no Lock site in this file"
		}
		if ms.irregular == "" {
			for k := range ms.fields {
				if isMu[ms.typ][k] || !written[ms.typ][k] {
					delete(ms.fields, k)
				}
			}
			if len(ms.fields) == 0 {
				ms.irregular = "no field read or written under the lock"
			} else if len(ms.fields) > maxFields {
				ms.irregular = fmt.Sprintf("%d fields under the lock", len(ms.fields))
			}
		}
		if ms.irregular != "" {
			fmt.Printf("SKIP %s:%d %s.%s: %s\n", path, ms.line, ms.typ, ms.name, ms.irregular)
			continue
		}
		var names []string
		for k := range ms.fields {
			names = append(names, k)
		}
		sort.Strings(names)
		text := ms.indent + "// " + ms.name + " guards " + join(names) + "."
		fmt.Printf("%s:%d %s.%s: %s\n", path, ms.line, ms.typ, ms.name, strings.TrimSpace(text))
		edits = append(edits, edit{ms.line, text})
	}
	if apply && len(edits) > 0 {
		sort.Slice(edits, func(i, j int) bool { return edits[i].line > edits[j].line })
		for _, e := range edits {
			lines = append(lines[:e.line-1], append([]string{e.text}, lines[e.line-1:]...)...)
		}
		_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		// a comment above a field breaks gofmt's alignment block
		g := filepath.Join(os.Getenv("HOME"), "go", "bin", "gofumpt")
		if _, err := os.Stat(g); err == nil {
			_ = exec.Command(g, "-w", path).Run()
		}
	}
	return len(edits)
}

func muxesSorted(m map[string][]*mu) []*mu {
	var all []*mu
	for _, v := range m {
		all = append(all, v...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].pos < all[j].pos })
	return all
}

func join(n []string) string {
	switch len(n) {
	case 1:
		return n[0]
	case 2:
		return n[0] + " and " + n[1]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

func recvType(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if i, ok := e.(*ast.IndexExpr); ok {
		e = i.X
	}
	if i, ok := e.(*ast.Ident); ok {
		return i.Name
	}
	return ""
}

// lockCall reports whether stmt is `recv.name.Lock()` (kind "lock") or
// `recv.name.Unlock()` (kind "unlock"), or the deferred form.
func lockCall(stmt ast.Stmt, recv string) (name, kind string, deferred bool) {
	var call *ast.CallExpr
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		call, _ = s.X.(*ast.CallExpr)
	case *ast.DeferStmt:
		call = s.Call
		deferred = true
	}
	if call == nil {
		return "", "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	if x, ok := inner.X.(*ast.Ident); !ok || x.Name != recv {
		return "", "", false
	}
	switch sel.Sel.Name {
	case "Lock", "RLock":
		return inner.Sel.Name, "lock", deferred
	case "Unlock", "RUnlock":
		return inner.Sel.Name, "unlock", deferred
	}
	return "", "", false
}

// walkBlock handles one statement list: a Lock statement opens a region that
// closes at the matching Unlock statement of the same list, or at the end of
// the list when an Unlock is deferred. Nested blocks are searched for further
// Lock statements as well.
func walkBlock(list []ast.Stmt, recv, typ string, ms []*mu, fields map[string]bool) {
	for i, st := range list {
		if b, ok := st.(*ast.BlockStmt); ok {
			walkBlock(b.List, recv, typ, ms, fields)
			continue
		}
		name, kind, deferred := lockCall(st, recv)
		if kind == "lock" && !deferred {
			var m *mu
			for _, c := range ms {
				if c.name == name {
					m = c
				}
			}
			if m != nil {
				m.sites++
				end := len(list)
				matched := false
				for j := i + 1; j < len(list); j++ {
					n2, k2, d2 := lockCall(list[j], recv)
					if n2 != name || k2 != "unlock" {
						continue
					}
					if d2 {
						matched = true // deferred: the region is the rest of the list
						break
					}
					end, matched = j, true
					break
				}
				if !matched {
					m.irregular = "a Lock without an Unlock in the same block"
				}
				for _, s := range list[i+1 : end] {
					collect(s, recv, typ, fields, m)
				}
			}
		}
		// nested blocks may hold their own Lock statements
		ast.Inspect(st, func(n ast.Node) bool {
			switch b := n.(type) {
			case *ast.BlockStmt:
				walkBlock(b.List, recv, typ, ms, fields)
				return false
			case *ast.CaseClause:
				walkBlock(b.Body, recv, typ, ms, fields)
			case *ast.CommClause:
				walkBlock(b.Body, recv, typ, ms, fields)
			}
			return true
		})
	}
}

// method summary: the receiver fields a method touches, the methods it calls
// on its receiver, and whether it takes a lock itself.
type summary struct {
	fields  map[string]bool
	callees map[string]bool
	locks   bool
}

var (
	summaries map[string]*summary // "Type.method" -> summary
	fieldType map[string]map[string]string
	written   map[string]map[string]bool // type -> fields some method of the type mutates
)

// root returns F for an expression rooted at recv.F (through index, star,
// paren and further selectors), or "".
func root(e ast.Expr, recv string) string {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == recv {
				return x.Sel.Name
			}
			e = x.X
		default:
			return ""
		}
	}
}

func mutatingType(t string) bool {
	for _, p := range []string{"sync.", "time.Timer", "time.Ticker", "strings.Builder", "bytes.Buffer", "atomic."} {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

func summarize(file *ast.File, fieldsOf map[string]map[string]bool) {
	summaries = map[string]*summary{}
	written = map[string]map[string]bool{}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
			continue
		}
		typ := recvType(fd.Recv.List[0].Type)
		recv := fd.Recv.List[0].Names[0].Name
		sm := &summary{fields: map[string]bool{}, callees: map[string]bool{}}
		if written[typ] == nil {
			written[typ] = map[string]bool{}
		}
		mark := func(e ast.Expr) {
			if f := root(e, recv); f != "" && fieldsOf[typ][f] {
				written[typ][f] = true
			}
		}
		ast.Inspect(fd.Body, func(x ast.Node) bool {
			switch n := x.(type) {
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					mark(l)
				}
			case *ast.IncDecStmt:
				mark(n.X)
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					mark(n.X)
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "delete" || id.Name == "clear") && len(n.Args) > 0 {
					mark(n.Args[0])
				}
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
					if f := root(sel.X, recv); f != "" && fieldsOf[typ][f] && mutatingType(fieldType[typ][f]) {
						written[typ][f] = true
					}
				}
			}
			sel, ok := x.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv {
				if fieldsOf[typ][sel.Sel.Name] {
					sm.fields[sel.Sel.Name] = true
				} else {
					sm.callees[sel.Sel.Name] = true
				}
			}
			if in, ok := sel.X.(*ast.SelectorExpr); ok {
				if id, ok := in.X.(*ast.Ident); ok && id.Name == recv && (sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock") {
					sm.locks = true
				}
			}
			return true
		})
		summaries[typ+"."+fd.Name.Name] = sm
	}
}

func addClosure(typ, method string, m *mu, seen map[string]bool) {
	key := typ + "." + method
	sm := summaries[key]
	if sm == nil || sm.locks || seen[key] {
		return
	}
	seen[key] = true
	for f := range sm.fields {
		m.fields[f] = true
	}
	for c := range sm.callees {
		addClosure(typ, c, m, seen)
	}
}

func collect(n ast.Node, recv string, typ string, fields map[string]bool, m *mu) {
	ast.Inspect(n, func(x ast.Node) bool {
		sel, ok := x.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv {
			if fields[sel.Sel.Name] {
				m.fields[sel.Sel.Name] = true
			} else {
				addClosure(typ, sel.Sel.Name, m, map[string]bool{})
			}
		}
		return true
	})
}
