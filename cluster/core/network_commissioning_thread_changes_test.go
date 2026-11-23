// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/contract"
)

// heardAttrs collects the attributes a server's change listener hears.
type heardAttrs struct {
	mu    sync.Mutex
	attrs []uint32
}

func (h *heardAttrs) listen(c contract.Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attrs = append(h.attrs, c.Attributes...)
}

// take answers what was heard since the last take.
func (h *heardAttrs) take() []uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.attrs
	h.attrs = nil
	return out
}

func threadChangeListener(t *testing.T, srv any) *heardAttrs {
	t.Helper()
	notifier, ok := srv.(contract.AttributeChangeNotifier)
	if !ok {
		t.Fatalf("%T implements no contract.AttributeChangeNotifier: its changes outside a command reach no subscriber", srv)
	}
	h := &heardAttrs{}
	t.Cleanup(notifier.OnAttributeChange(h.listen))
	return h
}

func hasAll(got []uint32, want ...uint32) bool {
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// The Thread server reports what it changes outside a command of its own
// (R3-par-3051): the fail-safe's expiry reverts Networks, InterfaceEnabled
// and — with an empty backup — the three Last… attributes; Refresh and the
// re-attach after an abandoned ConnectNetwork move Networks' Connected.
func TestThreadNetworkCommissioningReportsChangesOutsideACommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("fail-safe expiry", func(t *testing.T) {
		t.Parallel()
		br := &simBorderRouter{}
		armed, crumbs := true, []uint64{}
		n := newThreadNC(t, br, &armed, &crumbs)
		heard := threadChangeListener(t, n)
		if _, err := n.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset}); err != nil {
			t.Fatal(err)
		}
		if _, err := n.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID}); err != nil {
			t.Fatal(err)
		}
		heard.take() // the commands' own changes the bridge reports
		n.OnFailSafeExpired(ctx)
		if got := heard.take(); !hasAll(got, netcommAttrNetworks, netcommAttrInterfaceEnabled,
			netcommAttrLastNetworkingStatus, netcommAttrLastNetworkID, netcommAttrLastConnectErrorValue) {
			t.Errorf("the expiry's revert to an empty backup reported %v, want Networks, InterfaceEnabled and the three Last… attributes", got)
		}
	})

	t.Run("refresh", func(t *testing.T) {
		t.Parallel()
		br := &simBorderRouter{active: threadDataset, attached: true, enabled: true}
		armed, crumbs := false, []uint64{}
		n := newThreadNC(t, br, &armed, &crumbs)
		heard := threadChangeListener(t, n)
		n.Refresh(ctx)
		if got := heard.take(); len(got) != 0 {
			t.Errorf("a Refresh that found nothing moved reported %v", got)
		}
		br.mu.Lock()
		br.attached = false
		br.mu.Unlock()
		n.Refresh(ctx)
		if got := heard.take(); !hasAll(got, netcommAttrNetworks) {
			t.Errorf("a Refresh over a border router that detached reported %v, want Networks", got)
		}
	})

	t.Run("re-attach after an abandoned connect", func(t *testing.T) {
		t.Parallel()
		br := &simBorderRouter{active: threadDataset, attached: true, enabled: true}
		armed, crumbs := false, []uint64{}
		n := newThreadNC(t, br, &armed, &crumbs)
		heard := threadChangeListener(t, n)
		// The state the expiry left: the reverted network staged, read as
		// not attached while the abandoned attach still ran.
		n.mu.Lock()
		n.connected, n.enabled = false, false
		n.expiries = 1
		n.mu.Unlock()
		n.reattachReverted(ctx, 0)
		if got := heard.take(); !hasAll(got, netcommAttrNetworks, netcommAttrInterfaceEnabled) {
			t.Errorf("the re-attach reported %v, want Networks and InterfaceEnabled", got)
		}
	})
}

// TestFailSafeParticipantsReportTheirChanges is the standing guard of
// R3-par-3051's class: every server of this package that changes state at
// the fail-safe's expiry (an OnFailSafeExpired method) does so outside a
// command, so it has an OnAttributeChange through which the bridge hears
// it. Read from the package source, so a new participant is checked
// without a list to extend.
func TestFailSafeParticipantsReportTheirChanges(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]map[string]bool{} // receiver type → method names
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			typ := fn.Recv.List[0].Type
			if star, ok := typ.(*ast.StarExpr); ok {
				typ = star.X
			}
			id, ok := typ.(*ast.Ident)
			if !ok {
				continue
			}
			if methods[id.Name] == nil {
				methods[id.Name] = map[string]bool{}
			}
			methods[id.Name][fn.Name.Name] = true
		}
	}
	participants := 0
	for typ, ms := range methods {
		if !ms["OnFailSafeExpired"] {
			continue
		}
		participants++
		if !ms["OnAttributeChange"] {
			t.Errorf("%s changes state at the fail-safe's expiry but has no OnAttributeChange: a subscriber is not told", typ)
		}
	}
	if participants == 0 {
		t.Fatal("no OnFailSafeExpired found: the guard reads nothing")
	}
}
