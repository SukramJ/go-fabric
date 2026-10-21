// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commctrldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// The window a CommissionNode answer asks the client to open — the
// parameters connectedhomeip's fabric-sync bridge hands out
// (examples/fabric-sync/bridge/CommissionerControlDelegate.cpp:33-35 and
// GetCommissioningWindowParams :154-175 at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20): 1000 PBKDF iterations, a
// 300-second window, a random salt of kSpake2p_Max_PBKDF_Salt_Length
// (32) bytes.
const (
	CommissionerControlIterations    uint32 = 1000
	CommissionerControlWindowTimeout uint16 = 300
	commissionerControlSaltLength           = 32
)

// CommissioningApprovalRequest is one RequestCommissioningApproval —
// connectedhomeip's CommissioningApprovalRequest (src/app/clusters/
// commissioner-control-server/Delegate.h:30-38): the command's fields and
// the CASE subject and fabric it came from.
type CommissioningApprovalRequest struct {
	RequestID    uint64
	VendorID     uint16
	ProductID    uint16
	ClientNodeID uint64
	FabricIndex  uint8
	// Label is the request's optional label, at most 64 bytes.
	Label *string
}

// Matches reports whether a commissionable node advertising vendorID and
// productID (0 when its advertisement states none) may be the node the
// request asked to have commissioned — connectedhomeip's
// SetUpCodePairer::NodeMatchesCurrentFilter (src/controller/
// SetUpCodePairer.cpp:690-707), which the fabric-sync bridge's
// commissioner applies to the request's vendor and product
// (examples/fabric-sync/admin/FabricAdmin.cpp:88-113): an id absent on
// either side does not filter.
func (r CommissioningApprovalRequest) Matches(vendorID, productID uint16) bool {
	const notAvailable = 0 // SetUpCodePairer.h:278-279 kNotAvailable
	if r.VendorID != notAvailable && vendorID != notAvailable && r.VendorID != vendorID {
		return false
	}
	return r.ProductID == notAvailable || productID == notAvailable || r.ProductID == productID
}

// CommissioningApprover is the host's approval of a request — the
// "method indicated by the manufacturer" that connectedhomeip's CCTRL
// cases wait for (src/python_testing/TC_CCTRL_2_2.py:148, :236-238). It is
// asked asynchronously, after the command was answered SUCCESS.
type CommissioningApprover interface {
	// Approve decides req. An error is reported as a FAILURE result.
	Approve(ctx context.Context, req CommissioningApprovalRequest) (approved bool, err error)
}

// ReverseCommissioning is what the host's commissioner needs to
// commission the client's node through the window the client opens:
// connectedhomeip's CommissioningWindowPasscodeParams and the request's
// vendor and product (CommissionerControlDelegate.cpp:186-193). Passcode
// is the one the window's verifier was derived from; it is never sent.
type ReverseCommissioning struct {
	Request              CommissioningApprovalRequest
	Passcode             uint32
	Discriminator        uint16
	Iterations           uint32
	Salt                 []byte
	CommissioningTimeout time.Duration
	// ResponseTimeout is CommissionNode's ResponseTimeoutSeconds: how long
	// the client was given to open the window.
	ResponseTimeout time.Duration
}

// ReverseCommissioner commissions the client's node — the fabric-sync
// bridge's FabricAdminDelegate::CommissionRemoteBridge
// (examples/fabric-sync/admin/FabricAdmin.cpp:79-128): discover the node by
// rc.Discriminator (filtering with rc.Request.Matches), open PASE with
// rc.Passcode and commission it onto the host's fabric. It runs after the
// ReverseOpenCommissioningWindow answer has left; its ctx ends with the
// window.
type ReverseCommissioner interface {
	CommissionNode(ctx context.Context, rc ReverseCommissioning) error
}

// CommissionerControlConfig configures a [CommissionerControl] server.
type CommissionerControlConfig struct {
	// Approver approves requests. Required.
	Approver CommissioningApprover
	// Commissioner commissions an approved client's node. Required.
	Commissioner ReverseCommissioner
	// SupportedDeviceCategories is the SupportedDeviceCategories
	// attribute; fabric-sync sets FabricSynchronization
	// (CommissionerControlDelegate.cpp:240-245).
	SupportedDeviceCategories commctrldef.SupportedDeviceCategoryBitmap
	// ApprovalTimeout bounds the Approver's decision; past it the result
	// is TIMEOUT. 0 waits for the Approver, as connectedhomeip's delegate
	// (which decides at once) never times out.
	ApprovalTimeout time.Duration
	// ApprovalValidity is how long an approved request stays valid for
	// CommissionNode; past it CommissionNode answers FAILURE. 0 keeps it
	// until it is used or replaced, as connectedhomeip's delegate does
	// (kWaitCommissionNodeRequest, CommissionerControlDelegate.cpp:121).
	ApprovalValidity time.Duration
	// Rand mints the passcode, discriminator and salt; nil is
	// crypto/rand.
	Rand io.Reader
	// Clock is the clock of ApprovalValidity. Nil is [clock.System].
	Clock clock.Clock
	// Logger receives the server's log; nil discards it.
	Logger *slog.Logger
}

// ccStep is the delegate's state — CommissionerControlDelegate::Step
// (CommissionerControlDelegate.h:50-58), with the asynchronous approval
// the reference delegate does not need between idle and waiting.
type ccStep uint8

const (
	ccIdle ccStep = iota
	ccApproving
	ccWaitCommissionNode
	ccStartCommissionNode
)

// CommissionerControl is a CommissionerControl (0x0751) server: the
// generated server with connectedhomeip's command handling, the
// reference of ADR 0020 stage 6 — matter.js has none
// (packages/node/src/behaviors/commissioner-control/
// CommissionerControlServer.ts:14, an empty class at 0ceb202d).
//
// Mirrors connectedhomeip CommissionerControlCluster.cpp at the harness
// pin 6170af8461b10b1766044122ac83332c6d00ab20 (CCC below) and the
// fabric-sync bridge's delegate, CommissionerControlDelegate.cpp (CCD):
//
//   - RequestCommissioningApproval over anything but CASE is
//     UNSUPPORTED_ACCESS (CCC:141-148); otherwise the request is taken
//     when no other is open (CCD:77, FAILURE when one is) and answered
//     SUCCESS (CCC:175), the Approver asked asynchronously and its
//     decision reported in a CommissioningRequestResult event with the
//     request id, the client's node id, the fabric index and SUCCESS,
//     FAILURE or TIMEOUT (CCD:79-83, :117).
//   - CommissionNode checks ResponseTimeoutSeconds (30 to 120,
//     CONSTRAINT_ERROR, CCC:189-194 — the generated decoder does), then
//     CASE (UNSUPPORTED_ACCESS, CCC:196-201), then that an approved
//     request waits, from the same client, with the same id (FAILURE,
//     CCD:139-145, the statuses TC_CCTRL_2_2.py:185-191 and :252-259
//     expect); it answers ReverseOpenCommissioningWindow with a window
//     the server minted (CCD:154-175) and then hands the commissioning to
//     the ReverseCommissioner (CCC:217-226), the state reset after it
//     (CCD:200-201).
type CommissionerControl struct {
	*spec.Server

	cfg  CommissionerControlConfig
	log  *slog.Logger
	dv   cluster.DataVersionTracker
	rand io.Reader
	now  func() time.Time

	// mu guards approvedAt, closed, endpoint, request, step and wg.
	mu         sync.Mutex
	endpoint   uint16
	step       ccStep
	request    CommissioningApprovalRequest
	approvedAt time.Time
	closed     bool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

var (
	errNilApprover     = errors.New("core: an approver is required")
	errNilCommissioner = errors.New("core: a commissioner is required")
)

// NewCommissionerControl builds a CommissionerControl server.
func NewCommissionerControl(cfg CommissionerControlConfig) (*CommissionerControl, error) {
	if cfg.Approver == nil {
		return nil, fmt.Errorf("core: CommissionerControl: %w", errNilApprover)
	}
	if cfg.Commissioner == nil {
		return nil, fmt.Errorf("core: CommissionerControl: %w", errNilCommissioner)
	}
	c := &CommissionerControl{cfg: cfg, log: cfg.Logger, rand: cfg.Rand, now: clock.Or(cfg.Clock).Now}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	if c.rand == nil {
		c.rand = rand.Reader
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	handlers := map[uint32]spec.CommandHandler{
		commctrldef.CmdRequestCommissioningApproval: c.requestCommissioningApproval,
		commctrldef.CmdCommissionNode:               c.commissionNode,
	}
	srv, err := spec.NewServer(commctrldef.Definition, spec.Options{
		Commands: []uint32{commctrldef.CmdRequestCommissioningApproval, commctrldef.CmdCommissionNode},
	}, spec.ServerConfig{
		Handlers:    handlers,
		DataVersion: &c.dv,
		Initial:     map[uint32]any{commctrldef.AttrSupportedDeviceCategories: cfg.SupportedDeviceCategories},
	})
	if err != nil {
		return nil, fmt.Errorf("core: CommissionerControl: %w", err)
	}
	c.Server = srv
	return c, nil
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it when it mounts the server.
func (c *CommissionerControl) SetEndpoint(endpoint uint16) {
	c.mu.Lock()
	c.endpoint = endpoint
	c.mu.Unlock()
}

// Close cancels a running approval or commissioning and waits for it.
func (c *CommissionerControl) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.wg.Wait()
}

// caseSubject is CCC GetNodeId (:30-39): the CASE subject of the request,
// 0 for PASE or a group.
func caseSubject(ctx context.Context) uint64 {
	if im.IsPASEFromContext(ctx) {
		return 0
	}
	if _, group := im.GroupSubjectFromContext(ctx); group {
		return 0
	}
	nodeID, _ := im.SubjectFromContext(ctx)
	return nodeID
}

func requestFields[T any](fields any) (T, bool) {
	switch v := fields.(type) {
	case *T:
		if v != nil {
			return *v, true
		}
	case T:
		return v, true
	}
	var zero T
	return zero, false
}

// requestCommissioningApproval is CCC HandleRequestCommissioningApproval
// (:132-176) with CCD HandleCommissioningApprovalRequest (:72-130).
func (c *CommissionerControl) requestCommissioningApproval(ctx context.Context, fields any) (any, error) {
	req, ok := requestFields[commctrldef.RequestCommissioningApprovalRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CommissionerControl: RequestCommissioningApproval payload %T", fields)
	}
	clientNodeID := caseSubject(ctx)
	if clientNodeID == 0 {
		c.log.Warn("matter.commissioner_control.approval_not_case",
			slog.String("message", "Commissioning approval request not executed via CASE session, failing with UNSUPPORTED_ACCESS"))
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "CommissionerControl: RequestCommissioningApproval needs a CASE session")
	}
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	request := CommissioningApprovalRequest{
		RequestID: req.RequestId, VendorID: req.VendorId, ProductID: req.ProductId,
		ClientNodeID: clientNodeID, FabricIndex: fabricIndex, Label: req.Label,
	}
	c.mu.Lock()
	if c.closed || c.step != ccIdle {
		c.mu.Unlock()
		return nil, spec.Errorf(im.StatusFailure, "CommissionerControl: a commissioning request is open already")
	}
	c.step, c.request = ccApproving, request
	c.wg.Add(1)
	c.mu.Unlock()
	c.log.Info("matter.commissioner_control.approval_requested", slog.Uint64("request_id", request.RequestID),
		slog.Uint64("client_node_id", request.ClientNodeID), slog.Int("fabric_index", int(request.FabricIndex)),
		slog.Int("vendor_id", int(request.VendorID)), slog.Int("product_id", int(request.ProductID)))
	go c.approve(request) //nolint:contextcheck // the approval outlives the request; the server's own context bounds it
	return nil, nil
}

// approve asks the Approver and reports its decision.
func (c *CommissionerControl) approve(request CommissioningApprovalRequest) {
	defer c.wg.Done()
	ctx := c.ctx
	if c.cfg.ApprovalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.ApprovalTimeout)
		defer cancel()
	}
	type decision struct {
		approved bool
		err      error
	}
	done := make(chan decision, 1)
	go func() {
		approved, err := c.cfg.Approver.Approve(ctx, request)
		done <- decision{approved, err}
	}()
	status := commctrldef.StatusFailure
	select {
	case d := <-done:
		switch {
		case d.err != nil:
			c.log.Warn("matter.commissioner_control.approval_failed", slog.Uint64("request_id", request.RequestID), slog.String("err", d.err.Error()))
		case d.approved:
			status = commctrldef.StatusSuccess
		}
	case <-ctx.Done():
		if c.ctx.Err() != nil {
			c.reset()
			return
		}
		status = commctrldef.StatusTimeout
	}
	c.mu.Lock()
	if status == commctrldef.StatusSuccess {
		c.step, c.approvedAt = ccWaitCommissionNode, c.now()
	} else {
		c.step, c.request = ccIdle, CommissioningApprovalRequest{}
	}
	endpoint := c.endpoint
	c.mu.Unlock()
	c.log.Info("matter.commissioner_control.approval_result", slog.Uint64("request_id", request.RequestID), slog.Int("status", int(status)))
	if err := c.Emit(endpoint, commctrldef.EventCommissioningRequestResult, commctrldef.CommissioningRequestResultEvent{
		RequestId: request.RequestID, ClientNodeId: request.ClientNodeID, StatusCode: status, FabricIndex: request.FabricIndex,
	}); err != nil {
		c.log.Warn("matter.commissioner_control.event_failed", slog.String("err", err.Error()))
	}
}

// reset is CCD ResetDelegateState (:47-70).
func (c *CommissionerControl) reset() {
	c.mu.Lock()
	c.step, c.request, c.approvedAt = ccIdle, CommissioningApprovalRequest{}, time.Time{}
	c.mu.Unlock()
}

// commissionNode is CCC HandleCommissionNode (:178-236).
func (c *CommissionerControl) commissionNode(ctx context.Context, fields any) (any, error) {
	req, ok := requestFields[commctrldef.CommissionNodeRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "CommissionerControl: CommissionNode payload %T", fields)
	}
	// The generated decoder holds ResponseTimeoutSeconds to 30-120
	// (CCC:189-194); a payload built in process is checked here.
	if req.ResponseTimeoutSeconds < 30 || req.ResponseTimeoutSeconds > 120 {
		return nil, spec.Errorf(im.StatusConstraintError, "CommissionerControl: invalid responseTimeoutSeconds %d", req.ResponseTimeoutSeconds)
	}
	clientNodeID := caseSubject(ctx)
	if clientNodeID == 0 {
		c.log.Warn("matter.commissioner_control.commission_not_case",
			slog.String("message", "Commission node request not executed via CASE session, failing with UNSUPPORTED_ACCESS"))
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "CommissionerControl: CommissionNode needs a CASE session")
	}
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	request, err := c.validateCommissionNode(clientNodeID, fabricIndex, req.RequestId)
	if err != nil {
		return nil, err
	}
	rc, resp, err := c.windowParams(request, time.Duration(req.ResponseTimeoutSeconds)*time.Second)
	if err != nil {
		c.reset()
		return nil, spec.Errorf(im.StatusFailure, "CommissionerControl: window parameters: %v", err)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, spec.Errorf(im.StatusFailure, "CommissionerControl: closed")
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go c.commission(rc) //nolint:contextcheck // the commissioning follows the response; the server's own context bounds it
	return resp, nil
}

// validateCommissionNode is CCD ValidateCommissionNodeCommand (:132-152):
// an approved request waits, from the same client, with the same id. The
// client is compared with its fabric — connectedhomeip compares the node
// id alone and marks the move to a ScopedNodeId as TODO(#35627)
// (CommissionerControlDelegate.h:40). A request past ApprovalValidity is
// dropped.
func (c *CommissionerControl) validateCommissionNode(clientNodeID uint64, fabricIndex uint8, requestID uint64) (CommissioningApprovalRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.step != ccWaitCommissionNode {
		return CommissioningApprovalRequest{}, spec.Errorf(im.StatusFailure, "CommissionerControl: no approved request waits")
	}
	if c.request.ClientNodeID != clientNodeID || c.request.FabricIndex != fabricIndex {
		return CommissioningApprovalRequest{}, spec.Errorf(im.StatusFailure, "CommissionerControl: CommissionNode from another client than the request")
	}
	if c.request.RequestID != requestID {
		return CommissioningApprovalRequest{}, spec.Errorf(im.StatusFailure, "CommissionerControl: unknown request id 0x%016X", requestID)
	}
	if c.cfg.ApprovalValidity > 0 && c.now().Sub(c.approvedAt) > c.cfg.ApprovalValidity {
		c.step, c.request, c.approvedAt = ccIdle, CommissioningApprovalRequest{}, time.Time{}
		return CommissioningApprovalRequest{}, spec.Errorf(im.StatusFailure, "CommissionerControl: the approval of request 0x%016X expired", requestID)
	}
	c.step = ccStartCommissionNode
	return c.request, nil
}

// windowParams is CCD GetCommissioningWindowParams (:154-175): the
// iterations and window timeout, a random salt, and the PAKE verifier of
// a passcode — minted here at random, with a random discriminator, where
// the example bridge uses the fixed test values 20202021 and 3840
// (:33-36).
func (c *CommissionerControl) windowParams(request CommissioningApprovalRequest, responseTimeout time.Duration) (ReverseCommissioning, commctrldef.ReverseOpenCommissioningWindowResponse, error) {
	salt := make([]byte, commissionerControlSaltLength)
	if _, err := io.ReadFull(c.rand, salt); err != nil {
		return ReverseCommissioning{}, commctrldef.ReverseOpenCommissioningWindowResponse{}, err
	}
	passcode, discriminator, err := c.mintPasscode()
	if err != nil {
		return ReverseCommissioning{}, commctrldef.ReverseOpenCommissioningWindowResponse{}, err
	}
	vc, err := spake2.NewVerifierContext(passcode, salt, int(CommissionerControlIterations))
	if err != nil {
		return ReverseCommissioning{}, commctrldef.ReverseOpenCommissioningWindowResponse{}, err
	}
	l, err := vc.L.Bytes()
	if err != nil {
		return ReverseCommissioning{}, commctrldef.ReverseOpenCommissioningWindowResponse{}, err
	}
	verifier := append(vc.W0.FillBytes(make([]byte, spake2.VerifierW0Size)), l...)
	rc := ReverseCommissioning{
		Request: request, Passcode: passcode, Discriminator: discriminator, Iterations: CommissionerControlIterations,
		Salt: salt, CommissioningTimeout: time.Duration(CommissionerControlWindowTimeout) * time.Second, ResponseTimeout: responseTimeout,
	}
	return rc, commctrldef.ReverseOpenCommissioningWindowResponse{
		CommissioningTimeout: CommissionerControlWindowTimeout, PakePasscodeVerifier: verifier,
		Discriminator: discriminator, Iterations: CommissionerControlIterations, Salt: salt,
	}, nil
}

// mintPasscode draws a valid setup passcode (setup.IsValidSetupPIN) and a
// 12-bit discriminator.
func (c *CommissionerControl) mintPasscode() (passcode uint32, discriminator uint16, err error) {
	var b [6]byte
	for {
		if _, err := io.ReadFull(c.rand, b[:]); err != nil {
			return 0, 0, err
		}
		passcode = binary.BigEndian.Uint32(b[:4]) % 100000000
		if setup.IsValidSetupPIN(passcode) {
			return passcode, binary.BigEndian.Uint16(b[4:]) & 0x0FFF, nil
		}
	}
}

// commission is CCD HandleCommissionNode (:177-204), scheduled after the
// response (CCC:217-226): the host commissions the client's node; the
// state is reset whatever the outcome.
func (c *CommissionerControl) commission(rc ReverseCommissioning) {
	defer c.wg.Done()
	defer c.reset()
	ctx, cancel := context.WithTimeout(c.ctx, rc.CommissioningTimeout)
	defer cancel()
	if err := c.cfg.Commissioner.CommissionNode(ctx, rc); err != nil {
		c.log.Warn("matter.commissioner_control.commission_failed", slog.Uint64("request_id", rc.Request.RequestID), slog.String("err", err.Error()),
			slog.String("message", "Failed to reverse commission the fabric bridge"))
		return
	}
	c.log.Info("matter.commissioner_control.commissioned", slog.Uint64("request_id", rc.Request.RequestID))
}
