// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"slices"
	"strings"

	"thingfabric.dev/go-fabric/schema"
)

// Spec is a device as data: its device type by id, its [Options], its
// [Declaration]s and its parts — what a host whose devices come from a
// configuration file builds per device, and what every generated
// constructor (NewDimmableLight, …) builds for its type. Nothing in it is
// reflective: the declarations are the generated <cluster>def.Declare
// values and the kit's options, as in code.
type Spec struct {
	// Type is the device type id (DimmableLightID, …): a generated type
	// ([Lookup]).
	Type schema.DeviceTypeID
	// Options are the host's statements about the device.
	Options Options
	// Declarations are the device's cluster declarations and the kit's
	// options, in order.
	Declarations []Declaration
	// Parts are the device's parts, endpoints below it ([Device.AddPart]),
	// in order: a composed device (a power strip's outlets, a composite's
	// components). A composite type's mandatory components are among them,
	// found by their device type.
	Parts []Spec
}

// FromSpec returns the device s describes: [New] with the generated device
// type of s.Type, and each of s.Parts built the same way and added as a
// part. It builds what the generated constructor of the type builds:
// FromSpec(Spec{Type: DimmableLightID, Options: o, Declarations: d}) and
// NewDimmableLight(o, d...) build the same device, and for a composite
// (FloodlightCamera, VideoDoorbell) the components the constructor takes
// as arguments come from s.Parts — a missing one is refused with the
// constructor's [ErrComposition]. A part that is refused refuses the
// device.
func FromSpec(s Spec) (*Device, error) {
	t, ok := typesByID[uint32(s.Type)]
	if !ok {
		return nil, fmt.Errorf("%w: no generated device type 0x%04X", ErrConfiguration, s.Type)
	}
	if missing := missingComponents(t, s.Parts); len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s needs its components %s", ErrComposition, t.Name, strings.Join(missing, " and "))
	}
	d, err := newOfSpec(s)
	if d == nil || err != nil {
		// A device the specification level withheld comes with its
		// error ([New]) and without parts.
		return d, err
	}
	for i := range s.Parts {
		p, err := FromSpec(s.Parts[i])
		if err != nil {
			return nil, fmt.Errorf("%s: part %d: %w", t.Name, i, err)
		}
		if err := d.AddPart(p); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// newOfSpec is [New] with s's type, options and declarations — no parts
// and no component check: what a generated constructor builds before it
// adds the components it took as arguments.
func newOfSpec(s Spec) (*Device, error) {
	t, ok := typesByID[uint32(s.Type)]
	if !ok {
		return nil, fmt.Errorf("%w: no generated device type 0x%04X", ErrConfiguration, s.Type)
	}
	return New(t, s.Options, s.Declarations...)
}

// missingComponents names t's mandatory components no part of parts is of,
// in t's order.
func missingComponents(t *Type, parts []Spec) []string {
	var missing []string
	for _, c := range t.components {
		if !slices.ContainsFunc(parts, func(p Spec) bool { return p.Type == c }) {
			name := fmt.Sprintf("0x%04X", uint32(c))
			if ct, ok := typesByID[uint32(c)]; ok {
				name = ct.Name
			}
			missing = append(missing, name)
		}
	}
	return missing
}
