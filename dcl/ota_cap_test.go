// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestOTAVersionsExaminedPerCheckAreBounded holds R2-C2-bound-1902: a
// ledger that lists a long version series costs at most
// maxOTAVersionsPerCheck version lookups per check — the highest ones —
// and the cut is logged.
func TestOTAVersionsExaminedPerCheckAreBounded(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	const listed = 200
	versions := make([]int, listed)
	for i := range versions {
		versions[i] = i + 1
	}
	f.json("/dcl/model/versions/65521/32768", 200, map[string]any{"modelVersions": map[string]any{
		"vid": 65521, "pid": 32768, "softwareVersions": versions, "schemaVersion": 0,
	}})
	var log bytes.Buffer
	o := &OTAVersions{Client: f.client(), Logger: slog.New(slog.NewTextHandler(&log, nil))}
	if _, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 0, nil); ok {
		t.Fatal("an update from a ledger without version records")
	}
	asked := 0
	for v := 1; v <= listed; v++ {
		if n := f.hit(fmt.Sprintf("/dcl/model/versions/65521/32768/%d", v)); n > 0 {
			asked++
			if v <= listed-maxOTAVersionsPerCheck {
				t.Errorf("version %d asked; only the highest %d may be", v, maxOTAVersionsPerCheck)
			}
		}
	}
	if asked != maxOTAVersionsPerCheck {
		t.Fatalf("%d versions asked, want %d", asked, maxOTAVersionsPerCheck)
	}
	if !strings.Contains(log.String(), "dcl.ota.too_many_versions") {
		t.Fatalf("the cut was not logged:\n%s", log.String())
	}
}
