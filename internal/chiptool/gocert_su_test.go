// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otar "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node/controllernode"
)

// The SU cases, and the Go TH as OTA provider the SU and BDX cases share.
//
// matter.js's SU-2.x and SU-4.1 cases run the device as DUT, as here: the
// TS's TH is a controller that serves the image (support/chip-testing/
// test/cert/tc-bdx-support.ts:48-51, BDX_RECEIVER_ROLES), which the Go
// controller is, with its OTA provider on its own node (controller.OTAConfig).
// SU-1.1 and SU-3.x have matter.js's controller as the DUT provider and a
// requestor app as TH; read mirrored, the daemon is that requestor, and
// what it sends is what the cases check.

func init() {
	for _, c := range []func() goCertCase{
		caseSU11, caseSU21, caseSU22, caseSU23, caseSU24, caseSU25, caseSU26, caseSU27,
		caseSU31, caseSU32, caseSU33, caseSU34, caseSU41,
	} {
		registerGoCert(c)
	}
}

// The adapter's budgets (support/chip-testing/src/cert/
// InProcessControllerAdapter.ts:420-437) and the staged image's size
// (support/chip-testing/src/OtaTestIdentity.ts:30).
const (
	otaTransferTimeout = 90 * time.Second
	otaApplyTimeout    = 10 * time.Second
	otaQueryTimeout    = 30 * time.Second
	otaTestPayloadSize = 64 * 1024
	// otaRequestorEndpoint is the daemon's requestor: the root's.
	otaRequestorEndpoint uint16 = 0
)

// otaTestPayload is the staged payload, a pure function of the offset
// (otaTestPayload, OtaTestIdentity.ts:36-41).
func otaTestPayload(size int) []byte {
	p := make([]byte, size)
	for i := range p {
		p[i] = byte(i ^ 0x42)
	}
	return p
}

// otaTH is the Go TH as OTA provider: a controller with its own node
// serving the provider, the answers a case scripts per requestor, and the
// record of what the provider answered and what its BDX transfers carried
// — matter.js's RecordingOtaProviderServer and OtaExchangeRecording
// (InProcessControllerAdapter.ts:456-762).
type otaTH struct {
	th       *goTH
	ctrl     *gocontroller.Controller
	images   *softwareupdate.Images
	nodeID   uint64
	endpoint uint16

	mu        sync.Mutex
	queries   map[softwareupdate.Peer][]softwareupdate.QueryImageScript
	applies   map[softwareupdate.Peer][]softwareupdate.ApplyUpdateScript
	exchanges []softwareupdate.ProviderExchange
	transfers []*bdxTransfer
	observers []func(cluster, event uint32, number uint64, value any)
}

// observeEvents has fn told of every event the default subscription of a
// node reports — matter.js's observeEvents, which watches the subscription
// its controller sustains rather than opening one of its own
// (InProcessControllerAdapter.ts:2190-2201): a second subscription is
// cleared when the default one is set up again on a new session.
func (o *otaTH) observeEvents(fn func(cluster, event uint32, number uint64, value any)) {
	o.mu.Lock()
	o.observers = append(o.observers, fn)
	o.mu.Unlock()
}

func (o *otaTH) event(_ uint16, cluster, event uint32, e client.EventData, value any) {
	o.mu.Lock()
	obs := slices.Clone(o.observers)
	o.mu.Unlock()
	for _, fn := range obs {
		fn(cluster, event, e.Number, value)
	}
}

// otaProvider builds the TS's TH controller with its OTA provider.
func (th *goTH) otaProvider(label string) (*otaTH, error) {
	db, err := gocontroller.OpenDB(filepath.Join(th.t.TempDir(), label+".db"))
	if err != nil {
		return nil, err
	}
	th.t.Cleanup(func() { _ = db.Close() })
	o := &otaTH{
		th: th, images: softwareupdate.NewImages(softwareupdate.ImagesConfig{Logger: th.log}),
		queries: map[softwareupdate.Peer][]softwareupdate.QueryImageScript{}, applies: map[softwareupdate.Peer][]softwareupdate.ApplyUpdateScript{},
	}
	c, err := gocontroller.New(th.ctx, gocontroller.Config{
		DB: db, Attestation: attestation.TestStore(), Label: label, Logger: th.log,
		HooksFor: func(string) client.Hooks { return client.Hooks{Event: o.event} },
		OTA: &gocontroller.OTAConfig{
			Images: o.images, AllowTestImages: true, StartNode: controllernode.Start,
			ScriptQueryImage: o.scriptQuery, ScriptApplyUpdate: o.scriptApply, OnProviderExchange: o.record,
		},
	})
	if err != nil {
		return nil, err
	}
	th.ctrls = append(th.ctrls, c)
	o.ctrl = c
	own, nodeID, _ := c.OwnNode()
	if own == nil {
		return nil, errors.New("the controller started no own node")
	}
	ep, ok := own.OTAProviderEndpoint()
	if !ok {
		return nil, errors.New("the controller's own node serves no OTA provider")
	}
	o.nodeID, o.endpoint = nodeID, ep
	own.BDX().OnSessionStarted(o.sessionStarted)
	return o, nil
}

// commission commissions a device with the default subscription on: the
// update manager reads a node's identity and requestor from it
// (preparePeerForUpdate).
func (o *otaTH) commission(code string) (*client.Node, softwareupdate.Peer, error) {
	ctx, cancel := context.WithTimeout(o.th.ctx, 2*time.Minute)
	defer cancel()
	n, err := o.ctrl.Pair(ctx, code, gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()})
	if err != nil {
		return nil, softwareupdate.Peer{}, fmt.Errorf("commission: %w", err)
	}
	p, ok := n.PeerAddress()
	if !ok {
		return nil, softwareupdate.Peer{}, errors.New("the commissioned node has no operational address")
	}
	if !waitFor(o.th.ctx, time.Minute, n.SubscriptionActive) {
		return nil, softwareupdate.Peer{}, errors.New("the default subscription did not start")
	}
	return n, p, nil
}

// script installs a requestor's scripted answers, replacing earlier ones:
// one entry per command, in order; a spent list lets the provider answer
// for itself (scriptOtaProvider, InProcessControllerAdapter.ts:1737-1748).
func (o *otaTH) script(p softwareupdate.Peer, queries []softwareupdate.QueryImageScript, applies []softwareupdate.ApplyUpdateScript) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.queries[p] = slices.Clone(queries)
	o.applies[p] = slices.Clone(applies)
}

func (o *otaTH) scriptQuery(p softwareupdate.Peer, _ softwareupdate.QueryImageRequest) (softwareupdate.QueryImageScript, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	q := o.queries[p]
	if len(q) == 0 {
		return softwareupdate.QueryImageScript{}, false
	}
	o.queries[p] = q[1:]
	return q[0], true
}

func (o *otaTH) scriptApply(p softwareupdate.Peer, _ provdef.ApplyUpdateRequestRequest) (softwareupdate.ApplyUpdateScript, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	a := o.applies[p]
	if len(a) == 0 {
		return softwareupdate.ApplyUpdateScript{}, false
	}
	o.applies[p] = a[1:]
	return a[0], true
}

func (o *otaTH) record(x softwareupdate.ProviderExchange) {
	o.mu.Lock()
	o.exchanges = append(o.exchanges, x)
	o.mu.Unlock()
}

// mark opens a recording window: what the provider answers from now on.
func (o *otaTH) mark() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.exchanges)
}

// otaExchanges are the commands one requestor sent in a window, with the
// provider's answers — matter.js OtaProviderExchanges (packages/testing/
// src/chip/cert/controller-adapter.ts:480-484).
type otaExchanges struct {
	query  []softwareupdate.ProviderExchange
	apply  []softwareupdate.ProviderExchange
	notify []softwareupdate.ProviderExchange
}

func (o *otaTH) since(p softwareupdate.Peer, mark int) otaExchanges {
	o.mu.Lock()
	defer o.mu.Unlock()
	var e otaExchanges
	for _, x := range o.exchanges[mark:] {
		if x.Peer != p {
			continue
		}
		switch {
		case x.QueryImage != nil:
			e.query = append(e.query, x)
		case x.ApplyUpdate != nil:
			e.apply = append(e.apply, x)
		case x.NotifyUpdateApplied != nil:
			e.notify = append(e.notify, x)
		}
	}
	return e
}

// bdxTransfer is one transfer the provider served: the ReceiveInit that
// opened it and every message after it, as the TH sent and received them —
// the sender's account matter.js's OtaBdxTransfer gives
// (controller-adapter.ts:492-547) and the per-message record the cert
// suite reads from the message log (tc-bdx-support.ts:273-296).
type bdxTransfer struct {
	peer bdx.Peer
	init bdx.Init

	mu          sync.Mutex
	msgs        []bdxMsg
	finished    bool
	closed      bool
	transferred uint64
}

// bdxMsg is one message of a transfer.
type bdxMsg struct {
	sent    bool
	typ     bdx.MessageType
	counter uint32
	length  int
	v       any
}

func (o *otaTH) sessionStarted(s *bdx.ServerSession, _ string) {
	t := &bdxTransfer{peer: s.Peer(), init: s.Init()}
	s.OnMessage(func(e bdx.MessageEvent) {
		if e.ProtocolID != bdx.ProtocolID {
			return
		}
		m := bdxMsg{sent: e.Sent, typ: bdx.MessageType(e.Opcode)}
		if v, err := bdx.Decode(m.typ, e.Payload); err == nil {
			m.v = v
			switch x := v.(type) {
			case bdx.Block:
				m.counter, m.length = x.BlockCounter, len(x.Data)
			case bdx.CounterOnly:
				m.counter = x.BlockCounter
			case bdx.BlockQueryWithSkip:
				m.counter = x.BlockCounter
			}
		}
		t.mu.Lock()
		t.msgs = append(t.msgs, m)
		t.mu.Unlock()
	})
	s.OnFinished(func(total uint64) {
		t.mu.Lock()
		t.finished, t.transferred = true, total
		t.mu.Unlock()
	})
	s.OnClosed(func() {
		t.mu.Lock()
		t.closed = true
		if !t.finished {
			t.transferred = s.Transferred()
		}
		t.mu.Unlock()
	})
	o.mu.Lock()
	o.transfers = append(o.transfers, t)
	o.mu.Unlock()
}

// messages are the transfer's messages of type typ in direction sent.
func (t *bdxTransfer) messages(sent bool, typ bdx.MessageType) []bdxMsg {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []bdxMsg
	for _, m := range t.msgs {
		if m.sent == sent && m.typ == typ {
			out = append(out, m)
		}
	}
	return out
}

// accept is the ReceiveAccept the TH sent.
func (t *bdxTransfer) accept() (bdx.ReceiveAccept, bool) {
	for _, m := range t.messages(true, bdx.MessageReceiveAccept) {
		if a, ok := m.v.(bdx.ReceiveAccept); ok {
			return a, true
		}
	}
	return bdx.ReceiveAccept{}, false
}

func (t *bdxTransfer) state() (finished, closed bool, transferred uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finished, t.closed, t.transferred
}

// otaServed is one served update: matter.js's OtaBdxTransfer
// (controller-adapter.ts:492-547).
type otaServed struct {
	version  uint32
	fileSize int
	transfer *bdxTransfer
	applied  bool
	observed time.Duration
	ex       otaExchanges
}

// serveOpts are ServeOtaUpdateOptions (controller-adapter.ts) as
// serveOtaTransfer passes them (tc-bdx-support.ts:117-129).
type serveOpts struct {
	// noApply ends the update at the download (expectApply false).
	noApply                                           bool
	applyTimeout, notifyTimeout, observeAfter, budget time.Duration
}

// otaIdentity is the requestor's vendor, product and running version, read
// from its BasicInformation (#otaIdentity, InProcessControllerAdapter.ts:
// 1988-2000).
type otaIdentity struct {
	vendor, product uint16
	version         uint32
}

func (o *otaTH) identity(n *client.Node) (otaIdentity, error) {
	var id otaIdentity
	for _, a := range []struct {
		attr uint32
		set  func(uint64)
	}{
		{basicinfo.AttrVendorId, func(v uint64) { id.vendor = uint16(v) }},   //nolint:gosec // a vendor id
		{basicinfo.AttrProductId, func(v uint64) { id.product = uint16(v) }}, //nolint:gosec // a product id
		{basicinfo.AttrSoftwareVersion, func(v uint64) { id.version = uint32(v) }},
	} {
		v, err := o.th.readValue(n, attrPath(0, basicinfo.ClusterID, a.attr))
		if err != nil {
			return id, err
		}
		a.set(v.El.Uint)
	}
	return id, nil
}

// serve stages an image one version newer than the requestor runs, has the
// update manager push it (forceUpdate: the consent and the announcement)
// and waits for the transfer, the apply decision and the notification —
// serveOtaUpdate and #serveStagedImage (InProcessControllerAdapter.ts:
// 1869-1977), stageOtaImage (:842-880) and #runOtaTransfer (:2038-2105).
func (o *otaTH) serve(n *client.Node, p softwareupdate.Peer, opts serveOpts) (*otaServed, error) {
	id, err := o.identity(n)
	if err != nil {
		return nil, err
	}
	version := id.version + 1
	minV, maxV := uint32(0), id.version
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: id.vendor, ProductID: id.product, SoftwareVersion: version, SoftwareVersionString: "v" + strconv.FormatUint(uint64(version), 10),
		MinApplicableSoftwareVersion: &minV, MaxApplicableSoftwareVersion: &maxV, Payload: otaTestPayload(otaTestPayloadSize),
	})
	if err != nil {
		return nil, err
	}
	if _, err := o.images.Store(o.th.ctx, img, softwareupdate.ModeTest, nil); err != nil {
		return nil, err
	}
	mark := o.mark()
	o.mu.Lock()
	tmark := len(o.transfers)
	o.mu.Unlock()
	mgr := o.ctrl.SoftwareUpdates()
	target := softwareupdate.Target{VendorID: id.vendor, ProductID: id.product, TargetSoftwareVersion: version}
	if err := mgr.ForceUpdate(o.th.ctx, p, target); err != nil {
		return nil, fmt.Errorf("forceUpdate: %w", err)
	}
	budget := opts.budget
	if budget == 0 {
		budget = otaTransferTimeout
	}
	var t *bdxTransfer
	done := waitFor(o.th.ctx, budget, func() bool {
		o.mu.Lock()
		for _, x := range o.transfers[tmark:] {
			if x.peer.NodeID == p.NodeID && x.peer.FabricIndex == p.FabricIndex {
				t = x
				break
			}
		}
		o.mu.Unlock()
		if t == nil {
			return false
		}
		finished, closed, _ := t.state()
		return finished || closed
	})
	if !done || t == nil {
		// Anything but a completed transfer leaves the update queued
		// (:2081-2103): the consent goes.
		mgr.RemoveConsent(p, version)
		if t == nil {
			return nil, fmt.Errorf("node %d did not take the offered OTA image within %s — it opened no BDX transfer at all", p.NodeID, budget)
		}
		return nil, fmt.Errorf("node %d did not take the offered OTA image within %s", p.NodeID, budget)
	}
	if finished, _, transferred := t.state(); !finished {
		mgr.RemoveConsent(p, version)
		return nil, fmt.Errorf("BDX transfer to node %d ended after %d of %d bytes without completing", p.NodeID, transferred, len(img))
	}
	s := &otaServed{version: version, fileSize: len(img), transfer: t}
	if !opts.noApply {
		at := opts.applyTimeout
		if at == 0 {
			at = otaApplyTimeout
		}
		// awaitApplyDecision (:708-715): the last answer that is not an
		// AwaitNextAction decides.
		waitFor(o.th.ctx, at, func() bool {
			for _, x := range o.since(p, mark).apply {
				if a := x.ApplyUpdate.Response.Action; a != provdef.ApplyUpdateActionAwaitNextAction {
					s.applied = a == provdef.ApplyUpdateActionProceed
					return true
				}
			}
			return false
		})
		// The daemon applies in place under --ota-harness and holds Applying
		// for two seconds before it notifies (examples/reference-bridge/
		// ota.go otaHarnessApplyHold); a query scheduled meanwhile is
		// skipped. The matter.js requestor's apply returns at once, so the
		// TS goes on after the decision; the Go TH waits for the in-place
		// apply to end — the notification — before it does, so the next
		// step's announcement reaches an Idle requestor.
		nt := opts.notifyTimeout
		if nt == 0 {
			nt = 30 * time.Second
		}
		if s.applied {
			waitFor(o.th.ctx, nt, func() bool { return len(o.since(p, mark).notify) > 0 })
		}
	}
	if opts.observeAfter > 0 {
		start := time.Now()
		sleepCtx(o.th.ctx, opts.observeAfter)
		s.observed = time.Since(start)
	}
	s.ex = o.since(p, mark)
	o.th.note("served version %d (%d bytes) over BDX from endpoint %d, %d bytes moved; %d QueryImage, %d ApplyUpdateRequest, %d NotifyUpdateApplied",
		version, len(img), o.endpoint, func() uint64 { _, _, n := t.state(); return n }(), len(s.ex.query), len(s.ex.apply), len(s.ex.notify))
	return s, nil
}

// announceOpts are AnnounceOtaProviderOptions (controller-adapter.ts:
// 623-663).
type announceOpts struct {
	reason   otar.AnnouncementReasonEnum
	noQuery  bool
	timeout  time.Duration
	observe  time.Duration
	provider uint64
	endpoint uint16
}

// announced is what announceOtaProvider resolves with.
type announced struct {
	req      otar.AnnounceOtaProviderRequest
	ex       otaExchanges
	observed time.Duration
}

// announce sends AnnounceOTAProvider to the requestor — this TH's provider
// unless opts names another — and waits for its QueryImage
// (announceOtaProvider, InProcessControllerAdapter.ts:1750-1812; the
// default reason UpdateAvailable, :1819-1821).
func (o *otaTH) announce(n *client.Node, p softwareupdate.Peer, opts announceOpts) (*announced, error) {
	reason := opts.reason
	req := otar.AnnounceOtaProviderRequest{
		ProviderNodeId: o.nodeID, VendorId: o.ctrl.Fabric().RootVendorID, AnnouncementReason: reason, Endpoint: o.endpoint,
	}
	ours := opts.provider == 0
	if !ours {
		req.ProviderNodeId, req.Endpoint = opts.provider, opts.endpoint
	}
	mark := o.mark()
	if _, err := o.th.invoke(n, cmdPath(otaRequestorEndpoint, otar.ClusterID, otar.CmdAnnounceOtaProvider), req, false, client.InvokeOptions{}); err != nil {
		return nil, err
	}
	if ours && !opts.noQuery {
		to := opts.timeout
		if to == 0 {
			to = otaQueryTimeout
		}
		if !waitFor(o.th.ctx, to, func() bool { return len(o.since(p, mark).query) > 0 }) {
			return nil, fmt.Errorf("node %d did not query the announced OTA provider within %s", p.NodeID, to)
		}
	}
	a := &announced{req: req}
	if ours && opts.observe > 0 {
		start := time.Now()
		sleepCtx(o.th.ctx, opts.observe)
		a.observed = time.Since(start)
	}
	a.ex = o.since(p, mark)
	return a, nil
}

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// updateState reads the requestor's UpdateState.
func (th *goTH) updateState(n *client.Node) (uint64, error) {
	v, err := th.readValue(n, attrPath(otaRequestorEndpoint, otar.ClusterID, otar.AttrUpdateState))
	return v.El.Uint, err
}

// requestorIdle is requestorIdleCheck (tc-su-support.ts:572-581).
func (th *goTH) requestorIdle(n *client.Node) error {
	s, err := th.updateState(n)
	if err != nil {
		return err
	}
	if otar.UpdateStateEnum(s) != otar.UpdateStateIdle { //nolint:gosec // an enum value
		return fmt.Errorf("the DUT reported UpdateState %d, where Idle is %d", s, otar.UpdateStateIdle)
	}
	th.note("UpdateState Idle")
	return nil
}

// stateChange is one StateTransition event: its number and NewState
// (requestorStateChanges, tc-su-support.ts:525-544).
type stateChange struct {
	number   uint64
	newState uint64
}

var otaStateTransition = im.ConcreteEventPath{Endpoint: otaRequestorEndpoint, Cluster: otar.ClusterID, Event: otar.EventStateTransition, HasEndpoint: true, HasCluster: true, HasEvent: true}

// stateChanges reads the requestor's StateTransition events after after.
func (th *goTH) stateChanges(n *client.Node, after uint64) ([]stateChange, error) {
	r, err := n.Read(th.ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Events: []im.ConcreteEventPath{otaStateTransition}}, IncludeKnownVersions: true})
	if err != nil {
		return nil, err
	}
	var out []stateChange
	for i := range r.Events {
		e := &r.Events[i]
		if e.Number <= after && after != 0 {
			continue
		}
		ns, _ := structField(e.Value, 1)
		out = append(out, stateChange{number: e.Number, newState: ns.El.Uint})
	}
	slices.SortFunc(out, func(a, b stateChange) int { return int(a.number) - int(b.number) }) //nolint:gosec // event numbers of one run
	return out, nil
}

// latestStateChange is latestRequestorStateChange (tc-su-support.ts:553-559).
func (th *goTH) latestStateChange(n *client.Node) (uint64, error) {
	c, err := th.stateChanges(n, 0)
	if err != nil || len(c) == 0 {
		return 0, err
	}
	return c[len(c)-1].number, nil
}

func statesOf(c []stateChange) []uint64 {
	out := make([]uint64, 0, len(c))
	for _, x := range c {
		out = append(out, x.newState)
	}
	return out
}

// singleQuery is singleQueryImage (tc-su-support.ts:72-105).
func singleQuery(e otaExchanges) (*softwareupdate.QueryImageExchange, error) {
	if len(e.query) != 1 {
		return nil, fmt.Errorf("the DUT's provider answered %d QueryImage commands during this update, and this step is about the one the plan describes", len(e.query))
	}
	return e.query[0].QueryImage, nil
}

func singleApply(e otaExchanges) (*softwareupdate.ApplyUpdateExchange, error) {
	if len(e.apply) != 1 {
		return nil, fmt.Errorf("the DUT's provider answered %d ApplyUpdateRequest commands during this update, and this step is about the one the plan describes", len(e.apply))
	}
	return e.apply[0].ApplyUpdate, nil
}

// suRun is the state an SU case carries between its steps: the TH and the
// requestor it commissioned.
type suRun struct {
	o    *otaTH
	n    *client.Node
	peer softwareupdate.Peer
}

// precondition is the SU cases' step 0: the TH commissions the DUT and the
// requestor is Idle (recordRequestorIdle, tc-su-support.ts:584-586).
func (r *suRun) precondition(th *goTH) error {
	var err error
	if r.o, err = th.otaProvider("th"); err != nil {
		return err
	}
	if r.n, r.peer, err = r.o.commission(th.dut.info.manualCode); err != nil {
		return err
	}
	return th.requestorIdle(r.n)
}

func (r *suRun) ready() error {
	if r.o == nil || r.n == nil {
		return errors.New("the precondition step did not commission the DUT")
	}
	return nil
}

func busy(delay uint32) softwareupdate.QueryImageScript {
	return softwareupdate.QueryImageScript{Response: &provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: &delay}}
}

func status(s provdef.StatusEnum) softwareupdate.QueryImageScript {
	return softwareupdate.QueryImageScript{Response: &provdef.QueryImageResponse{Status: s}}
}

func needsConsent(v bool) softwareupdate.QueryImageScript {
	return softwareupdate.QueryImageScript{UserConsentNeeded: &v}
}

func applyAnswer(a provdef.ApplyUpdateActionEnum, delay *uint32) softwareupdate.ApplyUpdateScript {
	return softwareupdate.ApplyUpdateScript{Action: a, DelayedActionTime: delay}
}

// The QUERY_WINDOW and RETRY_MARGIN of TC-SU-2.1 and 2.2 (TC-SU-2.1.test.ts:
// 32-38) and the spaced query's budget (:53).
const (
	suQueryWindow   = 120 * time.Second
	suRetryMargin   = 15 * time.Second
	suSpacedTimeout = 150 * time.Second
)

// suPICS is the case PICS of the requestor cases.
var suPICS = []string{"MCORE.OTA.Requestor", "MCORE.OTA.Provider", "OTAR.C.M.AnnounceOTAProvider"}

// caseSU11 mirrors TC-SU-1.1: the administrator announces a second
// provider to the requestor, and the requestor receives the command with
// the fields it was sent. The TS's OTA-R/TH is the daemon, its OTA-P/TH2 a
// second daemon serving the --ota-provider endpoint.
func caseSU11() goCertCase {
	var (
		o              *otaTH
		requestor      *client.Node
		requestorPeer  softwareupdate.Peer
		providerDaemon *bridgeProcess
	)
	return goCertCase{
		tc: "TC-SU-1.1", line: 36, pics: []string{"MCORE.ACL.Administrator", "OTAR.C.M.AnnounceOTAProvider"},
		steps: []goStep{
			{n: "0", line: 45, title: "OTA-R/TH and the DUT are on the same fabric", run: func(th *goTH) error {
				var err error
				if o, err = th.otaProvider("dut"); err != nil {
					return err
				}
				if requestor, requestorPeer, err = o.commission(th.dut.info.manualCode); err != nil {
					return err
				}
				th.note("the DUT commissioned the OTA-R/TH as node %d", requestorPeer.NodeID)
				return nil
			}},
			{n: "1", line: 71, pics: "OTAR.C.M.AnnounceOTAProvider", title: "Commission OTA-P/TH2 and announce it", run: func(th *goTH) error {
				if o == nil {
					return errors.New("the OTA-R/TH was not commissioned by the preceding step")
				}
				providerDaemon = th.device(1, "--ota-provider")
				ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
				defer cancel()
				pn, err := o.ctrl.Pair(ctx, providerDaemon.info.manualCode, gocontroller.PairOptions{OnAttestationFailure: attestation.Accept()})
				if err != nil {
					return fmt.Errorf("commission OTA-P/TH2: %w", err)
				}
				pp, _ := pn.PeerAddress()
				ep, err := th.endpointServing(pn, provdef.ClusterID)
				if err != nil {
					return err
				}
				offset := len(th.dut.snapshotStderr())
				a, err := o.announce(requestor, requestorPeer, announceOpts{reason: otar.AnnouncementReasonSimpleAnnouncement, provider: pp.NodeID, endpoint: ep})
				if err != nil {
					return err
				}
				if a.req.ProviderNodeId != pp.NodeID || a.req.AnnouncementReason != otar.AnnouncementReasonSimpleAnnouncement {
					return fmt.Errorf("the announcement %+v does not name the OTA-P/TH2 (node %d) as a SimpleAnnouncement", a.req, pp.NodeID)
				}
				// The endpoint against what the OTA-P/TH2 publishes (:125-147).
				servers, err := th.readValue(pn, attrPath(ep, descriptor.ClusterID, descriptor.AttrServerList))
				if err != nil {
					return err
				}
				if !slices.ContainsFunc(servers.Children, func(c spec.Node) bool { return c.El.Uint == uint64(provdef.ClusterID) }) {
					return fmt.Errorf("the announced endpoint %d's ServerList %s lacks the OTA provider", ep, describeNode(servers))
				}
				// What the requestor received, from its log, as the TS reads
				// the OTA-R/TH's (announcementLines, tc-su-support.ts:302-326).
				want := fmt.Sprintf("msg=matter.ota.provider_announced reason=0 fabric_index=%d provider=%d endpoint=%d vendor_id=%d",
					requestorPeer.FabricIndex, pp.NodeID, ep, a.req.VendorId)
				if !waitFor(th.ctx, 15*time.Second, func() bool { return strings.Contains(th.dut.snapshotStderr()[offset:], want) }) {
					return fmt.Errorf("the OTA-R/TH logged no %q", want)
				}
				th.note("announced node %d endpoint %d (ServerList holds 0x0029), reason SimpleAnnouncement, vendor 0x%04X; the OTA-R/TH logged it", pp.NodeID, ep, a.req.VendorId)
				return nil
			}},
		},
	}
}

// ownFabricIndex is the fabric index the node's CASE session runs on — the
// accessing fabric's CurrentFabricIndex (readOwnFabricIndex,
// tc-support.ts:755).
func (th *goTH) ownFabricIndex(n *client.Node) (uint8, error) {
	v, err := th.readValue(n, attrPath(0, operationalcredentials.ClusterID, operationalcredentials.AttrCurrentFabricIndex))
	return uint8(v.El.Uint), err //nolint:gosec // a fabric index
}

// endpointServing is the first endpoint of n whose ServerList holds the
// cluster.
func (th *goTH) endpointServing(n *client.Node, cluster uint32) (uint16, error) {
	sl := descriptor.AttrServerList
	dc := descriptor.ClusterID
	// A wildcard read is a chunked exchange; one whose message is lost
	// past the retransmissions is read again (twice at most) — the Go TH's
	// own transport, not the step's subject.
	var r *client.Report
	var err error
	for range 3 {
		if r, err = th.read(n, wildcard(nil, &dc, &sl)); err == nil {
			break
		}
	}
	if err != nil {
		return 0, err
	}
	for i := range r.Attributes {
		a := &r.Attributes[i]
		if slices.ContainsFunc(a.Value.Children, func(c spec.Node) bool { return c.El.Uint == uint64(cluster) }) {
			return a.Path.Endpoint, nil
		}
	}
	return 0, fmt.Errorf("no endpoint serves cluster 0x%04X", cluster)
}

// caseSU21 is TC-SU-2.1: the QueryImage the requestor sends, its spacing
// after a Busy answer, and the provider an AnnounceOTAProvider names.
func caseSU21() goCertCase {
	r := &suRun{}
	return goCertCase{
		tc: "TC-SU-2.1", line: 261, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 274, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 296, title: "DUT sends a QueryImage command to the TH/OTA-P", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable})
				if err != nil {
					return err
				}
				q, err := singleQuery(a.ex)
				if err != nil {
					return err
				}
				req := q.Request
				id, err := r.o.identity(r.n)
				if err != nil {
					return err
				}
				var errs []error
				check := func(field string, sent, have uint64) {
					if sent != have {
						errs = append(errs, fmt.Errorf("%s: the DUT sent %d and reports %d in Basic Information", field, sent, have))
					}
				}
				check("vendorId", uint64(req.VendorId), uint64(id.vendor))
				check("productId", uint64(req.ProductId), uint64(id.product))
				check("softwareVersion", uint64(req.SoftwareVersion), uint64(id.version))
				if !slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolBdxSynchronous) {
					errs = append(errs, fmt.Errorf("ProtocolsSupported %v lists no BDX synchronous", req.ProtocolsSupported))
				}
				if https := slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolHttps); https != th.declares("MCORE.OTA.HTTPS") {
					errs = append(errs, fmt.Errorf("ProtocolsSupported lists HTTPS %t, the PICS MCORE.OTA.HTTPS %t", https, th.declares("MCORE.OTA.HTTPS")))
				}
				if req.HardwareVersion != nil {
					hw, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrHardwareVersion))
					if err != nil {
						return err
					}
					check("hardwareVersion", uint64(*req.HardwareVersion), hw.El.Uint)
				}
				if req.Location != nil {
					loc, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocation))
					if err != nil {
						return err
					}
					if *req.Location != loc.El.String {
						errs = append(errs, fmt.Errorf("location: the DUT sent %q and reports %q", *req.Location, loc.El.String))
					}
				}
				// The TS's MCORE.OTA.RequestorConsent is the device's own;
				// the Go TH's overlay does not name it, so declares reads the
				// daemon's.
				canConsent := req.RequestorCanConsent != nil && *req.RequestorCanConsent
				if canConsent != th.declares("MCORE.OTA.RequestorConsent") {
					errs = append(errs, fmt.Errorf("RequestorCanConsent %t, the PICS MCORE.OTA.RequestorConsent %t", canConsent, th.declares("MCORE.OTA.RequestorConsent")))
				}
				if err := errors.Join(errs...); err != nil {
					return err
				}
				th.note("QueryImage vendor 0x%04X product 0x%04X version %d protocols %v hardware %v location %v canConsent %t, as Basic Information reports",
					req.VendorId, req.ProductId, req.SoftwareVersion, req.ProtocolsSupported, deref(req.HardwareVersion), deref(req.Location), canConsent)
				return nil
			}},
			{
				n: "2", line: 307, long: "the plan watches the DUT for two minutes after a Busy answer (TC-SU-2.1.test.ts:312)",
				title: "DUT sends a QueryImage command, wait for 2 minutes", run: func(th *goTH) error {
					if err := r.ready(); err != nil {
						return err
					}
					r.o.script(r.peer, []softwareupdate.QueryImageScript{busy(1)}, nil) // BUSY_DELAYED_ACTION_TIME (:44)
					a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable, timeout: suSpacedTimeout, observe: suQueryWindow + suRetryMargin})
					if err != nil {
						return err
					}
					if a.observed < suQueryWindow {
						return fmt.Errorf("the TH watched for %s, against the plan's %s", a.observed, suQueryWindow)
					}
					return noQueryWithin(th, a.ex, provdef.StatusBusy, suQueryWindow)
				},
			},
			{n: "3", line: 317, title: "TH2/Administrator sends an AnnounceOTAProvider command to the DUT", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				offset := len(th.dut.snapshotStderr())
				a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable, timeout: suSpacedTimeout})
				if err != nil {
					return err
				}
				q, err := singleQuery(a.ex)
				if err != nil {
					return err
				}
				want := fmt.Sprintf("msg=matter.ota.provider_announced reason=1 fabric_index=%d provider=%d endpoint=%d vendor_id=%d",
					r.peer.FabricIndex, a.req.ProviderNodeId, a.req.Endpoint, a.req.VendorId)
				if !strings.Contains(th.dut.snapshotStderr()[offset:], want) {
					return fmt.Errorf("the DUT logged no %q", want)
				}
				th.note("the provider the DUT was told of, node %d endpoint %d, received its QueryImage for version %d", a.req.ProviderNodeId, a.req.Endpoint, q.Request.SoftwareVersion)
				return nil
			}},
		},
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return "(absent)"
	}
	return *p
}

// noQueryWithin is recordNoQueryInWindow's check (TC-SU-2.2.test.ts:
// 109-160, TC-SU-2.1.test.ts:196-234): the first query was answered as
// scripted, and no further query came within the window.
func noQueryWithin(th *goTH, e otaExchanges, want provdef.StatusEnum, window time.Duration) error {
	if len(e.query) == 0 {
		return errors.New("the DUT sent no QueryImage")
	}
	first := e.query[0]
	if first.QueryImage.Response.Status != want {
		return fmt.Errorf("the TH answered %d, the step scripted %d", first.QueryImage.Response.Status, want)
	}
	var spacing []string
	early := 0
	for _, x := range e.query[1:] {
		d := x.At.Sub(first.At)
		spacing = append(spacing, d.Round(time.Second).String())
		if d < window {
			early++
		}
	}
	if early > 0 {
		return fmt.Errorf("the DUT queried again after %v, within the %s window", spacing, window)
	}
	th.note("answered status %d, then %d further QueryImage (after %v) in the window", want, len(e.query)-1, spacing)
	return nil
}

// caseSU22 is TC-SU-2.2, which the chip-tool leg runs too (TC_SU_2_2.py):
// the requestor's answers to the QueryImageResponse variants.
func caseSU22() goCertCase {
	r := &suRun{}
	noQuery := func(answer softwareupdate.QueryImageScript, want provdef.StatusEnum) func(th *goTH) error {
		return func(th *goTH) error {
			if err := r.ready(); err != nil {
				return err
			}
			r.o.script(r.peer, []softwareupdate.QueryImageScript{answer, status(provdef.StatusNotAvailable)}, nil)
			a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable, timeout: suSpacedTimeout, observe: suQueryWindow + suRetryMargin})
			if err != nil {
				return err
			}
			if a.observed < suQueryWindow {
				return fmt.Errorf("the TH watched for %s, against the plan's %s", a.observed, suQueryWindow)
			}
			return noQueryWithin(th, a.ex, want, suQueryWindow)
		}
	}
	// recordNoTransferOf (:255-304): an offer the requestor must turn down.
	noTransfer := func(offer func(th *goTH) (*provdef.QueryImageResponse, error), offered func(resp provdef.QueryImageResponse) error) func(th *goTH) error {
		return func(th *goTH) error {
			if err := r.ready(); err != nil {
				return err
			}
			after, err := th.latestStateChange(r.n)
			if err != nil {
				return err
			}
			resp, err := offer(th)
			if err != nil {
				return err
			}
			r.o.script(r.peer, []softwareupdate.QueryImageScript{{Response: resp}, status(provdef.StatusNotAvailable)}, nil)
			a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable, timeout: suSpacedTimeout, observe: 30 * time.Second}) // REFUSAL_WINDOW (:66)
			if err != nil {
				return err
			}
			if len(a.ex.query) == 0 || a.ex.query[0].QueryImage.Response.Status != provdef.StatusUpdateAvailable {
				return errors.New("the TH did not answer the DUT's QueryImage with the scripted UpdateAvailable")
			}
			if err := offered(a.ex.query[0].QueryImage.Response); err != nil {
				return err
			}
			changes, err := th.stateChanges(r.n, after)
			if err != nil {
				return err
			}
			if slices.Contains(statesOf(changes), uint64(otar.UpdateStateDownloading)) {
				return fmt.Errorf("the DUT entered Downloading after the offer: states %v", statesOf(changes))
			}
			th.note("states after the offer: %v, none Downloading, within %s", statesOf(changes), a.observed.Round(time.Second))
			return th.requestorIdle(r.n)
		}
	}
	unheld := func(th *goTH, version uint32, uri string) *provdef.QueryImageResponse {
		return r.o.unheldUpdate(version, uri)
	}
	return goCertCase{
		tc: "TC-SU-2.2", line: 329, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 340, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 361, title: "QueryImageResponse UpdateAvailable: the image transfers", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				_, err := r.o.serve(r.n, r.peer, serveOpts{})
				return err
			}},
			{
				n: "2", line: 369, long: "the plan watches the DUT for two minutes after a Busy answer (TC-SU-2.2.test.ts:374)", title: "QueryImageResponse Busy, 60 s",
				run: noQuery(busy(60), provdef.StatusBusy),
			}, // STEP_2_DELAYED_ACTION_TIME (:57)
			{
				n: "3", line: 380, long: "the plan watches the DUT for two minutes after a NotAvailable answer (TC-SU-2.2.test.ts:385)", title: "QueryImageResponse NotAvailable",
				run: noQuery(status(provdef.StatusNotAvailable), provdef.StatusNotAvailable),
			},
			{n: "4", line: 391, long: "the DUT waits out the three minutes the TH named (TC-SU-2.2.test.ts:396)", title: "QueryImageResponse Busy, 180 s, then the transfer", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				r.o.script(r.peer, []softwareupdate.QueryImageScript{busy(180)}, nil) // STEP_4_DELAYED_ACTION_TIME (:60)
				s, err := r.o.serve(r.n, r.peer, serveOpts{budget: suSpacedTimeout + 270*time.Second})
				if err != nil {
					return err
				}
				if len(s.ex.query) < 2 {
					return fmt.Errorf("the DUT sent %d QueryImage command(s)", len(s.ex.query))
				}
				b, avail := s.ex.query[0], s.ex.query[1]
				if b.QueryImage.Response.Status != provdef.StatusBusy || avail.QueryImage.Response.Status != provdef.StatusUpdateAvailable {
					return fmt.Errorf("answers %d then %d, want Busy then UpdateAvailable", b.QueryImage.Response.Status, avail.QueryImage.Response.Status)
				}
				if w := avail.At.Sub(b.At); w < 180*time.Second {
					return fmt.Errorf("the DUT queried again %s after the Busy answer, against the 3m it was told", w)
				}
				th.note("queried again %s after Busy 180 s, then took the image", avail.At.Sub(b.At).Round(time.Second))
				return nil
			}},
			{
				n: "5", line: 404, title: "QueryImageResponse UpdateAvailable with an https ImageURI",
				na: "the TH's provider offers images over BDX only, so it cannot give the https ImageURI this step needs (TC-SU-2.2.test.ts:409-411)",
			},
			{n: "6", line: 414, title: "UpdateAvailable with a version no newer than the running one", run: noTransfer(func(th *goTH) (*provdef.QueryImageResponse, error) {
				id, err := r.o.identity(r.n)
				if err != nil {
					return nil, err
				}
				return unheld(th, id.version, ""), nil
			}, func(resp provdef.QueryImageResponse) error {
				id, err := r.o.identity(r.n)
				if err != nil {
					return err
				}
				if resp.SoftwareVersion == nil || *resp.SoftwareVersion > id.version {
					return fmt.Errorf("the TH offered SoftwareVersion %v against the %d the DUT runs", deref(resp.SoftwareVersion), id.version)
				}
				return nil
			})},
			{n: "7", line: 422, title: "UpdateAvailable with an invalid BDX ImageURI", run: noTransfer(func(th *goTH) (*provdef.QueryImageResponse, error) {
				id, err := r.o.identity(r.n)
				if err != nil {
					return nil, err
				}
				return unheld(th, id.version+1, "bdx://not-a-node-id/ota/image"), nil // INVALID_IMAGE_URI (:69)
			}, func(resp provdef.QueryImageResponse) error {
				if resp.ImageUri == nil || strings.HasPrefix(*resp.ImageUri, "bdx://") && len(*resp.ImageUri) > 22 && isHex16((*resp.ImageUri)[6:22]) {
					return fmt.Errorf("the TH offered ImageURI %v, whose authority is a node id", deref(resp.ImageUri))
				}
				return nil
			})},
		},
	}
}

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789ABCDEF", c) {
			return false
		}
	}
	return true
}

// unheldUpdate is #unheldUpdate (InProcessControllerAdapter.ts:552-573):
// an UpdateAvailable for an image the provider does not hold, its open
// fields filled as the provider fills its own — version, an "ota/unheld-<v>"
// BDX URI naming the provider, a 32-byte token (:765).
func (o *otaTH) unheldUpdate(version uint32, uri string) *provdef.QueryImageResponse {
	if uri == "" {
		uri = bdx.FileDesignator(fmt.Sprintf("ota/unheld-%d", version)).BdxURI(o.nodeID)
	}
	vs := fmt.Sprintf("%d.0.0", version)
	token := make([]byte, 32)
	for i := range token {
		token[i] = byte(i + 1)
	}
	return &provdef.QueryImageResponse{
		Status: provdef.StatusUpdateAvailable, ImageUri: &uri, SoftwareVersion: &version, SoftwareVersionString: &vs, UpdateToken: &token,
	}
}

// suUpdateTimeout is TC-SU-2.3's UPDATE_TIMEOUT (TC-SU-2.3.test.ts:34).
const suUpdateTimeout = 210 * time.Second

// caseSU23 is TC-SU-2.3: the consent the requestor obtains before the
// transfer, and the block size it asks for.
func caseSU23() goCertCase {
	r := &suRun{}
	return goCertCase{
		tc: "TC-SU-2.3", line: 211, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 219, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 240, pics: "MCORE.OTA.RequestorConsent", title: "RequestorCanConsent True, UserConsentNeeded True", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				after, err := th.latestStateChange(r.n)
				if err != nil {
					return err
				}
				r.o.script(r.peer, []softwareupdate.QueryImageScript{needsConsent(true)}, nil)
				s, err := r.o.serve(r.n, r.peer, serveOpts{budget: suUpdateTimeout})
				if err != nil {
					return err
				}
				q, err := singleQuery(s.ex)
				if err != nil {
					return err
				}
				if q.Request.RequestorCanConsent == nil || !*q.Request.RequestorCanConsent {
					return fmt.Errorf("the DUT sent RequestorCanConsent %v", deref(q.Request.RequestorCanConsent))
				}
				if q.Response.Status != provdef.StatusUpdateAvailable || q.Response.UserConsentNeeded == nil || !*q.Response.UserConsentNeeded {
					return fmt.Errorf("the TH answered status %d UserConsentNeeded %v", q.Response.Status, deref(q.Response.UserConsentNeeded))
				}
				changes, err := th.stateChanges(r.n, after)
				if err != nil {
					return err
				}
				consented, downloading := firstState(changes, otar.UpdateStateDelayedOnUserConsent), firstState(changes, otar.UpdateStateDownloading)
				if consented == 0 || downloading == 0 || consented > downloading {
					return fmt.Errorf("states %v: DelayedOnUserConsent as event %d, Downloading as event %d — consent must come first", statesOf(changes), consented, downloading)
				}
				_, _, moved := s.transfer.state()
				if moved != uint64(s.fileSize) {
					return fmt.Errorf("the DUT took %d of the %d bytes", moved, s.fileSize)
				}
				th.note("DelayedOnUserConsent (event %d) before Downloading (event %d); %d bytes taken", consented, downloading, moved)
				return nil
			}},
			{n: "2", line: 252, title: "UpdateAvailable: the transfer and its block size", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				s, err := r.o.serve(r.n, r.peer, serveOpts{budget: suUpdateTimeout})
				if err != nil {
					return err
				}
				q, err := singleQuery(s.ex)
				if err != nil {
					return err
				}
				if q.Response.Status != provdef.StatusUpdateAvailable {
					return fmt.Errorf("the TH answered %d", q.Response.Status)
				}
				if bs := s.transfer.init.MaxBlockSize; bs == 0 || bs > 1024 { // MAX_NON_TCP_BLOCK_SIZE (tc-su-support.ts:338)
					return fmt.Errorf("the DUT proposed %d bytes per block, against the plan's 1024 over a non-TCP transport", bs)
				}
				th.note("the DUT proposed %d bytes per block", s.transfer.init.MaxBlockSize)
				return nil
			}},
			{
				n: "3", line: 265, title: "UpdateAvailable with an https ImageURI",
				na: "the TH's provider offers images over BDX only, so it cannot give the https ImageURI this step needs (TC-SU-2.3.test.ts:272-273)",
			},
			{
				n: "4", line: 279, title: "force fail the transfer, wait for the Idle timeout",
				na: "the harness cannot fail a transfer part way (TC-SU-2.3.test.ts:286-288)",
			},
			{
				n: "5", line: 294, title: "force fail the transfer, resume with STARTOFS",
				na: "as step 4: with no transfer the harness can abort, there is nothing to resume (TC-SU-2.3.test.ts:302-304)",
			},
		},
	}
}

// firstState is the number of the first transition into state, 0 when
// none.
func firstState(c []stateChange, state otar.UpdateStateEnum) uint64 {
	for _, x := range c {
		if x.newState == uint64(state) {
			return x.number + 1 // event numbers start at 0; +1 keeps 0 for "none"
		}
	}
	return 0
}

// caseSU24 is TC-SU-2.4, which the chip-tool leg runs too: the
// ApplyUpdateRequest names the update the requestor downloaded.
func caseSU24() goCertCase {
	r := &suRun{}
	return goCertCase{
		tc: "TC-SU-2.4", line: 56, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 73, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 94, title: "the DUT sends ApplyUpdateRequest after the transfer", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				s, err := r.o.serve(r.n, r.peer, serveOpts{})
				if err != nil {
					return err
				}
				q, err := singleQuery(s.ex)
				if err != nil {
					return err
				}
				a, err := singleApply(s.ex)
				if err != nil {
					return err
				}
				if q.Response.UpdateToken == nil || !bytes.Equal(a.Request.UpdateToken, *q.Response.UpdateToken) {
					return fmt.Errorf("the DUT sent update token %x, the TH answered its QueryImage with %v", a.Request.UpdateToken, deref(q.Response.UpdateToken))
				}
				_, _, moved := s.transfer.state()
				if a.Request.NewVersion != s.version || moved != uint64(s.fileSize) {
					return fmt.Errorf("the DUT asked to apply version %d having downloaded %d of %d bytes of version %d", a.Request.NewVersion, moved, s.fileSize, s.version)
				}
				th.note("ApplyUpdateRequest token %x (the QueryImageResponse's), NewVersion %d", a.Request.UpdateToken, a.Request.NewVersion)
				return nil
			}},
		},
	}
}

// The SU-2.5 timings (TC-SU-2.5.test.ts:46-80).
const (
	su25ReturnWait  = 45 * time.Second
	su25PlanDelay   = 180
	su25ShortDelay  = 60
	su25ApplyFloor  = 120 * time.Second
	su25Tolerance   = 2 * time.Second
	su25Notify      = 30 * time.Second
	su25Refusal     = 15 * time.Second
	su25Margin      = 90 * time.Second
	su25RefusalSecs = 15
)

// caseSU25 is TC-SU-2.5: the requestor acts on each ApplyUpdateResponse
// action. The daemon applies in place under --ota-harness and runs its
// start-up path as a restart would, StartUp included
// (examples/reference-bridge/ota.go restartedWhenIdle).
func caseSU25() goCertCase {
	r := &suRun{}
	var (
		mu       sync.Mutex
		startUps []uint64
	)
	returned := func(th *goTH, version uint32) error {
		seen := func() bool { mu.Lock(); defer mu.Unlock(); return slices.Contains(startUps, uint64(version)) }
		if !waitFor(th.ctx, su25ReturnWait, seen) {
			return fmt.Errorf("the TH's subscription delivered no StartUp with SoftwareVersion %d within %s", version, su25ReturnWait)
		}
		return nil
	}
	runs := func(th *goTH, downloaded uint32) error {
		v, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrSoftwareVersion))
		if err != nil {
			return err
		}
		if v.El.Uint != uint64(downloaded) {
			return fmt.Errorf("the DUT reports SoftwareVersion %d, against the downloaded %d", v.El.Uint, downloaded)
		}
		return nil
	}
	deferred := func(answer softwareupdate.ApplyUpdateScript, asksAgain bool) func(th *goTH) error {
		return func(th *goTH) error {
			if err := r.ready(); err != nil {
				return err
			}
			deferral := su25PlanDelay * time.Second
			r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{answer})
			opts := serveOpts{applyTimeout: su25Margin, notifyTimeout: deferral + su25Notify}
			if asksAgain {
				opts = serveOpts{applyTimeout: deferral + su25Margin, notifyTimeout: su25Notify}
			}
			opts.budget = opts.applyTimeout + opts.notifyTimeout + su25Margin
			s, err := r.o.serve(r.n, r.peer, opts)
			if err != nil {
				return err
			}
			if len(s.ex.apply) == 0 {
				return errors.New("the DUT sent no ApplyUpdateRequest")
			}
			d := s.ex.apply[0]
			if d.ApplyUpdate.Response.Action != answer.Action || d.ApplyUpdate.Response.DelayedActionTime != su25PlanDelay {
				return fmt.Errorf("the TH answered %+v", d.ApplyUpdate.Response)
			}
			var resumed *softwareupdate.ProviderExchange
			if asksAgain && len(s.ex.apply) > 1 {
				resumed = &s.ex.apply[1]
			}
			if !asksAgain && len(s.ex.notify) > 0 {
				resumed = &s.ex.notify[0]
			}
			if resumed == nil {
				return fmt.Errorf("the TH received %d ApplyUpdateRequest and %d NotifyUpdateApplied", len(s.ex.apply), len(s.ex.notify))
			}
			if w := resumed.At.Sub(d.At); w < deferral-su25Tolerance {
				return fmt.Errorf("the DUT resumed %s after the deferral, against the %s it was told", w, deferral)
			}
			if asksAgain && s.ex.apply[1].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionProceed {
				return fmt.Errorf("the TH answered the second request %+v", s.ex.apply[1].ApplyUpdate.Response)
			}
			if len(s.ex.notify) == 0 {
				return errors.New("the DUT sent no NotifyUpdateApplied")
			}
			if err := runs(th, s.version); err != nil {
				return err
			}
			if err := returned(th, s.version); err != nil {
				return err
			}
			th.note("resumed %s after the %s deferral, applied version %d", resumed.At.Sub(d.At).Round(time.Second), deferral, s.version)
			return nil
		}
	}
	d180, d60 := uint32(su25PlanDelay), uint32(su25ShortDelay)
	return goCertCase{
		tc: "TC-SU-2.5", line: 482, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 498, title: "TH and DUT are on the same fabric, no OTA process, StartUp observed", run: func(th *goTH) error {
				if err := r.precondition(th); err != nil {
					return err
				}
				// The StartUp events the default subscription delivers
				// (observeEvents, TC-SU-2.5.test.ts:513-517).
				r.o.observeEvents(func(cluster, event uint32, _ uint64, value any) {
					if cluster != basicinfo.ClusterID || event != basicinfo.EventStartUp {
						return
					}
					var v uint64
					switch x := value.(type) {
					case basicinfo.StartUpEvent:
						v = uint64(x.SoftwareVersion)
					case *basicinfo.StartUpEvent:
						v = uint64(x.SoftwareVersion)
					case spec.Node:
						f, _ := structField(x, 0)
						v = f.El.Uint
					default:
						return
					}
					mu.Lock()
					startUps = append(startUps, v)
					mu.Unlock()
				})
				return nil
			}},
			{n: "1", line: 535, title: "ApplyUpdateResponse Proceed, DelayedActionTime 0", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				s, err := r.o.serve(r.n, r.peer, serveOpts{applyTimeout: su25Margin, notifyTimeout: su25Notify})
				if err != nil {
					return err
				}
				a, err := singleApply(s.ex)
				if err != nil {
					return err
				}
				if a.Response.Action != provdef.ApplyUpdateActionProceed || a.Response.DelayedActionTime != 0 {
					return fmt.Errorf("the TH answered %+v", a.Response)
				}
				if err := runs(th, s.version); err != nil {
					return err
				}
				if err := returned(th, s.version); err != nil {
					return err
				}
				th.note("Proceed 0 s, one ApplyUpdateRequest, version %d running, StartUp observed", s.version)
				return nil
			}},
			{
				n: "2", line: 548, long: "the DUT waits out the three minutes the TH named before it applies (TC-SU-2.5.test.ts:554)",
				title: "ApplyUpdateResponse Proceed, DelayedActionTime 3 minutes", run: deferred(applyAnswer(provdef.ApplyUpdateActionProceed, &d180), false),
			},
			{
				n: "3", line: 561, long: "the plan watches the DUT for the two-minute floor it owes (TC-SU-2.5.test.ts:567)",
				title: "ApplyUpdateResponse AwaitNextAction, 1 minute", run: func(th *goTH) error {
					if err := r.ready(); err != nil {
						return err
					}
					r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{applyAnswer(provdef.ApplyUpdateActionAwaitNextAction, &d60)})
					s, err := r.o.serve(r.n, r.peer, serveOpts{applyTimeout: su25ApplyFloor + su25Margin, notifyTimeout: su25Notify, budget: su25ApplyFloor + 2*su25Margin + su25Notify})
					if err != nil {
						return err
					}
					if len(s.ex.apply) < 2 {
						return fmt.Errorf("the TH received %d ApplyUpdateRequest command(s), where this step needs two", len(s.ex.apply))
					}
					first, again := s.ex.apply[0], s.ex.apply[1]
					if first.ApplyUpdate.Response.Action != provdef.ApplyUpdateActionAwaitNextAction || first.ApplyUpdate.Response.DelayedActionTime != su25ShortDelay {
						return fmt.Errorf("the TH answered %+v", first.ApplyUpdate.Response)
					}
					if w := again.At.Sub(first.At); w < su25ApplyFloor-su25Tolerance {
						return fmt.Errorf("the DUT asked again %s after the deferral, against the %s floor", w, su25ApplyFloor)
					}
					for _, x := range s.ex.notify {
						if x.At.Before(again.At) {
							return errors.New("the DUT applied while it was holding the update back")
						}
					}
					if err := returned(th, s.version); err != nil {
						return err
					}
					th.note("asked again %s after AwaitNextAction 60 s", again.At.Sub(first.At).Round(time.Second))
					return nil
				},
			},
			{
				n: "4", line: 574, long: "the DUT waits out the three minutes the TH named before it asks again (TC-SU-2.5.test.ts:581)",
				title: "ApplyUpdateResponse AwaitNextAction, 3 minutes, then Proceed", run: deferred(applyAnswer(provdef.ApplyUpdateActionAwaitNextAction, &d180), true),
			},
			{n: "5", line: 589, title: "ApplyUpdateResponse Discontinue", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				before, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrSoftwareVersion))
				if err != nil {
					return err
				}
				r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{applyAnswer(provdef.ApplyUpdateActionDiscontinue, nil)})
				s, err := r.o.serve(r.n, r.peer, serveOpts{applyTimeout: su25Margin, observeAfter: su25Refusal})
				if err != nil {
					return err
				}
				if len(s.ex.apply) == 0 || s.ex.apply[0].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionDiscontinue {
					return fmt.Errorf("the TH did not answer the DUT's ApplyUpdateRequest Discontinue: %d requests", len(s.ex.apply))
				}
				if s.observed < su25Refusal {
					return fmt.Errorf("the TH watched for %s after the refusal, against %s", s.observed, su25Refusal)
				}
				if len(s.ex.notify) > 0 {
					return fmt.Errorf("the DUT sent %d NotifyUpdateApplied after the refusal", len(s.ex.notify))
				}
				if err := runs(th, uint32(before.El.Uint)); err != nil { //nolint:gosec // a software version
					return err
				}
				th.note("Discontinue (DelayedActionTime %d), no notification in %s, still version %d", s.ex.apply[0].ApplyUpdate.Response.DelayedActionTime, s.observed.Round(time.Second), before.El.Uint)
				return th.requestorIdle(r.n)
			}},
		},
	}
}

// caseSU26 is TC-SU-2.6: the requestor's NotifyUpdateApplied and the boot
// reason after the update.
func caseSU26() goCertCase {
	r := &suRun{}
	var served *otaServed
	return goCertCase{
		tc: "TC-SU-2.6", line: 153, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 167, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 189, pics: "OTAR.C.M.NotifyUpdateApplied", title: "DUT sends NotifyUpdateApplied after installing the update", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				s, err := r.o.serve(r.n, r.peer, serveOpts{notifyTimeout: 30 * time.Second}) // NOTIFY_APPLIED_TIMEOUT (:38)
				if err != nil {
					return err
				}
				served = s
				q, err := singleQuery(s.ex)
				if err != nil {
					return err
				}
				if len(s.ex.notify) == 0 {
					return errors.New("the DUT sent no NotifyUpdateApplied")
				}
				n := s.ex.notify[0].NotifyUpdateApplied
				if q.Response.UpdateToken == nil || !bytes.Equal(n.UpdateToken, *q.Response.UpdateToken) {
					return fmt.Errorf("the DUT sent update token %x, the TH answered %v", n.UpdateToken, deref(q.Response.UpdateToken))
				}
				if n.SoftwareVersion != s.version {
					return fmt.Errorf("the DUT reported version %d applied, having downloaded %d", n.SoftwareVersion, s.version)
				}
				v, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrSoftwareVersion))
				if err != nil {
					return err
				}
				if v.El.Uint != uint64(s.version) {
					return fmt.Errorf("the DUT reports SoftwareVersion %d, against the downloaded %d", v.El.Uint, s.version)
				}
				th.note("NotifyUpdateApplied token %x version %d; SoftwareVersion %d", n.UpdateToken, n.SoftwareVersion, v.El.Uint)
				return nil
			}},
			{n: "2", line: 202, pics: "DGGEN.S.A0004", title: "TH reads BootReason", run: func(th *goTH) error {
				if served == nil || !served.applied {
					return errors.New("step 1's update never reached an apply the TH allowed, so no boot follows from it")
				}
				v, err := th.readValue(r.n, attrPath(0, generaldiagnostics.ClusterID, generaldiagnostics.AttrBootReason))
				if err != nil {
					return err
				}
				if generaldiagnostics.BootReasonEnum(v.El.Uint) != generaldiagnostics.BootReasonSoftwareUpdateCompleted { //nolint:gosec // an enum value
					return fmt.Errorf("the DUT reports BootReason %d, where SoftwareUpdateCompleted is %d", v.El.Uint, generaldiagnostics.BootReasonSoftwareUpdateCompleted)
				}
				return nil
			}},
		},
	}
}

// caseSU27 is TC-SU-2.7: the requestor's state transitions as a subscriber
// sees them, and the VersionApplied event.
func caseSU27() goCertCase {
	r := &suRun{}
	var (
		mu       sync.Mutex
		received []stateChange
		watching bool
	)
	watch := func(th *goTH) error {
		mu.Lock()
		received = nil
		w := watching
		mu.Unlock()
		if w {
			return nil
		}
		// The requestor's events the default subscription delivers
		// (observeEvents, TC-SU-2.7.test.ts:74-97).
		r.o.observeEvents(func(cluster, event uint32, number uint64, value any) {
			if cluster != otar.ClusterID || event != otar.EventStateTransition {
				return
			}
			var ns uint64
			switch x := value.(type) {
			case otar.StateTransitionEvent:
				ns = uint64(x.NewState)
			case *otar.StateTransitionEvent:
				ns = uint64(x.NewState)
			case spec.Node:
				f, _ := structField(x, 1)
				ns = f.El.Uint
			default:
				return
			}
			mu.Lock()
			received = append(received, stateChange{number: number, newState: ns})
			mu.Unlock()
		})
		mu.Lock()
		received, watching = nil, true
		mu.Unlock()
		return nil
	}
	seen := func() []uint64 {
		mu.Lock()
		defer mu.Unlock()
		c := slices.Clone(received)
		slices.SortFunc(c, func(a, b stateChange) int { return int(a.number) - int(b.number) }) //nolint:gosec // event numbers of one run
		return statesOf(c)
	}
	saw := func(th *goTH, states ...otar.UpdateStateEnum) error {
		for _, s := range states {
			if !waitFor(th.ctx, 45*time.Second, func() bool { return slices.Contains(seen(), uint64(s)) }) { // EVENT_WAIT (:46)
				return fmt.Errorf("the subscriber was told of %v, where this step is about %d", seen(), s)
			}
		}
		th.note("the subscriber was told of %v", seen())
		return nil
	}
	update := func(th *goTH) (*otaServed, error) {
		return r.o.serve(r.n, r.peer, serveOpts{applyTimeout: 180 * time.Second, notifyTimeout: 30 * time.Second, budget: 210 * time.Second}) // UPDATE_TIMEOUT, NOTIFY_APPLIED_TIMEOUT (:52-55)
	}
	busyDelay := uint32(60) // BUSY_DELAYED_ACTION_TIME (:64)
	return goCertCase{
		tc: "TC-SU-2.7", line: 360, pics: suPICS, native: true,
		steps: []goStep{
			{n: "0", line: 376, title: "TH and DUT are on the same fabric, no OTA process", run: r.precondition},
			{n: "1", line: 400, title: "Perform a software update on the DUT", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				if err := watch(th); err != nil {
					return err
				}
				if _, err := update(th); err != nil {
					return err
				}
				if err := saw(th, otar.UpdateStateQuerying, otar.UpdateStateDownloading, otar.UpdateStateApplying); err != nil {
					return err
				}
				// Idle is optional (:162-182).
				waitFor(th.ctx, 45*time.Second, func() bool { return slices.Contains(seen(), uint64(otar.UpdateStateIdle)) })
				return nil
			}},
			{n: "2", line: 405, title: "QueryImageResponse Busy", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				if err := watch(th); err != nil {
					return err
				}
				r.o.script(r.peer, []softwareupdate.QueryImageScript{busy(busyDelay)}, nil)
				a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable})
				if err != nil {
					return err
				}
				if len(a.ex.query) == 0 || a.ex.query[0].QueryImage.Response.Status != provdef.StatusBusy {
					return errors.New("the DUT did not query, or the TH did not answer Busy")
				}
				return saw(th, otar.UpdateStateDelayedOnQuery)
			}},
			{n: "3", line: 416, title: "TH/OTA-P does not respond", na: "the TH's provider answers every QueryImage it receives (TC-SU-2.7.test.ts:421-423)"},
			{n: "4", line: 429, pics: "MCORE.OTA.RequestorConsent", title: "UserConsentNeeded True", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				if err := watch(th); err != nil {
					return err
				}
				r.o.script(r.peer, []softwareupdate.QueryImageScript{needsConsent(true)}, nil)
				if _, err := update(th); err != nil {
					return err
				}
				return saw(th, otar.UpdateStateDelayedOnUserConsent)
			}},
			{n: "5", line: 441, title: "Force an error during the download", na: "the harness cannot fail a transfer part way (TC-SU-2.7.test.ts:447-449)"},
			{n: "6", line: 456, title: "ApplyUpdateResponse AwaitNextAction", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				if err := watch(th); err != nil {
					return err
				}
				r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{applyAnswer(provdef.ApplyUpdateActionAwaitNextAction, &busyDelay)})
				if _, err := update(th); err != nil {
					return err
				}
				return saw(th, otar.UpdateStateDelayedOnApply)
			}},
			{n: "7", line: 467, title: "read the VersionApplied event", run: func(th *goTH) error {
				if err := r.ready(); err != nil {
					return err
				}
				rep, err := r.n.Read(th.ctx, client.ReadRequest{ReadRequest: client.ReadPaths{Events: []im.ConcreteEventPath{{
					Endpoint: otaRequestorEndpoint, Cluster: otar.ClusterID, Event: otar.EventVersionApplied, HasEndpoint: true, HasCluster: true, HasEvent: true,
				}}}, IncludeKnownVersions: true})
				if err != nil {
					return err
				}
				if len(rep.Events) == 0 {
					return errors.New("the DUT reported no VersionApplied event")
				}
				latest := &rep.Events[0]
				for i := range rep.Events {
					e := &rep.Events[i]
					if e.Number > latest.Number {
						latest = e
					}
				}
				ev, _ := structField(latest.Value, 0)
				ep, _ := structField(latest.Value, 1)
				sw, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrSoftwareVersion))
				if err != nil {
					return err
				}
				pid, err := th.readValue(r.n, attrPath(0, basicinfo.ClusterID, basicinfo.AttrProductId))
				if err != nil {
					return err
				}
				if ev.El.Uint != sw.El.Uint || ep.El.Uint != pid.El.Uint {
					return fmt.Errorf("VersionApplied names version %d product 0x%04X, the DUT reports %d and 0x%04X", ev.El.Uint, ep.El.Uint, sw.El.Uint, pid.El.Uint)
				}
				th.note("VersionApplied version %d product 0x%04X, as Basic Information reports", ev.El.Uint, ep.El.Uint)
				return nil
			}},
		},
	}
}

// suMirrored is the state of the SU-3.x cases, read mirrored: the Go TH is
// the DUT provider, the daemon the requestor the TS calls TH.
type suMirrored = suRun

// caseSU31 mirrors TC-SU-3.1: the provider answers the requestor's
// QueryImage; what the daemon sent and took is what the case checks.
func caseSU31() goCertCase {
	r := &suMirrored{}
	var served *otaServed
	return goCertCase{
		tc: "TC-SU-3.1", line: 95, pics: []string{"MCORE.OTA.Provider"},
		steps: []goStep{
			{n: "0", line: 101, title: "the DUT commissions the TH, stages an image and announces itself", run: func(th *goTH) error {
				if err := r.precondition(th); err != nil {
					return err
				}
				var err error
				served, err = r.o.serve(r.n, r.peer, serveOpts{})
				return err
			}},
			{n: "1", line: 124, title: "OTA-R/TH sends a QueryImage Command, UserConsentNeeded False", run: func(th *goTH) error {
				if served == nil {
					return errors.New("the precondition step served no update")
				}
				q, err := singleQuery(served.ex)
				if err != nil {
					return err
				}
				resp := q.Response
				if resp.Status != provdef.StatusUpdateAvailable {
					return fmt.Errorf("the TH's QueryImage for version %d was answered %d", q.Request.SoftwareVersion, resp.Status)
				}
				if resp.ImageUri == nil || resp.SoftwareVersion == nil || resp.SoftwareVersionString == nil || resp.UpdateToken == nil {
					return fmt.Errorf("the UpdateAvailable answer lacks a mandatory field: %+v", resp)
				}
				_, _, moved := served.transfer.state()
				if moved != uint64(served.fileSize) {
					return fmt.Errorf("the requestor took %d of the %d bytes", moved, served.fileSize)
				}
				// userConsentCheck (tc-su-support.ts:117-154).
				if resp.UserConsentNeeded != nil && *resp.UserConsentNeeded && (q.Request.RequestorCanConsent == nil || !*q.Request.RequestorCanConsent) {
					return errors.New("UserConsentNeeded True to a requestor that cannot consent")
				}
				th.note("UpdateAvailable version %d (%q) at %s, token %x; the requestor took all %d bytes", *resp.SoftwareVersion, *resp.SoftwareVersionString, *resp.ImageUri, *resp.UpdateToken, moved)
				return nil
			}},
		},
	}
}

// caseSU32 mirrors TC-SU-3.2: the provider's QueryImageResponse fields, its
// NotAvailable with nothing staged, its BDX URI and its Busy answer.
func caseSU32() goCertCase {
	r := &suMirrored{}
	var (
		served   *otaServed
		unstaged otaExchanges
	)
	return goCertCase{
		tc: "TC-SU-3.2", line: 194, pics: []string{"MCORE.OTA.Provider"},
		steps: []goStep{
			{n: "0a", line: 200, title: "the DUT commissions the TH and announces itself with nothing staged", run: func(th *goTH) error {
				if err := r.precondition(th); err != nil {
					return err
				}
				a, err := r.o.announce(r.n, r.peer, announceOpts{reason: otar.AnnouncementReasonUpdateAvailable})
				if err != nil {
					return err
				}
				unstaged = a.ex
				if len(a.ex.query) != 1 {
					return fmt.Errorf("the announcement drew %d QueryImage command(s)", len(a.ex.query))
				}
				return nil
			}},
			{n: "0b", line: 234, title: "the DUT stages an image and serves it", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("the TH was not commissioned by the preceding step")
				}
				var err error
				served, err = r.o.serve(r.n, r.peer, serveOpts{})
				return err
			}},
			{n: "1", line: 249, title: "QueryImageResponse UpdateAvailable fields", run: func(th *goTH) error {
				if served == nil {
					return errors.New("no update was served")
				}
				q, err := singleQuery(served.ex)
				if err != nil {
					return err
				}
				resp := q.Response
				if resp.Status != provdef.StatusUpdateAvailable || resp.ImageUri == nil || resp.SoftwareVersion == nil || resp.SoftwareVersionString == nil {
					return fmt.Errorf("the answer %+v lacks UpdateAvailable's fields", resp)
				}
				if *resp.SoftwareVersion != served.version {
					return fmt.Errorf("offered version %d, served %d", *resp.SoftwareVersion, served.version)
				}
				if resp.UpdateToken == nil || len(*resp.UpdateToken) < 8 || len(*resp.UpdateToken) > 32 {
					return fmt.Errorf("UpdateToken %v is not 8-32 bytes", deref(resp.UpdateToken))
				}
				th.note("UpdateAvailable version %d token of %d bytes; the requestor took it", served.version, len(*resp.UpdateToken))
				return nil
			}},
			{n: "2", line: 261, title: "no update available: NotAvailable", run: func(th *goTH) error {
				q, err := singleQuery(unstaged)
				if err != nil {
					return err
				}
				if q.Response.Status != provdef.StatusNotAvailable {
					return fmt.Errorf("with nothing staged the answer was %d", q.Response.Status)
				}
				th.note("with nothing staged for the requestor's version %d, the answer was NotAvailable", q.Request.SoftwareVersion)
				return nil
			}},
			{n: "3", line: 283, title: "the ImageURI is a bdx URI", run: func(th *goTH) error {
				if served == nil {
					return errors.New("no update was served")
				}
				q, err := singleQuery(served.ex)
				if err != nil {
					return err
				}
				if q.Response.ImageUri == nil {
					return errors.New("the UpdateAvailable answer carries no ImageURI")
				}
				if f := bdxImageURIFindings(*q.Response.ImageUri, r.o.nodeID); len(f) > 0 {
					return fmt.Errorf("ImageURI %q: %s", *q.Response.ImageUri, strings.Join(f, "; "))
				}
				th.note("ImageURI %q: scheme, authority and path conform", *q.Response.ImageUri)
				return nil
			}},
			{n: "4", line: 321, pics: "MCORE.OTA.HTTPS", title: "https ImageURI", run: func(*goTH) error {
				return errors.New("this step needs an https image URI, which the provider's PICS says it does not send")
			}},
			{
				n: "5", line: 330, pics: "OTAP.S.M.DelayedActionTime", long: "the TH waits out the DelayedActionTime the DUT named (TC-SU-3.2.test.ts:337)",
				title: "Busy with DelayedActionTime 3 minutes, then UpdateAvailable", run: func(th *goTH) error {
					if r.n == nil {
						return errors.New("the TH was not commissioned")
					}
					delay := uint32(180) // PLAN_DELAYED_ACTION_TIME (tc-su-support.ts:415)
					r.o.script(r.peer, []softwareupdate.QueryImageScript{busy(delay)}, nil)
					s, err := r.o.serve(r.n, r.peer, serveOpts{budget: time.Duration(delay+90) * time.Second})
					if err != nil {
						return err
					}
					if len(s.ex.query) != 2 || s.ex.query[0].QueryImage.Response.Status != provdef.StatusBusy || s.ex.query[1].QueryImage.Response.Status != provdef.StatusUpdateAvailable {
						return fmt.Errorf("the requestor sent %d QueryImage; want Busy then UpdateAvailable", len(s.ex.query))
					}
					if d := s.ex.query[0].QueryImage.Response.DelayedActionTime; d == nil || *d != delay {
						return fmt.Errorf("Busy carried DelayedActionTime %v, the script %d", deref(d), delay)
					}
					th.note("Busy 180 s, the requestor queried again after %s and took the image", s.ex.query[1].At.Sub(s.ex.query[0].At).Round(time.Second))
					return nil
				},
			},
		},
	}
}

// bdxImageURIFindings is tc-su-support.ts:220-281.
func bdxImageURIFindings(uri string, provider uint64) []string {
	var f []string
	if !strings.HasPrefix(uri, "bdx://") {
		return []string{`it does not begin with the lowercase scheme "bdx://"`}
	}
	if len(uri) < 24 {
		f = append(f, fmt.Sprintf("it is %d characters, where the plan requires 24 or more", len(uri)))
	}
	rest := uri[len("bdx://"):]
	i := strings.Index(rest, "/")
	if i < 0 {
		return append(f, "it carries no path naming the software image")
	}
	authority, path := rest[:i], rest[i:]
	if strings.Contains(authority, "@") {
		f = append(f, "its authority carries a user section")
	}
	if want := fmt.Sprintf("%016X", provider); authority != want {
		f = append(f, fmt.Sprintf("its authority is %q, not %q", authority, want))
	}
	if strings.ContainsAny(path, "?#") {
		f = append(f, "it carries a query or fragment field")
	}
	if path == "/" {
		f = append(f, "its path names no software image")
	}
	for _, c := range path {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~!$&'()*+,;=:@/%", c) {
			f = append(f, fmt.Sprintf("its path carries %q, which a URI cannot hold", c))
		}
	}
	return f
}

// caseSU33 mirrors TC-SU-3.3: the transfer the provider conducts and the
// UserConsentNeeded it states.
func caseSU33() goCertCase {
	r := &suMirrored{}
	var served *otaServed
	consent := func(needed bool) func(th *goTH) error {
		return func(th *goTH) error {
			if r.n == nil {
				return errors.New("the TH was not commissioned by the precondition step")
			}
			r.o.script(r.peer, []softwareupdate.QueryImageScript{needsConsent(needed)}, nil)
			s, err := r.o.serve(r.n, r.peer, serveOpts{})
			if err != nil {
				return err
			}
			q, err := singleQuery(s.ex)
			if err != nil {
				return err
			}
			if q.Response.UserConsentNeeded == nil || *q.Response.UserConsentNeeded != needed {
				return fmt.Errorf("the provider sent UserConsentNeeded %v", deref(q.Response.UserConsentNeeded))
			}
			_, _, moved := s.transfer.state()
			if moved != uint64(s.fileSize) {
				return fmt.Errorf("the requestor took %d of %d bytes", moved, s.fileSize)
			}
			th.note("UserConsentNeeded %t; the requestor took all %d bytes", needed, moved)
			return nil
		}
	}
	return goCertCase{
		tc: "TC-SU-3.3", line: 171, pics: []string{"MCORE.OTA.Provider"},
		steps: []goStep{
			{n: "0", line: 177, title: "the DUT commissions the TH, stages an image and announces itself", run: func(th *goTH) error {
				if err := r.precondition(th); err != nil {
					return err
				}
				var err error
				served, err = r.o.serve(r.n, r.peer, serveOpts{})
				return err
			}},
			{n: "1", line: 197, title: "the transfer: receiver drive, block size, every block full", run: func(th *goTH) error {
				if served == nil {
					return errors.New("the precondition step served no update")
				}
				t := served.transfer
				a, ok := t.accept()
				if !ok {
					return errors.New("the TH sent no ReceiveAccept")
				}
				if !a.TransferControl.ReceiverDrive || a.TransferControl.AsynchronousTransfer {
					return fmt.Errorf("the provider granted %+v", a.TransferControl)
				}
				if !blockSizeConforms(t.init.MaxBlockSize, a.MaxBlockSize) {
					return fmt.Errorf("the requestor proposed %d bytes and the provider granted %d", t.init.MaxBlockSize, a.MaxBlockSize)
				}
				if t.init.MaxBlockSize >= 1024 && a.MaxBlockSize < 1024 {
					return fmt.Errorf("granted %d bytes, below the plan's 1024 over a non-TCP transport", a.MaxBlockSize)
				}
				blocks := t.messages(true, bdx.MessageBlock)
				want := (served.fileSize+int(a.MaxBlockSize)-1)/int(a.MaxBlockSize) - 1
				short := 0
				for _, b := range blocks {
					if b.length != int(a.MaxBlockSize) {
						short++
					}
				}
				if len(blocks) != want || short > 0 {
					return fmt.Errorf("%d non-final blocks of the %d a %d-byte image needs at %d bytes, %d not full", len(blocks), want, served.fileSize, a.MaxBlockSize, short)
				}
				th.note("receiver drive, proposed %d granted %d bytes, %d full blocks then BlockEOF", t.init.MaxBlockSize, a.MaxBlockSize, len(blocks))
				return nil
			}},
			{n: "2", line: 212, pics: "OTAP.S.M.UserConsentNeeded", title: "UserConsentNeeded True", run: consent(true)},
			{n: "3", line: 224, pics: "OTAP.S.M.UserConsentNeeded", title: "UserConsentNeeded False", run: consent(false)},
			{n: "4", line: 236, title: "force fail the transfer", na: "the harness cannot fail a transfer part way (TC-SU-3.3.test.ts:243-248)"},
			{n: "5", line: 251, title: "force fail the transfer, then resume", na: "as step 4 (TC-SU-3.3.test.ts:257-262)"},
		},
	}
}

// blockSizeConforms is tc-su-support.ts:361-373.
func blockSizeConforms(proposed, granted uint16) bool {
	if granted < 1 || granted > proposed {
		return false
	}
	if proposed > 128 {
		return granted&(granted-1) == 0
	}
	return proposed < 16 || granted == proposed
}

// caseSU34 mirrors TC-SU-3.4: the provider's ApplyUpdateResponse —
// Proceed, a deferral, and a refusal after which a later query starts
// again. The daemon applies what it downloaded, as the TS's
// --autoApplyImage requestor (TC-SU-3.4.test.ts:219).
func caseSU34() goCertCase {
	r := &suMirrored{}
	var served *otaServed
	return goCertCase{
		tc: "TC-SU-3.4", line: 211, pics: []string{"MCORE.OTA.Provider"},
		steps: []goStep{
			{n: "0", line: 225, title: "the DUT commissions the TH, stages an image and serves it to the apply", run: func(th *goTH) error {
				if err := r.precondition(th); err != nil {
					return err
				}
				var err error
				served, err = r.o.serve(r.n, r.peer, serveOpts{})
				return err
			}},
			{n: "1", line: 247, title: "ApplyUpdateRequest answered Proceed", run: func(th *goTH) error {
				if served == nil {
					return errors.New("the precondition step served no update")
				}
				a, err := singleApply(served.ex)
				if err != nil {
					return err
				}
				q, err := singleQuery(served.ex)
				if err != nil {
					return err
				}
				if a.Response.Action != provdef.ApplyUpdateActionProceed {
					return fmt.Errorf("the provider answered %+v", a.Response)
				}
				if q.Response.UpdateToken == nil || !bytes.Equal(a.Request.UpdateToken, *q.Response.UpdateToken) || a.Request.NewVersion != served.version {
					return fmt.Errorf("the ApplyUpdateRequest %+v does not name the update offered (version %d)", a.Request, served.version)
				}
				th.note("Proceed, DelayedActionTime %d s, for token %x version %d", a.Response.DelayedActionTime, a.Request.UpdateToken, a.Request.NewVersion)
				return nil
			}},
			{n: "2", line: 257, long: "the TH waits out the DelayedActionTime the DUT named (TC-SU-3.4.test.ts:263)", title: "AwaitNextAction, then Proceed", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("the TH was not commissioned")
				}
				delay := uint32(180)
				r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{applyAnswer(provdef.ApplyUpdateActionAwaitNextAction, &delay)})
				s, err := r.o.serve(r.n, r.peer, serveOpts{applyTimeout: time.Duration(delay+30) * time.Second, budget: time.Duration(delay+120) * time.Second})
				if err != nil {
					return err
				}
				if len(s.ex.apply) != 2 || s.ex.apply[0].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionAwaitNextAction ||
					s.ex.apply[0].ApplyUpdate.Response.DelayedActionTime != delay || s.ex.apply[1].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionProceed {
					return fmt.Errorf("the requestor sent %d ApplyUpdateRequest; want AwaitNextAction %d s then Proceed", len(s.ex.apply), delay)
				}
				th.note("AwaitNextAction 180 s, asked again after %s, Proceed", s.ex.apply[1].At.Sub(s.ex.apply[0].At).Round(time.Second))
				return nil
			}},
			{n: "3", line: 269, title: "Discontinue; a later QueryImage starts again", run: func(th *goTH) error {
				if r.n == nil {
					return errors.New("the TH was not commissioned")
				}
				r.o.script(r.peer, nil, []softwareupdate.ApplyUpdateScript{applyAnswer(provdef.ApplyUpdateActionDiscontinue, nil)})
				refused, err := r.o.serve(r.n, r.peer, serveOpts{applyTimeout: 5 * time.Second}) // REFUSED_APPLY_BUDGET (:83)
				if err != nil {
					return err
				}
				restarted, err := r.o.serve(r.n, r.peer, serveOpts{})
				if err != nil {
					return err
				}
				if len(refused.ex.apply) == 0 || refused.ex.apply[0].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionDiscontinue {
					return errors.New("the provider did not answer the ApplyUpdateRequest Discontinue")
				}
				q, err := singleQuery(restarted.ex)
				if err != nil {
					return err
				}
				_, _, moved := restarted.transfer.state()
				if q.Response.Status != provdef.StatusUpdateAvailable || moved != uint64(restarted.fileSize) {
					return fmt.Errorf("the later update: status %d, %d of %d bytes", q.Response.Status, moved, restarted.fileSize)
				}
				if so := restarted.transfer.init.StartOffset; so != nil && *so != 0 {
					return fmt.Errorf("the restarted update began at offset %d", *so)
				}
				th.note("Discontinue, then a fresh update from offset 0: version %d, %d bytes", restarted.version, moved)
				return nil
			}},
		},
	}
}

// caseSU41 is TC-SU-4.1, which the chip-tool leg runs too (TC_SU_4_1.py):
// DefaultOTAProviders per fabric. TH3 is a second Go controller with a
// fabric of its own.
func caseSU41() goCertCase {
	var (
		n1, n3   *client.Node
		fab      = map[string]uint8{}
		requestE uint16
	)
	const (
		th2Node, th3Node, th4Node = 1, 3, 4 // TH2_NODE_ID … TH4_NODE_ID (:29-31)
	)
	providers := func(role string, ids ...uint64) spec.Encodable {
		out := make([]otar.ProviderLocation, 0, len(ids))
		for _, id := range ids {
			out = append(out, otar.ProviderLocation{ProviderNodeId: id, Endpoint: 0, FabricIndex: fab[role]})
		}
		return listOf(out...)
	}
	path := func(attr uint32) im.ConcreteAttributePath { return attrPath(requestE, otar.ClusterID, attr) }
	holds := func(th *goTH, role string, n *client.Node, want ...uint64) error {
		v, err := th.readValue(n, path(otar.AttrDefaultOtaProviders))
		if err != nil {
			return err
		}
		var got []uint64
		for _, c := range v.Children {
			id, _ := structField(c, 1)
			fi, _ := structField(c, 254)
			if fi.El.Uint != uint64(fab[role]) {
				return fmt.Errorf("fabric %d (%s) DefaultOTAProviders holds an entry of fabric %d", fab[role], role, fi.El.Uint)
			}
			got = append(got, id.El.Uint)
		}
		if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
			return fmt.Errorf("fabric %d (%s) DefaultOTAProviders: provider nodes %v, expected %v", fab[role], role, got, want)
		}
		th.note("fabric %d (%s) holds %v", fab[role], role, got)
		return nil
	}
	write := func(th *goTH, n *client.Node, v spec.Encodable) (client.WriteStatuses, error) {
		ctx, cancel := context.WithTimeout(th.ctx, time.Minute)
		defer cancel()
		return n.Write(ctx, []client.WriteRequest{{Path: path(otar.AttrDefaultOtaProviders), Value: v}}, client.WriteOptions{})
	}
	return goCertCase{
		tc: "TC-SU-4.1", line: 128, pics: []string{"MCORE.OTA.Requestor"}, native: true,
		steps: []goStep{
			{n: "0", line: 135, title: "Commissioning, already done", run: func(th *goTH) error {
				c, n, err := th.commission("th")
				if err != nil {
					return err
				}
				n1 = n
				if fab["th"], err = th.ownFabricIndex(n1); err != nil {
					return err
				}
				if requestE, err = th.endpointServing(n1, otar.ClusterID); err != nil {
					return err
				}
				w, err := n1.OpenCommissioningWindow(th.ctx, 180*time.Second, client.WindowOptions{}) // COMMISSIONING_WINDOW_SECONDS (:35)
				if err != nil {
					return err
				}
				th3, err := th.controller("th3")
				if err != nil {
					return err
				}
				if n3, err = th.pair(th3, w.ManualCode); err != nil {
					return err
				}
				if fab["th3"], err = th.ownFabricIndex(n3); err != nil {
					return err
				}
				_ = c
				th.note("DUT on fabric %d (TH) and fabric %d (TH3), requestor on endpoint %d", fab["th"], fab["th3"], requestE)
				return nil
			}},
			{n: "1", line: 180, title: "write TH2 on the first fabric", run: func(th *goTH) error {
				st, err := write(th, n1, providers("th", th2Node))
				if err != nil {
					return err
				}
				th.note("write [TH2] on fabric %d: %v", fab["th"], st)
				return st.Err()
			}},
			{n: "2", line: 192, title: "read on the first fabric", run: func(th *goTH) error { return holds(th, "th", n1, th2Node) }},
			{n: "3", line: 198, title: "write TH3 on the second fabric", run: func(th *goTH) error {
				st, err := write(th, n3, providers("th3", th3Node))
				if err != nil {
					return err
				}
				th.note("write [TH3] on fabric %d: %v", fab["th3"], st)
				return st.Err()
			}},
			{n: "4", line: 210, title: "read on both fabrics", run: func(th *goTH) error {
				return errors.Join(holds(th, "th", n1, th2Node), holds(th, "th3", n3, th3Node))
			}},
			{n: "5", line: 223, title: "two providers on one fabric: CONSTRAINT_ERROR", run: func(th *goTH) error {
				st, err := write(th, n1, providers("th", th4Node, th2Node))
				if err != nil {
					return err
				}
				if !slices.ContainsFunc(st, func(s client.WriteStatus) bool { return s.Status == im.StatusConstraintError }) {
					return fmt.Errorf("the write answered %v, expected one CONSTRAINT_ERROR", st)
				}
				return errors.Join(holds(th, "th", n1, th4Node), holds(th, "th3", n3, th3Node))
			}},
			{n: "6", line: 242, title: "an empty list on the second fabric", run: func(th *goTH) error {
				st, err := write(th, n3, providers("th3"))
				if err != nil {
					return err
				}
				if err := st.Err(); err != nil {
					return err
				}
				return errors.Join(holds(th, "th", n1, th4Node), holds(th, "th3", n3))
			}},
			{n: "7", line: 258, title: "read UpdatePossible", run: func(th *goTH) error {
				v, err := th.readValue(n1, path(otar.AttrUpdatePossible))
				if err != nil {
					return err
				}
				if !v.El.Bool {
					return errors.New("UpdatePossible is false")
				}
				return nil
			}},
			{n: "8", line: 276, title: "read UpdateState", run: func(th *goTH) error {
				s, err := th.updateState(n1)
				if err != nil {
					return err
				}
				if s > uint64(otar.UpdateStateDelayedOnUserConsent) {
					return fmt.Errorf("UpdateState %d is not a defined value", s)
				}
				th.note("UpdateState %d", s)
				return nil
			}},
		},
	}
}
