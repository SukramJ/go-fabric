// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/rand"
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
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// ErrStopped is returned by [Node.Start] after [Node.Stop]: a node is not
// restarted; a host builds a new one on the same store.
var ErrStopped = errors.New("node: stopped")

// operationalTimeInterval is how often the operational time is persisted
// while the node runs (the daemon's time.Minute, main.go:430).
const operationalTimeInterval = time.Minute

// stopTimeout bounds the Stop [Node.Run] performs once its context ends
// (the daemon's 5 s, main.go:432).
const stopTimeout = 5 * time.Second

// Node is a Matter node: the bridge, its root and Aggregator endpoints and
// every port [bridge] documents, wired in the documented order.
type Node struct {
	cfg        Config
	logger     *slog.Logger
	st         *store.Store
	groups     *groups.Manager
	assembler  *endpoint.Assembler
	br         *bridge.Bridge
	root       *RootClusters
	agg        *Aggregator
	caseIDs    *caseIdentities
	sec        *security
	window     *commissioningHost
	diagRing   *diagevent.Ring
	advertiser mdns.Advertiser
	// stopReannounce ends the re-announce loop Start began; nil when none
	// runs (reannounce.go).
	stopReannounce func()
	bootReason     uint8
	persisted      persisted
	// endpoints is the endpoint-level persistence the facade's assembler
	// and Aggregator use: Config.EndpointPersistence, else persisted.
	endpoints EndpointPersistence
	// groupTx is the socket group messages leave through (groupsend.go).
	groupTx  *udp.Listener
	client   *clientStack
	bindings *binding.Manager
	bindSync bindingSync
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

	mu      sync.Mutex
	started bool
	stopped bool
}

// New builds the node: everything the reference daemon builds before
// bridge.Start (examples/reference-bridge/main.go:135-397), with every
// port of bridge/bridge.go's package doc attached in its documented order.
//
// Nothing is left as a noop except what the configuration omits on
// purpose: a nil Advertiser publishes nothing (mdns.NewNoop), a nil OTA
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
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Advertiser == nil {
		cfg.Advertiser = mdns.NewNoop()
	}
	ctx := context.Background()
	cfg.Clock = clock.Or(cfg.Clock)
	n := &Node{cfg: cfg, logger: cfg.Logger, advertiser: cfg.Advertiser, bootReason: mattercore.BootReasonPowerOnReboot}
	n.life, n.cancel = context.WithCancel(ctx)
	ok := false
	defer func() {
		if !ok {
			n.cancel()
		}
	}()

	// An advertiser that paces its own timers (mdns.Zeroconf) runs them on
	// the node's clock.
	if a, ok := n.advertiser.(interface{ SetClock(clock.Clock) }); ok {
		a.SetClock(n.cfg.Clock)
	}

	// --- persistence (main.go:146-165, persist.go openDB) ---------------
	if err := n.openStores(ctx); err != nil {
		return nil, err
	}
	n.cfg.Store.Credentials = n.st
	n.persisted = persisted{st: n.st, logger: n.logger}
	n.endpoints = cfg.EndpointPersistence
	if n.endpoints == nil {
		n.endpoints = n.persisted
	}
	var err error
	// The host's group manager when it supplies one (its assembler mounts
	// the Groups servers over it), else the facade's own.
	n.groups = cfg.Groups
	if n.groups == nil {
		if n.groups, err = groups.NewManager(n.st, n.logger); err != nil {
			return nil, fmt.Errorf("node: group state: %w", err)
		}
		n.groups.SetClock(n.cfg.Clock)
	}

	// --- the topology assembler and the Aggregator (main.go:167-186) ----
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

	// --- the bridge (main.go:188-228) -----------------------------------
	n.br, err = bridge.New(snapshotter, n.advertiser, bridge.Config{
		Listen:        cfg.Network.Listen,
		PreferIPv4:    cfg.Network.PreferIPv4,
		TCP:           cfg.Network.TCP,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		NodeLabel:     bi.NodeLabel,
		Discriminator: cfg.Commissioning.Discriminator,
		Clock:         n.cfg.Clock,
	}, n.logger)
	if err != nil {
		return nil, fmt.Errorf("node: bridge: %w", err)
	}
	n.br.SetDeviceTypeValidation(cfg.DeviceTypeValidation)

	// Access control fails closed without a lister; the stored ACL is what
	// a commissioner writes (main.go:230-233).
	n.br.AttachACLLister(n.st)
	// Group messaging and the auxiliary ACL, over the restored group state
	// (main.go:234-246).
	if err := n.groups.Load(ctx); err != nil {
		return nil, fmt.Errorf("node: group state: %w", err)
	}
	n.br.AttachGroupMessaging(n.groups)
	n.br.AttachAuxiliaryACL(n.groups)
	// A bounded trace of the moments that explain a failed pairing
	// (main.go:247-251).
	n.diagRing = diagevent.NewRing(256)
	n.diagRing.SetClock(n.cfg.Clock)
	n.br.AttachDiagnosticEvents(n.diagRing)

	// --- root + Aggregator endpoints (main.go:253-321) ------------------
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
	n.caseIDs = newCaseIdentities(n.logger)
	n.caseIDs.clock = n.cfg.Clock
	n.root, err = buildRoot(ctx, rootDeps{
		cfg: &n.cfg, bootReason: n.bootReason, st: n.st, groups: n.groups, connector: n.br,
		onFabricInstalled:       n.observedFabricInstalled,
		onCommissioningComplete: n.commissioningCompleted,
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
	// The OTA provider and its BDX responder (ADR 0021).
	if err := n.buildOTAProvider(); err != nil {
		return nil, err
	}

	// --- sessions, PASE, CASE (main.go:323-328) -------------------------
	cm := cfg.Commissioning
	n.sec = wireSecurity(n.life, n.br, n.st, n.root, n.caseIDs, paseParams{
		passcode: cm.Passcode, salt: cm.Salt, iterations: cm.PBKDFIterations,
	}, responderSessionParams(cfg.Network.TCP), n.logger)

	// --- the commissioning window (main.go:329-360) ---------------------
	advert, err := n.advertisement()
	if err != nil {
		return nil, err
	}
	n.window = &commissioningHost{
		br: n.br, sessions: n.sec.sessions, root: n.root, logger: n.logger,
		baseline: n.sec.pase, advert: advert, observer: cfg.Observers.OnWindowTransition,
		responderParams: n.sec.responderParams,
	}
	wireCommissioningWindow(n.life, n.window, n.st)

	// --- fabric hooks (main.go:362-414) ---------------------------------
	// The host's observers run after each (observers.go).
	n.root.OperationalCredentials.SetOnFabricRemoved(n.observedFabricRemoved)
	n.root.OperationalCredentials.SetOnFabricUpdated(n.observedFabricUpdated)

	// Event numbers continue across a reboot (main.go:416-426).
	if ceiling, found, cErr := n.st.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); cErr == nil && found {
		n.br.EventLog().SeedNumber(ceiling)
	}
	n.br.EventLog().SetCounterPersistence(func(ceiling uint64) {
		if err := n.st.SetMetadataCounter(context.Background(), store.MetadataKeyEventNumber, ceiling); err != nil {
			n.logger.Warn("eventlog.persist_ceiling", slog.String("err", err.Error()))
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
	ok = true
	return n, nil
}

// openStores resolves cfg.Store: the overrides, or the two stores on DB
// with their schemas applied (persist.go:45-60).
func (n *Node) openStores(ctx context.Context) error {
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

// advertisement is the commissionable record of the node's own window
// (main.go:329-347): CM=1, the configured discriminator, the product
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
// (main.go:263-289).
func (n *Node) fabricInstalled(ctx context.Context, fabricIndex uint8, _, _ uint64, _ []byte) {
	if err := n.caseIDs.load(ctx, n.st, fabricIndex); err != nil {
		n.logger.Warn("case.identity.reload_failed", slog.String("err", err.Error()))
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
// (main.go:362-385): Leave, the OTA providers, the CASE identity and
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
	n.removeFabricBindings(fabricIndex)
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
// (main.go:387-406): UpdateNOC moves the CASE identity and the
// operational record, and ends the fabric's other sessions, each told
// with a CloseSession (Bridge.CloseFabricSessionsExcept).
func (n *Node) fabricUpdated(ctx context.Context, fabricIndex uint8) {
	oldCompressed, oldNode, hadOld := n.caseIDs.announceIdentity(fabricIndex)
	if err := n.caseIDs.load(ctx, n.st, fabricIndex); err != nil {
		n.logger.Warn("case.identity.reload_failed", slog.String("err", err.Error()))
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
	fn := n.cfg.Observers.OnFabricAdded
	if fn == nil {
		return
	}
	info := FabricInfo{FabricIndex: fabricIndex, FabricID: fabricID, NodeID: nodeID, RootPublicKey: append([]byte(nil), rootPublicKey...)}
	if stored, ok := n.fabricInfo(ctx, fabricIndex); ok {
		info.CompressedFabricID = stored.CompressedFabricID
	}
	observe(n.logger, "fabric_added", func() error { return fn(info) })
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
	if fn := n.cfg.Observers.OnFabricRemoved; fn != nil {
		observe(n.logger, "fabric_removed", func() error { return fn(info) })
	}
}

// observedFabricUpdated is the facade's fabricUpdated, then the host's
// OnFabricUpdated with the fabric as the store now holds it.
func (n *Node) observedFabricUpdated(ctx context.Context, fabricIndex uint8) {
	n.fabricUpdated(ctx, fabricIndex)
	if fn := n.cfg.Observers.OnFabricUpdated; fn != nil {
		info, _ := n.fabricInfo(ctx, fabricIndex)
		observe(n.logger, "fabric_updated", func() error { return fn(info) })
	}
}

// commissioningCompleted runs the host's OnCommissioningComplete; the
// root's CommissioningComplete hook calls it after its own body
// (root.go).
func (n *Node) commissioningCompleted(ctx context.Context, fabricIndex uint8) {
	n.ensureOTAProviderACL(ctx)
	if fn := n.cfg.Observers.OnCommissioningComplete; fn != nil {
		observe(n.logger, "commissioning_complete", func() error { return fn(fabricIndex) })
	}
}

// Start brings the node online: the bridge starts, and the daemon's
// post-start sequence runs (main.go:428-489) — the diagnostic-log
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
	case n.started:
		return nil
	}
	if err := n.br.Start(ctx); err != nil {
		return fmt.Errorf("node: bridge start: %w", err)
	}
	n.started = true
	if n.agg != nil {
		n.agg.setStarted()
	}
	n.startGroupSender()
	n.startReannounce()

	n.root.DiagnosticLogs.AttachProvider(ringLogs{n.diagRing})
	// BasicInformation StartUp, then GeneralDiagnostics BootReason, the
	// order matter.js emits them in (main.go:439-444).
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

	// Fabrics of a previous run: their CASE identities, the subscriptions
	// their controllers held, then their operational records — matter.js
	// re-establishes before entering operational mode (main.go:461-489).
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("fabrics.list_failed", slog.String("err", err.Error()))
	}
	stored := len(fabrics)
	loaded := make([]store.FabricRecord, 0, stored)
	for _, fabric := range fabrics {
		if err := n.caseIDs.load(ctx, n.st, fabric.FabricIndex); err != nil {
			n.logger.Warn("case.identity.boot_load_failed", slog.String("err", err.Error()))
			continue
		}
		loaded = append(loaded, fabric)
	}
	n.br.ReestablishFormerSubscriptions(ctx)
	if n.root.OTARequestor != nil {
		if err := n.root.OTARequestor.Start(ctx); err != nil {
			n.logger.Warn("ota.requestor_start", slog.String("err", err.Error()))
		}
	}
	for _, fabric := range loaded {
		n.br.AnnounceFabric(ctx, fabric.CompressedID, fabric.NodeID)
	}
	// The Binding servers register their entries once the node is online
	// and its CASE identities are loaded (BindingManager.ts:65-78: the
	// queue flushes on the node's online event).
	n.startBindings()
	// An uncommissioned node opens its own window (main.go:491-501;
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
			n.logger.Warn("commissioning.own_window", slog.String("err", err.Error()))
		}
	case len(loaded) == 0:
		n.logger.Error("commissioning.no_fabric_loaded",
			slog.Int("stored_fabrics", stored),
			slog.String("hint", "every stored fabric failed to load its CASE identity; the node is not opened for commissioning"))
	}
	return nil
}

// Stop takes the node offline and releases what New and Start began, in
// the daemon's shutdown order (main.go:430-450): the OTA requestor, the
// operational time stored once more, BasicInformation ShutDown (when the
// node was started), the PASE providers, the CASE reaper, the
// subscription engine, the re-announce loop, the bridge and the
// advertiser. Stop is idempotent
// (matter.js Node.stop, Node.ts:193) and safe without Start.
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return nil
	}
	n.stopped = true
	if n.root.OTARequestor != nil {
		n.root.OTARequestor.Stop()
	}
	if n.started {
		if err := storeOperationalTime(ctx, n.st, n.root.GeneralDiagnostics); err != nil {
			n.logger.Warn("gendiag.operational_hours.persist", slog.String("err", err.Error()))
		}
		n.root.BasicInformation.EmitShutDown()
	}
	// Every live binding is removed before the client stack goes
	// (BindingServer.ts:52, :160-163).
	n.bindings.Close()
	if n.client != nil {
		n.client.close(ctx)
	}
	n.sec.pase.Stop()
	n.window.stop()
	n.sec.casep.StopReaper()
	n.sec.subs.Stop()
	n.stopGroupSender()
	if n.bdx != nil {
		n.bdx.Close()
	}
	if n.commissionerControl != nil {
		n.commissionerControl.Close()
	}
	n.stopReannounceLoop()
	err := n.br.Stop(ctx)
	_ = n.advertiser.Close()
	n.cancel()
	n.keeperWG.Wait()
	if err != nil {
		return fmt.Errorf("node: bridge stop: %w", err)
	}
	return nil
}

// Run starts the node, waits for ctx to end and stops it — matter.js
// Node.run (Node.ts:183-186).
func (n *Node) Run(ctx context.Context) error {
	if err := n.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	return n.Stop(stopCtx)
}

// Bridge returns the bridge, for a host that needs a port the facade does
// not cover (ADR 0018 decision 5).
func (n *Node) Bridge() *bridge.Bridge { return n.br }

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
func (n *Node) Sessions() *operational.Manager { return n.sec.sessions }

// Subscriptions returns the subscription manager, for a host's
// diagnostics (subscription.Manager.CountBySession, Active): the
// subscriptions riding on each session. The facade owns its lifecycle;
// a host reads it and leaves closing to the node.
func (n *Node) Subscriptions() *subscription.Manager { return n.sec.subs }

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
func (n *Node) CommissioningWindow() *bridge.CommissioningWindow { return n.br.CommissioningWindow() }

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
	n.mu.Lock()
	started := n.started
	n.mu.Unlock()
	if started && n.window != nil && n.st != nil {
		n.window.reopenIfDecommissioned(ctx, n.st)
	}
}

// Fabrics returns the installed fabrics.
func (n *Node) Fabrics(ctx context.Context) ([]store.FabricRecord, error) {
	return n.st.ListFabrics(ctx)
}

// BootReason returns the GeneralDiagnostics BootReason the node was built
// with — the value [Node.Start]'s EmitBootReason reports. The device layer
// passes it to the default OnOff servers (onoff.Config.BootReason), which
// skip StartUpOnOff after a software update (OnOffServer.ts:36, :326-331).
func (n *Node) BootReason() uint8 { return n.bootReason }

// Store returns the credential store the node runs on.
func (n *Node) Store() *store.Store { return n.st }

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
func (n *Node) Groups() *groups.Manager { return n.groups }

// PairingCodes returns the QR code payload and the manual pairing code of
// the configured passcode and discriminator — matter.js
// `commissioning.pairingCodes` (CommissioningServer.ts:400-410), as the
// daemon prints them (main.go:504-518).
func (n *Node) PairingCodes() (qr, manual string, err error) {
	cm, pd := n.cfg.Commissioning, n.cfg.ProductDescription
	qr, err = setup.QRCode(setup.Payload{
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		Discriminator: cm.Discriminator,
		Passcode:      cm.Passcode,
		DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		return "", "", fmt.Errorf("node: qr payload: %w", err)
	}
	manual, err = setup.ManualCode(cm.Discriminator, cm.Passcode)
	if err != nil {
		return "", "", fmt.Errorf("node: manual code: %w", err)
	}
	return qr, manual, nil
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
