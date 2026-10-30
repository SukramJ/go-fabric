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
// restarted; a host builds a new one on the same store.
var ErrStopped = errors.New("node: stopped")

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
	// ownedDB is the handle opened for Config.Store.Path, closed by Stop;
	// nil for a host's own.
	ownedDB   *sql.DB
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
	// (composeRoot).
	rootDecisions []surface.Decision
	caseIDs       *caseIdentities
	sec           *security
	window        *commissioningHost
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
	// attestationErr is the load error of Attestation.Files under
	// BlockCommissioningOnError (attestation.go).
	attestationErr error
	// stopReannounce ends the re-announce loop Start began; nil when none
	// runs (reannounce.go).
	stopReannounce func()
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
	// userLabels keeps the bridged endpoints' UserLabel lists in the
	// store (user_labels.go).
	userLabels userLabelSync
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
	cancel        context.CancelFunc
	keeperWG      sync.WaitGroup

	// mu serialises Start and Stop, the lifecycle; it is held across
	// them, host callbacks included (the host's Snapshotter), and
	// taken by nothing else. started is atomic so a host's call during
	// either (SetCommissioningBlocked) never waits for it (ADR 0026 P6).
	mu      sync.Mutex
	started atomic.Bool
	stopped bool
}

// New builds the node: everything the reference daemon builds before
// bridge.Start (examples/reference-bridge/main.go:135-397 at b4ea1f96^), with every
// port of bridge/bridge.go's package doc attached in its documented order.
//
// Nothing is left as a noop except what the configuration omits on
// purpose: a nil Advertiser is the node's own (Zeroconf with the subtype
// responder, or Noop under Network.MDNS.Disabled), a nil OTA
// mounts no OTA requestor. The TestEventTrigger enable key is not armed
// (Root().GeneralDiagnostics.EnableTestEventTriggers is the host's), and
// SetOnReassembled / SetOnFabricAdded stay the bridge's nil-safe hooks for
// the host.
//
// New starts the subscription engine and the PASE / CASE reapers, as the
// daemon does before Start; call [Node.Stop] to end them, whether or not
// the node was started.
func New(cfg Config) (*Node, error) { //nolint:funlen // the composition root: one sequential wiring pass, as main.go
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// The root's surface is decided before anything is opened or built.
	rootDecisions, err := composeRoot(&cfg)
	if err != nil {
		return nil, err
	}
	attestationErr, err := cfg.resolveAttestation()
	if err != nil {
		return nil, err
	}
	// Before anything derives a UniqueID (persist.go uniqueID, the
	// assembler's bridged endpoints).
	if cfg.BootID.RotateUniqueIDs {
		enableBootIDRotation()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	ctx := context.Background()
	cfg.Clock = clock.Or(cfg.Clock)
	n := &Node{
		cfg: cfg, logger: cfg.Logger, advertiser: cfg.Advertiser, callbacks: newCallbackQueue(cfg.Clock), bootReason: mattercore.BootReasonPowerOnReboot,
		attestationErr: attestationErr,
	}
	n.life, n.cancel = context.WithCancel(ctx)
	n.surfaceDigest = cfg.Surface.Digest()
	n.rootDecisions = rootDecisions
	ok := false
	defer func() {
		if !ok {
			n.abandon()
		}
	}()
	// A nil advertiser is the node's own (mdns.go).
	n.resolveAdvertiser()

	// An advertiser that paces its own timers (mdns.Zeroconf) runs them on
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
	// instead (Config.Topology), as the daemon did before the facade.
	pd, bi := cfg.ProductDescription, cfg.BasicInformation
	snapshotter := cfg.Topology
	if snapshotter == nil {
		n.assembler, err = endpoint.New(n.cfg.Store.Endpoints, endpoint.Config{
			VendorID:           pd.VendorID,
			ProductID:          pd.ProductID,
			NodeLabel:          bi.NodeLabel,
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
		Listen:        cfg.Network.Listen,
		PreferIPv4:    cfg.Network.PreferIPv4,
		TCP:           cfg.Network.TCP,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		NodeLabel:     bi.NodeLabel,
		Discriminator: cfg.Commissioning.Discriminator,
		Clock:         n.cfg.Clock,
		// The invoke path refuses what BasicInformation does not advertise.
		MaxPathsPerInvoke: cfg.MaxPathsPerInvoke,
		// The operational record advertises the reported intervals.
		SessionIntervals: cfg.Sessions.advertised(),
	}, n.logger)
	if err != nil {
		return nil, fmt.Errorf("node: bridge: %w", err)
	}
	n.br.SetDeviceTypeValidation(cfg.DeviceTypeValidation)

	// Access control fails closed without a lister; the stored ACL is what
	// a commissioner writes (main.go:230-233 at b4ea1f96^).
	n.br.AttachACLLister(n.st)
	// Group messaging and the auxiliary ACL, over the restored group state
	// (main.go:234-246 at b4ea1f96^).
	if err := n.groups.Load(ctx); err != nil {
		return nil, fmt.Errorf("node: group state: %w", err)
	}
	n.br.AttachGroupMessaging(n.groups)
	n.br.AttachAuxiliaryACL(n.groups)
	// A bounded trace of the moments that explain a failed pairing
	// (main.go:247-251 at b4ea1f96^).
	n.diagRing = diagevent.NewRing(n.cfg.Diagnostics.capacity())
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
	n.root, err = buildRoot(ctx, rootDeps{
		cfg: &n.cfg, logger: n.logger, bootReason: n.bootReason, st: n.st, groups: n.groups, connector: n.br,
		onFabricInstalled:       n.observedFabricInstalled,
		onCommissioningComplete: n.commissioningCompleted,
		onFailSafeExpired:       n.failSafeExpired,
	})
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
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
	n.sec = wireSecurity(n.life, n.br, n.st, n.root, n.caseIDs, paseParams{
		passcode: cm.Passcode, salt: cm.Salt, iterations: cm.PBKDFIterations,
	}, responderSessionParams(cfg.Network.TCP, cfg.MaxPathsPerInvoke, cfg.Sessions), cfg.Limits, n.logger)
	n.wireICD()

	// --- the commissioning window (main.go:329-360 at b4ea1f96^) ---------------------
	advert, err := n.advertisement()
	if err != nil {
		return nil, err
	}
	n.window = &commissioningHost{
		br: n.br, sessions: n.sec.sessions, root: n.root, logger: n.logger,
		baseline: n.sec.pase, advert: advert, observers: n.observerList,
		responderParams: n.sec.responderParams,
	}
	wireCommissioningWindow(n.life, n.window, n.st)

	// --- fabric hooks (main.go:362-414 at b4ea1f96^) ---------------------------------
	// The host's observers run after each (observers.go).
	n.root.OperationalCredentials.SetOnFabricRemoved(n.observedFabricRemoved)
	n.root.OperationalCredentials.SetOnFabricUpdated(n.observedFabricUpdated)
	n.root.OperationalCredentials.SetOnFabricChanged(n.observedFabricChanged)

	// Event numbers continue across a reboot (main.go:416-426 at b4ea1f96^).
	if ceiling, found, cErr := n.st.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); cErr == nil && found {
		n.br.EventLog().SeedNumber(ceiling)
	}
	n.br.EventLog().SetCounterPersistence(func(ceiling uint64) {
		if err := n.st.SetMetadataCounter(context.Background(), store.MetadataKeyEventNumber, ceiling); err != nil {
			n.logger.Warn("eventlog.persist_ceiling", slog.Any("err", err))
		}
	}, 0)

	// --- the client stack and the binding manager (brief AO) -------------
	if cfg.Client.Enabled {
		if n.client, err = n.newClientStack(); err != nil {
			return nil, err
		}
	}
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
		}
		if sc.Endpoints == nil {
			n.cfg.Store.Endpoints = sqlitestore.New(sc.DB)
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
	if n.ownedDB == nil {
		return
	}
	if err := n.ownedDB.Close(); err != nil {
		n.logger.Warn("node.store.close", slog.Any("err", err))
	}
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
	compressedID, nodeID, ok := n.caseIDs.announceIdentity(fabricIndex)
	if !ok {
		n.logger.Warn("case.identity.announce_skipped", slog.Int("fabric_index", int(fabricIndex)))
		return
	}
	n.br.AnnounceFabric(ctx, compressedID, nodeID)
}

// fabricRemoved is OperationalCredentials' OnFabricRemoved
// (main.go:362-385 at b4ea1f96^): Leave, the OTA providers, the CASE identity and
// record, the sessions, and the node's own window once the last fabric
// is gone.
func (n *Node) fabricRemoved(ctx context.Context, fabricIndex uint8) {
	n.root.BasicInformation.EmitLeave(fabricIndex)
	if n.root.OTARequestor != nil {
		n.root.OTARequestor.FabricRemoved(fabricIndex)
	}
	if compressedID, nodeID, ok := n.caseIDs.forget(fabricIndex); ok {
		n.br.WithdrawFabric(ctx, compressedID, nodeID)
	}
	n.br.EmitFabricRemovedContext(ctx, fabricIndex)
	// The removed fabric's fabric-sensitive events leave the log
	// (FabricScopedDataHandler.ts:114-170 at 862ad75f).
	if log := n.br.EventLog(); log != nil {
		log.DropFabric(fabricIndex)
	}
	n.removeFabricBindings(fabricIndex)
	n.fabricRemovedParticipants(ctx, fabricIndex)
	if srv := n.root.ICDManagement; srv != nil {
		srv.FabricRemoved(ctx, fabricIndex)
	}
	// The removed fabric's trusted time source goes, with
	// MissingTrustedTimeSource (TimeSynchronizationCluster.cpp:1020-1028
	// at 6170af84).
	if srv := n.root.TimeSynchronization; srv != nil {
		srv.FabricRemoved(ctx, fabricIndex)
	}
	// The root's TLS tables drop the fabric's entries
	// (TLSCertificateManagementCluster.cpp:571-575,
	// TLSClientManagementCluster.cpp:272-275 at 6170af84).
	if srv := n.root.TLSClientManagement; srv != nil {
		srv.FabricRemoved(ctx, fabricIndex)
	}
	if srv := n.root.TLSCertificateManagement; srv != nil {
		srv.FabricRemoved(ctx, fabricIndex)
	}
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
	n.keeperWG.Add(1)
	go func() {
		defer n.keeperWG.Done()
		defer n.reopenPending.Store(false)
		tick := n.cfg.Clock.NewTicker(reopenPollInterval)
		defer tick.Stop()
		for {
			armed := n.root.GeneralCommissioning != nil && n.root.GeneralCommissioning.FailSafeArmed()
			if !armed && n.sec.sessions.Active() == 0 {
				n.window.reopenIfDecommissioned(n.life, n.st)
				return
			}
			select {
			case <-n.life.Done():
				return
			case <-tick.C():
			}
		}
	}()
}

// fabricUpdated is OperationalCredentials' OnFabricUpdated
// (main.go:387-406 at b4ea1f96^): UpdateNOC moves the CASE identity and the
// operational record, and ends the fabric's other sessions, each told
// with a CloseSession (Bridge.CloseFabricSessionsExcept).
func (n *Node) fabricUpdated(ctx context.Context, fabricIndex uint8) {
	oldCompressed, oldNode, hadOld := n.caseIDs.announceIdentity(fabricIndex)
	if err := n.caseIDs.load(ctx, n.st, fabricIndex); err != nil {
		n.logger.Warn("case.identity.reload_failed", slog.Any("err", err))
		return
	}
	if newCompressed, newNode, ok := n.caseIDs.announceIdentity(fabricIndex); ok && (!hadOld || newCompressed != oldCompressed || newNode != oldNode) {
		if hadOld {
			n.br.WithdrawFabric(ctx, oldCompressed, oldNode)
		}
		n.br.AnnounceFabric(ctx, newCompressed, newNode)
	}
	n.br.CloseFabricSessionsAfterUpdate(ctx, fabricIndex)
}

// observedFabricInstalled is the facade's fabricInstalled, then the
// host's OnFabricAdded with what the AddNOC hook carries.
func (n *Node) observedFabricInstalled(ctx context.Context, fabricIndex uint8, fabricID, nodeID uint64, rootPublicKey []byte) {
	n.fabricInstalled(ctx, fabricIndex, fabricID, nodeID, rootPublicKey)
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
			observe(n.logger, "fabric_added", func() error { return fn(info) })
		}
	}
}

// observedFabricRemoved is the facade's fabricRemoved, then the host's
// OnFabricRemoved. The store no longer holds the fabric when the hook
// fires, so its identity is read from the CASE identity the facade holds
// until its own handler forgets it.
func (n *Node) observedFabricRemoved(ctx context.Context, fabricIndex uint8) {
	info := FabricInfo{FabricIndex: fabricIndex}
	if f, ok := n.caseIDs.forFabric(fabricIndex); ok {
		info.FabricID = f.identity.FabricID
		info.NodeID = f.identity.NodeID
		info.CompressedFabricID = f.identity.CompressedFabricID
		info.RootPublicKey = append([]byte(nil), f.rootPublicKey...)
	}
	n.fabricRemoved(ctx, fabricIndex)
	for _, o := range n.observerList() {
		if fn := o.OnFabricRemoved; fn != nil {
			observe(n.logger, "fabric_removed", func() error { return fn(info) })
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
	n.ensureOTAProviderACL(ctx)
	n.ensureRequestorACL(ctx)
	for _, o := range n.observerList() {
		if fn := o.OnCommissioningComplete; fn != nil {
			observe(n.logger, "commissioning_complete", func() error { return fn(fabricIndex) })
		}
	}
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
			observe(n.logger, "fabric_updated", func() error { return fn(info) })
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
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case n.stopped:
		return ErrStopped
	case n.started.Load():
		return nil
	}
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
	if err := n.br.Start(ctx); err != nil {
		return fmt.Errorf("node: bridge start: %w", err)
	}
	n.started.Store(true)
	n.callbacks.Start()
	if n.agg != nil {
		n.agg.setStarted()
	}
	n.startGroupSender()
	n.startReannounce()
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

	if err := attachPartsListProviders(n.br); err != nil {
		return err
	}
	n.ensureOTAProviderACL(ctx)
	n.ensureRequestorACL(ctx)

	// Fabrics of a previous run: their CASE identities, the subscriptions
	// their controllers held, then their operational records — matter.js
	// re-establishes before entering operational mode (main.go:461-489 at b4ea1f96^).
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("fabrics.list_failed", slog.Any("err", err))
	}
	stored := len(fabrics)
	loaded := make([]store.FabricRecord, 0, stored)
	for _, fabric := range fabrics {
		if err := n.caseIDs.load(ctx, n.st, fabric.FabricIndex); err != nil {
			n.logger.Warn("case.identity.boot_load_failed", slog.Any("err", err))
			continue
		}
		loaded = append(loaded, fabric)
	}
	work.pause()
	n.br.ReestablishFormerSubscriptions(ctx)
	work.resume()
	if n.root.OTARequestor != nil {
		if err := n.root.OTARequestor.Start(ctx); err != nil {
			n.logger.Warn("ota.requestor_start", slog.Any("err", err))
		}
	}
	for _, fabric := range loaded {
		n.br.AnnounceFabric(ctx, fabric.CompressedID, fabric.NodeID)
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
// (matter.js Node.stop, Node.ts:193) and safe without Start.
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return nil
	}
	n.stopped = true
	// The stop is work in hand of the node's in-flight group but for the
	// client stack's close, which waits on its peers (see Start).
	work := countWork(n.cfg.Clock, inflight.NodeStop)
	defer work.pause()
	if n.root.OTARequestor != nil {
		n.root.OTARequestor.Stop()
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
	n.stopReannounceLoop()
	n.stopHealthProbe()
	if srv := n.root.ICDManagement; srv != nil {
		srv.Stop()
	}
	err := n.br.Stop(ctx)
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
	if err != nil {
		return fmt.Errorf("node: bridge stop: %w", err)
	}
	return nil
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
// Node.run (Node.ts:183-186).
func (n *Node) Run(ctx context.Context) error {
	if err := n.Start(ctx); err != nil {
		return err
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
// when the host assembles its own topology ([Config].Topology).
func (n *Node) Aggregator() *Aggregator { return n.agg }

// Sessions returns the secure-session manager, for a host's diagnostics
// (operational.Manager.Sessions, Occupancy): which peers hold a PASE or
// CASE session, on which fabric, and when each was last active. The
// facade owns its lifecycle; a host reads it and leaves closing to the
// node.
func (n *Node) Sessions() *SessionManager { return n.sec.sessions }

// Subscriptions returns the subscription manager, for a host's
// diagnostics (subscription.Manager.CountBySession, Active): the
// subscriptions riding on each session. The facade owns its lifecycle;
// a host reads it and leaves closing to the node.
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

// CommissioningWindow returns the commissioning window.
func (n *Node) CommissioningWindow() *CommissioningWindow { return n.br.CommissioningWindow() }

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
	// Start or Stop, which hold n.mu (a Snapshotter, say).
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
	for _, rec := range recs {
		out = append(out, fabricInfoOf(rec))
	}
	return out, nil
}

// UniqueID returns the BasicInformation UniqueID the node serves: the
// host's [BasicInformation].UniqueID, else the value the node pinned in
// its store at the first boot (or, with [BootID].RotateUniqueIDs, this
// boot's value).
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
// own assembler ([Config].Topology) hands that assembler (issue #117).
func (n *Node) Persistence() EndpointPersistence { return n.endpoints }

// Groups returns the group state the node runs on — [Config].Groups, or
// the manager the facade built. A host that assembles its own topology
// binds it into its assembler (endpoint.Config.Groups), so the endpoints'
// Groups servers, the bridge's group-message routing and the root's
// GroupKeyManagement share one state.
func (n *Node) Groups() *Groups { return n.groups }

// PairingCodes returns the QR code payload and the manual pairing code of
// the configured passcode and discriminator — matter.js
// `commissioning.pairingCodes` (CommissioningServer.ts:400-410), as the
// daemon prints them (main.go:504-518 at b4ea1f96^).
func (n *Node) PairingCodes() (qr, manual string, err error) {
	cm := n.cfg.Commissioning
	return pairingCodes(n.cfg.ProductDescription, cm.Discriminator, cm.Passcode)
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
