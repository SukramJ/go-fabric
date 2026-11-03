// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	"github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The IDM cases: matter.js's TC-IDM-*.test.ts, whose TH is chip's or
// matter.js's all-clusters app. The TS addresses that app's endpoint 1;
// the mirrored step addresses the daemon's first endpoint serving the
// cluster (goStep.at), and a step whose attribute the daemon does not serve
// is skipped on that attribute's PICS (goStep.need).

func init() {
	for _, c := range []func() goCertCase{caseIDM11, caseIDM13, caseIDM21, caseIDM31, caseIDM41, caseIDM51, caseIDM63, caseIDM64} {
		registerGoCert(c)
	}
}

// faultInjectionCluster is chip's test-only FaultInjection cluster, served
// by an nlfaultinject build only (TC-IDM-1.3.test.ts:57-63).
const faultInjectionCluster uint32 = 0xFFF1FC03

// caseIDM11 mirrors TC-IDM-1.1: the TH invokes OnOff on and off; the DUT
// answers each with Success and acts on the path it was sent.
func caseIDM11() goCertCase {
	var n *client.Node
	invokeAndRead := func(th *goTH, cmd uint32, want bool) error {
		if _, err := th.invoke(n, cmdPath(th.ep, onoff.ClusterID, cmd), nil, false, client.InvokeOptions{}); err != nil {
			return err
		}
		v, err := th.readValue(n, attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff))
		if err != nil {
			return err
		}
		if v.El.Bool != want {
			return fmt.Errorf("OnOff reads %s after command 0x%02X, want %v", describeNode(v), cmd, want)
		}
		th.note("OnOff command 0x%02X on endpoint %d: Success, OnOff=%v", cmd, th.ep, want)
		return nil
	}
	return goCertCase{
		tc: "TC-IDM-1.1", line: 57, pics: []string{"MCORE.IDM.C.InvokeRequest"}, at: "OO.S",
		steps: []goStep{
			{n: "1", line: 58, title: "Invoke Request with one CommandDataIB, specific endpoint, cluster and command", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				return invokeAndRead(th, onoff.CmdOn, true)
			}},
			{n: "2", line: 78, title: "Invoke Request with a wildcard endpoint", na: "Out of Scope in CHIP's certification harness (TC-IDM-1.1.test.ts:83)"},
			{n: "3", line: 85, title: "Invoke Request with one CommandDataIB, three times", run: func(th *goTH) error {
				for range 3 {
					if err := invokeAndRead(th, onoff.CmdOff, false); err != nil {
						return err
					}
				}
				return nil
			}},
		},
	}
}

// caseIDM13 mirrors TC-IDM-1.3: a batched Invoke Request with two unique
// paths, answered in one InvokeResponse; the steps that need the TH to
// answer wrongly on purpose need chip's FaultInjection cluster.
func caseIDM13() goCertCase {
	var n *client.Node
	noFaults := func(th *goTH) error {
		v, err := th.readValue(n, attrPath(0, descriptor.ClusterID, descriptor.AttrServerList))
		if err != nil {
			return err
		}
		for _, c := range v.Children {
			if c.El.Uint == uint64(faultInjectionCluster) {
				return errors.New("the daemon serves FaultInjection: the fault steps can run, port them")
			}
		}
		return goSkip("(b) not supported: the TH's deliberately wrong answers are armed through chip's FaultInjection " +
			"cluster (0xFFF1FC03, an nlfaultinject build only, TC-IDM-1.3.test.ts:57-63), which the reference daemon " +
			"does not serve (its root ServerList checked)")
	}
	return goCertCase{
		tc: "TC-IDM-1.3", line: 127, pics: []string{"MCORE.IDM.C.InvokeRequest.BatchCommands"}, at: "OO.S",
		steps: []goStep{
			{n: "0.1", line: 135, title: "Commission TH Client and DUT to TH device (Server)", run: func(th *goTH) error {
				var err error
				_, n, err = th.commission("dut")
				if err == nil {
					th.note("commissioned; the daemon admits %d paths per invoke", n.MaxPathsPerInvoke())
				}
				return err
			}},
			{n: "0.2", line: 166, title: "TH Client sends FailAtFault to the FaultInjection cluster", at: "", run: noFaults},
			{n: "1", line: 213, title: "Invoke Request with multiple valid unique paths, answered in one response", run: func(th *goTH) error {
				if limit := n.MaxPathsPerInvoke(); limit < 2 {
					return fmt.Errorf("the daemon admits %d paths per invoke: a batch of two cannot travel in one message", limit)
				}
				ctx := th.ctx
				res, err := n.InvokeBatch(ctx, []client.CommandRequest{
					{Path: cmdPath(th.ep, onoff.ClusterID, onoff.CmdOn), Fields: emptyFields},
					{Path: cmdPath(th.ep, onoff.ClusterID, onoff.CmdOff), Fields: emptyFields},
				}, client.InvokeOptions{})
				if err != nil {
					return err
				}
				if len(res) != 2 || res[0].Status != im.StatusSuccess || res[1].Status != im.StatusSuccess {
					return fmt.Errorf("batch of On, Off answered %+v, want two Success", res)
				}
				th.note("On and Off on endpoint %d in one Invoke Request: both Success", th.ep)
				return nil
			}},
			{n: "2", line: 235, title: "TH answers each command in its own Invoke Response Message", run: noFaults},
			{n: "3", line: 261, title: "TH answers each command in its own message, in inverted order", run: noFaults},
			{n: "4", line: 292, title: "TH answers only the first command", run: noFaults},
			{n: "5", line: 318, title: "Invoke Request with one valid CommandDataIB, answered normally", run: func(th *goTH) error {
				if _, err := th.invoke(n, cmdPath(th.ep, onoff.ClusterID, onoff.CmdOn), nil, false, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("OnOff.On on endpoint %d: Success", th.ep)
				return nil
			}},
		},
	}
}

// wildcard is a read path with the named parts set.
func wildcard(ep *uint16, cluster, attr *uint32) im.ConcreteAttributePath {
	var p im.ConcreteAttributePath
	if ep != nil {
		p.Endpoint, p.HasEndpoint = *ep, true
	}
	if cluster != nil {
		p.Cluster, p.HasCluster = *cluster, true
	}
	if attr != nil {
		p.Attribute, p.HasAttribute = *attr, true
	}
	return p
}

func ptr[T any](v T) *T { return &v }

// isType holds a value to one TLV data type.
type isType func(spec.Node) bool

func tlvIn(types ...tlv.ElementType) isType {
	return func(n spec.Node) bool { return slices.Contains(types, n.El.Type) }
}

var (
	isBool     = tlvIn(tlv.TypeBoolFalse, tlv.TypeBoolTrue)
	isString   = tlvIn(tlv.TypeUTF8Str1, tlv.TypeUTF8Str2, tlv.TypeUTF8Str4, tlv.TypeUTF8Str8)
	isUnsigned = tlvIn(tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8, tlv.TypeNull)
	isSigned   = tlvIn(tlv.TypeSignedInt1, tlv.TypeSignedInt2, tlv.TypeSignedInt4, tlv.TypeSignedInt8, tlv.TypeNull)
	isFloat    = tlvIn(tlv.TypeFloat4, tlv.TypeFloat8, tlv.TypeNull)
	isStruct   = tlvIn(tlv.TypeStructure)
	isList     = tlvIn(tlv.TypeArray, tlv.TypeList)
	isOctets   = func(n spec.Node) bool {
		return isList(n) && len(n.Children) > 0 && tlvIn(tlv.TypeOctetStr1, tlv.TypeOctetStr2, tlv.TypeOctetStr4, tlv.TypeOctetStr8)(n.Children[0])
	}
)

// caseIDM21 mirrors TC-IDM-2.1: the TH reads concrete and wildcard paths
// and one attribute of every data type; the DUT reports exactly the paths
// asked for, with values of the type the attribute declares.
func caseIDM21() goCertCase {
	var n *client.Node
	readAll := func(th *goTH, p im.ConcreteAttributePath) ([]interaction.AttributeData, error) {
		r, err := th.read(n, p)
		if err != nil {
			return nil, err
		}
		if len(r.Attributes) == 0 {
			return nil, fmt.Errorf("read %s reported nothing", describePath(p))
		}
		for i := range r.Attributes {
			a := &r.Attributes[i]
			if (p.HasEndpoint && a.Path.Endpoint != p.Endpoint) || (p.HasCluster && a.Path.Cluster != p.Cluster) ||
				(p.HasAttribute && a.Path.Attribute != p.Attribute) {
				return nil, fmt.Errorf("read %s reported %s, outside the path", describePath(p), describePath(a.Path))
			}
		}
		return r.Attributes, nil
	}
	endpointsOf := func(as []interaction.AttributeData) int {
		seen := map[uint16]bool{}
		for i := range as {
			seen[as[i].Path.Endpoint] = true
		}
		return len(seen)
	}
	paths := func(p im.ConcreteAttributePath, least, minEndpoints int) func(th *goTH) error {
		return func(th *goTH) error {
			as, err := readAll(th, p)
			if err != nil {
				return err
			}
			if len(as) < least || endpointsOf(as) < minEndpoints {
				return fmt.Errorf("read %s reported %d attributes on %d endpoints, want at least %d on %d", describePath(p), len(as), endpointsOf(as), least, minEndpoints)
			}
			th.note("read %s: %d attributes on %d endpoints, every one on the path", describePath(p), len(as), endpointsOf(as))
			return nil
		}
	}
	typed := func(cluster, attr uint32, is isType, kind string) func(th *goTH) error {
		return func(th *goTH) error {
			v, err := th.readValue(n, attrPath(th.ep, cluster, attr))
			if err != nil {
				return err
			}
			if !is(v) {
				return fmt.Errorf("%d/0x%04X/0x%04X reads %s (TLV type 0x%02X), not %s", th.ep, cluster, attr, describeNode(v), v.El.Type, kind)
			}
			th.note("%d/0x%04X/0x%04X reads %s, %s", th.ep, cluster, attr, describeNode(v), kind)
			return nil
		}
	}
	nEP := 0
	return goCertCase{
		tc: "TC-IDM-2.1", line: 168, pics: []string{"MCORE.IDM.C.ReadRequest"},
		steps: []goStep{
			{n: "1", line: 169, title: "read one attribute on a given cluster and endpoint", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				nEP = len(th.pics.sorted)
				return paths(attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorId), 1, 1)(th)
			}},
			{n: "2", line: 202, title: "read all attributes on a given cluster and endpoint", run: func(th *goTH) error {
				return paths(wildcard(ptr(uint16(0)), ptr(basicinfo.ClusterID), nil), 10, 1)(th)
			}},
			{n: "3", line: 232, title: "read all attributes in all clusters and all endpoints", run: func(th *goTH) error {
				return paths(wildcard(nil, nil, nil), 100, nEP)(th)
			}},
			{n: "4", line: 264, title: "read a specific attribute from all clusters on all endpoints", run: func(th *goTH) error {
				return paths(wildcard(nil, nil, ptr(uint32(0xFFFD))), nEP, nEP)(th)
			}},
			{n: "5", line: 291, title: "read all attributes from a specific cluster on all endpoints", run: func(th *goTH) error {
				return paths(wildcard(nil, ptr(descriptor.ClusterID), nil), nEP, nEP)(th)
			}},
			{n: "6", line: 319, title: "read a specific attribute from a specific cluster on all endpoints", run: func(th *goTH) error {
				return paths(wildcard(nil, ptr(descriptor.ClusterID), ptr(descriptor.AttrServerList)), nEP, nEP)(th)
			}},
			{n: "7", line: 350, title: "read all attributes from all clusters on a given endpoint", run: func(th *goTH) error {
				return paths(wildcard(ptr(uint16(1)), nil, nil), 10, 1)(th)
			}},
			{n: "8", line: 378, title: "read a specific attribute from all clusters on a given endpoint", run: func(th *goTH) error {
				return paths(wildcard(ptr(uint16(1)), nil, ptr(uint32(0xFFFD))), 2, 1)(th)
			}},
			{
				n: "9", line: 406, title: "read an attribute of data type bool", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_Bool",
				at: "OO.S", need: "OO.S.A0000", run: typed(onoff.ClusterID, onoff.AttrOnOff, isBool, "bool"),
			},
			{
				n: "10", line: 436, title: "read an attribute of data type string", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_String",
				at: "MOD.S", need: "MOD.S.A0000", run: typed(modeselect.ClusterID, modeselect.AttrDescription, isString, "string"),
			},
			{
				n: "11", line: 466, title: "read an attribute of data type unsigned integer", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_UnsignedInteger",
				at: "LVL.S", need: "LVL.S.A0000", run: typed(levelcontrol.ClusterID, levelcontrol.AttrCurrentLevel, isUnsigned, "unsigned integer"),
			},
			{
				// The TS reads PressureMeasurement.MeasuredValue (:501-503);
				// the reference composition has no pressure sensor, and its
				// temperature sensor's MeasuredValue is the int16 it has.
				n: "12", line: 496, title: "read an attribute of data type signed integer", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_SignedInteger",
				at: "TMP.S", need: "TMP.S.A0000", run: typed(temperaturemeasurement.ClusterID, temperaturemeasurement.AttrMeasuredValue, isSigned, "signed integer"),
			},
			{
				n: "13", line: 522, title: "read an attribute of data type floating point", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_FloatingPoint",
				at: "CDOCONC.S", need: "CDOCONC.S.A0000",
				run: typed(carbondioxideconcentrationmeasurement.ClusterID, carbondioxideconcentrationmeasurement.AttrMeasuredValue, isFloat, "floating point"),
			},
			{
				n: "14", line: 550, title: "read an attribute of data type octet string", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_OctetString",
				run: typed(operationalcredentials.ClusterID, operationalcredentials.AttrTrustedRootCertificates, isOctets, "a list of octet strings"),
			},
			{
				n: "15", line: 581, title: "read an attribute of data type struct", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_Struct",
				run: typed(generalcommissioning.ClusterID, generalcommissioning.AttrBasicCommissioningInfo, isStruct, "struct"),
			},
			{
				n: "16", line: 615, title: "read an attribute of data type list", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_List",
				at: "MOD.S", need: "MOD.S.A0002", run: typed(modeselect.ClusterID, modeselect.AttrSupportedModes, isList, "list"),
			},
			{
				n: "17", line: 645, title: "read an attribute of data type enum", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_Enum",
				at: "OCC.S", need: "OCC.S.A0001", run: typed(occupancysensing.ClusterID, occupancysensing.AttrOccupancySensorType, isUnsigned, "enum"),
			},
			{
				n: "18", line: 675, title: "read an attribute of data type bitmap", pics: "MCORE.IDM.C.ReadRequest.Attribute.DataType_Bitmap",
				at: "LVL.S", need: "LVL.S.A000f", run: typed(levelcontrol.ClusterID, levelcontrol.AttrOptions, isUnsigned, "bitmap"),
			},
			{n: "19", line: 701, title: "read an attribute three times", at: "OO.S", run: func(th *goTH) error {
				var seen []string
				for range 3 {
					v, err := th.readValue(n, attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff))
					if err != nil {
						return err
					}
					if !isBool(v) {
						return fmt.Errorf("OnOff reads %s, not a bool", describeNode(v))
					}
					seen = append(seen, describeNode(v))
				}
				if seen[0] != seen[1] || seen[1] != seen[2] {
					return fmt.Errorf("three reads of OnOff answered %v", seen)
				}
				th.note("three reads of %d/OnOff: %v", th.ep, seen)
				return nil
			}},
			{n: "20", line: 732, title: "read something larger than 1 MTU, chunked", run: func(th *goTH) error {
				// The TS's proxy for a report over one MTU (:745-760): more than
				// 100 attributes, which the client reassembles from its chunks.
				return paths(wildcard(nil, nil, nil), 101, nEP)(th)
			}},
			{n: "21", line: 775, title: "read with manufacturer-specific clusters and attributes", run: func(th *goTH) error {
				as, err := readAll(th, wildcard(nil, nil, nil))
				if err != nil {
					return err
				}
				for i := range as {
					if as[i].Path.Cluster >= 0x10000 {
						return fmt.Errorf("the daemon serves manufacturer-specific cluster 0x%08X: port the step's check", as[i].Path.Cluster)
					}
				}
				return goSkip("(b) not supported: the reference composition serves no manufacturer-specific cluster "+
					"(none >= 0x10000 among %d attributes), as matter.js accepts for its own all-clusters app (TC-IDM-2.1.test.ts:798-807)", len(as))
			}},
		},
	}
}

// caseIDM31 mirrors TC-IDM-3.1: the TH writes one attribute of each data
// type; the DUT answers Success and the value reads back.
func caseIDM31() goCertCase {
	var n *client.Node
	writeBack := func(cluster, attr uint32, v spec.Encodable, want string) func(th *goTH) error {
		return func(th *goTH) error {
			p := attrPath(th.ep, cluster, attr)
			if err := th.write(n, p, v, client.WriteOptions{}); err != nil {
				return err
			}
			got, err := th.readValue(n, p)
			if err != nil {
				return err
			}
			if describeNode(got) != want {
				return fmt.Errorf("%s reads %s after the write, want %s", describePath(p), describeNode(got), want)
			}
			th.note("wrote %s to %s: Success, reads back", want, describePath(p))
			return nil
		}
	}
	unitTesting := "the reference daemon has no UnitTesting cluster, as matter.js's all-clusters app has none (unitTestingNotApplicable, test/cert/unit-testing-cluster.ts:16-20)"
	labels := listOf(userlabel.LabelStruct{Label: "room", Value: "kitchen"}, userlabel.LabelStruct{Label: "floor", Value: "1"})
	return goCertCase{
		tc: "TC-IDM-3.1", line: 209, pics: []string{"MCORE.IDM.C.WriteRequest"},
		steps: []goStep{
			{n: "1", line: 210, title: "modify one attribute on a given cluster and endpoint", at: "LVL.S", need: "LVL.S.A0011", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				return writeBack(levelcontrol.ClusterID, levelcontrol.AttrOnLevel, spec.Uint8(2), "2")(th)
			}},
			{n: "2", line: 232, title: "modify one attribute on all endpoints", run: func(th *goTH) error {
				p := wildcard(nil, ptr(identify.ClusterID), ptr(identify.AttrIdentifyTime))
				st, err := n.Write(th.ctx, []client.WriteRequest{{Path: p, Value: spec.Uint16(5)}}, client.WriteOptions{})
				if err != nil {
					return err
				}
				var written []uint16
				for _, s := range st {
					if s.Status == im.StatusSuccess {
						written = append(written, s.Path.Endpoint)
					}
				}
				if len(written) <= 1 {
					return fmt.Errorf("a wildcard write of IdentifyTime succeeded on %d endpoints (%+v): it must reach more than one", len(written), st)
				}
				for _, ep := range written {
					v, err := th.readValue(n, attrPath(ep, identify.ClusterID, identify.AttrIdentifyTime))
					if err != nil {
						return err
					}
					if !isUnsigned(v) || v.El.Uint > 5 {
						// IdentifyTime counts down from the written value (:270-272).
						return fmt.Errorf("endpoint %d reports IdentifyTime %s after the write of 5", ep, describeNode(v))
					}
				}
				th.note("IdentifyTime written on %d endpoints, each reads at most 5", len(written))
				return nil
			}},
			{
				n: "3", line: 286, title: "write an attribute of data type bool", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_Bool",
				need: "BINFO.S.A0010", run: writeBack(basicinfo.ClusterID, basicinfo.AttrLocalConfigDisabled, spec.Bool(true), "true"),
			},
			{
				n: "4", line: 302, title: "write an attribute of data type string", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_String",
				need: "BINFO.S.A0005", run: writeBack(basicinfo.ClusterID, basicinfo.AttrNodeLabel, spec.String("node"), `"node"`),
			},
			{
				// The TS writes LevelControl.OnOffTransitionTime (:323-325),
				// which no light of the reference composition serves; its
				// StartUpCurrentLevel is the writable unsigned integer it has.
				n: "5", line: 318, title: "write an attribute of data type unsigned integer", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_UnsignedInteger",
				at: "LVL.S.A4000", need: "LVL.S.A4000", run: writeBack(levelcontrol.ClusterID, levelcontrol.AttrStartUpCurrentLevel, spec.Uint8(1), "1"),
			},
			{
				n: "6", line: 334, title: "write an attribute of data type signed integer", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_SignedInteger",
				at: "TSTAT.S", need: "TSTAT.S.A0010", run: writeBack(thermostat.ClusterID, thermostat.AttrLocalTemperatureCalibration, spec.Int8(-5), "-5"),
			},
			{n: "7", line: 350, title: "write an attribute of data type float", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_FloatingPoint", na: unitTesting},
			{n: "8", line: 372, title: "write an attribute of data type octet string", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_OctetString", na: unitTesting},
			{n: "9", line: 394, title: "write an attribute of data type struct", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_Struct", na: unitTesting},
			{
				n: "10", line: 408, title: "write an attribute of data type list", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_List",
				at: "ULABEL.S", need: "ULABEL.S.A0000", run: writeBack(userlabel.ClusterID, userlabel.AttrLabelList, labels, `[["room", "kitchen"], ["floor", "1"]]`),
			},
			{
				n: "11", line: 428, title: "write an attribute of data type enum", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_Enum",
				at: "TSUIC.S", need: "TSUIC.S.A0000",
				run: writeBack(thermostatuserinterfaceconfiguration.ClusterID, thermostatuserinterfaceconfiguration.AttrTemperatureDisplayMode, spec.Uint8(1), "1"),
			},
			{
				n: "12", line: 447, title: "write an attribute of data type bitmap", pics: "MCORE.IDM.C.WriteRequest.Attribute.DataType_Bitmap",
				at: "CC.S", need: "CC.S.A000f", run: writeBack(colorcontrol.ClusterID, colorcontrol.AttrOptions, spec.Uint8(1), "1"),
			},
			{n: "13", line: 463, title: "write a large list larger than 1 MTU", na: unitTesting},
			{n: "14", line: 492, title: "write one attribute three times", at: "TSUIC.S", need: "TSUIC.S.A0000", run: func(th *goTH) error {
				for range 3 {
					if err := writeBack(thermostatuserinterfaceconfiguration.ClusterID, thermostatuserinterfaceconfiguration.AttrTemperatureDisplayMode, spec.Uint8(1), "1")(th); err != nil {
						return err
					}
				}
				return nil
			}},
			{n: "15", line: 513, title: "write two clusters with the data versions read before", at: "LVL.S", need: "LVL.S.A0011 & BINFO.S.A0005", run: func(th *goTH) error {
				label := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
				level := attrPath(th.ep, levelcontrol.ClusterID, levelcontrol.AttrOnLevel)
				r, err := th.read(n, label, level)
				if err != nil {
					return err
				}
				versions := map[uint32]uint32{}
				for i := range r.Attributes {
					versions[r.Attributes[i].Path.Cluster] = r.Attributes[i].DataVersion
				}
				lv, ok1 := versions[basicinfo.ClusterID]
				ov, ok2 := versions[levelcontrol.ClusterID]
				if !ok1 || !ok2 {
					return fmt.Errorf("the read reported data versions %v, not both clusters'", versions)
				}
				st, err := n.Write(th.ctx, []client.WriteRequest{
					{Path: label, Value: spec.String("tc-idm-3-1-step-15"), DataVersion: &lv},
					{Path: level, Value: spec.Uint8(3), DataVersion: &ov},
				}, client.WriteOptions{})
				if err != nil {
					return err
				}
				if err := st.Err(); err != nil || len(st) != 2 {
					return fmt.Errorf("the version-conditional write answered %+v: %w", st, err)
				}
				// Beyond the plan, as the TS (:584-605): the stale version must
				// now be refused.
				st, err = n.Write(th.ctx, []client.WriteRequest{{Path: label, Value: spec.String("tc-idm-3-1-stale"), DataVersion: &lv}}, client.WriteOptions{})
				if err != nil {
					return err
				}
				if len(st) != 1 || st[0].Status != im.StatusDataVersionMismatch {
					return fmt.Errorf("a write with the stale data version answered %+v, want DATA_VERSION_MISMATCH", st)
				}
				got, err := th.readValue(n, label)
				if err != nil {
					return err
				}
				if got.El.String != "tc-idm-3-1-step-15" {
					return fmt.Errorf("NodeLabel reads %s after the writes", describeNode(got))
				}
				th.note("both clusters written at versions %d/%d; the stale version refused with DATA_VERSION_MISMATCH", lv, ov)
				return nil
			}},
		},
	}
}

// goReports collects a subscription's reports for one attribute.
type goReports struct {
	mu   sync.Mutex
	seen []string
	ch   chan struct{}
}

func newGoReports() *goReports { return &goReports{ch: make(chan struct{}, 64)} }

func (r *goReports) add(v string) {
	r.mu.Lock()
	r.seen = append(r.seen, v)
	r.mu.Unlock()
	select {
	case r.ch <- struct{}{}:
	default:
	}
}

func (r *goReports) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.seen)
}

// The intervals of TC-IDM-4.1 (tc-idm-4.1-support.ts:27-28).
const (
	idm41MinFloor   = 10 * time.Second
	idm41MaxCeiling = 80 * time.Second
	// idm41ReportWait is the TS's REPORT_WAIT_TIMEOUT (:35).
	idm41ReportWait = idm41MinFloor + 20*time.Second
)

// subscribeOne subscribes to attribute paths with the given bounds and
// collects each path's reported values.
func (th *goTH) subscribeOne(n *client.Node, minFloor, maxCeiling time.Duration, paths ...im.ConcreteAttributePath) (sub *client.Subscription, reports map[im.ConcreteAttributePath]*goReports, err error) {
	reports = map[im.ConcreteAttributePath]*goReports{}
	for _, p := range paths {
		reports[p] = newGoReports()
	}
	sub, err = n.Subscribe(th.ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
		Attributes: paths, MinIntervalFloor: interaction.Interval(minFloor), MaxIntervalCeiling: interaction.Interval(maxCeiling),
		Updated: func(r *client.Report) {
			for _, p := range paths {
				if v, ok := r.Value(p.Endpoint, p.Cluster, p.Attribute); ok {
					reports[p].add(describeNode(v))
				}
			}
		},
	}})
	if err != nil {
		return nil, nil, err
	}
	th.t.Cleanup(sub.Close)
	if sub.ID() == 0 {
		return nil, nil, errors.New("the SubscribeResponse carried no subscription id")
	}
	if mi := sub.MaxInterval(); mi < minFloor || mi > max(time.Hour, maxCeiling) {
		return nil, nil, fmt.Errorf("the SubscribeResponse's MaxInterval %s is outside [%s, %s]", mi, minFloor, max(time.Hour, maxCeiling))
	}
	for _, p := range paths {
		if len(reports[p].values()) == 0 {
			return nil, nil, fmt.Errorf("the priming report carried no value for %s", describePath(p))
		}
	}
	return sub, reports, nil
}

// subscribeAndModify is tc-idm-4.1-support.ts:102 subscribeAndModify:
// subscribe, write each value, and hold the DUT to a report of each.
func (th *goTH) subscribeAndModify(n *client.Node, p im.ConcreteAttributePath, values []spec.Encodable, want []string) error {
	sub, reports, err := th.subscribeOne(n, idm41MinFloor, idm41MaxCeiling, p)
	if err != nil {
		return err
	}
	defer sub.Close()
	r := reports[p]
	for i, v := range values {
		if err := th.write(n, p, v, client.WriteOptions{}); err != nil {
			return err
		}
		ok := waitFor(th.ctx, idm41ReportWait, func() bool { return slices.Contains(r.values(), want[i]) })
		if !ok {
			return fmt.Errorf("no report of %s = %s within %s of the write; reports %v", describePath(p), want[i], idm41ReportWait, r.values())
		}
	}
	th.note("subscription %d on %s (MaxInterval %s) reported %v", sub.ID(), describePath(p), sub.MaxInterval(), r.values())
	return nil
}

// caseIDM41 mirrors TC-IDM-4.1: the TH subscribes; the DUT answers with a
// priming report and a SubscribeResponse within the bounds, and reports
// every change of the subscribed attribute.
func caseIDM41() goCertCase {
	var n *client.Node
	unitTesting := "the reference daemon has no UnitTesting cluster, as matter.js's all-clusters app has none (unitTestingNotApplicable, test/cert/unit-testing-cluster.ts:16-20)"
	modify := func(cluster, attr uint32, values []spec.Encodable, want []string) func(th *goTH) error {
		return func(th *goTH) error { return th.subscribeAndModify(n, attrPath(th.ep, cluster, attr), values, want) }
	}
	return goCertCase{
		tc: "TC-IDM-4.1", line: 88, pics: []string{"MCORE.IDM.C.SubscribeRequest"},
		steps: []goStep{
			{n: "1", line: 93, title: "subscription request for an attribute", pics: "MCORE.IDM.C.SubscribeRequest", at: "OO.S", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				sub, _, err := th.subscribeOne(n, idm41MinFloor, idm41MaxCeiling, attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff))
				if err != nil {
					return err
				}
				th.note("subscription %d, MaxInterval %s", sub.ID(), sub.MaxInterval())
				sub.Close()
				return nil
			}},
			{n: "2", line: 161, title: "TH sends a report data, DUT acknowledges", pics: "MCORE.IDM.C.SubscribeRequest", at: "OO.S", run: func(th *goTH) error {
				sub, reports, err := th.subscribeOne(n, idm41MinFloor, idm41MaxCeiling, attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff))
				if err != nil {
					return err
				}
				defer sub.Close()
				th.note("subscription %d primed with %v", sub.ID(), reports[attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff)].values())
				return nil
			}},
			{
				n: "3", line: 195, title: "subscription to a bool attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_Bool", need: "BINFO.S.A0010",
				run: modify(basicinfo.ClusterID, basicinfo.AttrLocalConfigDisabled, []spec.Encodable{spec.Bool(true), spec.Bool(false), spec.Bool(true)}, []string{"true", "false", "true"}),
			},
			{
				n: "4", line: 215, title: "subscription to a string attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_String", need: "BINFO.S.A0005",
				run: modify(basicinfo.ClusterID, basicinfo.AttrNodeLabel, []spec.Encodable{spec.String("tc-idm-4-1-a"), spec.String("tc-idm-4-1-b"), spec.String("tc-idm-4-1-c")},
					[]string{`"tc-idm-4-1-a"`, `"tc-idm-4-1-b"`, `"tc-idm-4-1-c"`}),
			},
			{
				n: "5", line: 235, title: "subscription to an unsigned integer attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_UnsignedInteger",
				// OnOffTransitionTime in the TS (:242-244); StartUpCurrentLevel
				// here, as in TC-IDM-3.1 step 5.
				at: "LVL.S.A4000", need: "LVL.S.A4000",
				run: modify(levelcontrol.ClusterID, levelcontrol.AttrStartUpCurrentLevel, []spec.Encodable{spec.Uint8(1), spec.Uint8(2), spec.Uint8(3)}, []string{"1", "2", "3"}),
			},
			{
				n: "6", line: 255, title: "subscription to a signed integer attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_Integer",
				at: "TSTAT.S", need: "TSTAT.S.A0010",
				run: modify(thermostat.ClusterID, thermostat.AttrLocalTemperatureCalibration, []spec.Encodable{spec.Int8(-7), spec.Int8(-12), spec.Int8(-15)}, []string{"-7", "-12", "-15"}),
			},
			{n: "7", line: 275, title: "subscription to a float attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_FloatingPoint", na: unitTesting},
			{
				n: "8", line: 297, title: "subscription to a list attribute, modified", pics: "MCORE.IDM.C.SubscribeRequest.Attribute.DataType_List",
				at: "ULABEL.S", need: "ULABEL.S.A0000",
				run: modify(userlabel.ClusterID, userlabel.AttrLabelList, []spec.Encodable{
					listOf(userlabel.LabelStruct{Label: "a", Value: "1"}),
					listOf(userlabel.LabelStruct{Label: "b", Value: "2"}),
					listOf(userlabel.LabelStruct{Label: "c", Value: "3"}),
				}, []string{`[["a", "1"]]`, `[["b", "2"]]`, `[["c", "3"]]`}),
			},
			{n: "9", line: 320, title: "TH withholds reports for the maximum interval", na: "Not verifiable/Out of scope in CHIP's certification harness: " +
				"forcing the TH to withhold reports and replay an expired subscription id has no defined mechanism (TC-IDM-4.1.test.ts:327-330)"},
			{
				n: "10", line: 332, title: "subscription to multiple attributes", pics: "MCORE.IDM.C.SubscribeRequest.MultipleAttributes",
				at: "OO.S & LVL.S.A0001 & I.S.A0000", run: func(th *goTH) error {
					ps := []im.ConcreteAttributePath{
						attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff),
						attrPath(th.ep, levelcontrol.ClusterID, levelcontrol.AttrRemainingTime),
						attrPath(th.ep, identify.ClusterID, identify.AttrIdentifyTime),
					}
					sub, _, err := th.subscribeOne(n, idm41MinFloor, idm41MaxCeiling, ps...)
					if err != nil {
						return err
					}
					defer sub.Close()
					th.note("subscription %d primed every one of OnOff, RemainingTime, IdentifyTime on endpoint %d", sub.ID(), th.ep)
					return nil
				},
			},
		},
	}
}

// caseIDM51 mirrors TC-IDM-5.1: a timed invoke and a timed write.
func caseIDM51() goCertCase {
	var n *client.Node
	const timeout = 2 * time.Second // TIMED_INTERACTION_TIMEOUT (TC-IDM-5.1.test.ts:25)
	return goCertCase{
		tc: "TC-IDM-5.1", line: 48, pics: []string{"MCORE.IDM.C"}, at: "OO.S",
		steps: []goStep{
			{n: "1", line: 49, title: "Timed Request, then an Invoke Request", pics: "MCORE.IDM.C.InvokeRequest", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, onoff.ClusterID, onoff.CmdOn), nil, false, client.InvokeOptions{Timed: true, TimedTimeout: timeout}); err != nil {
					return err
				}
				v, err := th.readValue(n, attrPath(th.ep, onoff.ClusterID, onoff.AttrOnOff))
				if err != nil || !v.El.Bool {
					return fmt.Errorf("OnOff reads %s after the timed On: %w", describeNode(v), err)
				}
				th.note("timed On (timeout %s) on endpoint %d: Success, OnOff=true", timeout, th.ep)
				return nil
			}},
			{n: "2", line: 84, title: "Timed Request, then a WriteRequestMessage", pics: "MCORE.IDM.C.WriteRequest", need: "OO.S.A4001", run: func(th *goTH) error {
				p := attrPath(th.ep, onoff.ClusterID, onoff.AttrOnTime)
				if err := th.write(n, p, spec.Uint16(2), client.WriteOptions{Timed: true, TimedTimeout: timeout}); err != nil {
					return err
				}
				v, err := th.readValue(n, p)
				if err != nil || v.El.Uint != 2 {
					return fmt.Errorf("OnTime reads %s after the timed write of 2: %w", describeNode(v), err)
				}
				th.note("timed write of OnTime=2 (timeout %s): Success, reads back", timeout)
				return nil
			}},
			{
				n: "3", line: 115, title: "Timed Request the TH does not answer", pics: "MCORE.IDM.C.WriteRequest | MCORE.IDM.C.InvokeRequest",
				na: "Not testable / Out of Scope in CHIP's certification harness (TC-IDM-5.1.test.ts:123)",
			},
		},
	}
}

// startUpEvent is the event TC-IDM-6.3 and 6.4 read: BasicInformation
// StartUp on the root (TC-IDM-6.3.test.ts:40-42).
var startUpEvent = im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: basicinfo.EventStartUp, HasEndpoint: true, HasCluster: true, HasEvent: true}

// caseIDM63 mirrors TC-IDM-6.3: the TH reads a supported event.
func caseIDM63() goCertCase {
	return goCertCase{
		tc: "TC-IDM-6.3", line: 46, pics: []string{"MCORE.IDM.C", "MCORE.IDM.C.ReadRequest", "MCORE.IDM.C.ReadEvent"},
		steps: []goStep{
			{n: "1", line: 51, title: "Read Request Message for a supported event", run: func(th *goTH) error {
				_, n, err := th.commission("dut")
				if err != nil {
					return err
				}
				r, err := n.Read(th.ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Events: []im.ConcreteEventPath{startUpEvent}}, IncludeKnownVersions: true})
				if err != nil {
					return err
				}
				for i := range r.Events {
					e := &r.Events[i]
					if e.Path.Endpoint != 0 || e.Path.Cluster != basicinfo.ClusterID || e.Path.Event != basicinfo.EventStartUp {
						return fmt.Errorf("the read returned event %+v outside the requested path", e.Path)
					}
				}
				// Beyond the TS, which accepts an empty answer (:73-75): the
				// daemon emitted StartUp when it started, so the record is there.
				if len(r.Events) == 0 {
					return errors.New("the read of BasicInformation.StartUp returned no event; the daemon emits one at start")
				}
				th.note("read of 0/BasicInformation/StartUp: %d event(s), every one on the path", len(r.Events))
				return nil
			}},
		},
	}
}

// caseIDM64 mirrors TC-IDM-6.4: the TH subscribes to a supported event.
func caseIDM64() goCertCase {
	var n *client.Node
	subscribe := func(th *goTH, minFloor, maxCeiling time.Duration) error {
		primed := make(chan uint32, 1)
		sub, err := n.Subscribe(th.ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
			Events: []im.ConcreteEventPath{startUpEvent}, MinIntervalFloor: interaction.Interval(minFloor), MaxIntervalCeiling: interaction.Interval(maxCeiling),
			Updated: func(r *client.Report) {
				select {
				case primed <- r.SubscriptionID:
				default:
				}
			},
		}})
		if err != nil {
			return err
		}
		defer sub.Close()
		if sub.ID() == 0 {
			return errors.New("the SubscribeResponse carried no subscription id")
		}
		if mi := sub.MaxInterval(); mi < minFloor || mi > max(time.Hour, maxCeiling) {
			return fmt.Errorf("MaxInterval %s is outside [%s, %s]", mi, minFloor, max(time.Hour, maxCeiling))
		}
		th.note("event subscription %d with %s/%s: MaxInterval %s", sub.ID(), minFloor, maxCeiling, sub.MaxInterval())
		select {
		case id := <-primed:
			if id != sub.ID() {
				return fmt.Errorf("the priming report names subscription %d, the response %d", id, sub.ID())
			}
			th.note("priming report for subscription %d", id)
		default:
			// The priming report of an event subscription may carry no event
			// (TC-IDM-6.4.test.ts:161-162); Subscribe returned, so it arrived.
		}
		return nil
	}
	notTestable := "Not testable / Out of Scope for V1.0 in CHIP's certification harness"
	return goCertCase{
		tc: "TC-IDM-6.4", line: 64, pics: []string{"MCORE.IDM.C", "MCORE.IDM.C.SubscribeRequest", "MCORE.IDM.C.SubscribeEvent"},
		steps: []goStep{
			{n: "1", line: 69, title: "Subscribe Request Message for a supported event", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				return subscribe(th, 10*time.Second, 100*time.Second) // STEP_1 (:38-39)
			}},
			{n: "2", line: 128, title: "TH sends Report Data, DUT answers Status Response", run: func(th *goTH) error {
				return subscribe(th, 20*time.Second, 400*time.Second) // STEP_2 (:40-41)
			}},
			{n: "3", line: 172, title: "TH does not respond with Report Data", na: notTestable + " (TC-IDM-6.4.test.ts:176)"},
			{n: "4", line: 178, title: "TH does not send a Subscribe Response", na: notTestable + " (TC-IDM-6.4.test.ts:183)"},
			{n: "5", line: 185, title: "TH sends Report Data with an invalid SubscriptionId", na: notTestable + " (TC-IDM-6.4.test.ts:190)"},
			{n: "6", line: 192, title: "TH sends Report Data after the max interval", na: notTestable + " (TC-IDM-6.4.test.ts:197)"},
		},
	}
}
