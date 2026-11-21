// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/caseauth"
)

// OTAProvider is the OTA Software Update Provider (0x0029): it answers an
// OTA requestor's QueryImage with an image its host's [OTAImageSource]
// names, serves the image over BDX as the following sender of the
// requestor's receiver-driven download, decides ApplyUpdateRequest and
// records NotifyUpdateApplied.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/
// OtaSoftwareUpdateProviderServer.ts at b25ed960 (OP below). What matter.js
// decides in its SoftwareUpdateManager — which query is admitted, whether
// an apply proceeds, what a status moves — is the [OTAUpdateManager]'s
// ([OTAProviderConfig].Manager, by default [NewOTAStandaloneManager]); the
// host's ports are the image catalogue ([OTAImageSource],
// checkUpdateAvailable, OP:395-414) and the user's consent ([OTAConsent],
// requestUserConsentForUpdate, OP:382-393). The DCL lookups and the
// peer's BasicInformation validation of SoftwareUpdateManager.updateExistsFor
// are the image source's to do, or not.
type OTAProvider struct {
	*spec.Server

	cfg     OTAProviderConfig
	mgr     OTAUpdateManager
	storage *otaImageStorage
	logger  *slog.Logger
}

// OTAPeer is the requestor a provider talks to: matter.js PeerAddress, its
// node on one of this node's fabrics.
type OTAPeer struct {
	FabricIndex uint8
	NodeID      uint64
}

// OTAImage is the update an [OTAImageSource] offers: matter.js
// OtaUpdateAvailableDetails (SoftwareUpdateManager.ts:95-102).
type OTAImage struct {
	// FileDesignator names the image in the provider's "ota" storage scope
	// — matter.js names it `ota/${filename}` (SoftwareUpdateManager.ts:454)
	// — and is what [OTAImageSource.Open] is asked for. It is a plain name:
	// the characters A-Z a-z 0-9 . - _, no "/".
	FileDesignator bdx.FileDesignator
	// SoftwareVersion and SoftwareVersionString are the image's version.
	SoftwareVersion       uint32
	SoftwareVersionString string
	// HTTPSURI is offered to a requestor that does not support BDX
	// synchronous download but HTTPS (OP:382-386); empty offers none.
	HTTPSURI string
	// ConsentRequired asks the user's consent before the update: the
	// provider asks [OTAConsent] when the requestor cannot consent itself
	// (OP:245-284), and tells the requestor it is needed (OP:399).
	// false states that the consent exists, as SoftwareUpdateManager does
	// for a peer it holds one for.
	ConsentRequired bool
	// MetadataForRequestor is passed back in the QueryImageResponse
	// (OP:400); at most 512 bytes.
	MetadataForRequestor []byte
}

// OTAImageSource is the host's image catalogue: matter.js's
// checkUpdateAvailable extension point (OP:491-502) and the blob storage
// behind the "ota" scope (SoftwareUpdateManager.storage, OP:109-114).
type OTAImageSource interface {
	// QueryImage returns the image for the requestor's vendor, product and
	// version — false when there is none (NotAvailable, OP:173-178).
	QueryImage(ctx context.Context, peer OTAPeer, req provdef.QueryImageRequest) (OTAImage, bool, error)
	// Open returns the bytes of an image QueryImage offered, named by its
	// FileDesignator text; an error wrapping fs.ErrNotExist is answered
	// FileDesignatorUnknown on the BDX exchange. A returned blob that
	// implements io.Closer is closed when the transfer ends.
	Open(ctx context.Context, file string) (bdx.Blob, error)
}

// OTAConsentState is matter.js OtaSoftwareUpdateConsentState (OP:66-71).
type OTAConsentState uint8

// The consent states.
const (
	OTAConsentGranted OTAConsentState = iota
	OTAConsentDenied
	OTAConsentObtaining
	OTAConsentUnknown
)

// OTAConsent asks the user's consent for an update the requestor cannot
// consent to itself: matter.js's requestUserConsentForUpdate extension
// point (OP:479-489, :640-656). The delay is how long a requestor told
// Obtaining waits before it asks again; zero is matter.js's 120 s
// (OP:248). A nil OTAConsent answers Unknown, which declines (OP:488).
type OTAConsent interface {
	RequestConsent(ctx context.Context, peer OTAPeer, req provdef.QueryImageRequest, img OTAImage) (OTAConsentState, time.Duration, error)
}

// OTAApplyPolicy decides an ApplyUpdateRequest before the
// [OTAUpdateManager]'s apply table: Proceed and how long the requestor
// waits before it applies (matter.js State.applyDelay, OP:426-435), or
// AwaitNextAction or Discontinue with the delay before it asks again
// (Matter Core §11.20.6.5.4). Proceed leaves the decision to the table,
// which may still refuse, and its delay replaces [OTAProviderConfig].ApplyDelay;
// any other action answers at once, and Discontinue cancels the update the
// token names. A nil policy is matter.js's default: the table decides,
// after ApplyDelay.
//
// Deprecated: matter.js decides an apply in its SoftwareUpdateManager
// only, after the provider's State.applyDelay: set
// [OTAProviderConfig].ApplyDelay for the delay, and decide the apply
// through [OTAProviderConfig].Manager. Removable in v0.26.0.
type OTAApplyPolicy interface {
	ApplyUpdate(ctx context.Context, peer OTAPeer, req provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateActionEnum, time.Duration, error)
}

// OTAUpdateStatus is matter.js OtaUpdateStatus (SoftwareUpdateManager.ts:
// 104-113): where a requestor's update stands.
type OTAUpdateStatus uint8

// The update states.
const (
	OTAUpdateUnknown OTAUpdateStatus = iota
	OTAUpdateWaitForConsent
	OTAUpdateQuerying
	OTAUpdateDownloading
	OTAUpdateWaitForApply
	OTAUpdateApplying
	OTAUpdateDone
	OTAUpdateCancelled
)

func (s OTAUpdateStatus) String() string {
	switch s {
	case OTAUpdateWaitForConsent:
		return "WaitForConsent"
	case OTAUpdateQuerying:
		return "Querying"
	case OTAUpdateDownloading:
		return "Downloading"
	case OTAUpdateWaitForApply:
		return "WaitForApply"
	case OTAUpdateApplying:
		return "Applying"
	case OTAUpdateDone:
		return "Done"
	case OTAUpdateCancelled:
		return "Cancelled"
	case OTAUpdateUnknown:
	}
	return "Unknown"
}

// OTAProviderConfig configures [NewOTAProvider].
type OTAProviderConfig struct {
	// ImageSource is the host's image catalogue; mandatory.
	ImageSource OTAImageSource
	// Consent asks the user; nil declines what needs a consent.
	Consent OTAConsent
	// Manager owns the requestors' update attempts and decides the queries
	// and applies the provider takes ([OTAUpdateManager]); nil is
	// [NewOTAStandaloneManager] over this configuration. A Manager is
	// exclusive with the deprecated hooks only the default manager runs
	// (OnStatusChange, RebootQuery, TransferBlockSize, SessionActivity):
	// [NewOTAProvider] refuses the two together.
	Manager OTAUpdateManager
	// ApplyDelay is how long a requestor allowed to apply an update waits
	// before it applies: the DelayedActionTime of the Proceed answer and
	// the apply delay the manager records with Applying, in whole seconds
	// rounded up and at most a day. Zero applies at once.
	//
	// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:426-435 at b25ed960
	// (State.applyDelay, default 0; rounded at :346-351).
	ApplyDelay time.Duration
	// ApplyPolicy decides an apply before the manager's apply table; nil
	// leaves it to the table.
	//
	// Deprecated: the delay is ApplyDelay and the apply is the Manager's
	// to decide (matter.js's SoftwareUpdateManager.applyUpdate); removable
	// in v0.26.0.
	ApplyPolicy OTAApplyPolicy
	// DelegateConsentToRequestor lets the default manager delegate the
	// consent of an image that needs one to a requestor whose QueryImage
	// says it can consent (RequestorCanConsent): it gets the offer without
	// the [OTAConsent] port being asked, with UserConsentNeeded, and may
	// apply it. Off — matter.js's default — the port is asked. A [Manager]
	// decides the delegation itself: [NewOTAProvider] refuses the two
	// together.
	//
	// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:2158-2171 at b25ed960
	// (State.delegateConsentToRequestor, default false).
	DelegateConsentToRequestor bool
	// BDX is the node's BDX responder the images are served through
	// (matter.js BdxProtocol, OP:254-296); nil offers no BDX download.
	BDX *bdx.Responder
	// BDXMaxBlockSize, when set, caps the block size of the image
	// transfers the default manager offers (bdxTransferOptionsFor
	// maxBlockSize, SoftwareUpdateManager.ts:1530-1536 at b25ed960).
	BDXMaxBlockSize uint16
	// NodeID returns this node's id on a fabric, which the BDX URI names
	// (OP:300: the session's associatedFabric.rootNodeId); mandatory with
	// BDX.
	NodeID func(fabricIndex uint8) (uint64, bool)
	// OnAttemptStatus observes the requestors' update statuses, whichever
	// manager runs: each status the provider reports and its manager
	// records ([OTAUpdateManager].ReportAttemptStatus), the outcome of an
	// ApplyUpdateRequest — Applying, or Cancelled for a refused apply of a
	// known attempt — and Done with the reported version at every
	// NotifyUpdateApplied; each with the version the attempt concerns. A
	// status the manager moves on its own (a failed queued update) is the
	// manager's to report. nil observes nothing. It runs with no lock held,
	// and the WaitForApply of a transfer runs on the receive path — the
	// final BlockAckEof finishes the download at its arrival — so it must
	// not block. matter.js's provider has no observer: its
	// SoftwareUpdateManager emits the queue's events.
	OnAttemptStatus func(peer OTAPeer, status OTAUpdateStatus, version uint32)
	// OnStatusChange is told, by the default manager, every status a
	// requestor's update attempt takes, with the version it offers, and
	// Done with the reported version at every NotifyUpdateApplied; nil
	// observes nothing. It runs with no lock held, and the WaitForApply of
	// a transfer runs on the receive path — the final BlockAckEof finishes
	// the download at its arrival — so it must not block.
	//
	// Deprecated: OnAttemptStatus observes the same statuses for every
	// manager; removable in v0.26.0.
	OnStatusChange func(peer OTAPeer, status OTAUpdateStatus, version uint32)
	// RebootQuery, when set, learns from the default manager a QueryImage
	// it admitted for the requestor's restart — another session than the
	// transfer's, which it cancelled — with the query's session: a queue
	// suppresses the StartUp event that follows on that session
	// (SoftwareUpdateManager.ts:1873-1879 at b25ed960, which suppresses it
	// only while the peer has a queued update).
	//
	// Deprecated: the restart is the Manager's to judge (AdmitQuery);
	// removable in v0.26.0.
	RebootQuery func(peer OTAPeer, sessionID uint16)
	// TransferBlockSize, when set, caps the block size of one peer's
	// transfer in place of BDXMaxBlockSize; 0 falls back to it.
	//
	// Deprecated: the Manager's BDXTransferOptionsFor names a peer's
	// transfer options; removable in v0.26.0.
	TransferBlockSize func(peer OTAPeer) uint16
	// SessionActivity, when set, reports when the peer last sent on the
	// secure session of the id, false for a session it does not know. The
	// default manager's admission compares a running transfer's session
	// with the query's by it (SoftwareUpdateManager.ts:1852 at b25ed960
	// compares their activeTimestamp); without it, or when it knows either
	// session not, the transfer's last block stands in for its session's
	// activity and the query's session is active now.
	//
	// Deprecated: the admission is the Manager's (AdmitQuery); removable
	// in v0.26.0.
	SessionActivity func(sessionID uint16) (time.Time, bool)
	// Clock is the provider's clock. Nil is [clock.System].
	Clock clock.Clock
	// Rand stands in for crypto/rand in tests.
	Rand io.Reader
	// Logger is the provider's logger; nil is slog.Default().
	Logger *slog.Logger
}

// The constants of the provider.
const (
	// otaMaxDelayedActionTime caps DelayedActionTime (OP:34-39,
	// MAX_DELAYED_ACTION_TIME = 86_400 s).
	otaMaxDelayedActionTime = 86_400
	// otaUpdateTokenLength is OTA_UPDATE_TOKEN_LENGTH_BYTES (OP:41).
	otaUpdateTokenLength = 32
	// otaBusyRetry is the Busy answer's DelayedActionTime of a query the
	// manager does not admit (OP:192: Seconds.of(Minutes(2))) and the
	// Discontinue answer's (OP:356).
	otaBusyRetry = 2 * time.Minute
	// otaBdxBusyRetry is the Busy answer when BDX could not be enabled
	// (OP:267: Minutes(5)).
	otaBdxBusyRetry = 5 * time.Minute
	// otaConsentRetry is the default delay of an Obtaining consent
	// (OP:205: Seconds(120)).
	otaConsentRetry = 120 * time.Second
	// otaProgressTimeout makes an update attempt stale: matter.js
	// OTA_PROGRESS_TIMEOUT (OtaProgress.ts:14 at b25ed960, Minutes(15),
	// read at OtaUpdateAttempts.ts:109).
	otaProgressTimeout = 15 * time.Minute
	// otaBdxMessageTimeout is the image transfer's idle timeout (OP:259:
	// Minutes(5)).
	otaBdxMessageTimeout = 5 * time.Minute
	// otaScope is the storage scope the images are served under
	// (ScopedStorage.ts:14, "ota").
	otaScope = "ota"
)

// Errors of the provider.
var (
	ErrNoImageSource = errors.New("core: OTA provider needs an image source")
	ErrNoNodeID      = errors.New("core: OTA provider with BDX needs the node id lookup")
)

// The provider's update attempts are fabric-scoped state (owner decision
// 167), held by its manager.
var _ contract.FabricScopedState = (*OTAProvider)(nil)

// errOTAManagerHooks refuses a Manager beside the hooks only the default
// manager runs.
var errOTAManagerHooks = errors.New("core: OTA provider with a Manager takes none of OnStatusChange, RebootQuery, " +
	"TransferBlockSize, SessionActivity and DelegateConsentToRequestor: they configure the default manager")

// otaProviderInst is the definition the provider serves: no attribute but
// the globals, the three commands and their responses.
var otaProviderInst = mustInstance(provdef.Definition, spec.Options{})

// NewOTAProvider builds the provider.
func NewOTAProvider(cfg OTAProviderConfig) (*OTAProvider, error) {
	if cfg.ImageSource == nil {
		return nil, ErrNoImageSource
	}
	if cfg.BDX != nil && cfg.NodeID == nil {
		return nil, ErrNoNodeID
	}
	if cfg.Manager != nil && (cfg.OnStatusChange != nil || cfg.RebootQuery != nil || cfg.TransferBlockSize != nil || cfg.SessionActivity != nil ||
		cfg.DelegateConsentToRequestor) {
		return nil, errOTAManagerHooks
	}
	cfg.Clock = clock.Or(cfg.Clock)
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mgr := cfg.Manager
	if mgr == nil {
		mgr = NewOTAStandaloneManager(cfg)
	}
	p := &OTAProvider{cfg: cfg, mgr: mgr, logger: logger, storage: &otaImageStorage{source: cfg.ImageSource}}
	srv, err := spec.NewServer(provdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		provdef.CmdQueryImage: func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.QueryImageRequest](fields)
			if err != nil {
				return nil, err
			}
			return p.QueryImage(ctx, req)
		},
		provdef.CmdApplyUpdateRequest: func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.ApplyUpdateRequestRequest](fields)
			if err != nil {
				return nil, err
			}
			return p.ApplyUpdateRequest(ctx, req)
		},
		provdef.CmdNotifyUpdateApplied: func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.NotifyUpdateAppliedRequest](fields)
			if err != nil {
				return nil, err
			}
			return nil, p.NotifyUpdateApplied(ctx, req)
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("core: OtaSoftwareUpdateProvider: %w", err)
	}
	p.Server = srv
	return p, nil
}

// fieldsAs takes a command's decoded request, as a value or a pointer.
func fieldsAs[T any](fields any) (T, error) {
	switch f := fields.(type) {
	case *T:
		if f != nil {
			return *f, nil
		}
	case T:
		return f, nil
	}
	var zero T
	return zero, otaStatusError{im.StatusInvalidCommand, fmt.Sprintf("command fields %T", fields)}
}

// otaImageStorage is the provider's "ota" scope over the image source
// (one value for the provider's life, so the responder sees the same
// storage on every QueryImage, BdxProtocol.ts:62).
type otaImageStorage struct{ source OTAImageSource }

func (s *otaImageStorage) Open(ctx context.Context, name string) (bdx.Blob, error) {
	return s.source.Open(ctx, name)
}

// remotePeer is OP's assertRemoteActor + NodeSession.assert: the
// requestor of a command, its fabric and node, and the session id the
// command arrived on. CASE is told apart by internal/caseauth, the one
// rule every CASE-only server of the module shares.
func remotePeer(ctx context.Context) (OTAPeer, uint16, error) {
	if !caseauth.IsCASE(ctx) {
		return OTAPeer{}, 0, otaStatusError{im.StatusUnsupportedAccess, "OTA provider commands need a CASE session"}
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	node, _ := im.SubjectFromContext(ctx)
	return OTAPeer{FabricIndex: fabric, NodeID: node}, InvokeSessionIDFromContext(ctx), nil
}

func (p OTAPeer) bdx() bdx.Peer { return bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID} }

func seconds(d time.Duration) *uint32 {
	s := uint32(min(max(math.Ceil(d.Seconds()), 0), math.MaxUint32))
	return &s
}

// QueryImage answers a requestor's QueryImage: the manager holds an
// attempt for the query, which ends with it unless the provider answers
// with an offer.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:143-159 at b25ed960
// (queryImage).
func (p *OTAProvider) QueryImage(ctx context.Context, req provdef.QueryImageRequest) (provdef.QueryImageResponse, error) {
	peer, sessionID, err := remotePeer(ctx)
	if err != nil {
		return provdef.QueryImageResponse{}, err
	}
	attempt := p.mgr.BeginQuery(peer)
	defer p.mgr.EndQuery(attempt)
	return p.answerQuery(ctx, req, peer, sessionID, attempt)
}

// answerQuery answers the query; the offer is recorded after the last wait
// on a host port, so nothing changes the decision before the answer goes.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:161-315 at b25ed960
// (#answerQuery).
func (p *OTAProvider) answerQuery(ctx context.Context, req provdef.QueryImageRequest, peer OTAPeer, sessionID uint16, attempt *OTAUpdateAttempt) (provdef.QueryImageResponse, error) {
	img, ok, err := p.cfg.ImageSource.QueryImage(ctx, peer, req)
	if err != nil {
		return provdef.QueryImageResponse{}, err
	}
	if !ok {
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil // OP:173-178
	}
	if fd := img.FileDesignator; len(fd) == 0 || fd.Text() != string(fd) || slices.Contains(fd, '/') {
		return provdef.QueryImageResponse{}, fmt.Errorf("core: OTA image designator %q is not a plain file name", fd.Text())
	}
	if !p.mgr.AdmitQuery(ctx, peer, sessionID, req.SoftwareVersion) { // OP:188-194
		return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(otaBusyRetry)}, nil
	}
	token := make([]byte, otaUpdateTokenLength) // OP:196-197
	if _, err := io.ReadFull(p.cfg.Rand, token); err != nil {
		return provdef.QueryImageResponse{}, err
	}

	// OP:199-231: a requestor that may obtain the consent itself gets the
	// update without asking; else a required consent is asked for.
	delegated := img.ConsentRequired && p.mgr.MayDelegateConsentTo(peer, req)
	if img.ConsentRequired && !delegated {
		if resp, answered, err := p.askConsent(ctx, req, peer, img, token, attempt); answered || err != nil {
			return resp, err
		}
	}

	// OP:233-244: BDX synchronous preferred, HTTPS when that is all the
	// requestor supports and the image has a URI.
	viaBDX := p.cfg.BDX != nil && slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolBdxSynchronous)
	httpsURI := ""
	if !viaBDX && slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolHttps) {
		httpsURI = img.HTTPSURI
	}
	if !viaBDX && httpsURI == "" {
		return provdef.QueryImageResponse{Status: provdef.StatusDownloadProtocolNotSupported}, nil
	}

	if !p.mgr.OfferUpdate(attempt, token, img.SoftwareVersion) { // OP:246-248
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
	}
	p.reportStatus(attempt, OTAUpdateQuerying) // OP:249

	imageURI := httpsURI
	if viaBDX {
		uri, busy := p.offerBDX(peer, img, attempt)
		if busy {
			return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(otaBdxBusyRetry)}, nil
		}
		imageURI = uri
	}

	// OP:305-314.
	p.mgr.OfferAnswered(attempt)
	version, versionString := img.SoftwareVersion, img.SoftwareVersionString
	resp := provdef.QueryImageResponse{
		Status:                provdef.StatusUpdateAvailable,
		ImageUri:              &imageURI,
		SoftwareVersion:       &version,
		SoftwareVersionString: &versionString,
		UpdateToken:           &token,
	}
	if delegated {
		yes := true
		resp.UserConsentNeeded = &yes // OP:312: only a delegated consent is the requestor's to obtain
	}
	if len(img.MetadataForRequestor) > 0 {
		md := slices.Clone(img.MetadataForRequestor)
		resp.MetadataForRequestor = &md
	}
	return resp, nil
}

// askConsent asks the host for the consent the requestor cannot give: it
// reports whether the query is answered — Busy while the consent is being
// obtained, NotAvailable when it is not granted — and nothing when it is
// granted, which lets the offer go on.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:202-231 at b25ed960
// (the consent of the query's answer).
func (p *OTAProvider) askConsent(ctx context.Context, req provdef.QueryImageRequest, peer OTAPeer, img OTAImage, token []byte, attempt *OTAUpdateAttempt) (provdef.QueryImageResponse, bool, error) {
	p.reportStatus(attempt, OTAUpdateWaitForConsent)
	state, delay := OTAConsentUnknown, time.Duration(0)
	if p.cfg.Consent != nil {
		var err error
		if state, delay, err = p.cfg.Consent.RequestConsent(ctx, peer, req, img); err != nil {
			return provdef.QueryImageResponse{}, true, err
		}
	}
	if delay == 0 {
		delay = otaConsentRetry
	}
	p.logger.Info("ota.provider.consent", slog.Uint64("node_id", peer.NodeID), slog.String("token", hex.EncodeToString(token)), slog.Int("state", int(state)))
	switch state {
	case OTAConsentGranted:
		return provdef.QueryImageResponse{}, false, nil
	case OTAConsentObtaining:
		return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(delay)}, true, nil
	default:
		p.reportStatus(attempt, OTAUpdateCancelled)
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, true, nil
	}
}

// offerBDX enables the requestor for the "ota" scope as the following
// sender of a receiver-driven transfer with a five-minute idle timeout and
// the manager's transfer options, and reports the transfers of the scope
// as the attempt's statuses until the attempt ends. It reports busy when
// the scope is held with other details.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:251-300 at b25ed960
// (the BDX offer of the query's answer).
func (p *OTAProvider) offerBDX(peer OTAPeer, img OTAImage, attempt *OTAUpdateAttempt) (string, bool) {
	responder := p.cfg.BDX
	opts := p.mgr.BDXTransferOptionsFor(peer)
	opts.PreferredDriverModes = []bdx.DriverMode{bdx.ReceiverDrive}
	opts.MessageTimeout = otaBdxMessageTimeout
	if !responder.EnablePeerForScope(peer.bdx(), otaScope, p.storage, opts) {
		p.logger.Info("ota.provider.bdx_not_enabled", slog.Uint64("node_id", peer.NodeID))
		return "", true
	}
	off := responder.OnSessionStarted(func(s *bdx.ServerSession, scope string) {
		sp := s.Peer()
		if scope != otaScope || sp.FabricIndex != peer.FabricIndex || sp.NodeID != peer.NodeID {
			return
		}
		// A failed download is retried on a fresh session, so the listener
		// stays until the attempt ends (OP:277-283); a transfer in flight
		// keeps the attempt alive.
		p.reportStatus(attempt, OTAUpdateDownloading)
		s.OnProgress(func(uint64, *uint64) { attempt.Touch() })
		s.OnFinished(func(uint64) { p.reportStatus(attempt, OTAUpdateWaitForApply) })
		s.OnCancelled(func() { p.reportStatus(attempt, OTAUpdateCancelled) })
		// OP:284-293: the closed transfer cancels an attempt not yet
		// settled; the check and the report are one step of the manager.
		s.OnClosed(func() {
			p.reportStatus(attempt, OTAUpdateCancelled,
				OTAUpdateWaitForApply, OTAUpdateApplying, OTAUpdateDone, OTAUpdateCancelled)
		})
	})
	attempt.OnEnd(off) // OP:296

	nodeID, ok := p.cfg.NodeID(peer.FabricIndex)
	if !ok {
		nodeID = 0
	}
	// OP:300; the image is `ota/<file>` (SoftwareUpdateManager.ts:454).
	return bdx.FileDesignator(otaScope + "/" + string(img.FileDesignator)).BdxURI(nodeID), false
}

// ApplyUpdateRequest decides a requestor's ApplyUpdateRequest: the
// transfer of the scope is closed — forced unless the attempt of the token
// waits for its apply — and the manager's apply table decides, Proceed
// after the apply delay or Discontinue for two minutes.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:317-363 at b25ed960
// (applyUpdateRequest).
func (p *OTAProvider) ApplyUpdateRequest(ctx context.Context, req provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateResponse, error) {
	peer, _, err := remotePeer(ctx)
	if err != nil {
		return provdef.ApplyUpdateResponse{}, err
	}
	attempt := p.mgr.AttemptFor(peer, req.UpdateToken)
	if p.cfg.BDX != nil {
		// OP:334-344: a transfer not known as completed is forced closed. A
		// completed transfer is known: its final BlockAckEof finished it —
		// WaitForApply — at its arrival, before the receive path took the
		// ApplyUpdateRequest sent after it.
		force := attempt == nil || attempt.Status() != OTAUpdateWaitForApply
		if err := p.cfg.BDX.DisablePeerForScope(peer.bdx(), otaScope, force); err != nil {
			p.logger.Info("ota.provider.bdx_close_failed", slog.Any("err", err))
		}
	}
	delay := p.cfg.ApplyDelay
	if p.cfg.ApplyPolicy != nil {
		resp, answered, d, err := p.applyPolicy(ctx, peer, req, attempt)
		if answered || err != nil {
			return resp, err
		}
		delay = d
	}
	// OP:346-351: whole seconds rounded up, at most a day.
	delayed := min(*seconds(delay), otaMaxDelayedActionTime)
	version := req.NewVersion
	if version == 0 && attempt != nil {
		version, _ = attempt.Version()
	}
	if !p.mgr.ApplyUpdate(ctx, peer, req.UpdateToken, req.NewVersion, time.Duration(delayed)*time.Second) { // OP:352-358
		if attempt != nil {
			p.observe(peer, OTAUpdateCancelled, version)
		}
		return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionDiscontinue, DelayedActionTime: *seconds(otaBusyRetry)}, nil
	}
	p.observe(peer, OTAUpdateApplying, version)
	return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed, DelayedActionTime: delayed}, nil
}

// applyPolicy asks the deprecated [OTAApplyPolicy] before the manager's
// table: Proceed hands the decision to the table with the policy's delay,
// in place of ApplyDelay;
// any other action answers at once — Discontinue cancels the update the
// token names, as the table's own refusal does — so the policy only adds
// a refusal. The policy sees version 0 as the token's version.
func (p *OTAProvider) applyPolicy(ctx context.Context, peer OTAPeer, req provdef.ApplyUpdateRequestRequest, attempt *OTAUpdateAttempt) (provdef.ApplyUpdateResponse, bool, time.Duration, error) {
	if req.NewVersion == 0 && attempt != nil {
		if v, ok := attempt.Version(); ok {
			req.NewVersion = v
		}
	}
	action, delay, err := p.cfg.ApplyPolicy.ApplyUpdate(ctx, peer, req)
	if err != nil {
		return provdef.ApplyUpdateResponse{}, true, 0, err
	}
	if action == provdef.ApplyUpdateActionProceed {
		return provdef.ApplyUpdateResponse{}, false, delay, nil
	}
	if action == provdef.ApplyUpdateActionDiscontinue && attempt != nil {
		p.reportStatus(attempt, OTAUpdateCancelled)
	}
	return provdef.ApplyUpdateResponse{Action: action, DelayedActionTime: min(*seconds(delay), otaMaxDelayedActionTime)}, true, 0, nil
}

// NotifyUpdateApplied hands the requestor's NotifyUpdateApplied to the
// manager.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts:365-380 at b25ed960
// (notifyUpdateApplied).
func (p *OTAProvider) NotifyUpdateApplied(ctx context.Context, req provdef.NotifyUpdateAppliedRequest) error {
	peer, _, err := remotePeer(ctx)
	if err != nil {
		return err
	}
	p.mgr.UpdateApplied(peer, req.UpdateToken, req.SoftwareVersion)
	p.observe(peer, OTAUpdateDone, req.SoftwareVersion)
	return nil
}

// reportStatus reports a status of the attempt to the manager and, when
// the manager recorded it, to [OTAProviderConfig].OnAttemptStatus.
func (p *OTAProvider) reportStatus(a *OTAUpdateAttempt, status OTAUpdateStatus, unless ...OTAUpdateStatus) {
	if p.mgr.ReportAttemptStatus(a, status, unless...) {
		v, _ := a.Version()
		p.observe(a.Peer(), status, v)
	}
}

// observe tells [OTAProviderConfig].OnAttemptStatus of a status.
func (p *OTAProvider) observe(peer OTAPeer, status OTAUpdateStatus, version uint32) {
	if fn := p.cfg.OnAttemptStatus; fn != nil {
		fn(peer, status, version)
	}
}

// MatterDropFabric implements [contract.FabricScopedState]: the manager,
// when it holds fabric-scoped state ([NewOTAStandaloneManager] does),
// forgets the removed fabric's update attempts and withdraws its
// requestors' transfer scope, so a later fabric that receives the index
// inherits none of it (review II, R2-P6-bound-1206, owner decision 167).
// matter.js ends nothing at a fabric's removal.
func (p *OTAProvider) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	if fs, ok := p.mgr.(contract.FabricScopedState); ok {
		return fs.MatterDropFabric(ctx, fabricIndex)
	}
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// the manager holds update attempts of, when it reports them.
func (p *OTAProvider) MatterHeldFabrics() []uint8 {
	if fs, ok := p.mgr.(contract.FabricScopedState); ok {
		return fs.MatterHeldFabrics()
	}
	return nil
}

// MatterAcceptedCommands implements contract.ClusterCommandLister.
func (p *OTAProvider) MatterAcceptedCommands() []uint32 {
	return otaProviderInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements contract.ClusterCommandLister.
func (p *OTAProvider) MatterGeneratedCommands() []uint32 {
	return otaProviderInst.MatterGeneratedCommands()
}
