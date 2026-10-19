// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts
// at 862ad75f: the schema defaults, the startup reconciliation of setpoints
// and limits, the startup limit validation, the runtime limit validation
// and the AutoMode limit deadband.
//
// matter.js builds a MockServerNode per case (createAutoThermo, :25-44)
// and writes locally (`device.set({ thermostat: … })`); the port builds a
// fabrictest device serving the server and writes inside Device.Act: a
// setpoint or user limit with the server's MatterWrite — the validated
// write path, the asserts and the reconcile matter.js's $Changing reactors
// run — and an absolute limit with SetAbsoluteSetpointLimit, the device's
// own change of a read-only attribute. A startup matter.js fails with an
// ImplementationError is New's ErrSetpoints; an error matter.js matches by
// its message is matched by the Go error's text, which is matter.js's.
package thermo_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/node"
)

func i16(v int16) *int16 { return &v }

// autoThermo is ThermostatBehaviorTest.ts:18-44 createAutoThermo:
// Heating, Cooling and AutoMode, CoolingAndHeating (derived), SystemMode
// Auto (the AUTO default), the absolute limits 7–30 / 16–32 °C, the
// user limits USER_LIMITS (:18-23) unless userLimits is false, the
// occupied setpoints 20 / 26 °C, MinSetpointDeadBand 2.0 °C, then the
// case's overrides.
func autoThermo(userLimits bool, overrides ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	deadband := int8(20)
	cfg := thermo.ThermostatConfig{
		Features:                thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureAUTO,
		AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000,
		AbsMinCoolSetpointLimit: 1600, AbsMaxCoolSetpointLimit: 3200,
		InitialHeatingSetpoint: 2000, InitialCoolingSetpoint: 2600,
		MinSetpointDeadBand: &deadband,
	}
	if userLimits {
		cfg.MinHeatSetpointLimit, cfg.MaxHeatSetpointLimit = i16(700), i16(3000)
		cfg.MinCoolSetpointLimit, cfg.MaxCoolSetpointLimit = i16(1600), i16(3200)
	}
	for _, o := range overrides {
		o(&cfg)
	}
	return cfg
}

// thermoNode is the MockServerNode of a case: a fabrictest device serving
// the thermostat.
type thermoNode struct {
	dev *fabrictest.Device
	srv *thermo.ThermostatServer
}

func createThermo(t *testing.T, cfg thermo.ThermostatConfig) *thermoNode {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	srv := newServer(t, site, cfg)
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{thermostatEndpoint(srv)}})
	return &thermoNode{dev: dev, srv: srv}
}

// set is device.set({ thermostat: { attr: v } }) for a setpoint or a user
// limit.
func (n *thermoNode) set(t *testing.T, attr uint32, v int16) error {
	t.Helper()
	return act(t, n.dev, func(ctx context.Context) error { return n.srv.MatterWrite(ctx, attr, v) })
}

// setAbs is device.set for an absolute limit.
func (n *thermoNode) setAbs(t *testing.T, attr uint32, v int16) error {
	t.Helper()
	return act(t, n.dev, func(context.Context) error { return n.srv.SetAbsoluteSetpointLimit(attr, v) })
}

// want checks attributes of device.state.thermostat.
func (n *thermoNode) want(t *testing.T, want map[uint32]int16) {
	t.Helper()
	for attr, v := range want {
		if got, ok := n.srv.MatterRead(attr); !ok || got != v {
			t.Errorf("attribute 0x%04X = %v, want %d", attr, got, v)
		}
	}
}

// wantText checks that err's text contains text.
func wantText(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Errorf("error %v, want one containing %q", err, text)
	}
}

func TestParityMatterJS_ThermostatBehaviorTest(t *testing.T) {
	const (
		minHeat = tdef.AttrMinHeatSetpointLimit
		maxHeat = tdef.AttrMaxHeatSetpointLimit
		minCool = tdef.AttrMinCoolSetpointLimit
		maxCool = tdef.AttrMaxCoolSetpointLimit
		occHeat = tdef.AttrOccupiedHeatingSetpoint
		occCool = tdef.AttrOccupiedCoolingSetpoint
	)

	t.Run("has correct Thermostat-specific celsius defaults in schema", func(t *testing.T) {
		msd := paritytest.ClusterSnapshot(t, tdef.ClusterID).Attribute(t, "MinSetpointDeadBand")
		if want := map[string]any{"type": "celsius", "value": float64(2)}; !reflect.DeepEqual(msd.Default, want) {
			t.Errorf("MinSetpointDeadBand default %v, want %v", msd.Default, want)
		}
	})

	t.Run("correctly specifies Thermostat-specific value in defaults", func(t *testing.T) {
		srv, err := thermo.New(thermo.DefaultThermostatConfig())
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := srv.MatterRead(tdef.AttrMinSetpointDeadBand); v != int8(20) {
			t.Errorf("minSetpointDeadBand %v, want 20", v)
		}
	})

	t.Run("startup reconciliation", func(t *testing.T) {
		t.Run("pulls a heat limit back when the cool limit cannot keep the deadband, so setpoint writes succeed", func(t *testing.T) {
			zero := int8(0)
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.MaxHeatSetpointLimit, c.AbsMaxHeatSetpointLimit, c.MinSetpointDeadBand = i16(3500), 3500, &zero
			}))
			n.want(t, map[uint32]int16{maxCool: 3200, maxHeat: 3200})
			if err := n.set(t, occHeat, 2200); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{occHeat: 2200})
		})

		t.Run("raises the cool limit when it can keep the deadband within its absolute limits", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) { c.MinHeatSetpointLimit = i16(1500) }))
			n.want(t, map[uint32]int16{minHeat: 1500, minCool: 1700})
		})

		t.Run("moves the cooling setpoint when the setpoints violate the deadband", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 2100, 2200
			}))
			n.want(t, map[uint32]int16{occHeat: 2100, occCool: 2300})
		})

		t.Run("clamps a setpoint outside its limits and moves the cooling setpoint to keep the deadband", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) { c.InitialHeatingSetpoint = 3100 }))
			n.want(t, map[uint32]int16{occHeat: 3000, occCool: 3200})
		})

		t.Run("fails startup with the violated condition when the absolute limits leave no room for the deadband", func(t *testing.T) {
			_, err := thermo.New(autoThermo(false, func(c *thermo.ThermostatConfig) {
				c.AbsMinHeatSetpointLimit, c.AbsMaxCoolSetpointLimit = 2900, 3000
				c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 2900, 3000
			}))
			if !errors.Is(err, thermo.ErrSetpoints) {
				t.Fatalf("New: %v, want ErrSetpoints", err)
			}
			wantText(t, err, "cannot be fixed: maxCoolSetpointLimit (3000) - maxHeatSetpointLimit (3000) is below the deadband (200); after adjusting,")
		})

		t.Run("clears the active preset when it adjusts a setpoint", func(t *testing.T) {
			presetHandle := []byte{0x01}
			cfg := thermostatConfig(5, []thermo.Preset{newPreset(withHandle(presetHandle))})
			cfg.ActivePresetHandle = presetHandle
			cfg.InitialHeatingSetpoint = 3100
			n := createThermo(t, cfg)
			n.want(t, map[uint32]int16{occHeat: 2950})
			if h := n.srv.ActivePresetHandle(); h != nil {
				t.Errorf("activePresetHandle %x, want null", h)
			}
		})
	})

	t.Run("startup limit validation", func(t *testing.T) {
		t.Run("accepts absolute limits narrower than the defaults without user limits", func(t *testing.T) {
			n := createThermo(t, thermo.ThermostatConfig{
				Features:                thermo.ThermostatFeatureHEAT,
				AbsMinHeatSetpointLimit: 1000, AbsMaxHeatSetpointLimit: 2500,
				InitialHeatingSetpoint: 2000,
			})
			n.want(t, map[uint32]int16{minHeat: 1000, maxHeat: 2500})
		})

		t.Run("fails startup with an ImplementationError for unordered absolute limits", func(t *testing.T) {
			_, err := thermo.New(autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.AbsMinHeatSetpointLimit, c.AbsMaxHeatSetpointLimit = 2500, 2000
			}))
			if !errors.Is(err, thermo.ErrSetpoints) {
				t.Fatalf("New: %v, want ErrSetpoints", err)
			}
			wantText(t, err, "absMinHeatSetpointLimit (2500) is above absMaxHeatSetpointLimit (2000)")
		})

		t.Run("orders unordered user limits by raising the max limit", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.MinHeatSetpointLimit, c.MaxHeatSetpointLimit = i16(2000), i16(1500)
			}))
			n.want(t, map[uint32]int16{minHeat: 2000, maxHeat: 2000})
		})

		// The two cases without absolute limits take matter.js's defaults
		// (ThermostatServer.ts:810-822).
		t.Run("orders unordered cool user limits on a heating and cooling thermostat", func(t *testing.T) {
			n := createThermo(t, thermo.ThermostatConfig{
				Features:                thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL,
				AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000,
				AbsMinCoolSetpointLimit: 1600, AbsMaxCoolSetpointLimit: 3200,
				MinCoolSetpointLimit: i16(2500), MaxCoolSetpointLimit: i16(2000),
				InitialHeatingSetpoint: 2000, InitialCoolingSetpoint: 2500,
			})
			n.want(t, map[uint32]int16{minCool: 2500, maxCool: 2500})
		})

		t.Run("orders unordered cool user limits on a cooling-only thermostat", func(t *testing.T) {
			n := createThermo(t, thermo.ThermostatConfig{
				Features:                thermo.ThermostatFeatureCOOL,
				AbsMinCoolSetpointLimit: 1600, AbsMaxCoolSetpointLimit: 3200,
				MinCoolSetpointLimit: i16(2500), MaxCoolSetpointLimit: i16(2000),
				InitialCoolingSetpoint: 2500,
			})
			n.want(t, map[uint32]int16{minCool: 2500, maxCool: 2500})
		})

		t.Run("pulls a user limit inside narrowed absolute limits", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) { c.AbsMaxHeatSetpointLimit = 2800 }))
			n.want(t, map[uint32]int16{maxHeat: 2800})
		})
	})

	t.Run("limit validation", func(t *testing.T) {
		t.Run("rejects an absolute limit change that leaves a user limit outside", func(t *testing.T) {
			n := createThermo(t, autoThermo(true))
			wantText(t, n.setAbs(t, tdef.AttrAbsMinHeatSetpointLimit, 1000),
				"absMinHeatSetpointLimit (1000) must be less than or equal to minHeatSetpointLimit (700)")
			wantText(t, n.setAbs(t, tdef.AttrAbsMaxCoolSetpointLimit, 3000),
				"maxCoolSetpointLimit (3200) must be less than or equal to absMaxCoolSetpointLimit (3000)")
		})

		t.Run("rejects a user limit outside the absolute limits", func(t *testing.T) {
			n := createThermo(t, autoThermo(true))
			wantText(t, n.set(t, maxCool, 3300), "CoolSetpointLimit (3300) must be within absolute limits [1600, 3200]")
		})

		t.Run("rejects setpoints outside the user limits", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.MinHeatSetpointLimit, c.MaxHeatSetpointLimit, c.MaxCoolSetpointLimit = i16(1000), i16(2800), i16(3000)
			}))
			wantText(t, n.set(t, occHeat, 900), "HeatOccupiedSetpoint (900) must be greater than or equal to minHeatSetpointLimit (1000)")
			wantText(t, n.set(t, occCool, 3100), "CoolOccupiedSetpoint (3100) must be less than or equal to maxCoolSetpointLimit (3000)")
		})
	})

	t.Run("AutoMode limit deadband", func(t *testing.T) {
		t.Run("raises the coupled cool limit when a heat min limit write would violate the deadband", func(t *testing.T) {
			n := createThermo(t, autoThermo(true))
			// minCool - deadband = 1600 - 200 = 1400; write 10 above so it
			// conflicts.
			if err := n.set(t, minHeat, 1410); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{minHeat: 1410, minCool: 1610})
		})

		t.Run("lowers the coupled heat limit when a cool max limit write would violate the deadband", func(t *testing.T) {
			n := createThermo(t, autoThermo(true))
			// maxHeat + deadband = 3000 + 200 = 3200; write 10 below so it
			// conflicts.
			if err := n.set(t, maxCool, 3190); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{maxCool: 3190, maxHeat: 2990})
		})

		t.Run("does not adjust when the write already satisfies the deadband", func(t *testing.T) {
			n := createThermo(t, autoThermo(true))
			if err := n.set(t, minHeat, 1000); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{minHeat: 1000, minCool: 1600})
		})

		t.Run("raises the coupled max limit when a min limit is written above it (ordering repair)", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.MinHeatSetpointLimit, c.MaxHeatSetpointLimit = i16(700), i16(710)
				c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 700, 1600
			}))
			if err := n.set(t, minHeat, 1000); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{minHeat: 1000, maxHeat: 1000})
		})

		t.Run("clamps a setpoint back inside the limits when a limit write crosses it", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 1400, 1650
			}))
			// Raise minCool above the current cooling setpoint (1650).
			if err := n.set(t, minCool, 1700); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{minCool: 1700, occCool: 1700})
		})

		t.Run("pins the written limit back when the coupled limit cannot move within its absolute bounds", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.MaxCoolSetpointLimit, c.AbsMaxCoolSetpointLimit, c.MaxHeatSetpointLimit = i16(3000), 3000, i16(2800)
			}))
			// Requested maxHeat=2900 would need maxCool=3100 > absMaxCool
			// 3000; instead maxCool pins to 3000 and maxHeat is pulled back to
			// 2800 to preserve the deadband (CHIP FixUserLimitDeadband
			// fallback).
			if err := n.set(t, maxHeat, 2900); err != nil {
				t.Fatal(err)
			}
			n.want(t, map[uint32]int16{maxCool: 3000, maxHeat: 2800})
		})

		t.Run("rejects a limit write the fix leaves invalid, naming the violated condition", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.AbsMinHeatSetpointLimit, c.MinHeatSetpointLimit, c.AbsMaxHeatSetpointLimit, c.MinCoolSetpointLimit = 1500, i16(1500), 3200, i16(1700)
			}))
			wantText(t, n.set(t, minHeat, 3050), "could not be reconciled within the configured limits: Heat setpoint limits [3050, 3000]")
		})

		t.Run("nudges the coupled setpoint to preserve the deadband when a limit write clamps a setpoint", func(t *testing.T) {
			n := createThermo(t, autoThermo(true, func(c *thermo.ThermostatConfig) {
				c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 1410, 1610
			}))
			// Lower maxCool onto minCool; the cooling setpoint clamps to 1600
			// and heating is nudged down to keep the 200 deadband (CHIP
			// FixRange).
			if err := n.set(t, maxCool, 1600); err != nil {
				t.Fatal(err)
			}
			cool, _ := n.srv.MatterRead(occCool)
			heat, _ := n.srv.MatterRead(occHeat)
			n.want(t, map[uint32]int16{maxCool: 1600})
			if c, h := cool.(int16), heat.(int16); c > 1600 || c-h < 200 {
				t.Errorf("occupied setpoints heat %d, cool %d: want cool <= 1600 and a 200 deadband", h, c)
			}
		})
	})
}
