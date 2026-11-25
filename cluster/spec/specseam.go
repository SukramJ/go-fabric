// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/internal/specseam"
)

func init() {
	specseam.SetAttributesStored = func(server any, origin contract.Origin, values map[uint32]any, stored func()) error {
		s, ok := server.(*Server)
		if !ok {
			return fmt.Errorf("spec: SetAttributesStored on %T, not a *spec.Server", server)
		}
		return s.setAttributesStored(origin, values, stored)
	}
}
