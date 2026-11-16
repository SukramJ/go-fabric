// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// rotatingID is the commissionable record's RI value; ok false without
// the key or without the record.
func rotatingID(adv *mdns.Noop) (string, bool) {
	services := adv.Active()
	for i := range services {
		svc := &services[i]
		if svc.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		for _, txt := range svc.TXT {
			if txt.Key == "RI" {
				return txt.Value, true
			}
		}
	}
	return "", false
}

// TestRotatingDeviceIDIsOffByDefault: a node configured without
// Commissioning.RotatingDeviceID advertises no RI (R2-P6-sec-0207).
func TestRotatingDeviceIDIsOffByDefault(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	startedNode(t, adv)
	waitRecord(t, adv, "published", func(_, _ uint64, ok bool) bool { return ok })
	if ri, ok := rotatingID(adv); ok {
		t.Fatalf("RI=%q advertised with the option off", ri)
	}
}

// TestRotatingDeviceIDRotates holds R2-P6-sec-0207: with the option on,
// the RI rotates — its lifetime counter advances with every commissioning
// advertisement and survives a restart — and it is not the value the
// node's public identity (vendor, product, serial number, label) yields.
func TestRotatingDeviceIDRotates(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	start := func() string {
		adv := mdns.NewNoop()
		cfg := minimalConfig(openDB(t, path))
		cfg.Advertiser = adv
		cfg.Commissioning.RotatingDeviceID = true
		n, err := node.New(context.Background(), cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := n.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		waitRecord(t, adv, "published", func(_, _ uint64, ok bool) bool { return ok })
		ri, ok := rotatingID(adv)
		if err := n.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if !ok {
			t.Fatal("no RI advertised with the option on")
		}
		return ri
	}
	first, second := start(), start()
	if len(first) != 36 || len(second) != 36 {
		t.Fatalf("RI %q / %q, want 36 hex characters each", first, second)
	}
	counter := func(ri string) uint16 {
		b, err := hex.DecodeString(ri[:4])
		if err != nil {
			t.Fatal(err)
		}
		return uint16(b[0]) | uint16(b[1])<<8
	}
	if c1, c2 := counter(first), counter(second); c2 != c1+1 || c1 == 0 {
		t.Fatalf("lifetime counters %d then %d, want a non-zero counter advanced by one per advertisement", c1, c2)
	}
	if first[4:] == second[4:] {
		t.Fatal("the RI's hash did not change with the lifetime counter")
	}
	cfg := minimalConfig(nil)
	pd, bi := cfg.ProductDescription, cfg.BasicInformation
	derived := mdns.DeriveUniqueIDFromIdentity(pd.VendorID, pd.ProductID, bi.SerialNumber, bi.NodeLabel) //nolint:staticcheck // the identity-derived value the RI must not be
	if first == mdns.GenerateRotatingID(derived, counter(first)) {
		t.Fatal("the RI is computable from the node's public identity")
	}
}
