// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// featureMapOverride is a server whose FeatureMap read answers what the
// test says, standing in for a hand-written server that is not built on
// its definition.
type featureMapOverride struct {
	contract.ClusterServer
	features any
}

func (s *featureMapOverride) MatterRead(id uint32) (any, bool) {
	if id == spec.AttrFeatureMap {
		return s.features, true
	}
	return s.ClusterServer.MatterRead(id)
}

func newGenerated(tb testing.TB, features uint32) contract.ClusterServer {
	tb.Helper()
	inst, err := spec.New(serverDef, spec.Options{Features: features})
	if err != nil {
		tb.Fatal(err)
	}
	return &fake{Instance: inst}
}

// TestCheckHandWrittenServerAcceptsAServerOnItsDefinition: a server whose
// FeatureMap and elements agree with the definition passes.
func TestCheckHandWrittenServerAcceptsAServerOnItsDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckHandWrittenServer(t, newGenerated(t, 1), serverDef)
}

// TestCheckHandWrittenServerReportsTheFeatureMap: a FeatureMap that is not
// a uint32, and one that sets a bit the definition does not define, are
// reported with the cluster's name.
func TestCheckHandWrittenServerReportsTheFeatureMap(t *testing.T) {
	t.Parallel()

	wantReport(t, "not a uint32", run(t, func(tb *recorder) {
		spectest.CheckHandWrittenServer(tb, &featureMapOverride{ClusterServer: newGenerated(tb, 1), features: "x"}, serverDef)
	}), "want a uint32")

	wantReport(t, "an undefined bit", run(t, func(tb *recorder) {
		spectest.CheckHandWrittenServer(tb, &featureMapOverride{ClusterServer: newGenerated(tb, 1), features: uint32(1 | 1<<31)}, serverDef)
	}), "the definition does not define")
}
