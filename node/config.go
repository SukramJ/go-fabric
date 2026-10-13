// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/store"
)

// Config is the node's configuration, code-first (ADR 0018 decision 5).
// Its groups are the behaviour ids matter.js's ServerNode.Configuration
// keys its options by — `network`, `commissioning`, `productDescription`,
// `basicInformation` (packages/node/src/node/ServerNode.ts:107-127) — for
// the subset the reference daemon uses; the rest is what the daemon's
// bring-up needs beyond them (examples/reference-bridge/main.go:84-497).
type Config struct {
	// Network mirrors matter.js `network` (NetworkServer.ts:202-222).
	Network Network
	// Commissioning mirrors matter.js `commissioning`
	// (CommissioningServer.ts:400-410).
	Commissioning Commissioning
	// ProductDescription mirrors matter.js `productDescription`
	// (ProductDescriptionServer.ts:70-90): the identity the commissionable
	// advertisement carries.
	ProductDescription ProductDescription
	// BasicInformation mirrors matter.js `basicInformation`
	// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:29-65).
	BasicInformation BasicInformation
	// Store is where the node keeps its fabrics, credentials, ACL, groups,
	// subscriptions, settings and endpoint numbers.
	Store Store
	// Advertiser publishes the node over DNS-SD; nil is mdns.NewNoop(),
	// as bridge.New treats a nil advertiser. [Node.Stop] closes it.
	Advertiser mdns.Advertiser
	// DeviceTypeValidation is the bridge's device-type judgement mode
	// (bridge.SetDeviceTypeValidation); the zero value is matter.js's
	// warn mode.
	DeviceTypeValidation endpoint.DeviceTypeValidationMode
	// Attestation is the device attestation material; the zero value is
	// the CSA test chain and test Certification Declaration for the
	// configured vendor and product (attestation.BuildTestChain,
	// attestation.BuildTestCertificationDeclaration) — pairable with every
	// commissioner's test PAA, and not shippable.
	Attestation Attestation
	// OTA mounts the OTA Software Update Requestor on the root when set;
	// nil leaves it out (and the OtaRequestor device type with it).
	OTA *OtaConfig
	// FixedLabels is the root FixedLabel list; nil serves an empty list.
	FixedLabels []fixedlabeldef.LabelStruct
	// Locales are the root LocalizationConfiguration's SupportedLocales,
	// the first of them its ActiveLocale; nil is en-US alone. matter.js
	// detects the active locale and lists it alone when the host states
	// neither (LocalizationConfigurationServer.ts:14-21 initialize); a host
	// states its own, as the reference daemon does (en-US, de-DE).
	Locales []string
	// AggregatorServers are mounted on the Aggregator endpoint after
	// Identify, Descriptor, FixedLabel and UserLabel — an Actions server,
	// say (the daemon's buildAggregatorClusters extra, wiring.go:330-345).
	AggregatorServers []contract.ClusterServer
	// SupportedFabrics is OperationalCredentials' SupportedFabrics, the
	// number of fabrics the node admits (Matter §11.18.4.4: 1 to 254); 0
	// is 5, the facade's value before it was configurable. matter.js
	// defaults it to 254 when unset (OperationalCredentialsServer.ts:
	// 84-88 initialize), which cluster/core does too; the facade keeps 5.
	SupportedFabrics uint8
	// DisableTimeSynchronization leaves the TimeSynchronization cluster
	// (0x0038) off the root; the zero value mounts it, as the facade
	// always did. RootNode carries it under an optional conformance
	// (device/root_node_gen.go, generated from the snapshot).
	// Without it GeneralDiagnostics has no UTC clock: TimeSnapshot
	// answers SystemTimeMs alone and PosixTimeMs null, as matter.js
	// GeneralDiagnosticsServer.timeSnapshot does without a
	// TimeSynchronizationBehavior (GeneralDiagnosticsServer.ts:168-182).
	DisableTimeSynchronization bool
	// Observers are the host's callbacks on fabric and commissioning
	// events, each chained after the facade's own handler (observers.go);
	// the zero value observes nothing.
	Observers Observers
	// Logger is the node's logger; nil is slog.Default().
	Logger *slog.Logger
}

// Network is matter.js `network` for the keys the daemon uses
// (NetworkServer.ts:202-222: port and listening address, ipv4, tcp).
type Network struct {
	// Listen is the UDP listen address; empty is ":5540".
	Listen string
	// PreferIPv4 forces an IPv4-only socket.
	PreferIPv4 bool
	// TCP selects the Matter TCP transport; the zero value is TCP off.
	TCP bridge.TCPConfig
}

// Commissioning is matter.js `commissioning` (CommissioningServer.ts:
// 370-380: passcode, discriminator) plus the SPAKE2+ PBKDF parameters the
// daemon configures (main.go:91-92).
type Commissioning struct {
	// Passcode is the setup passcode; it must pass setup.IsValidSetupPIN.
	Passcode uint32
	// Discriminator is the 12-bit commissioning discriminator.
	Discriminator uint16
	// PBKDFIterations is the SPAKE2+ PBKDF2 iteration count, within
	// [spake2.IterationsMin, spake2.IterationsMax].
	PBKDFIterations int
	// Salt is the SPAKE2+ salt, spake2.PBKDFMinSaltSize to
	// spake2.PBKDFMaxSaltSize bytes.
	Salt []byte
}

// ProductDescription is matter.js `productDescription`
// (ProductDescriptionServer.ts:70-90).
type ProductDescription struct {
	// Name is the advertised device name; empty is
	// BasicInformation.NodeLabel, as the daemon advertises (main.go:285).
	Name string
	// DeviceType is the advertised device type; 0 is RootNode (0x0016),
	// as the daemon advertises (main.go:290).
	DeviceType uint16
	// VendorID and ProductID are the node's identity; both mandatory.
	VendorID, ProductID uint16
}

// BasicInformation is the root BasicInformation the daemon configures
// (examples/reference-bridge/wiring.go:105-116, buildRootClusters), plus
// ProductLabel from matter.js's example (BridgedDevicesNode.ts:29-65).
type BasicInformation struct {
	VendorName            string
	ProductName           string
	ProductLabel          string
	NodeLabel             string // mandatory
	SerialNumber          string
	HardwareVersion       uint16
	HardwareVersionString string
	SoftwareVersion       uint32
	SoftwareVersionString string
}

// Store names the node's persistence. With DB the node applies both
// schemas (store.Apply, sqlitestore.Apply) and opens the two stores on it,
// as the daemon's openDB does (persist.go:45-60); the caller picks the
// SQLite driver and owns the handle. Credentials and Endpoints override
// the stores built from DB, or stand in for it when DB is nil.
type Store struct {
	DB          *sql.DB
	Credentials *store.Store
	Endpoints   endpoint.Store
}

// Attestation is the device attestation material.
type Attestation struct {
	// Chain is the DAC, its key and the PAI; nil is the CSA test chain.
	Chain *attestation.Chain
	// CertificationDeclaration is the CMS-signed CD; nil is the CSA test
	// CD for the vendor and product.
	CertificationDeclaration []byte
}

// OtaConfig mounts the OTA Software Update Requestor (ADR 0019).
type OtaConfig struct {
	// Dir receives the downloads; mandatory.
	Dir string
	// Harness applies an image in place, reporting its version as running
	// without a restart. TESTING ONLY — see the daemon's otaApplier.
	Harness bool
	// Applier carries out an update; nil is the daemon's applier, which
	// validates the image and keeps it under Dir/applied.
	Applier mattercore.OtaApplier
}

// ErrInvalidConfig wraps every [Config.Validate] failure.
var ErrInvalidConfig = errors.New("node: invalid configuration")

// setupPasscodeMax is the Matter §5.1.1.6 upper bound the daemon checks
// (main.go:121-125).
const setupPasscodeMax = 99999998

// Fabric-table bounds: Matter §11.18.4.4 limits SupportedFabrics to 1..254
// (matter.js: "FabricBuilder uses 254 as max",
// OperationalCredentialsServer.ts:86); the facade's default is 5.
const (
	maxSupportedFabrics     = 254
	defaultSupportedFabrics = 5
)

// Validate holds the checks the daemon applies to its flags
// (main.go:113-131): the discriminator's 12 bits, the passcode's range
// and the trivially-guessable list, the PBKDF iteration range and salt
// length the SPAKE2+ verifier needs, and the identity the bridge and the
// assembler refuse to start without.
func (c *Config) Validate() error {
	var errs []error
	cm := c.Commissioning
	if cm.Discriminator > setup.MaxDiscriminator {
		errs = append(errs, fmt.Errorf("discriminator %d exceeds the 12-bit maximum %d", cm.Discriminator, setup.MaxDiscriminator))
	}
	if cm.Passcode > setupPasscodeMax || !setup.IsValidSetupPIN(cm.Passcode) {
		errs = append(errs, fmt.Errorf("passcode %d is invalid or on the trivially-guessable list", cm.Passcode))
	}
	if cm.PBKDFIterations < spake2.IterationsMin || cm.PBKDFIterations > spake2.IterationsMax {
		errs = append(errs, fmt.Errorf("PBKDF iterations %d not in [%d, %d]", cm.PBKDFIterations, spake2.IterationsMin, spake2.IterationsMax))
	}
	if len(cm.Salt) < spake2.PBKDFMinSaltSize || len(cm.Salt) > spake2.PBKDFMaxSaltSize {
		errs = append(errs, fmt.Errorf("PBKDF salt is %d bytes, want %d to %d", len(cm.Salt), spake2.PBKDFMinSaltSize, spake2.PBKDFMaxSaltSize))
	}
	if c.ProductDescription.VendorID == 0 || c.ProductDescription.ProductID == 0 {
		errs = append(errs, errors.New("ProductDescription.VendorID and ProductID must be non-zero"))
	}
	if strings.TrimSpace(c.BasicInformation.NodeLabel) == "" {
		errs = append(errs, errors.New("BasicInformation.NodeLabel must be non-empty"))
	}
	if c.Store.DB == nil && (c.Store.Credentials == nil || c.Store.Endpoints == nil) {
		errs = append(errs, errors.New("Store needs DB, or both Credentials and Endpoints"))
	}
	if c.SupportedFabrics > maxSupportedFabrics {
		errs = append(errs, fmt.Errorf("SupportedFabrics %d exceeds %d", c.SupportedFabrics, maxSupportedFabrics))
	}
	if c.OTA != nil && c.OTA.Dir == "" {
		errs = append(errs, errors.New("OTA.Dir must be set"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}
