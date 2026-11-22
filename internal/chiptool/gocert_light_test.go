// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
	"thingfabric.dev/go-fabric/cluster/spec/identify"
	"thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/controller/client"
)

// The light cases: matter.js's client cases of ColorControl (TC-CC-3.4,
// 4.5, 5.4, 6.4, 7.5, 9.4), Identify (TC-I-3.2) and OnOff (TC-OO-3.2),
// read mirrored onto the reference daemon's light: the Go TH sends the
// commands the TS's DUT sends and reads what it reads, and the daemon
// answers as the TS's TH device does.
//
// These ports were read at [goCertReadAt], the snapshot pin b25ed960:
// every TS line cited below is that commit's.
//
// What the TS checks on the TH's side — its log showing the command's
// exact payload (tc-support.ts sendCommand, expectCommandPayload) or the
// read request (tc-cc-support.ts readColorAttribute) — is, read mirrored,
// the daemon's acceptance: the Go TH built the payload, and the daemon's
// Success (or its value) is the evidence that it received it. The daemon's
// log is not matched.

func init() {
	for _, c := range []func() goCertCase{caseCC34, caseCC45, caseCC54, caseCC64, caseCC75, caseCC94, caseI32, caseOO32} {
		registerGoCert(c)
	}
}

// ccEndpoint is a ColorControl case's endpoint: the first light that
// serves ColorControl with the case's features (its mirrored feature
// PICS) and OnOff, as both THs put ColorControl on the light at endpoint
// 1 (tc-cc-support.ts:35-36), the precondition requires the features
// there (:145-160) and switches that light's OnOff on (:162-167).
func ccEndpoint(features ...string) string {
	return "CC.S & OO.S & " + strings.Join(features, " & ")
}

// ccFeature is a ColorControl feature as tc-cc-support.ts:39 names it,
// with its FeatureMap bit (cluster/spec/colorcontrol definition_gen.go).
type ccFeature struct {
	name string
	bit  colorcontrol.Feature
}

var (
	ccHueSaturation    = ccFeature{"hueSaturation", colorcontrol.FeatureHueSaturation}
	ccEnhancedHue      = ccFeature{"enhancedHue", colorcontrol.FeatureEnhancedHue}
	ccColorLoop        = ccFeature{"colorLoop", colorcontrol.FeatureColorLoop}
	ccXY               = ccFeature{"xy", colorcontrol.FeatureXy}
	ccColorTemperature = ccFeature{"colorTemperature", colorcontrol.FeatureColorTemperature}
)

// ccAttr is a ColorControl attribute a step reads, by the TS's name.
type ccAttr struct {
	name string
	id   uint32
}

var (
	ccCurrentHue                 = ccAttr{"currentHue", colorcontrol.AttrCurrentHue}
	ccCurrentSaturation          = ccAttr{"currentSaturation", colorcontrol.AttrCurrentSaturation}
	ccColorTemperatureMireds     = ccAttr{"colorTemperatureMireds", colorcontrol.AttrColorTemperatureMireds}
	ccEnhancedCurrentHue         = ccAttr{"enhancedCurrentHue", colorcontrol.AttrEnhancedCurrentHue}
	ccColorLoopActive            = ccAttr{"colorLoopActive", colorcontrol.AttrColorLoopActive}
	ccColorLoopDirection         = ccAttr{"colorLoopDirection", colorcontrol.AttrColorLoopDirection}
	ccColorLoopTime              = ccAttr{"colorLoopTime", colorcontrol.AttrColorLoopTime}
	ccColorLoopStartEnhancedHue  = ccAttr{"colorLoopStartEnhancedHue", colorcontrol.AttrColorLoopStartEnhancedHue}
	ccColorTempPhysicalMinMireds = ccAttr{"colorTempPhysicalMinMireds", colorcontrol.AttrColorTempPhysicalMinMireds}
	ccColorTempPhysicalMaxMireds = ccAttr{"colorTempPhysicalMaxMireds", colorcontrol.AttrColorTempPhysicalMaxMireds}
)

// ccClient is a ColorControl client case's commissioned node.
type ccClient struct{ n *client.Node }

// setup mirrors colorControlPreconditions (tc-cc-support.ts:123-169): the
// DUT commissions the TH, reads its ColorControl FeatureMap, which must
// set every feature the case uses, and sends OnOff On to the light.
func (c *ccClient) setup(features ...ccFeature) func(th *goTH) error {
	return func(th *goTH) error {
		var err error
		if _, c.n, err = th.commission("dut"); err != nil {
			return err
		}
		fm, err := th.readValue(c.n, attrPath(th.ep, colorcontrol.ClusterID, 0xFFFC))
		if err != nil {
			return err
		}
		if fm.El.IsNull || !isUnsigned(fm) {
			return fmt.Errorf("FeatureMap is %s, which names no features", describeNode(fm))
		}
		var missing []string
		for _, f := range features {
			if fm.El.Uint&uint64(f.bit) == 0 {
				missing = append(missing, f.name)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("FeatureMap=%s lacks %s", describeNode(fm), strings.Join(missing, ", "))
		}
		if _, err := th.invoke(c.n, cmdPath(th.ep, onoff.ClusterID, onoff.CmdOn), onoff.OnRequest{}, false, client.InvokeOptions{}); err != nil {
			return err
		}
		th.note("FeatureMap=%s on endpoint %d; On: Success", describeNode(fm), th.ep)
		return nil
	}
}

// send mirrors sendsColorCommand (tc-cc-support.ts:172-186): the command
// with the TS's arguments, the options bitmaps left with no bit set
// (:41-45), answered with Success. fields is built when the step runs, so
// a step may take its arguments from an earlier step's reads.
func (c *ccClient) send(name string, cmd uint32, fields func() (spec.Encodable, error)) func(th *goTH) error {
	return func(th *goTH) error {
		f, err := fields()
		if err != nil {
			return err
		}
		if _, err := th.invoke(c.n, cmdPath(th.ep, colorcontrol.ClusterID, cmd), f, false, client.InvokeOptions{}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		th.note("%s %+v: Success", name, f)
		return nil
	}
}

// fixed is a command's arguments as the TS states them.
func fixed(f spec.Encodable) func() (spec.Encodable, error) {
	return func() (spec.Encodable, error) { return f, nil }
}

// read mirrors readColorAttribute (tc-cc-support.ts:189-209): one read of
// the attribute, which must answer with a value.
func (c *ccClient) read(th *goTH, a ccAttr) (spec.Node, error) {
	v, err := th.readValue(c.n, attrPath(th.ep, colorcontrol.ClusterID, a.id))
	if err != nil {
		return v, fmt.Errorf("the read of %s: %w", a.name, err)
	}
	return v, nil
}

// reads mirrors readsColorAttributes (tc-cc-support.ts:212-223): each
// attribute read once, in order.
func (c *ccClient) reads(attrs ...ccAttr) func(th *goTH) error {
	return func(th *goTH) error {
		for _, a := range attrs {
			v, err := c.read(th, a)
			if err != nil {
				return err
			}
			th.note("%s=%s", a.name, describeNode(v))
		}
		return nil
	}
}

// stopMoveStep is the StopMoveStep step every ColorControl case ends with.
func (c *ccClient) stopMoveStep() func(th *goTH) error {
	return c.send("StopMoveStep", colorcontrol.CmdStopMoveStep, fixed(colorcontrol.StopMoveStepRequest{}))
}

// caseCC34 mirrors TC-CC-3.4: the HueSaturation hue commands.
func caseCC34() goCertCase {
	c := &ccClient{}
	return goCertCase{
		tc: "TC-CC-3.4", line: 13, pics: []string{"CC.C", "CC.C.F00"}, at: ccEndpoint("CC.S.F00"),
		steps: []goStep{
			{n: "0", line: 14, title: "commission, TH OnOff On, the TH supports HueSaturation", run: c.setup(ccHueSaturation)},
			{n: "1a", line: 23, title: "MoveToHue", pics: "CC.C.C00.Tx", run: c.send("MoveToHue", colorcontrol.CmdMoveToHue,
				fixed(colorcontrol.MoveToHueRequest{Hue: 100, Direction: 0, TransitionTime: 0}))}, // :26
			{n: "1b", line: 33, title: "read CurrentHue", pics: "CC.C.C00.Tx & CC.C.A0000", run: c.reads(ccCurrentHue)},
			{n: "2a", line: 42, title: "MoveHue", pics: "CC.C.C01.Tx", run: c.send("MoveHue", colorcontrol.CmdMoveHue,
				fixed(colorcontrol.MoveHueRequest{MoveMode: 1, Rate: 10}))}, // :45
			{n: "2b", line: 52, title: "read CurrentHue", pics: "CC.C.C01.Tx & CC.C.A0000", run: c.reads(ccCurrentHue)},
			{n: "3a", line: 61, title: "StepHue", pics: "CC.C.C02.Tx", run: c.send("StepHue", colorcontrol.CmdStepHue,
				fixed(colorcontrol.StepHueRequest{StepMode: 1, StepSize: 20, TransitionTime: 0}))}, // :64
			{n: "3b", line: 71, title: "read CurrentHue", pics: "CC.C.C02.Tx & CC.C.A0000", run: c.reads(ccCurrentHue)},
			{n: "4a", line: 80, title: "StopMoveStep", pics: "CC.C.C47.Tx", run: c.stopMoveStep()},
			{n: "4b", line: 85, title: "read CurrentHue", pics: "CC.C.C47.Tx & CC.C.A0000", run: c.reads(ccCurrentHue)},
		},
	}
}

// caseCC45 mirrors TC-CC-4.5: the HueSaturation saturation commands.
func caseCC45() goCertCase {
	c := &ccClient{}
	return goCertCase{
		tc: "TC-CC-4.5", line: 13, pics: []string{"CC.C", "CC.C.F00"}, at: ccEndpoint("CC.S.F00"),
		steps: []goStep{
			{n: "0", line: 14, title: "commission, TH OnOff On, the TH supports HueSaturation", run: c.setup(ccHueSaturation)},
			{n: "1a", line: 23, title: "MoveToSaturation", pics: "CC.C.C03.Tx", run: c.send("MoveToSaturation", colorcontrol.CmdMoveToSaturation,
				fixed(colorcontrol.MoveToSaturationRequest{Saturation: 150, TransitionTime: 0}))}, // :26
			{n: "1b", line: 33, title: "read CurrentSaturation", pics: "CC.C.C03.Tx & CC.C.A0001", run: c.reads(ccCurrentSaturation)},
			{n: "2a", line: 42, title: "MoveSaturation", pics: "CC.C.C04.Tx", run: c.send("MoveSaturation", colorcontrol.CmdMoveSaturation,
				fixed(colorcontrol.MoveSaturationRequest{MoveMode: 1, Rate: 10}))}, // :45
			{n: "2b", line: 52, title: "read CurrentSaturation", pics: "CC.C.C04.Tx & CC.C.A0001", run: c.reads(ccCurrentSaturation)},
			{n: "3a", line: 61, title: "StepSaturation", pics: "CC.C.C05.Tx", run: c.send("StepSaturation", colorcontrol.CmdStepSaturation,
				fixed(colorcontrol.StepSaturationRequest{StepMode: 1, StepSize: 20, TransitionTime: 0}))}, // :64
			{n: "3b", line: 71, title: "read CurrentSaturation", pics: "CC.C.C05.Tx & CC.C.A0001", run: c.reads(ccCurrentSaturation)},
			{n: "4a", line: 80, title: "MoveToHueAndSaturation", pics: "CC.C.C06.Tx", run: c.send("MoveToHueAndSaturation", colorcontrol.CmdMoveToHueAndSaturation,
				fixed(colorcontrol.MoveToHueAndSaturationRequest{Hue: 50, Saturation: 100, TransitionTime: 0}))}, // :83
			{
				n: "4b", line: 90, title: "read CurrentHue and CurrentSaturation", pics: "CC.C.C06.Tx & CC.C.A0000 & CC.C.A0001",
				run: c.reads(ccCurrentHue, ccCurrentSaturation),
			},
			{n: "5a", line: 99, title: "StopMoveStep", pics: "CC.C.C47.Tx", run: c.stopMoveStep()},
			{n: "5b", line: 104, title: "read CurrentSaturation", pics: "CC.C.C47.Tx & CC.C.A0001", run: c.reads(ccCurrentSaturation)},
		},
	}
}

// caseCC54 mirrors TC-CC-5.4: the XY commands.
func caseCC54() goCertCase {
	c := &ccClient{}
	return goCertCase{
		tc: "TC-CC-5.4", line: 13, pics: []string{"CC.C", "CC.C.F03"}, at: ccEndpoint("CC.S.F03"),
		steps: []goStep{
			{n: "0", line: 14, title: "commission, TH OnOff On, the TH supports XY", run: c.setup(ccXY)},
			{n: "1", line: 23, title: "MoveToColor", pics: "CC.C.C07.Tx", run: c.send("MoveToColor", colorcontrol.CmdMoveToColor,
				fixed(colorcontrol.MoveToColorRequest{ColorX: 20000, ColorY: 20000, TransitionTime: 0}))}, // :26
			{n: "2", line: 33, title: "MoveColor", pics: "CC.C.C08.Tx", run: c.send("MoveColor", colorcontrol.CmdMoveColor,
				fixed(colorcontrol.MoveColorRequest{RateX: -100, RateY: 100}))}, // :36
			{n: "3", line: 43, title: "StepColor", pics: "CC.C.C09.Tx", run: c.send("StepColor", colorcontrol.CmdStepColor,
				fixed(colorcontrol.StepColorRequest{StepX: 1000, StepY: -1000, TransitionTime: 0}))}, // :46
			{n: "4", line: 53, title: "StopMoveStep", pics: "CC.C.C47.Tx", run: c.stopMoveStep()},
		},
	}
}

// caseCC64 mirrors TC-CC-6.4: the ColorTemperature commands, within the
// physical range step 1 reads. It runs on the daemon's extended color
// light, not on the first light with ColorTemperature (the CT-only one):
// the TS's TH light is the one at COLOR_CONTROL_ENDPOINT
// (tc-cc-support.ts:35-36), whose ColorControl declares every feature —
// HueSaturation, EnhancedHue, ColorLoop, XY and ColorTemperature in
// matter.js's all-clusters TH (support/chip-testing/src/AllClustersTestInstance.ts:467-473
// at b25ed960), FeatureMap 0x1F in chip's (all-clusters-app.matter:8886 at
// 6170af84) — the light every other CC port lands on.
func caseCC64() goCertCase {
	c := &ccClient{}
	const (
		miredsMin = 1      // MIREDS_MIN (TC-CC-6.4.test.ts:19)
		miredsMax = 0xfeff // MIREDS_MAX (:20)
	)
	var lo, hi uint16
	have := false
	// rangeOf is range() (:25-30): the steps after 1 need its range.
	rangeOf := func() (uint16, uint16, error) {
		if !have {
			return 0, 0, errors.New("step 1 recorded no colour temperature range of the TH to move within")
		}
		return lo, hi, nil
	}
	return goCertCase{
		tc: "TC-CC-6.4", line: 37, pics: []string{"CC.C", "CC.C.F04"}, at: ccEndpoint("CC.S.F00", "CC.S.F01", "CC.S.F02", "CC.S.F03", "CC.S.F04"),
		steps: []goStep{
			{n: "0", line: 38, title: "commission, TH OnOff On, the TH supports ColorTemperature", run: c.setup(ccColorTemperature)},
			{n: "1", line: 48, title: "the TH's physical colour temperature range is reasonable", run: func(th *goTH) error {
				minV, err := c.read(th, ccColorTempPhysicalMinMireds)
				if err != nil {
					return err
				}
				maxV, err := c.read(th, ccColorTempPhysicalMaxMireds)
				if err != nil {
					return err
				}
				// reasonable (:55-60).
				num := func(v spec.Node) bool { return !v.El.IsNull && isUnsigned(v) }
				if !num(minV) || !num(maxV) || minV.El.Uint < miredsMin || minV.El.Uint >= maxV.El.Uint || maxV.El.Uint > miredsMax {
					return fmt.Errorf("ColorTempPhysicalMinMireds=%s, ColorTempPhysicalMaxMireds=%s; a range within %d-%d expected",
						describeNode(minV), describeNode(maxV), miredsMin, miredsMax)
				}
				lo, hi, have = uint16(minV.El.Uint), uint16(maxV.El.Uint), true //nolint:gosec // within 1-0xfeff, checked above
				th.note("ColorTempPhysicalMinMireds=%d, ColorTempPhysicalMaxMireds=%d", lo, hi)
				return nil
			}},
			{n: "2a", line: 82, title: "MoveToColorTemperature", pics: "CC.C.C0a.Tx", run: c.send("MoveToColorTemperature", colorcontrol.CmdMoveToColorTemperature,
				func() (spec.Encodable, error) { // :85-88
					lo, hi, err := rangeOf()
					return colorcontrol.MoveToColorTemperatureRequest{ColorTemperatureMireds: (lo + hi) / 2, TransitionTime: 0}, err
				})},
			{n: "2b", line: 96, title: "read ColorTemperatureMireds", pics: "CC.C.C0a.Tx & CC.C.A0007", run: c.reads(ccColorTemperatureMireds)},
			{n: "3a", line: 105, title: "MoveColorTemperature", pics: "CC.C.C4b.Tx", run: c.send("MoveColorTemperature", colorcontrol.CmdMoveColorTemperature,
				func() (spec.Encodable, error) { // :108, rangeArgs :32-35
					lo, hi, err := rangeOf()
					return colorcontrol.MoveColorTemperatureRequest{MoveMode: 1, Rate: 10, ColorTemperatureMinimumMireds: lo, ColorTemperatureMaximumMireds: hi}, err
				})},
			{n: "3b", line: 116, title: "read ColorTemperatureMireds", pics: "CC.C.C4b.Tx & CC.C.A0007", run: c.reads(ccColorTemperatureMireds)},
			{n: "4a", line: 125, title: "StepColorTemperature", pics: "CC.C.C4c.Tx", run: c.send("StepColorTemperature", colorcontrol.CmdStepColorTemperature,
				func() (spec.Encodable, error) { // :128-133
					lo, hi, err := rangeOf()
					return colorcontrol.StepColorTemperatureRequest{StepMode: 1, StepSize: 10, TransitionTime: 0, ColorTemperatureMinimumMireds: lo, ColorTemperatureMaximumMireds: hi}, err
				})},
			{n: "4b", line: 141, title: "read ColorTemperatureMireds", pics: "CC.C.C4c.Tx & CC.C.A0007", run: c.reads(ccColorTemperatureMireds)},
			{n: "5a", line: 150, title: "StopMoveStep", pics: "CC.C.C47.Tx", run: c.stopMoveStep()},
			{n: "5b", line: 155, title: "read ColorTemperatureMireds", pics: "CC.C.C47.Tx & CC.C.A0007", run: c.reads(ccColorTemperatureMireds)},
		},
	}
}

// caseCC75 mirrors TC-CC-7.5: the EnhancedHue commands.
func caseCC75() goCertCase {
	c := &ccClient{}
	return goCertCase{
		tc: "TC-CC-7.5", line: 13, pics: []string{"CC.C", "CC.C.F01"}, at: ccEndpoint("CC.S.F01"),
		steps: []goStep{
			{n: "0", line: 14, title: "commission, TH OnOff On, the TH supports EnhancedHue", run: c.setup(ccEnhancedHue)},
			{n: "1a", line: 23, title: "EnhancedMoveToHue", pics: "CC.C.C40.Tx", run: c.send("EnhancedMoveToHue", colorcontrol.CmdEnhancedMoveToHue,
				fixed(colorcontrol.EnhancedMoveToHueRequest{EnhancedHue: 10000, Direction: 0, TransitionTime: 0}))}, // :26
			{n: "1b", line: 33, title: "read EnhancedCurrentHue", pics: "CC.C.C40.Tx & CC.C.A4000", run: c.reads(ccEnhancedCurrentHue)},
			{n: "2a", line: 42, title: "EnhancedMoveHue", pics: "CC.C.C41.Tx", run: c.send("EnhancedMoveHue", colorcontrol.CmdEnhancedMoveHue,
				fixed(colorcontrol.EnhancedMoveHueRequest{MoveMode: 1, Rate: 100}))}, // :45
			{n: "2b", line: 52, title: "read EnhancedCurrentHue", pics: "CC.C.C41.Tx & CC.C.A4000", run: c.reads(ccEnhancedCurrentHue)},
			{n: "3a", line: 61, title: "EnhancedStepHue", pics: "CC.C.C42.Tx", run: c.send("EnhancedStepHue", colorcontrol.CmdEnhancedStepHue,
				fixed(colorcontrol.EnhancedStepHueRequest{StepMode: 1, StepSize: 1000, TransitionTime: 0}))}, // :64
			{n: "3b", line: 71, title: "read EnhancedCurrentHue", pics: "CC.C.C42.Tx & CC.C.A4000", run: c.reads(ccEnhancedCurrentHue)},
			{n: "4a", line: 80, title: "EnhancedMoveToHueAndSaturation", pics: "CC.C.C43.Tx", run: c.send("EnhancedMoveToHueAndSaturation",
				colorcontrol.CmdEnhancedMoveToHueAndSaturation,
				fixed(colorcontrol.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 20000, Saturation: 100, TransitionTime: 0}))}, // :83-87
			{
				n: "4b", line: 94, title: "read EnhancedCurrentHue and CurrentSaturation", pics: "CC.C.C43.Tx & CC.C.A4000 & CC.C.A0001",
				run: c.reads(ccEnhancedCurrentHue, ccCurrentSaturation),
			},
			{n: "5a", line: 103, title: "StopMoveStep", pics: "CC.C.C47.Tx", run: c.stopMoveStep()},
			{n: "5b", line: 108, title: "read EnhancedCurrentHue", pics: "CC.C.C47.Tx & CC.C.A4000", run: c.reads(ccEnhancedCurrentHue)},
		},
	}
}

// caseCC94 mirrors TC-CC-9.4: ColorLoopSet, the loop's attributes, and
// EnhancedCurrentHue read over the loop's first 30 seconds.
func caseCC94() goCertCase {
	c := &ccClient{}
	const (
		hueReads    = 6               // HUE_READS (TC-CC-9.4.test.ts:21)
		hueInterval = 5 * time.Second // HUE_READ_INTERVAL (:22)
	)
	// COLOR_LOOP (:25-31): update every loop parameter, start from
	// EnhancedCurrentHue, count up, 30 s per loop.
	loop := colorcontrol.ColorLoopSetRequest{
		UpdateFlags: colorcontrol.UpdateFlagsUpdateAction | colorcontrol.UpdateFlagsUpdateDirection |
			colorcontrol.UpdateFlagsUpdateTime | colorcontrol.UpdateFlagsUpdateStartHue,
		Action: 2, Direction: 1, Time: 30, StartHue: 0,
	}
	return goCertCase{
		tc: "TC-CC-9.4", line: 33, pics: []string{"CC.C", "CC.C.F02", "CC.C.F01"}, at: ccEndpoint("CC.S.F02", "CC.S.F01"),
		steps: []goStep{
			{n: "0", line: 34, title: "commission, TH OnOff On, the TH supports ColorLoop and EnhancedHue", run: c.setup(ccColorLoop, ccEnhancedHue)},
			{n: "1", line: 45, title: "ColorLoopSet", pics: "CC.C.C44.Tx", run: c.send("ColorLoopSet", colorcontrol.CmdColorLoopSet, fixed(loop))},
			{n: "2a", line: 50, title: "read ColorLoopActive", pics: "CC.C.A4002", run: c.reads(ccColorLoopActive)},
			{n: "2b", line: 54, title: "read ColorLoopDirection", pics: "CC.C.A4003", run: c.reads(ccColorLoopDirection)},
			{n: "2c", line: 60, title: "read ColorLoopTime", pics: "CC.C.A4004", run: c.reads(ccColorLoopTime)},
			{n: "2d", line: 64, title: "read ColorLoopStartEnhancedHue", pics: "CC.C.A4005", run: c.reads(ccColorLoopStartEnhancedHue)},
			{n: "2e", line: 70, title: "EnhancedCurrentHue every 5 s over 30 s", pics: "CC.C.A4000", run: func(th *goTH) error {
				// :73-81: the read-th read at start + read*5 s
				// (sleepUntil, tc-support.ts:2877-2881).
				start := time.Now()
				var got []string
				for read := 1; read <= hueReads; read++ {
					sleepCtx(th.ctx, time.Until(start.Add(time.Duration(read)*hueInterval)))
					if err := th.ctx.Err(); err != nil {
						return fmt.Errorf("before EnhancedCurrentHue read %d: %w", read, err)
					}
					v, err := c.read(th, ccEnhancedCurrentHue)
					if err != nil {
						return fmt.Errorf("read %d: %w", read, err)
					}
					got = append(got, describeNode(v))
				}
				th.note("EnhancedCurrentHue at 5 s intervals: %s", strings.Join(got, ", "))
				return nil
			}},
		},
	}
}

// caseI32 mirrors TC-I-3.2: Identify and TriggerEffect.
func caseI32() goCertCase {
	var n *client.Node
	const (
		identifyTime = 60 // IDENTIFY_TIME (TC-I-3.2.test.ts:20)
		stopIdentify = 0  // STOP_IDENTIFY (:21)
	)
	ident := func(t uint16) func(th *goTH) error {
		return func(th *goTH) error {
			if _, err := th.invoke(n, cmdPath(th.ep, identify.ClusterID, identify.CmdIdentify), identify.IdentifyRequest{IdentifyTime: t}, false, client.InvokeOptions{}); err != nil {
				return err
			}
			th.note("Identify IdentifyTime=%d on endpoint %d: Success", t, th.ep)
			return nil
		}
	}
	return goCertCase{
		// The on/off light at endpoint 1 (:16-17).
		tc: "TC-I-3.2", line: 55, pics: []string{"I.C"}, at: "I.S & OO.S",
		steps: []goStep{
			{n: "0", line: 56, title: "the DUT commissions the TH", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				th.note("commissioned; the steps run on endpoint %d", th.ep)
				return nil
			}},
			{n: "1", line: 59, title: "Identify with IdentifyTime 0x003C", pics: "I.C.C00.Tx", run: ident(identifyTime)},
			{n: "2", line: 71, title: "Identify with IdentifyTime 0 (Stop)", pics: "I.C.C00.Tx", run: ident(stopIdentify)},
			{n: "3", line: 82, title: "TriggerEffect Breathe, variant 0", pics: "I.C.C40.Tx", run: func(th *goTH) error {
				// EFFECT_IDENTIFIER_BREATHE, EFFECT_VARIANT_DEFAULT (:24-25).
				req := identify.TriggerEffectRequest{EffectIdentifier: identify.EffectIdentifierBreathe, EffectVariant: identify.EffectVariantDefault}
				if _, err := th.invoke(n, cmdPath(th.ep, identify.ClusterID, identify.CmdTriggerEffect), req, false, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("TriggerEffect Breathe/Default on endpoint %d: Success", th.ep)
				return nil
			}},
		},
	}
}

// caseOO32 mirrors TC-OO-3.2: the OnOff commands of a Lighting light.
func caseOO32() goCertCase {
	var n *client.Node
	const (
		onTime      = 50 // ON_TIME (TC-OO-3.2.test.ts:27)
		offWaitTime = 30 // OFF_WAIT_TIME (:28)
	)
	send := func(name string, cmd uint32, fields spec.Encodable) func(th *goTH) error {
		return func(th *goTH) error {
			if _, err := th.invoke(n, cmdPath(th.ep, onoff.ClusterID, cmd), fields, false, client.InvokeOptions{}); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			th.note("%s %+v on endpoint %d: Success", name, fields, th.ep)
			return nil
		}
	}
	return goCertCase{
		// An OnOff cluster with the Lighting feature, which the last three
		// commands need (:16-17).
		tc: "TC-OO-3.2", line: 60, pics: []string{"OO.C"}, at: "OO.S & OO.S.F00",
		steps: []goStep{
			{n: "0", line: 61, title: "the DUT commissions the TH", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				th.note("commissioned; the steps run on endpoint %d", th.ep)
				return nil
			}},
			{n: "1", line: 64, title: "Off", pics: "OO.C.C00.Tx", run: send("Off", onoff.CmdOff, onoff.OffRequest{})},
			{n: "2", line: 68, title: "On", pics: "OO.C.C01.Tx", run: send("On", onoff.CmdOn, onoff.OnRequest{})},
			{n: "3", line: 72, title: "Toggle", pics: "OO.C.C02.Tx", run: send("Toggle", onoff.CmdToggle, onoff.ToggleRequest{})},
			// EFFECT_IDENTIFIER_DYING_LIGHT, EFFECT_VARIANT (:23-24).
			{n: "4", line: 76, title: "OffWithEffect DyingLight, variant 0", pics: "OO.C.C40.Tx", run: send("OffWithEffect", onoff.CmdOffWithEffect,
				onoff.OffWithEffectRequest{EffectIdentifier: onoff.EffectIdentifierDyingLight, EffectVariant: 0})},
			{n: "5", line: 104, title: "OnWithRecallGlobalScene", pics: "OO.C.C41.Tx", run: send("OnWithRecallGlobalScene", onoff.CmdOnWithRecallGlobalScene,
				onoff.OnWithRecallGlobalSceneRequest{})},
			// AcceptOnlyWhenOn set (:118, :123).
			{n: "6", line: 114, title: "OnWithTimedOff", pics: "OO.C.C42.Tx", run: send("OnWithTimedOff", onoff.CmdOnWithTimedOff,
				onoff.OnWithTimedOffRequest{OnOffControl: onoff.OnOffControlAcceptOnlyWhenOn, OnTime: onTime, OffWaitTime: offWaitTime})},
		},
	}
}
