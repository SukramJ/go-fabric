// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestSpecVersion decodes matter.js's example 0x01040200 as 1.4.2 and the
// commissioned site device's SpecificationVersion as the 1.x version it
// reports.
func TestSpecVersion(t *testing.T) {
	t.Parallel()
	v := client.SpecVersionOf(0x01040200)
	if v != (client.SpecVersion{Major: 1, Minor: 4, Patch: 2}) || v.String() != "1.4.2" || v.Uint32() != 0x01040200 {
		t.Fatalf("0x01040200: %+v %q", v, v)
	}
	if s := client.SpecVersionOf(0x01030001).String(); s != "1.3.0.1" {
		t.Fatalf("a reserved byte: %q", s)
	}
	if _, ok := (client.Properties{}).Specification(); ok {
		t.Fatal("an unknown version decoded")
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.UncommissionedPair(fabrictest.PairOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := n.Properties()
	got, ok := p.Specification()
	if !ok || got.Major != 1 || got.Uint32() != *p.SpecificationVersion {
		t.Fatalf("the device's version %+v (%v) of %v", got, ok, p.SpecificationVersion)
	}
}
