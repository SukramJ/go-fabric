// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/groupcast"
	"github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The BIND cases: matter.js's TC-BIND-2.1 and 2.3 run a light switch as the
// DUT, as here — the daemon with --light-switch
// (examples/reference-bridge/light_switch.go) — with TH1 a controller (the
// Go TH) and TH2, TH3 all-clusters apps (further daemons). The switch is
// triggered through the app pipe's SendOnOffToBindings, matter.js's
// backchannel sendOnOffToBindings (support/chip-testing/test/cert/
// tc-bind-support.ts:600-606).
//
// Where the TS reads a TH's log for the command it received, the Go TH
// reads what the TH device did with it: the IM status each bound target
// answered the switch's invoke with (the send fails on any other) and the
// OnOff attribute the command moved.

func init() {
	for _, c := range []func() goCertCase{caseBIND21, caseBIND23} {
		registerGoCert(c)
	}
}

// The BIND budgets (tc-bind-support.ts:105, tc-support.ts:64).
const (
	bindAbsenceWindow = 5 * time.Second
	bindLogTimeout    = 15 * time.Second
)

// bindDeclare are the codes of the --light-switch composition, which the
// committed slices (generated from the default composition) cannot state:
// matter.js's declarations for its cert light switch, a binding client
// that sends OnOff On and Off (LIGHT_SWITCH_PICS,
// support/chip-testing/src/cert/index.ts:324-331), and GRPKEY.C, which
// TC-BIND-2.3 gates on and matter.js takes from CHIP's shared PICS file
// (:320-323; ci-pics-values:655 at 6170af84 answers 1): the switch has no
// GroupKeyManagement client of its own, TH1 provisions it.
var bindDeclare = map[string]string{
	"BIND.C": "1", "OO.C": "1", "OO.C.C00.Tx": "1", "OO.C.C01.Tx": "1",
	"MCORE.ROLE.CONTROLLER": "1", "MCORE.DT_SW_COMP": "0", "GRPKEY.C": "1",
}

// bindRun is a BIND case's TH1, its nodes and the switch endpoint.
type bindRun struct {
	ctrl  *gocontroller.Controller
	nodes map[string]*client.Node
	devs  map[string]*bridgeProcess
	sw    uint16
}

func (b *bindRun) node(role string) (*client.Node, error) {
	n := b.nodes[role]
	if n == nil {
		return nil, fmt.Errorf("%s was not commissioned by the preceding steps", strings.ToUpper(role))
	}
	return n, nil
}

func (b *bindRun) nodeID(role string) (uint64, error) {
	n, err := b.node(role)
	if err != nil {
		return 0, err
	}
	a, ok := n.PeerAddress()
	if !ok {
		return 0, errors.New("no operational address")
	}
	return a.NodeID, nil
}

// factoryReset is the plan's "Factory Reset DUT" (factoryResetStep,
// tc-bind-support.ts:161-183): the DUT is a daemon started for this case on
// an empty database, and it advertises as commissionable — it holds no
// fabric.
func (b *bindRun) factoryReset(th *goTH) error {
	b.nodes, b.devs = map[string]*client.Node{}, map[string]*bridgeProcess{"dut": th.dut}
	if len(th.extraEPs) != 1 {
		return fmt.Errorf("the --light-switch daemon serves %v beyond the committed endpoints; the switch is one", th.extraEPs)
	}
	b.sw = th.extraEPs[0]
	names, err := th.names()
	if err != nil {
		return err
	}
	if _, err := th.commissionable(names, th.dut.info.discriminator, th.dut.info.port, ddMDNSTimeout); err != nil {
		return err
	}
	th.note("the DUT runs on a fresh database and advertises as commissionable; its switch is endpoint %d", b.sw)
	return nil
}

// commissionStep is commissionStep (tc-bind-support.ts:185-201).
func (b *bindRun) commissionStep(role string, index int) func(th *goTH) error {
	return func(th *goTH) error {
		if b.ctrl == nil {
			var err error
			if b.ctrl, err = th.controller("th1"); err != nil {
				return err
			}
		}
		dev := b.devs[role]
		if dev == nil {
			dev = th.device(index)
			b.devs[role] = dev
		}
		n, err := th.pair(b.ctrl, dev.info.manualCode)
		if err != nil {
			return err
		}
		b.nodes[role] = n
		id, _ := b.nodeID(role)
		th.note("TH1 commissioned %s as node %d", strings.ToUpper(role), id)
		return nil
	}
}

// bindEntry is a BindingEntrySpec (tc-bind-support.ts:111).
type bindEntry struct {
	role     string
	endpoint uint16
	group    uint16
}

// writeBinding is writeBindingStep (tc-bind-support.ts:207-252).
func (b *bindRun) writeBinding(specs ...bindEntry) func(th *goTH) error {
	return func(th *goTH) error {
		dut, err := b.node("dut")
		if err != nil {
			return err
		}
		// The accessing fabric's index, as the ACL writes carry it: the
		// field is mandatory on the wire and the server holds it to 1-254.
		fi, err := th.ownFabricIndex(dut)
		if err != nil {
			return err
		}
		entries := make([]bindingdef.TargetStruct, 0, len(specs))
		for _, s := range specs {
			if s.group != 0 {
				g := s.group
				entries = append(entries, bindingdef.TargetStruct{Group: &g, FabricIndex: fi})
				continue
			}
			id, err := b.nodeID(s.role)
			if err != nil {
				return err
			}
			ep, cl := s.endpoint, onoff.ClusterID
			entries = append(entries, bindingdef.TargetStruct{Node: &id, Endpoint: &ep, Cluster: &cl, FabricIndex: fi})
		}
		p := attrPath(b.sw, bindingdef.ClusterID, bindingdef.AttrBinding)
		if err := th.write(dut, p, listOf(entries...), client.WriteOptions{}); err != nil {
			return err
		}
		v, err := th.readValue(dut, p)
		if err != nil {
			return err
		}
		if len(v.Children) != len(entries) {
			return fmt.Errorf("Binding reads back %s, %d entries where %d were written", describeNode(v), len(v.Children), len(entries))
		}
		for _, want := range entries {
			if !slices.ContainsFunc(v.Children, func(c spec.Node) bool {
				var got bindingdef.TargetStruct
				if got.DecodeTLV(c) != nil {
					return false
				}
				if want.Group != nil {
					return got.Group != nil && *got.Group == *want.Group && got.Node == nil
				}
				return got.Node != nil && *got.Node == *want.Node && got.Endpoint != nil && *got.Endpoint == *want.Endpoint &&
					got.Cluster != nil && *got.Cluster == *want.Cluster
			}) {
				return fmt.Errorf("Binding reads back %s, without %+v", describeNode(v), want)
			}
		}
		th.note("Binding on endpoint %d holds exactly the %d entries written: %s", b.sw, len(entries), describeNode(v))
		return nil
	}
}

// appendACL is #appendAcl (tc-bind-support.ts:289-333): TH1 adds an entry to
// role's ACL beside the existing ones, and reads it back.
func (b *bindRun) appendACL(th *goTH, role string, entry accesscontrol.AccessControlEntryStruct, admits func(accesscontrol.AccessControlEntryStruct) bool) error {
	n, err := b.node(role)
	if err != nil {
		return err
	}
	aclPath := attrPath(0, accesscontrol.ClusterID, accesscontrol.AttrAcl)
	read := func() ([]accesscontrol.AccessControlEntryStruct, error) {
		v, err := th.readValue(n, aclPath)
		if err != nil {
			return nil, err
		}
		var out []accesscontrol.AccessControlEntryStruct
		for _, c := range v.Children {
			var e accesscontrol.AccessControlEntryStruct
			if err := e.DecodeTLV(c); err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, nil
	}
	existing, err := read()
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return errors.New("the ACL read back empty: the administer entry is missing")
	}
	entry.FabricIndex = existing[0].FabricIndex
	if err := th.write(n, aclPath, listOf(append(existing, entry)...), client.WriteOptions{}); err != nil {
		return err
	}
	after, err := read()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(after, admits) {
		return fmt.Errorf("%s's ACL does not admit the entry written: %d entries", strings.ToUpper(role), len(after))
	}
	return nil
}

// onOffTarget is an ACL target: OnOff on endpoint.
func onOffTarget(endpoint uint16) spec.Nullable[[]accesscontrol.AccessControlTargetStruct] {
	return spec.ValueOf([]accesscontrol.AccessControlTargetStruct{{
		Cluster: spec.ValueOf(onoff.ClusterID), Endpoint: spec.ValueOf(endpoint), DeviceType: spec.NullOf[uint32](),
	}})
}

// grantOperate is grantOperate (tc-bind-support.ts:254-270): role lets the
// DUT operate OnOff on endpoint over CASE.
func (b *bindRun) grantOperate(th *goTH, role string, endpoint uint16) error {
	dut, err := b.nodeID("dut")
	if err != nil {
		return err
	}
	const authModeCASE = 2 // AUTH_MODE_CASE (:63)
	err = b.appendACL(th, role, accesscontrol.AccessControlEntryStruct{
		Privilege: privOperate, AuthMode: authModeCASE, Subjects: spec.ValueOf([]uint64{dut}), Targets: onOffTarget(endpoint),
	}, func(e accesscontrol.AccessControlEntryStruct) bool {
		return e.Privilege >= privOperate && e.AuthMode == authModeCASE && !e.Subjects.Null && slices.Contains(e.Subjects.Value, dut)
	})
	if err == nil {
		th.note("%s admits node %d to operate OnOff on endpoint %d", strings.ToUpper(role), dut, endpoint)
	}
	return err
}

// trigger is trigger (tc-bind-support.ts:600-606): the DUT sends command to
// its binding targets through the app pipe, and the send fails unless every
// target answered SUCCESS (light_switch.go sendOnOff).
func (b *bindRun) trigger(th *goTH, command string) error {
	line, err := json.Marshal(map[string]any{"Name": "SendOnOffToBindings", "EndpointId": b.sw, "Command": command})
	if err != nil {
		return err
	}
	offset := len(th.dut.snapshotStderr())
	w, err := os.OpenFile(th.dut.opts.appPipe, os.O_WRONLY, 0) //nolint:gosec // the daemon's own FIFO
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	_ = w.Close()
	if err != nil {
		return err
	}
	var outcome string
	if !waitFor(th.ctx, 2*time.Minute, func() bool {
		tail := th.dut.snapshotStderr()[offset:]
		if strings.Contains(tail, "msg=apppipe.applied name=SendOnOffToBindings") {
			outcome = "applied"
			return true
		}
		if i := strings.Index(tail, "msg=apppipe.error"); i >= 0 {
			outcome = firstLineOf(tail[i:])
			return true
		}
		return false
	}) {
		return errors.New("SendOnOffToBindings: the DUT logged no outcome within 2m")
	}
	if outcome != "applied" {
		return fmt.Errorf("SendOnOffToBindings %s: %s", command, outcome)
	}
	sent := reSwitchSend.FindAllString(th.dut.snapshotStderr()[offset:], -1)
	th.note("the DUT sent OnOff.%s to its binding targets: %s", command, strings.Join(sent, "; "))
	return nil
}

var reSwitchSend = regexp.MustCompile(`msg=lightswitch\.send command=\w+ kind=\w+ entry=\S+`)

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// onOffIs waits up to d for role's OnOff on endpoint to read want.
func (b *bindRun) onOffIs(th *goTH, role string, endpoint uint16, want bool, d time.Duration) error {
	n, err := b.node(role)
	if err != nil {
		return err
	}
	var last any = "unread"
	if !waitFor(th.ctx, d, func() bool {
		v, err := th.readValue(n, attrPath(endpoint, onoff.ClusterID, onoff.AttrOnOff))
		if err != nil {
			last = err
			return false
		}
		last = v.El.Bool
		return v.El.Bool == want
	}) {
		return fmt.Errorf("%s endpoint %d OnOff=%v, want %t", strings.ToUpper(role), endpoint, last, want)
	}
	return nil
}

// stays holds role's attribute at want for d: the TS's absence check over
// its ABSENCE_WINDOW (tc-bind-support.ts:105).
func (b *bindRun) stays(th *goTH, role string, endpoint uint16, want bool, d time.Duration) error {
	n, err := b.node(role)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		v, err := th.readValue(n, attrPath(endpoint, onoff.ClusterID, onoff.AttrOnOff))
		if err != nil {
			return err
		}
		if v.El.Bool != want {
			return fmt.Errorf("%s endpoint %d OnOff changed to %t within %s", strings.ToUpper(role), endpoint, v.El.Bool, d)
		}
		sleepCtx(th.ctx, 250*time.Millisecond)
	}
	return nil
}

// readOnOff is readOnOffStep (tc-bind-support.ts:516-532).
func (b *bindRun) readOnOff(role string, endpoint func() uint16, want bool) func(th *goTH) error {
	return func(th *goTH) error {
		if err := b.onOffIs(th, role, endpoint(), want, 0); err != nil {
			return err
		}
		th.note("%s endpoint %d OnOff=%t", strings.ToUpper(role), endpoint(), want)
		return nil
	}
}

// onOffEndpoints are the daemon's OnOff servers, by its committed slices.
func (th *goTH) onOffEndpoints() []uint16 {
	var out []uint16
	for _, ep := range th.pics.sorted {
		if ok, _ := th.picsHolds("OO.S", ep); ok && ep != 0 {
			out = append(out, ep)
		}
	}
	return out
}

// caseBIND21 is TC-BIND-2.1: unicast bindings to two nodes, one removed.
// The TS binds TH2 endpoint 1 and TH3 endpoint 2 (:19-21); here the
// daemons' first and second OnOff servers.
func caseBIND21() goCertCase {
	b := &bindRun{}
	var th2EP, th3EP uint16
	eps := func(th *goTH) error {
		oo := th.onOffEndpoints()
		if len(oo) < 2 {
			return fmt.Errorf("the daemon serves OnOff on %v; the case binds two endpoints", oo)
		}
		th2EP, th3EP = oo[0], oo[1]
		return nil
	}
	return goCertCase{
		tc: "TC-BIND-2.1", line: 25, native: true, daemonArgs: []string{"--light-switch"}, declare: bindDeclare,
		pics: []string{"BIND.C", "MCORE.ROLE.CONTROLLER", "!MCORE.DT_SW_COMP"},
		steps: []goStep{
			{n: "1", line: 35, title: "Factory Reset DUT", run: b.factoryReset},
			{n: "2", line: 36, title: "Commission DUT to TH1's fabric", run: b.commissionStep("dut", 0)},
			{n: "3", line: 39, title: "Commission TH2 to TH1's fabric", run: b.commissionStep("th2", 1)},
			{n: "4", line: 42, title: "Commission TH3 to TH1's fabric", run: b.commissionStep("th3", 2)},
			{n: "5", line: 45, title: "TH1 writes Binding entries for TH2 and TH3 into DUT", run: func(th *goTH) error {
				if err := eps(th); err != nil {
					return err
				}
				return b.writeBinding(bindEntry{role: "th2", endpoint: th2EP}, bindEntry{role: "th3", endpoint: th3EP})(th)
			}},
			{n: "6", line: 55, title: "TH1 enables DUT as Controller", run: func(th *goTH) error {
				return errors.Join(b.grantOperate(th, "th2", th2EP), b.grantOperate(th, "th3", th3EP))
			}},
			{n: "7", line: 69, pics: "OO.C.C01.Tx", title: "DUT is triggered to send On to its binding entries", run: func(th *goTH) error {
				if err := b.trigger(th, "on"); err != nil {
					return err
				}
				return errors.Join(b.onOffIs(th, "th2", th2EP, true, bindLogTimeout), b.onOffIs(th, "th3", th3EP, true, bindLogTimeout))
			}},
			{n: "8", line: 92, pics: "OO.C.C01.Tx", title: "TH1 reads OnOff from TH2", run: b.readOnOff("th2", func() uint16 { return th2EP }, true)},
			{n: "9", line: 96, title: "TH1 removes the binding entry for TH3", run: func(th *goTH) error {
				return b.writeBinding(bindEntry{role: "th2", endpoint: th2EP})(th)
			}},
			{n: "10", line: 102, pics: "OO.C.C00.Tx", title: "DUT is triggered to send Off to its binding entries", run: func(th *goTH) error {
				if err := b.trigger(th, "off"); err != nil {
					return err
				}
				return errors.Join(b.onOffIs(th, "th2", th2EP, false, bindLogTimeout), b.stays(th, "th3", th3EP, true, bindAbsenceWindow))
			}},
			{n: "11", line: 133, pics: "OO.C.C00.Tx", title: "TH1 reads OnOff from TH2", run: b.readOnOff("th2", func() uint16 { return th2EP }, false)},
			{n: "12", line: 137, pics: "OO.C.C01.Tx", title: "TH1 reads OnOff from TH3", run: b.readOnOff("th3", func() uint16 { return th3EP }, true)},
		},
	}
}

// The BIND-2.3 group (TC-BIND-2.3.test.ts:32-33).
const (
	bindGroupID     uint16 = 1
	bindGroupKeySet uint16 = 0x01a1
)

// caseBIND23 is TC-BIND-2.3 on the branch the daemons' roots call for: a
// group binding, the group provisioned on TH2 and the key given to the
// DUT, and a group On that reaches TH2. matter.js runs it twice
// (TC-BIND-2.3-Groupcast and -NoGroupcast, :369-371), once per kind of
// root; the reference daemon's root serves Groupcast, so this is the
// Groupcast run — the family id BIND/2.3 names both. TH2 takes the Matter
// port: a group message goes to it (:167-169).
func caseBIND23() goCertCase {
	b := &bindRun{}
	var (
		th2EP  uint16
		branch string
		key    []byte
	)
	on := func(want string, run func(th *goTH) error) func(th *goTH) error {
		return func(th *goTH) error {
			if branch == "" {
				return errors.New("step 5 did not establish which branch of the plan this run takes")
			}
			if branch != want {
				return goSkip("the plan's %s branch, where the DUT's and TH2's roots call for the %s one (TC-BIND-2.3.test.ts:64-81)", want, branch)
			}
			return run(th)
		}
	}
	join := func(role string, endpoints []uint16) func(th *goTH) error {
		return func(th *goTH) error {
			n, err := b.node(role)
			if err != nil {
				return err
			}
			k := key
			if _, err := th.invoke(n, cmdPath(0, groupcast.ClusterID, groupcast.CmdJoinGroup),
				groupcast.JoinGroupRequest{GroupId: bindGroupID, Endpoints: endpoints, KeySetId: bindGroupKeySet, Key: &k}, false, client.InvokeOptions{}); err != nil {
				return err
			}
			th.note("%s answered JoinGroup %d (endpoints %v, key set 0x%04x) with SUCCESS", strings.ToUpper(role), bindGroupID, endpoints, bindGroupKeySet)
			return nil
		}
	}
	keySet := func() groupkeymanagement.GroupKeySetStruct {
		ks := groupKeySet()
		ks.GroupKeySetId = bindGroupKeySet
		ks.EpochKey0 = spec.ValueOf(key)
		return ks
	}
	// keySetWrite, groupKeyMap and addGroup are BindRun's
	// (tc-bind-support.ts:335-435), for the legacy branch.
	keySetWrite := func(role string) func(th *goTH) error {
		return func(th *goTH) error {
			n, err := b.node(role)
			if err != nil {
				return err
			}
			_, err = th.invoke(n, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetWrite),
				groupkeymanagement.KeySetWriteRequest{GroupKeySet: keySet()}, false, client.InvokeOptions{})
			return err
		}
	}
	groupKeyMap := func(role string) func(th *goTH) error {
		return func(th *goTH) error {
			n, err := b.node(role)
			if err != nil {
				return err
			}
			return th.write(n, attrPath(0, groupkeymanagement.ClusterID, groupkeymanagement.AttrGroupKeyMap),
				listOf(groupkeymanagement.GroupKeyMapStruct{GroupId: bindGroupID, GroupKeySetId: bindGroupKeySet}), client.WriteOptions{})
		}
	}
	addGroup := func(th *goTH) error {
		n, err := b.node("th2")
		if err != nil {
			return err
		}
		_, err = th.invoke(n, cmdPath(th2EP, groups.ClusterID, groups.CmdAddGroup),
			groups.AddGroupRequest{GroupId: bindGroupID, GroupName: ""}, true, client.InvokeOptions{})
		return err
	}
	return goCertCase{
		tc: "TC-BIND-2.3", line: 164, native: true, daemonArgs: []string{"--light-switch", "--log-level", "debug"}, listen: ":0", multicast: true,
		declare: bindDeclare, pics: []string{"BIND.C", "GRPKEY.C"},
		steps: []goStep{
			{n: "1", line: 177, title: "Factory Reset DUT", run: b.factoryReset},
			{n: "2", line: 180, title: "Commission DUT to TH1's fabric", run: b.commissionStep("dut", 0)},
			{
				n: "3", line: 183, title: "TH1 enables DUT as Controller",
				na: "a group command is admitted by TH2's ACL entry with the AuthMode Group, not by an entry naming the DUT (TC-BIND-2.3.test.ts:184-186)",
			},
			{n: "4", line: 188, title: "Commission TH2 to TH1's fabric", run: func(th *goTH) error {
				// TH2 on the Matter port (:167-169); the DUT runs on another.
				b.devs["th2"] = th.deviceOn(1, fmt.Sprintf(":%d", matterPort))
				if oo := th.onOffEndpoints(); len(oo) > 0 {
					th2EP = oo[0]
				}
				return b.commissionStep("th2", 1)(th)
			}},
			{n: "4a", line: 191, title: "TH2 has an ACL entry with AuthMode Group", run: func(th *goTH) error {
				err := b.appendACL(th, "th2", accesscontrol.AccessControlEntryStruct{
					Privilege: privOperate, AuthMode: authModeGroup, Subjects: spec.ValueOf([]uint64{uint64(bindGroupID)}), Targets: onOffTarget(th2EP),
				}, func(e accesscontrol.AccessControlEntryStruct) bool {
					return e.AuthMode == authModeGroup && !e.Subjects.Null && slices.Contains(e.Subjects.Value, uint64(bindGroupID))
				})
				if err == nil {
					th.note("TH2 admits group %d to operate OnOff on endpoint %d", bindGroupID, th2EP)
				}
				return err
			}},
			// chooseBranch (:84-108).
			{n: "5", line: 201, title: "the branch the roots call for, and Key1", run: func(th *goTH) error {
				key = make([]byte, 16)
				if _, err := rand.Read(key); err != nil {
					return err
				}
				has := map[string]bool{}
				for _, role := range []string{"dut", "th2"} {
					n, err := b.node(role)
					if err != nil {
						return err
					}
					sl, err := th.readValue(n, attrPath(0, descriptor.ClusterID, descriptor.AttrServerList))
					if err != nil {
						return err
					}
					has[role] = slices.ContainsFunc(sl.Children, func(c spec.Node) bool { return c.El.Uint == uint64(groupcast.ClusterID) })
				}
				if has["dut"] != has["th2"] {
					return errors.New("the DUT and TH2 disagree on the Groupcast cluster, so no branch of the plan fits both")
				}
				branch = map[bool]string{true: "groupcast", false: "legacy"}[has["dut"]]
				if branch != "groupcast" {
					return fmt.Errorf("this run takes the plan's %s branch, where its devices were chosen for the Groupcast one", branch)
				}
				th.note("both roots serve Groupcast: the plan's Groupcast branch; Key1 generated")
				return nil
			}},
			{n: "6", line: 214, title: "KeySetWrite to TH2", run: on("legacy", keySetWrite("th2"))},
			{n: "7", line: 221, title: "GroupKeyMap on TH2", run: on("legacy", groupKeyMap("th2"))},
			{n: "8", line: 230, title: "AddGroup on TH2", run: on("legacy", addGroup)},
			{n: "9a", line: 236, title: "the DUT's Groupcast Sender feature", run: on("groupcast", func(th *goTH) error {
				n, err := b.node("dut")
				if err != nil {
					return err
				}
				fm, err := th.readValue(n, attrPath(0, groupcast.ClusterID, 0xFFFC))
				if err != nil {
					return err
				}
				if fm.El.Uint&(1<<1) == 0 { // GROUPCAST_SENDER (tc-bind-support.ts:81)
					return fmt.Errorf("the DUT's Groupcast FeatureMap 0x%X lacks the Sender feature (bit 1)", fm.El.Uint)
				}
				th.note("the DUT's Groupcast FeatureMap 0x%X has the Sender feature", fm.El.Uint)
				return nil
			})},
			{n: "9b", line: 243, title: "Groupcast JoinGroup on TH2 with EP1 and Key1", run: on("groupcast", func(th *goTH) error {
				return join("th2", []uint16{th2EP})(th)
			})},
			{n: "10", line: 259, title: "TH1 writes a group Binding entry into the DUT", run: b.writeBinding(bindEntry{group: bindGroupID})},
			{n: "11", line: 267, title: "group settings on the DUT", run: on("legacy", func(th *goTH) error {
				return errors.Join(keySetWrite("dut")(th), groupKeyMap("dut")(th))
			})},
			{n: "12", line: 284, title: "Groupcast JoinGroup on the DUT with no endpoints and Key1", run: on("groupcast", join("dut", []uint16{}))},
			{n: "13a", line: 296, pics: "OO.C.C00.Tx", title: "TH1 sends a unicast Off to TH2", run: func(th *goTH) error {
				n, err := b.node("th2")
				if err != nil {
					return err
				}
				if _, err := th.invoke(n, cmdPath(th2EP, onoff.ClusterID, onoff.CmdOff), emptyFields, false, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("TH2 answered Off with SUCCESS")
				return nil
			}},
			{n: "13b", line: 314, pics: "OO.C.C01.Tx", title: "TH1 reads OnOff from TH2", run: b.readOnOff("th2", func() uint16 { return th2EP }, false)},
			// groupOnReachesTh2 (:110-162).
			{n: "14a", line: 318, pics: "OO.C.C01.Tx", title: "DUT is triggered to send a group On", run: func(th *goTH) error {
				if err := b.trigger(th, "on"); err != nil {
					return err
				}
				m := reGroupSender.FindStringSubmatch(th.dut.snapshotStderr())
				if m == nil {
					return errors.New("the DUT logged no group sender for its group send")
				}
				if m[1] != strconv.Itoa(int(bindGroupID)) || m[2] != "ff05::fa" { // IANA_GROUPCAST_MULTICAST_ADDRESS under the Groupcast branch (:143-146)
					return fmt.Errorf("the DUT sent group %s's command to [%s], where Groupcast's IanaAddr policy makes it ff05::fa", m[1], m[2])
				}
				if err := b.onOffIs(th, "th2", th2EP, true, bindLogTimeout); err != nil {
					return err
				}
				th.note("the DUT sent group %d's OnOff.on to [%s]; TH2 endpoint %d turned on", bindGroupID, m[2], th2EP)
				return nil
			}},
			{n: "14b", line: 330, pics: "OO.C.C01.Tx", title: "TH1 reads OnOff from TH2", run: b.readOnOff("th2", func() uint16 { return th2EP }, true)},
			{n: "15", line: 334, title: "TH1 removes all the binding entries from DUT", run: b.writeBinding()},
			{n: "16a", line: 337, pics: "OO.C.C00.Tx", title: "DUT is triggered to send a group Off", run: func(th *goTH) error {
				offset := len(th.dut.snapshotStderr())
				if err := b.trigger(th, "off"); err != nil {
					return err
				}
				if strings.Contains(th.dut.snapshotStderr()[offset:], "msg=lightswitch.send command=off") {
					return errors.New("the DUT sent Off with no binding entry")
				}
				return b.stays(th, "th2", th2EP, true, bindAbsenceWindow)
			}},
			{n: "16b", line: 362, pics: "OO.C.C01.Tx", title: "TH1 reads OnOff from TH2", run: b.readOnOff("th2", func() uint16 { return th2EP }, true)},
		},
	}
}

// reGroupSender is the DUT's group sender line (groups/sender.go
// matter.groups.sender_created, at the debug level).
var reGroupSender = regexp.MustCompile(`msg=matter\.groups\.sender_created fabric_index=\d+ group_id=(\d+) session_id=\d+ key_set=\d+ address=(\S+)`)
