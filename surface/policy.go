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
// element, Refuse wins over Remove, Remove over Keep.
const (
	// Keep serves the element; the zero value.
	Keep Verdict = iota
	// Remove composes without the element: a choice the code could not
	// foresee, an operator's setting. A mandatory element is kept.
	Remove
	// Refuse fails the composition: two decisions of the same developer
	// contradict each other.
	Refuse
)

var verdictNames = map[Verdict]string{Keep: "keep", Remove: "remove", Refuse: "refuse"}

// String returns the verdict's name ("keep", "remove", "refuse").
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
	// Custom are the host's own policies, judged in order after the
	// built-in ones.
	Custom []Policy
}

// policiesDigestDomain opens the digest's input, so a change of the
// encoding cannot collide with an earlier one. v2 added the extensions.
const policiesDigestDomain = "go-fabric surface.Policies v2\n"

// Digest identifies the policies: the SHA-256 over the enabled extensions,
// sorted, then every custom policy's kind, name and description, in order.
// The zero value has a fixed digest. A device carries the digest of the
// policies it was composed under, and a node refuses a device whose digest
// is not its own.
func (p Policies) Digest() [32]byte {
	var b strings.Builder
	b.WriteString(policiesDigestDomain)
	for _, id := range p.Extensions.IDs() {
		fmt.Fprintf(&b, "extensions\x00%s\n", id)
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
// policy, then the custom ones.
func (p Policies) chain() []Policy {
	return append([]Policy{referencePolicy{enabled: p.Extensions}}, p.Custom...)
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
// every candidate appears at least once.
//
// A Remove of a Mandatory element is kept: the decision is rewritten to
// Keep, its reason "kept: mandatory at pin (<the policy's reason>)". A
// Refuse stays a Refuse whatever the element. Any Refuse fails the
// evaluation with a [*RefusedError] naming every refusing decision; the
// decisions are returned beside it. When the reference policy refused, the
// error also matches [ErrExtensionNotEnabled] and names the register IDs.
func Evaluate(candidates []Element, p Policies) (kept []Element, decisions []Decision, err error) {
	chain := p.chain()
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
	return kept, decisions, nil
}

// Report is what a device or a node states about its surface: the digest
// of the policies it was composed under, the effective surface read from
// its built servers, and every decision.
type Report struct {
	Policies  [32]byte
	Effective Surface
	// Decisions has at least one row per candidate element ([Evaluate]).
	Decisions []Decision
	// Withheld are the elements a policy withheld without failing the
	// composition; empty in this phase.
	Withheld []Element
}

// Reporter is anything that reports its surface: a device.Device, a node.
type Reporter interface {
	Surface() Report
}

// Clone returns a deep copy of r.
func (r Report) Clone() Report {
	return Report{
		Policies: r.Policies, Effective: r.Effective.Clone(),
		Decisions: slices.Clone(r.Decisions), Withheld: slices.Clone(r.Withheld),
	}
}

// The wire forms of a report, fields in the order of their JSON keys.
type (
	wireReport struct {
		Decisions []wireDecision `json:"decisions"`
		Effective wireSurface    `json:"effective"`
		Policies  string         `json:"policies"`
		Withheld  []wireElement  `json:"withheld"`
	}
	wireElement struct {
		Cluster   id16   `json:"cluster"`
		Endpoint  string `json:"endpoint"`
		ID        kindID `json:"id"`
		Kind      Kind   `json:"kind"`
		Mandatory bool   `json:"mandatory"`
		Origin    Origin `json:"origin"`
		Reference string `json:"reference"`
	}
	wireDecision struct {
		Cluster   id16    `json:"cluster"`
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
		Cluster: id16(e.Cluster), Endpoint: e.Endpoint, ID: idOf(e.Kind, e.ID), Kind: e.Kind,
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
// says (a feature's mask eight digits).
func (r Report) MarshalJSON() ([]byte, error) {
	w := wireReport{
		Decisions: make([]wireDecision, 0, len(r.Decisions)),
		Effective: r.Effective.wire(true),
		Policies:  hex.EncodeToString(r.Policies[:]),
		Withheld:  make([]wireElement, 0, len(r.Withheld)),
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
	out := Report{Effective: w.Effective.value()}
	copy(out.Policies[:], digest)
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
