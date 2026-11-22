// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/cluster/valve"
)

// The servers that read the node's Matter time are its participants; a
// signature that drifts from the interface fails here, not silently at
// the type assertion in attachMatterTime.
var (
	_ matterTimeParticipant = (*thermo.ThermostatServer)(nil)
	_ matterTimeParticipant = (*valve.Server)(nil)
)
