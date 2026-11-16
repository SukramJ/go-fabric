// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
)

// removingSelector stands for a device whose map changes while it takes a
// selection: SelectAreas removes area 9 through the host's setter.
type removingSelector struct{ srv *servicearea.Server }

func (r *removingSelector) SelectAreas(context.Context, []uint32) (status servicearea.SelectAreasStatus, text string, err error) {
	if err := r.srv.SetSupportedAreas([]servicearea.Area{area(7, ptr[uint32](0), "Kitchen")}); err != nil {
		return 0, "", err
	}
	return servicearea.SelectAreasSuccess, "", nil
}

// TestSelectAreasDoesNotStoreAnAreaRemovedMeanwhile (R2-P6-conc-1510): a
// selection naming an area the host removed while the device took it is
// answered UnsupportedArea and not stored.
func TestSelectAreasDoesNotStoreAnAreaRemovedMeanwhile(t *testing.T) {
	t.Parallel()
	sel := &removingSelector{}
	cfg := vacuum(&device{})
	cfg.Selector = sel
	srv := newServer(t, cfg)
	sel.srv = srv
	resp, err := srv.MatterInvoke(context.Background(), sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7, 9}})
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resp.(sa.SelectAreasResponse); r.Status != servicearea.SelectAreasUnsupportedArea {
		t.Errorf("response %+v, want UnsupportedArea", resp)
	}
	if v, _ := srv.MatterRead(sa.AttrSelectedAreas); len(v.([]uint32)) != 0 {
		t.Errorf("SelectedAreas %v names an area that is no longer supported", v)
	}
}

// TestProgressIsCopiedDeep (decision 142): a Progress list handed in or
// read out shares no field with the one the server holds.
func TestProgressIsCopiedDeep(t *testing.T) {
	t.Parallel()
	srv := newServer(t, vacuum(&device{}))
	in := []servicearea.Progress{{AreaId: 7, Status: servicearea.StatusPending, TotalOperationalTime: &spec.Nullable[uint32]{Value: 10}}}
	if err := srv.SetProgress(in); err != nil {
		t.Fatal(err)
	}
	in[0].TotalOperationalTime.Value = 99
	read := func() uint32 {
		v, _ := srv.MatterRead(sa.AttrProgress)
		return v.(spec.List[servicearea.Progress])[0].TotalOperationalTime.Value
	}
	if got := read(); got != 10 {
		t.Errorf("Progress follows the caller's list after SetProgress: %d", got)
	}
	v, _ := srv.MatterRead(sa.AttrProgress)
	v.(spec.List[servicearea.Progress])[0].TotalOperationalTime.Value = 77
	if got := read(); got != 10 {
		t.Errorf("Progress follows a list a read returned: %d", got)
	}
}
