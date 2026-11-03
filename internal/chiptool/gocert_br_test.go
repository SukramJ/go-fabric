// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TC-BR-4 in matter.js's own direction: the module's controller is the
// DUT bridge client, the reference daemon with --bridge-sim the bridge TH
// (examples/reference-bridge/bridge_sim.go, after matter.js's
// support/chip-testing/src/BridgeTestInstance.ts). The DUT is commissioned
// with its default subscription, as matter.js's cert controller commissions
// with its sustained wildcard subscription (InProcessControllerAdapter.ts:
// 2745-2782), and every "the DUT took in" claim reads the controller's own
// image of the node — client.Node.Endpoints and Cluster.Value, matter.js's
// clientEndpoints and clientAttribute (:1630-1665) — taken before any read,
// because a read feeds that image too (TC-BR-4.test.ts:214-220).
//
// matter.js names the TH's endpoints by chip's bridge-app numbers
// (TC-BR-4.test.ts:63-77); the daemon's numbers are its own persisted
// allocation, so the case finds the simulated devices once, after the
// commissioning, by the NodeLabels the test instance gives them
// (BridgeTestInstance.ts:148-172) in the DUT's image. The daemon's own
// lights ("Desk Lamp", "Ceiling Light", …) are not the TS's and are not
// among them.
//
// matter.js also checks the TH's log for each read's AttributePathIB and
// the On command's CommandDataIB (expectAttributePathIB, tc-support.ts:973;
// expectCommandInvoke :1199; LOG_TIMEOUT 15 s :64); the daemon logs both at
// debug level (bridge's matter.rx.im.read_path and matter.rx.im.invoke), so
// the same checks run on its log.

func init() {
	registerGoCert(caseBR4)
}

// TC-BR-4.test.ts constants.
const (
	// brNoticeWait is how long the DUT is given to take in a change the TH
	// made, on its subscription alone (NOTICE_WAIT_MS, :110).
	brNoticeWait = 20 * time.Second
	// brPoll is the poll of the DUT's image while it waits (POLL_MS, :111).
	brPoll = 100 * time.Millisecond
	// brLogTimeout is LOG_TIMEOUT (tc-support.ts:64).
	brLogTimeout = 15 * time.Second
	// brOneDegree is one warming (ONE_DEGREE, :102).
	brOneDegree = 100
	// brRenamed is the name renameBridgedLights gives the first light
	// (RENAMED_LABEL, :99).
	brRenamed = "Light 1b"
)

// The device types the plan's endpoint list names (:57-61).
const (
	brOnOffLight        = 0x0100
	brTemperatureSensor = 0x0302
	brPowerSource       = 0x0011
	brAggregator        = 0x000e
)

// The simulated devices' NodeLabels (BridgeTestInstance.ts:152-172):
// LIGHTS are "Light 1" and the four action lights (TC-BR-4.test.ts:74), the
// sensors with a name of their own the two the aggregator holds (:75, :84),
// and the composed device (:76).
var (
	brLightLabels  = []string{"Light 1", "Action Light 1", "Action Light 2", "Action Light 3", "Action Light 4"}
	brSensorLabels = []string{"TempSensor 1", "TempSensor 2"}
)

const brComposedLabel = "Composed Device"

// brRun is the case's DUT controller, the node it commissioned the TH as,
// and the TH's endpoints the case found in the DUT's image.
type brRun struct {
	ctrl *gocontroller.Controller
	n    *client.Node

	aggregator uint16
	// lights are LIGHTS (:74), lights[0] LIGHT_1 (:93).
	lights []uint16
	// sensors are TEMPERATURE_SENSORS (:75): the two named ones, then the
	// composed device's parts (COMPOSED_SENSORS, :77).
	sensors         []uint16
	composed        uint16
	composedSensors []uint16
	// named are NAMED_DEVICES (:84-86): every endpoint carrying
	// BridgedDeviceBasicInformation.
	named []uint16
	// added is the light step 3c added (ADDED_LIGHT_ENDPOINT, :96).
	added uint16
}

func (r *brRun) light1() uint16 { return r.lights[0] }

// commission is step 1a's `dut.commission({passcode, discriminator})`
// (:254) with the TH's QR code and the DUT's default subscription up.
func (r *brRun) commission(th *goTH) error {
	var err error
	if r.ctrl, err = th.controller("dut"); err != nil {
		return err
	}
	from := mark(th)
	d := &dutRun{ctrl: r.ctrl}
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	if r.n, err = d.pair(ctx, thQR(th)); err != nil {
		return fmt.Errorf("DUT commissions TH: %w", err)
	}
	if !waitFor(th.ctx, ddCommissioningLogTimeout, func() bool {
		return strings.Contains(thLogSince(th, from), "msg=matter.gencomm.cmd_ok command_name=CommissioningComplete")
	}) {
		return errors.New("the TH logged no completed commissioning")
	}
	if !waitFor(th.ctx, time.Minute, r.n.SubscriptionActive) {
		return errors.New("the DUT's default subscription to the TH did not come up within a minute")
	}
	addr, _ := r.n.PeerAddress()
	th.note("DUT commissioned the TH as node %d, its default subscription up", addr.NodeID)
	return r.discover(th)
}

// discover finds the simulated devices in the DUT's image by their
// NodeLabels, before any read.
func (r *brRun) discover(th *goTH) error {
	byLabel := map[string][]uint16{}
	for _, no := range heldEndpoints(r.n) {
		if v, ok := held(r.n, no, bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel); ok {
			byLabel[v.El.String] = append(byLabel[v.El.String], no)
		}
		if slices.Contains(heldDeviceTypes(r.n, no), brAggregator) {
			if r.aggregator != 0 {
				return fmt.Errorf("the DUT holds two aggregators, endpoints %d and %d", r.aggregator, no)
			}
			r.aggregator = no
		}
	}
	one := func(label string) (uint16, error) {
		if got := byLabel[label]; len(got) == 1 {
			return got[0], nil
		}
		return 0, fmt.Errorf("the DUT holds %d endpoints named %q, want one (the daemon runs with --bridge-sim?)", len(byLabel[label]), label)
	}
	var errs []error
	for _, label := range brLightLabels {
		no, err := one(label)
		errs = append(errs, err)
		r.lights = append(r.lights, no)
	}
	for _, label := range brSensorLabels {
		no, err := one(label)
		errs = append(errs, err)
		r.sensors = append(r.sensors, no)
	}
	var err error
	r.composed, err = one(brComposedLabel)
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if r.aggregator == 0 {
		return errors.New("the DUT holds no aggregator")
	}
	for _, p := range heldParts(r.n, r.composed) {
		r.composedSensors = append(r.composedSensors, uint16(p)) //nolint:gosec // an endpoint number
	}
	if len(r.composedSensors) != 2 {
		return fmt.Errorf("the DUT holds parts %v of the composed device %d, want its two sensors", r.composedSensors, r.composed)
	}
	r.sensors = append(r.sensors, r.composedSensors...)
	r.named = slices.Sorted(slices.Values(append(append(slices.Clone(r.lights), r.sensors[:2]...), r.composed)))
	th.note("the DUT's image names the aggregator %d, LIGHTS %v (LIGHT_1 %d), TEMPERATURE_SENSORS %v, the composed device %d with parts %v",
		r.aggregator, r.lights, r.light1(), r.sensors, r.composed, r.composedSensors)
	return nil
}

// --- the DUT's image (clientEndpoints / clientAttribute) --------------------

// heldEndpoints are the endpoint numbers the DUT holds for the TH
// (clientEndpoints, InProcessControllerAdapter.ts:1630-1649).
func heldEndpoints(n *client.Node) []uint16 {
	eps := n.Endpoints()
	out := make([]uint16, 0, len(eps))
	for _, ep := range eps {
		out = append(out, ep.Number())
	}
	return out
}

// held is the DUT's own value of an attribute (clientAttribute,
// :1651-1665): false where it holds no such endpoint or value.
func held(n *client.Node, ep uint16, cluster, attr uint32) (spec.Node, bool) {
	e := n.Endpoint(ep)
	if e == nil {
		return spec.Node{}, false
	}
	return e.Cluster(cluster).Value(attr)
}

// heldDeviceTypes are the device types of the DUT's DeviceTypeList for ep
// (clientEndpoints' deviceTypes, :1637, :1641-1643).
func heldDeviceTypes(n *client.Node, ep uint16) []uint64 {
	v, _ := held(n, ep, descriptor.ClusterID, descriptor.AttrDeviceTypeList)
	return deviceTypesOf(v)
}

// heldParts is the DUT's PartsList for ep (clientEndpoints' parts, :1638,
// :1644).
func heldParts(n *client.Node, ep uint16) []uint64 {
	v, _ := held(n, ep, descriptor.ClusterID, descriptor.AttrPartsList)
	return partsOf(v)
}

// deviceTypesOf is deviceTypesOf (TC-BR-4.test.ts:149-158).
func deviceTypesOf(v spec.Node) []uint64 {
	var out []uint64
	for _, e := range v.Children {
		if f, ok := structField(e, 0); ok {
			out = append(out, f.El.Uint)
		}
	}
	return out
}

// hexTypes renders device type ids for a step's evidence.
func hexTypes(ids []uint64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("0x%04X", id))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// partsOf is partsOf (:161-163).
func partsOf(v spec.Node) []uint64 {
	out := make([]uint64, 0, len(v.Children))
	for _, e := range v.Children {
		out = append(out, e.El.Uint)
	}
	return out
}

// heldBefore is heldBefore (:221-232): the DUT's values for attr on each
// endpoint, taken before anything reads it.
func heldBefore(n *client.Node, eps []uint16, cluster, attr uint32) map[uint16]*spec.Node {
	out := map[uint16]*spec.Node{}
	for _, ep := range eps {
		if v, ok := held(n, ep, cluster, attr); ok {
			out[ep] = &v
		} else {
			out[ep] = nil
		}
	}
	return out
}

// until is until (:120-131): pred polled every POLL_MS until it holds or
// NOTICE_WAIT_MS pass; no read is sent meanwhile.
func until(th *goTH, pred func() bool) bool {
	deadline := time.Now().Add(brNoticeWait)
	for {
		if pred() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-th.ctx.Done():
			return pred()
		case <-time.After(brPoll):
		}
	}
}

// --- reads ------------------------------------------------------------------

// brRead is one wildcard read's answer.
type brRead struct{ r *client.Report }

// readEveryEndpoint is readEveryEndpoint (:173-194): one read of
// cluster/attribute with a wildcard endpoint, and the TH's log of the
// AttributePathIB it received (expectAttributePathIB, tc-support.ts:973),
// the daemon's matter.rx.im.read_path at debug level.
func (r *brRun) readEveryEndpoint(th *goTH, what string, cluster, attr uint32) (brRead, error) {
	from := mark(th)
	p := im.ConcreteAttributePath{Cluster: cluster, Attribute: attr, HasCluster: true, HasAttribute: true}
	rep, err := th.read(r.n, p)
	if err != nil {
		return brRead{}, fmt.Errorf("the DUT's read of %s: %w", what, err)
	}
	want := fmt.Sprintf("endpoint_set=false cluster=%d cluster_set=true attribute=%d attribute_set=true", cluster, attr)
	if !waitFor(th.ctx, brLogTimeout, func() bool {
		for l := range strings.SplitSeq(thLogSince(th, from), "\n") {
			if strings.Contains(l, "msg=matter.rx.im.read_path ") && strings.Contains(l, want) {
				return true
			}
		}
		return false
	}) {
		return brRead{}, fmt.Errorf("the TH logged no AttributePathIB of the DUT's read of %s within %s", what, brLogTimeout)
	}
	return brRead{rep}, nil
}

// valueAt is valueAt (:134-138).
func (b brRead) valueAt(ep uint16, cluster, attr uint32) (spec.Node, bool) {
	return b.r.Value(ep, cluster, attr)
}

// answering is endpointsAnswering (:141-146).
func (b brRead) answering(cluster, attr uint32) []uint16 {
	var out []uint16
	for i := range b.r.Attributes {
		if a := &b.r.Attributes[i]; a.Path.Cluster == cluster && a.Path.Attribute == attr {
			out = append(out, a.Path.Endpoint)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// missing are the endpoints of want the read did not answer for.
func (b brRead) missing(want []uint16, cluster, attr uint32) []uint16 {
	answered := b.answering(cluster, attr)
	var out []uint16
	for _, ep := range want {
		if !slices.Contains(answered, ep) {
			out = append(out, ep)
		}
	}
	return out
}

// same is the TS's `held === read` for the scalar values the case
// compares: both present and equal.
func same(h *spec.Node, read spec.Node, readOK bool) bool {
	return h != nil && readOK && describeNode(*h) == describeNode(read)
}

// describeHeld renders a held value, "undefined" where the DUT holds none.
func describeHeld(h *spec.Node) string {
	if h == nil {
		return "undefined"
	}
	return describeNode(*h)
}

// stateChecks is lightStateChecks / temperatureChecks (:937-1020): the
// DUT's values for eps taken before a wildcard read of cluster/attr, every
// endpoint answered, and the DUT already holding what the TH reports.
func (r *brRun) stateChecks(th *goTH, what string, eps []uint16, cluster, attr uint32) error {
	before := heldBefore(r.n, eps, cluster, attr)
	read, err := r.readEveryEndpoint(th, what, cluster, attr)
	if err != nil {
		return err
	}
	if m := read.missing(eps, cluster, attr); len(m) > 0 {
		return fmt.Errorf("the DUT read %s from endpoints %v, and none for %v", what, read.answering(cluster, attr), m)
	}
	var disagreeing []string
	for _, ep := range eps {
		v, ok := read.valueAt(ep, cluster, attr)
		if !same(before[ep], v, ok) {
			disagreeing = append(disagreeing, fmt.Sprintf("%d (held %s, read %s)", ep, describeHeld(before[ep]), describeNode(v)))
		}
	}
	if len(disagreeing) > 0 {
		return fmt.Errorf("the DUT held a different %s on endpoints %s", what, strings.Join(disagreeing, ", "))
	}
	vals := make([]string, 0, len(eps))
	for _, ep := range eps {
		vals = append(vals, fmt.Sprintf("%d: %s", ep, describeHeld(before[ep])))
	}
	th.note("the TH logged the wildcard read of %s; every one of %v answered; the DUT already held the %s the TH reports (%s)",
		what, eps, what, strings.Join(vals, ", "))
	return nil
}

// backchannel is `th.backchannel({name})`: the app-pipe command, waited
// for until the daemon logged it applied, as matter.js awaits the test
// instance's handler (BridgeTestInstance.ts:177-203).
func backchannel(th *goTH, name string) error {
	wait, err := th.pipeAsync(map[string]any{"Name": name})
	if err != nil {
		return err
	}
	return wait()
}

// --- the case ----------------------------------------------------------------

func caseBR4() goCertCase {
	r := &brRun{}
	need := func(run func(th *goTH) error) func(th *goTH) error {
		return func(th *goTH) error {
			if r.n == nil || r.composed == 0 {
				return errors.New("step 1a did not commission the TH and find its devices")
			}
			return run(th)
		}
	}
	return goCertCase{
		tc: "TC-BR-4", line: 234, role: roleDUT, pics: []string{"MCORE.BRIDGECLIENT"},
		daemonArgs: []string{"--bridge-sim"},
		// commissioned.decommissionAll (:927-929).
		finalize: func(th *goTH) {
			if r.n != nil {
				_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
			}
		},
		steps: []goStep{
			{n: "1a", line: 248, title: "commission TH to the DUT; the DUT reads DeviceTypeList and PartsList from all endpoints", run: func(th *goTH) error {
				if err := r.commission(th); err != nil {
					return err
				}
				return need(r.step1a)(th)
			}},
			{n: "1b", line: 332, pics: "MCORE.DEVLIST.UseDevices", title: "the DUT contains the devices of the endpoint list", run: need(r.step1b)},
			{n: "1c", line: 382, pics: "MCORE.DEVLIST.UseDeviceName", title: "the DUT has the NodeLabel of the bridged devices", run: need(func(th *goTH) error {
				return r.stateChecks(th, "NodeLabel", r.named, bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
			})},
			{n: "1d", line: 430, pics: "MCORE.DEVLIST.UseDeviceState", title: "the DUT has the OnOff of the bridged lights", run: need(func(th *goTH) error {
				return r.stateChecks(th, "OnOff", r.lights, onoff.ClusterID, onoff.AttrOnOff)
			})},
			{n: "1e", line: 442, pics: "MCORE.DEVLIST.UseDeviceState", title: "the TH toggles a bridged light; the DUT takes it in", run: need(r.step1e)},
			{n: "1f", line: 485, pics: "MCORE.DEVLIST.UseDeviceState", title: "the DUT has the MeasuredValue of the bridged sensors", run: need(func(th *goTH) error {
				return r.stateChecks(th, "MeasuredValue", r.sensors, temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue)
			})},
			{n: "1g", line: 497, pics: "MCORE.DEVLIST.UseDeviceState", title: "the TH warms the sensors; the DUT takes it in", run: need(r.step1g)},
			{n: "1h", line: 562, pics: "MCORE.DEVLIST.UseBatInfo", title: "the DUT has the composed device's BatChargeLevel", run: need(r.step1h)},
			{n: "2a", line: 604, title: "the DUT switches a bridged light", run: need(r.step2a)},
			{n: "3a", line: 692, pics: "MCORE.DEVLIST.UseDeviceName", title: "the TH renames a bridged light; the DUT takes it in", run: need(r.step3a)},
			{n: "3b", line: 722, pics: "MCORE.DEVLIST.UseDeviceName", title: "the DUT contains the updated name", run: need(r.step3b)},
			{n: "3c", line: 752, title: "the TH adds a bridged light; the DUT takes it in", run: need(r.step3c)},
			{n: "3d", line: 831, pics: "MCORE.DEVLIST.UseDevices", title: "the DUT contains the added device", run: need(r.step3d)},
			{n: "3e", line: 855, title: "the TH removes a bridged light; the DUT takes it in", run: need(r.step3e)},
			{n: "3f", line: 906, pics: "MCORE.DEVLIST.UseDevices", title: "the DUT no longer contains the removed device", run: need(r.step3f)},
		},
	}
}

// step1a: DeviceTypeList and PartsList read from every endpoint the plan
// names, the composed device's PartsList naming its sensors (:260-324).
func (r *brRun) step1a(th *goTH) error {
	types, err := r.readEveryEndpoint(th, "DeviceTypeList", descriptor.ClusterID, descriptor.AttrDeviceTypeList)
	if err != nil {
		return err
	}
	parts, err := r.readEveryEndpoint(th, "PartsList", descriptor.ClusterID, descriptor.AttrPartsList)
	if err != nil {
		return err
	}
	expected := slices.Sorted(slices.Values(append(append([]uint16{0, r.aggregator, r.composed}, r.lights...), r.sensors...)))
	if m := types.missing(expected, descriptor.ClusterID, descriptor.AttrDeviceTypeList); len(m) > 0 {
		return fmt.Errorf("the DUT read DeviceTypeList from endpoints %v, none for %v", types.answering(descriptor.ClusterID, descriptor.AttrDeviceTypeList), m)
	}
	if m := parts.missing(expected, descriptor.ClusterID, descriptor.AttrPartsList); len(m) > 0 {
		return fmt.Errorf("the DUT read PartsList from endpoints %v, none for %v", parts.answering(descriptor.ClusterID, descriptor.AttrPartsList), m)
	}
	v, _ := parts.valueAt(r.composed, descriptor.ClusterID, descriptor.AttrPartsList)
	composedParts := partsOf(v)
	for _, s := range r.composedSensors {
		if !slices.Contains(composedParts, uint64(s)) {
			return fmt.Errorf("the composed device on endpoint %d names parts %v, not its sensor %d", r.composed, composedParts, s)
		}
	}
	agg, _ := parts.valueAt(r.aggregator, descriptor.ClusterID, descriptor.AttrPartsList)
	th.note("the TH logged both wildcard reads; DeviceTypeList and PartsList answered by all %d endpoints the plan names %v (of %d the TH has); the composed device %d names parts %v; the aggregator names %d parts",
		len(expected), expected, len(types.answering(descriptor.ClusterID, descriptor.AttrDeviceTypeList)), r.composed, composedParts, len(partsOf(agg)))
	return nil
}

// step1b: the DUT's image holds the lights and sensors with their device
// types, the composed device as a power source, endpoint 1 as the
// aggregator (:335-376).
func (r *brRun) step1b(th *goTH) error {
	var wrong []string
	check := func(eps []uint16, dt uint64) {
		for _, ep := range eps {
			if !slices.Contains(heldDeviceTypes(r.n, ep), dt) {
				wrong = append(wrong, fmt.Sprintf("%d holds %v, not 0x%04X", ep, hexTypes(heldDeviceTypes(r.n, ep)), dt))
			}
		}
	}
	check(r.lights, brOnOffLight)
	check(r.sensors, brTemperatureSensor)
	check([]uint16{r.composed}, brPowerSource)
	check([]uint16{r.aggregator}, brAggregator)
	if len(wrong) > 0 {
		return fmt.Errorf("the DUT's device list: %s", strings.Join(wrong, "; "))
	}
	th.note("the DUT holds lights on %v (0x0100) and temperature sensors on %v (0x0302); endpoint %d is the aggregator and endpoint %d carries device types %v",
		r.lights, r.sensors, r.aggregator, r.composed, hexTypes(heldDeviceTypes(r.n, r.composed)))
	return nil
}

// step1e: the TH toggles; the DUT's own OnOff of LIGHT_1 reaches the
// toggled value on its subscription alone, then the state checks
// (:445-478).
func (r *brRun) step1e(th *goTH) error {
	before, ok := held(r.n, r.light1(), onoff.ClusterID, onoff.AttrOnOff)
	if !ok || (before.El.Type != tlv.TypeBoolFalse && before.El.Type != tlv.TypeBoolTrue) {
		return fmt.Errorf("the DUT holds no OnOff for endpoint %d (%s), so a toggle it took in cannot be told from a first value arriving", r.light1(), describeNode(before))
	}
	start := time.Now()
	if err := backchannel(th, "toggleBridgedLights"); err != nil {
		return err
	}
	expected := !before.El.Bool
	noticed := until(th, func() bool {
		v, ok := held(r.n, r.light1(), onoff.ClusterID, onoff.AttrOnOff)
		return ok && v.El.Bool == expected
	})
	after, _ := held(r.n, r.light1(), onoff.ClusterID, onoff.AttrOnOff)
	if !noticed {
		return fmt.Errorf("the DUT held %t for endpoint %d's OnOff and still holds %s after %s, where the toggle makes it %t",
			before.El.Bool, r.light1(), describeNode(after), brNoticeWait, expected)
	}
	th.note("the DUT held %t for endpoint %d's OnOff and holds %t %s after the toggle, on its subscription alone",
		before.El.Bool, r.light1(), expected, time.Since(start).Round(time.Millisecond))
	return r.stateChecks(th, "OnOff", r.lights, onoff.ClusterID, onoff.AttrOnOff)
}

// step1g: the TH warms all four sensors; every held MeasuredValue reaches
// one degree more, then the state checks (:500-555).
func (r *brRun) step1g(th *goTH) error {
	before := heldBefore(r.n, r.sensors, temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue)
	expected := map[uint16]int64{}
	for _, ep := range r.sensors {
		v := before[ep]
		if v == nil || v.El.IsNull {
			return fmt.Errorf("the DUT holds no MeasuredValue for endpoint %d (%s), so a warming it took in cannot be told from a first value arriving", ep, describeHeld(v))
		}
		expected[ep] = v.El.Int + brOneDegree
	}
	start := time.Now()
	if err := backchannel(th, "warmBridgedTemperatureSensors"); err != nil {
		return err
	}
	reached := func(ep uint16) bool {
		v, ok := held(r.n, ep, temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue)
		return ok && !v.El.IsNull && v.El.Int == expected[ep]
	}
	noticed := until(th, func() bool {
		for _, ep := range r.sensors {
			if !reached(ep) {
				return false
			}
		}
		return true
	})
	after := heldBefore(r.n, r.sensors, temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue)
	var vals []string
	for _, ep := range r.sensors {
		vals = append(vals, fmt.Sprintf("%d: %s (expected %d)", ep, describeHeld(after[ep]), expected[ep]))
	}
	if !noticed {
		return fmt.Errorf("the DUT still holds a temperature short of one warming after %s: %s", brNoticeWait, strings.Join(vals, ", "))
	}
	th.note("every sensor's held MeasuredValue reached the value one warming makes it within %s, on the subscription alone: %s",
		time.Since(start).Round(time.Millisecond), strings.Join(vals, ", "))
	return r.stateChecks(th, "MeasuredValue", r.sensors, temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue)
}

// step1h: the DUT's own BatChargeLevel of the composed device equals the
// TH's answer (:566-597).
func (r *brRun) step1h(th *goTH) error {
	h, ok := held(r.n, r.composed, powersource.ClusterID, powersource.AttrBatChargeLevel)
	var before *spec.Node
	if ok {
		before = &h
	}
	read, err := r.readEveryEndpoint(th, "BatChargeLevel", powersource.ClusterID, powersource.AttrBatChargeLevel)
	if err != nil {
		return err
	}
	v, readOK := read.valueAt(r.composed, powersource.ClusterID, powersource.AttrBatChargeLevel)
	if !readOK {
		return fmt.Errorf("the DUT read BatChargeLevel from endpoints %v, none for the composed device %d",
			read.answering(powersource.ClusterID, powersource.AttrBatChargeLevel), r.composed)
	}
	if !same(before, v, readOK) {
		return fmt.Errorf("the DUT held %s where the TH reports %s", describeHeld(before), describeNode(v))
	}
	th.note("the TH logged the wildcard read of BatChargeLevel; endpoints %v answered, the composed device %d reports %s, which the DUT already held",
		read.answering(powersource.ClusterID, powersource.AttrBatChargeLevel), r.composed, describeNode(v))
	return nil
}

// step2a: the DUT switches LIGHT_1 off, then on; the TH logs the On
// command (expectCommandInvoke) and the light reads on (:607-684).
func (r *brRun) step2a(th *goTH) error {
	ep := r.light1()
	if _, err := th.invoke(r.n, cmdPath(ep, onoff.ClusterID, onoff.CmdOff), emptyFields, false, client.InvokeOptions{}); err != nil {
		return err
	}
	before, err := th.readValue(r.n, attrPath(ep, onoff.ClusterID, onoff.AttrOnOff))
	if err != nil {
		return err
	}
	if before.El.Bool {
		return fmt.Errorf("endpoint %d's OnOff reads %s after an Off command, so an On command that follows cannot be told from the light already being on", ep, describeNode(before))
	}
	from := mark(th)
	if _, err := th.invoke(r.n, cmdPath(ep, onoff.ClusterID, onoff.CmdOn), emptyFields, false, client.InvokeOptions{}); err != nil {
		return fmt.Errorf("the DUT's On command for endpoint %d: %w", ep, err)
	}
	want := fmt.Sprintf(`req_paths="ep=%d cl=0x%04X cmd=0x%X"`, ep, onoff.ClusterID, onoff.CmdOn)
	if !waitFor(th.ctx, brLogTimeout, func() bool { return strings.Contains(thLogSince(th, from), want) }) {
		return fmt.Errorf("the TH logged no On command for endpoint %d within %s", ep, brLogTimeout)
	}
	after, err := th.readValue(r.n, attrPath(ep, onoff.ClusterID, onoff.AttrOnOff))
	if err != nil {
		return err
	}
	if !after.El.Bool {
		return fmt.Errorf("endpoint %d's OnOff read %s before the command and %s after it", ep, describeNode(before), describeNode(after))
	}
	th.note("the On command answered Success; the TH logged matter.rx.im.invoke %s; endpoint %d's OnOff read false before the command and true after it", want, ep)
	return nil
}

// step3a: the TH renames the lights; the DUT's own NodeLabel of LIGHT_1
// reaches "Light 1b" (:695-712).
func (r *brRun) step3a(th *goTH) error {
	start := time.Now()
	if err := backchannel(th, "renameBridgedLights"); err != nil {
		return err
	}
	label := func() spec.Node {
		v, _ := held(r.n, r.light1(), bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
		return v
	}
	if !until(th, func() bool { return label().El.String == brRenamed }) {
		return fmt.Errorf("the DUT holds %s for endpoint %d's NodeLabel %s after the rename", describeNode(label()), r.light1(), brNoticeWait)
	}
	th.note("the DUT holds %q for endpoint %d's NodeLabel %s after the rename, on its subscription alone",
		brRenamed, r.light1(), time.Since(start).Round(time.Millisecond))
	return nil
}

// step3b: the TH reports "Light 1b" and the DUT already held it
// (:725-745).
func (r *brRun) step3b(th *goTH) error {
	h, ok := held(r.n, r.light1(), bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
	var before *spec.Node
	if ok {
		before = &h
	}
	read, err := r.readEveryEndpoint(th, "NodeLabel", bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
	if err != nil {
		return err
	}
	v, readOK := read.valueAt(r.light1(), bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
	if !readOK || v.El.String != brRenamed || !same(before, v, readOK) {
		return fmt.Errorf("the TH reports %s and the DUT already held %s", describeNode(v), describeHeld(before))
	}
	th.note("the TH logged the wildcard read of NodeLabel; it reports %q for endpoint %d and the DUT already held it", brRenamed, r.light1())
	return nil
}

// step3c: the TH adds the second light; a new endpoint appears in the
// DUT's image on its subscription alone; the root and the aggregator name
// it, it answers its own PartsList, its DeviceTypeList holds an On/Off
// Light (:755-821). A reassembly reports the PartsList of the root and the
// aggregator and every attribute of the added endpoint
// (bridge/bridge.go Reassemble, announceAddedEndpoints).
func (r *brRun) step3c(th *goTH) error {
	known := heldEndpoints(r.n)
	start := time.Now()
	if err := backchannel(th, "addBridgedLight"); err != nil {
		return err
	}
	fresh := func() []uint16 {
		var out []uint16
		for _, no := range heldEndpoints(r.n) {
			if !slices.Contains(known, no) {
				out = append(out, no)
			}
		}
		return out
	}
	if !until(th, func() bool { return len(fresh()) > 0 }) {
		return fmt.Errorf("the DUT holds no endpoint beyond %v %s after the add", known, brNoticeWait)
	}
	took := time.Since(start).Round(time.Millisecond)
	got := fresh()
	if len(got) != 1 {
		return fmt.Errorf("the DUT took in endpoints %v after the add, want one", got)
	}
	r.added = got[0]
	parts, err := r.readEveryEndpoint(th, "PartsList", descriptor.ClusterID, descriptor.AttrPartsList)
	if err != nil {
		return err
	}
	root, _ := parts.valueAt(0, descriptor.ClusterID, descriptor.AttrPartsList)
	agg, _ := parts.valueAt(r.aggregator, descriptor.ClusterID, descriptor.AttrPartsList)
	answered := slices.Contains(parts.answering(descriptor.ClusterID, descriptor.AttrPartsList), r.added)
	types, err := r.readEveryEndpoint(th, "DeviceTypeList", descriptor.ClusterID, descriptor.AttrDeviceTypeList)
	if err != nil {
		return err
	}
	tv, _ := types.valueAt(r.added, descriptor.ClusterID, descriptor.AttrDeviceTypeList)
	addedTypes := deviceTypesOf(tv)
	if !slices.Contains(partsOf(root), uint64(r.added)) || !slices.Contains(partsOf(agg), uint64(r.added)) || !answered {
		return fmt.Errorf("the root names parts %v, the aggregator %v, and endpoint %d answered a PartsList of its own: %t",
			partsOf(root), partsOf(agg), r.added, answered)
	}
	if !slices.Contains(addedTypes, brOnOffLight) {
		return fmt.Errorf("endpoint %d reports device types %v", r.added, hexTypes(addedTypes))
	}
	th.note("the DUT holds endpoint %d %s after the add, on its subscription alone; the TH logged both wildcard reads; the root and the aggregator name it, it answered its own PartsList, and reports device types %v",
		r.added, took, hexTypes(addedTypes))
	return nil
}

// step3d: the DUT's image holds the added endpoint as an On/Off Light
// (:834-848).
func (r *brRun) step3d(th *goTH) error {
	if r.added == 0 {
		return errors.New("step 3c found no added endpoint")
	}
	if n := r.n.Endpoint(r.added); n == nil {
		return fmt.Errorf("the DUT's device list has no endpoint %d", r.added)
	}
	if dt := heldDeviceTypes(r.n, r.added); !slices.Contains(dt, brOnOffLight) {
		return fmt.Errorf("the DUT holds endpoint %d with device types %v", r.added, hexTypes(dt))
	}
	label, _ := held(r.n, r.added, bridgeddevicebasicinformation.ClusterID, bridgeddevicebasicinformation.AttrNodeLabel)
	th.note("the DUT holds endpoint %d with device types %v, named %s", r.added, hexTypes(heldDeviceTypes(r.n, r.added)), describeNode(label))
	return nil
}

// step3e: the TH removes LIGHT_1; it leaves the DUT's image on the
// subscription alone — the root's parts list no longer names it, so the
// image erases it (ClientStructure.ts:1076-1145; controller/client
// structure.go synchronizePartsList) —, and the root and the aggregator no longer name
// it (:858-897).
func (r *brRun) step3e(th *goTH) error {
	start := time.Now()
	if err := backchannel(th, "removeBridgedLight"); err != nil {
		return err
	}
	gone := func() bool { return !slices.Contains(heldEndpoints(r.n), r.light1()) }
	if !until(th, gone) {
		return fmt.Errorf("the DUT still holds endpoint %d %s after the remove", r.light1(), brNoticeWait)
	}
	took := time.Since(start).Round(time.Millisecond)
	parts, err := r.readEveryEndpoint(th, "PartsList", descriptor.ClusterID, descriptor.AttrPartsList)
	if err != nil {
		return err
	}
	root, _ := parts.valueAt(0, descriptor.ClusterID, descriptor.AttrPartsList)
	agg, _ := parts.valueAt(r.aggregator, descriptor.ClusterID, descriptor.AttrPartsList)
	if slices.Contains(partsOf(root), uint64(r.light1())) || slices.Contains(partsOf(agg), uint64(r.light1())) {
		return fmt.Errorf("the root names parts %v and the aggregator %v, still %d", partsOf(root), partsOf(agg), r.light1())
	}
	th.note("the DUT no longer holds endpoint %d %s after the remove, on its subscription alone; the TH logged the wildcard read of PartsList; neither the root nor the aggregator names it",
		r.light1(), took)
	return nil
}

// step3f: the DUT's image no longer names the removed device (:909-920).
func (r *brRun) step3f(th *goTH) error {
	eps := heldEndpoints(r.n)
	if slices.Contains(eps, r.light1()) {
		return fmt.Errorf("the DUT's device list names endpoints %v, still %d", eps, r.light1())
	}
	th.note("the DUT's device list names %d endpoints, not %d", len(eps), r.light1())
	return nil
}
