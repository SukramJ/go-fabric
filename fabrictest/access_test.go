// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"errors"
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

func TestPairGrant(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	if err := site.UncommissionedPair(fabrictest.PairOptions{}).Grant(ctx, store.PrivilegeView); !errors.Is(err, fabrictest.ErrNotCommissioned) {
		t.Fatalf("Grant on an uncommissioned pair: %v", err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{})
	if err := p.Grant(ctx, 0); err == nil {
		t.Fatal("Grant of privilege 0")
	}
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	if err := p.Grant(ctx, store.PrivilegeView); err != nil {
		t.Fatal(err)
	}
	if _, err := fabrictest.Read(ctx, p, label); err != nil {
		t.Fatalf("View read: %v", err)
	}
	var se *interaction.StatusError
	if err := fabrictest.Write(ctx, p, label, stringValue("x")); !errors.As(err, &se) || se.Status != im.StatusUnsupportedAccess {
		t.Fatalf("View write of NodeLabel (Manage): %v", err)
	}
	if err := p.Grant(ctx, store.PrivilegeAdminister); err != nil {
		t.Fatal(err)
	}
	if err := fabrictest.Write(ctx, p, label, stringValue("x")); err != nil {
		t.Fatalf("Administer write: %v", err)
	}
}
