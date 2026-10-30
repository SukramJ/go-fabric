// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"errors"
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
)

// Level is a specification level ([schema.Level]): a Matter revision
// matter.js has shipped a release for. [Policies].Level selects one; ""
// is the pin's.
type Level = schema.Level

// ErrUnknownLevel is returned by [Policies.Validate] for a level
// [schema.Levels] does not list.
var ErrUnknownLevel = errors.New("surface: unknown specification level")

// ErrLevelAbovePin is returned by [Policies.Validate] for a level above
// the pin's revision. [schema.Levels] lists none; a host that passes an
// operator's string gets it for "1.7".
var ErrLevelAbovePin = errors.New("surface: specification level above the pin")

// ErrWithheld is wrapped by the [*WithheldError] a composition returns
// when the specification level does not know the device's type or a
// cluster the type mandates: the device is not composed, its report is
// (concept v11 §C.3 — an operator's setting withholds, it never fails the
// node).
var ErrWithheld = errors.New("surface: device withheld by the specification level")

// WithheldError names the endpoint a level withheld and the elements the
// level does not know: the device type, the mandated clusters.
type WithheldError struct {
	Endpoint string
	Elements []Element
	// Report is the endpoint's report: the policies' digest, the
	// decisions so far and the withheld elements; the effective surface
	// is empty.
	Report Report
}

// Error names the endpoint, the level and each element.
func (e *WithheldError) Error() string {
	parts := make([]string, 0, len(e.Elements))
	for _, el := range e.Elements {
		parts = append(parts, el.String())
	}
	level := ""
	if e.Report.Level != "" {
		level = " " + string(e.Report.Level)
	}
	return fmt.Sprintf("%s: endpoint %q: level%s does not know %s", ErrWithheld, e.Endpoint, level, strings.Join(parts, ", "))
}

// Unwrap returns [ErrWithheld].
func (e *WithheldError) Unwrap() error { return ErrWithheld }

// PinLevel is the pin's level: the last of [schema.Levels].
func PinLevel() Level {
	ls := schema.Levels()
	return ls[len(ls)-1]
}

// EffectiveLevel is the level p composes at: [Policies].Level, the pin's
// when it is "".
func (p Policies) EffectiveLevel() Level {
	if p.Level == "" {
		return PinLevel()
	}
	return p.Level
}

// Validate rejects a level [schema.Levels] does not list:
// [ErrLevelAbovePin] for one above the pin, [ErrUnknownLevel] otherwise.
// device.New and node.New call it first.
func (p Policies) Validate() error {
	if p.Level == "" || slices.Contains(schema.Levels(), p.Level) {
		return nil
	}
	names := make([]string, 0, len(schema.Levels()))
	for _, l := range schema.Levels() {
		names = append(names, string(l))
	}
	known := strings.Join(names, ", ")
	if above(p.Level, PinLevel()) {
		return fmt.Errorf("%w: %q (the pin is %s; levels: %s)", ErrLevelAbovePin, p.Level, PinLevel(), known)
	}
	return fmt.Errorf("%w: %q (levels: %s)", ErrUnknownLevel, p.Level, known)
}

// above reports whether a parses as a dotted revision above b.
func above(a, b Level) bool {
	x, okA := dotted(a)
	y, okB := dotted(b)
	return okA && okB && slices.Compare(x, y) > 0
}

func dotted(l Level) ([]int, bool) {
	parts := strings.Split(string(l), ".")
	out := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// levelPolicyName is the built-in level policy's name in a [Decision].
const levelPolicyName = "level"

// The level policy's reasons.
const (
	// ReasonFeatureSetInvalid keeps a feature the level does not know
	// because the cluster's remaining selection fails its conformance at
	// the pin without it.
	ReasonFeatureSetInvalid = "kept: feature selection invalid at pin without it"
	// ReasonSurfaceChangeSignal keeps BridgedDeviceBasicInformation's
	// ConfigurationVersion at every level (concept v11 §D.9).
	ReasonSurfaceChangeSignal = "kept: surface-change signal (concept v11 D.9)"
	// ReasonServerInternal keeps an effective element the level does not
	// know that no candidate decided: a hand-written server's own choice.
	ReasonServerInternal = "kept: server-internal, not level-controlled"
)

// configurationVersion is BridgedDeviceBasicInformation (0x0039)'s
// ConfigurationVersion (cluster/spec/bridgeddevicebasicinformation
// AttrConfigurationVersion, 0x0018).
const (
	bdbiCluster          uint32 = 0x0039
	configurationVersion uint32 = 0x0018
)

// absentReason is the level policy's reason for an element the level
// does not know; the chain rewrites it for a mandatory element to
// "kept: mandatory at pin (absent at level <L>)".
func absentReason(l Level) string { return "absent at level " + string(l) }

// withheldReason is the reason of the keep decision on an element that
// withholds its endpoint.
func withheldReason(l Level) string { return "withheld: absent at level " + string(l) }

// clusterKey is one cluster of one endpoint.
type clusterKey struct {
	endpoint string
	cluster  uint32
}

// levelPolicy is the built-in policy that composes at a specification
// level: it removes the optional surface the level does not know, keeps
// what the pin mandates, and withholds an endpoint whose device type or
// mandated cluster the level does not know — never the root (Endpoint
// ""), which keeps such an element as mandatory at the pin. [Policies.chain] builds it
// over the candidates, so it judges each cluster's features as a set.
type levelPolicy struct {
	level Level
	// pin: the level is the pin's; the policy keeps everything silently.
	pin bool
	// readd are the features re-added so the selection stays valid at
	// the pin (rule 4).
	readd map[Element]bool
	// withheld are the elements that withhold their endpoint, in
	// candidate order.
	withheld []Element
}

func (*levelPolicy) Name() string { return levelPolicyName }

// newLevelPolicy precomputes the level's set decisions over candidates.
func newLevelPolicy(level Level, candidates []Element) *levelPolicy {
	p := &levelPolicy{level: level, pin: level == PinLevel(), readd: map[Element]bool{}}
	if p.pin {
		return p
	}
	var order []clusterKey
	features := map[clusterKey][]Element{}
	clusters := map[clusterKey]Element{}
	for _, el := range candidates {
		k := clusterKey{el.Endpoint, el.Cluster}
		switch el.Kind {
		case KindDeviceType:
			if el.Endpoint != "" && el.Levels != 0 && !el.Levels.Has(level) {
				p.withheld = append(p.withheld, el)
			}
		case KindCluster:
			clusters[k] = el
			if el.Endpoint != "" && el.Mandatory && el.Levels != 0 && !el.Levels.Has(level) {
				p.withheld = append(p.withheld, el)
			}
		case KindFeature:
			if _, ok := features[k]; !ok {
				order = append(order, k)
			}
			features[k] = append(features[k], el)
		default:
		}
	}
	for _, k := range order {
		p.checkFeatureSet(k, features[k], clusters)
	}
	return p
}

// checkFeatureSet re-adds removed features of one cluster, smallest bit
// first, until the remaining selection passes spec.CheckFeatures (rule
// 4); when none does, the cluster withholds its endpoint.
func (p *levelPolicy) checkFeatureSet(k clusterKey, feats []Element, clusters map[clusterKey]Element) {
	def := spec.Lookup(k.cluster)
	if def == nil {
		return
	}
	var selection uint32
	var removed []Element
	for _, f := range feats {
		selection |= f.ID
		if !f.Mandatory && f.Levels != 0 && !f.Levels.Has(p.level) {
			removed = append(removed, f)
		}
	}
	if len(removed) == 0 {
		return
	}
	slices.SortFunc(removed, func(a, b Element) int { return int(int64(a.ID) - int64(b.ID)) })
	remaining := selection
	for _, f := range removed {
		remaining &^= f.ID
	}
	for _, f := range removed {
		if spec.CheckFeatures(def, remaining) == nil {
			return
		}
		remaining |= f.ID
		p.readd[f] = true
	}
	if spec.CheckFeatures(def, remaining) == nil || k.endpoint == "" {
		// The root is never withheld: every feature is back.
		return
	}
	el, ok := clusters[k]
	if !ok {
		el = Element{Endpoint: k.endpoint, Cluster: k.cluster, Kind: KindCluster, ID: k.cluster}
	}
	if !slices.Contains(p.withheld, el) {
		p.withheld = append(p.withheld, el)
	}
}

// Judge answers from the precomputed sets: a known element is kept
// silently; an unknown one that withholds its endpoint is kept with the
// withheld reason, a re-added feature and ConfigurationVersion with
// theirs, and any other is removed (the chain keeps a mandatory one).
func (p *levelPolicy) Judge(el Element) (Verdict, string) {
	if p.pin || el.Levels == 0 || el.Levels.Has(p.level) {
		return Keep, ""
	}
	if slices.Contains(p.withheld, el) {
		return Keep, withheldReason(p.level)
	}
	switch {
	case el.Kind == KindAttribute && el.Cluster == bdbiCluster && el.ID == configurationVersion:
		return Keep, ReasonSurfaceChangeSignal
	case el.Kind == KindFeature && p.readd[el]:
		return Keep, ReasonFeatureSetInvalid
	default:
		return Remove, absentReason(p.level)
	}
}

// ServerInternal returns the level policy's keep decisions on the
// elements of the effective endpoint ep that the level of p does not know
// and no element of decided names: what a hand-written server chose
// itself ([ReasonServerInternal]). It returns none at the pin's level. A
// feature is its one-bit mask; a command id is matched against the
// accepted and the generated list alike.
func (p Policies) ServerInternal(ep Endpoint, decided []Element) []Decision {
	level := p.EffectiveLevel()
	if level == PinLevel() {
		return nil
	}
	type key struct {
		cluster uint32
		kind    Kind
		id      uint32
	}
	known := map[key]bool{}
	for _, el := range decided {
		known[key{el.Cluster, el.Kind, el.ID}] = true
	}
	var out []Decision
	add := func(cluster uint32, kind Kind, id uint32, levels schema.LevelSet) {
		if levels == 0 || levels.Has(level) || known[key{cluster, kind, id}] {
			return
		}
		known[key{cluster, kind, id}] = true
		el := Element{Endpoint: ep.Key, Cluster: cluster, Kind: kind, ID: id, Origin: OriginKitDefault, Levels: levels}
		out = append(out, Decision{Element: el, Policy: levelPolicyName, Verdict: Keep, Reason: ReasonServerInternal})
	}
	for _, c := range ep.Clusters {
		add(c.ID, KindCluster, c.ID, schema.ClusterLevels(c.ID))
		for x := c.FeatureMap; x != 0; x &= x - 1 {
			b := bits.TrailingZeros32(x)
			add(c.ID, KindFeature, uint32(1)<<b, schema.ClusterFeatureLevels(c.ID, uint8(b))) //nolint:gosec // G115: a bit index below 32
		}
		for _, id := range sortedIDs(c.Attributes) {
			add(c.ID, KindAttribute, id, schema.ClusterAttributeLevels(c.ID, id))
		}
		for _, id := range sortedIDs(append(slices.Clone(c.Accepted), c.Generated...)) {
			add(c.ID, KindCommand, id, schema.ClusterCommandLevels(c.ID, id))
		}
		for _, id := range sortedIDs(c.Events) {
			add(c.ID, KindEvent, id, schema.ClusterEventLevels(c.ID, id))
		}
	}
	return out
}

func sortedIDs(ids []uint32) []uint32 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// ElementLevels returns the levels the element exists at, from the
// generated table ([schema.ClusterLevels] and its siblings): what the
// device layer and the node fill [Element].Levels with.
func ElementLevels(el Element) schema.LevelSet {
	switch el.Kind {
	case KindDeviceType:
		return schema.DeviceTypeLevels(schema.DeviceTypeID(el.ID)) //nolint:gosec // G115: a device type id is 16-bit
	case KindCluster:
		return schema.ClusterLevels(el.ID)
	case KindFeature:
		if el.ID == 0 {
			return 0
		}
		return schema.ClusterFeatureLevels(el.Cluster, uint8(bits.TrailingZeros32(el.ID))) //nolint:gosec // G115: a bit index below 32
	case KindAttribute:
		return schema.ClusterAttributeLevels(el.Cluster, el.ID)
	case KindCommand:
		return schema.ClusterCommandLevels(el.Cluster, el.ID)
	case KindEvent:
		return schema.ClusterEventLevels(el.Cluster, el.ID)
	default:
		return 0
	}
}
