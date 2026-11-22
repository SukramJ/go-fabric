// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpointtest_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/endpoint/endpointtest"
)

// listingServer lists attrs, answers the ones in answers, reports
// reportable and answers FeatureMap as featureMap.
type listingServer struct {
	attrs, answers, reportable []uint32
	featureMap                 any
}

func (s listingServer) MatterClusterID() uint32 { return 0xFFF1FC01 }

func (s listingServer) MatterRead(attr uint32) (any, bool) {
	switch {
	case attr == 0xFFFC:
		return s.featureMap, s.featureMap != nil
	case attr == 0xFFFD:
		return uint16(1), true
	case slices.Contains(s.answers, attr):
		return uint8(0), true
	}
	return nil, false
}

func (listingServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (listingServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (s listingServer) MatterReportable() []uint32                           { return s.reportable }
func (s listingServer) MatterAttributes() []uint32                           { return s.attrs }

// TestAssertClusterServerContract (R2-P5-apidev-01): the helper fails a
// listed attribute that answers no read, a reported attribute that is not
// listed, a FeatureMap of the wrong type and a tolerated attribute that
// answers; it passes a server that keeps the contract.
func TestAssertClusterServerContract(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		srv      listingServer
		tolerate []uint32
		errs     int
		missing  []uint32
	}{
		{"kept", listingServer{attrs: []uint32{0, 1}, answers: []uint32{0, 1}, reportable: []uint32{1}, featureMap: uint32(0)}, nil, 0, nil},
		{"listed, unanswered", listingServer{attrs: []uint32{0, 1}, answers: []uint32{0}, featureMap: uint32(0)}, nil, 1, []uint32{1}},
		{"tolerated", listingServer{attrs: []uint32{0, 1}, answers: []uint32{0}, featureMap: uint32(0)}, []uint32{1}, 0, []uint32{1}},
		{"tolerated and answered", listingServer{attrs: []uint32{0}, answers: []uint32{0}, featureMap: uint32(0)}, []uint32{0}, 1, nil},
		{"reported, not listed", listingServer{attrs: []uint32{0}, answers: []uint32{0, 2}, reportable: []uint32{2}, featureMap: uint32(0)}, nil, 1, nil},
		{"FeatureMap not a uint32", listingServer{attrs: []uint32{0}, answers: []uint32{0}, featureMap: uint8(0)}, nil, 1, nil},
		// The global lists the dispatcher answers are not read; a listed
		// FeatureMap and a manufacturer-specific attribute are (review II, phase 5b).
		{"global lists", listingServer{attrs: []uint32{0, 0xFFF8, 0xFFF9, 0xFFFB, 0xFFFC, 0xFFFD}, answers: []uint32{0}, featureMap: uint32(0)}, nil, 0, nil},
		{"FeatureMap listed, unanswered", listingServer{attrs: []uint32{0, 0xFFFC}, answers: []uint32{0}}, nil, 1, []uint32{0xFFFC}},
		{"vendor attribute listed, unanswered", listingServer{attrs: []uint32{0, 0xFFF10001}, answers: []uint32{0}, featureMap: uint32(0)}, nil, 1, []uint32{0xFFF10001}},
		{"vendor attribute answered", listingServer{attrs: []uint32{0, 0xFFF10001}, answers: []uint32{0, 0xFFF10001}, featureMap: uint32(0)}, nil, 0, nil},
	} {
		r := &recorder{TB: t}
		missing := endpointtest.AssertClusterServerContract(r, c.srv, c.tolerate...)
		if len(r.errs) != c.errs || !slices.Equal(missing, c.missing) {
			t.Errorf("%s: errors %v, unanswered %v; want %d errors, unanswered %v", c.name, r.errs, missing, c.errs, c.missing)
		}
	}
}
