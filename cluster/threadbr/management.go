// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/thread"
)

// ThreadBorderRouterManagement (0x0452) after connectedhomeip
// src/app/clusters/thread-border-router-management-server/
// ThreadBorderRouterManagementCluster.cpp at 6170af84, cited below by
// line; the port after ThreadBorderRouterManagementDelegate.h there.

// BorderRouter is the border router the servers manage and read
// (= threadborderroutermanagementdef.BorderRouter, generated,
// Declare.BorderRouter): connectedhomeip's
// ThreadBorderRouterManagementDelegate (ThreadBorderRouterManagementDelegate.h:
// 63-106) in Go, with as few methods as the rules need.
//
//   - BorderRouterName, BorderAgentID, ThreadVersion and InterfaceEnabled
//     are GetBorderRouterName, GetBorderAgentId, GetThreadVersion and
//     GetInterfaceEnabled (:69-82); an agent id that is not 16 bytes is
//     refused (ThreadBorderRouterManagementCluster.cpp:51-54).
//   - Dataset is GetDataset (:88): the active or the pending dataset, nil
//     for CHIP_ERROR_NOT_FOUND (none configured).
//   - SetActiveDataset is SetActiveDataset with its ActivateDatasetCallback
//     (:36-46, :92-93): it returns once the border router is attached to
//     the network (nil) or the activation failed, or when ctx ends — the
//     server ends it when the fail-safe expires. CommitActiveDataset and
//     RevertActiveDataset (:96, :104) run at CommissioningComplete and at
//     the fail-safe's expiry; SetPendingDataset is SetPendingDataset
//     (:106).
//   - Diagnostics is what the ThreadNetworkDiagnostics server reads: chip
//     reads its in-process OpenThread instance there
//     (DirectThreadNetworkDiagnosticsProvider.cpp).
//
// chip's GetPanChangeSupported (:66) is the feature selection here (the
// PanChange feature, [ManagementConfig].Features) and its Init (:63) the
// server's construction. An error a method returns that carries an IM
// status ([im.StatusCodeError]) answers a command with it, any other
// FAILURE — chip's StatusIB(error).mStatus.
type BorderRouter = tbrm.BorderRouter

// BorderAgentIDLen is kBorderAgentIdLength
// (ThreadBorderRouterManagementDelegate.h:31).
const BorderAgentIDLen = 16

var (
	// ErrNoBorderRouter is a configuration without a [BorderRouter].
	ErrNoBorderRouter = errors.New("threadbr: no border router")
	// ErrBorderAgentID is a border agent id that is not
	// [BorderAgentIDLen] bytes: chip's CHIP_ERROR_READ_FAILED
	// (ThreadBorderRouterManagementCluster.cpp:51-54).
	ErrBorderAgentID = errors.New("threadbr: the border agent id is not 16 bytes")
)

// ManagementConfig carries the ThreadBorderRouterManagement server's
// construction parameters.
type ManagementConfig struct {
	// BorderRouter is the border router the server manages; required.
	BorderRouter BorderRouter
	// Features is the FeatureMap selection: FeaturePanChange serves
	// SetPendingDatasetRequest (chip reads GetPanChangeSupported,
	// :122-125, :253-256).
	Features tbrm.Feature
	// IsFailSafeArmed reports whether the node's fail-safe is armed
	// (GeneralCommissioning.FailSafeArmed); SetActiveDatasetRequest needs
	// it (:212-215). Nil reads as disarmed until [ManagementServer.AttachFailSafe].
	IsFailSafeArmed func() bool
	// SetBreadcrumb writes GeneralCommissioning's Breadcrumb once an
	// activation completed (:286-289); nil writes none.
	SetBreadcrumb func(uint64)
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
	// Logger receives what chip logs (LogErrorOnFailure, :321) and the
	// refreshes that fail; slog.Default when nil. A dataset is never
	// logged.
	Logger *slog.Logger
}

// activation is a SetActiveDatasetRequest whose answer waits for the
// border router: chip's mAsyncCommandHandle, mBreadcrumb and the sequence
// number it was given (:237-243). result is nil once the request's
// exchange is gone (the handle no longer resolves, :281-284).
type activation struct {
	seq        uint32
	breadcrumb *uint64
	result     chan im.StatusCode
}

// ManagementServer implements [contract.ClusterServer] for
// ThreadBorderRouterManagement: the four commands with chip's checks and
// status codes, the six attributes read from the [BorderRouter]. The
// node wires the fail-safe ([ManagementServer.AttachFailSafe],
// [ManagementServer.OnFailSafeExpired],
// [ManagementServer.OnCommissioningComplete]), chip's
// FailSafeContext, BreadCrumbTracker and platform events (:117-136).
type ManagementServer struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, write veto and
	// SetAttributes over srv.
	spec.SelfHeld

	srv       *spec.Server
	br        BorderRouter
	panChange bool
	logger    *slog.Logger

	// mu guards armed, setBreadcrumb, seq, pending and running.
	mu            sync.Mutex
	armed         func() bool
	setBreadcrumb func(uint64)
	// seq is mSetActiveDatasetSequenceNumber (:239, :306).
	seq uint32
	// pending is the activation whose request waits for its answer.
	pending *activation
	// running cancels the port's SetActiveDataset of the latest
	// activation; the fail-safe's expiry ends it.
	running context.CancelFunc
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*ManagementServer)(nil)
	_ contract.ClusterDataVersion             = (*ManagementServer)(nil)
	_ contract.ClusterAttributeLister         = (*ManagementServer)(nil)
	_ contract.ClusterCommandLister           = (*ManagementServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*ManagementServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*ManagementServer)(nil)
	_ contract.AttributeChangeNotifier        = (*ManagementServer)(nil)
)

// NewManagement builds the ThreadBorderRouterManagement server over
// cfg.BorderRouter and reads the border router once: its name, a border
// agent id of [BorderAgentIDLen] bytes and its Thread version must be
// served ([ErrBorderAgentID], or the port's error), the interface state
// and the two dataset timestamps are read as [ManagementServer.Refresh]
// reads them. The FeatureMap is the selection (:122-125); the attribute
// list the mandatory set (:86-91).
func NewManagement(ctx context.Context, cfg ManagementConfig) (*ManagementServer, error) {
	if cfg.BorderRouter == nil {
		return nil, ErrNoBorderRouter
	}
	s := &ManagementServer{
		br: cfg.BorderRouter, panChange: cfg.Features&tbrm.FeaturePanChange != 0, logger: cfg.Logger,
		armed: cfg.IsFailSafeArmed, setBreadcrumb: cfg.SetBreadcrumb,
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	initial, err := s.read(ctx, true)
	if err != nil {
		return nil, err
	}
	handlers := map[uint32]spec.CommandHandler{
		tbrm.CmdGetActiveDatasetRequest:  s.getDataset(false),
		tbrm.CmdGetPendingDatasetRequest: s.getDataset(true),
		tbrm.CmdSetActiveDatasetRequest:  s.setActiveDataset,
	}
	if s.panChange {
		handlers[tbrm.CmdSetPendingDatasetRequest] = s.setPendingDataset
	}
	srv, err := spec.NewServer(tbrm.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Initial:     initial,
		Handlers:    handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("threadbr: %w", err)
	}
	s.Instance, s.SelfHeld, s.srv = srv.Instance, spec.NewSelfHeld(srv), srv
	return s, nil
}

// AttachFailSafe wires the node's fail-safe: whether it is armed
// (FailSafeContext::IsFailSafeArmed, :212) and the Breadcrumb an
// activation writes (BreadCrumbTracker::SetBreadCrumb, :288). The node
// calls it as the endpoint joins its topology.
func (s *ManagementServer) AttachFailSafe(armed func() bool, setBreadcrumb func(uint64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed, s.setBreadcrumb = armed, setBreadcrumb
}

// read is the attribute values the border router serves now (ReadAttribute,
// :32-84). strict, at construction, fails on a name, agent id or version
// the border router does not serve; otherwise those are left out (the
// last value read stays) and the errors returned with the rest.
func (s *ManagementServer) read(ctx context.Context, strict bool) (map[uint32]any, error) {
	values := map[uint32]any{}
	var errs []error
	values[tbrm.AttrBorderRouterName] = s.br.BorderRouterName() // :41-46
	switch id, err := s.br.BorderAgentID(ctx); {                // :47-56
	case err != nil:
		errs = append(errs, fmt.Errorf("threadbr: BorderAgentID: %w", err))
	case len(id) != BorderAgentIDLen:
		errs = append(errs, fmt.Errorf("%w: %d bytes", ErrBorderAgentID, len(id)))
	default:
		values[tbrm.AttrBorderAgentId] = slices.Clone(id)
	}
	if v, err := s.br.ThreadVersion(ctx); err != nil { // :57-58
		errs = append(errs, fmt.Errorf("threadbr: ThreadVersion: %w", err))
	} else {
		values[tbrm.AttrThreadVersion] = v
	}
	if strict && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// A border router that does not answer has its interface down
	// (ADR 0027, Consequences).
	enabled, err := s.br.InterfaceEnabled(ctx) // :59-60
	if err != nil {
		errs = append(errs, fmt.Errorf("threadbr: InterfaceEnabled: %w", err))
	}
	values[tbrm.AttrInterfaceEnabled] = enabled && err == nil
	values[tbrm.AttrActiveDatasetTimestamp] = s.timestamp(ctx, false) // :61-70
	values[tbrm.AttrPendingDatasetTimestamp] = s.timestamp(ctx, true) // :71-80
	return values, errors.Join(errs...)
}

// timestamp is the Active Timestamp sub-TLV of the active or the pending
// dataset (both read GetActiveTimestamp, :65, :75), nil for null when
// there is no dataset, no timestamp, or the border router does not
// answer.
func (s *ManagementServer) timestamp(ctx context.Context, pending bool) any {
	ds, err := s.br.Dataset(ctx, pending)
	if err != nil || ds == nil {
		return nil
	}
	ts, ok := activeTimestamp(ds)
	if !ok {
		return nil
	}
	return ts
}

// activeTimestamp reads a dataset's Active Timestamp as chip's
// OperationalDataset does: framed (Init), then GetActiveTimestamp
// (src/lib/support/ThreadOperationalDataset.cpp:166-200).
func activeTimestamp(ds []byte) (uint64, bool) {
	view, err := thread.NewDatasetView(ds)
	if err != nil {
		return 0, false
	}
	ts, err := view.ActiveTimestamp()
	return ts, err == nil
}

// Refresh reads the border router again and reports what moved. A value
// it does not serve keeps the last one read and its error is returned; a
// border router that does not answer reads InterfaceEnabled false and
// both timestamps null. The host schedules it (chip's delegate reports
// its own changes through AttributeChangeCallback, Delegate.h:48-55).
func (s *ManagementServer) Refresh(ctx context.Context) error {
	values, err := s.read(ctx, false)
	if setErr := s.srv.SetAttributes(values); setErr != nil {
		return errors.Join(err, setErr)
	}
	return err
}

// refreshAfter refreshes after a command changed the border router; a
// failure is logged, the command's answer stands.
func (s *ManagementServer) refreshAfter(ctx context.Context) {
	if err := s.Refresh(context.WithoutCancel(ctx)); err != nil {
		s.logger.Warn("threadbr.management.refresh_failed", slog.Any("err", err))
	}
}

// failSafeArmed reports the node's fail-safe.
func (s *ManagementServer) failSafeArmed() bool {
	s.mu.Lock()
	armed := s.armed
	s.mu.Unlock()
	return armed != nil && armed()
}

// statusOf is chip's StatusIB(error).mStatus for a port's error: Success
// for nil, the IM status an error carries, FAILURE otherwise.
func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	if sc, ok := errors.AsType[im.StatusCodeError](err); ok {
		return sc.MatterStatusCode()
	}
	return im.StatusFailure
}

// answer is a command's error for status, nil for Success.
func answer(status im.StatusCode, format string, args ...any) error {
	if status == im.StatusSuccess {
		return nil
	}
	return spec.Errorf(status, format, args...)
}

// getDataset is GetActiveDatasetRequest and GetPendingDatasetRequest
// (:158-207): a session that is not CASE is UNSUPPORTED_ACCESS (:151-154);
// the border router's dataset, the empty one when it has none
// (CHIP_ERROR_NOT_FOUND, :164-169), its error otherwise (:170-173).
func (s *ManagementServer) getDataset(pending bool) spec.CommandHandler {
	return func(ctx context.Context, _ any) (any, error) {
		if !isCASE(ctx) {
			return nil, spec.Errorf(im.StatusUnsupportedAccess, "threadbr: the dataset needs a CASE session")
		}
		ds, err := s.br.Dataset(ctx, pending)
		if err != nil {
			return nil, answer(statusOf(err), "threadbr: Dataset: %v", err)
		}
		if ds == nil {
			ds = []byte{}
		}
		return tbrm.DatasetResponse{Dataset: slices.Clone(ds)}, nil
	}
}

// setActiveDataset is SetActiveDatasetRequest (:208-248): a session that
// is not CASE is UNSUPPORTED_ACCESS (:151-154), a disarmed fail-safe
// FAILSAFE_REQUIRED (:212-215), a dataset chip's Init refuses
// INVALID_COMMAND (:217-221), an active dataset with an Active Timestamp
// already configured INVALID_IN_STATE (:223-230), an activation still
// waiting for its answer BUSY (:232-235). Otherwise the sequence number
// advances, the breadcrumb is kept (:237-239) and the border router
// activates the dataset (:242-243); the request is answered when it
// completes ([ManagementServer.complete]) or the fail-safe expires
// (TIMEOUT, [ManagementServer.OnFailSafeExpired]).
func (s *ManagementServer) setActiveDataset(ctx context.Context, fields any) (any, error) {
	if !isCASE(ctx) {
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "threadbr: SetActiveDatasetRequest needs a CASE session")
	}
	req, ok := spec.RequestOf[tbrm.SetActiveDatasetRequestRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "threadbr: SetActiveDatasetRequest payload")
	}
	if !s.failSafeArmed() {
		return nil, spec.Errorf(im.StatusFailsafeRequired, "threadbr: SetActiveDatasetRequest needs an armed fail-safe")
	}
	if !thread.ValidDataset(req.ActiveDataset) {
		return nil, spec.Errorf(im.StatusInvalidCommand, "threadbr: SetActiveDatasetRequest: not an operational dataset")
	}
	if cur, err := s.br.Dataset(ctx, false); err == nil && cur != nil {
		if _, configured := activeTimestamp(cur); configured {
			return nil, spec.Errorf(im.StatusInvalidInState, "threadbr: an active dataset is configured; use SetPendingDatasetRequest")
		}
	}
	s.mu.Lock()
	if s.pending != nil {
		s.mu.Unlock()
		return nil, spec.Errorf(im.StatusBusy, "threadbr: an activation is still running")
	}
	s.seq++
	a := &activation{seq: s.seq, breadcrumb: req.Breadcrumb, result: make(chan im.StatusCode, 1)}
	s.pending = a
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if s.running != nil {
		s.running()
	}
	s.running = cancel
	s.mu.Unlock()
	dataset := slices.Clone(req.ActiveDataset)
	go func() {
		defer cancel()
		s.complete(a.seq, s.br.SetActiveDataset(run, dataset))
	}()
	select {
	case status := <-a.result:
		s.refreshAfter(ctx)
		return nil, answer(status, "threadbr: SetActiveDatasetRequest: %s", status)
	case <-ctx.Done():
		s.abandon(a)
		return nil, ctx.Err()
	}
}

// complete is OnActivateDatasetComplete (:272-295): a completion of an
// earlier sequence number is ignored (:274-277), one whose request is
// gone too (:279-284); a success writes the kept breadcrumb (:286-289),
// and the request is answered with the completion's status (:292-294).
func (s *ManagementServer) complete(seq uint32, err error) {
	s.mu.Lock()
	if s.seq != seq {
		s.mu.Unlock()
		return
	}
	a := s.pending
	s.pending = nil
	setBreadcrumb := s.setBreadcrumb
	s.mu.Unlock()
	if a == nil || a.result == nil {
		return
	}
	if err == nil && a.breadcrumb != nil && setBreadcrumb != nil {
		setBreadcrumb(*a.breadcrumb)
	}
	if err != nil {
		s.logger.Info("threadbr.management.activation_failed", slog.Any("err", err))
	}
	a.result <- statusOf(err)
}

// abandon forgets the request of an activation whose exchange ended: the
// handle no longer resolves (chip's CommandHandler::Handle, :281-284), so
// the completion answers nothing and another request is not BUSY.
func (s *ManagementServer) abandon(a *activation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == a {
		s.pending = nil
	}
}

// OnFailSafeExpired is the kFailSafeTimerExpired event (:301-317): the
// waiting request is taken and the sequence number advanced first, so a
// completion the border router reports while it reverts is ignored
// (:303-307); the border router reverts the activation (:309) and the
// request is answered TIMEOUT (:311-317).
func (s *ManagementServer) OnFailSafeExpired(ctx context.Context) {
	s.mu.Lock()
	a := s.pending
	s.pending = nil
	s.seq++
	running := s.running
	s.running = nil
	s.mu.Unlock()
	if running != nil {
		running()
	}
	if err := s.br.RevertActiveDataset(ctx); err != nil {
		s.logger.Warn("threadbr.management.revert_failed", slog.Any("err", err))
	}
	if a != nil && a.result != nil {
		a.result <- im.StatusTimeout
	}
	s.refreshAfter(ctx)
}

// OnCommissioningComplete is the kCommissioningComplete event (:319-322):
// the border router commits the active dataset; a failure is logged
// (LogErrorOnFailure).
func (s *ManagementServer) OnCommissioningComplete(ctx context.Context) {
	if err := s.br.CommitActiveDataset(ctx); err != nil {
		s.logger.Warn("threadbr.management.commit_failed", slog.Any("err", err))
	}
}

// setPendingDataset is SetPendingDatasetRequest (:249-266): a session
// that is not CASE is UNSUPPORTED_ACCESS (:151-154), a server without
// PanChange UNSUPPORTED_COMMAND (:253-256), a dataset chip's Init refuses
// INVALID_COMMAND (:258-262); the border router's answer otherwise
// (:264-265). The server registers it only with PanChange, so the
// AcceptedCommandList leaves it out without (:101-104).
func (s *ManagementServer) setPendingDataset(ctx context.Context, fields any) (any, error) {
	if !isCASE(ctx) {
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "threadbr: SetPendingDatasetRequest needs a CASE session")
	}
	if !s.panChange {
		return nil, spec.Errorf(im.StatusUnsupportedCommand, "threadbr: SetPendingDatasetRequest needs PanChange")
	}
	req, ok := spec.RequestOf[tbrm.SetPendingDatasetRequestRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "threadbr: SetPendingDatasetRequest payload")
	}
	if !thread.ValidDataset(req.PendingDataset) {
		return nil, spec.Errorf(im.StatusInvalidCommand, "threadbr: SetPendingDatasetRequest: not an operational dataset")
	}
	err := s.br.SetPendingDataset(ctx, slices.Clone(req.PendingDataset))
	s.refreshAfter(ctx)
	return nil, answer(statusOf(err), "threadbr: SetPendingDataset: %v", err)
}

// isCASE reports whether a request arrived over a CASE session: chip's
// `GetSubjectDescriptor().authMode != Access::AuthMode::kCase` (:151). The
// bridge stamps the session's fabric and subject and marks a PASE session
// and a group message; a CASE request is one with a fabric and a node
// subject that is neither, the rule cluster/netinfra's isCASE and
// cluster/core's OTA provider apply.
func isCASE(ctx context.Context) bool {
	if im.IsPASEFromContext(ctx) {
		return false
	}
	if _, group := im.GroupSubjectFromContext(ctx); group {
		return false
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	node, _ := im.SubjectFromContext(ctx)
	return fabric != 0 && node != 0
}

// SetAttributes implements [spec.AttributeSetter]: every attribute is the
// border router's ([spec.ErrNotHeld] for a value other than the one
// served); [ManagementServer.Refresh] reads them again.
func (s *ManagementServer) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		cur, _ := s.srv.Value(id)
		if !sameValue(cur, values[id]) {
			return fmt.Errorf("%w: threadbr: attribute 0x%04X follows the border router (Refresh)", spec.ErrNotHeld, id)
		}
	}
	return nil
}

// sameValue compares a stored value with a host's: nil, bytes or a
// comparable scalar.
func sameValue(a, b any) bool {
	if ab, ok := a.([]byte); ok {
		bb, ok := b.([]byte)
		return ok && bytes.Equal(ab, bb)
	}
	if _, ok := b.([]byte); ok {
		return false
	}
	return a == b
}

// --- plumbing -------------------------------------------------------------

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *ManagementServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *ManagementServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead implements [contract.ClusterServer].
func (s *ManagementServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer]: every attribute is
// read-only, which the definition answers.
func (s *ManagementServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer]: a host's handler
// first ([spec.SelfHeld.Handle]), then the server's own.
func (s *ManagementServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]: the
// three mandatory requests, SetPendingDatasetRequest with PanChange
// (:93-107).
func (s *ManagementServer) MatterAcceptedCommands() []uint32 {
	return s.srv.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// DatasetResponse (:109-115).
func (s *ManagementServer) MatterGeneratedCommands() []uint32 {
	return s.srv.MatterGeneratedCommands()
}
