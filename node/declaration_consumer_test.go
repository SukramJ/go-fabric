// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// The gate-1 consumer's bridge (notes/briefs/reports/G1-consumer/bridge.go.txt)
// on the declaration (brief BU §1, the acceptance test in spirit): a
// dimmable light whose level a simulated wall switch moves and a contact
// sensor, declared once, observed by origin — no Underlying, no
// WithServer, no Options.Clock, no copy of the host's own last value to
// tell its changes from a controller's.

// consumerEvent is one change a controller made to the light.
type consumerEvent struct {
	Attribute uint32
	On        bool
	Level     uint8
}

// consumerDevices is the host side of the bridge.
type consumerDevices struct {
	Light  *device.DimmableLight
	Sensor *device.ContactSensor

	mu      sync.Mutex
	level   uint8
	contact bool
}

func newConsumerDevices(onControl func(consumerEvent)) (*consumerDevices, error) {
	d := &consumerDevices{level: 1, contact: true}
	var err error
	d.Light, err = device.NewDimmableLight(
		device.Options{StableKey: endpoint.StringKey("light:1"), Name: "Kitchen light", Vendor: "G1"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}},
		levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(1))}},
	)
	if err != nil {
		return nil, err
	}
	d.Sensor, err = device.NewContactSensor(
		device.Options{StableKey: endpoint.StringKey("contact:1"), Name: "Front door", Vendor: "G1"},
		booleanstatedef.Declare{Initial: booleanstatedef.Patch{StateValue: new(true)}},
	)
	if err != nil {
		return nil, err
	}
	// Controller -> device: every change that is not the host's own.
	d.Light.OnOff().OnChanged(func(s onoffdef.State, c contract.Change) {
		if c.Origin != contract.OriginHostSet && slices.Contains(c.Attributes, onoffdef.AttrOnOff) {
			onControl(consumerEvent{Attribute: onoffdef.AttrOnOff, On: s.OnOff})
		}
	})
	d.Light.LevelControl().OnChanged(func(s levelcontroldef.State, c contract.Change) {
		if c.Origin != contract.OriginHostSet && slices.Contains(c.Attributes, levelcontroldef.AttrCurrentLevel) && s.CurrentLevel != nil {
			onControl(consumerEvent{Attribute: levelcontroldef.AttrCurrentLevel, Level: *s.CurrentLevel})
		}
	})
	return d, nil
}

func (d *consumerDevices) endpoints() []node.Endpoint { return []node.Endpoint{d.Light, d.Sensor} }

// tickLight is one wall-switch step.
func (d *consumerDevices) tickLight() error {
	d.mu.Lock()
	d.level = d.level%254 + 1
	lvl := d.level
	d.mu.Unlock()
	return d.Light.LevelControl().Set(levelcontroldef.Patch{CurrentLevel: &lvl})
}

// flipSensor toggles the contact.
func (d *consumerDevices) flipSensor() error {
	d.mu.Lock()
	d.contact = !d.contact
	v := d.contact
	d.mu.Unlock()
	return d.Sensor.BooleanState().Set(booleanstatedef.Patch{StateValue: &v})
}

// TestDeclarationConsumer is the consumer's own test over fabrictest: the
// declared state reaches the controller, the host's changes reach it
// without reaching the host's controller handler, and a controller's
// Toggle does.
func TestDeclarationConsumer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	var mu sync.Mutex
	var seen []consumerEvent
	devs, err := newConsumerDevices(func(ev consumerEvent) {
		mu.Lock()
		seen = append(seen, ev)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: devs.endpoints()}})
	lightEP, ok := devs.Light.EndpointNumber()
	if !ok {
		t.Fatal("the light has no endpoint number")
	}
	sensorEP, _ := devs.Sensor.EndpointNumber()

	read := func(ep uint16, cluster, attr uint32) (uint64, bool) {
		t.Helper()
		got, err := fabrictest.Read(ctx, pair, lifecycleAttr(ep, cluster, attr))
		if err != nil {
			t.Fatal(err)
		}
		return got.Value.El.Uint, got.Value.El.Bool
	}
	if _, on := read(lightEP, onoffdef.ClusterID, onoffdef.AttrOnOff); on {
		t.Error("OnOff on, declared off")
	}
	if lvl, _ := read(lightEP, levelcontroldef.ClusterID, levelcontroldef.AttrCurrentLevel); lvl != 1 {
		t.Errorf("CurrentLevel %d, declared 1", lvl)
	}
	if _, v := read(sensorEP, booleanstatedef.ClusterID, booleanstatedef.AttrStateValue); !v {
		t.Error("StateValue false, declared true")
	}

	if err := pair.Device.Act(func(*node.Node) error {
		if err := devs.tickLight(); err != nil {
			return err
		}
		return devs.flipSensor()
	}); err != nil {
		t.Fatal(err)
	}
	if lvl, _ := read(lightEP, levelcontroldef.ClusterID, levelcontroldef.AttrCurrentLevel); lvl != 2 {
		t.Errorf("CurrentLevel after the wall switch %d, want 2", lvl)
	}
	if _, v := read(sensorEP, booleanstatedef.ClusterID, booleanstatedef.AttrStateValue); v {
		t.Error("StateValue after the flip true, want false")
	}
	mu.Lock()
	if len(seen) != 0 {
		t.Errorf("the host's own changes reached the controller handler: %+v", seen)
	}
	mu.Unlock()

	toggle := im.ConcreteCommandPath{Endpoint: lightEP, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}
	if _, err := fabrictest.Invoke(ctx, pair, toggle, nil); err != nil {
		t.Fatal(err)
	}
	// Inline: the handler ran before the Toggle's response.
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0].Attribute != onoffdef.AttrOnOff || !seen[0].On {
		t.Errorf("after Toggle the handler saw %+v, want the light switched on", seen)
	}
}
