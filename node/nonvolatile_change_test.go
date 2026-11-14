// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/spec"
	fancontroldef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// nvHeat is the kit Thermostat with heating and no Sink: the module holds
// its setpoints (device/held.go).
var nvHeat = nvCase{key: "thermo", build: nvDevice(device.NewThermostat, thermostatdef.Declare{Features: thermostatdef.FeatureHeating}), cluster: thermostatdef.ClusterID, attr: thermostatdef.AttrOccupiedHeatingSetpoint}

// nvRaise invokes SetpointRaiseLower(Heat, +1 °C) on ep.
func nvRaise(t *testing.T, n *node.Node, ep uint16) {
	t.Helper()
	res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Invoke(t.Context(),
		im.ConcreteCommandPath{Endpoint: ep, Cluster: thermostatdef.ClusterID, Command: thermostatdef.CmdSetpointRaiseLower, HasEndpoint: true},
		thermostatdef.SetpointRaiseLowerRequest{Mode: thermostatdef.SetpointRaiseLowerModeHeat, Amount: 10})
	if res.Status != im.StatusSuccess {
		t.Fatalf("SetpointRaiseLower: %v", res.Status)
	}
}

// nvStoredTLV is v as the persister stores it.
func nvStoredTLV(t *testing.T, v any) string {
	t.Helper()
	b, err := bridge.AttributeValueTLV(v)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// TestNonVolatileChangeSurvivesARestart: a nonvolatile attribute the
// module holds that changes by a command, a host's Set or a companion move
// is served after a restart as it was served before it (R2-P2-dr-node-01).
func TestNonVolatileChangeSurvivesARestart(t *testing.T) {
	t.Parallel()
	heat := func(ep uint16) im.ConcreteAttributePath {
		return attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	}
	cool := func(ep uint16) im.ConcreteAttributePath {
		return attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedCoolingSetpoint)
	}
	auto := nvCase{key: "thermo", build: nvDevice(device.NewThermostat, thermostatdef.Declare{Features: thermostatdef.FeatureHeating | thermostatdef.FeatureCooling | thermostatdef.FeatureAutoMode})}
	cases := map[string]struct {
		tc     nvCase
		change func(t *testing.T, n *node.Node, ep uint16)
		paths  []func(uint16) im.ConcreteAttributePath
	}{
		"command": {tc: nvHeat, paths: []func(uint16) im.ConcreteAttributePath{heat}, change: func(t *testing.T, n *node.Node, ep uint16) {
			t.Helper()
			if st := dispatchWrite(t, n, heat(ep), int64(2100)); st != im.StatusSuccess {
				t.Fatalf("write: %v", st)
			}
			nvRaise(t, n, ep)
		}},
		"host set": {tc: nvHeat, paths: []func(uint16) im.ConcreteAttributePath{heat}, change: func(t *testing.T, n *node.Node, ep uint16) {
			t.Helper()
			if st := dispatchWrite(t, n, heat(ep), int64(2100)); st != im.StatusSuccess {
				t.Fatalf("write: %v", st)
			}
			srv := n.Bridge().Topology().FindByID(ep).Source.(*device.Device).Server(thermostatdef.ClusterID)
			if err := srv.(spec.AttributeSetter).SetAttributes(map[uint32]any{thermostatdef.AttrOccupiedHeatingSetpoint: int16(2250)}); err != nil {
				t.Fatal(err)
			}
		}},
		"companion": {tc: auto, paths: []func(uint16) im.ConcreteAttributePath{heat, cool}, change: func(t *testing.T, n *node.Node, ep uint16) {
			t.Helper()
			if st := dispatchWrite(t, n, heat(ep), int64(2300)); st != im.StatusSuccess {
				t.Fatalf("write heat: %v", st)
			}
			if st := dispatchWrite(t, n, cool(ep), int64(2400)); st != im.StatusSuccess {
				t.Fatalf("write cool: %v", st)
			}
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n, ep := nvRun(t, cfg, c.tc)
			c.change(t, n, ep)
			served := make([]string, len(c.paths))
			for i, p := range c.paths {
				served[i] = dispatchRead(t, n, p(ep))
			}
			if err := n.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			again, ep := nvRun(t, cfg, c.tc)
			for i, p := range c.paths {
				if got := dispatchRead(t, again, p(ep)); got != served[i] {
					t.Errorf("attribute 0x%04x after the restart %s, served %s before it", p(ep).Attribute, got, served[i])
				}
			}
		})
	}
}

// TestNonVolatileRejoinKeepsTheServedValue: a Device the host removes and
// adds again keeps what it served, also after a controller's write that a
// command then moved on (R2-P2-dr-node-01).
func TestNonVolatileRejoinKeepsTheServedValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	dev := nvHeat.build(t, nvHeat.key)
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, dev); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ep, _ := endpointOf(t, n, nvHeat.key)
	path := attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	if st := dispatchWrite(t, n, path, int64(2100)); st != im.StatusSuccess {
		t.Fatalf("write: %v", st)
	}
	nvRaise(t, n, ep)
	served := dispatchRead(t, n, path)
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey(nvHeat.key)); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ep, ok := endpointOf(t, n, nvHeat.key)
	if !ok {
		t.Fatal("the device is not served after the Add")
	}
	if got := dispatchRead(t, n, attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)); got != served {
		t.Errorf("after the rejoin %s, served %s before it", got, served)
	}
}

// TestNonVolatileForgottenDeviceIsReleased: a Device the host forgets
// (Aggregator.Forget) has left for good; the persister lets go of its
// servers, so a change it makes afterwards is not stored for the key.
func TestNonVolatileForgottenDeviceIsReleased(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	mem := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: mem, Endpoints: epmem.New()}
	n, ep := nvRun(t, cfg, nvHeat)
	path := attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	if st := dispatchWrite(t, n, path, int64(2100)); st != im.StatusSuccess {
		t.Fatalf("write: %v", st)
	}
	srv := n.Bridge().Topology().FindByID(ep).Source.(*device.Device).Server(thermostatdef.ClusterID)
	if err := n.Aggregator().Forget(ctx, endpoint.StringKey(nvHeat.key)); err != nil {
		t.Fatal(err)
	}
	if _, ok := endpointOf(t, n, nvHeat.key); ok {
		t.Fatal("the device is still served after Forget")
	}
	if err := srv.(spec.AttributeSetter).SetAttributes(map[uint32]any{thermostatdef.AttrOccupiedHeatingSetpoint: int16(2250)}); err != nil {
		t.Fatal(err)
	}
	key := "nonvolatile.thermo.0201.0012"
	if v, _, err := mem.GetSetting(ctx, key); err != nil || v != nvStoredTLV(t, int64(2100)) {
		t.Errorf("stored %q (%v) after the forgotten device's change, want 2100's TLV %q", v, err, nvStoredTLV(t, int64(2100)))
	}
}

// TestNonVolatileChangeOwedWhileTheStoreRefuses: a change the store
// refuses is written when the node stops, so the restart serves it
// (R2-P2-dr-node-01).
func TestNonVolatileChangeOwedWhileTheStoreRefuses(t *testing.T) {
	t.Parallel()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"nonvolatile."}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	n, ep := nvRun(t, cfg, nvHeat)
	path := attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	if s := dispatchWrite(t, n, path, int64(2100)); s != im.StatusSuccess {
		t.Fatalf("write: %v", s)
	}
	st.refuse.Store(true)
	nvRaise(t, n, ep)
	st.refuse.Store(false)
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := "nonvolatile.thermo.0201.0012"
	if v, ok, err := st.GetSetting(t.Context(), key); err != nil || !ok || v != nvStoredTLV(t, int64(2200)) {
		t.Errorf("stored %q (found %v, %v) after the stop, want 2200's TLV %q", v, ok, err, nvStoredTLV(t, int64(2200)))
	}
}

// TestNonVolatileHostRootServerIsTheHosts: a server the host mounts on the
// root (Config.RootServers) keeps its own state: a controller's write of
// its writable nonvolatile attribute is not stored, and the restart serves
// the host's Initial (R2-P2-dr-node-05, owner decision 38).
func TestNonVolatileHostRootServerIsTheHosts(t *testing.T) {
	t.Parallel()
	mem := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: mem, Endpoints: epmem.New()}
	hostFan := func(t *testing.T) contract.ClusterServer {
		t.Helper()
		srv, err := spec.NewServer(fancontroldef.Definition, spec.Options{}, spec.ServerConfig{Initial: fancontroldef.Patch{
			FanMode: new(fancontroldef.FanModeEnum), FanModeSequence: new(fancontroldef.FanModeSequenceEnum),
			PercentCurrent: new(uint8), Null: []uint32{fancontroldef.AttrPercentSetting},
		}.AttributeValues()})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	path := attrPath(0, fancontroldef.ClusterID, fancontroldef.AttrFanMode)
	c := cfg
	c.RootServers = []contract.ClusterServer{hostFan(t)}
	n, _ := nvRun(t, c, nvCase{})
	before := dispatchRead(t, n, path)
	if s := dispatchWrite(t, n, path, uint64(1)); s != im.StatusSuccess {
		t.Fatalf("write: %v", s)
	}
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := mem.GetSetting(t.Context(), "nonvolatile.@0.0202.0000"); err != nil || ok {
		t.Errorf("a key was stored for the host's root server: %q (found %v, %v)", v, ok, err)
	}
	c.RootServers = []contract.ClusterServer{hostFan(t)}
	again, _ := nvRun(t, c, nvCase{})
	if got := dispatchRead(t, again, path); got != before {
		t.Errorf("the host's FanMode after the restart %s, its Initial %s", got, before)
	}
}

// TestNonVolatileWriteNotHeldBehindAnotherAttributesVeto: a host's OnWrite
// veto that takes its time holds up only the writes of its own attribute,
// not a nonvolatile write of another endpoint (R2-P2-dr-node-04).
func TestNonVolatileWriteNotHeldBehindAnotherAttributesVeto(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	slow := nvDevice(device.NewDimmableLight, levelcontroldef.Declare{OnWrite: func(context.Context, levelcontroldef.State, uint32) error {
		close(entered)
		<-release
		return nil
	}})
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	n := newNode(t, cfg)
	ctx := t.Context()
	for key, build := range map[string]func(*testing.T, string) node.Endpoint{"slow": slow, "quick": nvDevice(device.NewDimmableLight)} {
		if err := n.Aggregator().Add(ctx, build(t, key)); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	slowEP, _ := endpointOf(t, n, "slow")
	quickEP, _ := endpointOf(t, n, "quick")
	write := func(ep uint16, level uint64, out chan<- im.StatusCode) {
		res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Write(ctx, attrPath(ep, levelcontroldef.ClusterID, levelcontroldef.AttrStartUpCurrentLevel), im.AttributeValue{Value: level})
		status := im.StatusFailure
		if len(res) == 1 {
			status = res[0].Status
		}
		out <- status
	}
	slowDone, quickDone := make(chan im.StatusCode, 1), make(chan im.StatusCode, 1)
	go write(slowEP, 10, slowDone)
	<-entered
	go write(quickEP, 20, quickDone)
	// The test's own deadline: the quick write takes no time of its own.
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case s := <-quickDone:
		if s != im.StatusSuccess {
			t.Errorf("the other endpoint's write: %v", s)
		}
	case <-wait.Done():
		t.Error("the other endpoint's write waited for the slow veto")
		close(release)
		<-quickDone
		<-slowDone
		return
	}
	close(release)
	if s := <-slowDone; s != im.StatusSuccess {
		t.Errorf("the vetoed write: %v", s)
	}
}

// TestNonVolatileWriteToAReplacedServerIsNotStored: a controller's write
// that reaches a Device's server after the host replaced the Device under
// the same key (a dispatcher of the topology before the replace) does not
// take the key from the new Device's server: the new server's later
// changes are stored, and the restart serves what it served
// (R2-P2-rr-ctl-01).
func TestNonVolatileWriteToAReplacedServerIsNotStored(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	n, ep := nvRun(t, cfg, nvHeat)
	old := endpoint.NewTopologyDispatcher(n.Bridge().Topology())
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey(nvHeat.key)); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, nvHeat.build(t, nvHeat.key)); err != nil {
		t.Fatal(err)
	}
	// The write a controller sent before the replace, answered by the old
	// Device's server on the topology it was resolved on.
	old.Write(ctx, attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint), im.AttributeValue{Value: int64(2100)})
	ep, ok := endpointOf(t, n, nvHeat.key)
	if !ok {
		t.Fatal("the new Device is not served")
	}
	srv := n.Bridge().Topology().FindByID(ep).Source.(*device.Device).Server(thermostatdef.ClusterID)
	if err := srv.(spec.AttributeSetter).SetAttributes(map[uint32]any{thermostatdef.AttrOccupiedHeatingSetpoint: int16(2250)}); err != nil {
		t.Fatal(err)
	}
	path := func(ep uint16) im.ConcreteAttributePath {
		return attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	}
	served := dispatchRead(t, n, path(ep))
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	again, ep := nvRun(t, cfg, nvHeat)
	if got := dispatchRead(t, again, path(ep)); got != served {
		t.Errorf("after the restart %s, served %s before it", got, served)
	}
}

// recordingSettings records the settings written under its prefix.
type recordingSettings struct {
	store.Port
	prefix string
	mu     sync.Mutex
	writes []string
}

func (s *recordingSettings) SetSetting(ctx context.Context, key, value string) error {
	if strings.HasPrefix(key, s.prefix) {
		s.mu.Lock()
		s.writes = append(s.writes, key+"="+value)
		s.mu.Unlock()
	}
	return s.Port.SetSetting(ctx, key, value)
}

// TestNonVolatileRestoreStoresNoValueNobodyChose: restoring a Thermostat
// whose stored cooling setpoint is below the Initial heating setpoint plus
// the deadband stores no heating setpoint but the one stored — a companion
// the restore moves on the way is not written over its own stored value
// (R2-P2-rr-ctl-04).
func TestNonVolatileRestoreStoresNoValueNobodyChose(t *testing.T) {
	t.Parallel()
	st := &recordingSettings{Port: memstore.New(), prefix: "nonvolatile."}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	auto := nvCase{key: "thermo", build: nvDevice(device.NewThermostat, thermostatdef.Declare{Features: thermostatdef.FeatureHeating | thermostatdef.FeatureCooling | thermostatdef.FeatureAutoMode})}
	heat := func(ep uint16) im.ConcreteAttributePath {
		return attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	}
	cool := func(ep uint16) im.ConcreteAttributePath {
		return attrPath(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedCoolingSetpoint)
	}
	n, ep := nvRun(t, cfg, auto)
	if s := dispatchWrite(t, n, heat(ep), int64(1600)); s != im.StatusSuccess {
		t.Fatalf("write heat: %v", s)
	}
	if s := dispatchWrite(t, n, cool(ep), int64(1900)); s != im.StatusSuccess {
		t.Fatalf("write cool: %v", s)
	}
	served := []string{dispatchRead(t, n, heat(ep)), dispatchRead(t, n, cool(ep))}
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.writes = nil
	st.mu.Unlock()
	again, ep := nvRun(t, cfg, auto)
	if got := []string{dispatchRead(t, again, heat(ep)), dispatchRead(t, again, cool(ep))}; !slices.Equal(got, served) {
		t.Errorf("heat, cool after the restart %v, served %v before it", got, served)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.writes) != 0 {
		t.Errorf("the restore wrote %v", st.writes)
	}
}
