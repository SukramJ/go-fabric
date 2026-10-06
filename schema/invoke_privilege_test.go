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

// TestInvokePrivilegeMatchesMatterJS reads every request command of the
// cited element files out of a matter.js checkout, when one is present, and
// holds InvokePrivilege to its access string: "A" is Administer, "M" Manage,
// anything else the Operate default.
func TestInvokePrivilegeMatchesMatterJS(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "matter.js", "packages", "model", "src", "standard", "elements")
	files := map[uint32]string{
		0x0003: "identify.element.ts", 0x0004: "groups.element.ts", 0x001F: "access-control.element.ts",
		0x002A: "ota-software-update-requestor.element.ts", 0x0030: "general-commissioning.element.ts",
		0x0031: "network-commissioning.element.ts", 0x0033: "general-diagnostics.element.ts",
		0x0034: "software-diagnostics.element.ts", 0x0037: "ethernet-network-diagnostics.element.ts",
		0x0038: "time-synchronization.element.ts", 0x003C: "administrator-commissioning.element.ts",
		0x003E: "operational-credentials.element.ts", 0x003F: "group-key-management.element.ts",
		0x0046: "icd-management.element.ts", 0x0062: "scenes-management.element.ts",
		0x0065: "groupcast.element.ts", 0x0101: "door-lock.element.ts", 0x0104: "closure-control.element.ts",
		0x0201: "thermostat.element.ts", 0x0006: "on-off.element.ts", 0x0008: "level-control.element.ts",
	}
	re := regexp.MustCompile(`Command\(\s*\{\s*name: "([A-Za-z]+)", id: (0x[0-9a-f]+)(?:, access: "([^"]*)")?([^}]*)\}`)
	checked := 0
	for cluster, file := range files {
		src, err := os.ReadFile(filepath.Join(dir, file)) //nolint:gosec // a fixed sibling-checkout path
		if err != nil {
			t.Skipf("no matter.js checkout at %s: %v", dir, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if regexp.MustCompile(`direction: "response"`).MatchString(m[4]) {
				continue
			}
			id, _ := strconv.ParseUint(m[2], 0, 32)
			want := schema.PrivilegeOperate
			switch {
			case regexp.MustCompile(`(^|\s)A(\s|$)`).MatchString(m[3]):
				want = schema.PrivilegeAdminister
			case regexp.MustCompile(`(^|\s)M(\s|$)`).MatchString(m[3]):
				want = schema.PrivilegeManage
			}
			if got := schema.InvokePrivilege(cluster, uint32(id)); got != want {
				t.Errorf("%s 0x%04X/%s (access %q): InvokePrivilege = %d, want %d", file, cluster, m[1], m[3], got, want)
			}
			checked++
		}
	}
	if checked < 60 {
		t.Fatalf("checked only %d commands; the element-file pattern no longer matches matter.js", checked)
	}
}
