// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"reflect"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestScenesHandFormCarriesTheRequestW2B2a (C4, fields_reader_scenes.go
// readScenesTree and scenesFieldsReader): the hand form the server takes
// for each of the eight ScenesManagement requests carries the request's
// values — TestEveryHandReadRequestIsJudgedInItsGeneratedForm compares
// only the generated form the gate judges, never the hand form.
func TestScenesHandFormCarriesTheRequestW2B2a(t *testing.T) {
	t.Parallel()
	tt := uint32(25)
	for _, tc := range []struct {
		name   string
		cmd    uint32
		encode func(enc *tlv.Encoder)
		want   any
	}{
		{
			name: "AddScene", cmd: 0x00,
			encode: func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(0), 0x0102)
				enc.PutUint(tlv.ContextTag(1), 3)
				enc.PutUint(tlv.ContextTag(2), 40)
				enc.PutUTF8(tlv.ContextTag(3), "evening")
				enc.StartArray(tlv.ContextTag(4))
				enc.StartStruct(tlv.AnonymousTag())
				enc.PutUint(tlv.ContextTag(0), 0x0006)
				enc.StartArray(tlv.ContextTag(1))
				enc.StartStruct(tlv.AnonymousTag())
				enc.PutUint(tlv.ContextTag(0), 0x0000)
				enc.PutUint(tlv.ContextTag(1), 1)
				_ = enc.EndContainer()
				_ = enc.EndContainer()
				_ = enc.EndContainer()
				_ = enc.EndContainer()
			},
			want: mattercore.AddSceneRequest{
				GroupID: 0x0102, SceneID: 3, TransitionTime: 40, SceneName: "evening",
				ExtensionFieldSets: []mattercore.ExtensionFieldSet{{
					ClusterID:          0x0006,
					AttributeValueList: []mattercore.AttributeValuePair{{AttributeID: 0, Tag: 1, Unsigned: 1, Fields: 1}},
				}},
			},
		},
		{name: "ViewScene", cmd: 0x01, encode: sceneRefW2B2a(0x0102, 3), want: mattercore.SceneRef{GroupID: 0x0102, SceneID: 3}},
		{name: "RemoveScene", cmd: 0x02, encode: sceneRefW2B2a(0x0203, 4), want: mattercore.SceneRef{GroupID: 0x0203, SceneID: 4}},
		{
			name: "RemoveAllScenes", cmd: 0x03,
			encode: func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 0x0304) },
			want:   mattercore.SceneGroupRequest{GroupID: 0x0304},
		},
		{name: "StoreScene", cmd: 0x04, encode: sceneRefW2B2a(0x0405, 5), want: mattercore.SceneRef{GroupID: 0x0405, SceneID: 5}},
		{
			name: "RecallScene", cmd: 0x05,
			encode: func(enc *tlv.Encoder) {
				sceneRefW2B2a(0x0506, 6)(enc)
				enc.PutUint(tlv.ContextTag(2), uint64(tt))
			},
			want: mattercore.RecallSceneRequest{GroupID: 0x0506, SceneID: 6, TransitionTime: &tt},
		},
		{
			name: "GetSceneMembership", cmd: 0x06,
			encode: func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 0x0607) },
			want:   mattercore.SceneGroupRequest{GroupID: 0x0607},
		},
		{
			name: "CopyScene", cmd: 0x40,
			encode: func(enc *tlv.Encoder) {
				enc.PutUint(tlv.ContextTag(0), 1)
				enc.PutUint(tlv.ContextTag(1), 0x0708)
				enc.PutUint(tlv.ContextTag(2), 7)
				enc.PutUint(tlv.ContextTag(3), 0x0809)
				enc.PutUint(tlv.ContextTag(4), 8)
			},
			want: mattercore.CopySceneRequest{CopyAllScenes: true, GroupFrom: 0x0708, SceneFrom: 7, GroupTo: 0x0809, SceneTo: 8},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enc := tlv.NewEncoder()
			enc.StartStruct(tlv.AnonymousTag())
			tc.encode(enc)
			_ = enc.EndContainer()
			raw, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			dec := tlv.NewDecoder(raw)
			el, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			path := im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: mattercore.ScenesManagementClusterID, Command: tc.cmd}
			fields, err := commandFieldsReader(path, dec, el)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			hf, ok := fields.(spec.HandForm)
			if !ok {
				t.Fatalf("decoded to %T, want a spec.HandForm", fields)
			}
			if !reflect.DeepEqual(hf.Hand, tc.want) {
				t.Fatalf("hand form %#v, want %#v", hf.Hand, tc.want)
			}
		})
	}
}

// sceneRefW2B2a encodes a (group, scene) request body.
func sceneRefW2B2a(group uint16, scene uint8) func(enc *tlv.Encoder) {
	return func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(group))
		enc.PutUint(tlv.ContextTag(1), uint64(scene))
	}
}
