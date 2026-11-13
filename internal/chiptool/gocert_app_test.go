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
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/actions"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The application-cluster cases and CADMIN-1.17: matter.js's TS files
// whose DUT is a cluster client (or a commissioner), read mirrored onto the
// reference daemon's servers.

func init() {
	for _, c := range []func() goCertCase{caseCADMIN117, caseACT32, caseG32, caseLVL81, caseS31, caseSWTCH32} {
		registerGoCert(c)
	}
}

// readFabrics reads OperationalCredentials.Fabrics unfiltered
// (tc-cadmin-support.ts:471-478) and returns the fabric indices.
func (th *goTH) readFabrics(n *client.Node) ([]uint8, error) {
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	p := attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrFabrics)
	r, err := n.Read(ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Attributes: []im.ConcreteAttributePath{p}, NoFabricFilter: true}, IncludeKnownVersions: true})
	if err != nil {
		return nil, err
	}
	v, ok := r.Value(0, operationalcredentials.ClusterID, operationalcredentials.AttrFabrics)
	if !ok {
		return nil, errors.New("the Fabrics read reported no value")
	}
	var out []uint8
	for _, c := range v.Children {
		var f operationalcredentials.FabricDescriptorStruct
		if err := f.DecodeTLV(c); err != nil {
			return nil, err
		}
		out = append(out, f.FabricIndex)
	}
	return out, nil
}

// caseCADMIN117 mirrors TC-CADMIN-1.17: three controllers commission the
// daemon through enhanced commissioning windows; one fabric is removed and
// the daemon refuses it, keeps the others' records and admits it again.
func caseCADMIN117() goCertCase {
	type role struct {
		ctrl *gocontroller.Controller
		node *client.Node
		inst string
	}
	var (
		cr       [3]role
		names    *discovery.Names
		window   string
		cr2Index uint8
	)
	open := func(th *goTH) error {
		ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
		defer cancel()
		w, err := cr[0].node.OpenCommissioningWindow(ctx, 180*time.Second, client.WindowOptions{}) // CW_DURATION_SECONDS (:20)
		if err != nil {
			return err
		}
		window = w.ManualCode
		th.note("enhanced commissioning window open for %s, manual code %s", w.Timeout, w.ManualCode)
		return nil
	}
	join := func(i int) func(th *goTH) error {
		return func(th *goTH) error {
			if window == "" {
				return errors.New("no commissioning window was opened")
			}
			var err error
			if cr[i].node != nil {
				// A TH whose fabric the device removed has lost the node: it
				// forgets it before commissioning again. The registry finds a
				// discovered device by its commissionable instance, which the
				// daemon keeps across windows, and refuses a node it still
				// counts as commissioned (client ErrAlreadyCommissioned).
				if err := cr[i].node.Erase(th.ctx); err != nil {
					return fmt.Errorf("forget the lost node: %w", err)
				}
				cr[i].node = nil
			}
			if cr[i].ctrl == nil {
				if cr[i].ctrl, err = th.controller(fmt.Sprintf("th_cr%d", i+1)); err != nil {
					return err
				}
			}
			if cr[i].node, err = th.pair(cr[i].ctrl, window); err != nil {
				return err
			}
			window = ""
			addr, _ := cr[i].node.PeerAddress()
			cr[i].inst = operationalInstance(cr[i].ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
			th.note("commissioned by controller %d as %s", i+1, cr[i].inst)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-CADMIN-1.17", line: 156, pics: []string{"CADMIN.C", "CADMIN.C.C00.Tx"},
		steps: []goStep{
			{n: "1", line: 162, title: "DUT_CR1 commissions TH_CE", pics: "CADMIN.C", run: func(th *goTH) error {
				var err error
				if names, err = th.names(); err != nil {
					return err
				}
				if cr[0].ctrl, cr[0].node, err = th.commission("dut_cr1"); err != nil {
					return err
				}
				addr, _ := cr[0].node.PeerAddress()
				cr[0].inst = operationalInstance(cr[0].ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
				th.note("commissioned by DUT_CR1 as %s", cr[0].inst)
				return nil
			}},
			{n: "2", line: 179, title: "DUT_CR1 opens a commissioning window on TH_CE", pics: "CADMIN.C.C00.Tx", run: open},
			{n: "3", line: 185, title: "TH_CR2 commissions TH_CE", pics: "CADMIN.C", run: join(1)},
			{n: "4", line: 200, title: "DUT_CR1 opens a commissioning window on TH_CE", pics: "CADMIN.C.C00.Tx", run: open},
			{n: "5", line: 206, title: "TH_CR3 commissions TH_CE", pics: "CADMIN.C", run: join(2)},
			{n: "6", line: 221, title: "DUT_CR1 reads the list of Fabrics", pics: "OPCREDS.C.A0001", run: func(th *goTH) error {
				fabrics, err := th.readFabrics(cr[0].node)
				if err != nil {
					return err
				}
				v, err := th.readValue(cr[1].node, attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrCurrentFabricIndex))
				if err != nil {
					return err
				}
				cr2Index = uint8(v.El.Uint) //nolint:gosec // a fabric index
				if len(fabrics) != 3 || !slices.Contains(fabrics, cr2Index) {
					return fmt.Errorf("Fabrics lists %v; TH_CR2's index is %d", fabrics, cr2Index)
				}
				th.note("Fabrics lists %v; TH_CR2 holds index %d", fabrics, cr2Index)
				return nil
			}},
			{n: "7", line: 249, title: "DUT_CR1 sends RemoveFabric for TH_CR2's fabric", pics: "OPCREDS.C.C0a.Tx", run: func(th *goTH) error {
				if cr2Index != 2 { // EXPECTED_CR2_FABRIC_INDEX (tc-cadmin-support.ts:645)
					return fmt.Errorf("TH_CR2's fabric index is %d, the plan assumes 2 for a dut→th_cr2→th_cr3 order", cr2Index)
				}
				r, err := th.invoke(cr[0].node, cmdPath(0, operationalcredentials.ClusterID, operationalcredentials.CmdRemoveFabric),
					operationalcredentials.RemoveFabricRequest{FabricIndex: cr2Index}, true, client.InvokeOptions{})
				if err != nil {
					return err
				}
				if idx, ok := structField(r.Fields, 1); ok && idx.El.Uint != uint64(cr2Index) {
					return fmt.Errorf("NOCResponse names fabric index %s", describeNode(idx))
				}
				th.note("RemoveFabric %d: NOCResponse StatusCode OK", cr2Index)
				return nil
			}},
			{n: "8", line: 328, title: "TH_CR2 writes and reads NodeLabel", pics: "BINFO.C.A0005", run: func(th *goTH) error {
				p := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
				ctx, cancel := context.WithTimeout(th.ctx, 60*time.Second) // POST_REMOVAL_TIMEOUT (:27)
				defer cancel()
				st, werr := cr[1].node.Write(ctx, []client.WriteRequest{{Path: p, Value: spec.String("post-removal")}}, client.WriteOptions{})
				if werr == nil && st.Err() == nil {
					return errors.New("TH_CR2's write of NodeLabel succeeded after its fabric was removed")
				}
				_, rerr := cr[1].node.Read(ctx, client.ReadRequest{ReadRequest: readOf(p), IncludeKnownVersions: true})
				if rerr == nil {
					return errors.New("TH_CR2's read of NodeLabel succeeded after its fabric was removed")
				}
				th.note("TH_CR2's write (%v) and read (%v) both refused", firstLine(werr, st.Err()), firstLine(rerr, nil))
				return nil
			}},
			{n: "9", line: 369, title: "DUT_CR1 reads the list of Fabrics", pics: "OPCREDS.C.A0001", run: func(th *goTH) error {
				fabrics, err := th.readFabrics(cr[0].node)
				if err != nil {
					return err
				}
				if len(fabrics) != 2 || slices.Contains(fabrics, cr2Index) {
					return fmt.Errorf("Fabrics lists %v after removing %d", fabrics, cr2Index)
				}
				th.note("Fabrics lists %v", fabrics)
				return nil
			}},
			{n: "10", line: 398, title: "TH_CE discoverable with 2 operational records", run: func(th *goTH) error {
				for _, i := range []int{0, 2} {
					if err := th.awaitOperational(names, cr[i].inst, true, 30*time.Second); err != nil {
						return err
					}
				}
				if err := th.awaitOperational(names, cr[1].inst, false, 30*time.Second); err != nil {
					return err
				}
				th.note("%s and %s advertised, %s withdrawn", cr[0].inst, cr[2].inst, cr[1].inst)
				return nil
			}},
			{n: "11", line: 441, title: "DUT_CR1 opens a commissioning window on TH_CE", pics: "CADMIN.C.C00.Tx", run: open},
			{n: "12", line: 451, title: "TH_CR2 commissions TH_CE", pics: "CADMIN.C", run: join(1)},
			{n: "13", line: 466, title: "TH_CR2 reads the list of Fabrics", pics: "OPCREDS.C.A0001", run: func(th *goTH) error {
				fabrics, err := th.readFabrics(cr[1].node)
				if err != nil {
					return err
				}
				if len(fabrics) != 3 {
					return fmt.Errorf("Fabrics lists %v after TH_CR2 commissioned again, want 3", fabrics)
				}
				th.note("Fabrics lists %v", fabrics)
				return nil
			}},
		},
	}
}

// firstLine is an error's first line, or the fallback's.
func firstLine(err, fallback error) string {
	if err == nil {
		err = fallback
	}
	if err == nil {
		return "no error"
	}
	return strings.SplitN(err.Error(), "\n", 2)[0]
}

// actionFields encodes an Actions command: ActionID, InvokeID and the
// command's third field when it has one (Duration or TransitionTime).
func actionFields(actionID uint16, invokeID uint32, third *uint32, thirdIs16 bool) spec.Encodable {
	return encodable(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		spec.PutUint[uint16](enc, tlv.ContextTag(0), actionID)
		spec.PutUint[uint32](enc, tlv.ContextTag(1), invokeID)
		if third != nil {
			if thirdIs16 {
				spec.PutUint[uint16](enc, tlv.ContextTag(2), uint16(*third)) //nolint:gosec // a uint16 field
			} else {
				spec.PutUint[uint32](enc, tlv.ContextTag(2), *third)
			}
		}
		_ = enc.EndContainer()
	})
}

// caseACT32 mirrors TC-ACT-3.2: the TH issues every Actions command to the
// bridge; the DUT answers each with a status. Any status passes, as the TS
// accepts any (invokeAndCheck, :49-63, anyStatus).
func caseACT32() goCertCase {
	var (
		n        *client.Node
		actionID uint16 = 0x1001 // ACTION_ID (TC-ACT-3.2.test.ts:15)
	)
	const (
		duration       uint32 = 100_000 // DURATION (:21)
		transitionTime uint32 = 1_234   // TRANSITION_TIME (:22)
	)
	issue := func(step int, cmd uint32, third *uint32, thirdIs16 bool) func(th *goTH) error {
		return func(th *goTH) error {
			ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
			defer cancel()
			_, err := n.Invoke(ctx, cmdPath(th.ep, actions.ClusterID, cmd), actionFields(actionID, uint32(700_000+step), third, thirdIs16), client.InvokeOptions{}) //nolint:gosec // invokeIdFor (:27-29)
			var se *interaction.StatusError
			switch {
			case err == nil:
				th.note("command 0x%02X for action 0x%04X on endpoint %d: Success", cmd, actionID, th.ep)
			case errors.As(err, &se):
				th.note("command 0x%02X for action 0x%04X on endpoint %d: status %s", cmd, actionID, th.ep, se.Status)
			default:
				return fmt.Errorf("command 0x%02X drew no status: %w", cmd, err)
			}
			return nil
		}
	}
	d, tt := duration, transitionTime
	return goCertCase{
		tc: "TC-ACT-3.2", line: 71, pics: []string{"ACT.C"}, at: "ACT.S",
		steps: []goStep{
			{n: "1", line: 72, title: "InstantAction", pics: "ACT.C.C00.Tx", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				// The bridge's own first action, where it lists one; the TS's
				// chip bridge app lists 0x1001.
				v, err := th.readValue(n, attrPath(th.ep, actions.ClusterID, actions.AttrActionList))
				if err != nil {
					return err
				}
				if len(v.Children) > 0 {
					if id, ok := structField(v.Children[0], 0); ok {
						actionID = uint16(id.El.Uint) //nolint:gosec // an action id
					}
				}
				th.note("ActionList holds %d action(s); the steps address 0x%04X", len(v.Children), actionID)
				return issue(1, actions.CmdInstantAction, nil, false)(th)
			}},
			{n: "2", line: 94, title: "StartAction", pics: "ACT.C.C02.Tx", run: issue(2, actions.CmdStartAction, nil, false)},
			{n: "3", line: 105, title: "StopAction", pics: "ACT.C.C04.Tx", run: issue(3, actions.CmdStopAction, nil, false)},
			{n: "4", line: 116, title: "PauseAction", pics: "ACT.C.C05.Tx", run: issue(4, actions.CmdPauseAction, nil, false)},
			{n: "5", line: 127, title: "ResumeAction", pics: "ACT.C.C07.Tx", run: issue(5, actions.CmdResumeAction, nil, false)},
			{n: "6", line: 138, title: "EnableAction", pics: "ACT.C.C08.Tx", run: issue(6, actions.CmdEnableAction, nil, false)},
			{n: "7", line: 149, title: "DisableAction", pics: "ACT.C.C0a.Tx", run: issue(7, actions.CmdDisableAction, nil, false)},
			{n: "8", line: 160, title: "StartActionWithDuration", pics: "ACT.C.C03.Tx", run: issue(8, actions.CmdStartActionWithDuration, &d, false)},
			{n: "9", line: 172, title: "PauseActionWithDuration", pics: "ACT.C.C06.Tx", run: issue(9, actions.CmdPauseActionWithDuration, &d, false)},
			{n: "10", line: 184, title: "EnableActionWithDuration", pics: "ACT.C.C09.Tx", run: issue(10, actions.CmdEnableActionWithDuration, &d, false)},
			{n: "11", line: 196, title: "DisableActionWithDuration", pics: "ACT.C.C0b.Tx", run: issue(11, actions.CmdDisableActionWithDuration, &d, false)},
			{n: "12", line: 208, title: "InstantActionWithTransition", pics: "ACT.C.C01.Tx", run: issue(12, actions.CmdInstantActionWithTransition, &tt, true)},
		},
	}
}

// caseG32 mirrors TC-G-3.2: the Groups commands on a light.
func caseG32() goCertCase {
	var n *client.Node
	const (
		keySet      uint16 = 1      // GROUP_KEY_SET_ID (TC-G-3.2.test.ts:22)
		removed     uint16 = 0x0002 // REMOVED_GROUP (:26)
		identifying uint16 = 0x0003 // IDENTIFYING_GROUP (:27)
		identifyFor uint16 = 0x0078 // IDENTIFY_TIME (:29)
	)
	start := epochUs(1_600_000_000_000_000)
	keySetOf := func() groupkeymanagement.GroupKeySetStruct { // groupKeySet (:64-76)
		k := func(first byte) []byte {
			return []byte{first, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf}
		}
		return groupkeymanagement.GroupKeySetStruct{
			GroupKeySetId: keySet, GroupKeySecurityPolicy: 0,
			EpochKey0: spec.ValueOf(k(0xd0)), EpochStartTime0: spec.ValueOf(start),
			EpochKey1: spec.ValueOf(k(0xd1)), EpochStartTime1: spec.ValueOf(start + 1_000_000),
			EpochKey2: spec.ValueOf(k(0xd2)), EpochStartTime2: spec.ValueOf(start + 2_000_000),
		}
	}
	return goCertCase{
		tc: "TC-G-3.2", line: 87, pics: []string{"G.C", "GRPKEY.C"}, at: "G.S & I.S",
		steps: []goStep{
			{n: "0", line: 99, title: "Preconditions: key set, GroupKeyMap and two groups", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetWrite),
					groupkeymanagement.KeySetWriteRequest{GroupKeySet: keySetOf()}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				if err := th.write(n, attrPath(0, groupkeymanagement.ClusterID, groupkeymanagement.AttrGroupKeyMap), listOf(
					groupkeymanagement.GroupKeyMapStruct{GroupId: removed, GroupKeySetId: keySet},
					groupkeymanagement.GroupKeyMapStruct{GroupId: identifying, GroupKeySetId: keySet},
				), client.WriteOptions{}); err != nil {
					return err
				}
				for _, g := range []groups.AddGroupRequest{{GroupId: removed, GroupName: "gp2"}, {GroupId: identifying, GroupName: "gp3"}} {
					if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdAddGroup), g, true, client.InvokeOptions{}); err != nil {
						return err
					}
				}
				th.note("key set %d written and bound to groups %d and %d, both added on endpoint %d", keySet, removed, identifying, th.ep)
				return nil
			}},
			{n: "1", line: 161, title: "GetGroupMembership", pics: "G.C.C02.Tx", run: func(th *goTH) error {
				r, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdGetGroupMembership), groups.GetGroupMembershipRequest{GroupList: []uint16{}}, false, client.InvokeOptions{})
				if err != nil {
					return err
				}
				list, _ := structField(r.Fields, 1)
				var ids []uint16
				for _, c := range list.Children {
					ids = append(ids, uint16(c.El.Uint)) //nolint:gosec // a group id
				}
				if !slices.Contains(ids, removed) || !slices.Contains(ids, identifying) {
					return fmt.Errorf("GetGroupMembershipResponse names groups %v, without %d or %d", ids, removed, identifying)
				}
				th.note("GetGroupMembershipResponse names groups %v", ids)
				return nil
			}},
			{n: "2", line: 188, title: "RemoveGroup", pics: "G.C.C03.Tx", run: func(th *goTH) error {
				if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdRemoveGroup), groups.RemoveGroupRequest{GroupId: removed}, true, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("RemoveGroup %d: SUCCESS", removed)
				return nil
			}},
			{n: "3", line: 199, title: "RemoveAllGroups", pics: "G.C.C04.Tx", run: func(th *goTH) error {
				if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdRemoveAllGroups), groups.RemoveAllGroupsRequest{}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("RemoveAllGroups: Success")
				return nil
			}},
			{n: "4", line: 208, title: "AddGroupIfIdentifying", pics: "G.C.C05.Tx", run: func(th *goTH) error {
				if _, err := th.invoke(n, cmdPath(th.ep, identify.ClusterID, identify.CmdIdentify), identify.IdentifyRequest{IdentifyTime: identifyFor}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdAddGroupIfIdentifying),
					groups.AddGroupIfIdentifyingRequest{GroupId: identifying, GroupName: "gp3"}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				// Beyond the TS: the identifying endpoint now holds the group.
				r, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdGetGroupMembership), groups.GetGroupMembershipRequest{GroupList: []uint16{identifying}}, false, client.InvokeOptions{})
				if err != nil {
					return err
				}
				list, _ := structField(r.Fields, 1)
				if len(list.Children) != 1 || list.Children[0].El.Uint != uint64(identifying) {
					return fmt.Errorf("after AddGroupIfIdentifying the endpoint is in groups %s", describeNode(list))
				}
				th.note("Identify %ds, then AddGroupIfIdentifying %d: Success; the endpoint is in the group", identifyFor, identifying)
				return nil
			}},
		},
	}
}

// caseLVL81 mirrors TC-LVL-8.1: the TH reads the level bounds and sends
// LevelControl commands within them; the DUT moves where it is told.
func caseLVL81() goCertCase {
	var (
		n        *client.Node
		lo, hi   uint8
		lighting bool
	)
	readU8 := func(th *goTH, attr uint32) (uint8, error) {
		v, err := th.readValue(n, attrPath(th.ep, levelcontrol.ClusterID, attr))
		if err != nil {
			return 0, err
		}
		if v.El.IsNull || !isUnsigned(v) {
			return 0, fmt.Errorf("LevelControl 0x%04X reads %s, not a number", attr, describeNode(v))
		}
		return uint8(v.El.Uint), nil //nolint:gosec // a uint8 attribute
	}
	zero := spec.ValueOf(uint16(0)) // NO_TRANSITION (TC-LVL-8.1.test.ts:31)
	return goCertCase{
		tc: "TC-LVL-8.1", line: 161, pics: []string{"LVL.C"}, at: "LVL.S & OO.S",
		steps: []goStep{
			{n: "0", line: 162, title: "the DUT commissions the TH and reads the levels it accepts", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				if lo, err = readU8(th, levelcontrol.AttrMinLevel); err != nil {
					return err
				}
				if hi, err = readU8(th, levelcontrol.AttrMaxLevel); err != nil {
					return err
				}
				fm, err := th.readValue(n, attrPath(th.ep, levelcontrol.ClusterID, 0xFFFC))
				if err != nil {
					return err
				}
				lighting = fm.El.Uint&(1<<1) != 0 // LIGHTING_FEATURE (:20)
				// boundsConform (:107-115).
				if lo > hi || hi > 254 {
					return fmt.Errorf("%d-%d is not a range CurrentLevel can take", lo, hi)
				}
				if lighting && (lo != 1 || hi != 254) {
					return fmt.Errorf("a Lighting device reports 1-254, not %d-%d", lo, hi)
				}
				th.note("levels %d-%d (Lighting %v)", lo, hi, lighting)
				return nil
			}},
			{n: "1", line: 198, title: "the DUT sends supported LevelControl commands", run: func(th *goTH) error {
				before, err := th.readValue(n, attrPath(th.ep, levelcontrol.ClusterID, levelcontrol.AttrCurrentLevel))
				if err != nil {
					return err
				}
				// levelOtherThan (:121-127).
				level := lo + (hi-lo)/2
				if !before.El.IsNull && before.El.Uint == uint64(level) {
					if level == hi {
						level = lo
					} else {
						level++
					}
				}
				if _, err := th.invoke(n, cmdPath(th.ep, levelcontrol.ClusterID, levelcontrol.CmdMoveToLevelWithOnOff),
					levelcontrol.MoveToLevelWithOnOffRequest{Level: level, TransitionTime: zero}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				got, err := readU8(th, levelcontrol.AttrCurrentLevel)
				if err != nil {
					return err
				}
				if got != level {
					return fmt.Errorf("CurrentLevel %s -> %d, commanded %d", describeNode(before), got, level)
				}
				if _, err := th.invoke(n, cmdPath(th.ep, levelcontrol.ClusterID, levelcontrol.CmdMove),
					levelcontrol.MoveRequest{MoveMode: 0, Rate: spec.ValueOf(uint8(20))}, false, client.InvokeOptions{}); err != nil { // MOVE_MODE_UP, MOVE_RATE (:32-33)
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, levelcontrol.ClusterID, levelcontrol.CmdStop), levelcontrol.StopRequest{}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, levelcontrol.ClusterID, levelcontrol.CmdStep),
					levelcontrol.StepRequest{StepMode: 1, StepSize: 10, TransitionTime: zero}, false, client.InvokeOptions{}); err != nil { // STEP_MODE_DOWN, STEP_SIZE (:34-35)
					return err
				}
				th.note("MoveToLevelWithOnOff %d reached (from %s); Move up, Stop, Step down 10: Success", level, describeNode(before))
				return nil
			}},
		},
	}
}

// caseS31 mirrors TC-S-3.1: the ScenesManagement commands on a light.
func caseS31() goCertCase {
	var n *client.Node
	const (
		keySet  uint16 = 0x01a1 // GROUP_KEY_SET_ID (TC-S-3.1.test.ts:19)
		group   uint16 = 0x0001 // GROUP (:20)
		scene   uint8  = 0x01   // SCENE_ID (:21)
		copied  uint8  = 0x02   // COPIED_SCENE_ID (:22)
		sceneNm        = "scene1"
	)
	start := epochUs(1_600_000_000_000_000)
	one := uint8(1) // SCENE_ON_OFF_VALUE (:26)
	invoke := func(cmd uint32, fields spec.Encodable, statusField bool) func(th *goTH) error {
		return func(th *goTH) error {
			if _, err := th.invoke(n, cmdPath(th.ep, scenesmanagement.ClusterID, cmd), fields, statusField, client.InvokeOptions{}); err != nil {
				return err
			}
			th.note("ScenesManagement command 0x%02X on endpoint %d: SUCCESS", cmd, th.ep)
			return nil
		}
	}
	zero := uint32(0)
	return goCertCase{
		tc: "TC-S-3.1", line: 116, pics: []string{"S.C"}, at: "S.S & G.S & OO.S",
		steps: []goStep{
			{n: "0", line: 128, title: "Preconditions: key set, GroupKeyMap and the group", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err != nil {
					return err
				}
				k0 := []byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf}
				k1 := []byte{0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf}
				ks := groupkeymanagement.GroupKeySetStruct{ // groupKeySet (:36-48)
					GroupKeySetId: keySet, EpochKey0: spec.ValueOf(k0), EpochStartTime0: spec.ValueOf(start),
					EpochKey1: spec.ValueOf(k1), EpochStartTime1: spec.ValueOf(start + 1_000_000),
					EpochKey2: spec.NullOf[[]byte](), EpochStartTime2: spec.NullOf[uint64](),
				}
				if _, err := th.invoke(n, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetWrite), groupkeymanagement.KeySetWriteRequest{GroupKeySet: ks}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				if err := th.write(n, attrPath(0, groupkeymanagement.ClusterID, groupkeymanagement.AttrGroupKeyMap),
					listOf(groupkeymanagement.GroupKeyMapStruct{GroupId: group, GroupKeySetId: keySet}), client.WriteOptions{}); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdRemoveAllGroups), groups.RemoveAllGroupsRequest{}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th.ep, groups.ClusterID, groups.CmdAddGroup), groups.AddGroupRequest{GroupId: group, GroupName: "gp1"}, true, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("group %d bound to key set 0x%04X and added on endpoint %d", group, keySet, th.ep)
				return nil
			}},
			{n: "1", line: 187, title: "AddScene", pics: "S.C.C00.Tx", run: invoke(scenesmanagement.CmdAddScene, scenesmanagement.AddSceneRequest{
				GroupId: group, SceneId: scene, SceneName: sceneNm,
				ExtensionFieldSetStructs: []scenesmanagement.ExtensionFieldSetStruct{{
					ClusterId: onoff.ClusterID, AttributeValueList: []scenesmanagement.AttributeValuePairStruct{{AttributeId: onoff.AttrOnOff, ValueUnsigned8: &one}},
				}},
			}, true)},
			{n: "2", line: 240, title: "ViewScene", pics: "S.C.C01.Tx", run: invoke(scenesmanagement.CmdViewScene, scenesmanagement.ViewSceneRequest{GroupId: group, SceneId: scene}, true)},
			{n: "3", line: 255, title: "RemoveScene", pics: "S.C.C02.Tx", run: invoke(scenesmanagement.CmdRemoveScene, scenesmanagement.RemoveSceneRequest{GroupId: group, SceneId: scene}, true)},
			{n: "4", line: 270, title: "RemoveAllScenes", pics: "S.C.C03.Tx", run: invoke(scenesmanagement.CmdRemoveAllScenes, scenesmanagement.RemoveAllScenesRequest{GroupId: group}, true)},
			{n: "5", line: 284, title: "StoreScene", pics: "S.C.C04.Tx", run: invoke(scenesmanagement.CmdStoreScene, scenesmanagement.StoreSceneRequest{GroupId: group, SceneId: scene}, true)},
			{n: "6", line: 299, title: "RecallScene", pics: "S.C.C05.Tx", run: invoke(scenesmanagement.CmdRecallScene, scenesmanagement.RecallSceneRequest{GroupId: group, SceneId: scene, TransitionTime: &spec.Nullable[uint32]{Value: zero}}, false)},
			{n: "7", line: 325, title: "GetSceneMembership", pics: "S.C.C06.Tx", run: invoke(scenesmanagement.CmdGetSceneMembership, scenesmanagement.GetSceneMembershipRequest{GroupId: group}, true)},
			{n: "8", line: 339, title: "CopyScene", pics: "S.C.C40.Tx", run: invoke(scenesmanagement.CmdCopyScene, scenesmanagement.CopySceneRequest{
				GroupIdentifierFrom: group, SceneIdentifierFrom: scene, GroupIdentifierTo: group, SceneIdentifierTo: copied,
			}, true)},
		},
	}
}

// switchEvents collects a Switch event subscription's events.
type switchEvents struct {
	mu     sync.Mutex
	events []interaction.EventData
}

func (s *switchEvents) add(r *client.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, r.Events...)
	return nil
}

// since returns the events after the first n.
func (s *switchEvents) since(n int) []interaction.EventData {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.events) {
		return nil
	}
	return slices.Clone(s.events[n:])
}

func (s *switchEvents) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// The Switch case's timing (TC-SWTCH-3.2.test.ts:52-82).
const (
	swPressed       = 1
	swPresses       = 2
	swPressMs       = 200
	swLongDelayMs   = 750
	swLongDuration  = 2_750
	swEventQuiet    = 750 * time.Millisecond
	swSwitchIdle    = time.Second
	swEventWait     = 5 * time.Second
	swOperations    = 2
	swFeatureLS     = 1 << 0
	swFeatureMS     = 1 << 1
	swFeatureMSR    = 1 << 2
	swFeatureMSL    = 1 << 3
	swFeatureMSM    = 1 << 4
	swFeatureAS     = 1 << 5
	swPositionCount = 2
)

// caseSWTCH32 mirrors TC-SWTCH-3.2: the TH observes the daemon's momentary
// switch by reads and events while the switch is operated through the
// app pipe — matter.js's backchannel (packages/testing/src/device/
// backchannel.ts:10-26) is the daemon's --app-pipe here.
func caseSWTCH32() goCertCase {
	var (
		n  *client.Node
		ev = &switchEvents{}
	)
	pics := func(th *goTH, code string) bool { return th.declares(code) }
	consistency := func(rule func(th *goTH) bool, violation string) func(th *goTH) error {
		return func(th *goTH) error {
			if rule(th) {
				return fmt.Errorf("the DUT declares %s", violation)
			}
			th.note("the DUT's declaration does not hold %s", violation)
			return nil
		}
	}
	readU := func(th *goTH, attr uint32) (uint64, error) {
		v, err := th.readValue(n, attrPath(th.ep, switchcluster.ClusterID, attr))
		if err != nil {
			return 0, err
		}
		return v.El.Uint, nil
	}
	idle := func(th *goTH) {
		th.dut.pipe(th.t, map[string]any{"Name": "SimulateSwitchIdle", "EndpointId": th.ep})
		time.Sleep(swSwitchIdle)
	}
	// quietBoundary is quietEventBoundary (:339-360): the count once no event
	// arrived for EVENT_QUIET_MS.
	quietBoundary := func() int {
		last := ev.count()
		for {
			time.Sleep(swEventQuiet)
			c := ev.count()
			if c == last {
				return c
			}
			last = c
		}
	}
	names := func(es []interaction.EventData) []uint32 {
		out := make([]uint32, 0, len(es))
		for i := range es {
			out = append(out, es[i].Path.Event)
		}
		return out
	}
	await := func(th *goTH, from int, want func([]interaction.EventData) bool) []interaction.EventData {
		waitFor(th.ctx, swEventWait+swLongDuration*time.Millisecond, func() bool { return want(ev.since(from)) })
		time.Sleep(swEventQuiet)
		return ev.since(from)
	}
	fieldOf := func(e interaction.EventData, tag uint8) uint64 {
		f, _ := structField(e.Value, tag)
		return f.El.Uint
	}
	has := func(es []interaction.EventData, id uint32) bool {
		return slices.ContainsFunc(es, func(e interaction.EventData) bool { return e.Path.Event == id })
	}
	pressFields := func(es []interaction.EventData) error {
		for i := range es {
			e := &es[i]
			if e.Path.Event == switchcluster.EventMultiPressComplete || e.Path.Event == switchcluster.EventMultiPressOngoing {
				continue
			}
			// newPosition (InitialPress, LongPress) or previousPosition
			// (ShortRelease, LongRelease) is field 0.
			if p := fieldOf(*e, 0); p != swPressed {
				return fmt.Errorf("event 0x%02X names position %d, the button pressed is %d", e.Path.Event, p, swPressed)
			}
		}
		return nil
	}
	return goCertCase{
		tc: "TC-SWTCH-3.2", line: 394, pics: []string{"SWTCH.C", "MCORE.IDM.C.SubscribeEvent"}, at: "SWTCH.S",
		steps: []goStep{
			{n: "0a", line: 399, title: "at least one of LS and MS", run: consistency(func(th *goTH) bool { return !pics(th, "SWTCH.S.F00") && !pics(th, "SWTCH.S.F01") }, "neither a latching nor a momentary switch")},
			{n: "0b", line: 408, title: "MSR needs MS", run: consistency(func(th *goTH) bool { return pics(th, "SWTCH.S.F02") && !pics(th, "SWTCH.S.F01") }, "momentary switch release without a momentary switch")},
			{n: "0c", line: 417, title: "MSL needs MS", run: consistency(func(th *goTH) bool { return pics(th, "SWTCH.S.F03") && !pics(th, "SWTCH.S.F01") }, "long press without a momentary switch")},
			{n: "0d", line: 426, title: "MSL needs MSR or AS", run: consistency(func(th *goTH) bool {
				return pics(th, "SWTCH.S.F03") && !pics(th, "SWTCH.S.F02") && !pics(th, "SWTCH.S.F05")
			}, "long press with neither momentary switch release nor an action switch")},
			{n: "0e", line: 441, title: "MSM needs MS", run: consistency(func(th *goTH) bool { return pics(th, "SWTCH.S.F04") && !pics(th, "SWTCH.S.F01") }, "multi press without a momentary switch")},
			{n: "0f", line: 450, title: "MSM needs MSR or AS", run: consistency(func(th *goTH) bool {
				return pics(th, "SWTCH.S.F04") && !pics(th, "SWTCH.S.F02") && !pics(th, "SWTCH.S.F05")
			}, "multi press with neither momentary switch release nor an action switch")},
			{n: "0g", line: 464, title: "polling or eventing", run: consistency(func(th *goTH) bool {
				return !pics(th, mirrorPICS("SWTCH.C.M.SwitchStatePolling")) && !pics(th, mirrorPICS("SWTCH.C.M.SwitchStateEventing"))
			}, "neither polling nor eventing of switch state")},
			{n: "0h", line: 473, title: "not MSR and AS together", run: consistency(func(th *goTH) bool { return pics(th, "SWTCH.S.F02") && pics(th, "SWTCH.S.F05") }, "momentary switch release together with an action switch")},
			{n: "1a", line: 482, title: "commission, latching switch observable", pics: "SWTCH.C.F00", run: func(*goTH) error {
				return errors.New("the latching steps are not ported: the reference daemon has no latching switch")
			}},
			{n: "1b", line: 488, title: "read FeatureMap (LS)", pics: "SWTCH.C.F00", run: func(*goTH) error { return errors.New("not ported") }},
			{n: "1c", line: 503, title: "read NumberOfPositions (LS)", pics: "SWTCH.C.F00", run: func(*goTH) error { return errors.New("not ported") }},
			{n: "1d", line: 518, title: "subscribe SwitchLatched", pics: "SWTCH.C.F00 & SWTCH.C.M.SwitchStateEventing", run: func(*goTH) error { return errors.New("not ported") }},
			{n: "2a", line: 524, title: "latching switch, CurrentPosition polled", pics: "SWTCH.C.F00 & SWTCH.C.M.SwitchStatePolling", run: func(*goTH) error { return errors.New("not ported") }},
			{n: "2b", line: 551, title: "latching switch, SwitchLatched events", pics: "SWTCH.C.F00 & SWTCH.C.M.SwitchStateEventing", run: func(*goTH) error { return errors.New("not ported") }},
			{n: "3a", line: 608, title: "commission, momentary switch observable", pics: "SWTCH.C.F01", run: func(th *goTH) error {
				var err error
				if _, n, err = th.commission("dut"); err == nil {
					th.note("commissioned; the switch is endpoint %d", th.ep)
				}
				return err
			}},
			{n: "3b", line: 614, title: "read FeatureMap", pics: "SWTCH.C.F01", run: func(th *goTH) error {
				fm, err := readU(th, 0xFFFC)
				if err != nil {
					return err
				}
				// MOMENTARY_FEATURE_MAPS (:32): MS|MSR|MSL|MSM or MS|MSL|MSM|AS.
				if fm != swFeatureMS|swFeatureMSR|swFeatureMSL|swFeatureMSM && fm != swFeatureMS|swFeatureMSL|swFeatureMSM|swFeatureAS {
					return fmt.Errorf("FeatureMap 0x%02X, want 0x1E or 0x3A", fm)
				}
				th.note("FeatureMap 0x%02X on endpoint %d", fm, th.ep)
				return nil
			}},
			{n: "3c", line: 629, title: "read NumberOfPositions", pics: "SWTCH.C.F01", run: func(th *goTH) error {
				np, err := readU(th, switchcluster.AttrNumberOfPositions)
				if err != nil {
					return err
				}
				if np != swPositionCount {
					return fmt.Errorf("NumberOfPositions %d, want %d", np, swPositionCount)
				}
				th.note("NumberOfPositions %d", np)
				return nil
			}},
			{n: "3d", line: 644, title: "subscribe to all switch events on the endpoint", pics: "SWTCH.C.F01 & SWTCH.C.M.SwitchStateEventing", run: func(th *goTH) error {
				sub, err := n.Subscribe(th.ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
					// Urgent, as the TS subscribes (`urgent: true`, :281): a
					// device reports an event when it occurs only on an urgent
					// path, otherwise with its next report.
					Events:           []im.ConcreteEventPath{{Endpoint: th.ep, Cluster: switchcluster.ClusterID, HasEndpoint: true, HasCluster: true, IsUrgent: true}},
					MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(time.Minute),
					Updated: ev.add,
				}})
				if err != nil {
					return err
				}
				th.t.Cleanup(sub.Close)
				th.note("event subscription %d on endpoint %d's Switch", sub.ID(), th.ep)
				return nil
			}},
			{n: "4a", line: 650, title: "momentary switch operated while CurrentPosition is read", pics: "SWTCH.C.F01 & SWTCH.C.M.SwitchStatePolling", run: func(th *goTH) error {
				// The TS sets the position through the backchannel's
				// simulateLatchPosition (:657-661); the daemon's pipe has no
				// such command, so a long press holds the button down long
				// enough to read it pressed, then released.
				for op := range swOperations {
					done, err := th.pipeAsync(map[string]any{
						"Name": "SimulateLongPress", "EndpointId": th.ep, "ButtonId": swPressed,
						"LongPressDelayMillis": swLongDelayMs, "LongPressDurationMillis": swLongDuration,
					})
					if err != nil {
						return err
					}
					for _, want := range []uint64{swPressed, 0} {
						var got uint64
						ok := waitFor(th.ctx, swEventWait, func() bool {
							v, err := readU(th, switchcluster.AttrCurrentPosition)
							got = v
							return err == nil && v == want
						})
						if !ok {
							return fmt.Errorf("operation %d: CurrentPosition reads %d, want %d", op+1, got, want)
						}
					}
					if err := done(); err != nil {
						return err
					}
				}
				th.note("CurrentPosition read 1 while pressed and 0 once released, %d times", swOperations)
				return nil
			}},
			{n: "4b", line: 677, title: "short press: InitialPress and ShortRelease", pics: "SWTCH.C.F01 & SWTCH.C.F02 & SWTCH.C.M.SwitchStateEventing", run: func(th *goTH) error {
				// The TS marks the step not applicable for its action-switch
				// TH (:679-683); the daemon's switch is not one, so the step
				// runs, a single press through SimulateMultiPress.
				idle(th)
				from := quietBoundary()
				th.dut.pipe(th.t, map[string]any{
					"Name": "SimulateMultiPress", "EndpointId": th.ep, "ButtonId": swPressed,
					"MultiPressPressedTimeMillis": swPressMs, "MultiPressReleasedTimeMillis": swPressMs, "MultiPressNumPresses": 1,
				})
				es := await(th, from, func(es []interaction.EventData) bool {
					return has(es, switchcluster.EventInitialPress) && has(es, switchcluster.EventShortRelease)
				})
				if len(es) < 2 || es[0].Path.Event != switchcluster.EventInitialPress || es[1].Path.Event != switchcluster.EventShortRelease {
					return fmt.Errorf("the short press reported events %v, want InitialPress then ShortRelease", names(es))
				}
				if err := pressFields(es); err != nil {
					return err
				}
				th.note("short press reported events %v", names(es))
				return nil
			}},
			{n: "4c", line: 685, title: "long press: InitialPress, LongPress and LongRelease", pics: "SWTCH.C.F01 & SWTCH.C.F03 & SWTCH.C.M.SwitchStateEventing", run: func(th *goTH) error {
				idle(th)
				from := quietBoundary()
				th.dut.pipe(th.t, map[string]any{
					"Name": "SimulateLongPress", "EndpointId": th.ep, "ButtonId": swPressed,
					"LongPressDelayMillis": swLongDelayMs, "LongPressDurationMillis": swLongDuration,
				})
				want := []uint32{switchcluster.EventInitialPress, switchcluster.EventLongPress, switchcluster.EventLongRelease}
				es := await(th, from, func(es []interaction.EventData) bool { return has(es, switchcluster.EventLongRelease) })
				if !slices.Equal(names(es), want) {
					return fmt.Errorf("the long press reported events %v, want %v once each and in order", names(es), want)
				}
				if err := pressFields(es); err != nil {
					return err
				}
				th.note("long press reported events %v, each naming position %d", names(es), swPressed)
				return nil
			}},
			{n: "4d", line: 756, title: "multi press on a switch that is not an action switch", pics: "SWTCH.C.F01 & SWTCH.C.F04 & !SWTCH.C.F05 & SWTCH.C.M.SwitchStateEventing", run: func(th *goTH) error {
				// Not applicable in the TS for its action-switch TH (:758);
				// the daemon's switch is not one.
				idle(th)
				from := quietBoundary()
				th.dut.pipe(th.t, map[string]any{
					"Name": "SimulateMultiPress", "EndpointId": th.ep, "ButtonId": swPressed,
					"MultiPressPressedTimeMillis": swPressMs, "MultiPressReleasedTimeMillis": swPressMs, "MultiPressNumPresses": swPresses,
				})
				es := await(th, from, func(es []interaction.EventData) bool { return has(es, switchcluster.EventMultiPressComplete) })
				var complete *interaction.EventData
				for i := range es {
					if es[i].Path.Event == switchcluster.EventMultiPressComplete {
						complete = &es[i]
					}
				}
				if complete == nil || !has(es, switchcluster.EventInitialPress) || !has(es, switchcluster.EventMultiPressOngoing) {
					return fmt.Errorf("the double press reported events %v, want InitialPress, MultiPressOngoing and MultiPressComplete", names(es))
				}
				// totalNumberOfPressesCounted is field 1 (switch.adoc MultiPressComplete).
				if c := fieldOf(*complete, 1); c != swPresses {
					return fmt.Errorf("MultiPressComplete counted %d presses, want %d", c, swPresses)
				}
				if err := pressFields(es); err != nil {
					return err
				}
				th.note("double press reported events %v; MultiPressComplete counted %d", names(es), swPresses)
				return nil
			}},
			{n: "4f", line: 760, title: "multi press on an action switch", pics: "SWTCH.C.F01 & SWTCH.C.F04 & SWTCH.C.F05 & SWTCH.C.M.SwitchStateEventing", run: func(*goTH) error {
				return errors.New("the action-switch step is not ported: the reference daemon has no action switch")
			}},
		},
	}
}
