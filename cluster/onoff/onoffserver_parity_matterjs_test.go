// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/on-off/OnOffServerTest.ts
// at 862ad75f: an async observer of OnOff, the OnWithTimedOff countdowns
// (0xFFFF holds, the timed-on and delayed-off timers, writes during a
// countdown) and the OffOnly feature.
//
// matter.js runs the cases on a MockServerNode; the port runs them on a
// fabrictest device with the light on a bridged endpoint. A local call
// (`endpoint.act(agent => agent.get(LightingOnOff).onWithTimedOff(…))`) is
// the server's MatterInvoke inside Device.Act; a local state write
// (`agent.get(LightingOnOff).state.onTime = …`) is its MatterWrite, the one
// path the Go server offers for a write of OnTime or OffWaitTime
// (Server.Set changes OnOff only). MockTime.advance is the site's fake
// clock Advance, MockTime.resolve is Site.Eventually, and
// MockTime.timerCountFor("Timed on" / "Delayed off") is onoff.TimersRunning.
// The OffOnly case invokes over the wire from a commissioned pair
// (interaction.invoke) and asserts the IM status matter.js reads.
package onoff_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// holdTime is OnOffServerTest.ts:15 HOLD.
const holdTime = 0xFFFF

// skip marks an attribute [mockLight.want] does not check.
const skip = -1

// mockLight is setupLight (OnOffServerTest.ts:323-329): a node with an
// OnOffLight whose OnOff starts off, the case's initial values over it.
type mockLight struct {
	site *fabrictest.Site
	dev  *fabrictest.Device
	srv  *onoff.Server
}

func setupLight(t *testing.T, initial onoffdef.Patch) *mockLight {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	if initial.OnOff == nil {
		initial.OnOff = new(false)
	}
	l, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light"},
		onoffdef.Declare{Initial: initial})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{l}})
	return &mockLight{site: site, dev: dev, srv: l.OnOff().Underlying().(*onoff.Server)}
}

// act runs fn on the light inside Device.Act: matter.js endpoint.act.
func (m *mockLight) act(t *testing.T, fn func(ctx context.Context, srv *onoff.Server) error) {
	t.Helper()
	if err := m.dev.Act(func(*node.Node) error { return fn(context.Background(), m.srv) }); err != nil {
		t.Fatal(err)
	}
}

func (m *mockLight) invoke(t *testing.T, cmd uint32, fields any) {
	t.Helper()
	m.act(t, func(ctx context.Context, srv *onoff.Server) error {
		_, err := srv.MatterInvoke(ctx, cmd, fields)
		return err
	})
}

// onWithTimedOff is the local onWithTimedOff call with an empty
// OnOffControl.
func (m *mockLight) onWithTimedOff(t *testing.T, onTime, offWaitTime uint16) {
	t.Helper()
	m.invoke(t, onoff.CmdOnWithTimedOff, onoffdef.OnWithTimedOffRequest{OnTime: onTime, OffWaitTime: offWaitTime})
}

// write is the local state write of OnTime or OffWaitTime.
func (m *mockLight) write(t *testing.T, attr uint32, v uint16) {
	t.Helper()
	m.act(t, func(ctx context.Context, srv *onoff.Server) error { return srv.MatterWrite(ctx, attr, uint64(v)) })
}

// advance is n × MockTime.advance(d).
func (m *mockLight) advance(n int, d time.Duration) {
	for range n {
		m.site.Fake().Advance(d)
	}
}

// resolve is MockTime.resolve(promise, { stepMs: 10 }) for a promise that
// settles once cond holds.
func (m *mockLight) resolve(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.site.Eventually(ctx, what, cond); err != nil {
		t.Fatal(err)
	}
}

// want checks OnOff and, unless skip, OnTime and OffWaitTime.
func (m *mockLight) want(t *testing.T, onOff bool, onTime, offWaitTime int) {
	t.Helper()
	if got := m.srv.OnOff(); got != onOff {
		t.Errorf("onOff %t, want %t", got, onOff)
	}
	for _, c := range []struct {
		name string
		attr uint32
		want int
	}{{"onTime", onoff.AttrOnTime, onTime}, {"offWaitTime", onoff.AttrOffWaitTime, offWaitTime}} {
		if c.want == skip {
			continue
		}
		if got := u16(t, m.srv, c.attr); int(got) != c.want {
			t.Errorf("%s %d, want %d", c.name, got, c.want)
		}
	}
}

// wantTimers checks timerCountFor("Timed on") and ("Delayed off").
func (m *mockLight) wantTimers(t *testing.T, timedOn, delayedOff bool) {
	t.Helper()
	on, off := onoff.TimersRunning(m.srv)
	if on != timedOn || off != delayedOff {
		t.Errorf(`"Timed on" running %t, "Delayed off" running %t; want %t, %t`, on, off, timedOn, delayedOff)
	}
}

func u16(t *testing.T, srv *onoff.Server, attr uint32) uint16 {
	t.Helper()
	v, ok := srv.MatterRead(attr)
	u, isU16 := v.(uint16)
	if !ok || !isU16 {
		t.Fatalf("attribute 0x%04X: %v (%T)", attr, v, v)
	}
	return u
}

// myOnOffServer is OnOffServerTest.ts:19-28 MyOnOffServer, an extension
// overriding on() of the OffOnly base. The Go form of a host's on/off
// override is the Sink (Server's doc): the type satisfies it.
type myOnOffServer struct{}

func (myOnOffServer) SetOnOff(context.Context, bool) error { return nil }

var _ onoff.Sink = myOnOffServer{}

func TestParityMatterJS_OnOffServerTest(t *testing.T) {
	t.Run("accepts extensions of off-only commands", func(t *testing.T) {
		// A type-only test in matter.js; here the compile-time assertion
		// above and an OffOnly server built with the extension.
		if _, err := onoff.New(onoff.Config{Features: onoff.FeatureOFFONLY, Sink: myOnOffServer{}}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("properly supports async observers", testAsyncObservers)
	t.Run("OnWithTimedOff", testOnWithTimedOff)

	t.Run("OffOnly", func(t *testing.T) {
		t.Run("rejects On and Toggle with UNSUPPORTED_COMMAND while Off succeeds", testOffOnlyCommands)
	})
}

// testAsyncObservers: an observer of OnOff that finishes asynchronously
// sees both toggles, in order (OnOffServerTest.ts:30-52). The observer
// hands each value to a goroutine of its own; matter.js awaits each
// observer before the act returns, which the port mirrors by waiting for
// the observation before the next toggle.
func testAsyncObservers(t *testing.T) {
	// MockServerNode.createOnline's part 1 is an OnOffLight.
	m := setupLight(t, onoffdef.Patch{})
	var mu sync.Mutex
	var observed []bool
	var pending sync.WaitGroup
	unsubscribe := m.srv.OnMatterAttributesChanged(func(ids []uint32) {
		for _, id := range ids {
			if id != onoff.AttrOnOff {
				continue
			}
			v := m.srv.OnOff()
			pending.Go(func() {
				mu.Lock()
				defer mu.Unlock()
				observed = append(observed, v)
			})
		}
	})
	for range 2 {
		m.invoke(t, onoff.CmdToggle, nil)
		pending.Wait()
	}
	unsubscribe()
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 2 || !observed[0] || observed[1] {
		t.Fatalf("observed %v, want [true false]", observed)
	}
}

// testOffOnlyCommands is the OffOnly case (OnOffServerTest.ts:303-314).
// OffOnlyDevice (:317-321) is the vendor device type 0xFFF1_FC20 with
// OnOffServer.with("OffOnly"); a Go device type is 16 bits, so the
// endpoint advertises OnOffPlugInUnit — the device type is not what the
// case checks.
func testOffOnlyCommands(t *testing.T) {
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureOFFONLY})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("offonly:1", onoff.DeviceTypeOnOffPlugInUnit, srv)},
	}})
	ep, ok := p.Device.EndpointOf("offonly:1")
	if !ok {
		t.Fatal("no endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, c := range []struct {
		cmd  uint32
		want im.StatusCode
	}{
		{onoff.CmdOn, im.StatusUnsupportedCommand},
		{onoff.CmdToggle, im.StatusUnsupportedCommand},
		{onoff.CmdOff, im.StatusSuccess},
	} {
		_, err := fabrictest.Invoke(ctx, p, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: c.cmd}, nil)
		got := im.StatusSuccess
		var se *interaction.StatusError
		switch {
		case errors.As(err, &se):
			got = se.Status
		case err != nil:
			t.Fatalf("command 0x%02X: %v", c.cmd, err)
		}
		if got != c.want {
			t.Errorf("command 0x%02X: %v, want %v", c.cmd, got, c.want)
		}
	}
}

// testOnWithTimedOff is the "OnWithTimedOff" describe block
// (OnOffServerTest.ts:54-301).
func testOnWithTimedOff(t *testing.T) {
	t.Run("holds OnTime indefinitely when set to 0xFFFF", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		// OffWaitTime is non-zero so a naive timer-start gate would still
		// arm the timer.
		m.onWithTimedOff(t, holdTime, 10)
		// OnTime is held (not 0), so OffWaitTime is retained, not cleared.
		m.want(t, true, holdTime, 10)
		m.advance(3, 100*time.Millisecond)
		// OnTime is not decremented and the device stays on.
		m.want(t, true, holdTime, skip)
	})

	t.Run("holds OffWaitTime indefinitely when set to 0xFFFF", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 5, holdTime)
		m.invoke(t, onoff.CmdOff, nil)
		m.want(t, false, skip, holdTime)
		m.advance(3, 100*time.Millisecond)
		m.want(t, false, skip, holdTime)
	})

	t.Run("runs the delayed-off countdown after a held device is turned off", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, holdTime, 50)
		m.want(t, true, skip, 50)
		// An explicit off enters the delayed-off guard period and counts
		// OffWaitTime down.
		m.invoke(t, onoff.CmdOff, nil)
		m.want(t, false, skip, skip)
		m.resolve(t, "OffWaitTime 0", func() bool { return u16(t, m.srv, onoff.AttrOffWaitTime) == 0 })
		m.want(t, false, skip, 0)
	})

	t.Run("stops a running timed-on timer when raised to 0xFFFF (no leaked wakeup)", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 5, 10)
		m.wantTimers(t, true, false)
		// Raising OnTime to the hold value stops the timer.
		m.onWithTimedOff(t, holdTime, 10)
		m.want(t, true, holdTime, skip)
		m.wantTimers(t, false, false)
	})

	t.Run("stops the timed-on timer if OnTime is written to 0xFFFF while it runs", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 5, 10)
		m.wantTimers(t, true, false)
		m.write(t, onoff.AttrOnTime, holdTime)
		m.advance(1, 100*time.Millisecond)
		m.wantTimers(t, false, false)
		m.want(t, true, holdTime, skip)
	})

	t.Run("stops the delayed-off timer if OffWaitTime is written to 0xFFFF while it runs", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 5, 20)
		m.invoke(t, onoff.CmdOff, nil)
		m.wantTimers(t, false, true)
		m.write(t, onoff.AttrOffWaitTime, holdTime)
		m.advance(1, 100*time.Millisecond)
		m.wantTimers(t, false, false)
		m.want(t, false, skip, holdTime)
	})

	t.Run("decrements OnTime to auto-off when not 0xFFFF", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 3, 0)
		m.want(t, true, skip, skip)
		m.resolve(t, "off again", func() bool { return !m.srv.OnOff() })
		m.want(t, false, 0, skip)
	})

	t.Run("stays off and only lowers OffWaitTime when OffWaitTime>0 and off", func(t *testing.T) {
		// Off with OffWaitTime > 0 and no delayed-off timer running (as if
		// written directly).
		m := setupLight(t, onoffdef.Patch{OffWaitTime: new(uint16(100))})
		m.onWithTimedOff(t, 5, 50)
		// Spec: keep OFF, OffWaitTime the minimum of current and requested.
		m.want(t, false, skip, 50)
		// The delayed-off countdown runs and the device stays off.
		m.resolve(t, "OffWaitTime 0", func() bool { return u16(t, m.srv, onoff.AttrOffWaitTime) == 0 })
		m.want(t, false, skip, skip)
	})

	t.Run("does not start a countdown when OffWaitTime is written while plain off", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		// Spec §1.5.6.5: a write of OffWaitTime from a plain off state does
		// not start a countdown.
		m.write(t, onoff.AttrOffWaitTime, 30)
		m.wantTimers(t, false, false)
		m.advance(3, 100*time.Millisecond)
		m.want(t, false, skip, 30)
	})

	t.Run("stays on and stops the timer when OnTime is written to 0 during a countdown", func(t *testing.T) {
		m := setupLight(t, onoffdef.Patch{})
		m.onWithTimedOff(t, 5, 0)
		m.wantTimers(t, true, false)
		// Writing OnTime to 0 ends the timed-on phase but leaves the device
		// on (CHIP UpdateTimer-on-write).
		m.write(t, onoff.AttrOnTime, 0)
		m.wantTimers(t, false, false)
		m.advance(3, 100*time.Millisecond)
		m.want(t, true, 0, skip)
	})
}
