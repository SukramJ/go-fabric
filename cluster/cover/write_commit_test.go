// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
)

// TestWindowCoveringWriteCommitNamesConfigStatus (F-db021da0-57): a Mode
// write names what it changed — Mode, and ConfigStatus when the Mode moved
// it — and the Mode in effect names nothing. Mirrors matter.js: a commit
// names a property only when its value differs (Datasource.ts:1476-1481,
// :1319 at db021da0).
func TestWindowCoveringWriteCommitNamesConfigStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, err := cover.New(cover.Config{FeatureMap: uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name string
		mode wcdef.ModeBitmap
		want []uint32
	}{
		{"MotorDirectionReversed moves ConfigStatus", wcdef.ModeMotorDirectionReversed, []uint32{wcdef.AttrConfigStatus, wcdef.AttrMode}},
		{"the Mode in effect", wcdef.ModeMotorDirectionReversed, nil},
		{"LedFeedback leaves ConfigStatus", wcdef.ModeMotorDirectionReversed | wcdef.ModeLedFeedback, []uint32{wcdef.AttrMode}},
	} {
		got, err := srv.MatterWriteCommit(ctx, wcdef.AttrMode, uint64(step.mode))
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if !slices.Equal(got, step.want) {
			t.Errorf("%s: named %v, want %v", step.name, got, step.want)
		}
	}
}
