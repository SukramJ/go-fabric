// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/im"
)

func ptr[T any](v T) *T { return &v }

// area is a location area on mapID (nil: no map).
func area(id uint32, mapID *uint32, name string) servicearea.Area {
	a := servicearea.Area{
		AreaId: id,
		MapId:  spec.NullOf[uint32](),
		AreaInfo: servicearea.AreaInfo{
			LocationInfo: spec.ValueOf(servicearea.LocationInfo{
				LocationName: name, FloorNumber: spec.NullOf[int16](), AreaType: spec.NullOf[uint8](),
			}),
			LandmarkInfo: spec.NullOf[servicearea.LandmarkInfo](),
		},
	}
	if mapID != nil {
		a.MapId = spec.ValueOf(*mapID)
	}
	return a
}

// landmark is a landmark-only area without a map.
func landmark(id uint32) servicearea.Area {
	return servicearea.Area{
		AreaId: id, MapId: spec.NullOf[uint32](),
		AreaInfo: servicearea.AreaInfo{
			LocationInfo: spec.NullOf[servicearea.LocationInfo](),
			LandmarkInfo: spec.ValueOf(servicearea.LandmarkInfo{LandmarkTag: 3, RelativePositionTag: spec.NullOf[uint8]()}),
		},
	}
}

// device is a host: it records the selections and skips it is asked for
// and answers with the configured status.
type device struct {
	mu         sync.Mutex
	selections [][]uint32
	skips      []uint32
	selStatus  servicearea.SelectAreasStatus
	skipStatus servicearea.SkipAreaStatus
	err        error
}

func (d *device) SelectAreas(_ context.Context, areas []uint32) (servicearea.SelectAreasStatus, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.selections = append(d.selections, areas)
	return d.selStatus, "", d.err
}

func (d *device) SkipArea(_ context.Context, a uint32) (servicearea.SkipAreaStatus, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.skips = append(d.skips, a)
	return d.skipStatus, "", d.err
}

// vacuum is the reference daemon's shape: one map, two areas, PROG and
// MAPS, CurrentArea and EstimatedEndTime, SkipArea.
func vacuum(d *device) servicearea.Config {
	return servicearea.Config{
		Features: servicearea.FeatureMaps | servicearea.FeatureProgressReporting,
		Selector: d, Skipper: d,
		SupportedMaps:  []servicearea.Map{{MapId: 0, Name: "Ground"}},
		SupportedAreas: []servicearea.Area{area(7, ptr[uint32](0), "Kitchen"), area(9, ptr[uint32](0), "Living Room")},
		CurrentArea:    true, EstimatedEndTime: true,
	}
}

func newServer(t *testing.T, cfg servicearea.Config) *servicearea.Server {
	t.Helper()
	srv, err := servicearea.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// notifications records the attribute changes a server reports.
func notifications(srv *servicearea.Server) *[]uint32 {
	var mu sync.Mutex
	seen := &[]uint32{}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		mu.Lock()
		*seen = append(*seen, ids...)
		mu.Unlock()
	})
	return seen
}

func read[T any](t *testing.T, srv *servicearea.Server, attr uint32) T {
	t.Helper()
	v, ok := srv.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X not read", attr)
	}
	got, ok := v.(T)
	if !ok {
		t.Fatalf("attribute 0x%04X is %T", attr, v)
	}
	return got
}

// TestConstructionRules walks matter.js's initialize assertions
// (ServiceAreaServer.ts:36-73) and the conformance gates of the optional
// elements: every bad configuration is refused.
func TestConstructionRules(t *testing.T) {
	t.Parallel()
	ok := vacuum(&device{})
	for name, mutate := range map[string]func(*servicearea.Config){
		"no selector":            func(c *servicearea.Config) { c.Selector = nil },
		"EstimatedEndTime alone": func(c *servicearea.Config) { c.CurrentArea = false; c.Skipper = nil },
		"SkipArea needs a source": func(c *servicearea.Config) {
			c.Features = servicearea.FeatureMaps
			c.CurrentArea, c.EstimatedEndTime = false, false
		},
		"current without attribute": func(c *servicearea.Config) {
			c.CurrentArea, c.EstimatedEndTime, c.InitialCurrentArea = false, false, ptr[uint32](7)
		},
		"maps without MAPS": func(c *servicearea.Config) { c.Features = servicearea.FeatureProgressReporting },
		"progress without PROG": func(c *servicearea.Config) {
			c.Features = servicearea.FeatureMaps
			c.Progress = []servicearea.Progress{{AreaId: 7}}
		},
		"undefined feature": func(c *servicearea.Config) { c.Features |= 1 << 5 },
		"duplicate MapID": func(c *servicearea.Config) {
			c.SupportedMaps = append(c.SupportedMaps, servicearea.Map{MapId: 0, Name: "Upstairs"})
		},
		"duplicate MapName": func(c *servicearea.Config) {
			c.SupportedMaps = append(c.SupportedMaps, servicearea.Map{MapId: 1, Name: "Ground"})
		},
		"map name too long": func(c *servicearea.Config) { c.SupportedMaps[0].Name = string(make([]byte, 65)) },
		"duplicate AreaID":  func(c *servicearea.Config) { c.SupportedAreas[1].AreaId = 7 },
		"no location or landmark": func(c *servicearea.Config) {
			c.SupportedAreas[0].AreaInfo.LocationInfo = spec.NullOf[servicearea.LocationInfo]()
		},
		"location name too long":      func(c *servicearea.Config) { c.SupportedAreas[0] = area(7, ptr[uint32](0), string(make([]byte, 129))) },
		"empty location":              func(c *servicearea.Config) { c.SupportedAreas[0] = area(7, ptr[uint32](0), "") },
		"equal AreaInfo on one map":   func(c *servicearea.Config) { c.SupportedAreas[1] = area(9, ptr[uint32](0), "Kitchen") },
		"null map next to maps":       func(c *servicearea.Config) { c.SupportedAreas[0].MapId = spec.NullOf[uint32]() },
		"selected not supported":      func(c *servicearea.Config) { c.SelectedAreas = []uint32{8} },
		"selected twice":              func(c *servicearea.Config) { c.SelectedAreas = []uint32{7, 7} },
		"current not supported":       func(c *servicearea.Config) { c.InitialCurrentArea = ptr[uint32](8) },
		"progress not supported":      func(c *servicearea.Config) { c.Progress = []servicearea.Progress{{AreaId: 8}} },
		"progress twice":              func(c *servicearea.Config) { c.Progress = []servicearea.Progress{{AreaId: 7}, {AreaId: 7}} },
		"progress status out of enum": func(c *servicearea.Config) { c.Progress = []servicearea.Progress{{AreaId: 7, Status: 9}} },
	} {
		cfg := ok
		cfg.SupportedMaps = slices.Clone(ok.SupportedMaps)
		cfg.SupportedAreas = slices.Clone(ok.SupportedAreas)
		mutate(&cfg)
		if _, err := servicearea.New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Without maps every area must have a null MapID, and areas on
	// different maps are no conflict; a landmark-only area is valid.
	noMaps := servicearea.Config{Selector: &device{}, SupportedAreas: []servicearea.Area{area(1, nil, "Hall"), landmark(2)}}
	newServer(t, noMaps)
	noMaps.SupportedAreas = []servicearea.Area{area(1, ptr[uint32](0), "Hall")}
	if _, err := servicearea.New(noMaps); !errors.Is(err, servicearea.ErrInvalidState) {
		t.Errorf("a MapID without SupportedMaps: %v, want ErrInvalidState", err)
	}
	twoMaps := vacuum(&device{})
	twoMaps.SupportedMaps = append(slices.Clone(twoMaps.SupportedMaps), servicearea.Map{MapId: 1, Name: "Upstairs"})
	twoMaps.SupportedAreas = []servicearea.Area{area(7, ptr[uint32](0), "Bath"), area(9, ptr[uint32](1), "Bath")}
	newServer(t, twoMaps)
	// Landmark-only areas compare by their tags; floors compare by value.
	twin := landmark(3)
	noMaps.SupportedAreas = []servicearea.Area{landmark(2), twin}
	if _, err := servicearea.New(noMaps); !errors.Is(err, servicearea.ErrInvalidState) {
		t.Errorf("two equal landmarks: %v", err)
	}
	twin.AreaInfo.LandmarkInfo.Value.LandmarkTag = 4
	noMaps.SupportedAreas = []servicearea.Area{landmark(2), twin}
	newServer(t, noMaps)
	upper, lower := area(1, nil, "Hall"), area(2, nil, "Hall")
	upper.AreaInfo.LocationInfo.Value.FloorNumber = spec.ValueOf[int16](1)
	lower.AreaInfo.LocationInfo.Value.FloorNumber = spec.ValueOf[int16](0)
	noMaps.SupportedAreas = []servicearea.Area{upper, lower}
	newServer(t, noMaps)
}

// TestSelectAreas is matter.js's selectAreas: an unsupported area answers
// UnsupportedArea without asking the device, duplicates are dropped, the
// device's refusal is answered as is, and Success stores the selection
// and resets Progress to Pending.
func TestSelectAreas(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv := newServer(t, vacuum(d))
	seen := notifications(srv)
	ctx := context.Background()

	resp, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7, 8}})
	if r := resp.(sa.SelectAreasResponse); err != nil || r.Status != servicearea.SelectAreasUnsupportedArea || r.StatusText == "" {
		t.Errorf("unsupported: %+v %v", resp, err)
	}
	if len(d.selections) != 0 {
		t.Errorf("device asked about an unsupported selection: %v", d.selections)
	}

	d.selStatus = servicearea.SelectAreasInvalidInMode
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSelectAreas, &sa.SelectAreasRequest{NewAreas: []uint32{9}})
	if resp.(sa.SelectAreasResponse).Status != servicearea.SelectAreasInvalidInMode {
		t.Errorf("device refusal: %+v", resp)
	}
	if got := read[[]uint32](t, srv, sa.AttrSelectedAreas); len(got) != 0 {
		t.Errorf("a refused selection was stored: %v", got)
	}

	d.selStatus = servicearea.SelectAreasSuccess
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{9, 7, 9}})
	if resp.(sa.SelectAreasResponse).Status != servicearea.SelectAreasSuccess {
		t.Errorf("select: %+v", resp)
	}
	if got := read[[]uint32](t, srv, sa.AttrSelectedAreas); !slices.Equal(got, []uint32{9, 7}) {
		t.Errorf("SelectedAreas %v, want [9 7]", got)
	}
	if last := d.selections[len(d.selections)-1]; !slices.Equal(last, []uint32{9, 7}) {
		t.Errorf("device saw %v", last)
	}
	progress := read[spec.List[servicearea.Progress]](t, srv, sa.AttrProgress)
	if len(progress) != 2 || progress[0].AreaId != 9 || progress[1].Status != servicearea.StatusPending {
		t.Errorf("Progress %+v", progress)
	}
	if !slices.Contains(*seen, sa.AttrSelectedAreas) || !slices.Contains(*seen, sa.AttrProgress) {
		t.Errorf("reported %v", *seen)
	}

	// An empty selection is valid and leaves Progress alone (:200).
	if resp, _ := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{}); resp.(sa.SelectAreasResponse).Status != servicearea.SelectAreasSuccess {
		t.Errorf("empty selection: %+v", resp)
	}
	if got := read[spec.List[servicearea.Progress]](t, srv, sa.AttrProgress); len(got) != 2 {
		t.Errorf("an empty selection changed Progress: %+v", got)
	}

	d.err = errors.New("offline")
	if _, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{7}}); err == nil {
		t.Error("a device error did not fail SelectAreas")
	}
}

// TestSkipArea is matter.js's assertSkipServiceArea, then the device.
func TestSkipArea(t *testing.T) {
	t.Parallel()
	d := &device{}
	srv := newServer(t, vacuum(d))
	ctx := context.Background()
	resp, err := srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{SkippedArea: 7})
	if err != nil || resp.(sa.SkipAreaResponse).Status != servicearea.SkipAreaInvalidAreaList {
		t.Errorf("nothing selected: %+v %v", resp, err)
	}
	if err := srv.SetSelectedAreas([]uint32{9}); err != nil {
		t.Fatal(err)
	}
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSkipArea, &sa.SkipAreaRequest{SkippedArea: 7})
	if r := resp.(sa.SkipAreaResponse); r.Status != servicearea.SkipAreaInvalidSkippedArea || r.StatusText == "" {
		t.Errorf("not selected: %+v", r)
	}
	d.skipStatus = servicearea.SkipAreaInvalidInMode
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{SkippedArea: 9})
	if resp.(sa.SkipAreaResponse).Status != servicearea.SkipAreaInvalidInMode || !slices.Equal(d.skips, []uint32{9}) {
		t.Errorf("device answer: %+v, skips %v", resp, d.skips)
	}
	d.err = errors.New("offline")
	if _, err := srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{SkippedArea: 9}); err == nil {
		t.Error("a device error did not fail SkipArea")
	}
}

// TestInvokeRefusals: an unaccepted command, and fields of the wrong
// shape.
func TestInvokeRefusals(t *testing.T) {
	t.Parallel()
	srv := newServer(t, servicearea.Config{Selector: &device{}})
	ctx := context.Background()
	_, err := srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
		t.Errorf("SkipArea not accepted: %v", err)
	}
	for _, fields := range []any{nil, (*sa.SelectAreasRequest)(nil), sa.SkipAreaRequest{}} {
		_, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, fields)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
			t.Errorf("SelectAreas %T: %v", fields, err)
		}
	}
	full := newServer(t, vacuum(&device{}))
	_, err = full.MatterInvoke(ctx, sa.CmdSkipArea, sa.SelectAreasRequest{})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("SkipArea with SelectAreas fields: %v", err)
	}
	if err := full.MatterWrite(ctx, sa.AttrSelectedAreas, []uint32{}); err == nil {
		t.Error("a write to SelectedAreas was accepted")
	}
}

// TestSetters holds every host setter to the reactor it mirrors.
func TestSetters(t *testing.T) {
	t.Parallel()
	srv := newServer(t, vacuum(&device{}))
	if err := srv.SetSupportedMaps([]servicearea.Map{{MapId: 0, Name: "Ground"}, {MapId: 1, Name: "Upstairs"}}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetSupportedMaps([]servicearea.Map{{MapId: 0, Name: "A"}, {MapId: 0, Name: "B"}}); !errors.Is(err, servicearea.ErrInvalidState) {
		t.Errorf("duplicate MapID: %v", err)
	}
	areas := []servicearea.Area{area(7, ptr[uint32](0), "Kitchen"), area(10, ptr[uint32](1), "Bedroom")}
	if err := srv.SetSupportedAreas(areas); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetSupportedAreas([]servicearea.Area{area(7, nil, "Kitchen")}); !errors.Is(err, servicearea.ErrInvalidState) {
		t.Errorf("null MapID next to maps: %v", err)
	}
	if got := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas); len(got) != 2 || got[1].AreaId != 10 {
		t.Errorf("SupportedAreas %+v", got)
	}
	if got := read[spec.List[servicearea.Map]](t, srv, sa.AttrSupportedMaps); len(got) != 2 {
		t.Errorf("SupportedMaps %+v", got)
	}
	if err := srv.SetSelectedAreas([]uint32{9}); err == nil {
		t.Error("a removed area was selected")
	}
	if err := srv.SetCurrentArea(ptr[uint32](9)); err == nil {
		t.Error("an unsupported CurrentArea was accepted")
	}
	if err := srv.SetProgress([]servicearea.Progress{{AreaId: 9}}); err == nil {
		t.Error("an unsupported Progress entry was accepted")
	}
	if err := srv.SetProgress([]servicearea.Progress{{AreaId: 10, Status: servicearea.StatusOperating}}); err != nil {
		t.Error(err)
	}
	if err := srv.SetCurrentArea(ptr[uint32](10)); err != nil {
		t.Fatal(err)
	}
	if got := read[uint32](t, srv, sa.AttrCurrentArea); got != 10 {
		t.Errorf("CurrentArea %v", got)
	}

	// The setters of elements the server does not serve are refused.
	bare := newServer(t, servicearea.Config{Selector: &device{}, SupportedAreas: []servicearea.Area{area(1, nil, "Hall")}})
	if err := bare.SetSupportedMaps(nil); !errors.Is(err, servicearea.ErrConfig) {
		t.Errorf("SetSupportedMaps without MAPS: %v", err)
	}
	if err := bare.SetProgress(nil); !errors.Is(err, servicearea.ErrConfig) {
		t.Errorf("SetProgress without PROG: %v", err)
	}
	if err := bare.SetCurrentArea(nil); !errors.Is(err, servicearea.ErrConfig) {
		t.Errorf("SetCurrentArea unserved: %v", err)
	}
	if err := bare.SetEstimatedEndTime(nil); !errors.Is(err, servicearea.ErrConfig) {
		t.Errorf("SetEstimatedEndTime unserved: %v", err)
	}
	if _, ok := bare.MatterRead(sa.AttrCurrentArea); ok {
		t.Error("an unserved attribute was read")
	}
	if v, ok := bare.MatterRead(0xFFFD); !ok || v != sa.Revision {
		t.Errorf("ClusterRevision %v", v)
	}
	bare.RemoveSupportedMapsEntry(0) // no maps: nothing to do
	empty := newServer(t, servicearea.Config{Selector: &device{}})
	empty.RemoveSupportedAreasEntry(1) // no areas: nothing to do
}

// TestEstimatedEndTime follows matter.js's shouldEmit (:57-71) and the
// CurrentArea null rule (:162-167): a null CurrentArea nulls it; a change
// to or from null or 0, or a decrease, reports at once; an increase goes
// through the quiet throttle; an equal value changes nothing.
func TestEstimatedEndTime(t *testing.T) {
	t.Parallel()
	srv := newServer(t, vacuum(&device{}))
	seen := notifications(srv)
	count := func() int {
		n := 0
		for _, id := range *seen {
			if id == sa.AttrEstimatedEndTime {
				n++
			}
		}
		return n
	}
	if v, _ := srv.MatterRead(sa.AttrEstimatedEndTime); v != nil {
		t.Errorf("EstimatedEndTime starts at %v, want null", v)
	}
	if err := srv.SetEstimatedEndTime(ptr[uint32](1000)); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatalf("from null: %d reports", count())
	}
	_ = srv.SetEstimatedEndTime(ptr[uint32](1000)) // equal: nothing
	_ = srv.SetEstimatedEndTime(ptr[uint32](900))  // decrease: now
	if count() != 2 {
		t.Errorf("decrease: %d reports", count())
	}
	_ = srv.SetEstimatedEndTime(ptr[uint32](950)) // increase: throttled
	if count() != 2 {
		t.Errorf("an increase inside the interval reported at once")
	}
	if got := read[uint32](t, srv, sa.AttrEstimatedEndTime); got != 950 {
		t.Errorf("a throttled value reads %v", got)
	}
	_ = srv.SetEstimatedEndTime(ptr[uint32](0)) // to 0: now
	if count() != 3 {
		t.Errorf("to 0: %d reports", count())
	}
	if err := srv.SetCurrentArea(nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(sa.AttrEstimatedEndTime); v != nil {
		t.Errorf("a null CurrentArea left EstimatedEndTime %v", v)
	}
	if got := srv.MatterReportable(); slices.Contains(got, sa.AttrEstimatedEndTime) || !slices.Contains(got, sa.AttrCurrentArea) {
		t.Errorf("MatterReportable %v", got)
	}
}

// TestRemoveEntries is matter.js removeSupportedAreasEntry and
// removeSupportedMapsEntry: an area leaves SelectedAreas, Progress and
// CurrentArea with it; a map takes the first area on it along.
func TestRemoveEntries(t *testing.T) {
	t.Parallel()
	cfg := vacuum(&device{})
	cfg.SupportedMaps = []servicearea.Map{{MapId: 0, Name: "Ground"}, {MapId: 1, Name: "Upstairs"}}
	cfg.SupportedAreas = []servicearea.Area{area(7, ptr[uint32](0), "Kitchen"), area(9, ptr[uint32](0), "Hall"), area(10, ptr[uint32](1), "Bedroom")}
	cfg.SelectedAreas = []uint32{7, 9, 10}
	cfg.Progress = []servicearea.Progress{{AreaId: 7}, {AreaId: 9}, {AreaId: 10}}
	cfg.InitialCurrentArea = ptr[uint32](7)
	dv := &cluster.DataVersionTracker{}
	cfg.DataVersion = dv
	srv := newServer(t, cfg)
	_ = srv.SetEstimatedEndTime(ptr[uint32](500))
	before := srv.MatterDataVersion()

	srv.RemoveSupportedAreasEntry(7)
	if got := read[[]uint32](t, srv, sa.AttrSelectedAreas); !slices.Equal(got, []uint32{9, 10}) {
		t.Errorf("SelectedAreas %v", got)
	}
	if v, _ := srv.MatterRead(sa.AttrCurrentArea); v != nil {
		t.Errorf("CurrentArea %v, want null", v)
	}
	if v, _ := srv.MatterRead(sa.AttrEstimatedEndTime); v != nil {
		t.Errorf("EstimatedEndTime %v, want null", v)
	}
	if got := read[spec.List[servicearea.Progress]](t, srv, sa.AttrProgress); len(got) != 2 {
		t.Errorf("Progress %+v", got)
	}
	if srv.MatterDataVersion() == before || dv.Current() != srv.MatterDataVersion() {
		t.Error("a removal did not bump the host's data version")
	}

	srv.RemoveSupportedMapsEntry(0) // takes area 9, the first left on map 0
	if got := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas); len(got) != 1 || got[0].AreaId != 10 {
		t.Errorf("SupportedAreas %+v", got)
	}
	if got := read[spec.List[servicearea.Map]](t, srv, sa.AttrSupportedMaps); len(got) != 1 || got[0].MapId != 1 {
		t.Errorf("SupportedMaps %+v", got)
	}
	srv.RemoveSupportedMapsEntry(5) // unknown: nothing
	srv.RemoveSupportedMapsEntry(1)
	if got := read[spec.List[servicearea.Map]](t, srv, sa.AttrSupportedMaps); len(got) != 0 {
		t.Errorf("SupportedMaps %+v", got)
	}

	// A map without areas goes alone.
	cfg = vacuum(&device{})
	cfg.SupportedMaps = append(slices.Clone(cfg.SupportedMaps), servicearea.Map{MapId: 3, Name: "Garage"})
	srv = newServer(t, cfg)
	srv.RemoveSupportedMapsEntry(3)
	if got := read[spec.List[servicearea.Area]](t, srv, sa.AttrSupportedAreas); len(got) != 2 {
		t.Errorf("SupportedAreas %+v", got)
	}
}
