// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import "thingfabric.dev/go-fabric/device"

// required is decls with the statements of the attributes New requires of
// the named type (device.InitialFor).
func required(name string, decls ...device.Declaration) []device.Declaration {
	return append(decls, device.InitialFor(name)...)
}
