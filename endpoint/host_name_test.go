// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"strings"
	"testing"
)

// TestHostName: the host's name is served until a label is written, is
// capped, persists nothing, and the next assembly's Spec takes over.
func TestHostName(t *testing.T) {
	t.Parallel()
	var written []string
	e := &Endpoint{FriendlyName: "spec", onNodeLabelWritten: func(_ SourceKey, l string) { written = append(written, l) }}
	if got := e.NodeLabel(); got != "spec" {
		t.Fatalf("NodeLabel %q", got)
	}
	if !e.SetHostName("host") || e.NodeLabel() != "host" {
		t.Errorf("host name not served: %q", e.NodeLabel())
	}
	if len(written) != 0 {
		t.Errorf("a host name was handed to the persist hook: %v", written)
	}
	e.SetNodeLabel("written")
	if e.SetHostName("later") || e.NodeLabel() != "written" {
		t.Errorf("a host name won over a written label: %q", e.NodeLabel())
	}
	long := strings.Repeat("x", 40)
	f := &Endpoint{}
	f.SetHostName(long)
	if got := f.NodeLabel(); len(got) != labelMaxBytes {
		t.Errorf("host name not capped: %d bytes", len(got))
	}
	f.endpointState().clearHostName()
	if f.NodeLabel() != "" {
		t.Errorf("cleared host name still served: %q", f.NodeLabel())
	}
	if labelMaxBytes != 32 {
		t.Errorf("labelMaxBytes %d from the generated constraint, want 32", labelMaxBytes)
	}
	if got := ComposeNodeLabel("a", "b"); got != "a (b)" {
		t.Errorf("ComposeNodeLabel %q", got)
	}
}
