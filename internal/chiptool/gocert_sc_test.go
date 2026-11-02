// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/groupcast"
	"github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	groupmsg "github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The SC cases: matter.js's TC-SC-*.test.ts whose DUT is the commissioner,
// read mirrored — the reference daemon is the commissionee the Go TH
// commissions, groups and addresses.

func init() {
	for _, c := range []func() goCertCase{caseSC42, caseSC48, caseSC53, caseSC61} {
		registerGoCert(c)
	}
}

// names opens the host's DNS-SD cache for the case, closed with it.
func (th *goTH) names() (*discovery.Names, error) {
	n, err := discovery.Open(nil)
	if err != nil {
		return nil, err
	}
	th.t.Cleanup(func() { _ = n.Close() })
	return n, nil
}

// commissionable finds the commissionable record of a long discriminator
// (matter.js mdns-check.ts:141 discoverCommissionable).
func (th *goTH) commissionable(names *discovery.Names, d uint16, timeout time.Duration) (*discovery.Device, error) {
	sc := discovery.NewScanner(names)
	defer sc.Close()
	ctx, cancel := context.WithTimeout(th.ctx, timeout)
	defer cancel()
	var found *discovery.Device
	_, err := sc.FindCommissionable(ctx, discovery.ByLongDiscriminator(d), func(dev discovery.Device) {
		if found == nil {
			found = &dev
			cancel()
		}
	})
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("no commissionable record for discriminator %d within %s: %w", d, timeout, err)
}

// browse collects the instances of a service type the host sees for d.
func (th *goTH) browse(names *discovery.Names, serviceType string, d time.Duration) ([]discovery.Instance, error) {
	ctx, cancel := context.WithTimeout(th.ctx, d)
	defer cancel()
	return names.Browse(ctx, serviceType)
}

// qrWith is the daemon's QR payload with the discriminator replaced
// (matter.js tc-dd-support.ts:513 qrPayloadWith).
func qrWith(qr string, d uint16) (string, error) {
	p, err := setup.DecodeQRCode(qr)
	if err != nil {
		return "", err
	}
	p.Discriminator = d
	return setup.QRCode(p)
}

// caseSC42 mirrors TC-SC-4.2: a commissionable record carrying an unknown
// TXT key points at the daemon, and commissioning through it succeeds; the
// daemon's own record stays as it was.
func caseSC42() goCertCase {
	var (
		names *discovery.Names
		own   *discovery.Device
		alias *mdns.Responder
		ctrl  *gocontroller.Controller
	)
	aliasD := func(th *goTH) uint16 { return th.dut.info.discriminator ^ 0xAAA } // aliasDiscriminator (TC-SC-4.2.test.ts:36-43)
	return goCertCase{
		tc: "TC-SC-4.2", line: 63, pics: []string{"MCORE.ROLE.COMMISSIONER"},
		steps: []goStep{
			{n: "0", line: 68, title: "a commissioner uses the discriminator its onboarding code names", run: func(th *goTH) error {
				var err error
				if names, err = th.names(); err != nil {
					return err
				}
				if own, err = th.commissionable(names, th.dut.info.discriminator, 30*time.Second); err != nil {
					return err
				}
				// absentDiscriminator (tc-dd-support.ts:895-905): no device
				// advertises it, so the daemon must not be reached by it.
				absent := th.dut.info.discriminator ^ 0xFFF
				code, err := qrWith(th.dut.info.qrCode, absent)
				if err != nil {
					return err
				}
				if ctrl, err = th.controller("dut"); err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(th.ctx, 20*time.Second) // ABSENT_DEVICE_GIVE_UP (tc-dd-support.ts:873)
				defer cancel()
				off := false
				if _, err := ctrl.Pair(ctx, code, gocontroller.PairOptions{AutoSubscribe: &off}); !errors.Is(err, gocontroller.ErrNoDevice) {
					return fmt.Errorf("a code naming discriminator %d, which nothing advertises, answered %w; want no device", absent, err)
				}
				if _, err := th.commissionable(names, th.dut.info.discriminator, 30*time.Second); err != nil {
					return fmt.Errorf("after the refused code: %w", err)
				}
				th.note("a code naming discriminator %d reached no device; the daemon still advertises %d", absent, th.dut.info.discriminator)
				return nil
			}},
			{n: "1", line: 78, title: "TH adds an unknown key/value pair (AB=12345) to the advertised data", run: func(th *goTH) error {
				if own == nil || own.CM == 0 {
					return errors.New("the daemon is not in commissioning mode")
				}
				port := uint16(th.dut.info.port) //nolint:gosec // a bound port
				var instance [8]byte
				_, _ = rand.Read(instance[:])
				cfg := mdns.CommissionableServiceConfig{
					InstanceID: instance, Discriminator: aliasD(th), CommissioningMode: own.CM, Port: port,
				}
				if own.VP != nil {
					var v, p uint16
					_, _ = fmt.Sscanf(*own.VP, "%d+%d", &v, &p)
					cfg.VendorID, cfg.ProductID = v, p
				}
				if own.DT != nil {
					cfg.DeviceTypeID = *own.DT
				}
				if own.DN != nil {
					cfg.DeviceName = *own.DN
				}
				svc := mdns.BuildCommissionableService(cfg)
				svc.TXT = append(svc.TXT, mdns.TXTRecord{Key: "AB", Value: "12345"})
				alias = mdns.NewResponder()
				if err := alias.Publish(th.ctx, svc); err != nil {
					return err
				}
				var seen, ownSeen bool
				ok := waitFor(th.ctx, 30*time.Second, func() bool {
					is, err := th.browse(names, "_matterc._udp.local", 3*time.Second)
					if err != nil {
						return false
					}
					seen, ownSeen = false, false
					for _, in := range is {
						v, has := in.Parameters.Get("AB")
						d, _ := in.Parameters.Get("D")
						switch d {
						case strconv.FormatUint(uint64(aliasD(th)), 10):
							seen = seen || (has && v == "12345")
						case strconv.FormatUint(uint64(th.dut.info.discriminator), 10):
							// The daemon's own record stays sound: its key set
							// carries no stranger's key.
							ownSeen = !has
						}
					}
					return seen && ownSeen
				})
				if !ok {
					return fmt.Errorf("the alias carries AB=12345: %v; the daemon's own record without it: %v", seen, ownSeen)
				}
				th.note("record %s (discriminator %d, the daemon's port %d) carries AB=12345; the daemon's own record does not", svc.InstanceName, aliasD(th), port)
				return nil
			}},
			{n: "2", line: 150, title: "DUT attempts to commission TH", run: func(th *goTH) error {
				code, err := qrWith(th.dut.info.qrCode, aliasD(th))
				if err != nil {
					return err
				}
				n, err := th.pair(ctrl, code)
				if err != nil {
					return fmt.Errorf("commissioning through the record carrying AB=12345: %w", err)
				}
				v, err := th.readValue(n, attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrCommissionedFabrics))
				if err != nil {
					return err
				}
				if v.El.Uint != 1 {
					return fmt.Errorf("CommissionedFabrics reads %s after the commissioning", describeNode(v))
				}
				th.note("commissioned from %s, which only the record carrying AB matches; CommissionedFabrics=1", code)
				return nil
			}},
		},
		finalize: func(th *goTH) {
			if alias != nil {
				_ = alias.Close()
			}
		},
	}
}

// operationalInstance is "<compressed fabric id>-<node id>", the instance
// name a node advertises its operational record under.
func operationalInstance(cfid [8]byte, nodeID uint64) string {
	return fmt.Sprintf("%016X-%016X", cfid, nodeID)
}

// awaitOperational waits for an operational record by instance name (or
// for its absence).
func (th *goTH) awaitOperational(names *discovery.Names, instance string, present bool, d time.Duration) error {
	ok := waitFor(th.ctx, d, func() bool {
		is, err := th.browse(names, "_matter._tcp.local", 3*time.Second)
		if err != nil {
			return false
		}
		found := slices.ContainsFunc(is, func(in discovery.Instance) bool { return strings.EqualFold(in.Name, instance) })
		return found == present
	})
	if !ok {
		if present {
			return fmt.Errorf("no operational record %s within %s", instance, d)
		}
		return fmt.Errorf("the operational record %s is still advertised after %s", instance, d)
	}
	return nil
}

// caseSC48 mirrors TC-SC-4.8: two devices commissioned to one fabric
// advertise the same compressed fabric id, and keep it when the fabric is
// removed and they are commissioned again. The TS's TH1 and TH2 are two
// reference daemons here.
func caseSC48() goCertCase {
	type dev struct {
		b    *bridgeProcess
		node *client.Node
		cfid string
	}
	var (
		names *discovery.Names
		ctrl  *gocontroller.Controller
		devs  [2]*dev
	)
	commission := func(th *goTH, d *dev) error {
		n, err := th.pair(ctrl, d.b.info.manualCode)
		if err != nil {
			return err
		}
		d.node = n
		addr, ok := n.PeerAddress()
		if !ok {
			return errors.New("the commissioned node has no operational address")
		}
		cfid := ctrl.Fabric().Identity.CompressedFabricID
		inst := operationalInstance(cfid, addr.NodeID)
		if err := th.awaitOperational(names, inst, true, 30*time.Second); err != nil { // COMMISSIONING_LOG_TIMEOUT (TC-SC-4.8.test.ts:80)
			return err
		}
		d.cfid = fmt.Sprintf("%016X", cfid)
		th.note("the device on port %d advertises %s", d.b.info.port, inst)
		return nil
	}
	recommission := func(i int) func(th *goTH) error {
		return func(th *goTH) error {
			d := devs[i]
			if d == nil || d.node == nil {
				return fmt.Errorf("TH%d was not commissioned", i+1)
			}
			first := d.cfid
			addr, _ := d.node.PeerAddress()
			inst := operationalInstance(ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
			if err := ctrl.Registry().Decommission(th.ctx, d.node); err != nil {
				return fmt.Errorf("RemoveFabric: %w", err)
			}
			if err := th.awaitOperational(names, inst, false, 30*time.Second); err != nil {
				return err
			}
			// recordBackInCommissioningMode (tc-dd-support.ts:1358): the
			// device with no fabric left opens its window again.
			if _, err := th.commissionable(names, d.b.info.discriminator, 30*time.Second); err != nil {
				return fmt.Errorf("after RemoveFabric: %w", err)
			}
			if err := commission(th, d); err != nil {
				return err
			}
			if d.cfid != first {
				return fmt.Errorf("compressed fabric id %s after the recommissioning, %s before", d.cfid, first)
			}
			th.note("RemoveFabric, back in commissioning mode, recommissioned: compressed fabric id %s unchanged", first)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-SC-4.8", line: 120, pics: []string{"MCORE.ROLE.COMMISSIONER"},
		steps: []goStep{
			{n: "1", line: 126, title: "Commission TH1 to DUT's Fabric", run: func(th *goTH) error {
				var err error
				if names, err = th.names(); err != nil {
					return err
				}
				if ctrl, err = th.controller("dut"); err != nil {
					return err
				}
				devs[0] = &dev{b: th.dut}
				return commission(th, devs[0])
			}},
			{n: "2", line: 137, title: "Commission TH2 to DUT's Fabric", run: func(th *goTH) error {
				// identityFor(1) (cert-dsl.ts:297-318): the next device's
				// discriminator.
				b := startBridgeWith(th.t, th.bin, th.flags, bridgeOptions{listen: ":0", extraArgs: []string{"--discriminator", strconv.FormatUint(uint64(th.dut.info.discriminator)+1, 10)}})
				th.extra = append(th.extra, b)
				devs[1] = &dev{b: b}
				if err := commission(th, devs[1]); err != nil {
					return err
				}
				if devs[1].cfid != devs[0].cfid {
					return fmt.Errorf("TH2 advertises compressed fabric id %s, TH1 %s", devs[1].cfid, devs[0].cfid)
				}
				return nil
			}},
			{n: "3", line: 156, title: "RemoveFabric from DUT to TH1 and commission TH1 again", run: recommission(0)},
			{n: "4", line: 161, title: "RemoveFabric from DUT to TH2 and commission TH2 again", run: recommission(1)},
		},
	}
}

// The group plan's constants (tc-group-support.ts:43-61).
const (
	groupID        uint16 = 1
	groupName             = "GroupOne"
	groupKeySetID  uint16 = 1
	privOperate           = 3
	privManage            = 4
	authModeGroup         = 3
	ipkKeySetID    uint16 = 0
	matterEpochSec        = 946_684_800 // 2000-01-01 in Unix seconds
)

// groupEpochKey0 is the first epoch key of the plan's key set
// (groupKeySet, tc-group-support.ts:72-83).
var groupEpochKey0 = []byte{0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf}

// epochUs is a Unix instant in microseconds as the Matter epoch-us the
// wire carries: matter.js takes an epoch-us as a Unix timestamp and
// converts it (TC-G-3.2.test.ts:60-66).
func epochUs(unixUs uint64) uint64 { return unixUs - matterEpochSec*1_000_000 }

// groupKeySet is the plan's key set 1 (tc-group-support.ts:72-83).
func groupKeySet() groupkeymanagement.GroupKeySetStruct {
	return groupkeymanagement.GroupKeySetStruct{
		GroupKeySetId: groupKeySetID, GroupKeySecurityPolicy: 0,
		EpochKey0: spec.ValueOf(groupEpochKey0), EpochStartTime0: spec.ValueOf(epochUs(1_600_000_000_000_000)),
		EpochKey1: spec.NullOf[[]byte](), EpochStartTime1: spec.NullOf[uint64](),
		EpochKey2: spec.NullOf[[]byte](), EpochStartTime2: spec.NullOf[uint64](),
	}
}

// groupPlan is the shared state of the group-communication cases
// (tc-group-support.ts): the commissioned node, the endpoint Groups lives
// on, and whether the root carries Groupcast.
type groupPlan struct {
	ctrl      *gocontroller.Controller
	node      *client.Node
	ep        uint16
	groupcast bool
	listener  bool
}

// aclAdmitsGroup is tc-group-support.ts:146 aclAdmitsGroupStep: commission,
// then add an ACL entry admitting the group to the existing ones, and the
// root's Groupcast decides the plan's branch (GroupcastBranch.learn, :371).
func (g *groupPlan) aclAdmitsGroup(privilege uint8) func(th *goTH) error {
	return func(th *goTH) error {
		var err error
		if g.ctrl, g.node, err = th.commission("dut"); err != nil {
			return err
		}
		if g.ep, err = th.mustEP("G.S & OO.S"); err != nil {
			return err
		}
		aclPath := attrPath(0, accesscontrol.ClusterID, accesscontrol.AttrAcl)
		v, err := th.readValue(g.node, aclPath)
		if err != nil {
			return err
		}
		var entries []accesscontrol.AccessControlEntryStruct
		for _, c := range v.Children {
			var e accesscontrol.AccessControlEntryStruct
			if err := e.DecodeTLV(c); err != nil {
				return err
			}
			entries = append(entries, e)
		}
		if len(entries) == 0 {
			return errors.New("the ACL read back empty: the administer entry is missing")
		}
		entries = append(entries, accesscontrol.AccessControlEntryStruct{
			Privilege: accesscontrol.AccessControlEntryPrivilegeEnum(privilege), AuthMode: authModeGroup,
			Subjects: spec.ValueOf([]uint64{uint64(groupID)}), Targets: spec.NullOf[[]accesscontrol.AccessControlTargetStruct](),
			FabricIndex: entries[0].FabricIndex,
		})
		if err := th.write(g.node, aclPath, listOf(entries...), client.WriteOptions{}); err != nil {
			return err
		}
		v, err = th.readValue(g.node, aclPath)
		if err != nil {
			return err
		}
		admits := false
		for _, c := range v.Children {
			var e accesscontrol.AccessControlEntryStruct
			if e.DecodeTLV(c) == nil && e.AuthMode == authModeGroup && !e.Subjects.Null && slices.Contains(e.Subjects.Value, uint64(groupID)) {
				admits = true
			}
		}
		if !admits {
			return fmt.Errorf("the ACL holds %d entries, none admitting group %d", len(v.Children), groupID)
		}
		sl, err := th.readValue(g.node, attrPath(0, descriptor.ClusterID, descriptor.AttrServerList))
		if err != nil {
			return err
		}
		g.groupcast = slices.ContainsFunc(sl.Children, func(c spec.Node) bool { return c.El.Uint == uint64(groupcast.ClusterID) })
		if g.groupcast {
			fm, err := th.readValue(g.node, attrPath(0, groupcast.ClusterID, 0xFFFC))
			if err != nil {
				return err
			}
			g.listener = fm.El.Uint&1 != 0
		}
		th.note("ACL admits group %d with privilege %d beside %d entries; the root %s Groupcast (Listener %v); Groups on endpoint %d",
			groupID, privilege, len(entries)-1, map[bool]string{true: "serves", false: "does not serve"}[g.groupcast], g.listener, g.ep)
		return nil
	}
}

// keyMaterial is tc-group-support.ts:183 keyMaterialStep: the key set the
// next step writes, produced in-process.
func keyMaterial(th *goTH) error {
	ks := groupKeySet()
	th.note("key set %d, policy TrustFirst, EpochKey0 %x, EpochStartTime0 %d", ks.GroupKeySetId, ks.EpochKey0.Value, ks.EpochStartTime0.Value)
	return nil
}

// keySetWrite is tc-group-support.ts:213 keySetWriteStep.
func (g *groupPlan) keySetWrite(th *goTH) error {
	if _, err := th.invoke(g.node, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetWrite),
		groupkeymanagement.KeySetWriteRequest{GroupKeySet: groupKeySet()}, false, client.InvokeOptions{}); err != nil {
		return err
	}
	th.note("KeySetWrite of key set %d: Success", groupKeySetID)
	return nil
}

// legacyOnly is GroupcastBranch.legacyStepsApply (tc-group-support.ts:409-412).
func (g *groupPlan) legacyOnly(run func(th *goTH) error) func(th *goTH) error {
	return func(th *goTH) error {
		if g.groupcast {
			return goSkip("The TH's root ServerList names the Groupcast cluster, so the plan skips to step 6 (GroupcastBranch.legacyStepsApply, tc-group-support.ts:409-412)")
		}
		return run(th)
	}
}

// groupcastOnly is GroupcastBranch.groupcastStepsApply (:414-424).
func (g *groupPlan) groupcastOnly(run func(th *goTH) error) func(th *goTH) error {
	return func(th *goTH) error {
		if !g.groupcast {
			return goSkip("The TH's root ServerList does not name the Groupcast cluster, so the plan skips its Groupcast steps (tc-group-support.ts:414-424)")
		}
		if !g.listener {
			return goSkip("The TH's root Groupcast FeatureMap lacks the Listener feature (tc-group-support.ts:418-423)")
		}
		return run(th)
	}
}

// groupKeyMap is tc-group-support.ts:239 groupKeyMapStep.
func (g *groupPlan) groupKeyMap(ids ...uint16) func(th *goTH) error {
	return func(th *goTH) error {
		m := make([]groupkeymanagement.GroupKeyMapStruct, 0, len(ids))
		for _, id := range ids {
			m = append(m, groupkeymanagement.GroupKeyMapStruct{GroupId: id, GroupKeySetId: groupKeySetID})
		}
		p := attrPath(0, groupkeymanagement.ClusterID, groupkeymanagement.AttrGroupKeyMap)
		if err := th.write(g.node, p, listOf(m...), client.WriteOptions{}); err != nil {
			return err
		}
		v, err := th.readValue(g.node, p)
		if err != nil {
			return err
		}
		th.note("GroupKeyMap reads %s", describeNode(v))
		return nil
	}
}

// addGroup is tc-group-support.ts:302 addGroupStep.
func (g *groupPlan) addGroup(th *goTH) error {
	if _, err := th.invoke(g.node, cmdPath(g.ep, groups.ClusterID, groups.CmdAddGroup),
		groups.AddGroupRequest{GroupId: groupID, GroupName: groupName}, true, client.InvokeOptions{}); err != nil {
		return err
	}
	th.note("AddGroup %d %q on endpoint %d: SUCCESS", groupID, groupName, g.ep)
	return nil
}

// joinGroup is tc-group-support.ts:429 joinGroupStep, with the endpoint
// Groups lives on.
func (g *groupPlan) joinGroup(th *goTH) error {
	if _, err := th.invoke(g.node, cmdPath(0, groupcast.ClusterID, groupcast.CmdJoinGroup),
		groupcast.JoinGroupRequest{GroupId: groupID, Endpoints: []uint16{g.ep}, KeySetId: groupKeySetID}, false, client.InvokeOptions{}); err != nil {
		return err
	}
	th.note("JoinGroup %d on endpoint %d with key set %d: Success", groupID, g.ep, groupKeySetID)
	return nil
}

// caseSC53 mirrors TC-SC-5.3: the group is provisioned on the daemon, and a
// command sent as a group message reaches the endpoint that joined.
func caseSC53() goCertCase {
	g := &groupPlan{}
	return goCertCase{
		tc: "TC-SC-5.3", line: 370, pics: []string{"MCORE.ROLE.COMMISSIONER", "GRPKEY.C"}, multicast: true,
		steps: []goStep{
			{n: "1a", line: 385, title: "TH should have the ACL entry with the AuthMode as Group by DUT", run: g.aclAdmitsGroup(privManage)},
			{n: "1b", line: 404, title: "DUT generates a random key and EpochKey0 assigned to GroupKeySetID 1", run: keyMaterial},
			{n: "2", line: 409, title: "KeySetWrite to GroupKeyManagement on EP0", pics: "GRPKEY.C.C00.Tx", run: g.keySetWrite},
			{n: "3", line: 418, title: "bind GroupId with GroupKeySetID in GroupKeyMap", pics: "GRPKEY.C.A0000", run: g.legacyOnly(g.groupKeyMap(groupID, 2))},
			{n: "4", line: 430, title: "AddGroup GroupID 1 \"GroupOne\"", pics: "G.C.C00.Tx", at: "G.S & OO.S", run: g.legacyOnly(g.addGroup)},
			{n: "5", line: 442, title: "AddGroup GroupID 2 sent as a group command using GroupID 1", pics: "G.C.C00.Tx", at: "G.S & OO.S", run: g.legacyOnly(func(th *goTH) error {
				return errors.New("the legacy groupcast of AddGroup is not ported: the reference daemon serves Groupcast")
			})},
			{n: "6", line: 455, title: "Groupcast JoinGroup on EP0 with GroupID 1, Endpoints and KeySetID 1", run: g.groupcastOnly(g.joinGroup)},
			{n: "7", line: 464, title: "a command sent as a group command using GroupID 1", run: g.groupcastOnly(g.toggleOverGroupcast)},
		},
	}
}

// toggleOverGroupcast sends OnOff Toggle to group 1 from the TH's own
// group state — the controller's key set 1 on its fabric, the IANA
// address the Groupcast join chose — and holds the joined endpoint to
// have toggled (TC-SC-5.3.test.ts:464-475).
func (g *groupPlan) toggleOverGroupcast(th *goTH) error {
	path := attrPath(g.ep, onoff.ClusterID, onoff.AttrOnOff)
	before, err := th.readValue(g.node, path)
	if err != nil {
		return err
	}
	f := g.ctrl.Fabric()
	st := memstore.New()
	idx, err := st.AddFabric(th.ctx, store.FabricRecord{
		FabricID: f.FabricID, NodeID: f.RootNodeID, RootPublicKey: f.RootPublicKey, CompressedID: f.Identity.CompressedFabricID, VendorID: f.RootVendorID,
	})
	if err != nil {
		return err
	}
	ks := groupKeySet()
	if err := st.UpsertGroupKeySet(th.ctx, store.GroupKeySet{
		FabricIndex: idx, GroupKeySetID: groupKeySetID, EpochKey0: groupEpochKey0, EpochStart0: ks.EpochStartTime0.Value,
	}); err != nil {
		return err
	}
	if err := st.SetGroupKeyMapping(th.ctx, store.GroupKeyMapping{FabricIndex: idx, GroupID: groupID, GroupKeySetID: groupKeySetID}); err != nil {
		return err
	}
	if err := st.UpsertGroupcastGroup(th.ctx, store.GroupcastGroup{FabricIndex: idx, GroupID: groupID, McastAddrPolicy: groupmsg.PolicyIanaAddr}); err != nil {
		return err
	}
	m, err := groupmsg.NewManager(st, th.log)
	if err != nil {
		return err
	}
	l, err := udp.New(udp.Config{LocalAddr: "[::]:0"})
	if err != nil {
		return err
	}
	defer func() { _ = l.Close() }()
	m.SetTransport(l)
	s, err := m.Sender(th.ctx, idx, groupID)
	if err != nil {
		return err
	}
	if err := s.Invoke(th.ctx, im.ConcreteCommandPath{Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, emptyFields, groupmsg.RequestOptions{}); err != nil {
		return err
	}
	want := !before.El.Bool
	var after spec.Node
	ok := waitFor(th.ctx, 10*time.Second, func() bool {
		after, err = th.readValue(g.node, path)
		return err == nil && after.El.Bool == want
	})
	if !ok {
		return fmt.Errorf("OnOff on endpoint %d reads %s after the group Toggle to %s, was %v", g.ep, describeNode(after), s.Address(), before.El.Bool)
	}
	th.note("group Toggle to [%s]:5540 on session %d: endpoint %d toggled %v → %v", s.Address(), s.SessionID(), g.ep, before.El.Bool, want)
	return nil
}

// caseSC61 mirrors TC-SC-6.1: group key management on the daemon.
func caseSC61() goCertCase {
	g := &groupPlan{}
	return goCertCase{
		tc: "TC-SC-6.1", line: 81, pics: []string{"MCORE.ROLE.COMMISSIONER", "GRPKEY.C"},
		steps: []goStep{
			{n: "1a", line: 92, title: "TH should have the ACL entry with the AuthMode as Group by DUT", run: g.aclAdmitsGroup(privOperate)},
			{n: "1b", line: 111, title: "DUT generates a random key and EpochKey0 assigned to GroupKeySetID 1", run: keyMaterial},
			{n: "2", line: 122, title: "KeySetWrite to GroupKeyManagement on EP0", pics: "GRPKEY.C.C00.Tx", run: g.keySetWrite},
			{n: "3", line: 131, title: "bind GroupID 1 with GroupKeySetID 1 in GroupKeyMap", pics: "GRPKEY.C.A0000", run: g.legacyOnly(g.groupKeyMap(groupID))},
			{n: "4", line: 141, title: "AddGroup GroupID 1 \"GroupOne\"", pics: "G.C.C00.Tx", at: "G.S & OO.S", run: g.legacyOnly(g.addGroup)},
			{n: "5", line: 151, title: "ViewGroup GroupID 1", pics: "G.C.C01.Tx", at: "G.S & OO.S", run: g.legacyOnly(func(th *goTH) error {
				r, err := th.invoke(g.node, cmdPath(g.ep, groups.ClusterID, groups.CmdViewGroup), groups.ViewGroupRequest{GroupId: groupID}, true, client.InvokeOptions{})
				if err != nil {
					return err
				}
				id, _ := structField(r.Fields, 1)
				name, _ := structField(r.Fields, 2)
				if id.El.Uint != uint64(groupID) || name.El.String != groupName {
					return fmt.Errorf("ViewGroupResponse answers group %s named %s", describeNode(id), describeNode(name))
				}
				th.note("ViewGroupResponse: group %d %q", groupID, groupName)
				return nil
			})},
			{n: "6", line: 203, title: "Groupcast JoinGroup on EP0 with GroupID 1, Endpoints and KeySetID 1", run: g.groupcastOnly(g.joinGroup)},
			{n: "7", line: 212, title: "read the Groupcast Membership attribute on EP0", run: g.groupcastOnly(func(th *goTH) error {
				v, err := th.readValue(g.node, attrPath(0, groupcast.ClusterID, groupcast.AttrMembership))
				if err != nil {
					return err
				}
				for _, c := range v.Children {
					var m groupcast.MembershipStruct
					if m.DecodeTLV(c) != nil {
						continue
					}
					if m.GroupId == groupID && m.KeySetId == groupKeySetID && m.Endpoints != nil && slices.Contains(*m.Endpoints, g.ep) {
						th.note("Membership lists group %d on endpoint %d with key set %d", groupID, g.ep, groupKeySetID)
						return nil
					}
				}
				return fmt.Errorf("Membership %s does not list group %d on endpoint %d with key set %d", describeNode(v), groupID, g.ep, groupKeySetID)
			})},
			{n: "8", line: 218, title: "KeySetRead", pics: "GRPKEY.C.C01.Tx", run: func(th *goTH) error {
				r, err := th.invoke(g.node, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetRead),
					groupkeymanagement.KeySetReadRequest{GroupKeySetId: groupKeySetID}, false, client.InvokeOptions{})
				if err != nil {
					return err
				}
				set, _ := structField(r.Fields, 0)
				id, _ := structField(set, 0)
				if id.El.Uint != uint64(groupKeySetID) {
					return fmt.Errorf("KeySetReadResponse carries key set %s", describeNode(id))
				}
				th.note("KeySetReadResponse carries key set %d", groupKeySetID)
				return nil
			}},
			{n: "9", line: 255, title: "KeySetRemove", pics: "GRPKEY.C.C03.Tx", run: func(th *goTH) error {
				if _, err := th.invoke(g.node, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetRemove),
					groupkeymanagement.KeySetRemoveRequest{GroupKeySetId: groupKeySetID}, false, client.InvokeOptions{}); err != nil {
					return err
				}
				th.note("KeySetRemove %d: Success", groupKeySetID)
				return nil
			}},
			{n: "10", line: 273, title: "KeySetReadAllIndices", pics: "GRPKEY.C.C04.Tx", run: func(th *goTH) error {
				r, err := th.invoke(g.node, cmdPath(0, groupkeymanagement.ClusterID, groupkeymanagement.CmdKeySetReadAllIndices),
					groupkeymanagement.KeySetReadAllIndicesRequest{}, false, client.InvokeOptions{})
				if err != nil {
					return err
				}
				ids, _ := structField(r.Fields, 0)
				// Exactly the IPK (:296-300).
				if len(ids.Children) != 1 || ids.Children[0].El.Uint != uint64(ipkKeySetID) {
					return fmt.Errorf("KeySetReadAllIndicesResponse lists %s; after removing %d only the IPK's %d may remain", describeNode(ids), groupKeySetID, ipkKeySetID)
				}
				th.note("KeySetReadAllIndicesResponse lists only the IPK's key set %d", ipkKeySetID)
				return nil
			}},
		},
	}
}
