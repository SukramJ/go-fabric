// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	acldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	basicdef "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	occdef "thingfabric.dev/go-fabric/cluster/spec/occupancysensing"
	onoffdef "thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// heldStub is a hand-written cluster server that accepts every write: what
// the dispatcher refuses never reaches it.
type heldStub struct {
	id     uint32
	attrs  []uint32
	values map[uint32]any
	writes int
}

func (s *heldStub) MatterClusterID() uint32 { return s.id }
func (s *heldStub) MatterRead(a uint32) (any, bool) {
	v, ok := s.values[a]
	return v, ok
}
func (s *heldStub) MatterReportable() []uint32 { return s.attrs }
func (s *heldStub) MatterAttributes() []uint32 { return s.attrs }
func (s *heldStub) MatterWrite(context.Context, uint32, any) error {
	s.writes++
	return nil
}

func (s *heldStub) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

var _ contract.ClusterAttributeLister = (*heldStub)(nil)

// TestWrittenValueIsHeldByTheDefinition: a controller's written value is
// held to the cluster's definition before any server runs — an enum
// outside its values, a string over its length, a number outside its
// bounds answer CONSTRAINT_ERROR and the server is not called (the only
// writable lists with a count constraint, Thermostat Presets and
// Schedules, are the server's to judge: script/clustergen/relaxed.go) — as matter.js validates the value entering the behavior's state
// (packages/node/src/behavior/state/managed/Datasource.ts:667-676 at
// db021da0; the enum validator
// packages/node/src/behavior/state/validation/ValueValidator.ts:167-179,
// the constraint validator's ConstraintError). A hand-written server's own write form is
// read through its declared field map (spec.MapFields).
func TestWrittenValueIsHeldByTheDefinition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		srv    *heldStub
		attrID uint32
		value  any
		want   im.StatusCode
	}{
		{
			name:   "an enum outside its values (OnOff StartUpOnOff 3)",
			srv:    &heldStub{id: onoffdef.ClusterID, attrs: []uint32{onoffdef.AttrStartUpOnOff}, values: map[uint32]any{0xFFFC: uint32(onoffdef.FeatureLighting)}},
			attrID: onoffdef.AttrStartUpOnOff, value: uint64(3), want: im.StatusConstraintError,
		},
		{
			name:   "a string over its length (BasicInformation NodeLabel of 33)",
			srv:    &heldStub{id: basicdef.ClusterID, attrs: []uint32{basicdef.AttrNodeLabel}},
			attrID: basicdef.AttrNodeLabel, value: strings.Repeat("n", 33), want: im.StatusConstraintError,
		},
		{
			name:   "a number outside its bounds (OccupancySensing PirUnoccupiedToOccupiedThreshold 0)",
			srv:    &heldStub{id: occdef.ClusterID, attrs: []uint32{occdef.AttrPirUnoccupiedToOccupiedThreshold}, values: map[uint32]any{0xFFFC: uint32(occdef.FeaturePassiveInfrared)}},
			attrID: occdef.AttrPirUnoccupiedToOccupiedThreshold, value: uint64(0), want: im.StatusConstraintError,
		},
		{
			// GroupKeySetId "1 to 65535" (group-key-management.element.ts:109),
			// read from the hand-written form; its FabricIndex 0 is the
			// accessing fabric's to fill and not judged
			// (AttributeWriteResponse.ts:466-472).
			name:   "a hand-written form: GroupKeyMap mapping key set 0",
			srv:    &heldStub{id: gkmdef.ClusterID, attrs: []uint32{gkmdef.AttrGroupKeyMap}},
			attrID: gkmdef.AttrGroupKeyMap, value: []mattercore.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 0}}, want: im.StatusConstraintError,
		},
		{
			name:   "a hand-written form within the model: GroupKeyMap mapping key set 1",
			srv:    &heldStub{id: gkmdef.ClusterID, attrs: []uint32{gkmdef.AttrGroupKeyMap}},
			attrID: gkmdef.AttrGroupKeyMap, value: []mattercore.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 1}}, want: im.StatusSuccess,
		},
		{
			// ProxyView's conformance is "D", which matter.js compiles as
			// optional (conformance-compiler.ts:234-241); TC_ACL_2_4.py step
			// 16 writes it and expects SUCCESS.
			name:   "an ACL entry with the deprecated ProxyView privilege",
			srv:    &heldStub{id: acldef.ClusterID, attrs: []uint32{acldef.AttrAcl}},
			attrID: acldef.AttrAcl, value: []mattercore.AccessControlEntryStruct{{Privilege: 2, AuthMode: 2}}, want: im.StatusSuccess,
		},
		{
			name:   "an ACL entry with privilege 6",
			srv:    &heldStub{id: acldef.ClusterID, attrs: []uint32{acldef.AttrAcl}},
			attrID: acldef.AttrAcl, value: []mattercore.AccessControlEntryStruct{{Privilege: 6, AuthMode: 2}}, want: im.StatusConstraintError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep := &Endpoint{ID: 2, Source: heldSource{tc.srv}}
			d := NewTopologyDispatcher(makeTopology(ep))
			res := d.Write(context.Background(), concreteAttrPath(2, tc.srv.id, tc.attrID), im.AttributeValue{Value: tc.value})
			if len(res) != 1 || res[0].Status != tc.want {
				t.Fatalf("results %+v, want %v", res, tc.want)
			}
			if wantCalled := tc.want.IsSuccess(); (tc.srv.writes == 1) != wantCalled {
				t.Errorf("the server was called %d times, want called %v", tc.srv.writes, wantCalled)
			}
		})
	}
}

// TestWrittenValueIsHeldOnAGenericServer: a generated server is behind the
// same check.
func TestWrittenValueIsHeldOnAGenericServer(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(occdef.Definition, spec.Options{
		Features: uint32(occdef.FeaturePassiveInfrared), Attributes: []uint32{occdef.AttrHoldTime, occdef.AttrPirOccupiedToUnoccupiedDelay, occdef.AttrPirUnoccupiedToOccupiedDelay, occdef.AttrPirUnoccupiedToOccupiedThreshold},
	}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ep := &Endpoint{ID: 2, Source: genericSource{srv}}
	d := NewTopologyDispatcher(makeTopology(ep))
	res := d.Write(context.Background(), concreteAttrPath(2, occdef.ClusterID, occdef.AttrPirUnoccupiedToOccupiedThreshold), im.AttributeValue{Value: uint64(0)})
	if len(res) != 1 || res[0].Status != im.StatusConstraintError {
		t.Fatalf("results %+v, want CONSTRAINT_ERROR", res)
	}
}

// TestHandWrittenFormsMapEveryDefinitionField: every hand-written write
// form a server declares (spec.MapFields) names a Go field for every field
// of its definition struct.
func TestHandWrittenFormsMapEveryDefinitionField(t *testing.T) {
	t.Parallel()
	if err := spec.VerifyFieldMaps(); err != nil {
		t.Fatal(err)
	}
}

type heldSource struct{ srv *heldStub }

func (heldSource) MatterDeviceType() uint16 { return 0x010A }
func (s heldSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

type genericSource struct{ srv *spec.Server }

func (genericSource) MatterDeviceType() uint16 { return 0x0107 }
func (s genericSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}
