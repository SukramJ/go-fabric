// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// OTASoftwareUpdateRequestor is the OTA Software Update Requestor (0x002A)
// on the root endpoint: it asks the OTA providers it knows for a newer
// image, downloads one over BDX, has the host apply it and reports the
// outcome (ADR 0019).
//
// Mirrors matter.js packages/node/src/behaviors/
// ota-software-update-requestor/OtaSoftwareUpdateRequestorServer.ts, rule
// for rule; each method names the lines it transcribes. Where the Go
// surface differs:
//
//   - matter.js's behavior state and transactions become a mutex; a query
//     runs on its own goroutine, as matter.js's reactor timer runs it.
//   - The host's extension interface (:1431-1435, requestUserConsent and
//     applyUpdate) is the [OtaApplier] port; validateUpdateFile is not
//     overridable — the host validates further in ApplyUpdate.
//   - The download location (:201-217, a blob "ota-<vendor>-<product>" in
//     the node's BDX store) is the file of that name in a host-provided
//     directory, written through a temporary file ([bdx.FileSink]).
//   - HTTPS downloads (#handleHttpsDownload, :1008-1062) are not ported:
//     the requestor offers BDX only, matter.js's default
//     transferProtocolsSupported (:1372).
//   - DefaultOTAProviders is fabric-scoped, which spec.Server refuses
//     (spec.ErrFabricScoped), so the server is built on [spec.Instance] as
//     EcosystemInformation is.
//   - The OtaRequestor device type that initialize adds to the endpoint
//     (:181) is the host's to list in the root Descriptor:
//     [OtaRequestorDeviceType].
type OTASoftwareUpdateRequestor struct {
	*spec.Instance
	cluster.AttributeChanges

	cfg    OTARequestorConfig
	clock  otaClock
	rnd    func(int) int
	logger *slog.Logger
	dv     cluster.DataVersionTracker

	// mu guards the emitter, the endpoint, the attribute state, the
	// persisted state and the query state: every field below it up to
	// stopped, wg aside (a WaitGroup).
	mu       sync.Mutex
	emitter  contract.EventEmitter
	endpoint uint16

	// Attribute state.
	defaults       []otadef.ProviderLocation
	updatePossible bool
	state          otadef.UpdateStateEnum
	progress       *uint8

	// Persisted state beside the attributes (:1354, :1362).
	active     []activeProvider
	inProgress *updateInProgress

	// Internal state (:1397-1429).
	timer           *queryTimer
	queryRunning    bool
	selected        *otadef.ProviderLocation
	providerRetries int
	pendingApply    *pendingApply
	// announcedDuringQuery are the announcements that arrived while a
	// query ran, handled once it ends (announceOtaProvider).
	announcedDuringQuery []otaAnnouncement
	runCtx               context.Context
	cancelRun            context.CancelFunc
	wg                   sync.WaitGroup
	started, stopped     bool
}

// OTARequestorConfig configures [NewOTASoftwareUpdateRequestor].
type OTARequestorConfig struct {
	// NodeInformation reads the root's BasicInformation, as matter.js's
	// #basicInformationState does (:1214-1220). Required;
	// [BasicInformation.OtaNodeInformation] is the usual source.
	NodeInformation func() OtaNodeInformation
	// Connector reaches a provider; nil fails every query as a provider
	// that cannot be reached (*bridge.Bridge implements it).
	Connector OtaProviderConnector
	// Applier is the host's extension interface; nil makes consent and
	// apply fail as matter.js's unimplemented defaults do (:1293-1304).
	Applier OtaApplier
	// Store persists DefaultOTAProviders and the requestor state; nil keeps
	// both in memory. Every store.Port implements it.
	Store OtaRequestorStore
	// DownloadDir is the directory the image is downloaded to. Required.
	DownloadDir string
	// UpdatePossible is the attribute's initial value; nil is the model's
	// default, true.
	UpdatePossible *bool
	// CanConsent says the host can ask the user for consent (:1381,
	// default false).
	CanConsent bool
	// UpdateQueryInterval is how often a provider is queried (:1365,
	// default 24 h).
	UpdateQueryInterval time.Duration
	// MinimumQueryInterval floors the delay a Busy answer names (:1335,
	// default 120 s; lowering it does not conform).
	MinimumQueryInterval time.Duration
	// MinimumApplyDelay floors the delay an AwaitNextAction answer names
	// (:1346, default 2 min; lowering it does not conform).
	MinimumApplyDelay time.Duration
	// AnnouncedUpdateQueryDelay, when set, replaces the random 1-600 s wait
	// after an announcement other than a simple one (:1323).
	AnnouncedUpdateQueryDelay *time.Duration
	// TimeSource is the requestor's clock; nil is [clock.System].
	TimeSource clock.Clock
	// Rand replaces the random source (tests).
	Rand func(n int) int
	// Logger receives the requestor's log lines; nil is slog.Default.
	Logger *slog.Logger
}

// OtaNodeInformation is what the requestor reads from BasicInformation.
type OtaNodeInformation struct {
	VendorID              uint16
	ProductID             uint16
	SoftwareVersion       uint32
	SoftwareVersionString string
	HardwareVersion       uint16
	Location              string
	LocalConfigDisabled   bool
}

// OtaProviderConnector reaches an OTA provider over a secure session this
// node initiates (matter.js #connectOtaProviderFor, :553-565).
type OtaProviderConnector interface {
	ConnectOtaProvider(ctx context.Context, fabricIndex uint8, providerNodeID uint64) (OtaProviderSession, error)
}

// OtaProviderSession is a secure session with a provider.
type OtaProviderSession interface {
	// Invoke sends one command and returns the fields of the response
	// command, nil for a status-only Success; another status is an error.
	Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error)
	// OpenExchange opens a new exchange on the session for a BDX transfer.
	// An exchange that also has a `Reliable() bool` method reporting true
	// runs over TCP, and the requestor asks for 8192-byte blocks on it
	// instead of 1024 (matter.js `messenger.channel.type === "tcp"`,
	// :962-963); the bridge's exchanges have it.
	OpenExchange(ctx context.Context) (bdx.Exchange, error)
}

// reliableExchange is the optional capability of an [OtaProviderSession]
// exchange that runs over a reliable (TCP) channel.
type reliableExchange interface {
	Reliable() bool
}

// OtaApplier is the host's part of an update: matter.js's extension
// interface (:1431-1435).
type OtaApplier interface {
	// RequestUserConsent asks the user whether to update to the version;
	// asked only when CanConsent is set and the provider wants consent
	// (requestUserConsent, :1297-1304).
	RequestUserConsent(ctx context.Context, newSoftwareVersion uint32, newSoftwareVersionString string) (bool, error)
	// ApplyUpdate applies the downloaded, validated image at file
	// (applyUpdate, :1293-1295). A product restarts into the new version,
	// after which [OTASoftwareUpdateRequestor.Start] reports it applied. An
	// update that fails, or a node that keeps running the old version,
	// deletes the file: a file left behind is applied again at the next
	// query without asking the provider (:153-159).
	ApplyUpdate(ctx context.Context, newSoftwareVersion uint32, file string) error
}

// OtaRequestorStore persists the requestor: the store.OTA area, which every
// store.Port implements.
type OtaRequestorStore interface {
	ListDefaultOTAProviders(ctx context.Context) ([]store.OTAProviderLocation, error)
	ReplaceDefaultOTAProviders(ctx context.Context, fabricIndex uint8, entries []store.OTAProviderLocation) error
	LoadOTARequestorState(ctx context.Context) ([]byte, bool, error)
	SaveOTARequestorState(ctx context.Context, state []byte) error
}

// otaClock is the requestor's time source.
type otaClock interface {
	Now() time.Time
	// AfterFunc runs f after d; stop prevents a run that has not begun.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// otaPortClock is a [clock.Clock] as the requestor's otaClock.
type otaPortClock struct{ c clock.Clock }

func (p otaPortClock) Now() time.Time { return p.c.Now() }
func (p otaPortClock) AfterFunc(d time.Duration, f func()) func() bool {
	return p.c.AfterFunc(d, f).Stop
}

// activeProvider is an ActiveProviderLocation (:65-69).
type activeProvider struct {
	Location otadef.ProviderLocation
	// PreviousQueryTimestamp is milliseconds since the epoch, nil before
	// the provider was queried.
	PreviousQueryTimestamp *int64
	MetadataForNode        []byte
}

// updateInProgress is UpdateInProgressDetails (:71-75).
type updateInProgress struct {
	NewSoftwareVersion uint32
	Location           *otadef.ProviderLocation
	UpdateToken        []byte
}

// pendingApply is an update #prepareApplyUpdate recorded, applied once the
// query that prepared it ends (:1258-1259, transaction.onFinalize).
type pendingApply struct {
	version uint32
	file    string
}

// The matter.js constants of the requestor.
const (
	// otaBdxMaxBlockSizeTCP: OTA_BDX_MAX_BLOCK_SIZE_TCP (:54), asked for
	// over a TCP channel (:962-963).
	otaBdxMaxBlockSizeTCP = 8192
	// otaBdxMaxBlockSizeNonTCP: OTA_BDX_MAX_BLOCK_SIZE_NON_TCP (:55).
	otaBdxMaxBlockSizeNonTCP = 1024
	// otaMaxBusyRetries: MAX_BUSY_RETRIES (:61).
	otaMaxBusyRetries = 3
	// otaBdxDownloadAttempts: "We try up to 3 times" (:965, :993).
	otaBdxDownloadAttempts = 3
	// otaMaxDelay caps every delay a provider requests (:809, :830, :913).
	otaMaxDelay = 24 * time.Hour
	// otaCommissionedQueryDelay: #scheduleInitialQuery (:251-253).
	otaCommissionedQueryDelay = 30 * time.Second
)

// scheduleReason is ScheduleReason (:89-95).
type scheduleReason uint8

const (
	reasonNewQuery scheduleReason = iota
	reasonNoUpdateAvailable
	reasonBusy
	reasonAnnounced
	reasonCommissioned
)

func (r scheduleReason) String() string {
	return [...]string{"NewQuery", "NoUpdateAvailable", "Busy", "Announced", "Commissioned"}[r]
}

// Cluster identity and attribute ids, the generated definition's.
const (
	otaRequestorClusterID       = otadef.ClusterID
	otaRequestorClusterRevision = otadef.Revision

	otaRequestorAttrDefaultOTAProviders = otadef.AttrDefaultOtaProviders
	otaRequestorAttrUpdatePossible      = otadef.AttrUpdatePossible
	otaRequestorAttrUpdateState         = otadef.AttrUpdateState
	otaRequestorAttrUpdateStateProgress = otadef.AttrUpdateStateProgress
)

// otaRequestorInst is the definition bound with AnnounceOtaProvider, which
// matter.js's server implements (:376).
var otaRequestorInst = mustInstance(otadef.Definition, spec.Options{Commands: []uint32{otadef.CmdAnnounceOtaProvider}})

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterDataVersion             = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterAttributeLister         = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterCommandLister           = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterEventLister             = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.FabricScopedReader             = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.AttributeChangeNotifier        = (*OTASoftwareUpdateRequestor)(nil)
	_ contract.EventReceiver                  = (*OTASoftwareUpdateRequestor)(nil)
)

// OtaRequestorDeviceType is the OtaRequestor device type the requestor's
// endpoint lists (matter.js initialize, :181:
// `addDeviceTypes("OtaRequestor")`), with the revision of the schema
// snapshot.
func OtaRequestorDeviceType() DeviceTypeStruct {
	const id = 0x0012 // OtaRequestor (ota-requestor.element.ts)
	rev, _ := schema.DeviceTypeRevision(id)
	return DeviceTypeStruct{DeviceType: id, Revision: rev}
}

// NewOTASoftwareUpdateRequestor builds the requestor. It does nothing on
// the network until [OTASoftwareUpdateRequestor.Start].
func NewOTASoftwareUpdateRequestor(cfg OTARequestorConfig) (*OTASoftwareUpdateRequestor, error) {
	if cfg.NodeInformation == nil {
		return nil, errors.New("core: OTA requestor needs NodeInformation")
	}
	if cfg.DownloadDir == "" {
		return nil, errors.New("core: OTA requestor needs a DownloadDir")
	}
	if cfg.UpdateQueryInterval <= 0 {
		cfg.UpdateQueryInterval = 24 * time.Hour
	}
	if cfg.MinimumQueryInterval <= 0 {
		cfg.MinimumQueryInterval = 120 * time.Second
	}
	if cfg.MinimumApplyDelay <= 0 {
		cfg.MinimumApplyDelay = 2 * time.Minute
	}
	r := &OTASoftwareUpdateRequestor{
		Instance:       otaRequestorInst,
		cfg:            cfg,
		rnd:            cfg.Rand,
		logger:         cfg.Logger,
		updatePossible: true,
		// initialize (:175-178): an Unknown state becomes Idle.
		state: otadef.UpdateStateIdle,
	}
	if cfg.UpdatePossible != nil {
		r.updatePossible = *cfg.UpdatePossible
	}
	r.clock = otaPortClock{clock.Or(cfg.TimeSource)}
	if r.rnd == nil {
		r.rnd = rand.IntN
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	r.runCtx, r.cancelRun = context.WithCancel(context.Background())
	return r, nil
}

// downloadPath is the download location (:201-217): "ota-<vendor>-<product>"
// in the download directory.
func (r *OTASoftwareUpdateRequestor) downloadPath() string {
	info := r.cfg.NodeInformation()
	return filepath.Join(r.cfg.DownloadDir, fmt.Sprintf("ota-%d-%d", info.VendorID, info.ProductID))
}

// SetEndpoint is told the endpoint the server lives on; its events carry it.
func (r *OTASoftwareUpdateRequestor) SetEndpoint(endpoint uint16) {
	r.mu.Lock()
	r.endpoint = endpoint
	r.mu.Unlock()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (r *OTASoftwareUpdateRequestor) SetMatterEventEmitter(emitter contract.EventEmitter) {
	r.mu.Lock()
	r.emitter = emitter
	r.mu.Unlock()
}

func (r *OTASoftwareUpdateRequestor) emit(eventID uint32, data any) {
	r.mu.Lock()
	emitter, ep := r.emitter, r.endpoint
	r.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(ep, otadef.ClusterID, eventID, data, r.EventPriority(eventID))
	}
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (r *OTASoftwareUpdateRequestor) MatterDataVersion() uint32 { return r.dv.Current() }

// changed bumps the data version and notifies the attributes.
func (r *OTASoftwareUpdateRequestor) changed(attrs ...uint32) {
	r.dv.Bump()
	r.Notify(attrs...)
}

// MatterRead answers a read without a request: every fabric's providers.
func (r *OTASoftwareUpdateRequestor) MatterRead(attrID uint32) (any, bool) {
	return r.read(attrID, false, 0)
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a
// fabric-filtered read of DefaultOTAProviders sees the reading fabric's
// entries, an unfiltered one every fabric's (ProviderLocation has no
// fabric-sensitive field).
func (r *OTASoftwareUpdateRequestor) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if !request {
		return r.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	return r.read(attrID, filtered, fabric)
}

func (r *OTASoftwareUpdateRequestor) read(attrID uint32, filtered bool, fabric uint8) (any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch attrID {
	case otadef.AttrDefaultOtaProviders:
		out := spec.List[otadef.ProviderLocation]{}
		for _, p := range r.defaults {
			if !filtered || p.FabricIndex == fabric {
				out = append(out, p)
			}
		}
		return out, true
	case otadef.AttrUpdatePossible:
		return r.updatePossible, true
	case otadef.AttrUpdateState:
		return uint8(r.state), true
	case otadef.AttrUpdateStateProgress:
		if r.progress == nil {
			return nil, true
		}
		return *r.progress, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return r.ReadGlobal(attrID)
	}
	return nil, false
}

// otaStatusError is a write or command refused with a status.
type otaStatusError struct {
	code im.StatusCode
	msg  string
}

func (e otaStatusError) Error() string                   { return "core: OtaSoftwareUpdateRequestor: " + e.msg }
func (e otaStatusError) MatterStatusCode() im.StatusCode { return e.code }

// MatterWrite implements [contract.ClusterServer]. Only DefaultOTAProviders
// is writable: a fabric-scoped list, so the written entries replace the
// accessing fabric's, stamped with its FabricIndex.
func (r *OTASoftwareUpdateRequestor) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != otadef.AttrDefaultOtaProviders {
		_, err := r.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("core: OtaSoftwareUpdateRequestor attribute 0x%04X is not writable", attrID), status: err}
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return otaStatusError{im.StatusUnsupportedAccess, "DefaultOTAProviders written without an accessing fabric"}
	}
	written, ok := value.(spec.List[otadef.ProviderLocation])
	if !ok {
		return otaStatusError{im.StatusConstraintError, fmt.Sprintf("DefaultOTAProviders value %T", value)}
	}
	entries := make(spec.List[otadef.ProviderLocation], len(written))
	for i, e := range written {
		e.FabricIndex = fabric
		entries[i] = e
	}
	if _, err := r.ValidateWrite(attrID, entries, nil); err != nil {
		return err
	}

	r.mu.Lock()
	next := make([]otadef.ProviderLocation, 0, len(r.defaults)+len(entries))
	for _, p := range r.defaults {
		if p.FabricIndex != fabric {
			next = append(next, p)
		}
	}
	next = append(next, entries...)
	r.mu.Unlock()

	// #assertDefaultOtaProviders (:339-349): one entry per FabricIndex.
	seen := map[uint8]bool{}
	for _, p := range next {
		if seen[p.FabricIndex] {
			return otaStatusError{im.StatusConstraintError, fmt.Sprintf("Only one defaultOtaProviders entry per FabricIndex %d is allowed.", p.FabricIndex)}
		}
		seen[p.FabricIndex] = true
	}

	if r.cfg.Store != nil {
		rows := make([]store.OTAProviderLocation, len(entries))
		for i, e := range entries {
			rows[i] = store.OTAProviderLocation{FabricIndex: fabric, ProviderNodeID: e.ProviderNodeId, Endpoint: e.Endpoint}
		}
		if err := r.cfg.Store.ReplaceDefaultOTAProviders(context.WithoutCancel(ctx), fabric, rows); err != nil {
			return otaStatusError{im.StatusFailure, err.Error()}
		}
	}
	r.mu.Lock()
	before := r.defaults
	r.defaults = next
	r.mu.Unlock()
	// A write that changes nothing reports nothing and schedules nothing:
	// matter.js reacts to defaultOtaProviders$Changed
	// (OtaSoftwareUpdateRequestorServer.ts:188), which a commit emits only
	// for a changed value (Datasource.ts:1440-1441).
	if slices.Equal(before, next) {
		return nil
	}
	r.dv.Bump()
	r.NotifyFrom(contract.OriginControllerWrite, otadef.AttrDefaultOtaProviders)
	r.handleUpdatedDefaultOtaProviders(ctx, fabric, entries)
	return nil
}

// handleUpdatedDefaultOtaProviders mirrors #handleUpdatedDefaultOtaProviders
// (:352-368): the writing fabric's new provider becomes active, and a
// requestor without a query timer schedules one 1-11 minutes out.
func (r *OTASoftwareUpdateRequestor) handleUpdatedDefaultOtaProviders(ctx context.Context, fabric uint8, entries []otadef.ProviderLocation) {
	for _, p := range entries {
		if p.FabricIndex == fabric {
			r.logger.Info("matter.ota.default_provider_changed", slog.Int("fabric_index", int(fabric)), slog.Uint64("provider", p.ProviderNodeId))
			r.addActiveOtaProvider(ctx, p, nil)
			break
		}
	}
	r.mu.Lock()
	noTimer := r.timer == nil
	hasActive := len(r.active) > 0
	r.mu.Unlock()
	if noTimer && hasActive {
		d := time.Duration(r.rnd(599)+60) * time.Second // :366, random delay 1..11 min
		r.scheduleUpdateQuery(&d, reasonAnnounced, nil)
	}
}

// MatterInvoke implements [contract.ClusterServer]: AnnounceOTAProvider.
func (r *OTASoftwareUpdateRequestor) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID != otadef.CmdAnnounceOtaProvider || !r.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("core: OtaSoftwareUpdateRequestor command 0x%02X not supported", cmdID)
	}
	req, ok := fields.(otadef.AnnounceOtaProviderRequest)
	if !ok {
		if p, isPtr := fields.(*otadef.AnnounceOtaProviderRequest); isPtr && p != nil {
			req, ok = *p, true
		}
	}
	if !ok {
		return nil, otaStatusError{im.StatusInvalidCommand, fmt.Sprintf("AnnounceOTAProvider fields %T", fields)}
	}
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return nil, r.announceOtaProvider(ctx, fabric, req)
	}
	// An announcement that arrives while a query runs is handled once the
	// query ended, so the query it schedules neither overlaps the running
	// one nor is skipped by it (performUpdateQuery). matter.js handles it
	// after the query because the announcement's state change
	// (#addActiveOtaProvider) waits for the transaction the running query
	// holds (OtaSoftwareUpdateRequestorServer.ts:376-420, :568-582 at
	// 862ad75f; OtaTest.ts "handles an announcement only after a running
	// query, so no second query overlaps it"); the Go requestor answers the
	// command at once and defers the handling, rather than holding the
	// answer for as long as a query runs.
	r.mu.Lock()
	if r.queryRunning {
		r.announcedDuringQuery = append(r.announcedDuringQuery, otaAnnouncement{ctx: context.WithoutCancel(ctx), fabric: fabric, req: req})
		r.mu.Unlock()
		r.logger.Info("matter.ota.announcement_after_query", slog.Uint64("provider", req.ProviderNodeId))
		return nil, nil
	}
	r.mu.Unlock()
	return nil, r.announceOtaProvider(ctx, fabric, req)
}

// otaAnnouncement is an AnnounceOTAProvider handled after the running
// query.
type otaAnnouncement struct {
	ctx    context.Context
	fabric uint8
	req    otadef.AnnounceOtaProviderRequest
}

// announceOtaProvider mirrors announceOtaProvider (:376-420).
func (r *OTASoftwareUpdateRequestor) announceOtaProvider(ctx context.Context, fabric uint8, req otadef.AnnounceOtaProviderRequest) error {
	// :383-389: an accessing fabric is required.
	if fabric == 0 {
		return otaStatusError{im.StatusUnsupportedAccess, "announceOtaProvider not allowed without an accessing fabric."}
	}
	provider := otadef.ProviderLocation{FabricIndex: fabric, ProviderNodeId: req.ProviderNodeId, Endpoint: req.Endpoint}
	var metadata []byte
	if req.MetadataForNode != nil {
		metadata = *req.MetadataForNode
	}
	r.addActiveOtaProvider(ctx, provider, metadata)
	r.logger.Info("matter.ota.provider_announced",
		slog.Int("reason", int(req.AnnouncementReason)), slog.Int("fabric_index", int(fabric)),
		slog.Uint64("provider", req.ProviderNodeId), slog.Int("endpoint", int(req.Endpoint)), slog.Int("vendor_id", int(req.VendorId)))
	// :408-416: Urgent or UpdateAvailable query soon, a simple announcement
	// only makes sure a query is scheduled.
	if req.AnnouncementReason != otadef.AnnouncementReasonSimpleAnnouncement {
		var d time.Duration
		if r.cfg.AnnouncedUpdateQueryDelay != nil {
			d = *r.cfg.AnnouncedUpdateQueryDelay
		} else {
			d = time.Duration(r.rnd(600)+1) * time.Second
		}
		r.scheduleUpdateQuery(&d, reasonAnnounced, &provider)
	} else {
		r.scheduleUpdateQuery(nil, reasonNewQuery, nil)
	}
	return nil
}

// addActiveOtaProvider mirrors #addActiveOtaProvider (:423-442): the entry
// for (fabric, node) is added or replaced, with no query timestamp so it is
// tried first.
func (r *OTASoftwareUpdateRequestor) addActiveOtaProvider(ctx context.Context, p otadef.ProviderLocation, metadata []byte) {
	r.mu.Lock()
	entry := activeProvider{Location: p, MetadataForNode: metadata}
	i := r.activeIndexLocked(p)
	if i < 0 {
		r.active = append(r.active, entry)
	} else {
		r.active[i] = entry
	}
	r.mu.Unlock()
	r.persist(ctx)
}

func (r *OTASoftwareUpdateRequestor) activeIndexLocked(p otadef.ProviderLocation) int {
	for i, a := range r.active {
		if a.Location.FabricIndex == p.FabricIndex && a.Location.ProviderNodeId == p.ProviderNodeId {
			return i
		}
	}
	return -1
}

// removeOtaProvider mirrors #removeOtaProvider (:445-460): out of the active
// and the default list.
func (r *OTASoftwareUpdateRequestor) removeOtaProvider(ctx context.Context, p otadef.ProviderLocation) {
	r.mu.Lock()
	if i := r.activeIndexLocked(p); i >= 0 {
		r.active = append(r.active[:i], r.active[i+1:]...)
	}
	removedDefault := false
	var remaining []otadef.ProviderLocation
	for i, d := range r.defaults {
		if d.FabricIndex == p.FabricIndex && d.ProviderNodeId == p.ProviderNodeId {
			r.defaults = append(r.defaults[:i:i], r.defaults[i+1:]...)
			removedDefault = true
			break
		}
	}
	if removedDefault {
		for _, d := range r.defaults {
			if d.FabricIndex == p.FabricIndex {
				remaining = append(remaining, d)
			}
		}
	}
	r.mu.Unlock()
	if removedDefault {
		if r.cfg.Store != nil {
			rows := make([]store.OTAProviderLocation, len(remaining))
			for i, d := range remaining {
				rows[i] = store.OTAProviderLocation{FabricIndex: d.FabricIndex, ProviderNodeID: d.ProviderNodeId, Endpoint: d.Endpoint}
			}
			if err := r.cfg.Store.ReplaceDefaultOTAProviders(context.WithoutCancel(ctx), p.FabricIndex, rows); err != nil {
				r.logger.Warn("matter.ota.persist_defaults", slog.String("err", err.Error()))
			}
		}
		r.changed(otadef.AttrDefaultOtaProviders)
	}
	r.persist(ctx)
}

// markActiveOtaProviderNoUpdate mirrors #markActiveOtaProviderNoUpdate
// (:463-476): the provider was asked now.
func (r *OTASoftwareUpdateRequestor) markActiveOtaProviderNoUpdate(ctx context.Context, p otadef.ProviderLocation) {
	now := r.clock.Now().UnixMilli()
	r.mu.Lock()
	if i := r.activeIndexLocked(p); i >= 0 {
		r.active[i].PreviousQueryTimestamp = &now
	} else {
		r.active = append(r.active, activeProvider{Location: p, PreviousQueryTimestamp: &now})
	}
	r.mu.Unlock()
	r.persist(ctx)
}

// FabricRemoved drops the removed fabric's DefaultOTAProviders entries, as
// the removal of a fabric drops every fabric-scoped entry it owned. The
// store's rows go with the fabric's own row.
func (r *OTASoftwareUpdateRequestor) FabricRemoved(fabricIndex uint8) {
	r.mu.Lock()
	kept := r.defaults[:0:0]
	for _, d := range r.defaults {
		if d.FabricIndex != fabricIndex {
			kept = append(kept, d)
		}
	}
	changed := len(kept) != len(r.defaults)
	r.defaults = kept
	r.mu.Unlock()
	if changed {
		r.changed(otadef.AttrDefaultOtaProviders)
	}
}

// SetUpdatePossible changes UpdatePossible, which starts or stops the
// queries (#enableOrDisableUpdates, :316-336).
func (r *OTASoftwareUpdateRequestor) SetUpdatePossible(v bool) {
	r.mu.Lock()
	if r.updatePossible == v {
		r.mu.Unlock()
		return
	}
	r.updatePossible = v
	r.mu.Unlock()
	r.changed(otadef.AttrUpdatePossible)
	r.enableOrDisableUpdates(v)
}

func (r *OTASoftwareUpdateRequestor) enableOrDisableUpdates(v bool) {
	if v {
		r.mu.Lock()
		running := r.timer != nil && r.timer.running
		idle := r.state == otadef.UpdateStateIdle
		r.mu.Unlock()
		if !running || idle {
			d := time.Duration(r.rnd(599)+60) * time.Second // :322, random delay 1..11 min
			r.logger.Info("matter.ota.updates_enabled", slog.Duration("next_query", d))
			r.scheduleUpdateQuery(&d, reasonNewQuery, nil)
		}
		return
	}
	r.logger.Info("matter.ota.updates_disabled")
	r.mu.Lock()
	r.stopTimerLocked()
	r.timer = nil
	delayed := r.state == otadef.UpdateStateDelayedOnQuery
	r.mu.Unlock()
	// :331-334: nothing is scheduled any more, so a Busy provider's retry
	// is no longer awaited.
	if delayed {
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
	}
}

// Start brings the requestor online: it loads what was persisted and runs
// matter.js's #online (:220-245) — the default providers join the active
// ones when there are none, a query is scheduled 2-12 minutes out (or at
// the regular interval right after an update), and an update that was in
// progress is reported as applied or failed.
func (r *OTASoftwareUpdateRequestor) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("core: OTA requestor already started")
	}
	r.started = true
	r.mu.Unlock()
	if err := r.load(ctx); err != nil {
		return err
	}
	r.online(ctx)
	return nil
}

// Restarted runs #online again without a process restart: what a host
// calls when it now runs the version it just applied in place — the
// reference daemon's harness mode does — so the requestor reports the
// update as it would after a reboot.
func (r *OTASoftwareUpdateRequestor) Restarted(ctx context.Context) { r.online(ctx) }

func (r *OTASoftwareUpdateRequestor) online(ctx context.Context) {
	r.mu.Lock()
	addDefaults := len(r.active) == 0 && len(r.defaults) > 0
	defaults := append([]otadef.ProviderLocation(nil), r.defaults...)
	r.mu.Unlock()
	if addDefaults {
		// :223-228: retry removed default providers at least once after a
		// restart.
		for _, p := range defaults {
			r.addActiveOtaProvider(ctx, p, nil)
		}
	}
	r.mu.Lock()
	hasActive := len(r.active) > 0
	details := r.inProgress
	r.mu.Unlock()
	if hasActive {
		// :232-238: 2-12 minutes, never sooner than the 120 s the
		// specification allows between queries; right after an update the
		// regular interval.
		if details == nil {
			d := time.Duration(r.rnd(599)+120) * time.Second
			r.scheduleUpdateQuery(&d, reasonNewQuery, nil)
		} else {
			r.scheduleUpdateQuery(nil, reasonNewQuery, nil)
		}
	}
	if details != nil {
		r.goRun(func(ctx context.Context) {
			r.handlePreviousUpdateOnStart(ctx, *details)
			r.mu.Lock()
			r.inProgress = nil
			r.mu.Unlock()
			r.persist(ctx)
		})
	}
}

// Commissioned schedules the query 30 s after a successful commissioning
// (#scheduleInitialQuery, :251-253).
func (r *OTASoftwareUpdateRequestor) Commissioned() {
	d := otaCommissionedQueryDelay
	r.scheduleUpdateQuery(&d, reasonCommissioned, nil)
}

// Stop takes the requestor offline: the query timer stops (#goingOffline,
// :247-249) and a running query's delays are cancelled (asyncDispose,
// :1306-1309).
func (r *OTASoftwareUpdateRequestor) Stop() {
	r.mu.Lock()
	r.stopped = true
	r.stopTimerLocked()
	r.mu.Unlock()
	r.cancelRun()
	r.wg.Wait()
}

func (r *OTASoftwareUpdateRequestor) goRun(fn func(ctx context.Context)) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		fn(r.runCtx)
	}()
}

// persistedState is the requestor state kept beside the attributes.
type persistedState struct {
	ActiveOtaProviders      []persistedActive    `json:"activeOtaProviders"`
	UpdateInProgressDetails *persistedInProgress `json:"updateInProgressDetails"`
}

type persistedLocation struct {
	FabricIndex    uint8  `json:"fabricIndex"`
	ProviderNodeID uint64 `json:"providerNodeId"`
	Endpoint       uint16 `json:"endpoint"`
}

type persistedActive struct {
	Location               persistedLocation `json:"location"`
	PreviousQueryTimestamp *int64            `json:"previousQueryTimestamp"`
	MetadataForNode        []byte            `json:"metadataForNode,omitempty"`
}

type persistedInProgress struct {
	NewSoftwareVersion uint32             `json:"newSoftwareVersion"`
	Location           *persistedLocation `json:"location,omitempty"`
	UpdateToken        []byte             `json:"updateToken,omitempty"`
}

func toPersisted(l otadef.ProviderLocation) persistedLocation {
	return persistedLocation{FabricIndex: l.FabricIndex, ProviderNodeID: l.ProviderNodeId, Endpoint: l.Endpoint}
}

func fromPersisted(l persistedLocation) otadef.ProviderLocation {
	return otadef.ProviderLocation{FabricIndex: l.FabricIndex, ProviderNodeId: l.ProviderNodeID, Endpoint: l.Endpoint}
}

func (r *OTASoftwareUpdateRequestor) load(ctx context.Context) error {
	if r.cfg.Store == nil {
		return nil
	}
	rows, err := r.cfg.Store.ListDefaultOTAProviders(ctx)
	if err != nil {
		return fmt.Errorf("core: OTA requestor: %w", err)
	}
	defaults := make([]otadef.ProviderLocation, len(rows))
	for i, row := range rows {
		defaults[i] = otadef.ProviderLocation{FabricIndex: row.FabricIndex, ProviderNodeId: row.ProviderNodeID, Endpoint: row.Endpoint}
	}
	raw, ok, err := r.cfg.Store.LoadOTARequestorState(ctx)
	if err != nil {
		return fmt.Errorf("core: OTA requestor: %w", err)
	}
	var st persistedState
	if ok {
		if err := json.Unmarshal(raw, &st); err != nil {
			r.logger.Warn("matter.ota.state_unreadable", slog.String("err", err.Error()))
			st = persistedState{}
		}
	}
	r.mu.Lock()
	r.defaults = defaults
	r.active = nil
	for _, a := range st.ActiveOtaProviders {
		r.active = append(r.active, activeProvider{Location: fromPersisted(a.Location), PreviousQueryTimestamp: a.PreviousQueryTimestamp, MetadataForNode: a.MetadataForNode})
	}
	r.inProgress = nil
	if d := st.UpdateInProgressDetails; d != nil {
		p := &updateInProgress{NewSoftwareVersion: d.NewSoftwareVersion, UpdateToken: d.UpdateToken}
		if d.Location != nil {
			l := fromPersisted(*d.Location)
			p.Location = &l
		}
		r.inProgress = p
	}
	r.mu.Unlock()
	return nil
}

// persist stores the active providers and the update in progress.
func (r *OTASoftwareUpdateRequestor) persist(ctx context.Context) {
	if r.cfg.Store == nil {
		return
	}
	r.mu.Lock()
	st := persistedState{}
	for _, a := range r.active {
		st.ActiveOtaProviders = append(st.ActiveOtaProviders, persistedActive{Location: toPersisted(a.Location), PreviousQueryTimestamp: a.PreviousQueryTimestamp, MetadataForNode: a.MetadataForNode})
	}
	if d := r.inProgress; d != nil {
		p := &persistedInProgress{NewSoftwareVersion: d.NewSoftwareVersion, UpdateToken: d.UpdateToken}
		if d.Location != nil {
			l := toPersisted(*d.Location)
			p.Location = &l
		}
		st.UpdateInProgressDetails = p
	}
	r.mu.Unlock()
	b, err := json.Marshal(st)
	if err == nil {
		err = r.cfg.Store.SaveOTARequestorState(context.WithoutCancel(ctx), b)
	}
	if err != nil {
		r.logger.Warn("matter.ota.persist_state", slog.String("err", err.Error()))
	}
}

// DecodeDefaultOTAProvidersWrite decodes a written DefaultOTAProviders
// value — the list, or for a list-append write (item) one entry, as a
// one-element list. FabricIndex may be absent: a client leaves the field of
// a fabric-scoped struct out of a write, and the server sets it.
// el is the value's opener, already consumed from dec. The bridge's write
// decoder calls it.
func DecodeDefaultOTAProvidersWrite(dec *tlv.Decoder, el tlv.Element, item bool) (any, error) {
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		return nil, err
	}
	entries := []spec.Node{n}
	if !item {
		if el.Type != tlv.TypeArray {
			return badOtaValue{}, nil
		}
		entries = n.Children
	}
	out := spec.List[otadef.ProviderLocation]{}
	for _, e := range entries {
		if e.El.Type != tlv.TypeStructure {
			return badOtaValue{}, nil
		}
		var p otadef.ProviderLocation
		seen := map[uint32]bool{}
		for _, f := range e.Children {
			if f.El.Tag.Kind != tlv.TagKindContext {
				continue
			}
			isUint := f.El.Type >= tlv.TypeUnsignedInt1 && f.El.Type <= tlv.TypeUnsignedInt8
			switch f.El.Tag.Number {
			case 1:
				if !isUint {
					return badOtaValue{}, nil
				}
				p.ProviderNodeId = f.El.Uint
			case 2:
				if !isUint || f.El.Uint > 0xFFFF {
					return badOtaValue{}, nil
				}
				p.Endpoint = uint16(f.El.Uint)
			case 254:
				if !isUint || f.El.Uint > 0xFF {
					return badOtaValue{}, nil
				}
				p.FabricIndex = uint8(f.El.Uint)
			}
			seen[f.El.Tag.Number] = true
		}
		if !seen[1] || !seen[2] {
			return badOtaValue{}, nil
		}
		out = append(out, p)
	}
	return out, nil
}

// badOtaValue is a written value that is no ProviderLocation list; the
// write answers CONSTRAINT_ERROR.
type badOtaValue struct{}
