// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"testing"

	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestForgetLeftHalfDoneIsFinishedAtTheNextNew (review II,
// R2-P6-store-2406): a Forget whose erasure of the values the store
// refuses is left half done — its number freed, its values kept, which a
// later Add of the key would restore; the next New on the store finishes
// that forget before the host adds anything.
func TestForgetLeftHalfDoneIsFinishedAtTheNextNew(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"nonvolatile."}}
	eps := epmem.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: eps}
	n, ep := nvRun(t, cfg, nvHeat)
	if s := dispatchWrite(t, n, attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint), int64(2100)); s != im.StatusSuccess {
		t.Fatalf("write: %v", s)
	}
	const valueKey = "nonvolatile.thermo.0201.0012"
	stored := func() bool {
		t.Helper()
		v, ok, err := st.GetSetting(ctx, valueKey)
		if err != nil {
			t.Fatal(err)
		}
		return ok && v != ""
	}
	records := func() int {
		t.Helper()
		recs, err := eps.ListEndpoints(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, r := range recs {
			if r.Key != nil && r.Key.String() == nvHeat.key {
				count++
			}
		}
		return count
	}
	st.refuse.Store(true)
	if err := n.Aggregator().Forget(ctx, endpoint.StringKey(nvHeat.key)); !errors.Is(err, errKeyRefused) {
		t.Fatalf("Forget with the store refusing the erasure: %v, want %v", err, errKeyRefused)
	}
	st.refuse.Store(false)
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !stored() {
		t.Fatal("the refused Forget erased the value; nothing is left half done")
	}
	newNode(t, cfg)
	if stored() {
		t.Errorf("%s still stored after the next New finished the forget", valueKey)
	}
	if got := records(); got != 0 {
		t.Errorf("%d endpoint records of the forgotten key after the next New, want none", got)
	}
	if v, ok, err := st.GetSetting(ctx, "aggregator.forget_pending"); err != nil || ok && v != "" {
		t.Errorf("forget record %q (found %v, %v) after it was finished, want none", v, ok, err)
	}
}
