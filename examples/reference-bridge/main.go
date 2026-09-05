// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Command reference-bridge is a runnable Matter bridge assembled only from
// go-fabric's public API.
//
// It bridges a small hard-coded fleet — one on/off light and one temperature
// sensor — over mDNS, accepts a commissioner over PASE, and serves reads,
// writes and commands over the operational CASE session that follows. It
// exists as the module's second consumer: everything it does, a third-party
// host has to be able to do from the outside, and anything it cannot reach
// without an internal is a defect in the seam rather than in the example.
//
// It is not a product. The identity it advertises is the CSA *test*
// vendor/product pair and the attestation chain is the CSA *test* chain; see
// the README next to this file for what that means.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
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

//nolint:funlen // a composition root: one sequential wiring pass, deliberately readable top to bottom
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
	endpointStore := newEndpointStore(db)

	// --- the fleet and its topology assembler --------------------------
	assemblerCfg := endpoint.Config{
		VendorID:  identity.vendorID,
		ProductID: identity.productID,
		NodeLabel: identity.nodeLabel,
	}
	devices, err := newFleet(endpointStore, assemblerCfg, logger)
	if err != nil {
		return err
	}

	// --- the bridge ----------------------------------------------------
	var advertiser mdns.Advertiser = mdns.NewNoop()
	if *advertise {
		zc := mdns.NewZeroconf()
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
	br, err := matterbridge.New(endpointStore, devices.snapshotter, advertiser, matterbridge.Config{
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
	// A bounded trace of the moments that explain a failed pairing. Attached
	// before Start because the first of those moments is the first
	// commissioner datagram.
	br.AttachDiagnosticEvents(diagevent.NewRing(256))

	// --- root + aggregator endpoints -----------------------------------
	caseIDs := newCaseIdentities(logger)
	chain, err := attestation.BuildTestChain(identity.vendorID, identity.productID)
	if err != nil {
		return fmt.Errorf("attestation chain: %w", err)
	}
	rootServers, refs, err := buildRootClusters(identity, credentials, chain,
		func(hookCtx context.Context, fabricIndex uint8, _, _ uint64, _ []byte) {
			if err := caseIDs.load(hookCtx, credentials, fabricIndex); err != nil {
				logger.Warn("case.identity.reload_failed", slog.String("err", err.Error()))
			}
		})
	if err != nil {
		return err
	}
	br.AttachRootClusters(rootServers)

	aggregatorServers, err := buildAggregatorClusters()
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
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sec.pase.Stop()
		sec.casep.StopReaper()
		sec.subs.Stop()
		if err := br.Stop(stopCtx); err != nil {
			logger.Warn("bridge stop", slog.String("err", err.Error()))
		}
		_ = advertiser.Close()
	}()

	// The two PartsList providers are what let a controller walk root →
	// aggregator → bridged devices. They are attached after the cluster sets
	// because they install onto the Descriptor inside each set.
	if !br.AttachRootPartsListProvider(func() []uint16 { return []uint16{1} }) {
		return errors.New("root Descriptor missing: PartsList provider could not be attached")
	}
	if !br.AttachAggregatorPartsListProvider(func() []uint16 {
		topology := br.Topology()
		if topology == nil {
			return nil
		}
		ids := make([]uint16, 0, len(topology.Bridged()))
		for _, ep := range topology.Bridged() {
			ids = append(ids, ep.ID)
		}
		return ids
	}) {
		return errors.New("aggregator Descriptor missing: PartsList provider could not be attached")
	}

	// Fabrics installed in a previous run: rebuild each one's CASE identity
	// and re-publish its operational record, so a controller that paired
	// before this restart reconnects instead of being told to pair again.
	for _, fabric := range listFabrics(ctx, credentials, logger) {
		if err := caseIDs.load(ctx, credentials, fabric.FabricIndex); err != nil {
			logger.Warn("case.identity.boot_load_failed", slog.String("err", err.Error()))
			continue
		}
		br.AnnounceFabric(ctx, fabric.CompressedID, fabric.NodeID)
	}

	// --- commissionable advertisement + pairing information -------------
	var instanceID [8]byte
	if _, err := rand.Read(instanceID[:]); err != nil {
		return fmt.Errorf("commissioning instance id: %w", err)
	}
	rotatingID := mdns.GenerateRotatingID(
		mdns.DeriveUniqueIDFromIdentity(identity.vendorID, identity.productID, identity.serialNumber, identity.nodeLabel), 0,
	)
	if err := br.AnnounceCommissioning(ctx, matterbridge.CommissioningAdvertisement{
		InstanceID:        instanceID,
		Discriminator:     discriminator,
		VendorID:          identity.vendorID,
		ProductID:         identity.productID,
		NodeLabel:         identity.nodeLabel,
		RotatingID:        rotatingID,
		CommissioningMode: 1, // §4.3.1.4 CM=1: standard commissioning window
		DeviceTypeID:      deviceTypeRootNode,
	}); err != nil {
		logger.Warn("mdns.commissioning_announce", slog.String("err", err.Error()))
	}

	if err := printPairingInfo(br, identity, discriminator, passcode); err != nil {
		return err
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
