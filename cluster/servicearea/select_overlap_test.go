// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/servicearea"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
)

// removingSelector stands for a device whose map changes while it takes a
// selection: SelectAreas removes area 9 through the host's setter.
type removingSelector struct{ srv *servicearea.Server }

func (r *removingSelector) SelectAreas(context.Context, []uint32) (servicearea.SelectAreasStatus, string, error) {
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
