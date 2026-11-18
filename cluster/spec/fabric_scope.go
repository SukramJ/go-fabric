// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/contract"

var _ contract.FabricScopeDeclarer = (*Instance)(nil)

// MatterFabricScope implements [contract.FabricScopeDeclarer] from the
// definition: an attribute whose access is fabric-scoped ("F") or
// fabric-sensitive ("S") declares Sensitive for "S" and, for a list of
// structs, the ids of the entry struct's fields marked FabricSensitive. A
// write-only attribute, one the definition does not hold and one without
// a fabric flag declare nothing.
//
// The dispatcher asks it for a cluster the module's schema snapshot does
// not hold and for a manufacturer-specific attribute ([VendorPrefix]) on
// one it does; for a standard attribute of a standard cluster the
// snapshot's table is the answer, and a definition generated from the
// snapshot states the same scope.
func (i *Instance) MatterFabricScope(attrID uint32) (contract.FabricScope, bool) {
	a := i.def.Attribute(attrID)
	if a == nil || !a.Access.Readable() || (a.Access.Fabric != "F" && a.Access.Fabric != "S") {
		return contract.FabricScope{}, false
	}
	scope := contract.FabricScope{Sensitive: a.Access.Fabric == "S"}
	if e := a.Type.Entry; e != nil && e.Type.Struct != nil {
		for i := range e.Type.Struct.Fields {
			if f := &e.Type.Struct.Fields[i]; f.FabricSensitive {
				scope.Fields = append(scope.Fields, f.ID)
			}
		}
	}
	return scope, true
}
