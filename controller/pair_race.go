// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// The sub-steps of "Discovery" 2.1 a Pair over both transports reports,
// one per transport: started when its search starts, then succeeded for
// the transport of the device that won PASE, skipped for the other, or
// failed with the search's error. They are this module's names (matter.js
// reports no step for its discovery), dotted as matter.js's step names
// are ("GeneralCommissioning.ArmFailsafe"), with Discovery's number 2.1.
const (
	PairingStepDiscoveryIP  = commissioner.StepDiscovery + ".IP"
	PairingStepDiscoveryBLE = commissioner.StepDiscovery + ".BLE"
)

// pairDiscovered is the commissioning of a device Pair has to find: a
// race over every transport the controller has and the code allows —
// DNS-SD, and BLE with [PairOptions].BLE — in which every device found
// is commissioned in parallel and the first to establish PASE wins
// (discovery.CommissioningRace, matter.js CommissioningDiscovery over
// ParallelPaseDiscovery, packages/node/src/behavior/system/controller/
// discovery/CommissioningDiscovery.ts:23-91 at 862ad75f, which
// CommissioningController.commissionNode runs, CommissioningController.ts:462-500).
// The transports are matter.js's selection (Discovery.ts:272-273): the IP
// network always, BLE where a QR code's capabilities name it or the code
// names none (a manual code).
func (c *Controller) pairDiscovered(ctx context.Context, pc PairingCode, identity discovery.Identity, opts PairOptions) (*client.Node, error) {
	r := &pairRace{progress: opts.Progress}
	var transports []discovery.Transport
	if c.names != nil {
		sc := discovery.NewScanner(c.names)
		defer sc.Close()
		transports = append(transports, r.observe(discovery.IPTransport(sc), PairingStepDiscoveryIP))
	}
	var bsc *ble.Scanner
	if opts.BLE != nil {
		bsc = ble.NewScanner(opts.BLE, ble.ScannerOptions{Clock: c.clock, Logger: c.log})
		defer func() { _ = bsc.Close(context.WithoutCancel(ctx)) }()
		transports = append(transports, r.observe(ble.Transport(bsc), PairingStepDiscoveryBLE))
	}
	var caps *discovery.Capabilities
	if !pc.Manual {
		cc := discovery.CapabilitiesOf(pc.Capabilities)
		caps = &cc
	}
	r.multi = len(discovery.SelectTransports(transports, caps)) > 1
	commissioner.ReportDiscovery(opts.Progress, PairingStarted, nil)
	s := discovery.CommissioningRace(ctx, transports, discovery.SessionOptions{
		Identifier: pc.Identifier(), Capabilities: caps, Clock: c.clock, Logger: c.log,
	}, identity, func(actx context.Context, cand discovery.Candidate, win func() bool) (*client.Node, error) {
		return c.pairAttempt(actx, ctx, cand, win, bsc, pc.Passcode, opts, r)
	})
	n, err := s.Wait(context.WithoutCancel(ctx))
	if err == nil {
		return n, nil
	}
	if n = r.winnerNode(); n != nil {
		return n, err
	}
	if !r.attempted() {
		err = fmt.Errorf("%w: %w", ErrNoDevice, err)
		commissioner.ReportDiscovery(opts.Progress, PairingFailed, err)
		return nil, err
	}
	r.report(commissioner.Step{Number: 2, Sub: 2, Name: commissioner.StepPASE, Phase: PairingFailed, Err: err})
	return nil, err
}

// pairAttempt commissions one candidate of the race: over BLE it opens the
// BTP channel to the peripheral first (ble.Dial); the commissioner's
// ClaimNodeIDAfterPASE is the race's gate (matter.js's
// continueCommissioningAfterPase, CommissioningDiscovery.ts:66-74). The
// attempt ends with Pair's ctx as well as with the race.
func (c *Controller) pairAttempt(actx, pairCtx context.Context, cand discovery.Candidate, win func() bool, bsc *ble.Scanner, passcode uint32, opts PairOptions, r *pairRace) (*client.Node, error) {
	actx, stop := bothContexts(actx, pairCtx)
	defer stop()
	r.attemptStarted()
	dev := cand.Device
	var channel commissioner.Channel
	if cand.Transport == discovery.TransportBLE {
		bd, err := bsc.Device(dev.InstanceID)
		if err != nil {
			return nil, err
		}
		ch, err := ble.Dial(actx, opts.BLE, bd.Peripheral, ble.DialOptions{Clock: c.clock, Logger: c.log})
		if err != nil {
			return nil, err
		}
		defer func() { _ = ch.Close() }()
		channel = ch
	}
	n, err := c.reg.ForDevice(actx, dev)
	if err != nil {
		return nil, err
	}
	o := c.pairOptions(passcode, channel, opts, r.attemptProgress)
	claim, nodeID := o.ClaimNodeIDAfterPASE, o.NodeID
	o.ClaimNodeIDAfterPASE = func() (uint64, bool) {
		if !win() {
			return 0, false
		}
		r.won(cand.Transport, n)
		if claim != nil {
			return claim()
		}
		return nodeID, true
	}
	return n, c.reg.Commission(actx, n, client.CommissionOptions{Options: o, AutoSubscribe: opts.AutoSubscribe})
}

// bothContexts is a context that ends with a or b.
func bothContexts(a, b context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(a)
	stop := context.AfterFunc(b, func() { cancel(context.Cause(b)) })
	return ctx, func() {
		stop()
		cancel(nil)
	}
}

// pairRace keeps a Pair's progress reports in the order a single search
// has them — Discovery, then PASE once the first attempt starts — while
// several attempts run: the attempts' own PASE reports are dropped, the
// race reports PASE succeeded for the winner, and only the winner reports
// the steps after it.
type pairRace struct {
	progress func(PairingStep)
	multi    bool

	mu       sync.Mutex
	started  bool
	winner   *client.Node
	searches map[string]bool // the per-transport sub-steps started
	results  map[string]bool // ... and reported
}

func (r *pairRace) report(st PairingStep) {
	if r.progress != nil {
		r.progress(st)
	}
}

func (r *pairRace) attempted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

func (r *pairRace) winnerNode() *client.Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.winner
}

// attemptStarted reports Discovery succeeded and PASE started for the
// first attempt.
func (r *pairRace) attemptStarted() {
	r.mu.Lock()
	first := !r.started
	r.started = true
	r.mu.Unlock()
	if first {
		commissioner.ReportDiscovery(r.progress, PairingSucceeded, nil)
		r.report(commissioner.Step{Number: 2, Sub: 2, Name: commissioner.StepPASE, Phase: PairingStarted})
	}
}

// attemptProgress is an attempt's progress hook: its PASE reports are
// the race's to make; every other step runs only after the attempt won.
func (r *pairRace) attemptProgress(st PairingStep) {
	if st.Name == commissioner.StepPASE {
		return
	}
	r.report(st)
}

// won reports the transports' sub-steps (the winner's succeeded, the
// others skipped) and PASE succeeded.
func (r *pairRace) won(kind discovery.TransportKind, n *client.Node) {
	r.mu.Lock()
	r.winner = n
	var steps []PairingStep
	if r.multi {
		for name := range r.searches {
			if r.results[name] {
				continue
			}
			r.results[name] = true
			phase := PairingSkipped
			if name == transportStep(kind) {
				phase = PairingSucceeded
			}
			steps = append(steps, commissioner.Step{Number: 2, Sub: 1, Name: name, Phase: phase})
		}
	}
	r.mu.Unlock()
	for _, st := range steps {
		r.report(st)
	}
	r.report(commissioner.Step{Number: 2, Sub: 2, Name: commissioner.StepPASE, Phase: PairingSucceeded})
}

func transportStep(kind discovery.TransportKind) string {
	if kind == discovery.TransportBLE {
		return PairingStepDiscoveryBLE
	}
	return PairingStepDiscoveryIP
}

// observe reports t's sub-step when the race runs on several transports.
func (r *pairRace) observe(t discovery.Transport, name string) discovery.Transport {
	return observedTransport{Transport: t, r: r, name: name}
}

type observedTransport struct {
	discovery.Transport
	r    *pairRace
	name string
}

func (o observedTransport) FindContinuously(ctx context.Context, id discovery.Identifier, found func(discovery.Device)) error {
	r := o.r
	if r.multi {
		r.mu.Lock()
		if r.searches == nil {
			r.searches, r.results = map[string]bool{}, map[string]bool{}
		}
		r.searches[o.name] = true
		r.mu.Unlock()
		r.report(commissioner.Step{Number: 2, Sub: 1, Name: o.name, Phase: PairingStarted})
	}
	err := o.Transport.FindContinuously(ctx, id, found)
	if r.multi && err != nil && !errors.Is(err, context.Canceled) {
		r.mu.Lock()
		first := !r.results[o.name]
		r.results[o.name] = true
		r.mu.Unlock()
		if first {
			r.report(commissioner.Step{Number: 2, Sub: 1, Name: o.name, Phase: PairingFailed, Err: err})
		}
	}
	return err
}
