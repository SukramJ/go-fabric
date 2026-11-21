// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/contract"
)

// The provider side: the manager as the ports of node/'s OTA provider
// (cluster/core.OTAProvider) — what matter.js's
// OtaSoftwareUpdateProviderServer asks its SoftwareUpdateManager
// (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts
// at b25ed960): checkUpdateAvailable → updateExistsFor (:395-414), the
// consent hook (:382-393) and the update attempts (beginQuery …
// updateApplied, :152-379), which the manager owns under its lock with
// its queue (SoftwareUpdateManager.ts:1739-2089 at b25ed960).

// ProviderConfig is what the OTA provider that serves the manager's
// updates runs with: the ports and hooks of node/'s provider
// (node.OTAProviderConfig, field by field), in cluster/core's types, so
// that this package stays below the node facade (ADR 0026 principle 1).
type ProviderConfig struct {
	ImageSource mattercore.OTAImageSource
	Consent     mattercore.OTAConsent
	// Manager is the manager as the provider's [mattercore.OTAUpdateManager]:
	// the update attempts, the admission of a query, the apply table and
	// the transfer options of a node.
	Manager mattercore.OTAUpdateManager
	// ApplyPolicy names the apply delay ([Config].ApplyDelay) and a
	// scripted refusal (script.go) before the manager's apply table.
	//
	// Deprecated: the apply is the Manager's to decide; removable in
	// v0.26.0.
	ApplyPolicy     mattercore.OTAApplyPolicy //nolint:staticcheck // the deprecated field keeps its type for its window
	BDXMaxBlockSize uint16
	// OnStatusChange reports a provider's status to the manager's
	// deprecated [Manager.OnStatusChange].
	//
	// Deprecated: the provider reports to the Manager's attempts;
	// removable in v0.26.0.
	OnStatusChange func(peer mattercore.OTAPeer, status mattercore.OTAUpdateStatus, version uint32)
	// RebootQuery hands a restart query to the deprecated
	// [Manager.SuppressNextStartUp].
	//
	// Deprecated: the Manager judges the restart in its AdmitQuery;
	// removable in v0.26.0.
	RebootQuery func(peer mattercore.OTAPeer, sessionID uint16)
	// TransferBlockSize is the block size [Manager.TransferOptionsFor]
	// names.
	//
	// Deprecated: the Manager's BDXTransferOptionsFor names a node's
	// transfer options; removable in v0.26.0.
	TransferBlockSize func(peer mattercore.OTAPeer) uint16
	// Commands wraps the provider's command handlers with [Config]'s
	// script and exchange observer; nil without them (script.go).
	Commands *ProviderCommands
}

// ProviderConfig is the provider configuration that serves the manager's
// updates. The controller builds its provider from it
// (controller/ota_provider.go newOTAProvider).
func (m *Manager) ProviderConfig() ProviderConfig {
	return ProviderConfig{
		ImageSource:     providerPorts{m},
		Consent:         providerPorts{m},
		Manager:         providerPorts{m},
		ApplyPolicy:     applyPolicyPort{m},
		BDXMaxBlockSize: m.cfg.MaxBDXBlockSize,
		OnStatusChange:  m.providerStatus,
		RebootQuery:     func(p mattercore.OTAPeer, sessionID uint16) { m.SuppressNextStartUp(peerOf(p), sessionID) },
		TransferBlockSize: func(p mattercore.OTAPeer) uint16 {
			if b := m.TransferOptionsFor(peerOf(p)).MaxBlockSize; b != nil {
				return *b
			}
			return 0
		},
		Commands: m.providerCommands(),
	}
}

// SetBDX gives the manager the provider's BDX responder, once the
// controller has built its provider (controller/ota.go openOTA). It may
// be called at any time; the manager reads it under its lock (review II,
// R2-P6-conc-1913).
func (m *Manager) SetBDX(r *bdx.Responder) {
	m.mu.Lock()
	m.cfg.BDX = r
	m.mu.Unlock()
}

func peerOf(p mattercore.OTAPeer) Peer { return Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID} }

// providerPorts is the manager as the provider's ports. It adds no method
// to [Manager]: the provider reaches the manager through it only.
type providerPorts struct{ m *Manager }

var (
	_ mattercore.OTAUpdateManager = providerPorts{}
	_ contract.FabricScopedState  = providerPorts{}
)

// QueryImage implements mattercore.OTAImageSource: checkUpdateAvailable
// (OtaSoftwareUpdateProviderServer.ts:402-414 at b25ed960) over
// [Manager.UpdateExistsFor].
func (s providerPorts) QueryImage(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	d, ok := s.m.UpdateExistsFor(ctx, peerOf(p), req)
	if !ok {
		return mattercore.OTAImage{}, false, nil
	}
	return mattercore.OTAImage{
		FileDesignator: bdx.FileDesignator(d.Filename), SoftwareVersion: d.NewSoftwareVersion,
		SoftwareVersionString: d.NewSoftwareVersionString, ConsentRequired: d.ConsentRequired,
	}, true, nil
}

// Open implements mattercore.OTAImageSource: the stored image.
func (s providerPorts) Open(ctx context.Context, file string) (bdx.Blob, error) {
	return s.m.cfg.Images.Open(ctx, file)
}

// RequestConsent implements mattercore.OTAConsent:
// requestUserConsentForUpdate (:382-393), Unknown without a hook.
func (s providerPorts) RequestConsent(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest, img mattercore.OTAImage) (mattercore.OTAConsentState, time.Duration, error) {
	if s.m.cfg.RequestConsent == nil {
		return mattercore.OTAConsentUnknown, 0, nil
	}
	d := AvailableDetails{
		NewSoftwareVersion: img.SoftwareVersion, NewSoftwareVersionString: img.SoftwareVersionString,
		Filename: string(img.FileDesignator), ConsentRequired: img.ConsentRequired,
	}
	state, delay := s.m.cfg.RequestConsent(ctx, peerOf(p), req, d)
	return state, delay, nil
}

// BeginQuery implements mattercore.OTAUpdateManager (beginQuery,
// SoftwareUpdateManager.ts:1733-1741 at b25ed960).
func (s providerPorts) BeginQuery(p mattercore.OTAPeer) *mattercore.OTAUpdateAttempt {
	s.m.mu.Lock()
	defer s.m.unlock()
	return s.m.attempts.Begin(p)
}

// EndQuery implements mattercore.OTAUpdateManager (endQuery, :1782-1791).
func (s providerPorts) EndQuery(a *mattercore.OTAUpdateAttempt) {
	s.m.mu.Lock()
	defer s.m.unlock()
	s.m.attempts.EndQuery(a)
}

// AdmitQuery implements mattercore.OTAUpdateManager: the admission table,
// with the queue's rows — an Applying attempt linked to the queued update
// of a node that queries below its target fails the update (row 2), and
// the startUp of a restart is suppressed only while the node has a queued
// update (row 5).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1802-1881 at b25ed960
// (admitQuery).
func (s providerPorts) AdmitQuery(_ context.Context, op mattercore.OTAPeer, sessionID uint16, reportedVersion uint32) bool {
	m := s.m
	p := peerOf(op)
	transfer := m.admissionTransferOf(p)
	m.mu.Lock()
	admitted, running, restart := m.attempts.AdmitQuery(op, sessionID, transfer, nil, m.clock.Now())
	if running != nil && admitted && running.Ended() {
		if e := m.linkedEntryLocked(running); e != nil && running.Status() == mattercore.OTAUpdateApplying &&
			reportedVersion < e.TargetSoftwareVersion {
			m.log.Warn("ota.manager.apply_failed_query", slog.String("node", peerString(p)),
				slog.Uint64("version", uint64(reportedVersion)), slog.Uint64("target", uint64(e.TargetSoftwareVersion)))
			m.failUpdateLocked(e) //nolint:contextcheck // the queue's work runs on the manager's lifetime
		}
	}
	if running != nil && !admitted && !restart {
		m.log.Info("ota.manager.query_busy", slog.String("node", peerString(p)), slog.String("attempt", running.String()))
	}
	m.unlock()
	if !restart {
		return admitted
	}
	if err := m.cancelTransferForRestart(p); err != nil {
		m.log.Warn("ota.manager.restart_cancel_failed", slog.String("node", peerString(p)), slog.Any("err", err))
		return false
	}
	m.mu.Lock()
	m.attempts.EndAttempt(running)
	// The node may have lost its queued update while the transfer was
	// cancelled (:1875-1878).
	if m.indexOfLocked(p) >= 0 {
		m.startUpSuppress[p] = sessionID
	}
	m.unlock()
	return true
}

// admissionTransferOf is the node's running "ota" transfer, as an untyped
// nil when none runs.
func (m *Manager) admissionTransferOf(p Peer) admissionTransfer {
	if m.admitTransfer != nil {
		return m.admitTransfer(p)
	}
	r := m.bdxResponder()
	if r == nil {
		return nil
	}
	t := r.SessionFor(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota")
	if t == nil {
		return nil // no typed nil in the interface
	}
	return t
}

// cancelTransferForRestart cancels the node's transfer for its restart
// (:1863-1872), outside m.mu.
func (m *Manager) cancelTransferForRestart(p Peer) error {
	if m.disableTransfer != nil {
		return m.disableTransfer(p)
	}
	if r := m.bdxResponder(); r != nil {
		return r.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
	}
	return nil
}

// MayDelegateConsentTo implements mattercore.OTAUpdateManager
// ([Manager.mayDelegateConsentTo]).
func (s providerPorts) MayDelegateConsentTo(_ mattercore.OTAPeer, req provdef.QueryImageRequest) bool {
	return s.m.mayDelegateConsentTo(req)
}

// ReportAttemptStatus implements mattercore.OTAUpdateManager
// (reportAttemptStatus, :2006-2019 at b25ed960).
func (s providerPorts) ReportAttemptStatus(a *mattercore.OTAUpdateAttempt, status mattercore.OTAUpdateStatus, unless ...mattercore.OTAUpdateStatus) {
	s.m.mu.Lock()
	defer s.m.unlock()
	if a.Ended() || slices.Contains(unless, a.Status()) {
		return
	}
	s.m.reportAttemptStatusLocked(a, status, 0)
}

// OfferUpdate implements mattercore.OTAUpdateManager (offerUpdate,
// :1743-1761 at b25ed960).
func (s providerPorts) OfferUpdate(a *mattercore.OTAUpdateAttempt, token []byte, version uint32) bool {
	s.m.mu.Lock()
	defer s.m.unlock()
	return s.m.attempts.OfferUpdate(a, token, version)
}

// OfferAnswered implements mattercore.OTAUpdateManager: the attempt is
// answered, ends the node's attempts it replaces, and is linked to the
// node's queued update when it offers the update's target.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1763-1780 at b25ed960
// (offerAnswered).
func (s providerPorts) OfferAnswered(a *mattercore.OTAUpdateAttempt) {
	m := s.m
	m.mu.Lock()
	defer m.unlock()
	if a.Ended() {
		return
	}
	m.attempts.AnswerAttempt(a)
	if !m.suppress {
		m.linkAttemptLocked(a)
	}
}

// AttemptFor implements mattercore.OTAUpdateManager (attemptFor,
// :1793-1800 at b25ed960).
func (s providerPorts) AttemptFor(p mattercore.OTAPeer, token []byte) *mattercore.OTAUpdateAttempt {
	s.m.mu.Lock()
	defer s.m.unlock()
	return s.m.attempts.Find(p, token)
}

// ApplyUpdate implements mattercore.OTAUpdateManager: the apply table, and
// the status it records — Applying with the delay, or Cancelled — moves
// the queued update linked to the attempt.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1921-1943 at b25ed960
// (applyUpdate).
func (s providerPorts) ApplyUpdate(_ context.Context, p mattercore.OTAPeer, token []byte, newVersion uint32, applyDelay time.Duration) bool {
	m := s.m
	m.mu.Lock()
	defer m.unlock()
	a, allowed := m.attempts.ApplyUpdate(p, token, newVersion)
	switch {
	case allowed:
		m.reportAttemptStatusLocked(a, mattercore.OTAUpdateApplying, applyDelay) //nolint:contextcheck // the queue's work runs on the manager's lifetime
	case a != nil:
		m.reportAttemptStatusLocked(a, mattercore.OTAUpdateCancelled, 0) //nolint:contextcheck // the queue's work runs on the manager's lifetime
	default:
		m.log.Info("ota.manager.apply_version_zero_unknown_token", slog.String("node", peerString(peerOf(p))))
	}
	return allowed
}

// UpdateApplied implements mattercore.OTAUpdateManager (updateApplied,
// :1958-1968 at b25ed960).
func (s providerPorts) UpdateApplied(p mattercore.OTAPeer, token []byte, softwareVersion uint32) {
	s.m.mu.Lock()
	defer s.m.unlock()
	s.m.updateAppliedLocked(p, token, softwareVersion)
}

// BDXTransferOptionsFor implements mattercore.OTAUpdateManager: the
// options [Manager.TransferOptionsFor] names (bdxTransferOptionsFor,
// :1530-1536 at b25ed960).
func (s providerPorts) BDXTransferOptionsFor(p mattercore.OTAPeer) bdx.ScopeConfig {
	o := s.m.TransferOptionsFor(peerOf(p))
	out := bdx.ScopeConfig{AdditionalMrpDelay: o.AdditionalMrpDelay}
	if o.MaxBlockSize != nil {
		out.MaxBlockSize = *o.MaxBlockSize
	}
	return out
}

// MatterDropFabric implements contract.FabricScopedState: the removed
// fabric's attempts end — the queued updates that followed them are reset
// — and its nodes lose their transfer scope (owner decision 167).
func (s providerPorts) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	m := s.m
	m.mu.Lock()
	peers := m.attempts.DropFabric(fabricIndex)
	m.unlock()
	for _, p := range peers {
		m.closeTransfer(peerOf(p))
	}
	return nil
}

// MatterHeldFabrics implements contract.FabricScopedState: the fabrics of
// the running attempts.
func (s providerPorts) MatterHeldFabrics() []uint8 {
	s.m.mu.Lock()
	defer s.m.unlock()
	return s.m.attempts.Fabrics()
}

// applyPolicyPort is the manager as the provider's deprecated
// mattercore.OTAApplyPolicy: the apply delay and the scripted refusal.
type applyPolicyPort struct{ m *Manager }

// ApplyUpdate implements mattercore.OTAApplyPolicy: Proceed after the
// apply delay, in whole seconds rounded up and at most a day
// (OtaSoftwareUpdateProviderServer.ts:346-351 at b25ed960), which the
// manager's apply table then decides; a scripted ApplyUpdateRequest
// (script.go) replaces the delay, or refuses as the table refuses an
// update without its apply right.
func (s applyPolicyPort) ApplyUpdate(ctx context.Context, p mattercore.OTAPeer, _ provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateActionEnum, time.Duration, error) {
	configured := s.m.cfg.ApplyDelay
	if o, ok := applyOverrideFrom(ctx); ok {
		if o.refuse {
			return provdef.ApplyUpdateActionDiscontinue, applyRefusalDelay, nil
		}
		if o.set {
			configured = o.delay
		}
	}
	delay := wholeSeconds(configured)
	s.m.mu.Lock()
	s.m.applyDelays[peerOf(p)] = delay
	s.m.mu.Unlock()
	return provdef.ApplyUpdateActionProceed, delay, nil
}

// providerStatus takes a status of the deprecated
// [ProviderConfig].OnStatusChange.
func (m *Manager) providerStatus(p mattercore.OTAPeer, status mattercore.OTAUpdateStatus, version uint32) {
	peer := peerOf(p)
	var delay time.Duration
	if status == mattercore.OTAUpdateApplying {
		m.mu.Lock()
		delay = m.applyDelays[peer]
		m.mu.Unlock()
	}
	var v *uint32
	if version != 0 {
		v = &version
	}
	m.OnStatusChange(peer, status, v, delay)
}
