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
// OtaUpdateStatus (OtaProgress.ts:16-25 at b25ed960),
// cluster/core's OTAUpdateStatus.
type Status = mattercore.OTAUpdateStatus

// ProgressTimeout is how long an update may go without a status change or
// BDX activity before it counts as stalled — OTA_PROGRESS_TIMEOUT
// (OtaProgress.ts:14 at b25ed960, Minutes(15)), the provider's too.
const ProgressTimeout = 15 * time.Minute

// queueCheckInterval is the period of the stalled-update check
// (#restartQueueTimer, SoftwareUpdateManager.ts:1355-1362 at b25ed960,
// Minutes(5)).
const queueCheckInterval = 5 * time.Minute

// maxApplyDelay is the longest delay an apply is allowed with, a day
// (OtaSoftwareUpdateProviderServer.ts:39 at b25ed960, MAX_DELAYED_ACTION_TIME
// = 86_400 seconds, clamped at OtaSoftwareUpdateProviderServer.ts:346-351 at
// b25ed960).
const maxApplyDelay = 24 * time.Hour

// ErrNotApplicable is a node that is not currently applicable for OTA
// updates — matter.js's OtaUpdateError of forceUpdate
// (SoftwareUpdateManager.ts:1486-1489 at b25ed960).
var ErrNotApplicable = errors.New("softwareupdate: node is not currently applicable for OTA updates")

// ErrNoNode is a peer address no node has: [Manager.AddUpdateConsent] and
// [Manager.ForceUpdate] change nothing for it — matter.js's OtaUpdateError
// of #otaEndpointFor (SoftwareUpdateManager.ts:1574-1578 at b25ed960), as
// for a node removed after the caller looked it up.
// It is an [ErrNotApplicable] too (errors.Is), as matter.js throws the one
// OtaUpdateError for both (C-24).
var ErrNoNode error = noNodeError{}

type noNodeError struct{}

func (noNodeError) Error() string { return "softwareupdate: no node has the peer address" }
func (noNodeError) Unwrap() error { return ErrNotApplicable }

// ErrMismatch is a consent whose vendor or product does not match the
// node — ImplementationError (SoftwareUpdateManager.ts:1584-1586 at
// b25ed960).
var ErrMismatch = errors.New("softwareupdate: node does not match the given vendorId/productId")

// ErrTransferOptions is an invalid transfer option — assertTransferOptions
// (SoftwareUpdateManager.ts:66-76 at b25ed960).
var ErrTransferOptions = errors.New("softwareupdate: invalid transfer options")

// Target names the update a consent applies to and how to transfer it —
// SoftwareUpdateManager.UpdateTarget (SoftwareUpdateManager.ts:2191-2208 at
// b25ed960).
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
// (SoftwareUpdateManager.ts:1526-1536 at b25ed960): 0 and nil leave the
// decision to the peer and its medium.
type TransferOptions struct {
	MaxBlockSize       *uint16
	AdditionalMrpDelay *time.Duration
}

// SoftwareUpdateInfo is an update found for a node — matter.js
// SoftwareUpdateInfo (SoftwareUpdateManager.ts:170-191 at b25ed960).
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
// (SoftwareUpdateManager.ts:193-216 at b25ed960).
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
// (SoftwareUpdateManager.ts:193-216 at b25ed960).
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
// node — OtaUpdateAvailableDetails (SoftwareUpdateManager.ts:161-168 at
// b25ed960).
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
	// carry them (OtaAnnouncements.ts:34-39 at b25ed960, the record
	// OtaAnnouncements.ts:157-161 at b25ed960, the command
	// OtaAnnouncements.ts:263-270 at b25ed960).
	FabricIndex      uint8
	ProviderNodeID   uint64
	ProviderEndpoint uint16
	// VendorID is our fabric's vendor, the announcement's
	// (OtaAnnouncements.ts:263-270 at b25ed960).
	VendorID uint16
	// BDX is the provider's BDX responder: the manager reads a transfer's
	// liveness and closes a stale one (#bdxSessionFor,
	// SoftwareUpdateManager.ts:432-438 at b25ed960); nil
	// knows no transfer.
	BDX *bdx.Responder
	// Armer re-subscribes a node that restarts after it applied
	// (#rebootResubscribeArmer, SoftwareUpdateManager.ts:378-386 at
	// b25ed960); nil arms nothing.
	Armer *peer.RebootResubscribeArmer
	// SessionID is the id of the newest session with a peer — the
	// fallback of the startUp suppression's session
	// (SoftwareUpdateManager.ts:1045-1047 at b25ed960); nil knows
	// none.
	SessionID func(p Peer) (uint16, bool)

	// AllowTestImages also offers test-net and local images
	// (State.allowTestOtaImages, SoftwareUpdateManager.ts:2144 at b25ed960,
	// default false).
	AllowTestImages bool
	// UpdateCheckInterval is the period of the update check
	// (State.updateCheckInterval, default 24 h); 0 is the default.
	UpdateCheckInterval time.Duration
	// DelegateConsentToRequestor offers a node whose QueryImage says it can
	// obtain the user's consent itself (RequestorCanConsent) the newest
	// applicable stored image without a consent given to the manager, with
	// UserConsentNeeded, and accepts its apply (State.delegateConsentToRequestor,
	// SoftwareUpdateManager.ts:2158-2171 at b25ed960, default false). Off,
	// such a node is offered only a version the manager holds a consent for.
	DelegateConsentToRequestor bool
	// AnnounceAsDefaultProvider writes the provider as every node's
	// default provider (State.announceAsDefaultProvider, default false).
	AnnounceAsDefaultProvider bool
	// AnnouncementInterval is the period of that write, at least 24 h
	// (State.announcementInterval); 0 is 24 h.
	AnnouncementInterval time.Duration
	// MaxBDXBlockSize and BDXAdditionalMrpDelay are the default transfer
	// options (State.maxBdxBlockSize, bdxAdditionalMrpDelay,
	// SoftwareUpdateManager.ts:2173-2188 at b25ed960).
	MaxBDXBlockSize       uint16
	BDXAdditionalMrpDelay *time.Duration
	// ApplyDelay is how long an allowed apply waits, rounded up to whole
	// seconds and at most a day (the provider's State.applyDelay,
	// OtaSoftwareUpdateProviderServer.ts:427-435 at b25ed960, default 0).
	ApplyDelay time.Duration
	// RequestConsent asks the user for an update a requestor that cannot
	// consent needs consent for (requestUserConsentForUpdate,
	// OtaSoftwareUpdateProviderServer.ts:382-393 at b25ed960); nil answers
	// Unknown,
	// which declines.
	RequestConsent func(ctx context.Context, p Peer, req QueryImageRequest, d AvailableDetails) (ConsentState, time.Duration)

	// OnUpdateAvailable learns an update a node has no consent for
	// (events.updateAvailable, SoftwareUpdateManager.ts:2281-2285 at
	// b25ed960); the host grants it with
	// AddUpdateConsent.
	OnUpdateAvailable func(p Peer, info SoftwareUpdateInfo)
	// OnUpdateApplying, OnUpdateDone and OnUpdateFailed are
	// events.updateApplying, updateDone and updateFailed
	// (SoftwareUpdateManager.ts:2287-2309 at b25ed960); [Manager.Observe]
	// reports them too.
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
	// InProcessControllerAdapter.ts:628-814 at b25ed960).
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

// consent is the consent the application gave for a peer, one per peer —
// UpdateConsent (SoftwareUpdateManager.ts:78-85 at b25ed960).
type consent struct {
	Target
	peer Peer
	// failedAt is when the update to the consent's version failed since
	// the consent was given; zero while it did not.
	failedAt time.Time
}

// failedUpdateRetention is how long a failed update keeps its consent and
// queue entry: meanwhile the node may query and apply the update on its
// own, but it is not announced again — FAILED_UPDATE_RETENTION
// (SoftwareUpdateManager.ts:120-124 at b25ed960).
const failedUpdateRetention = 24 * time.Hour

// phaseState is where a queued update stands — UpdatePhase's state
// (SoftwareUpdateManager.ts:87-118 at b25ed960).
type phaseState uint8

const (
	// phaseQueued waits for an announcement; resetAt when it was reset.
	// Not announced while its consent failed.
	phaseQueued phaseState = iota
	// phaseAnnouncing: the announcement runs.
	phaseAnnouncing
	// phaseAnnounced: the node was announced and made no answered attempt
	// since since.
	phaseAnnounced
	// phaseProgress: the node follows attempt (or, without one,
	// the deprecated OnStatusChange moved the entry: legacy).
	phaseProgress
)

func (s phaseState) String() string {
	switch s {
	case phaseQueued:
		return "queued"
	case phaseAnnouncing:
		return "announcing"
	case phaseAnnounced:
		return "announced"
	case phaseProgress:
		return "progress"
	}
	return "unknown"
}

// updatePhase is UpdatePhase (SoftwareUpdateManager.ts:87-118 at
// b25ed960). A phase is replaced, never changed in place, except for the
// liveness of progress: an announcement compares the phase it started with
// by identity (#runAnnouncement, SoftwareUpdateManager.ts:1399-1415 at
// b25ed960).
type updatePhase struct {
	state phaseState
	// resetAt is when a queued entry was reset; zero while it never was.
	resetAt time.Time
	// since is when an announced entry was announced.
	since time.Time
	// attempt is the update attempt an entry in progress follows.
	attempt *mattercore.OTAUpdateAttempt
	// bdxTransferred is the transfer counter last seen in progress.
	bdxTransferred *uint64
	// legacy is the status of an entry in progress without an attempt,
	// which the deprecated [Manager.OnStatusChange] moved there. Go-only
	// (C-16): matter.js has no update without an attempt any more.
	legacy *legacyProgress
}

// legacyProgress is the status the deprecated [Manager.OnStatusChange]
// reported for an entry that follows no attempt, with its apply delay and
// time.
type legacyProgress struct {
	status     Status
	applyDelay time.Duration
	since      time.Time
}

// underway reports an entry the node was announced for or follows an
// attempt for; an announcing entry is not underway yet, so its target may
// still be replaced (isUnderway, SoftwareUpdateManager.ts:126-132 at
// b25ed960).
func (p *updatePhase) underway() bool {
	return p.state == phaseAnnounced || p.state == phaseProgress
}

// status is the status of an entry in progress: its attempt's, or the
// legacy one; nil in any other phase.
func (p *updatePhase) status() *Status {
	if p.state != phaseProgress {
		return nil
	}
	if p.attempt != nil {
		s := p.attempt.Status()
		return &s
	}
	if p.legacy != nil {
		s := p.legacy.status
		return &s
	}
	return nil
}

// applying reports an entry in progress whose status is Applying.
func (p *updatePhase) applying() bool {
	s := p.status()
	return s != nil && *s == mattercore.OTAUpdateApplying
}

// stalled reports an announced or progressing update that shows no
// activity for its timeout: since plus [ProgressTimeout] for an announced
// one, the attempt's staleness for one in progress, and for a legacy one
// its time plus its apply delay and the timeout.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:464-473 at b25ed960
// (#isStalled).
func (p *updatePhase) stalled(now time.Time) bool {
	switch p.state {
	case phaseAnnounced:
		return p.since.Add(ProgressTimeout).Before(now)
	case phaseProgress:
		if p.attempt != nil {
			return p.attempt.IsStale(now)
		}
		if p.legacy != nil {
			return p.legacy.since.Add(p.legacy.applyDelay + ProgressTimeout).Before(now)
		}
	default:
	}
	return false
}

// announcement is the abort of a running announcement (the entry's
// announcement, SoftwareUpdateManager.ts:142-143 at b25ed960): it aborts
// on its own after [ProgressTimeout]. An abort before the announcement's
// goroutine bound its context ends that context as soon as it is bound.
type announcement struct {
	timer clock.Timer

	mu     sync.Mutex
	cause  error
	cancel context.CancelCauseFunc
}

// abort ends the announcement with cause; the first cause holds.
func (a *announcement) abort(cause error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cause != nil {
		return
	}
	a.cause = cause
	if a.cancel != nil {
		a.cancel(cause)
	}
}

// bind gives the announcement the cancel of its context.
func (a *announcement) bind(cancel context.CancelCauseFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancel = cancel
	if a.cause != nil {
		cancel(a.cause)
	}
}

var (
	// errAnnouncementTimeout aborts an announcement the node did not
	// answer within [ProgressTimeout].
	errAnnouncementTimeout = errors.New("softwareupdate: the announcement got no answer within the progress timeout")
	// errAnnouncementAborted aborts an announcement whose entry moved on
	// or whose manager went offline.
	errAnnouncementAborted = errors.New("softwareupdate: the announcement was aborted")
)

// queueEntry is UpdateQueueEntry (SoftwareUpdateManager.ts:134-144 at
// b25ed960).
type queueEntry struct {
	// Target is the update: vendor, product and target version are the
	// entry's; outside progress the version is the consent's (#retarget).
	// The transfer options are the consent's when the entry took it, for
	// the events only: a transfer takes the consent's ([TransferOptionsFor]).
	Target
	peer     Peer
	endpoint uint16
	phase    *updatePhase
	// announcement aborts the announcement still running for the entry,
	// also after an attempt moved it past announcing.
	announcement *announcement
	// transferStarted is whether [UpdateTransferStarted] was emitted for
	// the entry; a reset leaves it, so a retry does not emit it again.
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
	// suppress is internal.suppressUpdates (SoftwareUpdateManager.ts:2270-2271
	// at b25ed960): set until Start
	// and again from Close.
	suppress bool
	// consents are internal.consents, one per peer.
	consents map[Peer]*consent
	queue    []*queueEntry
	known    map[Peer]SoftwareUpdateInfo
	// startUpSuppress is internal.pendingStartUpSuppress
	// (SoftwareUpdateManager.ts:2266 at b25ed960).
	startUpSuppress map[Peer]uint16
	// observed are the nodes preparePeerForUpdate registered on, with the
	// software version last seen (the versionUpdateObservers,
	// SoftwareUpdateManager.ts:2253-2254 at b25ed960).
	observed map[Peer]uint32
	// attempts are the nodes' update attempts — internal.attempts
	// (SoftwareUpdateManager.ts:2273-2274 at b25ed960) — under m.mu with
	// the queue, as matter.js changes both in one synchronous change
	// (#mutate, SoftwareUpdateManager.ts:539-561 at b25ed960).
	attempts *mattercore.OTAUpdateAttempts
	// post is what a change under m.mu leaves to run once m.mu is
	// released — #mutate's queued events (SoftwareUpdateManager.ts:563-583
	// at b25ed960); unlock runs it.
	post []func()

	checkTimer, queueTimer clock.Timer
	// queueGen tells the queue timer's tick apart from a stopped one's.
	queueGen uint64
	// expiryTimer is internal.expiryTimer (#scheduleExpiry), expiryGen
	// tells its tick apart from a stopped one's.
	expiryTimer clock.Timer
	expiryGen   uint64
	// queueRunPending asks the end of the running change (unlock) for a
	// queue run (#triggerQueuedUpdate): the run reads the registry, which
	// the manager does not read with m.mu held.
	queueRunPending bool
	// seq counts the changes that give a peer state; touched is, per peer,
	// the seq of the last one. A peer is judged gone only when its node was
	// absent from a registry read and it gained no state after the read
	// began, as matter.js reads #peerOf inside the change (review-a M1).
	seq     uint64
	touched map[Peer]uint64
	// running are internal.runningAnnouncements: the aborts of the
	// announcements that run; Close aborts them and waits for them.
	running       map[*announcement]struct{}
	announcements *announcements
	closed        bool
	work          sync.WaitGroup
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
	// node, when set, stands in for the registry's node of a peer — a
	// test seam, as OtaTest's peers are the controller's ClientNodes
	// (#peerOf, #basicInformationOf, #otaEndpointFor,
	// SoftwareUpdateManager.ts:599-612, :1574-1588 at b25ed960): false is
	// no node with the address. Set before the manager runs, nil in
	// production.
	node func(p Peer) (nodeView, bool)
	// announce, when set, stands in for the AnnounceOTAProvider invoke of
	// a queued update — a test seam, as OtaTest's interceptAnnouncements
	// replaces announceOtaProvider (OtaTest.ts:3482-3499 at b25ed960);
	// ctx is the announcement's abort. Set before the manager runs, nil in
	// production.
	announce func(ctx context.Context, p Peer, endpoint uint16) error
	// presenceOf, when set, stands in for presence's one read of the
	// registry — a test seam that answers from a snapshot, as the registry
	// read does. Set before the manager runs, nil in production.
	presenceOf func() func(Peer) bool
}

// nodeView is what the node test seam says of a peer's node: its cached
// Basic Information (nil without) and the endpoint of its OTA requestor
// while the node is applicable for updates (nil while it is not).
type nodeView struct {
	basic    *basicinfo.State
	endpoint *uint16
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
// SoftwareUpdateManager.ts:392-447 at b25ed960).
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
		observed: map[Peer]uint32{}, consents: map[Peer]*consent{}, running: map[*announcement]struct{}{},
		touched:  map[Peer]uint64{},
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

// Start brings the manager online — #nodeOnline
// (SoftwareUpdateManager.ts:264-294 at b25ed960): the
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

// initializeUpdateCheck is #initializeUpdateCheck
// (SoftwareUpdateManager.ts:359-372 at b25ed960). It runs, as
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
// (SoftwareUpdateManager.ts:296-310, :2127-2138 at b25ed960): no update
// work starts any more; the timers stop; every running announcement is
// aborted and waited for (#stopAnnouncements,
// SoftwareUpdateManager.ts:1446-1453 at b25ed960), so an entry
// whose announcement it ended waits as queued; the announcements stop and
// the attempts end, an entry that followed one keeps following it
// (#endAllAttempts, SoftwareUpdateManager.ts:1229-1237 at b25ed960). A
// running update check a timer started is
// cancelled — its download included — and waited for, so nothing is
// stored after Close returns (review II, R2-P6-conc-1907).
func (m *Manager) Close() {
	m.mu.Lock()
	m.suppress = true
	m.closed = true
	for _, t := range []clock.Timer{m.checkTimer, m.queueTimer, m.expiryTimer} {
		if t != nil {
			t.Stop()
		}
	}
	m.queueTimer, m.expiryTimer = nil, nil
	m.queueGen++
	m.expiryGen++
	for run := range m.running {
		run.abort(errAnnouncementAborted)
	}
	a := m.announcements
	m.announcements = nil
	for _, at := range m.attempts.All() {
		m.attempts.End(at)
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

// presence reports, for a peer, whether a node has its address now — the
// question of #peerOf (SoftwareUpdateManager.ts:599-602 at b25ed960),
// answered from one read of the registry, so the queue run asks it with
// m.mu held without reaching into the registry. Called without m.mu.
func (m *Manager) presence() func(Peer) bool {
	if m.presenceOf != nil {
		return m.presenceOf()
	}
	if m.node != nil {
		return func(p Peer) bool { _, ok := m.node(p); return ok }
	}
	present := map[Peer]bool{}
	for _, n := range m.cfg.Registry.Nodes() {
		if p, ok := n.PeerAddress(); ok {
			present[p] = true
		}
	}
	return func(p Peer) bool { return present[p] }
}

// peerPresent reports whether a node has p's address now, by one lookup
// (#peerOf, SoftwareUpdateManager.ts:599-602 at b25ed960). Called without
// m.mu.
func (m *Manager) peerPresent(p Peer) bool {
	if m.node != nil {
		_, ok := m.node(p)
		return ok
	}
	return m.nodeAt(p) != nil
}

// basicInformationOf is the node's cached Basic Information, false while
// no node has the address or its cache holds none (#basicInformationOf,
// SoftwareUpdateManager.ts:604-607 at b25ed960). Called without m.mu.
func (m *Manager) basicInformationOf(p Peer) (basicinfo.State, bool) {
	if m.node != nil {
		v, ok := m.node(p)
		if !ok || v.basic == nil {
			return basicinfo.State{}, false
		}
		return *v.basic, true
	}
	n := m.nodeAt(p)
	if n == nil {
		return basicinfo.State{}, false
	}
	return basicInformation(n)
}

// softwareVersionOf is the software version of the node's cached Basic
// Information (#softwareVersionOf, SoftwareUpdateManager.ts:609-612 at
// b25ed960). Called without m.mu.
func (m *Manager) softwareVersionOf(p Peer) (uint32, bool) {
	st, ok := m.basicInformationOf(p)
	return st.SoftwareVersion, ok
}

// otaEndpointFor is the OTA requestor endpoint of the node while it is
// applicable for updates: ErrNoNode while no node has the address,
// [ErrMismatch] for an applicable node whose vendor or product differ from
// t's. Called without m.mu.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1569-1588 at b25ed960
// (#otaEndpointFor).
func (m *Manager) otaEndpointFor(p Peer, t Target) (endpoint uint16, applicable bool, err error) {
	var vid, pid uint16
	if m.node != nil {
		v, ok := m.node(p)
		if !ok {
			return 0, false, fmt.Errorf("%w: %s", ErrNoNode, peerString(p))
		}
		if v.endpoint == nil || v.basic == nil {
			return 0, false, nil
		}
		endpoint, vid, pid = *v.endpoint, v.basic.VendorId, v.basic.ProductId
	} else {
		n := m.nodeAt(p)
		if n == nil {
			return 0, false, fmt.Errorf("%w: %s", ErrNoNode, peerString(p))
		}
		d, ok := m.preparePeerForUpdate(n)
		if !ok {
			return 0, false, nil
		}
		endpoint, vid, pid = d.endpoint, d.vendorID, d.productID
	}
	if vid != t.VendorID || pid != t.ProductID {
		return 0, false, fmt.Errorf("%w: %s", ErrMismatch, peerString(p))
	}
	return endpoint, true, nil
}

// announceTo sends AnnounceOTAProvider with reason UpdateAvailable to the
// node's requestor endpoint under ctx, the announcement's abort.
func (m *Manager) announceTo(ctx context.Context, a *announcements, p Peer, endpoint uint16) error {
	if m.announce != nil {
		return m.announce(ctx, p, endpoint)
	}
	n := m.nodeAt(p)
	if n == nil {
		return fmt.Errorf("%w: %s", ErrNoNode, peerString(p))
	}
	return a.announceOtaProvider(ctx, n, endpoint, announceUpdateAvailable)
}

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

// preparePeerForUpdate is #preparePeerForUpdate
// (SoftwareUpdateManager.ts:946-992 at b25ed960): an applicable
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
		m.touchLocked(p.peer)
	}
	m.mu.Unlock()
	return p, true
}

// NodeChanged, NodeEvent and NodeSubscription are the node events the
// manager observes a node through once it prepared it for an update —
// softwareVersion$Changed, startUp and subscriptionStatusChanged
// (SoftwareUpdateManager.ts:964-990 at b25ed960). The controller feeds
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
// onSoftwareVersionChanged — softwareVersion$Changed
// (SoftwareUpdateManager.ts:964-969 at b25ed960).
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

// entryOfLocked is the node's queue entry, nil for none (#entryFor,
// SoftwareUpdateManager.ts:595-597 at b25ed960).
func (m *Manager) entryOfLocked(p Peer) *queueEntry {
	if i := m.indexOfLocked(p); i >= 0 {
		return m.queue[i]
	}
	return nil
}

// onSoftwareVersionChanged is #onSoftwareVersionChanged: a startUp of the
// reboot the provider already handled is suppressed; a version below the
// target after a startUp of an applying update fails it; a version at or
// above the target completes it; a version below resets an update that is
// underway, and the queue runs.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1035-1089 at b25ed960
// (#onSoftwareVersionChanged).
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
	e := m.entryOfLocked(p)
	if e == nil {
		m.mu.Unlock()
		return
	}
	previous, target := e.phase.status(), e.TargetSoftwareVersion
	if newVersion < target && isStartUp && previous != nil && *previous == mattercore.OTAUpdateApplying {
		m.log.Warn("ota.manager.apply_failed_reboot", slog.String("node", peerString(p)),
			slog.Uint64("version", uint64(newVersion)), slog.Uint64("target", uint64(target)))
		m.failUpdateLocked(e)
		m.unlock()
		return
	}
	if newVersion >= target {
		m.log.Info("ota.manager.updated", slog.String("node", peerString(p)), slog.Uint64("version", uint64(newVersion)))
		m.completeUpdateLocked(e, newVersion)
		m.unlock()
		return
	}
	m.log.Info("ota.manager.below_target", slog.String("node", peerString(p)),
		slog.Uint64("version", uint64(newVersion)), slog.Uint64("target", uint64(target)))
	// An announcing entry was already restarted, as by the other event of
	// the same reboot (SoftwareUpdateManager.ts:1081 at b25ed960).
	if e.phase.underway() {
		m.log.Info("ota.manager.version_change_clears_progress", slog.String("node", peerString(p)))
		m.resetEntryProgressLocked(e)
	}
	m.requestQueueRunLocked()
	m.unlock()
}

// onSubscribedAfterApply is #onSubscribedAfterApply
// (SoftwareUpdateManager.ts:1091-1118 at b25ed960): an
// Applying update whose node reports a version below the target over a
// subscription established after its restart deadline did not apply.
func (m *Manager) onSubscribedAfterApply(p Peer, n *client.Node) {
	if st, ok := basicInformation(n); ok {
		m.subscribedAfterApply(p, st.SoftwareVersion)
	}
}

// subscribedAfterApply is onSubscribedAfterApply over the version the
// node reports: an update in progress whose attempt applies, resubscribed
// after its restart deadline below the target, failed.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1091-1118 at b25ed960
// (#onSubscribedAfterApply).
func (m *Manager) subscribedAfterApply(p Peer, version uint32) {
	m.mu.Lock()
	e := m.entryOfLocked(p)
	if e == nil || e.phase.state != phaseProgress {
		m.mu.Unlock()
		return
	}
	ph := e.phase
	var since time.Time
	var delay time.Duration
	switch {
	case ph.attempt != nil:
		since, delay = ph.attempt.LastActivity(), ph.attempt.ApplyDelay()
	case ph.legacy != nil:
		since, delay = ph.legacy.since, ph.legacy.applyDelay
	default:
	}
	if s := ph.status(); s == nil || *s != mattercore.OTAUpdateApplying ||
		m.clock.Now().Before(since.Add(peer.RebootReturnTimeout(delay))) || version >= e.TargetSoftwareVersion {
		m.mu.Unlock()
		return
	}
	m.log.Warn("ota.manager.apply_failed_resubscribed", slog.String("node", peerString(p)), slog.Uint64("version", uint64(version)))
	m.failUpdateLocked(e)
	m.unlock()
}

// failUpdateLocked marks the entry's update failed and runs the queue on:
// the announcement is aborted, the startUp suppression dropped, the attempt
// the entry follows Cancelled when it applies and ended, and the entry
// waits as queued. Without a consent the entry leaves with updateFailed;
// else the first failure of the consent's version sets failedAt and
// reports updateFailed, a later one of that version is not reported again,
// a failure of another version is, and the entry takes the consent's
// version. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1142-1174 at b25ed960
// (#failUpdate).
func (m *Manager) failUpdateLocked(e *queueEntry) {
	p, failed, ph := e.peer, e.Target, e.phase
	m.abortAnnouncementLocked(e)
	delete(m.startUpSuppress, p)
	if ph.state == phaseProgress && ph.attempt != nil {
		if ph.attempt.Status() == mattercore.OTAUpdateApplying {
			m.attempts.SetStatus(ph.attempt, mattercore.OTAUpdateCancelled, 0)
		}
		m.attempts.End(ph.attempt)
	}
	e.phase = &updatePhase{state: phaseQueued}
	if c := m.consents[p]; c == nil {
		m.dequeueLocked(e, UpdateFailed, ErrNotApplied)
	} else {
		if c.TargetSoftwareVersion != failed.TargetSoftwareVersion || c.failedAt.IsZero() {
			m.postOutcomeLocked(UpdateFailed, p, failed, ErrNotApplied)
		}
		if c.TargetSoftwareVersion == failed.TargetSoftwareVersion && c.failedAt.IsZero() {
			c.failedAt = m.clock.Now()
			m.scheduleExpiryLocked()
		}
		m.retargetLocked(e)
	}
	m.requestQueueRunLocked()
}

// scheduleExpiryLocked starts the timer for the next failed consent to
// expire, or stops it when no consent failed. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1176-1190 at b25ed960
// (#scheduleExpiry).
func (m *Manager) scheduleExpiryLocked() {
	if m.expiryTimer != nil {
		m.expiryTimer.Stop()
		m.expiryTimer = nil
	}
	m.expiryGen++
	if m.closed {
		return
	}
	var next time.Time
	for _, c := range m.consents {
		if !c.failedAt.IsZero() && (next.IsZero() || c.failedAt.Before(next)) {
			next = c.failedAt
		}
	}
	if next.IsZero() {
		return
	}
	gen := m.expiryGen
	delay := max(0, next.Add(failedUpdateRetention).Sub(m.clock.Now()))
	m.expiryTimer = m.clock.AfterFunc(delay, func() { m.expireFailedUpdates(gen) })
}

// expireFailedUpdates removes the consents whose update failed
// [failedUpdateRetention] ago; their entries leave the queue without an
// event, as updateFailed reported the failure. An attempt keeps the apply
// right its offer gave. Each removed consent is reported as
// [UpdateConsentChanged] (Go-only).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1192-1214 at b25ed960
// (#expireFailedUpdates).
func (m *Manager) expireFailedUpdates(gen uint64) {
	m.mu.Lock()
	if gen != m.expiryGen || m.closed {
		m.mu.Unlock()
		return
	}
	m.expiryTimer = nil
	now := m.clock.Now()
	for p, c := range m.consents {
		if c.failedAt.IsZero() || c.failedAt.Add(failedUpdateRetention).After(now) {
			continue
		}
		m.log.Info("ota.manager.failed_update_expired", slog.String("node", peerString(p)))
		delete(m.consents, p)
		m.postConsentChangedLocked(p, c.Target, false)
		if e := m.entryOfLocked(p); e != nil {
			m.dequeueLocked(e, 0, nil)
		}
	}
	m.scheduleExpiryLocked()
	m.requestQueueRunLocked()
	m.unlock()
}

// completeUpdateLocked removes the entry as done, with the consent when
// its version is at or below completed, ends the node's attempts up to that
// version, and queues the consent's version when it is higher. m.mu is
// held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1120-1140 at b25ed960
// (#completeUpdate).
func (m *Manager) completeUpdateLocked(e *queueEntry, completed uint32) {
	p := e.peer
	delete(m.known, p)
	m.dequeueLocked(e, UpdateDone, nil)
	if c := m.consents[p]; c != nil && c.TargetSoftwareVersion <= completed {
		delete(m.consents, p)
		m.scheduleExpiryLocked()
	}
	for _, a := range m.attempts.Of(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}) {
		if v, ok := a.Version(); ok && v <= completed {
			m.attempts.EndAttempt(a)
		}
	}
	m.queueUpdateLocked(p, e.endpoint, e.VendorID, e.ProductID)
}

// resetEntryProgressLocked unlinks the entry from its attempt and lets it
// wait for the next one as queued, reset now, and take the consent's
// version (#retarget). m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:475-480 at b25ed960
// (#resetEntryProgress).
func (m *Manager) resetEntryProgressLocked(e *queueEntry) {
	m.abortAnnouncementLocked(e)
	e.phase = &updatePhase{state: phaseQueued, resetAt: m.clock.Now()}
	m.retargetLocked(e)
}

// retargetLocked lets an entry that follows no attempt take the version of
// the peer's consent: one whose version changes waits as queued again, its
// announcement aborted. Without a consent the entry leaves the queue with
// updateFailed. A changed target reports [UpdateQueued] (Go-only). m.mu is
// held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:482-507 at b25ed960
// (#retarget).
func (m *Manager) retargetLocked(e *queueEntry) {
	if e.phase.state == phaseProgress {
		return
	}
	c := m.consents[e.peer]
	if c == nil {
		m.dequeueLocked(e, UpdateFailed, ErrUpdateCancelled)
		return
	}
	e.VendorID, e.ProductID = c.VendorID, c.ProductID
	e.MaxBDXBlockSize, e.BDXAdditionalMrpDelay = c.MaxBDXBlockSize, c.BDXAdditionalMrpDelay
	if c.TargetSoftwareVersion == e.TargetSoftwareVersion {
		return
	}
	m.log.Info("ota.manager.retarget", slog.String("node", peerString(e.peer)),
		slog.Uint64("from", uint64(e.TargetSoftwareVersion)), slog.Uint64("to", uint64(c.TargetSoftwareVersion)))
	e.TargetSoftwareVersion = c.TargetSoftwareVersion
	m.abortAnnouncementLocked(e)
	e.phase = &updatePhase{state: phaseQueued}
	p, t := e.peer, e.Target
	m.post = append(m.post, func() { m.emit(UpdateEvent{Kind: UpdateQueued, Peer: p, Target: t}) })
}

// isFailedLocked reports an entry that waits as failed: the update to the
// version of the peer's consent failed since the consent was given. It is
// not announced and not listed; an entry in progress is never failed.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:509-515 at b25ed960
// (#isFailed).
func (m *Manager) isFailedLocked(e *queueEntry) bool {
	c := m.consents[e.peer]
	return e.phase.state != phaseProgress && c != nil && !c.failedAt.IsZero()
}

// targetOfLocked is the version of the peer's consent for the product, also
// while the update to it counts as failed.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:517-523 at b25ed960
// (#targetOf).
func (m *Manager) targetOfLocked(p Peer, vid, pid uint16) (uint32, bool) {
	if c := m.consents[p]; c != nil && c.VendorID == vid && c.ProductID == pid {
		return c.TargetSoftwareVersion, true
	}
	return 0, false
}

// dequeueLocked removes the entry from the queue, with its announcement and
// its startUp suppression, and reports kind for it — [UpdateDone],
// [UpdateFailed] with err, or nothing for 0. It is the only way an entry
// leaves the queue. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:525-537 at b25ed960
// (#dequeue).
func (m *Manager) dequeueLocked(e *queueEntry, kind UpdateEventKind, err error) {
	m.abortAnnouncementLocked(e)
	delete(m.startUpSuppress, e.peer)
	m.queue = slices.DeleteFunc(m.queue, func(x *queueEntry) bool { return x == e })
	if kind != 0 {
		m.postOutcomeLocked(kind, e.peer, e.Target, err)
	}
}

// abortAnnouncementLocked ends a running announcement of the entry early;
// its result is ignored anyway (#abortAnnouncement,
// SoftwareUpdateManager.ts:590-593 at b25ed960).
func (m *Manager) abortAnnouncementLocked(e *queueEntry) {
	if e.announcement != nil {
		e.announcement.abort(errAnnouncementAborted)
	}
}

// postOutcomeLocked queues [UpdateDone] or [UpdateFailed] of the peer with
// its hook, emitted once the change ends (#queueEvent,
// SoftwareUpdateManager.ts:563-569 at b25ed960).
func (m *Manager) postOutcomeLocked(kind UpdateEventKind, p Peer, t Target, err error) {
	hook := m.cfg.OnUpdateFailed
	if kind == UpdateDone {
		hook = m.cfg.OnUpdateDone
	}
	m.post = append(m.post, func() {
		if hook != nil {
			hook(p)
		}
		m.emit(UpdateEvent{Kind: kind, Peer: p, Target: t, Err: err})
	})
}

// postUpdateAvailableLocked queues updateAvailable of the peer with its
// hook (requestConsentForUpdate, SoftwareUpdateManager.ts:1252-1259 at
// b25ed960).
func (m *Manager) postUpdateAvailableLocked(p Peer, details SoftwareUpdateInfo, t Target) {
	hook := m.cfg.OnUpdateAvailable
	m.post = append(m.post, func() {
		if hook != nil {
			hook(p, details)
		}
		m.emit(UpdateEvent{Kind: UpdateAvailable, Peer: p, Target: t, Info: details})
	})
}

// postConsentChangedLocked queues [UpdateConsentChanged] (Go-only).
func (m *Manager) postConsentChangedLocked(p Peer, t Target, consented bool) {
	m.post = append(m.post, func() { m.emit(UpdateEvent{Kind: UpdateConsentChanged, Peer: p, Target: t, Consented: consented}) })
}

// requestQueueRunLocked has the running change end with a queue run
// (#triggerQueuedUpdate).
func (m *Manager) requestQueueRunLocked() { m.queueRunPending = true }

// touchLocked records that a change gave p state (seq, touched). m.mu is
// held.
func (m *Manager) touchLocked(p Peer) {
	m.seq++
	m.touched[p] = m.seq
}

// readPresenceLocked reads the registry with m.mu released and returns, with
// m.mu held again, which peers are gone as of that read: no node has the
// address, and no change gave the peer state after the read began. m.mu
// is held on entry and on return.
func (m *Manager) readPresenceLocked() (gone func(Peer) bool) {
	snap := m.seq
	m.mu.Unlock()
	present := m.presence()
	m.mu.Lock()
	return func(p Peer) bool { return !present(p) && m.touched[p] <= snap }
}

// unlock ends a change of the manager's state (#mutate,
// SoftwareUpdateManager.ts:539-561 at b25ed960): the attempts the rules
// ended reset the entries that followed them (#endAttempt,
// SoftwareUpdateManager.ts:1216-1227 at b25ed960), a queue run the change
// asked for runs — it reads the registry outside m.mu, so m.mu is released
// for that read (readPresenceLocked) — and once m.mu is released the
// change's own events run, on its caller: they are taken before each
// release, so no other change emits them.
func (m *Manager) unlock() {
	var events []func()
	for {
		m.resetEndedLocked()
		if !m.queueRunPending {
			break
		}
		m.queueRunPending = false
		events = append(events, m.post...)
		m.post = nil
		m.runQueueLocked(m.readPresenceLocked())
	}
	events = append(events, m.post...)
	m.post = nil
	m.mu.Unlock()
	for _, fn := range events {
		fn()
	}
}

// resetEndedLocked resets the entries linked to the attempts the rules
// ended since the last call (#endAttempt, SoftwareUpdateManager.ts:1216-1227
// at b25ed960), except while updates are suppressed. m.mu is held.
func (m *Manager) resetEndedLocked() {
	for _, a := range m.attempts.TakeEnded() {
		if m.suppress {
			continue
		}
		if e := m.linkedEntryLocked(a); e != nil {
			m.log.Info("ota.manager.attempt_ended_reset", slog.String("node", peerString(e.peer)), slog.String("attempt", a.String()))
			m.resetEntryProgressLocked(e)
		}
	}
}

// entryForLocked is the node's queue entry, nil for none.
func (m *Manager) entryForLocked(p mattercore.OTAPeer) *queueEntry {
	return m.entryOfLocked(peerOf(p))
}

func (m *Manager) linkedEntryLocked(a *mattercore.OTAUpdateAttempt) *queueEntry {
	if e := m.entryForLocked(a.Peer()); e != nil && e.phase.state == phaseProgress && e.phase.attempt == a {
		return e
	}
	return nil
}

func (m *Manager) linkAttemptLocked(a *mattercore.OTAUpdateAttempt) *queueEntry {
	// The attempts the answer replaced ended first and reset the entry they
	// held, so it may take a new target before it is linked (#answerAttempt
	// runs before #linkAttempt, SoftwareUpdateManager.ts:1774-1778 at
	// b25ed960).
	m.resetEndedLocked()
	e := m.entryForLocked(a.Peer())
	if e == nil || !a.Answered() {
		return nil
	}
	if v, ok := a.Version(); !ok || v != e.TargetSoftwareVersion {
		return nil
	}
	if e.phase.state == phaseProgress && e.phase.attempt == a {
		return e
	}
	wasFailed := m.isFailedLocked(e)
	e.phase = &updatePhase{state: phaseProgress, attempt: a}
	if wasFailed {
		m.requestQueueRunLocked()
	}
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
	m.attempts.SetStatus(a, status, applyDelay)
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
		m.resetEntryProgressLocked(e) // #retryUpdate (SoftwareUpdateManager.ts:585-588 at b25ed960)
		m.requestQueueRunLocked()
		armer := m.cfg.Armer
		m.post = append(m.post, func() {
			if armer != nil {
				armer.Disarm(p)
			}
		})
	case mattercore.OTAUpdateApplying:
		// Armed under m.mu with the entry still queued, so a NodeDeleted
		// that removes it disarms after (review II, R2-P6-bound-1914).
		if m.cfg.Armer != nil {
			m.cfg.Armer.Arm(p, applyDelay)
		}
		if !wasApplying {
			m.postApplyingLocked(p, target)
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

// bdxSessionFor is #bdxSessionFor (SoftwareUpdateManager.ts:432-438 at
// b25ed960); nil when no transfer runs.
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

// isTransferring is #isTransferring (SoftwareUpdateManager.ts:440-447 at
// b25ed960): a retried download restarts its counter below the one
// recorded, so any change of the counter is liveness.
func isTransferring(ph *updatePhase, transferred *uint64) bool {
	if transferred == nil {
		return false
	}
	var recorded uint64
	if ph.state == phaseProgress && ph.bdxTransferred != nil {
		recorded = *ph.bdxTransferred
	}
	return *transferred != recorded
}

// refreshBdxProgressLocked records a moving transfer of an entry in
// progress as the attempt's activity (a legacy entry's time). m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:449-462 at b25ed960
// (#refreshBdxProgress).
func (m *Manager) refreshBdxProgressLocked(e *queueEntry) bool {
	ph := e.phase
	t := transferredOf(m.bdxSessionForLocked(e.peer))
	if ph.state != phaseProgress || !isTransferring(ph, t) {
		return false
	}
	ph.bdxTransferred = t
	switch {
	case ph.attempt != nil:
		ph.attempt.Touch()
	case ph.legacy != nil:
		ph.legacy.since = m.clock.Now()
	default:
	}
	return true
}

func transferredOf(s bdxTransfer) *uint64 {
	if s == nil {
		return nil
	}
	v := s.Transferred()
	return &v
}

// QueuedUpdates is a snapshot of the update queue. A failed update is not
// listed — updateFailed reported it — until the node retries it on its
// own; the status is the phase's, stalled for an update underway without
// activity or a moving transfer.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:388-430 at b25ed960
// (queuedUpdates).
func (m *Manager) QueuedUpdates() []PendingUpdate {
	type listed struct {
		u  PendingUpdate
		ph updatePhase
	}
	m.mu.Lock()
	entries := make([]listed, 0, len(m.queue))
	for _, e := range m.queue {
		if m.isFailedLocked(e) {
			continue
		}
		ph := *e.phase
		if ph.legacy != nil {
			l := *ph.legacy // refreshBdxProgressLocked moves its time under m.mu
			ph.legacy = &l
		}
		u := PendingUpdate{Peer: e.peer, VendorID: e.VendorID, ProductID: e.ProductID, TargetSoftwareVersion: e.TargetSoftwareVersion}
		switch ph.state {
		case phaseProgress:
			u.LastProgressStatus = ph.status()
			if ph.attempt != nil {
				u.LastProgressUpdateTime = ph.attempt.LastActivity()
			} else if ph.legacy != nil {
				u.LastProgressUpdateTime = ph.legacy.since
			}
		case phaseAnnounced:
			u.LastProgressUpdateTime = ph.since
		case phaseQueued:
			if !ph.resetAt.IsZero() {
				unknown := mattercore.OTAUpdateUnknown
				u.LastProgressStatus = &unknown
			}
		default:
		}
		entries = append(entries, listed{u: u, ph: ph})
	}
	m.mu.Unlock()
	now := m.clock.Now()
	out := make([]PendingUpdate, 0, len(entries))
	for i := range entries {
		u, ph := entries[i].u, entries[i].ph
		s := m.bdxSessionFor(u.Peer)
		switch {
		case ph.state == phaseQueued:
			u.Status = PendingQueued
		case ph.stalled(now) && !isTransferring(&ph, transferredOf(s)):
			u.Status = PendingStalled
		default:
			u.Status = PendingInProgress
		}
		if u.Status == PendingInProgress && s != nil {
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

// validatePeerDetails is #matchingBasicInformation
// (SoftwareUpdateManager.ts:615-629 at b25ed960). Beside it, a
// query whose ids are not the node's attested ones is logged
// ([Manager.noteAttestedIdentity]); the answer does not depend on it.
func (m *Manager) validatePeerDetails(p Peer, version uint32, vid, pid uint16) bool {
	if n := m.nodeAt(p); n != nil {
		m.noteAttestedIdentity(p, n.Record(), vid, pid)
	}
	st, ok := m.basicInformationOf(p)
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

// UpdateExistsFor is the stored update the querying node gets: the
// node's details must match the query; the candidates are the applicable
// stored images over its version (prod ones, the others only with
// AllowTestImages), newest first; the one whose version is the consent's
// comes first — also for 24 hours after its update failed — else the
// newest, flagged consent-required, when the consent may be delegated to
// the node ([Config].DelegateConsentToRequestor, mayDelegateConsentTo);
// else nothing. A stored image the store would not serve ([Images.Open]:
// a prod or test image not bound to a ledger checksum, unless
// [ImagesConfig].AllowUnverifiedLedgerImages) is no candidate.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:657-715 at b25ed960
// (updateExistsFor).
func (m *Manager) UpdateExistsFor(ctx context.Context, p Peer, req provdef.QueryImageRequest) (AvailableDetails, bool) {
	if !m.validatePeerDetails(p, req.SoftwareVersion, req.VendorId, req.ProductId) {
		m.log.Info("ota.manager.peer_details_mismatch", slog.String("node", peerString(p)))
		return AvailableDetails{}, false
	}
	candidates := m.storedCandidates(ctx, req.VendorId, req.ProductId, req.SoftwareVersion)
	if len(candidates) == 0 {
		return AvailableDetails{}, false
	}
	delegate := m.mayDelegateConsentTo(p, req)
	m.mu.Lock()
	chosen, required := m.offerOfLocked(p, candidates, req.VendorId, req.ProductId, delegate)
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

// offerOfLocked is the candidate UpdateExistsFor offers: the one whose
// version is the peer's consent for the product, else — when delegate —
// the newest one, flagged consent-required; nil for none. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:698-701 at b25ed960
// (the consented candidate, else the delegated one).
func (m *Manager) offerOfLocked(p Peer, candidates []StoredImage, vid, pid uint16, delegate bool) (*StoredImage, bool) {
	if v, ok := m.targetOfLocked(p, vid, pid); ok {
		for i := range candidates {
			if candidates[i].SoftwareVersion == v {
				return &candidates[i], false
			}
		}
	}
	if delegate && len(candidates) > 0 {
		return &candidates[0], true
	}
	return nil, false
}

// mayDelegateConsentTo reports whether a node whose QueryImage is req may
// obtain the user's consent itself: it says it can, the delegation is on
// ([Config].DelegateConsentToRequestor), and its cached Basic Information
// exists and does not have LocalConfigDisabled set. Called without m.mu.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:638-647 at b25ed960
// (mayDelegateConsentTo).
func (m *Manager) mayDelegateConsentTo(p Peer, req provdef.QueryImageRequest) bool {
	if req.RequestorCanConsent == nil || !*req.RequestorCanConsent || !m.cfg.DelegateConsentToRequestor {
		return false
	}
	st, ok := m.basicInformationOf(p)
	return ok && !st.LocalConfigDisabled
}

// storedCandidates are UpdateExistsFor's candidates: the stored images
// of vid/pid applicable over version, test and local ones only with
// AllowTestImages, as [Images.applicable] selects them for the update
// check — the same set in the same order.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:685-690 at b25ed960
// (updateExistsFor's findApplicable call; #includeNonProductionImages at
// SoftwareUpdateManager.ts:653-655 at b25ed960).
func (m *Manager) storedCandidates(ctx context.Context, vid, pid uint16, version uint32) []StoredImage {
	return m.cfg.Images.applicable(ctx, vid, pid, version, m.cfg.AllowTestImages)
}

// CheckForUpdates checks every node for updates, stored images included,
// and purges the stored images no node needs — checkForUpdates
// (SoftwareUpdateManager.ts:722-725 at b25ed960).
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

// cleanupObsoleteUpdates is #cleanupObsoleteUpdates
// (SoftwareUpdateManager.ts:734-799 at b25ed960): a stored
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
// found one for — queryUpdates (SoftwareUpdateManager.ts:810-870 at
// b25ed960). A check the manager ran (it
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
		m.forgetGone()
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

// forgetGone drops what the manager keeps for every peer that is gone as
// of one read of the registry (readPresenceLocked): the version it
// observed, the update it found, a startUp to suppress, its attempts, its
// consent (reported as [UpdateConsentChanged], as NodeDeleted does), and
// its queued update, without an event — what #forgetPeer drops
// (SoftwareUpdateManager.ts:312-342 at b25ed960) and #dequeueIfGone
// (SoftwareUpdateManager.ts:1342-1353 at b25ed960). A check of every node
// runs it, so what a node that left the registry without NodeDeleted
// leaves behind lasts until the next check or queue run (review II,
// R2-P6-bound-1914).
func (m *Manager) forgetGone() {
	m.mu.Lock()
	gone := m.readPresenceLocked()
	maps.DeleteFunc(m.observed, func(p Peer, _ uint32) bool { return gone(p) })
	for _, a := range m.attempts.All() {
		if gone(peerOf(a.Peer())) {
			m.attempts.End(a)
		}
	}
	maps.DeleteFunc(m.known, func(p Peer, _ SoftwareUpdateInfo) bool { return gone(p) })
	maps.DeleteFunc(m.startUpSuppress, func(p Peer, _ uint16) bool { return gone(p) })
	for p, c := range m.consents {
		if gone(p) {
			delete(m.consents, p)
			m.postConsentChangedLocked(p, c.Target, false)
		}
	}
	for _, e := range slices.Clone(m.queue) {
		m.dequeueIfGoneLocked(e, gone)
	}
	maps.DeleteFunc(m.touched, func(p Peer, _ uint64) bool { return gone(p) })
	m.scheduleExpiryLocked()
	m.unlock()
}

func (m *Manager) suppressed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suppress
}

// checkProductForUpdates is #checkProductForUpdates
// (SoftwareUpdateManager.ts:876-938 at b25ed960): the
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
		// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:876-938 at b25ed960
		// (#checkProductForUpdates, its loop :902-935: a node removed during the check is
		// skipped; a consent for the product queues its version; consent is
		// asked when there is none or it is below the version found).
		if !m.peerPresent(t.peer) {
			m.log.Debug("ota.manager.removed_during_check", slog.String("node", peerString(t.peer)))
			continue
		}
		peers = append(peers, t.peer)
		m.mu.Lock()
		m.known[t.peer] = details
		m.touchLocked(t.peer)
		if m.suppress {
			m.mu.Unlock()
			break
		}
		target, ok := m.targetOfLocked(t.peer, vid, pid)
		if ok {
			m.queueUpdateLocked(t.peer, t.endpoint, vid, pid)
		}
		if !ok || target < info.SoftwareVersion {
			m.postUpdateAvailableLocked(t.peer, details, Target{VendorID: vid, ProductID: pid, TargetSoftwareVersion: info.SoftwareVersion})
		}
		m.unlock()
	}
	return peers, nil
}

// queueUpdateLocked queues an update of the peer to the version of its
// consent for the product, or lets its entry take that version
// (#retarget), and has the queue run, which does not announce a failed
// update. It returns the peer's entry, if any. A new entry reports
// [UpdateQueued] (Go-only, matter.js emits nothing here). m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1261-1293 at b25ed960
// (#queueUpdate).
func (m *Manager) queueUpdateLocked(p Peer, endpoint, vid, pid uint16) *queueEntry {
	if e := m.entryOfLocked(p); e != nil {
		e.endpoint = endpoint
		m.retargetLocked(e)
	} else if _, ok := m.targetOfLocked(p, vid, pid); ok {
		m.log.Info("ota.manager.queued", slog.String("node", peerString(p)))
		e := &queueEntry{Target: m.consents[p].Target, peer: p, endpoint: endpoint, phase: &updatePhase{state: phaseQueued}}
		m.queue = append(m.queue, e)
		m.touchLocked(p)
		t := e.Target
		m.post = append(m.post, func() { m.emit(UpdateEvent{Kind: UpdateQueued, Peer: p, Target: t}) })
	}
	m.requestQueueRunLocked()
	return m.entryOfLocked(p)
}

// triggerQueuedUpdate runs the update queue as a change of its own.
func (m *Manager) triggerQueuedUpdate() {
	m.mu.Lock()
	m.requestQueueRunLocked()
	m.unlock()
}

// runQueueLocked runs the update queue: an entry whose node is gone leaves
// it without an event, a stalled entry is reset, and with no entry active
// the first queued entry that is not failed is announced — entries reset
// for a retry go after fresh ones, the longest-waiting first. While an
// entry waits or is active a periodic timer runs the queue every 5
// minutes. gone says which peers no node has the address of
// (readPresenceLocked). m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1295-1340 at b25ed960
// (#triggerQueuedUpdate).
func (m *Manager) runQueueLocked(gone func(Peer) bool) {
	if m.suppress {
		return
	}
	now := m.clock.Now()
	active := 0
	for _, e := range slices.Clone(m.queue) {
		if !slices.Contains(m.queue, e) || m.dequeueIfGoneLocked(e, gone) || e.phase.state == phaseQueued {
			continue
		}
		if !m.refreshBdxProgressLocked(e) && e.phase.stalled(now) {
			m.log.Info("ota.manager.stalled_reset", slog.String("node", peerString(e.peer)))
			m.resetEntryProgressLocked(e)
			continue
		}
		active++
	}
	if active > 0 {
		if m.queueTimer == nil {
			m.restartQueueTimerLocked()
		}
		return
	}
	m.stopQueueTimerLocked()
	resetAt := func(e *queueEntry) int64 {
		if e.phase.state == phaseQueued && !e.phase.resetAt.IsZero() {
			return e.phase.resetAt.UnixNano()
		}
		return 0
	}
	slices.SortStableFunc(m.queue, func(a, b *queueEntry) int { return cmp.Compare(resetAt(a), resetAt(b)) })
	i := slices.IndexFunc(m.queue, func(e *queueEntry) bool { return e.phase.state == phaseQueued && !m.isFailedLocked(e) })
	if i < 0 {
		return
	}
	m.startAnnouncementLocked(m.queue[i], gone)
	m.restartQueueTimerLocked()
}

// dequeueIfGoneLocked removes the entry without an event when no node has
// its peer address any more, as when the node was removed while updates
// were suppressed. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1342-1353 at b25ed960
// (#dequeueIfGone).
func (m *Manager) dequeueIfGoneLocked(e *queueEntry, gone func(Peer) bool) bool {
	if !gone(e.peer) {
		return false
	}
	m.log.Debug("ota.manager.node_gone_dequeued", slog.String("node", peerString(e.peer)))
	m.dequeueLocked(e, 0, nil)
	return true
}

// restartQueueTimerLocked starts the periodic queue run anew (#restartQueueTimer,
// SoftwareUpdateManager.ts:1355-1362 at b25ed960, every 5 minutes).
func (m *Manager) restartQueueTimerLocked() {
	m.stopQueueTimerLocked()
	if m.closed {
		return
	}
	gen := m.queueGen
	m.queueTimer = m.clock.AfterFunc(queueCheckInterval, func() { m.queueTick(gen) })
}

func (m *Manager) stopQueueTimerLocked() {
	if m.queueTimer != nil {
		m.queueTimer.Stop()
		m.queueTimer = nil
	}
	m.queueGen++
}

// queueTick is a tick of the periodic queue run (#runQueue,
// SoftwareUpdateManager.ts:1364-1366 at b25ed960): the timer runs on until
// a run stops it.
func (m *Manager) queueTick(gen uint64) {
	m.mu.Lock()
	if gen != m.queueGen || m.closed {
		m.mu.Unlock()
		return
	}
	m.queueTimer = m.clock.AfterFunc(queueCheckInterval, func() { m.queueTick(gen) })
	m.requestQueueRunLocked()
	m.unlock()
}

// startAnnouncementLocked starts announcing the provider to the node of a
// queued entry, which usually answers with a QueryImage. It starts nothing
// while updates are suppressed, before the announcements run, while a BDX
// transfer of the node runs, or when the entry is not queued; the entry of
// a node that is gone leaves the queue. The entry is announcing at once,
// its announcement aborts after [ProgressTimeout] and runs on a goroutine
// Close aborts and waits for. m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1368-1397 at b25ed960
// (#startAnnouncement).
func (m *Manager) startAnnouncementLocked(e *queueEntry, gone func(Peer) bool) {
	if m.suppress || e.phase.state != phaseQueued || m.dequeueIfGoneLocked(e, gone) {
		return
	}
	a := m.announcements
	if a == nil {
		m.log.Info("ota.manager.not_initialized", slog.String("node", peerString(e.peer)))
		return
	}
	if m.bdxSessionForLocked(e.peer) != nil {
		m.log.Info("ota.manager.bdx_active_skip", slog.String("node", peerString(e.peer)))
		return
	}
	phase := &updatePhase{state: phaseAnnouncing}
	run := &announcement{}
	run.timer = m.clock.AfterFunc(ProgressTimeout, func() { run.abort(errAnnouncementTimeout) })
	e.phase, e.announcement = phase, run
	m.running[run] = struct{}{}
	p, endpoint := e.peer, e.endpoint
	m.log.Info("ota.manager.announcing", slog.String("node", peerString(p)))
	m.work.Go(func() { m.runAnnouncement(a, e, phase, run, p, endpoint) })
}

// runAnnouncement announces and then moves the entry on: to announced, or
// back to queued, reset now, when the announcement failed or was aborted.
// Once the entry left phase the result is ignored, as an attempt or a
// reset already decided how the update goes on; on a removed entry it
// has no effect (its phase is written, as matter.js writes it, but the
// entry is in no queue).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1399-1415 at b25ed960
// (#runAnnouncement).
//
//nolint:contextcheck // the announcement runs on the manager's lifetime (m.life), as matter.js's on its own abort, not on a caller's context
func (m *Manager) runAnnouncement(a *announcements, e *queueEntry, phase *updatePhase, run *announcement, p Peer, endpoint uint16) {
	ctx, cancel := context.WithCancelCause(m.life)
	run.bind(cancel)
	err := m.announceTo(ctx, a, p, endpoint)
	m.logAnnouncement(ctx, p, err)
	run.timer.Stop()
	cancel(nil)
	m.mu.Lock()
	delete(m.running, run)
	if e.announcement == run {
		e.announcement = nil
	}
	if e.phase == phase {
		if err == nil {
			e.phase = &updatePhase{state: phaseAnnounced, since: m.clock.Now()}
		} else {
			e.phase = &updatePhase{state: phaseQueued, resetAt: m.clock.Now()}
		}
	}
	m.unlock()
}

// logAnnouncement classifies a failed announcement: one aborted after
// [ProgressTimeout] got no answer; any other abort logs nothing; a closed
// connection is a debug line; any other failure an info line.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1417-1444 at b25ed960
// (#announce).
func (m *Manager) logAnnouncement(ctx context.Context, p Peer, err error) {
	switch {
	case err == nil:
	case ctx.Err() != nil:
		if errors.Is(context.Cause(ctx), errAnnouncementTimeout) {
			m.log.Info("ota.manager.announce_unanswered", slog.String("node", peerString(p)), slog.Duration("within", ProgressTimeout))
		}
	case errors.Is(err, exchange.ErrSessionClosed) || errors.Is(err, peer.ErrClosed):
		m.log.Debug("ota.manager.announce_closed", slog.String("node", peerString(p)), slog.Any("err", err))
	default:
		m.log.Info("ota.manager.announce_failed", slog.String("node", peerString(p)), slog.Any("err", err))
	}
}

// ForceUpdate replaces the node's consent as [Manager.AddUpdateConsent]
// does and announces the update at once, also while updates of other
// nodes are in progress:
//
//   - no node has the address: [ErrNoNode]; not applicable for updates:
//     [ErrNotApplicable]; vendor or product differ: [ErrMismatch] —
//     nothing changes;
//   - the node runs the target or a higher version: [ErrNotApplicable];
//   - a BDX transfer of the node runs for the target: the consent is
//     replaced, the transfer continues and no announcement starts; one
//     runs for another or an unknown version: [ErrNotApplicable];
//   - otherwise the consent is replaced, an update the node was announced
//     for or follows is reset, and the announcement starts unless one runs.
//
// A BDX registration of the node is not cancelled; the next transfer
// replaces it. That an announcement started is not promised.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1455-1524 at b25ed960
// (forceUpdate).
func (m *Manager) ForceUpdate(_ context.Context, p Peer, t Target) error {
	if err := t.assert(); err != nil {
		return err
	}
	endpoint, applicable, err := m.otaEndpointFor(p, t)
	if err != nil {
		return err
	}
	if !applicable {
		return fmt.Errorf("%w: %s", ErrNotApplicable, peerString(p))
	}
	running, known := m.softwareVersionOf(p)
	if known && running >= t.TargetSoftwareVersion {
		return fmt.Errorf("%w: %s runs version %d, not below the target %d", ErrNotApplicable, peerString(p), running, t.TargetSoftwareVersion)
	}
	m.mu.Lock()
	gone := m.readPresenceLocked()
	transfers := m.bdxSessionForLocked(p) != nil
	if transfers {
		var transferred *uint32
		for _, a := range m.attempts.Offered(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}) {
			if a.Answered() {
				if v, ok := a.Version(); ok {
					transferred = &v
				}
				break
			}
		}
		if transferred == nil || *transferred != t.TargetSoftwareVersion {
			m.mu.Unlock()
			what := "an update of unknown version"
			if transferred != nil {
				what = "version " + strconv.FormatUint(uint64(*transferred), 10)
			}
			return fmt.Errorf("%w: %s transfers %s right now, cannot force version %d", ErrNotApplicable, peerString(p), what, t.TargetSoftwareVersion)
		}
		m.log.Info("ota.manager.force_transfers_target", slog.String("node", peerString(p)))
	}
	if e := m.entryOfLocked(p); !transfers && e != nil && e.phase.underway() {
		m.log.Info("ota.manager.force_resets", slog.String("node", peerString(p)))
		m.abortAnnouncementLocked(e)
		e.phase = &updatePhase{state: phaseQueued, resetAt: m.clock.Now()}
	}
	// #storeConsent runs the queue (#queueUpdate,
	// SoftwareUpdateManager.ts:1266-1293 at b25ed960) before the forced
	// announcement (SoftwareUpdateManager.ts:1521 at b25ed960), so a waiting
	// update of another node is announced too.
	if e := m.storeConsentLocked(p, t, endpoint, true, running, known); e != nil {
		m.queueRunPending = false
		m.runQueueLocked(gone)
		m.startAnnouncementLocked(e, gone)
	}
	m.unlock()
	return nil
}

// closeTransfer disables the node for the "ota" scope and closes its
// transfer (#cancelBdxTransfer, SoftwareUpdateManager.ts:1239-1250 at
// b25ed960), outside m.mu: the
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

// TransferOptionsFor is how BDX carries the node's update: the options
// given with its consent, else the defaults.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1526-1536 at b25ed960
// (bdxTransferOptionsFor).
func (m *Manager) TransferOptionsFor(p Peer) TransferOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := TransferOptions{AdditionalMrpDelay: m.cfg.BDXAdditionalMrpDelay}
	if m.cfg.MaxBDXBlockSize != 0 {
		b := m.cfg.MaxBDXBlockSize
		out.MaxBlockSize = &b
	}
	if c := m.consents[p]; c != nil {
		if c.MaxBDXBlockSize != nil {
			out.MaxBlockSize = c.MaxBDXBlockSize
		}
		if c.BDXAdditionalMrpDelay != nil {
			out.AdditionalMrpDelay = c.BDXAdditionalMrpDelay
		}
	}
	return out
}

// AddUpdateConsent gives the consent to update the node to t's version; a
// node has one consent:
//
//   - no node has the address: [ErrNoNode]; vendor or product of an
//     applicable node differ: [ErrMismatch] — nothing changes;
//   - the node runs the target or a higher version: false, nothing stored;
//   - the node is not applicable for updates: the consent is stored, false;
//   - otherwise it replaces the node's consent, also a failed one; a queued
//     update the node does not follow yet takes the version; true when the
//     node has a queued update afterwards.
//
// The update is announced with the next queue slot. A consent given while
// the node follows an update to another version takes effect when that
// update ends. The consents are not persisted.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1538-1567 at b25ed960
// (addUpdateConsent).
func (m *Manager) AddUpdateConsent(_ context.Context, p Peer, t Target) (bool, error) {
	if err := t.assert(); err != nil {
		return false, err
	}
	endpoint, applicable, err := m.otaEndpointFor(p, t)
	if err != nil {
		return false, err
	}
	running, known := m.softwareVersionOf(p)
	m.mu.Lock()
	e := m.storeConsentLocked(p, t, endpoint, applicable, running, known)
	m.unlock()
	return e != nil, nil
}

// storeConsentLocked replaces the peer's consent, lets its queue entry take
// the version, and queues the update when the node is applicable; a
// consent for a version at or below the one the node runs (running, when
// known) is not stored. It returns the peer's queue entry, nil when nothing
// was queued. A stored consent reports [UpdateConsentChanged] (Go-only).
// m.mu is held.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1590-1625 at b25ed960
// (#storeConsent).
func (m *Manager) storeConsentLocked(p Peer, t Target, endpoint uint16, applicable bool, running uint32, known bool) *queueEntry {
	if known && t.TargetSoftwareVersion <= running {
		m.log.Info("ota.manager.consent_not_above_running", slog.String("node", peerString(p)),
			slog.Uint64("running", uint64(running)), slog.Uint64("target", uint64(t.TargetSoftwareVersion)))
		return nil
	}
	m.consents[p] = &consent{Target: t, peer: p}
	m.touchLocked(p)
	m.postConsentChangedLocked(p, t, true)
	m.scheduleExpiryLocked()
	if e := m.entryOfLocked(p); e != nil {
		m.retargetLocked(e)
	}
	if !applicable {
		m.log.Info("ota.manager.not_applicable_yet", slog.String("node", peerString(p)))
		return nil
	}
	return m.queueUpdateLocked(p, endpoint, t.VendorID, t.ProductID)
}

// HasConsent reports a consent of the node, for version when it is not 0;
// a failed update keeps its consent for 24 hours.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1627-1637 at b25ed960
// (hasConsent).
func (m *Manager) HasConsent(p Peer, version uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.consents[p]
	return c != nil && (version == 0 || c.TargetSoftwareVersion == version)
}

// RemoveConsent removes the node's consent for version, or for any
// version when it is 0. The node's offers of the version (of any, for 0)
// lose their apply right, so an ApplyUpdateRequest under their token is
// refused unless the update already applies, and its queries that made no
// offer yet end, answered NotAvailable. The queued update to a removed
// version:
//
//   - applies: continues, it can no longer be stopped;
//   - was announced or is followed by the node: leaves the queue with
//     updateFailed ([ErrUpdateCancelled]), its BDX transfer cancelled;
//   - is queued or announcing: leaves with updateFailed, its announcement
//     aborted.
//
// updateFailed is not reported again for an update whose failure it
// reported. For 0, a transfer of the node is cancelled in every case but an
// applying update. A consent left after an update was cancelled with its
// transfer is queued again.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1639-1723 at b25ed960
// (removeConsent).
func (m *Manager) RemoveConsent(p Peer, version uint32) {
	removesAll := version == 0
	m.mu.Lock()
	c := m.consents[p]
	if c != nil && (removesAll || c.TargetSoftwareVersion == version) {
		delete(m.consents, p)
		m.scheduleExpiryLocked()
		m.postConsentChangedLocked(p, c.Target, false)
	} else {
		m.log.Info("ota.manager.no_consent", slog.String("node", peerString(p)))
	}
	m.attempts.RemoveConsent(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}, version, removesAll)
	cancelTransfer := false
	var cancelled *queueEntry
	e := m.entryOfLocked(p)
	switch {
	case e == nil || (!removesAll && version != e.TargetSoftwareVersion):
		m.log.Info("ota.manager.cancel_none", slog.String("node", peerString(p)))
		cancelTransfer = removesAll
	case e.phase.applying():
		m.log.Info("ota.manager.cancel_too_late", slog.String("node", peerString(p)))
	default:
		m.log.Info("ota.manager.cancelled", slog.String("node", peerString(p)))
		if armer := m.cfg.Armer; armer != nil {
			m.post = append(m.post, func() { armer.Disarm(p) })
		}
		reportedFailed := c != nil && c.TargetSoftwareVersion == e.TargetSoftwareVersion && !c.failedAt.IsZero()
		underway := e.phase.underway()
		if reportedFailed {
			m.dequeueLocked(e, 0, nil)
		} else {
			m.dequeueLocked(e, UpdateFailed, ErrUpdateCancelled)
		}
		m.requestQueueRunLocked()
		cancelTransfer = removesAll || underway
		if underway {
			cancelled = e
		}
	}
	m.unlock()
	if !cancelTransfer {
		return
	}
	m.closeTransfer(p)
	if cancelled != nil {
		m.mu.Lock()
		m.queueUpdateLocked(cancelled.peer, cancelled.endpoint, cancelled.VendorID, cancelled.ProductID)
		m.unlock()
	}
}

// NodeDeleted forgets the node at p at once, when it left the registry
// (client.Hooks.Deleted): its consent (reported as [UpdateConsentChanged],
// Go-only), a startUp suppression, the update found for it, what the
// manager observed of it, its reboot arming, its attempts and its BDX
// scope (a transfer that runs is closed). Its queued update leaves the
// queue without an event when the queue runs, as no node has the address
// any more, and the queue runs on for the other nodes. A node the manager
// never saw is no error. Nothing happens while updates are suppressed —
// before [Manager.Start] and after [Manager.Close]; the next full check
// drops the same for a node whose deletion was not reported (review II,
// R2-P6-bound-1914).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:312-342 at b25ed960
// (#forgetPeer).
func (m *Manager) NodeDeleted(p Peer) {
	m.mu.Lock()
	if m.suppress {
		m.mu.Unlock()
		return
	}
	m.log.Info("ota.manager.node_deleted", slog.String("node", peerString(p)))
	if c := m.consents[p]; c != nil {
		delete(m.consents, p)
		m.postConsentChangedLocked(p, c.Target, false)
	}
	delete(m.startUpSuppress, p)
	delete(m.known, p)
	delete(m.observed, p)
	delete(m.touched, p)
	for _, a := range m.attempts.Of(mattercore.OTAPeer{FabricIndex: p.FabricIndex, NodeID: p.NodeID}) {
		m.attempts.End(a)
	}
	m.scheduleExpiryLocked()
	m.requestQueueRunLocked()
	armer := m.cfg.Armer
	m.unlock()
	if armer != nil {
		armer.Disarm(p)
	}
	m.closeTransfer(p)
}

// SuppressNextStartUp suppresses the node's next StartUp event on session
// sessionID — #suppressNextStartUp (SoftwareUpdateManager.ts:1725-1731 at
// b25ed960).
//
// Deprecated: the manager suppresses the startUp itself when it admits a
// query for the node's restart while an update of the node is queued
// (admitQuery, SoftwareUpdateManager.ts:1802-1881 at b25ed960); removable
// in v0.26.0.
func (m *Manager) SuppressNextStartUp(p Peer, sessionID uint16) {
	m.mu.Lock()
	m.startUpSuppress[p] = sessionID
	m.touchLocked(p)
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
	if e := m.entryOfLocked(p); !m.suppress && e != nil {
		if a := e.phase.attempt; e.phase.state == phaseProgress && a != nil && !a.Ended() {
			if status == mattercore.OTAUpdateDone {
				v := e.TargetSoftwareVersion
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
	e := m.entryOfLocked(p)
	if e == nil {
		m.mu.Unlock()
		if status != mattercore.OTAUpdateCancelled {
			m.log.Warn("ota.manager.status_unknown_node", slog.String("node", peerString(p)), slog.String("status", status.String()))
		}
		return
	}
	switch status {
	case mattercore.OTAUpdateDone:
		completed := e.TargetSoftwareVersion
		if version != nil {
			completed = *version
		}
		m.log.Info("ota.manager.done", slog.String("node", peerString(p)))
		m.completeUpdateLocked(e, completed)
	case mattercore.OTAUpdateCancelled:
		m.log.Info("ota.manager.bdx_cancelled_retry", slog.String("node", peerString(p)))
		m.resetEntryProgressLocked(e)
		m.requestQueueRunLocked()
		if armer := m.cfg.Armer; armer != nil {
			m.post = append(m.post, func() { armer.Disarm(p) })
		}
	default:
		m.progressLocked(p, e, status, applyDelay)
	}
	m.unlock()
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
	e := m.entryOfLocked(p)
	if e == nil {
		return
	}
	switch {
	case softwareVersion >= e.TargetSoftwareVersion:
		m.log.Info("ota.manager.done", slog.String("node", peerString(p)))
		m.completeUpdateLocked(e, softwareVersion)
	case linked == e:
		m.log.Info("ota.manager.applied_below_target_retry", slog.String("node", peerString(p)), slog.Uint64("version", uint64(softwareVersion)))
		m.resetEntryProgressLocked(e) // #retryUpdate (SoftwareUpdateManager.ts:585-588 at b25ed960)
		m.requestQueueRunLocked()
	default:
		m.log.Debug("ota.manager.applied_below_target_ignored", slog.String("node", peerString(p)), slog.Uint64("version", uint64(softwareVersion)))
	}
}

// progressLocked is onOtaStatusChange's last branch for an entry
// the deprecated OnStatusChange moves: the entry is in progress with the
// status and its time; the first Downloading reports the transfer started,
// Applying arms the reboot re-subscription and, the first time, reports
// the update applying. m.mu is held.
func (m *Manager) progressLocked(p Peer, e *queueEntry, status Status, applyDelay time.Duration) {
	wasApplying := e.phase.applying()
	lp := &legacyProgress{status: status, since: m.clock.Now()}
	if status == mattercore.OTAUpdateApplying {
		lp.applyDelay = applyDelay
	}
	var transferred *uint64
	if e.phase.state == phaseProgress {
		transferred = e.phase.bdxTransferred
	}
	e.phase = &updatePhase{state: phaseProgress, legacy: lp, bdxTransferred: transferred}
	target := e.Target
	if status == mattercore.OTAUpdateDownloading && !e.transferStarted {
		e.transferStarted = true
		m.post = append(m.post, func() { m.emit(UpdateEvent{Kind: UpdateTransferStarted, Peer: p, Target: target}) })
	}
	if status != mattercore.OTAUpdateApplying {
		return
	}
	// The arming happens under m.mu, with the entry it is for still queued:
	// a NodeDeleted that removes the entry disarms after it, so a deleted
	// peer is never left armed (review II, R2-P6-bound-1914). The armer runs
	// no callback under its own lock.
	if m.cfg.Armer != nil {
		m.cfg.Armer.Arm(p, applyDelay)
	}
	if !wasApplying {
		m.postApplyingLocked(p, target)
	}
}

// postApplyingLocked queues [UpdateApplying] of the peer with its hook.
func (m *Manager) postApplyingLocked(p Peer, t Target) {
	applying := m.cfg.OnUpdateApplying
	m.post = append(m.post, func() {
		if applying != nil {
			applying(p)
		}
		m.emit(UpdateEvent{Kind: UpdateApplying, Peer: p, Target: t})
	})
}

// dropUndeliverableUpdate fails the peer's queued update when the peer's
// QueryImage found no image for it: only while the peer is being or was
// announced for it — so it reported no status since — and still runs a
// version below the target, by its QueryImage (reported) and by its cached
// Basic Information. A peer that already runs the target keeps its entry,
// so the update ends as done. The provider's image lookup calls it when
// [Manager.UpdateExistsFor] offers nothing (C-5).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:2091-2125 at b25ed960
// (dropUndeliverableUpdate).
func (m *Manager) dropUndeliverableUpdate(p Peer, reported uint32) {
	cached, known := m.softwareVersionOf(p)
	m.mu.Lock()
	e := m.entryOfLocked(p)
	if e == nil || (e.phase.state != phaseAnnouncing && e.phase.state != phaseAnnounced) ||
		reported >= e.TargetSoftwareVersion || !known || cached != reported {
		m.mu.Unlock()
		return
	}
	m.log.Info("ota.manager.undeliverable", slog.String("node", peerString(p)), slog.Uint64("target", uint64(e.TargetSoftwareVersion)))
	m.failUpdateLocked(e)
	m.unlock()
}

func peerString(p Peer) string {
	return "@" + strconv.Itoa(int(p.FabricIndex)) + ":" + strconv.FormatUint(p.NodeID, 16)
}
