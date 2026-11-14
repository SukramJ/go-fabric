// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/binding"
	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/internal/serialqueue"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// ErrStopped is returned by [Node.Start] after [Node.Stop]: a node is not
// restarted; a host builds a new one on the same store. [Node.OpenOwnWindow],
// [Node.OpenBasicWindow] and [Node.CloseWindow] return it from the first
// Stop on.
var ErrStopped = errors.New("node: stopped")

// ErrNotStarted is returned by a call that needs the running node's
// assembled topology before [Node.Start] assembled it
// ([Aggregator.IncreaseConfigurationVersions]).
var ErrNotStarted = errors.New("node: not started")

// operationalTimeInterval is how often the operational time is persisted
// while the node runs (the daemon's time.Minute, main.go:430 at b4ea1f96^).
const operationalTimeInterval = time.Minute

// stopTimeout bounds the Stop [Node.Run] performs once its context ends
// (the daemon's 5 s, main.go:432 at b4ea1f96^).
const stopTimeout = 5 * time.Second

// Node is a Matter node: the bridge, its root and Aggregator endpoints and
// every port [bridge] documents, wired in the documented order.
type Node struct {
	cfg    Config
	logger *slog.Logger
	st     store.Port
	// ownedDB is the handle opened for Config.Store.Path, closed (and
	// cleared) by Stop; nil for a host's own. keepOwnedDB keeps it open
	// across the stop a FactoryReset makes, which erases it afterwards.
	ownedDB     *sql.DB
	keepOwnedDB bool
	// builtCredentials and builtEndpoints are set when New built the
	// credential or the endpoint store on Store.DB or Store.Path itself,
	// not taken from a host override: a FactoryReset after Stop rebuilds
	// exactly those on the reopened file and erases the overrides as they
	// are.
	builtCredentials, builtEndpoints bool

	groups    *groups.Manager
	assembler *endpoint.Assembler
	br        *bridge.Bridge
	root      *RootClusters
	agg       *Aggregator
	// aggServers are the Aggregator endpoint's servers ([Node.Surface]);
	// surfaceDigest is Config.Surface's digest, computed once.
	aggServers    []contract.ClusterServer
	surfaceDigest [32]byte
	// rootDecisions are the decisions on the root's candidates
	// (composeRoot, levelRootClusters); rootCandidates the candidates.
	rootDecisions  []surface.Decision
	rootCandidates []surface.Element
	// withheld are the endpoints the level withheld at Add or Replace,
	// by rendered key ([Node.Surface]).
	withheldMu sync.Mutex
	withheld   map[string]surface.Report
	// announceMu serialises the surface announcement (announceSurface);
	// changed are the changes it announced, the last maxChanged, under
	// changedMu ([Node.Surface]).
	announceMu sync.Mutex
	changedMu  sync.Mutex
	changed    []surface.SurfaceChange
	caseIDs    *caseIdentities
	sec        *security
	window     *commissioningHost
	// observers are the hooks Observe registered after
	// Config.Observers (observers.go).
	observers observerSet
	diagRing  *diagevent.Ring
	// callbacks is the node's serial goroutine of the `…Async` callbacks
	// it owns (OnDiagnosticEventAsync), running from Start to Stop.
	callbacks  *serialqueue.Queue
	advertiser mdns.Advertiser
	// ownsAdvertiser is set when New built the advertiser (a nil
	// Config.Advertiser), so a failed New closes it.
	ownsAdvertiser bool
	// sessionEvents turns session and subscription changes into the
	// Observers' session hooks (sessions_events.go).
	sessionEvents sessionEvents
	// healthListeners and otaStatusListeners are the host's On…
	// callbacks of the health probe and the OTA provider.
	healthListeners    listeners[HealthSample]
	otaStatusListeners listeners[otaProviderStatus]
	// attestationErr is the load error of Attestation.Files under
	// BlockCommissioningOnError (attestation.go).
	attestationErr error
	// health is the probe Start began (Config.Health); nil when none runs
	// (health.go).
	healthMu   sync.Mutex
	health     *healthProbe
	bootReason uint8
	persisted  persisted
	// endpoints is the endpoint-level persistence the facade's assembler
	// and Aggregator use: Config.EndpointPersistence, else persisted.
	endpoints EndpointPersistence
	// groupTx is the socket group messages leave through (groupsend.go).
	groupTx  *udp.Listener
	client   *clientStack
	bindings *binding.Manager
	bindSync bindingSync
	// failSafe holds the topology's fail-safe participants (failsafe.go).
	failSafe failSafeSync
	// matterTime holds the topology's Matter time participants
	// (matter_time.go).
	matterTime matterTimeSync
	// userLabels keeps the bridged endpoints' UserLabel lists in the
	// store (user_labels.go).
	userLabels userLabelSync
	// nonVolatile stores and restores the nonvolatile attributes the
	// module holds (nonvolatile.go).
	nonVolatile *nonVolatile
	// bdx serves the BDX transfers peers open (the provider's images) and
	// otaProvider is the OTA Software Update Provider; both nil without
	// Config.OTAProvider (ota_provider.go).
	bdx         *bdx.Responder
	otaProvider *mattercore.OTAProvider
	// commissionerControl is the Aggregator's CommissionerControl server,
	// nil without Config.CommissionerControl (commissioner_control.go).
	commissionerControl *mattercore.CommissionerControl

	// life bounds the goroutines New and Start begin (subscription
	// engine, reapers, the operational-time keeper); Stop cancels it.
	life context.Context //nolint:containedctx // the node's lifetime, cancelled by Stop
	// reopenPending is set while a reopen after the last fabric waits for
	// the node to become idle (reopenWhenIdle).
	reopenPending atomic.Bool
	// commissioned is matter.js's CommissioningServer state.commissioned
	// (CommissioningServer.ts:148-165 at db021da0): true from a start
	// with stored fabrics and from a committed CommissioningComplete, false
	// once the last fabric of a commissioned node is removed. A fabric
	// that only a fail-safe added leaves it false, so the fail-safe's
	// rollback of that fabric decommissions nothing.
	commissioned atomic.Bool
	cancel       context.CancelFunc
	keeperWG     sync.WaitGroup
	// keeperMu orders the keepers a handler starts (goKeeper:
	// reopenWhenIdle's, leaveRadioWhenCommissioned's) against Stop:
	// stopLocked sets stopping under it before the teardown, and a keeper
	// is added to keeperWG — and a reopen opens the window — only under
	// it while stopping is clear. So no Add races keeperWG.Wait (a handler
	// can outlive br.Stop's bounded wait), no keeper starts once Stop
	// began, and no window opens after br.Stop stopped the window's timer.
	keeperMu sync.Mutex
	stopping bool
	// windowLife bounds the commissioning window's transitions — the
	// announce and the BLE advertisement of the transition hook — and a
	// reopen's (wireCommissioningWindow, reopenUnlessStopping). Stop
	// cancels it first, before it takes keeperMu, which a reopen holds
	// across the window's open: a reopen waiting on its context ends, and
	// Stop does not wait on the lock for a lifetime only it ends (review
	// II, R2-P1-dr-req-04). No transition is announced once Stop began.
	windowLife   context.Context //nolint:containedctx // a child of life that Stop cancels first
	cancelWindow context.CancelFunc

	// lifecycle serialises Start, Stop and FactoryReset: a slot of
	// capacity one, held across them, host callbacks included (the host's
	// Snapshotter), and taken by nothing else. Stop's wait for it ends
	// with Stop's ctx (enterLifecycle; review II, R2-P2-stop-08). started
	// is atomic so a host's call during either (SetCommissioningBlocked)
	// never waits for it (ADR 0026 P6).
	lifecycle chan struct{}
	started   atomic.Bool
	stopped   bool
	// stopBegun: a Stop took the node's own parts down; stopped is set
	// only once the bridge stopped too, so a Stop whose bridge stop gave
	// up is resumed by the next one (review II, R2-P1-dr-req-01).
	stopBegun bool
	// rolledBackReported: Start ran the removal consequences of the
	// fabrics construction rolled back (reportRolledBackFabrics); a Start
	// after a failed one does not run them again. Under lifecycle.
	rolledBackReported bool

	// announcedMu guards announced: the fabrics the host may know of —
	// stored when the node started, or installed in this process
	// (OnFabricAdded's hook ran). The row of an AddNOC that failed is in
	// neither, so its rollback does not reach the host's OnFabricRemoved.
	announcedMu sync.Mutex
	announced   map[uint8]bool
}

// New builds the node: everything the reference daemon builds before
// bridge.Start (examples/reference-bridge/main.go:135-397 at b4ea1f96^), with every
// port of bridge/bridge.go's package doc attached in its documented order.
//
// Nothing is left as a noop except what the configuration omits on
// purpose: a nil Advertiser is the node's own (Zeroconf with the subtype
// responder, or Noop under Network.MDNS.Off), a nil OTA
// mounts no OTA requestor. The TestEventTrigger enable key is not armed
// (Root().GeneralDiagnostics.EnableTestEventTriggers is the host's), and
// SetOnReassembled stays the bridge's nil-safe hook for the host (a
// fabric's installation is Config.Observers.OnFabricAdded).
//
// New opens and migrates the store, loads the group state and reads the
// store under ctx, so a host bounds or cancels the construction; it
// starts no goroutine of its own. The subscription engine, the session,
// PASE and CASE reapers and the DNS-SD subtype responder start in
// [Node.Start], so a node that is built and dropped leaks nothing but
// what the host handed it (ADR 0026 principle 5). The one exception is
// the opt-in client stack ([Config].Client), whose socket and discovery
// cache open here. [Node.Stop] releases what New opened, whether or not
// the node was started. ctx bounds New only: the node's lifetime is its
// own, ended by Stop. Mirrors matter.js ServerNode.create, the awaited
// construction (packages/node/src/node/ServerNode.ts:107-127).
func New(ctx context.Context, cfg Config) (*Node, error) { //nolint:funlen // the composition root: one sequential wiring pass, as main.go
	// The root's surface is decided before anything is opened or built.
	rootDecisions, rootCandidates, err := validateAndComposeRoot(&cfg)
	if err != nil {
		return nil, err
	}
	attestationErr, err := cfg.resolveAttestation()
	if err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cfg.Clock = clock.Or(cfg.Clock)
	n := &Node{
		cfg: cfg, logger: cfg.Logger, advertiser: cfg.Advertiser, callbacks: newCallbackQueue(cfg.Clock), bootReason: mattercore.BootReasonPowerOnReboot,
		attestationErr: attestationErr, lifecycle: make(chan struct{}, 1),
	}
	n.life, n.cancel = context.WithCancel(context.WithoutCancel(ctx))
	n.windowLife, n.cancelWindow = context.WithCancel(n.life)
	n.surfaceDigest = cfg.Surface.Digest()
	n.rootDecisions, n.rootCandidates = rootDecisions, rootCandidates
	ok := false
	defer func() {
		if !ok {
			n.abandon()
		}
	}()
	// A nil advertiser is the node's own (mdns.go).
	n.resolveAdvertiser()

	// An advertiser that paces its own timers (mdns.Responder) runs them on
	// the node's clock.
	if a, ok := n.advertiser.(interface{ SetClock(clock.Clock) }); ok {
		a.SetClock(n.cfg.Clock)
	}

	// --- persistence (main.go:146-165 at b4ea1f96^, persist.go openDB) ---------------
	if err := n.openStores(ctx); err != nil {
		return nil, err
	}
	n.cfg.Store.Credentials = n.st
	n.persisted = persisted{st: n.st, logger: n.logger}
	n.nonVolatile = &nonVolatile{n: n}
	n.endpoints = cfg.EndpointPersistence
	if n.endpoints == nil {
		n.endpoints = n.persisted
	}
	// The host's group manager when it supplies one (its assembler mounts
	// the Groups servers over it), else the facade's own.
	n.groups = cfg.Groups
	if n.groups == nil {
		if n.groups, err = groups.NewManager(n.st, n.logger); err != nil {
			return nil, fmt.Errorf("node: group state: %w", err)
		}
		n.groups.SetClock(n.cfg.Clock)
	}

	// --- the topology assembler and the Aggregator (main.go:167-186 at b4ea1f96^) ----
	// A host with its own assembler supplies the bridge's Snapshotter
	// instead (Config.Snapshotter), as the daemon did before the facade.
	pd, bi := cfg.ProductDescription, cfg.BasicInformation
	snapshotter := cfg.Snapshotter
	if snapshotter == nil {
		n.assembler, err = endpoint.New(n.cfg.Store.Endpoints, endpoint.Config{
			VendorID:           pd.VendorID,
			ProductID:          pd.ProductID,
			VendorName:         bi.VendorName,
			Groups:             n.groups,
			OnNodeLabelWritten: n.endpoints.OnNodeLabelWritten,
			Scenes:             n.endpoints,
			// The node's own conditions on the root: WiFi with a Wi-Fi
			// reporter (Network.WiFi).
			RootDeviceConditions: n.cfg.rootDeviceConditions(),
			// The bridged endpoints' Identify servers count down on the
			// node's clock.
			Clock: n.cfg.Clock,
		}, n.logger)
		if err != nil {
			return nil, fmt.Errorf("node: endpoint assembler: %w", err)
		}
		n.agg = &Aggregator{n: n}
		snapshotter = n.agg.snapshot
	}
	// The binding manager reads the topology the bridge serves.
	snapshotter = n.observeTopology(snapshotter)

	// --- the bridge (main.go:188-228 at b4ea1f96^) -----------------------------------
	n.br, err = bridge.New(snapshotter, n.advertiser, bridge.Config{
		Listen:     cfg.Network.Listen,
		PreferIPv4: cfg.Network.PreferIPv4,
		TCP:        cfg.Network.TCP,
		BLE:        cfg.Network.BLE,
		Clock:      n.cfg.Clock,
		// The invoke path refuses what BasicInformation does not advertise.
		MaxPathsPerInvoke: cfg.MaxPathsPerInvoke,
		// The operational record advertises the reported intervals.
		SessionIntervals: cfg.Sessions.advertised(),
	}, n.logger)
	if err != nil {
		return nil, fmt.Errorf("node: bridge: %w", err)
	}
	n.br.SetDeviceTypeValidation(cfg.DeviceTypeValidation)
	n.br.SetOnAttributeChange(n.attributeChanged)
	n.br.SetOnEvent(n.eventEmitted)

	// Access control fails closed without a lister; the stored ACL is what
	// a commissioner writes (main.go:230-233 at b4ea1f96^).
	n.br.AttachACLLister(n.st)
	// Group messaging and the auxiliary ACL, over the restored group state
	// (main.go:234-246 at b4ea1f96^).
	if err := n.groups.Load(ctx); err != nil {
		return nil, fmt.Errorf("node: group state: %w", err)
	}
	n.br.AttachGroupMessaging(n.groups) //nolint:contextcheck // the reconcile hook runs per group change, for the node's lifetime, not New's call
	n.br.AttachAuxiliaryACL(n.groups)
	// A bounded trace of the moments that explain a failed pairing
	// (main.go:247-251 at b4ea1f96^).
	n.diagRing = diagevent.NewRing(n.cfg.Diagnostics.Effective())
	n.diagRing.SetClock(n.cfg.Clock)
	n.br.AttachDiagnosticEvents(n.diagRing)
	// The named counters and gauges (metrics.go), before the bridge
	// opens a socket so its listeners' panic hook counts.
	n.br.AttachMetrics(n.cfg.Metrics)

	// --- root + Aggregator endpoints (main.go:253-321 at b4ea1f96^) ------------------
	// UniqueID is pinned before the root is built: the host's value, or
	// the one the store keeps (persist.go uniqueID).
	if n.cfg.BasicInformation.UniqueID == "" {
		n.cfg.BasicInformation.UniqueID, err = n.persisted.uniqueID(ctx, func() string {
			return mattercore.DeriveUniqueID(pd.VendorID, pd.ProductID, bi.NodeLabel, bi.SerialNumber)
		})
		if err != nil {
			return nil, fmt.Errorf("node: unique id: %w", err)
		}
	}
	n.caseIDs = newCaseIdentities(n.logger, n.groups)
	n.caseIDs.clock = n.cfg.Clock
	// A KeySetWrite or KeySetRemove reloads the fabric's group state; the
	// CASE identity follows key set 0 (matter.js Fabric.destinationIdsFor
	// reads the fabric's KeySets, which GroupKeyManagementServer.keySetWrite
	// replaced in place).
	n.groups.OnGroupcastChanged(n.caseIDs.refreshIPK)
	if err := n.buildLevelledRoot(ctx, rootDeps{
		cfg: &n.cfg, logger: n.logger, bootReason: n.bootReason, st: n.st, groups: n.groups, connector: n.br,
		wifiDriver:              n.wifiDriver(),
		onFabricInstalled:       n.observedFabricInstalled,
		onCommissioningComplete: n.commissioningCompleted,
		onFailSafeExpired:       n.failSafeExpired,
	}); err != nil {
		return nil, err
	}
	n.persisted.restoreRoot(ctx, n.root.BasicInformation)
	// DiagnosticLogs hands logs over BDX on the requesting session
	// (ADR 0021).
	n.root.DiagnosticLogs.AttachExchangeOpener(n.br)
	n.root.DiagnosticLogs.SetLogger(n.logger)
	n.br.AttachRootClusters(n.root.Servers())
	if n.commissionerControl, err = n.buildCommissionerControl(); err != nil {
		return nil, err
	}
	aggExtra := cfg.AggregatorServers
	if n.commissionerControl != nil {
		aggExtra = append(slices.Clone(aggExtra), n.commissionerControl)
	}
	aggregatorServers, err := buildAggregatorClusters(ctx, n.st, aggExtra, n.cfg.Clock)
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	n.br.AttachAggregatorClusters(aggregatorServers)
	n.aggServers = aggregatorServers
	// The OTA provider and its BDX responder (ADR 0021).
	if err := n.buildOTAProvider(); err != nil {
		return nil, err
	}

	// --- sessions, PASE, CASE (main.go:323-328 at b4ea1f96^) -------------------------
	cm := cfg.Commissioning
	n.sec = wireSecurity(n.br, n.st, n.root, n.caseIDs, paseParams{ //nolint:contextcheck // the handshake hooks run per session, for the node's lifetime, not New's call
		passcode: cm.Passcode, salt: cm.Salt, iterations: int(cm.PBKDFIterations), life: n.life,
	}, responderSessionParams(cfg.Network.TCP, cfg.MaxPathsPerInvoke, cfg.Sessions), cfg.Limits, n.logger, &n.sessionEvents)
	n.sessionEvents.n = n
	n.wireICD()

	// --- the commissioning window (main.go:329-360 at b4ea1f96^) ---------------------
	advert, err := n.advertisement()
	if err != nil {
		return nil, err
	}
	n.window = &commissioningHost{
		br: n.br, sessions: n.sec.sessions, root: n.root, logger: n.logger,
		baseline: n.sec.pase, advert: advert, observers: n.observerList,
		responderParams: n.sec.responderParams, life: n.life,
		ble: cfg.Network.BLE != nil, clk: clock.Or(n.cfg.Clock),
	}
	wireCommissioningWindow(n.windowLife, n.window, n.st) //nolint:contextcheck // the window lives as long as the node: windowLife, not New's ctx

	// --- fabric hooks (main.go:362-414 at b4ea1f96^) ---------------------------------
	// The host's observers run after each (observers.go).
	n.root.OperationalCredentials.SetOnFabricLeaving(n.fabricLeaving)
	n.root.OperationalCredentials.SetOnFabricRemoved(n.observedFabricRemoved)
	n.root.OperationalCredentials.SetOnFabricUpdated(n.observedFabricUpdated)
	n.root.OperationalCredentials.SetOnFabricChanged(n.observedFabricChanged)

	n.wireEventsUTC()

	// Event numbers continue across a reboot (main.go:416-426 at b4ea1f96^).
	// A ceiling the store cannot read fails New, as the reboot count does:
	// numbering from 1 would write a lower ceiling over the stored one and
	// hand out numbers a controller has seen.
	ceiling, found, err := n.st.GetMetadataCounter(ctx, store.MetadataKeyEventNumber)
	if err != nil {
		return nil, fmt.Errorf("node: event number ceiling: %w", err)
	}
	if found {
		n.br.EventLog().SeedNumber(ceiling)
	}
	// A refused ceiling stays unwritten in the log too, which writes it
	// again at the next event (im.EventLog.SetCeilingPersistence).
	n.br.EventLog().SetCeilingPersistence(func(ceiling uint64) error { //nolint:contextcheck // runs per event-number ceiling, for the node's lifetime
		err := n.st.SetMetadataCounter(context.Background(), store.MetadataKeyEventNumber, ceiling)
		if err != nil {
			n.logger.Warn("eventlog.persist_ceiling", slog.Any("err", err))
		}
		return err
	}, 0)

	// --- the client stack and the binding manager (brief AO) -------------
	if cfg.Client != nil {
		if n.client, err = n.newClientStack(); err != nil { //nolint:contextcheck // the client stack runs for the node's lifetime, not New's call
			return nil, err
		}
		// The root's time client reads the trusted time source over the
		// client stack, as the commissioner reach does (brief 39 §2.3).
		if ts := n.root.TimeSynchronization; ts != nil {
			ts.AttachTimePeerReader(timePeerReader{n})
		}
	}
	n.watchMatterTime()
	if n.bindings, err = n.newBindings(); err != nil {
		if n.client != nil {
			n.client.close(ctx)
		}
		return nil, err
	}
	n.blockOnAttestationError(ctx)
	ok = true
	return n, nil
}

// blockOnAttestationError refuses commissioning while the configured
// chain did not load (Attestation.BlockCommissioningOnError).
func (n *Node) blockOnAttestationError(ctx context.Context) {
	if n.attestationErr == nil {
		return
	}
	n.logger.Error("attestation.files", slog.Any("err", n.attestationErr),
		slog.String("hint", "new commissioning is refused until the files load; paired controllers keep working"))
	n.SetCommissioningBlocked(ctx, n.attestationErr)
}

// openStores resolves cfg.Store: the overrides, or the two stores on DB
// with their schemas applied (persist.go:95-110).
func (n *Node) openStores(ctx context.Context) error {
	if path := n.cfg.Store.Path; path != "" {
		db, err := openStorePath(path)
		if err != nil {
			return err
		}
		n.ownedDB = db
		n.cfg.Store.DB = db
	}
	sc := n.cfg.Store
	n.st = sc.Credentials
	if sc.DB != nil {
		if err := store.Apply(ctx, sc.DB); err != nil {
			return fmt.Errorf("node: apply store schema: %w", err)
		}
		if err := sqlitestore.Apply(ctx, sc.DB); err != nil {
			return fmt.Errorf("node: apply endpoint schema: %w", err)
		}
		if n.st == nil {
			n.st = store.New(sc.DB)
			n.builtCredentials = true
		}
		if sc.Endpoints == nil {
			n.cfg.Store.Endpoints = sqlitestore.New(sc.DB)
			n.builtEndpoints = true
		}
	}
	return nil
}

// abandon releases what a New that failed had begun: the node's
// lifetime, the advertiser it built and the store file it opened.
func (n *Node) abandon() {
	n.cancel()
	if n.ownsAdvertiser {
		_ = n.advertiser.Close()
	}
	n.closeOwnedDB()
}

// closeOwnedDB closes the handle the node opened for Store.Path; a no-op
// for a host's own handle.
func (n *Node) closeOwnedDB() {
	if n.ownedDB == nil || n.keepOwnedDB {
		return
	}
	if err := n.ownedDB.Close(); err != nil {
		n.logger.Warn("node.store.close", slog.Any("err", err))
	}
	n.ownedDB = nil
}

// advertisement is the commissionable record of the node's own window
// (main.go:329-347 at b4ea1f96^): CM=1, the configured discriminator, the product
// description's name and device type.
func (n *Node) advertisement() (bridge.CommissioningAdvertisement, error) {
	var instanceID [8]byte
	if _, err := rand.Read(instanceID[:]); err != nil {
		return bridge.CommissioningAdvertisement{}, fmt.Errorf("node: commissioning instance id: %w", err)
	}
	pd, bi := n.cfg.ProductDescription, n.cfg.BasicInformation
	name := pd.Name
	if name == "" {
		name = bi.NodeLabel
	}
	deviceType := uint32(pd.DeviceType)
	if deviceType == 0 {
		deviceType = deviceTypeRootNode
	}
	return bridge.CommissioningAdvertisement{
		InstanceID:    instanceID,
		Discriminator: n.cfg.Commissioning.Discriminator,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		NodeLabel:     name,
		RotatingID: mdns.GenerateRotatingID(
			mdns.DeriveUniqueIDFromIdentity(pd.VendorID, pd.ProductID, bi.SerialNumber, bi.NodeLabel), 0,
		),
		CommissioningMode: 1, // §4.3.1.4 CM=1: standard commissioning window
		DeviceTypeID:      deviceType,
	}, nil
}

// fabricInstalled is OperationalCredentials' OnFabricInstalled: the CASE
// identity is rebuilt from the new fabric and its operational record
// published — the only publish a freshly commissioned fabric gets
// (main.go:263-289 at b4ea1f96^).
func (n *Node) fabricInstalled(ctx context.Context, fabricIndex uint8, _, _ uint64, _ []byte) {
	if err := n.caseIDs.load(ctx, n.st, fabricIndex); err != nil {
		n.logger.Warn("case.identity.reload_failed", slog.Any("err", err))
		return
	}
	if !n.announceInstalled(ctx, fabricIndex) {
		n.logger.Warn("case.identity.announce_skipped", slog.Int("fabric_index", int(fabricIndex)))
	}
}

// announceInstalled publishes fabricIndex's operational record from the
// fabric's loaded CASE identity, and reports false for a fabric without
// one. A removal or an UpdateNOC that forgot or replaced the identity
// while the record was being published withdrew a record that was not
// yet out; the record is withdrawn again once it is, so a removed
// fabric's record does not stay published (review II, R2-P2-fabric-05).
// The withdraw and the publish of one fabric are ordered by the identity:
// a removal forgets it before it withdraws ([Node.fabricRemoved]).
func (n *Node) announceInstalled(ctx context.Context, fabricIndex uint8) bool {
	compressedID, nodeID, ok := n.caseIDs.announceIdentity(fabricIndex)
	if !ok {
		return false
	}
	n.br.AnnounceFabric(ctx, fabricIndex, compressedID, nodeID)
	if c, id, still := n.caseIDs.announceIdentity(fabricIndex); !still || c != compressedID || id != nodeID {
		n.br.WithdrawFabric(ctx, fabricIndex, compressedID, nodeID)
	}
	return true
}

// fabricLeaving is the cluster's OnFabricLeaving hook: the Leave event,
// then the flush of the fabric's subscriptions, both while the store
// still holds the fabric. Mirrors matter.js Fabric.leave
// (packages/protocol/src/fabric/Fabric.ts:447-457 at db021da0, leave):
// the leaving event BasicInformationServer answers with Leave
// (BasicInformationServer.ts:175, :186-188), then every session's
// closeSubscriptions(true), then the delete. A fail-safe's revert
// deletes without leaving (FailsafeContext.ts:271-274) and emits none.
func (n *Node) fabricLeaving(ctx context.Context, fabricIndex uint8) {
	n.root.BasicInformation.EmitLeave(fabricIndex)
	n.br.EmitFabricLeavingContext(ctx, fabricIndex)
}

// fabricRemoved is OperationalCredentials' OnFabricRemoved
// (main.go:362-385 at b4ea1f96^), the one function that runs every
// consequence of a fabric's removal once the store no longer holds it:
// first every server's state for the fabric, through the removal
// contract on the root, the Aggregator and every endpoint
// ([Node.dropFabricState]); then what is no cluster server's state — the
// CASE identity and record, the bridge's subscriptions, window, groups,
// scenes, sessions and the host's hook, the event log, the participants,
// the terms-and-conditions acceptance and the node's own window once the
// last fabric is gone. The Leave event went out in [Node.fabricLeaving].
func (n *Node) fabricRemoved(ctx context.Context, fabricIndex uint8) {
	n.dropFabricState(ctx, fabricIndex)
	// The client stack's peers of the fabric, while its identity lasts.
	n.forgetClientPeers(ctx, fabricIndex)
	if compressedID, nodeID, ok := n.caseIDs.forget(fabricIndex); ok {
		n.br.WithdrawFabric(ctx, fabricIndex, compressedID, nodeID)
	}
	n.br.EmitFabricRemovedContext(ctx, fabricIndex)
	// The removed fabric's fabric-sensitive events leave the log
	// (FabricScopedDataHandler.ts:114-170 at 862ad75f).
	if log := n.br.EventLog(); log != nil {
		log.DropFabric(fabricIndex)
	}
	n.fabricRemovedParticipants(ctx, fabricIndex)
	// The last fabric's removal resets the terms-and-conditions acceptance
	// (GeneralCommissioningCluster.cpp:357-374 at 6170af84).
	if srv := n.root.GeneralCommissioning; srv != nil {
		switch fabrics, err := n.st.ListFabrics(ctx); {
		case err != nil:
			n.logger.Warn("node.terms.reset", slog.Any("err", err))
		case len(fabrics) == 0:
			if err := srv.LastFabricRemoved(ctx); err != nil {
				n.logger.Warn("node.terms.reset", slog.Any("err", err))
			}
		}
	}
	n.reopenIfDecommissionedWhenIdle(ctx)
}

// reopenIfDecommissionedWhenIdle reopens the own window after the removal
// of a commissioned node's last fabric (reopenWhenIdle). A fabric that
// only the fail-safe added — a first commissioning rolled back — does not
// decommission the node: matter.js's #syncCommissioned sees no change
// for it (commissioned was never set, CommissioningServer.ts:148-152 at
// db021da0; a deletion is synced at once, :118-134) and starts no reset
// (:136-140), so the window stays as the commissioning left it.
func (n *Node) reopenIfDecommissionedWhenIdle(ctx context.Context) {
	if !n.commissioned.Load() {
		return
	}
	if fabrics, err := n.st.ListFabrics(ctx); err != nil || len(fabrics) > 0 {
		return
	}
	n.commissioned.Store(false)
	n.reopenWhenIdle()
}

// reopenPollInterval is how often a pending reopen after the last fabric's
// removal checks whether the node has become idle.
const reopenPollInterval = 250 * time.Millisecond

// reopenWhenIdle reopens the node's own commissioning window once the last
// fabric is gone and the node is idle: no fail-safe armed and no session
// left — the session that carried RemoveFabric closes after its response.
// A fabric that exists by then (a commissioning completed under the
// fail-safe) cancels it. Mirrors matter.js CommissioningServer
// #resetWhenIdle (CommissioningServer.ts:174-199); the erase that follows
// there is not mirrored (BD-Matter-LastFabricReopenWithoutErase). One
// pending reopen at a time, bounded by the node's lifetime.
func (n *Node) reopenWhenIdle() {
	if !n.reopenPending.CompareAndSwap(false, true) {
		return
	}
	started := n.goKeeper(func() {
		defer n.reopenPending.Store(false)
		tick := n.cfg.Clock.NewTicker(reopenPollInterval)
		defer tick.Stop()
		for {
			armed := n.root.GeneralCommissioning != nil && n.root.GeneralCommissioning.FailSafeArmed()
			if !armed && n.sec.sessions.Active() == 0 {
				n.reopenUnlessStopping()
				return
			}
			select {
			case <-n.life.Done():
				return
			case <-tick.C():
			}
		}
	})
	if !started {
		n.reopenPending.Store(false)
	}
}

// reopenUnlessStopping is reopenWhenIdle's reopen, which a Stop that began
// cancels: under keeperMu, so it either opens the window before stopLocked
// set stopping — and br.Stop then stops the window's timer — or not at all
// (TestReopenPendingAtStopArmsNoWindow). It runs on windowLife, which Stop
// cancels before it waits for keeperMu (TestStopEndsAReopenWaitingOnTheLifetime).
func (n *Node) reopenUnlessStopping() {
	n.keeperMu.Lock()
	defer n.keeperMu.Unlock()
	if n.stopping {
		return
	}
	n.window.reopenIfDecommissioned(n.windowLife, n.st)
}

// stopBegan reports whether a Stop began: the gate of the keepers
// (keeperMu, stopping), which the host's window calls share (review II,
// R2-P2-stop-02).
func (n *Node) stopBegan() bool {
	n.keeperMu.Lock()
	defer n.keeperMu.Unlock()
	return n.stopping
}

// goKeeper runs fn on a goroutine Stop waits for (keeperWG), unless Stop
// began; it reports whether it started one. A handler that outlives
// br.Stop's bounded wait starts nothing (TestLateHandlerKeeperDoesNotRaceStop).
func (n *Node) goKeeper(fn func()) bool {
	n.keeperMu.Lock()
	defer n.keeperMu.Unlock()
	if n.stopping {
		return false
	}
	n.keeperWG.Add(1)
	go func() {
		defer n.keeperWG.Done()
		fn()
	}()
	return true
}

// fabricUpdated is OperationalCredentials' OnFabricUpdated
// (main.go:387-406 at b4ea1f96^): UpdateNOC moves the CASE identity and the
// operational record, and ends the fabric's other sessions, each told
// with a CloseSession (Bridge.CloseFabricSessionsExcept). The fabric's
// resumption records go with the replaced identity, so no peer resumes a
// session established under the old NOC, and so do the client stack's
// sessions and records towards the fabric's peers
// ([Node.closeClientFabricSessions]). Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:167-170 (replaceFabric →
// SessionManager.deleteResumptionRecordsForFabric, SessionManager.ts:537).
func (n *Node) fabricUpdated(ctx context.Context, fabricIndex uint8) {
	if err := n.st.RemoveResumptionsByFabric(ctx, fabricIndex); err != nil {
		n.logger.Warn("case.resumption.remove_failed", slog.Int("fabric_index", int(fabricIndex)), slog.Any("err", err))
	}
	n.closeClientFabricSessions(ctx, fabricIndex)
	oldCompressed, oldNode, hadOld := n.caseIDs.announceIdentity(fabricIndex)
	if err := n.caseIDs.load(ctx, n.st, fabricIndex); err != nil {
		n.logger.Warn("case.identity.reload_failed", slog.Any("err", err))
		return
	}
	if newCompressed, newNode, ok := n.caseIDs.announceIdentity(fabricIndex); ok && (!hadOld || newCompressed != oldCompressed || newNode != oldNode) {
		if hadOld {
			n.br.WithdrawFabric(ctx, fabricIndex, oldCompressed, oldNode)
		}
		n.br.AnnounceFabric(ctx, fabricIndex, newCompressed, newNode)
	}
	n.br.CloseFabricSessionsAfterUpdate(ctx, fabricIndex)
}

// observedFabricInstalled is the facade's fabricInstalled, then the
// host's OnFabricAdded with what the AddNOC hook carries.
func (n *Node) observedFabricInstalled(ctx context.Context, fabricIndex uint8, fabricID, nodeID uint64, rootPublicKey []byte) {
	n.fabricInstalled(ctx, fabricIndex, fabricID, nodeID, rootPublicKey)
	n.setAnnounced(fabricIndex, true)
	obs := n.observerList()
	if !slices.ContainsFunc(obs, func(o Observers) bool { return o.OnFabricAdded != nil }) {
		return
	}
	info := FabricInfo{FabricIndex: fabricIndex, FabricID: fabricID, NodeID: nodeID, RootPublicKey: append([]byte(nil), rootPublicKey...)}
	if stored, ok := n.fabricInfo(ctx, fabricIndex); ok {
		info.CompressedFabricID = stored.CompressedFabricID
	}
	for _, o := range obs {
		if fn := o.OnFabricAdded; fn != nil {
			observe(n.logger, "fabric_added", func() { fn(info) })
		}
	}
}

// observedFabricRemoved is the facade's fabricRemoved, then the host's
// OnFabricRemoved. The store no longer holds the fabric when the hook
// fires, so its identity is read from the CASE identity the facade holds
// until its own handler forgets it.
func (n *Node) observedFabricRemoved(ctx context.Context, fabricIndex uint8) {
	if !n.setAnnounced(fabricIndex, false) {
		// The rolled-back row of an AddNOC that failed: the host never
		// saw it added, so it is not told of a removal; the facade's
		// cleanup runs all the same.
		n.fabricRemoved(ctx, fabricIndex)
		return
	}
	info := FabricInfo{FabricIndex: fabricIndex}
	if f, ok := n.caseIDs.forFabric(fabricIndex); ok {
		info.FabricID = f.identity.FabricID
		info.NodeID = f.identity.NodeID
		info.CompressedFabricID = f.identity.CompressedFabricID
		info.RootPublicKey = append([]byte(nil), f.rootPublicKey...)
	}
	n.fabricRemovedWith(ctx, info)
}

// loadStoredFabrics loads the CASE identity of every fabric stored when
// the node starts and returns those it loaded; every stored fabric counts
// as one the host may know of.
func (n *Node) loadStoredFabrics(ctx context.Context, fabrics []store.FabricRecord) []store.FabricRecord {
	loaded := make([]store.FabricRecord, 0, len(fabrics))
	for i := range fabrics {
		n.setAnnounced(fabrics[i].FabricIndex, true)
		if err := n.caseIDs.load(ctx, n.st, fabrics[i].FabricIndex); err != nil {
			n.logger.Warn("case.identity.boot_load_failed", slog.Any("err", err))
			continue
		}
		loaded = append(loaded, fabrics[i])
	}
	return loaded
}

// setAnnounced records whether the host may know of fabricIndex and
// returns whether it could before.
func (n *Node) setAnnounced(fabricIndex uint8, known bool) bool {
	n.announcedMu.Lock()
	defer n.announcedMu.Unlock()
	was := n.announced[fabricIndex]
	if known {
		if n.announced == nil {
			n.announced = map[uint8]bool{}
		}
		n.announced[fabricIndex] = true
	} else {
		delete(n.announced, fabricIndex)
	}
	return was
}

// reportRolledBackFabrics runs, once, the removal consequences of every
// fabric OperationalCredentials rolled back at construction: an AddNOC
// of the previous run that no CommissioningComplete committed before the
// node went down. That run announced the fabric (OnFabricAdded) and the
// commissioner may have written fabric-scoped state for it outside the
// rows its removal cascades — a binding, a trusted time source, an
// Extension entry — so the node runs its fabric-removed fan-out and the
// host's OnFabricRemoved for it, with the identity the row carried, before
// it serves. connectedhomeip does the same after its start-up rollback:
// FabricTable::Init remembers the deleted index
// (src/credentials/FabricTable.cpp:1129-1140) and Server::Init schedules
// the fail-safe cleanup for it once everything is initialised
// (src/app/server/Server.cpp:662-685 at 6170af84). Review II,
// R2-P2-comm-03. The lifecycle slot is held.
func (n *Node) reportRolledBackFabrics(ctx context.Context) {
	if n.rolledBackReported {
		return
	}
	n.rolledBackReported = true
	recs := n.root.OperationalCredentials.RolledBackFabrics()
	for i := range recs {
		n.logger.Warn("fabric.rolled_back_at_start", slog.Int("fabric_index", int(recs[i].FabricIndex)))
		n.fabricRemovedWith(ctx, fabricInfoOf(recs[i]))
	}
}

// fabricRemovedWith is the facade's fabricRemoved for info's fabric, then
// the host's OnFabricRemoved with info.
func (n *Node) fabricRemovedWith(ctx context.Context, info FabricInfo) {
	n.fabricRemoved(ctx, info.FabricIndex)
	for _, o := range n.observerList() {
		if fn := o.OnFabricRemoved; fn != nil {
			observe(n.logger, "fabric_removed", func() { fn(info) })
		}
	}
}

// observedFabricUpdated is the facade's fabricUpdated, then the host's
// OnFabricUpdated with the fabric as the store now holds it.
func (n *Node) observedFabricUpdated(ctx context.Context, fabricIndex uint8) {
	n.fabricUpdated(ctx, fabricIndex)
	n.notifyFabricUpdated(ctx, fabricIndex)
}

// observedFabricChanged is the host's OnFabricUpdated after UpdateFabricLabel
// or SetVIDVerificationStatement changed a fabric under the same
// operational identity: the facade has nothing to move (no CASE identity,
// no operational record, no session), the host sees the fabric as the
// store now holds it. matter.js reports these as an "updated" fabric, the
// fabric's persist emitting its replaced event (FabricManager.ts:236-246),
// which the node hands on as fabricsChanged (CommissioningServer.ts:118-141).
func (n *Node) observedFabricChanged(ctx context.Context, fabricIndex uint8) {
	n.notifyFabricUpdated(ctx, fabricIndex)
}

// commissioningCompleted runs the host's OnCommissioningComplete; the
// root's CommissioningComplete hook calls it after its own body
// (root.go).
func (n *Node) commissioningCompleted(ctx context.Context, fabricIndex uint8) {
	n.failSafeCommitted(ctx)
	n.commissioned.Store(true)
	n.leaveRadioWhenCommissioned() //nolint:contextcheck // the stop outlives the CommissioningComplete request on purpose (its response must not wait for the radio); it is bounded by the node's lifetime, n.life
	// Matter §11.10.6.6 step 4, the PASE sessions cleared after the
	// response, is the bridge's: it reads the command's response on its
	// invoke path (bridge/pase_close.go), for a host without this facade
	// too.
	n.ensureOTAProviderACL(ctx)
	n.ensureRequestorACL(ctx)
	for _, o := range n.observerList() {
		if fn := o.OnCommissioningComplete; fn != nil {
			observe(n.logger, "commissioning_complete", func() { fn(fabricIndex) })
		}
	}
}

// leaveRadioWhenCommissioned takes a node with a BLE radio off it once its
// commissioning is committed — matter.js endUncommissionedMode on the
// commissioned event (ServerNetworkRuntime.ts:294-308, wired at :386, at
// db021da0), which a fabric added under a fail-safe raises only once the
// fail-safe ended in its commit (CommissioningServer.ts:118-127, :148-158).
// matter.js hands the deletion of the advertiser and the transport to the
// runtime (:300-307); here it runs on a goroutine of its own, so the
// CommissioningComplete response does not wait for the radio, and Stop
// waits for it.
func (n *Node) leaveRadioWhenCommissioned() {
	if n.cfg.Network.BLE == nil {
		return
	}
	n.goKeeper(func() {
		ctx, cancel := clock.WithTimeout(n.life, clock.Or(n.cfg.Clock), bleStopTimeout)
		defer cancel()
		if err := n.br.StopBLE(ctx); err != nil {
			n.logger.Warn("commissioning.ble.stop", slog.Any("err", err))
		}
	})
}

// notifyFabricUpdated runs every OnFabricUpdated with the fabric as the
// store now holds it.
func (n *Node) notifyFabricUpdated(ctx context.Context, fabricIndex uint8) {
	obs := n.observerList()
	if !slices.ContainsFunc(obs, func(o Observers) bool { return o.OnFabricUpdated != nil }) {
		return
	}
	info, _ := n.fabricInfo(ctx, fabricIndex)
	for _, o := range obs {
		if fn := o.OnFabricUpdated; fn != nil {
			observe(n.logger, "fabric_updated", func() { fn(info) })
		}
	}
}

// Start brings the node online: the bridge starts, and the daemon's
// post-start sequence runs (main.go:428-489 at b4ea1f96^) — the diagnostic-log
// provider, the StartUp and BootReason events, the operational-time
// keeper, the PartsList providers, the CASE identities of the fabrics a
// previous run installed, the re-establishment of their subscriptions,
// the OTA requestor, their operational records, and the node's own
// commissioning window when no fabric exists. Start is idempotent, as
// matter.js Node.start (Node.ts:114-132); after Stop it is [ErrStopped].
//
// The node counts as started once Start completed. A Start that fails
// leaves the node startable, as matter.js's Node.start does — it sets the
// target state back to "offline" and rethrows (Node.ts:113-128): the node
// is not started, a later Start runs the whole start path again, and
// what New opened (the store file of Store.Path) stays open until Stop.
// Every step that can fail — the DataModelTest check, the PartsList
// providers, the bridge's start (topology, sockets) — runs before
// anything is started, and the bridge's start undoes its own partial
// work (bridge.Bridge.Start), so a failed Start leaves no goroutine, no
// socket and no open window behind. A host that gives up calls Stop;
// [Node.Run] does.
func (n *Node) Start(ctx context.Context) error {
	n.lifecycle <- struct{}{}
	defer n.leaveLifecycle()
	switch {
	case n.stopped || n.stopBegun:
		return ErrStopped
	case n.started.Load():
		return nil
	}
	if err := n.startLocked(ctx); err != nil {
		return err
	}
	n.started.Store(true)
	return nil
}

// startLocked is Start's work; the lifecycle slot is held. It fails only before it has
// started anything (see Start): every fallible step comes first.
func (n *Node) startLocked(ctx context.Context) error {
	// The start is work in hand of the node's in-flight group but for its
	// wait on the controllers of re-established subscriptions: a
	// fabrictest site does not move its clock under the start's own work,
	// and does move it for the start's peer waits (internal/inflight).
	work := countWork(n.cfg.Clock, inflight.NodeStart)
	defer work.pause()
	// The DataModelTest feature against the node's MaxPathsPerInvoke, before
	// anything goes online. Mirrors matter.js
	// packages/node/src/behaviors/general-diagnostics/GeneralDiagnosticsServer.ts
	// #online (:352) → #assertDataModelTest (:125-143): without the feature a
	// MaxPathsPerInvoke above one fails the node's start, with it a limit of
	// one is logged.
	if err := n.checkDataModelTest(); err != nil {
		return err
	}
	// The root's and the Aggregator's PartsList providers read the live
	// topology on each read; attaching them needs only the root servers,
	// so it is checked before anything starts.
	if err := attachPartsListProviders(n.br); err != nil {
		return err
	}
	// The fabrics construction rolled back go before anything is served
	// (R2-P2-comm-03).
	n.reportRolledBackFabrics(ctx)
	// The bridge's start is the last step that can fail; it leaves
	// nothing bound or published when it does (bridge.Bridge.Start).
	if err := n.br.Start(ctx); err != nil {
		return fmt.Errorf("node: bridge start: %w", err)
	}
	// The goroutines New leaves unstarted (ADR 0026 principle 5): the
	// subscription engine, the session, PASE and CASE reapers — all
	// bounded by the node's lifetime, and started only
	// once the start can no longer fail, so a failed Start leaves none
	// running (the engine cannot be restarted once stopped).
	n.sec.start(n.life) //nolint:contextcheck // the engine and the reapers run for the node's lifetime, not the call's
	// The assembled surface against the one a previous run persisted: a
	// change raises the ConfigurationVersions (surface_stability.go).
	n.announceSurface(ctx)
	n.callbacks.Start()
	if n.agg != nil {
		n.agg.setStarted()
	}
	n.startGroupSender()
	n.startHealthProbe()

	n.root.DiagnosticLogs.AttachProvider(ringLogs{n.diagRing})
	// BasicInformation StartUp, then GeneralDiagnostics BootReason, the
	// order matter.js emits them in (main.go:439-444 at b4ea1f96^).
	n.root.BasicInformation.EmitStartUp()
	n.root.GeneralDiagnostics.EmitBootReason()
	n.keeperWG.Add(1)
	go func() {
		defer n.keeperWG.Done()
		keepOperationalTime(n.life, n.cfg.Clock, n.st, n.root.GeneralDiagnostics, operationalTimeInterval, n.logger)
	}()

	n.ensureOTAProviderACL(ctx)
	n.camerasOnline(ctx)
	// The committed Wi-Fi network is rejoined before the fabrics'
	// subscriptions and records go out ([Network].WiFi's Driver): chip's
	// link comes back with the device (WiFiNetworkCommissioning.Start).
	if wifi := n.root.WiFiNetworkCommissioning; wifi != nil {
		work.pause()
		wifi.Start(ctx)
		work.resume()
	}

	// Fabrics of a previous run: their CASE identities, the subscriptions
	// their controllers held, then their operational records — matter.js
	// re-establishes before entering operational mode (main.go:461-489 at b4ea1f96^).
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("fabrics.list_failed", slog.Any("err", err))
	}
	stored := len(fabrics)
	// A node with stored fabrics starts commissioned — marked from this
	// read, before the CASE identities load: from then on an
	// administrator can remove the last fabric while Start still runs,
	// and that removal reopens the window (reopenIfDecommissionedWhenIdle)
	// only on a node already marked commissioned; marking it at the end
	// would mark it again from this stale count.
	n.commissioned.Store(err == nil && stored > 0)
	loaded := n.loadStoredFabrics(ctx, fabrics)
	work.pause()
	n.br.ReestablishFormerSubscriptions(ctx)
	work.resume()
	if n.root.OTARequestor != nil {
		if err := n.root.OTARequestor.Start(ctx); err != nil {
			n.logger.Warn("ota.requestor_start", slog.Any("err", err))
		}
	}
	// A fabric removed since its identity loaded is not announced, and
	// one removed while its record went out is withdrawn again.
	for i := range loaded {
		n.announceInstalled(ctx, loaded[i].FabricIndex)
	}
	// The server is ready: without a time the root's TimeSynchronization
	// asks the trusted time source, then the default NTP server
	// (OnPlatformEventFn, TimeSynchronizationCluster.cpp:556-569 at
	// 6170af84), for the node's lifetime.
	if ts := n.root.TimeSynchronization; ts != nil {
		ts.Start(n.life) //nolint:contextcheck // the time client runs for the node's lifetime, not the call's
	}
	n.startICD(ctx)
	// The Binding servers register their entries once the node is online
	// and its CASE identities are loaded (BindingManager.ts:65-78: the
	// queue flushes on the node's online event).
	n.startBindings()
	// An uncommissioned node opens its own window (main.go:491-501 at b4ea1f96^;
	// matter.js CommissioningServer enterCommissionableMode, which asks
	// whether the fabric table is empty — not whether its identities
	// loaded). A node with stored fabrics none of which loaded is a broken
	// commissioned node, not a new one: advertising it as commissionable
	// would invite a stranger onto a bridge its owners still administer.
	// A store that cannot list its fabrics fails closed the same way.
	switch {
	case err != nil:
	case stored == 0:
		if err := n.br.CommissioningWindow().OpenOwnWindow(ctx, 0); err != nil {
			n.logger.Warn("commissioning.own_window", slog.Any("err", err))
		}
	case len(loaded) == 0:
		n.logger.Error("commissioning.no_fabric_loaded",
			slog.Int("stored_fabrics", stored),
			slog.String("hint", "every stored fabric failed to load its CASE identity; the node is not opened for commissioning"))
	}
	return nil
}

// Stop takes the node offline and releases what New and Start began, in
// the daemon's shutdown order (main.go:430-450 at b4ea1f96^): the OTA requestor, the
// operational time stored once more, BasicInformation ShutDown (when the
// node was started), the PASE providers, the CASE reaper, the session
// table's idle reaper, the subscription engine, the re-announce loop, the health probe, the
// bridge, the advertiser and the store the node opened for Store.Path.
// Stop is idempotent
// (matter.js Node.stop, Node.ts:193) and safe without Start. A context
// already done cuts short only Stop's waits; Stop returns an error only
// when the bridge did not stop (a bridge Start or Stop the host runs
// itself held it until ctx ended, or a Start of the node's own was still
// running when ctx ended), and then the node is not stopped yet: it keeps
// its store and the next Stop finishes the work. Start is refused from the
// first Stop on.
func (n *Node) Stop(ctx context.Context) error {
	if err := n.enterLifecycle(ctx); err != nil {
		return err
	}
	defer n.leaveLifecycle()
	return n.stopLocked(ctx)
}

// enterLifecycle takes the lifecycle slot, waiting for a Start or
// FactoryReset in progress until ctx ends. A free slot is taken whatever
// ctx's state, so a Stop with a context already done still stops an idle
// node. The shape of bridge.Bridge.enterLifecycle (review II,
// R2-P2-stop-08).
func (n *Node) enterLifecycle(ctx context.Context) error {
	select {
	case n.lifecycle <- struct{}{}:
		return nil
	default:
	}
	select {
	case n.lifecycle <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("node: waiting for a start in progress: %w", ctx.Err())
	}
}

// leaveLifecycle releases the lifecycle slot.
func (n *Node) leaveLifecycle() { <-n.lifecycle }

// stopLocked is Stop's work; the lifecycle slot is held. When the bridge does not
// stop (its Stop waited for a bridge Start or Stop in progress until ctx
// ended), the node is not stopped: the advertiser, the lifetime context
// and the store stay, and the next Stop resumes at the bridge (review II,
// R2-P1-dr-req-01).
func (n *Node) stopLocked(ctx context.Context) error {
	if n.stopped {
		return nil
	}
	// The stop is work in hand of the node's in-flight group but for the
	// client stack's close, which waits on its peers (see Start).
	work := countWork(n.cfg.Clock, inflight.NodeStop)
	defer work.pause()
	if !n.stopBegun {
		n.stopBegun = true
		n.stopParts(ctx, work)
	}
	// A fail-safe rollback that began before the stop ends before the
	// store goes (review II, R2-P2-stop-06); a ctx that ends first fails
	// Stop, and the next Stop waits again.
	if err := n.root.GeneralCommissioning.Close(ctx); err != nil {
		return fmt.Errorf("node: %w", err)
	}
	if err := n.br.Stop(ctx); err != nil {
		return fmt.Errorf("node: bridge stop: %w", err)
	}
	n.stopped = true
	// The devices are unbound once no controller reaches them: each
	// device's queue drains before Stop returns.
	if n.agg != nil {
		n.agg.detachAll()
	}
	// The node's own callbacks drain the same way (OnDiagnosticEventAsync).
	n.callbacks.Stop()
	_ = n.advertiser.Close()
	n.cancel()
	n.keeperWG.Wait()
	n.closeOwnedDB()
	return nil
}

// stopParts is the part of Stop before the bridge's, run once; work is
// stopLocked's count.
func (n *Node) stopParts(ctx context.Context, work *countedWork) {
	// No keeper starts and no reopen opens the window from here on
	// (keeperMu); a reopen in progress, which holds it, has its waits
	// ended first (windowLife).
	n.cancelWindow()
	n.keeperMu.Lock()
	n.stopping = true
	n.keeperMu.Unlock()
	if n.root.OTARequestor != nil {
		n.root.OTARequestor.Stop()
	}
	n.unwatchMatterTime()
	if ts := n.root.TimeSynchronization; ts != nil {
		ts.Stop()
	}
	if gd := n.root.GeneralDiagnostics; gd != nil {
		gd.Stop()
	}
	if n.root.otaApplier != nil {
		n.root.otaApplier.close()
	}
	if n.started.Load() {
		if err := storeOperationalTime(ctx, n.st, n.root.GeneralDiagnostics); err != nil {
			n.logger.Warn("gendiag.operational_hours.persist", slog.Any("err", err))
		}
		n.root.BasicInformation.EmitShutDown()
	}
	// Every live binding is removed before the client stack goes
	// (BindingServer.ts:52, :160-163).
	n.bindings.Close()
	if n.client != nil {
		work.pause()
		n.client.close(ctx)
		work.resume()
	}
	n.sec.pase.Stop()
	n.window.stop()
	n.sec.casep.StopReaper()
	n.sec.sessions.StopReaper()
	n.sec.subs.Stop()
	n.stopGroupSender()
	if n.bdx != nil {
		n.bdx.Close()
	}
	if n.commissionerControl != nil {
		n.commissionerControl.Close()
	}
	n.stopHealthProbe()
	if srv := n.root.ICDManagement; srv != nil {
		srv.Stop()
	}
}

// countedWork is one unit of a node's work in hand on the in-flight group
// its clock carries (internal/inflight), paused around a wait on peers.
type countedWork struct {
	g    *inflight.Group
	name string
	held bool
}

// countWork counts one unit of name on the group clk carries.
func countWork(clk clock.Clock, name string) *countedWork {
	w := &countedWork{g: inflight.Of(clk), name: name}
	w.resume()
	return w
}

// pause stops counting the unit; a no-op when it is not counted.
func (w *countedWork) pause() {
	if w.held {
		w.held = false
		w.g.Leave(w.name)
	}
}

// resume counts the unit again; a no-op when it is counted.
func (w *countedWork) resume() {
	if !w.held {
		w.held = true
		w.g.Enter(w.name)
	}
}

// newCallbackQueue is the node's own `…Async` callback queue; a callback
// counts as work of the in-flight group clk carries until it returned
// (internal/inflight DeviceCallbacks).
func newCallbackQueue(clk clock.Clock) *serialqueue.Queue {
	q := serialqueue.New()
	g := inflight.Of(clk)
	q.CountIn(func() *inflight.Group { return g })
	return q
}

// Run starts the node, waits for ctx to end and stops it — matter.js
// Node.run (Node.ts:183-186). A Start that fails is followed by Stop, so
// Run releases what New opened either way.
func (n *Node) Run(ctx context.Context) error {
	if err := n.Start(ctx); err != nil {
		return errors.Join(err, n.Stop(context.WithoutCancel(ctx)))
	}
	<-ctx.Done()
	stopCtx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), n.cfg.Clock, stopTimeout)
	defer cancel()
	return n.Stop(stopCtx)
}

// Bridge returns the bridge, for a host that needs a port the facade does
// not cover (ADR 0018 decision 5).
func (n *Node) Bridge() *bridge.Bridge { return n.br }

// LocalAddr returns the bound `host:port` of the node's operational UDP
// listener — empty before [Node.Start] and after [Node.Stop]
// (bridge.Bridge.LocalAddr). It is what the health probe checks.
func (n *Node) LocalAddr() string { return n.br.LocalAddr() }

// Root returns the root endpoint's servers.
func (n *Node) Root() *RootClusters { return n.root }

// Aggregator returns the Aggregator endpoint's bridged-device set — nil
// when the host assembles its own topology ([Config].Snapshotter).
func (n *Node) Aggregator() *Aggregator { return n.agg }

// Sessions returns the secure-session manager, for a host's diagnostics
// (operational.Manager.Sessions, Occupancy): which peers hold a PASE or
// CASE session, on which fabric, and when each was last active. The
// facade owns its lifecycle; a host reads it and leaves closing to the
// node.
func (n *Node) Sessions() *SessionManager { return n.sec.sessions }

// Subscriptions returns the subscription manager, for a host's
// diagnostics: Snapshot lists every active subscription with its paths
// and intervals as copies ([SubscriptionInfo]), CountBySession and Active
// count them. The facade owns its lifecycle; a host reads it and leaves
// closing to the node.
func (n *Node) Subscriptions() *SubscriptionManager { return n.sec.subs }

// RemoveFabric removes a fabric without a commissioner — a host's revoke
// or factory reset — exactly as the RemoveFabric command does: the
// fabric's rows go, and the same fan-out runs (the Leave event, the OTA
// providers, the CASE identity and the operational record, the sessions
// and subscriptions, the group state, the node's own window once the last
// fabric is gone) followed by the host's OnFabricRemoved observer. matter.js
// reaches it the same way from the host, Fabric.leave() (Fabric.ts:423-433),
// the call the command itself makes (OperationalCredentialsServer.ts:425).
// An unknown index is store.ErrFabricNotFound.
func (n *Node) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	return n.root.OperationalCredentials.RemoveFabric(ctx, fabricIndex)
}

// SetCommissioningBlocked refuses every new commissioning while err is
// non-nil — the node's own window (at Start and after the last fabric is
// removed), [Node.OpenBasicWindow], the host's opener and both
// AdministratorCommissioning commands of a paired controller, which
// answer FAILURE — and revokes a window that is open; controllers already
// paired keep working. nil lifts the block and, on a node without a
// fabric, reopens its own window at once. A host that cannot serve the
// attestation chain it was configured with calls it before [Node.Start].
// See [bridge.CommissioningWindow.SetBlocked]; matter.js has no
// counterpart (BD-Matter-CommissioningBlocked).
func (n *Node) SetCommissioningBlocked(ctx context.Context, err error) {
	n.br.CommissioningWindow().SetBlocked(ctx, err)
	if err != nil {
		return
	}
	// Lock-free: a host may call this from a callback that runs inside
	// Start or Stop, which hold the lifecycle slot (a Snapshotter, say).
	if n.started.Load() && n.window != nil && n.st != nil {
		n.window.reopenIfDecommissioned(ctx, n.st)
	}
}

// FabricInfos returns the installed fabrics in the store's order (by
// fabric index): index, fabric and node id, root public key, compressed
// fabric id, vendor and label.
func (n *Node) FabricInfos(ctx context.Context) ([]FabricInfo, error) {
	recs, err := n.st.ListFabrics(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FabricInfo, 0, len(recs))
	for i := range recs {
		out = append(out, fabricInfoOf(recs[i]))
	}
	return out, nil
}

// UniqueID returns the BasicInformation UniqueID the node serves: the
// host's [BasicInformation].UniqueID, else the value the node pinned in
// its store at the first boot (or, once bootid.EnableRotation ran in the
// process, this boot's value).
func (n *Node) UniqueID() string { return n.cfg.BasicInformation.UniqueID }

// BootReason returns the GeneralDiagnostics BootReason the node was built
// with — the value [Node.Start]'s EmitBootReason reports. The device layer
// passes it to the default OnOff servers (onoff.Config.BootReason), which
// skip StartUpOnOff after a software update (OnOffServer.ts:36, :326-331).
func (n *Node) BootReason() uint8 { return n.bootReason }

// Store returns the credential store the node runs on.
func (n *Node) Store() store.Port { return n.st }

// Persistence is the endpoint-level persistence (scene tables, bridged
// NodeLabels) the facade's assembler uses: [Config].EndpointPersistence
// when set, else the facade's own over [Node.Store] — what a host with its
// own assembler ([Config].Snapshotter) hands that assembler (issue #117).
func (n *Node) Persistence() EndpointPersistence { return n.endpoints }

// Groups returns the group state the node runs on — [Config].Groups, or
// the manager the facade built. A host that assembles its own topology
// binds it into its assembler (endpoint.Config.Groups), so the endpoints'
// Groups servers, the bridge's group-message routing and the root's
// GroupKeyManagement share one state.
func (n *Node) Groups() *Groups { return n.groups }

// wireEventsUTC stamps the events' EpochTimestamp with the node's UTC,
// the root TimeSynchronization's, when it is mounted (issue 445).
func (n *Node) wireEventsUTC() {
	if ts := n.root.TimeSynchronization; ts != nil {
		n.br.EventLog().SetUTCClock(ts.UTC)
	}
}

// ringLogs serves DiagnosticLogs RetrieveLogsRequest from the diagnostic
// event ring, one line per event (the daemon's ringLogs, wiring.go:
// 1045-1074); CrashLogs answers NoLogs.
type ringLogs struct{ ring *diagevent.Ring }

// Logs implements [mattercore.LogProvider].
func (r ringLogs) Logs(_ context.Context, intent uint8) ([]byte, error) {
	if intent == mattercore.IntentCrashLogs {
		return nil, nil
	}
	var b strings.Builder
	for _, e := range r.ring.Snapshot() {
		fmt.Fprintf(&b, "%s %s %s", e.At.UTC().Format(time.RFC3339), e.Kind, e.Message)
		for _, k := range slices.Sorted(maps.Keys(e.Detail)) {
			fmt.Fprintf(&b, " %s=%s", k, e.Detail[k])
		}
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		b.WriteString("no diagnostic events recorded\n")
	}
	return []byte(b.String()), nil
}

// checkDataModelTest holds the DataModelTest feature against the node's
// MaxPathsPerInvoke (GeneralDiagnosticsServer.ts #assertDataModelTest,
// :125-143).
func (n *Node) checkDataModelTest() error {
	maxPaths := n.cfg.MaxPathsPerInvoke
	if maxPaths == 0 {
		maxPaths = im.DefaultMaxPathsPerInvoke
	}
	if err := n.root.GeneralDiagnostics.CheckDataModelTest(maxPaths, n.logger); err != nil {
		return fmt.Errorf("node: %w", err)
	}
	return nil
}
