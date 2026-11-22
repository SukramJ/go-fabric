// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	matterfan "thingfabric.dev/go-fabric/cluster/fan"
	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	"thingfabric.dev/go-fabric/cluster/onoff"
	fandef "thingfabric.dev/go-fabric/cluster/spec/fancontrol"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	tempdef "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
)

// hostNotifier is a host's [contract.ChangeNotifier]: fire calls every
// registered callback.
type hostNotifier struct {
	mu   sync.Mutex
	next int
	cbs  map[int]func()
}

func (n *hostNotifier) OnMatterValueChanged(cb func()) func() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cbs == nil {
		n.cbs = map[int]func(){}
	}
	id := n.next
	n.next++
	n.cbs[id] = cb
	return func() { n.mu.Lock(); delete(n.cbs, id); n.mu.Unlock() }
}

func (n *hostNotifier) fire() {
	n.mu.Lock()
	cbs := make([]func(), 0, len(n.cbs))
	for _, cb := range n.cbs {
		cbs = append(cbs, cb)
	}
	n.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// notifyingFanHost is a host fan that notifies inside ApplyFanSettings,
// before the write returns; also, when set, runs after the fan's own
// notification.
type notifyingFanHost struct {
	fanDevice
	hostNotifier
	also func()
}

func (d *notifyingFanHost) ApplyFanSettings(ctx context.Context, p fandef.Patch) error {
	err := d.fanDevice.ApplyFanSettings(ctx, p)
	d.fire()
	if d.also != nil {
		d.also()
	}
	return err
}

// unchangedVerdictFan is a FanControl server whose verdict says every
// write changed nothing, whatever the host's fan did.
type unchangedVerdictFan struct{ *matterfan.Server }

func (s unchangedVerdictFan) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	_, err := s.Server.MatterWriteChanged(ctx, attrID, value)
	return false, err
}

func newNotifyingFan(t *testing.T, dev *notifyingFanHost) *matterfan.Server {
	t.Helper()
	srv, err := matterfan.NewServer(matterfan.Config{
		Source:   dev,
		Features: matterfan.FeatureMultiSpeed | matterfan.FeatureAuto | matterfan.FeatureStep,
		Sequence: matterfan.SequenceOffLowMedHighAuto, SpeedMax: 4,
		DeviceType: matterfan.DeviceTypeFan,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// notifyingLevelPort is a LevelControl port that notifies inside
// SetOnLevel.
type notifyingLevelPort struct {
	hostNotifier
	mu      sync.Mutex
	onLevel *uint8
}

func (p *notifyingLevelPort) CurrentLevel() (uint8, bool)   { return 100, true }
func (p *notifyingLevelPort) Options() lvldef.OptionsBitmap { return 0 }
func (p *notifyingLevelPort) OnLevel() (uint8, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onLevel == nil {
		return 0, false
	}
	return *p.onLevel, true
}

func (p *notifyingLevelPort) SetOptions(context.Context, lvldef.OptionsBitmap) error { return nil }
func (p *notifyingLevelPort) SetOnLevel(_ context.Context, l *uint8) error {
	p.mu.Lock()
	p.onLevel = l
	p.mu.Unlock()
	p.fire()
	return nil
}

func (p *notifyingLevelPort) MoveToLevel(context.Context, lvldef.MoveToLevelRequest) error {
	return nil
}

func (p *notifyingLevelPort) MoveToLevelWithOnOff(context.Context, lvldef.MoveToLevelWithOnOffRequest) error {
	return nil
}
func (p *notifyingLevelPort) Move(context.Context, lvldef.MoveRequest) error { return nil }
func (p *notifyingLevelPort) MoveWithOnOff(context.Context, lvldef.MoveWithOnOffRequest) error {
	return nil
}
func (p *notifyingLevelPort) Step(context.Context, lvldef.StepRequest) error { return nil }
func (p *notifyingLevelPort) StepWithOnOff(context.Context, lvldef.StepWithOnOffRequest) error {
	return nil
}
func (p *notifyingLevelPort) Stop(context.Context, lvldef.StopRequest) error { return nil }
func (p *notifyingLevelPort) StopWithOnOff(context.Context, lvldef.StopWithOnOffRequest) error {
	return nil
}

// hostTemperature is a host's TemperatureMeasurement server that notifies
// each change of its MeasuredValue.
type hostTemperature struct {
	*hostNotifier
	mu    sync.Mutex
	value int16
}

func (*hostTemperature) MatterClusterID() uint32 { return tempdef.ClusterID }
func (s *hostTemperature) MatterRead(attrID uint32) (any, bool) {
	if attrID != tempdef.AttrMeasuredValue {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, true
}

func (*hostTemperature) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*hostTemperature) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*hostTemperature) MatterReportable() []uint32                             { return []uint32{tempdef.AttrMeasuredValue} }

// move changes MeasuredValue and notifies, as a host does.
func (s *hostTemperature) move() {
	s.mu.Lock()
	s.value++
	s.mu.Unlock()
	s.fire()
}

// notifyingAppDevice is a bridged host device whose endpoint Source is a
// change notifier that is not a cluster server: its notifier covers every
// reportable path of the endpoint.
type notifyingAppDevice struct {
	appDevice
	*hostNotifier
}

func newNotifyingAppHarness(t *testing.T, d notifyingAppDevice) *appHarness {
	t.Helper()
	h := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		specs := []endpoint.Spec{{StableKey: endpoint.StringKey("dev-0"), DeviceType: d.deviceType, FriendlyName: "Device 0", Source: d}}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: specs}})
		}, nil
	})
	h.allowAll()
	ah := &appHarness{secureHarness: h, endpoints: map[uint16]uint16{}}
	for _, ep := range h.bridge.Topology().Bridged() {
		ah.endpoints[ep.DeviceType] = ep.ID
	}
	return ah
}

// clusterWatch follows one cluster of one endpoint: its DataVersion and
// the reports a subscription to the whole cluster gets.
type clusterWatch struct {
	t       *testing.T
	ep      *endpoint.Endpoint
	cluster uint32
	mgr     *subscription.Manager
	spy     *reachAttrReporterSpy
	now     time.Time
	version uint32
}

// watchCluster attaches a subscription manager to h's bridge with one
// subscription to the cluster of the endpoint.
func watchCluster(t *testing.T, h *appHarness, epID uint16, cluster uint32) *clusterWatch {
	t.Helper()
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 600,
		AttributePaths: []im.ConcreteAttributePath{{Endpoint: epID, Cluster: cluster, HasEndpoint: true, HasCluster: true}},
	}); err != nil {
		t.Fatal(err)
	}
	h.bridge.AttachSubscriptionManager(mgr)
	ep := h.bridge.Topology().FindByID(epID)
	if ep == nil {
		t.Fatalf("no endpoint %d", epID)
	}
	w := &clusterWatch{t: t, ep: ep, cluster: cluster, mgr: mgr, spy: spy, now: time.Now()}
	w.version = ep.ClusterDataVersion(cluster)
	return w
}

// step runs what and returns how far the cluster's DataVersion advanced
// and how many times attr was reported meanwhile.
func (w *clusterWatch) step(what func(), attr uint32) (advanced uint32, reports int) {
	w.t.Helper()
	what()
	w.now = w.now.Add(2 * time.Second)
	w.mgr.Tick(context.Background(), w.now)
	v := w.ep.ClusterDataVersion(w.cluster)
	advanced, w.version = v-w.version, v
	w.spy.mu.Lock()
	defer w.spy.mu.Unlock()
	for _, call := range w.spy.calls {
		for _, p := range call {
			if p.Endpoint == w.ep.ID && p.Cluster == w.cluster && p.Attribute == attr {
				reports++
			}
		}
	}
	w.spy.calls = nil
	return advanced, reports
}

// expect fails unless step(what, attr) advanced want versions and
// reported attr wantReports times.
func (w *clusterWatch) expect(name string, what func(), attr, want uint32, wantReports int) {
	w.t.Helper()
	advanced, reports := w.step(what, attr)
	if advanced != want || reports != wantReports {
		w.t.Errorf("%s: DataVersion advanced %d, attribute 0x%04X reported %d times; want %d and %d",
			name, advanced, attr, reports, want, wantReports)
	}
}

func uintValue(v uint64) func(enc *tlv.Encoder, tag tlv.Tag) {
	return func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, v) }
}

// TestControllerWriteAdvancesOnceBesideAHostNotifier: a controller's
// write that changed an attribute advances its cluster's DataVersion once
// and is reported once, whether the host's change notifier covering the
// attribute fires inside the write or after it, and a later notification
// that moved nothing advances nothing and reports nothing. matter.js
// writes each attribute data in a transaction of its own
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:426-435
// at db021da0, openForWrite then commit), and a commit advances the
// version once for whatever it changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0).
func TestControllerWriteAdvancesOnceBesideAHostNotifier(t *testing.T) {
	t.Parallel()
	write := func(h *appHarness, epID uint16, cluster, attr uint32, v uint64) func() {
		return func() {
			if st := h.writeAttribute(epID, cluster, attr, uintValue(v)); st != 0 {
				t.Fatalf("write 0x%04X/0x%04X: status 0x%02X", cluster, attr, st)
			}
		}
	}

	t.Run("LevelControl OnLevel, the port notifies inside SetOnLevel", func(t *testing.T) {
		t.Parallel()
		port := &notifyingLevelPort{}
		srv := levelcontrol.NewServer(levelcontrol.Config{Source: port})
		h := newAppHarness(t, appDevice{deviceType: 0x0101, servers: []contract.ClusterServer{srv}})
		epID := h.endpoints[0x0101]
		w := watchCluster(t, h, epID, lvldef.ClusterID)
		w.expect("the OnLevel write", write(h, epID, lvldef.ClusterID, lvldef.AttrOnLevel, 77), lvldef.AttrOnLevel, 1, 1)
		w.expect("a later notification that moved nothing", port.fire, lvldef.AttrOnLevel, 0, 0)
	})

	t.Run("FanControl PercentSetting, the host notifies inside ApplyFanSettings", func(t *testing.T) {
		t.Parallel()
		dev := &notifyingFanHost{}
		h := newAppHarness(t, appDevice{deviceType: matterfan.DeviceTypeFan, servers: []contract.ClusterServer{newNotifyingFan(t, dev)}})
		epID := h.endpoints[matterfan.DeviceTypeFan]
		w := watchCluster(t, h, epID, matterfan.ClusterID)
		w.expect("the PercentSetting write", write(h, epID, matterfan.ClusterID, matterfan.AttrPercentSetting, 50), matterfan.AttrPercentSetting, 1, 1)
		w.expect("a later notification that moved nothing", dev.fire, matterfan.AttrPercentSetting, 0, 0)
		// FanMode Auto moves PercentSetting (to null) with it: one commit, one
		// advance, and the attribute the host moved is reported.
		w.expect("the FanMode write, PercentSetting moved with it", write(h, epID, matterfan.ClusterID, matterfan.AttrFanMode, uint64(matterfan.FanModeAuto)), matterfan.AttrPercentSetting, 1, 1)
	})

	t.Run("a host change of another cluster inside the write advances that cluster once", func(t *testing.T) {
		t.Parallel()
		temp := &hostTemperature{hostNotifier: &hostNotifier{}}
		dev := &notifyingFanHost{also: temp.move}
		h := newAppHarness(t, appDevice{deviceType: matterfan.DeviceTypeFan, servers: []contract.ClusterServer{newNotifyingFan(t, dev), temp}})
		epID := h.endpoints[matterfan.DeviceTypeFan]
		w := watchCluster(t, h, epID, tempdef.ClusterID)
		fan := h.bridge.Topology().FindByID(epID)
		w.expect("a host change outside a write", temp.move, tempdef.AttrMeasuredValue, 1, 1)
		fanV := fan.ClusterDataVersion(matterfan.ClusterID)
		w.expect("the PercentSetting write, MeasuredValue moved by the host", write(h, epID, matterfan.ClusterID, matterfan.AttrPercentSetting, 50), tempdef.AttrMeasuredValue, 1, 1)
		if d := fan.ClusterDataVersion(matterfan.ClusterID) - fanV; d != 1 {
			t.Errorf("FanControl DataVersion advanced %d, want 1", d)
		}
	})

	t.Run("a verdict of unchanged leaves the host's notified change advanced and reported", func(t *testing.T) {
		t.Parallel()
		dev := &notifyingFanHost{}
		h := newAppHarness(t, appDevice{deviceType: matterfan.DeviceTypeFan, servers: []contract.ClusterServer{unchangedVerdictFan{newNotifyingFan(t, dev)}}})
		epID := h.endpoints[matterfan.DeviceTypeFan]
		w := watchCluster(t, h, epID, matterfan.ClusterID)
		w.expect("the PercentSetting write", write(h, epID, matterfan.ClusterID, matterfan.AttrPercentSetting, 50), matterfan.AttrPercentSetting, 1, 1)
		w.expect("a later notification that moved nothing", dev.fire, matterfan.AttrPercentSetting, 0, 0)
	})

	t.Run("OnOff OnTime, the endpoint source notifies after the write", func(t *testing.T) {
		t.Parallel()
		srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
		if err != nil {
			t.Fatal(err)
		}
		n := &hostNotifier{}
		h := newNotifyingAppHarness(t, notifyingAppDevice{appDevice{deviceType: 0x0100, servers: []contract.ClusterServer{srv}}, n})
		epID := h.endpoints[0x0100]
		w := watchCluster(t, h, epID, onoff.ClusterID)
		w.expect("a notification before any write", n.fire, onoff.AttrOnTime, 0, 0)
		w.expect("the OnTime write", write(h, epID, onoff.ClusterID, onoff.AttrOnTime, 50), onoff.AttrOnTime, 1, 1)
		w.expect("the next notification, which moved nothing", n.fire, onoff.AttrOnTime, 0, 0)
		w.expect("a second notification", n.fire, onoff.AttrOnTime, 0, 0)
	})
}

// TestWriteHoldScope: a write holds the notifications of the endpoints it
// names and of every endpoint when a path names none; its end returns what
// was held for the endpoints no write holds any longer and leaves another
// write's hold open.
func TestWriteHoldScope(t *testing.T) {
	t.Parallel()
	ep2, ep3 := &endpoint.Endpoint{ID: 2}, &endpoint.Endpoint{ID: 3}
	path := func(ep uint16) []im.ConcreteAttributePath {
		return []im.ConcreteAttributePath{{Endpoint: ep, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}}
	}
	var tbl writeHoldTable
	if got := tbl.holdPaths(ep2, nil, path(2)); len(got) != 1 {
		t.Fatal("a path was held with no write applied")
	}
	to2 := writeScopeOf(im.WriteRequest{Writes: []im.AttributeWrite{{Path: path(2)[0]}, {Path: path(2)[0]}}})
	to3 := writeScopeOf(im.WriteRequest{Writes: []im.AttributeWrite{{Path: path(3)[0]}}})
	tbl.begin(to2)
	tbl.begin(to3)
	if got := tbl.holdPaths(ep2, nil, path(2)); len(got) != 0 {
		t.Fatal("a path of an endpoint being written was not held")
	}
	if got := tbl.holdPaths(ep3, nil, path(3)); len(got) != 0 {
		t.Fatal("a path of the other endpoint being written was not held")
	}
	if held := tbl.end(to2, writeRecord{}); len(held) != 1 || held[2] == nil || len(held[2].paths) != 1 {
		t.Fatalf("the write to endpoint 2 ended with %+v, want its one held path", held)
	}
	if got := tbl.holdPaths(ep2, nil, path(2)); len(got) != 1 {
		t.Fatal("a path of an endpoint no longer written was held")
	}
	wild := writeScopeOf(im.WriteRequest{Writes: []im.AttributeWrite{{Path: im.ConcreteAttributePath{Cluster: 6, HasCluster: true}}}})
	tbl.begin(wild)
	if got := tbl.holdPaths(ep2, nil, path(2)); len(got) != 0 {
		t.Fatal("a write naming no endpoint did not hold endpoint 2")
	}
	if held := tbl.end(wild, writeRecord{}); len(held) != 1 || held[2] == nil {
		t.Fatalf("the wildcard write ended with %+v, want endpoint 2's alone: the write to endpoint 3 still holds it", held)
	}
	if held := tbl.end(to3, writeRecord{}); len(held) != 1 || held[3] == nil || len(held[3].paths) != 1 {
		t.Fatalf("the write to endpoint 3 ended with %+v, want its one held path", held)
	}
}

// TestOverlappingWritesSettleTheHoldOnce: two writes to one endpoint
// overlap, and a notification of the change the second one made arrives
// while both run. The first write's end hands out nothing; the second's
// settles the held change against both writes' records, so the cluster's
// DataVersion advances once — the dispatcher's advance for the write that
// changed it — and the held path is not marked again (review II,
// R2-P5-write-05).
func TestOverlappingWritesSettleTheHoldOnce(t *testing.T) {
	t.Parallel()
	ep := &endpoint.Endpoint{ID: 3}
	p := im.ConcreteAttributePath{Endpoint: 3, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnTime, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 600,
		AttributePaths: []im.ConcreteAttributePath{p},
	}); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{}
	first := writeScope{endpoints: []uint16{3}}
	second := writeScope{endpoints: []uint16{3}}
	b.writeHolds.begin(first)
	b.writeHolds.begin(second)
	if left := b.writeHolds.holdPaths(ep, mgr, []im.ConcreteAttributePath{p}); len(left) != 0 {
		t.Fatal("the notification was not held")
	}
	before := ep.ClusterDataVersion(onoff.ClusterID)
	// The first write changed nothing of the endpoint.
	b.settleWrite(first, recordOf([]im.WriteResult{{Path: p, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: []uint32{}}}}))
	// The second changed OnTime; its dispatcher advanced the cluster.
	ep.BumpClusterDataVersion(onoff.ClusterID)
	b.settleWrite(second, recordOf([]im.WriteResult{{Path: p, Status: im.StatusSuccess, Commit: &im.WriteCommit{Changed: []uint32{onoff.AttrOnTime}}}}))
	if got := ep.ClusterDataVersion(onoff.ClusterID) - before; got != 1 {
		t.Errorf("DataVersion advanced %d times, want once", got)
	}
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 0 {
		t.Errorf("the held change was marked again: %+v", spy.calls)
	}
}
