// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Ahead names the elements of one cluster server that implement one row of
// the "Ahead of matter.js" register (notes/parity/by_design.md): surface
// matter.js does not serve at the pin.
type Ahead struct {
	// Features are the FeatureMap bits the row implements.
	Features uint32
	// Attributes, Commands and Events are the optional elements the row
	// adds beside its features.
	Attributes []uint32
	Commands   []uint32
	Events     []uint32
}

// has reports whether a names the element of kind with id: a feature's
// one-bit mask, or an attribute, command or event id.
func (a Ahead) has(kind Kind, id uint32) bool {
	switch kind {
	case KindFeature:
		return id != 0 && a.Features&id == id
	case KindAttribute:
		return slices.Contains(a.Attributes, id)
	case KindCommand:
		return slices.Contains(a.Commands, id)
	case KindEvent:
		return slices.Contains(a.Events, id)
	default:
		return false
	}
}

// AheadSet is a cluster server's declaration of its ahead-of-matter.js
// elements: register ID → the elements that implement the row. A server
// package exports one next to the server; the device layer and the node
// tag each candidate it names ([Element].Reference.Ahead).
type AheadSet map[string]Ahead

// IDs returns the register IDs of s, sorted.
func (s AheadSet) IDs() []string { return slices.Sorted(maps.Keys(s)) }

// Row returns the register ID whose elements include the element of kind
// with id (a feature's one-bit mask, or an attribute, command or event id),
// or "" when no row of s names it. Rows are tried in [AheadSet.IDs] order.
func (s AheadSet) Row(kind Kind, id uint32) string {
	for _, rid := range s.IDs() {
		if s[rid].has(kind, id) {
			return rid
		}
	}
	return ""
}

// Extensions are the "Ahead of matter.js" register IDs a host enables
// ([Policies].Extensions). An ID this module does not declare is allowed:
// a host may enable a row before the module ships it.
type Extensions map[string]struct{}

// NewExtensions returns the extensions ids.
func NewExtensions(ids ...string) Extensions {
	e := make(Extensions, len(ids))
	for _, id := range ids {
		e[id] = struct{}{}
	}
	return e
}

// Has reports whether the register ID id is enabled.
func (e Extensions) Has(id string) bool {
	_, ok := e[id]
	return ok
}

// IDs returns the enabled register IDs, sorted.
func (e Extensions) IDs() []string { return slices.Sorted(maps.Keys(e)) }

// ErrExtensionNotEnabled is wrapped by the [*RefusedError] [Evaluate]
// returns when the built-in reference policy refuses an element: a
// candidate tagged with an "Ahead of matter.js" register row
// ([Reference].Ahead) the [Policies].Extensions do not enable. The code
// selected an element the same code did not enable (ADR 0030 decision 3).
var ErrExtensionNotEnabled = errors.New("surface: ahead-of-matter.js extension not enabled")

// referencePolicyName is the built-in reference policy's name in a
// [Decision].
const referencePolicyName = "reference"

// referencePolicy is the built-in policy that refuses an ahead-of-matter.js
// element whose register row is not enabled.
type referencePolicy struct{ enabled Extensions }

func (referencePolicy) Name() string { return referencePolicyName }

func (p referencePolicy) Judge(el Element) (v Verdict, reason string) {
	if el.Reference.Ahead == "" || p.enabled.Has(el.Reference.Ahead) {
		return Keep, ""
	}
	return Refuse, fmt.Sprintf("ahead-of-matter.js row %s is not enabled (surface.Policies.Extensions)", el.Reference.Ahead)
}

// aheadText is the part of a [RefusedError]'s text that names the
// register IDs the reference policy refused.
func aheadText(ids []string) string {
	return ErrExtensionNotEnabled.Error() + ": " + strings.Join(ids, ", ")
}
