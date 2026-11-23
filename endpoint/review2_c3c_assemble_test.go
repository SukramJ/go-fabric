// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"
)

// TestAssembleRefusesAnEmptySpec (R2-C2-err-0453, decision 528): a bridged
// spec with no Source, no Measurement and no PowerSource serves no cluster —
// not even a Descriptor — yet would be listed in the PartsList. The
// assembly refuses it, naming the key.
func TestAssembleRefusesAnEmptySpec(t *testing.T) {
	t.Parallel()
	empty := Spec{StableKey: StringKey("empty-spec"), DeviceType: 0x0100, FriendlyName: "e"}
	_, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{empty}}})
	if err == nil {
		t.Fatal("Assemble of a spec with no Source, Measurement or PowerSource succeeded")
	}
	if !strings.Contains(err.Error(), "empty-spec") {
		t.Errorf("err = %v, want it to name the key", err)
	}
}
