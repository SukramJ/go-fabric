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

	"github.com/SukramJ/go-fabric/client/discovery"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The device-side DD cases: matter.js's TC-DD-3.18, 3.20 and 3.21 have its
// controller as the DUT commissioner and all-clusters apps as the THs.
// Read mirrored, the daemon is the commissionee: it advertises the
// discriminator its code names and no other, it is commissioned from its
// QR code, it goes back into commissioning mode once its last fabric is
// removed, and it serves the endpoints the case reaches. The DD cases
// whose substance is the controller's alone (parsing and refusing
// onboarding payloads) are phase 29b's.

func init() {
	for _, c := range []func() goCertCase{caseDD318, caseDD320, caseDD321} {
		registerGoCert(c)
	}
}

// The DD budgets (support/chip-testing/test/cert/tc-dd-support.ts:78-79,
// :873, :884).
const (
	ddCommissioningLogTimeout = 30 * time.Second
	ddMDNSTimeout             = 30 * time.Second
	ddAbsentGiveUp            = 20 * time.Second
	ddAbsentWait              = 90 * time.Second
)

// ddPICS is the DD cases' PICS (TC-DD-3.20.test.ts:40).
var ddPICS = []string{"MCORE.ROLE.COMMISSIONER", "MCORE.DD.QR_COMMISSIONING"}

// ddRun is a DD case's commissioner and the DNS-SD cache it browses.
type ddRun struct {
	names *discovery.Names
	ctrl  *gocontroller.Controller
	nodes map[*bridgeProcess]*client.Node
}

func (d *ddRun) setup(th *goTH) error {
	if d.ctrl != nil {
		return nil
	}
	var err error
	if d.names, err = th.names(); err != nil {
		return err
	}
	if d.ctrl, err = th.controller("dut"); err != nil {
		return err
	}
	d.nodes = map[*bridgeProcess]*client.Node{}
	return nil
}

// discriminatorHonored is recordDiscriminatorHonored
// (tc-dd-support.ts:931-958), read mirrored: a code naming a discriminator
// no device advertises commissions nothing — the daemon answers to its own
// discriminator only — and the daemon stays commissionable.
func (d *ddRun) discriminatorHonored(devices ...*bridgeProcess) func(th *goTH) error {
	return func(th *goTH) error {
		if err := d.setup(th); err != nil {
			return err
		}
		dev := th.dut
		// absentDiscriminator (tc-dd-support.ts:895-905): the device's
		// discriminator with every bit flipped, which no device of the run
		// may advertise.
		absent := dev.info.discriminator ^ 0xFFF
		if slices.ContainsFunc(devices, func(b *bridgeProcess) bool { return b.info.discriminator == absent }) {
			return fmt.Errorf("discriminator %d was meant to name no device in this run", absent)
		}
		code, err := qrWith(dev.info.qrCode, absent)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(th.ctx, ddAbsentGiveUp)
		defer cancel()
		if n, err := d.ctrl.Pair(ctx, code, gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()}); err == nil {
			_ = d.ctrl.Registry().Decommission(th.ctx, n)
			return fmt.Errorf("a code naming discriminator %d commissioned a device", absent)
		}
		for _, b := range append([]*bridgeProcess{dev}, devices...) {
			if _, err := th.commissionable(d.names, b.info.discriminator, b.info.port, ddMDNSTimeout); err != nil {
				return fmt.Errorf("after the refused code: %w", err)
			}
		}
		th.note("a code naming discriminator %d (no device advertises it) commissioned nothing within %s; the device still advertises %d", absent, ddAbsentGiveUp, dev.info.discriminator)
		return nil
	}
}

// commissionableStep is recordCommissionable (tc-dd-support.ts:1108-1114).
func (d *ddRun) commissionableStep(b func(th *goTH) *bridgeProcess) func(th *goTH) error {
	return func(th *goTH) error {
		if err := d.setup(th); err != nil {
			return err
		}
		dev := b(th)
		found, err := th.commissionable(d.names, dev.info.discriminator, dev.info.port, ddMDNSTimeout)
		if err != nil {
			return err
		}
		th.note("commissionable record %s for discriminator %d", found.InstanceID, dev.info.discriminator)
		return nil
	}
}

// parseStep is recordParse (tc-dd-support.ts:264-312), read mirrored: the
// QR code the device prints decodes to its own discriminator and passcode.
func parseStep(b func(th *goTH) *bridgeProcess) func(th *goTH) error {
	return func(th *goTH) error {
		dev := b(th)
		p, err := setup.DecodeQRCode(dev.info.qrCode)
		if err != nil {
			return fmt.Errorf("the device's QR code %q: %w", dev.info.qrCode, err)
		}
		if p.Discriminator != dev.info.discriminator || p.Passcode != dev.info.passcode {
			return fmt.Errorf("the QR code names discriminator %d passcode %d, the device %d and %d", p.Discriminator, p.Passcode, dev.info.discriminator, dev.info.passcode)
		}
		th.note("QR %s: version %d, vendor 0x%04X product 0x%04X, flow %d, discriminator %d", dev.info.qrCode, p.Version, p.VendorID, p.ProductID, p.CustomFlow, p.Discriminator)
		return nil
	}
}

// commissionByQR is commissionByQr (tc-dd-support.ts:1458): the device is
// commissioned from its QR code and advertises its operational record.
func (d *ddRun) commissionByQR(b func(th *goTH) *bridgeProcess) func(th *goTH) error {
	return func(th *goTH) error {
		if err := d.setup(th); err != nil {
			return err
		}
		dev := b(th)
		n, err := th.pair(d.ctrl, dev.info.qrCode)
		if err != nil {
			return err
		}
		d.nodes[dev] = n
		addr, ok := n.PeerAddress()
		if !ok {
			return errors.New("the commissioned node has no operational address")
		}
		inst := operationalInstance(d.ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
		if err := th.awaitOperational(d.names, inst, true, ddCommissioningLogTimeout); err != nil {
			return err
		}
		th.note("commissioned from %s as node %d; it advertises %s", dev.info.qrCode, addr.NodeID, inst)
		return nil
	}
}

// interacts is TC-DD-3.18's step 4: the commissioner still reads the
// device.
func (d *ddRun) interacts(b func(th *goTH) *bridgeProcess) func(th *goTH) error {
	return func(th *goTH) error {
		n := d.nodes[b(th)]
		if n == nil {
			return errors.New("the device was not commissioned by the preceding steps")
		}
		v, err := th.readValue(n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorId))
		if err != nil {
			return err
		}
		th.note("read VendorID 0x%04X", v.El.Uint)
		return nil
	}
}

func dutDevice(th *goTH) *bridgeProcess { return th.dut }

// caseDD318 mirrors TC-DD-3.18: two devices commissioned in turn from
// their QR codes, both reachable after.
func caseDD318() goCertCase {
	d := &ddRun{}
	var th2 *bridgeProcess
	second := func(th *goTH) *bridgeProcess {
		if th2 == nil {
			th2 = th.device(1)
		}
		return th2
	}
	return goCertCase{
		tc: "TC-DD-3.18", line: 98, pics: ddPICS,
		steps: []goStep{
			{n: "0", line: 104, title: "the commissioner uses the discriminator the code names", run: func(th *goTH) error {
				return d.discriminatorHonored(second(th))(th)
			}},
			{n: "1.a", line: 114, title: "TH1 in commissioning mode", run: func(th *goTH) error {
				if th.dut.info.discriminator == second(th).info.discriminator {
					return errors.New("the two THs advertise the same discriminator")
				}
				return d.commissionableStep(dutDevice)(th)
			}},
			{n: "1.b", line: 138, title: "TH2 in commissioning mode", run: d.commissionableStep(second)},
			{n: "2.a", line: 147, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan TH1's QR code", run: parseStep(dutDevice)},
			{n: "2.b", line: 160, title: "commission TH1 from its QR code", run: d.commissionByQR(dutDevice)},
			{n: "3.a", line: 184, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan TH2's QR code", run: parseStep(second)},
			{n: "3.b", line: 197, title: "commission TH2 from its QR code", run: d.commissionByQR(second)},
			{n: "4.a", line: 220, title: "the commissioner can still interact with TH1", run: d.interacts(dutDevice)},
			{n: "4.b", line: 226, title: "the commissioner can still interact with TH2", run: d.interacts(second)},
		},
	}
}

// caseDD320 mirrors TC-DD-3.20: commissioned, unpaired, back into
// commissioning mode, commissioned again.
func caseDD320() goCertCase {
	d := &ddRun{}
	var unpairedAt int
	return goCertCase{
		tc: "TC-DD-3.20", line: 38, pics: ddPICS,
		steps: []goStep{
			{n: "0", line: 43, title: "the commissioner uses the discriminator the code names", run: d.discriminatorHonored()},
			{n: "1", line: 53, title: "TH in commissioning mode", run: d.commissionableStep(dutDevice)},
			{n: "2.a", line: 59, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan TH's QR code", run: parseStep(dutDevice)},
			{n: "2.b", line: 71, title: "commission TH from its QR code", run: d.commissionByQR(dutDevice)},
			// recordUnpair (tc-dd-support.ts:1301-1331): the fabric removal,
			// and the device ending the fabric's presence — its operational
			// record withdrawn, as its log reports.
			{n: "3", line: 85, title: "unpair the TH", run: func(th *goTH) error {
				n := d.nodes[th.dut]
				if n == nil {
					return errors.New("the TH was not commissioned by the preceding step")
				}
				fi, err := th.ownFabricIndex(n)
				if err != nil {
					return err
				}
				addr, _ := n.PeerAddress()
				inst := operationalInstance(d.ctrl.Fabric().Identity.CompressedFabricID, addr.NodeID)
				unpairedAt = len(th.dut.snapshotStderr())
				if err := d.ctrl.Registry().Decommission(th.ctx, n); err != nil {
					return fmt.Errorf("RemoveFabric: %w", err)
				}
				delete(d.nodes, th.dut)
				if !waitFor(th.ctx, 15*time.Second, func() bool {
					return strings.Contains(th.dut.snapshotStderr()[unpairedAt:], "msg=matter.mdns.fabric_withdrawn")
				}) {
					return errors.New("the TH logged no withdrawal of the removed fabric's record")
				}
				if err := th.awaitOperational(d.names, inst, false, ddCommissioningLogTimeout); err != nil {
					return err
				}
				th.note("removed fabric %d; the TH withdrew %s", fi, inst)
				return nil
			}},
			// recordBackInCommissioningMode (tc-dd-support.ts:1358-1412): the
			// device without a fabric opens its window again and advertises.
			{n: "4", line: 93, title: "the TH back in commissioning mode", run: func(th *goTH) error {
				if !waitFor(th.ctx, ddCommissioningLogTimeout, func() bool {
					return strings.Contains(th.dut.snapshotStderr()[unpairedAt:], "msg=commissioning.own_window.open")
				}) {
					return errors.New("the TH logged no reopened commissioning window after its last fabric left")
				}
				return d.commissionableStep(dutDevice)(th)
			}},
			{n: "5.a", line: 100, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan TH's QR code", run: parseStep(dutDevice)},
			{n: "5.b", line: 112, title: "commission TH from its QR code again", run: d.commissionByQR(dutDevice)},
		},
	}
}

// caseDD321 mirrors TC-DD-3.21: commissioned from its QR code, the device's
// On/Off Light endpoints each take an On.
func caseDD321() goCertCase {
	d := &ddRun{}
	return goCertCase{
		tc: "TC-DD-3.21", line: 84, pics: ddPICS,
		steps: []goStep{
			{n: "0", line: 89, title: "the commissioner uses the discriminator the code names", run: d.discriminatorHonored()},
			{n: "1", line: 99, title: "TH in commissioning mode", run: d.commissionableStep(dutDevice)},
			{n: "2.a", line: 105, pics: "MCORE.DD.SCAN_QR_CODE", title: "scan TH's QR code", run: parseStep(dutDevice)},
			{n: "2.b", line: 117, title: "commission TH from its QR code", run: d.commissionByQR(dutDevice)},
			{n: "3", line: 131, title: "each On/Off Light endpoint takes an On", run: func(th *goTH) error {
				n := d.nodes[th.dut]
				if n == nil {
					return errors.New("the TH was not commissioned by the preceding step")
				}
				eps, err := onOffLightEndpoints(th, n)
				if err != nil {
					return err
				}
				// The TS holds its all-clusters TH to two such endpoints
				// (:141-145), that app's topology; the reference daemon's
				// topology has what it has, and the step is about each.
				if len(eps) == 0 {
					return errors.New("the TH implements the On/Off Light device type on no endpoint")
				}
				for _, ep := range eps {
					if _, err := th.invoke(n, cmdPath(ep, onoff.ClusterID, onoff.CmdOn), emptyFields, false, client.InvokeOptions{}); err != nil {
						return err
					}
					v, err := th.readValue(n, attrPath(ep, onoff.ClusterID, onoff.AttrOnOff))
					if err != nil {
						return err
					}
					if !v.El.Bool {
						return fmt.Errorf("endpoint %d reports OnOff false after the On", ep)
					}
				}
				th.note("On/Off Light endpoints %v each took an On and report OnOff true", eps)
				return nil
			}},
		},
	}
}

// onOffLightEndpoints is TC-DD-3.21.test.ts:50-82: the root's PartsList
// entries whose DeviceTypeList holds the On/Off Light (0x0100, :36).
func onOffLightEndpoints(th *goTH, n *client.Node) ([]uint16, error) {
	parts, err := th.readValue(n, attrPath(0, descriptor.ClusterID, descriptor.AttrPartsList))
	if err != nil {
		return nil, err
	}
	var out []uint16
	for _, p := range parts.Children {
		ep := uint16(p.El.Uint) //nolint:gosec // an endpoint number
		dts, err := th.readValue(n, attrPath(ep, descriptor.ClusterID, descriptor.AttrDeviceTypeList))
		if err != nil {
			return nil, err
		}
		for _, dt := range dts.Children {
			if id, ok := structField(dt, 0); ok && id.El.Uint == 0x0100 {
				out = append(out, ep)
				break
			}
		}
	}
	return out, nil
}
