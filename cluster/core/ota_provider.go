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
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/im"
)

// OTAProvider is the OTA Software Update Provider (0x0029): it answers an
// OTA requestor's QueryImage with an image its host's [OTAImageSource]
// names, serves the image over BDX as the following sender of the
// requestor's receiver-driven download, allows the update in
// ApplyUpdateRequest and closes it on NotifyUpdateApplied.
//
// Mirrors matter.js packages/node/src/behaviors/ota-software-update-provider/
// OtaSoftwareUpdateProviderServer.ts at 0ceb202d (OP below). What matter.js
// decides in its SoftwareUpdateManager — which image a peer gets, whether
// the user consented, how long an allowed apply waits — are the host's
// ports here: [OTAImageSource] (checkUpdateAvailable, OP:497-502),
// [OTAConsent] (requestUserConsentForUpdate, OP:483-489) and
// [OTAApplyPolicy] (State.applyDelay, OP:623-631). The DCL lookups and the
// peer's BasicInformation validation of SoftwareUpdateManager.updateExistsFor
// are the image source's to do, or not.
type OTAProvider struct {
	*spec.Server

	cfg     OTAProviderConfig
	storage *otaImageStorage
	logger  *slog.Logger

	mu sync.Mutex
	// inProgress is internal.inProgressDetails (OP:633-636), keyed by the
	// requestor and the update token.
	inProgress map[string]*otaInProgress
	// consents are the versions the image source offered a peer without
	// asking for consent: SoftwareUpdateManager reports consentRequired
	// false only for a peer it holds a consent for (SoftwareUpdateManager.ts:
	// 417-446), which hasConsent then finds (:1150-1157, read at OP:504-509).
	consents map[OTAPeer]uint32
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
	// FileDesignatorUnknown on the BDX exchange.
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

// OTAApplyPolicy decides an ApplyUpdateRequest the provider allows — after
// the consent check of OP:430-437 passed: Proceed and how long the
// requestor waits before it applies (matter.js State.applyDelay,
// OP:623-631), or AwaitNextAction or Discontinue with the delay before it
// asks again (Matter Core §11.20.6.5.4). A nil policy is matter.js's
// default: Proceed at once.
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
	// ApplyPolicy decides an allowed apply; nil proceeds at once.
	ApplyPolicy OTAApplyPolicy
	// BDX is the node's BDX responder the images are served through
	// (matter.js BdxProtocol, OP:291-306); nil offers no BDX download.
	BDX *bdx.Responder
	// BDXMaxBlockSize, when set, caps the block size of the image
	// transfers (SoftwareUpdateManager.bdxTransferOptionsFor maxBlockSize,
	// OP:297).
	BDXMaxBlockSize uint16
	// NodeID returns this node's id on a fabric, which the BDX URI names
	// (OP:381: the session's associatedFabric.rootNodeId); mandatory with
	// BDX.
	NodeID func(fabricIndex uint8) (uint64, bool)
	// OnStatusChange is told every change of a requestor's update state —
	// matter.js SoftwareUpdateManager.onOtaStatusChange (OP:608-612) — with
	// the version the update brings; nil observes nothing.
	OnStatusChange func(peer OTAPeer, status OTAUpdateStatus, version uint32)
	// Now and Rand stand in for the clock and crypto/rand in tests.
	Now  func() time.Time
	Rand io.Reader
	// Logger is the provider's logger; nil is slog.Default().
	Logger *slog.Logger
}

// otaInProgress is OtaUpdateInProgressDetails (OP:55-64).
type otaInProgress struct {
	peer                  OTAPeer
	lastState             OTAUpdateStatus
	timestamp             time.Time
	versionToApply        *uint32
	directConsentObtained bool
	cleanup               func()
}

// The constants of the provider.
const (
	// otaMaxDelayedActionTime caps DelayedActionTime (OP:46-51,
	// MAX_DELAYED_ACTION_TIME = 86_400 s).
	otaMaxDelayedActionTime = 86_400
	// otaUpdateTokenLength is OTA_UPDATE_TOKEN_LENGTH_BYTES (OP:53).
	otaUpdateTokenLength = 32
	// otaBusyRetry is the Busy answer's DelayedActionTime while an update
	// is in flight (OP:218, :235: Seconds.of(Minutes(2))) and the
	// Discontinue answer's (OP:435).
	otaBusyRetry = 2 * time.Minute
	// otaBdxBusyRetry is the Busy answer when BDX could not be enabled
	// (OP:302-305: Minutes(5)).
	otaBdxBusyRetry = 5 * time.Minute
	// otaConsentRetry is the default delay of an Obtaining consent
	// (OP:248: Seconds(120)).
	otaConsentRetry = 120 * time.Second
	// otaProgressTimeout makes an in-progress entry stale: matter.js
	// OTA_PROGRESS_TIMEOUT (OtaProgress.ts:15, Minutes(15), read at
	// OP:537).
	otaProgressTimeout = 15 * time.Minute
	// otaBdxMessageTimeout is the image transfer's idle timeout (OP:296:
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
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	p := &OTAProvider{
		cfg: cfg, logger: logger,
		storage:    &otaImageStorage{source: cfg.ImageSource},
		inProgress: map[string]*otaInProgress{},
		consents:   map[OTAPeer]uint32{},
	}
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
// command arrived on.
func remotePeer(ctx context.Context) (OTAPeer, uint16, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	node, _ := im.SubjectFromContext(ctx)
	if fabric == 0 || node == 0 || im.IsPASEFromContext(ctx) {
		return OTAPeer{}, 0, otaStatusError{im.StatusUnsupportedAccess, "OTA provider commands need a CASE session"}
	}
	return OTAPeer{FabricIndex: fabric, NodeID: node}, InvokeSessionIDFromContext(ctx), nil
}

func (p OTAPeer) bdx() bdx.Peer { return bdx.Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID} }

func otaKey(peer OTAPeer, token []byte) string {
	return fmt.Sprintf("%d-%d-%s", peer.NodeID, peer.FabricIndex, hex.EncodeToString(token))
}

func seconds(d time.Duration) *uint32 {
	s := uint32(min(max(math.Ceil(d.Seconds()), 0), math.MaxUint32))
	return &s
}

// QueryImage mirrors OP:160-402.
func (p *OTAProvider) QueryImage(ctx context.Context, req provdef.QueryImageRequest) (provdef.QueryImageResponse, error) {
	peer, sessionID, err := remotePeer(ctx)
	if err != nil {
		return provdef.QueryImageResponse{}, err
	}
	img, ok, err := p.cfg.ImageSource.QueryImage(ctx, peer, req)
	if err != nil {
		return provdef.QueryImageResponse{}, err
	}
	if !ok {
		// OP:173-178.
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
	}
	if fd := img.FileDesignator; len(fd) == 0 || fd.Text() != string(fd) || slices.Contains(fd, '/') {
		return provdef.QueryImageResponse{}, fmt.Errorf("core: OTA image designator %q is not a plain file name", fd.Text())
	}

	// OP:188-238: an update already in progress for this peer.
	if p.detailsForPeer(peer, nil) != nil {
		if resp, busy := p.inProgressAnswer(peer, sessionID); busy {
			return resp, nil
		}
	}

	token := make([]byte, otaUpdateTokenLength) // OP:240-241
	if _, err := io.ReadFull(p.cfg.Rand, token); err != nil {
		return provdef.QueryImageResponse{}, err
	}
	canConsent := req.RequestorCanConsent != nil && *req.RequestorCanConsent
	if !img.ConsentRequired {
		p.mu.Lock()
		p.consents[peer] = img.SoftwareVersion
		p.mu.Unlock()
	}

	// OP:243-284: the requestor that can consent gets the update without
	// asking; else a required consent is asked for.
	if img.ConsentRequired && !canConsent {
		p.updateInProgress(peer, token, OTAUpdateWaitForConsent, &img.SoftwareVersion, false)
		state, delay := OTAConsentUnknown, time.Duration(0)
		if p.cfg.Consent != nil {
			if state, delay, err = p.cfg.Consent.RequestConsent(ctx, peer, req, img); err != nil {
				p.removeInProgress(peer)
				return provdef.QueryImageResponse{}, err
			}
		}
		if delay == 0 {
			delay = otaConsentRetry
		}
		p.logger.Info("ota.provider.consent", slog.Uint64("peer", peer.NodeID), slog.String("token", hex.EncodeToString(token)), slog.Int("state", int(state)))
		switch state {
		case OTAConsentGranted:
			p.updateInProgress(peer, token, OTAUpdateQuerying, &img.SoftwareVersion, true)
		case OTAConsentObtaining:
			return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(delay)}, nil
		default:
			p.removeInProgress(peer)
			return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
		}
	} else {
		p.updateInProgress(peer, token, OTAUpdateQuerying, &img.SoftwareVersion, false)
	}

	// OP:286-391: BDX synchronous preferred, HTTPS when that is all the
	// requestor supports and the image has a URI.
	var imageURI string
	switch {
	case p.cfg.BDX != nil && slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolBdxSynchronous):
		uri, resp, busy := p.offerBDX(peer, token, img)
		if busy {
			return resp, nil
		}
		imageURI = uri
	case img.HTTPSURI != "" && slices.Contains(req.ProtocolsSupported, provdef.DownloadProtocolHttps):
		imageURI = img.HTTPSURI
	default:
		return provdef.QueryImageResponse{Status: provdef.StatusDownloadProtocolNotSupported}, nil
	}

	// OP:393-401.
	version, versionString := img.SoftwareVersion, img.SoftwareVersionString
	resp := provdef.QueryImageResponse{
		Status:                provdef.StatusUpdateAvailable,
		ImageUri:              &imageURI,
		SoftwareVersion:       &version,
		SoftwareVersionString: &versionString,
		UpdateToken:           &token,
	}
	if img.ConsentRequired {
		yes := true
		resp.UserConsentNeeded = &yes
	}
	if len(img.MetadataForRequestor) > 0 {
		md := slices.Clone(img.MetadataForRequestor)
		resp.MetadataForRequestor = &md
	}
	return resp, nil
}

// inProgressAnswer is OP:189-237 for a peer with an update in progress: a
// stale entry without a running transfer is dropped, a transfer on another
// session older than this query is taken for the requestor's reboot and
// cancelled, and anything else is Busy for two minutes. It reports whether
// the query is answered.
func (p *OTAProvider) inProgressAnswer(peer OTAPeer, sessionID uint16) (provdef.QueryImageResponse, bool) {
	busy := provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(otaBusyRetry)}
	var running *bdx.ServerSession
	if p.cfg.BDX != nil {
		running = p.cfg.BDX.SessionFor(peer.bdx(), otaScope)
	}
	switch {
	case running == nil:
		// OP:194-196: no transfer runs, the entry is stale.
		p.removeInProgress(peer)
		return provdef.QueryImageResponse{}, false
	case running.Peer().SessionID != sessionID && running.LastActivity().Before(time.Now()):
		// OP:197-225: another session, last active before this query's —
		// matter.js compares the two secure sessions' activeTimestamp; the
		// query's session is active now, so the transfer's last block
		// stands in for its session's last activity. The old transfer is
		// cancelled and the query proceeds.
		if err := p.cfg.BDX.DisablePeerForScope(peer.bdx(), otaScope, true); err != nil {
			p.logger.Warn("ota.provider.reboot_cancel_failed", slog.String("err", err.Error()))
			return busy, true
		}
		p.removeInProgress(peer)
		return provdef.QueryImageResponse{}, false
	}
	p.logger.Info("ota.provider.busy", slog.Uint64("peer", peer.NodeID))
	return busy, true
}

// offerBDX is OP:290-381: the requestor is enabled for the "ota" scope as
// the following sender of a receiver-driven transfer with a five-minute
// idle timeout, and its transfer sessions move the update's state.
func (p *OTAProvider) offerBDX(peer OTAPeer, token []byte, img OTAImage) (string, provdef.QueryImageResponse, bool) {
	responder := p.cfg.BDX
	if !responder.EnablePeerForScope(peer.bdx(), otaScope, p.storage, bdx.ScopeConfig{
		PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive},
		MessageTimeout:       otaBdxMessageTimeout,
		MaxBlockSize:         p.cfg.BDXMaxBlockSize,
	}) {
		// OP:299-305.
		p.logger.Info("ota.provider.bdx_not_enabled", slog.Uint64("peer", peer.NodeID))
		return "", provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: seconds(otaBdxBusyRetry)}, true
	}
	version := img.SoftwareVersion
	off := responder.OnSessionStarted(func(s *bdx.ServerSession, scope string) {
		sp := s.Peer()
		if scope != otaScope || sp.FabricIndex != peer.FabricIndex || sp.NodeID != peer.NodeID {
			return
		}
		// OP:314-320: a failed download is retried on a fresh session, so
		// the listener stays for the whole update.
		details := p.detailsForPeer(peer, token)
		if details == nil {
			return
		}
		p.updateInProgress(peer, token, OTAUpdateDownloading, &version, false)
		s.OnProgress(func(uint64, *uint64) { // OP:329-331
			p.mu.Lock()
			details.timestamp = p.cfg.Now()
			p.mu.Unlock()
		})
		s.OnFinished(func(uint64) { p.updateInProgress(peer, token, OTAUpdateWaitForApply, &version, false) })
		s.OnCancelled(func() { p.updateInProgress(peer, token, OTAUpdateCancelled, &version, false) })
		s.OnClosed(func() { // OP:348-364
			if d := p.detailsForPeer(peer, token); d != nil {
				switch d.lastState {
				case OTAUpdateWaitForApply, OTAUpdateApplying, OTAUpdateDone, OTAUpdateCancelled:
				default:
					p.updateInProgress(peer, token, OTAUpdateCancelled, &version, false)
				}
			}
		})
	})
	// OP:368-377: the entry's cleanup unregisters the listener.
	p.mu.Lock()
	if d := p.inProgress[otaKey(peer, token)]; d != nil {
		d.cleanup = off
	} else {
		off()
	}
	p.mu.Unlock()

	nodeID, ok := p.cfg.NodeID(peer.FabricIndex)
	if !ok {
		nodeID = 0
	}
	// OP:381; the image is `ota/<file>` (SoftwareUpdateManager.ts:454).
	return bdx.FileDesignator(otaScope + "/" + string(img.FileDesignator)).BdxURI(nodeID), provdef.QueryImageResponse{}, false
}

// ApplyUpdateRequest mirrors OP:404-460.
func (p *OTAProvider) ApplyUpdateRequest(ctx context.Context, req provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateResponse, error) {
	peer, _, err := remotePeer(ctx)
	if err != nil {
		return provdef.ApplyUpdateResponse{}, err
	}
	details := p.detailsForPeer(peer, req.UpdateToken)
	if p.cfg.BDX != nil {
		// OP:418-428: the transfer is closed — forced when it is not known
		// to have completed.
		force := details == nil || details.lastState != OTAUpdateWaitForApply
		if err := p.cfg.BDX.DisablePeerForScope(peer.bdx(), otaScope, force); err != nil {
			p.logger.Info("ota.provider.bdx_close_failed", slog.String("err", err.Error()))
		}
	}
	newVersion := req.NewVersion
	if !p.hasUpdateConsent(peer, newVersion) {
		// OP:430-437.
		p.updateInProgress(peer, req.UpdateToken, OTAUpdateCancelled, &newVersion, false)
		return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionDiscontinue, DelayedActionTime: *seconds(otaBusyRetry)}, nil
	}
	action, delay := provdef.ApplyUpdateActionProceed, time.Duration(0)
	if p.cfg.ApplyPolicy != nil {
		if action, delay, err = p.cfg.ApplyPolicy.ApplyUpdate(ctx, peer, req); err != nil {
			return provdef.ApplyUpdateResponse{}, err
		}
	}
	// OP:441-446: whole seconds rounded up, at most a day.
	delayed := min(*seconds(delay), otaMaxDelayedActionTime)
	switch action {
	case provdef.ApplyUpdateActionProceed:
		p.updateInProgress(peer, req.UpdateToken, OTAUpdateApplying, &newVersion, false)
	case provdef.ApplyUpdateActionDiscontinue:
		p.updateInProgress(peer, req.UpdateToken, OTAUpdateCancelled, &newVersion, false)
	case provdef.ApplyUpdateActionAwaitNextAction:
		// The update stays where it is; the requestor asks again.
	}
	return provdef.ApplyUpdateResponse{Action: action, DelayedActionTime: delayed}, nil
}

// NotifyUpdateApplied mirrors OP:462-477: the update is done, its entry
// and the consent it used are gone (SoftwareUpdateManager.ts:1219-1232).
func (p *OTAProvider) NotifyUpdateApplied(ctx context.Context, req provdef.NotifyUpdateAppliedRequest) error {
	peer, _, err := remotePeer(ctx)
	if err != nil {
		return err
	}
	version := req.SoftwareVersion
	p.updateInProgress(peer, req.UpdateToken, OTAUpdateDone, &version, false)
	p.mu.Lock()
	if v, ok := p.consents[peer]; ok && v <= version {
		delete(p.consents, peer)
	}
	p.mu.Unlock()
	return nil
}

// hasUpdateConsent is OP:504-509: a consent obtained for the update in
// progress, or the one the image source stated for the version.
func (p *OTAProvider) hasUpdateConsent(peer OTAPeer, version uint32) bool {
	if d := p.detailsForPeer(peer, nil); d != nil && d.directConsentObtained {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.consents[peer]
	return ok && v == version
}

// detailsForPeer is #inProgressDetailsForPeer (OP:511-534): the entry of
// the token, or the peer's first; a stale one is removed and not returned.
func (p *OTAProvider) detailsForPeer(peer OTAPeer, token []byte) *otaInProgress {
	p.mu.Lock()
	var cleanup func()
	var out *otaInProgress
	now := p.cfg.Now()
	check := func(key string, d *otaInProgress) {
		if d.timestamp.Add(otaProgressTimeout).Before(now) {
			// OP:536-548.
			p.logger.Info("ota.provider.stale_entry", slog.Uint64("peer", peer.NodeID), slog.Duration("age", now.Sub(d.timestamp)))
			cleanup = d.cleanup
			delete(p.inProgress, key)
			return
		}
		out = d
	}
	if token != nil {
		key := otaKey(peer, token)
		if d := p.inProgress[key]; d != nil {
			check(key, d)
		}
	} else {
		for _, key := range p.sortedKeysLocked() {
			if d := p.inProgress[key]; d.peer == peer {
				check(key, d)
				break
			}
		}
	}
	p.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
	return out
}

// sortedKeysLocked returns the entry keys in a stable order (a JavaScript
// Map iterates in insertion order; one peer has one entry at a time).
func (p *OTAProvider) sortedKeysLocked() []string {
	keys := make([]string, 0, len(p.inProgress))
	for k := range p.inProgress {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// removeInProgress is #removeInProgressDetails (OP:550-561).
func (p *OTAProvider) removeInProgress(peer OTAPeer) {
	p.mu.Lock()
	var cleanups []func()
	for key, d := range p.inProgress {
		if d.peer == peer {
			if d.cleanup != nil {
				cleanups = append(cleanups, d.cleanup)
			}
			delete(p.inProgress, key)
		}
	}
	p.mu.Unlock()
	for _, fn := range cleanups {
		fn()
	}
}

// updateInProgress is #updateInProgressDetails (OP:563-619). Every call
// sets directConsentObtained to its argument, false by default (:572,
// :598), as matter.js does.
func (p *OTAProvider) updateInProgress(peer OTAPeer, token []byte, state OTAUpdateStatus, version *uint32, directConsent bool) {
	key := otaKey(peer, token)
	now := p.cfg.Now()
	p.mu.Lock()
	d := p.inProgress[key]
	previous := OTAUpdateUnknown
	if d == nil {
		d = &otaInProgress{peer: peer}
	} else {
		previous = d.lastState
	}
	d.lastState = state
	d.timestamp = now
	if version != nil {
		if d.versionToApply != nil && *d.versionToApply != *version {
			p.logger.Warn("ota.provider.version_changed", slog.Uint64("from", uint64(*d.versionToApply)), slog.Uint64("to", uint64(*version)))
		}
		v := *version
		d.versionToApply = &v
	}
	d.directConsentObtained = directConsent
	p.inProgress[key] = d
	var cleanup func()
	if state == OTAUpdateDone || state == OTAUpdateCancelled {
		// OP:614-618: a terminal state does not block future updates.
		cleanup = d.cleanup
		d.cleanup = nil
		delete(p.inProgress, key)
	}
	var applied uint32
	if d.versionToApply != nil {
		applied = *d.versionToApply
	}
	p.mu.Unlock()
	p.logger.Info("ota.provider.status", slog.Uint64("peer", peer.NodeID), slog.String("state", state.String()), slog.String("previous", previous.String()))
	if fn := p.cfg.OnStatusChange; fn != nil {
		fn(peer, state, applied)
	}
	if cleanup != nil {
		cleanup()
	}
}

// MatterAcceptedCommands implements contract.ClusterCommandLister.
func (p *OTAProvider) MatterAcceptedCommands() []uint32 {
	return otaProviderInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements contract.ClusterCommandLister.
func (p *OTAProvider) MatterGeneratedCommands() []uint32 {
	return otaProviderInst.MatterGeneratedCommands()
}
