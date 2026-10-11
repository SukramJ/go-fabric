// SPDX-License-Identifier: MIT
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
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
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
	persisted  persisted

	// life bounds the goroutines New and Start begin (subscription
	// engine, reapers, the operational-time keeper); Stop cancels it.
	life     context.Context //nolint:containedctx // the node's lifetime, cancelled by Stop
	cancel   context.CancelFunc
	keeperWG sync.WaitGroup

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
	n := &Node{cfg: cfg, logger: cfg.Logger, advertiser: cfg.Advertiser}
	n.life, n.cancel = context.WithCancel(ctx)
	ok := false
	defer func() {
		if !ok {
			n.cancel()
		}
	}()

	// --- persistence (main.go:146-165, persist.go openDB) ---------------
	if err := n.openStores(ctx); err != nil {
		return nil, err
	}
	n.cfg.Store.Credentials = n.st
	n.persisted = persisted{st: n.st, logger: n.logger}
	var err error
	if n.groups, err = groups.NewManager(n.st, n.logger); err != nil {
		return nil, fmt.Errorf("node: group state: %w", err)
	}

	// --- the topology assembler and the Aggregator (main.go:167-186) ----
	pd, bi := cfg.ProductDescription, cfg.BasicInformation
	n.assembler, err = endpoint.New(n.cfg.Store.Endpoints, endpoint.Config{
		VendorID:           pd.VendorID,
		ProductID:          pd.ProductID,
		NodeLabel:          bi.NodeLabel,
		VendorName:         bi.VendorName,
		Groups:             n.groups,
		OnNodeLabelWritten: n.persisted.storeLabel,
		Scenes:             n.persisted,
	}, n.logger)
	if err != nil {
		return nil, fmt.Errorf("node: endpoint assembler: %w", err)
	}
	n.agg = &Aggregator{n: n}

	// --- the bridge (main.go:188-228) -----------------------------------
	n.br, err = bridge.New(n.agg.snapshot, n.advertiser, bridge.Config{
		Listen:        cfg.Network.Listen,
		PreferIPv4:    cfg.Network.PreferIPv4,
		TCP:           cfg.Network.TCP,
		VendorID:      pd.VendorID,
		ProductID:     pd.ProductID,
		NodeLabel:     bi.NodeLabel,
		Discriminator: cfg.Commissioning.Discriminator,
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
	n.br.AttachDiagnosticEvents(n.diagRing)

	// --- root + Aggregator endpoints (main.go:253-321) ------------------
	n.caseIDs = newCaseIdentities(n.logger)
	n.root, err = buildRoot(ctx, rootDeps{
		cfg: &n.cfg, st: n.st, groups: n.groups, connector: n.br,
		onFabricInstalled: n.fabricInstalled,
	})
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	n.persisted.restoreRoot(ctx, n.root.BasicInformation)
	n.br.AttachRootClusters(n.root.Servers())
	aggregatorServers, err := buildAggregatorClusters(ctx, n.st, cfg.AggregatorServers)
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	n.br.AttachAggregatorClusters(aggregatorServers)

	// --- sessions, PASE, CASE (main.go:323-328) -------------------------
	cm := cfg.Commissioning
	n.sec = wireSecurity(n.life, n.br, n.st, n.root, n.caseIDs, paseParams{
		passcode: cm.Passcode, salt: cm.Salt, iterations: cm.PBKDFIterations,
	}, n.logger)

	// --- the commissioning window (main.go:329-360) ---------------------
	advert, err := n.advertisement()
	if err != nil {
		return nil, err
	}
	n.window = &commissioningHost{
		br: n.br, sessions: n.sec.sessions, root: n.root, logger: n.logger,
		baseline: n.sec.pase, advert: advert,
	}
	wireCommissioningWindow(n.life, n.window, n.st)

	// --- fabric hooks (main.go:362-414) ---------------------------------
	n.root.OperationalCredentials.SetOnFabricRemoved(n.fabricRemoved)
	n.root.OperationalCredentials.SetOnFabricUpdated(n.fabricUpdated)

	// Event numbers continue across a reboot (main.go:416-426).
	if ceiling, found, cErr := n.st.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); cErr == nil && found {
		n.br.EventLog().SeedNumber(ceiling)
	}
	n.br.EventLog().SetCounterPersistence(func(ceiling uint64) {
		if err := n.st.SetMetadataCounter(context.Background(), store.MetadataKeyEventNumber, ceiling); err != nil {
			n.logger.Warn("eventlog.persist_ceiling", slog.String("err", err.Error()))
		}
	}, 0)
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
	n.window.reopenIfDecommissioned(ctx, n.st)
}

// fabricUpdated is OperationalCredentials' OnFabricUpdated
// (main.go:387-406): UpdateNOC moves the CASE identity and the
// operational record, and ends the fabric's other sessions.
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
	n.sec.sessions.CloseFabricExcept(fabricIndex, mattercore.InvokeSessionIDFromContext(ctx))
}

// Start brings the node online: the bridge starts, and the daemon's
// post-start sequence runs (main.go:428-489) — the diagnostic-log
// provider, the StartUp and BootReason events, the operational-time
// keeper, the PartsList providers, the CASE identities of the fabrics a
// previous run installed, the re-establishment of their subscriptions,
// the OTA requestor, their operational records, and the node's own
// commissioning window when no fabric exists. Start is idempotent, as
// matter.js Node.start (Node.ts:113-131); after Stop it is [ErrStopped].
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
	n.agg.setStarted()

	n.root.DiagnosticLogs.AttachProvider(ringLogs{n.diagRing})
	// BasicInformation StartUp, then GeneralDiagnostics BootReason, the
	// order matter.js emits them in (main.go:439-444).
	n.root.BasicInformation.EmitStartUp()
	n.root.GeneralDiagnostics.EmitBootReason()
	n.keeperWG.Add(1)
	go func() {
		defer n.keeperWG.Done()
		keepOperationalTime(n.life, n.st, n.root.GeneralDiagnostics, operationalTimeInterval, n.logger)
	}()

	if err := attachPartsListProviders(n.br); err != nil {
		return err
	}

	// Fabrics of a previous run: their CASE identities, the subscriptions
	// their controllers held, then their operational records — matter.js
	// re-establishes before entering operational mode (main.go:461-489).
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("fabrics.list_failed", slog.String("err", err.Error()))
	}
	loaded := fabrics[:0]
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
	// An uncommissioned node opens its own window (main.go:491-501;
	// matter.js CommissioningServer enterCommissionableMode).
	if len(loaded) == 0 {
		if err := n.br.CommissioningWindow().OpenOwnWindow(ctx, 0); err != nil {
			n.logger.Warn("commissioning.own_window", slog.String("err", err.Error()))
		}
	}
	return nil
}

// Stop takes the node offline and releases what New and Start began, in
// the daemon's shutdown order (main.go:430-450): the OTA requestor, the
// operational time stored once more, BasicInformation ShutDown (when the
// node was started), the PASE providers, the CASE reaper, the
// subscription engine, the bridge and the advertiser. Stop is idempotent
// (matter.js Node.stop, Node.ts:188) and safe without Start.
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
	n.sec.pase.Stop()
	n.window.stop()
	n.sec.casep.StopReaper()
	n.sec.subs.Stop()
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
// Node.run (Node.ts:178-181).
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

// Aggregator returns the Aggregator endpoint's bridged-device set.
func (n *Node) Aggregator() *Aggregator { return n.agg }

// CommissioningWindow returns the commissioning window.
func (n *Node) CommissioningWindow() *bridge.CommissioningWindow { return n.br.CommissioningWindow() }

// Fabrics returns the installed fabrics.
func (n *Node) Fabrics(ctx context.Context) ([]store.FabricRecord, error) {
	return n.st.ListFabrics(ctx)
}

// Store returns the credential store the node runs on.
func (n *Node) Store() *store.Store { return n.st }

// PairingCodes returns the QR code payload and the manual pairing code of
// the configured passcode and discriminator — matter.js
// `commissioning.pairingCodes` (CommissioningServer.ts:370-380), as the
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
