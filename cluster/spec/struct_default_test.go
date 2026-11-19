// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
)

// TestStructDefaultIsServed: the generated server starts a struct
// attribute at the default its fields state — OperationalError at
// {ErrorStateId: 0} (operational-state.element.ts:110-111 at b25ed960).
func TestStructDefaultIsServed(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(opdef.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := srv.MatterRead(opdef.AttrOperationalError)
	if got, isStruct := v.(opdef.ErrorStateStruct); !ok || !isStruct || got.ErrorStateId != 0 {
		t.Fatalf("OperationalError = %#v (%v), want ErrorStateStruct{ErrorStateId: 0}", v, ok)
	}
}
