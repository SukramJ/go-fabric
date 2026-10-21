// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/AttributeWriteResponseTest.ts at
// 862ad75f: attribute writes against a server node — concrete and wildcard,
// the access check, read-only attributes, data versions, constraints and an
// invalid wildcard.
//
// matter.js runs AttributeWriteResponse on a MockServerNode; the port writes
// over the wire from a commissioned fabrictest pair. matter.js's writeAttr
// acts at the mock's default access level, Operate
// (mock-server-node.ts:180); writeAttrAsAdmin on a fabric as administrator —
// the pair's own subject. A raw request (the wildcard with a DataVersion,
// the path without its attribute) goes out over fabrictest.Exchange, past
// the controller's own formulation, which refuses it. matter.js's counts
// are not on the wire; the statuses are.
//
// Not ported: "writes with invalid wildcard combination" (matter.js answers
// the whole interaction with a StatusResponse of InvalidAction,
// packages/protocol/src/action/server/AttributeWriteResponse.ts:279-285;
// the Go server answers an AttributeStatus of InvalidAction for the path —
// a response-shape change) and the two "write skips an attribute that
// another write interaction holds" cases (AttributeWriteResponse's
// isHeldByOtherWrite lock, :355, which the Go server does not have).
package node_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

func TestAttributeWriteResponseParity(t *testing.T) {
	t.Parallel()

	nodeLabel := attrPath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	wildLabel := nodeLabel
	wildLabel.HasEndpoint = false
	vendorName := attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorName)
	wildVendor := vendorName
	wildVendor.HasEndpoint = false
	tooLong := "12345678901234567890123456789012345" // 32 chars max

	cases := []struct {
		name      string
		privilege store.Privilege
		write     interaction.WriteRequest
		want      []interaction.WriteStatus
	}{
		{
			name: "writes concrete attribute", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: nodeLabel, Value: strVal("Test Label")},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusSuccess}},
		},
		{
			name: "writes endpoint wildcard attribute", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: wildLabel, Value: strVal("Test Label 2")},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusSuccess}},
		},
		{
			name: "writes concrete attribute with ACL error", privilege: store.PrivilegeOperate,
			write: interaction.WriteRequest{Path: nodeLabel, Value: strVal("Test Label")},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusUnsupportedAccess}},
		},
		{
			// BasicInformation.LocalConfigDisabled (0x10): optional, absent
			// here, write privilege Manage.
			name: "writes model-known absent high-privilege attribute as unsupported attribute for view-only subject", privilege: store.PrivilegeView,
			write: interaction.WriteRequest{Path: attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocalConfigDisabled), Value: boolVal(true)},
			want:  []interaction.WriteStatus{{Path: attrPath(0, basicinfo.ClusterID, basicinfo.AttrLocalConfigDisabled), Status: im.StatusUnsupportedAttribute}},
		},
		{
			name: "writes endpoint wildcard attribute with ACL issue", privilege: store.PrivilegeOperate,
			write: interaction.WriteRequest{Path: wildLabel, Value: strVal("Test Label 2")},
		},
		{
			name: "writes non-writable concrete attribute with error", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: vendorName, Value: strVal(mockVendorName)},
			want:  []interaction.WriteStatus{{Path: vendorName, Status: im.StatusUnsupportedWrite}},
		},
		{
			name: "writes non-writable wildcard attribute with no error returned", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: wildVendor, Value: strVal(mockVendorName)},
		},
		{
			name: "writes version mismatch concrete attribute with error", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: nodeLabel, Value: strVal("Test Label"), DataVersion: new(uint32(99))},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusDataVersionMismatch}},
		},
		{
			name: "writes concrete attribute with constraint error", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: nodeLabel, Value: strVal(tooLong)},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusConstraintError}},
		},
		{
			name: "writes wildcard attribute with constraint error", privilege: store.PrivilegeAdminister,
			write: interaction.WriteRequest{Path: wildLabel, Value: strVal(tooLong)},
			want:  []interaction.WriteStatus{{Path: nodeLabel, Status: im.StatusConstraintError}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := newMockPair(t, mockOptions{light: true})
			ctx := parityCtx(t)
			if c.privilege != store.PrivilegeAdminister {
				if err := m.pair.Grant(ctx, c.privilege); err != nil {
					t.Fatal(err)
				}
			}
			got := writeStatuses(ctx, t, m, c.write)
			assertWriteStatuses(t, got, c.want)
		})
	}

	t.Run("writes version mismatch wildcard attribute where mismatch got ignored", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		a := rawWrite(t, m, im.WriteRequest{Writes: []im.AttributeWrite{{
			Path: wildLabel, Value: im.AttributeValue{Value: strVal("Test Label")}, DataVersion: 99, HasDataVersion: true,
		}}})
		if a.status != nil {
			t.Fatalf("StatusResponse 0x%02x", *a.status)
		}
		if len(a.resp.Responses) != 1 || a.resp.Responses[0].Status.Status != im.StatusSuccess || !samePath(a.resp.Responses[0].Path, nodeLabel) {
			t.Fatalf("responses %+v, want Success at 0/40/5", a.resp.Responses)
		}
	})
}

// writeStatuses writes over the pair's interaction and returns every
// status the device answered.
func writeStatuses(ctx context.Context, t *testing.T, m *mockPair, writes ...interaction.WriteRequest) interaction.WriteStatuses {
	t.Helper()
	var got interaction.WriteStatuses
	if err := m.site.Run(ctx, func(ctx context.Context) error {
		var err error
		got, err = m.pair.Interaction.Write(ctx, writes, interaction.WriteOptions{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

func assertWriteStatuses(t *testing.T, got interaction.WriteStatuses, want []interaction.WriteStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("statuses %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Status != want[i].Status || !samePath(got[i].Path, want[i].Path) || got[i].ClusterStatus != nil {
			t.Fatalf("status %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// samePath compares the endpoint, cluster and attribute of two paths.
func samePath(a, b im.ConcreteAttributePath) bool {
	return a.Endpoint == b.Endpoint && a.Cluster == b.Cluster && a.Attribute == b.Attribute
}
