// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/endpoint"
)

// Device type validation of every assembled topology: matter.js's
// DeviceTypeConformanceService, which judges a server node's endpoints
// against their device types once their construction completes
// (packages/node/src/node/server/DeviceTypeConformanceService.ts,
// docs/DEVICE_TYPE_VALIDATION.md). Start and every Reassemble judge the
// topology the snapshotter returns, with the root's and the Aggregator's
// servers published on it, before it replaces the live one. See
// docs/adr/0016-device-type-validation.md.

// SetDeviceTypeValidation selects what an assembly does with a topology
// that departs from its device types — matter.js's `endpoint.validation`:
//
//   - [endpoint.DeviceTypeValidationWarn], the default: each new violation
//     is logged once, as a warning, and the topology is installed; only a
//     misplaced singleton refuses it.
//   - [endpoint.DeviceTypeValidationStrict]: any new violation refuses it —
//     for tests, and for a host that must not run a non-conforming node.
//   - [endpoint.DeviceTypeValidationOff]: nothing is judged but singleton
//     placement, and nothing is logged.
//
// A refused topology is not installed: [Bridge.Start] or
// [Bridge.Reassemble] returns the *endpoint.DeviceTypeConformanceError,
// naming every endpoint, device type and requirement, and the bridge keeps
// serving the topology it had. Call before [Bridge.Start]; a later call
// takes effect at the next Reassemble and forgets what was reported.
func (b *Bridge) SetDeviceTypeValidation(mode endpoint.DeviceTypeValidationMode) {
	b.mu.Lock()
	b.deviceTypes = endpoint.NewDeviceTypeValidator(mode)
	b.mu.Unlock()
}

// DeviceTypeViolations returns what the validation of the installed
// topology found — the current violations, endpoint by endpoint, whether
// or not they were logged before (matter.js violationsOf). Empty in mode
// off and before Start.
func (b *Bridge) DeviceTypeViolations() []endpoint.DeviceTypeViolation {
	return b.deviceTypeValidator().Violations()
}

func (b *Bridge) deviceTypeValidator() *endpoint.DeviceTypeValidator {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deviceTypes == nil {
		b.deviceTypes = endpoint.NewDeviceTypeValidator(endpoint.DeviceTypeValidationWarn)
	}
	return b.deviceTypes
}

// validateDeviceTypes judges topology and logs what it newly violates, as
// DeviceTypeConformanceService logs "Endpoint … violates device type
// requirements"; a refusal is returned unlogged.
func (b *Bridge) validateDeviceTypes(topology *endpoint.Topology) error {
	verdict, err := b.deviceTypeValidator().Validate(topology)
	if err != nil {
		return err
	}
	for i := range verdict.Fresh {
		v := &verdict.Fresh[i]
		b.logger.Warn(
			"matter.devicetype.violation",
			slog.Int("endpoint", int(v.Endpoint)),
			slog.String("device_type", v.DeviceType),
			slog.String("requirement", v.Requirement),
			slog.String("kind", string(v.Kind)),
			slog.String("detail", v.Detail),
		)
	}
	return nil
}
