// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"thingfabric.dev/go-fabric/cluster/spec"
	acldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
)

// The Go fields of the AccessControl write forms (the bridge decodes ACL
// and Extension writes into these types), so the dispatcher's check of a
// written value against the definition reads them (spec.MapFields; owner
// decision 64). spec.VerifyFieldMaps holds every mapping to its
// definition struct.
func init() {
	spec.MapFields[AccessControlEntryStruct](acldef.AccessControlEntryStructDef, spec.FieldMap{
		"Privilege": "Privilege", "AuthMode": "AuthMode", "Subjects": "Subjects", "Targets": "Targets",
		"AuxiliaryType": "AuxiliaryType", "FabricIndex": "FabricIndex",
	})
	spec.MapFields[ACLTargetStruct](acldef.AccessControlTargetStructDef, spec.FieldMap{
		"Cluster": "Cluster", "Endpoint": "Endpoint", "DeviceType": "DeviceType",
	})
	spec.MapFields[AccessControlExtensionEntry](acldef.AccessControlExtensionStructDef, spec.FieldMap{
		"Data": "Data", "FabricIndex": "FabricIndex",
	})
}
