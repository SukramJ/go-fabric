// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"database/sql"
	"fmt"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/client/discovery"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/attestation"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/secure/setup"
	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/store"
)

// The names below are the lower packages' own, re-exported so a host that
// configures and observes a node through this package names them without
// importing bridge, endpoint, secure/* or im/subscription. Each alias is
// the same type as its original (an alias, not a new type): a value passes
// between the two names unconverted, and code written against the
// original keeps compiling. docs/guides/advanced-seams.md lists what stays
// reachable only below the facade.

// Snapshotter is the type of [Config].Snapshotter: a host that assembles its
// own endpoints returns one [Topology] per call ([bridge.Snapshotter]).
type Snapshotter = bridge.Snapshotter

// Topology is the assembled endpoint tree a [Snapshotter] returns
// ([endpoint.Topology]).
type Topology = endpoint.Topology

// TCPConfig is the type of [Network].TCP: the Matter TCP roles
// ([bridge.TCPConfig]).
type TCPConfig = bridge.TCPConfig

// SessionManager is the secure-session table [Node.Sessions] returns
// ([operational.Manager]): Sessions lists the PASE and CASE sessions,
// Occupancy the session-id space.
type SessionManager = operational.Manager

// SubscriptionManager is the subscription table [Node.Subscriptions]
// returns ([subscription.Manager]): Active counts the subscriptions,
// CountBySession groups them by session, Snapshot lists them.
type SubscriptionManager = subscription.Manager

// SemanticTag is one entry of a Descriptor's TagList, as
// [BridgedDevice].Tags lists it ([mattercore.SemanticTag]).
type SemanticTag = mattercore.SemanticTag

// SubscriptionInfo is one subscription as [SubscriptionManager]'s
// Snapshot lists it, a copy ([subscription.Info]).
type SubscriptionInfo = subscription.Info

// Sealer seals the node's secrets at rest: the type of [Store].Sealer and
// of [Reseal]'s keys ([store.Sealer]); [NewAESGCMSealer] returns the
// module's own (review II, R4-api-4706, decision 657).
type Sealer = store.Sealer

// DiagnosticEvent is one entry of the node's diagnostic trace
// ([diagevent.Event]): the moments that explain a failed pairing.
type DiagnosticEvent = diagevent.Event

// The commissioning-window errors a host compares with errors.Is. They are
// the bridge's values, not copies: errors.Is(err, node.ErrX) and
// errors.Is(err, bridge.ErrX) answer alike.
var (
	// ErrAdministratorWindowOpen refuses the node's own window while an
	// administrator's window is open ([Node.OpenBasicWindow],
	// [Node.OpenOwnWindow]).
	ErrAdministratorWindowOpen = bridge.ErrAdministratorWindowOpen
	// ErrCommissioningBlocked is wrapped by every window open refused
	// while [Node.SetCommissioningBlocked] holds an error.
	ErrCommissioningBlocked = bridge.ErrCommissioningBlocked
	// ErrCommissioningWindowAlreadyOpen refuses a second window while one
	// is open.
	//
	// Deprecated: no node method returns it; its only source is
	// bridge.CommissioningWindowOpener, which the node does not use —
	// compare with bridge.ErrCommissioningWindowAlreadyOpen there.
	// Removable in v0.25.0.
	ErrCommissioningWindowAlreadyOpen = bridge.ErrCommissioningWindowAlreadyOpen
	// ErrCommissioningWindowNotConfigured is the window's answer when no
	// passcode is configured.
	//
	// Deprecated: no node method returns it; its only source is
	// bridge.CommissioningWindowOpener, which the node does not use —
	// compare with bridge.ErrCommissioningWindowNotConfigured there.
	// Removable in v0.25.0.
	ErrCommissioningWindowNotConfigured = bridge.ErrCommissioningWindowNotConfigured
)

// The store errors node methods return, for errors.Is. They are the
// store's values, not copies.
var (
	// ErrScrubIncomplete is wrapped by a [Node.FactoryReset] whose erase
	// completed but whose scrub of the SQLite file did not finish
	// (store.ErrScrubIncomplete).
	ErrScrubIncomplete = store.ErrScrubIncomplete
	// ErrFabricNotFound is [Node.RemoveFabric]'s answer for an index no
	// fabric holds (store.ErrFabricNotFound).
	ErrFabricNotFound = store.ErrFabricNotFound
	// ErrUnseal is wrapped by [New] when [Store].Sealer does not open the
	// sealed database — another key than the one that sealed it, or a
	// tampered value (store.ErrUnseal).
	ErrUnseal = store.ErrUnseal
	// ErrNoSealer is wrapped by a read or write of a sealed secret on a
	// store opened without a sealer (store.ErrNoSealer): the database was
	// sealed, and [Store].Sealer must be set.
	ErrNoSealer = store.ErrNoSealer
)

// NewAESGCMSealer returns the module's stdlib sealer for [Store].Sealer —
// AES-256-GCM under key, which must be 32 bytes. It is
// store.NewAESGCMSealer, for a host that imports the facade only
// (decision 551). The key is the host's: generated once (crypto/rand) and
// kept outside the database (docs/security.md, "Sealing at rest").
func NewAESGCMSealer(key []byte) (*store.AESGCMSealer, error) {
	return store.NewAESGCMSealer(key)
}

// Reseal moves a node's sealed database from the sealer old to the sealer
// next — the rotation of [Store].Sealer's key, for a host that imports the
// facade only (decision 561). It is store.Reseal: one transaction opens
// the seal canary under old (the key check), seals every secret under
// next and writes the canary last, then scrubs the file. A wrong old key,
// a value that does not open under it and a secret in clear are
// [ErrUnseal] and leave the database as it was; an error after the commit
// wraps [ErrScrubIncomplete] — the values are sealed under next, and the
// next [New] under next runs the scrub again. A start without a sealer on
// the resealed database reads a secret as [ErrNoSealer]; New under old
// fails with ErrUnseal. old and next are [NewAESGCMSealer]'s or the host's
// own [Sealer]; neither may be nil.
//
// db is the node's SQLite file — [Store].DB, or [Store].Path opened with
// database/sql under the "sqlite" driver the node registers — and no node
// may run on it while Reseal does: a running node holds the old sealer.
// It carries the schema a New applied.
// A controller's database moves with controller.Reseal, which also
// reseals its node registry.
func Reseal(ctx context.Context, db *sql.DB, old, next Sealer) error {
	return store.Reseal(ctx, db, old, next)
}

// The commissioning bounds [Config.Validate] and [Node.OpenBasicWindow]
// check, for a host that validates its own configuration before [New].
const (
	// MaxDiscriminator is the largest 12-bit discriminator
	// ([setup.MaxDiscriminator]).
	MaxDiscriminator = setup.MaxDiscriminator
	// PBKDFIterationsMin and PBKDFIterationsMax bound
	// [Commissioning].PBKDFIterations ([spake2.IterationsMin],
	// [spake2.IterationsMax]).
	PBKDFIterationsMin = spake2.IterationsMin
	PBKDFIterationsMax = spake2.IterationsMax
	// PBKDFSaltMin and PBKDFSaltMax bound the length of
	// [Commissioning].Salt in bytes ([spake2.PBKDFMinSaltSize],
	// [spake2.PBKDFMaxSaltSize]).
	PBKDFSaltMin = spake2.PBKDFMinSaltSize
	PBKDFSaltMax = spake2.PBKDFMaxSaltSize
)

// The RegulatoryLocationTypeEnum values (mattercore.RegulatoryIndoor, …).
// [Commissioning].LocationCapability takes RegulatoryOutdoor or
// RegulatoryIndoorOutdoor; RegulatoryIndoor is its zero value and reads as
// the default, IndoorOutdoor — Indoor is [Commissioning].IndoorOnly.
const (
	RegulatoryIndoor        = mattercore.RegulatoryIndoor
	RegulatoryOutdoor       = mattercore.RegulatoryOutdoor
	RegulatoryIndoorOutdoor = mattercore.RegulatoryIndoorOutdoor
)

// SoftwareVersionFromString maps a version string such as "1.4.2" to the
// BasicInformation SoftwareVersion number, for
// [BasicInformation].SoftwareVersion
// ([mattercore.SoftwareVersionFromString]: the major, minor and patch
// components packed, floored at 1).
func SoftwareVersionFromString(version string) uint32 {
	return mattercore.SoftwareVersionFromString(version)
}

// ValidateSetupPIN reports whether pin is a usable setup passcode: within
// 1..99999998 and not on the trivially-guessable list
// ([setup.IsValidSetupPIN]) — the check [Config.Validate] applies to
// [Commissioning].Passcode. The error wraps [ErrInvalidConfig].
func ValidateSetupPIN(pin uint32) error {
	if !validSetupPIN(pin) {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errSetupPIN)
	}
	return nil
}

// Host ports and values named by the Config fields (G1-6): the same types as
// the lower packages' own, owned here so `go doc` shows node.X and a host
// implements node.OTAImageSource without importing cluster/core.

// TimeSynchronizationConfig is the type of [Config].TimeSynchronization: the root TimeSynchronization's features ([mattercore.TimeSynchronizationConfig]).
type TimeSynchronizationConfig = mattercore.TimeSynchronizationConfig

// NTPClient is the type of [TimeSynchronizationConfig].NTPClient: the host's NTP query the time client falls back to ([mattercore.NTPClient]).
type NTPClient = mattercore.NTPClient

// TimePeerReader is the type of [TimeSynchronizationConfig].PeerReader: the read of the trusted time source's node ([mattercore.TimePeerReader]); without one the node reads over its client stack.
type TimePeerReader = mattercore.TimePeerReader

// OTAImageSource is the type of [OTAProviderConfig].ImageSource: where the provider reads the images it offers ([mattercore.OTAImageSource]).
type OTAImageSource = mattercore.OTAImageSource

// OTAConsent is the type of [OTAProviderConfig].Consent: the user-consent port of the provider ([mattercore.OTAConsent]).
type OTAConsent = mattercore.OTAConsent

// OTAConsentState is what an [OTAConsent] answers ([mattercore.OTAConsentState]).
type OTAConsentState = mattercore.OTAConsentState

// OTAApplyPolicy is the type of [OTAProviderConfig].ApplyPolicy: when a requestor may apply a downloaded image ([mattercore.OTAApplyPolicy]).
//
// Deprecated: the delay is [OTAProviderConfig].ApplyDelay and the apply is the [OTAUpdateManager]'s to decide; removable in v0.26.0.
type OTAApplyPolicy = mattercore.OTAApplyPolicy //nolint:staticcheck // the deprecated alias keeps its type for its window

// OTAUpdateManager is the type of [OTAProviderConfig].Manager: the owner of the requestors' update attempts the provider asks ([mattercore.OTAUpdateManager]).
type OTAUpdateManager = mattercore.OTAUpdateManager

// OTAUpdateAttempt is one requestor's update flow an [OTAUpdateManager] owns ([mattercore.OTAUpdateAttempt]).
type OTAUpdateAttempt = mattercore.OTAUpdateAttempt

// OTAPeer names the requestor an [OTAProviderConfig] callback is about ([mattercore.OTAPeer]).
type OTAPeer = mattercore.OTAPeer

// OTAUpdateStatus is the status [Node.OnOTAProviderStatus] reports for a requestor ([mattercore.OTAUpdateStatus]).
type OTAUpdateStatus = mattercore.OTAUpdateStatus

// OTAApplier is the type of [OTAConfig].Applier: the requestor's consent and apply hooks ([mattercore.OtaApplier]).
type OTAApplier = mattercore.OtaApplier

// OTADownloadProtocol is the element type of [OTAConfig].TransferProtocols ([mattercore.DownloadProtocol]).
type OTADownloadProtocol = mattercore.DownloadProtocol

// OTAHTTPClient is the type of [OTAConfig].HTTPClient: the GET of an HTTPS download ([mattercore.OtaHTTPClient]); *http.Client implements it.
type OTAHTTPClient = mattercore.OtaHTTPClient

// The download protocols [OTAConfig].TransferProtocols takes
// ([mattercore.DownloadProtocolBDXSynchronous], [mattercore.DownloadProtocolHTTPS]).
const (
	OTADownloadBDXSynchronous = mattercore.DownloadProtocolBDXSynchronous
	OTADownloadHTTPS          = mattercore.DownloadProtocolHTTPS
)

// WiFiReporter is the type of [WiFi].Reporter: the live Wi-Fi diagnostics the host reports ([mattercore.WiFiReporter]).
type WiFiReporter = mattercore.WiFiReporter

// CommissioningApprover is the type of [CommissionerControlConfig].Approver: the host's decision on a commissioning request ([mattercore.CommissioningApprover]).
type CommissioningApprover = mattercore.CommissioningApprover

// ReverseCommissioner is the type of [CommissionerControlConfig].Commissioner: the host's commissioner of the requesting node ([mattercore.ReverseCommissioner]).
type ReverseCommissioner = mattercore.ReverseCommissioner

// CapabilityMinimaStruct is the type of [Config].CapabilityMinima: the BasicInformation CapabilityMinima ([mattercore.CapabilityMinimaStruct]).
type CapabilityMinimaStruct = mattercore.CapabilityMinimaStruct

// GeneralDiagnosticsFeatures is the type of [Config].GeneralDiagnosticsFeatures: the feature selection of the root GeneralDiagnostics ([mattercore.GeneralDiagnosticsFeatures]).
type GeneralDiagnosticsFeatures = mattercore.GeneralDiagnosticsFeatures

// DeviceTypeValidationMode is the type of [Config].DeviceTypeValidation: how the node treats a device type that breaks its conformance ([endpoint.DeviceTypeValidationMode]).
type DeviceTypeValidationMode = endpoint.DeviceTypeValidationMode

// WindowStatus is the AdministratorCommissioning view [WindowSnapshot].Administrator carries ([wire.WindowStatusSnapshot]).
type WindowStatus = wire.WindowStatusSnapshot

// AttestationChain is the type of [Attestation].Chain: the DAC, its key and the PAI ([attestation.Chain]).
type AttestationChain = attestation.Chain

// The lower-layer types the advanced seams return, under names the facade
// owns (ADR 0025 principle 3): a host names them without importing the
// package below, and code written against the originals keeps compiling.
type (
	// SessionParameters is what [Node.SessionParameters] returns: the
	// session parameters the node's PASE and CASE responders advertise
	// ([sessionparams.Parameters], controller/client.SessionParameters on
	// the controller side).
	SessionParameters = sessionparams.Parameters
	// OTAProviderServer is the OTA Software Update Provider
	// [Node.OTAProvider] returns ([mattercore.OTAProvider]).
	OTAProviderServer = mattercore.OTAProvider
	// BDXResponder is the BDX responder [Node.BDX] returns
	// ([bdx.Responder]).
	BDXResponder = bdx.Responder
	// CommissionerControlServer is the CommissionerControl server
	// [Node.CommissionerControl] returns ([mattercore.CommissionerControl]).
	CommissionerControlServer = mattercore.CommissionerControl
	// DiscoveryNames is the DNS-SD cache of [ClientConfig].Names
	// ([discovery.Names]).
	DiscoveryNames = discovery.Names
)
