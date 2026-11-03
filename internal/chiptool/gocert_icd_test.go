// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The ICD client cases in matter.js's own direction: the module's
// controller is the DUT ICD client (client.Node.ICD, controller/client/
// icd.go), the reference daemon with --icd-lit the lit-icd TH
// (examples/reference-bridge/icd.go, after matter.js's IcdTestInstance and
// IcdTestEventServer). matter.js reads the TH's log for the CommandDataIB
// of each command (tc-support.ts expectCommandInvoke); here a step's
// evidence is the DUT's own result, the TH's IcdManagement state read
// back, and the TH's log of the invoke it served (bridge's
// matter.rx.im.invoke with the command's path).

func init() {
	registerGoCert(caseICDM61)
	registerGoCert(caseICDB13)
}

// icdDeclare are the --icd-lit composition's ICD Management codes, which
// the committed slices (generated without the flag, ICDM.S=0) do not
// describe: the server on the root with the four features matter.js's
// lit-icd app has (IcdTestInstance.ts:67-72; examples/reference-bridge
// icd.go icdConfig).
var icdDeclare = map[string]string{
	"ICDM.S": "1", "ICDM.S.F00": "1", "ICDM.S.F01": "1", "ICDM.S.F02": "1", "ICDM.S.F03": "1",
}

// icdCheckInTimeout is TC-ICDB-1.3's CHECK_IN_TIMEOUT (:78).
const icdCheckInTimeout = 90 * time.Second

// icdRun is an ICD case's DUT controller and the node it commissioned the
// TH as, with the ICD client's events in arrival order (matter.js
// InProcessIcdClient's #events, InProcessControllerAdapter.ts:1137-1170, :1267-1272).
type icdRun struct {
	ctrl *gocontroller.Controller
	n    *client.Node

	mu     sync.Mutex
	events []icdEvent
}

// icdEvent is a CertIcdEvent: an accepted Check-In or a key refresh.
type icdEvent struct {
	checkIn *client.ICDCheckIn
	key     []byte
}

func (r *icdRun) push(e icdEvent) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *icdRun) snapshot() []icdEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]icdEvent(nil), r.events...)
}

// waitFor is InProcessIcdClient.waitFor (:1224-1265): the first event of
// the kind at or after index from, within the timeout.
func (r *icdRun) waitFor(th *goTH, checkIn bool, from int, d time.Duration) (icdEvent, int, error) {
	var found icdEvent
	at := -1
	waitFor(th.ctx, d, func() bool {
		for i, e := range r.snapshot() {
			if i >= from && (e.checkIn != nil) == checkIn {
				found, at = e, i
				return true
			}
		}
		return false
	})
	if at < 0 {
		kind := "key refresh"
		if checkIn {
			kind = "Check-In"
		}
		return icdEvent{}, -1, fmt.Errorf("no ICD %s recorded within %s (recorded: %d)", kind, d, len(r.snapshot()))
	}
	return found, at, nil
}

// commission is step 0's commissionChecked (tc-dd-support.ts:130-155)
// with the TH's passcode and discriminator, as the QR code names them,
// and the DUT's default subscription up — which a registration needs
// (IcdClient.register, controller/client/icd.go Register).
func (r *icdRun) commission(th *goTH) error {
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
	r.n.ICD().OnCheckIn(func(c client.ICDCheckIn) { r.push(icdEvent{checkIn: &c}) })
	r.n.ICD().OnKeyRefreshed(func() {
		if reg, ok := r.n.ICD().Registration(); ok {
			r.push(icdEvent{key: bytes.Clone(reg.Key)})
		}
	})
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
	return nil
}

// served is the TH's log of an invoke of an IcdManagement command on the
// root since a mark (bridge's matter.rx.im.invoke req_paths).
func served(th *goTH, from int, cluster, cmd uint32) bool {
	return strings.Contains(thLogSince(th, from), fmt.Sprintf(`req_paths="ep=0 cl=0x%04X cmd=0x%X"`, cluster, cmd))
}

// registeredClients reads the TH's RegisteredClients through n (fabric
// filtered to n's fabric): CheckInNodeID, MonitoredSubject and ClientType
// of each entry.
func registeredClients(th *goTH, n *client.Node) ([][3]uint64, error) {
	v, err := th.readValue(n, attrPath(0, icdmanagement.ClusterID, icdmanagement.AttrRegisteredClients))
	if err != nil {
		return nil, err
	}
	out := make([][3]uint64, 0, len(v.Children))
	for _, e := range v.Children {
		var row [3]uint64
		for i, tag := range []uint8{1, 2, 4} {
			if f, ok := structField(e, tag); ok {
				row[i] = f.El.Uint
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// ownNodeID is the DUT's operational node id on its fabric.
func (r *icdRun) ownNodeID() uint64 { return r.ctrl.Fabric().RootNodeID }

// register is the RegisterClient half of TC-ICDM-6.1 step 1 and
// TC-ICDB-1.3 step 1: the DUT registers, the TH served RegisterClient and
// lists the DUT as its Check-In client with the DUT's node id as
// CheckInNodeID and MonitoredSubject (icdRegisterClientFields,
// tc-support.ts) — the Key is not readable back, its length is the DUT's.
func (r *icdRun) register(th *goTH, opts client.ICDRegisterOptions) (client.ICDRegistration, error) {
	from := mark(th)
	ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
	defer cancel()
	if err := r.n.ICD().Register(ctx, opts); err != nil {
		return client.ICDRegistration{}, fmt.Errorf("RegisterClient: %w", err)
	}
	reg, ok := r.n.ICD().Registration()
	if !ok {
		return reg, errors.New("the DUT registered without storing its registration")
	}
	if !waitFor(th.ctx, 10*time.Second, func() bool { return served(th, from, icdmanagement.ClusterID, icdmanagement.CmdRegisterClient) }) {
		return reg, errors.New("the TH logged no RegisterClient it served")
	}
	clients, err := registeredClients(th, r.n)
	if err != nil {
		return reg, err
	}
	own := r.ownNodeID()
	if len(clients) != 1 || clients[0][0] != own || clients[0][1] != own || clients[0][2] != uint64(icdmanagement.ClientTypePermanent) {
		return reg, fmt.Errorf("the TH lists %v (CheckInNodeID, MonitoredSubject, ClientType), want the DUT's node %d as both, Permanent", clients, own)
	}
	th.note("RegisterClient answered with ICDCounter=%d; the TH served it and lists CheckInNodeID=MonitoredSubject=%d (the DUT), ClientType Permanent; the DUT's Key has %d bytes",
		reg.CounterStart, own, len(reg.Key))
	return reg, nil
}

// caseICDM61 is TC-ICDM-6.1: RegisterClient, UnregisterClient and
// StayActiveRequest from the DUT, each well formed.
func caseICDM61() goCertCase {
	r := &icdRun{}
	return goCertCase{
		tc: "TC-ICDM-6.1", line: 79, role: roleDUT, pics: []string{"ICDM.C"},
		// --icd-lit alone: the app's default timings (IcdTestInstance.ts:107-108),
		// as matter.js starts its lit-icd TH for this case without app args.
		daemonArgs: []string{"--icd-lit"}, declare: icdDeclare,
		finalize: func(th *goTH) {
			if r.n != nil {
				_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
			}
		},
		steps: []goStep{
			{n: "0", line: 90, pics: "ICDM.C", need: "ICDM.S", title: "commission TH to the DUT", run: r.commission},
			{n: "1", line: 107, pics: "ICDM.C.C00.Tx", need: "ICDM.S.F00", title: "the DUT issues RegisterClient", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("step 0 did not commission the TH")
				}
				_, err := r.register(th, client.ICDRegisterOptions{})
				return err
			}},
			{n: "2", line: 127, pics: "ICDM.C.C02.Tx", need: "ICDM.S.F00", title: "the DUT issues UnregisterClient", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("step 0 did not commission the TH")
				}
				if _, ok := r.n.ICD().Registration(); !ok {
					return errors.New("step 1 did not register the DUT")
				}
				from := mark(th)
				ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
				defer cancel()
				if err := r.n.ICD().Unregister(ctx); err != nil {
					return fmt.Errorf("UnregisterClient: %w", err)
				}
				if !waitFor(th.ctx, 10*time.Second, func() bool { return served(th, from, icdmanagement.ClusterID, icdmanagement.CmdUnregisterClient) }) {
					return errors.New("the TH logged no UnregisterClient it served")
				}
				clients, err := registeredClients(th, r.n)
				if err != nil {
					return err
				}
				if len(clients) != 0 {
					return fmt.Errorf("the TH still lists %v after the UnregisterClient", clients)
				}
				th.note("UnregisterClient answered SUCCESS; the TH served it and lists no Check-In client of the DUT's fabric")
				return nil
			}},
			{n: "3", line: 154, pics: "ICDM.C.C03.Tx", need: "ICDM.S.F02", title: "the DUT issues StayActiveRequest", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("step 0 did not commission the TH")
				}
				const stayActive = 10 * time.Second // STAY_ACTIVE_DURATION_MS (:32)
				from := mark(th)
				ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
				defer cancel()
				promised, err := r.n.ICD().StayActive(ctx, stayActive)
				if err != nil {
					return fmt.Errorf("StayActiveRequest: %w", err)
				}
				if !waitFor(th.ctx, 10*time.Second, func() bool { return served(th, from, icdmanagement.ClusterID, icdmanagement.CmdStayActiveRequest) }) {
					return errors.New("the TH logged no StayActiveRequest it served")
				}
				th.note("StayActiveRequest of %s answered with PromisedActiveDuration %s; the TH served it", stayActive, promised)
				return nil
			}},
		},
	}
}

// icdbEnableKey is TC-ICDB-1.3's ENABLE_KEY (:57).
var icdbEnableKey, _ = hex.DecodeString("00112233445566778899aabbccddeeff")

// The counter triggers TH2 sends (:64-65), the daemon's icd.go handles.
const (
	icdInvalidateHalfCounterValues uint64 = 0x0046_0000_0000_0003
	icdInvalidateAllCounterValues  uint64 = 0x0046_0000_0000_0004
)

// icdKeyRefreshOffset is KEY_REFRESH_OFFSET (:81): a Check-In with this
// many counter values used asks for a new key.
const icdKeyRefreshOffset = 1 << 31

// caseICDB13 is TC-ICDB-1.3: the DUT registers as a Check-In client of a
// TH with a second administrator, TH2; TH2's test event triggers
// invalidate the TH's counter values, and the DUT refreshes its key on a
// Check-In showing 2^31 values used and drops a Check-In whose counter
// repeats an earlier one.
func caseICDB13() goCertCase {
	r := &icdRun{}
	var (
		th2     *gocontroller.Controller
		th2Node *client.Node
		reg1    client.ICDRegistration
		// refreshFrom is where step 2a left the TH's log and the event
		// list (:89, :286).
		refreshLog, refreshEvents int
	)
	trigger := func(th *goTH, eventTrigger uint64, label string) error {
		if th2Node == nil {
			return errors.New("step 0 did not commission the TH to TH2")
		}
		from := mark(th)
		req := generaldiagnostics.TestEventTriggerRequest{EnableKey: icdbEnableKey, EventTrigger: eventTrigger}
		if _, err := th.invoke(th2Node, cmdPath(0, generaldiagnostics.ClusterID, generaldiagnostics.CmdTestEventTrigger), req, false, client.InvokeOptions{}); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if !waitFor(th.ctx, 10*time.Second, func() bool {
			return served(th, from, generaldiagnostics.ClusterID, generaldiagnostics.CmdTestEventTrigger) &&
				strings.Contains(thLogSince(th, from), "msg=icd.test_event.counter_advance")
		}) {
			return fmt.Errorf("%s: the TH logged no counter advance for the trigger it served", label)
		}
		th.note("%s answered SUCCESS; the TH served it and advanced its Check-In counter", label)
		return nil
	}
	offset := func(counter, start uint32) uint32 { return counter - start }
	return goCertCase{
		tc: "TC-ICDB-1.3", line: 145, role: roleDUT, pics: []string{"ICDB.C"},
		// TH_ARGS (:68-75): the lit-icd app with a 5 s idle and a 1 s active
		// window, and TestEventTrigger armed with ENABLE_KEY.
		daemonArgs: []string{"--icd-lit", "--icd-idle-mode-duration", "5", "--icd-active-mode-duration-ms", "1000", "--enable-key", hex.EncodeToString(icdbEnableKey)},
		declare:    icdDeclare,
		finalize: func(th *goTH) {
			if r.n != nil {
				_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
			}
			if th2Node != nil {
				_ = th2.Registry().Decommission(th.ctx, th2Node)
			}
		},
		steps: []goStep{
			{n: "0", line: 158, pics: "ICDB.C", need: "ICDM.S & ICDM.S.F00 & ICDM.S.F02", title: "commission TH1 to the DUT, and to TH2 through a window the DUT opens", run: func(th *goTH) error {
				if err := r.commission(th); err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
				defer cancel()
				// openCommissioningWindow({timeout: 180, enhanced: true}) (:177).
				w, err := r.n.OpenCommissioningWindow(ctx, 180*time.Second, client.WindowOptions{})
				if err != nil {
					return fmt.Errorf("the DUT opens an enhanced commissioning window: %w", err)
				}
				if th2, err = th.controller("th2"); err != nil {
					return err
				}
				from := mark(th)
				// TH2 commissions without a subscription of its own: matter.js
				// commissions it with one and ends it at once (:192), so it does
				// not register as a Check-In client once TH1 turns LIT.
				if th2Node, err = th.pair(th2, w.ManualCode); err != nil {
					return fmt.Errorf("TH2 commissions TH1 through the DUT's window: %w", err)
				}
				if !waitFor(th.ctx, ddCommissioningLogTimeout, func() bool {
					return strings.Contains(thLogSince(th, from), "msg=matter.gencomm.cmd_ok command_name=CommissioningComplete")
				}) {
					return errors.New("the TH logged no completed commissioning for TH2")
				}
				th.note("the DUT opened an enhanced window (manual code %s); TH2 commissioned TH1 through it, without a subscription", w.ManualCode)
				return nil
			}},
			{n: "1", line: 201, pics: "ICDB.C", title: "the DUT sends RegisterClient with CheckInNodeID, MonitoredSubject and Key1", run: func(th *goTH) error {
				if r.n == nil || th2Node == nil {
					return errors.New("step 0 did not commission the TH to the DUT and TH2")
				}
				var err error
				// TH1 has a second administrator, TH2, the plan's own topology (:211).
				if reg1, err = r.register(th, client.ICDRegisterOptions{AllowMultiAdmin: true}); err != nil {
					return err
				}
				// An ICD sends Check-Ins only to a client without an active
				// subscription (Core § 9.15): the DUT ends its own (:232-246,
				// stopSubscription), and TH2's NodeLabel write makes the TH report
				// to it at once, which ends it on the TH's side too.
				events := len(r.snapshot())
				ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
				defer cancel()
				if err := r.n.SetAutoSubscribe(ctx, false); err != nil {
					return fmt.Errorf("the DUT ends its subscription: %w", err)
				}
				if err := th.write(th2Node, attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel), spec.String("TC-ICDB-1.3"), client.WriteOptions{}); err != nil {
					return fmt.Errorf("TH2 writes NodeLabel: %w", err)
				}
				e, _, err := r.waitFor(th, true, events, icdCheckInTimeout)
				if err != nil {
					return fmt.Errorf("the DUT ended its subscription but accepted no Check-In: %w", err)
				}
				// Read by TH2, so fabric-filtered to TH2's own registrations.
				th2Clients, err := registeredClients(th, th2Node)
				if err != nil {
					return err
				}
				if len(th2Clients) != 0 {
					return fmt.Errorf("TH2 is a Check-In client of TH1: %v", th2Clients)
				}
				th.note("the DUT ended its subscription and accepted a Check-In, counter %d, offset %d; TH2's RegisteredClients on TH1 is empty",
					e.checkIn.Counter, offset(e.checkIn.Counter, reg1.CounterStart))
				return nil
			}},
			{n: "2a", line: 281, pics: "ICDB.C", title: "TH2 sends TestEventTrigger for the Invalidate ICD half counter values event", run: func(th *goTH) error {
				refreshLog, refreshEvents = mark(th), len(r.snapshot())
				return trigger(th, icdInvalidateHalfCounterValues, "TestEventTrigger (half counter)")
			}},
			{n: "2b", line: 300, pics: "ICDB.C", title: "on a Check-In with 2^31 counter values used the DUT refreshes its entry with Key2", run: func(th *goTH) error {
				if reg1.Key == nil {
					return errors.New("step 1 did not register the DUT")
				}
				refreshed, at, err := r.waitFor(th, false, refreshEvents, icdCheckInTimeout)
				if err != nil {
					return fmt.Errorf("key refresh: %w", err)
				}
				var trig *client.ICDCheckIn
				for _, e := range r.snapshot()[refreshEvents:at] {
					if e.checkIn != nil && offset(e.checkIn.Counter, reg1.CounterStart) >= icdKeyRefreshOffset {
						trig = e.checkIn
						break
					}
				}
				if trig == nil {
					return errors.New("no Check-In with an offset of 2^31 or more preceded the key refresh")
				}
				if bytes.Equal(refreshed.key, reg1.Key) {
					return errors.New("the DUT re-registered with the key it had")
				}
				if !served(th, refreshLog, icdmanagement.ClusterID, icdmanagement.CmdRegisterClient) {
					return errors.New("the TH logged no RegisterClient for the refresh")
				}
				th.note("ICDCounter2=%d, offset %d; the DUT refreshed its key (Key2 %x, Key1 %x) and the TH served the refreshing RegisterClient",
					trig.Counter, offset(trig.Counter, reg1.CounterStart), refreshed.key, reg1.Key)
				return nil
			}},
			{n: "3", line: 376, pics: "ICDB.C", title: "TH2 sends TestEventTrigger to increase the ICD counter to an invalid value", run: func(th *goTH) error {
				dutLog := len(th.logs.String())
				if err := trigger(th, icdInvalidateAllCounterValues, "TestEventTrigger (all counter values)"); err != nil {
					return err
				}
				// The DUT's own log of the drop (:400-406): matter.js's "Dropping
				// replayed check-in from peer", the module's peer table's
				// icd.peer.replayed_checkin — a Check-In the TH sent after the
				// trigger, received and dropped.
				if !waitFor(th.ctx, icdCheckInTimeout, func() bool {
					return strings.Contains(th.logs.String()[dutLog:], "msg=icd.peer.replayed_checkin")
				}) {
					return errors.New("the DUT logged no dropped Check-In within the Check-In timeout")
				}
				var counters []uint32
				var repeated []uint32
				for _, e := range r.snapshot() {
					if e.checkIn == nil {
						continue
					}
					for _, c := range counters {
						if c == e.checkIn.Counter {
							repeated = append(repeated, c)
						}
					}
					counters = append(counters, e.checkIn.Counter)
				}
				if len(repeated) > 0 {
					return fmt.Errorf("the DUT accepted counter(s) %v twice", repeated)
				}
				th.note("the DUT dropped the TH's replayed Check-In (icd.peer.replayed_checkin) and accepted no counter twice among %d Check-Ins", len(counters))
				return nil
			}},
		},
	}
}
