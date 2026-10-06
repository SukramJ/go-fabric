// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import "strings"

// Privilege is an access level, numbered as matter.js's AccessLevel
// (packages/model/src/aspects/Access.ts): View 1, ProxyView 2, Operate 3,
// Manage 4, Administer 5. The values match schema.PrivilegeOperate & co.
type Privilege uint8

// Privilege levels.
const (
	PrivilegeView       Privilege = 1
	PrivilegeProxyView  Privilege = 2
	PrivilegeOperate    Privilege = 3
	PrivilegeManage     Privilege = 4
	PrivilegeAdminister Privilege = 5
)

// Access is an element's effective access, the fields of matter.js's
// Access aspect. A zero privilege means the element states none; the
// readers below apply matter.js's defaults (AccessControl.ts: View to
// read, Operate to write or invoke).
type Access struct {
	// RW is "R", "W", "RW" or "R[W]" (optional write); empty when unstated.
	RW string
	// Read and Write are the stated read and write (invoke) privileges.
	Read, Write Privilege
	// Fabric is "F" (fabric-scoped) or "S" (fabric-sensitive), empty when
	// the element is fabric-unaware.
	Fabric string
	// Timed requires a timed interaction ("T").
	Timed bool
}

// Readable mirrors Access.readable: everything but write-only.
func (a Access) Readable() bool { return a.RW != "W" }

// Writable mirrors Access.writable: "W", "RW" and "R[W]".
func (a Access) Writable() bool { return a.RW != "" && a.RW != "R" }

// ReadPrivilege is the privilege a read needs: the stated one, View when
// none is stated (AccessControl.ts readLevel).
func (a Access) ReadPrivilege() Privilege {
	if a.Read == 0 {
		return PrivilegeView
	}
	return a.Read
}

// WritePrivilege is the privilege a write or an invoke needs: the stated
// one, Operate when none is stated (AccessControl.ts writeLevel).
func (a Access) WritePrivilege() Privilege {
	if a.Write == 0 {
		return PrivilegeOperate
	}
	return a.Write
}

// Quality is an element's effective quality flags, the fields of matter.js's
// Quality aspect (packages/model/src/aspects/Quality.ts).
type Quality struct {
	Nullable       bool // X
	NonVolatile    bool // N
	Fixed          bool // F
	Scene          bool // S
	Reportable     bool // P
	ChangesOmitted bool // C
	Singleton      bool // I
	Quieter        bool // Q
	LargeMessage   bool // L
	Diagnostics    bool // K
	Atomic         bool // T
}

// Bound is one side of a constraint: a number, the name of a sibling
// element whose value bounds this one ("minMeasuredValue"), or an
// expression the runtime does not evaluate (Expr; such a bound admits every
// value, as matter.js's does when it cannot compute one).
type Bound struct {
	Int  int64
	Ref  string
	Expr string
}

// IsInt reports whether the bound is a number.
func (b *Bound) IsInt() bool { return b != nil && b.Ref == "" && b.Expr == "" }

// Constraint is an element's effective constraint, the fields of matter.js's
// Constraint aspect: a single value, a range, a set ("in"), list entries,
// or several parts.
type Constraint struct {
	// Text is the constraint as matter.js prints it ("2 to 255").
	Text string
	// Desc marks a constraint the specification states in prose.
	Desc bool
	// Value, Min and Max bound the value — or, for a string, octet string
	// or list, its length.
	Value, Min, Max *Bound
	// In names the element whose entries the value must be one of.
	In string
	// Entry constrains each entry of a list.
	Entry *Constraint
	// Parts are alternatives ("0, 2 to 5").
	Parts []Constraint
}

// Kind is the encoded shape of a value.
type Kind uint8

// Value kinds, after matter.js's metatypes.
const (
	KindAny Kind = iota
	KindBool
	KindUint
	KindInt
	KindFloat32
	KindFloat64
	KindString
	KindBytes
	KindEnum
	KindBitmap
	KindStruct
	KindList
)

// Type describes a value: its declared name and the shape it encodes as.
type Type struct {
	// Name is the type the element states ("uint8", "percent",
	// "OperationModeEnum", "list").
	Name string
	// Kind is the encoded shape.
	Kind Kind
	// Bits is the width of an integer, enum or bitmap (8, 16, 24, 32, 40,
	// 48, 56 or 64).
	Bits int
	// Enum, Bitmap and Struct describe a named datatype.
	Enum   *Enum
	Bitmap *Bitmap
	Struct *Struct
	// Entry describes the entries of a list.
	Entry *Field
}

// Field is a member of a struct, a command or an event payload, or a list
// entry.
type Field struct {
	ID          uint32
	Name        string
	Type        Type
	Conformance Conformance
	Quality     Quality
	Constraint  Constraint
	// FabricSensitive is access "S": the field is withheld from other
	// fabrics.
	FabricSensitive bool
	// Default is the default value matter.js states: a float64, bool or
	// string, [NullDefault] for a stated null, nil when none is stated (or
	// when it is a field value the generator does not render).
	Default any
}

// NullDefault is the default of an element whose stated default is null.
// fabric:reachable:reason="the Default of a generated attribute whose stated default is null; it is only ever a package-level initializer value, which RTA does not follow"
type NullDefault struct{}

// Mandatory mirrors ValueModel.mandatory (Conformance.isMandatory): the
// field must be present in a payload.
func (f *Field) Mandatory() bool { return f.Conformance.IsMandatory() }

// EnumValue is one value of an enum.
type EnumValue struct {
	Value       uint64
	Name        string
	Conformance Conformance
}

// Enum is an enum datatype.
type Enum struct {
	Name   string
	Bits   int
	Values []EnumValue
}

// BitmapMember is one bit, or range of bits, of a bitmap.
type BitmapMember struct {
	Name string
	// Bit is the first bit; Width the number of bits (1 for a flag).
	Bit, Width  uint
	Conformance Conformance
}

// Mask is the member's bits within the bitmap.
func (m BitmapMember) Mask() uint64 { return (uint64(1)<<m.Width - 1) << m.Bit }

// Bitmap is a bitmap datatype.
type Bitmap struct {
	Name    string
	Bits    int
	Members []BitmapMember
}

// Defined is the union of every member's bits; any other bit is reserved.
func (b *Bitmap) Defined() uint64 {
	var mask uint64
	for mi := range b.Members {
		mask |= b.Members[mi].Mask()
	}
	return mask
}

// Struct is a struct datatype.
type Struct struct {
	Name   string
	Fields []Field
}

// Attribute is a cluster attribute.
type Attribute struct {
	ID          uint32
	Name        string
	Type        Type
	Conformance Conformance
	Access      Access
	Quality     Quality
	Constraint  Constraint
	Default     any
	// Decode decodes a value of the attribute into the generated type —
	// for an attribute whose value is a struct (the struct) or a list of
	// structs (a [List]); nil for any other.
	Decode func(Node) (any, error)
}

// Writable mirrors AccessControl limits.writable: the access allows a
// write and the attribute is not fixed (quality "F").
func (a *Attribute) Writable() bool { return a.Access.Writable() && !a.Quality.Fixed }

// Direction is a command's direction.
type Direction uint8

// Command directions.
const (
	Request Direction = iota
	Response
)

// Command is a cluster command, a request or a response.
type Command struct {
	ID        uint32
	Name      string
	Direction Direction
	// Response names the response command of a request, or "status" for
	// one answered with a status only; empty on a response.
	Response    string
	Conformance Conformance
	Access      Access
	Fields      []Field
	// Decode decodes the payload into the generated payload type.
	Decode func(Node) (any, error)
}

// Priority is an event priority.
type Priority uint8

// Event priorities, as matter.js's EventElement.Priority.
const (
	PriorityDebug Priority = iota
	PriorityInfo
	PriorityCritical
)

// Event is a cluster event.
type Event struct {
	ID          uint32
	Name        string
	Priority    Priority
	Conformance Conformance
	Access      Access
	Fields      []Field
	// Decode decodes the payload into the generated payload type.
	Decode func(Node) (any, error)
}

// Feature is a FeatureMap bit.
type Feature struct {
	// Name is the specification's short name ("PRSCONST"), the name a
	// conformance expression states.
	Name string
	// Title is the long name ("ConstantPressure").
	Title       string
	Bit         uint
	Conformance Conformance
}

// Mask is the feature's FeatureMap bit.
func (f Feature) Mask() uint32 { return 1 << f.Bit }

// Cluster is a generated cluster definition.
type Cluster struct {
	ID       uint32
	Name     string
	Revision uint16
	// Base names the cluster this one derives from ("ModeBase"), empty
	// for a cluster that derives from none.
	Base       string
	Features   []Feature
	Attributes []Attribute
	Commands   []Command
	Events     []Event
}

// Attribute returns the attribute with id, or nil.
func (c *Cluster) Attribute(id uint32) *Attribute {
	for i := range c.Attributes {
		if c.Attributes[i].ID == id {
			return &c.Attributes[i]
		}
	}
	return nil
}

// AttributeByName returns the attribute named name — matter.js's name or
// the camelCase property name a constraint states ("minMeasuredValue") —
// or nil. The match ignores case: camelize lower-cases a leading acronym
// ("PIROccupiedToUnoccupiedDelay" is "pirOccupiedToUnoccupiedDelay"), and
// no cluster has two attributes whose names differ in case only.
func (c *Cluster) AttributeByName(name string) *Attribute {
	for i := range c.Attributes {
		if a := &c.Attributes[i]; strings.EqualFold(a.Name, name) {
			return a
		}
	}
	return nil
}

// Command returns the command with id and direction, or nil.
func (c *Cluster) Command(id uint32, dir Direction) *Command {
	for i := range c.Commands {
		if cmd := &c.Commands[i]; cmd.ID == id && cmd.Direction == dir {
			return cmd
		}
	}
	return nil
}

// CommandByName returns the command named name, or nil.
func (c *Cluster) CommandByName(name string) *Command {
	for i := range c.Commands {
		if c.Commands[i].Name == name {
			return &c.Commands[i]
		}
	}
	return nil
}

// Event returns the event with id, or nil.
func (c *Cluster) Event(id uint32) *Event {
	for i := range c.Events {
		if c.Events[i].ID == id {
			return &c.Events[i]
		}
	}
	return nil
}

// Feature returns the feature named name (short name or title), or nil.
func (c *Cluster) Feature(name string) *Feature {
	for i := range c.Features {
		if f := &c.Features[i]; f.Name == name || f.Title == name {
			return f
		}
	}
	return nil
}

// FeatureMask is the union of every defined feature bit.
func (c *Cluster) FeatureMask() uint32 {
	var mask uint32
	for fi := range c.Features {
		mask |= c.Features[fi].Mask()
	}
	return mask
}
