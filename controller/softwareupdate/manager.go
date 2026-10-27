// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/client"
)

// Peer is a node's address on our fabric.
type Peer = exchange.PeerAddress

// Status is an update's status as the provider reports it — matter.js
// OtaUpdateStatus (SoftwareUpdateManager.ts:105-114 at 862ad75f),
// cluster/core's OTAUpdateStatus.
type Status = mattercore.OTAUpdateStatus

// ProgressTimeout is how long an update may go without a status change or
// BDX activity before it counts as stalled — OTA_PROGRESS_TIMEOUT
// (OtaProgress.ts:15, Minutes(15)), the provider's too.
const ProgressTimeout = 15 * time.Minute

// queueCheckInterval is the period of the stalled-update check
// (SoftwareUpdateManager.ts:961-967, Minutes(5)).
const queueCheckInterval = 5 * time.Minute

// maxApplyDelay is the longest delay an apply is allowed with, a day
// (OtaSoftwareUpdateProviderServer.ts:467-473, MAX_DELAYED_ACTION_TIME).
const maxApplyDelay = 24 * time.Hour

// ErrNotApplicable is a node that is not currently applicable for OTA
// updates — matter.js's OtaUpdateError of forceUpdate
// (SoftwareUpdateManager.ts:1074-1077).
var ErrNotApplicable = errors.New("softwareupdate: node is not currently applicable for OTA updates")

// ErrMismatch is a consent whose vendor or product does not match the
// node — ImplementationError (SoftwareUpdateManager.ts:1166-1168).
var ErrMismatch = errors.New("softwareupdate: node does not match the given vendorId/productId")

// ErrTransferOptions is an invalid transfer option — assertTransferOptions
// (SoftwareUpdateManager.ts:59-69).
var ErrTransferOptions = errors.New("softwareupdate: invalid transfer options")

// Target names the update a consent applies to and how to transfer it —
// SoftwareUpdateManager.UpdateTarget (:1393-1413).
type Target struct {
	VendorID              uint16
	ProductID             uint16
	TargetSoftwareVersion uint32
	// MaxBDXBlockSize caps the block size of this transfer over
	// [Config].MaxBDXBlockSize; nil falls back to it. It must be positive.
	MaxBDXBlockSize *uint16
	// BDXAdditionalMrpDelay is the transfer's MRP margin over
	// [Config].BDXAdditionalMrpDelay; nil falls back to it. It must not
	// be negative.
	BDXAdditionalMrpDelay *time.Duration
}

func (t Target) assert() error {
	if t.MaxBDXBlockSize != nil && *t.MaxBDXBlockSize == 0 {
		return fmt.Errorf("%w: maxBdxBlockSize must be a positive integer, got 0", ErrTransferOptions)
	}
	if t.BDXAdditionalMrpDelay != nil && *t.BDXAdditionalMrpDelay < 0 {
		return fmt.Errorf("%w: bdxAdditionalMrpDelay must be a finite, non-negative duration, got %v", ErrTransferOptions, *t.BDXAdditionalMrpDelay)
	}
	return nil
}

// TransferOptions are a peer's BDX options — bdxTransferOptionsFor
// (:1133-1144): 0 and nil leave the decision to the peer and its medium.
type TransferOptions struct {
	MaxBlockSize       *uint16
	AdditionalMrpDelay *time.Duration
}

// SoftwareUpdateInfo is an update found for a node — matter.js
// SoftwareUpdateInfo (:116-137).
type SoftwareUpdateInfo struct { //nolint:revive // matter.js's name (SoftwareUpdateInfo); UpdateInfo is the store's
	VendorID              uint16
	ProductID             uint16
	SoftwareVersion       uint32
	SoftwareVersionString string
	ReleaseNotesURL       string
	SpecificationVersion  uint32
	// Source is SourceLocal, SourceProd or SourceTest.
	Source string
}

// PeerUpdate is a node with an update, as [Manager.QueryUpdates] returns it.
type PeerUpdate struct {
	Peer Peer
	Info SoftwareUpdateInfo
}

// PendingStatus is a queued update's state — PendingUpdateInfo.status
// (:139-162).
type PendingStatus string

// The pending statuses.
const (
	// PendingQueued waits for its turn; no announcement was sent yet.
	PendingQueued PendingStatus = "queued"
	// PendingInProgress was announced to the node; the manager waits for it.
	PendingInProgress PendingStatus = "in-progress"
	// PendingStalled had no status change and no BDX activity within
	// [ProgressTimeout]; it is reset and retried.
	PendingStalled PendingStatus = "stalled"
)

// PendingUpdate is one entry of [Manager.QueuedUpdates] — PendingUpdateInfo
// (:139-162).
type PendingUpdate struct {
	Peer                  Peer
	VendorID              uint16
	ProductID             uint16
	TargetSoftwareVersion uint32
	Status                PendingStatus
	// LastProgressStatus is the last status the provider reported; nil
	// before any.
	LastProgressStatus *Status
	// LastProgressUpdateTime is when the update last moved; zero while
	// queued.
	LastProgressUpdateTime time.Time
	// BDXTransferred and BDXDataLength are the transfer's progress of an
	// in-progress update; nil otherwise or while unknown.
	BDXTransferred, BDXDataLength *uint64
}

// AvailableDetails is what [Manager.UpdateExistsFor] offers a querying
// node — OtaUpdateAvailableDetails (:96-103).
type AvailableDetails struct {
	NewSoftwareVersion       uint32
	NewSoftwareVersionString string
	// Filename is the stored image (the file designator "ota/<filename>").
	Filename        string
	ConsentRequired bool
}

// ConsentState is the answer of a [Config].RequestConsent hook.
type ConsentState = mattercore.OTAConsentState

// Config configures a [Manager]. The controller builds it (controller.Config.OTA).
type Config struct {
	// Registry holds the nodes the manager updates (required).
	Registry *client.Registry
	// Images is the image store and the ledger lookups (required).
	Images *Images
	// FabricIndex, ProviderNodeID and ProviderEndpoint name the provider
	// on our fabric: the DefaultOTAProviders record and the announcement
	// carry them (OtaAnnouncements.ts:47-58, :170-174, :272-278).
	FabricIndex      uint8
	ProviderNodeID   uint64
	ProviderEndpoint uint16
	// VendorID is our fabric's vendor, the announcement's (:272-278).
	VendorID uint16
	// BDX is the provider's BDX responder: the manager reads a transfer's
	// liveness and closes a stale one (#bdxSessionFor, :322-328); nil
	// knows no transfer.
	BDX *bdx.Responder
	// Armer re-subscribes a node that restarts after it applied
	// (#rebootResubscribeArmer, :280-288); nil arms nothing.
	Armer *peer.RebootResubscribeArmer
	// SessionID is the id of the newest session with a peer — the
	// fallback of the startUp suppression's session (:779-782); nil knows
	// none.
	SessionID func(p Peer) (uint16, bool)

	// AllowTestImages also offers test-net and local images
	// (State.allowTestOtaImages, :1374-1375, default false).
	AllowTestImages bool
	// UpdateCheckInterval is the period of the update check
	// (State.updateCheckInterval, default 24 h); 0 is the default.
	UpdateCheckInterval time.Duration
	// AnnounceAsDefaultProvider writes the provider as every node's
	// default provider (State.announceAsDefaultProvider, default false).
	AnnounceAsDefaultProvider bool
	// AnnouncementInterval is the period of that write, at least 24 h
	// (State.announcementInterval); 0 is 24 h.
	AnnouncementInterval time.Duration
	// MaxBDXBlockSize and BDXAdditionalMrpDelay are the default transfer
	// options (State.maxBdxBlockSize, bdxAdditionalMrpDelay, :1383-1391).
	MaxBDXBlockSize       uint16
	BDXAdditionalMrpDelay *time.Duration
	// ApplyDelay is how long an allowed apply waits, rounded up to whole
	// seconds and at most a day (the provider's State.applyDelay,
	// OtaSoftwareUpdateProviderServer.ts:652-660, default 0).
	ApplyDelay time.Duration
	// RequestConsent asks the user for an update a requestor that cannot
	// consent needs consent for (requestUserConsentForUpdate,
	// OtaSoftwareUpdateProviderServer.ts:507-515); nil answers Unknown,
	// which declines.
	RequestConsent func(ctx context.Context, p Peer, req QueryImageRequest, d AvailableDetails) (ConsentState, time.Duration)

	// OnUpdateAvailable learns an update a node has no consent for
	// (events.updateAvailable, :1421-1422); the host grants it with
	// AddUpdateConsent.
	OnUpdateAvailable func(p Peer, info SoftwareUpdateInfo)
	// OnUpdateApplying, OnUpdateDone and OnUpdateFailed are
	// events.updateApplying, updateDone and updateFailed (:1424-1438); [Manager.Observe] reports them too.
	OnUpdateApplying, OnUpdateDone, OnUpdateFailed func(p Peer)
	// OnStatus observes each status the provider reports for a node.
	OnStatus func(p Peer, status Status, version uint32)

	// Clock runs the timers; nil is the system clock.
	Clock clock.Clock
	// Rand draws the timers' jitter in [0, 1); nil is math/rand/v2.
	Rand func() float64
	// Logger receives the lines; nil is slog.Default().
	Logger *slog.Logger
}

type consent struct {
	Target
	peer Peer
}

// queueEntry is UpdateQueueEntry (:75-82).
type queueEntry struct {
	consent
	endpoint uint16
	// started is lastProgressUpdateTime; zero while queued.
	started time.Time
	// status is lastProgressStatus; nil while undefined.
	status *Status
	// transferred is lastBdxTransferredBytes.
	transferred *uint64
	// applyDelay is the delay of an Applying update.
	applyDelay time.Duration
}

// Manager is the controller's SoftwareUpdateManager. Safe for concurrent use.
type Manager struct {
	cfg   Config
	clock clock.Clock
	rand  func() float64
	log   *slog.Logger

	mu sync.Mutex
	// suppress is internal.suppressUpdates (:1438-1439): set until Start
	// and again from Close.
	suppress bool
	consents []consent
	queue    []*queueEntry
	known    map[Peer]SoftwareUpdateInfo
	// startUpSuppress is internal.pendingStartUpSuppress (:1421-1431).
	startUpSuppress map[Peer]uint16
	// observed are the nodes preparePeerForUpdate registered on, with the
	// software version last seen (the versionUpdateObservers, :733-772).
	observed map[Peer]uint32
	// applyDelays are the delays the apply policy allowed, per peer.
	applyDelays map[Peer]time.Duration

	checkTimer, queueTimer clock.Timer
	announcements          *announcements
	closed                 bool
	work                   sync.WaitGroup
	// obs are the observers of [Manager.Observe], with their own lock.
	obs observers
}

// New returns the manager over cfg; [Manager.Start] starts it.
func New(cfg Config) (*Manager, error) {
	if cfg.Registry == nil || cfg.Images == nil {
		return nil, errors.New("softwareupdate: a Manager needs Registry and Images")
	}
	m := &Manager{
		cfg: cfg, clock: clock.Or(cfg.Clock), rand: cfg.Rand, log: cfg.Logger,
		suppress: true, known: map[Peer]SoftwareUpdateInfo{}, startUpSuppress: map[Peer]uint16{},
		observed: map[Peer]uint32{}, applyDelays: map[Peer]time.Duration{},
	}
	if m.rand == nil {
		m.rand = rand.Float64
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	if m.cfg.UpdateCheckInterval == 0 {
		m.cfg.UpdateCheckInterval = 24 * time.Hour
	}
	if m.cfg.AnnouncementInterval == 0 {
		m.cfg.AnnouncementInterval = 24 * time.Hour
	}
	return m, nil
}

// Start brings the manager online — #nodeOnline (:200-230): the
// announcements start per [Config].AnnounceAsDefaultProvider and the first
// update check is scheduled 5 to 10 minutes later, then every
// UpdateCheckInterval.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.suppress = false
	m.announcements = newAnnouncements(m)
	if m.cfg.AnnounceAsDefaultProvider {
		m.announcements.setInterval(m.cfg.AnnouncementInterval)
	}
	delay := 5*time.Minute + time.Duration(math.Floor(m.rand()*300))*time.Second
	m.log.Info("ota.manager.first_check", slog.Duration("in", delay))
	if m.checkTimer != nil {
		m.checkTimer.Stop()
	}
	m.checkTimer = m.clock.AfterFunc(delay, m.initializeUpdateCheck)
}

// initializeUpdateCheck is #initializeUpdateCheck (:261-271).
func (m *Manager) initializeUpdateCheck() {
	if err := m.CheckForUpdates(context.Background()); err != nil {
		m.log.Info("ota.manager.initial_check_failed", slog.Any("err", err))
	}
	m.scheduleCheck()
}

func (m *Manager) scheduleCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.checkTimer = m.clock.AfterFunc(m.cfg.UpdateCheckInterval, func() {
		_ = m.CheckForUpdates(context.Background())
		m.scheduleCheck()
	})
}

// Close takes the manager offline — #nodeGoingOffline and the disposal
// (:232-244, :1323-1329): no update work starts any more, the timers and
// the announcements stop.
func (m *Manager) Close() {
	m.mu.Lock()
	m.suppress = true
	m.closed = true
	for _, t := range []clock.Timer{m.checkTimer, m.queueTimer} {
		if t != nil {
			t.Stop()
		}
	}
	a := m.announcements
	m.announcements = nil
	m.mu.Unlock()
	if a != nil {
		a.close()
	}
	m.work.Wait()
	m.obs.close()
}

// goWork runs fn on a goroutine Close waits for, unless closed.
func (m *Manager) goWork(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.work.Go(fn)
}

// nodeAt is the registry's node of p, nil for none.
func (m *Manager) nodeAt(p Peer) *client.Node { return m.cfg.Registry.GetByAddress(p) }

// basicInformation is the node's BasicInformation from its image, false
// without one.
func basicInformation(n *client.Node) (basicinfo.State, bool) {
	ep := n.Endpoint(0)
	if ep == nil {
		return basicinfo.State{}, false
	}
	c := ep.Cluster(basicinfo.ClusterID)
	if c == nil || !c.Ready() {
		return basicinfo.State{}, false
	}
	st, _ := basicinfo.StateFrom(c)
	return st, true
}

// prepared is #preparePeerForUpdate's result.
type prepared struct {
	endpoint              uint16
	peer                  Peer
	vendorID, productID   uint16
	softwareVersion       uint32
	softwareVersionString string
}

// preparePeerForUpdate is #preparePeerForUpdate (:703-776): an applicable
// node's identity, and the node observed for its software version, its
// StartUp event and its subscription from now on.
func (m *Manager) preparePeerForUpdate(n *client.Node) (prepared, bool) {
	ota, ok := peerApplicableForUpdate(n, m.log)
	if !ok {
		return prepared{}, false
	}
	st, _ := basicInformation(n)
	p := prepared{
		endpoint: ota.endpoint, peer: ota.peer, vendorID: st.VendorId, productID: st.ProductId,
		softwareVersion: st.SoftwareVersion, softwareVersionString: st.SoftwareVersionString,
	}
	m.mu.Lock()
	if _, seen := m.observed[p.peer]; !seen {
		m.observed[p.peer] = p.softwareVersion
	}
	m.mu.Unlock()
	return p, true
}

// NodeChanged, NodeEvent and NodeSubscription are the node events the
// manager observes a node through once it prepared it for an update —
// softwareVersion$Changed, startUp and subscriptionStatusChanged
// (SoftwareUpdateManager.ts:714-760 at 862ad75f). The controller feeds
// them from every node's hooks (client.RegistryConfig.HooksFor).

// NodeChanged takes a change of the node's image.
func (m *Manager) NodeChanged(n *client.Node) { m.versionMaybeChanged(n) }

// NodeEvent takes an event the node reported.
func (m *Manager) NodeEvent(n *client.Node, endpoint uint16, cluster, event uint32, value any) {
	if endpoint != 0 || cluster != basicinfo.ClusterID || event != basicinfo.EventStartUp {
		return
	}
	ev, ok := value.(basicinfo.StartUpEvent)
	if !ok {
		return
	}
	if p, ok := n.PeerAddress(); ok && m.isObserved(p) {
		m.onSoftwareVersionChanged(p, ev.SoftwareVersion, true)
	}
}

// NodeSubscription takes a change of the node's subscription.
func (m *Manager) NodeSubscription(n *client.Node, active bool) {
	if !active {
		return
	}
	if p, ok := n.PeerAddress(); ok && m.isObserved(p) {
		m.onSubscribedAfterApply(p, n)
	}
}

// SetProvider names the provider's endpoint, once the provider node runs.
func (m *Manager) SetProvider(nodeID uint64, endpoint uint16) {
	m.mu.Lock()
	m.cfg.ProviderNodeID, m.cfg.ProviderEndpoint = nodeID, endpoint
	m.mu.Unlock()
}

// QueryImageRequest is a requestor's QueryImage.
type QueryImageRequest = provdef.QueryImageRequest

func (m *Manager) isObserved(p Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.observed[p]
	return ok
}

// versionMaybeChanged feeds a changed software version to
// onSoftwareVersionChanged — softwareVersion$Changed (:736-746).
func (m *Manager) versionMaybeChanged(n *client.Node) {
	p, ok := n.PeerAddress()
	if !ok {
		return
	}
	st, ok := basicInformation(n)
	if !ok {
		return
	}
	m.mu.Lock()
	last, seen := m.observed[p]
	changed := seen && last != st.SoftwareVersion
	if changed {
		m.observed[p] = st.SoftwareVersion
	}
	m.mu.Unlock()
	if changed {
		m.onSoftwareVersionChanged(p, st.SoftwareVersion, false)
	}
}

func (m *Manager) indexOfLocked(p Peer) int {
	return slices.IndexFunc(m.queue, func(e *queueEntry) bool { return e.peer == p })
}

// onSoftwareVersionChanged is #onSoftwareVersionChanged (:769-842): a
// startUp of the reboot the provider already handled is suppressed; a
// version below the target after a startUp of an Applying or WaitForApply
// update fails it; a version at or above the target completes it and
// drops the consents it covers.
func (m *Manager) onSoftwareVersionChanged(p Peer, newVersion uint32, isStartUp bool) {
	m.mu.Lock()
	if isStartUp {
		if suppressed, ok := m.startUpSuppress[p]; ok {
			delete(m.startUpSuppress, p)
			var current uint16
			var have bool
			if m.cfg.SessionID != nil {
				m.mu.Unlock()
				current, have = m.cfg.SessionID(p)
				m.mu.Lock()
			}
			if have && current == suppressed {
				m.mu.Unlock()
				return
			}
		}
	}
	i := m.indexOfLocked(p)
	if i < 0 {
		m.mu.Unlock()
		return
	}
	e := m.queue[i]
	previous := e.status
	if !e.started.IsZero() {
		m.log.Info("ota.manager.version_change_clears_progress", slog.String("node", peerString(p)))
		e.reset()
	}
	if newVersion < e.TargetSoftwareVersion {
		if isStartUp && previous != nil && (*previous == mattercore.OTAUpdateApplying || *previous == mattercore.OTAUpdateWaitForApply) {
			m.log.Warn("ota.manager.apply_failed_reboot", slog.String("node", peerString(p)),
				slog.Uint64("version", uint64(newVersion)), slog.Uint64("target", uint64(e.TargetSoftwareVersion)))
			m.failAppliedLocked(i)
			return
		}
		m.mu.Unlock()
		return
	}
	m.log.Info("ota.manager.updated", slog.String("node", peerString(p)), slog.Uint64("version", uint64(newVersion)))
	delete(m.known, p)
	delete(m.startUpSuppress, p)
	m.queue = slices.Delete(m.queue, i, i+1)
	m.consents = slices.DeleteFunc(m.consents, func(c consent) bool { return c.peer == p && c.TargetSoftwareVersion <= newVersion })
	done := m.cfg.OnUpdateDone
	m.mu.Unlock()
	if done != nil {
		done(p)
	}
	m.emit(UpdateEvent{Kind: UpdateDone, Peer: p, Target: e.Target})
	m.triggerQueuedUpdate()
}

// onSubscribedAfterApply is #onSubscribedAfterApply (:848-870): an
// Applying update whose node reports a version below the target over a
// subscription established after its restart deadline did not apply.
func (m *Manager) onSubscribedAfterApply(p Peer, n *client.Node) {
	if st, ok := basicInformation(n); ok {
		m.subscribedAfterApply(p, st.SoftwareVersion)
	}
}

// subscribedAfterApply is onSubscribedAfterApply over the version the
// node reports.
func (m *Manager) subscribedAfterApply(p Peer, version uint32) {
	m.mu.Lock()
	i := m.indexOfLocked(p)
	if i < 0 {
		m.mu.Unlock()
		return
	}
	e := m.queue[i]
	if e.status == nil || *e.status != mattercore.OTAUpdateApplying || e.started.IsZero() ||
		m.clock.Now().Before(e.started.Add(peer.RebootReturnTimeout(e.applyDelay))) || version >= e.TargetSoftwareVersion {
		m.mu.Unlock()
		return
	}
	m.log.Warn("ota.manager.apply_failed_resubscribed", slog.String("node", peerString(p)), slog.Uint64("version", uint64(version)))
	m.failAppliedLocked(i)
}

// failAppliedLocked is #failAppliedUpdate (:875-880); it unlocks m.mu.
func (m *Manager) failAppliedLocked(i int) {
	p, target := m.queue[i].peer, m.queue[i].Target
	m.queue = slices.Delete(m.queue, i, i+1)
	delete(m.startUpSuppress, p)
	failed := m.cfg.OnUpdateFailed
	m.mu.Unlock()
	if failed != nil {
		failed(p)
	}
	m.emit(UpdateEvent{Kind: UpdateFailed, Peer: p, Target: target, Err: ErrNotApplied})
	m.triggerQueuedUpdate()
}

func (e *queueEntry) reset() {
	e.started = time.Time{}
	unknown := mattercore.OTAUpdateUnknown
	e.status = &unknown
	e.transferred = nil
	e.applyDelay = 0
}

// bdxSessionFor is #bdxSessionFor (:322-328).
func (m *Manager) bdxSessionFor(p Peer) *bdx.ServerSession {
	if m.cfg.BDX == nil {
		return nil
	}
	return m.cfg.BDX.SessionFor(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota")
}

// isTransferring is #isTransferring (:334-336): any change of the
// counter is liveness.
func isTransferring(e *queueEntry, transferred *uint64) bool {
	if transferred == nil {
		return false
	}
	var last uint64
	if e.transferred != nil {
		last = *e.transferred
	}
	return *transferred != last
}

func transferredOf(s *bdx.ServerSession) *uint64 {
	if s == nil {
		return nil
	}
	v := s.Transferred()
	return &v
}

// QueuedUpdates is a snapshot of the update queue — queuedUpdates (:293-320).
func (m *Manager) QueuedUpdates() []PendingUpdate {
	m.mu.Lock()
	entries := make([]queueEntry, len(m.queue))
	for i, e := range m.queue {
		entries[i] = *e
	}
	m.mu.Unlock()
	now := m.clock.Now()
	out := make([]PendingUpdate, 0, len(entries))
	for _, e := range entries {
		s := m.bdxSessionFor(e.peer)
		var status PendingStatus
		switch {
		case e.started.IsZero():
			status = PendingQueued
		case e.started.Add(e.applyDelay+ProgressTimeout).Before(now) && !isTransferring(&e, transferredOf(s)):
			status = PendingStalled
		default:
			status = PendingInProgress
		}
		u := PendingUpdate{
			Peer: e.peer, VendorID: e.VendorID, ProductID: e.ProductID, TargetSoftwareVersion: e.TargetSoftwareVersion,
			Status: status, LastProgressStatus: e.status, LastProgressUpdateTime: e.started,
		}
		if status == PendingInProgress && s != nil {
			u.BDXTransferred = transferredOf(s)
			if res, ok := s.Negotiated(); ok && res.Length != nil {
				l := *res.Length
				u.BDXDataLength = &l
			}
		}
		out = append(out, u)
	}
	return out
}

// validatePeerDetails is #validatePeerDetails (:360-379).
func (m *Manager) validatePeerDetails(p Peer, version uint32, vid, pid uint16) bool {
	n := m.nodeAt(p)
	if n == nil {
		return false
	}
	st, ok := basicInformation(n)
	return ok && st.SoftwareVersion == version && st.VendorId == vid && st.ProductId == pid
}

// UpdateExistsFor is the stored update the querying node gets —
// updateExistsFor (:385-468): the node's details must match the query;
// the applicable stored images over its version (prod ones, the others
// only with AllowTestImages), highest first; a requestor that can consent
// gets the first, flagged consent-required unless the manager holds its
// consent; one that cannot only a version the manager holds a consent
// for.
func (m *Manager) UpdateExistsFor(ctx context.Context, p Peer, req provdef.QueryImageRequest) (AvailableDetails, bool) {
	if !m.validatePeerDetails(p, req.SoftwareVersion, req.VendorId, req.ProductId) {
		m.log.Info("ota.manager.peer_details_mismatch", slog.String("node", peerString(p)))
		return AvailableDetails{}, false
	}
	vid, pid, version := req.VendorId, req.ProductId, req.SoftwareVersion
	var candidates []StoredImage
	for _, st := range m.cfg.Images.Find(ctx, FindOptions{VendorID: &vid, ProductID: &pid}) {
		lo, hi := uint32(0), version
		if st.MinApplicable != nil {
			lo = *st.MinApplicable
		}
		if st.MaxApplicable != nil {
			hi = *st.MaxApplicable
		}
		if version < st.SoftwareVersion && version >= lo && version <= hi && (st.Mode == ModeProd || m.cfg.AllowTestImages) {
			candidates = append(candidates, st)
		}
	}
	slices.SortStableFunc(candidates, func(a, b StoredImage) int { return cmp.Compare(b.SoftwareVersion, a.SoftwareVersion) })
	if len(candidates) == 0 {
		return AvailableDetails{}, false
	}
	m.mu.Lock()
	consented := func(c StoredImage) bool {
		return slices.ContainsFunc(m.consents, func(k consent) bool {
			return k.VendorID == c.VendorID && k.ProductID == c.ProductID && k.TargetSoftwareVersion >= c.SoftwareVersion && k.peer == p
		})
	}
	var chosen *StoredImage
	required := false
	canConsent := req.RequestorCanConsent != nil && *req.RequestorCanConsent
	if canConsent {
		chosen = &candidates[0]
		required = !consented(*chosen)
	} else {
		for i := range candidates {
			if consented(candidates[i]) {
				chosen = &candidates[i]
				break
			}
		}
	}
	m.mu.Unlock()
	if chosen == nil {
		m.log.Info("ota.manager.no_candidate", slog.String("node", peerString(p)))
		return AvailableDetails{}, false
	}
	return AvailableDetails{
		NewSoftwareVersion: chosen.SoftwareVersion, NewSoftwareVersionString: chosen.SoftwareVersionString,
		Filename: chosen.Filename, ConsentRequired: required,
	}, true
}

// CheckForUpdates checks every node for updates, stored images included,
// and purges the stored images no node needs — checkForUpdates (:471-474).
func (m *Manager) CheckForUpdates(ctx context.Context) error {
	if _, err := m.QueryUpdates(ctx, nil, true); err != nil {
		return err
	}
	m.cleanupObsoleteUpdates(ctx)
	return nil
}

// cleanupObsoleteUpdates is #cleanupObsoleteUpdates (:483-556): a stored
// prod or test image every node of its product runs at or above goes, and
// so does every test image while test images are not allowed; local
// images stay.
func (m *Manager) cleanupObsoleteUpdates(ctx context.Context) {
	type key struct{ vid, pid uint16 }
	versions := map[key]uint32{}
	for _, n := range m.cfg.Registry.Nodes() {
		st, ok := basicInformation(n)
		if !ok {
			continue
		}
		k := key{st.VendorId, st.ProductId}
		if v, seen := versions[k]; !seen || st.SoftwareVersion < v {
			versions[k] = st.SoftwareVersion
		}
	}
	purgeTest := !m.cfg.AllowTestImages
	if len(versions) == 0 && !purgeTest {
		return
	}
	opts := FindOptions{}
	if len(versions) == 0 {
		opts.Mode = ModeTest
	}
	for _, st := range m.cfg.Images.Find(ctx, opts) {
		if st.Mode == ModeLocal {
			continue
		}
		if st.Mode == ModeTest && purgeTest {
			if err := m.cfg.Images.Delete(ctx, st.Filename); err == nil {
				m.log.Info("ota.manager.cleaned_test_image", slog.String("file", st.Filename))
			}
			continue
		}
		v, ok := versions[key{st.VendorID, st.ProductID}]
		if !ok {
			continue
		}
		if st.SoftwareVersion <= v {
			if err := m.cfg.Images.Delete(ctx, st.Filename); err == nil {
				m.log.Info("ota.manager.cleaned_obsolete_image", slog.String("file", st.Filename))
			}
		}
	}
}

// QueryUpdates checks every node, or only, for updates over the ledgers
// and — with includeStored — the stored images, and returns the nodes it
// found one for — queryUpdates (:559-623). A check the manager ran (it
// was not suppressed) ends with an [UpdateCheckFinished] event.
func (m *Manager) QueryUpdates(ctx context.Context, only *client.Node, includeStored bool) ([]PeerUpdate, error) {
	out, ran, failed := m.queryUpdates(ctx, only, includeStored)
	if ran {
		ev := UpdateEvent{Kind: UpdateCheckFinished, Updates: slices.Clone(out), Err: failed}
		if only != nil {
			ev.Peer, _ = only.PeerAddress()
		}
		m.emit(ev)
	}
	return out, nil
}

// queryUpdates is QueryUpdates; ran is false when the manager was
// suppressed before the check ended, failed joins the errors of the
// products whose check failed and was passed over.
func (m *Manager) queryUpdates(ctx context.Context, only *client.Node, includeStored bool) (_ []PeerUpdate, ran bool, failed error) {
	type group struct {
		vid, pid uint16
		version  uint32
		targets  []prepared
	}
	var order []string
	groups := map[string]*group{}
	for _, n := range m.cfg.Registry.Nodes() {
		if m.suppressed() {
			return nil, false, nil
		}
		if only != nil && n != only {
			continue
		}
		d, ok := m.preparePeerForUpdate(n)
		if !ok {
			continue
		}
		k := strconv.Itoa(int(d.vendorID)) + "-" + strconv.Itoa(int(d.productID)) + "-" + strconv.FormatUint(uint64(d.softwareVersion), 10)
		g := groups[k]
		if g == nil {
			g = &group{vid: d.vendorID, pid: d.productID, version: d.softwareVersion}
			groups[k] = g
			order = append(order, k)
		}
		g.targets = append(g.targets, d)
	}
	if len(groups) == 0 {
		m.log.Info("ota.manager.no_updatable_nodes")
		return nil, true, nil
	}
	var out []PeerUpdate
	for _, k := range order {
		if m.suppressed() {
			return nil, false, nil
		}
		g := groups[k]
		peers, err := m.checkProductForUpdates(ctx, g.vid, g.pid, g.version, g.targets, includeStored)
		if err != nil {
			m.log.Warn("ota.manager.check_failed", slog.Int("vendor_id", int(g.vid)), slog.Int("product_id", int(g.pid)), slog.Any("err", err))
			failed = errors.Join(failed, err)
			continue
		}
		m.mu.Lock()
		for _, p := range peers {
			if info, ok := m.known[p]; ok {
				out = append(out, PeerUpdate{Peer: p, Info: info})
			}
		}
		m.mu.Unlock()
	}
	return out, true, failed
}

func (m *Manager) suppressed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suppress
}

// checkProductForUpdates is #checkProductForUpdates (:625-701): the
// product's update is found and made available in the store; a node the
// manager holds a consent for exactly that version is queued, every other
// one reported as needing consent.
func (m *Manager) checkProductForUpdates(ctx context.Context, vid, pid uint16, version uint32, targets []prepared, includeStored bool) ([]Peer, error) {
	opts := CheckOptions{IncludeStored: includeStored}
	if !m.cfg.AllowTestImages {
		yes := true
		opts.IsProduction = &yes
	}
	info, ok := m.cfg.Images.CheckForUpdate(ctx, vid, pid, version, opts)
	if !ok || m.suppressed() {
		return nil, nil
	}
	if _, err := m.cfg.Images.DownloadUpdate(ctx, info); err != nil {
		return nil, err
	}
	details := SoftwareUpdateInfo{
		VendorID: info.VendorID, ProductID: info.ProductID, SoftwareVersion: info.SoftwareVersion,
		SoftwareVersionString: info.SoftwareVersionString, ReleaseNotesURL: info.ReleaseNotesURL,
		SpecificationVersion: info.SpecificationVersion, Source: info.Source,
	}
	peers := make([]Peer, 0, len(targets))
	for _, t := range targets {
		peers = append(peers, t.peer)
		m.mu.Lock()
		m.known[t.peer] = details
		var held *consent
		for i := range m.consents {
			c := m.consents[i]
			if c.VendorID == vid && c.ProductID == pid && c.TargetSoftwareVersion == info.SoftwareVersion && c.peer == t.peer {
				held = &c
				break
			}
		}
		suppressed := m.suppress
		m.mu.Unlock()
		if suppressed {
			break
		}
		if held != nil {
			e := &queueEntry{consent: consent{Target: Target{
				VendorID: vid, ProductID: pid, TargetSoftwareVersion: info.SoftwareVersion,
				MaxBDXBlockSize: held.MaxBDXBlockSize, BDXAdditionalMrpDelay: held.BDXAdditionalMrpDelay,
			}, peer: t.peer}, endpoint: t.endpoint}
			m.queueUpdate(e) //nolint:contextcheck // the queue's work runs on the manager's lifetime
		} else {
			if fn := m.cfg.OnUpdateAvailable; fn != nil {
				fn(t.peer, details) // requestConsentForUpdate (:885-887)
			}
			m.emit(UpdateEvent{Kind: UpdateAvailable, Peer: t.peer, Target: Target{VendorID: vid, ProductID: pid, TargetSoftwareVersion: info.SoftwareVersion}, Info: details})
		}
	}
	return peers, nil
}

// queueUpdate is #queueUpdate (:892-920): an entry of a node already
// queued replaces it unless it is in progress.
func (m *Manager) queueUpdate(e *queueEntry) {
	m.mu.Lock()
	if i := m.indexOfLocked(e.peer); i >= 0 {
		existing := m.queue[i]
		if !existing.started.IsZero() {
			m.mu.Unlock()
			m.log.Info("ota.manager.in_progress_skip", slog.String("node", peerString(e.peer)))
			return
		}
		existing.Target = e.Target
		existing.endpoint = e.endpoint
	} else {
		m.queue = append(m.queue, e)
	}
	m.mu.Unlock()
	m.triggerQueuedUpdate()
}

// triggerQueuedUpdate is #triggerQueuedUpdate (:931-983): in-progress
// entries are refreshed by their transfer's liveness and reset when
// stalled; with none in progress the queue — fresh entries first, reset
// ones last — announces its first entry, and a periodic check watches for
// stalls.
func (m *Manager) triggerQueuedUpdate() {
	m.mu.Lock()
	if m.suppress {
		m.mu.Unlock()
		return
	}
	now := m.clock.Now()
	inProgress := 0
	for _, e := range m.queue {
		if e.started.IsZero() {
			continue
		}
		inProgress++
		if t := transferredOf(m.bdxSessionFor(e.peer)); isTransferring(e, t) {
			e.transferred = t
			e.started = now
			continue
		}
		if e.started.Add(e.applyDelay + ProgressTimeout).Before(now) {
			m.log.Info("ota.manager.stalled_reset", slog.String("node", peerString(e.peer)))
			e.reset()
			inProgress--
		}
	}
	if inProgress == 0 && m.queueTimer != nil {
		m.queueTimer.Stop()
		m.queueTimer = nil
	}
	if inProgress > 0 {
		m.mu.Unlock()
		return
	}
	slices.SortStableFunc(m.queue, func(a, b *queueEntry) int { return cmp.Compare(unknownLast(a), unknownLast(b)) })
	if len(m.queue) == 0 {
		m.mu.Unlock()
		return
	}
	next := m.queue[0]
	if m.queueTimer == nil {
		m.queueTimer = m.clock.AfterFunc(queueCheckInterval, m.periodicQueueCheck)
	}
	m.mu.Unlock()
	m.goWork(func() { m.triggerUpdateOnNode(next) })
}

func (m *Manager) periodicQueueCheck() {
	m.mu.Lock()
	m.queueTimer = nil
	m.mu.Unlock()
	m.triggerQueuedUpdate()
	m.mu.Lock()
	if m.queueTimer == nil && !m.closed && slices.ContainsFunc(m.queue, func(e *queueEntry) bool { return !e.started.IsZero() }) {
		m.queueTimer = m.clock.AfterFunc(queueCheckInterval, m.periodicQueueCheck)
	}
	m.mu.Unlock()
}

func unknownLast(e *queueEntry) int {
	if e.status != nil && *e.status == mattercore.OTAUpdateUnknown {
		return 1
	}
	return 0
}

// triggerUpdateOnNode is #triggerUpdateOnNode (:991-1025): unless a BDX
// transfer of the node runs, the provider is announced with reason
// UpdateAvailable; the entry is in progress once the announcement went
// out, reset when it failed.
func (m *Manager) triggerUpdateOnNode(e *queueEntry) {
	// The entry's endpoint is read under the lock that queueUpdate writes
	// it under: a later consent for the same node replaces Target and
	// endpoint of a queued entry (#queueUpdate :892-920) while this
	// goroutine, started by triggerQueuedUpdate, announces the earlier one.
	m.mu.Lock()
	a := m.announcements
	endpoint := e.endpoint
	m.mu.Unlock()
	if a == nil {
		m.log.Info("ota.manager.not_initialized", slog.String("node", peerString(e.peer)))
		return
	}
	if m.bdxSessionFor(e.peer) != nil {
		m.log.Info("ota.manager.bdx_active_skip", slog.String("node", peerString(e.peer)))
		return
	}
	n := m.nodeAt(e.peer)
	if n == nil {
		return
	}
	m.log.Info("ota.manager.announcing", slog.String("node", peerString(e.peer)))
	if err := a.announceOtaProvider(context.Background(), n, endpoint, announceUpdateAvailable); err != nil {
		m.log.Warn("ota.manager.announce_failed", slog.String("node", peerString(e.peer)), slog.Any("err", err))
		m.mu.Lock()
		e.reset()
		m.mu.Unlock()
		m.triggerQueuedUpdate()
		return
	}
	m.mu.Lock()
	if slices.Contains(m.queue, e) {
		e.started = m.clock.Now()
	}
	m.mu.Unlock()
}

// ForceUpdate updates the node now: a stale in-progress entry without a
// running transfer is dropped, the consent added and the update announced
// at once — forceUpdate (:1031-1086). A node with a transfer running is
// left alone; one not applicable is [ErrNotApplicable].
func (m *Manager) ForceUpdate(ctx context.Context, p Peer, t Target) error {
	if err := t.assert(); err != nil {
		return err
	}
	m.mu.Lock()
	i := slices.IndexFunc(m.queue, func(e *queueEntry) bool { return e.peer == p && !e.started.IsZero() })
	m.mu.Unlock()
	if i >= 0 {
		if m.bdxSessionFor(p) != nil {
			m.log.Info("ota.manager.force_skipped_active", slog.String("node", peerString(p)))
			return nil
		}
		m.mu.Lock()
		if j := slices.IndexFunc(m.queue, func(e *queueEntry) bool { return e.peer == p && !e.started.IsZero() }); j >= 0 {
			m.queue = slices.Delete(m.queue, j, j+1)
			delete(m.startUpSuppress, p)
		}
		m.mu.Unlock()
		if m.cfg.BDX != nil {
			_ = m.cfg.BDX.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
		}
	}
	added, err := m.AddUpdateConsent(ctx, p, t)
	if err != nil {
		return err
	}
	if !added {
		return fmt.Errorf("%w: %s", ErrNotApplicable, peerString(p))
	}
	m.mu.Lock()
	var entry *queueEntry
	for _, e := range m.queue {
		if e.peer == p && e.VendorID == t.VendorID && e.ProductID == t.ProductID && e.TargetSoftwareVersion == t.TargetSoftwareVersion {
			entry = e
		}
	}
	only := len(m.queue) == 1
	m.mu.Unlock()
	if entry == nil {
		return errors.New("softwareupdate: failed to find queued update after adding consent")
	}
	if only {
		return nil // only entry, already being processed
	}
	m.triggerUpdateOnNode(entry) //nolint:contextcheck // as matter.js, the announcement is not bound to the caller
	return nil
}

// cancelUpdate is #cancelUpdate (:1091-1123): the node's transfer is
// closed; an update not yet Applying or Done is dropped and reported
// failed, the reboot arming lifted.
func (m *Manager) cancelUpdate(p Peer) {
	if m.cfg.BDX != nil {
		_ = m.cfg.BDX.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
	}
	m.mu.Lock()
	i := m.indexOfLocked(p)
	if i < 0 {
		m.mu.Unlock()
		m.log.Warn("ota.manager.cancel_none", slog.String("node", peerString(p)))
		return
	}
	if s := m.queue[i].status; s != nil && (*s == mattercore.OTAUpdateApplying || *s == mattercore.OTAUpdateDone) {
		m.mu.Unlock()
		m.log.Info("ota.manager.cancel_too_late", slog.String("node", peerString(p)))
		return
	}
	target := m.queue[i].Target
	m.queue = slices.Delete(m.queue, i, i+1)
	delete(m.startUpSuppress, p)
	failed := m.cfg.OnUpdateFailed
	m.mu.Unlock()
	if m.cfg.Armer != nil {
		m.cfg.Armer.Disarm(p)
	}
	m.log.Info("ota.manager.cancelled", slog.String("node", peerString(p)))
	if failed != nil {
		failed(p)
	}
	m.emit(UpdateEvent{Kind: UpdateFailed, Peer: p, Target: target, Err: ErrUpdateCancelled})
	m.triggerQueuedUpdate()
}

// TransferOptionsFor is how BDX carries the node's pending update: its
// queued entry's or consent's options, else the defaults —
// bdxTransferOptionsFor (:1133-1144).
func (m *Manager) TransferOptionsFor(p Peer) TransferOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	var pending *Target
	if i := m.indexOfLocked(p); i >= 0 {
		pending = &m.queue[i].Target
	} else if j := slices.IndexFunc(m.consents, func(c consent) bool { return c.peer == p }); j >= 0 {
		pending = &m.consents[j].Target
	}
	out := TransferOptions{AdditionalMrpDelay: m.cfg.BDXAdditionalMrpDelay}
	if m.cfg.MaxBDXBlockSize != 0 {
		b := m.cfg.MaxBDXBlockSize
		out.MaxBlockSize = &b
	}
	if pending != nil {
		if pending.MaxBDXBlockSize != nil {
			out.MaxBlockSize = pending.MaxBDXBlockSize
		}
		if pending.BDXAdditionalMrpDelay != nil {
			out.AdditionalMrpDelay = pending.BDXAdditionalMrpDelay
		}
	}
	return out
}

// AddUpdateConsent adds (or replaces) the node's consent and, when the
// node is applicable now, queues its update — addUpdateConsent
// (:1153-1196). It reports whether the update was queued; a node whose
// vendor or product differs from t is [ErrMismatch].
func (m *Manager) AddUpdateConsent(_ context.Context, p Peer, t Target) (bool, error) {
	if err := t.assert(); err != nil {
		return false, err
	}
	var d prepared
	ok := false
	if n := m.nodeAt(p); n != nil {
		d, ok = m.preparePeerForUpdate(n)
	}
	if ok && (d.vendorID != t.VendorID || d.productID != t.ProductID) {
		return false, fmt.Errorf("%w: %s", ErrMismatch, peerString(p))
	}
	m.mu.Lock()
	m.consents = append(slices.DeleteFunc(m.consents, func(c consent) bool { return c.peer == p }), consent{Target: t, peer: p})
	m.mu.Unlock()
	m.emit(UpdateEvent{Kind: UpdateConsentChanged, Peer: p, Target: t, Consented: true})
	if !ok {
		m.log.Info("ota.manager.not_applicable_yet", slog.String("node", peerString(p)))
		return false, nil
	}
	m.queueUpdate(&queueEntry{consent: consent{Target: t, peer: p}, endpoint: d.endpoint}) //nolint:contextcheck // the queue's work runs on the manager's lifetime
	return true, nil
}

// HasConsent reports a consent of the node, for version when it is not 0
// — hasConsent (:1204-1211).
func (m *Manager) HasConsent(p Peer, version uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.consents, func(c consent) bool {
		return c.peer == p && (version == 0 || c.TargetSoftwareVersion == version)
	})
}

// RemoveConsent removes the node's consent (for version when it is not 0)
// and cancels its update — removeConsent (:1217-1232).
func (m *Manager) RemoveConsent(p Peer, version uint32) {
	m.mu.Lock()
	var removed *Target
	if i := slices.IndexFunc(m.consents, func(c consent) bool {
		return c.peer == p && (version == 0 || c.TargetSoftwareVersion == version)
	}); i >= 0 {
		t := m.consents[i].Target
		removed = &t
		m.consents = slices.Delete(m.consents, i, i+1)
	} else {
		m.log.Info("ota.manager.no_consent", slog.String("node", peerString(p)))
	}
	m.mu.Unlock()
	if removed != nil {
		m.emit(UpdateEvent{Kind: UpdateConsentChanged, Peer: p, Target: *removed})
	}
	m.cancelUpdate(p)
}

// SuppressNextStartUp suppresses the node's next StartUp event on session
// sessionID — suppressNextStartUp (:1239-1241), called by the provider for
// a QueryImage it took for the node's restart.
func (m *Manager) SuppressNextStartUp(p Peer, sessionID uint16) {
	m.mu.Lock()
	m.startUpSuppress[p] = sessionID
	m.mu.Unlock()
}

// OnStatusChange takes a status the provider reports for the node —
// onOtaStatusChange (:1253-1321): Done completes the update and drops
// the consents it covers; Cancelled (a BDX interruption) resets it for a
// retry; any other status moves it on, and Applying arms the reboot
// re-subscription with the apply delay and, the first time, reports the
// update applying.
func (m *Manager) OnStatusChange(p Peer, status Status, version *uint32, applyDelay time.Duration) {
	if fn := m.cfg.OnStatus; fn != nil {
		var v uint32
		if version != nil {
			v = *version
		}
		fn(p, status, v)
	}
	m.mu.Lock()
	if m.suppress {
		m.mu.Unlock()
		return
	}
	i := m.indexOfLocked(p)
	if i < 0 {
		m.mu.Unlock()
		if status != mattercore.OTAUpdateCancelled {
			m.log.Warn("ota.manager.status_unknown_node", slog.String("node", peerString(p)), slog.String("status", status.String()))
		}
		return
	}
	e := m.queue[i]
	switch status {
	case mattercore.OTAUpdateDone:
		completed := e.TargetSoftwareVersion
		if version != nil {
			completed = *version
		}
		m.queue = slices.Delete(m.queue, i, i+1)
		delete(m.known, p)
		delete(m.startUpSuppress, p)
		m.consents = slices.DeleteFunc(m.consents, func(c consent) bool { return c.peer == p && c.TargetSoftwareVersion <= completed })
		done := m.cfg.OnUpdateDone
		m.mu.Unlock()
		m.log.Info("ota.manager.done", slog.String("node", peerString(p)))
		if done != nil {
			done(p)
		}
		m.emit(UpdateEvent{Kind: UpdateDone, Peer: p, Target: e.Target})
		m.triggerQueuedUpdate()
	case mattercore.OTAUpdateCancelled:
		e.reset()
		m.mu.Unlock()
		if m.cfg.Armer != nil {
			m.cfg.Armer.Disarm(p)
		}
		m.log.Info("ota.manager.bdx_cancelled_retry", slog.String("node", peerString(p)))
		m.triggerQueuedUpdate()
	default:
		wasApplying := e.status != nil && *e.status == mattercore.OTAUpdateApplying
		e.started = m.clock.Now()
		s := status
		e.status = &s
		e.applyDelay = 0
		if status == mattercore.OTAUpdateApplying {
			e.applyDelay = applyDelay
		}
		applying := m.cfg.OnUpdateApplying
		target := e.Target
		m.mu.Unlock()
		if status == mattercore.OTAUpdateApplying {
			if m.cfg.Armer != nil {
				m.cfg.Armer.Arm(p, applyDelay)
			}
			if !wasApplying {
				if applying != nil {
					applying(p)
				}
				m.emit(UpdateEvent{Kind: UpdateApplying, Peer: p, Target: target})
			}
		}
	}
}

func peerString(p Peer) string {
	return "@" + strconv.Itoa(int(p.FabricIndex)) + ":" + strconv.FormatUint(p.NodeID, 16)
}
