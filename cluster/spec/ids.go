// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
)

// ErrIllegalID is a cluster, attribute, command or event id outside the
// ranges the specification reserves for it (Core § 7.10, § 7.19.2.31-34).
// [New] refuses a definition with one, naming each.
var ErrIllegalID = errors.New("spec: illegal element id")

// fromMei splits a Manufacturer Extensible Identifier into its vendor
// prefix and type suffix, refusing a prefix above 0xfff4 and the suffix
// 0xffff. Mirrors matter.js
// packages/types/src/datatype/ManufacturerExtensibleIdentifier.ts:29-52
// at 862ad75f (Mei.fromMei).
func fromMei(mei uint32) (prefix, suffix uint32, why string) {
	prefix, suffix = mei>>16, mei&0xffff
	switch {
	case prefix > 0xfff4:
		return 0, 0, fmt.Sprintf("invalid vendor prefix in MEI 0x%08x: the prefix must be 0x0000 - 0xfff4", mei)
	case suffix > 0xfffe:
		return 0, 0, fmt.Sprintf("invalid type suffix in MEI 0x%08x: the suffix must not be 0xffff", mei)
	}
	return prefix, suffix, ""
}

// CheckClusterID refuses an illegal cluster id: a standard cluster's
// suffix is 0x0000 - 0x7fff, a vendor-specific one's 0xfc00 - 0xfffe.
// Mirrors matter.js packages/types/src/datatype/ClusterId.ts:22-41 at
// 862ad75f.
func CheckClusterID(id uint32) error {
	prefix, suffix, why := fromMei(id)
	switch {
	case why != "":
	case prefix == 0 && suffix > 0x7fff:
		why = fmt.Sprintf("invalid cluster ID 0x%08x: a standard cluster suffix must be 0x0000 - 0x7fff", id)
	case prefix != 0 && (suffix < 0xfc00 || suffix > 0xfffe):
		why = fmt.Sprintf("invalid cluster ID 0x%08x: a vendor-specific cluster suffix must be 0xfc00 - 0xfffe", id)
	}
	return idError(why)
}

// CheckAttributeID refuses an illegal attribute id: a global one is
// 0xf000 - 0xfffe, any other's suffix 0x0000 - 0x4fff. Mirrors matter.js
// packages/types/src/datatype/AttributeId.ts:20-34 at 862ad75f.
func CheckAttributeID(id uint32) error {
	if id >= 0xf000 && id <= 0xfffe {
		return nil
	}
	_, suffix, why := fromMei(id)
	if why == "" && suffix > 0x4fff {
		why = fmt.Sprintf("invalid attribute ID 0x%08x: the suffix must be 0x0000 - 0x4fff, or the ID must be a global attribute ID 0xf000 - 0xfffe", id)
	}
	return idError(why)
}

// CheckCommandID refuses an illegal command id: its suffix is 0x00 -
// 0xff. Mirrors matter.js packages/types/src/datatype/CommandId.ts:21-32
// at 862ad75f.
func CheckCommandID(id uint32) error {
	_, suffix, why := fromMei(id)
	if why == "" && suffix > 0xff {
		why = fmt.Sprintf("invalid command ID 0x%08x: the suffix must be 0x00 - 0xff", id)
	}
	return idError(why)
}

// CheckEventID refuses an illegal event id: its suffix is 0x00 - 0xff.
// Mirrors matter.js packages/types/src/datatype/EventId.ts:20-29 at
// 862ad75f.
func CheckEventID(id uint32) error {
	_, suffix, why := fromMei(id)
	if why == "" && suffix > 0xff {
		why = fmt.Sprintf("invalid event ID 0x%08x: the suffix must be 0x00 - 0xff", id)
	}
	return idError(why)
}

func idError(why string) error {
	if why == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrIllegalID, why)
}

// checkElementIDs refuses a definition whose cluster id or any attribute,
// command or event id is illegal, every one named — matter.js
// ValidatedElements #validateSchema, which records a fatal error per
// illegal id and reports them together
// (packages/node/src/behavior/cluster/ValidatedElements.ts:190-222 at
// 862ad75f).
func checkElementIDs(def *Cluster) error {
	var errs []error
	named := func(element string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.%s: %w", def.Name, element, err))
		}
	}
	named("cluster", CheckClusterID(def.ID))
	for i := range def.Attributes {
		named(def.Attributes[i].Name, CheckAttributeID(def.Attributes[i].ID))
	}
	for i := range def.Commands {
		named(def.Commands[i].Name, CheckCommandID(def.Commands[i].ID))
	}
	for i := range def.Events {
		named(def.Events[i].Name, CheckEventID(def.Events[i].ID))
	}
	return errors.Join(errs...)
}
