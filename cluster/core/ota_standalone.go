// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/contract"
)

// OTAUpdateManager is what the [OTAProvider] asks of the update manager
// that owns its requestors' [OTAUpdateAttempt]s: exactly the calls
// matter.js's OtaSoftwareUpdateProviderServer makes of its
// SoftwareUpdateManager (beginQuery :153, endQuery :157, admitQuery :189,
// mayDelegateConsentTo :201, reportAttemptStatus :203, :226, :249, :419,
// offerUpdate :246, bdxTransferOptionsFor :260, offerAnswered :305,
// attemptFor :332, applyUpdate :352, updateApplied :379 — packages/node/src/behaviors/
// ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts at b25ed960).
//
// The manager decides; the provider asks. Each method takes the manager's
// own lock, and a manager may change its own state — a queued update —
// in the same step as the attempt. [NewOTAStandaloneManager] is the
// manager without a queue a provider runs with by default;
// controller/softwareupdate's Manager is the one with a queue. A manager
// that also implements [contract.FabricScopedState] is handed the
// provider's fabric removal.
type OTAUpdateManager interface {
	// BeginQuery starts the attempt of a QueryImage that just arrived
	// (SoftwareUpdateManager.ts:1733-1741).
	BeginQuery(peer OTAPeer) *OTAUpdateAttempt
	// EndQuery ends the query: its attempt ends too, unless the provider
	// answered the query with its offer (:1782-1791).
	EndQuery(a *OTAUpdateAttempt)
	// AdmitQuery decides whether the query that arrived on the session of
	// sessionID, from a requestor that reports reportedVersion, may get an
	// offer while the requestor has an attempt with one; false answers
	// Busy (:1802-1881).
	AdmitQuery(ctx context.Context, peer OTAPeer, sessionID uint16, reportedVersion uint32) bool
	// MayDelegateConsentTo reports whether the requestor may obtain the
	// user's consent itself (:631-647).
	MayDelegateConsentTo(peer OTAPeer, req provdef.QueryImageRequest) bool
	// ReportAttemptStatus records a status of the attempt, other than
	// Done (:2006-2019), and reports whether it did: an ended attempt
	// takes none, and a status in unless leaves the attempt as it is. The
	// check and the report are one step under the manager's lock — the
	// transfer's closed listener checks the status
	// (OtaSoftwareUpdateProviderServer.ts:285-290) and reports through
	// endpoint.act (:417-419). The provider's [OTAProviderConfig].OnAttemptStatus
	// learns a status that was recorded.
	ReportAttemptStatus(a *OTAUpdateAttempt, status OTAUpdateStatus, unless ...OTAUpdateStatus) bool
	// OfferUpdate records the offer of the query, after the provider's
	// last wait; false when the attempt ended meanwhile, which the
	// provider answers NotAvailable (:1743-1761).
	OfferUpdate(a *OTAUpdateAttempt, token []byte, version uint32) bool
	// OfferAnswered records that the provider answered the query with its
	// offer (:1763-1780).
	OfferAnswered(a *OTAUpdateAttempt)
	// AttemptFor is the requestor's running attempt with an offer under
	// token, nil when none (:1793-1800).
	AttemptFor(peer OTAPeer, token []byte) *OTAUpdateAttempt
	// ApplyUpdate decides an ApplyUpdateRequest by the apply table and
	// records the outcome, Applying with applyDelay or Cancelled; true
	// answers Proceed, false Discontinue (:1897-1943).
	ApplyUpdate(ctx context.Context, peer OTAPeer, token []byte, newVersion uint32, applyDelay time.Duration) bool
	// UpdateApplied records the requestor's NotifyUpdateApplied
	// (:1958-2004).
	UpdateApplied(peer OTAPeer, token []byte, softwareVersion uint32)
	// BDXTransferOptionsFor is how BDX carries the requestor's image: the
	// block size and MRP margin of its scope (:1530-1536).
	BDXTransferOptionsFor(peer OTAPeer) bdx.ScopeConfig
}

// otaStandaloneManager is the [OTAUpdateManager] without a queue.
type otaStandaloneManager struct {
	mu  sync.Mutex
	set *OTAUpdateAttempts

	bdx         *bdx.Responder
	maxBlock    uint16
	blockFor    func(OTAPeer) uint16
	activity    func(uint16) (time.Time, bool)
	delegate    bool
	onStatus    func(OTAPeer, OTAUpdateStatus, uint32)
	rebootQuery func(OTAPeer, uint16)
	clock       clock.Clock
	logger      *slog.Logger
}

var (
	_ OTAUpdateManager           = (*otaStandaloneManager)(nil)
	_ contract.FabricScopedState = (*otaStandaloneManager)(nil)
)

// NewOTAStandaloneManager returns the manager a provider without an update
// queue runs with — [OTAProviderConfig].Manager left nil: the attempts of
// the requestors and the rules matter.js decides on them alone
// ([OTAUpdateAttempts]), with no update queued for any requestor. So a host
// with its own [OTAImageSource] and no manager still answers by the Matter
// rules: one answered offer per requestor, the admission of a new query,
// the apply table and the end of an attempt at NotifyUpdateApplied.
// matter.js has no provider without its SoftwareUpdateManager
// (OtaSoftwareUpdateProviderServer.ts:81-82 at b25ed960).
//
// It reads cfg's BDX, BDXMaxBlockSize, DelegateConsentToRequestor, Clock
// and Logger, and honours a
// manager-less provider's hooks (all four deprecated): OnStatusChange learns each
// status an attempt takes (and Done at each NotifyUpdateApplied),
// RebootQuery each query admitted for the requestor's restart (the host's
// queue decides what it suppresses), TransferBlockSize the block size per
// requestor, and SessionActivity the sessions' activity the admission
// compares. It also implements [contract.FabricScopedState]: a removed
// fabric's attempts end and its requestors lose their transfer scope.
func NewOTAStandaloneManager(cfg OTAProviderConfig) OTAUpdateManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clk := clock.Or(cfg.Clock)
	return &otaStandaloneManager{
		set: NewOTAUpdateAttempts(clk), bdx: cfg.BDX, maxBlock: cfg.BDXMaxBlockSize,
		blockFor: cfg.TransferBlockSize, activity: cfg.SessionActivity,
		delegate: cfg.DelegateConsentToRequestor, onStatus: cfg.OnStatusChange, rebootQuery: cfg.RebootQuery,
		clock: clk, logger: logger,
	}
}

// unlock releases m.mu and then tells the status observer of each status
// the step recorded. The ended attempts have no queue to reset.
func (m *otaStandaloneManager) unlock(reports []otaStatusReport) {
	m.set.TakeEnded()
	m.mu.Unlock()
	for _, r := range reports {
		m.logger.Info("ota.provider.status", slog.Uint64("node_id", r.peer.NodeID), slog.String("state", r.status.String()),
			slog.String("previous", r.previous.String()))
		if m.onStatus != nil {
			m.onStatus(r.peer, r.status, r.version)
		}
	}
}

// otaStatusReport is a status an attempt took, told after the lock.
type otaStatusReport struct {
	peer             OTAPeer
	status, previous OTAUpdateStatus
	version          uint32
}

func reportOf(a *OTAUpdateAttempt, previous OTAUpdateStatus) otaStatusReport {
	v, _ := a.Version()
	return otaStatusReport{peer: a.Peer(), status: a.Status(), previous: previous, version: v}
}

func (m *otaStandaloneManager) setStatus(a *OTAUpdateAttempt, status OTAUpdateStatus, delay time.Duration) otaStatusReport {
	previous := a.Status()
	m.set.SetStatus(a, status, delay)
	return reportOf(a, previous)
}

// BeginQuery implements [OTAUpdateManager].
func (m *otaStandaloneManager) BeginQuery(peer OTAPeer) *OTAUpdateAttempt {
	m.mu.Lock()
	defer m.unlock(nil)
	return m.set.Begin(peer)
}

// EndQuery implements [OTAUpdateManager].
func (m *otaStandaloneManager) EndQuery(a *OTAUpdateAttempt) {
	m.mu.Lock()
	defer m.unlock(nil)
	m.set.EndQuery(a)
}

// AdmitQuery implements [OTAUpdateManager]: matter.js's admission table
// without row 2, which needs a queued update.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1819-1881 at b25ed960
// (admitQuery).
func (m *otaStandaloneManager) AdmitQuery(_ context.Context, peer OTAPeer, sessionID uint16, _ uint32) bool {
	m.mu.Lock()
	admitted, running, restart := m.set.AdmitQuery(peer, sessionID, transferOf(m.bdx, peer), m.activity, m.clock.Now())
	if running != nil && !restart && !admitted {
		m.logger.Info("ota.provider.busy", slog.Uint64("node_id", peer.NodeID), slog.String("attempt", running.String()))
	}
	m.unlock(nil)
	if !restart {
		return admitted
	}
	if err := m.bdx.DisablePeerForScope(peer.bdx(), otaScope, true); err != nil {
		m.logger.Warn("ota.provider.reboot_cancel_failed", slog.Any("err", err))
		return false
	}
	m.mu.Lock()
	m.set.EndAttempt(running)
	m.unlock(nil)
	if m.rebootQuery != nil {
		m.rebootQuery(peer, sessionID)
	}
	return true
}

// transferOf is the requestor's running "ota" transfer, as an untyped nil
// when none runs.
func transferOf(r *bdx.Responder, peer OTAPeer) interface {
	Peer() bdx.Peer
	LastActivity() time.Time
} {
	if r == nil {
		return nil
	}
	s := r.SessionFor(peer.bdx(), otaScope)
	if s == nil {
		return nil // no typed nil in the interface
	}
	return s
}

// MayDelegateConsentTo implements [OTAUpdateManager]: with
// [OTAProviderConfig].DelegateConsentToRequestor on, a requestor that says
// it can consent may; off, none may. matter.js also refuses a node whose
// Basic Information it has not cached or that has LocalConfigDisabled set
// (:645-646); this manager holds no Basic Information of a requestor and
// delegates on the switch and RequestorCanConsent alone
// (BD-Matter-OTAProvider-StandaloneManager, F-b25ed960-21).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:638-647 at b25ed960
// (mayDelegateConsentTo).
func (m *otaStandaloneManager) MayDelegateConsentTo(_ OTAPeer, req provdef.QueryImageRequest) bool {
	return m.delegate && req.RequestorCanConsent != nil && *req.RequestorCanConsent
}

// ReportAttemptStatus implements [OTAUpdateManager].
func (m *otaStandaloneManager) ReportAttemptStatus(a *OTAUpdateAttempt, status OTAUpdateStatus, unless ...OTAUpdateStatus) bool {
	m.mu.Lock()
	if a.Ended() || slices.Contains(unless, a.Status()) {
		m.unlock(nil)
		return false
	}
	m.unlock([]otaStatusReport{m.setStatus(a, status, 0)})
	return true
}

// OfferUpdate implements [OTAUpdateManager].
func (m *otaStandaloneManager) OfferUpdate(a *OTAUpdateAttempt, token []byte, version uint32) bool {
	m.mu.Lock()
	defer m.unlock(nil)
	return m.set.OfferUpdate(a, token, version)
}

// OfferAnswered implements [OTAUpdateManager].
func (m *otaStandaloneManager) OfferAnswered(a *OTAUpdateAttempt) {
	m.mu.Lock()
	defer m.unlock(nil)
	if !a.Ended() {
		m.set.AnswerAttempt(a)
	}
}

// AttemptFor implements [OTAUpdateManager].
func (m *otaStandaloneManager) AttemptFor(peer OTAPeer, token []byte) *OTAUpdateAttempt {
	m.mu.Lock()
	defer m.unlock(nil)
	return m.set.Find(peer, token)
}

// ApplyUpdate implements [OTAUpdateManager].
func (m *otaStandaloneManager) ApplyUpdate(_ context.Context, peer OTAPeer, token []byte, newVersion uint32, applyDelay time.Duration) bool {
	m.mu.Lock()
	a, allowed := m.set.ApplyUpdate(peer, token, newVersion)
	var reports []otaStatusReport
	switch {
	case allowed:
		reports = append(reports, m.setStatus(a, OTAUpdateApplying, applyDelay))
	case a != nil:
		reports = append(reports, m.setStatus(a, OTAUpdateCancelled, 0))
	default:
		m.logger.Info("ota.provider.apply_version_zero_unknown_token", slog.Uint64("node_id", peer.NodeID))
	}
	m.unlock(reports)
	return allowed
}

// UpdateApplied implements [OTAUpdateManager]: the attempt of the token
// ends Done. The status observer learns Done with the reported version for
// every NotifyUpdateApplied, as it did before the attempts.
func (m *otaStandaloneManager) UpdateApplied(peer OTAPeer, token []byte, softwareVersion uint32) {
	m.mu.Lock()
	previous := OTAUpdateUnknown
	if a := m.set.Find(peer, token); a != nil {
		previous = a.Status()
	}
	m.set.UpdateApplied(peer, token)
	m.unlock([]otaStatusReport{{peer: peer, status: OTAUpdateDone, previous: previous, version: softwareVersion}})
}

// BDXTransferOptionsFor implements [OTAUpdateManager].
func (m *otaStandaloneManager) BDXTransferOptionsFor(peer OTAPeer) bdx.ScopeConfig {
	block := m.maxBlock
	if m.blockFor != nil {
		if b := m.blockFor(peer); b != 0 {
			block = b
		}
	}
	return bdx.ScopeConfig{MaxBlockSize: block}
}

// MatterDropFabric implements [contract.FabricScopedState]: the fabric's
// attempts end (their transfer listeners with them) and each of its
// requestors loses the BDX scope its query enabled — a transfer of it that
// still runs is closed (owner decision 167).
func (m *otaStandaloneManager) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	m.mu.Lock()
	peers := m.set.DropFabric(fabricIndex)
	m.unlock(nil)
	if m.bdx != nil {
		for _, peer := range peers {
			if err := m.bdx.DisablePeerForScope(peer.bdx(), otaScope, true); err != nil {
				m.logger.Info("ota.provider.bdx_close_failed", slog.Any("err", err))
			}
		}
	}
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of the running attempts.
func (m *otaStandaloneManager) MatterHeldFabrics() []uint8 {
	m.mu.Lock()
	defer m.unlock(nil)
	return m.set.Fabrics()
}
