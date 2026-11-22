// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// handReadForms are the 33 (cluster, command) requests a bridge hand reader
// decodes into a cluster/core form, as the P2-H post-read measured them
// (fable-post-H-C2.md A3: 10 judged by name coincidence, 23 not).
var handReadForms = []string{
	"GeneralCommissioning.ArmFailSafe", "GeneralCommissioning.SetRegulatoryConfig",
	"OperationalCredentials.AttestationRequest", "OperationalCredentials.CertificateChainRequest",
	"OperationalCredentials.CsrRequest", "OperationalCredentials.AddNoc", "OperationalCredentials.UpdateNoc",
	"OperationalCredentials.UpdateFabricLabel", "OperationalCredentials.RemoveFabric",
	"OperationalCredentials.AddTrustedRootCertificate", "OperationalCredentials.SetVidVerificationStatement",
	"OperationalCredentials.SignVidVerificationRequest",
	"AdministratorCommissioning.OpenCommissioningWindow",
	"Groups.AddGroup", "Groups.ViewGroup", "Groups.GetGroupMembership", "Groups.RemoveGroup", "Groups.AddGroupIfIdentifying",
	"GroupKeyManagement.KeySetRead", "GroupKeyManagement.KeySetRemove",
	"Groupcast.JoinGroup", "Groupcast.LeaveGroup", "Groupcast.UpdateGroupKey", "Groupcast.ConfigureAuxiliaryAcl", "Groupcast.GroupcastTesting",
	"ScenesManagement.AddScene", "ScenesManagement.ViewScene", "ScenesManagement.RemoveScene", "ScenesManagement.RemoveAllScenes",
	"ScenesManagement.StoreScene", "ScenesManagement.RecallScene", "ScenesManagement.GetSceneMembership", "ScenesManagement.CopyScene",
}

// TestEveryHandReadRequestIsJudgedInItsGeneratedForm (A3 of the P2-H
// post-read, decision 339): every request a hand reader decodes reaches
// the dispatcher as a spec.HandForm whose Generated is the definition's own
// decoding of the same bytes — the type its decoder returns — so the model
// gate judges it by construction, whatever the hand form's Go field names.
// A payload is synthesised from each command's definition (every mandatory
// field at a value its constraint admits).
func TestEveryHandReadRequestIsJudgedInItsGeneratedForm(t *testing.T) {
	t.Parallel()
	var got []string
	for _, cluster := range []uint32{0x0030, 0x003E, 0x003C, 0x0004, 0x003F, 0x0065, 0x0062} {
		def, _ := handReader(im.ConcreteCommandPath{Cluster: cluster})
		if def == nil {
			t.Fatalf("cluster 0x%04X has no hand reader", cluster)
		}
		for ci := range def.Commands {
			cmd := &def.Commands[ci]
			if cmd.Direction != spec.Request || len(cmd.Fields) == 0 {
				continue
			}
			enc := tlv.NewEncoder()
			synthStruct(enc, tlv.AnonymousTag(), cmd.Fields)
			raw, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			dec := tlv.NewDecoder(raw)
			el, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			path := im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: cluster, Command: cmd.ID}
			fields, err := commandFieldsReader(path, dec, el)
			name := def.Name + "." + cmd.Name
			if err != nil {
				t.Errorf("%s: the synthesised payload is refused: %v", name, err)
				continue
			}
			hf, ok := fields.(spec.HandForm)
			if !ok {
				continue // decoded by definition, or the generated form is the hand form
			}
			got = append(got, name)
			want, _ := cmd.Decode(spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}})
			if reflect.TypeOf(hf.Generated) != reflect.TypeOf(want) {
				t.Errorf("%s: judged form %T, want the definition's %T", name, hf.Generated, want)
			}
		}
	}
	slices.Sort(got)
	want := slices.Clone(handReadForms)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("hand-read forms carried for the gate:\n got  %v\n want %v", got, want)
	}
}

// synthStruct encodes a structure carrying every mandatory field of fields
// at a value its type and constraint admit.
func synthStruct(enc *tlv.Encoder, tag tlv.Tag, fields []spec.Field) {
	enc.StartStruct(tag)
	for fi := range fields {
		f := &fields[fi]
		if f.Conformance.Op != spec.ConfMandatory {
			continue
		}
		synthValue(enc, tlv.ContextTag(uint8(f.ID)), f)
	}
	_ = enc.EndContainer()
}

// synthValue encodes a value of f at tag.
func synthValue(enc *tlv.Encoder, tag tlv.Tag, f *spec.Field) {
	c := f.Constraint
	if len(c.Parts) > 0 {
		c = c.Parts[0]
	}
	low := func(def int64) int64 {
		switch {
		case c.Value != nil && c.Value.IsInt():
			return c.Value.Int
		case c.Min != nil && c.Min.IsInt():
			return c.Min.Int
		}
		return def
	}
	switch f.Type.Kind {
	case spec.KindBool:
		enc.PutBool(tag, false)
	case spec.KindUint, spec.KindBitmap:
		enc.PutUint(tag, uint64(low(1)))
	case spec.KindEnum:
		var v uint64
		if e := f.Type.Enum; e != nil && len(e.Values) > 0 {
			v = e.Values[0].Value
		}
		enc.PutUint(tag, v)
	case spec.KindInt:
		enc.PutInt(tag, low(0))
	case spec.KindString:
		enc.PutUTF8(tag, strings.Repeat("a", int(low(1))))
	case spec.KindBytes:
		enc.PutOctets(tag, make([]byte, low(1)))
	case spec.KindList:
		enc.StartArray(tag)
		if n := low(0); n > 0 && f.Type.Entry != nil {
			for range n {
				synthValue(enc, tlv.AnonymousTag(), f.Type.Entry)
			}
		}
		_ = enc.EndContainer()
	case spec.KindStruct:
		synthStruct(enc, tag, f.Type.Struct.Fields)
	default:
		panic(fmt.Sprintf("synthValue: %s of kind %d", f.Name, f.Type.Kind))
	}
}
