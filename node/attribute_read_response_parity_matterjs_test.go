// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/AttributeReadResponseTest.ts at
// 862ad75f: attribute reads against a server node — concrete, wildcard,
// version-filtered, non-existent, and the full wildcards with and without a
// device endpoint.
//
// matter.js runs AttributeReadResponse on a MockServerNode with a mocked
// exchange of the given access level; the port reads over the wire from a
// commissioned fabrictest pair (the access level is fabrictest Pair.Grant).
// Where matter.js counts on the mock's composition, the Go node's
// composition differs in two places, each asserted as such:
//
//   - the root endpoint serves more clusters than MockServerNode's nine
//     (node/root.go: Localization, Time Format, Unit Localization, Power
//     Source Configuration, Network Commissioning, Diagnostic Logs, Software
//     and Ethernet Diagnostics, Time Synchronization, Fixed and User Label)
//     and BasicInformation serves SerialNumber, which the mock lacks; the
//     nine clusters the mock serves carry the mock's counts exactly, every
//     other cluster reports each attribute of its AttributeList once;
//   - the device side is a bridge: endpoint 1 is the Aggregator, a device is
//     a bridged endpoint below it (BridgedDeviceBasicInformation added).
//
// Data versions are seeded at random on the Go node (matter.js's mock seeds
// 0x80808081), so the version filter reads the version first.
package node_test

import (
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// rootFullClusterList is AttributeReadResponseTest.ts:15-25
// ROOT_ENDPOINT_FULL_CLUSTER_LIST: MockServerNode's root clusters and their
// attribute counts.
var rootFullClusterList = map[uint32]int{29: 9, 31: 11, 40: 22, 48: 10, 51: 11, 60: 8, 62: 11, 63: 9, 101: 10}

// lightClusterCounts is the OnOffLight endpoint of
// AttributeReadResponseTest.ts:242-248.
var lightClusterCounts = map[uint32]int{3: 7, 4: 6, 6: 10, 29: 9, 98: 7}

// goRootExtra is what the Go root serves beyond the mock's counts:
// BasicInformation.SerialNumber.
const goRootExtra = 1

func TestAttributeReadResponseParity(t *testing.T) {
	t.Parallel()

	vendorName := attrPath(0, basicinfo.ClusterID, basicinfo.AttrVendorName)

	t.Run("reads concrete attribute", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{vendorName}})
		if err != nil {
			t.Fatal(err)
		}
		assertOneValue(t, r, vendorName, mockVendorName)
	})

	t.Run("reads endpoint wildcard attribute", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		wild := vendorName
		wild.HasEndpoint, wild.Endpoint = false, 0
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{wild}})
		if err != nil {
			t.Fatal(err)
		}
		assertOneValue(t, r, vendorName, mockVendorName)
	})

	t.Run("reads concrete attribute with version filter", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		ctx := parityCtx(t)
		cur, err := fabrictest.Read(ctx, m.pair, vendorName)
		if err != nil {
			t.Fatal(err)
		}
		// The request names cluster and attribute only (endpoint wildcard),
		// the filter the root's BasicInformation at its current version.
		wild := vendorName
		wild.HasEndpoint, wild.Endpoint = false, 0
		r, err := fabrictest.ReadWith(ctx, m.pair, interaction.ReadRequest{
			Attributes:         []im.ConcreteAttributePath{wild},
			DataVersionFilters: []im.DataVersionFilter{{Endpoint: 0, Cluster: basicinfo.ClusterID, DataVersion: cur.DataVersion}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Empty() {
			t.Fatalf("data %+v, statuses %+v, want none", r.Attributes, r.Statuses)
		}
	})

	t.Run("reads non-existent concrete endpoint", func(t *testing.T) {
		t.Parallel()
		// Endpoint 2 is free on a node without a device (endpoint 1 is the
		// Aggregator).
		m := newMockPair(t, mockOptions{})
		p := vendorName
		p.Endpoint = 2
		assertOneStatus(t, m, p, im.StatusUnsupportedEndpoint, store.PrivilegeAdminister)
	})

	t.Run("reads non-existent concrete attribute", func(t *testing.T) {
		t.Parallel()
		// matter.js reads serialNumber (15), absent on MockServerNode; the
		// Go node always serves it (cluster/core/basic_information.go), so
		// the port reads ManufacturingDate (11), absent on both.
		m := newMockPair(t, mockOptions{light: true})
		assertOneStatus(t, m, attrPath(0, basicinfo.ClusterID, basicinfo.AttrManufacturingDate), im.StatusUnsupportedAttribute, store.PrivilegeAdminister)
	})

	t.Run("reads model-known absent high-privilege attribute as unsupported attribute for view-only subject", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		// GeneralCommissioning.TCAcceptedVersion: read privilege
		// Administer, absent without the TC feature.
		assertOneStatus(t, m, attrPath(0, gencomm.ClusterID, gencomm.AttrTcAcceptedVersion), im.StatusUnsupportedAttribute, store.PrivilegeView)
	})

	t.Run("reads wildcard endpoint & attributes", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Cluster: basicinfo.ClusterID, HasCluster: true}}})
		if err != nil {
			t.Fatal(err)
		}
		got := countAttrs(r)
		if len(got) != 1 || len(got[0]) != 1 || got[0][basicinfo.ClusterID] != rootFullClusterList[basicinfo.ClusterID]+goRootExtra {
			t.Fatalf("counts %v, want {0: {40: %d}}", got, rootFullClusterList[basicinfo.ClusterID]+goRootExtra)
		}
		if _, ok := r.Value(0, basicinfo.ClusterID, basicinfo.AttrSerialNumber); !ok {
			t.Fatal("no SerialNumber — the one attribute the Go root adds")
		}
		if len(r.Statuses) != 0 {
			t.Fatalf("statuses %+v", r.Statuses)
		}
	})

	t.Run("reads full wildcard", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		r := readAll(t, m)
		assertRootCounts(t, r)
		if len(r.Statuses) != 0 {
			t.Fatalf("statuses %+v", r.Statuses)
		}
	})

	t.Run("reads full wildcard with adding and removing endpoint", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		before := countAttrs(readAll(t, m))
		ctx := parityCtx(t)
		light := newLight(t, m.site, "light:added")
		if err := m.pair.Device.Act(func(n *node.Node) error { return n.Aggregator().Add(ctx, light) }); err != nil {
			t.Fatal(err)
		}
		ep, ok := m.pair.Device.EndpointOf("light:added")
		if !ok {
			t.Fatal("the added light has no endpoint")
		}
		with := readAll(t, m)
		assertRootCounts(t, with)
		got := countAttrs(with)
		for c, want := range lightClusterCounts {
			if got[ep][c] != want {
				t.Errorf("light endpoint %d cluster %d: %d attributes, want %d", ep, c, got[ep][c], want)
			}
		}
		if len(got) != len(before)+1 {
			t.Fatalf("endpoints %d after the add, %d before", len(got), len(before))
		}
		if err := m.pair.Device.Act(func(n *node.Node) error { return n.Aggregator().Remove(ctx, endpoint.StringKey("light:added")) }); err != nil {
			t.Fatal(err)
		}
		after := readAll(t, m)
		assertRootCounts(t, after)
		if got := countAttrs(after); len(got) != len(before) || got[ep] != nil {
			t.Fatalf("endpoints after the remove %v", got)
		}
	})

	t.Run("reads attributeList global Attribute full wildcard", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Attribute: 0xFFFB, HasAttribute: true}}})
		if err != nil {
			t.Fatal(err)
		}
		got := countAttrs(r)
		for c := range rootFullClusterList {
			if got[0][c] != 1 {
				t.Errorf("root cluster %d: %d AttributeList values, want 1", c, got[0][c])
			}
		}
		for ep, clusters := range got {
			for c, n := range clusters {
				if n != 1 {
					t.Errorf("endpoint %d cluster %d: %d AttributeList values", ep, c, n)
				}
			}
		}
		if len(r.Statuses) != 0 {
			t.Fatalf("statuses %+v", r.Statuses)
		}
	})
}

// assertOneValue asserts a report of exactly one attribute value at path.
func assertOneValue(t *testing.T, r *interaction.Report, path im.ConcreteAttributePath, want string) {
	t.Helper()
	if len(r.Attributes) != 1 || len(r.Statuses) != 0 {
		t.Fatalf("data %+v, statuses %+v, want one value", r.Attributes, r.Statuses)
	}
	a := r.Attributes[0]
	if a.Path.Endpoint != path.Endpoint || a.Path.Cluster != path.Cluster || a.Path.Attribute != path.Attribute || a.Value.El.String != want || a.DataVersion == 0 {
		t.Fatalf("value %+v, want %q at %d/%d/%d with a data version", a, want, path.Endpoint, path.Cluster, path.Attribute)
	}
}

// assertOneStatus reads path as a subject of privilege and asserts exactly
// one attribute status at path.
func assertOneStatus(t *testing.T, m *mockPair, path im.ConcreteAttributePath, want im.StatusCode, privilege store.Privilege) {
	t.Helper()
	ctx := parityCtx(t)
	if privilege != store.PrivilegeAdminister {
		if err := m.pair.Grant(ctx, privilege); err != nil {
			t.Fatal(err)
		}
	}
	r, err := fabrictest.ReadWith(ctx, m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Attributes) != 0 || len(r.Statuses) != 1 {
		t.Fatalf("data %+v, statuses %+v, want one status", r.Attributes, r.Statuses)
	}
	s := r.Statuses[0]
	if s.Path.Endpoint != path.Endpoint || s.Path.Cluster != path.Cluster || s.Path.Attribute != path.Attribute || s.Status != want {
		t.Fatalf("status %+v, want 0x%02x at %d/%d/%d", s, want, path.Endpoint, path.Cluster, path.Attribute)
	}
}

// readAll is the full wildcard read, Read.Attribute().
func readAll(t *testing.T, m *mockPair) *interaction.Report {
	t.Helper()
	r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// assertRootCounts asserts the root of a full wildcard: the mock's nine
// clusters at the mock's counts (BasicInformation plus SerialNumber), every
// other root cluster each attribute of its AttributeList once.
func assertRootCounts(t *testing.T, r *interaction.Report) {
	t.Helper()
	got := countAttrs(r)[0]
	for c, want := range rootFullClusterList {
		if c == basicinfo.ClusterID {
			want += goRootExtra
		}
		if got[c] != want {
			t.Errorf("root cluster %d: %d attributes, want %d", c, got[c], want)
		}
	}
	for c, n := range got {
		if l, ok := listLen(r, 0, c, 0xFFFB); !ok || l != n {
			t.Errorf("root cluster %d: %d attributes, AttributeList %d", c, n, l)
		}
	}
}
