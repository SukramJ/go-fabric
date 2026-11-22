// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"thingfabric.dev/go-fabric/cluster/spec"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// decodeByDefinition decodes a request payload through the registered
// generated definition of its cluster ([spec.Lookup]): the fields
// container, opened by el, is read through its EndContainer, a mandatory
// field a controller in the field omits is given its fallback
// ([requestFallbacks]), and the definition's decoder returns the
// generated request struct or the status matter.js's request schema
// answers the payload with, the server's relaxations included — the
// definition carries them ([spec.Command.DecodeRequest], the relaxation
// table of script/clustergen: DoorLock's request rules, ICD's
// VerificationKey); a command without fields decodes to nil. ok is
// false, and nothing is read, for a cluster or command no registered
// definition decodes.
func decodeByDefinition(path im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (fields any, ok bool, err error) {
	if handWrittenServers[path.Cluster] {
		return nil, false, nil
	}
	def := spec.Lookup(path.Cluster)
	if def == nil {
		return nil, false, nil
	}
	cmd := def.Command(path.Command, spec.Request)
	if cmd == nil || cmd.Decode == nil {
		return nil, false, nil
	}
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		return nil, true, fieldsReadError(def.Name+"."+cmd.Name, err)
	}
	fields, err = cmd.DecodeRequest(withFallbacks(path, n))
	if err == nil && len(cmd.Fields) == 0 {
		// A command without fields hands no payload on, whether its
		// CommandFields arrived empty or absent (read as an empty
		// structure, im.UnmarshalInvokeRequestTLV). Mirrors matter.js
		// packages/types/src/tlv/TlvVoid.ts:20-22, :28-29 at b25ed960 (the
		// request schema of a command without fields, TlvNoArguments,
		// decodes to nothing and validates only undefined).
		return nil, true, nil
	}
	return fields, true, err
}

// handWrittenServers are the clusters whose committed definition is
// definition-only (script/clustergen/clusters.go definitionOnly): the
// hand-written cluster/core servers of AccessControl (0x001F),
// OperationalCredentials (0x003E) and GroupKeyManagement (0x003F) are
// written against their own request forms, so their requests never decode
// into the generated structs — whether or not a binary links the
// definitions (controller/client does, for its typed JSON route). A
// command of theirs no hand-written reader decodes is salvaged as a tag
// map, as before the definitions existed.
var handWrittenServers = map[uint32]bool{0x001F: true, 0x003E: true, 0x003F: true}

// fallback is the value an omitted mandatory request field decodes as:
// TLV null for a nullable field, an unsigned 0 otherwise.
type fallback struct {
	field uint8
	null  bool
}

// requestFallbacks are the mandatory request fields a controller in the
// field omits, with the value each decodes as. Mirrors matter.js
// packages/types/src/tlv/TlvObject.ts:233-245 (a mandatory field's
// fallback populates it when the payload omits it) and :393-398 (TlvField
// takes the fallback "when devices omit" the field); matter.js declares
// none for LevelControl, so the entries are the module's interop list
// (notes/parity/by_design.md BD-Matter-LevelControl-LenientDecoders):
// Google Home omits the mandatory-but-nullable TransitionTime (and Rate)
// entirely, and an omitted OptionsMask / OptionsOverride reads as 0, as
// the lenient decoders before read them.
var requestFallbacks = map[[2]uint32][]fallback{
	{lvldef.ClusterID, lvldef.CmdMoveToLevel}:          {{1, true}, {2, false}, {3, false}},
	{lvldef.ClusterID, lvldef.CmdMoveToLevelWithOnOff}: {{1, true}, {2, false}, {3, false}},
	{lvldef.ClusterID, lvldef.CmdMove}:                 {{1, true}, {2, false}, {3, false}},
	{lvldef.ClusterID, lvldef.CmdMoveWithOnOff}:        {{1, true}, {2, false}, {3, false}},
	{lvldef.ClusterID, lvldef.CmdStep}:                 {{2, true}, {3, false}, {4, false}},
	{lvldef.ClusterID, lvldef.CmdStepWithOnOff}:        {{2, true}, {3, false}, {4, false}},
	{lvldef.ClusterID, lvldef.CmdStop}:                 {{0, false}, {1, false}},
	{lvldef.ClusterID, lvldef.CmdStopWithOnOff}:        {{0, false}, {1, false}},
}

// withFallbacks is n with every omitted field of [requestFallbacks] for
// the command added as its fallback value; n itself is not changed.
func withFallbacks(path im.ConcreteCommandPath, n spec.Node) spec.Node {
	fbs := requestFallbacks[[2]uint32{path.Cluster, path.Command}]
	if len(fbs) == 0 || n.El.Type != tlv.TypeStructure {
		return n
	}
	present := map[uint32]bool{}
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext {
			present[c.El.Tag.Number] = true
		}
	}
	children := append([]spec.Node(nil), n.Children...)
	for _, fb := range fbs {
		if present[uint32(fb.field)] {
			continue
		}
		el := tlv.Element{Tag: tlv.ContextTag(fb.field), Type: tlv.TypeUnsignedInt1}
		if fb.null {
			el.Type, el.IsNull = tlv.TypeNull, true
		}
		children = append(children, spec.Node{El: el})
	}
	n.Children = children
	return n
}
