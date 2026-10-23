// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"io"
	"log/slog"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/dcl"
)

// The node features a host reaches beside its Config (gate 2, G2-9): the
// values its OTA image source hands back, the OTA image file format, the
// ledger's software versions, the Aggregator's Actions server, the
// EcosystemInformation directory, and the root servers a host drives after
// Start (Node.Root). Each is the lower package's own type or function,
// owned here so a host on the facade names it as node.X (ADR 0025
// principle 3); none adds behaviour.

// OTAImage is what an [OTAImageSource] offers a requestor: its file
// designator, version, HTTPS URI, consent requirement and metadata
// ([mattercore.OTAImage]).
type OTAImage = mattercore.OTAImage

// OTAFileDesignator is the type of [OTAImage].FileDesignator: the image's
// name in the provider's "ota" scope ([bdx.FileDesignator]).
type OTAFileDesignator = bdx.FileDesignator

// OTABlob is what [OTAImageSource].Open returns: the bytes of an offered
// image, read at offsets as the BDX transfer asks ([bdx.Blob]).
type OTABlob = bdx.Blob

// The consent states an [OTAConsent] answers (matter.js
// OtaSoftwareUpdateConsentState).
const (
	OTAConsentGranted   = mattercore.OTAConsentGranted
	OTAConsentDenied    = mattercore.OTAConsentDenied
	OTAConsentObtaining = mattercore.OTAConsentObtaining
	OTAConsentUnknown   = mattercore.OTAConsentUnknown
)

// OTAImageHeader is the header of a Matter OTA image file — vendor and
// product id, the software version and its string, the applicable range,
// the payload's size and digest ([otaimage.Header]).
type OTAImageHeader = otaimage.Header

// OTAImageOptions is what [CreateOTAImage] builds an image from
// ([otaimage.CreateOptions]).
type OTAImageOptions = otaimage.CreateOptions

// CreateOTAImage builds a Matter OTA image file — header, payload and its
// digest — from opts ([otaimage.Create]).
func CreateOTAImage(opts OTAImageOptions) ([]byte, error) { return otaimage.Create(opts) }

// ReadOTAImage reads an OTA image file from r, writes its payload to w and
// returns its header, the digest checked; expectedTotalSize, when non-nil,
// is checked against the file's size ([otaimage.ExtractPayload]).
func ReadOTAImage(r io.Reader, w io.Writer, expectedTotalSize *uint64) (OTAImageHeader, error) {
	return otaimage.ExtractPayload(r, w, expectedTotalSize)
}

// DCLClient asks the CSA Distributed Compliance Ledger ([dcl.Client]).
// Nothing reaches the network unless a host builds one.
type DCLClient = dcl.Client

// The ledger's endpoints for [NewDCLClient].
const (
	DCLProductionURL = dcl.ProductionURL
	DCLTestNetURL    = dcl.TestNetURL
)

// NewDCLClient is a ledger client at baseURL ([dcl.NewClient]).
func NewDCLClient(baseURL string) *DCLClient { return dcl.NewClient(baseURL) }

// DCLOTAVersions looks a model's software versions up in the ledger: the
// update for a current version, one version's record, its download
// ([dcl.OTAVersions]). An OTA image source checks what it offers with it.
type DCLOTAVersions = dcl.OTAVersions

// DCLSoftwareVersion is the ledger's record of one software version
// ([dcl.SoftwareVersion]).
type DCLSoftwareVersion = dcl.SoftwareVersion

// VerifyOTAImage checks an OTA image read from r against the ledger's
// record of its version — header, size and checksum ([dcl.VerifyImage]).
func VerifyOTAImage(r io.Reader, v DCLSoftwareVersion, log *slog.Logger) (OTAImageHeader, error) {
	return dcl.VerifyImage(r, v, log)
}

// Actions is the Actions server a host mounts on the Aggregator
// ([Config].AggregatorServers): its action list, endpoint lists and
// commands, the host deciding each command ([mattercore.Actions]).
type Actions = mattercore.Actions

// ActionsConfig configures [NewActions] ([mattercore.ActionsConfig]).
type ActionsConfig = mattercore.ActionsConfig

// ActionsHost is the host port of an [Actions] server: its actions and
// endpoint lists, and the decision on each command ([mattercore.ActionsHost]).
type ActionsHost = mattercore.ActionsHost

// ActionRequest is one Actions command an [ActionsHost] decides
// ([mattercore.ActionRequest]).
type ActionRequest = mattercore.ActionRequest

// NewActions builds an Actions server over cfg ([mattercore.NewActions]).
func NewActions(cfg ActionsConfig) (*Actions, error) { return mattercore.NewActions(cfg) }

// EcosystemDirectory is the host port of an EcosystemInformation server:
// the devices and locations of the host's ecosystem ([mattercore.EcosystemDirectory]).
type EcosystemDirectory = mattercore.EcosystemDirectory

// EcosystemInformation is an EcosystemInformation server over an
// [EcosystemDirectory] ([mattercore.EcosystemInformation]); a device
// declares it with ecosysteminformationdef.Declare{HostServer: …}.
type EcosystemInformation = mattercore.EcosystemInformation

// NewEcosystemInformation builds an EcosystemInformation server over dir
// ([mattercore.NewEcosystemInformation]).
func NewEcosystemInformation(dir EcosystemDirectory) *EcosystemInformation {
	return mattercore.NewEcosystemInformation(dir)
}

// The root servers [Node.Root] holds that a host drives after Start: the
// same types as [RootClusters]' fields.
type (
	// BasicInformationServer is the root BasicInformation server — its software
	// version after an applied update, its NodeLabel and its reachable
	// event ([mattercore.BasicInformation]).
	BasicInformationServer = mattercore.BasicInformation
	// OTARequestorServer is the root OTA Software Update Requestor server
	// ([mattercore.OTASoftwareUpdateRequestor]).
	OTARequestorServer = mattercore.OTASoftwareUpdateRequestor
	// WiFiNetworkDiagnosticsServer is the root Wi-Fi Network Diagnostics server
	// and its events ([mattercore.WiFiNetworkDiagnostics]).
	WiFiNetworkDiagnosticsServer = mattercore.WiFiNetworkDiagnostics
	// PowerSourceConfigurationServer is the root PowerSourceConfiguration server
	// and its Sources provider ([mattercore.PowerSourceConfiguration]).
	PowerSourceConfigurationServer = mattercore.PowerSourceConfiguration
)
