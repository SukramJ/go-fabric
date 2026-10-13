// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

func attrPath(ep *uint16, cluster, attr *uint32) im.ConcreteAttributePath {
	var p im.ConcreteAttributePath
	if ep != nil {
		p.Endpoint, p.HasEndpoint = *ep, true
	}
	if cluster != nil {
		p.Cluster, p.HasCluster = *cluster, true
	}
	if attr != nil {
		p.Attribute, p.HasAttribute = *attr, true
	}
	return p
}

func u16(v uint16) *uint16 { return &v }
func u32(v uint32) *uint32 { return &v }

// TestScope holds newScope to every branch of ReadScope.ts:54-106.
func TestScope(t *testing.T) {
	t.Parallel()
	type probe struct {
		ep                 uint16
		cluster            uint32
		relevant, wildcard bool
	}
	cases := []struct {
		name   string
		paths  []im.ConcreteAttributePath
		probes []probe
	}{
		{"no attribute paths: never (:55-57)", nil, []probe{{0, 6, false, false}}},
		{"full wildcard: always (:69-73)", []im.ConcreteAttributePath{attrPath(nil, nil, nil)}, []probe{{3, 6, true, true}}},
		{"full wildcard naming an attribute: any only (:63-66)", []im.ConcreteAttributePath{attrPath(nil, nil, u32(0))}, []probe{{3, 6, true, false}}},
		{"wildcard endpoint with a cluster (:76)", []im.ConcreteAttributePath{attrPath(nil, u32(6), nil)}, []probe{{1, 6, true, true}, {2, 6, true, true}, {1, 8, false, false}}},
		{"an endpoint whole (:81-84)", []im.ConcreteAttributePath{attrPath(u16(1), nil, nil)}, []probe{{1, 6, true, true}, {2, 6, false, false}}},
		{"an endpoint and a cluster (:86-96)", []im.ConcreteAttributePath{attrPath(u16(1), u32(6), nil)}, []probe{{1, 6, true, true}, {1, 8, false, false}, {2, 6, false, false}}},
		{"an endpoint whole wins over its clusters (:88-91)", []im.ConcreteAttributePath{attrPath(u16(1), nil, nil), attrPath(u16(1), u32(6), nil)}, []probe{{1, 8, true, true}}},
		{"an attribute: any, not all (:63-66, :86-87)", []im.ConcreteAttributePath{attrPath(u16(1), u32(6), u32(0))}, []probe{{1, 6, false, false}}},
	}
	for _, tc := range cases {
		sc := newScope(tc.paths, false)
		for _, p := range tc.probes {
			if got := sc.isRelevant(p.ep, p.cluster); got != p.relevant {
				t.Errorf("%s: isRelevant(%d, %d) = %v", tc.name, p.ep, p.cluster, got)
			}
			if got := sc.isWildcard(p.ep, p.cluster); got != p.wildcard {
				t.Errorf("%s: isWildcard(%d, %d) = %v", tc.name, p.ep, p.cluster, got)
			}
		}
	}
	if !newScope(nil, false).fabricFiltered || newScope(nil, true).fabricFiltered {
		t.Fatal("fabricFiltered is not !NoFabricFilter (:50)")
	}
}
