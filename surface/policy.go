// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Verdict is a policy's judgement of one element.
type Verdict uint8

// Verdicts, in ascending precedence: when several policies judge an
// element, Refuse wins over Remove, Remove over Warn, Warn over Keep.
const (
	// Keep serves the element; the zero value.
	Keep Verdict = iota
	// Warn serves the element and flags it: the policy has evidence
	// against it that does not remove it — a controller profile's row of
	// a confidence below Reproduced. The element stays in the surface; the
	// decision is always recorded, so a reader counts warnings by this
	// verdict and never by the reason's text.
	Warn
	// Remove composes without the element: a choice the code could not
	// foresee, an operator's setting. A mandatory element is kept.
	Remove
	// Refuse fails the composition: two decisions of the same developer
	// contradict each other.
	Refuse
)

var verdictNames = map[Verdict]string{Keep: "keep", Warn: "warn", Remove: "remove", Refuse: "refuse"}

// String returns the verdict's name ("keep", "warn", "remove", "refuse").
func (v Verdict) String() string { return enumName(verdictNames, v) }

// MarshalText implements [encoding.TextMarshaler].
func (v Verdict) MarshalText() ([]byte, error) { return marshalEnum(verdictNames, v) }

// UnmarshalText implements [encoding.TextUnmarshaler].
func (v *Verdict) UnmarshalText(b []byte) error { return unmarshalEnum(verdictNames, v, b) }

// Policy is one rule about the surface. Judge sees every candidate
// element and returns its verdict with a reason; the reason is "" for a
// plain Keep. Name identifies the policy in a [Decision] and, with its
// [Describer] description, in [Policies.Digest].
type Policy interface {
	Name() string
	Judge(Element) (Verdict, string)
}

// Describer is a [Policy] whose name alone does not identify what it
// judges: Describe returns a stable description of its parameters, which
// [Policies.Digest] covers beside the name.
type Describer interface {
	Describe() string
}

// Decision is one policy's verdict on one element. Policy is "" on the
// row that records an element no policy spoke about.
type Decision struct {
	Element
	Policy  string
	Verdict Verdict
	Reason  string
}

// Policies are the policies a node composes its devices under. The zero
// value enables no extension and adds no custom policy: every candidate of
// the matter.js baseline is served, and an ahead-of-matter.js candidate is
// refused.
type Policies struct {
	// Extensions are the "Ahead of matter.js" register rows the host
	// enables. The built-in reference policy, judged first, refuses a
	// candidate tagged with a row ([Reference].Ahead) not in it
	// ([ErrExtensionNotEnabled]).
	Extensions Extensions
	// Level is the specification level the node composes at ([Level]):
	// the built-in level policy, judged after the reference policy,
	// removes the optional surface the level does not know, keeps what
	// the pin mandates and withholds a device whose type or mandated
	// cluster it does not know ([ErrWithheld]). "" is the pin's level,
	// which changes nothing.
	Level Level
	// Profiles are the controller profiles the operator selects, by name
	// ([Profiles], [ProfileByName]): one built-in policy "profile:<name>"
	// per profile, judged after the level policy, removes what a
	// Reproduced row of the register names (a mandatory element is kept)
	// and records a warning for every other row that names an element. A
	// profile's level applies when Level is "". An unknown name fails
	// [Policies.Validate] ([ErrUnknownProfile]), and so do two profiles
	// with different levels and no explicit Level
	// ([ErrProfileLevelConflict]).
	Profiles []string
	// Custom are the host's own policies, judged in order after the
	// built-in ones.
	Custom []Policy

	// injected are profiles package surfacetest adds through
	// internal/surfaceseam: known beside the register's.
	injected []Profile
}

// policiesDigestDomain opens the digest's input, so a change of the
// encoding cannot collide with an earlier one. v2 added the extensions.
const policiesDigestDomain = "go-fabric surface.Policies v2\n"

// Digest identifies the policies: the SHA-256 over the enabled extensions,
// sorted, the effective level when it is not the pin's, the selected
// profile names, sorted, then every custom policy's kind, name and
// description, in order. The zero value has a fixed digest, and a Level
// naming the pin's level has the same one: the two compose alike. A
// profile the register lists is identified by its name; one package
// surfacetest injected also by its rules. A device carries the digest of the policies it was
// composed under, and a node refuses a device whose digest is not its own.
func (p Policies) Digest() [32]byte {
	var b strings.Builder
	b.WriteString(policiesDigestDomain)
	for _, id := range p.Extensions.IDs() {
		fmt.Fprintf(&b, "extensions\x00%s\n", id)
	}
	if l := p.EffectiveLevel(); l != PinLevel() {
		fmt.Fprintf(&b, "level\x00%s\n", l)
	}
	for _, name := range p.profileNames() {
		fmt.Fprintf(&b, "profile\x00%s", name)
		for _, q := range p.injected {
			if q.Name == name {
				fmt.Fprintf(&b, "\x00injected\x00%s", q.describe())
				break
			}
		}
		b.WriteByte('\n')
	}
	for _, pol := range p.Custom {
		name, desc := "<nil>", ""
		if pol != nil {
			name = pol.Name()
			if d, ok := pol.(Describer); ok {
				desc = d.Describe()
			}
		}
		fmt.Fprintf(&b, "custom\x00%s\x00%s\n", name, desc)
	}
	return sha256.Sum256([]byte(b.String()))
}

// chain is every policy, in the order they judge: the built-in reference
// policy, the built-in level policy — built over the candidates, so it
// judges each cluster's features as a set — one built-in profile policy
// per selected profile, sorted by name, then the custom ones.
func (p Policies) chain(candidates []Element) []Policy {
	profiles := p.selectedProfiles()
	chain := make([]Policy, 0, 2+len(profiles)+len(p.Custom))
	chain = append(chain, referencePolicy{enabled: p.Extensions}, newLevelPolicy(p.EffectiveLevel(), candidates))
	for _, q := range profiles {
		chain = append(chain, profilePolicy{profile: q})
	}
	return append(chain, p.Custom...)
}

// ErrRefused is wrapped by the [*RefusedError] [Evaluate] returns when a
// policy refuses an element.
var ErrRefused = errors.New("surface: composition refused")

// RefusedError names every refusing decision of one evaluation.
type RefusedError struct {
	Decisions []Decision
	// ahead are the register IDs the built-in reference policy refused,
	// sorted; empty when only custom policies refused.
	ahead []string
}

// Error names each refusal: the policy, the element and the reason; when
// the reference policy refused, the register IDs first.
func (e *RefusedError) Error() string {
	parts := make([]string, 0, len(e.Decisions))
	for _, d := range e.Decisions {
		parts = append(parts, fmt.Sprintf("policy %q refuses %s: %s", d.Policy, d.Element, d.Reason))
	}
	head := ErrRefused.Error()
	if len(e.ahead) > 0 {
		head += " (" + aheadText(e.ahead) + ")"
	}
	return head + ": " + strings.Join(parts, "; ")
}

// Unwrap returns [ErrRefused].
func (e *RefusedError) Unwrap() error { return ErrRefused }

// Is reports whether target is [ErrExtensionNotEnabled] and the reference
// policy refused at least one element; errors.Is finds [ErrRefused]
// through Unwrap.
func (e *RefusedError) Is(target error) bool {
	return target == ErrExtensionNotEnabled && len(e.ahead) > 0 //nolint:errorlint // a sentinel compared by identity, as errors.Is does
}

// mandatoryKeptPrefix opens the reason of a Remove rewritten to Keep.
const mandatoryKeptPrefix = "kept: mandatory at pin ("

// Evaluate judges every candidate by every policy — the built-in reference
// policy first, then [Policies].Custom; no policy short-cuts another — and
// returns the elements kept, in candidate order, and the
// decisions. Every verdict other than a plain Keep is a decision; an
// element no policy spoke about has one Keep decision with Policy "", so
// every candidate appears at least once. A Warn keeps the element and is
// recorded whatever its reason.
//
// A Remove of a Mandatory element is kept: the decision is rewritten to
// Keep, its reason "kept: mandatory at pin (<the policy's reason>)". A
// Refuse stays a Refuse whatever the element. Any Refuse fails the
// evaluation with a [*RefusedError] naming every refusing decision; the
// decisions are returned beside it. When the reference policy refused, the
// error also matches [ErrExtensionNotEnabled] and names the register IDs.
// Without a refusal, a level that does not know a candidate device type or
// mandated cluster fails it with a [*WithheldError] ([ErrWithheld]), the
// decisions beside it. A [Policies].Level [Policies.Validate] rejects
// fails it before any judgement.
func Evaluate(candidates []Element, p Policies) (kept []Element, decisions []Decision, err error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	chain := p.chain(candidates)
	for i, pol := range chain {
		if pol == nil {
			return nil, nil, fmt.Errorf("surface: policy %d of the chain is nil", i)
		}
	}
	kept = make([]Element, 0, len(candidates))
	decisions = make([]Decision, 0, len(candidates))
	var refused []Decision
	ahead := map[string]struct{}{}
	for _, el := range candidates {
		spoke, removed := false, false
		for _, pol := range chain {
			v, reason := pol.Judge(el)
			d := Decision{Element: el, Policy: pol.Name(), Verdict: v, Reason: reason}
			switch v {
			case Keep:
				if reason == "" {
					continue
				}
			case Warn:
			case Remove:
				if el.Mandatory {
					d.Verdict = Keep
					d.Reason = mandatoryKeptPrefix + reason + ")"
				} else {
					removed = true
				}
			case Refuse:
				refused = append(refused, d)
				if _, ok := pol.(referencePolicy); ok {
					ahead[el.Reference.Ahead] = struct{}{}
				}
			default:
				return nil, nil, fmt.Errorf("surface: policy %q returned verdict %d for %s", d.Policy, v, el)
			}
			spoke = true
			decisions = append(decisions, d)
		}
		if !spoke {
			decisions = append(decisions, Decision{Element: el, Verdict: Keep})
		}
		if !removed {
			kept = append(kept, el)
		}
	}
	if len(refused) > 0 {
		return nil, decisions, &RefusedError{Decisions: refused, ahead: slices.Sorted(maps.Keys(ahead))}
	}
	for _, pol := range chain {
		if lp, ok := pol.(*levelPolicy); ok && len(lp.withheld) > 0 {
			return nil, decisions, &WithheldError{
				Endpoint: lp.withheld[0].Endpoint, Elements: slices.Clone(lp.withheld),
				Report: Report{Level: lp.level, Decisions: decisions, Withheld: slices.Clone(lp.withheld)},
			}
		}
	}
	return kept, decisions, nil
}

// Report is what a device or a node states about its surface: the digest
// of the policies it was composed under, the effective surface read from
// its built servers, and every decision.
type Report struct {
	Policies [32]byte
	// Level is the specification level the surface was composed at: the
	// pin's when neither [Policies].Level nor a selected profile sets one.
	Level Level
	// LevelOrigin names where Level comes from ([Policies.LevelOrigin]):
	// "explicit", "profile <name>[, <name>…]" or "pin".
	LevelOrigin string
	// Profiles are the selected controller profiles ([Policies].Profiles),
	// sorted, each once.
	Profiles []string
	// Extensions are the enabled "Ahead of matter.js" register IDs
	// ([Policies].Extensions), sorted; nil when none is enabled. A host's
	// pages show them next to the level and the profiles.
	Extensions []string
	Effective  Surface
	// Decisions has at least one row per candidate element ([Evaluate]).
	Decisions []Decision
	// Withheld are the elements the level policy withheld its endpoint
	// for: a device type or a mandated cluster the level does not know.
	Withheld []Element
	// Assembled reports that Effective was read from the assembled
	// topology: every endpoint as the endpoint assembler mounts it, its
	// clusters (Descriptor, BridgedDeviceBasicInformation, Identify,
	// Groups, ScenesManagement, Binding, measurement clusters) and its
	// device types (BridgedNode) included. False is the device-level view
	// before the first assembly: each device's own servers and device type.
	Assembled bool
	// Changed are the surface changes the reporter announced in this
	// process ([SurfaceChange]), oldest first; a node keeps the last 100.
	Changed []SurfaceChange
}

// Reporter is anything that reports its surface: a device.Device, a node.
type Reporter interface {
	Surface() Report
}

// Clone returns a deep copy of r.
func (r Report) Clone() Report {
	out := Report{
		Policies: r.Policies, Level: r.Level, LevelOrigin: r.LevelOrigin, Profiles: slices.Clone(r.Profiles),
		Extensions: slices.Clone(r.Extensions), Effective: r.Effective.Clone(), Decisions: slices.Clone(r.Decisions), Withheld: slices.Clone(r.Withheld),
		Assembled: r.Assembled,
	}
	if r.Changed != nil {
		out.Changed = make([]SurfaceChange, len(r.Changed))
		copy(out.Changed, r.Changed)
		for i := range out.Changed {
			out.Changed[i].Changes = slices.Clone(r.Changed[i].Changes)
		}
	}
	return out
}

// The wire forms of a report, fields in the order of their JSON keys.
type (
	wireReport struct {
		Assembled   bool                `json:"assembled"`
		Changed     []wireSurfaceChange `json:"changed"`
		Decisions   []wireDecision      `json:"decisions"`
		Effective   wireSurface         `json:"effective"`
		Extensions  []string            `json:"extensions"`
		Level       string              `json:"level"`
		LevelOrigin string              `json:"level_origin"`
		Policies    string              `json:"policies"`
		Profiles    []string            `json:"profiles"`
		Withheld    []wireElement       `json:"withheld"`
	}
	wireElement struct {
		Cluster   id32   `json:"cluster"`
		Endpoint  string `json:"endpoint"`
		ID        kindID `json:"id"`
		Kind      Kind   `json:"kind"`
		Mandatory bool   `json:"mandatory"`
		Origin    Origin `json:"origin"`
		Reference string `json:"reference"`
	}
	wireDecision struct {
		Cluster   id32    `json:"cluster"`
		Endpoint  string  `json:"endpoint"`
		ID        kindID  `json:"id"`
		Kind      Kind    `json:"kind"`
		Mandatory bool    `json:"mandatory"`
		Origin    Origin  `json:"origin"`
		Policy    string  `json:"policy"`
		Reason    string  `json:"reason"`
		Reference string  `json:"reference"`
		Verdict   Verdict `json:"verdict"`
	}
)

func elementWire(e Element) wireElement {
	return wireElement{
		Cluster: id32(e.Cluster), Endpoint: e.Endpoint, ID: idOf(e.Kind, e.ID), Kind: e.Kind,
		Mandatory: e.Mandatory, Origin: e.Origin, Reference: e.Reference.Ahead,
	}
}

func (w wireElement) value() Element {
	return Element{
		Endpoint: w.Endpoint, Cluster: uint32(w.Cluster), Kind: w.Kind, ID: w.ID.v,
		Origin: w.Origin, Reference: Reference{Ahead: w.Reference}, Mandatory: w.Mandatory,
	}
}

// MarshalJSON implements [json.Marshaler]: the canonical encoding of
// [Surface.MarshalJSON], the policies' digest in hex, the decisions and
// withheld elements in their order, their ids in hex as wide as their kind
// says (a feature's mask eight digits), and the announced changes, their
// digests in hex and their time in RFC 3339; the selected profiles and
// the level's origin.
func (r Report) MarshalJSON() ([]byte, error) {
	w := wireReport{
		Assembled:   r.Assembled,
		Changed:     make([]wireSurfaceChange, 0, len(r.Changed)),
		Decisions:   make([]wireDecision, 0, len(r.Decisions)),
		Effective:   r.Effective.wire(true),
		Extensions:  append(make([]string, 0, len(r.Extensions)), r.Extensions...),
		Level:       string(r.Level),
		LevelOrigin: r.LevelOrigin,
		Policies:    hex.EncodeToString(r.Policies[:]),
		Profiles:    append(make([]string, 0, len(r.Profiles)), r.Profiles...),
		Withheld:    make([]wireElement, 0, len(r.Withheld)),
	}
	for _, d := range r.Decisions {
		e := elementWire(d.Element)
		w.Decisions = append(w.Decisions, wireDecision{
			Cluster: e.Cluster, Endpoint: e.Endpoint, ID: e.ID, Kind: e.Kind, Mandatory: e.Mandatory,
			Origin: e.Origin, Policy: d.Policy, Reason: d.Reason, Reference: e.Reference, Verdict: d.Verdict,
		})
	}
	for _, e := range r.Withheld {
		w.Withheld = append(w.Withheld, elementWire(e))
	}
	for i := range r.Changed {
		w.Changed = append(w.Changed, r.Changed[i].wire())
	}
	return json.Marshal(w)
}

// UnmarshalJSON implements [json.Unmarshaler] for the encoding
// [Report.MarshalJSON] writes.
func (r *Report) UnmarshalJSON(b []byte) error {
	var w wireReport
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	digest, err := hex.DecodeString(w.Policies)
	if err != nil || len(digest) != len(r.Policies) {
		return fmt.Errorf("surface: report policies %q is no SHA-256 digest", w.Policies)
	}
	out := Report{
		Level: Level(w.Level), LevelOrigin: w.LevelOrigin, Profiles: w.Profiles, Extensions: w.Extensions,
		Effective: w.Effective.value(), Assembled: w.Assembled,
	}
	copy(out.Policies[:], digest)
	if len(out.Profiles) == 0 {
		out.Profiles = nil
	}
	if len(out.Extensions) == 0 {
		out.Extensions = nil
	}
	for _, c := range w.Changed {
		v, err := c.value()
		if err != nil {
			return err
		}
		out.Changed = append(out.Changed, v)
	}
	for _, d := range w.Decisions {
		el := wireElement{
			Cluster: d.Cluster, Endpoint: d.Endpoint, ID: d.ID, Kind: d.Kind,
			Mandatory: d.Mandatory, Origin: d.Origin, Reference: d.Reference,
		}.value()
		out.Decisions = append(out.Decisions, Decision{Element: el, Policy: d.Policy, Verdict: d.Verdict, Reason: d.Reason})
	}
	for _, e := range w.Withheld {
		out.Withheld = append(out.Withheld, e.value())
	}
	*r = out
	return nil
}
