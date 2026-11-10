// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestDiagnoseOperationalBrowse: the finding needs a waiting node and no
// discovered one.
func TestDiagnoseOperationalBrowse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		commissioned, waiting, discovered int
		want                              bool
	}{
		{0, 0, 0, false},
		{2, 0, 0, false},
		{2, 1, 1, false},
		{2, 1, 0, true},
		{3, 3, 0, true},
	} {
		got := mdns.DiagnoseOperationalBrowse(tc.commissioned, tc.waiting, tc.discovered)
		if (len(got) == 1) != tc.want || len(got) > 1 {
			t.Fatalf("%+v: %+v", tc, got)
		}
		if tc.want {
			f := got[0]
			if f.Code != mdns.CodeNoOperationalNames || f.Severity != mdns.SeverityWarning || f.Service != "" ||
				!strings.HasPrefix(f.Message, "None of the "+strconv.Itoa(tc.commissioned)+" commissioned nodes") {
				t.Fatalf("%+v: finding %+v", tc, f)
			}
		}
	}
}
