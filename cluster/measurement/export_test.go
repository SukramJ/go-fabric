// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import "github.com/SukramJ/go-fabric/contract"

// HasBuiltinMaterializer reports whether this package installed a
// materialiser for the built-in class, for the external tests.
func HasBuiltinMaterializer(class contract.MeasurementClass) bool {
	_, ok := builtinMaterializers[class]
	return ok
}
