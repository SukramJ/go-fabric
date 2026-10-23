// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// heatingSink takes the thermostat's admitted setpoint changes.
type heatingSink struct {
	mu       sync.Mutex
	setpoint int16
}

func (h *heatingSink) SetSystemMode(context.Context, uint8) error { return nil }

func (h *heatingSink) SetSetpoint(_ context.Context, _ uint32, value int16, _ []thermostatdef.SetpointChange) error {
	h.mu.Lock()
	h.setpoint = value
	h.mu.Unlock()
	return nil
}

// TestTypedReadsConsumer is the gate-2 consumer's controller program
// (notes/briefs/reports/G2-consumer/controller_test.go.txt) on the typed
// surface: it commissions a bridge with a dimmable light and a
// thermostat, subscribes to the light through the role's own types, reads
// the image back as each cluster's generated State, writes the setpoint
// with a spec.Value and reads it back typed — no tlv, im or
// client/interaction import, no widened integer.
func TestTypedReadsConsumer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewDimmableLight(device.Options{StableKey: device.StringKey("light"), Name: "Hall light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(true)}},
		levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(10))}})
	if err != nil {
		t.Fatal(err)
	}
	heat := &heatingSink{setpoint: 2000}
	thermo, err := device.NewThermostat(device.Options{StableKey: device.StringKey("thermo"), Name: "Living room"},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating,
			Initial: thermostatdef.Patch{
				LocalTemperature:        new(int16(1950)),
				OccupiedHeatingSetpoint: new(int16(2000)),
			},
			Sink: heat,
		})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light, thermo}})
	ctl := site.AddController(fabrictest.ControllerOptions{})
	cn, err := ctl.CommissionNode(ctx, dev, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lightEP, _ := light.EndpointNumber()
	thermoEP, _ := thermo.EndpointNumber()

	var mu sync.Mutex
	reports := 0
	var sub *client.Subscription
	err = site.Run(ctx, func(ctx context.Context) (err error) {
		sub, err = cn.Subscribe(ctx, client.SubscribeRequest{
			Attributes: []client.AttributePath{
				client.ClusterAt(lightEP, onoffdef.ClusterID),
				client.ClusterAt(lightEP, levelcontroldef.ClusterID),
			},
			Updated: func(*client.Report) {
				mu.Lock()
				reports++
				mu.Unlock()
			},
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	ep := cn.Endpoint(lightEP)
	if ep == nil {
		t.Fatal("the image has no light endpoint")
	}
	onOff := func() onoffdef.State {
		st, _ := onoffdef.StateFrom(ep.Cluster(onoffdef.ClusterID))
		return st
	}
	level := func() levelcontroldef.State {
		st, _ := levelcontroldef.StateFrom(ep.Cluster(levelcontroldef.ClusterID))
		return st
	}
	if st, absent := onoffdef.StateFrom(ep.Cluster(onoffdef.ClusterID)); !st.OnOff || len(absent) == len(onoffdef.Definition.Attributes) {
		t.Fatalf("primed OnOff %+v, absent %v", st, absent)
	}
	if l := level().CurrentLevel; l == nil || *l != 10 {
		t.Fatalf("primed CurrentLevel %v, want 10", l)
	}

	// The host switches the light off and dims it: both reach the image,
	// in the generated types.
	if err := dev.Act(func(*node.Node) error {
		return errors.Join(light.OnOff().Set(onoffdef.Patch{OnOff: new(false)}),
			light.LevelControl().Set(levelcontroldef.Patch{CurrentLevel: new(uint8(20))}))
	}); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the OnOff and level reports", func() bool {
		l := level().CurrentLevel
		return !onOff().OnOff && l != nil && *l == 20
	}); err != nil {
		t.Fatalf("%v: OnOff %+v, level %+v", err, onOff(), level())
	}
	mu.Lock()
	if reports == 0 {
		t.Error("no report reached Updated")
	}
	mu.Unlock()
	// The device's own typed state and the controller's agree.
	if got, want := level().CurrentLevel, light.LevelControl().Get().CurrentLevel; got == nil || want == nil || *got != *want {
		t.Errorf("CurrentLevel: controller %v, device %v", got, want)
	}

	// Write the setpoint with a scalar value, read it back typed — under
	// Site.Run, so the site's fake clock moves while the interaction waits.
	var rep *client.Report
	if err := site.Run(ctx, func(ctx context.Context) error {
		res, err := cn.Write(ctx, []client.WriteRequest{{
			Path:  client.AttributeAt(thermoEP, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint),
			Value: spec.Int16(2250),
		}}, client.WriteOptions{})
		if err != nil {
			return err
		}
		if err := res.Err(); err != nil {
			return err
		}
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{client.ClusterAt(thermoEP, thermostatdef.ClusterID)}
		rep, err = cn.Read(ctx, req)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fromReport, _ := thermostatdef.StateFrom(rep.Values(thermoEP, thermostatdef.ClusterID))
	fromImage, _ := thermostatdef.StateFrom(cn.Endpoint(thermoEP).Cluster(thermostatdef.ClusterID))
	if fromReport.OccupiedHeatingSetpoint != 2250 || fromImage.OccupiedHeatingSetpoint != 2250 {
		t.Errorf("OccupiedHeatingSetpoint: report %d, image %d, want 2250", fromReport.OccupiedHeatingSetpoint, fromImage.OccupiedHeatingSetpoint)
	}
	if lt := fromImage.LocalTemperature; lt == nil || *lt != 1950 {
		t.Errorf("LocalTemperature %v, want 1950", lt)
	}
	heat.mu.Lock()
	if heat.setpoint != 2250 {
		t.Errorf("the sink holds %d, want 2250", heat.setpoint)
	}
	heat.mu.Unlock()

	// An event path is the role's own type too.
	if ev := client.EventAt(lightEP, onoffdef.ClusterID, 0); !ev.HasEndpoint || !ev.HasCluster || !ev.HasEvent || ev.Endpoint != lightEP {
		t.Errorf("EventAt %+v", ev)
	}

	// A cluster the endpoint does not have: every attribute absent.
	if _, absent := onoffdef.StateFrom(cn.Endpoint(thermoEP).Cluster(onoffdef.ClusterID)); len(absent) == 0 {
		t.Error("OnOff on the thermostat: nothing absent")
	}
	// Let the site go quiet: the last reports' acknowledgements run on
	// its fake clock, and a teardown with one in flight waits it out.
	sub.Close()
	site.Fake().Advance(time.Second)
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
}
