// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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
// OtaUpdateStatus (SoftwareUpdateManager.ts:105-114 at db021da0),
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
// (OtaSoftwareUpdateProviderServer.ts:39, MAX_DELAYED_ACTION_TIME = 86_400
// seconds, clamped at :346-351).
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
	// OtaSoftwareUpdateProviderServer.ts:427-435, default 0).
	ApplyDelay time.Duration
	// RequestConsent asks the user for an update a requestor that cannot
	// consent needs consent for (requestUserConsentForUpdate,
	// OtaSoftwareUpdateProviderServer.ts:382-393); nil answers Unknown,
	// which declines.
	RequestConsent func(ctx context.Context, p Peer, req QueryImageRequest, d AvailableDetails) (ConsentState, time.Duration)

	// OnUpdateAvailable learns an update a node has no consent for
	// (events.updateAvailable, :1421-1422); the host grants it with
	// AddUpdateConsent.
	OnUpdateAvailable func(p Peer, info SoftwareUpdateInfo)
	// OnUpdateApplying, OnUpdateDone and OnUpdateFailed are
	// events.updateApplying, updateDone and updateFailed (:1424-1438); [Manager.Observe] reports them too.
	OnUpdateApplying, OnUpdateDone, OnUpdateFailed func(p Peer)
	// OnStatus observes each status the provider reports for a node. A
	// download's WaitForApply is reported on the receive path, when the
	// node's final BlockAckEof arrives, so it must not block and must not
	// call back into the controller synchronously.
	OnStatus func(p Peer, status Status, version uint32)

	// ScriptQueryImage and ScriptApplyUpdate, when set, are asked before
	// the provider answers a QueryImage or an ApplyUpdateRequest; ok has
	// the provider answer as the script says (script.go,
	// [QueryImageScript], [ApplyUpdateScript]), and not ok leaves the
	// answer to the provider. OnProviderExchange is told each command the
	// provider answered, with its answer. A test harness's seam: matter.js
	// keeps all three in its cert harness (support/chip-testing/src/cert/
	// InProcessControllerAdapter.ts:610-792 at db021da0).
	ScriptQueryImage   func(p Peer, req QueryImageRequest) (QueryImageScript, bool)
	ScriptApplyUpdate  func(p Peer, req provdef.ApplyUpdateRequestRequest) (ApplyUpdateScript, bool)
	OnProviderExchange func(x ProviderExchange)

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

// queueEntry is UpdateQueueEntry (SoftwareUpdateManager.ts:114-118,
// :133-140 at b25ed960).
type queueEntry struct {
	consent
	endpoint uint16
	// started is when the entry left the queue for the node — announced
	// or linked to an attempt; zero while queued.
	started time.Time
	// attempt is the update attempt the entry follows — UpdatePhase
	// "progress" (:118), linked by identity (#linkAttempt :2074-2089,
	// #linkedEntry :2059-2065); nil while the entry follows none. Its
	// status, activity and apply delay are the entry's.
	attempt *mattercore.OTAUpdateAttempt
	// status is the entry's own status while it follows no attempt:
	// Unknown once reset, or a status the deprecated
	// [Manager.OnStatusChange] reported; nil while undefined.
	status *Status
	// transferred is lastBdxTransferredBytes.
	transferred *uint64
	// applyDelay is the delay of an Applying update the deprecated
	// [Manager.OnStatusChange] reported.
	applyDelay time.Duration
	// transferStarted is whether [UpdateTransferStarted] was emitted for
	// the entry; reset leaves it, so a retry does not emit it again.
	// Go-only: matter.js keeps no such mark.
	transferStarted bool
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
	// attempts are the nodes' update attempts — internal.attempts
	// (SoftwareUpdateManager.ts:1739-1741 at b25ed960) — under m.mu with
	// the queue, as matter.js changes both in one synchronous change
	// (:536-560).
	attempts *mattercore.OTAUpdateAttempts
	// post is what a change under m.mu leaves to run once m.mu is
	// released — #mutate's queued events (:543-560); unlock runs it.
	post []func()

	checkTimer, queueTimer clock.Timer
	announcements          *announcements
	closed                 bool
	work                   sync.WaitGroup
	// life is the context of the update checks the timers run, cancelled
	// by Close, which also waits for them (review II, R2-P6-conc-1907).
	life     context.Context
	stopLife context.CancelFunc
	// check is the timers' update check; nil is checkForUpdates. A test
	// seam.
	check func(ctx context.Context) error
	// query stands in for queryUpdates; nil is queryUpdates. A test seam:
	// a product check that fails needs a commissioned node.
	query func(ctx context.Context, only *client.Node, includeStored bool) ([]PeerUpdate, bool, error)
	// obs are the observers of [Manager.Observe], with their own lock.
	obs observers
	// bdxSession, when set, stands in for the BDX responder's session
	// lookup — a test seam (manager_bdx_parity_matterjs_test.go), as
	// matter.js's OtaTest replaces BdxProtocol.sessionFor; set before the
	// manager runs, nil in production.
	bdxSession func(p Peer) bdxTransfer
	// admitTransfer and disableTransfer, when set, stand in for the BDX
	// responder's session lookup and forced close in the admission of a
	// query — test seams, as OtaTest replaces BdxProtocol.sessionFor and
	// disablePeerForScope (OtaTest.ts:5455-5465 at b25ed960).
	admitTransfer   func(p Peer) admissionTransfer
	disableTransfer func(p Peer) error
}

// admissionTransfer is what the admission of a query reads of a node's
// running transfer, a *bdx.ServerSession.
type admissionTransfer interface {
	Peer() bdx.Peer
	LastActivity() time.Time
}

// bdxTransfer is what the manager reads of a node's running BDX transfer,
// a *bdx.ServerSession: matter.js reads the session's transferredBytes
// and dataLength (#isTransferring, queuedUpdates,
// SoftwareUpdateManager.ts:293-336 at db021da0).
type bdxTransfer interface {
	Transferred() uint64
	Negotiated() (bdx.Result, bool)
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
		attempts: mattercore.NewOTAUpdateAttempts(cfg.Clock),
	}
	m.life, m.stopLife = context.WithCancel(context.Background())
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
	m.checkTimer = m.clock.AfterFunc(delay, func() { m.goWork(m.initializeUpdateCheck) })
}

// initializeUpdateCheck is #initializeUpdateCheck (:261-271). It runs, as
// every timer's check, on a goroutine Close cancels and waits for.
func (m *Manager) initializeUpdateCheck() {
	if err := m.runCheck(); err != nil {
		m.log.Info("ota.manager.initial_check_failed", slog.Any("err", err))
	}
	m.scheduleCheck()
}

// runCheck is a timer's update check under the manager's life: the
// failures of the products whose check failed (review II,
// R2-P6-err-1912).
func (m *Manager) runCheck() error {
	if m.check != nil {
		return m.check(m.life)
	}
	return m.checkForUpdates(m.life)
}

func (m *Manager) scheduleCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.checkTimer = m.clock.AfterFunc(m.cfg.UpdateCheckInterval, func() {
		m.goWork(func() {
			if err := m.runCheck(); err != nil {
				m.log.Info("ota.manager.check_failed_products", slog.Any("err", err))
			}
			m.scheduleCheck()
		})
	})
}

// Close takes the manager offline — #nodeGoingOffline and the disposal
// (:232-244, :1323-1329): no update work starts any more, the timers and
// the announcements stop. A running update check a timer started is
// cancelled — its download included — and waited for, so nothing is
// stored after Close returns (review II, R2-P6-conc-1907).
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
	// #endAllAttempts (:1229-1237, :309 at b25ed960): the attempts end;
	// an entry that followed one keeps following it.
	for _, at := range m.attempts.All() {
		at.End()
	}
	m.unlock()
	m.stopLife()
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
// (SoftwareUpdateManager.ts:714-760 at db021da0). The controller feeds
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

// SetProvider names the provider's node id and endpoint, once the
// controller has built its provider (controller/ota.go openOTA). It may
// be called at any time; the announcements read the values it set under
// the manager's lock (review II, R2-P6-conc-1913).
func (m *Manager) SetProvider(nodeID uint64, endpoint uint16) {
	m.mu.Lock()
	m.cfg.ProviderNodeID, m.cfg.ProviderEndpoint = nodeID, endpoint
	m.mu.Unlock()
}

// provider is the provider's node id and endpoint SetProvider set.
func (m *Manager) provider() (nodeID uint64, endpoint uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.ProviderNodeID, m.cfg.ProviderEndpoint
}

// bdxResponder is the BDX responder SetBDX set; nil for none.
func (m *Manager) bdxResponder() *bdx.Responder {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.BDX
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
	// The failed apply is judged before anything resets the entry, so
	// #failUpdate still reaches the attempt the entry follows
	// (SoftwareUpdateManager.ts:1061-1069 at b25ed960; the reset comes only
	// on the path that neither fails nor completes, :1082-1087).
	previous := e.statusOf()
	if newVersion < e.TargetSoftwareVersion && isStartUp && previous != nil &&
		(*previous == mattercore.OTAUpdateApplying || *previous == mattercore.OTAUpdateWaitForApply) {
		m.log.Warn("ota.manager.apply_failed_reboot", slog.String("node", peerString(p)),
			slog.Uint64("version", uint64(newVersion)), slog.Uint64("target", uint64(e.TargetSoftwareVersion)))
		m.failAppliedLocked(i)
		return
	}
	if !e.started.IsZero() {
		m.log.Info("ota.manager.version_change_clears_progress", slog.String("node", peerString(p)))
		e.reset()
	}
	if newVersion < e.TargetSoftwareVersion {
		m.mu.Unlock()
		return
	}
	m.log.Info("ota.manager.updated", slog.String("node", peerString(p)), slog.Uint64("version", uint64(newVersion)))
	m.completeUpdateLocked(i, newVersion)
	m.unlock()
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
	// :1104-1112 at b25ed960: the linked attempt's status, activity and
	// apply delay; an entry the deprecated OnStatusChange moved, its own.
	since, delay := e.started, e.applyDelay
	if e.attempt != nil {
		since, delay = e.attempt.LastActivity(), e.attempt.ApplyDelay()
	}
	if s := e.statusOf(); s == nil || *s != mattercore.OTAUpdateApplying || e.started.IsZero() ||
		m.clock.Now().Before(since.Add(peer.RebootReturnTimeout(delay))) || version >= e.TargetSoftwareVersion {
		m.mu.Unlock()
		return
	}
	m.log.Warn("ota.manager.apply_failed_resubscribed", slog.String("node", peerString(p)), slog.Uint64("version", uint64(version)))
	m.failAppliedLocked(i)
}

// failAppliedLocked is #failUpdate (:1148-1174 at b25ed960) of the entry
// at i; it unlocks m.mu.
func (m *Manager) failAppliedLocked(i int) {
	m.failUpdateLocked(m.queue[i])
	m.unlock()
}

// failUpdateLocked fails the entry's update: the attempt it follows is
// Cancelled when it applies and ends (:1152-1157 at b25ed960), and the
// entry leaves the queue with updateFailed. matter.js keeps the entry
// queued as failed while its consent lasts (failedAt, #retarget,
// :1158-1173); the module's queue drops it, as before the attempts.
// m.mu is held.
func (m *Manager) failUpdateLocked(e *queueEntry) {
	if a := e.attempt; a != nil {
		if a.Status() == mattercore.OTAUpdateApplying {
			a.SetStatus(mattercore.OTAUpdateCancelled, 0)
		}
		a.End()
	}
	i := slices.Index(m.queue, e)
	if i < 0 {
		return
	}
	p, target := e.peer, e.Target
	m.queue = slices.Delete(m.queue, i, i+1)
	delete(m.startUpSuppress, p)
	failed := m.cfg.OnUpdateFailed
	m.post = append(m.post, func() {
		if failed != nil {
			failed(p)
		}
		m.emit(UpdateEvent{Kind: UpdateFailed, Peer: p, Target: target, Err: ErrNotApplied})
		m.triggerQueuedUpdate()
	})
}

// completeUpdateLocked removes the entry at i as done, with the consents
// at or below completed, and ends the node's attempts up to that version
// (#completeUpdate, SoftwareUpdateManager.ts:1120-1140 at b25ed960). m.mu
// is held.
func (m *Manager) completeUpdateLocked(i int, completed uint32) {
	e := m.queue[i]
	p := e.peer
	m.queue = slices.Delete(m.queue, i, i+1)
	delete(m.known, p)
	delete(m.startUpSuppress, p)
	m.consents = slices.DeleteFunc(m.consents, func(c consent) bool { return c.peer == p && c.TargetSoftwareVersion <= completed })
	for _, a := range m.attempts.Of(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}) {
		if v, ok := a.Version(); ok && v <= completed {
			m.attempts.EndAttempt(a)
		}
	}
	done := m.cfg.OnUpdateDone
	m.post = append(m.post, func() {
		if done != nil {
			done(p)
		}
		m.emit(UpdateEvent{Kind: UpdateDone, Peer: p, Target: e.Target})
		m.triggerQueuedUpdate()
	})
}

// reset returns the entry to the queue: it follows no attempt any more
// (#resetEntryProgress, :475-480 at b25ed960).
func (e *queueEntry) reset() {
	e.started = time.Time{}
	e.attempt = nil
	unknown := mattercore.OTAUpdateUnknown
	e.status = &unknown
	e.transferred = nil
	e.applyDelay = 0
}

// statusOf is the entry's status: its attempt's while it follows one.
func (e *queueEntry) statusOf() *Status {
	if e.attempt != nil {
		s := e.attempt.Status()
		return &s
	}
	return e.status
}

// stalled reports whether the entry shows no activity for the progress
// timeout: its attempt's staleness (#isStalled, :464-473 at b25ed960), or
// the entry's own time plus the apply delay the deprecated OnStatusChange
// reported.
func (e *queueEntry) stalled(now time.Time) bool {
	if e.attempt != nil {
		return e.attempt.IsStale(now)
	}
	return e.started.Add(e.applyDelay + ProgressTimeout).Before(now)
}

// unlock ends a change of the manager's state: the attempts the rules
// ended reset the entries that followed them (#endAttempt, :1216-1227 at
// b25ed960), m.mu is released, and what the change left to run — its
// events, the next queue run — runs (#mutate, :543-560).
func (m *Manager) unlock() {
	for _, a := range m.attempts.TakeEnded() {
		if m.suppress {
			continue
		}
		if e := m.linkedEntryLocked(a); e != nil {
			m.log.Info("ota.manager.attempt_ended_reset", slog.String("node", peerString(e.peer)), slog.String("attempt", a.String()))
			e.reset()
		}
	}
	post := m.post
	m.post = nil
	m.mu.Unlock()
	for _, fn := range post {
		fn()
	}
}

// entryForLocked is the node's queue entry, nil for none.
func (m *Manager) entryForLocked(p mattercore.OTAPeer) *queueEntry {
	if i := m.indexOfLocked(peerOf(p)); i >= 0 {
		return m.queue[i]
	}
	return nil
}

// linkedEntryLocked is the node's queue entry while it follows the
// attempt (#linkedEntry, SoftwareUpdateManager.ts:2059-2065 at b25ed960).
func (m *Manager) linkedEntryLocked(a *mattercore.OTAUpdateAttempt) *queueEntry {
	if e := m.entryForLocked(a.Peer()); e != nil && e.attempt == a {
		return e
	}
	return nil
}

// linkAttemptLocked links the node's queue entry to the attempt and
// returns it: only an answered attempt that offers the entry's target is
// linked, and a newly linked attempt replaces the one before.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:2067-2089 at b25ed960
// (#linkAttempt).
func (m *Manager) linkAttemptLocked(a *mattercore.OTAUpdateAttempt) *queueEntry {
	e := m.entryForLocked(a.Peer())
	if e == nil || !a.Answered() {
		return nil
	}
	if v, ok := a.Version(); !ok || v != e.TargetSoftwareVersion {
		return nil
	}
	if e.attempt == a {
		return e
	}
	e.attempt = a
	e.status = nil
	e.transferred = nil
	e.applyDelay = 0
	e.started = m.clock.Now()
	return e
}

// reportAttemptStatusLocked records a status of the attempt and moves the
// queue entry linked to it: Cancelled resets it for a retry, Applying arms
// the restart's fast resubscription and reports the update applying once.
// Every status also reaches [Config].OnStatus. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:2021-2050 at b25ed960
// (#reportAttemptStatus).
func (m *Manager) reportAttemptStatusLocked(a *mattercore.OTAUpdateAttempt, status Status, applyDelay time.Duration) {
	wasApplying := a.Status() == mattercore.OTAUpdateApplying
	a.SetStatus(status, applyDelay)
	p := peerOf(a.Peer())
	version, _ := a.Version()
	if fn := m.cfg.OnStatus; fn != nil {
		m.post = append(m.post, func() { fn(p, status, version) })
	}
	if m.suppress {
		return
	}
	var e *queueEntry
	if status == mattercore.OTAUpdateCancelled {
		e = m.linkedEntryLocked(a)
	} else {
		e = m.linkAttemptLocked(a)
	}
	if e == nil {
		return
	}
	target := e.Target
	switch status {
	case mattercore.OTAUpdateCancelled:
		m.log.Info("ota.manager.bdx_cancelled_retry", slog.String("node", peerString(p)))
		e.reset() // #retryUpdate (:585-588)
		armer := m.cfg.Armer
		m.post = append(m.post, func() {
			if armer != nil {
				armer.Disarm(p)
			}
			m.triggerQueuedUpdate()
		})
	case mattercore.OTAUpdateApplying:
		// Armed under m.mu with the entry still queued, so a NodeDeleted
		// that removes it disarms after (review II, R2-P6-bound-1914).
		if m.cfg.Armer != nil {
			m.cfg.Armer.Arm(p, applyDelay)
		}
		if !wasApplying {
			applying := m.cfg.OnUpdateApplying
			m.post = append(m.post, func() {
				if applying != nil {
					applying(p)
				}
				m.emit(UpdateEvent{Kind: UpdateApplying, Peer: p, Target: target})
			})
		}
	case mattercore.OTAUpdateDownloading:
		// Go-only: the entry's first transfer reports [UpdateTransferStarted].
		if !e.transferStarted {
			e.transferStarted = true
			m.post = append(m.post, func() { m.emit(UpdateEvent{Kind: UpdateTransferStarted, Peer: p, Target: target}) })
		}
	default:
		// Any other status only moves the attempt.
	}
}

// bdxSessionFor is #bdxSessionFor (:322-328); nil when no transfer runs.
func (m *Manager) bdxSessionFor(p Peer) bdxTransfer {
	return m.bdxSessionWith(p, m.bdxResponder())
}

// bdxSessionForLocked is bdxSessionFor with m.mu held.
func (m *Manager) bdxSessionForLocked(p Peer) bdxTransfer {
	return m.bdxSessionWith(p, m.cfg.BDX)
}

func (m *Manager) bdxSessionWith(p Peer, r *bdx.Responder) bdxTransfer {
	if m.bdxSession != nil {
		return m.bdxSession(p)
	}
	if r == nil {
		return nil
	}
	s := r.SessionFor(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota")
	if s == nil {
		return nil // no typed nil in the interface
	}
	return s
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

func transferredOf(s bdxTransfer) *uint64 {
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
		case e.stalled(now) && !isTransferring(&e, transferredOf(s)):
			status = PendingStalled
		default:
			status = PendingInProgress
		}
		u := PendingUpdate{
			Peer: e.peer, VendorID: e.VendorID, ProductID: e.ProductID, TargetSoftwareVersion: e.TargetSoftwareVersion,
			Status: status, LastProgressStatus: e.statusOf(), LastProgressUpdateTime: e.started,
		}
		if e.attempt != nil {
			u.LastProgressUpdateTime = e.attempt.LastActivity()
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

// validatePeerDetails is #validatePeerDetails (:360-379). Beside it, a
// query whose ids are not the node's attested ones is logged
// ([Manager.noteAttestedIdentity]); the answer does not depend on it.
func (m *Manager) validatePeerDetails(p Peer, version uint32, vid, pid uint16) bool {
	n := m.nodeAt(p)
	if n == nil {
		return false
	}
	m.noteAttestedIdentity(p, n.Record(), vid, pid)
	st, ok := basicInformation(n)
	return ok && st.SoftwareVersion == version && st.VendorId == vid && st.ProductId == pid
}

// noteAttestedIdentity logs, as one Warn line, a QueryImage whose vendor
// and product id differ from the ids the node's attestation validated when
// it was commissioned (client.Record.AttestedVendorID/AttestedProductID,
// review II decision 105). A query the manager answers carries the node's
// BasicInformation ids, so the line means the node reports another
// identity than the one its Certification Declaration was checked
// against. Nothing is refused on it. A node whose attested ids are
// unknown is not judged. A white-label product is not flagged for its
// DAC's ids: the attested ids are its BasicInformation ones, which the
// declaration lists. A product id the same declaration also lists, other
// than the attested one, is flagged: the declaration's product list is not
// kept.
func (m *Manager) noteAttestedIdentity(p Peer, rec client.Record, vid, pid uint16) {
	av, ap := rec.AttestedVendorID, rec.AttestedProductID
	if av == nil || ap == nil || (*av == vid && *ap == pid) {
		return
	}
	m.log.Warn("ota.manager.attested_identity_mismatch", slog.String("node", peerString(p)),
		slog.Int("attested_vendor_id", int(*av)), slog.Int("attested_product_id", int(*ap)),
		slog.Int("vendor_id", int(vid)), slog.Int("product_id", int(pid)))
}

// UpdateExistsFor is the stored update the querying node gets —
// updateExistsFor (:385-468): the node's details must match the query;
// the applicable stored images over its version (prod ones, the others
// only with AllowTestImages), highest first; a requestor that can consent
// gets the first, flagged consent-required unless the manager holds its
// consent; one that cannot only a version the manager holds a consent
// for. A stored image the store would not serve ([Images.Open]: a prod or
// test image not bound to a ledger checksum, unless
// [ImagesConfig].AllowUnverifiedLedgerImages) is no candidate.
func (m *Manager) UpdateExistsFor(ctx context.Context, p Peer, req provdef.QueryImageRequest) (AvailableDetails, bool) {
	if !m.validatePeerDetails(p, req.SoftwareVersion, req.VendorId, req.ProductId) {
		m.log.Info("ota.manager.peer_details_mismatch", slog.String("node", peerString(p)))
		return AvailableDetails{}, false
	}
	candidates := m.storedCandidates(ctx, req.VendorId, req.ProductId, req.SoftwareVersion)
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

// storedCandidates are UpdateExistsFor's candidates: the stored images
// of vid/pid applicable over version, prod ones and test ones only with
// AllowTestImages, that the store serves, highest version first.
func (m *Manager) storedCandidates(ctx context.Context, vid, pid uint16, version uint32) []StoredImage {
	var candidates []StoredImage
	for _, st := range m.cfg.Images.Find(ctx, FindOptions{VendorID: &vid, ProductID: &pid}) {
		lo, hi := uint32(0), version
		if st.MinApplicable != nil {
			lo = *st.MinApplicable
		}
		if st.MaxApplicable != nil {
			hi = *st.MaxApplicable
		}
		if version < st.SoftwareVersion && version >= lo && version <= hi && (st.Mode == ModeProd || m.cfg.AllowTestImages) &&
			m.cfg.Images.servable(ctx, st.Filename) {
			candidates = append(candidates, st)
		}
	}
	slices.SortStableFunc(candidates, func(a, b StoredImage) int { return cmp.Compare(b.SoftwareVersion, a.SoftwareVersion) })
	return candidates
}

// CheckForUpdates checks every node for updates, stored images included,
// and purges the stored images no node needs — checkForUpdates (:471-474).
// A product whose check failed is logged and passed over, the others are
// still checked and the purge still runs; the error joins the failures of
// those products (errors.Join), as [Manager.QueryUpdates] returns them,
// and is nil only when every product's check succeeded (review II,
// R2-P6-err-1912).
func (m *Manager) CheckForUpdates(ctx context.Context) error {
	return m.checkForUpdates(ctx)
}

// checkForUpdates is CheckForUpdates.
func (m *Manager) checkForUpdates(ctx context.Context) error {
	_, failed := m.queryUpdatesAndReport(ctx, nil, true)
	m.cleanupObsoleteUpdates(ctx)
	return failed
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
			if n, err := m.cfg.Images.Delete(ctx, DeleteOptions{Filename: st.Filename}); err == nil && n > 0 {
				m.log.Info("ota.manager.cleaned_test_image", slog.String("file", st.Filename))
			}
			continue
		}
		v, ok := versions[key{st.VendorID, st.ProductID}]
		if !ok {
			continue
		}
		if st.SoftwareVersion <= v {
			if n, err := m.cfg.Images.Delete(ctx, DeleteOptions{Filename: st.Filename}); err == nil && n > 0 {
				m.log.Info("ota.manager.cleaned_obsolete_image", slog.String("file", st.Filename))
			}
		}
	}
}

// QueryUpdates checks every node, or only, for updates over the ledgers
// and — with includeStored — the stored images, and returns the nodes it
// found one for — queryUpdates (:559-623). A check the manager ran (it
// was not suppressed) ends with an [UpdateCheckFinished] event. A product
// whose check failed is logged and passed over and the others are still
// checked: the nodes returned are those of the products that succeeded,
// and the error joins the failures, one [*ProductCheckError] per product
// naming its nodes (errors.Join, [ProductCheckErrors]; the event's Err is
// the same error). It is nil only when every product's check succeeded, so a
// caller that treats any error as fatal discards the updates found
// besides (review II, R2-P6-err-1912).
func (m *Manager) QueryUpdates(ctx context.Context, only *client.Node, includeStored bool) ([]PeerUpdate, error) {
	return m.queryUpdatesAndReport(ctx, only, includeStored)
}

// queryUpdatesAndReport is QueryUpdates.
func (m *Manager) queryUpdatesAndReport(ctx context.Context, only *client.Node, includeStored bool) ([]PeerUpdate, error) {
	query := m.queryUpdates
	if m.query != nil {
		query = m.query
	}
	out, ran, failed := query(ctx, only, includeStored)
	if ran {
		ev := UpdateEvent{Kind: UpdateCheckFinished, Updates: slices.Clone(out), Err: failed}
		if only != nil {
			ev.Peer, _ = only.PeerAddress()
		}
		m.emit(ev)
	}
	return out, failed
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
	nodes := m.cfg.Registry.Nodes()
	if only == nil {
		m.forgetGone(nodes)
	}
	for _, n := range nodes {
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
			failed = errors.Join(failed, productCheckFailed(g.vid, g.pid, g.version, g.targets, err))
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

// forgetGone drops what the manager keeps per node for every node that
// is no longer one of nodes, the registry's, and has no queued update: the
// version it observed, the apply delay, the update it found, a startUp to
// suppress, its consents. A check of every node runs it, so what a node
// that left the registry leaves behind lasts until the next check (review
// II, R2-P6-bound-1914).
func (m *Manager) forgetGone(nodes []*client.Node) {
	keep := make(map[Peer]bool, len(nodes))
	for _, n := range nodes {
		if p, ok := n.PeerAddress(); ok {
			keep[p] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.queue {
		keep[e.peer] = true
	}
	maps.DeleteFunc(m.observed, func(p Peer, _ uint32) bool { return !keep[p] })
	for _, a := range m.attempts.All() {
		if !keep[peerOf(a.Peer())] {
			a.End() // #forgetPeer (:333-334 at b25ed960)
		}
	}
	maps.DeleteFunc(m.applyDelays, func(p Peer, _ time.Duration) bool { return !keep[p] })
	maps.DeleteFunc(m.known, func(p Peer, _ SoftwareUpdateInfo) bool { return !keep[p] })
	maps.DeleteFunc(m.startUpSuppress, func(p Peer, _ uint16) bool { return !keep[p] })
	m.consents = slices.DeleteFunc(m.consents, func(c consent) bool { return !keep[c.peer] })
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
	if _, err := m.cfg.Images.DownloadUpdate(ctx, info, false); err != nil {
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
// queued replaces it unless it is in progress. A new entry, and a
// replacement that changes the queued target, report [UpdateQueued]
// (Go-only, matter.js emits nothing here); a periodic re-check that
// queues the same target again does not.
func (m *Manager) queueUpdate(e *queueEntry) {
	m.mu.Lock()
	queued := true
	if i := m.indexOfLocked(e.peer); i >= 0 {
		existing := m.queue[i]
		if !existing.started.IsZero() {
			m.mu.Unlock()
			m.log.Info("ota.manager.in_progress_skip", slog.String("node", peerString(e.peer)))
			return
		}
		queued = existing.Target != e.Target
		existing.Target = e.Target
		existing.endpoint = e.endpoint
	} else {
		m.queue = append(m.queue, e)
	}
	m.mu.Unlock()
	if queued {
		m.emit(UpdateEvent{Kind: UpdateQueued, Peer: e.peer, Target: e.Target})
	}
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
		// #refreshBdxProgress (:453-462 at b25ed960): a moving transfer
		// touches the attempt the entry follows.
		if t := transferredOf(m.bdxSessionForLocked(e.peer)); isTransferring(e, t) {
			e.transferred = t
			if e.attempt != nil {
				e.attempt.Touch()
			} else {
				e.started = now
			}
			continue
		}
		if e.stalled(now) {
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
	if s := e.statusOf(); s != nil && *s == mattercore.OTAUpdateUnknown {
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
		if r := m.bdxResponder(); r != nil {
			_ = r.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
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

// cancelUpdate cancels the node's queued update to version, or to any
// version when version is 0, after its consent went: an update already
// applying can no longer be cancelled; any other leaves the queue with
// updateFailed, its reboot arming lifted. The node's BDX transfer is
// closed when every consent went (version 0) or the update was underway —
// announced or following an attempt — but not while it applies.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1670-1718 at b25ed960
// (removeConsent, the queue): the dequeue decision reads the status of
// the attempt the entry follows (:1700).
func (m *Manager) cancelUpdate(p Peer, version uint32) {
	removesAll := version == 0
	m.mu.Lock()
	i := m.indexOfLocked(p)
	if i < 0 || (!removesAll && m.queue[i].TargetSoftwareVersion != version) {
		m.unlock()
		m.log.Info("ota.manager.cancel_none", slog.String("node", peerString(p)))
		if removesAll {
			m.closeTransfer(p)
		}
		return
	}
	e := m.queue[i]
	// :1700 reads the attempt's status; statusOf is the attempt's while the
	// entry follows one, and the deprecated OnStatusChange's otherwise.
	if s := e.statusOf(); s != nil && *s == mattercore.OTAUpdateApplying {
		m.unlock()
		m.log.Info("ota.manager.cancel_too_late", slog.String("node", peerString(p)))
		return
	}
	underway := !e.started.IsZero()
	target := e.Target
	m.queue = slices.Delete(m.queue, i, i+1)
	delete(m.startUpSuppress, p)
	failed := m.cfg.OnUpdateFailed
	m.unlock()
	if m.cfg.Armer != nil {
		m.cfg.Armer.Disarm(p)
	}
	m.log.Info("ota.manager.cancelled", slog.String("node", peerString(p)))
	if failed != nil {
		failed(p)
	}
	m.emit(UpdateEvent{Kind: UpdateFailed, Peer: p, Target: target, Err: ErrUpdateCancelled})
	m.triggerQueuedUpdate()
	if removesAll || underway {
		m.closeTransfer(p)
	}
}

// closeTransfer disables the node for the "ota" scope and closes its
// transfer (#cancelBdxTransfer, :1239-1250 at b25ed960), outside m.mu: the
// closed transfer reports its status back into the manager.
func (m *Manager) closeTransfer(p Peer) {
	if m.disableTransfer != nil {
		_ = m.disableTransfer(p)
		return
	}
	if r := m.bdxResponder(); r != nil {
		_ = r.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
	}
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
// and cancels its update to that version — removeConsent
// (SoftwareUpdateManager.ts:1639-1723 at b25ed960). The node's offers of
// the version (of any, for 0) lose their apply right, so an
// ApplyUpdateRequest under their token is refused unless the update
// already applies, and its queries that made no offer yet end, answered
// NotAvailable (:1686-1692).
func (m *Manager) RemoveConsent(p Peer, version uint32) {
	m.mu.Lock()
	m.attempts.RemoveConsent(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, version, version == 0)
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
	m.unlock()
	if removed != nil {
		m.emit(UpdateEvent{Kind: UpdateConsentChanged, Peer: p, Target: *removed})
	}
	m.cancelUpdate(p, version)
}

// NodeDeleted forgets the node at p at once, when it left the registry
// (client.Hooks.Deleted): its consents (each reported as
// [UpdateConsentChanged]), its queued update whatever its state (reported
// as [UpdateFailed] with [ErrUpdateCancelled]), its BDX scope (a transfer
// that runs is closed), its reboot arming, and what the manager observed
// of it — the scope and the arming whether or not an update was queued. A later
// node at the same address starts with nothing of it. A node the manager
// never saw is no error. The next full check drops the same for a node
// whose deletion was not reported (review II, R2-P6-bound-1914).
func (m *Manager) NodeDeleted(p Peer) {
	m.mu.Lock()
	var removed []Target
	m.consents = slices.DeleteFunc(m.consents, func(c consent) bool {
		if c.peer != p {
			return false
		}
		removed = append(removed, c.Target)
		return true
	})
	var cancelled *Target
	if i := m.indexOfLocked(p); i >= 0 {
		t := m.queue[i].Target
		cancelled = &t
		m.queue = slices.Delete(m.queue, i, i+1)
	}
	delete(m.observed, p)
	delete(m.applyDelays, p)
	delete(m.known, p)
	delete(m.startUpSuppress, p)
	// #forgetPeer (:333-334 at b25ed960): the node's attempts end.
	for _, a := range m.attempts.Of(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}) {
		a.End()
	}
	failed := m.cfg.OnUpdateFailed
	m.unlock()
	// The scope and the arming go whether or not an update is queued: the
	// provider enables the scope and a status arms the peer on their own.
	if r := m.bdxResponder(); r != nil {
		_ = r.DisablePeerForScope(bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, "ota", true)
	}
	if m.cfg.Armer != nil {
		m.cfg.Armer.Disarm(p)
	}
	for _, t := range removed {
		m.emit(UpdateEvent{Kind: UpdateConsentChanged, Peer: p, Target: t})
	}
	if cancelled == nil {
		return
	}
	m.log.Info("ota.manager.node_deleted", slog.String("node", peerString(p)))
	if failed != nil {
		failed(p)
	}
	m.emit(UpdateEvent{Kind: UpdateFailed, Peer: p, Target: *cancelled, Err: ErrUpdateCancelled})
	m.triggerQueuedUpdate()
}

// SuppressNextStartUp suppresses the node's next StartUp event on session
// sessionID — #suppressNextStartUp (SoftwareUpdateManager.ts:1725-1731 at
// b25ed960).
//
// Deprecated: the manager suppresses the startUp itself when it admits a
// query for the node's restart while an update of the node is queued
// (admitQuery, :1873-1879); removable in v0.26.0.
func (m *Manager) SuppressNextStartUp(p Peer, sessionID uint16) {
	m.mu.Lock()
	m.startUpSuppress[p] = sessionID
	m.mu.Unlock()
}

// OnStatusChange takes a status an OTA provider reports for the node
// without an update attempt — the onOtaStatusChange of matter.js before
// the attempts. While the node's queued update follows an attempt, the
// status is the attempt's: Done records a NotifyUpdateApplied of its
// token with version (the update's target when nil), and any other status
// is reported on the attempt (reportAttemptStatus). An update that
// follows none moves as before: Done completes it and drops the consents
// it covers; Cancelled resets it for a retry; any other status moves it
// on, and Applying arms the reboot re-subscription with the apply delay
// and, the first time, reports the update applying; the entry's first
// Downloading reports [UpdateTransferStarted].
//
// Deprecated: the provider reports to the manager's attempts through
// [ProviderConfig].Manager (mattercore.OTAUpdateManager); removable in
// v0.26.0.
func (m *Manager) OnStatusChange(p Peer, status Status, version *uint32, applyDelay time.Duration) {
	m.mu.Lock()
	if i := m.indexOfLocked(p); !m.suppress && i >= 0 {
		if a := m.queue[i].attempt; a != nil && !a.Ended() {
			if status == mattercore.OTAUpdateDone {
				v := m.queue[i].TargetSoftwareVersion
				if version != nil {
					v = *version
				}
				m.updateAppliedLocked(a.Peer(), a.Token(), v)
			} else {
				m.reportAttemptStatusLocked(a, status, applyDelay)
			}
			m.unlock()
			return
		}
	}
	m.mu.Unlock()
	m.legacyStatus(p, status, version, applyDelay)
}

// legacyStatus is [Manager.OnStatusChange] for an update that follows no
// attempt.
func (m *Manager) legacyStatus(p Peer, status Status, version *uint32, applyDelay time.Duration) {
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
		m.log.Info("ota.manager.done", slog.String("node", peerString(p)))
		m.completeUpdateLocked(i, completed)
		m.unlock()
	case mattercore.OTAUpdateCancelled:
		e.reset()
		m.mu.Unlock()
		if m.cfg.Armer != nil {
			m.cfg.Armer.Disarm(p)
		}
		m.log.Info("ota.manager.bdx_cancelled_retry", slog.String("node", peerString(p)))
		m.triggerQueuedUpdate()
	default:
		m.progressLocked(p, e, status, applyDelay)
	}
}

// updateAppliedLocked records the node's NotifyUpdateApplied under token:
// the attempt of the token ends Done, linked to the node's queued update
// first; the update is done at or above its target, retried below it when
// it followed that attempt, and left as it is otherwise. Every
// NotifyUpdateApplied reaches [Config].OnStatus as Done with the version.
// m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1958-2004 at b25ed960
// (updateApplied, #updateApplied).
func (m *Manager) updateAppliedLocked(op mattercore.OTAPeer, token []byte, softwareVersion uint32) {
	p := peerOf(op)
	if fn := m.cfg.OnStatus; fn != nil {
		m.post = append(m.post, func() { fn(p, mattercore.OTAUpdateDone, softwareVersion) })
	}
	var linked *queueEntry
	if a := m.attempts.Find(op, token); a != nil && !m.suppress {
		linked = m.linkAttemptLocked(a)
	}
	m.attempts.UpdateApplied(op, token)
	if m.suppress {
		return
	}
	i := m.indexOfLocked(p)
	if i < 0 {
		return
	}
	e := m.queue[i]
	switch {
	case softwareVersion >= e.TargetSoftwareVersion:
		m.log.Info("ota.manager.done", slog.String("node", peerString(p)))
		m.completeUpdateLocked(i, softwareVersion)
	case linked == e:
		m.log.Info("ota.manager.applied_below_target_retry", slog.String("node", peerString(p)), slog.Uint64("version", uint64(softwareVersion)))
		e.reset() // #retryUpdate (:585-588)
		m.post = append(m.post, m.triggerQueuedUpdate)
	default:
		m.log.Debug("ota.manager.applied_below_target_ignored", slog.String("node", peerString(p)), slog.Uint64("version", uint64(softwareVersion)))
	}
}

// progressLocked is onOtaStatusChange's last branch (:1296-1320): the
// entry takes the status and its time; the first Downloading reports the
// transfer started, Applying arms the reboot re-subscription and, the
// first time, reports the update applying. It is called with m.mu held
// and releases it.
func (m *Manager) progressLocked(p Peer, e *queueEntry, status Status, applyDelay time.Duration) {
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
	transferStarted := status == mattercore.OTAUpdateDownloading && !e.transferStarted
	if transferStarted {
		e.transferStarted = true
	}
	// The arming happens under m.mu, with the entry it is for still queued:
	// a NodeDeleted that removes the entry disarms after it, so a deleted
	// peer is never left armed (review II, R2-P6-bound-1914). The armer runs
	// no callback under its own lock.
	if status == mattercore.OTAUpdateApplying && m.cfg.Armer != nil {
		m.cfg.Armer.Arm(p, applyDelay)
	}
	m.mu.Unlock()
	if transferStarted {
		m.emit(UpdateEvent{Kind: UpdateTransferStarted, Peer: p, Target: target})
	}
	if status != mattercore.OTAUpdateApplying {
		return
	}
	if !wasApplying {
		if applying != nil {
			applying(p)
		}
		m.emit(UpdateEvent{Kind: UpdateApplying, Peer: p, Target: target})
	}
}

func peerString(p Peer) string {
	return "@" + strconv.Itoa(int(p.FabricIndex)) + ":" + strconv.FormatUint(p.NodeID, 16)
}
