// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// The names below are the lower packages' own, re-exported so a host that
// configures and observes a node through this package names them without
// importing bridge, endpoint, secure/* or im/subscription. Each alias is
// the same type as its original (an alias, not a new type): a value passes
// between the two names unconverted, and code written against the
// original keeps compiling. docs/guides/advanced-seams.md lists what stays
// reachable only below the facade.

// Snapshotter is the type of [Config].Topology: a host that assembles its
// own endpoints returns one [Topology] per call ([bridge.Snapshotter]).
type Snapshotter = bridge.Snapshotter

// Topology is the assembled endpoint tree a [Snapshotter] returns
// ([endpoint.Topology]).
type Topology = endpoint.Topology

// TCPConfig is the type of [Network].TCP: the Matter TCP roles
// ([bridge.TCPConfig]).
type TCPConfig = bridge.TCPConfig

// CommissioningWindow is the commissioning window [Node.CommissioningWindow]
// returns ([bridge.CommissioningWindow]).
type CommissioningWindow = bridge.CommissioningWindow

// SessionManager is the secure-session table [Node.Sessions] returns
// ([operational.Manager]): Sessions lists the PASE and CASE sessions,
// Occupancy the session-id space.
type SessionManager = operational.Manager

// SubscriptionManager is the subscription table [Node.Subscriptions]
// returns ([subscription.Manager]): Active counts the subscriptions,
// CountBySession groups them by session.
type SubscriptionManager = subscription.Manager

// DiagnosticEvent is one entry of the node's diagnostic trace
// ([diagevent.Event]): the moments that explain a failed pairing.
type DiagnosticEvent = diagevent.Event

// The commissioning-window errors a host compares with errors.Is. They are
// the bridge's values, not copies: errors.Is(err, node.ErrX) and
// errors.Is(err, bridge.ErrX) answer alike.
var (
	// ErrAdministratorWindowOpen refuses the node's own window while an
	// administrator's window is open ([Node.OpenBasicWindow],
	// CommissioningWindow().OpenOwnWindow).
	ErrAdministratorWindowOpen = bridge.ErrAdministratorWindowOpen
	// ErrCommissioningBlocked is wrapped by every window open refused
	// while [Node.SetCommissioningBlocked] holds an error.
	ErrCommissioningBlocked = bridge.ErrCommissioningBlocked
	// ErrCommissioningWindowAlreadyOpen refuses a second window while one
	// is open.
	ErrCommissioningWindowAlreadyOpen = bridge.ErrCommissioningWindowAlreadyOpen
	// ErrCommissioningWindowNotConfigured is the window's answer when no
	// passcode is configured.
	ErrCommissioningWindowNotConfigured = bridge.ErrCommissioningWindowNotConfigured
)

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

// The RegulatoryLocationTypeEnum values [Commissioning].LocationCapability
// takes (mattercore.RegulatoryIndoor, …).
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
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errSetupPIN(pin))
	}
	return nil
}

// Host ports and values named by the Config fields (G1-6): the same types as
// the lower packages' own, owned here so `go doc` shows node.X and a host
// implements node.OTAImageSource without importing cluster/core.

// OTAImageSource is the type of [OTAProviderConfig].ImageSource: where the provider reads the images it offers ([mattercore.OTAImageSource]).
type OTAImageSource = mattercore.OTAImageSource

// OTAConsent is the type of [OTAProviderConfig].Consent: the user-consent port of the provider ([mattercore.OTAConsent]).
type OTAConsent = mattercore.OTAConsent

// OTAConsentState is what an [OTAConsent] answers ([mattercore.OTAConsentState]).
type OTAConsentState = mattercore.OTAConsentState

// OTAApplyPolicy is the type of [OTAProviderConfig].ApplyPolicy: when a requestor may apply a downloaded image ([mattercore.OTAApplyPolicy]).
type OTAApplyPolicy = mattercore.OTAApplyPolicy

// OTAPeer names the requestor an [OTAProviderConfig] callback is about ([mattercore.OTAPeer]).
type OTAPeer = mattercore.OTAPeer

// OTAUpdateStatus is the status [OTAProviderConfig].OnStatusChange reports for a requestor ([mattercore.OTAUpdateStatus]).
type OTAUpdateStatus = mattercore.OTAUpdateStatus

// OTAApplier is the type of [OtaConfig].Applier: the requestor's consent and apply hooks ([mattercore.OtaApplier]).
type OTAApplier = mattercore.OtaApplier

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
