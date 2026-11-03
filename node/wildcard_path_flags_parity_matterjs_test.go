// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of the WildcardPathFlags cases of matter.js
// packages/node/test/endpoint/server/InteractionProtocolTest.ts at 862ad75f:
// the wildcardTestCases table (:1049-1105) the "replies with attributes with
// ${testCase} wildcard Filter" case runs (:1262-1290), and the
// WildcardFilterConfigurationVersion cases (:1292-1322).
//
// matter.js reads through handleReadRequest on a mocked exchange; the port
// reads over the wire from a commissioned fabrictest pair, the paths carrying
// tag 6 and 7 of the AttributePathIB. The Go root's BasicInformation serves
// the 23 attributes the matter.js node does (cluster/core/basic_information.go
// with SerialNumber); the Wi-Fi node mounts WiFiNetworkDiagnostics without
// features, as node.behaviors.require(WiFiNetworkDiagnosticsServer, …) does.
package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func TestWildcardPathFlagsParityMatterJS(t *testing.T) { //nolint:tparallel // the subtests share the site this test holds; parallel ones would wait for a parallel slot that tests blocked on the site slot hold
	t.Parallel()

	m := newMockPair(t, mockOptions{configure: func(cfg *node.Config) {
		cfg.Network.WiFi = &node.WiFi{Reporter: wifiLink{}}
	}})

	read := func(t *testing.T, path im.ConcreteAttributePath) int {
		t.Helper()
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}})
		if err != nil {
			t.Fatal(err)
		}
		return len(r.Attributes)
	}
	wildcardEndpoint := func(cluster, flags uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Cluster: cluster, HasCluster: true, WildcardPathFlags: flags}
	}

	// InteractionProtocolTest.ts:1049-1105, the counts and their comments
	// verbatim.
	cases := []struct {
		name    string
		cluster uint32
		flags   uint32
		count   int
	}{
		{"no", basicinfo.ClusterID, 0, 23},
		{"skipRootNode", basicinfo.ClusterID, im.WildcardSkipRootNode, 0},                                                // all sorted out
		{"skipGlobalAttributes", basicinfo.ClusterID, im.WildcardSkipGlobalAttributes, 20},                               // 3 less
		{"skipAttributeList", basicinfo.ClusterID, im.WildcardSkipAttributeList, 22},                                     // 1 less
		{"skipCommandLists", basicinfo.ClusterID, im.WildcardSkipCommandLists, 21},                                       // 2 less
		{"skipFixedAttributes", basicinfo.ClusterID, im.WildcardSkipFixedAttributes, 4},                                  // 19 less
		{"skipChangesOmittedAttributes", basicinfo.ClusterID, im.WildcardSkipChangesOmittedAttributes, 23},               // nothing filtered
		{"no for WiFiDiag", wifidiagdef.ClusterID, 0, 10},                                                                // nothing filtered
		{"skipChangesOmittedAttributes for WiFiDiag", wifidiagdef.ClusterID, im.WildcardSkipChangesOmittedAttributes, 9}, // 1 filtered
		{"skipDiagnosticsClusters", wifidiagdef.ClusterID, im.WildcardSkipDiagnosticsClusters, 0},                        // all filtered
	}
	for _, c := range cases {
		t.Run("replies with attributes with "+c.name+" wildcard Filter", func(t *testing.T) {
			if got := read(t, wildcardEndpoint(c.cluster, c.flags)); got != c.count {
				t.Fatalf("%d attributes, want %d", got, c.count)
			}
		})
	}

	// InteractionProtocolTest.ts:1292-1322: ConfigurationVersion defaults
	// to 1; skipGlobalAttributes drops 3 of the 23 BasicInformation
	// attributes while the filter version is current.
	withVersion := func(version uint32, has bool) im.ConcreteAttributePath {
		p := wildcardEndpoint(basicinfo.ClusterID, im.WildcardSkipGlobalAttributes)
		p.WildcardFilterConfigurationVersion, p.HasWildcardFilterConfigurationVersion = version, has
		return p
	}
	t.Run("applies WildcardPathFlags when the filter version is current", func(t *testing.T) {
		if got := read(t, withVersion(0, false)); got != 20 { // omitted == current
			t.Fatalf("omitted filter version: %d attributes, want 20", got)
		}
		if got := read(t, withVersion(1, true)); got != 20 { // equal to current
			t.Fatalf("current filter version: %d attributes, want 20", got)
		}
	})
	t.Run("ignores WildcardPathFlags when the configuration changed past the filter version", func(t *testing.T) {
		if got := read(t, withVersion(0, true)); got != 23 { // stale filter -> flags do not apply
			t.Fatalf("stale filter version: %d attributes, want 23", got)
		}
	})
}
