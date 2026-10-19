// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/general-diagnostics/GeneralDiagnosticsFeaturesTest.ts
// at 862ad75f: a GeneralDiagnostics server with a feature selection of its
// own, and the DataModelTest feature held against the node's
// maxPathsPerInvoke (GeneralDiagnosticsServer.ts:51-52 the default
// selection, #assertDataModelTest :125-143, run from #online :352).
//
// matter.js builds a MockServerNode whose root carries
// GeneralDiagnosticsServer.with() and a basicInformation.maxPathsPerInvoke,
// and starts it. node.Config carries neither a GeneralDiagnostics feature
// selection nor a MaxPathsPerInvoke (the node serves DMTEST and
// im.DefaultMaxPathsPerInvoke, 10), so the port builds the server in its
// package with core.NewGeneralDiagnosticsWith and runs the check the node
// start runs in matter.js, CheckDataModelTest, with the limit the root's
// BasicInformation advertises. A failed start is the check's error; the
// advisory is an INFO record on the logger the check is given.
package core_test

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// maxPathsOf is the MaxPathsPerInvoke a BasicInformation server built with
// limit advertises: matter.js's basicInformation state, its default
// (limit 0) DEFAULT_MAX_PATHS_PER_INVOKE.
func maxPathsOf(t *testing.T, limit uint16) uint16 {
	t.Helper()
	cfg := validBasicInfoConfig()
	cfg.MaxPathsPerInvoke = limit
	b, err := core.NewBasicInformation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := b.MatterRead(0x0016) // MaxPathsPerInvoke
	n, ok := v.(uint16)
	if !ok {
		t.Fatalf("MaxPathsPerInvoke = %T", v)
	}
	return n
}

// countAdvisories is countAdvisories (GeneralDiagnosticsFeaturesTest.ts:12-30):
// the default server (DataModelTest selected) started against limit, and
// the INFO lines naming "DataModelTest feature is enabled".
func countAdvisories(t *testing.T, limit uint16) int {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	g := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
	if err := g.CheckDataModelTest(maxPathsOf(t, limit), logger); err != nil {
		t.Fatalf("the default selection refused: %v", err)
	}
	return strings.Count(buf.String(), "DataModelTest feature is enabled")
}

// TestGeneralDiagnosticsFeatures ports the five cases of
// "GeneralDiagnosticsServer features".
func TestGeneralDiagnosticsFeatures(t *testing.T) {
	t.Parallel()

	// :33-43 — matter.js reads totalOperationalHours through the type
	// without features; the Go server without DMTEST serves it, 0, and
	// FeatureMap and the command lists drop the feature's PayloadTest pair.
	t.Run("constructs with a feature selection of its own", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnosticsWith(core.BootReasonPowerOnReboot, core.GeneralDiagnosticsFeatures{})
		if err := g.CheckDataModelTest(maxPathsOf(t, 1), nil); err != nil {
			t.Fatalf("start with maxPathsPerInvoke 1: %v", err)
		}
		if v, _ := g.MatterRead(0x0003); v != uint32(0) { // TotalOperationalHours
			t.Errorf("TotalOperationalHours = %v, want 0", v)
		}
		if v, _ := g.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(0) {
			t.Errorf("FeatureMap = %v, want 0", v)
		}
		if slices.Contains(g.MatterAcceptedCommands(), 0x03) || slices.Contains(g.MatterGeneratedCommands(), 0x04) {
			t.Errorf("PayloadTestRequest/Response listed without DataModelTest: %v / %v", g.MatterAcceptedCommands(), g.MatterGeneratedCommands())
		}
		if _, err := g.MatterInvoke(t.Context(), 0x03, nil); !errors.Is(err, im.ErrUnsupportedCommand) {
			t.Errorf("PayloadTestRequest without DataModelTest: %v, want UNSUPPORTED_COMMAND", err)
		}
	})

	// :45-60 — the default limit (10) without DataModelTest fails the start.
	t.Run("rejects a node that accepts several paths per invoke without DataModelTest", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnosticsWith(core.BootReasonPowerOnReboot, core.GeneralDiagnosticsFeatures{})
		err := g.CheckDataModelTest(maxPathsOf(t, 0), nil)
		if err == nil || !strings.Contains(err.Error(), "DataModelTest feature is mandatory with a maxPathsPerInvoke of 10") {
			t.Errorf("CheckDataModelTest = %v, want the mandatory-feature error for 10", err)
		}
	})

	// :62-68
	t.Run("accepts a node that accepts one path per invoke without DataModelTest", func(t *testing.T) {
		t.Parallel()
		g := core.NewGeneralDiagnosticsWith(core.BootReasonPowerOnReboot, core.GeneralDiagnosticsFeatures{})
		if err := g.CheckDataModelTest(maxPathsOf(t, 1), nil); err != nil {
			t.Errorf("CheckDataModelTest(1) = %v, want nil", err)
		}
	})

	// :70-72
	t.Run("advises disabling DataModelTest when only one path per invoke is accepted", func(t *testing.T) {
		t.Parallel()
		if n := countAdvisories(t, 1); n != 1 {
			t.Errorf("advisories = %d, want 1", n)
		}
	})

	// :74-77
	t.Run("stays silent when more than one path per invoke is accepted", func(t *testing.T) {
		t.Parallel()
		if n := countAdvisories(t, 0); n != 0 {
			t.Errorf("advisories at the default limit = %d, want 0", n)
		}
		if n := countAdvisories(t, 3); n != 0 {
			t.Errorf("advisories at 3 = %d, want 0", n)
		}
	})
}
