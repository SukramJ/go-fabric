// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/contract"
)

// Global attribute ids every server answers (Matter Core §7.13).
const (
	AttrGeneratedCommandList uint32 = 0xFFF8
	AttrAcceptedCommandList  uint32 = 0xFFF9
	AttrEventList            uint32 = 0xFFFA
	AttrAttributeList        uint32 = 0xFFFB
	AttrFeatureMap           uint32 = 0xFFFC
	AttrClusterRevision      uint32 = 0xFFFD
)

// Configuration errors [New] reports; each wraps one of these.
var (
	// ErrNoDefinition: New was handed a nil definition.
	ErrNoDefinition = errors.New("spec: no cluster definition")
	// ErrUnknownFeature: a FeatureMap bit the cluster does not define.
	ErrUnknownFeature = errors.New("spec: unknown FeatureMap bits")
	// ErrFeatureSelection: the feature selection violates a feature's
	// conformance ("O.a+" with none of its group, "X", "[LEV]" without
	// LEV, a mandatory feature left out).
	ErrFeatureSelection = errors.New("spec: illegal feature selection")
	// ErrUnknownElement: a declared attribute, command or event the
	// cluster does not define (or a response command declared as
	// accepted).
	ErrUnknownElement = errors.New("spec: element not defined by the cluster")
	// ErrDisallowed: a declared element whose conformance disallows it for
	// the feature selection.
	ErrDisallowed = errors.New("spec: element disallowed by its conformance")
)

// Options is what the host declares beyond the definition.
type Options struct {
	// Features is the FeatureMap.
	Features uint32
	// Attributes lists the optional attributes the server serves; the
	// mandatory ones (for the feature selection) are served regardless.
	Attributes []uint32
	// Commands lists the optional request commands the server accepts.
	Commands []uint32
	// Events lists the optional events the server emits.
	Events []uint32
}

// Instance is a definition bound to a feature selection and the optional
// elements a server serves: matter.js's ValidatedElements for one
// behavior. Its methods carry the names of the contract interfaces they
// answer, so a server can embed it.
type Instance struct {
	def       *Cluster
	features  uint32
	attrs     []uint32
	accepted  []uint32
	required  []uint32
	generated []uint32
	events    []uint32
}

// featureContext answers conformance names from a definition's features
// and a FeatureMap.
type featureContext struct {
	def      *Cluster
	features uint32
}

func (f featureContext) Defined(name string) bool { return f.feature(name) != nil }

func (f featureContext) Supported(name string) bool {
	ft := f.feature(name)
	return ft != nil && f.features&ft.Mask() != 0
}

func (f featureContext) feature(name string) *Feature {
	for i := range f.def.Features {
		if f.def.Features[i].Name == name {
			return &f.def.Features[i]
		}
	}
	return nil
}

// Revision implements [RevisionContext]: the definition's revision.
func (f featureContext) Revision() uint16 { return f.def.Revision }

// featuresOnly hides the revision of a context, so a revision condition
// stays conditional ([RevisionContext]).
type featuresOnly struct{ FeatureContext }

// Context returns the conformance context of a feature selection. It is a
// [RevisionContext]: a "Rev >= vN" condition is decided against the
// definition's revision.
func Context(def *Cluster, features uint32) FeatureContext {
	return featureContext{def: def, features: features}
}

// CheckFeatures checks a FeatureMap against the definition: every bit must
// be a defined feature, and the selection must satisfy every feature's
// conformance, as matter.js's FeatureSelectionErrors judges a selection
// (packages/model/src/logic/cluster-variance/FeatureSelectionErrors.ts).
func CheckFeatures(def *Cluster, features uint32) error {
	if extra := features &^ def.FeatureMask(); extra != 0 {
		return fmt.Errorf("%w: %s 0x%X", ErrUnknownFeature, def.Name, extra)
	}
	// A feature selection is judged on features alone: matter.js's
	// IllegalFeatureCombinations lets a revision condition gate nothing
	// ("Revision gates presence on the cluster revision rather than on
	// features", IllegalFeatureCombinations.ts:441-443; an optionalIf on a
	// revision disallows nothing, :464-468).
	ctx := featuresOnly{Context(def, features)}
	type group struct {
		choice   *Choice
		selected []string
		members  []string
	}
	groups := map[string]*group{}
	var order []string
	for fi := range def.Features {
		f := &def.Features[fi]
		selected := features&f.Mask() != 0
		switch featureApplicability(f.Conformance, ctx) {
		case ApplicabilityNone:
			if selected {
				return fmt.Errorf("%w: %s feature %s is not allowed", ErrFeatureSelection, def.Name, f.Title)
			}
		case ApplicabilityMandatory:
			if !selected {
				return fmt.Errorf("%w: %s feature %s is mandatory", ErrFeatureSelection, def.Name, f.Title)
			}
		default:
		}
		ch, expr, ok := f.Conformance.choiceOf()
		if !ok || expr.Applicability(ctx) == ApplicabilityNone {
			continue
		}
		g := groups[ch.Name]
		if g == nil {
			g = &group{choice: ch}
			groups[ch.Name] = g
			order = append(order, ch.Name)
		}
		g.members = append(g.members, f.Title)
		if selected {
			g.selected = append(g.selected, f.Title)
		}
	}
	for _, name := range order {
		g := groups[name]
		n := len(g.selected)
		switch {
		case n < g.choice.Num && !g.choice.OrLess:
			return fmt.Errorf("%w: %s needs at least %d of %s", ErrFeatureSelection, def.Name, g.choice.Num, strings.Join(g.members, ", "))
		case n > g.choice.Num && !g.choice.OrMore:
			return fmt.Errorf("%w: %s allows at most %d of %s", ErrFeatureSelection, def.Name, g.choice.Num, strings.Join(g.members, ", "))
		}
	}
	return nil
}

// featureApplicability judges one feature's conformance for CheckFeatures
// the way matter.js judges a feature selection: in an otherwise list "the
// first applicable entry governs, so an entry's rules hold only where every
// earlier entry is inapplicable" (IllegalFeatureCombinations.ts
// addOtherwiseRules :388-390, behind FeatureSelectionErrors). Applicability
// mirrors Conformance.ts computeApplicability instead, where a later
// mandatory term outranks an earlier conditional one — right for
// attributes, commands and events (ValidatedElements), wrong for features:
// DeviceEnergyManagement's PFR "[!PA].a, STA | PAU | FA | CON, O" would
// make PowerForecastReporting mandatory next to StateForecastReporting
// whenever STA, PAU, FA or CON is set, a selection matter.js accepts and
// chip's TC-DEM-2.6 runs (--featureSet 0x7C).
func featureApplicability(c Conformance, ctx FeatureContext) Applicability {
	if c.Op != ConfOtherwise {
		return assessOuter(c, ctx)
	}
	provisional := false
	for i, node := range c.Args {
		if node.Op == ConfProvisional {
			provisional = true
			continue
		}
		// A trailing "D" announces a future deprecation; the terms before
		// it decide.
		if node.Op == ConfDeprecated && i == len(c.Args)-1 {
			continue
		}
		applies, demands := featureEntry(node, ctx)
		if applies == ApplicabilityNone {
			continue // inapplicable here; a later entry may govern
		}
		if applies == ApplicabilityConditional {
			return ApplicabilityConditional
		}
		if demands == ApplicabilityMandatory && provisional {
			return ApplicabilityOptional
		}
		return demands
	}
	// Where no entry governs the feature is not applicable at all.
	return ApplicabilityNone
}

// featureEntry splits one otherwise-list entry into whether it applies to
// the selection (Mandatory: yes, None: no, Conditional: not decidable from
// features) and what it demands of the feature when it does. A bracketed
// condition applies where its expression holds and makes the feature
// optional (its choice group is counted by CheckFeatures); a bare feature
// expression applies where it holds and mandates the feature; M, O, X and
// the like always apply.
func featureEntry(node Conformance, ctx FeatureContext) (applies, demands Applicability) {
	switch node.Op {
	case ConfChoice:
		return featureEntry(arg(node), ctx)
	case ConfOptionalIf:
		return assessInner(arg(node), ctx), ApplicabilityOptional
	case ConfOptional, ConfEmpty, "", ConfProvisional:
		return ApplicabilityMandatory, ApplicabilityOptional
	case ConfMandatory:
		return ApplicabilityMandatory, ApplicabilityMandatory
	case ConfDisallowed, ConfDeprecated, ConfObsolete:
		return ApplicabilityMandatory, ApplicabilityNone
	case ConfDesc, ConfRevision:
		return ApplicabilityConditional, ApplicabilityConditional
	default:
		return assessInner(node, ctx), ApplicabilityMandatory
	}
}

// New binds def to a feature selection and the optional elements the
// server serves. An element is present when its conformance makes it
// mandatory for the selection, or optional (or conditional on something
// other than features) and declared — the rule ValidatedElements applies
// to a behavior's state and command implementations. Declaring an element
// the selection disallows, or one the cluster does not define, is an
// error rather than a silent drop: the host asked to serve something the
// server will not.
func New(def *Cluster, opts Options) (*Instance, error) {
	if def == nil {
		return nil, ErrNoDefinition
	}
	if err := CheckFeatures(def, opts.Features); err != nil {
		return nil, err
	}
	ctx := Context(def, opts.Features)
	inst := &Instance{def: def, features: opts.Features}

	for _, id := range opts.Attributes {
		if def.Attribute(id) == nil {
			return nil, fmt.Errorf("%w: %s attribute 0x%04X", ErrUnknownElement, def.Name, id)
		}
	}
	for ai := range def.Attributes {
		a := &def.Attributes[ai]
		present, err := resolve(def.Name, "attribute", a.Name, a.Conformance, ctx, slices.Contains(opts.Attributes, a.ID))
		if err != nil {
			return nil, err
		}
		if present {
			inst.attrs = append(inst.attrs, a.ID)
		}
	}

	for _, id := range opts.Commands {
		if def.Command(id, Request) == nil {
			return nil, fmt.Errorf("%w: %s request command 0x%02X", ErrUnknownElement, def.Name, id)
		}
	}
	for ci := range def.Commands {
		c := &def.Commands[ci]
		if c.Direction != Request {
			continue
		}
		present, err := resolve(def.Name, "command", c.Name, c.Conformance, ctx, slices.Contains(opts.Commands, c.ID))
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		inst.accepted = append(inst.accepted, c.ID)
		if c.Conformance.Applicability(ctx) == ApplicabilityMandatory {
			inst.required = append(inst.required, c.ID)
		}
		if resp := def.responseOf(c); resp != nil && !slices.Contains(inst.generated, resp.ID) {
			inst.generated = append(inst.generated, resp.ID)
		}
	}

	for _, id := range opts.Events {
		if def.Event(id) == nil {
			return nil, fmt.Errorf("%w: %s event 0x%02X", ErrUnknownElement, def.Name, id)
		}
	}
	for ei := range def.Events {
		e := &def.Events[ei]
		present, err := resolve(def.Name, "event", e.Name, e.Conformance, ctx, slices.Contains(opts.Events, e.ID))
		if err != nil {
			return nil, err
		}
		if present {
			inst.events = append(inst.events, e.ID)
		}
	}

	for _, l := range [][]uint32{inst.attrs, inst.accepted, inst.generated, inst.events} {
		slices.Sort(l)
	}
	return inst, nil
}

// resolve decides one element's presence.
func resolve(cluster, kind, name string, c Conformance, ctx FeatureContext, declared bool) (bool, error) {
	switch c.Applicability(ctx) {
	case ApplicabilityMandatory:
		return true, nil
	case ApplicabilityNone:
		if declared {
			return false, fmt.Errorf("%w: %s %s %s (%q)", ErrDisallowed, cluster, kind, name, c.Text)
		}
		return false, nil
	default:
		return declared, nil
	}
}

// responseOf returns the response command a request names, nil for a
// status-only one.
func (c *Cluster) responseOf(req *Command) *Command {
	if req.Response == "" || req.Response == "status" {
		return nil
	}
	for i := range c.Commands {
		if r := &c.Commands[i]; r.Name == req.Response && r.Direction == Response {
			return r
		}
	}
	return nil
}

// Definition returns the cluster definition.
func (i *Instance) Definition() *Cluster { return i.def }

// MatterClusterID implements the cluster id half of [contract.ClusterServer].
func (i *Instance) MatterClusterID() uint32 { return i.def.ID }

// FeatureMap returns the FeatureMap.
func (i *Instance) FeatureMap() uint32 { return i.features }

// HasFeature reports whether the feature named name (short name or title)
// is selected.
func (i *Instance) HasFeature(name string) bool {
	f := i.def.Feature(name)
	return f != nil && i.features&f.Mask() != 0
}

// Revision returns the ClusterRevision.
func (i *Instance) Revision() uint16 { return i.def.Revision }

// Context returns the conformance context of the instance's feature
// selection.
func (i *Instance) Context() FeatureContext { return Context(i.def, i.features) }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// served attributes in id order, globals excluded.
func (i *Instance) MatterAttributes() []uint32 { return append([]uint32{}, i.attrs...) }

// Serves reports whether attrID is served.
func (i *Instance) Serves(attrID uint32) bool { return slices.Contains(i.attrs, attrID) }

// MatterReportable lists the served attributes that change: every one but
// those of quality "F" (fixed).
func (i *Instance) MatterReportable() []uint32 {
	out := make([]uint32, 0, len(i.attrs))
	for _, id := range i.attrs {
		if !i.def.Attribute(id).Quality.Fixed {
			out = append(out, id)
		}
	}
	return out
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (i *Instance) MatterAcceptedCommands() []uint32 { return append([]uint32{}, i.accepted...) }

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: the
// responses of the accepted commands.
func (i *Instance) MatterGeneratedCommands() []uint32 { return append([]uint32{}, i.generated...) }

// Accepts reports whether cmdID is an accepted request.
func (i *Instance) Accepts(cmdID uint32) bool { return slices.Contains(i.accepted, cmdID) }

// MatterEvents implements [contract.ClusterEventLister].
func (i *Instance) MatterEvents() []uint32 { return append([]uint32{}, i.events...) }

// Emits reports whether eventID is in EventList.
func (i *Instance) Emits(eventID uint32) bool { return slices.Contains(i.events, eventID) }

// ReadGlobal answers FeatureMap (uint32) and ClusterRevision (uint16); it
// reports false for any other attribute. The dispatcher synthesises the
// four list globals from the lister methods.
func (i *Instance) ReadGlobal(attrID uint32) (any, bool) {
	switch attrID {
	case AttrFeatureMap:
		return i.features, true
	case AttrClusterRevision:
		return i.def.Revision, true
	}
	return nil, false
}

// MinReadPrivilege implements [contract.ClusterAttributeReadPrivilege].
func (i *Instance) MinReadPrivilege(attrID uint32) uint8 {
	if a := i.def.Attribute(attrID); a != nil {
		return uint8(a.Access.ReadPrivilege())
	}
	return uint8(PrivilegeView)
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege].
func (i *Instance) MinWritePrivilege(attrID uint32) uint8 {
	if a := i.def.Attribute(attrID); a != nil {
		return uint8(a.Access.WritePrivilege())
	}
	return uint8(PrivilegeOperate)
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
func (i *Instance) MinInvokePrivilege(cmdID uint32) uint8 {
	if c := i.def.Command(cmdID, Request); c != nil {
		return uint8(c.Access.WritePrivilege())
	}
	return uint8(PrivilegeOperate)
}

// IsTimed reports whether the request command cmdID needs a timed
// interaction (access "T").
func (i *Instance) IsTimed(cmdID uint32) bool {
	c := i.def.Command(cmdID, Request)
	return c != nil && c.Access.Timed
}

// IsTimedCommand implements [contract.TimedCommander] from the definition's
// own model: [Instance.IsTimed]. Every server built on an Instance — a
// [Server], or a hand-written server embedding *Instance — therefore answers
// the IM's timed gate from the generated definition, as matter.js reads
// `limits.timed` from the command's model
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:292-295).
func (i *Instance) IsTimedCommand(cmdID uint32) bool { return i.IsTimed(cmdID) }

// EventPriority returns the priority matter.js declares for eventID; Info
// for an event the cluster does not define.
func (i *Instance) EventPriority(eventID uint32) contract.EventPriority {
	if e := i.def.Event(eventID); e != nil {
		return contract.EventPriority(e.Priority)
	}
	return contract.EventPriorityInfo
}

// EnumSupported reports whether v is a value of e the feature selection
// allows: defined, or inside one of e's ranges, and its conformance not
// disallowed (OperationModeEnum Minimum needs SPD). It is the membership
// check matter.js's enum validator makes against the conformant members
// (ValueValidator.createEnumValidator).
func (i *Instance) EnumSupported(e *Enum, v uint64) bool {
	return EnumSupported(e, v, i.Context())
}

// EnumSupported is [Instance.EnumSupported] for a conformance context.
//
// Mirrors matter.js packages/node/src/behavior/state/validation/ValueValidator.ts:172-178
// (createEnumValidator: a value is valid when it is a member's id or lies in
// a range member's constraint) and conformance-compiler.ts:857-858
// (addEnumMemberValidation: a member's id is looked up first, then the
// range whose constraint holds the value, and that member's conformance
// decides).
func EnumSupported(e *Enum, v uint64, ctx FeatureContext) bool {
	for vi := range e.Values {
		if ev := &e.Values[vi]; ev.Value == v {
			return ev.Conformance.Applicability(ctx) != ApplicabilityNone
		}
	}
	for ri := range e.Ranges {
		if r := &e.Ranges[ri]; r.Contains(v) {
			return r.Conformance.Applicability(ctx) != ApplicabilityNone
		}
	}
	return false
}
