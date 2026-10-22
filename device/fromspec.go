// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"

	"github.com/SukramJ/go-fabric/schema"
)

// Spec is a device as data: its device type by id, its [Options] and its
// [Declaration]s — what a host whose devices come from a configuration
// file builds per device, and what every generated constructor
// (NewDimmableLight, …) builds for its type. Nothing in it is reflective:
// the declarations are the generated <cluster>def.Declare values and the
// kit's options, as in code.
type Spec struct {
	// Type is the device type id (DimmableLightID, …): a generated type
	// ([TypeByID]).
	Type schema.DeviceTypeID
	// Options are the host's statements about the device.
	Options Options
	// Declarations are the device's cluster declarations and the kit's
	// options, in order.
	Declarations []Declaration
}

// FromSpec returns the device s describes: [New] with the generated device
// type of s.Type. Every generated constructor is this call for its type,
// so FromSpec(Spec{Type: DimmableLightID, Options: o, Declarations: d})
// and NewDimmableLight(o, d...) build the same device.
func FromSpec(s Spec) (*Device, error) {
	t, ok := TypeByID(s.Type)
	if !ok {
		return nil, fmt.Errorf("%w: no generated device type 0x%04X", ErrConfiguration, s.Type)
	}
	return New(t, s.Options, s.Declarations...)
}
