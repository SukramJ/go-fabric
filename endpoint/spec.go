// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"strings"

	"github.com/SukramJ/go-fabric/contract"
)

// AvailabilityProbe reports whether the source behind a bridged
// endpoint is reachable right now.
//
// It is a probe rather than a captured bool because the cluster
// surface is rebuilt on every dispatch and re-reads it there: a source
// that dies between two assemblies must stop advertising
// Reachable=true, and a topology-time snapshot cannot express that.
// [Topology] assembly still records one reading into
// [Endpoint.Reachable] so a commissioner that never subscribes sees
// the state the topology was built with. nil means "always reachable",
// which is what an endpoint with no live source (the root and the
// aggregator) needs.
// loom:reachable:reason="carried on Spec and Endpoint and called by materialize on every dispatch; a named func type reached only through a struct field is invisible to the analyzer"
type AvailabilityProbe func() bool

// Spec is one bridged endpoint as its owner describes it: the
// assembly input for [Assembler.Assemble].
//
// It is deliberately flat and free of any device-model type. The
// caller walks whatever tree it has, decides what deserves an
// endpoint, resolves the operator-facing strings through its own
// naming authority and hands the result over as values. Everything the assembly then does — endpoint-id allocation
// and persistence, the three-tier root/aggregator scaffolding, the
// per-endpoint state that must survive a reassembly, the cluster
// surface — depends on nothing but the fields below.
// loom:reachable:reason="constructed by the host adapter and consumed by Assemble in this package; a struct that only ever travels as a slice element has no construction site the analyzer follows"
type Spec struct {
	// StableKey identifies the source across reassemblies and daemon
	// restarts. It is the owner's persisted endpoint-identity key, so
	// it decides which endpoint id the endpoint gets back, which
	// per-endpoint state it reuses, and which UniqueID it publishes.
	// Two specs must never share one key. See [SourceKey] for the
	// stability the rendering owes a paired controller.
	StableKey SourceKey
	// DeviceAddress names the physical device this endpoint belongs to,
	// in the owner's own namespace. Carried through to
	// [Endpoint.DeviceAddress], where it is what
	// [Bridge.NotifyDeviceReachable] fans a device-level signal out
	// over. Empty when the owner has no such notion.
	DeviceAddress string
	// DeviceType is the Matter Device Type ID the endpoint advertises
	// as its primary type (e.g. 0x010A OnOffPlugInUnit).
	DeviceType uint16
	// VendorName is the bridged device's manufacturer, served as its
	// BridgedDeviceBasicInformation VendorName (capped at the attribute's
	// 32 bytes). The vendor is the host's knowledge, never the module's.
	// Empty serves the node's own VendorName ([Config.VendorName]).
	VendorName string
	// FriendlyName is the finished BridgedDeviceBasicInformation
	// NodeLabel. The assembly caps it at the Matter 32-byte maximum but
	// never derives it — naming is the owner's authority.
	FriendlyName string
	// ChannelAddress is the source's address in the owner's own
	// namespace, carried through verbatim for diagnostics
	// ([Endpoint.ChannelAddress]). Empty when the owner has no such
	// notion.
	ChannelAddress string
	// Availability probes the source's live reachability. nil reads as
	// permanently reachable.
	Availability AvailabilityProbe
	// Source is the rich-model implementation of the endpoint's cluster
	// surface. nil for measurement-only endpoints, which carry
	// Measurement instead.
	Source contract.EndpointSource
	// Measurement is set on sensor endpoints assembled from a
	// measurement source. nil otherwise.
	Measurement contract.MeasurementSource
	// NodeLabel is a BridgedDeviceBasicInformation NodeLabel a controller
	// wrote in an earlier run and the host persisted through
	// [Config.OnNodeLabelWritten]. Empty serves FriendlyName until a
	// controller writes one.
	NodeLabel string
	// ConfigurationVersion is the bridged device's
	// BridgedDeviceBasicInformation ConfigurationVersion as the host last
	// persisted it ([Endpoint.IncreaseConfigurationVersion] returns each new
	// value). Zero serves 1. The version may never decrease, so a host that
	// raises it keeps it across restarts — matter.js persists it as state.
	ConfigurationVersion uint32
	// PowerSource carries a battery reading to be served by the
	// PowerSource cluster (0x002F) on this endpoint. At most one
	// endpoint per physical device sets it — see the assembly's
	// power-source placement rule.
	PowerSource contract.MeasurementSource
	// DeviceConditions are the device-type conditions the host states for
	// the endpoint ([Endpoint.DeviceConditions]); nil states none.
	DeviceConditions []string
	// Parts are the endpoint's child endpoints — matter.js Endpoint
	// `parts` — such as the PowerSource (0x0011) component a SmokeCoAlarm
	// requires (matter.js examples/device-smoke-co-alarm). Each part is an
	// endpoint of its own: its StableKey (unique like any other) persists
	// its number, it sits in this endpoint's Descriptor PartsList and in
	// the Aggregator's full-family one, and its ParentEndpoint is this
	// endpoint. A part is a component, not a bridged node: its Descriptor
	// lists its DeviceType alone, it serves no BridgedDeviceBasicInformation,
	// and Identify only when its device type mandates it. Parts may carry
	// parts; FriendlyName, VendorName, NodeLabel and ConfigurationVersion
	// of a part are not served.
	Parts []Spec
}

// ComposeNodeLabel appends the parameter suffix to the base label and
// caps the result at the Matter NodeLabel maximum. Over-long inputs
// lose the suffix first, then the tail of the base label.
//
// Exported for the owner that fills [Spec.FriendlyName]: the suffix
// convention and the 32-byte cap are Matter's, so a caller composing a
// label must not re-derive either of them.
func ComposeNodeLabel(base, suffix string) string {
	if suffix != "" {
		base = strings.TrimSpace(base + " (" + suffix + ")")
	}
	return truncateLabel(base)
}

// labelMaxBytes is the Matter maximum length of the
// BridgedDeviceBasicInformation strings the assembly fills — NodeLabel and
// VendorName, both "max 32" — 32 utf-8 BYTES, not 32 codepoints (Matter
// Core §9.13.6.5).
const labelMaxBytes = 32

// truncateLabel caps s at [labelMaxBytes], snapping back to a rune
// boundary so a multi-byte codepoint is never cut in half.
func truncateLabel(s string) string {
	if len(s) <= labelMaxBytes {
		return s
	}
	cut := labelMaxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 { //nolint:revive // continuation byte
		cut--
	}
	return s[:cut]
}
