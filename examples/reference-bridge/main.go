// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Command reference-bridge is a runnable Matter bridge assembled only from
// go-fabric's public API.
//
// It bridges a hard-coded fleet of simulated devices — one per surface the
// module serves: lights, a speaker, a fan, a smoke alarm, a pump, a washer,
// a vacuum robot, a thermostat, a blind, a lock, sensors and a button — over
// mDNS, accepts a commissioner over PASE, and serves reads, writes and
// commands over the operational CASE session that follows. It
// exists as the module's second consumer: everything it does, a third-party
// host has to be able to do from the outside, and anything it cannot reach
// without an internal is a defect in the seam rather than in the example.
//
// It is not a product. The identity it advertises is the CSA *test*
// vendor/product pair and the attestation chain is the CSA *test* chain; see
// the README next to this file for what that means.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
)

// CSA test identity. 0xFFF1 is the Connectivity Standards Alliance's test
// vendor block: every commissioner ships the matching test PAA, which is what
// lets this example pair with no vendor-supplied attestation material. It is
// also why nothing built on it may ship — see the README.
const (
	testVendorID  uint16 = 0xFFF1
	testProductID uint16 = 0x8001
)

// bridgeIdentity is the static identity three separate constructors need
// (the bridge config, the endpoint assembler and BasicInformation), kept in
// one place so they cannot disagree.
type bridgeIdentity struct {
	vendorID     uint16
	productID    uint16
	nodeLabel    string
	serialNumber string
}

func main() {
	if err := run(); err != nil {
		slog.Error("reference-bridge exited", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

//nolint:funlen,gocognit,gocyclo // a composition root: one sequential wiring pass, deliberately readable top to bottom
func run() error {
	var (
		dbPath       = flag.String("db", "reference-bridge.db", "SQLite file holding fabrics, credentials and endpoint numbers")
		listen       = flag.String("listen", ":5540", "UDP listen address for Matter traffic")
		label        = flag.String("label", "go-fabric reference bridge", "operator-visible bridge name (BasicInformation.NodeLabel)")
		passcodeFlag = flag.Uint("passcode", 20202021, "Matter setup passcode (8 digits, not trivially guessable)")
		discFlag     = flag.Uint("discriminator", 3840, "12-bit commissioning discriminator")
		iterations   = flag.Int("pbkdf-iterations", 1000, "SPAKE2+ PBKDF2 iteration count")
		salt         = flag.String("pbkdf-salt", "go-fabric-ref-salt", "SPAKE2+ salt (16-32 bytes; change it for anything real)")
		advertise    = flag.Bool("advertise", true, "publish the commissionable mDNS record (false = no discovery, direct address only)")
		logLevel     = flag.String("log-level", "info", "debug, info, warn or error")
		appPipe      = flag.String("app-pipe", "", "TESTING ONLY: named pipe of CHIP-style JSON test commands (button presses, sensor values, …); see control.go")
		enableKey    = flag.String("enable-key", "", "TESTING ONLY: 16-byte hex test enable key that arms GeneralDiagnostics TestEventTrigger; see control.go")
		osHostName   = flag.Bool("mdns-os-hostname", false, "TESTING ONLY: advertise the OS host name as SRV target instead of the MAC-derived one, for a test host whose LAN interface has no IPv6")
	)
	flag.Parse()

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	if *discFlag > setup.MaxDiscriminator {
		return fmt.Errorf("--discriminator %d exceeds the 12-bit maximum %d", *discFlag, setup.MaxDiscriminator)
	}
	discriminator := uint16(*discFlag)
	// setupPasscodeMax is the Matter §5.1.1.6 upper bound; the narrowing
	// below is safe exactly because of this check.
	const setupPasscodeMax = 99999998
	if *passcodeFlag > setupPasscodeMax {
		return fmt.Errorf("--passcode %d exceeds the Matter maximum %d", *passcodeFlag, setupPasscodeMax)
	}
	passcode := uint32(*passcodeFlag)
	if !setup.IsValidSetupPIN(passcode) {
		return fmt.Errorf("--passcode %d is invalid or on the trivially-guessable list", passcode)
	}
	if *iterations < 0 {
		return fmt.Errorf("--pbkdf-iterations %d must be positive", *iterations)
	}

	identity := bridgeIdentity{
		vendorID:  testVendorID,
		productID: testProductID,
		nodeLabel: *label,
		// The serial only has to be stable across restarts; it seeds the
		// rotating device identifier and the derived UniqueID.
		serialNumber: "GOFABRIC-REF-0001",
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- persistence ---------------------------------------------------
	db, err := openDB(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	credentials := store.New(db)
	// This host's source identities are plain strings (see the fleet), so
	// the default endpoint.StringKey decoding is the right one. A host with
	// a composite key type must pass sqlitestore.WithKeyDecoder — its
	// documentation says what silently breaks otherwise.
	endpointStore := sqlitestore.New(db)
	// The node's group state: key sets, GroupKeyMap and group table of
	// every fabric. GroupKeyManagement writes it, every Groups server the
	// assembler mounts reads and changes it, and the bridge receives group
	// messages through it. One instance for all three.
	groupState, err := groups.NewManager(credentials, logger)
	if err != nil {
		return fmt.Errorf("group state: %w", err)
	}

	// --- the fleet and its topology assembler --------------------------
	// A NodeLabel a controller writes on a bridged endpoint is kept in
	// the settings table and handed back to the assembler on the next
	// boot (fleet.snapshotter), as matter.js persists a written nodeLabel.
	labels := persistedLabels{st: credentials, logger: logger}
	assemblerCfg := endpoint.Config{
		VendorID:           identity.vendorID,
		ProductID:          identity.productID,
		NodeLabel:          identity.nodeLabel,
		VendorName:         exampleVendorName,
		Groups:             groupState,
		OnNodeLabelWritten: labels.store,
		Scenes:             labels,
	}
	devices, err := newFleet(endpointStore, assemblerCfg, logger)
	if err != nil {
		return err
	}
	devices.labels = labels.load
	devices.configVersions = labels.configVersion
	labels.restoreBlind(ctx, devices.blind)

	// --- the bridge ----------------------------------------------------
	var advertiser mdns.Advertiser = mdns.NewNoop()
	if *advertise {
		zc := mdns.NewZeroconf()
		if *osHostName {
			if h, err := os.Hostname(); err == nil && h != "" {
				zc.HostName = strings.TrimSuffix(h, ".local")
			}
		}
		// The subtype side-car is NOT optional for discovery, only for the
		// generic browse: without it `_matterc._udp` carries the instance
		// but `_L<discriminator>._sub._matterc._udp` answers nothing, and
		// every commissioner that filters by discriminator — chip-tool's
		// `pairing onnetwork-long`, Apple Home, Google Home — sees no
		// device against a perfectly valid QR code. Zeroconf publishes the
		// subtype PTRs only when a responder is attached.
		responder, rErr := mdns.NewSubtypeResponder(logger)
		if rErr != nil {
			logger.Warn("mdns.subtype_responder", slog.String("err", rErr.Error()),
				slog.String("hint", "discriminator-filtered browse will find nothing"))
		} else {
			responder.Start(ctx)
			zc.AttachSubtypeResponder(responder)
			defer func() { _ = responder.Close() }()
		}
		advertiser = zc
	}
	br, err := matterbridge.New(devices.snapshotter, advertiser, matterbridge.Config{
		Listen:        *listen,
		VendorID:      identity.vendorID,
		ProductID:     identity.productID,
		NodeLabel:     identity.nodeLabel,
		Discriminator: discriminator,
	}, logger)
	if err != nil {
		return fmt.Errorf("bridge: %w", err)
	}

	// Access control fails CLOSED: leaving the lister nil denies every
	// operational request rather than allowing them. The stored ACL is what
	// a commissioner writes during AddNOC, so this is the production wiring.
	br.AttachACLLister(credentials)
	// Group messages: authenticated through the node's group state, routed
	// to the member endpoints, and received on the multicast address of
	// every group with a member endpoint. Restore the persisted groups first
	// so their addresses are joined when the bridge starts.
	if err := groupState.Load(ctx); err != nil {
		return fmt.Errorf("group state: %w", err)
	}
	br.AttachGroupMessaging(groupState)
	// The Groupcast server (root, below) synthesises auxiliary access
	// control entries; the bridge enforces them from the same state.
	br.AttachAuxiliaryACL(groupState)
	// A bounded trace of the moments that explain a failed pairing. Attached
	// before Start because the first of those moments is the first
	// commissioner datagram.
	diagRing := diagevent.NewRing(256)
	br.AttachDiagnosticEvents(diagRing)

	// --- root + aggregator endpoints -----------------------------------
	caseIDs := newCaseIdentities(logger)
	chain, err := attestation.BuildTestChain(identity.vendorID, identity.productID)
	if err != nil {
		return fmt.Errorf("attestation chain: %w", err)
	}
	rootServers, refs, err := buildRootClusters(identity, credentials, groupState, chain,
		func(hookCtx context.Context, fabricIndex uint8, _, _ uint64, _ []byte) {
			if err := caseIDs.load(hookCtx, credentials, fabricIndex); err != nil {
				logger.Warn("case.identity.reload_failed", slog.String("err", err.Error()))
				return
			}
			// Publishing the operational record here is not an optimisation
			// of the boot-time publish below -- it is the only publish a
			// freshly commissioned fabric ever gets. The commissioner
			// finishes AddNOC over PASE and immediately resolves
			// `<compressed>-<node>._matter._tcp` to open its first CASE
			// session; on a first pairing there was no such fabric at boot,
			// so nothing has advertised it. Without this the pairing gets
			// through PASE, installs the fabric, and then times out in
			// operational discovery -- a failure that reads like a network
			// fault and is not one.
			compressedID, nodeID, ok := caseIDs.announceIdentity(fabricIndex)
			if !ok {
				logger.Warn("case.identity.announce_skipped",
					slog.Int("fabric_index", int(fabricIndex)))
				return
			}
			br.AnnounceFabric(hookCtx, compressedID, nodeID)
		})
	if err != nil {
		return err
	}
	labels.restoreRoot(ctx, refs.basicInfo)
	br.AttachRootClusters(rootServers)
	devices.configChange = func() error {
		topo := br.Topology()
		if topo == nil {
			return errors.New("no topology yet")
		}
		seen := map[string]bool{}
		for _, ep := range topo.Bridged() {
			if seen[ep.Scope+"|"+ep.DeviceAddress] {
				continue
			}
			seen[ep.Scope+"|"+ep.DeviceAddress] = true
			labels.storeConfigVersions(topo, br.IncreaseConfigurationVersion(ep.Scope, ep.DeviceAddress))
		}
		return nil
	}

	aggregatorServers, err := buildAggregatorClusters(ctx, credentials)
	if err != nil {
		return err
	}
	br.AttachAggregatorClusters(aggregatorServers)

	// --- sessions, PASE, CASE ------------------------------------------
	sec := wireSecurity(ctx, br, credentials, refs, caseIDs, paseParams{
		passcode:   passcode,
		salt:       []byte(*salt),
		iterations: *iterations,
	}, logger)
	// The commissionable advertisement: published with the configured
	// passcode's discriminator (CM=1) while the node's own window is open —
	// from the end of start-up of an uncommissioned node until it is
	// commissioned — and replaced by the enhanced window's own (CM=2) while
	// an admin has one open.
	var instanceID [8]byte
	if _, err := rand.Read(instanceID[:]); err != nil {
		return fmt.Errorf("commissioning instance id: %w", err)
	}
	advert := matterbridge.CommissioningAdvertisement{
		InstanceID:    instanceID,
		Discriminator: discriminator,
		VendorID:      identity.vendorID,
		ProductID:     identity.productID,
		NodeLabel:     identity.nodeLabel,
		RotatingID: mdns.GenerateRotatingID(
			mdns.DeriveUniqueIDFromIdentity(identity.vendorID, identity.productID, identity.serialNumber, identity.nodeLabel), 0,
		),
		CommissioningMode: 1, // §4.3.1.4 CM=1: standard commissioning window
		DeviceTypeID:      deviceTypeRootNode,
	}
	// Multi-admin: AdministratorCommissioning's window, wired to this
	// daemon's PASE provider and advertisement (commissioning.go).
	commissioning := &commissioningHost{
		br: br, sessions: sec.sessions, refs: refs, logger: logger,
		baseline: sec.pase, advert: advert,
	}
	wireCommissioningWindow(ctx, commissioning, credentials)

	// A removed fabric takes its persisted subscriptions with it.
	// Its groups go with it too: EmitFabricRemoved hands the fabric to the
	// attached group messaging.
	refs.opCreds.SetOnFabricRemoved(func(ctx context.Context, fabricIndex uint8) {
		// BasicInformation Leave, as matter.js BasicInformationServer
		// emits it when a fabric goes.
		refs.basicInfo.EmitLeave(fabricIndex)
		// Its CASE identity and operational record go too — a rolled-back
		// commissioning included, which no RemoveFabric announces.
		if compressedID, nodeID, ok := caseIDs.forget(fabricIndex); ok {
			br.WithdrawFabric(ctx, compressedID, nodeID)
		}
		// The fabric's sessions end once the NOCResponse is out.
		br.EmitFabricRemovedContext(ctx, fabricIndex)
		// The last fabric gone, the node is commissionable again: matter.js
		// CommissioningServer resets a decommissioned node, which then
		// opens its own window as at a first start.
		commissioning.reopenIfDecommissioned(ctx, credentials)
	})

	// UpdateNOC rewrites the fabric's operational identity, possibly with a
	// new node id: the CASE responder must answer as the new identity, the
	// operational record must move to the new instance name at once (a
	// controller reaches the node under it before CommissioningComplete),
	// and the fabric's other sessions end. matter.js FabricManager's
	// replaced event drives the same through DeviceAdvertiser and
	// SessionManager.
	refs.opCreds.SetOnFabricUpdated(func(hookCtx context.Context, fabricIndex uint8) {
		oldCompressed, oldNode, hadOld := caseIDs.announceIdentity(fabricIndex)
		if err := caseIDs.load(hookCtx, credentials, fabricIndex); err != nil {
			logger.Warn("case.identity.reload_failed", slog.String("err", err.Error()))
			return
		}
		if newCompressed, newNode, ok := caseIDs.announceIdentity(fabricIndex); ok && (!hadOld || newCompressed != oldCompressed || newNode != oldNode) {
			if hadOld {
				br.WithdrawFabric(hookCtx, oldCompressed, oldNode)
			}
			br.AnnounceFabric(hookCtx, newCompressed, newNode)
		}
		sec.sessions.CloseFabricExcept(fabricIndex, mattercore.InvokeSessionIDFromContext(hookCtx))
	})

	// Event numbers must not restart at zero across a reboot: a controller
	// filters event reads on the last number it saw, so a reset makes it
	// drop every event the bridge emits afterwards.
	if ceiling, ok, cErr := credentials.GetMetadataCounter(ctx, store.MetadataKeyEventNumber); cErr == nil && ok {
		br.EventLog().SeedNumber(ceiling)
	}
	br.EventLog().SetCounterPersistence(func(ceiling uint64) {
		if err := credentials.SetMetadataCounter(context.Background(), store.MetadataKeyEventNumber, ceiling); err != nil {
			logger.Warn("eventlog.persist_ceiling", slog.String("err", err.Error()))
		}
	}, 0)

	if err := br.Start(ctx); err != nil {
		return fmt.Errorf("bridge start: %w", err)
	}
	// The node's lifecycle events, in the order matter.js emits them when
	// a node comes online: BasicInformation StartUp, then GeneralDiagnostics
	// BootReason (BasicInformationServer.ts / GeneralDiagnosticsServer.ts).
	// A controller's wildcard event subscription is primed with these; a
	// node that never emits them answers it with nothing at all, which the
	// CHIP device-composition checker (TC-IDM-10.1) fails.
	if *enableKey != "" {
		key, err := hex.DecodeString(*enableKey)
		if err != nil {
			return fmt.Errorf("--enable-key: %w", err)
		}
		if err := refs.genDiag.EnableTestEventTriggers(key, devices.testEventTrigger); err != nil {
			return fmt.Errorf("--enable-key: %w", err)
		}
	}
	refs.diagLogs.AttachProvider(ringLogs{diagRing})
	refs.basicInfo.EmitStartUp()
	refs.genDiag.EmitBootReason()
	go keepOperationalTime(ctx, credentials, refs.genDiag, time.Minute, logger)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := storeOperationalTime(stopCtx, credentials, refs.genDiag); err != nil {
			logger.Warn("gendiag.operational_hours.persist", slog.String("err", err.Error()))
		}
		refs.basicInfo.EmitShutDown()
		sec.pase.Stop()
		commissioning.stop()
		sec.casep.StopReaper()
		sec.subs.Stop()
		if err := br.Stop(stopCtx); err != nil {
			logger.Warn("bridge stop", slog.String("err", err.Error()))
		}
		_ = advertiser.Close()
	}()

	// The two PartsList providers are what let a controller walk root →
	// aggregator → bridged devices.
	if err := attachPartsListProviders(br); err != nil {
		return err
	}

	// Fabrics installed in a previous run: rebuild each one's CASE identity
	// and re-publish its operational record, so a controller that paired
	// before this restart reconnects instead of being told to pair again.
	fabrics := listFabrics(ctx, credentials, logger)
	loaded := fabrics[:0]
	for _, fabric := range fabrics {
		if err := caseIDs.load(ctx, credentials, fabric.FabricIndex); err != nil {
			logger.Warn("case.identity.boot_load_failed", slog.String("err", err.Error()))
			continue
		}
		loaded = append(loaded, fabric)
	}
	// With the identities back, resume the subscriptions the controllers
	// held before the restart, under their old ids — before announcing, as
	// matter.js re-establishes ahead of entering operational mode. Bounded
	// at two seconds per controller; a controller that cannot be reached
	// recovers on its own, as it would have without this.
	br.ReestablishFormerSubscriptions(ctx)
	for _, fabric := range loaded {
		br.AnnounceFabric(ctx, fabric.CompressedID, fabric.NodeID)
	}

	// --- commissionable advertisement + pairing information -------------
	// An uncommissioned node opens its own commissioning window (48 h,
	// CM=1, the configured passcode); a commissioned one is not
	// commissionable until an administrator opens a window. Mirrors
	// matter.js CommissioningServer's start-up: enterOperationalMode when
	// commissioned, enterCommissionableMode otherwise.
	if len(loaded) == 0 {
		if err := br.CommissioningWindow().OpenOwnWindow(ctx, 0); err != nil {
			logger.Warn("commissioning.own_window", slog.String("err", err.Error()))
		}
	}

	if err := printPairingInfo(br, identity, discriminator, passcode); err != nil {
		return err
	}
	printTopology(br)
	if *appPipe != "" {
		if err := serveAppPipe(ctx, devices, *appPipe, logger); err != nil {
			return err
		}
	}

	<-ctx.Done()
	logger.Info("shutting down")
	return nil
}

// printPairingInfo writes the onboarding payload to stdout. It goes to stdout
// rather than the log because it is the one thing an operator has to copy.
func printPairingInfo(br *matterbridge.Bridge, identity bridgeIdentity, discriminator uint16, passcode uint32) error {
	qr, err := setup.QRCode(setup.Payload{
		VendorID:      identity.vendorID,
		ProductID:     identity.productID,
		Discriminator: discriminator,
		Passcode:      passcode,
		DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		return fmt.Errorf("qr payload: %w", err)
	}
	manual, err := setup.ManualCode(discriminator, passcode)
	if err != nil {
		return fmt.Errorf("manual code: %w", err)
	}

	topology := br.Topology()
	bridged := 0
	if topology != nil {
		bridged = len(topology.Bridged())
	}

	fmt.Printf(`
  go-fabric reference bridge
  --------------------------------------------------------------
  listening on     %s
  bridged devices  %d
  vendor/product   0x%04X / 0x%04X  (CSA TEST identity — not shippable)

  discriminator    %d
  setup passcode   %08d
  manual code      %s
  QR payload       %s

  pair with, e.g.:
    chip-tool pairing onnetwork-long 1 %d %d
  --------------------------------------------------------------

`, br.LocalAddr(), bridged, identity.vendorID, identity.productID,
		discriminator, passcode, manual, qr, passcode, discriminator)
	return nil
}

// listFabrics reads the persisted fabric set, logging rather than failing:
// an unreadable fabric table costs re-pairing, not startup.
func listFabrics(ctx context.Context, credentials *store.Store, logger *slog.Logger) []store.FabricRecord {
	fabrics, err := credentials.ListFabrics(ctx)
	if err != nil {
		logger.Warn("fabrics.list_failed", slog.String("err", err.Error()))
		return nil
	}
	return fabrics
}

// printTopology writes the assembled endpoints and their device types to
// stdout after the banner — what a test driver reads to point a case at
// the endpoint that serves its cluster, without a controller of its own.
// Like the banner it is for an operator's eyes too.
func printTopology(br *matterbridge.Bridge) {
	topology := br.Topology()
	if topology == nil {
		return
	}
	var b bytes.Buffer
	b.WriteString("  topology\n")
	for _, id := range endpointIDs(br, func(*endpoint.Endpoint) bool { return true }) {
		ep := topology.FindByID(id)
		if ep == nil {
			continue
		}
		fmt.Fprintf(&b, "    endpoint %3d  device types 0x%04X\n", id, ep.DeviceType)
	}
	b.WriteString("  topology end\n\n")
	fmt.Print(b.String())
}
