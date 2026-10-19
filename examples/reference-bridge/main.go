// SPDX-License-Identifier: Apache-2.0
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
// It is built the way the README points a host at (ADR 0018): one
// node.Config, node.New, one device.New<Type> call per device added to the
// node's Aggregator, and Run. The node performs the bring-up — stores,
// root and Aggregator endpoints, sessions, PASE and CASE, the commissioning
// window, the fabric hooks — in the order bridge/bridge.go documents. What
// stays here is the host's own: its device models and the servers whose
// rules need a host port (fleet*.go), the out-of-band test controls
// (control.go), the OTA applier (ota.go), the mDNS responder and the
// operator's banner.
//
// It is not a product. The identity it advertises is the CSA *test*
// vendor/product pair and the attestation chain is the CSA *test* chain; see
// the README next to this file for what that means.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// CSA test identity. 0xFFF1 is the Connectivity Standards Alliance's test
// vendor block: every commissioner ships the matching test PAA, which is what
// lets this example pair with no vendor-supplied attestation material. It is
// also why nothing built on it may ship — see the README.
const (
	testVendorID  uint16 = 0xFFF1
	testProductID uint16 = 0x8001
)

// serialNumber only has to be stable across restarts; it seeds the rotating
// device identifier and the derived UniqueID.
const serialNumber = "GOFABRIC-REF-0001"

// exampleVendorName is the node's BasicInformation VendorName. The
// assembler serves it as the BridgedDeviceBasicInformation VendorName of
// every bridged device (endpoint.Config.VendorName): the demo devices have
// no manufacturer of their own, which a host with real devices would set
// per device through device.Options.Vendor.
const exampleVendorName = "go-fabric example"

func main() {
	if err := run(); err != nil {
		slog.Error("reference-bridge exited", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

// flags are the daemon's command line.
type flags struct {
	dbPath, listen, tcp, label, salt, logLevel string
	appPipe, enableKey, otaDir, dcl            string
	attestationDir                             string
	passcode, discriminator                    uint
	iterations                                 int
	advertise, osHostName, otaHarness, wifi    bool
	wifiSim                                    bool
	otaProvider, media                         bool
}

func parseFlags() flags {
	var f flags
	flag.StringVar(&f.dbPath, "db", "reference-bridge.db", "SQLite file holding fabrics, credentials and endpoint numbers")
	flag.StringVar(&f.listen, "listen", ":5540", "UDP listen address for Matter traffic")
	flag.StringVar(&f.tcp, "tcp", "true", "Matter TCP: true (accept and open connections), false, or incoming / outgoing comma-separated; incoming listens on the UDP port, both are advertised in the operational T key")
	flag.StringVar(&f.label, "label", "go-fabric reference bridge", "operator-visible bridge name (BasicInformation.NodeLabel)")
	flag.UintVar(&f.passcode, "passcode", 20202021, "Matter setup passcode (8 digits, not trivially guessable)")
	flag.UintVar(&f.discriminator, "discriminator", 3840, "12-bit commissioning discriminator")
	flag.IntVar(&f.iterations, "pbkdf-iterations", 1000, "SPAKE2+ PBKDF2 iteration count")
	flag.StringVar(&f.salt, "pbkdf-salt", "go-fabric-ref-salt", "SPAKE2+ salt (16-32 bytes; change it for anything real)")
	flag.BoolVar(&f.advertise, "advertise", true, "publish the commissionable mDNS record (false = no discovery, direct address only)")
	flag.StringVar(&f.logLevel, "log-level", "info", "debug, info, warn or error")
	flag.StringVar(&f.appPipe, "app-pipe", "", "TESTING ONLY: named pipe of CHIP-style JSON test commands (button presses, sensor values, …); see control.go")
	flag.StringVar(&f.enableKey, "enable-key", "", "TESTING ONLY: 16-byte hex test enable key that arms GeneralDiagnostics TestEventTrigger; see control.go")
	flag.BoolVar(&f.osHostName, "mdns-os-hostname", false, "TESTING ONLY: advertise the OS host name as SRV target instead of the MAC-derived one, for a test host whose LAN interface has no IPv6")
	flag.StringVar(&f.otaDir, "ota-dir", "", "directory OTA images are downloaded to (default: an \"ota\" directory beside --db)")
	flag.BoolVar(&f.otaHarness, "ota-harness", false, "TESTING ONLY: apply a downloaded OTA image in place — report its version as running without a restart, as the CHIP SU cases read it; see ota.go")
	flag.BoolVar(&f.otaProvider, "ota-provider", false, "TESTING ONLY: serve one embedded test OTA image (software version 2 of the test vendor/product) from an OtaProvider endpoint, consent granted unless the app pipe withholds it; see ota_provider.go")
	flag.StringVar(&f.attestationDir, "attestation-dir", "", "directory with a vendor attestation chain — dac.pem, dac-key.pem, pai.pem, cd.der (PEM or DER each, the key PKCS#8 or SEC1) — served instead of the CSA test chain; see attestation.go")
	flag.StringVar(&f.dcl, "dcl", "off", "prod, testnet or off: look the --ota-provider image up in the CSA Distributed Compliance Ledger (sends the requestor's vendor/product id and the offered version to the ledger); see dcl.go")
	flag.BoolVar(&f.wifi, "wifi", false, "TESTING ONLY: serve WiFiNetworkDiagnostics on the root from a simulated Wi-Fi link (fixed values, counters the app pipe steps), stating the RootNode condition WiFi; see wifi.go")
	flag.BoolVar(&f.wifiSim, "wifi-sim", false, "TESTING ONLY: NetworkCommissioning with the Wi-Fi feature instead of Ethernet, over a simulated driver that connects to any SSID after a short delay and stays on the Ethernet address; see wifisim.go")
	flag.BoolVar(&f.media, "media", false, "TESTING ONLY: add a simulated video player (CastingVideoPlayer) and content app (ContentApp) after chip's tv-app, for the media certification families; see media.go")
	flag.Parse()
	return f
}

// checkFlags applies the checks whose message names the flag; the node's
// Config.Validate holds the same ranges again.
func checkFlags(f flags) (passcode uint32, discriminator uint16, err error) {
	if f.discriminator > setup.MaxDiscriminator {
		return 0, 0, fmt.Errorf("--discriminator %d exceeds the 12-bit maximum %d", f.discriminator, setup.MaxDiscriminator)
	}
	// setupPasscodeMax is the Matter §5.1.1.6 upper bound; the narrowing
	// below is safe exactly because of this check.
	const setupPasscodeMax = 99999998
	if f.passcode > setupPasscodeMax {
		return 0, 0, fmt.Errorf("--passcode %d exceeds the Matter maximum %d", f.passcode, setupPasscodeMax)
	}
	passcode = uint32(f.passcode)
	if !setup.IsValidSetupPIN(passcode) {
		return 0, 0, fmt.Errorf("--passcode %d is invalid or on the trivially-guessable list", passcode)
	}
	if f.iterations < 0 {
		return 0, 0, fmt.Errorf("--pbkdf-iterations %d must be positive", f.iterations)
	}
	if _, err := dclClient(f.dcl); err != nil {
		return 0, 0, err
	}
	return passcode, uint16(f.discriminator), nil
}

func run() error { //nolint:funlen // the composition root: one sequential pass, readable top to bottom
	f := parseFlags()
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(f.logLevel)); err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	tcpConfig, err := parseTCPFlag(f.tcp)
	if err != nil {
		return err
	}
	passcode, discriminator, err := checkFlags(f)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- persistence: the node applies both schemas to the handle ---------
	db, err := openDB(ctx, f.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// --- mDNS ---------------------------------------------------------------
	var advertiser mdns.Advertiser = mdns.NewNoop()
	if f.advertise {
		zc := mdns.NewZeroconf()
		if f.osHostName {
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

	// --- the node -----------------------------------------------------------
	devices := newFleet()
	otaDownloads := f.otaDir
	if otaDownloads == "" {
		otaDownloads = filepath.Join(filepath.Dir(f.dbPath), "ota")
	}
	// The daemon's applier (ota.go); it reaches BasicInformation and the
	// requestor once the node has built them.
	applier := &otaApplier{
		vendorID: testVendorID, productID: testProductID,
		dir: otaDownloads, harness: f.otaHarness, logger: logger,
	}
	// Actions on the Aggregator (Aggregator: optional), over the demo
	// scenes (fleet_node.go).
	actions, err := mattercore.NewActions(mattercore.ActionsConfig{Host: devices.actions})
	if err != nil {
		return fmt.Errorf("aggregator actions: %w", err)
	}
	cfg := nodeConfig(f, passcode, discriminator, tcpConfig, db, advertiser, applier, actions, logger)
	if cfg.Attestation, err = loadAttestation(f.attestationDir); err != nil {
		return err
	}
	applyTestWiFi(&cfg, f, devices)
	if f.otaProvider {
		// The OTA provider with its test image (ota_provider.go), TESTING
		// ONLY.
		images, err := newTestImages()
		if err != nil {
			return err
		}
		devices.otaImages = images
		cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: images, Consent: images}
		// The ledger lookup of the offered image (dcl.go), off by default.
		if ledger, err := dclClient(f.dcl); err != nil {
			return err
		} else if ledger != nil {
			cfg.OTAProvider.ImageSource = &ledgerCheckedImages{
				OTAImageSource: images, versions: &dcl.OTAVersions{Client: ledger, Logger: logger},
				image: func(string) []byte { return images.image }, logger: logger,
			}
		}
	}
	if f.media {
		// The video player and content app (media.go), TESTING ONLY.
		if devices.media, err = newDemoMedia(logger); err != nil {
			return err
		}
	}
	n, err := assemble(ctx, devices, cfg, applier)
	if err != nil {
		return err
	}
	started := false
	defer func() {
		if !started {
			_ = n.Stop(context.WithoutCancel(ctx))
		}
	}()

	// TestEventTrigger, armed by the test enable key (control.go).
	if f.enableKey != "" {
		key, err := hex.DecodeString(f.enableKey)
		if err != nil {
			return fmt.Errorf("--enable-key: %w", err)
		}
		if err := n.Root().GeneralDiagnostics.EnableTestEventTriggers(key, devices.testEventTrigger); err != nil {
			return fmt.Errorf("--enable-key: %w", err)
		}
	}

	if err := n.Start(ctx); err != nil {
		return err
	}
	started = true
	if err := printPairingInfo(n, discriminator, passcode); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return err
	}
	printTopology(n.Bridge())
	if f.appPipe != "" {
		if err := serveAppPipe(ctx, devices, f.appPipe, logger); err != nil {
			_ = n.Stop(context.WithoutCancel(ctx))
			return err
		}
	}

	// Run on a started node waits for ctx and stops it (node.Run is Start,
	// wait, Stop; Start is idempotent).
	if err := n.Run(ctx); err != nil {
		return err
	}
	logger.Info("shutting down")
	return nil
}

// assemble builds the node for the fleet, unstarted: node.New, the OTA
// applier's handles, the blind's persisted position, one device per fleet
// member under the Aggregator (fleet.endpoints), the set marked complete,
// the app pipe's configuration-version change, and the node-level
// providers that name endpoints by StableKey.
func assemble(ctx context.Context, devices *fleet, cfg node.Config, applier *otaApplier) (*node.Node, error) {
	n, err := node.New(cfg) //nolint:contextcheck // the node's constructor takes no context; it loads the stores once
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = n.Stop(context.WithoutCancel(ctx))
		}
	}()
	// The simulated Wi-Fi link raises its events on the root's server
	// (nil without --wifi).
	devices.wifi.attach(n.Root().WiFiNetworkDiagnostics)
	if applier != nil {
		applier.basicInfo = n.Root().BasicInformation
		applier.requestor = func() *mattercore.OTASoftwareUpdateRequestor { return n.Root().OTARequestor }
	}
	restoreBlind(ctx, n.Store(), devices.blind, cfg.Logger)
	devices.lock.auth = n.Store()
	devices.thermostat.attachStore(n.Store())

	eps, err := devices.endpoints() //nolint:contextcheck // thermo.New takes no context; its PresetStore load runs once, at construction
	if err != nil {
		return nil, err
	}
	agg := n.Aggregator()
	for _, ep := range eps {
		if err := agg.Add(ctx, ep); err != nil {
			return nil, fmt.Errorf("aggregator: %w", err)
		}
	}
	// The fleet is hard-coded, so it is complete from the first assembly. A
	// host whose model loads asynchronously marks it complete once loaded,
	// or the assembler's vanished-source collection wipes every persisted
	// endpoint number at boot.
	agg.MarkComplete() //nolint:contextcheck // the node's own; the first assembly is Start's
	// The app pipe's SimulateConfigurationVersionChange raises every
	// bridged device's ConfigurationVersion, persisted by the node.
	devices.configChange = func() error { //nolint:contextcheck // the app pipe's command carries no context; the node persists with its own
		errs := make([]error, 0, len(eps))
		for _, ep := range eps {
			errs = append(errs, agg.IncreaseConfigurationVersion(ep.Spec().StableKey))
		}
		return errors.Join(errs...)
	}
	// PowerSourceConfiguration and the demo actions name endpoints by
	// their StableKey, resolved through the live topology.
	attachNodeProviders(n.Bridge(), n.Root().PowerSourceConfiguration, devices)
	ok = true
	return n, nil
}

// nodeConfig is the daemon's node.Config: the CSA test identity, the
// flags, and the root's optional labels and locales.
func nodeConfig(f flags, passcode uint32, discriminator uint16, tcp matterbridge.TCPConfig, db dbHandle,
	advertiser mdns.Advertiser, applier *otaApplier, actions contract.ClusterServer, logger *slog.Logger,
) node.Config {
	return node.Config{
		Network: node.Network{Listen: f.listen, TCP: tcp},
		Commissioning: node.Commissioning{
			Passcode: passcode, Discriminator: discriminator,
			PBKDFIterations: f.iterations, Salt: []byte(f.salt),
		},
		ProductDescription: node.ProductDescription{VendorID: testVendorID, ProductID: testProductID},
		BasicInformation: node.BasicInformation{
			VendorName:            exampleVendorName,
			ProductName:           "reference-bridge",
			NodeLabel:             f.label,
			SerialNumber:          serialNumber,
			HardwareVersion:       1,
			HardwareVersionString: "1",
			SoftwareVersion:       1,
			SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: advertiser,
		// The OTA Software Update Requestor (ADR 0019) on the root, with
		// the daemon's applier (ota.go).
		OTA: &node.OtaConfig{Dir: applier.dir, Harness: applier.harness, Applier: applier},
		FixedLabels: []fixedlabeldef.LabelStruct{
			{Label: "role", Value: "bridge"},
			{Label: "example", Value: "reference"},
		},
		// The detected values matter.js takes from Intl are this host's
		// choice: en-US, with de-DE supported.
		Locales:           []string{"en-US", "de-DE"},
		AggregatorServers: []contract.ClusterServer{actions},
		Logger:            logger,
	}
}

// printPairingInfo writes the onboarding payload to stdout. It goes to stdout
// rather than the log because it is the one thing an operator has to copy.
func printPairingInfo(n *node.Node, discriminator uint16, passcode uint32) error {
	qr, manual, err := n.PairingCodes()
	if err != nil {
		return err
	}
	br := n.Bridge()
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

`, br.LocalAddr(), bridged, testVendorID, testProductID,
		discriminator, passcode, manual, qr, passcode, discriminator)
	return nil
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
	ids := make([]uint16, 0, len(topology.Endpoints))
	for _, ep := range topology.Endpoints {
		if ep != nil {
			ids = append(ids, ep.ID)
		}
	}
	slices.Sort(ids)
	var b bytes.Buffer
	b.WriteString("  topology\n")
	for _, id := range ids {
		ep := topology.FindByID(id)
		if ep == nil {
			continue
		}
		fmt.Fprintf(&b, "    endpoint %3d  device types 0x%04X\n", id, ep.DeviceType)
	}
	b.WriteString("  topology end\n\n")
	fmt.Print(b.String())
}

// deviceTypeRevision reads a device type's revision from the generated
// matter.js snapshot instead of restating it, so a schema regeneration
// propagates here without an edit. A device type absent from the snapshot is
// a programming error in this file, not a runtime condition.
func deviceTypeRevision(id uint32) uint16 {
	rev, ok := schema.DeviceTypeRevision(id)
	if !ok {
		panic(fmt.Sprintf("device type %#06x is not in the matter.js schema snapshot", id))
	}
	return rev
}

// endpointByKey returns the live endpoint id of the endpoint with key;
// false before the first assembly or for a key the topology does not hold.
func endpointByKey(br *matterbridge.Bridge, key endpoint.StringKey) (uint16, bool) {
	topology := br.Topology()
	if topology == nil {
		return 0, false
	}
	for _, ep := range topology.Endpoints {
		if ep != nil && ep.SourceKey == key {
			return ep.ID, true
		}
	}
	return 0, false
}
