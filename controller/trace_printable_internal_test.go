// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
)

// TestTraceEscapesControlCharacters (review II, R2-P6-sec-2311): a string
// a LAN host or a device controls — here a _meshcop network name with a
// newline and an ESC sequence, and an invalid byte — enters the trace
// escaped, in the message and in the detail. Before, it entered as it
// came.
func TestTraceEscapesControlCharacters(t *testing.T) {
	t.Parallel()
	c, _ := traced()
	hostile := "Lab\nFAKE: pairing ok\x1b[2J\xff"
	c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Thread border router "+hostile+" appeared.", map[string]string{"network_name": hostile})
	e := only(t, c)
	for what, s := range map[string]string{"message": e.Message, "detail": e.Detail["network_name"]} {
		if strings.ContainsAny(s, "\n\x1b") || strings.Contains(s, "\xff") {
			t.Fatalf("the %s carries a control character or an invalid byte: %q", what, s)
		}
		if !strings.Contains(s, `Lab\nFAKE: pairing ok\x1b[2J\xff`) {
			t.Fatalf("the %s %q does not show the escaped name", what, s)
		}
	}
	c.record(diagevent.KindDiscovery, diagevent.SeverityInfo, "Thread border router Küche (Wohnung) appeared.", nil)
	kept := false
	for _, e := range c.DiagnosticEvents() {
		kept = kept || e.Message == "Thread border router Küche (Wohnung) appeared."
	}
	if !kept {
		t.Fatalf("a printable message changed: %+v", c.DiagnosticEvents())
	}
}
