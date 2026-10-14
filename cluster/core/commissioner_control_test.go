// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	commctrldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

type approverFunc func(context.Context, CommissioningApprovalRequest) (bool, error)

func (f approverFunc) Approve(ctx context.Context, r CommissioningApprovalRequest) (bool, error) {
	return f(ctx, r)
}

type reverseCommissionerFunc func(context.Context, ReverseCommissioning) error

func (f reverseCommissionerFunc) CommissionNode(ctx context.Context, rc ReverseCommissioning) error {
	return f(ctx, rc)
}

// ccEvents records the CommissioningRequestResult events.
type ccEvents struct {
	mu  sync.Mutex
	got []commctrldef.CommissioningRequestResultEvent
	eps []uint16
	ch  chan struct{}
}

func newCCEvents() *ccEvents { return &ccEvents{ch: make(chan struct{}, 16)} }

func (e *ccEvents) MatterEmitEvent(endpoint uint16, clusterID, eventID uint32, data any, _ contract.EventPriority) {
	if clusterID != commctrldef.ClusterID || eventID != commctrldef.EventCommissioningRequestResult {
		return
	}
	e.mu.Lock()
	e.got = append(e.got, data.(commctrldef.CommissioningRequestResultEvent))
	e.eps = append(e.eps, endpoint)
	e.mu.Unlock()
	e.ch <- struct{}{}
}

func (e *ccEvents) next(t *testing.T) commctrldef.CommissioningRequestResultEvent {
	t.Helper()
	select {
	case <-e.ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no CommissioningRequestResult")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.got[len(e.got)-1]
}

// ccHarness is a server whose approver answers from a channel and whose
// commissioner records what it was asked.
type ccHarness struct {
	c         *CommissionerControl
	events    *ccEvents
	decisions chan bool
	approved  chan CommissioningApprovalRequest
	commissed chan ReverseCommissioning
}

func newCCHarness(t *testing.T, edit func(*CommissionerControlConfig)) *ccHarness {
	t.Helper()
	h := &ccHarness{events: newCCEvents(), decisions: make(chan bool, 4), approved: make(chan CommissioningApprovalRequest, 4), commissed: make(chan ReverseCommissioning, 4)}
	cfg := CommissionerControlConfig{
		Approver: approverFunc(func(ctx context.Context, r CommissioningApprovalRequest) (bool, error) {
			h.approved <- r
			select {
			case d := <-h.decisions:
				return d, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}),
		Commissioner: reverseCommissionerFunc(func(_ context.Context, rc ReverseCommissioning) error {
			h.commissed <- rc
			return nil
		}),
	}
	if edit != nil {
		edit(&cfg)
	}
	c, err := NewCommissionerControl(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.SetMatterEventEmitter(h.events)
	c.SetEndpoint(1)
	t.Cleanup(c.Close)
	h.c = c
	return h
}

// caseCtx is a request over CASE from nodeID on fabric 1.
func caseCtx(nodeID uint64) context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), true, 1), nodeID, nil)
}

func (h *ccHarness) request(ctx context.Context, id uint64) error {
	label := "Test Ecosystem"
	_, err := h.c.MatterInvoke(ctx, commctrldef.CmdRequestCommissioningApproval,
		commctrldef.RequestCommissioningApprovalRequest{RequestId: id, VendorId: 0xFFF1, ProductId: 0x8001, Label: &label})
	return err
}

func (h *ccHarness) commission(ctx context.Context, id uint64, timeout uint16) (any, error) {
	return h.c.MatterInvoke(ctx, commctrldef.CmdCommissionNode, &commctrldef.CommissionNodeRequest{RequestId: id, ResponseTimeoutSeconds: timeout})
}

const (
	goodRequest uint64 = 0x1234567887654321
	badRequest  uint64 = 0x1234567887654322
	clientNode  uint64 = 112233
)

// TestCommissionerControlCASEOnly: both commands over PASE, a group or a
// session without a subject are UNSUPPORTED_ACCESS (CCC:141-148,
// :196-201; TC_CCTRL_2_2.py steps 7-8).
func TestCommissionerControlCASEOnly(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, nil)
	for name, ctx := range map[string]context.Context{
		"PASE":       im.WithAuthModePASE(caseCtx(clientNode)),
		"group":      im.WithGroupSubject(caseCtx(clientNode), im.GroupSubject{GroupID: 7}),
		"no subject": im.WithFabricFilter(context.Background(), true, 1),
	} {
		if err := h.request(ctx, goodRequest); statusOf(err) != im.StatusUnsupportedAccess {
			t.Errorf("%s RequestCommissioningApproval: %v", name, err)
		}
		if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusUnsupportedAccess {
			t.Errorf("%s CommissionNode: %v", name, err)
		}
	}
	// ResponseTimeoutSeconds is checked before the session (CCC:189-201).
	for _, timeout := range []uint16{29, 121} {
		if _, err := h.commission(im.WithAuthModePASE(caseCtx(clientNode)), goodRequest, timeout); statusOf(err) != im.StatusConstraintError {
			t.Errorf("timeout %d: %v", timeout, err)
		}
	}
	if _, err := h.c.MatterInvoke(caseCtx(clientNode), commctrldef.CmdCommissionNode, "nope"); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("bad payload: %v", err)
	}
	if _, err := h.c.MatterInvoke(caseCtx(clientNode), commctrldef.CmdRequestCommissioningApproval, (*commctrldef.RequestCommissioningApprovalRequest)(nil)); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("nil payload: %v", err)
	}
}

// TestCommissionerControlApproved: the request is answered SUCCESS at
// once, the approver asked with the request, the client's node id and the
// fabric; its approval is a SUCCESS event on the server's endpoint
// (CCD:79-83). CommissionNode then refuses an unknown request id, another
// client and another fabric with FAILURE and keeps the request
// (CCD:139-145, TC_CCTRL_2_2.py step 14), and with the right id answers
// the window: a 97-byte verifier of the passcode the commissioner gets,
// 1000 iterations, a 32-byte salt, a 12-bit discriminator, 300 s
// (CCD:154-175); the commissioner runs after it and the state resets
// (CCD:200-201), so a new request is taken.
func TestCommissionerControlApproved(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, nil)
	ctx := caseCtx(clientNode)
	if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("CommissionNode before a request: %v", err)
	}
	if err := h.request(ctx, goodRequest); err != nil {
		t.Fatalf("RequestCommissioningApproval: %v", err)
	}
	asked := <-h.approved
	if asked.RequestID != goodRequest || asked.ClientNodeID != clientNode || asked.FabricIndex != 1 || asked.VendorID != 0xFFF1 ||
		asked.ProductID != 0x8001 || asked.Label == nil || *asked.Label != "Test Ecosystem" {
		t.Fatalf("approver asked %+v", asked)
	}
	// Open: another request is refused (CCD:77).
	if err := h.request(caseCtx(clientNode+1), goodRequest+1); statusOf(err) != im.StatusFailure {
		t.Fatalf("a second request while one is open: %v", err)
	}
	// Not yet approved.
	if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("CommissionNode before the approval: %v", err)
	}
	h.decisions <- true
	ev := h.events.next(t)
	if ev.RequestId != goodRequest || ev.ClientNodeId != clientNode || ev.FabricIndex != 1 || ev.StatusCode != commctrldef.StatusSuccess || h.events.eps[0] != 1 {
		t.Fatalf("event %+v on %v", ev, h.events.eps)
	}

	for name, try := range map[string]func() error{
		"unknown request id": func() error { _, err := h.commission(ctx, badRequest, 30); return err },
		"another client":     func() error { _, err := h.commission(caseCtx(clientNode+1), goodRequest, 30); return err },
		"another fabric": func() error {
			_, err := h.commission(im.WithSubject(im.WithFabricFilter(context.Background(), true, 2), clientNode, nil), goodRequest, 30)
			return err
		},
	} {
		if err := try(); statusOf(err) != im.StatusFailure {
			t.Errorf("%s: %v", name, err)
		}
	}

	resp, err := h.commission(ctx, goodRequest, 45)
	if err != nil {
		t.Fatalf("CommissionNode: %v", err)
	}
	win, ok := resp.(commctrldef.ReverseOpenCommissioningWindowResponse)
	if !ok {
		t.Fatalf("response %T", resp)
	}
	if len(win.PakePasscodeVerifier) != 97 || win.Iterations != 1000 || len(win.Salt) != 32 || win.Discriminator > 0x0FFF || win.CommissioningTimeout != 300 {
		t.Fatalf("window %+v", win)
	}
	rc := <-h.commissed
	if rc.Request.RequestID != goodRequest || rc.Discriminator != win.Discriminator || !bytes.Equal(rc.Salt, win.Salt) ||
		rc.Iterations != win.Iterations || rc.ResponseTimeout != 45*time.Second || rc.CommissioningTimeout != 300*time.Second || !setup.IsValidSetupPIN(rc.Passcode) {
		t.Fatalf("commissioner asked %+v", rc)
	}
	// The verifier is the passcode's.
	vc, err := spake2.NewVerifierContext(rc.Passcode, rc.Salt, int(rc.Iterations))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := vc.L.Bytes()
	if !bytes.Equal(win.PakePasscodeVerifier, append(vc.W0.FillBytes(make([]byte, 32)), l...)) {
		t.Fatal("the verifier is not the passcode's")
	}
	// The state resets after the commissioner: a new request is taken; the
	// used one is gone.
	deadline := time.Now().Add(5 * time.Second)
	for h.request(ctx, goodRequest+2) != nil {
		if time.Now().After(deadline) {
			t.Fatal("the state did not reset")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("a used request: %v", err)
	}
}

// TestCommissionerControlDenied: a denial and an approver error are
// FAILURE results; the request is gone.
func TestCommissionerControlDenied(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, nil)
	ctx := caseCtx(clientNode)
	if err := h.request(ctx, goodRequest); err != nil {
		t.Fatal(err)
	}
	<-h.approved
	h.decisions <- false
	if ev := h.events.next(t); ev.StatusCode != commctrldef.StatusFailure || ev.RequestId != goodRequest {
		t.Fatalf("denied: %+v", ev)
	}
	if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("CommissionNode after a denial: %v", err)
	}

	failing := newCCHarness(t, func(c *CommissionerControlConfig) {
		c.Approver = approverFunc(func(context.Context, CommissioningApprovalRequest) (bool, error) { return true, errors.New("no user") })
	})
	if err := failing.request(ctx, goodRequest); err != nil {
		t.Fatal(err)
	}
	if ev := failing.events.next(t); ev.StatusCode != commctrldef.StatusFailure {
		t.Fatalf("approver error: %+v", ev)
	}
}

// TestCommissionerControlTimeout: an approver that does not decide within
// ApprovalTimeout is a TIMEOUT result.
func TestCommissionerControlTimeout(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, func(c *CommissionerControlConfig) { c.ApprovalTimeout = 30 * time.Millisecond })
	if err := h.request(caseCtx(clientNode), goodRequest); err != nil {
		t.Fatal(err)
	}
	if ev := h.events.next(t); ev.StatusCode != commctrldef.StatusTimeout {
		t.Fatalf("timed out: %+v", ev)
	}
	if _, err := h.commission(caseCtx(clientNode), goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("CommissionNode after a timeout: %v", err)
	}
}

// TestCommissionerControlExpired: an approved request past
// ApprovalValidity is FAILURE and dropped.
func TestCommissionerControlExpired(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	h := newCCHarness(t, func(c *CommissionerControlConfig) {
		c.ApprovalValidity = time.Minute
		c.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	})
	ctx := caseCtx(clientNode)
	if err := h.request(ctx, goodRequest); err != nil {
		t.Fatal(err)
	}
	<-h.approved
	h.decisions <- true
	h.events.next(t)
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	if _, err := h.commission(ctx, goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("expired: %v", err)
	}
	// Dropped: a new request is taken.
	if err := h.request(ctx, goodRequest+1); err != nil {
		t.Fatalf("after the expiry: %v", err)
	}
}

// TestCommissionerControlClose: Close cancels an open approval without a
// result and refuses what follows.
func TestCommissionerControlClose(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, nil)
	if err := h.request(caseCtx(clientNode), goodRequest); err != nil {
		t.Fatal(err)
	}
	<-h.approved
	h.c.Close()
	select {
	case <-h.events.ch:
		t.Fatal("a result after Close")
	default:
	}
	if err := h.request(caseCtx(clientNode), goodRequest); statusOf(err) != im.StatusFailure {
		t.Fatalf("after Close: %v", err)
	}
}

// TestCommissionerControlRandFailure: a window that cannot be minted is
// FAILURE.
func TestCommissionerControlRandFailure(t *testing.T) {
	t.Parallel()
	h := newCCHarness(t, func(c *CommissionerControlConfig) { c.Rand = bytes.NewReader(nil) })
	if err := h.request(caseCtx(clientNode), goodRequest); err != nil {
		t.Fatal(err)
	}
	<-h.approved
	h.decisions <- true
	h.events.next(t)
	if _, err := h.commission(caseCtx(clientNode), goodRequest, 30); statusOf(err) != im.StatusFailure {
		t.Fatalf("no randomness: %v", err)
	}
}

// TestCommissioningApprovalRequestMatches: SetUpCodePairer.cpp:690-707 —
// a vendor or product id absent on either side does not filter.
func TestCommissioningApprovalRequestMatches(t *testing.T) {
	t.Parallel()
	r := CommissioningApprovalRequest{VendorID: 0xFFF1, ProductID: 0x8001}
	for _, tc := range []struct {
		vid, pid uint16
		want     bool
	}{
		{0xFFF1, 0x8001, true},
		{0, 0, true},
		{0xFFF1, 0, true},
		{0, 0x8001, true},
		{0x6006, 0x8001, false},
		{0xFFF1, 0x8002, false},
	} {
		if got := r.Matches(tc.vid, tc.pid); got != tc.want {
			t.Errorf("Matches(%#x, %#x) = %v", tc.vid, tc.pid, got)
		}
	}
	if !(CommissioningApprovalRequest{}).Matches(0x6006, 1) {
		t.Error("a request without ids filtered")
	}
}
