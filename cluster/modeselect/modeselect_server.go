// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package modeselect contains the Matter ModeSelect cluster server
// (0x0050) — the cluster Matter offers for a characteristic that is set
// to one of several predefined, labelled values rather than to a number
// on a continuous scale.
//
// The mode list is host-supplied: a host names its own modes through
// [ModeSource], and this package owns only the Matter projection of
// them. Nothing here knows what a mode means to the device, which is why
// ChangeToMode reaches the host rather than mutating state kept locally.
package modeselect

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"
)

// ClusterID is the ModeSelect cluster identifier.
//
// Mirrors matter.js packages/model/src/standard/elements/mode-select-cluster.element.ts:19.
const ClusterID uint32 = 0x0050

// Attribute IDs.
//
// Mirrors matter.js mode-select-cluster.element.ts:25-50. StartUpMode
// (0x0004, conformance "O") and OnMode (0x0005, conformance "DEPONOFF")
// are named for completeness; this server implements neither, which is
// why it advertises an empty FeatureMap — see [featureMap].
const (
	AttrDescription       uint32 = 0x0000
	AttrStandardNamespace uint32 = 0x0001
	AttrSupportedModes    uint32 = 0x0002
	AttrCurrentMode       uint32 = 0x0003
	AttrStartUpMode       uint32 = 0x0004
	AttrOnMode            uint32 = 0x0005
)

// CmdChangeToMode is the only command the cluster defines
// (conformance M).
//
// Mirrors matter.js mode-select-cluster.element.ts:51-54.
const CmdChangeToMode uint32 = 0x00

// featureMap is what this server advertises: nothing.
//
// The cluster defines one feature bit, DEPONOFF at constraint "0" → bit
// 0 (mode-select-cluster.element.ts:21-24), and this server leaves it
// clear. DEPONOFF makes OnMode mandatory (conformance "DEPONOFF",
// mode-select-cluster.element.ts:47-50) and couples the cluster to an
// OnOff cluster on the same endpoint. This server implements neither
// OnMode nor StartUpMode, so advertising the bit would promise an
// attribute that is not there. There is deliberately no knob to set it.
const featureMap uint32 = 0

// DescriptionMaxBytes is the Description constraint "max 64"
// (matter.js mode-select-cluster.element.ts:25-28). The bound is applied
// at encode time rather than trusted from the host, because a host
// string longer than the constraint is a wire violation a controller
// sees before anyone reads the host's code.
const DescriptionMaxBytes = 64

// ModeOptionStruct field tags, and the ModeOptionStruct constraints.
//
// Mirrors matter.js mode-select-cluster.element.ts:61-69: Label 0x0
// string "M" max 64, Mode 0x1 uint8 "M", SemanticTags 0x2 list "M"
// max 64. All three carry quality F.
const (
	ModeOptionFieldLabel        uint8 = 0x00
	ModeOptionFieldMode         uint8 = 0x01
	ModeOptionFieldSemanticTags uint8 = 0x02

	// LabelMaxBytes is the ModeOptionStruct Label constraint "max 64"
	// (mode-select-cluster.element.ts:63).
	LabelMaxBytes = 64

	// SemanticTagsMaxEntries is the ModeOptionStruct SemanticTags
	// constraint "max 64" (mode-select-cluster.element.ts:66), in list
	// entries.
	SemanticTagsMaxEntries = 64
)

// SupportedModesMaxEntries is the SupportedModes constraint "max 255"
// (matter.js mode-select-cluster.element.ts:37), in list entries. Like
// Label's byte bound it is applied at encode time rather than trusted
// from the host: a list longer than the constraint is a wire violation
// a controller sees before anyone reads the host's code.
const SupportedModesMaxEntries = 255

// SemanticTagStruct field tags.
//
// Mirrors matter.js mode-select-cluster.element.ts:55-59: MfgCode 0x0
// vendor-id "M", Value 0x1 uint16 "M". vendor-id is uint16
// (matter.js vendor-id.element.ts:12).
const (
	SemanticTagFieldMfgCode uint8 = 0x00
	SemanticTagFieldValue   uint8 = 0x01
)

// ChangeToModeFieldNewMode is the context tag of the ChangeToMode
// NewMode field (id 0x0, uint8, conformance M).
//
// Mirrors matter.js mode-select-cluster.element.ts:53.
const ChangeToModeFieldNewMode uint8 = 0x00

// SemanticTagStruct is one semantic tag on a mode: a value within a
// namespace, either a standard namespace (MfgCode absent from the host's
// point of view) or the vendor's own.
//
// Mirrors matter.js mode-select-cluster.element.ts:55-59.
type SemanticTagStruct struct {
	// MfgCode is the vendor id owning the namespace (vendor-id → uint16).
	MfgCode uint16
	// Value is the tag value within that namespace.
	Value uint16
}

// ModeOptionStruct is one selectable mode: the label a user picks from,
// the mode value ChangeToMode carries, and the semantic tags a client
// can interpret without reading the label.
//
// Mirrors matter.js mode-select-cluster.element.ts:61-69. SemanticTags
// is conformance M, so an option with no tags encodes as an empty list,
// never as an absent field — the list being empty is what says "this
// mode is anonymous".
type ModeOptionStruct struct {
	Label        string
	Mode         uint8
	SemanticTags []SemanticTagStruct
}

// ChangeToModeRequest is the cluster-native payload of ChangeToMode.
type ChangeToModeRequest struct {
	NewMode uint8
}

// ModeSource is the narrow host port this server projects.
//
// The host owns every value: the mode list, the current mode, and what
// changing the mode does to the device. The server owns the Matter
// shape of those values and the one rule the spec puts on them — a
// ChangeToMode naming a mode outside SupportedModes is refused.
//
// CurrentMode has no "not observed yet" reading. The attribute is
// conformance M and carries no X quality
// (mode-select-cluster.element.ts:42), so there is no null to report;
// the host names the value a controller should see before it has heard
// from the device.
//
// ChangeToMode owns the southbound urgency of the write it performs:
// the cluster contract carries no priority, so a host whose command
// queue ranks by urgency names the value it wants inside ChangeToMode,
// where the device vocabulary is already in scope.
type ModeSource interface {
	// ModeDescription reports what this cluster instance selects
	// between, in readable text — a coffee machine's "Milk" as against
	// its "Sugar" instance.
	ModeDescription() string
	// ModeNamespace reports the StandardNamespace, and whether there is
	// one. present=false encodes as null: no standard namespace, and so
	// no standard semantic tags in this instance.
	ModeNamespace() (namespace uint8, present bool)
	// SupportedModes reports the selectable modes. Each entry needs a
	// distinct Mode value.
	SupportedModes() []ModeOptionStruct
	// CurrentMode reports the mode the device is in.
	CurrentMode() uint8
	// ChangeToMode applies a mode the server has already checked against
	// SupportedModes.
	ChangeToMode(ctx context.Context, newMode uint8) error
}

// Config carries the construction parameters for [Server].
type Config struct {
	// Source supplies the mode list and takes ChangeToMode. A nil
	// Source makes every attribute read null and ChangeToMode fail —
	// the server never invents a mode for a device it cannot reach.
	Source ModeSource
	// DataVersion is an optional tracker owned by the host. When
	// non-nil the server bumps and reports the caller's counter, so a
	// mode the device changed on its own reaches a subscriber the
	// moment the host bumps it, and the version survives server
	// reconstruction; when nil an embedded tracker is used.
	DataVersion *cluster.DataVersionTracker
}

// Server implements [contract.ClusterServer] for the Matter ModeSelect
// cluster (0x0050).
type Server struct {
	src ModeSource

	embedded cluster.DataVersionTracker // used when Config.DataVersion is nil
	ext      *cluster.DataVersionTracker
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*Server)(nil)
	_ contract.ClusterDataVersion     = (*Server)(nil)
	_ contract.ClusterAttributeLister = (*Server)(nil)
	_ contract.ClusterCommandLister   = (*Server)(nil)
	_ contract.ChangeNotifier         = (*Server)(nil)
)

// NewServer constructs a ModeSelect server over cfg.Source.
func NewServer(cfg Config) *Server { return &Server{src: cfg.Source, ext: cfg.DataVersion} }

// tracker returns the active DataVersion counter — the host's when it
// supplied one, the embedded one otherwise.
func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// OnMatterValueChanged implements [contract.ChangeNotifier] by
// forwarding the host port's own notifier, bumping the DataVersion
// before each fire so the report the bridge ships carries the
// post-change version. CurrentMode carries quality N
// (mode-select-cluster.element.ts:42) and is the one attribute this
// server reports; without this hop a mode the device changed by itself
// would reach a controller only on its next read. Mirrors the
// forwarding in cluster/measurement/measurement.go
// (ElectricalPowerServer.OnMatterValueChanged). Returns a no-op
// unsubscribe when the port cannot notify.
func (s *Server) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil {
		return func() {}
	}
	return n.OnMatterValueChanged(func() {
		s.tracker().Bump()
		if cb != nil {
			cb()
		}
	})
}

// Revision returns the cluster revision from the generated matter.js
// schema snapshot (default 2 at mode-select-cluster.element.ts:20).
// Reading it rather than restating it is the point: a regeneration moves
// this value, and a hand-written copy would not follow.
func Revision() uint16 { return schema.ClusterRevisions[ClusterID] }

// MatterClusterID returns 0x0050 (ModeSelect).
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves the four conformance-M attributes plus the two
// universal globals every cluster server answers itself.
//
// With no host port the four projected attributes read as TLV null.
// Only StandardNamespace carries quality X, so null is not the reading
// the spec asks for on the other three — but the alternative is a
// fabricated Description or a CurrentMode the device never reported,
// and a value invented here is indistinguishable on the wire from one
// the device confirmed.
func (s *Server) MatterRead(attrID uint32) (value any, ok bool) {
	switch attrID {
	case AttrDescription, AttrStandardNamespace, AttrSupportedModes, AttrCurrentMode:
		if s.src == nil {
			return nil, true
		}
		return s.readFromSource(attrID)
	case cluster.AttrGlobalFeatureMap:
		return featureMap, true
	case cluster.AttrGlobalClusterRevision:
		return Revision(), true
	default:
		return nil, false
	}
}

// readFromSource projects one host-reported value onto the wire types
// the encoder expects. Only reached with a non-nil port.
func (s *Server) readFromSource(attrID uint32) (any, bool) {
	switch attrID {
	case AttrDescription:
		// BoundedString so the encoder applies the spec's "max 64" at
		// encode time instead of trusting the host's string length.
		return tlv.BoundedString{Value: s.src.ModeDescription(), MaxBytes: DescriptionMaxBytes}, true
	case AttrStandardNamespace:
		// Quality "X F", default null (element :29-32). The datatype
		// `namespace` is enum8 (matter.js namespace.element.ts:13).
		ns, present := s.src.ModeNamespace()
		if !present {
			return nil, true
		}
		return ns, true
	case AttrSupportedModes:
		return copyModes(s.src.SupportedModes()), true
	case AttrCurrentMode:
		return s.src.CurrentMode(), true
	default:
		return nil, false
	}
}

// copyModes deep-copies the host's mode list, including each option's
// semantic tags. The caller must not be able to mutate host state
// through a value it read.
func copyModes(in []ModeOptionStruct) []ModeOptionStruct {
	out := make([]ModeOptionStruct, len(in))
	for i, opt := range in {
		out[i] = opt
		out[i].SemanticTags = append([]SemanticTagStruct(nil), opt.SemanticTags...)
	}
	return out
}

// unsupportedWriteErr is a typed [im.StatusCodeError] for a write to an
// attribute this cluster exposes read-only.
type unsupportedWriteErr struct{ msg string }

func (e unsupportedWriteErr) Error() string                 { return e.msg }
func (unsupportedWriteErr) MatterStatusCode() im.StatusCode { return im.StatusUnsupportedWrite }

// unsupportedAttributeErr is a typed [im.StatusCodeError] for a write to
// an attribute this server does not implement at all.
type unsupportedAttributeErr struct{ msg string }

func (e unsupportedAttributeErr) Error() string                 { return e.msg }
func (unsupportedAttributeErr) MatterStatusCode() im.StatusCode { return im.StatusUnsupportedAttribute }

// invalidCommandErr is a typed [im.StatusCodeError] for a ChangeToMode
// naming a mode outside SupportedModes.
type invalidCommandErr struct{ msg string }

func (e invalidCommandErr) Error() string                 { return e.msg }
func (invalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

var (
	_ im.StatusCodeError = unsupportedWriteErr{}
	_ im.StatusCodeError = unsupportedAttributeErr{}
	_ im.StatusCodeError = invalidCommandErr{}
)

// MatterWrite reports that nothing on this server is writable.
//
// The four implemented attributes all carry access "R V"
// (mode-select-cluster.element.ts:25-42); the two writable ones,
// StartUpMode and OnMode ("RW VO", :43-50), belong to surfaces this
// server does not implement, so a write to them is UnsupportedAttribute
// rather than UnsupportedWrite — the attribute is absent, not
// read-only.
func (*Server) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	switch attrID {
	case AttrDescription, AttrStandardNamespace, AttrSupportedModes, AttrCurrentMode:
		return unsupportedWriteErr{fmt.Sprintf("modeselect: attribute 0x%04X is read-only", attrID)}
	default:
		return unsupportedAttributeErr{
			fmt.Sprintf("modeselect: attribute 0x%04X is not implemented by this server", attrID),
		}
	}
}

// MatterInvoke handles ChangeToMode.
//
// A NewMode outside SupportedModes is refused with INVALID_COMMAND, not
// ConstraintError: matter.js raises Status.InvalidCommand from
// ModeSelectServer.ts:88-95 (#assertModeValue), and the spec text is
// explicit — "otherwise, the server shall respond with an
// INVALID_COMMAND status response" (matter.js
// packages/model/src/standard/resources/mode-select-cluster.resource.ts,
// ChangeToMode details).
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (response any, err error) {
	if cmdID != CmdChangeToMode {
		return nil, im.UnsupportedCommandf("modeselect: unknown command 0x%02X", cmdID)
	}
	req, err := changeToModeRequest(fields)
	if err != nil {
		return nil, err
	}
	if s.src == nil {
		return nil, errNoSource("ChangeToMode")
	}
	if !s.isSupported(req.NewMode) {
		return nil, invalidCommandErr{
			fmt.Sprintf("modeselect: mode %d provided in NewMode is not supported", req.NewMode),
		}
	}
	if err := s.src.ChangeToMode(ctx, req.NewMode); err != nil {
		// Deliberately no version bump: the device refused, so nothing
		// a subscriber caches has changed.
		return nil, fmt.Errorf("modeselect: ChangeToMode: %w", err)
	}
	s.tracker().Bump()
	return nil, nil
}

// errNoSource reports that the server has no host port to reach.
func errNoSource(what string) error {
	return fmt.Errorf("modeselect: %s has no host port", what)
}

// isSupported reports whether mode is one of the host's SupportedModes.
func (s *Server) isSupported(mode uint8) bool {
	for _, opt := range s.src.SupportedModes() {
		if opt.Mode == mode {
			return true
		}
	}
	return false
}

// changeToModeRequest normalises the decoded command payload the bridge
// hands over. The bridge has no typed decoder for this cluster, so the
// real wire path arrives as the generic tag map its fields reader
// salvages; the typed shapes are accepted so a host or a test can invoke
// the server directly.
func changeToModeRequest(fields any) (ChangeToModeRequest, error) {
	switch v := fields.(type) {
	case ChangeToModeRequest:
		return v, nil
	case *ChangeToModeRequest:
		if v == nil {
			return ChangeToModeRequest{}, invalidCommandErr{"modeselect: ChangeToMode carried no fields"}
		}
		return *v, nil
	case map[uint8]any:
		raw, ok := v[ChangeToModeFieldNewMode]
		if !ok {
			return ChangeToModeRequest{}, invalidCommandErr{
				"modeselect: ChangeToMode is missing the mandatory NewMode field",
			}
		}
		mode, ok := cluster.AsUint8(raw)
		if !ok {
			return ChangeToModeRequest{}, invalidCommandErr{
				fmt.Sprintf("modeselect: ChangeToMode NewMode is %T, want a uint8", raw),
			}
		}
		return ChangeToModeRequest{NewMode: mode}, nil
	default:
		return ChangeToModeRequest{}, invalidCommandErr{
			fmt.Sprintf("modeselect: ChangeToMode expected ChangeToModeRequest or map[uint8]any, got %T", fields),
		}
	}
}

// MatterReportable lists the attributes that change at runtime.
// Description, StandardNamespace and SupportedModes all carry quality F
// (fixed, element :25-40) and never report; CurrentMode carries quality
// N (:42) and does.
func (*Server) MatterReportable() []uint32 { return []uint32{AttrCurrentMode} }

// MatterAttributes implements [contract.ClusterAttributeLister], in
// attribute-ID order.
func (*Server) MatterAttributes() []uint32 {
	return []uint32{AttrDescription, AttrStandardNamespace, AttrSupportedModes, AttrCurrentMode}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (*Server) MatterAcceptedCommands() []uint32 { return []uint32{CmdChangeToMode} }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// ChangeToMode has response "status" (element :52), so the server emits
// no command of its own.
func (*Server) MatterGeneratedCommands() []uint32 { return nil }
