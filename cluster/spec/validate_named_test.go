// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
)

// namedEpoch is a named scalar an in-process caller may hand a write.
type namedEpoch uint32

// TestValidateWriteUnwrapsANamedScalar holds a controller write of a
// named scalar type to the same answer as the host's Set and CheckWritten:
// the value is judged by its underlying kind. Before, ValidateWrite judged
// the named type itself and answered CONSTRAINT_ERROR "outside 0..N" for a
// value Set and CheckWritten accept (review II, R2-P6-api-1309).
func TestValidateWriteUnwrapsANamedScalar(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	if err := srv.MatterWrite(context.Background(), hepa.AttrLastChangedTime, namedEpoch(1000)); err != nil {
		t.Errorf("MatterWrite(LastChangedTime, namedEpoch(1000)) = %v, want nil", err)
	}
	if v, _ := srv.Value(hepa.AttrLastChangedTime); v != uint32(1000) {
		t.Errorf("stored LastChangedTime = %#v, want uint32(1000)", v)
	}
	if err := srv.Set(hepa.AttrLastChangedTime, namedEpoch(2000)); err != nil {
		t.Errorf("Set(LastChangedTime, namedEpoch(2000)) = %v, want nil", err)
	}
}
