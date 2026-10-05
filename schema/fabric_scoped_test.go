// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestFabricScopedInvokeMatchesMatterJS reads the commands matter.js marks
// "F" out of the element files the table cites, when a matter.js checkout is
// present, and holds the table against them: every fabric-scoped request of a
// listed cluster is in the table, and nothing else is.
func TestFabricScopedInvokeMatchesMatterJS(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "matter.js", "packages", "model", "src", "standard", "elements")
	files := map[uint32]string{
		0x001F: "access-control.element.ts", 0x0030: "general-commissioning.element.ts",
		0x003E: "operational-credentials.element.ts", 0x003F: "group-key-management.element.ts",
		0x0004: "groups.element.ts", 0x0065: "groupcast.element.ts", 0x0062: "scenes-management.element.ts",
		0x0046: "icd-management.element.ts", 0x0038: "time-synchronization.element.ts",
		0x002A: "ota-software-update-requestor.element.ts",
	}
	re := regexp.MustCompile(`name: "([A-Za-z]+)", id: (0x[0-9a-f]+), access: "([^"]*)"[^}]*direction: "request"`)
	for cluster, file := range files {
		src, err := os.ReadFile(filepath.Join(dir, file)) //nolint:gosec // a fixed sibling-checkout path
		if err != nil {
			t.Skipf("no matter.js checkout at %s: %v", dir, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			id, _ := strconv.ParseUint(m[2], 0, 32)
			want := regexp.MustCompile(`(^|\s)F(\s|$)`).MatchString(m[3])
			if got := schema.IsFabricScopedInvoke(cluster, uint32(id)); got != want {
				t.Errorf("%s 0x%04X/%s (access %q): IsFabricScopedInvoke = %v, want %v", file, cluster, m[1], m[3], got, want)
			}
		}
	}
	if schema.IsFabricScopedInvoke(0x0006, 0x00) {
		t.Error("OnOff.Off is not fabric-scoped")
	}
}
